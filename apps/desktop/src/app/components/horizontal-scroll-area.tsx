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
      onKeyDown={(event) => {
        if (
          event.target !== event.currentTarget ||
          event.altKey ||
          event.ctrlKey ||
          event.metaKey ||
          event.shiftKey ||
          (event.key !== "ArrowLeft" && event.key !== "ArrowRight") ||
          event.currentTarget.scrollWidth <= event.currentTarget.clientWidth
        )
          return;
        // Chromium's native arrow routing can miss a focused overflow region
        // after a resize. Keep the region's keyboard path explicit and immediate.
        event.preventDefault();
        event.currentTarget.scrollBy({
          left: event.key === "ArrowRight" ? 80 : -80,
          behavior: "instant",
        });
      }}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: A labelled overflow region accepts keyboard scrolling for read-only tables.
      tabIndex={0}
    >
      {children}
    </section>
  );
}
