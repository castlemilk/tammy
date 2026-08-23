import { create } from "@bufbuild/protobuf";
import type { CommandContext } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { Role } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  MachineCredentialState,
  ProductIdState,
  RunSbrReadinessFixtureRequestSchema,
  RunSbrReadinessFixtureResponseSchema,
  SbrEnvironment,
  type SbrReadiness,
  SbrReadinessFixtureFailure,
  SbrReadinessFixtureOutcome,
  SbrReadinessState,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { FlaskConical, LoaderCircle, RotateCcw } from "lucide-react";
import { type FormEvent, useEffect, useRef, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { assertFreshFactor, commandContext, fieldClassName, SBR_PURPOSE } from "./sbr-form-support";

export const SBR_SIMULATOR_FIXTURE_ID = "SIM-SBR-READINESS-V1";

const runCodec = createProtoMethodCodec({
  input: RunSbrReadinessFixtureRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: RunSbrReadinessFixtureResponseSchema,
});

const diagnosticCases = [
  {
    label: "Accepted",
    name: "ACCEPTED",
    value: SbrReadinessFixtureFailure.UNSPECIFIED,
  },
  {
    label: "Not started",
    name: "NOT_STARTED",
    value: SbrReadinessFixtureFailure.NOT_STARTED,
  },
  {
    label: "Maybe sent",
    name: "MAYBE_SENT",
    value: SbrReadinessFixtureFailure.MAYBE_SENT,
  },
  {
    label: "Malformed response",
    name: "MALFORMED_RESPONSE",
    value: SbrReadinessFixtureFailure.MALFORMED_RESPONSE,
  },
  {
    label: "Helper death",
    name: "HELPER_DEATH",
    value: SbrReadinessFixtureFailure.HELPER_DEATH,
  },
  {
    label: "Timeout",
    name: "TIMEOUT",
    value: SbrReadinessFixtureFailure.TIMEOUT,
  },
] as const;

type DiagnosticName = (typeof diagnosticCases)[number]["name"];
type ResultName =
  | DiagnosticName
  | "AUTHORIZATION_FAILED"
  | "EXACT_REPLAY"
  | "IDEMPOTENCY_CONFLICT"
  | "UNKNOWN";
type SimulatorAPI = Pick<TammyDesktopAPI, "assertTotp" | "runSbrReadinessFixture">;
type Operation = {
  readonly command: CommandContext;
  readonly failureCase: SbrReadinessFixtureFailure;
};

function caseForName(name: DiagnosticName): (typeof diagnosticCases)[number] {
  return diagnosticCases.find((entry) => entry.name === name) ?? diagnosticCases[0];
}

function validSimulatorReadiness(readiness: SbrReadiness): boolean {
  return (
    readiness.environment === SbrEnvironment.SIMULATOR &&
    readiness.state === SbrReadinessState.READY_FOR_SIMULATOR &&
    readiness.machineCredentialState === MachineCredentialState.PRESENT &&
    readiness.productIdState === ProductIdState.MISSING &&
    readiness.readinessCodes.length === 0 &&
    readiness.evteProductIdentifier === "" &&
    readiness.evteServiceIdentifier === "" &&
    [
      readiness.credentialFingerprint,
      readiness.profileFingerprint,
      readiness.componentFingerprint,
    ].every((fingerprint) => fingerprint.length <= 128)
  );
}

function resultNameForOutcome(outcome: SbrReadinessFixtureOutcome): ResultName | undefined {
  switch (outcome) {
    case SbrReadinessFixtureOutcome.ACCEPTED:
      return "ACCEPTED";
    case SbrReadinessFixtureOutcome.EXACT_REPLAY:
      return "EXACT_REPLAY";
    case SbrReadinessFixtureOutcome.NOT_STARTED:
      return "NOT_STARTED";
    case SbrReadinessFixtureOutcome.MAYBE_SENT:
      return "MAYBE_SENT";
    case SbrReadinessFixtureOutcome.MALFORMED_RESPONSE:
      return "MALFORMED_RESPONSE";
    case SbrReadinessFixtureOutcome.HELPER_DEATH:
      return "HELPER_DEATH";
    case SbrReadinessFixtureOutcome.TIMEOUT:
      return "TIMEOUT";
    case SbrReadinessFixtureOutcome.UNKNOWN:
      return "UNKNOWN";
    case SbrReadinessFixtureOutcome.IDEMPOTENCY_CONFLICT:
      return "IDEMPOTENCY_CONFLICT";
    default:
      return undefined;
  }
}

function resultDetail(result: ResultName | undefined, authoritativeUnknown = false): string {
  switch (result) {
    case "ACCEPTED":
      return "The fixed local fixture completed with its deterministic test receipt.";
    case "EXACT_REPLAY":
      return "Core returned the owned result for the same operation identity without a new factor.";
    case "IDEMPOTENCY_CONFLICT":
      return "Core rejected the same idempotency key with a deliberately altered diagnostic case.";
    case "NOT_STARTED":
      return "The deterministic transport stopped before dispatch.";
    case "MAYBE_SENT":
      return "Dispatch completion is uncertain. The operation is locked until status is refreshed.";
    case "MALFORMED_RESPONSE":
      return "The deterministic helper response was rejected. Refresh status before another run.";
    case "HELPER_DEATH":
      return "The helper ended after dispatch began. Refresh status before another run.";
    case "TIMEOUT":
      return "The bounded helper deadline elapsed. Refresh status before another run.";
    case "UNKNOWN":
      return authoritativeUnknown
        ? "Core reports an unknown outcome after restart recovery. It is never automatically resent."
        : "The local operation outcome is unknown. Refresh status before trying again; it is never automatically resent.";
    case "AUTHORIZATION_FAILED":
      return "The fresh security check failed. No simulator operation was started.";
    default:
      return "Choose a closed diagnostic case and provide a fresh security code.";
  }
}

export function SbrSimulatorPanel({
  api,
  factorEnabled = true,
  onRefresh,
  readiness,
  workspace,
}: {
  readonly api: SimulatorAPI;
  readonly factorEnabled?: boolean;
  readonly onRefresh: () => void;
  readonly readiness: SbrReadiness;
  readonly workspace: AuthenticatedWorkspace & { readonly roles: readonly Role[] };
}) {
  const [selectedCase, setSelectedCase] = useState<DiagnosticName>("ACCEPTED");
  const [totp, setTotp] = useState("");
  const [busy, setBusy] = useState(false);
  const [locked, setLocked] = useState(false);
  const [result, setResult] = useState<ResultName>();
  const [authoritativeUnknown, setAuthoritativeUnknown] = useState(false);
  const [storedOperation, setStoredOperation] = useState<Operation>();
  const operation = useRef<Operation | undefined>(undefined);
  const operationInFlight = useRef(false);
  const operationOwner = useRef<symbol | undefined>(undefined);
  const mounted = useRef(true);
  const generation = useRef(0);
  const simulatorReady =
    readiness.environment === SbrEnvironment.SIMULATOR &&
    readiness.state === SbrReadinessState.READY_FOR_SIMULATOR &&
    readiness.machineCredentialState === MachineCredentialState.PRESENT;
  const authorised = workspace.roles.includes(Role.BUSINESS_LODGER);
  const actionable = simulatorReady && authorised && factorEnabled;
  const actionKey = `${workspace.workspaceId}:${workspace.organisationId}:${workspace.sessionId}:${workspace.userId}:${readiness.environment}:${readiness.state}:${readiness.machineCredentialState}:${authorised ? "lodger" : "no-lodger"}:${factorEnabled ? "factor" : "no-factor"}`;
  const currentAction = useRef(actionKey);
  const previousAction = useRef(actionKey);
  currentAction.current = actionKey;

  useEffect(() => {
    mounted.current = true;
    if (previousAction.current !== actionKey) {
      previousAction.current = actionKey;
      generation.current += 1;
      operation.current = undefined;
      operationInFlight.current = false;
      operationOwner.current = undefined;
      setStoredOperation(undefined);
      setBusy(false);
      setLocked(false);
      setResult(undefined);
      setAuthoritativeUnknown(false);
      setTotp("");
    }
    return () => {
      mounted.current = false;
      generation.current += 1;
    };
  }, [actionKey]);

  const isCurrent = (capturedGeneration: number, capturedAction: string) =>
    mounted.current &&
    generation.current === capturedGeneration &&
    currentAction.current === capturedAction;

  const dispatch = async (
    pendingOperation: Operation,
    mode: "conflict" | "initial" | "replay",
    capturedGeneration: number,
    capturedAction: string,
  ) => {
    let frame: Uint8Array | undefined;
    try {
      const request = create(RunSbrReadinessFixtureRequestSchema, {
        commandContext: pendingOperation.command,
        failureCase: pendingOperation.failureCase,
        fixtureId: SBR_SIMULATOR_FIXTURE_ID,
      });
      frame = runCodec.encodeRequest(request);
      const pending = api.runSbrReadinessFixture(frame);
      const responseFrame = await pending;
      const response = runCodec.decodeResponse(responseFrame);
      const returned = response.result;
      const resultName = returned ? resultNameForOutcome(returned.outcome) : undefined;
      const expectedSucceeded =
        returned?.outcome === SbrReadinessFixtureOutcome.ACCEPTED ||
        (returned?.outcome === SbrReadinessFixtureOutcome.EXACT_REPLAY &&
          pendingOperation.failureCase === SbrReadinessFixtureFailure.UNSPECIFIED);
      if (
        !returned ||
        !resultName ||
        returned.fixtureId !== SBR_SIMULATOR_FIXTURE_ID ||
        returned.failureCase !== pendingOperation.failureCase ||
        returned.succeeded !== expectedSucceeded ||
        !returned.readiness ||
        !validSimulatorReadiness(returned.readiness)
      ) {
        throw new Error("invalid simulator response");
      }
      if (
        mode === "conflict" &&
        returned.outcome !== SbrReadinessFixtureOutcome.IDEMPOTENCY_CONFLICT
      )
        throw new Error("invalid conflict outcome");
      if (mode === "replay" && returned.outcome !== SbrReadinessFixtureOutcome.EXACT_REPLAY)
        throw new Error("invalid replay outcome");
      if (
        mode === "initial" &&
        (returned.outcome === SbrReadinessFixtureOutcome.EXACT_REPLAY ||
          returned.outcome === SbrReadinessFixtureOutcome.IDEMPOTENCY_CONFLICT)
      )
        throw new Error("invalid initial outcome");
      if (!isCurrent(capturedGeneration, capturedAction)) return;
      setAuthoritativeUnknown(returned.outcome === SbrReadinessFixtureOutcome.UNKNOWN);
      setResult(resultName);
      if (
        returned.outcome === SbrReadinessFixtureOutcome.MAYBE_SENT ||
        returned.outcome === SbrReadinessFixtureOutcome.MALFORMED_RESPONSE ||
        returned.outcome === SbrReadinessFixtureOutcome.HELPER_DEATH ||
        returned.outcome === SbrReadinessFixtureOutcome.TIMEOUT ||
        returned.outcome === SbrReadinessFixtureOutcome.UNKNOWN
      ) {
        setLocked(true);
      }
    } catch {
      if (!isCurrent(capturedGeneration, capturedAction)) return;
      setAuthoritativeUnknown(false);
      setLocked(true);
      setResult("UNKNOWN");
    } finally {
      frame?.fill(0);
    }
  };

  const start = async (event: FormEvent) => {
    event.preventDefault();
    if (!actionable || locked || operationInFlight.current || !/^\d{6}$/.test(totp)) return;
    operationInFlight.current = true;
    const owner = Symbol("simulator-operation");
    operationOwner.current = owner;
    setBusy(true);
    setResult(undefined);
    setAuthoritativeUnknown(false);
    const capturedGeneration = generation.current;
    const capturedAction = actionKey;
    try {
      const freshFactor = await assertFreshFactor(api, workspace, totp, SBR_PURPOSE.useCredential);
      if (!isCurrent(capturedGeneration, capturedAction) || !actionable) return;
      setTotp("");
      const pendingOperation: Operation = {
        command: commandContext(workspace, freshFactor),
        failureCase: caseForName(selectedCase).value,
      };
      operation.current = pendingOperation;
      setStoredOperation(pendingOperation);
      await dispatch(pendingOperation, "initial", capturedGeneration, capturedAction);
    } catch {
      if (isCurrent(capturedGeneration, capturedAction)) {
        setTotp("");
        setResult("AUTHORIZATION_FAILED");
      }
    } finally {
      if (operationOwner.current === owner) {
        operationOwner.current = undefined;
        operationInFlight.current = false;
        if (isCurrent(capturedGeneration, capturedAction)) setBusy(false);
      }
    }
  };

  const repeat = async (mode: "conflict" | "replay") => {
    const previous = operation.current;
    if (!previous || !actionable || locked || operationInFlight.current) return;
    const nextCase = caseForName(selectedCase).value;
    if (mode === "conflict" && nextCase === previous.failureCase) return;
    operationInFlight.current = true;
    const owner = Symbol("simulator-repeat");
    operationOwner.current = owner;
    setBusy(true);
    setResult(undefined);
    setAuthoritativeUnknown(false);
    const capturedGeneration = generation.current;
    const capturedAction = actionKey;
    const pendingOperation = mode === "replay" ? previous : { ...previous, failureCase: nextCase };
    try {
      await dispatch(pendingOperation, mode, capturedGeneration, capturedAction);
    } finally {
      if (operationOwner.current === owner) {
        operationOwner.current = undefined;
        operationInFlight.current = false;
        if (isCurrent(capturedGeneration, capturedAction)) setBusy(false);
      }
    }
  };

  const announcement = busy
    ? "Running the local simulator fixture."
    : result
      ? `${result}. ${resultDetail(result, authoritativeUnknown)}`
      : "SBR readiness simulator is idle.";
  const changedCase =
    storedOperation && caseForName(selectedCase).value !== storedOperation.failureCase;

  return (
    <section aria-labelledby="sbr-simulator-heading" className="border-y border-border py-5">
      <p aria-live="polite" className="sr-only" role="status">
        {announcement}
      </p>
      <div className="flex items-start justify-between gap-5">
        <div className="flex min-w-0 gap-3">
          <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-full bg-muted text-forest">
            <FlaskConical aria-hidden="true" className="size-3.5" />
          </span>
          <div>
            <p className="text-[10px] font-semibold uppercase tracking-[0.11em] text-forest">
              Simulator-only diagnostic
            </p>
            <h2 id="sbr-simulator-heading" className="mt-1 text-[13px] font-semibold">
              SBR readiness simulator
            </h2>
            <p className="mt-1 max-w-[620px] text-[10px] leading-5 text-muted-foreground">
              A deterministic, network-disabled check. BAS work stays preparation-only; no live
              accounting record is read or transmitted.
            </p>
          </div>
        </div>
        <span className="shrink-0 border border-border px-2 py-1 text-[9px] font-semibold uppercase tracking-[0.09em] text-muted-foreground">
          Test evidence
        </span>
      </div>

      <dl className="mt-4 grid grid-cols-2 border-t border-border text-[10px] max-[620px]:grid-cols-1">
        <IdentityRow label="Fixture" value={SBR_SIMULATOR_FIXTURE_ID} />
        <IdentityRow label="Organisation" value="Wattle & Co Test Pty Ltd" />
        <IdentityRow label="Simulator ABN" value="ABN 11 000 000 560" />
        <IdentityRow label="Service" value="SIM.READINESS.0001" />
      </dl>

      {readiness.environment === SbrEnvironment.EVTE ? (
        <StatusOnly copy="EVTE status only. Local simulator controls cannot invoke an EVTE component operation." />
      ) : !simulatorReady ? (
        <StatusOnly copy="The simulator is not ready in the current authoritative SBR status." />
      ) : !authorised ? (
        <StatusOnly copy="The business lodger role is required to run this fixed local fixture." />
      ) : !factorEnabled ? (
        <StatusOnly copy="Enable a security factor before running this fixed local fixture." />
      ) : (
        <form className="mt-4 grid gap-3" onSubmit={start}>
          <div className="grid grid-cols-[minmax(0,1fr)_220px] gap-3 max-[620px]:grid-cols-1">
            <label className="grid gap-1.5 text-[10px] font-medium">
              Test-only diagnostic case
              <select
                className={fieldClassName}
                disabled={busy || locked}
                onChange={(event) => setSelectedCase(event.target.value as DiagnosticName)}
                value={selectedCase}
              >
                {diagnosticCases.map((entry) => (
                  <option key={entry.name} value={entry.name}>
                    {entry.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="grid gap-1.5 text-[10px] font-medium">
              Fresh six-digit code
              <input
                autoComplete="one-time-code"
                className={fieldClassName}
                disabled={busy || locked}
                inputMode="numeric"
                maxLength={6}
                onChange={(event) => setTotp(event.target.value)}
                pattern="[0-9]{6}"
                required
                value={totp}
              />
            </label>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button className="h-9 text-[11px]" disabled={busy || locked} type="submit">
              {busy ? <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" /> : null}
              Run simulator fixture
            </Button>
            {storedOperation ? (
              <>
                <Button
                  className="h-9 text-[11px]"
                  disabled={busy || locked}
                  onClick={() => void repeat("replay")}
                  type="button"
                  variant="outline"
                >
                  Replay exact request
                </Button>
                <Button
                  className="h-9 text-[11px]"
                  disabled={busy || locked || !changedCase}
                  onClick={() => void repeat("conflict")}
                  type="button"
                  variant="ghost"
                >
                  Check idempotency conflict
                </Button>
              </>
            ) : null}
            {locked ? (
              <Button
                className="h-9 text-[11px]"
                onClick={onRefresh}
                type="button"
                variant="outline"
              >
                <RotateCcw aria-hidden="true" className="size-3" />
                Refresh authoritative status
              </Button>
            ) : null}
          </div>
          <div className="min-h-10 border-l border-border pl-3">
            {result ? (
              <>
                <p className="text-[10px] font-semibold tracking-[0.04em] text-foreground">
                  {result}
                </p>
                <p className="mt-1 text-[10px] leading-5 text-muted-foreground">
                  {resultDetail(result, authoritativeUnknown)}
                </p>
              </>
            ) : (
              <p className="text-[10px] leading-5 text-muted-foreground">
                {resultDetail(undefined)}
              </p>
            )}
          </div>
        </form>
      )}
    </section>
  );
}

function IdentityRow({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="grid grid-cols-[105px_minmax(0,1fr)] border-b border-border py-2.5 even:pl-4 max-[620px]:even:pl-0">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="m-0 min-w-0 font-medium text-foreground [overflow-wrap:anywhere]">{value}</dd>
    </div>
  );
}

function StatusOnly({ copy }: { readonly copy: string }) {
  return (
    <p className="mt-4 border-l border-border pl-3 text-[10px] leading-5 text-muted-foreground">
      {copy}
    </p>
  );
}
