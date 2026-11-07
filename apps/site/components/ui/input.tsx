import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

export function Input({ className, type = "text", ...props }: ComponentProps<"input">) {
  return <input data-slot="input" type={type} className={cn(
    "flex min-h-11 w-full min-w-0 rounded-md border border-input bg-card px-3 py-2 text-sm text-foreground",
    "placeholder:text-muted-foreground",
    "focus-visible:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
    "aria-invalid:border-destructive disabled:cursor-not-allowed disabled:opacity-50",
    "motion-reduce:transition-none",
    className,
  )} {...props}/>;
}
