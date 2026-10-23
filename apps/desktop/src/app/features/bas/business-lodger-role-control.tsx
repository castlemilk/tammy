import { create } from "@bufbuild/protobuf";
import { CommandContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  AssignRolesRequestSchema,
  AssignRolesResponseSchema,
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
  Role,
  type User,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { useMemo, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const currentUserCodec = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetCurrentUserResponseSchema,
});
const assignRolesCodec = createProtoMethodCodec({
  input: AssignRolesRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: AssignRolesResponseSchema,
});
type RoleControlAPI = Pick<TammyConveyorModule<"organisation">, "getCurrentUser" | "assignRoles">;
type PendingAttempt = {
  readonly frame: Uint8Array;
  readonly expectedVersion: bigint;
  readonly roles: readonly Role[];
  readonly desiredLodger: boolean;
  readonly operatorName: string;
};

function uuidV7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  let milliseconds = Date.now();
  for (let index = 5; index >= 0; index -= 1) {
    bytes[index] = milliseconds & 0xff;
    milliseconds = Math.floor(milliseconds / 256);
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function isCompleteRoleSet(roles: readonly Role[]): boolean {
  return (
    roles.length > 0 &&
    roles.every(
      (role) =>
        role === Role.WORKSPACE_ADMIN ||
        role === Role.BUSINESS_PREPARER ||
        role === Role.BUSINESS_LODGER ||
        role === Role.AUDITOR,
    ) &&
    new Set(roles).size === roles.length &&
    roles.every((role, index) => index === 0 || role > (roles[index - 1] ?? 0))
  );
}

function sameRoles(left: readonly Role[], right: readonly Role[]): boolean {
  return left.length === right.length && left.every((role, index) => role === right[index]);
}

function decodeCurrentUser(frame: Uint8Array | undefined): User | undefined {
  if (!frame) return undefined;
  try {
    return currentUserCodec.decodeResponse(frame).user;
  } catch {
    return undefined;
  }
}

function roleName(role: Role): string {
  switch (role) {
    case Role.WORKSPACE_ADMIN:
      return "Workspace admin";
    case Role.BUSINESS_PREPARER:
      return "Business preparer";
    case Role.BUSINESS_LODGER:
      return "Business lodger";
    case Role.AUDITOR:
      return "Auditor";
    default:
      return "Unknown";
  }
}

export function BASLodgerRoleControl({
  api,
  workspace,
}: {
  readonly api: RoleControlAPI;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const actorUserId = workspace?.userId ?? "";
  const sessionId = workspace?.sessionId ?? "";
  const authentication = useMemo(() => ({ actorUserId, sessionId }), [actorUserId, sessionId]);
  const currentUserInput = useMemo(
    () => currentUserCodec.encodeRequest(create(GetCurrentUserRequestSchema, { authentication })),
    [authentication],
  );
  const currentUserQuery = api.getCurrentUser.useQuery({
    enabled: Boolean(actorUserId && sessionId),
    input: currentUserInput,
    retry: false,
    refetchOnMount: "always",
  });
  const currentUser = useMemo(
    () => decodeCurrentUser(currentUserQuery.data),
    [currentUserQuery.data],
  );
  const currentUserIsValid = Boolean(
    !currentUserQuery.isError &&
      workspace &&
      currentUser &&
      currentUser.id === actorUserId &&
      currentUser.version > 0n &&
      isCompleteRoleSet(currentUser.roles),
  );
  const isAdministrator = Boolean(
    currentUserIsValid && currentUser?.roles.includes(Role.WORKSPACE_ADMIN),
  );
  const liveLodgerRole = currentUser?.roles.includes(Role.BUSINESS_LODGER) ?? false;
  const [desiredLodger, setDesiredLodger] = useState<boolean>();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [needsReview, setNeedsReview] = useState(false);
  const [pendingAttempt, setPendingAttempt] = useState<PendingAttempt>();
  const [message, setMessage] = useState<string>();
  const [error, setError] = useState<string>();
  const checked = desiredLodger ?? liveLodgerRole;
  const operatorName = currentUser?.displayName.trim() || currentUser?.username || "this operator";

  if (!actorUserId || !sessionId) return null;
  if (currentUserQuery.isError) {
    return (
      <section className="work-surface grid gap-4 p-5" aria-label="BAS approval authority">
        <h2 className="text-[16px] font-semibold">BAS approval authority</h2>
        <p className="text-[12px] text-muted-foreground" role="status">
          Current roles could not be verified, so role controls are unavailable.
        </p>
        <Button
          className="h-11 justify-self-start text-[13px]"
          onClick={() => void currentUserQuery.refetch()}
          type="button"
          variant="outline"
        >
          Refresh current roles
        </Button>
      </section>
    );
  }
  if (!currentUserQuery.data || !currentUserIsValid || !isAdministrator || !currentUser)
    return null;

  const executeAttempt = async (attempt: PendingAttempt) => {
    setBusy(true);
    setError(undefined);
    setMessage(undefined);
    try {
      const responseFrame = await api.assignRoles(attempt.frame);
      const saved = assignRolesCodec.decodeResponse(responseFrame).user;
      if (
        !saved ||
        saved.id !== actorUserId ||
        saved.version <= attempt.expectedVersion ||
        !isCompleteRoleSet(saved.roles) ||
        !sameRoles(saved.roles, attempt.roles)
      )
        throw new Error("The resulting role set did not match the confirmed change.");
      setPendingAttempt(undefined);
      setDesiredLodger(undefined);
      setConfirming(false);
      setNeedsReview(false);
      setMessage(`BAS approval authority updated for ${attempt.operatorName}.`);
      await api.getCurrentUser.invalidate(currentUserInput);
    } catch {
      // Keep the exact protobuf frame and idempotency key for a safe retry.
      setPendingAttempt(attempt);
      setConfirming(false);
      setError(
        "Core did not confirm the role change. Retry the exact request or refresh roles before starting again.",
      );
    } finally {
      setBusy(false);
    }
  };

  const confirmRoleChange = async () => {
    if (!currentUser || desiredLodger === undefined || desiredLodger === liveLodgerRole) return;
    setBusy(true);
    setError(undefined);
    setMessage(undefined);
    try {
      const refreshed = await currentUserQuery.refetch();
      const fresh = refreshed.isError ? undefined : decodeCurrentUser(refreshed.data);
      if (
        !fresh ||
        fresh.id !== actorUserId ||
        fresh.version <= 0n ||
        !isCompleteRoleSet(fresh.roles) ||
        !fresh.roles.includes(Role.WORKSPACE_ADMIN)
      ) {
        setDesiredLodger(undefined);
        setConfirming(false);
        setNeedsReview(true);
        setError(
          "Current administrator roles could not be verified. Refresh and review them before trying again.",
        );
        return;
      }
      if (fresh.version !== currentUser.version || !sameRoles(fresh.roles, currentUser.roles)) {
        setDesiredLodger(undefined);
        setConfirming(false);
        setNeedsReview(true);
        setError(
          "Current roles changed while you reviewed this action. Review the updated roles and choose again.",
        );
        return;
      }
      if (fresh.roles.includes(Role.BUSINESS_LODGER) === desiredLodger) {
        setDesiredLodger(undefined);
        setConfirming(false);
        setMessage("The current role already matches that choice.");
        return;
      }

      const nextRoles = new Set(fresh.roles);
      if (desiredLodger) nextRoles.add(Role.BUSINESS_LODGER);
      else nextRoles.delete(Role.BUSINESS_LODGER);
      const roles = [...nextRoles].sort((left, right) => left - right);
      if (!isCompleteRoleSet(roles)) throw new Error("The complete role set is invalid.");
      const request = create(AssignRolesRequestSchema, {
        commandContext: create(CommandContextSchema, {
          idempotencyKey: uuidV7(),
          authentication,
        }),
        userId: fresh.id,
        expectedVersion: fresh.version,
        roles,
      });
      const attempt: PendingAttempt = {
        frame: assignRolesCodec.encodeRequest(request),
        expectedVersion: fresh.version,
        roles,
        desiredLodger,
        operatorName: fresh.displayName.trim() || fresh.username,
      };
      setPendingAttempt(attempt);
      await executeAttempt(attempt);
    } catch {
      setConfirming(false);
      setNeedsReview(true);
      setError(
        "The role change could not be prepared. Refresh the current roles before choosing again.",
      );
    } finally {
      setBusy(false);
    }
  };

  const refreshAndDiscardAttempt = async () => {
    setBusy(true);
    setError(undefined);
    setMessage(undefined);
    try {
      const refreshed = await currentUserQuery.refetch();
      const fresh = refreshed.isError ? undefined : decodeCurrentUser(refreshed.data);
      if (
        !fresh ||
        fresh.id !== actorUserId ||
        fresh.version <= 0n ||
        !isCompleteRoleSet(fresh.roles) ||
        !fresh.roles.includes(Role.WORKSPACE_ADMIN)
      ) {
        setError(
          "Current roles could not be verified. The pending request is preserved until a fresh read succeeds.",
        );
        return;
      }
      setPendingAttempt(undefined);
      setDesiredLodger(undefined);
      setConfirming(false);
      setNeedsReview(true);
      setMessage(
        "Current roles were refreshed. Review the complete role set before choosing again.",
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="work-surface grid gap-4 p-5" aria-label="BAS approval authority">
      <div>
        <h2 className="text-[16px] font-semibold">BAS approval authority</h2>
        <p className="mt-1 text-[14px] text-muted-foreground">
          Manage only this account’s business lodger role. Other roles are preserved.
        </p>
        <p className="mt-1 text-[12px] text-muted-foreground">
          {operatorName} · Current roles: {currentUser.roles.map(roleName).join(", ")}
        </p>
      </div>
      {pendingAttempt ? (
        <p className="text-[12px] text-muted-foreground" role="status">
          The last request may have completed. Retrying uses the same request and idempotency key.
        </p>
      ) : null}
      {needsReview ? (
        <p className="text-[12px] text-muted-foreground" role="status">
          Review the current complete role set above before choosing a new value.
        </p>
      ) : null}
      <label className="flex min-h-11 items-center gap-3 text-[13px]">
        <input
          checked={checked}
          className="focus-ring size-5 accent-[var(--color-primary)]"
          disabled={busy || Boolean(pendingAttempt)}
          onChange={(event) => {
            setDesiredLodger(event.currentTarget.checked);
            setNeedsReview(false);
            setConfirming(false);
            setError(undefined);
            setMessage(undefined);
          }}
          type="checkbox"
        />
        Grant business lodger approval authority for BAS actions
      </label>
      {confirming && desiredLodger !== undefined ? (
        <fieldset
          className="grid gap-4 rounded-xl border border-border bg-muted/30 p-4"
          aria-label="Confirm BAS role change"
        >
          <p className="text-[14px] leading-6">
            Confirm {desiredLodger ? "adding" : "removing"} the business lodger role for{" "}
            {operatorName}. This changes only this account’s business lodger role.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button
              className="h-11 text-[13px]"
              disabled={busy}
              onClick={() => void confirmRoleChange()}
              type="button"
            >
              {busy ? "Checking current roles…" : "Confirm and save"}
            </Button>
            <Button
              className="h-11 text-[13px]"
              disabled={busy}
              onClick={() => setConfirming(false)}
              type="button"
              variant="outline"
            >
              Cancel
            </Button>
          </div>
        </fieldset>
      ) : null}
      {error ? (
        <p className="text-[12px] text-destructive" role="alert">
          {error}
        </p>
      ) : null}
      {message ? (
        <p className="text-[12px] text-forest" role="status">
          {message}
        </p>
      ) : null}
      {pendingAttempt ? (
        <div className="flex flex-wrap gap-2">
          <Button
            className="h-11 text-[13px]"
            disabled={busy}
            onClick={() => void executeAttempt(pendingAttempt)}
            type="button"
          >
            {busy ? "Retrying…" : "Retry exact request"}
          </Button>
          <Button
            className="h-11 text-[13px]"
            disabled={busy}
            onClick={() => void refreshAndDiscardAttempt()}
            type="button"
            variant="outline"
          >
            Refresh roles and cancel retry
          </Button>
        </div>
      ) : !confirming ? (
        <Button
          className="h-11 justify-self-start text-[13px]"
          disabled={busy || needsReview || checked === liveLodgerRole}
          onClick={() => {
            setError(undefined);
            setMessage(undefined);
            setConfirming(true);
          }}
          type="button"
        >
          Review role change
        </Button>
      ) : null}
    </section>
  );
}
