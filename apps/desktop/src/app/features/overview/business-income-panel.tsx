import { create } from "@bufbuild/protobuf";
import { CivilDateSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  GetBusinessIncomeSummaryRequestSchema,
  GetBusinessIncomeSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { useMemo, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { businessIncomeToRecord } from "../../../shared/invoice-accounting";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import { SurfaceHeading } from "../../components/workspace-primitives";
import {
  ReportExportActions,
  type ReportOrganisationApi,
} from "../reporting/report-export-actions";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const codec = createProtoMethodCodec({
  input: GetBusinessIncomeSummaryRequestSchema,
  output: GetBusinessIncomeSummaryResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 32768,
});
type IncomeApi = Pick<TammyConveyorModule<"accounting">, "getBusinessIncomeSummary">;

export function BusinessIncomePanel({
  api,
  now,
  onReviewInvoices,
  exportApi,
  organisationApi,
  workspace,
}: {
  readonly api: IncomeApi;
  readonly exportApi?: Pick<TammyConveyorModule<"reporting">, "exportIncome">;
  readonly organisationApi?: ReportOrganisationApi;
  readonly now?: Date;
  readonly onReviewInvoices?: () => void;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const [today] = useState(() => now ?? new Date());
  const [mode, setMode] = useState("quarter");
  const [start, setStart] = useState(() => period(today, "quarter").start);
  const [end, setEnd] = useState(() => period(today, "quarter").end);
  const [asOf, setAsOf] = useState(() => localDate(today));
  const organisationId = workspace?.organisationId ?? "";
  const userId = workspace?.userId ?? "";
  const sessionId = workspace?.sessionId ?? "";
  const valid = validDate(start) && validDate(end) && start <= end && validDate(asOf);
  const input = useMemo(
    () =>
      codec.encodeRequest(
        create(GetBusinessIncomeSummaryRequestSchema, {
          organisationId,
          authentication: { actorUserId: userId, sessionId },
          periodStart: civilDate(start),
          periodEnd: civilDate(end),
          asOfDate: civilDate(asOf),
        }),
      ),
    [organisationId, userId, sessionId, start, end, asOf],
  );
  const query = api.getBusinessIncomeSummary.useQuery({
    enabled: organisationId.length > 0 && valid,
    input,
    retry: false,
    refetchOnMount: "always",
  });
  const value = useMemo(() => {
    if (!query.data || query.isError || !valid) return undefined;
    try {
      const summary = codec.decodeResponse(query.data).summary;
      if (!summary || summary.organisationId !== organisationId) return undefined;
      const record = businessIncomeToRecord(summary);
      return record.periodStart === start && record.periodEnd === end && record.asOfDate === asOf
        ? record
        : undefined;
    } catch {
      return undefined;
    }
  }, [query.data, query.isError, valid, organisationId, start, end, asOf]);

  function choosePeriod(next: string) {
    setMode(next);
    if (next === "custom") return;
    const selected = period(today, next);
    setStart(selected.start);
    setEnd(selected.end);
  }
  return (
    <section
      className="work-surface overflow-hidden"
      aria-label="Business income"
      aria-busy={query.isFetching}
    >
      <SurfaceHeading
        title="Business income"
        description="Posted revenue excluding GST, with invoice receipts and balances."
        action={
          <Button
            className="h-11 text-[13px]"
            disabled={!valid || query.isFetching}
            onClick={() => api.getBusinessIncomeSummary.invalidate(input)}
            type="button"
            variant="ghost"
          >
            Refresh income
          </Button>
        }
      />
      <div className="grid grid-cols-4 gap-3 border-b border-border p-5 max-[760px]:grid-cols-2 max-[460px]:grid-cols-1">
        <label className="grid gap-1.5 text-[12px] font-semibold text-foreground">
          Income period
          <select
            className={fieldClass}
            onChange={(event) => choosePeriod(event.target.value)}
            value={mode}
          >
            <option value="quarter">This quarter</option>
            <option value="financial-year">This financial year</option>
            <option value="custom">Custom period</option>
          </select>
        </label>
        <DateField
          label="Income period start"
          onChange={(date) => {
            setMode("custom");
            setStart(date);
          }}
          value={start}
        />
        <DateField
          label="Income period end"
          onChange={(date) => {
            setMode("custom");
            setEnd(date);
          }}
          value={end}
        />
        <DateField label="Balances as of" onChange={setAsOf} value={asOf} />
      </div>
      {!valid ? (
        <p className="p-5 text-[13px] text-destructive" role="alert">
          Choose valid dates with the period start on or before its end.
        </p>
      ) : value ? (
        <div className="grid gap-5 p-5">
          <dl className="grid grid-cols-4 gap-3 max-[760px]:grid-cols-2 max-[460px]:grid-cols-1">
            <Metric
              label="Business income (ex GST)"
              minor={value.businessIncomeMinor}
              testId="business-income-total"
            />
            <Metric
              label="Invoice income (ex GST)"
              minor={value.invoiceIncomeMinor}
              testId="business-invoice-income"
            />
            <Metric
              label="Payments received"
              minor={value.paymentsReceivedMinor}
              testId="business-payments-received"
            />
            <Metric
              label="Invoices owing"
              minor={value.outstandingMinor}
              testId="business-outstanding"
            />
          </dl>
          <p className="border-t border-border pt-4 text-[12px] leading-5 text-muted-foreground">
            Income and receipts cover {value.periodStart} to {value.periodEnd}. Invoices owing
            include earlier unpaid invoices as of {value.asOfDate}. Invoice GST attributed in this
            period: {money(value.invoiceGstMinor)} (
            {value.gstBasis === "cash"
              ? "cash basis"
              : value.gstBasis === "non-cash"
                ? "non-cash basis"
                : "basis not configured"}
            ).
          </p>
          {value.unpostedIssuedCount > 0 ? (
            <div className="flex flex-wrap items-center gap-3 border-t border-border pt-4">
              <p className="mr-auto text-[13px] leading-5 text-warning">
                {value.unpostedIssuedCount} older issued invoice
                {value.unpostedIssuedCount === 1 ? " needs" : "s need"} accounting review. Unposted
                invoice income in this period: {money(value.unpostedIncomeMinor)}.
              </p>
              {onReviewInvoices ? (
                <Button
                  className="h-11 text-[13px]"
                  onClick={onReviewInvoices}
                  type="button"
                  variant="outline"
                >
                  Review invoices
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      ) : (
        <p className="p-5 text-[13px] text-muted-foreground" role="status">
          {query.isFetching
            ? "Loading business income…"
            : "Business income could not be read. Refresh to try again."}
        </p>
      )}
      {exportApi && organisationApi ? (
        <ReportExportActions
          organisationApi={organisationApi}
          workspace={workspace}
          label="Income"
          disabled={!valid || query.isFetching || !value}
          execute={(format, expectedOrganisationVersion) =>
            exportApi.exportIncome({
              authentication: { actorUserId: userId, sessionId },
              organisationId,
              expectedOrganisationVersion,
              format,
              periodStart: start,
              periodEnd: end,
              asOfDate: asOf,
              expectedFinancialRevision: String(value?.financialRevision ?? 0),
            })
          }
        />
      ) : null}
    </section>
  );
}

const fieldClass =
  "focus-ring h-11 min-w-0 rounded-xl border border-border bg-surface px-3 text-[14px] text-foreground";
function DateField({
  label,
  value,
  onChange,
}: {
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
}) {
  return (
    <label className="grid min-w-0 gap-1.5 text-[12px] font-semibold text-foreground">
      {label}
      <input
        className={fieldClass}
        onChange={(event) => onChange(event.target.value)}
        type="date"
        value={value}
      />
    </label>
  );
}
function Metric({
  label,
  minor,
  testId,
}: {
  readonly label: string;
  readonly minor: number;
  readonly testId: string;
}) {
  return (
    <div data-testid={testId}>
      <dt className="text-[12px] leading-5 text-muted-foreground">{label}</dt>
      <dd className="mt-1 text-[22px] font-semibold leading-7 tabular-nums text-foreground">
        {money(minor)}
      </dd>
    </div>
  );
}
function money(minor: number) {
  return new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD" }).format(minor / 100);
}
function localDate(date: Date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}
function period(date: Date, mode: string) {
  const year = date.getFullYear();
  const month = date.getMonth();
  if (mode === "financial-year") {
    const startYear = month >= 6 ? year : year - 1;
    return { start: `${startYear}-07-01`, end: `${startYear + 1}-06-30` };
  }
  const startMonth = Math.floor(month / 3) * 3;
  return {
    start: localDate(new Date(year, startMonth, 1)),
    end: localDate(new Date(year, startMonth + 3, 0)),
  };
}
function validDate(value: string) {
  const date = new Date(`${value}T00:00:00Z`);
  return (
    /^\d{4}-\d{2}-\d{2}$/.test(value) &&
    Number.isFinite(date.getTime()) &&
    date.toISOString().slice(0, 10) === value
  );
}
function civilDate(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  return create(CivilDateSchema, { year: year ?? 0, month: month ?? 0, day: day ?? 0 });
}
