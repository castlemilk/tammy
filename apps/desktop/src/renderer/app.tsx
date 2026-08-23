import { create } from "@bufbuild/protobuf";
import { AuthenticationContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  FactorState,
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
  Role,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
  OrganisationVerificationState,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { AlertTriangle, LoaderCircle, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { createProtoMethodCodec } from "../shared/proto-ipc";
import { AppShell } from "./app-shell/app-shell";
import { resolveAppLocation, type WorkspaceAccess } from "./app-shell/router";
import { Button } from "./components/ui/button";
import { ChartScreen } from "./features/accounting/chart-screen";
import { JournalsScreen } from "./features/accounting/journals-screen";
import { TrialBalanceScreen } from "./features/accounting/trial-balance-screen";
import { AuditScreen } from "./features/audit/audit-screen";
import { BankingScreen } from "./features/banking/banking-screen";
import { BasScreen } from "./features/bas/bas-screen";
import { DiagnosticsCard, type DiagnosticsState } from "./features/diagnostics/diagnostics-card";
import { DocumentsScreen } from "./features/documents/documents-screen";
import { EmptyLedgerScreen } from "./features/ledger/empty-ledger-screen";
import { OverviewScreen } from "./features/overview/overview-screen";
import { PrivacyScreen, PrivacyStatement } from "./features/privacy/privacy-statement";
import { OrganisationVerificationForm } from "./features/sbr/organisation-verification-form";
import { validTimestamp } from "./features/sbr/sbr-form-support";
import { SbrReadinessScreen } from "./features/sbr/sbr-readiness-screen";
import { type AuthenticatedWorkspace, SetupScreen } from "./features/setup/setup-screen";
import { UnlockScreen } from "./features/workspace/unlock-screen";

const WORKSPACE_ID_STORAGE = "tammy.workspace.id";
const ORGANISATION_ID_STORAGE = "tammy.organisation.id";
const SESSION_STORAGE = "tammy.session.active";
const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const currentUserCodec = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetCurrentUserResponseSchema,
});
const organisationCodec = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetOrganisationResponseSchema,
});

type SettingsWorkspace = AuthenticatedWorkspace &
  Required<
    Pick<
      AuthenticatedWorkspace,
      "organisationCanonicalAbn" | "organisationDisplayName" | "organisationId" | "roles"
    >
  >;

interface SettingsWorkspaceDetails {
  readonly organisationEntityType: string;
  readonly organisationLegalName: string;
  readonly organisationVerificationState: OrganisationVerificationState;
  readonly organisationVerificationExpiresAt?: { readonly nanos: number; readonly seconds: bigint };
  readonly organisationVersion: bigint;
  readonly userFactorState?: FactorState;
}

type CompleteSettingsWorkspace = SettingsWorkspace & SettingsWorkspaceDetails;

type SettingsProjectionState =
  | { readonly status: "idle" }
  | { readonly key: string; readonly status: "loading" | "unavailable" }
  | {
      readonly key: string;
      readonly status: "ready";
      readonly workspace: CompleteSettingsWorkspace;
    };

function validRoles(roles: unknown): roles is readonly Role[] {
  return (
    Array.isArray(roles) &&
    roles.length > 0 &&
    roles.every(
      (role) =>
        role === Role.WORKSPACE_ADMIN ||
        role === Role.BUSINESS_PREPARER ||
        role === Role.BUSINESS_LODGER ||
        role === Role.AUDITOR,
    ) &&
    new Set(roles).size === roles.length &&
    roles.every((role, index) => index === 0 || role > (roles[index - 1] ?? 0))
  );
}

function validOrganisationVerificationState(state: OrganisationVerificationState): boolean {
  return (
    state === OrganisationVerificationState.UNVERIFIED ||
    state === OrganisationVerificationState.VERIFIED ||
    state === OrganisationVerificationState.FAILED ||
    state === OrganisationVerificationState.EXPIRED ||
    state === OrganisationVerificationState.SUPERSEDED
  );
}

