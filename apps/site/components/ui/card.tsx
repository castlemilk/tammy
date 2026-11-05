import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

export function Card({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("rounded-xl border border-border bg-card text-card-foreground shadow-sm",className)} {...props}/>;
}
export function CardHeader({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-col gap-1 p-5 pb-3",className)} {...props}/>;
}
export function CardTitle({ className, children, ...props }: ComponentProps<"h3">) {
  return <h3 className={cn("font-semibold text-xl leading-tight text-foreground",className)} {...props}>{children}</h3>;
}
export function CardDescription({ className, ...props }: ComponentProps<"p">) {
  return <p className={cn("text-sm leading-6 text-muted-foreground",className)} {...props}/>;
}
export function CardContent({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("p-5 pt-2",className)} {...props}/>;
}
export function CardFooter({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-wrap items-center gap-2 border-t border-border p-5",className)} {...props}/>;
}
