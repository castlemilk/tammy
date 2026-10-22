import { create } from "@bufbuild/protobuf";
import {
  ApprovedFileRefSchema,
  AuthenticationContextSchema,
  CommandContextSchema,
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
  CreateOrganisationRequestSchema,
  CreateOrganisationResponseSchema,
  GstBasis,
  GstReportingFrequency,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  ReportingEntityType,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import {
  ConfirmRecoveryRequestSchema,
  ConfirmRecoveryResponseSchema,
  CreateWorkspaceRequestSchema,
  CreateWorkspaceResponseSchema,
  RecoveryGroupConfirmationSchema,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { CheckCircle2, KeyRound, LoaderCircle } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createBundledTaxRuleSourceRef } from "../../../shared/bundled-tax-rule";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import paperLoop from "../../assets/paper-loop.png";
import { Button } from "../../components/ui/button";
import { ReportingCapabilityNotice } from "../reporting/reporting-capability-notice";

const createCodec = createProtoMethodCodec({
  input: CreateWorkspaceRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 65_536,
  output: CreateWorkspaceResponseSchema,
});
const confirmCodec = createProtoMethodCodec({
  input: ConfirmRecoveryRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: ConfirmRecoveryResponseSchema,
});
const signInCodec = createProtoMethodCodec({
  input: SignInRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: SignInResponseSchema,
});
const createOrganisationCodec = createProtoMethodCodec({
  input: CreateOrganisationRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: CreateOrganisationResponseSchema,
});
const currentUserCodec = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetCurrentUserResponseSchema,
});

export interface AuthenticatedWorkspace {
  readonly organisationCanonicalAbn?: string;
  readonly organisationDisplayName?: string;
  readonly organisationId?: string;
  readonly roles?: readonly Role[];
  readonly sessionId: string;
  readonly userId: string;
  readonly workspaceId: string;
}

interface SetupScreenProps {
  readonly organisation: Pick<
    TammyConveyorModule<"organisation">,
    "createOrganisation" | "getCurrentUser"
  >;
  readonly onAuthenticated: (workspace: AuthenticatedWorkspace) => void;
  readonly onPrivacy?: () => void;
  readonly reporting: Pick<TammyConveyorModule<"reporting">, "getReportingCapability">;
  readonly workspace: Pick<
    TammyConveyorModule<"workspace">,
    "confirmRecovery" | "createWorkspace" | "signIn"
  >;
}

interface PendingSetup {
  readonly gstBasis: GstBasis;
  readonly administratorPassword: string;
  readonly abn: string;
  readonly businessDisplayName: string;
  readonly businessLegalName: string;
  readonly recoveryCode: string;
  readonly setupId: string;
  readonly username: string;
  readonly workspaceId: string;
}

interface PendingAuthentication {
  readonly sessionId: string;
  readonly setup: PendingSetup;
  readonly userId: string;
}

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

function fieldClassName(): string {
  return "focus-ring h-11 w-full rounded-xl border border-border bg-surface px-3 text-[14px] text-foreground outline-none placeholder:text-muted-foreground";
}

