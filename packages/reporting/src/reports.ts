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
function minorUnits(
  value: { readonly currencyCode: string; readonly minorUnits: bigint } | undefined,
) {
  if (value?.currencyCode !== "AUD") throw new Error("INVALID_REPORT_SNAPSHOT");
  exactDecimal(value.minorUnits);
  return value.minorUnits;
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
const BAS_PURCHASE_CSV_COLUMNS = [
  ...BAS_CSV_COLUMNS,
  "source_kind",
  "posting_id",
  "posting_hash",
  "original_content_hash",
  "reviewed_snapshot_hash",
  "payment_hash",
  "attribution_id",
  "attribution_hash",
  "account_id",
  "account_version",
  "supplier_name",
  "allocation_sequence",
  "use",
  "bas_classification",
  "cash_flow_classification",
  "invoice_date",
  "actual_payment_date",
  "normal_tax_event_date",
  "release_posting_date",
  "claim_period_start",
  "claim_period_end",
  "supplier_gst",
  "eligible_gst",
  "claimed_gst",
  "current_unclaimed_gst",
  "evidence_id",
  "evidence_version",
  "evidence_hash",
  "evidence_held_date",
  "review_id",
  "review_status",
  "review_kind",
  "review_reason",
  "review_affected_period_start",
  "review_affected_period_end",
  "deferral_id",
  "deferral_organisation_id",
  "deferral_affected_period_start",
  "deferral_affected_period_end",
  "deferral_hash",
  "deferral_accounting_version",
  "deferral_evidence_id",
  "original_deferred_gst",
  "deferral_source_kind",
  "deferral_source_posting_id",
  "deferral_source_posting_hash",
  "deferral_source_payment_id",
  "deferral_source_payment_hash",
  "deferral_allocation_sequence",
  "normal_event_date",
  "earliest_eligible_period_start",
  "earliest_eligible_period_end",
  "reviewed_as_of",
  "allowable_period_confirmed",
  "ordinary_time_limit_confirmed",
  "evidence_held_confirmed",
  "timing_reason",
  "required_return_due_date",
  "due_date_evidence_id",
  "due_date_evidence_version",
  "due_date_evidence_hash",
  "ordinary_last_claim_date",
  "legacy_review_id",
  "legacy_source_kind",
  "legacy_source_id",
  "legacy_source_version",
  "legacy_source_hash",
  "legacy_source_original_content_hash",
  "legacy_source_reviewed_snapshot_hash",
  "legacy_disposition",
  "legacy_affected_period_start",
  "legacy_affected_period_end",
  "reporting_balance_date",
  "previously_claimed_period_start",
  "previously_claimed_period_end",
  "claim_evidence_id",
  "claim_evidence_version",
  "claim_evidence_hash",
  "linked_posting_id",
  "review_source_revision",
  "review_source_hash",
  "actor_user_id",
  "reviewed_at",
  "legacy_reason",
  "deferral_reason",
  "deferral_actor_user_id",
  "deferral_recorded_at",
  "support_review_id",
  "support_kind",
  "support_sequence",
  "support_source_id",
  "support_source_version",
  "support_source_hash",
] as const;
const purchaseUseNames = { 1: "business", 2: "private" } as const;
const purchaseBasNames = { 1: "capital", 2: "non_capital", 3: "excluded" } as const;
const purchaseCashFlowNames = { 1: "operating", 2: "investing", 3: "financing" } as const;
const purchaseStatusNames = {
  1: "clear",
  2: "needs_evidence",
  3: "needs_timing_review",
  4: "unresolved_legacy",
  5: "unsupported",
  6: "deferred",
} as const;
const purchaseReviewNames = {
  1: "unresolved_eligibility",
  2: "missing_evidence",
  3: "claim_timing",
  4: "excluded_unpaid",
  5: "excluded_private_or_noncreditable",
  6: "duplicate",
  7: "unsupported_settlement",
  8: "deferred_future_claim",
  9: "old_legacy_document",
  10: "undated_legacy_document",
  11: "opening_payable",
  12: "resolved_legacy",
} as const;
const legacyDispositionNames = {
  1: "confirmed_unpaid_through_balance",
  2: "already_claimed",
  3: "duplicate",
  4: "noncreditable",
  5: "evidence_only_linked_purchase",
  6: "unsupported_existing_settlement",
} as const;
const legacySupportNames = { 1: "payment", 2: "evidence" } as const;
type PurchaseCsvColumn = (typeof BAS_PURCHASE_CSV_COLUMNS)[number];
function purchaseCsvRow(
  kind: string,
  fields: Partial<Record<PurchaseCsvColumn, string>>,
  numeric: readonly PurchaseCsvColumn[] = [],
) {
  const row = Array<string>(BAS_PURCHASE_CSV_COLUMNS.length).fill("");
  row[BAS_PURCHASE_CSV_COLUMNS.indexOf("kind")] = kind;
  for (const [name, value] of Object.entries(fields)) {
    const index = BAS_PURCHASE_CSV_COLUMNS.indexOf(name as PurchaseCsvColumn);
    if (index < 0) throw new Error("INVALID_REPORT_SNAPSHOT");
    if (value === undefined) continue;
    row[index] = numeric.includes(name as PurchaseCsvColumn) ? value : safeText(value);
  }
  return row;
}
function purchaseName<T extends Record<number, string>>(value: number, names: T) {
  return names[value as keyof T] ?? "invalid";
}
function optionalDate(value: Parameters<typeof reportDate>[0]) {
  return value ? reportDate(value) : "not retained";
}
function retainedVersion(value?: bigint) {
  return value === undefined ? "not retained" : String(value);
}
function retainedHash(value?: Uint8Array) {
  return value === undefined ? "not retained" : hash(value);
}
function sourceRefFields(
  value:
    | { readonly sourceId: string; readonly sourceVersion: bigint; readonly sourceHash: Uint8Array }
    | undefined,
  prefix = "evidence_",
) {
  return value
    ? {
        [`${prefix}id`]: value.sourceId,
        [`${prefix}version`]: String(value.sourceVersion),
        [`${prefix}hash`]: hash(value.sourceHash),
      }
    : {};
}
function claimPeriodFields(
  value:
    | {
        readonly periodStart?: Parameters<typeof reportDate>[0];
        readonly periodEnd?: Parameters<typeof reportDate>[0];
      }
    | undefined,
  prefix: string,
) {
  return value
    ? {
        [`${prefix}start`]: reportDate(value.periodStart),
        [`${prefix}end`]: reportDate(value.periodEnd),
      }
    : {};
}
function legacySourceFields(
  value:
    | {
        readonly source:
          | {
              readonly case: "document";
              readonly value: {
                readonly documentId: string;
                readonly reviewedVersion: bigint;
                readonly originalContentHash: Uint8Array;
                readonly reviewedSnapshotHash: Uint8Array;
              };
            }
          | {
              readonly case: "openingPayable";
              readonly value: {
                readonly openingPayableId: string;
                readonly sourceVersion: bigint;
                readonly sourceHash: Uint8Array;
              };
            }
          | { readonly case: undefined; readonly value?: undefined };
      }
    | undefined,
) {
  if (!value) return {};
  switch (value.source.case) {
    case "document":
      return {
        legacy_source_kind: "document",
        legacy_source_id: value.source.value.documentId,
        legacy_source_version: String(value.source.value.reviewedVersion),
        legacy_source_hash: "",
        legacy_source_original_content_hash: hash(value.source.value.originalContentHash),
        legacy_source_reviewed_snapshot_hash: hash(value.source.value.reviewedSnapshotHash),
      };
    case "openingPayable":
      return {
        legacy_source_kind: "opening_payable",
        legacy_source_id: value.source.value.openingPayableId,
        legacy_source_version: String(value.source.value.sourceVersion),
        legacy_source_hash: hash(value.source.value.sourceHash),
        legacy_source_original_content_hash: "",
        legacy_source_reviewed_snapshot_hash: "",
      };
    default:
      return {};
  }
}
function basMetadata(value: BasSnapshot): Metric[] {
  const s = value.workpaper;
  const rows: Metric[] = [
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
  if (s.purchaseSourceFormat === 1) {
    rows.push(
      [
        "purchase_source_format",
        "Reconciled v1",
        "",
        "Typed retained purchase sources and review decisions",
      ],
      [
        "legacy_candidate_gst",
        s.legacyCandidateGst ? money(s.legacyCandidateGst) : "not retained",
        s.legacyCandidateGst ? "AUD" : "",
        "Provisional candidate; excluded from GST credits 1B",
      ],
    );
  }
  return rows;
}
function basTotals(value: BasSnapshot): Metric[] {
  const s = value.workpaper;
  const rows: Metric[] = [
    ["sales_g1", money(s.salesG1), "AUD", "Sales G1"],
    ["gst_on_sales_1a", money(s.gstOnSales1a), "AUD", "GST on sales 1A"],
    ["gst_credits_1b", money(s.gstCredits1b), "AUD", "GST credits 1B"],
    ["net_gst_payable", money(s.netGstPayable), "AUD", "Negative means refundable"],
  ];
  if (s.purchaseSourceFormat === 1) {
    const purchases = s.purchaseSources ?? [];
    const typedClaimed = purchases.reduce((sum, source) => sum + minorUnits(source.claimedGst), 0n);
    const directClaims = s.sources.reduce((sum, source) => sum + minorUnits(source.gstCredit), 0n);
    const gross = (predicate: (source: (typeof purchases)[number]) => boolean) =>
      purchases.reduce(
        (sum, source) => sum + (predicate(source) ? minorUnits(source.allocatedGross) : 0n),
        0n,
      );
    const decimal = (minor: bigint) => exactDecimal(minor);
    rows.push(
      [
        "reconciled_purchase_credits_1b",
        decimal(typedClaimed + directClaims),
        "AUD",
        "Claimed typed sources plus existing direct purchase credits; reconciles to 1B",
      ],
      [
        "typed_purchase_claims",
        decimal(typedClaimed),
        "AUD",
        "Claimed GST from retained typed purchase sources",
      ],
      [
        "legacy_direct_purchase_credits",
        decimal(directClaims),
        "AUD",
        "Existing retained direct purchase source credits",
      ],
      [
        "typed_purchase_source_row_gross",
        decimal(gross(() => true)),
        "AUD",
        "Gross allocated to retained typed purchase source rows; allocation-level total, not unique invoice gross",
      ],
      [
        "capital_purchase_source_row_gross",
        decimal(gross((source) => source.basClassification === 1)),
        "AUD",
        "Capital source-row gross",
      ],
      [
        "non_capital_purchase_source_row_gross",
        decimal(gross((source) => source.basClassification === 2)),
        "AUD",
        "Non-capital source-row gross",
      ],
      [
        "excluded_purchase_source_row_gross",
        decimal(gross((source) => source.basClassification === 3)),
        "AUD",
        "Excluded source-row gross",
      ],
      [
        "business_purchase_source_row_gross",
        decimal(gross((source) => source.use === 1)),
        "AUD",
        "Business-use source-row gross",
      ],
      [
        "private_purchase_source_row_gross",
        decimal(gross((source) => source.use === 2)),
        "AUD",
        "Private-use source-row gross",
      ],
    );
  }
  return rows;
}
function purchaseSourceCsvFields(
  source: NonNullable<BasSnapshot["workpaper"]["purchaseSources"]>[number],
) {
  return {
    source_kind: "purchase_allocation",
    document_id: source.documentId,
    posting_id: source.postingId,
    posting_hash: hash(source.postingHash),
    original_content_hash: hash(source.originalContentHash),
    reviewed_snapshot_hash: hash(source.reviewedSnapshotHash),
    payment_id: source.paymentId ?? "",
    payment_hash: source.paymentHash === undefined ? "" : hash(source.paymentHash),
    attribution_id: source.attributionId ?? "",
    attribution_hash: source.attributionHash === undefined ? "" : hash(source.attributionHash),
    source_revision: String(source.sourceRevision),
    source_hash: hash(source.sourceHash),
    allocation_sequence: String(source.allocationSequence),
    account_id: source.accountId ?? "not retained",
    account_version: retainedVersion(source.accountVersion),
    supplier_name: source.supplierName ?? "not retained",
    invoice_number: source.invoiceNumber ?? "not retained",
    use: purchaseName(source.use, purchaseUseNames),
    bas_classification: purchaseName(source.basClassification, purchaseBasNames),
    cash_flow_classification: purchaseName(source.cashFlowClassification, purchaseCashFlowNames),
    invoice_date: reportDate(source.invoiceDate),
    actual_payment_date: optionalDate(source.actualPaymentDate),
    normal_tax_event_date: reportDate(source.normalTaxEventDate),
    release_posting_date: optionalDate(source.releasePostingDate),
    claim_period_start: reportDate(source.claimPeriod?.periodStart),
    claim_period_end: reportDate(source.claimPeriod?.periodEnd),
    source_date: reportDate(source.normalTaxEventDate),
    gross: money(source.allocatedGross),
    supplier_gst: money(source.supplierGst),
    eligible_gst: money(source.eligibleGst),
    claimed_gst: money(source.claimedGst),
    gst: money(source.claimedGst),
    evidence_id: source.evidence?.sourceId ?? "",
    evidence_version: retainedVersion(source.evidence?.sourceVersion),
    evidence_hash: retainedHash(source.evidence?.sourceHash),
    evidence_held_date: "not retained",
    party: source.supplierName ?? "not retained",
    context: "Retained purchase allocation; claimed GST contributes to 1B",
  };
}
function purchaseReviewCsvFields(
  review: NonNullable<BasSnapshot["workpaper"]["purchaseReviews"]>[number],
) {
  const future = review.futureClaimObligation;
  const legacy = review.legacyReview;
  const portion = future?.portion;
  const source = portion?.source;
  const deferralSource =
    source?.case === "posting"
      ? {
          deferral_source_kind: "posting",
          deferral_source_posting_id: source.value.postingId,
          deferral_source_posting_hash: hash(source.value.postingHash),
          deferral_allocation_sequence: String(source.value.allocationSequence),
        }
      : source?.case === "payment"
        ? {
            deferral_source_kind: "payment",
            deferral_source_posting_id: source.value.postingId,
            deferral_source_posting_hash: hash(source.value.postingHash),
            deferral_source_payment_id: source.value.paymentId,
            deferral_source_payment_hash: hash(source.value.paymentHash),
            deferral_allocation_sequence: String(source.value.allocationSequence),
          }
        : {};
  return {
    source_kind: "purchase_review",
    document_id: review.documentId ?? future?.documentId ?? "",
    posting_id: review.postingId ?? future?.postingId ?? "",
    payment_id: review.paymentId ?? "",
    source_revision: String(review.sourceRevision),
    source_hash: hash(review.sourceHash),
    source_date: optionalDate(review.actualPaymentDate ?? review.invoiceDate),
    invoice_date: optionalDate(review.invoiceDate),
    actual_payment_date: optionalDate(review.actualPaymentDate),
    gross: "",
    gst: money(review.unclaimedGst),
    eligible_gst: money(review.eligibleGst),
    current_unclaimed_gst: money(review.unclaimedGst),
    review_status: purchaseName(review.status, purchaseStatusNames),
    review_kind: purchaseName(review.kind, purchaseReviewNames),
    review_reason: review.reason,
    ...claimPeriodFields(review.affectedPeriod, "review_affected_period_"),
    review_id: legacy?.id ?? "",
    legacy_review_id: legacy?.id ?? "",
    deferral_id: future?.deferralId ?? review.deferralId ?? "",
    deferral_organisation_id: future?.organisationId ?? "",
    ...claimPeriodFields(future?.affectedPeriod, "deferral_affected_period_"),
    deferral_hash: future ? hash(future.deferralHash) : "",
    deferral_accounting_version: future ? String(future.accountingVersion) : "",
    deferral_evidence_id: portion?.evidenceId ?? "",
    original_deferred_gst: future ? money(future.allocatedDeferredGst) : "",
    normal_event_date: portion ? reportDate(portion.normalEventDate) : "",
    ...claimPeriodFields(portion?.earliestOtherwiseEligiblePeriod, "earliest_eligible_period_"),
    required_return_due_date: portion ? reportDate(portion.requiredReturnDueDate) : "",
    due_date_evidence_id: portion?.dueDateEvidence?.sourceId ?? "",
    due_date_evidence_version: retainedVersion(portion?.dueDateEvidence?.sourceVersion),
    due_date_evidence_hash: retainedHash(portion?.dueDateEvidence?.sourceHash),
    ordinary_last_claim_date: portion ? reportDate(portion.ordinaryLastClaimDate) : "",
    evidence_id: review.evidence?.sourceId ?? "",
    evidence_version: retainedVersion(review.evidence?.sourceVersion),
    evidence_hash: retainedHash(review.evidence?.sourceHash),
    evidence_held_date: portion ? reportDate(portion.evidenceHeldDate) : "",
    reviewed_as_of: future?.timingReview ? reportDate(future.timingReview.reviewedAsOf) : "",
    allowable_period_confirmed: future ? String(future.timingReview?.allowablePeriodConfirmed) : "",
    ordinary_time_limit_confirmed: future
      ? String(future.timingReview?.ordinaryTimeLimitConfirmed)
      : "",
    evidence_held_confirmed: future ? String(future.timingReview?.evidenceHeldConfirmed) : "",
    timing_reason: future?.timingReview?.reason ?? "",
    deferral_reason: future?.reason ?? "",
    deferral_actor_user_id: future?.actorUserId ?? "",
    deferral_recorded_at: future ? reportTimestamp(future.recordedAt) : "",
    ...legacySourceFields(review.legacySource),
    legacy_disposition: legacy ? purchaseName(legacy.disposition, legacyDispositionNames) : "",
    ...claimPeriodFields(legacy?.affectedPeriod, "legacy_affected_period_"),
    reporting_balance_date: legacy ? reportDate(legacy.reportingBalanceDate) : "",
    ...claimPeriodFields(legacy?.previouslyClaimedPeriod, "previously_claimed_period_"),
    ...sourceRefFields(legacy?.claimEvidence, "claim_evidence_"),
    linked_posting_id: legacy?.linkedPostingId ?? "",
    review_source_revision: legacy ? String(legacy.sourceRevision) : "",
    review_source_hash: legacy ? hash(legacy.sourceHash) : "",
    actor_user_id: legacy?.actorUserId ?? "",
    reviewed_at: legacy ? reportTimestamp(legacy.recordedAt) : "",
    legacy_reason: legacy?.reason ?? "",
    ...sourceRefFields(review.evidence),
    ...deferralSource,
    party: review.legacySource
      ? (legacySourceFields(review.legacySource).legacy_source_id ?? "")
      : "",
    context: "Review outcome; unclaimed GST is not included in 1B",
  };
}
function purchaseSupportCsvFields(
  value: NonNullable<BasSnapshot["workpaper"]["legacyPurchaseSupport"]>[number],
) {
  return {
    source_kind: "legacy_purchase_support",
    review_id: value.reviewId,
    support_review_id: value.reviewId,
    support_kind: purchaseName(value.kind, legacySupportNames),
    support_sequence: String(value.sequence),
    support_source_id: value.source?.sourceId ?? "",
    support_source_version: retainedVersion(value.source?.sourceVersion),
    support_source_hash: retainedHash(value.source?.sourceHash),
    context: "Retained supporting source link; original evidence remains server-owned",
  };
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
function legacyBasCsv(value: BasSnapshot) {
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
function reconciledBasCsv(value: BasSnapshot) {
  const rows: string[][] = [[...BAS_PURCHASE_CSV_COLUMNS]];
  const metric = (kind: string, row: Metric) =>
    rows.push(
      purchaseCsvRow(
        kind,
        { metric: row[0], value: row[1], currency: row[2], context: row[3] },
        row[2] === "AUD" ? ["value"] : [],
      ),
    );
  for (const row of basMetadata(value)) metric("metadata", row);
  for (const row of basTotals(value)) metric("totals", row);
  for (const source of value.workpaper.salesSources)
    rows.push(
      purchaseCsvRow(
        "sales",
        {
          source_kind: "invoice_sale",
          invoice_id: source.invoiceId,
          payment_id: source.paymentId ?? "",
          party: source.customerName,
          invoice_number: source.invoiceNumber,
          source_date: reportDate(source.taxDate),
          gross: money(source.gross),
          gst: money(source.gst),
          source_revision: String(source.sourceRevision),
          source_hash: hash(source.sourceHash),
          currency: "AUD",
        },
        ["gross", "gst"],
      ),
    );
  for (const source of value.workpaper.sources)
    rows.push(
      purchaseCsvRow(
        "purchases",
        {
          source_kind: "legacy_direct_purchase_credit",
          document_id: source.documentId,
          party: source.supplierName,
          invoice_number: source.invoiceNumber,
          source_date: reportDate(source.documentDate),
          gross: money(source.gross),
          gst: money(source.gstCredit),
          currency: "AUD",
          context: "Retained legacy direct purchase source credit included in 1B",
        },
        ["gross", "gst"],
      ),
    );
  for (const source of value.workpaper.purchaseSources ?? [])
    rows.push(
      purchaseCsvRow("purchase_sources", purchaseSourceCsvFields(source), [
        "gross",
        "supplier_gst",
        "eligible_gst",
        "claimed_gst",
        "gst",
      ]),
    );
  for (const review of value.workpaper.purchaseReviews ?? []) {
    const fields = purchaseReviewCsvFields(review);
    rows.push(
      purchaseCsvRow(
        "purchase_reviews",
        {
          ...fields,
          metric: purchaseName(review.status, purchaseStatusNames),
          value: money(review.unclaimedGst),
          currency: "AUD",
          eligible_gst: money(review.eligibleGst),
          current_unclaimed_gst: money(review.unclaimedGst),
        },
        ["value", "eligible_gst", "current_unclaimed_gst", "original_deferred_gst"],
      ),
    );
  }
  for (const support of value.workpaper.legacyPurchaseSupport ?? [])
    rows.push(purchaseCsvRow("legacy_purchase_support", purchaseSupportCsvFields(support)));
  for (const warning of basWarnings(value)) metric("warnings", ["warning", warning, "", ""]);
  return csv(rows);
}
export function renderBasCsv(value: BasSnapshot) {
  validateBasSnapshot(value);
  return value.workpaper.purchaseSourceFormat === 1 ? reconciledBasCsv(value) : legacyBasCsv(value);
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
  partyLabel: "Customer" | "Supplier" | "Source",
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
function purchaseSourcePdfRow(
  source: NonNullable<BasSnapshot["workpaper"]["purchaseSources"]>[number],
) {
  const evidence = source.evidence;
  const detail = [
    `Source allocation ${source.postingId} sequence ${source.allocationSequence} · Document ${source.documentId}`,
    `Supplier ${source.supplierName ?? "name not retained"} · Invoice ${source.invoiceNumber ?? "not retained"}`,
    `Posting ${source.postingId} · posting hash ${hash(source.postingHash)}`,
    `Original content hash ${hash(source.originalContentHash)} · reviewed snapshot hash ${hash(source.reviewedSnapshotHash)}`,
    `Payment ${source.paymentId ?? "not retained"} · payment hash ${retainedHash(source.paymentHash)}`,
    `Attribution ${source.attributionId ?? "not retained"} · attribution hash ${retainedHash(source.attributionHash)}`,
    `Allocation ${source.allocationSequence} · account ${source.accountId ?? "not retained"} · account version ${retainedVersion(source.accountVersion)}`,
    `Use ${purchaseName(source.use, purchaseUseNames)} · BAS class ${purchaseName(source.basClassification, purchaseBasNames)} · cash flow ${purchaseName(source.cashFlowClassification, purchaseCashFlowNames)}`,
    `Invoice date ${reportDate(source.invoiceDate)} · actual payment date ${optionalDate(source.actualPaymentDate)} · normal tax event ${reportDate(source.normalTaxEventDate)}`,
    `Selected claim period ${reportDate(source.claimPeriod?.periodStart)} to ${reportDate(source.claimPeriod?.periodEnd)} · release posting date ${optionalDate(source.releasePostingDate)}`,
    `Allocation amounts · gross allocated $${money(source.allocatedGross)} · supplier GST $${money(source.supplierGst)} · eligible GST $${money(source.eligibleGst)} · claimed GST $${money(source.claimedGst)}`,
    `Evidence ${evidence?.sourceId ?? "not retained"} · version ${retainedVersion(evidence?.sourceVersion)} · hash ${retainedHash(evidence?.sourceHash)}`,
    `Source revision ${source.sourceRevision} · source hash ${hash(source.sourceHash)}`,
  ];
  return [
    reportDate(source.normalTaxEventDate),
    source.supplierName ?? "Supplier name not retained",
    detail.join("\n"),
    money(source.allocatedGross),
    money(source.claimedGst),
  ];
}
function purchaseReviewPdfRow(
  review: NonNullable<BasSnapshot["workpaper"]["purchaseReviews"]>[number],
  support: NonNullable<BasSnapshot["workpaper"]["legacyPurchaseSupport"]>,
) {
  const legacy = review.legacyReview;
  const future = review.futureClaimObligation;
  const portion = future?.portion;
  const dueDateEvidence = portion?.dueDateEvidence;
  const legacySource = legacySourceFields(review.legacySource);
  const details = [
    `Review ${purchaseName(review.status, purchaseStatusNames)} · ${purchaseName(review.kind, purchaseReviewNames).replaceAll("_", " ")}`,
    `Review reason: ${review.reason}`,
    `Review document ${review.documentId ?? future?.documentId ?? "not retained"} · posting ${review.postingId ?? future?.postingId ?? "not retained"} · payment ${review.paymentId ?? "not retained"} · deferral ${review.deferralId ?? "not retained"}`,
    `Source revision ${review.sourceRevision} · source hash ${hash(review.sourceHash)}`,
    `Invoice date ${optionalDate(review.invoiceDate)} · actual payment date ${optionalDate(review.actualPaymentDate)} · eligible GST $${money(review.eligibleGst)} · current physical unclaimed GST $${money(review.unclaimedGst)}`,
    review.evidence
      ? `Evidence ${review.evidence.sourceId} · version ${review.evidence.sourceVersion} · hash ${hash(review.evidence.sourceHash)}`
      : "Evidence provenance: not retained",
    legacy
      ? `Legacy review ${legacy.id} · source ${legacySource.legacy_source_kind ?? "not retained"} ${legacySource.legacy_source_id ?? ""} · version ${legacySource.legacy_source_version ?? "not retained"} · hash ${legacySource.legacy_source_hash ?? "not retained"}`
      : "",
    legacySource.legacy_source_kind === "document"
      ? `Legacy source document ${legacySource.legacy_source_id} · version ${legacySource.legacy_source_version} · original content hash ${legacySource.legacy_source_original_content_hash} · reviewed snapshot hash ${legacySource.legacy_source_reviewed_snapshot_hash}`
      : legacySource.legacy_source_kind === "opening_payable"
        ? `Legacy source opening payable ${legacySource.legacy_source_id} · version ${legacySource.legacy_source_version} · source hash ${legacySource.legacy_source_hash}`
        : "",
    legacy && legacySource.legacy_source_kind === "document"
      ? `Legacy document original content hash ${legacySource.legacy_source_original_content_hash} · reviewed snapshot hash ${legacySource.legacy_source_reviewed_snapshot_hash}`
      : "",
    legacy
      ? `Legacy disposition ${purchaseName(legacy.disposition, legacyDispositionNames)} · affected period ${reportDate(legacy.affectedPeriod?.periodStart)} to ${reportDate(legacy.affectedPeriod?.periodEnd)} · reporting balance date ${reportDate(legacy.reportingBalanceDate)}`
      : "",
    legacy?.previouslyClaimedPeriod
      ? `Previously claimed period ${reportDate(legacy.previouslyClaimedPeriod.periodStart)} to ${reportDate(legacy.previouslyClaimedPeriod.periodEnd)}`
      : "Previously claimed period: not retained",
    legacy?.claimEvidence
      ? `Claim evidence ${legacy.claimEvidence.sourceId} · version ${legacy.claimEvidence.sourceVersion} · hash ${hash(legacy.claimEvidence.sourceHash)}`
      : "Claim evidence: not retained",
    legacy?.linkedPostingId ? `Linked purchase posting ${legacy.linkedPostingId}` : "",
    legacy ? `Legacy decision reason: ${legacy.reason}` : "",
    legacy
      ? `Decision source revision ${legacy.sourceRevision} · source hash ${hash(legacy.sourceHash)} · actor ${legacy.actorUserId} · recorded ${reportTimestamp(legacy.recordedAt)}`
      : "",
    future
      ? `Original deferral ${future.deferralId} · organisation ${future.organisationId} · affected period ${reportDate(future.affectedPeriod?.periodStart)} to ${reportDate(future.affectedPeriod?.periodEnd)} · hash ${hash(future.deferralHash)} · accounting version ${future.accountingVersion} · original deferred GST $${money(future.allocatedDeferredGst)}`
      : "",
    portion
      ? `Deferred source ${portion.source.case} · normal event ${reportDate(portion.normalEventDate)} · earliest eligible period ${reportDate(portion.earliestOtherwiseEligiblePeriod?.periodStart)} to ${reportDate(portion.earliestOtherwiseEligiblePeriod?.periodEnd)}`
      : "",
    portion && portion.source.case === "payment"
      ? `Payment portion ${portion.source.value.paymentId} · hash ${hash(portion.source.value.paymentHash)} · allocation ${portion.source.value.allocationSequence}`
      : portion && portion.source.case === "posting"
        ? `Posting portion ${portion.source.value.postingId} · hash ${hash(portion.source.value.postingHash)} · allocation ${portion.source.value.allocationSequence}`
        : "",
    portion
      ? `Reviewed as of ${reportDate(future?.timingReview?.reviewedAsOf)} · allowable period confirmed ${String(future?.timingReview?.allowablePeriodConfirmed)} · ordinary time limit confirmed ${String(future?.timingReview?.ordinaryTimeLimitConfirmed)} · evidence held confirmed ${String(future?.timingReview?.evidenceHeldConfirmed)}`
      : "",
    portion ? `Timing review reason: ${future?.timingReview?.reason ?? "not retained"}` : "",
    portion
      ? `Required return due ${reportDate(portion.requiredReturnDueDate)} · due-date evidence ${dueDateEvidence?.sourceId ?? "not retained"} version ${retainedVersion(dueDateEvidence?.sourceVersion)} hash ${retainedHash(dueDateEvidence?.sourceHash)}`
      : "",
    portion
      ? `Ordinary last claim date ${reportDate(portion.ordinaryLastClaimDate)} · evidence ${portion.evidenceId} held ${reportDate(portion.evidenceHeldDate)}`
      : "",
    future
      ? `Deferral reason: ${future.reason} · actor ${future.actorUserId} · recorded ${reportTimestamp(future.recordedAt)}`
      : "",
    ...(legacy
      ? support
          .filter((link) => link.reviewId === legacy.id)
          .map(
            (link) =>
              `Supporting ${purchaseName(link.kind, legacySupportNames)} ${link.sequence}: ${link.source?.sourceId ?? "not retained"} · version ${retainedVersion(link.source?.sourceVersion)} · hash ${retainedHash(link.source?.sourceHash)}`,
          )
      : []),
  ].filter(Boolean);
  const legacySourceId = legacySource.legacy_source_id ?? "";
  return [
    optionalDate(review.actualPaymentDate ?? review.invoiceDate),
    legacySourceId || review.postingId || future?.postingId || "Source not retained",
    details.join("\n"),
    money(review.eligibleGst),
    money(review.unclaimedGst),
  ];
}
async function renderReconciledBasPdf(value: BasSnapshot) {
  const s = value.workpaper;
  const sales = s.salesSources.map((source) => [
    reportDate(source.taxDate),
    source.customerName,
    `${source.invoiceNumber}\nInvoice ${source.invoiceId}${source.paymentId ? `\nReceipt ${source.paymentId}` : ""}\nRevision ${source.sourceRevision}\nHash ${hash(source.sourceHash)}`,
    money(source.gross),
    money(source.gst),
  ]);
  const legacyPurchases = s.sources.map((source) => [
    reportDate(source.documentDate),
    source.supplierName,
    `${source.invoiceNumber}\nDocument ${source.documentId}\nLegacy purchase row ${source.documentId} · supplier ${source.supplierName} · invoice ${source.invoiceNumber} · source date ${reportDate(source.documentDate)} · gross $${money(source.gross)} · GST credit $${money(source.gstCredit)}`,
    money(source.gross),
    money(source.gstCredit),
  ]);
  const purchases = (s.purchaseSources ?? []).map(purchaseSourcePdfRow);
  const reviews = (s.purchaseReviews ?? []).map((review) =>
    purchaseReviewPdfRow(review, s.legacyPurchaseSupport ?? []),
  );
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
      h(
        Text,
        { style: { fontWeight: 700, marginTop: 16 } },
        "Existing legacy direct purchase credits",
      ),
      legacyPurchases.length
        ? sourceTable(["Document date", "Gross", "GST credit"], legacyPurchases, "Supplier")
        : h(Text, {}, "No legacy direct purchase credits retained."),
      h(
        Text,
        { style: { fontWeight: 700, marginTop: 16 } },
        "Reconciled purchase source allocations",
      ),
      purchases.length
        ? sourceTable(["Normal event", "Gross allocated", "GST claimed"], purchases, "Supplier")
        : h(Text, {}, "No reconciled purchase source allocations retained."),
      h(
        Text,
        { style: { fontWeight: 700, marginTop: 16 } },
        "Purchase evidence, timing and legacy review outcomes",
      ),
      reviews.length
        ? sourceTable(["Review event", "Eligible GST", "Unclaimed GST"], reviews, "Source")
        : h(Text, {}, "No purchase review outcomes retained."),
    ],
    [
      ...metadata.flat(),
      ...totals.flat(),
      ...sales.flat(),
      ...legacyPurchases.flat(),
      ...purchases.flat(),
      ...reviews.flat(),
      ...warnings,
    ],
  );
}
export async function renderBasPdf(value: BasSnapshot) {
  validateBasSnapshot(value);
  const s = value.workpaper;
  if (s.purchaseSourceFormat === 1) return renderReconciledBasPdf(value);
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