function storedAuthenticatedWorkspace(): AuthenticatedWorkspace | undefined {
  const retained = window.sessionStorage.getItem(SESSION_STORAGE);
  if (!retained || retained.length > 4_096) return undefined;
  try {
    const parsed = JSON.parse(retained) as Partial<AuthenticatedWorkspace>;
    if (
      typeof parsed.workspaceId === "string" &&
      typeof parsed.userId === "string" &&
      typeof parsed.sessionId === "string" &&
      typeof parsed.organisationId === "string" &&
      typeof parsed.organisationDisplayName === "string" &&
      typeof parsed.organisationCanonicalAbn === "string" &&
      validRoles(parsed.roles) &&
      UUID_V7.test(parsed.workspaceId) &&
      UUID_V7.test(parsed.userId) &&
      UUID_V7.test(parsed.sessionId) &&
      UUID_V7.test(parsed.organisationId) &&
      parsed.organisationDisplayName.trim().length > 0 &&
      parsed.organisationDisplayName.length <= 256 &&
      /^[0-9]{11}$/.test(parsed.organisationCanonicalAbn) &&
      parsed.roles.length > 0
    ) {
      return {
        workspaceId: parsed.workspaceId,
        userId: parsed.userId,
        sessionId: parsed.sessionId,
        organisationId: parsed.organisationId,
        organisationDisplayName: parsed.organisationDisplayName,
        organisationCanonicalAbn: parsed.organisationCanonicalAbn,
        roles: [...parsed.roles],
      };
    }
  } catch {
    return undefined;
  }
  return undefined;
}

function initialAccess(): WorkspaceAccess {
  if (storedAuthenticatedWorkspace()) return "authenticated";
  if (window.localStorage.getItem(WORKSPACE_ID_STORAGE)) return "locked";
  return "unconfigured";
}

