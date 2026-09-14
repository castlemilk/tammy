import { create } from "@bufbuild/protobuf";
import {
  ApprovedFileRefSchema,
  AuthenticationContextSchema,
  SecretInputSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
  Role,
  SignInRequestSchema,
  SignInResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  UnlockWorkspaceRequestSchema,
  UnlockWorkspaceResponseSchema,
  WorkspaceUnlockProofSchema,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { KeyRound, LoaderCircle } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const unlockCodec = createProtoMethodCodec({
  input: UnlockWorkspaceRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: UnlockWorkspaceResponseSchema,
});
const signInCodec = createProtoMethodCodec({
  input: SignInRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: SignInResponseSchema,
});
const currentUserCodec = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetCurrentUserResponseSchema,
});
const organisationCodec = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetOrganisationResponseSchema,
});

interface UnlockScreenProps {
  readonly organisation: Pick<
    TammyConveyorModule<"organisation">,
    "getCurrentUser" | "getOrganisation"
  >;
  readonly onAuthenticated: (workspace: AuthenticatedWorkspace) => void;
  readonly onPrivacy?: () => void;
  readonly organisationId: string;
  readonly workspace: Pick<TammyConveyorModule<"workspace">, "signIn" | "unlockWorkspace">;
}

function fieldClassName(): string {
  return "focus-ring h-9 w-full rounded-[6px] border border-border bg-surface px-3 text-[12px] text-foreground outline-none placeholder:text-muted-foreground";
}

