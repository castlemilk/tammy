import { randomBytes } from "node:crypto";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import {
  type BasCalculation,
  BasExportEventInputSchema,
  type BasExportEventSummary,
  BasExportFormat,
  BasExportOutcome,
  BasExportPhase,
  type BasExportTarget,
  BasExportTargetSchema,
  type BasFrozenBas,
  GetBasCalculationRequestSchema,
  GetBasPreparationRequestSchema,
  GetFrozenBasRequestSchema,
  ListBasExportEventsRequestSchema,
  RecordBasExportEventRequestSchema,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import {
  type AuthenticationContext,
  AuthenticationContextSchema,
  CommandContextSchema,
  PageRequestSchema,
  type SourceRef,
  SourceRefSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  type BasPreparationReportSnapshot,
  reportOrganisationFromCalculation,
  validateBasPreparationReportSnapshot,
} from "@tammy/reporting";
import type { BasPreparationExportRequest } from "../shared/report-export";
import type { CoreClient } from "./core-client";
import { type BasExportDiagnosticStage, safeBasExportFailure } from "./startup-diagnostics";

type BasPreparationClient = NonNullable<CoreClient["basPreparation"]>;
type Guard = () => void;

export type ExactBasTarget = BasPreparationReportSnapshot["target"];

export interface BasPreparationExportOperation {
  readonly authentication: AuthenticationContext;
  readonly organisationId: string;
  readonly target: ExactBasTarget;
  readonly format: "pdf" | "csv";
  started: boolean;
  terminal?: boolean;
  artifactHash?: Uint8Array;
  guard?: Guard;
  failureDiagnosticLogged?: boolean;
}

export function logBasExportFailure(
  operation: BasPreparationExportOperation,
  stage: BasExportDiagnosticStage,
  error: unknown,
) {
  if (operation.failureDiagnosticLogged) return;
  operation.failureDiagnosticLogged = true;
  console.error(safeBasExportFailure(stage, error));
}

type BasSnapshotDiagnosticStage = Extract<
  BasExportDiagnosticStage,
  "FROZEN_READ" | "CALCULATION_READ" | "PREPARATION_READ" | "SNAPSHOT_VALIDATION"
>;

type SnapshotFailureReporter = (stage: BasSnapshotDiagnosticStage, error: unknown) => void;

async function withSnapshotDiagnostic<T>(
  stage: BasSnapshotDiagnosticStage,
  reportFailure: SnapshotFailureReporter,
  operation: () => T | Promise<T>,
): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    try {
      reportFailure(stage, error);
    } catch {
      // Diagnostic output must not replace the original read or validation error.
    }
    throw error;
  }
}

function copyBytes(value: Uint8Array) {
  return new Uint8Array(value);
}

function sameBytes(left: Uint8Array, right: Uint8Array) {
  return left.length === right.length && left.every((byte, index) => byte === right[index]);
}