export function SetupScreen({
  organisation,
  onAuthenticated,
  onPrivacy,
  reporting,
  workspace,
}: SetupScreenProps) {
  const [displayName, setDisplayName] = useState("");
  const [username, setUsername] = useState("");
  const [businessLegalName, setBusinessLegalName] = useState("");
  const [businessDisplayName, setBusinessDisplayName] = useState("");
  const [abn, setAbn] = useState("");
  const [gstBasis, setGstBasis] = useState(GstBasis.NON_CASH);
  const [workspacePassphrase, setWorkspacePassphrase] = useState("");
  const [administratorPassword, setAdministratorPassword] = useState("");
  const [pending, setPending] = useState<PendingSetup>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [pendingAuthentication, setPendingAuthentication] = useState<PendingAuthentication>();
  const completing = useRef<string | undefined>(undefined);
  const currentUserInput = useMemo(
    () =>
      currentUserCodec.encodeRequest(
        create(GetCurrentUserRequestSchema, {
          authentication: create(AuthenticationContextSchema, {
            actorUserId: pendingAuthentication?.userId ?? "",
            sessionId: pendingAuthentication?.sessionId ?? "",
          }),
        }),
      ),
    [pendingAuthentication?.sessionId, pendingAuthentication?.userId],
  );
  const currentUserQuery = organisation.getCurrentUser.useQuery({
    enabled: Boolean(pendingAuthentication),
    input: currentUserInput,
    retry: false,
  });

  useEffect(() => {
    if (!pendingAuthentication) return;
    const key = `${pendingAuthentication.sessionId}:${pendingAuthentication.userId}`;
    if (currentUserQuery.isError) {
      setPendingAuthentication(undefined);
      setBusy(false);
      setError("Recovery confirmation or sign in failed. Your workspace remains on this device.");
      return;
    }
    if (!currentUserQuery.data || completing.current === key) return;
    completing.current = key;
    void (async () => {
      try {
        const currentUser = currentUserCodec.decodeResponse(currentUserQuery.data);
        if (
          !currentUser.user ||
          currentUser.user.id !== pendingAuthentication.userId ||
          !validRoles(currentUser.user.roles)
        )
          throw new Error("invalid current user");
        const authentication = create(AuthenticationContextSchema, {
          actorUserId: pendingAuthentication.userId,
          sessionId: pendingAuthentication.sessionId,
        });
        const setup = pendingAuthentication.setup;
        const request = create(CreateOrganisationRequestSchema, {
          commandContext: create(CommandContextSchema, {
            idempotencyKey: uuidV7(),
            authentication,
          }),
          abn: setup.abn,
          legalName: setup.businessLegalName,
          displayName: setup.businessDisplayName,
          entityType: "AU_PRIVATE_COMPANY",
          gstBasis: setup.gstBasis,
          gstReportingFrequency: GstReportingFrequency.QUARTERLY,
          financialYearEndMonth: 6,
          activeTaxRuleBundle: createBundledTaxRuleSourceRef(),
        });
        const created = createOrganisationCodec.decodeResponse(
          await organisation.createOrganisation(createOrganisationCodec.encodeRequest(request)),
        );
        if (
          !created.organisation?.id ||
          !created.organisation.displayName ||
          !/^[0-9]{11}$/.test(created.organisation.abn)
        )
          throw new Error("invalid organisation");
        setAdministratorPassword("");
        setWorkspacePassphrase("");
        onAuthenticated({
          sessionId: pendingAuthentication.sessionId,
          userId: pendingAuthentication.userId,
          workspaceId: setup.workspaceId,
          organisationId: created.organisation.id,
          organisationDisplayName: created.organisation.displayName,
          organisationCanonicalAbn: created.organisation.abn,
          roles: [...currentUser.user.roles],
        });
      } catch {
        setError("Recovery confirmation or sign in failed. Your workspace remains on this device.");
      } finally {
        completing.current = undefined;
        setPendingAuthentication(undefined);
        setBusy(false);
      }
    })();
  }, [
    currentUserQuery.data,
    currentUserQuery.isError,
    onAuthenticated,
    organisation,
    pendingAuthentication,
  ]);

  const createWorkspace = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    const setupId = uuidV7();
    try {
      const request = create(CreateWorkspaceRequestSchema, {
        setupId,
        destination: create(ApprovedFileRefSchema, { capabilityId: "local-workspace-directory" }),
        workspacePassphrase: create(SecretInputSchema, {
          utf8: new TextEncoder().encode(workspacePassphrase),
        }),
        administratorUsername: username,
        administratorDisplayName: displayName,
        administratorPassword: create(SecretInputSchema, {
          utf8: new TextEncoder().encode(administratorPassword),
        }),
      });
      const response = createCodec.decodeResponse(
        await workspace.createWorkspace(createCodec.encodeRequest(request)),
      );
      if (!response.workspace || !response.recoverySecret?.utf8.byteLength)
        throw new Error("invalid");
      setPending({
        gstBasis,
        administratorPassword,
        abn,
        businessDisplayName,
        businessLegalName,
        recoveryCode: new TextDecoder().decode(response.recoverySecret.utf8),
        setupId,
        username,
        workspaceId: response.workspace.id,
      });
    } catch {
      setError("The local workspace could not be created. Check the details and try again.");
    } finally {
      setBusy(false);
    }
  };

  const confirmAndSignIn = async () => {
    if (!pending) return;
    setBusy(true);
    setError(undefined);
    try {
      const groups = pending.recoveryCode
        .replaceAll("-", "")
        .toUpperCase()
        .match(/.{1,4}/g);
      if (!groups || groups.length < 2) throw new Error("invalid recovery code");
      const confirmation = create(ConfirmRecoveryRequestSchema, {
        setupId: pending.setupId,
        confirmations: groups.slice(0, 2).map((value, groupIndex) =>
          create(RecoveryGroupConfirmationSchema, {
            groupIndex,
            value: new TextEncoder().encode(value),
          }),
        ),
      });
      const confirmed = confirmCodec.decodeResponse(
        await workspace.confirmRecovery(confirmCodec.encodeRequest(confirmation)),
      );
      if (confirmed.workspace?.id !== pending.workspaceId) throw new Error("invalid workspace");
      const signIn = create(SignInRequestSchema, {
        username: pending.username,
        password: create(SecretInputSchema, {
          utf8: new TextEncoder().encode(pending.administratorPassword),
        }),
      });
      const authenticated = signInCodec.decodeResponse(
        await workspace.signIn(signInCodec.encodeRequest(signIn)),
      );
      if (!authenticated.user || !authenticated.session) throw new Error("invalid session");
      setPendingAuthentication({
        sessionId: authenticated.session.id,
        setup: pending,
        userId: authenticated.user.id,
      });
    } catch {
      setError("Recovery confirmation or sign in failed. Your workspace remains on this device.");
      setBusy(false);
    }
  };

  return (
    <main className="grid min-h-screen place-items-center bg-background px-5 py-8">
      <section className="workspace-surface">
        <div className="mb-6 flex items-start gap-4">
          <span className="grid size-9 shrink-0 place-items-center rounded-full bg-forest-soft text-forest">
            <KeyRound aria-hidden="true" className="size-4" />
          </span>
          <div>
            <p className="font-serif text-[28px] font-bold tracking-[-0.04em] text-foreground">
              tammy
            </p>
            <h1 className="mt-1 text-foreground page-title">
              {pending ? "Save your recovery code" : "Create your local workspace"}
            </h1>
            <p className="mt-1 text-[14px] leading-5 text-muted-foreground">
              {pending
                ? "Keep this one-time code somewhere safe before continuing."
                : "Your accounting data stays encrypted on this device."}
            </p>
          </div>
          <img alt="" aria-hidden="true" className="arrival-art" src={paperLoop} />
        </div>

        <div className="mb-5">
          <ReportingCapabilityNotice
            api={reporting}
            entityType={ReportingEntityType.AU_BUSINESS}
            fallbackCopy="Local BAS preparation requires confirmed supported obligations and source evidence. Tammy does not declare or lodge a BAS with the ATO."
            report={ReportKind.GST_WORKPAPER}
            taxYear={2024}
          />
        </div>

        {pending ? (
          <div className="grid gap-5">
            <div className="rounded-xl border border-ready-border bg-forest-soft px-4 py-4">
              <p className="mb-2 flex items-center gap-2 text-[14px] font-semibold text-forest">
                <CheckCircle2 aria-hidden="true" className="size-4" /> One-time recovery code
              </p>
              <p className="break-all font-mono text-[13px] leading-6 text-foreground">
                {pending.recoveryCode}
              </p>
            </div>
            <Button
              className="h-11 w-full text-[14px]"
              disabled={busy}
              onClick={confirmAndSignIn}
              type="button"
            >
              {busy ? <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" /> : null}I
              saved my recovery code
            </Button>
          </div>
        ) : (
          <form className="grid gap-4" onSubmit={createWorkspace}>
            <label className="grid gap-1.5 text-[14px] font-medium text-foreground">
              Your name
              <input
                autoComplete="name"
                className={fieldClassName()}
                onChange={(event) => setDisplayName(event.target.value)}
                required
                value={displayName}
              />
            </label>
            <label className="grid gap-1.5 text-[14px] font-medium text-foreground">
              Email or username
              <input
                autoComplete="username"
                className={fieldClassName()}
                onChange={(event) => setUsername(event.target.value)}
                required
                value={username}
              />
            </label>
            <label className="grid gap-1.5 text-[14px] font-medium text-foreground">
              Business legal name
              <input
                className={fieldClassName()}
                maxLength={256}
                onChange={(event) => setBusinessLegalName(event.target.value)}
                required
                value={businessLegalName}
              />
            </label>
            <label className="grid gap-1.5 text-[14px] font-medium text-foreground">
              Business display name
              <input
                className={fieldClassName()}
                maxLength={256}
                onChange={(event) => setBusinessDisplayName(event.target.value)}
                required
                value={businessDisplayName}
              />
            </label>
            <label className="grid gap-1.5 text-[14px] font-medium text-foreground">
              ABN
              <input
                className={fieldClassName()}
                inputMode="numeric"
                maxLength={11}
                minLength={11}
                onChange={(event) => setAbn(event.target.value)}
                pattern="[0-9]{11}"
                required
                value={abn}
              />
            </label>
            <label className="grid gap-1.5 text-[14px] font-medium text-foreground">
              GST accounting basis
              <select
                aria-label="GST accounting basis"
                aria-describedby="gst-basis-description"
                className={fieldClassName()}
                onChange={(event) => setGstBasis(Number(event.target.value) as GstBasis)}
                value={gstBasis}
              >
                <option value={GstBasis.NON_CASH}>Non-cash (accrual)</option>
                <option value={GstBasis.CASH}>Cash</option>
              </select>
              <span
                id="gst-basis-description"
                className="text-[12px] font-normal leading-5 text-muted-foreground"
              >
                Use the basis your business reports to the ATO. Cash-basis invoice GST follows
                recorded receipts; cash purchase credits require payment evidence.
              </span>
            </label>
            <label className="grid gap-1.5 text-[14px] font-medium text-foreground">
              Workspace passphrase
              <input
                autoComplete="new-password"
                className={fieldClassName()}
                minLength={16}
                onChange={(event) => setWorkspacePassphrase(event.target.value)}
                required
                type="password"
                value={workspacePassphrase}
              />
            </label>
            <label className="grid gap-1.5 text-[14px] font-medium text-foreground">
              Administrator password
              <input
                autoComplete="new-password"
                className={fieldClassName()}
                minLength={16}
                onChange={(event) => setAdministratorPassword(event.target.value)}
                required
                type="password"
                value={administratorPassword}
              />
            </label>
            <Button className="mt-1 h-11 w-full text-[14px]" disabled={busy} type="submit">
              {busy ? <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" /> : null}
              Create local workspace
            </Button>
          </form>
        )}
        {error ? (
          <p className="mt-4 text-[14px] text-destructive" role="alert">
            {error}
          </p>
        ) : null}
        {onPrivacy ? (
          <button
            className="focus-ring mt-4 text-[14px] font-medium text-forest underline"
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
