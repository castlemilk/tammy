import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

/** Native text input with shadcn/Tailwind semantic tokens. */
export function Input({ className, type = "text", ...props }: ComponentProps<"input">) {
  return <input
    data-slot="input"
    type={type}
    className={cn(
      "flex min-h-11 w-full min-w-0 rounded-md border border-input",
      "bg-card px-3 py-2 text-sm text-foreground shadow-xs outline-none",
      "placeholder:text-muted-foreground",
      "focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/35",
      "aria-invalid:border-red-800 aria-invalid:ring-red-800/20",
      "disabled:cursor-not-allowed disabled:opacity-50",
      className,
    )}
    {...props}
  />;
}