function currentLocation(): string {
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

function hasSbrWorkspaceProjection(
  workspace: AuthenticatedWorkspace | undefined,
): workspace is AuthenticatedWorkspace &
  Required<
    Pick<
      AuthenticatedWorkspace,
      "organisationCanonicalAbn" | "organisationDisplayName" | "organisationId" | "roles"
    >
  > {
  return Boolean(
    workspace?.organisationId &&
      workspace.organisationDisplayName &&
      workspace.organisationCanonicalAbn &&
      workspace.roles,
  );
}

export function App() {
  const [diagnosticsState, setDiagnosticsState] = useState<DiagnosticsState>({ status: "loading" });
  const [access, setAccess] = useState<WorkspaceAccess>(initialAccess);
  const [workspace, setWorkspace] = useState<AuthenticatedWorkspace | undefined>(
    storedAuthenticatedWorkspace,
  );
  const [activePath, setActivePath] = useState(
    () => resolveAppLocation(currentLocation(), initialAccess()).path,
  );
  const requestSequence = useRef(0);
  const appMounted = useRef(false);
  const settingsRequestSequence = useRef(0);
  const settingsRequestedKey = useRef<string | undefined>(undefined);
  const [settingsProjection, setSettingsProjection] = useState<SettingsProjectionState>({
    status: "idle",
  });
  const [settingsReload, setSettingsReload] = useState(0);
  const settingsRoute =
    activePath === "/settings/organisation" ||
    activePath === "/settings/sbr" ||
    activePath === "/settings/sbr?doctor=1";
  const settingsRequestKey =
    settingsRoute && hasSbrWorkspaceProjection(workspace)
      ? `${workspace.sessionId}:${workspace.userId}:${workspace.organisationId}:${settingsReload}`
      : undefined;

  const loadDiagnostics = useCallback(async () => {
    const request = requestSequence.current + 1;
    requestSequence.current = request;
    setDiagnosticsState({ status: "loading" });
    try {
      const diagnostics = await window.tammy.getSystemDiagnostics();
      if (requestSequence.current === request)
        setDiagnosticsState({ diagnostics, status: "ready" });
    } catch {
      if (requestSequence.current === request) setDiagnosticsState({ status: "unavailable" });
    }
  }, []);

  useEffect(() => {
    appMounted.current = true;
    return () => {
      appMounted.current = false;
    };
  }, []);

  useEffect(() => {
    void loadDiagnostics();
    return () => {
      requestSequence.current += 1;
    };
  }, [loadDiagnostics]);

  useEffect(() => {
    if (!settingsRequestKey || !hasSbrWorkspaceProjection(workspace)) {
      settingsRequestSequence.current += 1;
      settingsRequestedKey.current = undefined;
      setSettingsProjection({ status: "idle" });
      return;
    }
    if (settingsRequestedKey.current === settingsRequestKey) return;
    settingsRequestedKey.current = settingsRequestKey;
    const sequence = settingsRequestSequence.current + 1;
    settingsRequestSequence.current = sequence;
    setSettingsProjection({ key: settingsRequestKey, status: "loading" });
    const authentication = create(AuthenticationContextSchema, {
      actorUserId: workspace.userId,
      sessionId: workspace.sessionId,
    });
    const currentWorkspace = workspace;
    void Promise.all([
      Promise.resolve()
        .then(() =>
          window.tammy.getCurrentUser(
            currentUserCodec.encodeRequest(create(GetCurrentUserRequestSchema, { authentication })),
          ),
        )
        .then((frame) => currentUserCodec.decodeResponse(frame)),
      Promise.resolve()
        .then(() =>
          window.tammy.getOrganisation(
            organisationCodec.encodeRequest(
              create(GetOrganisationRequestSchema, {
                authentication,
                organisationId: workspace.organisationId,
              }),
            ),
          ),
        )
        .then((frame) => organisationCodec.decodeResponse(frame)),
    ])
      .then(([currentUser, currentOrganisation]) => {
        if (
          !currentUser.user ||
          currentUser.user.id !== currentWorkspace.userId ||
          !validRoles(currentUser.user.roles) ||
          !currentOrganisation.organisation ||
          currentOrganisation.organisation.id !== currentWorkspace.organisationId ||
          !currentOrganisation.organisation.displayName.trim() ||
          currentOrganisation.organisation.displayName.length > 256 ||
          !/^[0-9]{11}$/.test(currentOrganisation.organisation.abn) ||
          currentOrganisation.organisation.version < 1n ||
          !currentOrganisation.organisation.legalName.trim() ||
          currentOrganisation.organisation.legalName.length > 256 ||
          !currentOrganisation.organisation.entityType.trim() ||
          currentOrganisation.organisation.entityType.length > 96 ||
          !validOrganisationVerificationState(currentOrganisation.organisation.verificationState) ||
          (currentUser.user.factorState !== undefined &&
            currentUser.user.factorState !== FactorState.PENDING_CONFIRMATION &&
            currentUser.user.factorState !== FactorState.ENABLED &&
            currentUser.user.factorState !== FactorState.DISABLED) ||
          (currentOrganisation.currentVerification !== undefined &&
            (!validOrganisationVerificationState(currentOrganisation.currentVerification.state) ||
              !validTimestamp(currentOrganisation.currentVerification.expiresAt)))
        ) {
          throw new Error("invalid authoritative workspace projection");
        }
        const refreshed: CompleteSettingsWorkspace = {
          ...currentWorkspace,
          organisationCanonicalAbn: currentOrganisation.organisation.abn,
          organisationDisplayName: currentOrganisation.organisation.displayName,
          organisationId: currentOrganisation.organisation.id,
          roles: [...currentUser.user.roles],
          ...(currentUser.user.factorState === undefined
            ? {}
            : { userFactorState: currentUser.user.factorState }),
          organisationVersion: currentOrganisation.organisation.version,
          organisationLegalName: currentOrganisation.organisation.legalName,
          organisationEntityType: currentOrganisation.organisation.entityType,
          organisationVerificationState: currentOrganisation.organisation.verificationState,
          ...(currentOrganisation.currentVerification?.expiresAt === undefined
            ? {}
            : {
                organisationVerificationExpiresAt:
                  currentOrganisation.currentVerification.expiresAt,
              }),
        };
        if (!appMounted.current || settingsRequestSequence.current !== sequence) return;
        window.sessionStorage.setItem(
          SESSION_STORAGE,
          JSON.stringify({
            workspaceId: refreshed.workspaceId,
            userId: refreshed.userId,
            sessionId: refreshed.sessionId,
            organisationId: refreshed.organisationId,
            organisationDisplayName: refreshed.organisationDisplayName,
            organisationCanonicalAbn: refreshed.organisationCanonicalAbn,
            roles: refreshed.roles,
          } satisfies AuthenticatedWorkspace),
        );
        setWorkspace(refreshed);
        setSettingsProjection({ key: settingsRequestKey, status: "ready", workspace: refreshed });
      })
      .catch(() => {
        if (appMounted.current && settingsRequestSequence.current === sequence) {
          setSettingsProjection({ key: settingsRequestKey, status: "unavailable" });
        }
      });
  }, [settingsRequestKey, workspace]);

  useEffect(() => {
    const restore = () => {
      const location = currentLocation();
      const resolved = resolveAppLocation(location, access);
      if (location !== resolved.path) {
        window.history.replaceState(null, "", resolved.path);
      }
      setActivePath(resolved.path);
    };
    restore();
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, [access]);

  const navigate = useCallback(
    (path: string) => {
      const resolved = resolveAppLocation(path, access);
      window.history.pushState(null, "", resolved.path);
      setActivePath(resolved.path);
    },
    [access],
  );

  const authenticated = useCallback((workspace: AuthenticatedWorkspace) => {
    window.localStorage.setItem(WORKSPACE_ID_STORAGE, workspace.workspaceId);
    if (workspace.organisationId) {
      window.localStorage.setItem(ORGANISATION_ID_STORAGE, workspace.organisationId);
    }
    window.sessionStorage.setItem(SESSION_STORAGE, JSON.stringify(workspace));
    setWorkspace(workspace);
    setAccess("authenticated");
    window.history.replaceState(null, "", "/overview");
    setActivePath("/overview");
  }, []);

  if (activePath === "/privacy") {
    return <PrivacyScreen onBack={() => navigate("/overview")} />;
  }

  if (access === "unconfigured") {
    return (
      <SetupScreen
        api={window.tammy}
        onAuthenticated={authenticated}
        onPrivacy={() => navigate("/privacy")}
      />
    );
  }

  if (access === "locked") {
    const organisationId = window.localStorage.getItem(ORGANISATION_ID_STORAGE);
    if (!organisationId) {
      return (
        <SetupScreen
          api={window.tammy}
          onAuthenticated={authenticated}
          onPrivacy={() => navigate("/privacy")}
        />
      );
    }
    return (
      <UnlockScreen
        api={window.tammy}
        onAuthenticated={authenticated}
        onPrivacy={() => navigate("/privacy")}
        organisationId={organisationId}
      />
    );
  }

  return (
    <AppShell activePath={activePath} onNavigate={navigate}>
      <EngineStatus onRetry={loadDiagnostics} state={diagnosticsState} />
      <RouteContent
        onNavigate={navigate}
        path={activePath}
        state={diagnosticsState}
        settingsProjection={settingsProjection}
        settingsRequestKey={settingsRequestKey}
        onReloadSettings={() => setSettingsReload((value) => value + 1)}
        workspace={workspace}
      />
    </AppShell>
  );
}

function EngineStatus({
  onRetry,
  state,
}: {
  readonly onRetry: () => void;
  readonly state: DiagnosticsState;
}) {
  if (state.status === "ready") {
    return (
      <p
        aria-live="polite"
        className="sr-only"
        data-startup-transition="starting-to-ready"
        role="status"
      >
        Local engine ready. Offline. No cloud required.
      </p>
    );
  }
  if (state.status === "loading") {
    return (
      <div
        aria-live="polite"
        className="mb-4 flex items-center gap-2 text-[10px] text-muted-foreground"
        role="status"
      >
        <LoaderCircle aria-hidden="true" className="size-3 animate-spin" />
        Starting local engine
      </div>
    );
  }
  return (
    <div
      aria-live="polite"
      className="mb-4 flex items-center justify-between gap-4 rounded-[6px] border border-warning-border bg-warning-soft px-3 py-2"
      role="status"
    >
      <div className="flex items-center gap-2 text-[10px] text-foreground">
        <AlertTriangle aria-hidden="true" className="size-3.5 text-warning" />
        Local engine unavailable. Your data has not left this device.
      </div>
      <Button className="h-7 text-[10px]" onClick={onRetry} type="button" variant="outline">
        <RefreshCw aria-hidden="true" className="size-3" />
        Retry local engine
      </Button>
    </div>
  );
}

function RouteContent({
  onNavigate,
  path,
  settingsProjection,
  settingsRequestKey,
  onReloadSettings,
  state,
  workspace,
}: {
  readonly onNavigate: (path: string) => void;
  readonly path: string;
  readonly settingsProjection: SettingsProjectionState;
  readonly settingsRequestKey: string | undefined;
  readonly onReloadSettings: () => void;
  readonly state: DiagnosticsState;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  if (path === "/overview") return <OverviewScreen api={window.tammy} workspace={workspace} />;
  if (path === "/documents") return <DocumentsScreen api={window.tammy} workspace={workspace} />;
  if (path === "/banking") return <BankingScreen api={window.tammy} workspace={workspace} />;
  if (path === "/gst-bas") return <BasScreen api={window.tammy} workspace={workspace} />;
  if (path === "/accounting/chart") {
    return <ChartScreen api={window.tammy} workspace={workspace} />;
  }
  if (path.startsWith("/accounting/journals")) {
    return (
      <JournalsScreen
        api={window.tammy}
        onNavigate={onNavigate}
        path={path}
        workspace={workspace}
      />
    );
  }
  if (path === "/accounting/trial-balance") {
    return <TrialBalanceScreen api={window.tammy} workspace={workspace} />;
  }
  if (path === "/audit") {
    return <AuditScreen api={window.tammy} workspace={workspace} />;
  }
  if (path === "/settings") {
    return (
      <div className="mx-auto grid w-full max-w-[920px] gap-5">
        <div>
          <h1 className="text-[18px] font-semibold tracking-[-0.02em] text-foreground">Settings</h1>
          <p className="mt-1 text-[11px] leading-5 text-muted-foreground">
            Local workspace and system information.
          </p>
        </div>
        <DiagnosticsCard onRetry={() => undefined} state={state} />
        <PrivacyStatement />
      </div>
    );
  }
  if (path === "/settings/organisation") {
    if (settingsProjection.status !== "ready" || settingsProjection.key !== settingsRequestKey) {
      return <SettingsProjectionStatus state={settingsProjection} />;
    }
    const trustedWorkspace = settingsProjection.workspace;
    return (
      <div className="mx-auto grid w-full max-w-[920px] gap-5">
        <div className="border-b border-border pb-4">
          <p className="mb-1 text-[10px] font-semibold uppercase tracking-[0.12em] text-forest">
            Settings / organisation
          </p>
          <h1 className="text-[19px] font-semibold tracking-[-0.025em] text-foreground">
            Organisation
          </h1>
        </div>
        <dl className="grid min-w-0 border-t border-border text-[11px]">
          <div className="grid grid-cols-[180px_minmax(0,1fr)] border-b border-border py-3">
            <dt className="text-muted-foreground">Display name</dt>
            <dd className="m-0 min-w-0 font-medium text-foreground [overflow-wrap:anywhere]">
              {trustedWorkspace.organisationDisplayName}
            </dd>
          </div>
          <div className="grid grid-cols-[180px_minmax(0,1fr)] border-b border-border py-3">
            <dt className="text-muted-foreground">Canonical ABN</dt>
            <dd className="m-0 font-medium text-foreground">
              {trustedWorkspace.organisationCanonicalAbn}
            </dd>
          </div>
        </dl>
        <OrganisationVerificationForm
          api={window.tammy}
          onChanged={onReloadSettings}
          workspace={trustedWorkspace}
        />
      </div>
    );
  }
  if (path === "/settings/sbr" || path === "/settings/sbr?doctor=1") {
    if (settingsProjection.status !== "ready" || settingsProjection.key !== settingsRequestKey) {
      return <SettingsProjectionStatus state={settingsProjection} />;
    }
    return (
      <SbrReadinessScreen
        api={window.tammy}
        doctorMode={path === "/settings/sbr?doctor=1"}
        onNavigate={onNavigate}
        workspace={settingsProjection.workspace}
      />
    );
  }
  return (
    <EmptyLedgerScreen
      description="This workspace screen is not yet connected."
      emptyLabel="Workspace action unavailable"
      title="Workspace"
    />
  );
}

function SettingsProjectionStatus({ state }: { readonly state: SettingsProjectionState }) {
  const unavailable = state.status === "unavailable";
  return (
    <div className="mx-auto w-full max-w-[920px] border-y border-border py-8">
      <p aria-live="polite" className="text-[11px] text-muted-foreground" role="status">
        {unavailable ? "Workspace details unavailable" : "Loading authenticated workspace details"}
      </p>
      {unavailable ? (
        <p className="mt-2 text-[10px] leading-5 text-muted-foreground">
          Tammy could not confirm the current user and organisation. No privileged settings are
          shown.
        </p>
      ) : null}
    </div>
  );
}
