import type { ReactNode } from "react";

export function ScreenHeading({
  area,
  title,
  description,
  action,
}: {
  readonly area: string;
  readonly title: string;
  readonly description: ReactNode;
  readonly action?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div className="max-w-[720px]">
        <p className="work-eyebrow mb-2">{area}</p>
        <h1 className="page-title">{title}</h1>
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
