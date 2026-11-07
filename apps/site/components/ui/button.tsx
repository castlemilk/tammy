import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

export type ButtonVariant = "default" | "secondary" | "outline" | "ghost" | "destructive";
export type ButtonSize = "default" | "sm" | "lg" | "icon";
export interface ButtonProps extends ComponentProps<"button"> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
}

/** shadcn/ui-style native primitive, adapted to Tammy's Soft Precision tokens. */
export function Button({
  className, type = "button", variant = "default", size = "default",
  loading = false, disabled, children, ...props
}: ButtonProps) {
  const variants: Record<ButtonVariant, string> = {
    default: "bg-primary text-primary-foreground hover:bg-primary/90",
    secondary: "bg-secondary text-secondary-foreground hover:bg-secondary/80",
    outline: "border border-border bg-card text-foreground hover:bg-secondary",
    ghost: "text-foreground hover:bg-secondary",
    destructive: "bg-destructive text-primary-foreground hover:bg-destructive/90",
  };
  const sizes: Record<ButtonSize, string> = {
    default: "min-h-11 px-4 py-2",
    sm: "min-h-11 px-3 py-2 text-xs",
    lg: "min-h-12 px-6 py-3",
    icon: "size-11 p-2",
  };
  return <button data-slot="button" type={type} disabled={disabled || loading} aria-busy={loading || undefined}
    className={cn(
      "relative inline-flex cursor-pointer items-center justify-center gap-2 whitespace-normal rounded-md",
      "font-semibold text-sm leading-5 transition-[background-color,color,border-color] duration-150",
      "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
      "disabled:pointer-events-none disabled:opacity-45 motion-reduce:transition-none",
      variants[variant],sizes[size],className,
    )} {...props}>
    <span className={cn("inline-flex items-center justify-center gap-2", loading && "opacity-0")}>{children}</span>
    {loading && <span aria-hidden="true" className="absolute size-3.5 animate-spin rounded-full border-2 border-current border-t-transparent motion-reduce:animate-none"/>}
  </button>;
}
