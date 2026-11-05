import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

/** Native checkbox: keeps label activation, keyboard toggling and print-friendly state. */
export function Checkbox({ className, ...props }: Omit<ComponentProps<"input">, "type">) {
  return <input
    data-slot="checkbox"
    type="checkbox"
    className={cn(
      "size-5 shrink-0 rounded-sm border border-input accent-primary",
      "focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/35",
      "disabled:cursor-not-allowed disabled:opacity-50",
      className,
    )}
    {...props}
  />;
}