function uuidV7(now: Date) {
  const bytes = randomBytes(16);
  const milliseconds = BigInt(now.getTime());
  for (let index = 5; index >= 0; index--) {
    bytes[index] = Number((milliseconds >> BigInt((5 - index) * 8)) & 0xffn);
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = bytes.toString("hex");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export function basPreparationTargetRef(target: ExactBasTarget): BasExportTarget {
  const value = create(SourceRefSchema, {
    type: target.kind === "calculation" ? "bas_calculation" : "bas_frozen",
    id: target.id,
    revision: target.revision,
    contentHash: copyBytes(target.hash),
  });
  return create(BasExportTargetSchema, {
    exactTarget:
      target.kind === "calculation" ? { case: "calculation", value } : { case: "frozenBas", value },
  });
}

async function getCalculation(
  client: BasPreparationClient,
  authentication: AuthenticationContext,
  organisationId: string,
  source: SourceRef,
  guard: Guard,
) {
  if (source.type !== "bas_calculation" || source.contentHash.length !== 32)
    throw new Error("REPORT_REFRESH_REQUIRED");
  const response = await client.getBasCalculation(
    create(GetBasCalculationRequestSchema, {
      authentication,
      organisationId,
      calculationId: source.id,
      calculationHash: copyBytes(source.contentHash),
    }),
  );
  guard();
  const calculation = response.calculation;
  if (
    !calculation ||
    calculation.calculationId !== source.id ||
    calculation.organisationId !== organisationId ||
    calculation.version !== source.revision ||
    !sameBytes(calculation.contentHash, source.contentHash)
  )
    throw new Error("REPORT_REFRESH_REQUIRED");
  return structuredClone(calculation);
}

async function getPreparation(
  client: BasPreparationClient,
  authentication: AuthenticationContext,
  organisationId: string,
  source: SourceRef,
  guard: Guard,
) {
  if (source.type !== "bas_preparation" || source.contentHash.length !== 32)
    throw new Error("REPORT_REFRESH_REQUIRED");
  const response = await client.getBasPreparation(
    create(GetBasPreparationRequestSchema, {
      authentication,
      organisationId,
      preparationId: source.id,
      preparationHash: copyBytes(source.contentHash),
    }),
  );
  guard();
  const preparation = response.preparation;
  if (
    !preparation ||
    preparation.preparationId !== source.id ||
    preparation.organisationId !== organisationId ||
    preparation.version !== source.revision ||
    !sameBytes(preparation.contentHash, source.contentHash)
  )
    throw new Error("REPORT_REFRESH_REQUIRED");
  return {
    preparation: structuredClone(preparation),
    currentCalculation: response.currentCalculation,
  };
}

async function getFrozenBas(
  client: BasPreparationClient,
  authentication: AuthenticationContext,
  organisationId: string,
  target: ExactBasTarget,
  guard: Guard,
) {
  const response = await client.getFrozenBas(
    create(GetFrozenBasRequestSchema, {
      authentication,
      organisationId,
      frozenId: target.id,
      frozenHash: copyBytes(target.hash),
    }),
  );
  guard();
  const frozen = response.frozenBas;
  if (
    !frozen ||
    frozen.frozenId !== target.id ||
    frozen.organisationId !== organisationId ||
    !sameBytes(frozen.contentHash, target.hash) ||
    !frozen.calculationSnapshot ||
    !frozen.calculation ||
    frozen.calculationSnapshot.calculationId !== frozen.calculation.id ||
    frozen.calculationSnapshot.version !== frozen.calculation.revision ||
    !sameBytes(frozen.calculationSnapshot.contentHash, frozen.calculation.contentHash)
  )
    throw new Error("REPORT_REFRESH_REQUIRED");
  return structuredClone(frozen);
}

export async function loadBasPreparationReportSnapshot(
  client: BasPreparationClient,
  request: BasPreparationExportRequest,
  guard: Guard,
  acquiredAt: string,
  reportFailure: SnapshotFailureReporter,
): Promise<BasPreparationReportSnapshot> {
  const authentication = create(AuthenticationContextSchema, {
    actorUserId: request.authentication.actorUserId,
    sessionId: request.authentication.sessionId,
  });
  const target: ExactBasTarget = {
    kind: request.target.kind,
    id: request.target.id,
    revision: BigInt(request.target.revision),
    hash: Uint8Array.from(request.target.hash.match(/.{2}/gu) ?? [], (byte) =>
      Number.parseInt(byte, 16),
    ),
  };
  let frozenBas: BasFrozenBas | undefined;
  let calculation: BasCalculation;
  if (target.kind === "frozen_bas") {
    const frozenResult = await withSnapshotDiagnostic("FROZEN_READ", reportFailure, async () => {
      const frozen = await getFrozenBas(
        client,
        authentication,
        request.organisationId,
        target,
        guard,
      );
      const calculationSnapshot = frozen.calculationSnapshot;
      if (!calculationSnapshot) throw new Error("REPORT_REFRESH_REQUIRED");
      const frozenCalculation = structuredClone(calculationSnapshot);
      if (frozenCalculation.organisationId !== request.organisationId)
        throw new Error("REPORT_REFRESH_REQUIRED");
      return { frozen, calculation: frozenCalculation };
    });
    frozenBas = frozenResult.frozen;
    calculation = frozenResult.calculation;
  } else {
    const source = create(SourceRefSchema, {
      type: "bas_calculation",
      id: target.id,
      revision: target.revision,
      contentHash: copyBytes(target.hash),
    });
    calculation = await withSnapshotDiagnostic("CALCULATION_READ", reportFailure, () =>
      getCalculation(client, authentication, request.organisationId, source, guard),
    );
  }

  const currentPreparation = await withSnapshotDiagnostic("PREPARATION_READ", reportFailure, () => {
    if (calculation.preparation?.type !== "bas_preparation")
      throw new Error("REPORT_REFRESH_REQUIRED");
    return getPreparation(
      client,
      authentication,
      request.organisationId,
      calculation.preparation,
      guard,
    );
  });
  let predecessor: BasPreparationReportSnapshot["predecessor"];
  const predecessorRef = currentPreparation.preparation.predecessor;
  if (predecessorRef) {
    const prior = await withSnapshotDiagnostic("PREPARATION_READ", reportFailure, () =>
      getPreparation(client, authentication, request.organisationId, predecessorRef, guard),
    );
    const priorCalculationRef =
      prior.preparation.currentCalculation ??
      (prior.currentCalculation
        ? create(SourceRefSchema, {
            type: "bas_calculation",
            id: prior.currentCalculation.calculationId,
            revision: prior.currentCalculation.version,
            contentHash: copyBytes(prior.currentCalculation.contentHash),
          })
        : undefined);
    const priorCalculation = priorCalculationRef
      ? await withSnapshotDiagnostic("CALCULATION_READ", reportFailure, () =>
          getCalculation(
            client,
            authentication,
            request.organisationId,
            priorCalculationRef,
            guard,
          ),
        )
      : undefined;
    predecessor = {
      preparation: prior.preparation,
      ...(priorCalculation ? { calculation: priorCalculation } : {}),
    };
  }
  return withSnapshotDiagnostic("SNAPSHOT_VALIDATION", reportFailure, () => {
    const snapshot: BasPreparationReportSnapshot = {
      kind: "bas_preparation_report_v1",
      target,
      organisation: reportOrganisationFromCalculation(calculation),
      acquiredAt,
      calculation,
      ...(frozenBas ? { frozenBas } : {}),
      preparation: currentPreparation.preparation,
      ...(predecessor ? { predecessor } : {}),
    };
    validateBasPreparationReportSnapshot(snapshot);
    return structuredClone(snapshot);
  });
}

interface ExportEventHistory {
  readonly count: bigint;
  readonly latest?: BasExportEventSummary;
}

function historyTargetMatches(summary: BasExportEventSummary, target: ExactBasTarget) {
  const exact = summary.target?.exactTarget;
  const kindMatches =
    target.kind === "calculation" ? exact?.case === "calculation" : exact?.case === "frozenBas";
  if (!kindMatches || !exact || exact.case === undefined) return false;
  const reference = exact.value;
  return (
    reference.type === (target.kind === "calculation" ? "bas_calculation" : "bas_frozen") &&
    reference.id === target.id &&
    reference.revision === target.revision &&
    sameBytes(reference.contentHash, target.hash)
  );
}

async function readEventHistory(
  client: BasPreparationClient,
  operation: BasPreparationExportOperation,
  guard: Guard,
): Promise<ExportEventHistory> {
  try {
    let cursor: string | undefined;
    let count = 0n;
    let latest: BasExportEventSummary | undefined;
    const seen = new Set<string>();
    do {
      const response = await client.listBasExportEvents(
        create(ListBasExportEventsRequestSchema, {
          authentication: operation.authentication,
          organisationId: operation.organisationId,
          target: basPreparationTargetRef(operation.target),
          page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
        }),
      );
      guard();
      if (!response.page || response.page.returnedCount !== response.events.length)
        throw new Error("REPORT_SAVE_FAILED");
      for (const event of response.events) {
        if (!historyTargetMatches(event, operation.target)) throw new Error("REPORT_SAVE_FAILED");
        latest = event;
      }
      count += BigInt(response.events.length);
      if (count > 18446744073709551615n) throw new Error("REPORT_SAVE_FAILED");
      cursor = response.page.nextCursor;
      if (cursor !== undefined) {
        if (!cursor || seen.has(cursor) || seen.size >= 10000)
          throw new Error("REPORT_SAVE_FAILED");
        seen.add(cursor);
      }
    } while (cursor !== undefined);
    return { count, ...(latest ? { latest } : {}) };
  } catch (error) {
    logBasExportFailure(operation, "HISTORY_READ", error);
    throw error;
  }
}

function exportFormatFromSummary(value: BasExportEventSummary): "pdf" | "csv" {
  if (value.format === BasExportFormat.PDF) return "pdf";
  if (value.format === BasExportFormat.CSV) return "csv";
  throw new Error("REPORT_SAVE_FAILED");
}

async function appendBasPreparationExportEvent(
  client: BasPreparationClient,
  operation: BasPreparationExportOperation,
  phase: BasExportPhase,
  outcome: BasExportOutcome | undefined,
  expectedVersion: bigint,
  now: Date,
  guard: Guard,
  recovery = false,
) {
  const event = create(BasExportEventInputSchema, {
    target: basPreparationTargetRef(operation.target),
    format: operation.format === "pdf" ? BasExportFormat.PDF : BasExportFormat.CSV,
    phase,
    ...(outcome === undefined ? {} : { outcome }),
    ...(outcome === BasExportOutcome.SUCCEEDED && operation.artifactHash
      ? { artifactHash: copyBytes(operation.artifactHash) }
      : {}),
    occurredAt: create(TimestampSchema, {
      seconds: BigInt(Math.floor(now.getTime() / 1000)),
      nanos: (now.getTime() % 1000) * 1000000,
    }),
    reason: recovery
      ? "interrupted_before_completion"
      : outcome === BasExportOutcome.CANCELLED
        ? "native_save_dialog_cancelled"
        : outcome === BasExportOutcome.FAILED
          ? "export_failed"
          : "",
  });
  const idempotencyKey = uuidV7(now);
  const request = create(RecordBasExportEventRequestSchema, {
    commandContext: create(CommandContextSchema, {
      idempotencyKey,
      authentication: operation.authentication,
    }),
    organisationId: operation.organisationId,
    expectedVersion,
    event,
  });
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      await client.recordBasExportEvent(request);
      guard();
      return;
    } catch (error) {
      if (attempt === 1) {
        logBasExportFailure(
          operation,
          phase === BasExportPhase.STARTED ? "EVENT_APPEND_STARTED" : "EVENT_APPEND_FINISHED",
          error,
        );
        throw new Error("REPORT_SAVE_FAILED");
      }
    }
  }
}

