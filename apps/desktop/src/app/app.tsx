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
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type TammyConveyor, tammyConveyor } from "../conveyor/client";
import type { DesktopLaunchScenario } from "../shared/launch-scenario";
import { createProtoMethodCodec } from "../shared/proto-ipc";
import { AppShell } from "./app-shell/app-shell";
import {
  parseBasPreparationRouteTarget,
  resolveAppLocation,
  type WorkspaceAccess,
} from "./app-shell/router";
import { SimulatorBanner } from "./app-shell/simulator-banner";
import { Button } from "./components/ui/button";
import { ScreenHeading, SurfaceHeading } from "./components/workspace-primitives";
import { ChartScreen } from "./features/accounting/chart-screen";
import { JournalsScreen } from "./features/accounting/journals-screen";
import { TrialBalanceScreen } from "./features/accounting/trial-balance-screen";
import { AuditScreen } from "./features/audit/audit-screen";
import { BankingScreen } from "./features/banking/banking-screen";
import { BasLifecycleScreen } from "./features/bas/bas-lifecycle-screen";
import { DiagnosticsCard, type DiagnosticsState } from "./features/diagnostics/diagnostics-card";
import { DocumentsScreen } from "./features/documents/documents-screen";
import { ConnectedInvoicesScreen } from "./features/invoices/connected-invoices-screen";
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

