import { create } from "@bufbuild/protobuf";
import { AuthenticationContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { FactorState, Role } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  GetSbrReadinessRequestSchema,
  GetSbrReadinessResponseSchema,
  MachineCredentialState,
  ProductIdState,
  SbrEnvironment,
  type SbrReadiness,
  SbrReadinessState,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { AlertTriangle, CheckCircle2, CircleDashed, LockKeyhole, RadioTower } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Badge } from "../../components/ui/badge";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { MachineCredentialForm } from "./machine-credential-form";
import { SbrSimulatorPanel } from "./sbr-simulator-panel";
import { TotpSetup } from "./totp-setup";

const readinessCodec = createProtoMethodCodec({
  input: GetSbrReadinessRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetSbrReadinessResponseSchema,
});

type ScreenState =
  | { readonly status: "loading" }
  | { readonly status: "unavailable" }
  | { readonly readiness: SbrReadiness; readonly status: "ready" };

const issueCopy: Readonly<Record<string, string>> = {
  UNSUPPORTED_SBR_TARGET: "This device is not an approved SBR target.",
  SBR_PROFILE_MISSING: "The signed SBR runtime profile is missing.",
  SBR_PROFILE_INVALID: "The signed SBR runtime profile is invalid.",
  SBR_PROFILE_UNTRUSTED: "The signed SBR runtime profile could not be trusted.",
  SBR_PROFILE_EXPIRED: "The signed SBR runtime profile has expired.",
  SBR_HELPER_UNTRUSTED: "The local SBR helper could not be trusted.",
  SBR_COMPONENT_MISSING: "The approved SBR component is missing.",
  SBR_COMPONENT_UNTRUSTED: "The approved SBR component could not be trusted.",
  SBR_COMPONENT_LICENCE_NOT_APPROVED: "The SBR component licence is not approved.",
  SBR_REGISTRATION_MANIFEST_MISSING: "Signed registration evidence is missing.",
  SBR_REGISTRATION_MANIFEST_INVALID: "Signed registration evidence is invalid.",
  SBR_REGISTRATION_MANIFEST_UNTRUSTED: "Signed registration evidence could not be trusted.",
  SBR_REGISTRATION_MANIFEST_EXPIRED: "Signed registration evidence has expired.",
  SBR_DSP_REGISTRATION_NOT_APPROVED: "DSP registration is not approved.",
  SBR_PRODUCT_REGISTRATION_NOT_APPROVED: "Product registration is not approved.",
  SBR_OSF_ASSESSMENT_NOT_APPROVED: "Operational security assessment is not approved.",
  SBR_EVTE_ACCESS_NOT_APPROVED: "EVTE access is not approved.",
  SBR_ENDPOINT_PROFILE_MISSING: "The signed EVTE endpoint profile is missing.",
  SBR_ENDPOINT_PROFILE_UNTRUSTED: "The signed EVTE endpoint profile could not be trusted.",
  SBR_ENDPOINT_PROFILE_EXPIRED: "The signed EVTE endpoint profile has expired.",
  SBR_SERVICE_ENROLMENT_NOT_APPROVED: "Service enrolment is not approved.",
  SBR_SERVICE_CONFORMANCE_NOT_PASSED: "Service conformance has not passed.",
  SBR_PRODUCT_ID_MISSING: "The service-scoped Product ID is missing.",
  SBR_PRODUCT_ID_INACCESSIBLE: "The service-scoped Product ID is inaccessible.",
  SBR_SECURE_STORE_UNAVAILABLE: "Protected credential storage is unavailable.",
  SBR_CREDENTIAL_MISSING: "A RAM machine credential is required.",
  SBR_CREDENTIAL_INCOMPATIBLE: "The RAM machine credential is incompatible.",
  SBR_CREDENTIAL_REVOKED: "The RAM machine credential has been revoked.",
  SBR_CREDENTIAL_EXPIRED: "The RAM machine credential has expired.",
  SBR_CREDENTIAL_ORGANISATION_MISMATCH:
    "The RAM machine credential does not match this organisation.",
  SBR_CREDENTIAL_REIMPORT_REQUIRED:
    "Reimport the RAM machine credential on this device before running SBR readiness checks.",
};

