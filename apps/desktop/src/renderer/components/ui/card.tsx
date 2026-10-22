import type * as React from "react";

import { cn } from "../../lib/utils";

// Source: ui.shadcn.com/r/styles/new-york-v4/card.json; audited with shadcn@4.13.1.
export function Card({ className, ...props }: React.ComponentProps<"section">) {
  return (
    <section
      className={cn(
        "rounded-lg border border-border bg-surface text-surface-foreground",
        className,
      )}
      data-slot="card"
      {...props}
    />
  );
}

export function CardHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div className={cn("grid gap-1.5 px-6 pt-6", className)} data-slot="card-header" {...props} />
  );
}

export function CardContent({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={cn("px-6 pb-6", className)} data-slot="card-content" {...props} />;
}
