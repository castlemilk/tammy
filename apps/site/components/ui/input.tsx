import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

export function Input({ className, type = "text", ...props }: ComponentProps<"input">) {
  return <input type={type} className={cn(
    "flex min-h-11 w-full min-w-0 rounded-md border border-input bg-card px-3 py-2 text-sm text-foreground",
    "shadow-sm transition-colors outline-none placeholder:text-muted-foreground",
    "focus-visible:border-primary focus-visible:ring-3 focus-visible:ring-ring/35",
    "aria-invalid:border-red-700 aria-invalid:ring-red-700/15 disabled:cursor-not-allowed disabled:opacity-50",
    "motion-reduce:transition-none",
    className,
  )} {...props}/>;
}