interface SbrReadinessScreenProps {
  readonly api: Pick<TammyDesktopAPI, "getCurrentUser" | "getSbrReadiness"> &
    Partial<
      Pick<
        TammyDesktopAPI,
        | "assertTotp"
        | "confirmTotp"
        | "enrolTotp"
        | "importMachineCredential"
        | "removeMachineCredential"
        | "replaceMachineCredential"
        | "runSbrReadinessFixture"
        | "selectMachineCredentialFile"
        | "unlockMachineCredential"
      >
    >;
  readonly doctorMode?: boolean;
  readonly onNavigate?: (path: string) => void;
  readonly workspace: AuthenticatedWorkspace &
    Required<
      Pick<
        AuthenticatedWorkspace,
        "organisationCanonicalAbn" | "organisationDisplayName" | "organisationId" | "roles"
      >
    > & { readonly userFactorState?: FactorState };
}

function readinessLabel(state: SbrReadinessState): string {
  switch (state) {
    case SbrReadinessState.READY_FOR_SIMULATOR:
      return "Ready for simulator";
    case SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE:
      return "Ready for EVTE pre-conformance";
    case SbrReadinessState.READY_FOR_EVTE_POST_CONFORMANCE:
      return "Ready for EVTE post-conformance";
    default:
      return "Readiness unavailable";
  }
}

function credentialLabel(state: MachineCredentialState): string {
  switch (state) {
    case MachineCredentialState.MISSING:
      return "Missing";
    case MachineCredentialState.PRESENT:
      return "Present";
    case MachineCredentialState.INACCESSIBLE:
      return "Inaccessible";
    case MachineCredentialState.INCOMPATIBLE:
      return "Incompatible";
    case MachineCredentialState.REVOKED:
      return "Revoked";
    case MachineCredentialState.EXPIRED:
      return "Expired";
    case MachineCredentialState.ABN_MISMATCH:
      return "Organisation mismatch";
    default:
      return "Unavailable";
  }
}

function productLabel(state: ProductIdState): string {
  switch (state) {
    case ProductIdState.PRESENT:
      return "Present";
    case ProductIdState.MISSING:
      return "Missing";
    case ProductIdState.INACCESSIBLE:
      return "Inaccessible";
    default:
      return "Not used";
  }
}

function environmentCopy(environment: SbrEnvironment): { label: string; detail: string } {
  if (environment === SbrEnvironment.SIMULATOR) {
    return {
      label: "Simulator",
      detail:
        "Synthetic and network-disabled. Simulator results are test evidence, not ATO lodgment outcomes.",
    };
  }
  if (environment === SbrEnvironment.EVTE) {
    return {
      label: "EVTE",
      detail:
        "Non-production ATO verification environment. Readiness does not enable production lodgment.",
    };
  }
  return {
    label: "Unavailable",
    detail: "No authenticated SBR environment is currently available.",
  };
}

function safeIssues(codes: readonly string[]): string[] {
  return [...new Set(codes.flatMap((code) => (issueCopy[code] ? [issueCopy[code]] : [])))];
}