export async function recordBasPreparationExportEvent(
  client: BasPreparationClient,
  operation: BasPreparationExportOperation,
  phase: BasExportPhase,
  outcome?: BasExportOutcome,
  now = new Date(),
  guard: Guard = () => undefined,
) {
  let history = await readEventHistory(client, operation, guard);
  if (phase === BasExportPhase.STARTED && history.latest?.phase === BasExportPhase.STARTED) {
    const recoveredOperation: BasPreparationExportOperation = {
      ...operation,
      format: exportFormatFromSummary(history.latest),
    };
    try {
      await appendBasPreparationExportEvent(
        client,
        recoveredOperation,
        BasExportPhase.FINISHED,
        BasExportOutcome.FAILED,
        history.count,
        now,
        guard,
        true,
      );
    } catch {
      // Another caller may have completed this exact target while we were
      // recovering it. Re-read the immutable history before deciding whether
      // a new STARTED is safe.
    }
    history = await readEventHistory(client, operation, guard);
    if (history.latest?.phase === BasExportPhase.STARTED) throw new Error("REPORT_SAVE_FAILED");
  }
  if (
    phase === BasExportPhase.FINISHED &&
    (history.latest?.phase !== BasExportPhase.STARTED ||
      exportFormatFromSummary(history.latest) !== operation.format)
  ) {
    throw new Error("REPORT_SAVE_FAILED");
  }
  await appendBasPreparationExportEvent(
    client,
    operation,
    phase,
    outcome,
    history.count,
    now,
    guard,
  );
}

export function operationForBasPreparationExport(
  request: BasPreparationExportRequest,
): BasPreparationExportOperation {
  return {
    authentication: { $typeName: "tammy.v1.AuthenticationContext", ...request.authentication },
    organisationId: request.organisationId,
    target: {
      kind: request.target.kind,
      id: request.target.id,
      revision: BigInt(request.target.revision),
      hash: Uint8Array.from(request.target.hash.match(/.{2}/gu) ?? [], (byte) =>
        Number.parseInt(byte, 16),
      ),
    },
    format: request.format,
    started: false,
  };
}
