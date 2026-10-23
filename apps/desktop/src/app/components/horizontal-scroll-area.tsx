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
      // biome-ignore lint/a11y/noNoninteractiveTabindex: A labelled overflow region accepts keyboard scrolling for read-only tables.
      tabIndex={0}
    >
      {children}
    </section>
  );
}