function validReadiness(readiness: SbrReadiness): boolean {
  const environmentValid =
    readiness.environment === SbrEnvironment.SIMULATOR ||
    readiness.environment === SbrEnvironment.EVTE;
  const stateValid =
    readiness.state === SbrReadinessState.UNAVAILABLE ||
    readiness.state === SbrReadinessState.READY_FOR_SIMULATOR ||
    readiness.state === SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE ||
    readiness.state === SbrReadinessState.READY_FOR_EVTE_POST_CONFORMANCE;
  const credentialValid =
    readiness.machineCredentialState === MachineCredentialState.MISSING ||
    readiness.machineCredentialState === MachineCredentialState.PRESENT ||
    readiness.machineCredentialState === MachineCredentialState.INACCESSIBLE ||
    readiness.machineCredentialState === MachineCredentialState.INCOMPATIBLE ||
    readiness.machineCredentialState === MachineCredentialState.REVOKED ||
    readiness.machineCredentialState === MachineCredentialState.EXPIRED ||
    readiness.machineCredentialState === MachineCredentialState.ABN_MISMATCH;
  const productValid =
    readiness.productIdState === ProductIdState.PRESENT ||
    readiness.productIdState === ProductIdState.MISSING ||
    readiness.productIdState === ProductIdState.INACCESSIBLE;
  const fingerprintsValid = [
    readiness.credentialFingerprint,
    readiness.profileFingerprint,
    readiness.componentFingerprint,
  ].every((fingerprint) => fingerprint.length <= 128);
  const scopePattern = /^[A-Za-z0-9._:-]{1,128}$/;
  const scopeValid =
    readiness.environment === SbrEnvironment.SIMULATOR
      ? readiness.evteProductIdentifier === "" && readiness.evteServiceIdentifier === ""
      : scopePattern.test(readiness.evteProductIdentifier) &&
        scopePattern.test(readiness.evteServiceIdentifier);
  if (!environmentValid || !stateValid || !credentialValid || !productValid) return false;
  if (!fingerprintsValid || !scopeValid) return false;

  if (readiness.environment === SbrEnvironment.SIMULATOR) {
    if (
      readiness.state !== SbrReadinessState.UNAVAILABLE &&
      readiness.state !== SbrReadinessState.READY_FOR_SIMULATOR
    ) {
      return false;
    }
    if (
      readiness.state === SbrReadinessState.READY_FOR_SIMULATOR &&
      (readiness.productIdState !== ProductIdState.MISSING || readiness.readinessCodes.length !== 0)
    ) {
      return false;
    }
  } else if (readiness.state === SbrReadinessState.READY_FOR_SIMULATOR) {
    return false;
  }

  if (readiness.state !== SbrReadinessState.UNAVAILABLE) {
    if (readiness.machineCredentialState !== MachineCredentialState.PRESENT) return false;
    if (
      readiness.environment === SbrEnvironment.EVTE &&
      readiness.productIdState !== ProductIdState.PRESENT
    ) {
      return false;
    }
  }
  return true;
}

export function SbrReadinessScreen({
  api,
  doctorMode = false,
  onNavigate = () => undefined,
  workspace,
}: SbrReadinessScreenProps) {
  const [state, setState] = useState<ScreenState>({ status: "loading" });
  const requestSequence = useRef(0);
  const requestedKey = useRef<string | undefined>(undefined);
  const [refresh, setRefresh] = useState(0);
  const [factorEnabled, setFactorEnabled] = useState(
    workspace.userFactorState === FactorState.ENABLED,
  );
  const factorPrincipal = useRef(workspace.userId);
  useEffect(() => {
    if (factorPrincipal.current !== workspace.userId) factorPrincipal.current = workspace.userId;
    setFactorEnabled(workspace.userFactorState === FactorState.ENABLED);
  }, [workspace.userFactorState, workspace.userId]);
  const requestKey = `${workspace.sessionId}:${workspace.userId}:${doctorMode ? "doctor" : "screen"}:${refresh}`;

  useEffect(() => {
    if (requestedKey.current === requestKey) return;
    requestedKey.current = requestKey;
    const sequence = requestSequence.current + 1;
    requestSequence.current = sequence;
    setState((current) => (current.status === "ready" ? current : { status: "loading" }));
    const request = create(GetSbrReadinessRequestSchema, {
      authentication: create(AuthenticationContextSchema, {
        actorUserId: workspace.userId,
        sessionId: workspace.sessionId,
      }),
    });
    void Promise.resolve()
      .then(() => api.getSbrReadiness(readinessCodec.encodeRequest(request)))
      .then((frame) => {
        const response = readinessCodec.decodeResponse(frame);
        if (!response.readiness || !validReadiness(response.readiness)) {
          throw new Error("invalid readiness response");
        }
        if (requestSequence.current === sequence) {
          setState({ readiness: response.readiness, status: "ready" });
        }
      })
      .catch(() => {
        if (requestSequence.current === sequence) setState({ status: "unavailable" });
      });
  }, [api, requestKey, workspace.sessionId, workspace.userId]);

  const announcement =
    state.status === "loading"
      ? "Checking SBR readiness."
      : state.status === "unavailable"
        ? "SBR readiness is unavailable."
        : `${readinessLabel(state.readiness.state)}.`;

  return (
    <div className="mx-auto grid w-full max-w-[920px] gap-5">
      <p aria-live="polite" className="sr-only" role="status">
        {announcement}
      </p>
      <header className="flex items-end justify-between gap-5 border-b border-border pb-4">
        <div className="min-w-0">
          <p className="mb-1 text-[10px] font-semibold uppercase tracking-[0.12em] text-forest">
            Settings / secure reporting
          </p>
          <h1 className="text-[19px] font-semibold tracking-[-0.025em] text-foreground">
            SBR readiness
          </h1>
          <p
            className="mt-1 min-w-0 text-[11px] leading-5 text-muted-foreground [overflow-wrap:anywhere]"
            data-testid="sbr-organisation-identity"
          >
            {workspace.organisationDisplayName} · ABN {workspace.organisationCanonicalAbn}
          </p>
        </div>
        <Badge variant="outline">Local only</Badge>
      </header>

      {state.status === "loading" ? <LoadingSurface /> : null}
      {state.status === "unavailable" ? <UnavailableSurface /> : null}
      {state.status === "ready" ? (
        <ReadinessSurface
          api={api}
          factorEnabled={factorEnabled}
          onFactorEnabled={() => setFactorEnabled(true)}
          onNavigate={onNavigate}
          onRefresh={() => setRefresh((value) => value + 1)}
          readiness={state.readiness}
          roles={workspace.roles}
          workspace={workspace}
        />
      ) : null}
    </div>
  );
}

