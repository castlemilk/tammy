import type { ReactNode, Ref } from "react";

export function ScreenHeading({
  area,
  title,
  description,
  action,
  variant = "display",
  headingRef,
  status,
}: {
  readonly area: string;
  readonly title: string;
  readonly description: ReactNode;
  readonly action?: ReactNode;
  readonly variant?: "display" | "workspace";
  readonly headingRef?: Ref<HTMLHeadingElement>;
  readonly status?: ReactNode;
}) {
  return (
    <header
      className={`flex flex-wrap ${status ? "items-start" : "items-end"} justify-between gap-4`}
    >
      <div className="max-w-[720px]">
        <p className={variant === "workspace" ? "sr-only" : "work-eyebrow mb-2"}>{area}</p>
        <div className="flex flex-wrap items-center gap-3">
          <h1
            className={
              variant === "workspace"
                ? "text-[26px] font-semibold leading-8 tracking-[-0.025em] outline-none"
                : "page-title"
            }
            ref={headingRef}
            tabIndex={-1}
          >
            {title}
          </h1>
          {status}
        </div>
        <p className="mt-2 text-[14px] leading-6 text-muted-foreground">{description}</p>
      </div>
      {action ? <div className="flex flex-wrap items-center gap-2">{action}</div> : null}
    </header>
  );
}

export function SurfaceHeading({
  title,
  headingId,
  description,
  action,
}: {
  readonly title: string;
  readonly headingId?: string;
  readonly description?: string;
  readonly action?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-5 py-4">
      <div className="min-w-0">
        <h2 className="text-[16px] font-semibold leading-6 text-foreground" id={headingId}>
          {title}
        </h2>
        {description ? (
          <p className="mt-1 max-w-[720px] text-[13px] leading-5 text-muted-foreground">
            {description}
          </p>
        ) : null}
      </div>
      {action ? <div className="flex shrink-0 flex-wrap items-center gap-2">{action}</div> : null}
    </div>
  );
}
