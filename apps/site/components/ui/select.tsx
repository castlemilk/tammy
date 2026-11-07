import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

/** Native select used for years and source choices; keyboard and screen reader friendly. */
export function Select({ className, ...props }: ComponentProps<"select">) {
  return <select
    data-slot="select"
    className={cn(
      "min-h-11 w-full min-w-0 rounded-md border border-input bg-card",
      "px-3 py-2 text-sm text-foreground cursor-pointer",
      "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
      "disabled:cursor-not-allowed disabled:opacity-50",
      className,
    )}
    {...props}
  />;
}
