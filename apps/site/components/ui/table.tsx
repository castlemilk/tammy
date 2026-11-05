import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

export function Table({ className, ...props }: ComponentProps<"table">) {
  return <table className={cn("w-full caption-bottom text-sm tabular-nums",className)} {...props}/>;
}
export function TableHeader({ className, ...props }: ComponentProps<"thead">) {
  return <thead className={cn("bg-secondary/70 [&_tr]:border-b",className)} {...props}/>;
}
export function TableBody({ className, ...props }: ComponentProps<"tbody">) {
  return <tbody className={cn("[&_tr:last-child]:border-0",className)} {...props}/>;
}
export function TableRow({ className, ...props }: ComponentProps<"tr">) {
  return <tr className={cn("border-b border-border transition-colors hover:bg-secondary/35 motion-reduce:transition-none",className)} {...props}/>;
}
export function TableHead({ className, ...props }: ComponentProps<"th">) {
  return <th className={cn("px-3 py-3 text-left align-middle font-semibold text-muted-foreground",className)} {...props}/>;
}
export function TableCell({ className, ...props }: ComponentProps<"td">) {
  return <td className={cn("px-3 py-3 align-middle",className)} {...props}/>;
}
export function TableCaption({ className, ...props }: ComponentProps<"caption">) {
  return <caption className={cn("py-3 text-left text-xs leading-5 text-muted-foreground",className)} {...props}/>;
}
