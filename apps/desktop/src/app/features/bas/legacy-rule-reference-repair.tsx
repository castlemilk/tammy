import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import { CivilDateSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
  OrganisationPatchSchema,
  UpdateOrganisationRequestSchema,
  UpdateOrganisationResponseSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { type FormEvent, useEffect, useRef, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import {
  createBundledTaxRuleSourceRef,
  isCurrentBundledTaxRuleSourceRef,
  isKnownLegacyBundledTaxRuleSourceRef,
} from "../../../shared/bundled-tax-rule";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import {
  assertFreshFactor,
  authentication,
  commandContext,
  fieldClassName,
} from "../sbr/sbr-form-support";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const getOrganisationCodec = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetOrganisationResponseSchema,
});
const updateOrganisationCodec = createProtoMethodCodec({
  input: UpdateOrganisationRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: UpdateOrganisationResponseSchema,
});

const UPDATE_ORGANISATION_PURPOSE = "update_organisation_high_risk";
type IdentityApi = Pick<
  TammyConveyorModule<"organisation">,
  "assertTotp" | "getOrganisation" | "updateOrganisation"
>;
type OrganisationSnapshot = NonNullable<
  ReturnType<typeof getOrganisationCodec.decodeResponse>["organisation"]
>;

export function nextMelbourneCivilDate(now = new Date()): string {
  const parts = new Intl.DateTimeFormat("en-AU", {
    day: "2-digit",
    month: "2-digit",
    timeZone: "Australia/Melbourne",
    year: "numeric",
  }).formatToParts(now);
  const part = (type: "day" | "month" | "year") =>
    parts.find((value) => value.type === type)?.value ?? "";
  const today = new Date(0);
  today.setUTCFullYear(Number(part("year")), Number(part("month")) - 1, Number(part("day")));
  today.setUTCHours(0, 0, 0, 0);
  today.setUTCDate(today.getUTCDate() + 1);
  return `${String(today.getUTCFullYear()).padStart(4, "0")}-${String(today.getUTCMonth() + 1).padStart(2, "0")}-${String(today.getUTCDate()).padStart(2, "0")}`;
}

function melbourneToday(now: Date): string {
  const parts = new Intl.DateTimeFormat("en-AU", {
    day: "2-digit",
    month: "2-digit",
    timeZone: "Australia/Melbourne",
    year: "numeric",
  }).formatToParts(now);
  const part = (type: "day" | "month" | "year") =>
    parts.find((value) => value.type === type)?.value ?? "";
  return `${part("year")}-${part("month")}-${part("day")}`;
}

function parseFutureCivilDate(
  value: string,
): { year: number; month: number; day: number } | undefined {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) return undefined;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  if (year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31) return undefined;
  const parsed = new Date(0);
  parsed.setUTCFullYear(year, month - 1, day);
  parsed.setUTCHours(0, 0, 0, 0);
  if (
    parsed.getUTCFullYear() !== year ||
    parsed.getUTCMonth() !== month - 1 ||
    parsed.getUTCDate() !== day ||
    value <= melbourneToday(new Date())
  )
    return undefined;
  return { year, month, day };
}

function exactVersion(
  value: OrganisationSnapshot | undefined,
  organisationId: string,
): value is OrganisationSnapshot {
  return Boolean(value && value.id === organisationId && value.version > 0n);
}

function sourceHash(value: Uint8Array | undefined): string {
  return value ? [...value].map((byte) => byte.toString(16).padStart(2, "0")).join("") : "";
}

function boundedReason(value: string): boolean {
  return (
    value.length > 0 && value === value.trim() && new TextEncoder().encode(value).byteLength <= 512
  );
}

