import assert from "node:assert/strict";
import type { BasWorkpaper } from "@tammy/connect-client/tammy/v1/tax_pb.js";
import {
  exactDecimal,
  type ParsedReportCsv,
  parseReportCsv,
  readPdfPages,
} from "./reporting-export-artifacts.js";

const statusNames: Record<number, string> = {
  1: "clear",
  2: "needs_evidence",
  3: "needs_timing_review",
  4: "unresolved_legacy",
  5: "unsupported",
  6: "deferred",
};
const reviewNames: Record<number, string> = {
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
};
const dispositionNames: Record<number, string> = {
  1: "confirmed_unpaid_through_balance",
  2: "already_claimed",
  3: "duplicate",
  4: "noncreditable",
  5: "evidence_only_linked_purchase",
  6: "unsupported_existing_settlement",
};
const supportNames: Record<number, string> = { 1: "payment", 2: "evidence" };
const useNames: Record<number, string> = { 1: "business", 2: "private" };
const basNames: Record<number, string> = { 1: "capital", 2: "non_capital", 3: "excluded" };
const cashFlowNames: Record<number, string> = { 1: "operating", 2: "investing", 3: "financing" };

function amount(value: { readonly minorUnits: bigint } | undefined) {
  assert.ok(value, "retained purchase export amount is missing");
  return exactDecimal(value.minorUnits);
}

function date(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
) {
  if (!value) return "not retained";
  return `${String(value.year).padStart(4, "0")}-${String(value.month).padStart(2, "0")}-${String(value.day).padStart(2, "0")}`;
}

function hash(value: Uint8Array | undefined) {
  return value ? Buffer.from(value).toString("hex") : "";
}

function safeText(value: string) {
  return /^[\s\p{Cc}\p{Cf}]*[=+@-]/u.test(value) ? `'${value}` : value;
}

function timestamp(value: { readonly seconds: bigint; readonly nanos: number } | undefined) {
  if (!value) return "";
  const milliseconds = Number(value.seconds) * 1000;
  const fraction =
    value.nanos === 0 ? "" : `.${String(value.nanos).padStart(9, "0").replace(/0+$/u, "")}`;
  return `${new Date(milliseconds).toISOString().replace(/\.000Z$/u, "")}${fraction}Z`;
}

function rowValue(row: Readonly<Record<string, string>>, key: string) {
  assert.ok(Object.hasOwn(row, key), `purchase CSV must include ${key}`);
  return row[key];
}

function metric(csv: ParsedReportCsv, key: string) {
  const row = csv.rows.find(
    (candidate) => candidate.kind === "metadata" && candidate.metric === key,
  );
  assert.ok(row, `purchase CSV must include metadata ${key}`);
  return row;
}

function sourceRef(
  row: Readonly<Record<string, string>>,
  value:
    | { readonly sourceId: string; readonly sourceVersion: bigint; readonly sourceHash: Uint8Array }
    | undefined,
  prefix: string,
) {
  assert.equal(rowValue(row, `${prefix}id`), value?.sourceId ?? "");
  assert.equal(rowValue(row, `${prefix}version`), value?.sourceVersion.toString() ?? "");
  assert.equal(rowValue(row, `${prefix}hash`), hash(value?.sourceHash));
}