function ScenarioFrame({
  children,
  scenario,
}: {
  readonly children: ReactNode;
  readonly scenario: DesktopLaunchScenario;
}) {
  return (
    <div className="flex min-h-screen flex-col">
      <SimulatorBanner scenario={scenario} />
      <div className="min-h-0 flex-1">{children}</div>
    </div>
  );
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

export function App({ conveyor = tammyConveyor }: { readonly conveyor?: TammyConveyor } = {}) {
  const launchScenario = window.tammyLaunchScenario ?? "accounting";
  const privacyReturnPath = useRef("/overview");
  const [access, setAccess] = useState<WorkspaceAccess>(initialAccess);
  const [workspace, setWorkspace] = useState<AuthenticatedWorkspace | undefined>(
    storedAuthenticatedWorkspace,
  );
  const [activePath, setActivePath] = useState(() =>
    launchScenario === "sbr-doctor" && initialAccess() === "authenticated"
      ? "/settings/sbr?doctor=1"
      : resolveAppLocation(currentLocation(), initialAccess()).path,
  );
  const settingsRoute =
    activePath === "/settings/organisation" ||
    activePath === "/settings/sbr" ||
    activePath === "/settings/sbr?doctor=1";
  const settingsAuthorityKey =
    settingsRoute && hasSbrWorkspaceProjection(workspace)
      ? `${workspace.sessionId}:${workspace.userId}:${workspace.organisationId}`
      : undefined;
  const diagnosticsQuery = conveyor.system.getSystemDiagnostics.useQuery({ retry: false });
  const diagnosticsState = useMemo<DiagnosticsState>(() => {
    if (diagnosticsQuery.isError) return { status: "unavailable" };
    if (!diagnosticsQuery.data) return { status: "loading" };
    return { diagnostics: diagnosticsQuery.data, status: "ready" };
  }, [diagnosticsQuery.data, diagnosticsQuery.isError]);
  const loadDiagnostics = useCallback(() => {
    void conveyor.system.getSystemDiagnostics.invalidate();
  }, [conveyor.system.getSystemDiagnostics]);
  const settingsInputs = useMemo(() => {
    const authentication = create(AuthenticationContextSchema, {
      actorUserId: workspace?.userId ?? "",
      sessionId: workspace?.sessionId ?? "",
    });
    return {
      organisation: organisationCodec.encodeRequest(
        create(GetOrganisationRequestSchema, {
          authentication,
          organisationId: workspace?.organisationId ?? "",
        }),
      ),
      user: currentUserCodec.encodeRequest(create(GetCurrentUserRequestSchema, { authentication })),
    };
  }, [workspace?.organisationId, workspace?.sessionId, workspace?.userId]);
  const settingsEnabled = Boolean(settingsAuthorityKey);
  const currentUserQuery = conveyor.organisation.getCurrentUser.useQuery({
    enabled: settingsEnabled,
    input: settingsInputs.user,
    retry: false,
  });
  const currentOrganisationQuery = conveyor.organisation.getOrganisation.useQuery({
    enabled: settingsEnabled,
    input: settingsInputs.organisation,
    retry: false,
  });
  const settingsProjection = useMemo<SettingsProjectionState>(() => {
    if (!settingsAuthorityKey || !hasSbrWorkspaceProjection(workspace)) return { status: "idle" };
    if (currentUserQuery.isError || currentOrganisationQuery.isError)
      return { key: settingsAuthorityKey, status: "unavailable" };
    if (!currentUserQuery.data || !currentOrganisationQuery.data)
      return { key: settingsAuthorityKey, status: "loading" };
    try {
      const currentUser = currentUserCodec.decodeResponse(currentUserQuery.data);
      const currentOrganisation = organisationCodec.decodeResponse(currentOrganisationQuery.data);
      if (
        !currentUser.user ||
        currentUser.user.id !== workspace.userId ||
        !validRoles(currentUser.user.roles) ||
        !currentOrganisation.organisation ||
        currentOrganisation.organisation.id !== workspace.organisationId ||
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
        return { key: settingsAuthorityKey, status: "unavailable" };
      }
      const refreshed: CompleteSettingsWorkspace = {
        ...workspace,
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
              organisationVerificationExpiresAt: currentOrganisation.currentVerification.expiresAt,
            }),
      };
      return { key: settingsAuthorityKey, status: "ready", workspace: refreshed };
    } catch {
      return { key: settingsAuthorityKey, status: "unavailable" };
    }
  }, [
    currentOrganisationQuery.data,
    currentOrganisationQuery.isError,
    currentUserQuery.data,
    currentUserQuery.isError,
    settingsAuthorityKey,
    workspace,
  ]);
  useEffect(() => {
    if (settingsProjection.status !== "ready") return;
    const refreshed = settingsProjection.workspace;
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
    setWorkspace((current) =>
      current?.organisationDisplayName === refreshed.organisationDisplayName &&
      current.organisationCanonicalAbn === refreshed.organisationCanonicalAbn &&
      current.roles?.join(",") === refreshed.roles.join(",") &&
      (current as Partial<CompleteSettingsWorkspace>).organisationVersion ===
        refreshed.organisationVersion &&
      (current as Partial<CompleteSettingsWorkspace>).userFactorState === refreshed.userFactorState
        ? current
        : refreshed,
    );
  }, [settingsProjection]);
  const reloadSettings = useCallback(() => {
    void Promise.all([
      conveyor.organisation.getCurrentUser.invalidate(settingsInputs.user),
      conveyor.organisation.getOrganisation.invalidate(settingsInputs.organisation),
    ]);
  }, [
    conveyor.organisation.getCurrentUser,
    conveyor.organisation.getOrganisation,
    settingsInputs.organisation,
    settingsInputs.user,
  ]);

  useEffect(() => {
    const restore = () => {
      const location =
        launchScenario === "sbr-doctor" && access === "authenticated"
          ? "/settings/sbr?doctor=1"
          : currentLocation();
      const resolved = resolveAppLocation(location, access);
      if (currentLocation() !== resolved.path) {
        window.history.replaceState(null, "", resolved.path);
      }
      setActivePath(resolved.path);
    };
    restore();
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, [access, launchScenario]);

  const navigate = useCallback(
    (path: string) => {
      const resolved = resolveAppLocation(path, access);
      if (resolved.path === "/privacy" && currentLocation() !== "/privacy") {
        privacyReturnPath.current = currentLocation();
      }
      window.history.pushState(null, "", resolved.path);
      setActivePath(resolved.path);
    },
    [access],
  );

  const authenticated = useCallback(
    (workspace: AuthenticatedWorkspace) => {
      window.localStorage.setItem(WORKSPACE_ID_STORAGE, workspace.workspaceId);
      if (workspace.organisationId) {
        window.localStorage.setItem(ORGANISATION_ID_STORAGE, workspace.organisationId);
      }
      window.sessionStorage.setItem(SESSION_STORAGE, JSON.stringify(workspace));
      setWorkspace(workspace);
      setAccess("authenticated");
      const destination = launchScenario === "sbr-doctor" ? "/settings/sbr?doctor=1" : "/overview";
      window.history.replaceState(null, "", destination);
      setActivePath(destination);
    },
    [launchScenario],
  );

  if (activePath === "/privacy") {
    return (
      <ScenarioFrame scenario={launchScenario}>
        <PrivacyScreen onBack={() => navigate(privacyReturnPath.current)} />
      </ScenarioFrame>
    );
  }

  if (access === "unconfigured") {
    return (
      <ScenarioFrame scenario={launchScenario}>
        <SetupScreen
          organisation={conveyor.organisation}
          onAuthenticated={authenticated}
          onPrivacy={() => navigate("/privacy")}
          reporting={conveyor.reporting}
          workspace={conveyor.workspace}
        />
      </ScenarioFrame>
    );
  }

  if (access === "locked") {
    const organisationId = window.localStorage.getItem(ORGANISATION_ID_STORAGE);
    if (!organisationId) {
      return (
        <ScenarioFrame scenario={launchScenario}>
          <SetupScreen
            organisation={conveyor.organisation}
            onAuthenticated={authenticated}
            onPrivacy={() => navigate("/privacy")}
            reporting={conveyor.reporting}
            workspace={conveyor.workspace}
          />
        </ScenarioFrame>
      );
    }
    return (
      <ScenarioFrame scenario={launchScenario}>
        <UnlockScreen
          organisation={conveyor.organisation}
          onAuthenticated={authenticated}
          onPrivacy={() => navigate("/privacy")}
          organisationId={organisationId}
          workspace={conveyor.workspace}
        />
      </ScenarioFrame>
    );
  }

  return (
    <ScenarioFrame scenario={launchScenario}>
      <AppShell
        activePath={activePath}
        onNavigate={navigate}
        {...(workspace?.organisationDisplayName
          ? { organisationDisplayName: workspace.organisationDisplayName }
          : {})}
      >
        <EngineStatus onRetry={loadDiagnostics} state={diagnosticsState} />
        <RouteContent
          conveyor={conveyor}
          onNavigate={navigate}
          path={activePath}
          state={diagnosticsState}
          settingsProjection={settingsProjection}
          settingsAuthorityKey={settingsAuthorityKey}
          onReloadSettings={reloadSettings}
          onReloadDiagnostics={loadDiagnostics}
          workspace={workspace}
        />
      </AppShell>
    </ScenarioFrame>
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
        aria-label="Local engine"
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
        aria-label="Local engine"
        className="mb-4 flex items-center gap-2 text-[14px] text-muted-foreground"
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
      aria-label="Local engine"
      className="mb-4 flex items-center justify-between gap-4 rounded-xl border border-warning-border bg-warning-soft px-3 py-2"
      role="status"
    >
      <div className="flex items-center gap-2 text-[14px] text-foreground">
        <AlertTriangle aria-hidden="true" className="size-3.5 text-warning" />
        Local engine unavailable. Your data has not left this device.
      </div>
      <Button className="h-7 text-[14px]" onClick={onRetry} type="button" variant="outline">
        <RefreshCw aria-hidden="true" className="size-3" />
        Retry local engine
      </Button>
    </div>
  );
}