export function LegacyRuleReferenceRepair({
  api,
  canManage,
  onUpdated,
  workspace,
}: {
  readonly api: IdentityApi;
  readonly canManage: boolean;
  readonly onUpdated?: () => void;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const [storedSnapshot, setStoredSnapshot] = useState<{
    readonly scope: string;
    readonly organisation: OrganisationSnapshot;
  }>();
  const [reason, setReason] = useState("");
  const [effectiveDate, setEffectiveDate] = useState(() => nextMelbourneCivilDate());
  const [totp, setTotp] = useState("");
  const [busy, setBusy] = useState(false);
  const [checking, setChecking] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const scope = `${workspace?.workspaceId ?? ""}:${workspace?.organisationId ?? ""}:${workspace?.userId ?? ""}:${workspace?.sessionId ?? ""}`;
  const previousScope = useRef(scope);
  const scopeRef = useRef(scope);
  scopeRef.current = scope;
  const storedOrganisation =
    storedSnapshot?.scope === scope ? storedSnapshot.organisation : undefined;
  const storedRef = storedOrganisation?.activeTaxRuleBundle;
  const knownLegacy = isKnownLegacyBundledTaxRuleSourceRef(storedRef);
  const current = isCurrentBundledTaxRuleSourceRef(storedRef);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    if (previousScope.current === scope) return;
    previousScope.current = scope;
    inFlight.current = false;
    setStoredSnapshot(undefined);
    setReason("");
    setEffectiveDate(nextMelbourneCivilDate());
    setTotp("");
    setBusy(false);
    setChecking(false);
    setNotice("");
    setError("");
  }, [scope]);

  const isCurrentScope = (expected: string) => mounted.current && scopeRef.current === expected;

  const checkStoredReference = async () => {
    if (!workspace?.organisationId || checking || busy) return;
    const expectedScope = scope;
    setChecking(true);
    setStoredSnapshot(undefined);
    setNotice("");
    setError("");
    const frame = getOrganisationCodec.encodeRequest(
      create(GetOrganisationRequestSchema, {
        authentication: authentication(workspace),
        organisationId: workspace.organisationId,
      }),
    );
    try {
      const response = getOrganisationCodec.decodeResponse(await api.getOrganisation(frame));
      if (!isCurrentScope(expectedScope)) return;
      if (!exactVersion(response.organisation, workspace.organisationId))
        throw new Error("invalid organisation response");
      setStoredSnapshot({ scope: expectedScope, organisation: response.organisation });
    } catch {
      if (isCurrentScope(expectedScope))
        setError(
          "The stored tax-rule reference could not be checked. Refresh the workspace and try again.",
        );
    } finally {
      frame.fill(0);
      if (isCurrentScope(expectedScope)) setChecking(false);
    }
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const date = parseFutureCivilDate(effectiveDate);
    if (
      !workspace?.organisationId ||
      !storedOrganisation ||
      !exactVersion(storedOrganisation, workspace.organisationId) ||
      !isKnownLegacyBundledTaxRuleSourceRef(storedOrganisation.activeTaxRuleBundle) ||
      !canManage ||
      inFlight.current ||
      !boundedReason(reason.trim()) ||
      !date ||
      !/^\d{6}$/.test(totp)
    )
      return;

    inFlight.current = true;
    setBusy(true);
    setNotice("");
    setError("");
    let factorConfirmed = false;
    const expectedScope = scope;
    try {
      const freshFactor = await assertFreshFactor(
        api,
        workspace,
        totp,
        UPDATE_ORGANISATION_PURPOSE,
      );
      if (!isCurrentScope(expectedScope)) return;
      factorConfirmed = true;
      const request = create(UpdateOrganisationRequestSchema, {
        commandContext: commandContext(workspace, freshFactor),
        organisationId: workspace.organisationId,
        expectedVersion: storedOrganisation.version,
        updateMask: create(FieldMaskSchema, { paths: ["active_tax_rule_bundle"] }),
        patch: create(OrganisationPatchSchema, {
          activeTaxRuleBundle: createBundledTaxRuleSourceRef(),
        }),
        reason: reason.trim(),
        effectiveDate: create(CivilDateSchema, date),
      });
      const frame = updateOrganisationCodec.encodeRequest(request);
      try {
        const response = updateOrganisationCodec.decodeResponse(
          await api.updateOrganisation(frame),
        );
        if (!isCurrentScope(expectedScope)) return;
        const updated = response.organisation;
        if (
          !exactVersion(updated, workspace.organisationId) ||
          updated.version !== storedOrganisation.version + 1n ||
          !isCurrentBundledTaxRuleSourceRef(updated.activeTaxRuleBundle)
        )
          throw new Error("unexpected update response");
        setStoredSnapshot({ scope: expectedScope, organisation: updated });
        setReason("");
        setTotp("");
        setNotice("The active tax-rule reference was updated. Frozen BAS history is unchanged.");
        onUpdated?.();
      } finally {
        frame.fill(0);
      }
    } catch {
      if (isCurrentScope(expectedScope)) {
        setStoredSnapshot(undefined);
        setTotp("");
        setError(
          factorConfirmed
            ? "The organisation could not be updated. Check the stored reference again before retrying."
            : "The fresh TOTP code could not be confirmed. Set up TOTP in Settings → SBR readiness if needed; no SBR machine credential is required.",
        );
      }
    } finally {
      if (isCurrentScope(expectedScope)) {
        inFlight.current = false;
        setBusy(false);
      }
    }
  };

  const date = parseFutureCivilDate(effectiveDate);
  const formReady =
    canManage &&
    Boolean(
      workspace?.organisationId && exactVersion(storedOrganisation, workspace.organisationId),
    ) &&
    !busy &&
    !checking &&
    boundedReason(reason.trim()) &&
    Boolean(date) &&
    /^\d{6}$/.test(totp) &&
    knownLegacy;

  return (
    <details aria-label="Tax-rule reference" className="rounded border border-border bg-surface">
      <summary className="cursor-pointer list-none px-3 py-2 text-[10px] font-semibold [&::-webkit-details-marker]:hidden">
        Tax-rule reference
      </summary>
      <div className="grid gap-3 border-t border-border p-3">
        <p className="text-[9px] leading-4 text-muted-foreground">
          Check the stored active rule reference when BAS capture reports that its retained rule
          bundle cannot be loaded. No BAS history is changed by this check or repair.
        </p>
        <Button
          disabled={!workspace?.organisationId || checking || busy}
          onClick={() => void checkStoredReference()}
          type="button"
          variant="outline"
        >
          {checking ? "Checking…" : "Check stored tax-rule reference"}
        </Button>
        {error ? (
          <p className="text-[9px] text-warning" role="alert">
            {error}
          </p>
        ) : null}
        {notice ? (
          <p className="text-[9px] text-forest" role="status">
            {notice}
          </p>
        ) : null}
        {storedOrganisation ? (
          <dl className="grid gap-1 rounded border border-border bg-background p-2 text-[9px]">
            <div>
              <dt className="inline text-muted-foreground">Stored source: </dt>
              <dd className="m-0 inline break-all">
                {storedRef?.type ?? "missing"} · {storedRef?.id ?? "missing"} · revision{" "}
                {storedRef?.revision ?? 0n}
              </dd>
            </div>
            <div>
              <dt className="inline text-muted-foreground">SHA-256: </dt>
              <dd className="m-0 inline break-all">
                {sourceHash(storedRef?.contentHash) || "missing"}
              </dd>
            </div>
            <div>
              <dt className="inline text-muted-foreground">Organisation version: </dt>
              <dd className="m-0 inline">{storedOrganisation.version.toString()}</dd>
            </div>
          </dl>
        ) : null}
        {current ? (
          <p className="text-[9px] text-forest" role="status">
            This organisation already uses the supported tax-rule reference.
          </p>
        ) : null}
        {knownLegacy ? (
          <p className="text-[9px] text-warning" role="status">
            This workspace uses the known legacy tax-rule reference.
          </p>
        ) : null}
        {storedOrganisation && !current && !knownLegacy ? (
          <p className="text-[9px] text-warning" role="alert">
            This reference does not match the supported bundle or the one recognized legacy value.
            It was left unchanged; contact support.
          </p>
        ) : null}
        {knownLegacy && !canManage ? (
          <p className="text-[9px] text-warning" role="note">
            Ask a workspace administrator to update this known legacy tax-rule reference.
          </p>
        ) : null}
        {knownLegacy && canManage ? (
          <form className="grid gap-3" onSubmit={(event) => void submit(event)}>
            <p className="text-[9px] leading-4 text-warning">
              This exact legacy reference is recognized. Updating the active organisation reference
              does not rewrite existing calculations or frozen BAS handoffs.
            </p>
            <label className="grid gap-1 text-[9px]">
              Reason for rule-reference update
              <input
                className={fieldClassName}
                maxLength={512}
                onChange={(event) => setReason(event.target.value)}
                required
                value={reason}
              />
            </label>
            <label className="grid gap-1 text-[9px]">
              Effective from
              <input
                className={fieldClassName}
                onChange={(event) => setEffectiveDate(event.target.value)}
                required
                type="date"
                value={effectiveDate}
              />
            </label>
            {!date ? (
              <p className="text-[9px] text-warning" role="note">
                Choose a valid date after today in Australia/Melbourne.
              </p>
            ) : null}
            <label className="grid gap-1 text-[9px]">
              Fresh TOTP code
              <input
                autoComplete="one-time-code"
                className={fieldClassName}
                inputMode="numeric"
                maxLength={6}
                onChange={(event) => setTotp(event.target.value.replace(/\D/gu, "").slice(0, 6))}
                required
                value={totp}
              />
            </label>
            <p className="text-[9px] text-muted-foreground">
              If you need to set up TOTP, open Settings → SBR readiness. An SBR machine credential
              is not required for this update.
            </p>
            <Button disabled={!formReady} type="submit">
              {busy ? "Updating…" : "Confirm tax-rule reference update"}
            </Button>
          </form>
        ) : null}
      </div>
    </details>
  );
}