function assertPurchaseCsv(csv: ParsedReportCsv, bas: BasWorkpaper) {
  assert.equal(
    bas.purchaseSourceFormat,
    1,
    "purchase export assertions require retained purchase format v1",
  );
  for (const field of [
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
    "review_status",
    "review_kind",
    "review_affected_period_start",
    "review_affected_period_end",
    "review_reason",
    "deferral_hash",
    "deferral_organisation_id",
    "deferral_affected_period_start",
    "deferral_affected_period_end",
    "original_deferred_gst",
    "required_return_due_date",
    "due_date_evidence_id",
    "due_date_evidence_hash",
    "ordinary_last_claim_date",
    "legacy_source_original_content_hash",
    "legacy_source_reviewed_snapshot_hash",
    "legacy_disposition",
    "review_source_revision",
    "review_source_hash",
    "support_kind",
    "support_sequence",
    "support_source_id",
    "support_source_version",
    "support_source_hash",
  ])
    assert.ok(csv.header.includes(field), `purchase CSV header must include ${field}`);

  assert.equal(metric(csv, "purchase_source_format").value, "Reconciled v1");
  assert.equal(
    metric(csv, "legacy_candidate_gst").value,
    bas.legacyCandidateGst ? amount(bas.legacyCandidateGst) : "not retained",
  );
  assert.equal(
    metric(csv, "legacy_candidate_gst").context,
    "Provisional candidate; excluded from GST credits 1B",
  );

  const directRows = csv.rows.filter(
    (row) => row.kind === "purchases" && row.source_kind === "legacy_direct_purchase_credit",
  );
  assert.equal(directRows.length, bas.sources.length);
  for (const [index, source] of bas.sources.entries()) {
    const row = directRows[index];
    assert.ok(row, `legacy direct purchase row ${index + 1} is missing`);
    assert.equal(rowValue(row, "document_id"), source.documentId);
    assert.equal(rowValue(row, "party"), safeText(source.supplierName));
    assert.equal(rowValue(row, "invoice_number"), safeText(source.invoiceNumber));
    assert.equal(rowValue(row, "source_date"), date(source.documentDate));
    assert.equal(rowValue(row, "gross"), amount(source.gross));
    assert.equal(rowValue(row, "gst"), amount(source.gstCredit));
  }

  const sourceRows = csv.rows.filter((row) => row.kind === "purchase_sources");
  assert.equal(sourceRows.length, bas.purchaseSources.length);
  for (const [index, source] of bas.purchaseSources.entries()) {
    const row = sourceRows[index];
    assert.ok(row, `purchase source row ${index + 1} is missing`);
    assert.equal(rowValue(row, "source_kind"), "purchase_allocation");
    assert.equal(rowValue(row, "document_id"), source.documentId);
    assert.equal(rowValue(row, "posting_id"), source.postingId);
    assert.equal(rowValue(row, "posting_hash"), hash(source.postingHash));
    assert.equal(rowValue(row, "original_content_hash"), hash(source.originalContentHash));
    assert.equal(rowValue(row, "reviewed_snapshot_hash"), hash(source.reviewedSnapshotHash));
    assert.equal(rowValue(row, "payment_id"), source.paymentId ?? "");
    assert.equal(rowValue(row, "payment_hash"), hash(source.paymentHash));
    assert.equal(rowValue(row, "attribution_id"), source.attributionId ?? "");
    assert.equal(rowValue(row, "attribution_hash"), hash(source.attributionHash));
    assert.equal(rowValue(row, "source_revision"), source.sourceRevision.toString());
    assert.equal(rowValue(row, "source_hash"), hash(source.sourceHash));
    assert.equal(rowValue(row, "allocation_sequence"), String(source.allocationSequence));
    assert.equal(rowValue(row, "account_id"), source.accountId ?? "not retained");
    assert.equal(
      rowValue(row, "account_version"),
      source.accountVersion?.toString() ?? "not retained",
    );
    assert.equal(rowValue(row, "supplier_name"), safeText(source.supplierName ?? "not retained"));
    assert.equal(rowValue(row, "invoice_number"), safeText(source.invoiceNumber ?? "not retained"));
    assert.equal(rowValue(row, "use"), useNames[source.use]);
    assert.equal(rowValue(row, "bas_classification"), basNames[source.basClassification]);
    assert.equal(
      rowValue(row, "cash_flow_classification"),
      cashFlowNames[source.cashFlowClassification],
    );
    assert.equal(rowValue(row, "invoice_date"), date(source.invoiceDate));
    assert.equal(rowValue(row, "actual_payment_date"), date(source.actualPaymentDate));
    assert.equal(rowValue(row, "normal_tax_event_date"), date(source.normalTaxEventDate));
    assert.equal(rowValue(row, "release_posting_date"), date(source.releasePostingDate));
    assert.equal(rowValue(row, "claim_period_start"), date(source.claimPeriod?.periodStart));
    assert.equal(rowValue(row, "claim_period_end"), date(source.claimPeriod?.periodEnd));
    assert.equal(rowValue(row, "gross"), amount(source.allocatedGross));
    assert.equal(rowValue(row, "supplier_gst"), amount(source.supplierGst));
    assert.equal(rowValue(row, "eligible_gst"), amount(source.eligibleGst));
    assert.equal(rowValue(row, "claimed_gst"), amount(source.claimedGst));
    assert.equal(rowValue(row, "evidence_id"), source.evidence?.sourceId ?? "");
    assert.equal(
      rowValue(row, "evidence_version"),
      source.evidence?.sourceVersion.toString() ?? "not retained",
    );
    assert.equal(
      rowValue(row, "evidence_hash"),
      source.evidence ? hash(source.evidence.sourceHash) : "not retained",
    );
  }

  const reviewRows = csv.rows.filter((row) => row.kind === "purchase_reviews");
  assert.equal(reviewRows.length, bas.purchaseReviews.length);
  for (const [index, review] of bas.purchaseReviews.entries()) {
    const row = reviewRows[index];
    assert.ok(row, `purchase review row ${index + 1} is missing`);
    assert.equal(
      rowValue(row, "document_id"),
      review.documentId ?? review.futureClaimObligation?.documentId ?? "",
    );
    assert.equal(rowValue(row, "review_status"), statusNames[review.status]);
    assert.equal(rowValue(row, "review_kind"), reviewNames[review.kind]);
    assert.equal(
      rowValue(row, "review_affected_period_start"),
      date(review.affectedPeriod?.periodStart),
    );
    assert.equal(
      rowValue(row, "review_affected_period_end"),
      date(review.affectedPeriod?.periodEnd),
    );
    assert.equal(rowValue(row, "review_reason"), safeText(review.reason));
    assert.equal(rowValue(row, "source_revision"), review.sourceRevision.toString());
    assert.equal(rowValue(row, "source_hash"), hash(review.sourceHash));
    assert.equal(rowValue(row, "eligible_gst"), amount(review.eligibleGst));
    assert.equal(rowValue(row, "current_unclaimed_gst"), amount(review.unclaimedGst));
    assert.equal(
      rowValue(row, "posting_id"),
      review.postingId ?? review.futureClaimObligation?.postingId ?? "",
    );
    assert.equal(rowValue(row, "payment_id"), review.paymentId ?? "");
    assert.equal(rowValue(row, "invoice_date"), date(review.invoiceDate));
    assert.equal(rowValue(row, "actual_payment_date"), date(review.actualPaymentDate));
    assert.equal(
      rowValue(row, "deferral_id"),
      review.deferralId ?? review.futureClaimObligation?.deferralId ?? "",
    );
    assert.equal(rowValue(row, "evidence_id"), review.evidence?.sourceId ?? "");
    assert.equal(
      rowValue(row, "evidence_version"),
      review.evidence?.sourceVersion.toString() ?? "not retained",
    );
    assert.equal(
      rowValue(row, "evidence_hash"),
      review.evidence ? hash(review.evidence.sourceHash) : "not retained",
    );
    const retainedLegacySource = review.legacySource?.source;
    if (retainedLegacySource?.case === "document") {
      assert.equal(rowValue(row, "legacy_source_kind"), "document");
      assert.equal(rowValue(row, "legacy_source_id"), retainedLegacySource.value.documentId);
      assert.equal(
        rowValue(row, "legacy_source_version"),
        retainedLegacySource.value.reviewedVersion.toString(),
      );
      assert.equal(rowValue(row, "legacy_source_hash"), "");
      assert.equal(
        rowValue(row, "legacy_source_original_content_hash"),
        hash(retainedLegacySource.value.originalContentHash),
      );
      assert.equal(
        rowValue(row, "legacy_source_reviewed_snapshot_hash"),
        hash(retainedLegacySource.value.reviewedSnapshotHash),
      );
    } else if (retainedLegacySource?.case === "openingPayable") {
      assert.equal(rowValue(row, "legacy_source_kind"), "opening_payable");
      assert.equal(rowValue(row, "legacy_source_id"), retainedLegacySource.value.openingPayableId);
      assert.equal(
        rowValue(row, "legacy_source_version"),
        retainedLegacySource.value.sourceVersion.toString(),
      );
      assert.equal(
        rowValue(row, "legacy_source_hash"),
        hash(retainedLegacySource.value.sourceHash),
      );
      assert.equal(rowValue(row, "legacy_source_original_content_hash"), "");
      assert.equal(rowValue(row, "legacy_source_reviewed_snapshot_hash"), "");
    }
    if (review.futureClaimObligation) {
      const future = review.futureClaimObligation;
      assert.equal(rowValue(row, "document_id"), future.documentId);
      assert.equal(rowValue(row, "deferral_hash"), hash(future.deferralHash));
      assert.equal(rowValue(row, "deferral_organisation_id"), future.organisationId);
      assert.equal(
        rowValue(row, "deferral_affected_period_start"),
        date(future.affectedPeriod?.periodStart),
      );
      assert.equal(
        rowValue(row, "deferral_affected_period_end"),
        date(future.affectedPeriod?.periodEnd),
      );
      assert.equal(
        rowValue(row, "deferral_accounting_version"),
        future.accountingVersion.toString(),
      );
      assert.equal(rowValue(row, "deferral_evidence_id"), future.portion?.evidenceId ?? "");
      assert.equal(rowValue(row, "original_deferred_gst"), amount(future.allocatedDeferredGst));
      assert.equal(rowValue(row, "deferral_reason"), future.reason);
      assert.equal(rowValue(row, "deferral_actor_user_id"), future.actorUserId);
      assert.equal(rowValue(row, "deferral_recorded_at"), timestamp(future.recordedAt));
      const portion = future.portion;
      assert.ok(portion);
      assert.equal(rowValue(row, "normal_event_date"), date(portion.normalEventDate));
      assert.equal(
        rowValue(row, "earliest_eligible_period_start"),
        date(portion.earliestOtherwiseEligiblePeriod?.periodStart),
      );
      assert.equal(
        rowValue(row, "earliest_eligible_period_end"),
        date(portion.earliestOtherwiseEligiblePeriod?.periodEnd),
      );
      assert.equal(rowValue(row, "required_return_due_date"), date(portion.requiredReturnDueDate));
      assert.equal(rowValue(row, "due_date_evidence_id"), portion.dueDateEvidence?.sourceId ?? "");
      assert.equal(
        rowValue(row, "due_date_evidence_version"),
        portion.dueDateEvidence?.sourceVersion.toString() ?? "",
      );
      assert.equal(
        rowValue(row, "due_date_evidence_hash"),
        hash(portion.dueDateEvidence?.sourceHash),
      );
      assert.equal(rowValue(row, "ordinary_last_claim_date"), date(portion.ordinaryLastClaimDate));
      assert.equal(rowValue(row, "evidence_held_date"), date(portion.evidenceHeldDate));
      assert.equal(rowValue(row, "reviewed_as_of"), date(future.timingReview?.reviewedAsOf));
      assert.equal(rowValue(row, "timing_reason"), future.timingReview?.reason ?? "");
      assert.equal(
        rowValue(row, "allowable_period_confirmed"),
        String(future.timingReview?.allowablePeriodConfirmed),
      );
      assert.equal(
        rowValue(row, "ordinary_time_limit_confirmed"),
        String(future.timingReview?.ordinaryTimeLimitConfirmed),
      );
      assert.equal(
        rowValue(row, "evidence_held_confirmed"),
        String(future.timingReview?.evidenceHeldConfirmed),
      );
      const sourcePortion = portion.source.value;
      assert.ok(sourcePortion);
      assert.equal(rowValue(row, "deferral_source_kind"), portion.source.case);
      assert.equal(rowValue(row, "deferral_source_posting_id"), sourcePortion.postingId);
      assert.equal(rowValue(row, "deferral_source_posting_hash"), hash(sourcePortion.postingHash));
      assert.equal(
        rowValue(row, "deferral_allocation_sequence"),
        String(sourcePortion.allocationSequence),
      );
      if (portion.source.case === "payment") {
        const paymentPortion = portion.source.value;
        assert.ok(paymentPortion);
        assert.equal(rowValue(row, "deferral_source_payment_id"), paymentPortion.paymentId);
        assert.equal(
          rowValue(row, "deferral_source_payment_hash"),
          hash(paymentPortion.paymentHash),
        );
      } else {
        assert.equal(rowValue(row, "deferral_source_payment_id"), "");
        assert.equal(rowValue(row, "deferral_source_payment_hash"), "");
      }
    }
    if (review.legacyReview) {
      const decision = review.legacyReview;
      assert.equal(rowValue(row, "legacy_review_id"), decision.id);
      assert.equal(rowValue(row, "legacy_disposition"), dispositionNames[decision.disposition]);
      assert.equal(rowValue(row, "reporting_balance_date"), date(decision.reportingBalanceDate));
      assert.equal(rowValue(row, "legacy_reason"), safeText(decision.reason));
      assert.equal(rowValue(row, "review_source_revision"), decision.sourceRevision.toString());
      assert.equal(rowValue(row, "review_source_hash"), hash(decision.sourceHash));
      assert.equal(rowValue(row, "actor_user_id"), decision.actorUserId);
      assert.equal(rowValue(row, "reviewed_at"), timestamp(decision.recordedAt));
      assert.equal(
        rowValue(row, "legacy_affected_period_start"),
        date(decision.affectedPeriod?.periodStart),
      );
      assert.equal(
        rowValue(row, "legacy_affected_period_end"),
        date(decision.affectedPeriod?.periodEnd),
      );
      assert.equal(
        rowValue(row, "previously_claimed_period_start"),
        decision.previouslyClaimedPeriod ? date(decision.previouslyClaimedPeriod.periodStart) : "",
      );
      assert.equal(
        rowValue(row, "previously_claimed_period_end"),
        decision.previouslyClaimedPeriod ? date(decision.previouslyClaimedPeriod.periodEnd) : "",
      );
      assert.equal(rowValue(row, "linked_posting_id"), decision.linkedPostingId ?? "");
      sourceRef(row, decision.claimEvidence, "claim_evidence_");
      const source = decision.source?.source;
      if (source?.case === "document") {
        assert.equal(rowValue(row, "legacy_source_kind"), "document");
        assert.equal(rowValue(row, "legacy_source_id"), source.value.documentId);
        assert.equal(
          rowValue(row, "legacy_source_version"),
          source.value.reviewedVersion.toString(),
        );
        assert.equal(
          rowValue(row, "legacy_source_original_content_hash"),
          hash(source.value.originalContentHash),
        );
        assert.equal(
          rowValue(row, "legacy_source_reviewed_snapshot_hash"),
          hash(source.value.reviewedSnapshotHash),
        );
      } else if (source?.case === "openingPayable") {
        assert.equal(rowValue(row, "legacy_source_kind"), "opening_payable");
        assert.equal(rowValue(row, "legacy_source_id"), source.value.openingPayableId);
        assert.equal(rowValue(row, "legacy_source_version"), source.value.sourceVersion.toString());
        assert.equal(rowValue(row, "legacy_source_hash"), hash(source.value.sourceHash));
      }
    }
  }

  const supportRows = csv.rows.filter((row) => row.kind === "legacy_purchase_support");
  assert.equal(supportRows.length, bas.legacyPurchaseSupport.length);
  for (const [index, link] of bas.legacyPurchaseSupport.entries()) {
    const row = supportRows[index];
    assert.ok(row, `legacy support row ${index + 1} is missing`);
    assert.equal(rowValue(row, "review_id"), link.reviewId);
    assert.equal(rowValue(row, "support_review_id"), link.reviewId);
    assert.equal(rowValue(row, "support_kind"), supportNames[link.kind]);
    assert.equal(rowValue(row, "support_sequence"), String(link.sequence));
    assert.equal(rowValue(row, "support_source_id"), link.source?.sourceId ?? "");
    assert.equal(
      rowValue(row, "support_source_version"),
      link.source?.sourceVersion.toString() ?? "",
    );
    assert.equal(rowValue(row, "support_source_hash"), hash(link.source?.sourceHash));
  }

  const typedClaims = bas.purchaseSources.reduce(
    (sum, source) => sum + (source.claimedGst?.minorUnits ?? 0n),
    0n,
  );
  const legacyClaims = bas.sources.reduce(
    (sum, source) => sum + (source.gstCredit?.minorUnits ?? 0n),
    0n,
  );
  assert.equal(
    csv.rows.find((row) => row.kind === "totals" && row.metric === "reconciled_purchase_credits_1b")
      ?.value,
    exactDecimal(typedClaims + legacyClaims),
  );
  assert.equal(typedClaims + legacyClaims, bas.gstCredits1b?.minorUnits);
}

