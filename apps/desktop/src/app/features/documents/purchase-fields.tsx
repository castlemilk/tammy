import { create } from "@bufbuild/protobuf";
import { CivilDateSchema, CommandContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { Role } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import type { ReactNode } from "react";

import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { uuidV7 } from "./purchase-command-state";

export const purchaseFieldClass =
  "focus-ring h-11 min-w-0 w-full rounded-xl border border-border bg-background px-3 text-[14px] font-normal";
export function PurchaseField({
  label,
  value,
  onChange,
  type = "text",
  maxLength = 512,
}: {
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly type?: string;
  readonly maxLength?: number;
}) {
  return (
    <label className="grid min-w-0 gap-1.5 text-[12px] font-semibold text-foreground">
      {label}
      <input
        className={purchaseFieldClass}
        type={type}
        inputMode={label.includes("AUD") ? "decimal" : undefined}
        maxLength={maxLength}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </label>
  );
}
export function PurchaseChoice({
  label,
  value,
  onChange,
  children,
}: {
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly children: ReactNode;
}) {
  return (
    <label className="grid min-w-0 gap-1.5 text-[12px] font-semibold text-foreground">
      {label}
      <select
        className={purchaseFieldClass}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        {children}
      </select>
    </label>
  );
}
export function PurchaseCheck({
  label,
  checked,
  onChange,
}: {
  readonly label: string;
  readonly checked: boolean;
  readonly onChange: (value: boolean) => void;
}) {
  return (
    <label className="flex min-h-11 items-center gap-3 text-[14px] leading-6">
      <input
        className="focus-ring size-4 shrink-0"
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span>{label}</span>
    </label>
  );
}
export function civil(value: string) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  const date = new Date(`${value}T00:00:00Z`);
  if (!match || !Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== value)
    return undefined;
  return create(CivilDateSchema, {
    year: Number(match[1]),
    month: Number(match[2]),
    day: Number(match[3]),
  });
}
export function iso(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
) {
  return value
    ? `${value.year}-${String(value.month).padStart(2, "0")}-${String(value.day).padStart(2, "0")}`
    : "";
}
export function dateLabel(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
) {
  return value
    ? `${String(value.day).padStart(2, "0")}/${String(value.month).padStart(2, "0")}/${value.year}`
    : "—";
}
export function mayPrepare(workspace: AuthenticatedWorkspace | undefined): boolean {
  return Boolean(
    workspace?.roles?.some(
      (role) => role === Role.WORKSPACE_ADMIN || role === Role.BUSINESS_PREPARER,
    ),
  );
}
export function commandContext(workspace: AuthenticatedWorkspace) {
  return create(CommandContextSchema, {
    authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
    idempotencyKey: uuidV7(),
  });
}
