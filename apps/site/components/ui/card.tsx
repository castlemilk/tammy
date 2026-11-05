import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

export function Card({ className, ...props }: ComponentProps<"div">) {
  return <div
    data-slot="card"
    className={cn("flex min-w-0 flex-col gap-5 rounded-xl border border-border bg-card text-card-foreground", className)}
    {...props}
  />;
}

export function CardHeader({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="card-header"
    className={cn("flex flex-col gap-2 px-6 pt-6", className)} {...props} />;
}

export function CardContent({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="card-content" className={cn("px-6", className)} {...props} />;
}

export function CardFooter({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="card-footer" className={cn("flex items-center gap-3 px-6 pb-6", className)} {...props} />;
}
