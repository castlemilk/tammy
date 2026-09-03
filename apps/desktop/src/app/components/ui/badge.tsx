import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";

import { cn } from "../../lib/utils";

// Source: ui.shadcn.com/r/styles/new-york-v4/badge.json; audited with shadcn@4.13.1.
const badgeVariants = cva(
  "inline-flex w-fit shrink-0 items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-semibold leading-4",
  {
    variants: {
      variant: {
        default: "border-transparent bg-primary text-primary-foreground",
        offline: "border-ready-border bg-ready-soft text-ready-foreground",
        outline: "border-border bg-transparent text-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

export function Badge({
  className,
  variant,
  ...props
}: React.ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />;
}
