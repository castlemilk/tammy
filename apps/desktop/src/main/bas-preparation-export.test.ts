// @vitest-environment node
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readdir, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  BasAmountState,
  type BasCalculation,
  BasCalculationOutcome,
  BasCalculationResultSchema,
  BasCivilDateRangeSchema,
  BasConfirmedPeriodSchema,
  BasCoverageCandidateKind,
  BasCoverageDisposition,
  type BasExportEvent,
  BasExportEventInputSchema,
  BasExportEventSchema,
  BasExportEventSummarySchema,
  BasExportFormat,
  BasExportOutcome,
  BasExportPhase,
  BasExportTargetSchema,
  type BasFrozenBas,
  BasFrozenBasSchema,
  BasLabelTraceSchema,
  BasObligationsProfileSchema,
  BasPaygiInputsSchema,
  BasPaygwInputsSchema,
  BasPeriodConfirmationInputSchema,
  BasPreparationSchema,
  BasRawSourceRefSchema,
  BasSourceCoverageSchema,
  BasSourceProofScope,
  BasSourceRevisionVectorSchema,
  BasTraceOperation,
  type RecordBasExportEventRequest,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import {
  AuthenticationContextSchema,
  MoneySchema,
  SourceRefSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { UserSchema, UserState } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { OrganisationSchema } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  BasSalesSourceLineSchema,
  BasSourceLineSchema,
  BasWorkpaperSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { parse } from "csv-parse/sync";
import { afterEach, expect, it, vi } from "vitest";
import type { BasPreparationExportRequest } from "../shared/report-export";
import { createReportExports } from "./report-export";

const dirs: string[] = [];
afterEach(async () => {
  vi.restoreAllMocks();
  await Promise.all(
    dirs.splice(0).map((directory) => rm(directory, { recursive: true, force: true })),
  );
});

const orgId = "018f0000-0000-7000-8000-000000000004";
const userId = "018f0000-0000-7000-8000-000000000002";
const sessionId = "018f0000-0000-7000-8000-000000000003";
const fixedTime = new Date("2026-10-22T01:23:45.123Z");
const hash = (byte: number) => new Uint8Array(32).fill(byte);
const hashHex = (byte: number) => Buffer.from(hash(byte)).toString("hex");
const id = (number: number) => `018f0000-0000-7000-8000-${String(number).padStart(12, "0")}`;
const date = (day: number) => ({ year: 2026, month: 10, day });
const timestamp = create(TimestampSchema, { seconds: 1791165784n, nanos: 123000000 });

async function pdfText(bytes: Uint8Array) {
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const loading = getDocument({
    data: new Uint8Array(bytes),
    useSystemFonts: false,
    useWorkerFetch: false,
    verbosity: 0,
  });
  const document = await loading.promise;
  try {
    const pages: string[] = [];
    for (let number = 1; number <= document.numPages; number++) {
      const page = await document.getPage(number);
      const content = await page.getTextContent();
      pages.push(content.items.map((item) => ("str" in item ? item.str : "")).join(" "));
      page.cleanup();
    }
    return pages.join("\n");
  } finally {
    await loading.destroy();
  }
}

function ref(type: string, number: number, byte: number, revision = 1n) {
  return create(SourceRefSchema, { type, id: id(number), revision, contentHash: hash(byte) });
}

function fixture(
  targetKind: "calculation" | "frozen_bas" = "calculation",
  withPredecessor = false,
) {
  const family = ref("bas_family", 10, 10);
  const periodRef = ref("bas_period", 11, 11);
  const profileRef = ref("bas_obligations_profile", 12, 12);
  const preparationRef = ref("bas_preparation", 13, 13, 2n);
  const workpaperRef = ref("bas_workpaper", 14, 14);
  const period = create(BasCivilDateRangeSchema, { startDate: date(1), endDate: date(31) });
  const confirmation = create(BasPeriodConfirmationInputSchema, { period });
  const periodSnapshot = create(BasConfirmedPeriodSchema, {
    periodId: periodRef.id,
    organisationId: orgId,
    version: periodRef.revision,
    confirmation,
    actorUserId: userId,
    confirmedAt: timestamp,
    contentHash: hash(11),
  });
  const profileSnapshot = create(BasObligationsProfileSchema, {
    profileId: profileRef.id,
    organisationId: orgId,
    revision: profileRef.revision,
    contentHash: hash(12),
    actorUserId: userId,
    recordedAt: timestamp,
  });
  const organisationSnapshot = create(OrganisationSchema, {
    id: orgId,
    version: 3n,
    legalName: "Pinned BAS Entity Pty Ltd",
    displayName: "Pinned BAS Entity",
    abn: "51824753556",
  });
  const workpaperSnapshot = create(BasWorkpaperSchema, {
    id: workpaperRef.id,
    organisationId: orgId,
    version: 1n,
    periodStart: date(1),
    periodEnd: date(31),
    status: 1,
    salesG1: { currencyCode: "AUD", minorUnits: 12345n },
    gstOnSales1a: { currencyCode: "AUD", minorUnits: 1122n },
    gstCredits1b: { currencyCode: "AUD", minorUnits: 0n },
    netGstPayable: { currencyCode: "AUD", minorUnits: 1122n },
    gstBasis: 1,
    createdAt: timestamp,
    sourceFingerprint: hash(44),
    sources: [
      create(BasSourceLineSchema, {
        documentId: id(55),
        supplierName: "Retained vendor name",
        invoiceNumber: "PUR-RETAINED-55",
        documentDate: date(8),
        gross: create(MoneySchema, { currencyCode: "AUD", minorUnits: 1100n }),
        gstCredit: create(MoneySchema, { currencyCode: "AUD", minorUnits: 100n }),
      }),
    ],
    salesSources: [
      create(BasSalesSourceLineSchema, {
        invoiceId: id(56),
        invoiceNumber: "INV-RETAINED-56",
        customerName: "Retained customer name",
        taxDate: date(9),
        gross: create(MoneySchema, { currencyCode: "AUD", minorUnits: 2200n }),
        gst: create(MoneySchema, { currencyCode: "AUD", minorUnits: 200n }),
        sourceRevision: 3n,
        sourceHash: hash(56),
      }),
    ],
  });
  const labels = Array.from({ length: 18 }, (_, index) => {
    const label = index + 1;
    const knownZero = label !== 11;
    return create(BasLabelTraceSchema, {
      label,
      operation: BasTraceOperation.INPUT,
      state: knownZero ? BasAmountState.KNOWN_ZERO : BasAmountState.KNOWN_ZERO,
      ruleId: "test-rule-v1",
      ...(label === 11
        ? { exactRateNumerator: "0", exactRateDenominator: "1" }
        : {
            exactCentsNumerator: "0",
            exactCentsDenominator: "1",
            reportedWholeDollars: 0n,
            roundingDifferenceCentsNumerator: "0",
            roundingDifferenceCentsDenominator: "1",
          }),
    });
  });
  const result = create(BasCalculationResultSchema, {
    outcome: BasCalculationOutcome.COMPLETE,
    label9Direction: 1,
    labels,
  });
  const calculation: BasCalculation = {
    ...({} as BasCalculation),
    $typeName: "tammy.v1.BasCalculation",
    calculationId: id(20),
    organisationId: orgId,
    version: 4n,
    contentHash: hash(20),
    preparation: preparationRef,
    family,
    period: periodRef,
    profile: profileRef,
    centWorkpaper: workpaperRef,
    workpaperSnapshot,
    sourceSnapshotHash: hash(21),
    coverageManifestHash: hash(22),
    revisionVectorHash: hash(23),
    ruleVersion: "rules-v1",
    ruleHash: hash(24),
    calculatorVersion: "calc-v1",
    calculatorHash: hash(25),
    formVersion: "form-v1",
    formHash: hash(26),
    schemaVersion: "schema-v1",
    schemaHash: hash(27),
    descriptorVersion: "descriptor-v1",
    descriptorHash: hash(28),
    result,
    coverage: [],
    findings: [],
    createdAt: timestamp,
    inputRevision: 8n,
    inputHash: hash(29),
    periodSnapshot,
    profileSnapshot,
    paygwInputs: create(BasPaygwInputsSchema, { reviewReason: "Retained withholding answers" }),
    paygiInputs: create(BasPaygiInputsSchema),
    organisationSnapshot,
    organisationIdentity: ref("organisation", 4, 30, 3n),
    organisationSnapshotHash: hash(30),
    revisionVector: create(BasSourceRevisionVectorSchema, { financialRevision: 9n }),
  };
  calculation.coverage.push(
    create(BasSourceCoverageSchema, {
      rawSource: create(BasRawSourceRefSchema, {
        type: "purchase_payment_allocation",
        id: "opaque legacy allocation / alpha",
        originalRevision: 2n,
        originalContentHash: hash(57),
        computedProofHash: hash(58),
        proofScope: BasSourceProofScope.SOURCE_ROW_PROJECTION,
        componentSequence: 7n,
        relatedId: "opaque related payment / beta",
        relatedSequence: 8n,
      }),
      candidateKind: BasCoverageCandidateKind.PURCHASE_SETTLEMENT,
      disposition: BasCoverageDisposition.UNRESOLVED,
      eligibleAmount: create(MoneySchema, { currencyCode: "AUD", minorUnits: 500n }),
      taxDate: date(10),
      lineage: [ref("purchase_posting", 57, 59)],
      evidence: [ref("purchase_document", 58, 60)],
      classificationCode: "PURCHASE_REVIEW_REQUIRED",
      reason: "Retained opaque allocation requires review.",
    }),
  );
  const predecessorPreparationRef = withPredecessor
    ? ref("bas_preparation", 15, 15, 1n)
    : undefined;
  const predecessorLabels = calculation.result!.labels.map((trace) => structuredClone(trace));
  if (withPredecessor) {
    predecessorLabels[0] = create(BasLabelTraceSchema, {
      label: 1,
      operation: BasTraceOperation.INPUT,
      state: BasAmountState.KNOWN,
      ruleId: "prior-rule-v1",
      exactCentsNumerator: "100",
      exactCentsDenominator: "1",
      reportedWholeDollars: 1n,
      roundingDifferenceCentsNumerator: "0",
      roundingDifferenceCentsDenominator: "1",
    });
  }
  const predecessorCalculation: BasCalculation | undefined =
    withPredecessor && predecessorPreparationRef
      ? {
          ...structuredClone(calculation),
          calculationId: id(16),
          version: 3n,
          contentHash: hash(16),
          preparation: predecessorPreparationRef,
          result: create(BasCalculationResultSchema, {
            outcome: BasCalculationOutcome.COMPLETE,
            label9Direction: 1,
            labels: predecessorLabels,
          }),
        }
      : undefined;
  const predecessorCalculationRef = predecessorCalculation
    ? ref("bas_calculation", 16, 16, predecessorCalculation.version)
    : undefined;
  const predecessorPreparation =
    predecessorPreparationRef && predecessorCalculationRef
      ? create(BasPreparationSchema, {
          preparationId: predecessorPreparationRef.id,
          organisationId: orgId,
          version: predecessorPreparationRef.revision,
          family,
          period: periodRef,
          profile: profileRef,
          state: 1,
          currentCalculation: predecessorCalculationRef,
          disputeOverlay: 1,
          sourceSnapshotHash: hash(31),
          createdAt: timestamp,
          updatedAt: timestamp,
          contentHash: predecessorPreparationRef.contentHash,
          inputRevision: 7n,
          inputHash: hash(32),
          creationReason: "Initial preparation: reconcile the October source records.",
        })
      : undefined;
  const preparation = create(BasPreparationSchema, {
    preparationId: preparationRef.id,
    organisationId: orgId,
    version: preparationRef.revision,
    family,
    period: periodRef,
    profile: profileRef,
    state: 1,
    currentCalculation: ref("bas_calculation", 20, 20, 4n),
    ...(predecessorPreparationRef ? { predecessor: predecessorPreparationRef } : {}),
    disputeOverlay: 1,
    sourceSnapshotHash: hash(21),
    createdAt: timestamp,
    updatedAt: timestamp,
    contentHash: preparationRef.contentHash,
    inputRevision: 8n,
    inputHash: hash(29),
    creationReason: withPredecessor
      ? "Linked revision: correct the source evidence recorded in preparation 15."
      : "Initial preparation: begin review of the October reporting period.",
  });
  const frozen: BasFrozenBas = create(BasFrozenBasSchema, {
    frozenId: id(40),
    organisationId: orgId,
    contentHash: hash(40),
    preparation: preparationRef,
    family,
    period: periodRef,
    profile: profileRef,
    calculation: ref("bas_calculation", 20, 20, 4n),
    validation: ref("bas_validation", 41, 41),
    calculationSnapshot: calculation,
    approvedSnapshotHash: hash(42),
    approvalActorUserId: userId,
    freshFactorAssertionId: id(43),
    approvedAt: timestamp,
    approvalReason: "Manual handoff approved after review",
  });
  const target =
    targetKind === "calculation"
      ? {
          kind: "calculation" as const,
          id: calculation.calculationId,
          revision: String(calculation.version),
          hash: hashHex(20),
        }
      : {
          kind: "frozen_bas" as const,
          id: frozen.frozenId,
          revision: "1" as const,
          hash: hashHex(40),
        };
  const exportEvents: BasExportEvent[] = [];
  const exportSummaries = () =>
    exportEvents.map((event) =>
      create(BasExportEventSummarySchema, {
        ...(event.event?.target ? { target: event.event.target } : {}),
        ...(event.event?.format === undefined ? {} : { format: event.event.format }),
        ...(event.event?.phase === undefined ? {} : { phase: event.event.phase }),
        ...(event.event?.outcome === undefined ? {} : { outcome: event.event.outcome }),
      }),
    );
  const basPreparation = {
    getBasCalculation: vi.fn(async (request: { readonly calculationId: string }) => {
      if (request.calculationId === calculation.calculationId) return { calculation };
      expect(request.calculationId).toBe(predecessorCalculation?.calculationId);
      return { calculation: predecessorCalculation };
    }),
    getFrozenBas: vi.fn(async (request: { readonly frozenId: string }) => {
      expect(request.frozenId).toBe(frozen.frozenId);
      return { frozenBas: frozen };
    }),
    getBasPreparation: vi.fn(async (request: { readonly preparationId: string }) => {
      if (request.preparationId === preparation.preparationId) return { preparation };
      expect(request.preparationId).toBe(predecessorPreparation?.preparationId);
      return { preparation: predecessorPreparation };
    }),
    listBasExportEvents: vi.fn(async () => ({
      events: exportSummaries(),
      page: { returnedCount: exportEvents.length },
    })),
    recordBasExportEvent: vi.fn(
      async (request: {
        readonly expectedVersion: bigint;
        readonly event?: BasExportEvent["event"];
      }) => {
        expect(request.expectedVersion).toBe(BigInt(exportEvents.length));
        if (request.event)
          exportEvents.push(create(BasExportEventSchema, { event: request.event }));
        return {};
      },
    ),
  };
  return {
    calculation,
    preparation,
    frozen,
    target,
    basPreparation,
    exportEvents,
    predecessorCalculation,
    predecessorPreparation,
  };
}

async function exporterFor(
  value: ReturnType<typeof fixture>,
  options: {
    readonly format?: "pdf" | "csv";
    readonly canceled?: boolean;
    readonly invalidDestination?: boolean;
    readonly render?: (snapshot: unknown, format: "pdf" | "csv") => Promise<Uint8Array>;
  } = {},
) {
  const directory = await mkdtemp(path.join(tmpdir(), "tammy-bas-preparation-export-"));
  dirs.push(directory);
  const pathValue = path.join(directory, `bas.${options.format ?? "csv"}`);
  if (options.invalidDestination) await mkdir(pathValue);
  const client = {
    basPreparation: value.basPreparation,
    getCurrentUser: vi.fn(async () => ({
      user: create(UserSchema, { id: userId, state: UserState.ACTIVE }),
    })),
    getOrganisation: vi.fn(async () => {
      throw new Error("must use pinned organisation snapshot");
    }),
    getBusinessIncomeSummary: vi.fn(async () => ({})),
    getCurrentBasDraft: vi.fn(async () => ({})),
  };
  const rendered: unknown[] = [];
  const renderBasPreparation = options.render;
  const exporter = createReportExports({
    client: client as never,
    capture: () => () => undefined,
    select: async () => ({ canceled: options.canceled ?? false, filePath: pathValue }),
    now: () => new Date(fixedTime),
    ...(renderBasPreparation
      ? {
          renderBasPreparation: async (snapshot, format) => {
            rendered.push(snapshot);
            return renderBasPreparation(snapshot, format);
          },
        }
      : {}),
  });
  const request: BasPreparationExportRequest = {
    authentication: { actorUserId: userId, sessionId },
    organisationId: orgId,
    target: value.target,
    format: options.format ?? "csv",
  };
  return { exporter, request, client, path: pathValue, directory, rendered };
}

it("exports the exact calculation as rectangular CSV and persists hashed STARTED/SUCCEEDED events", async () => {
  const value = fixture();
  const f = await exporterFor(value);
  await expect(f.exporter.exportBasPreparation(f.request)).resolves.toEqual({ saved: true });
  expect(f.client.getOrganisation).not.toHaveBeenCalled();
  expect(value.basPreparation.getBasCalculation).toHaveBeenCalledTimes(1);
  expect(value.exportEvents.map((event) => event.event?.phase)).toEqual([
    BasExportPhase.STARTED,
    BasExportPhase.FINISHED,
  ]);
  expect(value.exportEvents.map((event) => event.event?.outcome)).toEqual([
    undefined,
    BasExportOutcome.SUCCEEDED,
  ]);
  expect(
    value.exportEvents.every((event) => event.event?.target?.exactTarget.case === "calculation"),
  ).toBe(true);
  const bytes = await readFile(f.path);
  const records = parse(bytes.toString("utf8"), { columns: true, skip_empty_lines: true }) as {
    readonly record_type: string;
    readonly field: string;
    readonly value: string;
    readonly record_json: string;
    readonly label: string;
    readonly exact_cents_numerator: string;
    readonly target_hash: string;
  }[];
  expect(
    records.filter((row: { record_type: string }) => row.record_type === "label"),
  ).toHaveLength(18);
  expect(
    records.find((row: { field: string }) => row.field === "organisation_legal_name")!.value,
  ).toBe("Pinned BAS Entity Pty Ltd");
  expect(records.find((row: { label: string }) => row.label === "G1")!.exact_cents_numerator).toBe(
    "0",
  );
  expect(records.find((row) => row.field === "target_hash")!.value).toBe(hashHex(20));
  const labels = records.filter((row) => row.record_type === "label");
  expect(labels.map((row) => row.label)).toEqual([
    "G1",
    "1A",
    "1B",
    "W1",
    "W2",
    "W3",
    "W4",
    "W5",
    "4",
    "T1",
    "T2",
    "T7",
    "T11",
    "5A",
    "5B",
    "8A",
    "8B",
    "9",
  ]);
  expect(records.find((row) => row.record_type === "coverage")).toMatchObject({
    source_id: "opaque legacy allocation / alpha",
    component_sequence: "7",
    related_source_id: "opaque related payment / beta",
    related_sequence: "8",
  });
  expect(records.find((row) => row.record_type === "sales_source")?.record_json).toContain(
    "Retained customer name",
  );
  expect(
    records.find((row) => row.record_type === "legacy_purchase_source")?.record_json,
  ).toContain("Retained vendor name");
  expect(records.find((row) => row.field === "paygw_inputs")?.record_json).toContain(
    "Retained withholding answers",
  );
  expect(records.every((row) => Object.keys(row).length === 48)).toBe(true);
  expect(Buffer.from(value.exportEvents[1]?.event?.artifactHash ?? []).toString("hex")).toBe(
    createHash("sha256").update(bytes).digest("hex"),
  );
  expect(value.exportEvents[1]?.event?.format).toBe(BasExportFormat.CSV);
  expect(await readdir(f.directory)).toEqual(["bas.csv"]);
});

it("exports the exact predecessor label comparison when that retained version is available", async () => {
  for (const format of ["csv", "pdf"] as const) {
    const value = fixture("calculation", true);
    const f = await exporterFor(value, { format });
    await expect(f.exporter.exportBasPreparation(f.request)).resolves.toEqual({ saved: true });
    expect(value.basPreparation.getBasPreparation).toHaveBeenCalledTimes(2);
    expect(value.basPreparation.getBasCalculation).toHaveBeenCalledTimes(2);
    const body = await readFile(f.path);
    if (format === "csv") {
      const records = parse(body.toString("utf8"), { columns: true, skip_empty_lines: true }) as {
        readonly record_type: string;
        readonly label: string;
        readonly value: string;
        readonly reason: string;
      }[];
      const deltas = records.filter((row) => row.record_type === "label_delta");
      expect(deltas).toHaveLength(18);
      expect(deltas.find((row) => row.label === "G1")).toMatchObject({
        value: "'-100/1 cents",
        reason: "predecessor=KNOWN: 100/1 cents; current=KNOWN_ZERO: 0/1 cents",
      });
    } else {
      const text = await pdfText(body);
      expect(text).toContain("Exact predecessor calculation comparison");
      expect(text).toContain("-100/1 cents");
      expect(text).toContain("KNOWN: 100/1 cents");
    }
  }
}, 30_000);

it("retains linked preparation reasons in PDF and rectangular CSV", async () => {
  const priorReason = "Initial preparation: reconcile the October source records.";
  const currentReason = "Linked revision: correct the source evidence recorded in preparation 15.";
  for (const format of ["csv", "pdf"] as const) {
    const value = fixture("calculation", true);
    const f = await exporterFor(value, { format });
    await expect(f.exporter.exportBasPreparation(f.request)).resolves.toEqual({ saved: true });
    const body = await readFile(f.path);
    if (format === "csv") {
      const records = parse(body.toString("utf8"), { columns: true, skip_empty_lines: true }) as {
        readonly record_type: string;
        readonly field: string;
        readonly value: string;
        readonly reason: string;
        readonly record_json: string;
      }[];
      expect(records.find((row) => row.field === "preparation_creation_reason")).toMatchObject({
        value: currentReason,
        record_json: expect.stringContaining(currentReason),
      });
      expect(
        records.find((row) => row.field === "predecessor_preparation_creation_reason"),
      ).toMatchObject({
        value: priorReason,
        record_json: expect.stringContaining(priorReason),
      });
      expect(records.find((row) => row.record_type === "preparation")?.reason).toBe(currentReason);
      expect(records.find((row) => row.record_type === "predecessor_preparation")?.reason).toBe(
        priorReason,
      );
    } else {
      const text = await pdfText(body);
      expect(text).toContain("Creation reason");
      expect(text).toContain(currentReason);
      expect(text).toContain("Prior creation reason");
      expect(text).toContain(priorReason);
    }
  }
}, 30_000);

it("rejects an over-bound retained preparation creation reason before writing the report", async () => {
  const value = fixture();
  value.preparation.creationReason = "r".repeat(513);
  const f = await exporterFor(value);
  await expect(f.exporter.exportBasPreparation(f.request)).rejects.toThrow(
    "REPORT_REFRESH_REQUIRED",
  );
  expect(await readdir(f.directory)).toEqual([]);
});

it("exports the frozen pinned snapshot as a manual handoff without lodging claims", async () => {
  const value = fixture("frozen_bas");
  const f = await exporterFor(value, { format: "pdf" });
  await expect(f.exporter.exportBasPreparation(f.request)).resolves.toEqual({ saved: true });
  expect(value.basPreparation.getFrozenBas).toHaveBeenCalledTimes(1);
  expect(value.basPreparation.getBasCalculation).not.toHaveBeenCalled();
  expect(value.exportEvents[1]?.event?.target?.exactTarget.case).toBe("frozenBas");
  const body = await readFile(f.path);
  expect(body.subarray(0, 5).toString("ascii")).toBe("%PDF-");
  const text = await pdfText(body);
  expect(text).toContain("FROZEN MANUAL HANDOFF");
  expect(text).toContain("NOT LODGED");
  for (const label of [
    "G1",
    "1A",
    "1B",
    "W1",
    "W2",
    "W3",
    "W4",
    "W5",
    "4",
    "T1",
    "T2",
    "T7",
    "T11",
    "5A",
    "5B",
    "8A",
    "8B",
    "9",
  ])
    expect(text).toContain(label);
  expect(text).toContain("exactCentsNumerator");
  expect(text).toContain("opaque legacy allocation / alpha");
  expect(text).toContain("opaque related payment / beta");
  expect(text).toContain("Retained withholding answers");
  expect(text).toContain("Retained customer name");
  expect(text).toContain("Retained vendor name");
  expect(text).toContain("Approved snapshot hash");
  expect(text).toContain(hashHex(42));
  expect(Buffer.from(value.exportEvents[1]?.event?.artifactHash ?? []).toString("hex")).toBe(
    createHash("sha256").update(body).digest("hex"),
  );
}, 30_000);

it("records cancellation for an exact frozen BAS target", async () => {
  const value = fixture("frozen_bas");
  const f = await exporterFor(value, { canceled: true, format: "pdf" });

  await expect(f.exporter.exportBasPreparation(f.request)).resolves.toEqual({ saved: false });

  expect(value.basPreparation.getFrozenBas).toHaveBeenCalledTimes(1);
  expect(value.basPreparation.getBasCalculation).not.toHaveBeenCalled();
  expect(value.exportEvents.map((event) => event.event?.phase)).toEqual([
    BasExportPhase.STARTED,
    BasExportPhase.FINISHED,
  ]);
  expect(value.exportEvents.map((event) => event.event?.outcome)).toEqual([
    undefined,
    BasExportOutcome.CANCELLED,
  ]);
  expect(
    value.exportEvents.every((event) => event.event?.target?.exactTarget.case === "frozenBas"),
  ).toBe(true);
  expect(value.exportEvents[1]?.event?.reason).toBe("native_save_dialog_cancelled");
  expect(await readdir(f.directory)).toEqual([]);
});

it("records cancellation after STARTED without creating a report file", async () => {
  const value = fixture();
  const f = await exporterFor(value, { canceled: true });
  await expect(f.exporter.exportBasPreparation(f.request)).resolves.toEqual({ saved: false });
  expect(value.exportEvents.map((event) => event.event?.phase)).toEqual([
    BasExportPhase.STARTED,
    BasExportPhase.FINISHED,
  ]);
  expect(value.exportEvents[1]?.event?.outcome).toBe(BasExportOutcome.CANCELLED);
  expect(value.exportEvents[1]?.event?.reason).toBe("native_save_dialog_cancelled");
  expect(await readdir(f.directory)).toEqual([]);
});

it("logs only the safe stage and Connect code when export history cannot be read", async () => {
  const value = fixture("frozen_bas");
  value.basPreparation.listBasExportEvents.mockRejectedValue(
    new ConnectError("private path /tmp/evidence password=secret", Code.InvalidArgument),
  );
  const diagnostic = vi.spyOn(console, "error").mockImplementation(() => undefined);
  const f = await exporterFor(value, { format: "pdf", canceled: true });

  await expect(f.exporter.exportBasPreparation(f.request)).rejects.toThrow(
    "REPORT_REFRESH_REQUIRED",
  );

  expect(value.basPreparation.recordBasExportEvent).not.toHaveBeenCalled();
  expect(diagnostic).toHaveBeenCalledTimes(1);
  const output = diagnostic.mock.calls.flat().join(" ");
  expect(output).toContain("LOCAL_ENGINE_CONNECT_INVALID_ARGUMENT");
  expect(output).toContain("LOCAL_ENGINE_BAS_EXPORT_HISTORY_READ");
  expect(output).not.toContain("private path");
  expect(output).not.toContain("password=secret");
  expect(output).not.toContain(f.request.authentication.actorUserId);
  expect(output).not.toContain(f.request.authentication.sessionId);
});

it.each([
  ["frozen read", "FROZEN_READ", "frozen_bas"],
  ["calculation read", "CALCULATION_READ", "calculation"],
  ["preparation read", "PREPARATION_READ", "calculation"],
] as const)(
  "logs only the fixed BAS snapshot stage and Connect code for %s",
  async (_, stage, kind) => {
    const value = fixture(kind);
    const failure = new ConnectError(
      "private snapshot path password=secret",
      Code.DeadlineExceeded,
    );
    if (stage === "FROZEN_READ") value.basPreparation.getFrozenBas.mockRejectedValue(failure);
    if (stage === "CALCULATION_READ")
      value.basPreparation.getBasCalculation.mockRejectedValue(failure);
    if (stage === "PREPARATION_READ")
      value.basPreparation.getBasPreparation.mockRejectedValue(failure);
    const diagnostic = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const f = await exporterFor(value, { format: "pdf", canceled: true });

    await expect(f.exporter.exportBasPreparation(f.request)).rejects.toThrow(
      "REPORT_REFRESH_REQUIRED",
    );

    expect(value.exportEvents).toHaveLength(0);
    expect(diagnostic).toHaveBeenCalledTimes(1);
    expect(diagnostic).toHaveBeenCalledWith(
      `LOCAL_ENGINE_BAS_EXPORT_${stage} LOCAL_ENGINE_CONNECT_DEADLINE_EXCEEDED`,
    );
    const output = diagnostic.mock.calls.flat().join(" ");
    expect(output).not.toContain("private snapshot path");
    expect(output).not.toContain("password=secret");
  },
);

it("classifies report-model rejection without exposing invalid snapshot values", async () => {
  const value = fixture("frozen_bas");
  value.preparation.creationReason = "private-invalid-".repeat(40);
  const diagnostic = vi.spyOn(console, "error").mockImplementation(() => undefined);
  const f = await exporterFor(value, { format: "pdf", canceled: true });

  await expect(f.exporter.exportBasPreparation(f.request)).rejects.toThrow(
    "REPORT_REFRESH_REQUIRED",
  );

  expect(value.exportEvents).toHaveLength(0);
  expect(diagnostic).toHaveBeenCalledTimes(1);
  expect(diagnostic).toHaveBeenCalledWith("LOCAL_ENGINE_BAS_EXPORT_SNAPSHOT_VALIDATION");
  expect(diagnostic.mock.calls.flat().join(" ")).not.toContain("private-invalid");
});

it("logs the exact safe stage when the STARTED event append fails", async () => {
  const value = fixture("frozen_bas");
  value.basPreparation.recordBasExportEvent.mockRejectedValue(
    new ConnectError("private path /tmp/evidence password=secret", Code.InvalidArgument),
  );
  const diagnostic = vi.spyOn(console, "error").mockImplementation(() => undefined);
  const f = await exporterFor(value, { format: "pdf", canceled: true });

  await expect(f.exporter.exportBasPreparation(f.request)).rejects.toThrow("REPORT_SAVE_FAILED");

  expect(value.basPreparation.recordBasExportEvent).toHaveBeenCalledTimes(2);
  expect(value.exportEvents).toHaveLength(0);
  expect(diagnostic).toHaveBeenCalledTimes(1);
  const output = diagnostic.mock.calls.flat().join(" ");
  expect(output).toContain("LOCAL_ENGINE_CONNECT_INVALID_ARGUMENT");
  expect(output).toContain("LOCAL_ENGINE_BAS_EXPORT_EVENT_APPEND_STARTED");
  expect(output).not.toContain("private path");
  expect(output).not.toContain("password=secret");
  expect(output).not.toContain(f.request.authentication.actorUserId);
  expect(output).not.toContain(f.request.authentication.sessionId);
});

it("logs the FINISHED event append stage without exposing renderer diagnostics", async () => {
  const value = fixture("frozen_bas");
  const record = value.basPreparation.recordBasExportEvent;
  record.mockImplementationOnce(async (request) => {
    expect(request.expectedVersion).toBe(0n);
    if (request.event)
      value.exportEvents.push(create(BasExportEventSchema, { event: request.event }));
    return {};
  });
  record.mockRejectedValueOnce(
    new ConnectError("private path /tmp/evidence password=secret", Code.PermissionDenied),
  );
  record.mockRejectedValueOnce(
    new ConnectError("private path /tmp/evidence password=secret", Code.PermissionDenied),
  );
  const diagnostic = vi.spyOn(console, "error").mockImplementation(() => undefined);
  const f = await exporterFor(value, { format: "pdf", canceled: true });

  await expect(f.exporter.exportBasPreparation(f.request)).rejects.toThrow("REPORT_SAVE_FAILED");

  expect(value.exportEvents.map((event) => event.event?.phase)).toEqual([BasExportPhase.STARTED]);
  expect(record).toHaveBeenCalledTimes(3);
  expect(diagnostic).toHaveBeenCalledTimes(1);
  const output = diagnostic.mock.calls.flat().join(" ");
  expect(output).toContain("LOCAL_ENGINE_CONNECT_PERMISSION_DENIED");
  expect(output).toContain("LOCAL_ENGINE_BAS_EXPORT_EVENT_APPEND_FINISHED");
  expect(output).not.toContain("private path");
  expect(output).not.toContain("password=secret");
  expect(output).not.toContain(f.request.authentication.actorUserId);
  expect(output).not.toContain(f.request.authentication.sessionId);
});

it("records render failure safely and leaves no partial output", async () => {
  const value = fixture();
  const diagnostic = vi.spyOn(console, "error").mockImplementation(() => undefined);
  const f = await exporterFor(value, {
    render: async () => {
      throw new Error("private report path and contents");
    },
  });
  await expect(f.exporter.exportBasPreparation(f.request)).rejects.toThrow("REPORT_RENDER_FAILED");
  expect(value.exportEvents[1]?.event?.outcome).toBe(BasExportOutcome.FAILED);
  expect(value.exportEvents[1]?.event?.reason).toBe("export_failed");
  expect(await readdir(f.directory)).toEqual([]);
  expect(diagnostic).toHaveBeenCalledWith("LOCAL_ENGINE_BAS_EXPORT_EXPORT_HANDLER");
  expect(diagnostic).toHaveBeenCalledTimes(1);
  const output = diagnostic.mock.calls.flat().join(" ");
  expect(output).not.toContain("private report path");
  expect(output).not.toContain("private report contents");
});

it("rejects a preparation reference with the right hash but the wrong revision before exporting", async () => {
  const value = fixture();
  const stalePreparation = structuredClone(value.preparation);
  stalePreparation.version += 1n;
  value.basPreparation.getBasPreparation.mockResolvedValue({ preparation: stalePreparation });
  const f = await exporterFor(value);
  await expect(f.exporter.exportBasPreparation(f.request)).rejects.toThrow(
    "REPORT_REFRESH_REQUIRED",
  );
  expect(value.exportEvents).toHaveLength(0);
  expect(await readdir(f.directory)).toEqual([]);
});

it("records native target-save failure and leaves no temporary report behind", async () => {
  const value = fixture();
  const f = await exporterFor(value, { invalidDestination: true });
  await expect(f.exporter.exportBasPreparation(f.request)).rejects.toThrow("REPORT_SAVE_FAILED");
  expect(value.exportEvents.map((event) => event.event?.outcome)).toEqual([
    undefined,
    BasExportOutcome.FAILED,
  ]);
  expect(value.exportEvents[1]?.event?.reason).toBe("export_failed");
  expect(await readdir(f.directory)).toEqual(["bas.csv"]);
  expect(await readdir(f.path)).toEqual([]);
});

it("counts all event pages, including an empty page with a continuation cursor", async () => {
  const value = fixture();
  const operation = {
    authentication: create(AuthenticationContextSchema, { actorUserId: userId, sessionId }),
    organisationId: orgId,
    target: {
      kind: "calculation" as const,
      id: value.calculation.calculationId,
      revision: 4n,
      hash: hash(20),
    },
    format: "csv" as const,
    started: false,
  };
  const existing = create(BasExportEventSchema, {
    exportEventId: id(50),
    organisationId: orgId,
    actorUserId: userId,
    contentHash: hash(50),
    event: create(BasExportEventInputSchema, {
      format: BasExportFormat.CSV,
      phase: BasExportPhase.FINISHED,
      outcome: BasExportOutcome.CANCELLED,
      target: create(BasExportTargetSchema, {
        exactTarget: { case: "calculation", value: ref("bas_calculation", 20, 20, 4n) },
      }),
    }),
  });
  const existingSummary = create(BasExportEventSummarySchema, {
    ...(existing.event?.target ? { target: existing.event.target } : {}),
    ...(existing.event?.format === undefined ? {} : { format: existing.event.format }),
    ...(existing.event?.phase === undefined ? {} : { phase: existing.event.phase }),
    ...(existing.event?.outcome === undefined ? {} : { outcome: existing.event.outcome }),
  });
  const listBasExportEvents = vi
    .fn()
    .mockResolvedValueOnce({ events: [], page: { returnedCount: 0, nextCursor: "later-page" } })
    .mockResolvedValueOnce({ events: [existingSummary], page: { returnedCount: 1 } });
  const recordBasExportEvent = vi.fn(async (_request: RecordBasExportEventRequest) => ({}));
  recordBasExportEvent.mockRejectedValueOnce(new Error("ambiguous write response"));
  const client = { ...value.basPreparation, listBasExportEvents, recordBasExportEvent } as never;
  const { recordBasPreparationExportEvent } = await import("./bas-preparation-export");
  await recordBasPreparationExportEvent(
    client,
    operation,
    BasExportPhase.STARTED,
    undefined,
    fixedTime,
  );
  expect(listBasExportEvents).toHaveBeenCalledTimes(2);
  expect(recordBasExportEvent).toHaveBeenCalledTimes(2);
  expect(recordBasExportEvent.mock.calls[0]?.[0].expectedVersion).toBe(1n);
  expect(recordBasExportEvent.mock.calls[1]?.[0].expectedVersion).toBe(1n);
  expect(recordBasExportEvent.mock.calls[1]?.[0]?.commandContext?.idempotencyKey).toBe(
    recordBasExportEvent.mock.calls[0]?.[0]?.commandContext?.idempotencyKey,
  );
});

it("finishes an interrupted exact-target attempt before starting a different format", async () => {
  const value = fixture();
  const operation = {
    authentication: create(AuthenticationContextSchema, { actorUserId: userId, sessionId }),
    organisationId: orgId,
    target: {
      kind: "calculation" as const,
      id: value.calculation.calculationId,
      revision: 4n,
      hash: hash(20),
    },
    format: "csv" as const,
    started: false,
  };
  const target = create(BasExportTargetSchema, {
    exactTarget: { case: "calculation", value: ref("bas_calculation", 20, 20, 4n) },
  });
  const history = [
    create(BasExportEventSummarySchema, {
      target,
      format: BasExportFormat.PDF,
      phase: BasExportPhase.STARTED,
    }),
  ];
  const requests: RecordBasExportEventRequest[] = [];
  const completedKeys = new Set<string>();
  let loseFirstResponse = true;
  const recordBasExportEvent = vi.fn(async (request: RecordBasExportEventRequest) => {
    requests.push(request);
    const key = request.commandContext?.idempotencyKey ?? "";
    if (!completedKeys.has(key)) {
      expect(request.expectedVersion).toBe(BigInt(history.length));
      const event = request.event;
      if (event)
        history.push(
          create(BasExportEventSummarySchema, {
            ...(event.target ? { target: event.target } : {}),
            ...(event.format === undefined ? {} : { format: event.format }),
            ...(event.phase === undefined ? {} : { phase: event.phase }),
            ...(event.outcome === undefined ? {} : { outcome: event.outcome }),
          }),
        );
      completedKeys.add(key);
      if (loseFirstResponse) {
        loseFirstResponse = false;
        throw new Error("server committed, response lost");
      }
    }
    return {};
  });
  const client = {
    listBasExportEvents: vi.fn(async () => ({
      events: [...history],
      page: { returnedCount: history.length },
    })),
    recordBasExportEvent,
  } as never;
  const { recordBasPreparationExportEvent } = await import("./bas-preparation-export");

  await recordBasPreparationExportEvent(
    client,
    operation,
    BasExportPhase.STARTED,
    undefined,
    fixedTime,
  );

  expect(recordBasExportEvent).toHaveBeenCalledTimes(3);
  const recovery = requests[0]?.event;
  expect(recovery).toMatchObject({
    target,
    format: BasExportFormat.PDF,
    phase: BasExportPhase.FINISHED,
    outcome: BasExportOutcome.FAILED,
    reason: "interrupted_before_completion",
  });
  expect(requests[0]?.expectedVersion).toBe(1n);
  expect(requests[1]?.expectedVersion).toBe(1n);
  expect(requests[1]?.commandContext?.idempotencyKey).toBe(
    requests[0]?.commandContext?.idempotencyKey,
  );
  expect(requests[2]?.expectedVersion).toBe(2n);
  expect(requests[2]?.event).toMatchObject({
    target,
    format: BasExportFormat.CSV,
    phase: BasExportPhase.STARTED,
  });
  expect(history.map((event) => event.phase)).toEqual([
    BasExportPhase.STARTED,
    BasExportPhase.FINISHED,
    BasExportPhase.STARTED,
  ]);
});

it("serializes concurrent exports of the same exact target until the first terminal event", async () => {
  const value = fixture();
  let markRendering!: () => void;
  const rendering = new Promise<void>((resolve) => {
    markRendering = resolve;
  });
  let releaseRendering!: () => void;
  const holdRenderer = new Promise<void>((resolve) => {
    releaseRendering = resolve;
  });
  const f = await exporterFor(value, {
    render: async () => {
      markRendering();
      await holdRenderer;
      return Buffer.from("record_type,value\nmeta,ok\n");
    },
  });

  const first = f.exporter.exportBasPreparation(f.request);
  await rendering;
  const second = f.exporter.exportBasPreparation(f.request);
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(value.exportEvents.map((event) => event.event?.phase)).toEqual([BasExportPhase.STARTED]);

  releaseRendering();
  await expect(Promise.all([first, second])).resolves.toEqual([{ saved: true }, { saved: true }]);
  expect(value.exportEvents.map((event) => event.event?.phase)).toEqual([
    BasExportPhase.STARTED,
    BasExportPhase.FINISHED,
    BasExportPhase.STARTED,
    BasExportPhase.FINISHED,
  ]);
});
