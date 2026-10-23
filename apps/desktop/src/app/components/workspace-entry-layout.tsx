import type { ReactNode } from "react";

interface WorkspaceEntryLayoutProps {
  readonly children: ReactNode;
  readonly description: string;
  readonly onPrivacy?: () => void;
  readonly stage: "details" | "recovery" | "unlock";
  readonly title: string;
}

const setupSteps = ["Business details", "Save recovery code", "Open your workspace"] as const;

export function WorkspaceEntryLayout({
  children,
  description,
  onPrivacy,
  stage,
  title,
}: WorkspaceEntryLayoutProps) {
  const currentStep = stage === "details" ? 0 : stage === "recovery" ? 1 : -1;

  return (
    <main className="min-h-screen bg-background px-4 py-6 sm:px-7 sm:py-10">
      <div className="mx-auto grid w-full max-w-6xl gap-7 lg:grid-cols-[minmax(250px,0.8fr)_minmax(0,1.2fr)] lg:gap-12">
        <aside className="flex min-w-0 flex-col justify-between gap-7 lg:py-5">
          <div>
            <div className="flex items-center gap-3">
              <span className="grid size-10 place-items-center rounded-xl bg-forest text-[15px] font-semibold text-primary-foreground">
                t
              </span>
              <span className="font-serif text-[30px] font-bold tracking-[-0.045em] text-foreground">
                tammy
              </span>
            </div>
            <div className="mt-7 max-w-md lg:mt-16">
              <p className="text-[12px] font-semibold uppercase tracking-[0.16em] text-forest">
                Your local accounting desk
              </p>
              <h2 className="mt-3 max-w-sm font-serif text-[26px] leading-[1.12] tracking-[-0.035em] text-foreground sm:text-[32px]">
                Clear records, kept close.
              </h2>
              <p className="mt-3 max-w-sm text-[14px] leading-6 text-muted-foreground">
                Set up the business details you use for your local books, then protect the workspace
                with credentials you control.
              </p>
            </div>
          </div>

          {currentStep >= 0 ? (
            <ol aria-label="Workspace setup steps" className="grid gap-3">
              {setupSteps.map((step, index) => {
                const active = index === currentStep;
                const complete = index < currentStep;
                return (
                  <li
                    aria-current={active ? "step" : undefined}
                    className={`flex items-center gap-3 text-[12px] ${active ? "font-semibold text-foreground" : "text-muted-foreground"}`}
                    key={step}
                  >
                    <span
                      aria-hidden="true"
                      className={`grid size-7 shrink-0 place-items-center rounded-full border text-[12px] ${complete || active ? "border-forest bg-forest-soft text-forest" : "border-border bg-surface"}`}
                    >
                      {complete ? "✓" : String(index + 1).padStart(2, "0")}
                    </span>
                    {step}
                  </li>
                );
              })}
            </ol>
          ) : (
            <p className="max-w-sm border-l-2 border-forest/30 pl-4 text-[14px] leading-6 text-muted-foreground">
              Your saved workspace remains encrypted on this device.
            </p>
          )}

          <div className="flex flex-wrap items-end justify-between gap-4 border-t border-border pt-4">
            <p className="max-w-xs text-[14px] leading-6 text-muted-foreground">
              Tammy keeps workspace data on this device. Recovery details are shown once during
              setup.
            </p>
            {onPrivacy ? (
              <button
                className="focus-ring inline-flex min-h-11 shrink-0 items-center rounded-md text-[12px] font-medium text-forest underline underline-offset-2"
                onClick={onPrivacy}
                type="button"
              >
                Privacy and support
              </button>
            ) : null}
          </div>
        </aside>

        <section className="min-w-0 rounded-2xl border border-border bg-surface px-5 py-6 shadow-sm sm:px-8 sm:py-8">
          <header className="mb-6">
            <h1 className="page-title">{title}</h1>
            <p className="mt-2 max-w-2xl text-[14px] leading-6 text-muted-foreground">
              {description}
            </p>
          </header>
          {children}
        </section>
      </div>
    </main>
  );
}
