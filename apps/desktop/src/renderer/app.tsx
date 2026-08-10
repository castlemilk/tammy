import { AlertTriangle, LoaderCircle, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { AppShell } from "./app-shell/app-shell";
import { resolveAppLocation, type WorkspaceAccess } from "./app-shell/router";
import { Button } from "./components/ui/button";
import { ChartScreen } from "./features/accounting/chart-screen";
import { JournalsScreen } from "./features/accounting/journals-screen";
import { TrialBalanceScreen } from "./features/accounting/trial-balance-screen";
import { DiagnosticsCard, type DiagnosticsState } from "./features/diagnostics/diagnostics-card";
import { EmptyLedgerScreen } from "./features/ledger/empty-ledger-screen";
import { DocumentsScreen } from "./features/documents/documents-screen";
import { BankingScreen } from "./features/banking/banking-screen";
import { BasScreen } from "./features/bas/bas-screen";
import { OverviewScreen } from "./features/overview/overview-screen";
import { SetupScreen, type AuthenticatedWorkspace } from "./features/setup/setup-screen";
import { UnlockScreen } from "./features/workspace/unlock-screen";

const WORKSPACE_ID_STORAGE = "tammy.workspace.id";
const ORGANISATION_ID_STORAGE = "tammy.organisation.id";
const SESSION_STORAGE = "tammy.session.active";

function storedAuthenticatedWorkspace(): AuthenticatedWorkspace | undefined {
  const retained = window.sessionStorage.getItem(SESSION_STORAGE);
  if (!retained) return undefined;
  try {
    const parsed = JSON.parse(retained) as Partial<AuthenticatedWorkspace>;
    if (typeof parsed.workspaceId === "string" && typeof parsed.userId === "string" &&
      typeof parsed.sessionId === "string" && parsed.workspaceId && parsed.userId && parsed.sessionId) {
      return {
        workspaceId: parsed.workspaceId,
        userId: parsed.userId,
        sessionId: parsed.sessionId,
        ...(typeof parsed.organisationId === "string" && parsed.organisationId
          ? { organisationId: parsed.organisationId }
          : {}),
      };
    }
  } catch {
    return undefined;
  }
  return undefined;
}

function initialAccess(): WorkspaceAccess {
  if (window.sessionStorage.getItem(SESSION_STORAGE)) return "authenticated";
  if (window.localStorage.getItem(WORKSPACE_ID_STORAGE)) return "locked";
  return "unconfigured";
}

function currentLocation(): string {
  const location = `${window.location.pathname}${window.location.search}`;
  return location === "/" ? "/overview" : location;
}

export function App() {
  const [diagnosticsState, setDiagnosticsState] = useState<DiagnosticsState>({ status: "loading" });
  const [access, setAccess] = useState<WorkspaceAccess>(initialAccess);
  const [workspace, setWorkspace] = useState<AuthenticatedWorkspace | undefined>(storedAuthenticatedWorkspace);
  const [activePath, setActivePath] = useState(() =>
    resolveAppLocation(currentLocation(), initialAccess()).path,
  );
  const requestSequence = useRef(0);

  const loadDiagnostics = useCallback(async () => {
    const request = requestSequence.current + 1;
    requestSequence.current = request;
    setDiagnosticsState({ status: "loading" });
    try {
      const diagnostics = await window.tammy.getSystemDiagnostics();
      if (requestSequence.current === request) setDiagnosticsState({ diagnostics, status: "ready" });
    } catch {
      if (requestSequence.current === request) setDiagnosticsState({ status: "unavailable" });
    }
  }, []);

  useEffect(() => {
    void loadDiagnostics();
    return () => {
      requestSequence.current += 1;
    };
  }, [loadDiagnostics]);

  useEffect(() => {
    const restore = () => setActivePath(resolveAppLocation(currentLocation(), access).path);
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, [access]);

  const navigate = useCallback((path: string) => {
    const resolved = resolveAppLocation(path, access);
    window.history.pushState(null, "", resolved.path);
    setActivePath(resolved.path);
  }, [access]);

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

  if (access === "unconfigured") {
    return <SetupScreen api={window.tammy} onAuthenticated={authenticated} />;
  }

  if (access === "locked") {
    const organisationId = window.localStorage.getItem(ORGANISATION_ID_STORAGE);
    return (
      <UnlockScreen
        api={window.tammy}
        onAuthenticated={authenticated}
        {...(organisationId ? { organisationId } : {})}
      />
    );
  }

  return (
    <AppShell activePath={activePath} onNavigate={navigate}>
      <EngineStatus onRetry={loadDiagnostics} state={diagnosticsState} />
      <RouteContent onNavigate={navigate} path={activePath} state={diagnosticsState} workspace={workspace} />
    </AppShell>
  );
}

function EngineStatus({ onRetry, state }: { readonly onRetry: () => void; readonly state: DiagnosticsState }) {
  if (state.status === "ready") {
    return (
      <p aria-live="polite" className="sr-only" data-startup-transition="starting-to-ready" role="status">
        Local engine ready. Offline. No cloud required.
      </p>
    );
  }
  if (state.status === "loading") {
    return (
      <div aria-live="polite" className="mb-4 flex items-center gap-2 text-[10px] text-muted-foreground" role="status">
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

function RouteContent({ onNavigate, path, state, workspace }: { readonly onNavigate: (path: string) => void; readonly path: string; readonly state: DiagnosticsState; readonly workspace: AuthenticatedWorkspace | undefined }) {
  if (path === "/overview") return <OverviewScreen api={window.tammy} workspace={workspace} />;
  if (path === "/documents") return <DocumentsScreen api={window.tammy} workspace={workspace} />;
  if (path === "/banking") return <BankingScreen api={window.tammy} workspace={workspace} />;
  if (path === "/gst-bas") return <BasScreen api={window.tammy} workspace={workspace} />;
  if (path === "/accounting/chart") {
    return <ChartScreen api={window.tammy} workspace={workspace} />;
  }
  if (path.startsWith("/accounting/journals")) {
    return <JournalsScreen api={window.tammy} onNavigate={onNavigate} path={path} workspace={workspace} />;
  }
  if (path === "/accounting/general-ledger") {
    return <EmptyLedgerScreen description="Account movements with retained source links." emptyLabel="No ledger movements yet" title="General ledger" />;
  }
  if (path === "/accounting/trial-balance") {
    return <TrialBalanceScreen api={window.tammy} workspace={workspace} />;
  }
  if (path === "/audit") {
    return <EmptyLedgerScreen description="Verifiable business actions retained on this device." emptyLabel="No audit events yet" title="Audit trail" />;
  }
  if (path === "/settings") {
    return (
      <div className="mx-auto grid w-full max-w-[920px] gap-5">
        <div>
          <h1 className="text-[18px] font-semibold tracking-[-0.02em] text-foreground">Settings</h1>
          <p className="mt-1 text-[11px] leading-5 text-muted-foreground">Local workspace and system information.</p>
        </div>
        <DiagnosticsCard onRetry={() => undefined} state={state} />
      </div>
    );
  }
  return <EmptyLedgerScreen description="This workspace screen is not yet connected." emptyLabel="Workspace action unavailable" title="Workspace" />;
}
