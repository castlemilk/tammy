import { create } from "@bufbuild/protobuf";
import { CivilDateSchema, CommandContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
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
import { validateBasSnapshot } from "@tammy/reporting/model";
import { Calculator, FileCheck2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import { aud } from "../documents/purchase-command-state";
import {
  ReportExportActions,
  type ReportOrganisationApi,
} from "../reporting/report-export-actions";
import { ReportingCapabilityNotice } from "../reporting/reporting-capability-notice";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { type BasPurchaseControls, BasPurchaseDetails } from "./bas-purchase-details";
import { BASLodgerRoleControl } from "./business-lodger-role-control";

const createCodec = createProtoMethodCodec({
  input: CreateBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4_194_304,
  output: CreateBasDraftResponseSchema,
});
const getCodec = createProtoMethodCodec({
  input: GetCurrentBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4_194_304,
  output: GetCurrentBasDraftResponseSchema,
});
type BasAPI = Pick<
  TammyConveyorModule<"reporting">,
  "createBasDraft" | "getCurrentBasDraft" | "getReportingCapability"
> &
  Partial<Pick<TammyConveyorModule<"reporting">, "exportBas">>;

export function BasScreen({
  api,
  now,
  onBasDraftChanged,
  organisationApi,
  roleControlApi,
  workspace,
  purchaseApi,
  documentApi,
  accountingApi,
  onReviewDocument,
  purchaseReviewOnly = false,
  embedded = false,
}: {
  readonly api: BasAPI;
  readonly embedded?: boolean;
  readonly organisationApi?: ReportOrganisationApi;
  readonly roleControlApi?: Pick<
    TammyConveyorModule<"organisation">,
    "getCurrentUser" | "assignRoles"
  >;
  readonly now?: Date;
  readonly onBasDraftChanged?: () => Promise<void>;
  readonly purchaseReviewOnly?: boolean;
  readonly workspace: AuthenticatedWorkspace | undefined;
} & BasPurchaseControls) {
  const screenScope = `${workspace?.workspaceId ?? ""}:${workspace?.organisationId ?? ""}:${workspace?.userId ?? ""}:${workspace?.sessionId ?? ""}`;
  const scopeRef = useRef(screenScope);
  scopeRef.current = screenScope;
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const [defaultPeriod] = useState(() => currentQuarter(now ?? new Date()));
  const [editedPeriod, setEditedPeriod] = useState<{
    readonly start: string;
    readonly end: string;
  }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [gstCapability, setGstCapability] = useState<{
    readonly status: ReportingCapabilityStatus | undefined;
    readonly taxYear: number;
  }>();
  const updateGstCapability = useCallback(
    (queriedTaxYear: number, status: ReportingCapabilityStatus | undefined) => {
      setGstCapability({ status, taxYear: queriedTaxYear });
    },
    [],
  );

  const organisationId = workspace?.organisationId ?? "";
  const userId = workspace?.userId ?? "";
  const sessionId = workspace?.sessionId ?? "";
  const currentBasInput = useMemo(
    () =>
      getCodec.encodeRequest(
        create(GetCurrentBasDraftRequestSchema, {
          authentication: { actorUserId: userId, sessionId },
          organisationId,
        }),
      ),
    [organisationId, sessionId, userId],
  );
  const currentBas = api.getCurrentBasDraft.useQuery({
    enabled: organisationId.length > 0,
    input: currentBasInput,
    retry: false,
  });
  const workpaper = useMemo<BasWorkpaper | undefined>(() => {
    if (!currentBas.data || currentBas.isError) return undefined;
    try {
      return getCodec.decodeResponse(currentBas.data).workpaper;
    } catch {
      return undefined;
    }
  }, [currentBas.data, currentBas.isError]);

  const start =
    editedPeriod?.start ?? (workpaper ? isoDate(workpaper.periodStart) : defaultPeriod.start);
  const end = editedPeriod?.end ?? (workpaper ? isoDate(workpaper.periodEnd) : defaultPeriod.end);
  const taxYear = auTaxYear(parseDate(end));
  const gstWorkpaperAvailable =
    taxYear !== undefined &&
    gstCapability?.taxYear === taxYear &&
    gstCapability.status === ReportingCapabilityStatus.AVAILABLE;

  const createDraft = async () => {
    if (!workspace?.organisationId) return;
    const commandScope = screenScope;
    const isCurrent = () => active.current && scopeRef.current === commandScope;
    if (!gstWorkpaperAvailable) {
      setError("GST workpaper support is unavailable for the selected tax year.");
      return;
    }
    const periodStart = parseDate(start);
    const periodEnd = parseDate(end);
    if (!periodStart || !periodEnd || start > end) {
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
      if (!isCurrent()) return;
      if (!response.workpaper) throw new Error("missing workpaper");
      await Promise.all([
        api.getCurrentBasDraft.invalidate(currentBasInput),
        onBasDraftChanged?.(),
      ]);
    } catch {
      if (isCurrent())
        setError(
          "The local BAS draft could not be created. Check the selected period and source records, then try again.",
        );
    } finally {
      if (isCurrent()) setBusy(false);
    }
  };

  const exportBas = api.exportBas;
  const Heading = embedded ? "h2" : "h1";
  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-4">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <Heading className="page-title">
            {purchaseReviewOnly ? "Purchase source review" : "GST & BAS"}
          </Heading>
          <p className="mt-1 text-[14px] text-muted-foreground">
            {purchaseReviewOnly
              ? "Review purchase evidence and GST-credit blockers in the retained legacy workpaper."
              : "A local workpaper from invoice sales and reviewed expenses. Tammy does not lodge this BAS."}
          </p>
        </div>
        {workpaper ? (
          <div className="rounded-xl border border-warning-border bg-warning-soft px-4 py-2">
            <p className="text-[14px] font-semibold text-warning">Draft — not lodged</p>
            <p className="mt-1 text-[12px] text-muted-foreground">Review locally when ready.</p>
          </div>
        ) : null}
      </div>
      {!purchaseReviewOnly && roleControlApi ? (
        <BASLodgerRoleControl api={roleControlApi} workspace={workspace} />
      ) : null}
      {!purchaseReviewOnly && exportBas && organisationApi ? (
        <ReportExportActions
          organisationApi={organisationApi}
          workspace={workspace}
          label="BAS"
          validateReport={(organisation) => {
            if (!workpaper) throw new Error();
            validateBasSnapshot({ organisation, workpaper, acquiredAt: new Date().toISOString() });
          }}
          disabled={
            busy ||
            currentBas.isFetching ||
            currentBas.isError ||
            !workpaper ||
            workpaper?.status !== BasWorkpaperStatus.DRAFT_NOT_LODGED
          }
          execute={(format, expectedOrganisationVersion) =>
            exportBas({
              authentication: { actorUserId: userId, sessionId },
              organisationId,
              expectedOrganisationVersion,
              format,
              periodStart: isoDate(workpaper?.periodStart),
              periodEnd: isoDate(workpaper?.periodEnd),
              expectedWorkpaperId: workpaper?.id ?? "",
              expectedWorkpaperVersion: String(workpaper?.version ?? 0n),
            })
          }
        />
      ) : null}
      {!purchaseReviewOnly ? (
        <section className="grid gap-2" aria-label="Reporting support">
          {taxYear === undefined ? (
            <p aria-live="polite" role="status">
              Choose a valid reporting period to check reporting support.
            </p>
          ) : (
            <>
              <div className="grid gap-1">
                <h2 className="text-[14px] font-semibold">GST workpaper</h2>
                <ReportingCapabilityNotice
                  api={api}
                  entityType={ReportingEntityType.AU_BUSINESS}
                  onStatusChange={updateGstCapability}
                  report={ReportKind.GST_WORKPAPER}
                  taxYear={taxYear}
                />
              </div>
              <div className="grid gap-1">
                <h2 className="text-[14px] font-semibold">Full BAS return</h2>
                <ReportingCapabilityNotice
                  api={api}
                  entityType={ReportingEntityType.AU_BUSINESS}
                  report={ReportKind.BAS}
                  taxYear={taxYear}
                />
              </div>
            </>
          )}
        </section>
      ) : null}
      {!purchaseReviewOnly ? (
        <section
          className="flex flex-wrap items-end gap-3 rounded-xl border border-border bg-surface p-3"
          aria-label="BAS reporting period"
        >
          <DateField
            label="Period start"
            onChange={(date) => {
              setEditedPeriod((current) => ({ start: date, end: current?.end ?? end }));
            }}
            onFocus={() => {
              setEditedPeriod((current) => current ?? { start, end });
            }}
            value={start}
          />
          <DateField
            label="Period end"
            onChange={(date) => {
              setEditedPeriod((current) => ({ start: current?.start ?? start, end: date }));
            }}
            onFocus={() => {
              setEditedPeriod((current) => current ?? { start, end });
            }}
            value={end}
          />
          <Button
            className="h-11 text-[14px]"
            disabled={
              busy || !gstWorkpaperAvailable || !parseDate(start) || !parseDate(end) || start > end
            }
            onClick={createDraft}
            type="button"
          >
            {busy ? "Creating…" : workpaper ? "Regenerate local draft" : "Create local draft"}
          </Button>
          {workpaper ? (
            <p className="text-[12px] leading-4 text-muted-foreground">
              Saved figures remain visible until the new draft is created.
            </p>
          ) : null}
        </section>
      ) : null}
      {error ? (
        <p
          className="rounded-xl border border-warning-border bg-warning-soft px-3 py-2 text-[14px]"
          role="alert"
        >
          {error}
        </p>
      ) : null}
      {!workpaper && !purchaseReviewOnly ? (
        <section className="grid min-h-[420px] place-items-center rounded-xl border border-dashed border-border bg-surface p-8 text-center">
          <div className="max-w-md">
            <Calculator className="mx-auto size-7 text-forest" />
            <h2 className="mt-3 text-[13px] font-semibold">Create a BAS workpaper</h2>
            <p className="mt-2 text-[14px] leading-5 text-muted-foreground">
              Issued invoice sales and recorded receipts contribute according to the business’s GST
              basis. Reviewed purchases need appropriate evidence for GST credits.
            </p>
          </div>
        </section>
      ) : workpaper ? (
        <Workpaper
          value={workpaper}
          purchaseApi={purchaseApi}
          documentApi={documentApi}
          accountingApi={accountingApi}
          workspace={workspace}
          onReviewDocument={onReviewDocument}
          onChanged={() => api.getCurrentBasDraft.invalidate(currentBasInput)}
        />
      ) : (
        <p className="rounded border border-dashed border-border bg-surface p-6 text-[14px] text-muted-foreground">
          No retained local workpaper is available for purchase review. Review the source document
          in Documents to resolve purchase blockers.
        </p>
      )}
    </div>
  );
}

function Workpaper({ value, ...controls }: { readonly value: BasWorkpaper } & BasPurchaseControls) {
  return (
    <div className="grid gap-4">
      <section
        className="grid gap-2 rounded-xl border border-border bg-surface p-3"
        aria-label="BAS source review"
      >
        <p className="text-[14px] font-medium">
          {value.gstBasis === GstBasis.CASH
            ? "Cash basis · sales follow recorded payments"
            : value.gstBasis === GstBasis.NON_CASH
              ? "Non-cash basis · sales follow invoice issue dates"
              : "Legacy workpaper · regenerate to review income and GST basis"}
        </p>
        <p className="text-[12px] text-muted-foreground">
          Saved reporting period: {formatDate(value.periodStart)} – {formatDate(value.periodEnd)}
        </p>
        <p className="text-[12px] text-muted-foreground break-all">
          Saved workpaper: {value.id} · version {value.version}
        </p>
        {value.sourcesChanged ? (
          <p className="text-[14px] text-warning" role="status">
            Sources changed since this workpaper was saved. Regenerate the local draft to include
            current income and GST. The figures below are the saved snapshot.
          </p>
        ) : null}
        {value.reviewWarnings.map((warning) => (
          <p className="text-[14px] leading-5 text-warning" key={warning}>
            {warning}
          </p>
        ))}
        {value.excludedCashPurchaseCount > 0n ? (
          <p className="text-[12px] text-muted-foreground">
            {value.excludedCashPurchaseCount} reviewed purchase
            {value.excludedCashPurchaseCount === 1n ? " has" : "s have"} no recorded payment
            evidence and {value.excludedCashPurchaseCount === 1n ? "is" : "are"} excluded from GST
            credits in this cash-basis draft.
          </p>
        ) : null}
      </section>
      <section className="grid grid-cols-4 gap-3 max-[760px]:grid-cols-2">
        <Metric label="Sales (G1)" testId="bas-sales-g1" value={value.salesG1?.minorUnits} />
        <Metric
          label="GST on sales (1A)"
          testId="bas-gst-on-sales-1a"
          value={value.gstOnSales1a?.minorUnits}
        />
        <Metric label="GST credits (1B)" value={value.gstCredits1b?.minorUnits} />
        <Metric
          emphasis
          label={
            (value.netGstPayable?.minorUnits ?? 0n) < 0n ? "Net GST refundable" : "Net GST payable"
          }
          value={abs(value.netGstPayable?.minorUnits)}
        />
      </section>
      <section className="overflow-x-auto rounded-[20px] border border-border bg-surface">
        <div className="border-b border-border px-3 py-3">
          <h2 className="text-[14px] font-semibold">Invoice sales sources</h2>
          <p className="mt-1 text-[12px] text-muted-foreground">
            GST-inclusive sales and GST attributed to this period. Out-of-scope lines are excluded.
          </p>
        </div>
        {value.salesSources.length === 0 ? (
          <p className="p-5 text-center text-[14px] text-muted-foreground">
            No eligible invoice sales are attributed to this period.
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table
              aria-label="BAS sales sources"
              className="w-full border-collapse text-left text-[12px]"
            >
              <thead>
                <tr className="border-b border-border text-muted-foreground">
                  <th className="px-3 py-2">Tax date</th>
                  <th className="px-3 py-2">Customer</th>
                  <th className="px-3 py-2">Invoice</th>
                  <th className="px-3 py-2">Source</th>
                  <th className="px-3 py-2 text-right">Sales (G1)</th>
                  <th className="px-3 py-2 text-right">GST (1A)</th>
                </tr>
              </thead>
              <tbody>
                {value.salesSources.map((source) => (
                  <tr
                    className="border-b border-border last:border-0"
                    key={`${source.invoiceId}-${source.paymentId ?? "issue"}`}
                  >
                    <td className="px-3 py-2">{formatDate(source.taxDate)}</td>
                    <td className="px-3 py-2 font-medium">{source.customerName}</td>
                    <td className="px-3 py-2">{source.invoiceNumber}</td>
                    <td className="px-3 py-2">
                      {source.paymentId ? "Recorded receipt" : "Issued invoice"}
                    </td>
                    <td className="px-3 py-2 text-right">{money(source.gross?.minorUnits)}</td>
                    <td className="px-3 py-2 text-right font-semibold text-forest">
                      {money(source.gst?.minorUnits)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <section className="overflow-x-auto rounded-[20px] border border-border bg-surface">
        <div className="flex items-center justify-between border-b border-border px-3 py-3">
          <div>
            <h2 className="text-[14px] font-semibold">Reviewed source documents</h2>
            <p className="mt-1 text-[12px] text-muted-foreground">
              {formatDate(value.periodStart)} – {formatDate(value.periodEnd)}
            </p>
          </div>
          <span className="flex items-center gap-1 rounded-full bg-success-soft px-2 py-1 text-[12px] font-semibold text-forest">
            <FileCheck2 className="size-3" />
            {value.sources.length} source{value.sources.length === 1 ? "" : "s"}
          </span>
        </div>
        {value.sources.length === 0 ? (
          <p className="p-8 text-center text-[14px] text-muted-foreground">
            No reviewed purchase documents are included in GST credits for this period.
          </p>
        ) : (
          <table className="w-full border-collapse text-left text-[12px]">
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
      <BasPurchaseDetails value={value} {...controls} />
      <p className="text-[12px] text-muted-foreground">
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
  testId,
  value,
}: {
  readonly emphasis?: boolean;
  readonly label: string;
  readonly testId?: string;
  readonly value: bigint | undefined;
}) {
  return (
    <div
      data-testid={testId}
      className={`rounded-xl border p-4 ${emphasis ? "border-success-border bg-success-soft" : "border-border bg-surface"}`}
    >
      <p className="text-[12px] font-semibold uppercase tracking-[0.05em] text-muted-foreground">
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
  onFocus,
  value,
}: {
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly onFocus: () => void;
  readonly value: string;
}) {
  return (
    <label className="grid gap-1 text-left text-[12px] font-semibold">
      {label}
      <input
        className="focus-ring h-11 rounded-xl border border-border px-2 text-[14px]"
        onChange={(event) => onChange(event.target.value)}
        onFocus={onFocus}
        type="date"
        value={value}
      />
    </label>
  );
}
function parseDate(value: string) {
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})$/);
  const date = new Date(`${value}T00:00:00Z`);
  return match && Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value
    ? create(CivilDateSchema, {
        year: Number(match[1]),
        month: Number(match[2]),
        day: Number(match[3]),
      })
    : undefined;
}
function isoDate(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
) {
  return value
    ? `${value.year}-${String(value.month).padStart(2, "0")}-${String(value.day).padStart(2, "0")}`
    : "";
}
function currentQuarter(date: Date) {
  const month = Math.floor(date.getMonth() / 3) * 3;
  const format = (value: Date) =>
    `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, "0")}-${String(value.getDate()).padStart(2, "0")}`;
  return {
    start: format(new Date(date.getFullYear(), month, 1)),
    end: format(new Date(date.getFullYear(), month + 3, 0)),
  };
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
  return aud(value);
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
