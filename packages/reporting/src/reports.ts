import { renderDocument } from "@formepdf/core";
import { Cell, Document, Page, Table, View } from "@formepdf/react";
import { PageFooter, PageHeader, TableRow, Text, theme } from "@tammy/invoices/pdf-components";
import { assertInvoicePdfTextSupported } from "@tammy/invoices/pdf-text-policy";
import { createElement as h } from "react";
import { notoSansBold } from "./fonts/noto-sans-bold.ts";
import { notoSansRegular } from "./fonts/noto-sans-regular.ts";

import {
  type BasSnapshot,
  boundedReportBytes,
  exactDecimal,
  type IncomeSnapshot,
  type ReportOrganisation,
  reportDate,
  reportTimestamp,
  validateBasSnapshot,
  validateIncomeSnapshot,
} from "./model.ts";

export * from "./model.ts";

type Context = { readonly organisation: ReportOrganisation; readonly acquiredAt: string };
function money(value: { readonly currencyCode: string; readonly minorUnits: bigint } | undefined) {
  if (value?.currencyCode !== "AUD") throw new Error("INVALID_REPORT_SNAPSHOT");
  return exactDecimal(value.minorUnits);
}
function basis(value: number) {
  return value === 1 ? "Cash" : value === 2 ? "Non-cash" : "Unconfigured";
}

function hash(value: Uint8Array, _legacy = false) {
  return [...value].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}
