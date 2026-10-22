import type { ReactNode } from "react";
import { cn } from "../lib/utils";

export function HorizontalScrollArea({
  label,
  className,
  children,
}: {
  readonly label: string;
  readonly className?: string;
  readonly children: ReactNode;
}) {
  return (
    <section
      aria-label={label}
      className={cn("focus-ring relative min-w-0 overflow-x-auto", className)}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: This labelled overflow region must accept keyboard scrolling when table cells are read-only.
      tabIndex={0}
    >
      {children}
    </section>
  );
}
