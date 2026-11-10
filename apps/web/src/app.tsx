import { useCallback, useEffect, useState } from "react";
import { ChartScreen, JournalsScreen, type PendingJournal, TrialBalanceScreen } from "./accounting";
import { ApiError, api } from "./api";
import { WorkspaceAccess } from "./auth";
import { BankingScreen } from "./banking";
import { BasScreen } from "./bas";
import { DocumentsScreen } from "./documents";
import { ConnectionsScreen, IntelligenceScreen } from "./insights";
import { OverviewScreen } from "./overview";
import { navigation, Shell } from "./shell";
import type { Session } from "./types";
import { errorMessage } from "./utils";

export function App() {
  const [session, setSession] = useState<Session>();
  const [error, setError] = useState<string>();
  const [attempt, setAttempt] = useState(0);
  const [pendingJournal, setPendingJournal] = useState<PendingJournal>();
  const [path, setPath] = useState(() =>
    navigation.some((item) => item.path === window.location.pathname)
      ? window.location.pathname
      : "/overview",
  );
  // biome-ignore lint/correctness/useExhaustiveDependencies: attempt deliberately retries the initial session request.
  useEffect(() => {
    let active = true;
    setError(undefined);
    void api.session().then(
      (value) => {
        if (active) setSession(value);
      },
      (cause: unknown) => {
        if (active) setError(errorMessage(cause));
      },
    );
    return () => {
      active = false;
    };
  }, [attempt]);
  useEffect(() => {
    const pop = () =>
      setPath(
        navigation.some((item) => item.path === window.location.pathname)
          ? window.location.pathname
          : "/overview",
      );
    window.addEventListener("popstate", pop);
    return () => window.removeEventListener("popstate", pop);
  }, []);
  useEffect(() => {
    if (!pendingJournal) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [pendingJournal]);
  useEffect(() => {
    let refreshing = false;
    let active = true;
    const expired = () => {
      if (refreshing) return;
      refreshing = true;
      setSession(undefined);
      void api
        .session()
        .then(
          (value) => {
            if (active) setSession(value);
          },
          (cause: unknown) => {
            if (active) setError(errorMessage(cause));
          },
        )
        .finally(() => {
          refreshing = false;
        });
    };
    window.addEventListener("tammy:session-expired", expired);
    return () => {
      active = false;
      window.removeEventListener("tammy:session-expired", expired);
    };
  }, []);
  const navigate = useCallback((next: string) => {
    window.history.pushState(null, "", next);
    setPath(next);
    window.scrollTo?.(0, 0);
  }, []);
  if (error)
    return (
      <main className="auth-page">
        <section className="auth-card">
          <span className="wordmark">Tammy</span>
          <h1>Local service unavailable</h1>
          <p>{error}</p>
          <button
            className="button primary"
            onClick={() => setAttempt((value) => value + 1)}
            type="button"
          >
            Try again
          </button>
        </section>
      </main>
    );
  if (!session)
    return (
      <main className="auth-page">
        <p role="status">Starting Tammy…</p>
      </main>
    );
  if (session.state !== "authenticated")
    return <WorkspaceAccess onSession={setSession} session={session} />;
  async function lock() {
    try {
      await api.logout();
      setSession(await api.session());
    } catch (cause) {
      if (!(cause instanceof ApiError && cause.status === 401)) setError(errorMessage(cause));
    }
  }
  return (
    <Shell navigate={navigate} onLock={() => void lock()} path={path} session={session}>
      {path === "/overview" ? (
        <OverviewScreen navigate={navigate} />
      ) : path === "/accounts" ? (
        <ChartScreen />
      ) : path === "/journals" ? (
        <JournalsScreen
          navigate={navigate}
          pending={pendingJournal}
          setPending={setPendingJournal}
        />
      ) : path === "/trial-balance" ? (
        <TrialBalanceScreen />
      ) : path === "/documents" ? (
        <DocumentsScreen />
      ) : path === "/banking" ? (
        <BankingScreen />
      ) : path === "/gst-bas" ? (
        <BasScreen />
      ) : path === "/intelligence" ? (
        <IntelligenceScreen navigate={navigate} />
      ) : (
        <ConnectionsScreen />
      )}
    </Shell>
  );
}
