import { Leaf, LockKeyhole } from "lucide-react";
import type { ReactNode } from "react";

import { Navigation } from "./navigation";

interface AppShellProps {
  readonly activePath: string;
  readonly children: ReactNode;
  readonly onNavigate: (path: string) => void;
}

export function AppShell({ activePath, children, onNavigate }: AppShellProps) {
  return (
    <div className="grid min-h-dvh grid-cols-[216px_minmax(0,1fr)] bg-background max-[900px]:grid-cols-[64px_minmax(0,1fr)]">
      <a
        className="focus-ring sr-only z-10 rounded-xl bg-surface p-3 focus:not-sr-only focus:fixed focus:left-4 focus:top-4"
        href="#workspace-content"
      >
        Skip to content
      </a>
      <aside className="sticky top-0 flex h-dvh flex-col border-r border-border bg-surface">
        <div className="flex h-20 shrink-0 items-center gap-2 px-6 max-[900px]:justify-center max-[900px]:px-2">
          <span className="font-serif text-[36px] font-bold tracking-[-0.04em] text-foreground max-[900px]:hidden">
            tammy
          </span>
          <Leaf
            aria-hidden="true"
            className="hidden size-6 text-forest max-[900px]:block"
            strokeWidth={1.8}
          />
        </div>
        <Navigation activePath={activePath} onNavigate={onNavigate} />
        <p className="px-6 pb-5 text-[12px] text-muted-foreground max-[900px]:hidden">
          A little clarity, every day.
        </p>
      </aside>
      <div className="min-w-0">
        <header className="flex min-h-16 flex-wrap items-center justify-between gap-2 border-b border-border px-8 py-3 max-[700px]:px-4 [-webkit-app-region:drag]">
          <div className="flex items-center gap-2 text-[12px] font-medium text-muted-foreground">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-success" />
            <span>Local data</span>
          </div>
          <div className="flex items-center gap-2 text-[12px] text-muted-foreground">
            <LockKeyhole aria-hidden="true" className="size-4 text-forest" strokeWidth={1.8} />
            <span>Local workspace</span>
          </div>
        </header>
        <main
          className="min-h-[calc(100dvh-64px)] px-8 py-8 outline-none max-[700px]:px-4 max-[700px]:py-6"
          id="workspace-content"
          tabIndex={-1}
        >
          {children}
        </main>
      </div>
    </div>
  );
}