function RouteContent(props: Parameters<typeof ActiveRouteContent>[0]) {
  const authorityKey = props.workspace
    ? `${props.workspace.workspaceId}:${props.workspace.userId}:${props.workspace.sessionId}:${props.workspace.organisationId ?? ""}`
    : undefined;
  const [invoiceVisitedFor, setInvoiceVisitedFor] = useState<string>();
  const showingInvoice = props.path === "/invoices";
  const retainInvoice = Boolean(
    authorityKey && (showingInvoice || invoiceVisitedFor === authorityKey),
  );
  useEffect(() => {
    if (showingInvoice) setInvoiceVisitedFor(authorityKey);
    else if (invoiceVisitedFor !== authorityKey) setInvoiceVisitedFor(undefined);
  }, [authorityKey, invoiceVisitedFor, showingInvoice]);

  return (
    <>
      <div aria-hidden={!showingInvoice} hidden={!showingInvoice} inert={!showingInvoice}>
        {retainInvoice ? (
          <ConnectedInvoicesScreen
            conveyor={props.conveyor}
            key={authorityKey}
            workspace={props.workspace}
          />
        ) : null}
      </div>
      {!showingInvoice ? <ActiveRouteContent {...props} /> : null}
    </>
  );
}

function ActiveRouteContent({
  conveyor,
  onNavigate,
  path,
  settingsAuthorityKey,
  settingsProjection,
  onReloadSettings,
  onReloadDiagnostics,
  state,
  workspace,
}: {
  readonly conveyor: TammyConveyor;
  readonly onNavigate: (path: string) => void;
  readonly path: string;
  readonly settingsAuthorityKey: string | undefined;
  readonly settingsProjection: SettingsProjectionState;
  readonly onReloadSettings: () => void;
  readonly onReloadDiagnostics: () => void;
  readonly state: DiagnosticsState;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const initialBasPreparationTarget = useMemo(() => parseBasPreparationRouteTarget(path), [path]);
  const initialDocumentId = useMemo(
    () => new URLSearchParams(path.split("?")[1]).get("document") ?? undefined,
    [path],
  );
  if (path === "/overview")
    return (
      <OverviewScreen
        accountingApi={conveyor.accounting}
        organisationApi={conveyor.organisation}
        api={conveyor.reporting}
        onOpenBas={onNavigate}
        onReviewInvoices={() => onNavigate("/invoices")}
        workspace={workspace}
      />
    );
  if (path === "/documents" || path.startsWith("/documents?"))
    return (
      <DocumentsScreen
        key={`${workspace?.workspaceId}:${workspace?.organisationId}:${workspace?.userId}:${workspace?.sessionId}`}
        api={conveyor.documents}
        purchaseApi={conveyor.purchases}
        accountingApi={conveyor.accounting}
        {...(initialDocumentId ? { initialDocumentId } : {})}
        onDocumentsChanged={() => conveyor.reporting.getAttentionSummary.invalidate()}
        workspace={workspace}
      />
    );
  if (path === "/banking")
    return (
      <BankingScreen
        api={conveyor.banking}
        onBankingChanged={() => conveyor.reporting.getAttentionSummary.invalidate()}
        workspace={workspace}
      />
    );
  if (path === "/gst-bas" || path.startsWith("/gst-bas?"))
    return (
      <BasLifecycleScreen
        key={`${workspace?.workspaceId}:${workspace?.userId}:${workspace?.sessionId}:${workspace?.organisationId ?? ""}`}
        api={conveyor.reporting}
        reportApi={conveyor.reporting}
        lifecycleApi={conveyor.basPreparation}
        identityApi={conveyor.organisation}
        organisationApi={conveyor.organisation}
        roleControlApi={conveyor.organisation}
        purchaseApi={conveyor.purchases}
        documentApi={conveyor.documents}
        accountingApi={conveyor.accounting}
        {...(initialBasPreparationTarget
          ? { initialPreparationTarget: initialBasPreparationTarget }
          : {})}
        onReviewDocument={(id) => onNavigate(`/documents?document=${encodeURIComponent(id)}`)}
        onBasDraftChanged={() => conveyor.reporting.getAttentionSummary.invalidate()}
        workspace={workspace}
      />
    );
  if (path === "/accounting/chart") {
    return <ChartScreen api={conveyor.accounting} workspace={workspace} />;
  }
  if (path.startsWith("/accounting/journals")) {
    return (
      <JournalsScreen
        api={conveyor.accounting}
        onJournalPosted={() =>
          Promise.all([
            conveyor.accounting.getTrialBalance.invalidate(),
            conveyor.reporting.getAttentionSummary.invalidate(),
          ]).then(() => undefined)
        }
        onNavigate={onNavigate}
        path={path}
        workspace={workspace}
      />
    );
  }
  if (path === "/accounting/trial-balance") {
    return <TrialBalanceScreen api={conveyor.accounting} workspace={workspace} />;
  }
  if (path === "/audit") {
    return (
      <AuditScreen
        accounting={conveyor.accounting}
        banking={conveyor.banking}
        documents={conveyor.documents}
        reporting={conveyor.reporting}
        workspace={workspace}
      />
    );
  }
  if (path === "/settings") {
    return (
      <div className="grid min-w-0 gap-6">
        <ScreenHeading
          area="Your workspace"
          title="Settings"
          description="Your business, local access and the tools that keep your records on this device."
        />
        <div className="grid min-w-0 items-start gap-5 min-[1100px]:grid-cols-2">
          <section className="work-surface min-w-0">
            <SurfaceHeading
              title="Your local workspace"
              description="Business identity and reporting settings belong to this workspace."
            />
            <div className="grid gap-5 p-5">
              <dl className="m-0 grid gap-4 text-[14px]">
                <div className="grid gap-1">
                  <dt className="text-[12px] text-muted-foreground">Business</dt>
                  <dd className="m-0 break-words font-semibold">
                    {workspace?.organisationDisplayName ?? "Local workspace"}
                  </dd>
                </div>
                <div className="grid gap-1">
                  <dt className="text-[12px] text-muted-foreground">Accounting records</dt>
                  <dd className="m-0">Encrypted on this device</dd>
                </div>
              </dl>
              <div className="flex flex-wrap gap-2 border-t border-border pt-4">
                <Button
                  onClick={() => onNavigate("/settings/organisation")}
                  type="button"
                  variant="outline"
                >
                  Organisation details
                </Button>
                <Button onClick={() => onNavigate("/settings/sbr")} type="button" variant="outline">
                  SBR readiness
                </Button>
              </div>
            </div>
          </section>
          <PrivacyStatement onPrivacy={() => onNavigate("/privacy")} />
        </div>
        <DiagnosticsCard onRetry={onReloadDiagnostics} state={state} />
      </div>
    );
  }
  if (path === "/settings/organisation") {
    if (settingsProjection.status !== "ready" || settingsProjection.key !== settingsAuthorityKey) {
      return <SettingsProjectionStatus state={settingsProjection} />;
    }
    const trustedWorkspace = settingsProjection.workspace;
    return (
      <div className="grid min-w-0 gap-6">
        <ScreenHeading
          area="Settings / organisation"
          title="Organisation"
          description="Keep your business identity and the evidence behind it clear and connected."
        />
        <dl className="work-surface m-0 grid min-w-0 px-5 text-[14px]">
          <div className="grid grid-cols-[180px_minmax(0,1fr)] gap-2 border-b border-border py-4 max-[640px]:grid-cols-1">
            <dt className="text-muted-foreground">Display name</dt>
            <dd className="m-0 min-w-0 font-medium text-foreground [overflow-wrap:anywhere]">
              {trustedWorkspace.organisationDisplayName}
            </dd>
          </div>
          <div className="grid grid-cols-[180px_minmax(0,1fr)] gap-2 py-4 max-[640px]:grid-cols-1">
            <dt className="text-muted-foreground">Canonical ABN</dt>
            <dd className="m-0 font-medium text-foreground">
              {trustedWorkspace.organisationCanonicalAbn}
            </dd>
          </div>
        </dl>
        <OrganisationVerificationForm
          api={conveyor.organisation}
          onChanged={onReloadSettings}
          workspace={trustedWorkspace}
        />
      </div>
    );
  }
  if (path === "/settings/sbr" || path === "/settings/sbr?doctor=1") {
    if (settingsProjection.status !== "ready" || settingsProjection.key !== settingsAuthorityKey) {
      return <SettingsProjectionStatus state={settingsProjection} />;
    }
    return (
      <SbrReadinessScreen
        doctorMode={path === "/settings/sbr?doctor=1"}
        onNavigate={onNavigate}
        organisation={conveyor.organisation}
        sbr={conveyor.sbr}
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
