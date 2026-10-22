import { create } from "@bufbuild/protobuf";
import { CivilDateSchema, CommandContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  ReportingCapabilityStatus,
  ReportingEntityType,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import {
  type BasWorkpaper,
  BasWorkpaperStatus,
  CreateBasDraftRequestSchema,
  CreateBasDraftResponseSchema,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { Calculator, FileCheck2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import { ReportingCapabilityNotice } from "../reporting/reporting-capability-notice";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const createCodec = createProtoMethodCodec({
  input: CreateBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: CreateBasDraftResponseSchema,
});
const getCodec = createProtoMethodCodec({
  input: GetCurrentBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: GetCurrentBasDraftResponseSchema,
});
type BasAPI = Pick<
  TammyDesktopAPI,
  "createBasDraft" | "getCurrentBasDraft" | "getReportingCapability"
>;

export function BasScreen({
  api,
  workspace,
}: {
  readonly api: BasAPI;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const [workpaper, setWorkpaper] = useState<BasWorkpaper>();
  const [start, setStart] = useState("2024-04-01");
  const [end, setEnd] = useState("2024-06-30");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [gstCapability, setGstCapability] = useState<{
    readonly status: ReportingCapabilityStatus | undefined;
    readonly taxYear: number;
  }>();
  const taxYear = auTaxYear(parseDate(end));
  const gstWorkpaperAvailable =
    taxYear !== undefined &&
    gstCapability?.taxYear === taxYear &&
    gstCapability.status === ReportingCapabilityStatus.AVAILABLE;
  const updateGstCapability = useCallback(
    (queriedTaxYear: number, status: ReportingCapabilityStatus | undefined) => {
      setGstCapability({ status, taxYear: queriedTaxYear });
    },
    [],
  );

  useEffect(() => {
    if (!workspace?.organisationId) return;
    const request = create(GetCurrentBasDraftRequestSchema, {
      authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
      organisationId: workspace.organisationId,
    });
    void api
      .getCurrentBasDraft(getCodec.encodeRequest(request))
      .then((frame) => setWorkpaper(getCodec.decodeResponse(frame).workpaper))
      .catch(() => undefined);
  }, [api, workspace]);

  const createDraft = async () => {
    if (!workspace?.organisationId) return;
    if (!gstWorkpaperAvailable) {
      setError("GST workpaper support is unavailable for the selected tax year.");
      return;
    }
    const periodStart = parseDate(start);
    const periodEnd = parseDate(end);
    if (!periodStart || !periodEnd) {
      setError("Choose a valid reporting period.");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      const request = create(CreateBasDraftRequestSchema, {
        commandContext: create(CommandContextSchema, {
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
          idempotencyKey: uuidV7(),
        }),
        organisationId: workspace.organisationId,
        periodStart,
        periodEnd,
      });
      const response = createCodec.decodeResponse(
        await api.createBasDraft(createCodec.encodeRequest(request)),
      );
      setWorkpaper(response.workpaper);
    } catch {
      setError("The local BAS draft could not be created.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-4">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-[18px] font-semibold tracking-[-0.02em]">GST &amp; BAS</h1>
          <p className="mt-1 text-[11px] text-muted-foreground">
            A local workpaper from reviewed documents. Tammy does not lodge this BAS.
          </p>
        </div>
        {workpaper ? (
          <div className="rounded-[6px] border border-warning-border bg-warning-soft px-4 py-2">
            <p className="text-[12px] font-semibold text-warning">Draft — not lodged</p>
            <p className="mt-1 text-[9px] text-muted-foreground">Review locally when ready.</p>
          </div>
        ) : null}
      </div>
      <section className="grid gap-2" aria-label="Reporting support">
        {taxYear === undefined ? (
          <p aria-live="polite" role="status">
            Choose a valid reporting period to check reporting support.
          </p>
        ) : (
          <>
            <ReportingCapabilityNotice
              api={api}
              entityType={ReportingEntityType.AU_BUSINESS}
              onStatusChange={updateGstCapability}
              report={ReportKind.GST_WORKPAPER}
              taxYear={taxYear}
            />
            <ReportingCapabilityNotice
              api={api}
              entityType={ReportingEntityType.AU_BUSINESS}
              report={ReportKind.BAS}
              taxYear={taxYear}
            />
          </>
        )}
      </section>
      {error ? (
        <p
          className="rounded-[6px] border border-warning-border bg-warning-soft px-3 py-2 text-[10px]"
          role="alert"
        >
          {error}
        </p>
      ) : null}
      {!workpaper ? (
        <section className="grid min-h-[420px] place-items-center rounded-[6px] border border-dashed border-border bg-surface p-8 text-center">
          <div className="max-w-md">
            <Calculator className="mx-auto size-7 text-forest" />
            <h2 className="mt-3 text-[13px] font-semibold">Create a BAS workpaper</h2>
            <p className="mt-2 text-[10px] leading-5 text-muted-foreground">
              Reviewed purchase documents in the period contribute to GST credits. No declaration or
              transmission is performed.
            </p>
            <div className="mx-auto mt-5 grid max-w-sm grid-cols-2 gap-2">
              <DateField label="Period start" onChange={setStart} value={start} />
              <DateField label="Period end" onChange={setEnd} value={end} />
            </div>
            <Button
              className="mt-4"
              disabled={busy || !gstWorkpaperAvailable}
              onClick={createDraft}
              type="button"
            >
              {busy ? "Creating…" : "Create local draft"}
            </Button>
          </div>
        </section>
      ) : (
        <Workpaper value={workpaper} />
      )}
    </div>
  );
}

function Workpaper({ value }: { readonly value: BasWorkpaper }) {
  return (
    <div className="grid gap-4">
      <section className="grid grid-cols-4 gap-3 max-[760px]:grid-cols-2">
        <Metric label="Sales (G1)" value={value.salesG1?.minorUnits} />
        <Metric label="GST on sales (1A)" value={value.gstOnSales1a?.minorUnits} />
        <Metric label="GST credits (1B)" value={value.gstCredits1b?.minorUnits} />
        <Metric
          emphasis
          label={
            (value.netGstPayable?.minorUnits ?? 0n) < 0n ? "Net GST refundable" : "Net GST payable"
          }
          value={abs(value.netGstPayable?.minorUnits)}
        />
      </section>
      <section className="overflow-hidden rounded-[6px] border border-border bg-surface">
        <div className="flex items-center justify-between border-b border-border px-3 py-3">
          <div>
            <h2 className="text-[11px] font-semibold">Reviewed source documents</h2>
            <p className="mt-1 text-[9px] text-muted-foreground">
              {formatDate(value.periodStart)} – {formatDate(value.periodEnd)}
            </p>
          </div>
          <span className="flex items-center gap-1 rounded-full bg-success-soft px-2 py-1 text-[8px] font-semibold text-forest">
            <FileCheck2 className="size-3" />
            {value.sources.length} source{value.sources.length === 1 ? "" : "s"}
          </span>
        </div>
        {value.sources.length === 0 ? (
          <p className="p-8 text-center text-[10px] text-muted-foreground">
            No reviewed purchase documents fall in this period.
          </p>
        ) : (
          <table className="w-full border-collapse text-left text-[9px]">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="px-3 py-2">Date</th>
                <th className="px-3 py-2">Supplier</th>
                <th className="px-3 py-2">Invoice</th>
                <th className="px-3 py-2 text-right">Gross</th>
                <th className="px-3 py-2 text-right">GST credit</th>
              </tr>
            </thead>
            <tbody>
              {value.sources.map((source) => (
                <tr className="border-b border-border last:border-0" key={source.documentId}>
                  <td className="px-3 py-2">{formatDate(source.documentDate)}</td>
                  <td className="px-3 py-2 font-medium">{source.supplierName || "—"}</td>
                  <td className="px-3 py-2">{source.invoiceNumber || "—"}</td>
                  <td className="px-3 py-2 text-right">{money(source.gross?.minorUnits)}</td>
                  <td className="px-3 py-2 text-right font-semibold text-forest">
                    {money(source.gstCredit?.minorUnits)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
      <p className="text-[9px] text-muted-foreground">
        Status:{" "}
        {value.status === BasWorkpaperStatus.DRAFT_NOT_LODGED
          ? "Draft — not lodged"
          : "Unavailable"}
        . This screen has no lodge, submit or declaration control.
      </p>
    </div>
  );
}
function Metric({
  emphasis = false,
  label,
  value,
}: {
  readonly emphasis?: boolean;
  readonly label: string;
  readonly value: bigint | undefined;
}) {
  return (
    <div
      className={`rounded-[6px] border p-4 ${emphasis ? "border-success-border bg-success-soft" : "border-border bg-surface"}`}
    >
      <p className="text-[8px] font-semibold uppercase tracking-[0.05em] text-muted-foreground">
        {label}
      </p>
      <p
        className={`mt-2 text-[17px] font-semibold ${emphasis ? "text-forest" : "text-foreground"}`}
      >
        {money(value)}
      </p>
    </div>
  );
}
function DateField({
  label,
  onChange,
  value,
}: {
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly value: string;
}) {
  return (
    <label className="grid gap-1 text-left text-[9px] font-semibold">
      {label}
      <input
        className="focus-ring h-8 rounded-[5px] border border-border px-2 text-[10px]"
        onChange={(event) => onChange(event.target.value)}
        type="date"
        value={value}
      />
    </label>
  );
}
function parseDate(value: string) {
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})$/);
  return match
    ? create(CivilDateSchema, {
        year: Number(match[1]),
        month: Number(match[2]),
        day: Number(match[3]),
      })
    : undefined;
}
function auTaxYear(
  value: { readonly year: number; readonly month: number } | undefined,
): number | undefined {
  return value ? value.year + (value.month >= 7 ? 1 : 0) : undefined;
}
function formatDate(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
): string {
  return value
    ? `${value.day.toString().padStart(2, "0")}/${value.month.toString().padStart(2, "0")}/${value.year}`
    : "—";
}
function money(value: bigint | undefined): string {
  return new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD" }).format(
    Number(value ?? 0n) / 100,
  );
}
function abs(value: bigint | undefined): bigint {
  return (value ?? 0n) < 0n ? -(value ?? 0n) : (value ?? 0n);
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