type PurchasePdfMarker =
  | { readonly kind: "legacy"; readonly documentId: string; readonly start: number }
  | {
      readonly kind: "typed";
      readonly postingId: string;
      readonly allocationSequence: number;
      readonly start: number;
    };

function pdfMinorUnits(text: string, label: string) {
  const escaped = label.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
  const match = text.match(new RegExp(`${escaped}\\s*\\$?(-?[0-9,]+\\.[0-9]{2})`, "iu"));
  assert.ok(match?.[1], `purchase PDF row is missing ${label}`);
  const [whole, fraction] = match[1].replaceAll(",", "").replace(/^-/, "").split(".");
  assert.ok(whole !== undefined && fraction !== undefined);
  const minor = BigInt(whole) * 100n + BigInt(fraction);
  return match[1].startsWith("-") ? -minor : minor;
}

/** Checks row identities and amounts from parsed PDF text, preserving table row boundaries. */
export function assertPurchasePdfRows(pages: readonly string[], bas: BasWorkpaper) {
  assert.ok(pages.length > 0, "purchase PDF must contain at least one parsed page");
  const normalized = pages.join(" ").replace(/\s+/gu, " ");
  const markers: PurchasePdfMarker[] = [];
  const markerPattern =
    /Legacy purchase row ([0-9a-f-]{36})|Source allocation ([0-9a-f-]{36}) sequence ([0-9]+)/gu;
  for (const match of normalized.matchAll(markerPattern)) {
    assert.notEqual(match.index, undefined);
    markers.push(
      match[1]
        ? { kind: "legacy", documentId: match[1], start: match.index }
        : {
            kind: "typed",
            postingId: match[2] ?? "",
            allocationSequence: Number(match[3]),
            start: match.index,
          },
    );
  }

  const expectedMarkers: Omit<PurchasePdfMarker, "start">[] = [
    ...bas.sources.map((source) => ({ kind: "legacy" as const, documentId: source.documentId })),
    ...bas.purchaseSources.map((source) => ({
      kind: "typed" as const,
      postingId: source.postingId,
      allocationSequence: source.allocationSequence,
    })),
  ];
  assert.deepEqual(
    markers.map(({ start: _start, ...marker }) => marker),
    expectedMarkers,
    "purchase PDF rows must appear exactly once in retained workpaper order",
  );

  let claimedFromPdf = 0n;
  for (const [index, source] of bas.sources.entries()) {
    const marker = markers[index];
    assert.ok(marker?.kind === "legacy");
    const block = normalized.slice(marker.start, markers[index + 1]?.start ?? normalized.length);
    for (const token of [
      source.documentId,
      source.supplierName,
      source.invoiceNumber,
      date(source.documentDate),
      amount(source.gross),
      amount(source.gstCredit),
    ])
      assert.ok(block.includes(token), `purchase PDF row ${source.documentId} is missing ${token}`);
    claimedFromPdf += pdfMinorUnits(block, "GST credit");
  }
  const typedOffset = bas.sources.length;
  for (const [index, source] of bas.purchaseSources.entries()) {
    const marker = markers[typedOffset + index];
    assert.ok(marker?.kind === "typed");
    const block = normalized.slice(
      marker.start,
      markers[typedOffset + index + 1]?.start ?? normalized.length,
    );
    for (const token of [
      source.documentId,
      `Posting ${source.postingId}`,
      `posting hash ${hash(source.postingHash)}`,
      `Original content hash ${hash(source.originalContentHash)}`,
      `reviewed snapshot hash ${hash(source.reviewedSnapshotHash)}`,
      `Payment ${source.paymentId ?? "not retained"}`,
      `payment hash ${hash(source.paymentHash) || "not retained"}`,
      `Attribution ${source.attributionId ?? "not retained"}`,
      `attribution hash ${hash(source.attributionHash) || "not retained"}`,
      `Allocation ${source.allocationSequence}`,
      `account ${source.accountId ?? "not retained"}`,
      `account version ${source.accountVersion?.toString() ?? "not retained"}`,
      `Supplier ${source.supplierName ?? "name not retained"}`,
      `Invoice ${source.invoiceNumber ?? "not retained"}`,
      `Use ${useNames[source.use]}`,
      `BAS class ${basNames[source.basClassification]}`,
      `cash flow ${cashFlowNames[source.cashFlowClassification]}`,
      `Invoice date ${date(source.invoiceDate)}`,
      `actual payment date ${date(source.actualPaymentDate)}`,
      `normal tax event ${date(source.normalTaxEventDate)}`,
      `Selected claim period ${date(source.claimPeriod?.periodStart)} to ${date(source.claimPeriod?.periodEnd)}`,
      `release posting date ${date(source.releasePostingDate)}`,
      `gross allocated $${amount(source.allocatedGross)}`,
      `supplier GST $${amount(source.supplierGst)}`,
      `eligible GST $${amount(source.eligibleGst)}`,
      `claimed GST $${amount(source.claimedGst)}`,
      `Evidence ${source.evidence?.sourceId ?? "not retained"}`,
      `version ${source.evidence?.sourceVersion.toString() ?? "not retained"}`,
      `hash ${hash(source.evidence?.sourceHash) || "not retained"}`,
      `Source revision ${source.sourceRevision}`,
      `source hash ${hash(source.sourceHash)}`,
    ])
      assert.ok(
        block.includes(token),
        `purchase PDF row ${source.postingId}/${source.allocationSequence} is missing ${token}`,
      );
    claimedFromPdf += pdfMinorUnits(block, "claimed GST");
  }
  const total = normalized.match(/GST credits 1B\s*\$?(-?[0-9,]+\.[0-9]{2})/iu)?.[1];
  assert.ok(total, "purchase PDF is missing the saved GST credits 1B total");
  const parsedTotal = pdfMinorUnits(`GST credits 1B ${total}`, "GST credits 1B");
  assert.equal(claimedFromPdf, parsedTotal, "parsed purchase PDF row amounts must reconcile to 1B");
  assert.equal(parsedTotal, bas.gstCredits1b?.minorUnits);
}

