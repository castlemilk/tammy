import { BookOpenText, LayoutGrid, LockKeyhole } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "./components/ui/button";
import { DiagnosticsCard, type DiagnosticsState } from "./features/diagnostics/diagnostics-card";

const navigation = [{ href: "#overview", icon: LayoutGrid, label: "Overview" }] as const;

export function App() {
  const [diagnosticsState, setDiagnosticsState] = useState<DiagnosticsState>({
    status: "loading",
  });
  const requestSequence = useRef(0);

  const loadDiagnostics = useCallback(async () => {
    const request = requestSequence.current + 1;
    requestSequence.current = request;
    setDiagnosticsState({ status: "loading" });

    try {
      const diagnostics = await window.tammy.getSystemDiagnostics();
      if (requestSequence.current === request) {
        setDiagnosticsState({ diagnostics, status: "ready" });
      }
    } catch {
      if (requestSequence.current === request) {
        setDiagnosticsState({ status: "unavailable" });
      }
    }
  }, []);

  useEffect(() => {
    void loadDiagnostics();
    return () => {
      requestSequence.current += 1;
    };
  }, [loadDiagnostics]);

  return (
    <div className="grid min-h-screen grid-cols-[216px_minmax(0,1fr)] bg-background max-[720px]:grid-cols-[72px_minmax(0,1fr)]">
      <aside className="flex min-h-screen flex-col bg-rail text-rail-foreground">
        <div className="flex h-11 shrink-0 items-center gap-2.5 border-b border-rail-border px-4 max-[720px]:justify-center max-[720px]:px-2">
          <BookOpenText aria-hidden="true" className="size-[18px] shrink-0 text-ready-light" />
          <span className="text-sm font-bold tracking-tight max-[720px]:sr-only">Tammy</span>
        </div>

        <div className="px-3 pt-5 max-[720px]:px-2">
          <div className="mb-4 px-2 max-[720px]:sr-only">
            <p className="text-[11px] font-semibold uppercase tracking-[0.14em] text-rail-muted">
              Workspace
            </p>
            <p className="mt-1 truncate text-sm font-semibold">Local ledger</p>
          </div>

          <nav aria-label="Workspace">
            <ul>
              {navigation.map((item) => {
                const Icon = item.icon;
                return (
                  <li key={item.label}>
                    <a
                      aria-current="page"
                      className="focus-ring-rail flex h-9 items-center gap-2.5 rounded-md bg-rail-active px-2.5 text-sm font-semibold outline-none transition-colors hover:bg-rail-active max-[720px]:justify-center max-[720px]:px-2"
                      href={item.href}
                      title={item.label}
                    >
                      <Icon aria-hidden="true" className="size-4 shrink-0 text-ready-light" />
                      <span className="max-[720px]:sr-only">{item.label}</span>
                    </a>
                  </li>
                );
              })}
            </ul>
          </nav>
        </div>

        <div className="mt-auto border-t border-rail-border p-4 max-[720px]:p-3">
          <div className="flex items-center gap-2 text-xs text-rail-muted max-[720px]:justify-center">
            <LockKeyhole aria-hidden="true" className="size-3.5 shrink-0" />
            <span className="max-[720px]:sr-only">Private on this device</span>
          </div>
        </div>
      </aside>

      <div className="min-w-0">
        <header className="flex h-11 items-center border-b border-border bg-surface px-6 [-webkit-app-region:drag] max-[720px]:px-4">
          <span className="text-xs font-medium text-muted-foreground">Local workspace</span>
        </header>

        <main className="px-8 py-8 max-[720px]:px-4 max-[720px]:py-6" id="overview">
          <div className="mx-auto grid w-full max-w-[960px] gap-8">
            <div className="grid max-w-[680px] gap-2">
              <h1 className="text-2xl font-bold tracking-[-0.025em] text-foreground">Overview</h1>
              <p className="text-sm leading-6 text-muted-foreground">
                Your accounting workspace stays on this device and remains available without a
                network.
              </p>
            </div>

            <DiagnosticsCard onRetry={loadDiagnostics} state={diagnosticsState} />

            <div className="flex max-w-[680px] items-center justify-between gap-4 border-t border-border pt-6 max-[520px]:items-start max-[520px]:flex-col">
              <div className="grid gap-1">
                <p className="text-sm font-semibold text-foreground">Accounting workspace</p>
                <p className="text-xs leading-5 text-muted-foreground">
                  Setup will unlock after the local foundation is complete.
                </p>
              </div>
              <Button
                aria-label="Workspace setup comes next"
                className="max-[520px]:w-full"
                disabled
                type="button"
              >
                Workspace setup comes next
              </Button>
            </div>
          </div>
        </main>
      </div>
    </div>
  );
}