function LoadingSurface() {
  return (
    <section aria-label="SBR readiness summary" className="border-y border-border py-8">
      <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
        <CircleDashed aria-hidden="true" className="size-4 animate-spin text-forest" />
        Checking signed profile, protected credentials, and registration evidence…
      </div>
    </section>
  );
}

function UnavailableSurface() {
  return (
    <section aria-label="SBR readiness summary" className="border-y border-warning-border py-6">
      <div className="flex items-start gap-3">
        <AlertTriangle aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-warning" />
        <div>
          <h2 className="text-[13px] font-semibold text-foreground">Readiness unavailable</h2>
          <p className="mt-1 max-w-[620px] text-[11px] leading-5 text-muted-foreground">
            Tammy could not inspect the authenticated local SBR state. Accounting remains available
            and no network request was started.
          </p>
        </div>
      </div>
      <PreparationBoundary />
    </section>
  );
}

function ReadinessSurface({
  api,
  factorEnabled,
  onFactorEnabled,
  onNavigate,
  onRefresh,
  readiness,
  roles,
  workspace,
}: {
  readonly api: SbrReadinessScreenProps["api"];
  readonly factorEnabled: boolean;
  readonly onFactorEnabled: () => void;
  readonly onNavigate: (path: string) => void;
  readonly onRefresh: () => void;
  readonly readiness: SbrReadiness;
  readonly roles: readonly Role[];
  readonly workspace: SbrReadinessScreenProps["workspace"];
}) {
  const environment = environmentCopy(readiness.environment);
  const label = readinessLabel(readiness.state);
  const ready = readiness.state !== SbrReadinessState.UNAVAILABLE;
  const issues = safeIssues(readiness.readinessCodes);

  return (
    <>
      <section aria-labelledby="sbr-summary-heading" className="border-y border-border">
        <div className="grid min-h-[150px] grid-cols-[minmax(0,1.35fr)_minmax(220px,0.65fr)] max-[720px]:grid-cols-1">
          <div className="flex gap-4 py-6 pr-8 max-[720px]:pr-0">
            <span
              className={`grid size-9 shrink-0 place-items-center rounded-full ${ready ? "bg-ready-soft text-ready-foreground" : "bg-warning-soft text-warning"}`}
            >
              {ready ? (
                <CheckCircle2 aria-hidden="true" className="size-4" />
              ) : (
                <AlertTriangle aria-hidden="true" className="size-4" />
              )}
            </span>
            <div>
              <p className="text-[10px] font-semibold uppercase tracking-[0.11em] text-muted-foreground">
                Current boundary
              </p>
              <h2
                id="sbr-summary-heading"
                className="mt-2 text-[17px] font-semibold text-foreground"
              >
                {label}
              </h2>
              <p className="mt-2 max-w-[520px] text-[11px] leading-5 text-muted-foreground">
                {environment.detail}
              </p>
            </div>
          </div>
          <div className="border-l border-border py-6 pl-7 max-[720px]:border-l-0 max-[720px]:border-t max-[720px]:pl-0">
            <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.1em] text-muted-foreground">
              <RadioTower aria-hidden="true" className="size-3.5" /> Environment
            </div>
            <p className="mt-3 text-[14px] font-semibold text-foreground">{environment.label}</p>
            <p className="mt-1 text-[10px] text-muted-foreground">Authenticated signed profile</p>
          </div>
        </div>
      </section>

      <section aria-labelledby="sbr-evidence-heading">
        <div className="mb-3 flex items-center gap-2">
          <LockKeyhole aria-hidden="true" className="size-3.5 text-forest" />
          <h2 id="sbr-evidence-heading" className="text-[12px] font-semibold text-foreground">
            Local readiness evidence
          </h2>
        </div>
        <dl className="grid border-t border-border text-[11px]">
          <StatusRow
            label="Machine credential"
            value={credentialLabel(readiness.machineCredentialState)}
          />
          <StatusRow
            label="EVTE Product ID"
            value={
              readiness.environment === SbrEnvironment.SIMULATOR
                ? "Not used by simulator"
                : productLabel(readiness.productIdState)
            }
          />
          {readiness.credentialFingerprint ? (
            <StatusRow
              fingerprint
              label="Credential fingerprint"
              value={readiness.credentialFingerprint}
            />
          ) : null}
          {readiness.profileFingerprint ? (
            <StatusRow
              fingerprint
              label="Profile fingerprint"
              value={readiness.profileFingerprint}
            />
          ) : null}
          {readiness.componentFingerprint ? (
            <StatusRow
              fingerprint
              label="Component fingerprint"
              value={readiness.componentFingerprint}
            />
          ) : null}
        </dl>
      </section>

      {issues.length ? (
        <section
          aria-labelledby="sbr-needs-attention-heading"
          className="bg-warning-soft px-4 py-4"
        >
          <h2
            id="sbr-needs-attention-heading"
            className="text-[11px] font-semibold text-foreground"
          >
            Needs attention
          </h2>
          <ul className="mb-0 mt-2 grid gap-1.5 pl-4 text-[11px] leading-5 text-muted-foreground">
            {issues.map((issue) => (
              <li key={issue}>{issue}</li>
            ))}
          </ul>
          <button
            className="focus-ring mt-3 text-[10px] font-medium text-forest underline"
            onClick={() => onNavigate("/settings/organisation")}
            type="button"
          >
            Review organisation verification
          </button>
        </section>
      ) : null}

      {roles.includes(Role.WORKSPACE_ADMIN) ? (
        factorEnabled ? (
          <MachineCredentialForm
            api={api as Parameters<typeof MachineCredentialForm>[0]["api"]}
            credentialState={readiness.machineCredentialState}
            onChanged={onRefresh}
            workspace={workspace}
          />
        ) : (
          <TotpSetup
            api={api as Parameters<typeof TotpSetup>[0]["api"]}
            onEnabled={onFactorEnabled}
            workspace={workspace}
          />
        )
      ) : null}

      <SbrSimulatorPanel
        api={api as Parameters<typeof SbrSimulatorPanel>[0]["api"]}
        factorEnabled={factorEnabled}
        onRefresh={onRefresh}
        readiness={readiness}
        workspace={workspace}
      />

      <PreparationBoundary />
    </>
  );
}

function StatusRow({
  fingerprint = false,
  label,
  value,
}: {
  readonly fingerprint?: boolean;
  readonly label: string;
  readonly value: string;
}) {
  return (
    <div className="grid grid-cols-[minmax(150px,0.42fr)_minmax(0,0.58fr)] items-baseline gap-6 border-b border-border py-3 max-[560px]:grid-cols-1 max-[560px]:gap-1">
      <dt className="text-muted-foreground">{label}</dt>
      <dd
        className={`m-0 min-w-0 text-foreground ${fingerprint ? "font-mono text-[10px] [overflow-wrap:anywhere]" : "font-medium"}`}
      >
        {value}
      </dd>
    </div>
  );
}

function PreparationBoundary() {
  return (
    <p className="mt-5 border-l-2 border-forest pl-3 text-[10px] leading-5 text-muted-foreground">
      BAS remains preparation-only. This screen cannot declare, lodge, amend, or submit a BAS.
    </p>
  );
}