function assertPurchasePdf(pdf: string, bas: BasWorkpaper, pages: readonly string[]) {
  const normalized = pdf.replace(/\s+/gu, " ");
  const tokens = pdf.replace(/\s+/gu, "");
  assert.match(normalized, /Reconciled purchase source allocations/u);
  assert.match(normalized, /Purchase evidence, timing and legacy review outcomes/u);
  assertPurchasePdfRows(pages, bas);
  for (const review of bas.purchaseReviews) {
    for (const token of [
      review.reason,
      hash(review.sourceHash),
      amount(review.eligibleGst),
      amount(review.unclaimedGst),
    ])
      if (token) assert.ok(normalized.includes(token), `purchase PDF is missing ${token}`);
    const future = review.futureClaimObligation;
    const reviewDocumentId = review.documentId ?? future?.documentId;
    assert.ok(
      normalized.includes(`Review document ${reviewDocumentId ?? "not retained"}`),
      `purchase PDF is missing review document ${reviewDocumentId ?? "not retained"}`,
    );
    if (future) {
      for (const token of [
        future.deferralId,
        future.organisationId,
        date(future.affectedPeriod?.periodStart),
        date(future.affectedPeriod?.periodEnd),
        hash(future.deferralHash),
        future.documentId,
        amount(future.allocatedDeferredGst),
        date(future.portion?.requiredReturnDueDate),
        future.portion?.dueDateEvidence?.sourceId ?? "",
        future.portion?.dueDateEvidence?.sourceVersion.toString() ?? "",
        hash(future.portion?.dueDateEvidence?.sourceHash),
        date(future.portion?.normalEventDate),
        date(future.portion?.earliestOtherwiseEligiblePeriod?.periodStart),
        date(future.portion?.earliestOtherwiseEligiblePeriod?.periodEnd),
        date(future.portion?.ordinaryLastClaimDate),
        date(future.portion?.evidenceHeldDate),
        future.portion?.source.case ?? "",
        future.portion?.source.case === "payment" ? future.portion.source.value.paymentId : "",
        future.portion?.source.case === "payment"
          ? hash(future.portion.source.value.paymentHash)
          : "",
        future.portion?.source.case === "posting" ? future.portion.source.value.postingId : "",
        future.portion?.source.case === "posting"
          ? hash(future.portion.source.value.postingHash)
          : "",
        future.portion?.source.case ? String(future.portion.source.value.allocationSequence) : "",
        date(future.timingReview?.reviewedAsOf),
        future.timingReview?.reason ?? "",
        String(future.timingReview?.allowablePeriodConfirmed),
        String(future.timingReview?.ordinaryTimeLimitConfirmed),
        String(future.timingReview?.evidenceHeldConfirmed),
        future.actorUserId,
        timestamp(future.recordedAt),
      ])
        if (token) assert.ok(normalized.includes(token), `purchase PDF is missing ${token}`);
    }
    const decision = review.legacyReview;
    if (decision) {
      for (const token of [
        decision.id,
        decision.reason,
        dispositionNames[decision.disposition],
        date(decision.reportingBalanceDate),
        decision.actorUserId,
        hash(decision.sourceHash),
        decision.linkedPostingId ?? "",
        date(decision.affectedPeriod?.periodStart),
        date(decision.affectedPeriod?.periodEnd),
        date(decision.previouslyClaimedPeriod?.periodStart),
        date(decision.previouslyClaimedPeriod?.periodEnd),
        decision.claimEvidence?.sourceId ?? "",
        hash(decision.claimEvidence?.sourceHash),
      ])
        if (token) assert.ok(normalized.includes(token), `purchase PDF is missing ${token}`);
    }
    const legacySource = review.legacySource?.source;
    if (legacySource?.case === "document") {
      for (const token of [
        legacySource.value.documentId,
        legacySource.value.reviewedVersion.toString(),
        hash(legacySource.value.originalContentHash),
        hash(legacySource.value.reviewedSnapshotHash),
      ])
        assert.ok(normalized.includes(token), `purchase PDF is missing legacy source ${token}`);
    } else if (legacySource?.case === "openingPayable") {
      for (const token of [
        legacySource.value.openingPayableId,
        legacySource.value.sourceVersion.toString(),
        hash(legacySource.value.sourceHash),
      ])
        assert.ok(normalized.includes(token), `purchase PDF is missing opening payable ${token}`);
    }
  }
  for (const link of bas.legacyPurchaseSupport) {
    assert.ok(
      tokens.includes(link.reviewId),
      `purchase PDF is missing support review ${link.reviewId}`,
    );
    if (link.source) {
      assert.ok(
        tokens.includes(link.source.sourceId),
        `purchase PDF is missing support source ${link.source.sourceId}`,
      );
      assert.ok(
        tokens.includes(hash(link.source.sourceHash)),
        "purchase PDF is missing a support hash",
      );
      assert.ok(
        normalized.includes(link.source.sourceVersion.toString()),
        "purchase PDF is missing a support source version",
      );
    }
  }
  assert.ok(normalized.includes(amount(bas.gstCredits1b)), "purchase PDF is missing 1B");
  if (bas.legacyCandidateGst)
    assert.ok(
      normalized.includes(amount(bas.legacyCandidateGst)),
      "purchase PDF is missing the separate legacy candidate",
    );
  else
    assert.ok(
      normalized.includes("not retained"),
      "purchase PDF must label an absent legacy candidate",
    );
}

/** Parses saved artifacts independently and checks them against the exact retained workpaper. */
export async function assertPurchaseExportArtifacts(
  csvFile: string,
  pdfFile: string,
  bas: BasWorkpaper,
) {
  const [csv, pages] = await Promise.all([parseReportCsv(csvFile), readPdfPages(pdfFile)]);
  const pdf = pages.join("\f");
  assertPurchaseCsv(csv, bas);
  assertPurchasePdf(pdf, bas, pages);
}
