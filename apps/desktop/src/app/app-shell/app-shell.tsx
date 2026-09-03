import { ChevronDown, Menu } from "lucide-react";
import type { ReactNode } from "react";

import { Navigation } from "./navigation";

interface AppShellProps {
  readonly activePath: string;
  readonly children: ReactNode;
  readonly onNavigate: (path: string) => void;
}

export function AppShell({ activePath, children, onNavigate }: AppShellProps) {
  return (
    <div className="grid min-h-screen grid-cols-[148px_minmax(0,1fr)] bg-background max-[900px]:grid-cols-[68px_minmax(0,1fr)]">
      <aside className="flex min-h-screen flex-col border-r border-border bg-surface">
        <div className="flex h-10 shrink-0 items-center justify-between border-b border-border px-3 max-[900px]:justify-center max-[900px]:px-2">
          <span className="font-serif text-[15px] font-bold tracking-[-0.02em] text-forest max-[900px]:sr-only">
            Tammy
          </span>
          <Menu
            aria-hidden="true"
            className="size-[14px] text-muted-foreground"
            strokeWidth={1.8}
          />
        </div>
        <Navigation activePath={activePath} onNavigate={onNavigate} />
      </aside>

      <div className="min-w-0">
        <header className="flex h-10 items-center justify-between border-b border-border bg-surface px-4 [-webkit-app-region:drag]">
          <div className="flex items-center gap-2 text-[10px] font-medium text-muted-foreground">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-success" />
            <span>Local data</span>
          </div>
          <button
            className="focus-ring flex items-center gap-2 rounded px-1.5 py-1 text-[10px] font-medium text-foreground [-webkit-app-region:no-drag]"
            type="button"
          >
            <span className="grid size-[19px] place-items-center rounded-full bg-forest text-[8px] font-bold text-white">
              TB
            </span>
            <span>Tammy Business</span>
            <ChevronDown aria-hidden="true" className="size-3 text-muted-foreground" />
          </button>
        </header>
        <main className="min-h-[calc(100vh-40px)] px-5 py-6 max-[700px]:px-4">{children}</main>
      </div>
    </div>
  );
}
