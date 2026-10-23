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
import { CheckCircle2, LoaderCircle } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { abnLookupUrl, isValidAbn, normalizeAbnInput } from "../../../shared/abn";
import { createBundledTaxRuleSourceRef } from "../../../shared/bundled-tax-rule";
import {
  getNewCredentialFeedback,
  newCredentialValuesMatch,
} from "../../../shared/new-credential-policy";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { NewCredentialField } from "../../components/new-credential-field";
import { Button } from "../../components/ui/button";
import { WorkspaceEntryLayout } from "../../components/workspace-entry-layout";
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
  return "focus-ring h-11 min-w-0 w-full rounded-xl border border-border bg-surface px-3 text-[14px] text-foreground outline-none placeholder:text-muted-foreground";
}

function isWhitespaceOnly(value: string): boolean {
  return value.length > 0 && value.trim().length === 0;
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
  const [workspacePassphraseConfirmation, setWorkspacePassphraseConfirmation] = useState("");
  const [administratorPassword, setAdministratorPassword] = useState("");
  const [administratorPasswordConfirmation, setAdministratorPasswordConfirmation] = useState("");
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
  const workspacePassphraseFeedback = getNewCredentialFeedback(workspacePassphrase);
  const administratorPasswordFeedback = getNewCredentialFeedback(administratorPassword);
  const abnIsValid = isValidAbn(abn);
  const abnLookup = abnLookupUrl(abn);
  const nameFieldsAreNonblank = [
    displayName,
    username,
    businessLegalName,
    businessDisplayName,
  ].every((value) => value.trim().length > 0);
  const canCreateWorkspace =
    nameFieldsAreNonblank &&
    abnIsValid &&
    workspacePassphraseFeedback.valid &&
    administratorPasswordFeedback.valid &&
    newCredentialValuesMatch(workspacePassphrase, workspacePassphraseConfirmation) &&
    newCredentialValuesMatch(administratorPassword, administratorPasswordConfirmation);

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
    if (!nameFieldsAreNonblank) {
      setError("Enter a value in each name field before creating the workspace.");
      return;
    }
    if (!canCreateWorkspace) {
      setError("Review the ABN and credential checks before creating the workspace.");
      return;
    }
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
      setWorkspacePassphrase("");
      setWorkspacePassphraseConfirmation("");
      setAdministratorPasswordConfirmation("");
      setPending({
        gstBasis,
        administratorPassword,
        abn: normalizeAbnInput(abn),
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
    <WorkspaceEntryLayout
      description={
        pending
          ? "Keep this one-time code somewhere safe before continuing."
          : "Your accounting data stays encrypted on this device."
      }
      {...(onPrivacy === undefined ? {} : { onPrivacy })}
      stage={pending ? "recovery" : "details"}
      title={pending ? "Save your recovery code" : "Create your local workspace"}
    >
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
        <form className="grid gap-5" onSubmit={createWorkspace}>
          <fieldset className="grid min-w-0 gap-4">
            <legend className="mb-3 text-[12px] font-semibold uppercase tracking-[0.12em] text-forest">
              Business details
            </legend>
            <div className="grid min-w-0 gap-4 sm:grid-cols-2">
              <label className="grid min-w-0 gap-1.5 text-[13px] font-medium text-foreground">
                Your name
                <input
                  aria-describedby={
                    isWhitespaceOnly(displayName) ? "setup-display-name-feedback" : undefined
                  }
                  aria-invalid={isWhitespaceOnly(displayName)}
                  autoComplete="name"
                  className={fieldClassName()}
                  id="setup-display-name"
                  onChange={(event) => setDisplayName(event.target.value)}
                  required
                  value={displayName}
                />
                {isWhitespaceOnly(displayName) ? (
                  <span
                    className="text-[12px] font-normal leading-5 text-destructive"
                    id="setup-display-name-feedback"
                    role="status"
                  >
                    Enter your name.
                  </span>
                ) : null}
              </label>
              <label className="grid min-w-0 gap-1.5 text-[13px] font-medium text-foreground">
                Email or username
                <input
                  aria-describedby={
                    isWhitespaceOnly(username) ? "setup-username-feedback" : undefined
                  }
                  aria-invalid={isWhitespaceOnly(username)}
                  autoComplete="username"
                  className={fieldClassName()}
                  id="setup-username"
                  onChange={(event) => setUsername(event.target.value)}
                  required
                  value={username}
                />
                {isWhitespaceOnly(username) ? (
                  <span
                    className="text-[12px] font-normal leading-5 text-destructive"
                    id="setup-username-feedback"
                    role="status"
                  >
                    Enter an email or username.
                  </span>
                ) : null}
              </label>
              <label className="grid min-w-0 gap-1.5 text-[13px] font-medium text-foreground">
                Business legal name
                <input
                  aria-describedby={
                    isWhitespaceOnly(businessLegalName) ? "setup-legal-name-feedback" : undefined
                  }
                  aria-invalid={isWhitespaceOnly(businessLegalName)}
                  className={fieldClassName()}
                  id="setup-legal-name"
                  maxLength={256}
                  onChange={(event) => setBusinessLegalName(event.target.value)}
                  required
                  value={businessLegalName}
                />
                {isWhitespaceOnly(businessLegalName) ? (
                  <span
                    className="text-[12px] font-normal leading-5 text-destructive"
                    id="setup-legal-name-feedback"
                    role="status"
                  >
                    Enter a business name.
                  </span>
                ) : null}
              </label>
              <label className="grid min-w-0 gap-1.5 text-[13px] font-medium text-foreground">
                Business display name
                <input
                  aria-describedby={
                    isWhitespaceOnly(businessDisplayName)
                      ? "setup-business-display-name-feedback"
                      : undefined
                  }
                  aria-invalid={isWhitespaceOnly(businessDisplayName)}
                  className={fieldClassName()}
                  id="setup-business-display-name"
                  maxLength={256}
                  onChange={(event) => setBusinessDisplayName(event.target.value)}
                  required
                  value={businessDisplayName}
                />
                {isWhitespaceOnly(businessDisplayName) ? (
                  <span
                    className="text-[12px] font-normal leading-5 text-destructive"
                    id="setup-business-display-name-feedback"
                    role="status"
                  >
                    Enter a business name.
                  </span>
                ) : null}
              </label>
              <div className="grid min-w-0 gap-1.5 text-[13px] font-medium text-foreground sm:col-span-2">
                <label htmlFor="setup-abn">ABN</label>
                <input
                  aria-describedby="abn-description abn-lookup-help"
                  aria-invalid={abn.length > 0 && !abnIsValid}
                  autoComplete="off"
                  className={fieldClassName()}
                  id="setup-abn"
                  inputMode="numeric"
                  onChange={(event) => setAbn(normalizeAbnInput(event.target.value))}
                  required
                  spellCheck={false}
                  value={abn}
                />
                <span
                  className={`text-[12px] font-normal leading-5 ${abn.length > 0 && !abnIsValid ? "text-destructive" : "text-muted-foreground"}`}
                  id="abn-description"
                  role="status"
                  aria-live="polite"
                >
                  {abn.length === 0
                    ? "Enter 11 digits. Spaces are removed automatically."
                    : abnIsValid
                      ? "The checksum passes; confirm the ABN in the register to verify its current details."
                      : !/^[0-9]*$/.test(abn)
                        ? "Enter the 11 digits shown on your ABN record."
                        : abn.length !== 11
                          ? "Enter exactly 11 digits."
                          : "This number does not pass the ABN checksum. Check it against the register."}
                </span>
                {abnLookup ? (
                  <a
                    className="focus-ring w-fit rounded-sm text-[12px] font-medium text-forest underline underline-offset-2"
                    href={abnLookup}
                    rel="noopener noreferrer"
                    target="_blank"
                  >
                    Look up this ABN on the Australian Business Register
                  </a>
                ) : null}
                {abnLookup ? (
                  <span
                    className="text-[12px] font-normal leading-5 text-muted-foreground"
                    id="abn-lookup-help"
                  >
                    This opens the Australian Business Register in your browser. It does not fill
                    this form or replace checking the register details.
                  </span>
                ) : (
                  <span className="sr-only" id="abn-lookup-help">
                    A register lookup link appears when the checksum passes.
                  </span>
                )}
              </div>
              <label className="grid min-w-0 gap-1.5 text-[13px] font-medium text-foreground sm:col-span-2">
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
            </div>
          </fieldset>

          <fieldset className="grid min-w-0 gap-4 border-t border-border pt-4">
            <legend className="mb-3 text-[12px] font-semibold uppercase tracking-[0.12em] text-forest">
              Workspace credentials
            </legend>
            <div className="grid min-w-0 gap-5 sm:grid-cols-2">
              <NewCredentialField
                confirmation={workspacePassphraseConfirmation}
                confirmationLabel="Confirm workspace passphrase"
                label="Workspace passphrase"
                onChange={setWorkspacePassphrase}
                onConfirmationChange={setWorkspacePassphraseConfirmation}
                value={workspacePassphrase}
              />
              <NewCredentialField
                confirmation={administratorPasswordConfirmation}
                confirmationLabel="Confirm administrator password"
                label="Administrator password"
                onChange={setAdministratorPassword}
                onConfirmationChange={setAdministratorPasswordConfirmation}
                value={administratorPassword}
              />
            </div>
          </fieldset>

          <Button
            className="mt-1 h-11 w-full text-[14px]"
            disabled={busy || !canCreateWorkspace}
            type="submit"
          >
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
    </WorkspaceEntryLayout>
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
