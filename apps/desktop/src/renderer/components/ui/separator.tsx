import type * as React from "react";

import { cn } from "../../lib/utils";

// Source: ui.shadcn.com/r/styles/new-york-v4/separator.json; audited with shadcn@4.13.1.
export function Separator({
  className,
  decorative = true,
  orientation = "horizontal",
  ...props
}: React.ComponentProps<"div"> & {
  decorative?: boolean;
  orientation?: "horizontal" | "vertical";
}) {
  const accessibilityProps = decorative
    ? ({ "aria-hidden": true, role: "none" } as const)
    : ({ "aria-orientation": orientation, role: "separator" } as const);

  return (
    <div
      className={cn(
        "shrink-0 bg-border",
        orientation === "horizontal" ? "h-px w-full" : "h-full w-px",
        className,
      )}
      data-slot="separator"
      {...accessibilityProps}
      {...props}
    />
  );
}