export function UnlockScreen({
  organisation,
  onAuthenticated,
  onPrivacy,
  organisationId,
  workspace,
}: UnlockScreenProps) {
  const [workspacePassphrase, setWorkspacePassphrase] = useState("");
  const [username, setUsername] = useState("");
  const [administratorPassword, setAdministratorPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [pendingAuthentication, setPendingAuthentication] = useState<{
    readonly sessionId: string;
    readonly userId: string;
    readonly workspaceId: string;
  }>();
  const completing = useRef<string | undefined>(undefined);
  const queryInputs = useMemo(() => {
    const authentication = create(AuthenticationContextSchema, {
      actorUserId: pendingAuthentication?.userId ?? "",
      sessionId: pendingAuthentication?.sessionId ?? "",
    });
    return {
      organisation: organisationCodec.encodeRequest(
        create(GetOrganisationRequestSchema, { authentication, organisationId }),
      ),
      user: currentUserCodec.encodeRequest(create(GetCurrentUserRequestSchema, { authentication })),
    };
  }, [organisationId, pendingAuthentication?.sessionId, pendingAuthentication?.userId]);
  const currentUserQuery = organisation.getCurrentUser.useQuery({
    enabled: Boolean(pendingAuthentication),
    input: queryInputs.user,
    retry: false,
  });
  const currentOrganisationQuery = organisation.getOrganisation.useQuery({
    enabled: Boolean(pendingAuthentication),
    input: queryInputs.organisation,
    retry: false,
  });

  useEffect(() => {
    if (!pendingAuthentication) return;
    const key = `${pendingAuthentication.sessionId}:${pendingAuthentication.userId}`;
    if (currentUserQuery.isError || currentOrganisationQuery.isError) {
      setPendingAuthentication(undefined);
      setBusy(false);
      setError("The workspace could not be unlocked. Check your passphrase and sign-in details.");
      return;
    }
    if (!currentUserQuery.data || !currentOrganisationQuery.data || completing.current === key)
      return;
    completing.current = key;
    try {
      const currentUser = currentUserCodec.decodeResponse(currentUserQuery.data);
      const currentOrganisation = organisationCodec.decodeResponse(currentOrganisationQuery.data);
      if (
        !currentUser.user ||
        currentUser.user.id !== pendingAuthentication.userId ||
        !validRoles(currentUser.user.roles)
      )
        throw new Error("invalid current user");
      if (
        !currentOrganisation.organisation ||
        currentOrganisation.organisation.id !== organisationId ||
        !currentOrganisation.organisation.displayName ||
        !/^[0-9]{11}$/.test(currentOrganisation.organisation.abn)
      )
        throw new Error("invalid organisation");
      setWorkspacePassphrase("");
      setAdministratorPassword("");
      onAuthenticated({
        sessionId: pendingAuthentication.sessionId,
        userId: pendingAuthentication.userId,
        workspaceId: pendingAuthentication.workspaceId,
        organisationId,
        organisationDisplayName: currentOrganisation.organisation.displayName,
        organisationCanonicalAbn: currentOrganisation.organisation.abn,
        roles: [...currentUser.user.roles],
      });
    } catch {
      setError("The workspace could not be unlocked. Check your passphrase and sign-in details.");
    } finally {
      completing.current = undefined;
      setPendingAuthentication(undefined);
      setBusy(false);
    }
  }, [
    currentOrganisationQuery.data,
    currentOrganisationQuery.isError,
    currentUserQuery.data,
    currentUserQuery.isError,
    onAuthenticated,
    organisationId,
    pendingAuthentication,
  ]);

  const unlockAndSignIn = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const proof = create(WorkspaceUnlockProofSchema, {
        proof: {
          case: "passphrase",
          value: create(SecretInputSchema, {
            utf8: new TextEncoder().encode(workspacePassphrase),
          }),
        },
      });
      const unlock = create(UnlockWorkspaceRequestSchema, {
        workspaceFile: create(ApprovedFileRefSchema, { capabilityId: "local-workspace-file" }),
        proof,
      });
      const opened = unlockCodec.decodeResponse(
        await workspace.unlockWorkspace(unlockCodec.encodeRequest(unlock)),
      );
      if (!opened.workspace?.id) throw new Error("invalid workspace");

      const signIn = create(SignInRequestSchema, {
        username,
        password: create(SecretInputSchema, {
          utf8: new TextEncoder().encode(administratorPassword),
        }),
      });
      const authenticated = signInCodec.decodeResponse(
        await workspace.signIn(signInCodec.encodeRequest(signIn)),
      );
      if (!authenticated.user || !authenticated.session) throw new Error("invalid session");
      setPendingAuthentication({
        sessionId: authenticated.session.id,
        userId: authenticated.user.id,
        workspaceId: opened.workspace.id,
      });
    } catch {
      setError("The workspace could not be unlocked. Check your passphrase and sign-in details.");
      setBusy(false);
    }
  };

  return (
    <main className="grid min-h-screen place-items-center bg-background px-5 py-8">
      <section className="w-full max-w-[440px] rounded-[8px] border border-border bg-surface p-7 shadow-sm">
        <div className="mb-6 flex items-start gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-full bg-forest-soft text-forest">
            <KeyRound aria-hidden="true" className="size-4" />
          </span>
          <div>
            <p className="font-serif text-[16px] font-bold text-forest">Tammy</p>
            <h1 className="mt-1 text-[20px] font-semibold tracking-[-0.02em] text-foreground">
              Unlock your workspace
            </h1>
            <p className="mt-1 text-[11px] leading-5 text-muted-foreground">
              Your encrypted accounting data stays on this device.
            </p>
          </div>
        </div>

        <form className="grid gap-4" onSubmit={unlockAndSignIn}>
          <label className="grid gap-1.5 text-[11px] font-medium text-foreground">
            Workspace passphrase
            <input
              autoComplete="current-password"
              className={fieldClassName()}
              minLength={16}
              onChange={(event) => setWorkspacePassphrase(event.target.value)}
              required
              type="password"
              value={workspacePassphrase}
            />
          </label>
          <label className="grid gap-1.5 text-[11px] font-medium text-foreground">
            Email or username
            <input
              autoComplete="username"
              className={fieldClassName()}
              onChange={(event) => setUsername(event.target.value)}
              required
              value={username}
            />
          </label>
          <label className="grid gap-1.5 text-[11px] font-medium text-foreground">
            Administrator password
            <input
              autoComplete="current-password"
              className={fieldClassName()}
              minLength={16}
              onChange={(event) => setAdministratorPassword(event.target.value)}
              required
              type="password"
              value={administratorPassword}
            />
          </label>
          <Button className="mt-1 h-9 w-full text-[11px]" disabled={busy} type="submit">
            {busy ? <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" /> : null}
            Unlock workspace
          </Button>
        </form>
        {error ? (
          <p className="mt-4 text-[11px] text-destructive" role="alert">
            {error}
          </p>
        ) : null}
        {onPrivacy ? (
          <button
            className="focus-ring mt-4 text-[10px] font-medium text-forest underline"
            onClick={onPrivacy}
            type="button"
          >
            Privacy and support
          </button>
        ) : null}
      </section>
    </main>
  );
}

function validRoles(roles: readonly Role[]): boolean {
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