function safeText(value: string) {
  return /^[\s\p{Cc}\p{Cf}]*[=+@-]/u.test(value) ? `'${value}` : value;
}
function quote(value: string) {
  return /[",\r\n]/u.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}
function csv(rows: readonly (readonly string[])[]) {
  return boundedReportBytes(
    new TextEncoder().encode(`${rows.map((row) => row.map(quote).join(",")).join("\r\n")}\r\n`),
  );
}
type Metric = readonly [string, string, string, string];
function common(value: Context): Metric[] {
  const org = value.organisation;
  return [
    ["organisation_id", org.id, "", ""],
    ["organisation_version", String(org.version), "", ""],
    ["organisation_legal_name", org.legalName, "", ""],
    ["organisation_display_name", org.displayName, "", ""],
    ["organisation_abn", org.abn, "", ""],
    ["acquired_at", value.acquiredAt, "", "Immutable report acquisition"],
  ];
}
function incomeMetrics(value: IncomeSnapshot): Metric[] {
  const s = value.summary;
  return [
    ...common(value),
    ["report_type", "Income summary", "", "Summary, not a transaction listing"],
    ["period_start", reportDate(s.periodStart), "", ""],
    ["period_end", reportDate(s.periodEnd), "", ""],
    ["as_of_date", reportDate(s.asOfDate), "", "Invoices owing balance date"],
    ["gst_basis", basis(s.gstBasis), "", ""],
    ["financial_revision", String(s.financialRevision), "", ""],
    ["business_income", money(s.businessIncome), "AUD", "Revenue excluding GST"],
    ["invoice_income", money(s.invoiceIncome), "AUD", "Invoice revenue excluding GST"],
    [
      "payments_received",
      money(s.paymentsReceived),
      "AUD",
      "Dated receipts in the selected period",
    ],
    ["outstanding", money(s.outstanding), "AUD", "Invoices owing as of the balance date"],
    ["invoice_gst", money(s.invoiceGst), "AUD", "Invoice GST attributed in the selected period"],
    ["unposted_income", money(s.unpostedIncome), "AUD", "Unposted invoice income requiring review"],
    ["issued_count", String(s.issuedCount), "", ""],
    ["unposted_issued_count", String(s.unpostedIssuedCount), "", ""],
    ...(s.unpostedIssuedCount > 0n
      ? [
          [
            "warning",
            `${s.unpostedIssuedCount} older issued invoices need accounting review.`,
            "",
            "",
          ] as Metric,
        ]
      : []),
  ];
}
export function renderIncomeCsv(value: IncomeSnapshot) {
  validateIncomeSnapshot(value);
  return csv([
    ["metric", "value", "currency", "context"],
    ...incomeMetrics(value).map((row) =>
      row.map((cell, index) => (index === 1 && row[2] === "AUD" ? cell : safeText(cell))),
    ),
  ]);
}
export const BAS_CSV_COLUMNS = [
  "kind",
  "metric",
  "value",
  "currency",
  "context",
  "document_id",
  "invoice_id",
  "payment_id",
  "party",
  "invoice_number",
  "source_date",
  "gross",
  "gst",
  "source_revision",
  "source_hash",
] as const;
function basMetadata(value: BasSnapshot): Metric[] {
  const s = value.workpaper;
  return [
    ...common(value),
    ["report_type", "GST workpaper", "", "Local workpaper"],
    ["status", "Draft — not lodged", "", ""],
    ["period_start", reportDate(s.periodStart), "", ""],
    ["period_end", reportDate(s.periodEnd), "", ""],
    ["gst_basis", basis(s.gstBasis), "", ""],
    ["workpaper_id", s.id, "", ""],
    ["workpaper_version", String(s.version), "", ""],
    ["created_at", reportTimestamp(s.createdAt), "", ""],
    ["source_fingerprint", hash(s.sourceFingerprint, true), "", ""],
    ["sources_changed", String(s.sourcesChanged), "", "Saved figures retained"],
    [
      "excluded_cash_purchase_count",
      String(s.excludedCashPurchaseCount),
      "",
      "Purchases excluded without payment evidence",
    ],
  ];
}
function basTotals(value: BasSnapshot): Metric[] {
  const s = value.workpaper;
  return [
    ["sales_g1", money(s.salesG1), "AUD", "Sales G1"],
    ["gst_on_sales_1a", money(s.gstOnSales1a), "AUD", "GST on sales 1A"],
    ["gst_credits_1b", money(s.gstCredits1b), "AUD", "GST credits 1B"],
    ["net_gst_payable", money(s.netGstPayable), "AUD", "Negative means refundable"],
  ];
}
function basWarnings(value: BasSnapshot) {
  return [
    ...(value.workpaper.sourcesChanged
      ? [
          "Source changes detected. Saved workpaper figures are retained; regenerate explicitly to refresh.",
        ]
      : []),
    ...value.workpaper.reviewWarnings,
  ];
}
export function renderBasCsv(value: BasSnapshot) {
  validateBasSnapshot(value);
  const rows: string[][] = [[...BAS_CSV_COLUMNS]];
  const metric = (kind: string, row: Metric) =>
    rows.push([
      kind,
      ...row.map((cell, index) => (index === 1 && row[2] === "AUD" ? cell : safeText(cell))),
      ...Array(10).fill(""),
    ]);
  for (const row of basMetadata(value)) metric("metadata", row);
  for (const row of basTotals(value)) metric("totals", row);
  for (const s of value.workpaper.salesSources)
    rows.push([
      "sales",
      "",
      "",
      "AUD",
      "",
      "",
      s.invoiceId,
      s.paymentId ?? "",
      safeText(s.customerName),
      safeText(s.invoiceNumber),
      reportDate(s.taxDate),
      money(s.gross),
      money(s.gst),
      String(s.sourceRevision),
      hash(s.sourceHash),
    ]);
  for (const s of value.workpaper.sources)
    rows.push([
      "purchases",
      "",
      "",
      "AUD",
      "",
      s.documentId,
      "",
      "",
      safeText(s.supplierName),
      safeText(s.invoiceNumber),
      reportDate(s.documentDate),
      money(s.gross),
      money(s.gstCredit),
      "",
      "",
    ]);
  for (const warning of basWarnings(value)) metric("warnings", ["warning", warning, "", ""]);
  return csv(rows);
}
function ReportCell({
  children,
  header = false,
  width,
  colSpan,
}: {
  readonly children?: string;
  readonly header?: boolean;
  readonly width?: number;
  readonly colSpan?: number;
}) {
  return h(
    Cell,
    {
      ...(colSpan === undefined ? {} : { colSpan }),
      style: {
        paddingTop: 5,
        paddingBottom: 5,
        paddingLeft: 4,
        paddingRight: 4,
        ...(width === undefined ? {} : { width, minWidth: width, maxWidth: width }),
      },
    },
    h(
      Text,
      {
        style: {
          fontSize: 8,
          marginBottom: 0,
          fontWeight: header ? 700 : 400,
          color: header ? theme.colors.primaryForeground : theme.colors.foreground,
          ...(width === undefined ? {} : { width: width - 8, maxWidth: width - 8 }),
        },
      },
      children,
    ),
  );
}
function sourceTable(
  headers: readonly string[],
  rows: readonly (readonly string[])[],
  partyLabel: "Customer" | "Supplier",
) {
  // Forme uses min-content widths for unbroken identity text. Keep those fields
  // in a spanning detail row so they cannot take width from dates or amounts.
  const widths = [171.76, 171.76, 171.76];
  return h(
    Table,
    { columns: widths.map((width) => ({ width: { fixed: width } })) },
    h(
      TableRow,
      { header: true },
      ...headers.map((label, index) =>
        h(ReportCell, { key: index, header: true, width: 171.76 }, label),
      ),
    ),
    ...rows.map((row, index) =>
      h(
        TableRow,
        { key: index, stripe: index % 2 === 1 },
        h(
          Cell,
          {
            colSpan: 3,
            style: { paddingTop: 5, paddingBottom: 5, paddingLeft: 4, paddingRight: 4 },
          },
          h(
            View,
            { style: { flexDirection: "row" } },
            ...[row[0], row[3], row[4]].map((cell, column) =>
              h(
                Text,
                {
                  key: column,
                  style: {
                    fontSize: 8,
                    marginBottom: 0,
                    width: column === 2 ? 163.76 : 171.76,
                    flexShrink: 0,
                  },
                },
                cell,
              ),
            ),
          ),
          h(
            Text,
            { style: { fontSize: 8, marginTop: 5, marginBottom: 0, width: 507.28 } },
            `${partyLabel}: ${row[1]}\n${row[2]}`,
          ),
        ),
      ),
    ),
  );
}
function reportTable(
  headers: readonly string[],
  rows: readonly (readonly string[])[],
  widths: readonly number[],
) {
  return h(
    Table,
    { columns: widths.map((width) => ({ width: { fraction: width } })) },
    h(
      TableRow,
      { header: true },
      ...headers.map((label, index) => h(ReportCell, { key: index, header: true }, label)),
    ),
    ...rows.map((row, index) =>
      h(
        TableRow,
        { key: index, stripe: index % 2 === 1 },
        ...(["organisation legal name", "organisation display name"].includes(row[0] ?? "")
          ? [h(ReportCell, { colSpan: headers.length, width: 515.28 }, `${row[0]}: ${row[1]}`)]
          : row.map((cell, column) => h(ReportCell, { key: column }, cell))),
      ),
    ),
  );
}
function preflight(strings: readonly string[]) {
  for (const value of strings) {
    try {
      // Forme's ToUnicode CMap cannot retain multi-character shaping clusters.
      // Reject combining/format text explicitly; CSV retains every code point.
      if (/[\p{M}\p{Cf}]/u.test(value)) throw new Error("unsupported shaping cluster");
      assertInvoicePdfTextSupported(value, "report");
    } catch {
      throw new Error("REPORT_PDF_UNSUPPORTED_TEXT");
    }
  }
}
async function pdf(
  value: Context,
  title: string,
  periodText: string,
  content: ReturnType<typeof h>[],
  strings: string[],
) {
  preflight([title, periodText, value.organisation.legalName, value.organisation.abn, ...strings]);
  return boundedReportBytes(
    await renderDocument(
      h(
        Document,
        {
          title,
          author: value.organisation.legalName,
          creator: "Tammy · pdfcn / Forme",
          lang: "en-AU",
          tagged: true,
          fonts: [
            { family: "Noto Sans", fontWeight: 400, src: notoSansRegular },
            { family: "Noto Sans", fontWeight: 700, src: notoSansBold },
          ],
          style: { fontFamily: "Noto Sans", fontSize: 10, color: theme.colors.foreground },
        },
        h(
          Page,
          { size: "A4", margin: { top: 40, left: 40, right: 40, bottom: 36 } },
          h(PageFooter, { leftText: `${title} · AUD` }),
          h(PageHeader, {
            title,
            subtitle: value.organisation.legalName,
            rightText: `ABN ${value.organisation.abn} · All amounts in AUD`,
          }),
          h(Text, {}, periodText),
          ...content,
        ),
      ),
    ),
  );
}
export async function renderIncomePdf(value: IncomeSnapshot) {
  validateIncomeSnapshot(value);
  const metrics = incomeMetrics(value);
  const s = value.summary;
  const rows = metrics.map(([key, amount, currency, description]) => [
    description || key.replaceAll("_", " "),
    currency ? `$${amount}` : amount,
  ]);
  return pdf(
    value,
    "Income summary",
    `${reportDate(s.periodStart)} to ${reportDate(s.periodEnd)} · Balances as of ${reportDate(s.asOfDate)}`,
    [
      h(Text, {}, `Financial revision ${s.financialRevision} · Acquired ${value.acquiredAt}`),
      reportTable(["Metric", "Value"], rows, [0.62, 0.38]),
    ],
    rows.flat(),
  );
}
export async function renderBasPdf(value: BasSnapshot) {
  validateBasSnapshot(value);
  const s = value.workpaper;
  const sales = s.salesSources.map((source) => [
    reportDate(source.taxDate),
    source.customerName,
    `${source.invoiceNumber}\nInvoice ${source.invoiceId}${source.paymentId ? `\nReceipt ${source.paymentId}` : ""}\nRevision ${source.sourceRevision}\nHash ${hash(source.sourceHash)}`,
    money(source.gross),
    money(source.gst),
  ]);
  const purchases = s.sources.map((source) => [
    reportDate(source.documentDate),
    source.supplierName,
    `${source.invoiceNumber}\nDocument ${source.documentId}`,
    money(source.gross),
    money(source.gstCredit),
  ]);
  const metadata = basMetadata(value).map(([label, v]) => [label.replaceAll("_", " "), v]);
  const totals = basTotals(value).map(([label, v]) => [label.replaceAll("_", " "), `$${v}`]);
  const warnings = basWarnings(value);
  return pdf(
    value,
    "GST workpaper — Draft — not lodged",
    `${reportDate(s.periodStart)} to ${reportDate(s.periodEnd)} · ${basis(s.gstBasis)} basis`,
    [
      reportTable(["Workpaper", "Retained context"], metadata, [0.35, 0.65]),
      reportTable(["Total", "AUD"], totals, [0.65, 0.35]),
      ...warnings.map((warning) => h(Text, { style: { fontWeight: 700 } }, warning)),
      h(Text, { style: { fontWeight: 700, marginTop: 16 } }, "Invoice sales sources"),
      sales.length
        ? sourceTable(["Tax date", "Sales G1", "GST 1A"], sales, "Customer")
        : h(Text, {}, "No sales sources retained."),
      h(Text, { style: { fontWeight: 700, marginTop: 16 } }, "Reviewed purchase sources"),
      purchases.length
        ? sourceTable(["Document date", "Gross", "GST credit"], purchases, "Supplier")
        : h(Text, {}, "No purchase sources retained."),
    ],
    [...metadata.flat(), ...totals.flat(), ...sales.flat(), ...purchases.flat(), ...warnings],
  );
}
