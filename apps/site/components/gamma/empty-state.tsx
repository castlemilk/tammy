import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../../lib/utils";

/**
 * Gamma catalogue "Empty state" composition, adapted to Tammy's Tailwind /
 * shadcn tokens rather than importing Gamma's complete stylesheet.
 * The call to action is always supplied by the consumer as real children.
 */
export type GammaEmptyStateProps = Omit<HTMLAttributes<HTMLDivElement>, "title"> & {
  title: string;
  description: string;
  icon?: ReactNode;
};
export function GammaEmptyState({
  title, description, icon, children, className, ...props
}: GammaEmptyStateProps) {
  return <div {...props} className={cn(
    "flex min-w-0 flex-col items-center gap-3 rounded-[var(--gamma-radius-panel)]",
    "border border-[var(--gamma-border)] bg-[var(--gamma-surface-2)]",
    "px-6 py-10 text-center text-[var(--gamma-ink)]",className,
  )}>
    {icon && <span aria-hidden="true" className="grid size-11 place-items-center rounded-[var(--gamma-radius-control)] bg-[var(--gamma-surface)]">{icon}</span>}
    <h3 className="max-w-[35ch] text-lg font-semibold tracking-tight">{title}</h3>
    <p className="max-w-[45ch] text-sm leading-6 text-[var(--gamma-muted)]">{description}</p>
    {children && <div className="mt-2 flex flex-wrap items-center justify-center gap-2">{children}</div>}
  </div>;
}
