import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

/** Tammy's shadcn-compatible, locally owned Tailwind button primitive.
 * Intentionally uses a native button: no hidden navigation or submit action.
 */
const variants = {
  default: "border border-primary bg-primary text-primary-foreground hover:bg-primary/90",
  secondary: "border border-secondary bg-secondary text-secondary-foreground hover:bg-secondary/80",
  outline: "border border-border bg-card text-foreground hover:border-primary hover:bg-secondary",
  ghost: "border border-transparent bg-transparent text-foreground hover:bg-secondary",
  destructive: "border border-red-800 bg-red-800 text-white hover:bg-red-900",
} as const;
const sizes = {
  default: "min-h-11 px-4 py-2.5",
  sm: "min-h-11 px-3 py-2 text-xs",
  lg: "min-h-12 px-6 py-3",
  icon: "size-11 shrink-0 p-2",
} as const;

export type ButtonProps = ComponentProps<"button"> & {
  variant?: keyof typeof variants;
  size?: keyof typeof sizes;
};

export function Button({
  className, variant = "default", size = "default", type = "button", ...props
}: ButtonProps) {
  return <button
    data-slot="button"
    type={type}
    className={cn(
      "inline-flex items-center justify-center gap-2 rounded-md font-semibold",
      "text-sm leading-tight transition-colors duration-150",
      "focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50",
      "disabled:pointer-events-none disabled:opacity-45",
      "motion-reduce:transition-none",
      variants[variant], sizes[size], className,
    )}
    {...props}
  />;
}
