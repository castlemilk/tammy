import { create } from "@bufbuild/protobuf";
import {
  ApprovedFileRefSchema,
  AuthenticationContextSchema,
  CommandContextSchema,
  SecretInputSchema,
  SourceRefSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
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
import { type FormEvent, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
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

export interface AuthenticatedWorkspace {
  readonly organisationId?: string;
  readonly sessionId: string;
  readonly userId: string;
  readonly workspaceId: string;
}

interface SetupScreenProps {
  readonly api: Pick<
    TammyDesktopAPI,
    | "confirmRecovery"
    | "createOrganisation"
    | "createWorkspace"
    | "getReportingCapability"
    | "signIn"
  >;
  readonly onAuthenticated: (workspace: AuthenticatedWorkspace) => void;
  readonly onPrivacy?: () => void;
}

interface PendingSetup {
  readonly administratorPassword: string;
  readonly abn: string;
  readonly businessDisplayName: string;
  readonly businessLegalName: string;
  readonly recoveryCode: string;
  readonly setupId: string;
  readonly username: string;
  readonly workspaceId: string;
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
  return "focus-ring h-9 w-full rounded-[6px] border border-border bg-surface px-3 text-[12px] text-foreground outline-none placeholder:text-muted-foreground";
}

export function SetupScreen({ api, onAuthenticated, onPrivacy }: SetupScreenProps) {
  const [displayName, setDisplayName] = useState("");
  const [username, setUsername] = useState("");
  const [businessLegalName, setBusinessLegalName] = useState("");
  const [businessDisplayName, setBusinessDisplayName] = useState("");
  const [abn, setAbn] = useState("");
  const [workspacePassphrase, setWorkspacePassphrase] = useState("");
  const [administratorPassword, setAdministratorPassword] = useState("");
  const [pending, setPending] = useState<PendingSetup>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

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
        await api.createWorkspace(createCodec.encodeRequest(request)),
      );
      if (!response.workspace || !response.recoverySecret?.utf8.byteLength)
        throw new Error("invalid");
      setPending({
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
        await api.confirmRecovery(confirmCodec.encodeRequest(confirmation)),
      );
      if (confirmed.workspace?.id !== pending.workspaceId) throw new Error("invalid workspace");
      const signIn = create(SignInRequestSchema, {
        username: pending.username,
        password: create(SecretInputSchema, {
          utf8: new TextEncoder().encode(pending.administratorPassword),
        }),
      });
      const authenticated = signInCodec.decodeResponse(
        await api.signIn(signInCodec.encodeRequest(signIn)),
      );
      if (!authenticated.user || !authenticated.session) throw new Error("invalid session");
      const authentication = create(AuthenticationContextSchema, {
        actorUserId: authenticated.user.id,
        sessionId: authenticated.session.id,
      });
      const organisation = create(CreateOrganisationRequestSchema, {
        commandContext: create(CommandContextSchema, {
          idempotencyKey: uuidV7(),
          authentication,
        }),
        abn: pending.abn,
        legalName: pending.businessLegalName,
        displayName: pending.businessDisplayName,
        entityType: "AU_PRIVATE_COMPANY",
        gstBasis: GstBasis.NON_CASH,
        gstReportingFrequency: GstReportingFrequency.QUARTERLY,
        financialYearEndMonth: 6,
        activeTaxRuleBundle: create(SourceRefSchema, {
          type: "tax_rule_bundle",
          id: "018f0000-0000-7000-8000-000000000022",
          revision: 1n,
          contentHash: hexBytes("e2f9cde094db43c30260dc54f089a1ab835912e75e8c29add5c7a240e90497e4"),
        }),
      });
      const createdOrganisation = createOrganisationCodec.decodeResponse(
        await api.createOrganisation(createOrganisationCodec.encodeRequest(organisation)),
      );
      if (!createdOrganisation.organisation?.id) throw new Error("invalid organisation");
      setAdministratorPassword("");
      setWorkspacePassphrase("");
      onAuthenticated({
        sessionId: authenticated.session.id,
        userId: authenticated.user.id,
        workspaceId: pending.workspaceId,
        organisationId: createdOrganisation.organisation.id,
      });
    } catch {
      setError("Recovery confirmation or sign in failed. Your workspace remains on this device.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="grid min-h-screen place-items-center bg-background px-5 py-8">
      <section className="w-full max-w-[520px] rounded-[8px] border border-border bg-surface p-7 shadow-sm">
        <div className="mb-6 flex items-start gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-full bg-forest-soft text-forest">
            <KeyRound aria-hidden="true" className="size-4" />
          </span>
          <div>
            <p className="font-serif text-[16px] font-bold text-forest">Tammy</p>
            <h1 className="mt-1 text-[20px] font-semibold tracking-[-0.02em] text-foreground">
              {pending ? "Save your recovery code" : "Create your local workspace"}
            </h1>
            <p className="mt-1 text-[11px] leading-5 text-muted-foreground">
              {pending
                ? "Keep this one-time code somewhere safe before continuing."
                : "Your accounting data stays encrypted on this device."}
            </p>
          </div>
        </div>

        <div className="mb-5">
          <ReportingCapabilityNotice
            api={api}
            entityType={ReportingEntityType.AU_BUSINESS}
            fallbackCopy="Tammy does not prepare, declare, or lodge a complete BAS."
            report={ReportKind.GST_WORKPAPER}
            taxYear={2024}
          />
        </div>

        {pending ? (
          <div className="grid gap-5">
            <div className="rounded-[6px] border border-forest/20 bg-forest-soft px-4 py-4">
              <p className="mb-2 flex items-center gap-2 text-[11px] font-semibold text-forest">
                <CheckCircle2 aria-hidden="true" className="size-4" /> One-time recovery code
              </p>
              <p className="break-all font-mono text-[13px] leading-6 text-foreground">
                {pending.recoveryCode}
              </p>
            </div>
            <Button
              className="h-9 w-full text-[11px]"
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
            <label className="grid gap-1.5 text-[11px] font-medium text-foreground">
              Your name
              <input
                autoComplete="name"
                className={fieldClassName()}
                onChange={(event) => setDisplayName(event.target.value)}
                required
                value={displayName}
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
              Business legal name
              <input
                className={fieldClassName()}
                maxLength={256}
                onChange={(event) => setBusinessLegalName(event.target.value)}
                required
                value={businessLegalName}
              />
            </label>
            <label className="grid gap-1.5 text-[11px] font-medium text-foreground">
              Business display name
              <input
                className={fieldClassName()}
                maxLength={256}
                onChange={(event) => setBusinessDisplayName(event.target.value)}
                required
                value={businessDisplayName}
              />
            </label>
            <label className="grid gap-1.5 text-[11px] font-medium text-foreground">
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
            <label className="grid gap-1.5 text-[11px] font-medium text-foreground">
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
            <label className="grid gap-1.5 text-[11px] font-medium text-foreground">
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
            <Button className="mt-1 h-9 w-full text-[11px]" disabled={busy} type="submit">
              {busy ? <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" /> : null}
              Create local workspace
            </Button>
          </form>
        )}
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

function hexBytes(value: string): Uint8Array {
  if (!/^[0-9a-f]{64}$/.test(value)) throw new Error("invalid checksum");
  const bytes = new Uint8Array(value.length / 2);
  for (let index = 0; index < bytes.length; index += 1) {
    bytes[index] = Number.parseInt(value.slice(index * 2, index * 2 + 2), 16);
  }
  return bytes;
}
