import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { CommandContextSchema, SourceRefSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { Role } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  OrganisationVerificationState,
  RecordEntityVerificationRequestSchema,
  RecordEntityVerificationResponseSchema,
  VerificationEvidenceSchema,
  VerificationSourceMethod,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { LoaderCircle } from "lucide-react";
import { type FormEvent, useEffect, useRef, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import {
  authentication,
  fieldClassName,
  unknownOutcomeCopy,
  uuidV7,
  validTimestamp,
} from "./sbr-form-support";

const codec = createProtoMethodCodec({
  input: RecordEntityVerificationRequestSchema,
  maximumRequestBytes: Math.floor(1.1 * 1024 * 1024),
  maximumResponseBytes: 32_768,
  output: RecordEntityVerificationResponseSchema,
});
const allowedTypes = new Set(["application/pdf", "image/jpeg", "image/png"]);

function stateLabel(state: OrganisationVerificationState): string {
  return state === OrganisationVerificationState.VERIFIED
    ? "Verified"
    : state === OrganisationVerificationState.FAILED
      ? "Failed"
      : state === OrganisationVerificationState.EXPIRED
        ? "Expired"
        : state === OrganisationVerificationState.SUPERSEDED
          ? "Superseded"
          : "Unverified";
}

function validVerificationState(state: OrganisationVerificationState): boolean {
  return (
    state === OrganisationVerificationState.UNVERIFIED ||
    state === OrganisationVerificationState.VERIFIED ||
    state === OrganisationVerificationState.FAILED ||
    state === OrganisationVerificationState.EXPIRED ||
    state === OrganisationVerificationState.SUPERSEDED
  );
}

export interface OrganisationVerificationWorkspace {
  readonly organisationCanonicalAbn: string;
  readonly organisationDisplayName: string;
  readonly organisationEntityType: string;
  readonly organisationId: string;
  readonly organisationLegalName: string;
  readonly organisationVerificationState: OrganisationVerificationState;
  readonly organisationVerificationExpiresAt?: { readonly nanos: number; readonly seconds: bigint };
  readonly organisationVersion: bigint;
  readonly roles: readonly Role[];
  readonly sessionId: string;
  readonly userId: string;
  readonly workspaceId: string;
}

export function OrganisationVerificationForm({
  api,
  onChanged,
  workspace,
}: {
  readonly api: Pick<TammyDesktopAPI, "recordEntityVerification">;
  readonly onChanged: () => void;
  readonly workspace: OrganisationVerificationWorkspace;
}) {
  const [legalName, setLegalName] = useState(workspace.organisationLegalName);
  const [entityType, setEntityType] = useState(workspace.organisationEntityType);
  const [outcome, setOutcome] = useState<OrganisationVerificationState>(
    OrganisationVerificationState.VERIFIED,
  );
  const fileRef = useRef<File | undefined>(undefined);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const [fileReady, setFileReady] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string>();
  const [expiry, setExpiry] = useState<string | undefined>(() =>
    workspace.organisationVerificationExpiresAt
      ? new Date(
          Number(workspace.organisationVerificationExpiresAt.seconds) * 1000,
        ).toLocaleDateString()
      : undefined,
  );
  const inFlight = useRef(false);
  const outcomeLocked = useRef(false);
  const [locked, setLocked] = useState(false);
  const mounted = useRef(true);
  const clearEvidence = () => {
    fileRef.current = undefined;
    if (fileInputRef.current) fileInputRef.current.value = "";
    if (mounted.current) setFileReady(false);
  };
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      fileRef.current = undefined;
      if (fileInputRef.current) fileInputRef.current.value = "";
    };
  }, []);

  const selectEvidence = (file: File | undefined) => {
    clearEvidence();
    setNotice(undefined);
    if (!file) return;
    if (!allowedTypes.has(file.type) || file.size < 1 || file.size > 1024 * 1024) {
      setNotice("Choose one PDF, JPEG, or PNG up to 1 MiB.");
      return;
    }
    fileRef.current = file;
    setFileReady(true);
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (inFlight.current || outcomeLocked.current || !fileRef.current) return;
    inFlight.current = true;
    setBusy(true);
    setNotice(undefined);
    let content: Uint8Array | undefined;
    let digestInputBytes: Uint8Array | undefined;
    let frame: Uint8Array | undefined;
    let commandStarted = false;
    try {
      const file = fileRef.current;
      content = new Uint8Array(await file.arrayBuffer());
      if (!mounted.current) return;
      if (!allowedTypes.has(file.type) || content.length < 1 || content.length > 1024 * 1024)
        throw new Error("invalid evidence");
      const digestInput = new ArrayBuffer(content.length);
      digestInputBytes = new Uint8Array(digestInput);
      digestInputBytes.set(content);
      const hash = new Uint8Array(await crypto.subtle.digest("SHA-256", digestInput));
      if (!mounted.current) return;
      const now = Date.now();
      const timestamp = create(TimestampSchema, {
        seconds: BigInt(Math.floor(now / 1000)),
        nanos: (now % 1000) * 1_000_000,
      });
      const request = create(RecordEntityVerificationRequestSchema, {
        commandContext: create(CommandContextSchema, {
          authentication: authentication(workspace),
          idempotencyKey: uuidV7(),
        }),
        organisationId: workspace.organisationId,
        expectedVersion: workspace.organisationVersion,
        sourceMethod: VerificationSourceMethod.ABR_EXTRACT_MANUAL,
        source: create(SourceRefSchema, {
          type: "abr_extract",
          id: uuidV7(),
          revision: 1n,
          contentHash: hash,
        }),
        verifiedLegalName: legalName,
        verifiedEntityType: entityType,
        outcome,
        evidence: create(VerificationEvidenceSchema, {
          mimeType: file.type,
          content,
          contentHash: hash,
        }),
        lookupTime: timestamp,
      });
      frame = codec.encodeRequest(request);
      clearEvidence();
      const pending = api.recordEntityVerification(frame);
      commandStarted = true;
      frame.fill(0);
      frame = undefined;
      content.fill(0);
      content = undefined;
      const response = codec.decodeResponse(await pending);
      if (
        !response.organisation ||
        response.organisation.id !== workspace.organisationId ||
        !response.verification ||
        response.verification.organisationId !== workspace.organisationId ||
        !validVerificationState(response.organisation.verificationState) ||
        response.organisation.verificationState !== response.verification.state ||
        response.verification.state !== outcome ||
        !validTimestamp(response.verification.expiresAt)
      )
        throw new Error("invalid response");
      if (!mounted.current) return;
      setExpiry(
        response.verification.expiresAt
          ? new Date(Number(response.verification.expiresAt.seconds) * 1000).toLocaleDateString()
          : "Not supplied",
      );
      setNotice("Entity verification evidence recorded.");
      onChanged();
    } catch {
      if (mounted.current) {
        clearEvidence();
        if (commandStarted) {
          outcomeLocked.current = true;
          setLocked(true);
        }
        setNotice(
          commandStarted
            ? unknownOutcomeCopy
            : "Verification could not be prepared. No organisation operation was started.",
        );
      }
    } finally {
      frame?.fill(0);
      content?.fill(0);
      digestInputBytes?.fill(0);
      inFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  };

  const admin = workspace.roles.includes(Role.WORKSPACE_ADMIN);
  return (
    <section aria-labelledby="entity-verification-heading" className="border-t border-border pt-4">
      <div className="flex items-baseline justify-between gap-3">
        <h2 id="entity-verification-heading" className="text-[12px] font-semibold">
          Independent entity verification
        </h2>
        <span className="text-[10px] font-medium text-muted-foreground">
          {stateLabel(workspace.organisationVerificationState)}
        </span>
      </div>
      <p className="mt-1 text-[10px] leading-5 text-muted-foreground">
        Record bounded evidence from an independent ABR extract. The core retains it encrypted and
        authors the expiry.
      </p>
      {expiry ? <p className="mt-2 text-[10px]">Evidence expiry: {expiry}</p> : null}
      {admin ? (
        <form className="mt-4 grid max-w-[520px] gap-3" onSubmit={submit}>
          <label className="grid gap-1.5 text-[11px] font-medium">
            Verified legal name
            <input
              className={fieldClassName}
              maxLength={256}
              onChange={(event) => setLegalName(event.target.value)}
              required
              value={legalName}
            />
          </label>
          <label className="grid gap-1.5 text-[11px] font-medium">
            Verified entity type
            <input
              className={fieldClassName}
              maxLength={96}
              onChange={(event) => setEntityType(event.target.value)}
              required
              value={entityType}
            />
          </label>
          <label className="grid gap-1.5 text-[11px] font-medium">
            Verification outcome
            <select
              className={fieldClassName}
              onChange={(event) =>
                setOutcome(Number(event.target.value) as OrganisationVerificationState)
              }
              value={outcome}
            >
              <option value={OrganisationVerificationState.VERIFIED}>Verified</option>
              <option value={OrganisationVerificationState.FAILED}>Failed</option>
            </select>
          </label>
          <label className="grid gap-1.5 text-[11px] font-medium">
            Independent evidence
            <input
              accept="application/pdf,image/jpeg,image/png"
              className="focus-ring block min-h-9 text-[11px]"
              onChange={(event) => selectEvidence(event.target.files?.[0])}
              ref={fileInputRef}
              type="file"
            />
          </label>
          {fileReady ? (
            <p className="text-[10px] text-ready-foreground">
              Evidence selected and bounded. Contents are not displayed.
            </p>
          ) : null}
          <Button className="h-9 w-fit text-[11px]" disabled={busy || !fileReady} type="submit">
            {busy ? <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" /> : null}
            Record verification
          </Button>
        </form>
      ) : (
        <p className="mt-3 text-[10px] text-muted-foreground">
          A workspace administrator records verification evidence.
        </p>
      )}
      {locked ? (
        <Button
          className="mt-3 h-9 text-[11px]"
          onClick={onChanged}
          type="button"
          variant="outline"
        >
          Refresh status
        </Button>
      ) : null}
      <p
        aria-live="polite"
        className="mt-3 min-h-4 text-[11px] text-muted-foreground"
        role="status"
      >
        {notice ?? ""}
      </p>
    </section>
  );
}
