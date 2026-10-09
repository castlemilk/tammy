import assert from "node:assert/strict";
import test from "node:test";
import { parse } from "csv-parse/sync";
import { pdfStreams } from "../../invoices/test-support/pdf-text.ts";

const orgId = "018f0000-0000-7000-8000-000000000004";
const id = (index = 5) => `018f0000-0000-7000-8000-${String(index).padStart(12, "0")}`;
const hash = (byte: number) => new Uint8Array(32).fill(byte);
const date = { year: 2026, month: 10, day: 1 };
export const organisation = () => ({
  id: orgId,
  version: 2n,
  legalName: 'Gamma, "Systems"\nPty Ltd',
  displayName: "Gamma Systems",
  abn: "51824753556",
});
export const income = () => ({
  organisation: organisation(),
  acquiredAt: "2026-10-05T02:03:04.000Z",
  summary: {
    organisationId: orgId,
    periodStart: { ...date },
    periodEnd: { ...date, day: 31 },
    asOfDate: { ...date, day: 5 },
    businessIncome: { currencyCode: "AUD", minorUnits: 9007199254740993n },
    invoiceIncome: { currencyCode: "AUD", minorUnits: 10001n },
    paymentsReceived: { currencyCode: "AUD", minorUnits: 5500n },
    outstanding: { currencyCode: "AUD", minorUnits: 5501n },
    invoiceGst: { currencyCode: "AUD", minorUnits: 500n },
    unpostedIncome: { currencyCode: "AUD", minorUnits: 0n },
    issuedCount: 1n,
    unpostedIssuedCount: 0n,
    gstBasis: 1,
    financialRevision: 4n,
  },
});
export const bas = (count = 1) => ({
  organisation: organisation(),
  acquiredAt: "2026-10-05T02:03:04.000Z",
  workpaper: {
    id: id(),
    organisationId: orgId,
    version: 1n,
    periodStart: { ...date },
    periodEnd: { ...date, day: 31 },
    status: 1,
    createdAt: { seconds: 1791165784n, nanos: 0 },
    gstBasis: 1,
    salesG1: { currencyCode: "AUD", minorUnits: 5500n },
    gstOnSales1a: { currencyCode: "AUD", minorUnits: 500n },
    gstCredits1b: { currencyCode: "AUD", minorUnits: 1000n },
    netGstPayable: { currencyCode: "AUD", minorUnits: -500n },
    sourceFingerprint: new Uint8Array(32),
    excludedCashPurchaseCount: 0n,
    sourcesChanged: true,
    reviewWarnings: ['Review, "carefully"\nnow'],
    sources: Array.from({ length: count }, (_, index) => ({
      documentId: id(index + 100),
      supplierName: '  \t=HYPERLINK("bad")',
      invoiceNumber: "P-1",
      documentDate: { ...date },
      gross: { currencyCode: "AUD", minorUnits: 11000n },
      gstCredit: { currencyCode: "AUD", minorUnits: 1000n },
    })),
    salesSources: Array.from({ length: count }, (_, index) => ({
      invoiceId: id(index + 2000),
      invoiceNumber: "INV-001",
      customerName: "Δοκιμή",
      taxDate: { ...date },
      gross: { currencyCode: "AUD", minorUnits: 5500n },
      gst: { currencyCode: "AUD", minorUnits: 500n },
      paymentId: id(index + 4000),
      sourceRevision: 2n,
      sourceHash: new Uint8Array(32).fill(7),
    })),
  },
});
const claimPeriod = {
  periodStart: { ...date },
  periodEnd: { ...date, day: 31 },
};
const retainedRef = (sourceId: string, sourceVersion: bigint, byte: number) => ({
  sourceId,
  sourceVersion,
  sourceHash: hash(byte),
});
function reconciledBas() {
  const value = bas();
  value.workpaper.gstCredits1b.minorUnits = 1250n;
  const legacySource = {
    source: {
      case: "openingPayable",
      value: { openingPayableId: id(507), sourceVersion: 3n, sourceHash: hash(7) },
    },
  };
  const legacyReview = {
    id: id(508),
    organisationId: orgId,
    source: structuredClone(legacySource),
    affectedPeriod: structuredClone(claimPeriod),
    reportingBalanceDate: { year: 2026, month: 10, day: 31 },
    disposition: 1,
    reason: "Confirmed unpaid through the balance date.",
    sourceRevision: 2n,
    sourceHash: hash(8),
    actorUserId: id(509),
    recordedAt: { seconds: 1791165784n, nanos: 123456789 },
  };
  const portion = {
    source: {
      case: "payment",
      value: {
        postingId: id(500),
        postingHash: hash(1),
        paymentId: id(502),
        paymentHash: hash(2),
        allocationSequence: 2,
      },
    },
    normalEventDate: { year: 2026, month: 10, day: 2 },
    earliestOtherwiseEligiblePeriod: structuredClone(claimPeriod),
    requiredReturnDueDate: { year: 2027, month: 1, day: 28 },
    dueDateEvidence: retainedRef(id(513), 4n, 13),
    ordinaryLastClaimDate: { year: 2029, month: 6, day: 30 },
    evidenceId: id(505),
    evidenceHeldDate: { year: 2026, month: 10, day: 4 },
  };
  const futureClaim = {
    deferralId: id(504),
    organisationId: orgId,
    postingId: id(500),
    documentId: id(501),
    accountingVersion: 7n,
    deferralHash: hash(14),
    portion,
    affectedPeriod: structuredClone(claimPeriod),
    timingReview: {
      reviewedAsOf: { year: 2026, month: 10, day: 5 },
      allowablePeriodConfirmed: true,
      ordinaryTimeLimitConfirmed: true,
      evidenceHeldConfirmed: true,
      reason: "Evidence was held after the payment event.",
    },
    allocatedDeferredGst: { currencyCode: "AUD", minorUnits: 500n },
    reason: "Deferred pending a later claim-period review.",
    actorUserId: id(509),
    recordedAt: { seconds: 1791165784n, nanos: 123456789 },
  };
  const typedSource = {
    postingId: id(500),
    postingHash: hash(1),
    documentId: id(501),
    originalContentHash: hash(3),
    reviewedSnapshotHash: hash(4),
    paymentId: id(502),
    paymentHash: hash(2),
    attributionId: id(503),
    attributionHash: hash(5),
    allocationSequence: 2,
    accountId: id(506),
    accountVersion: 6n,
    supplierName: "Northwind Office Supplies",
    invoiceNumber: "PUR-2026-0042",
    use: 1,
    basClassification: 1,
    cashFlowClassification: 2,
    invoiceDate: { ...date },
    actualPaymentDate: { year: 2026, month: 10, day: 2 },
    normalTaxEventDate: { year: 2026, month: 10, day: 2 },
    releasePostingDate: { year: 2026, month: 10, day: 3 },
    claimPeriod: structuredClone(claimPeriod),
    allocatedGross: { currencyCode: "AUD", minorUnits: 5500n },
    supplierGst: { currencyCode: "AUD", minorUnits: 500n },
    eligibleGst: { currencyCode: "AUD", minorUnits: 500n },
    claimedGst: { currencyCode: "AUD", minorUnits: 250n },
    evidence: retainedRef(id(505), 2n, 6),
    sourceRevision: 9n,
    sourceHash: hash(9),
  };
  Object.assign(value.workpaper, {
    purchaseSourceFormat: 1,
    legacyCandidateGst: { currencyCode: "AUD", minorUnits: 321n },
    purchaseSources: [typedSource],
    purchaseReviews: [
      {
        legacySource,
        status: 4,
        affectedPeriod: structuredClone(claimPeriod),
        eligibleGst: { currencyCode: "AUD", minorUnits: 600n },
        unclaimedGst: { currencyCode: "AUD", minorUnits: 600n },
        legacyReview,
        sourceRevision: 2n,
        sourceHash: hash(10),
        reason: "Opening payable has no linked source document.",
        kind: 11,
      },
      {
        postingId: id(500),
        paymentId: id(502),
        deferralId: id(504),
        status: 6,
        affectedPeriod: structuredClone(claimPeriod),
        invoiceDate: { ...date },
        actualPaymentDate: { year: 2026, month: 10, day: 2 },
        eligibleGst: { currencyCode: "AUD", minorUnits: 500n },
        unclaimedGst: { currencyCode: "AUD", minorUnits: 250n },
        futureClaimObligation: futureClaim,
        evidence: retainedRef(id(505), 2n, 6),
        sourceRevision: 10n,
        sourceHash: hash(11),
        reason: "The original deferral remains partly outstanding.",
        kind: 8,
      },
    ],
    legacyPurchaseSupport: [
      {
        reviewId: id(508),
        kind: 1,
        sequence: 1,
        source: retainedRef(id(511), 2n, 12),
      },
      {
        reviewId: id(508),
        kind: 2,
        sequence: 1,
        source: retainedRef(id(512), 5n, 13),
      },
    ],
  });
  return value;
}
function first<T>(values: readonly T[]): T {
  const value = values[0];
  assert.ok(value);
  return value;
}
function csv(bytes: Uint8Array): string[][] {
  return parse(new TextDecoder().decode(bytes), { relax_column_count: false });
}
async function pdfText(bytes: Uint8Array) {
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const loading = getDocument({
    data: new Uint8Array(bytes),
    useSystemFonts: false,
    useWorkerFetch: false,
    verbosity: 0,
  });
  const document = await loading.promise;
  try {
    const pages: string[] = [];
    for (let number = 1; number <= document.numPages; number++) {
      const page = await document.getPage(number);
      const content = await page.getTextContent();
      pages.push(content.items.map((item) => ("str" in item ? item.str : "")).join(" "));
      page.cleanup();
    }
    return { text: pages.join("\n"), pages, pageCount: document.numPages };
  } finally {
    await loading.destroy();
  }
}
async function renderer() {
  return import("./reports.ts");
}

test("income summary CSV preserves exact integer cents, dates, counts and authoritative context", async () => {
  const { renderIncomeCsv } = await renderer();
  const rows = csv(renderIncomeCsv(income()));
  assert.deepEqual(rows[0], ["metric", "value", "currency", "context"]);
  assert.deepEqual(
    rows.find((row) => row[0] === "business_income"),
    ["business_income", "90071992547409.93", "AUD", "Revenue excluding GST"],
  );
  assert.equal(rows.find((row) => row[0] === "financial_revision")?.[1], "4");
  assert.equal(rows.find((row) => row[0] === "as_of_date")?.[1], "2026-10-05");
  assert.equal(
    rows.find((row) => row[0] === "organisation_legal_name")?.[1],
    income().organisation.legalName,
  );
  assert.ok(rows.every((row) => row.length === 4));
});
test("BAS CSV is rectangular, retains every source, negative GST, formulas and warnings", async () => {
  const { renderBasCsv } = await renderer();
  const rows = csv(renderBasCsv(bas(1000)));
  const header = rows[0];
  assert.ok(header);
  assert.ok(rows.every((row) => row.length === header.length));
  assert.equal(rows.filter((row) => row[0] === "sales").length, 1000);
  assert.equal(rows.filter((row) => row[0] === "purchases").length, 1000);
  assert.equal(rows.find((row) => row[1] === "net_gst_payable")?.[2], "-5.00");
  assert.equal(
    rows.find((row) => row[0] === "purchases")?.[header.indexOf("party")],
    '\'  \t=HYPERLINK("bad")',
  );
  assert.equal(
    rows.find((row) => row[0] === "sales")?.[header.indexOf("source_hash")],
    "07".repeat(32),
  );
  assert.ok(rows.some((row) => row[1] === "sources_changed" && row[2] === "true"));
  assert.ok(rows.some((row) => row[0] === "warnings" && row[2] === 'Review, "carefully"\nnow'));
});
test("empty sources and zero totals remain explicit, unsupported PDF text keeps Unicode CSV available", async () => {
  const { renderBasCsv, renderBasPdf } = await renderer();
  const value = bas(0);
  value.organisation.legalName = "中文 😀";
  value.workpaper.netGstPayable.minorUnits = 0n;
  assert.equal(csv(renderBasCsv(value)).find((row) => row[1] === "net_gst_payable")?.[2], "0.00");
  assert.ok(csv(renderBasCsv(value)).some((row) => row[2] === "中文 😀"));
  await assert.rejects(renderBasPdf(value), /REPORT_PDF_UNSUPPORTED_TEXT/);
});
test("malformed dates, currencies, identity, money and source overflow fail visibly", async () => {
  const { renderIncomeCsv, renderBasCsv } = await renderer();
  for (const change of [
    (v: ReturnType<typeof income>) => {
      v.summary.periodStart.day = 32;
    },
    (v: ReturnType<typeof income>) => {
      v.summary.businessIncome.currencyCode = "USD";
    },
    (v: ReturnType<typeof income>) => {
      v.summary.organisationId = id(77);
    },
    (v: ReturnType<typeof income>) => {
      v.summary.businessIncome.minorUnits = 9223372036854775808n;
    },
  ]) {
    const value = income();
    change(value);
    assert.throws(() => renderIncomeCsv(value), /INVALID_REPORT_SNAPSHOT/);
  }
  assert.throws(() => renderBasCsv(bas(1001)), /REPORT_SOURCE_LIMIT/);
});
test("both actual PDFs retain report totals and BAS source headers repeat across pages", async () => {
  const { renderIncomePdf, renderBasPdf } = await renderer();
  const i = income();
  i.organisation.legalName = "Gamma Systems Pty Ltd";
  const incomeText = pdfStreams(await renderIncomePdf(i));
  assert.match(incomeText, /90071992547409\.93/);
  assert.match(incomeText, /Financial revision 4/);
  const b = bas(35);
  b.organisation.legalName = "Gamma Systems Pty Ltd";
  b.workpaper.sources.forEach((source) => {
    source.supplierName = "Supplier";
  });
  const bytes = await renderBasPdf(b),
    text = pdfStreams(bytes);
  assert.match(text, /Draft.*not lodged/);
  assert.match(text, /-5\.00/);
  assert.ok((text.match(/Tax date/g) ?? []).length >= 2);
  assert.ok((text.match(/Document/g) ?? []).length >= 2);
  assert.match(text, /INV-001/);
});

test("CSV neutralises every formula prefix through whitespace and controls while keeping numeric negatives exact", async () => {
  const { renderBasCsv, renderIncomeCsv } = await renderer();
  for (const prefix of ["", " ", "\t", "\r\n", "\u0001", "\u200b"]) {
    for (const operator of ["=", "+", "-", "@"]) {
      const value = bas();
      const source = value.workpaper.sources[0];
      assert.ok(source);
      source.supplierName = `${prefix}${operator}SUM(1,2)`;
      const rows = csv(renderBasCsv(value)),
        header = rows[0];
      assert.ok(header);
      assert.equal(
        rows.find((row) => row[0] === "purchases")?.[header.indexOf("party")],
        `'${source.supplierName}`,
      );
    }
  }
  const value = income();
  value.summary.businessIncome.minorUnits = -1n;
  assert.equal(
    csv(renderIncomeCsv(value)).find((row) => row[0] === "business_income")?.[1],
    "-0.01",
  );
});
test("reconciled BAS CSV exports exact purchase provenance, allocation, review and 1B reconciliation", async () => {
  const { renderBasCsv } = await renderer();
  const value = reconciledBas();
  const reviews = (value.workpaper as unknown as { purchaseReviews: Record<string, unknown>[] })
    .purchaseReviews;
  const deferredReview = reviews.find((review) => review.futureClaimObligation !== undefined);
  assert.ok(deferredReview);
  deferredReview.documentId = id(501);
  const rows = csv(renderBasCsv(value));
  const header = rows[0];
  assert.ok(header);
  assert.ok(rows.every((row) => row.length === header.length));
  const field = (row: string[] | undefined, name: string) => {
    assert.ok(row, `missing ${name} row`);
    const index = header.indexOf(name);
    assert.notEqual(index, -1, `missing ${name} column`);
    return row[index];
  };
  const source = rows.find((row) => row[0] === "purchase_sources");
  assert.equal(field(source, "document_id"), id(501));
  assert.equal(field(source, "posting_id"), id(500));
  assert.equal(field(source, "posting_hash"), "01".repeat(32));
  assert.equal(field(source, "original_content_hash"), "03".repeat(32));
  assert.equal(field(source, "reviewed_snapshot_hash"), "04".repeat(32));
  assert.equal(field(source, "payment_id"), id(502));
  assert.equal(field(source, "payment_hash"), "02".repeat(32));
  assert.equal(field(source, "attribution_id"), id(503));
  assert.equal(field(source, "attribution_hash"), "05".repeat(32));
  assert.equal(field(source, "source_revision"), "9");
  assert.equal(field(source, "source_hash"), "09".repeat(32));
  assert.equal(field(source, "allocation_sequence"), "2");
  assert.equal(field(source, "account_id"), id(506));
  assert.equal(field(source, "account_version"), "6");
  assert.equal(field(source, "supplier_name"), "Northwind Office Supplies");
  assert.equal(field(source, "invoice_number"), "PUR-2026-0042");
  assert.equal(field(source, "use"), "business");
  assert.equal(field(source, "bas_classification"), "capital");
  assert.equal(field(source, "cash_flow_classification"), "investing");
  assert.equal(field(source, "invoice_date"), "2026-10-01");
  assert.equal(field(source, "actual_payment_date"), "2026-10-02");
  assert.equal(field(source, "normal_tax_event_date"), "2026-10-02");
  assert.equal(field(source, "claim_period_start"), "2026-10-01");
  assert.equal(field(source, "claim_period_end"), "2026-10-31");
  assert.equal(field(source, "release_posting_date"), "2026-10-03");
  assert.equal(field(source, "gross"), "55.00");
  assert.equal(field(source, "supplier_gst"), "5.00");
  assert.equal(field(source, "eligible_gst"), "5.00");
  assert.equal(field(source, "claimed_gst"), "2.50");
  assert.equal(field(source, "evidence_id"), id(505));
  assert.equal(field(source, "evidence_version"), "2");
  assert.equal(field(source, "evidence_hash"), "06".repeat(32));

  const legacyDirect = rows.find((row) => row[0] === "purchases");
  assert.equal(field(legacyDirect, "gst"), "10.00");
  const reconciliation = rows.find(
    (row) => row[0] === "totals" && row[1] === "reconciled_purchase_credits_1b",
  );
  assert.equal(field(reconciliation, "value"), "12.50");
  const candidate = rows.find((row) => row[0] === "metadata" && row[1] === "legacy_candidate_gst");
  assert.equal(field(candidate, "value"), "3.21");

  const deferred = rows.find((row) => row[0] === "purchase_reviews" && row[1] === "deferred");
  assert.equal(field(deferred, "review_id"), "");
  assert.equal(field(deferred, "posting_id"), id(500));
  assert.equal(field(deferred, "payment_id"), id(502));
  assert.equal(field(deferred, "deferral_id"), id(504));
  assert.equal(field(deferred, "review_status"), "deferred");
  assert.equal(field(deferred, "review_kind"), "deferred_future_claim");
  assert.equal(field(deferred, "document_id"), id(501));
  assert.equal(field(deferred, "invoice_date"), "2026-10-01");
  assert.equal(field(deferred, "actual_payment_date"), "2026-10-02");
  assert.equal(field(deferred, "review_affected_period_start"), "2026-10-01");
  assert.equal(field(deferred, "review_affected_period_end"), "2026-10-31");
  assert.equal(
    field(deferred, "review_reason"),
    "The original deferral remains partly outstanding.",
  );
  assert.equal(field(deferred, "eligible_gst"), "5.00");
  assert.equal(field(deferred, "current_unclaimed_gst"), "2.50");
  assert.equal(field(deferred, "original_deferred_gst"), "5.00");
  assert.equal(field(deferred, "deferral_organisation_id"), orgId);
  assert.equal(field(deferred, "deferral_affected_period_start"), "2026-10-01");
  assert.equal(field(deferred, "deferral_affected_period_end"), "2026-10-31");
  assert.equal(field(deferred, "normal_event_date"), "2026-10-02");
  assert.equal(field(deferred, "reviewed_as_of"), "2026-10-05");
  assert.equal(field(deferred, "required_return_due_date"), "2027-01-28");
  assert.equal(field(deferred, "due_date_evidence_id"), id(513));
  assert.equal(field(deferred, "due_date_evidence_version"), "4");
  assert.equal(field(deferred, "due_date_evidence_hash"), "0d".repeat(32));
  assert.equal(field(deferred, "ordinary_last_claim_date"), "2029-06-30");
  assert.equal(field(deferred, "evidence_held_date"), "2026-10-04");
  assert.equal(field(deferred, "timing_reason"), "Evidence was held after the payment event.");

  const opening = rows.find(
    (row) => row[0] === "purchase_reviews" && row[1] === "unresolved_legacy",
  );
  assert.equal(field(opening, "legacy_source_kind"), "opening_payable");
  assert.equal(field(opening, "legacy_source_id"), id(507));
  assert.equal(field(opening, "legacy_source_hash"), "07".repeat(32));
  assert.equal(field(opening, "legacy_review_id"), id(508));
  assert.equal(field(opening, "legacy_disposition"), "confirmed_unpaid_through_balance");
  assert.equal(field(opening, "review_reason"), "Opening payable has no linked source document.");
  assert.equal(field(opening, "reviewed_at"), "2026-10-05T02:03:04.123456789Z");
  const supports = rows.filter((row) => row[0] === "legacy_purchase_support");
  assert.equal(supports.length, 2);
  assert.deepEqual(
    supports.map((row) => [field(row, "support_kind"), field(row, "support_sequence")]),
    [
      ["payment", "1"],
      ["evidence", "1"],
    ],
  );
  assert.ok(rows.some((row) => row[0] === "warnings" && row[2] === 'Review, "carefully"\nnow'));
});
test("reconciled BAS PDF independently exposes retained purchase hashes, review and opening source", async () => {
  const { renderBasPdf } = await renderer();
  const value = reconciledBas();
  const reviews = (value.workpaper as unknown as { purchaseReviews: Record<string, unknown>[] })
    .purchaseReviews;
  const deferredReview = reviews.find((review) => review.futureClaimObligation !== undefined);
  assert.ok(deferredReview);
  deferredReview.documentId = id(501);
  const { text, pageCount } = await pdfText(await renderBasPdf(value));
  assert.ok(pageCount > 0);
  for (const value of [
    "Reconciled purchase source allocations",
    id(500),
    id(501),
    id(502),
    id(503),
    id(506),
    "PUR-2026-0042",
    "Northwind Office Supplies",
    "2026-10-01",
    "2026-10-02",
    "2026-10-31",
    "55.00",
    "5.00",
    "2.50",
    "01".repeat(32),
    "04".repeat(32),
    "09".repeat(32),
    "deferred future claim",
    `Review document ${id(501)}`,
    orgId,
    "Original deferral",
    "Evidence was held after the payment event.",
    "2027-01-28",
    "2029-06-30",
    "Confirmed unpaid through the balance date.",
    "opening_payable",
    id(507),
    id(508),
    "3.21",
    "12.50",
  ])
    assert.ok(text.includes(value), `PDF text is missing ${value}`);
});
test("reconciled BAS preserves optional retained metadata as not retained without a live lookup", async () => {
  const { renderBasCsv, renderBasPdf } = await renderer();
  const value = reconciledBas();
  const source = (value.workpaper as unknown as { purchaseSources: Record<string, unknown>[] })
    .purchaseSources[0];
  assert.ok(source);
  for (const key of [
    "accountId",
    "accountVersion",
    "supplierName",
    "invoiceNumber",
    "releasePostingDate",
  ])
    delete source[key];

  const rows = csv(renderBasCsv(value));
  const header = rows[0];
  assert.ok(header);
  const row = rows.find((candidate) => candidate[0] === "purchase_sources");
  assert.ok(row);
  for (const [fieldName, expected] of [
    ["account_id", "not retained"],
    ["account_version", "not retained"],
    ["supplier_name", "not retained"],
    ["invoice_number", "not retained"],
    ["release_posting_date", "not retained"],
  ] as const)
    assert.equal(row[header.indexOf(fieldName)] ?? "", expected);

  const { text } = await pdfText(await renderBasPdf(value));
  for (const value of [
    "account not retained",
    "account version not retained",
    "Invoice not retained",
    "Supplier name not retained",
    "release posting date not retained",
  ])
    assert.ok(text.includes(value), `PDF text is missing ${value}`);
});
test("reconciled BAS permits supplier and invoice metadata to be independently absent", async () => {
  const { renderBasCsv, renderBasPdf } = await renderer();
  for (const omitted of ["supplierName", "invoiceNumber"] as const) {
    const value = reconciledBas();
    const source = (value.workpaper as unknown as { purchaseSources: Record<string, unknown>[] })
      .purchaseSources[0];
    assert.ok(source);
    delete source[omitted];

    const rows = csv(renderBasCsv(value));
    const header = rows[0];
    assert.ok(header);
    const row = rows.find((candidate) => candidate[0] === "purchase_sources");
    assert.ok(row);
    assert.equal(
      row[header.indexOf("supplier_name")],
      omitted === "supplierName" ? "not retained" : "Northwind Office Supplies",
    );
    assert.equal(
      row[header.indexOf("invoice_number")],
      omitted === "invoiceNumber" ? "not retained" : "PUR-2026-0042",
    );

    const { text } = await pdfText(await renderBasPdf(value));
    assert.ok(
      text.includes(
        omitted === "supplierName" ? "Supplier name not retained" : "Northwind Office Supplies",
      ),
    );
    assert.ok(
      text.includes(omitted === "invoiceNumber" ? "Invoice not retained" : "PUR-2026-0042"),
    );
  }
});
test("reconciled BAS preserves both hashes for a reviewed legacy purchase document", async () => {
  const { renderBasCsv, renderBasPdf } = await renderer();
  const value = reconciledBas();
  const reviews = (value.workpaper as unknown as { purchaseReviews: Record<string, unknown>[] })
    .purchaseReviews;
  const review = reviews.find((candidate) => candidate.legacyReview !== undefined);
  assert.ok(review);
  const source = {
    source: {
      case: "document",
      value: {
        documentId: id(777),
        reviewedVersion: 5n,
        originalContentHash: hash(15),
        reviewedSnapshotHash: hash(16),
      },
    },
  };
  review.legacySource = structuredClone(source);
  (review.legacyReview as Record<string, unknown>).source = structuredClone(source);
  review.kind = 9;
  review.reason = "Old legacy document reviewed against retained source hashes.";

  const rows = csv(renderBasCsv(value));
  const header = rows[0];
  assert.ok(header);
  const row = rows.find(
    (candidate) => candidate[0] === "purchase_reviews" && candidate[1] === "unresolved_legacy",
  );
  assert.ok(row);
  assert.equal(row[header.indexOf("legacy_source_kind")], "document");
  assert.equal(row[header.indexOf("legacy_source_id")], id(777));
  assert.equal(row[header.indexOf("legacy_source_version")], "5");
  assert.equal(row[header.indexOf("legacy_source_original_content_hash")], "0f".repeat(32));
  assert.equal(row[header.indexOf("legacy_source_reviewed_snapshot_hash")], "10".repeat(32));

  const { text } = await pdfText(await renderBasPdf(value));
  assert.ok(text.includes("Legacy document original content hash"));
  assert.ok(text.includes("0f".repeat(32)));
  assert.ok(text.includes("10".repeat(32)));
});
test("reconciled BAS preserves a legacy decision's narrower reviewed period without treating it as current-period resolution", async () => {
  const { renderBasCsv, renderBasPdf } = await import("./reports.ts");
  const value = reconciledBas();
  const reviews = (value.workpaper as unknown as { purchaseReviews: Record<string, unknown>[] })
    .purchaseReviews;
  const review = reviews.find((candidate) => candidate.legacyReview !== undefined);
  assert.ok(review);
  const decision = review.legacyReview as {
    affectedPeriod: typeof claimPeriod;
    reportingBalanceDate: typeof date;
    disposition: number;
  };
  decision.affectedPeriod.periodEnd.day = 9;
  decision.reportingBalanceDate.day = 9;
  decision.disposition = 4;
  review.status = 4;
  const rows = parse(Buffer.from(renderBasCsv(value)), { columns: true }) as Record<
    string,
    string
  >[];
  const row = rows.find((candidate) => candidate.legacy_review_id === id(508));
  assert.ok(row);
  assert.equal(row.review_affected_period_end, "2026-10-31");
  assert.equal(row.legacy_affected_period_end, "2026-10-09");
  assert.equal(row.reporting_balance_date, "2026-10-09");
  assert.equal(row.review_status, "unresolved_legacy");
  assert.equal(row.legacy_disposition, "noncreditable");
  assert.equal(row.current_unclaimed_gst, "6.00");
  assert.equal(rows.find((candidate) => candidate.metric === "gst_credits_1b")?.value, "12.50");
  const { text } = await pdfText(await renderBasPdf(value));
  assert.ok(text.includes("2026-10-09"));
});

test("reconciled BAS PDF retains unresolved legacy source identity and document hashes without a decision", async () => {
  const { renderBasPdf } = await renderer();
  const value = reconciledBas();
  const reviews = (value.workpaper as unknown as { purchaseReviews: Record<string, unknown>[] })
    .purchaseReviews;
  const review = reviews.find((candidate) => candidate.legacyReview !== undefined);
  assert.ok(review);
  review.legacySource = {
    source: {
      case: "document",
      value: {
        documentId: id(777),
        reviewedVersion: 5n,
        originalContentHash: hash(17),
        reviewedSnapshotHash: hash(18),
      },
    },
  };
  delete review.legacyReview;
  Object.assign(value.workpaper, { legacyPurchaseSupport: [] });
  review.kind = 9;
  review.reason = "Unresolved retained legacy document source.";

  const { text } = await pdfText(await renderBasPdf(value));
  for (const expected of [
    "Legacy source document",
    id(777),
    "version 5",
    "original content hash",
    "11".repeat(32),
    "12".repeat(32),
  ])
    assert.ok(text.includes(expected), `PDF text is missing unresolved source detail ${expected}`);
});
test("reconciled BAS neutralises formula text and preserves Unicode plus negative cents", async () => {
  const { renderBasCsv, renderBasPdf } = await renderer();
  const value = reconciledBas();
  const source = (value.workpaper as unknown as { purchaseSources: Record<string, unknown>[] })
    .purchaseSources[0];
  assert.ok(source);
  const supplier = "\u200b=SUM(1,2)";
  source.supplierName = supplier;
  source.invoiceNumber = "+INV-Élodie-Δοκιμή-Привет";
  value.workpaper.netGstPayable.minorUnits = -1n;

  const rows = csv(renderBasCsv(value));
  const header = rows[0];
  assert.ok(header);
  const purchase = rows.find((candidate) => candidate[0] === "purchase_sources");
  assert.ok(purchase);
  assert.equal(purchase[header.indexOf("supplier_name")], `'${supplier}`);
  assert.equal(purchase[header.indexOf("invoice_number")], "'+INV-Élodie-Δοκιμή-Привет");
  assert.equal(
    rows.find((candidate) => candidate[0] === "totals" && candidate[1] === "net_gst_payable")?.[2],
    "-0.01",
  );

  source.supplierName = "Northwind Élodie Δοκιμή Привет";
  source.invoiceNumber = "INV-Élodie-Δοκιμή-Привет";
  const { text } = await pdfText(await renderBasPdf(value));
  assert.ok(text.includes("Northwind Élodie Δοκιμή Привет"));
  assert.ok(text.includes("INV-Élodie-Δοκιμή-Привет"));
  assert.ok(text.includes("-0.01"));
});
test("reconciled BAS keeps original deferral scope and evidence distinct from the current review", async () => {
  const { renderBasCsv, renderBasPdf } = await renderer();
  const value = reconciledBas();
  const reviews = (value.workpaper as unknown as { purchaseReviews: Record<string, unknown>[] })
    .purchaseReviews;
  const review = reviews.find((candidate) => candidate.futureClaimObligation !== undefined);
  assert.ok(review);
  const future = review.futureClaimObligation as { affectedPeriod: typeof claimPeriod };
  future.affectedPeriod.periodEnd.day = 9;
  review.evidence = retainedRef(id(514), 6n, 14);
  review.status = 2;
  const rows = parse(Buffer.from(renderBasCsv(value)), { columns: true }) as Record<
    string,
    string
  >[];
  const row = rows.find((candidate) => candidate.deferral_id === id(504));
  assert.ok(row);
  assert.equal(row.review_affected_period_end, "2026-10-31");
  assert.equal(row.deferral_affected_period_end, "2026-10-09");
  assert.equal(row.evidence_id, id(514));
  assert.equal(row.deferral_evidence_id, id(505));
  assert.equal(row.review_status, "needs_evidence");
  assert.equal(row.current_unclaimed_gst, "2.50");
  const { text } = await pdfText(await renderBasPdf(value));
  assert.ok(text.includes("2026-10-09"));
  assert.ok(text.includes(id(514)));
  assert.ok(text.includes(id(505)));
});

test("reconciled BAS retains a future-claim obligation when its current status is clear", async () => {
  const { renderBasCsv } = await renderer();
  const value = reconciledBas();
  const reviews = (
    value.workpaper as unknown as {
      purchaseReviews: { status: number; kind: number }[];
    }
  ).purchaseReviews;
  const futureClaim = reviews.find((review) => review.kind === 8);
  assert.ok(futureClaim);
  futureClaim.status = 1;

  const rows = csv(renderBasCsv(value));
  const header = rows[0];
  assert.ok(header);
  const deferred = rows.find((row) => row[0] === "purchase_reviews" && row[1] === "clear");
  assert.ok(deferred);
  assert.equal(deferred[header.indexOf("review_kind")], "deferred_future_claim");
  assert.equal(deferred[header.indexOf("original_deferred_gst")], "5.00");
  assert.equal(deferred[header.indexOf("current_unclaimed_gst")], "2.50");
});
test("reconciled BAS requires complete bounded identities and reconciles only claimed source GST", async () => {
  const { renderBasCsv } = await renderer();
  for (const change of [
    (value: ReturnType<typeof reconciledBas>) => {
      const source = (value.workpaper as unknown as { purchaseSources: Record<string, unknown>[] })
        .purchaseSources[0];
      assert.ok(source);
      source.postingHash = new Uint8Array(31);
    },
    (value: ReturnType<typeof reconciledBas>) => {
      const source = (value.workpaper as unknown as { purchaseSources: Record<string, unknown>[] })
        .purchaseSources[0];
      assert.ok(source);
      source.use = 99;
    },
    (value: ReturnType<typeof reconciledBas>) => {
      const source = (value.workpaper as unknown as { purchaseSources: Record<string, unknown>[] })
        .purchaseSources[0];
      assert.ok(source);
      source.accountVersion = 0n;
    },
    (value: ReturnType<typeof reconciledBas>) => {
      const source = (value.workpaper as unknown as { purchaseSources: Record<string, unknown>[] })
        .purchaseSources[0];
      assert.ok(source);
      source.claimPeriod = { periodStart: { ...date }, periodEnd: { ...date, day: 30 } };
    },
    (value: ReturnType<typeof reconciledBas>) => {
      value.workpaper.gstCredits1b.minorUnits = 1249n;
    },
    (value: ReturnType<typeof reconciledBas>) => {
      const support = (
        value.workpaper as unknown as { legacyPurchaseSupport: Record<string, unknown>[] }
      ).legacyPurchaseSupport[0];
      assert.ok(support);
      support.reviewId = id(900);
    },
    (value: ReturnType<typeof reconciledBas>) => {
      const reviews = (value.workpaper as unknown as { purchaseReviews: Record<string, unknown>[] })
        .purchaseReviews;
      const deferred = reviews.find((review) => review.futureClaimObligation !== undefined);
      assert.ok(deferred);
      delete deferred.futureClaimObligation;
    },
    (value: ReturnType<typeof reconciledBas>) => {
      const reviews = (value.workpaper as unknown as { purchaseReviews: Record<string, unknown>[] })
        .purchaseReviews;
      const deferred = reviews.find((review) => review.futureClaimObligation !== undefined);
      assert.ok(deferred);
      delete deferred.deferralId;
      delete deferred.futureClaimObligation;
    },
    (value: ReturnType<typeof reconciledBas>) => {
      const reviews = (value.workpaper as unknown as { purchaseReviews: Record<string, unknown>[] })
        .purchaseReviews;
      const deferred = reviews.find((review) => review.futureClaimObligation !== undefined);
      assert.ok(deferred);
      deferred.documentId = "invalid-document-id";
    },
    (value: ReturnType<typeof reconciledBas>) => {
      const reviews = (value.workpaper as unknown as { purchaseReviews: Record<string, unknown>[] })
        .purchaseReviews;
      const deferred = reviews.find((review) => review.futureClaimObligation !== undefined);
      assert.ok(deferred);
      deferred.documentId = id(999);
    },
  ]) {
    const value = reconciledBas();
    change(value);
    assert.throws(() => renderBasCsv(value), /INVALID_REPORT_SNAPSHOT/);
  }
  const tooMany = reconciledBas();
  Object.assign(tooMany.workpaper, {
    purchaseSources: Array.from({ length: 1001 }, (_, index) => ({
      ...(reconciledBas().workpaper as unknown as { purchaseSources: object[] }).purchaseSources[0],
      allocationSequence: index + 1,
    })),
  });
  assert.throws(() => renderBasCsv(tooMany), /REPORT_SOURCE_LIMIT/);
  for (const field of ["purchaseReviews", "legacyPurchaseSupport"] as const) {
    const tooManyRelatedRows = reconciledBas();
    Object.assign(tooManyRelatedRows.workpaper, {
      [field]: Array.from({ length: 1001 }, () => ({})),
    });
    assert.throws(() => renderBasCsv(tooManyRelatedRows), /REPORT_SOURCE_LIMIT/);
  }
});

test("reconciled BAS independently parses all 1000 supported purchase source rows", async () => {
  const { renderBasCsv, renderBasPdf } = await renderer();
  const value = reconciledBas();
  const source = (value.workpaper as unknown as { purchaseSources: Record<string, unknown>[] })
    .purchaseSources[0];
  assert.ok(source);
  const postingId = id(500);
  const sources = Array.from({ length: 1000 }, (_, index) => ({
    ...structuredClone(source),
    postingId,
    allocationSequence: index + 1,
  }));
  Object.assign(value.workpaper, {
    purchaseSources: sources,
    gstCredits1b: { currencyCode: "AUD", minorUnits: 251000n },
  });

  const csvBytes = renderBasCsv(value);
  const rows = csv(csvBytes);
  const header = rows[0];
  assert.ok(header);
  assert.equal(rows.filter((row) => row[0] === "purchase_sources").length, 1000);
  assert.ok(rows.every((row) => row.length === header.length));
  const lastSource = rows.find(
    (row) => row[0] === "purchase_sources" && row[header.indexOf("allocation_sequence")] === "1000",
  );
  assert.ok(lastSource);
  assert.equal(lastSource[header.indexOf("claimed_gst")], "2.50");
  assert.equal(
    rows.find((row) => row[0] === "totals" && row[1] === "reconciled_purchase_credits_1b")?.[2],
    "2510.00",
  );
  assert.ok(csvBytes.length < 16 * 1024 * 1024);

  const pdfBytes = await renderBasPdf(value);
  assert.ok(pdfBytes.length < 16 * 1024 * 1024);
  const { text, pageCount } = await pdfText(pdfBytes);
  assert.ok(pageCount > 0);
  const allocationMarkers = [
    ...text.matchAll(/Source allocation\s+([0-9a-f-]{36})\s+sequence\s+([0-9]+)/gu),
  ].map((match) => `${match[1]}/${match[2]}`);
  assert.deepEqual(
    allocationMarkers,
    sources.map((source) => `${postingId}/${source.allocationSequence}`),
    "PDF must retain every allocation marker exactly once and in saved order",
  );
  const purchaseReviews = (
    value.workpaper as unknown as {
      purchaseReviews: { reason: string; sourceRevision: bigint; sourceHash: Uint8Array }[];
    }
  ).purchaseReviews;
  assert.equal((text.match(/Review document /gu) ?? []).length, purchaseReviews.length);
  for (const review of purchaseReviews) {
    assert.ok(text.includes(`Review reason: ${review.reason}`));
    assert.ok(
      text.includes(
        `Source revision ${review.sourceRevision} · source hash ${Buffer.from(review.sourceHash).toString("hex")}`,
      ),
    );
  }
  const legacyPurchaseSupport = (
    value.workpaper as unknown as {
      legacyPurchaseSupport: {
        kind: number;
        sequence: number;
        source?: { sourceId: string; sourceVersion: bigint; sourceHash: Uint8Array };
      }[];
    }
  ).legacyPurchaseSupport;
  assert.equal(
    (text.match(/Supporting (?:payment|evidence) /gu) ?? []).length,
    legacyPurchaseSupport.length,
  );
  const normalized = text.replace(/\s+/gu, " ");
  const tokens = normalized.replaceAll(" ", "");
  for (const link of legacyPurchaseSupport) {
    const kind = link.kind === 1 ? "payment" : "evidence";
    const source = link.source;
    assert.ok(source);
    assert.ok(normalized.includes(`Supporting ${kind} ${link.sequence}:`));
    assert.ok(tokens.includes(source.sourceId));
    assert.ok(normalized.includes(`version ${source.sourceVersion}`));
    assert.ok(tokens.includes(Buffer.from(source.sourceHash).toString("hex")));
  }
  assert.ok(text.includes("2510.00"));
});
test("purchase PDF rows keep each allocation identity, provenance and exact amounts together", async () => {
  const { renderBasPdf } = await renderer();
  const value = reconciledBas();
  const source = (value.workpaper as unknown as { purchaseSources: Record<string, unknown>[] })
    .purchaseSources[0];
  assert.ok(source);
  source.claimedGst = { currencyCode: "AUD", minorUnits: 100n };
  const second = structuredClone(source);
  Object.assign(second, {
    postingId: id(520),
    postingHash: hash(20),
    documentId: id(521),
    originalContentHash: hash(21),
    reviewedSnapshotHash: hash(22),
    paymentId: id(522),
    paymentHash: hash(23),
    attributionId: id(523),
    attributionHash: hash(24),
    allocationSequence: 1,
    accountId: id(526),
    evidence: retainedRef(id(525), 3n, 25),
    sourceHash: hash(26),
    claimedGst: { currencyCode: "AUD", minorUnits: 150n },
  });
  const sources = [source, second];
  Object.assign(value.workpaper, { purchaseSources: sources });
  const { text, pageCount } = await pdfText(await renderBasPdf(value));
  const normalized = text.replace(/\s+/gu, " ");
  assert.ok(pageCount > 0);
  for (const expected of [
    `Source allocation ${id(500)} sequence 2`,
    `Document ${id(501)}`,
    `Posting ${id(500)}`,
    `Payment ${id(502)}`,
    `Attribution ${id(503)}`,
    `source hash ${"09".repeat(32)}`,
    `Evidence ${id(505)} · version 2`,
    "gross allocated $55.00 · supplier GST $5.00 · eligible GST $5.00 · claimed GST $1.00",
    `Source allocation ${id(520)} sequence 1`,
    `Document ${id(521)}`,
    `Posting ${id(520)}`,
    `Payment ${id(522)}`,
    `Attribution ${id(523)}`,
    `source hash ${"1a".repeat(32)}`,
    `Evidence ${id(525)} · version 3`,
    "gross allocated $55.00 · supplier GST $5.00 · eligible GST $5.00 · claimed GST $1.50",
  ])
    assert.ok(normalized.includes(expected), `PDF row text is missing ${expected}`);
  assert.equal((normalized.match(/Source allocation /gu) ?? []).length, sources.length);
});

test("byte bounds reject oversized output without truncation and accept the exact 16 MiB limit", async () => {
  const { boundedReportBytes, MAX_REPORT_BYTES } = await renderer();
  assert.equal(boundedReportBytes(new Uint8Array(MAX_REPORT_BYTES)).length, MAX_REPORT_BYTES);
  assert.throws(
    () => boundedReportBytes(new Uint8Array(MAX_REPORT_BYTES + 1)),
    /REPORT_SIZE_LIMIT/,
  );
  assert.throws(() => boundedReportBytes(new Uint8Array()), /REPORT_RENDER_FAILED/);
});

test("saved workpaper creation time retains full nanosecond precision", async () => {
  const { renderBasCsv } = await renderer();
  const value = bas(0);
  value.workpaper.createdAt.nanos = 123456789;
  assert.match(
    csv(renderBasCsv(value)).find((row) => row[1] === "created_at")?.[2] ?? "",
    /\.123456789Z$/,
  );
});

test("BAS rejects invalid money, currencies, source identity, hash, basis and bounds", async () => {
  const { renderBasCsv } = await renderer();
  for (const change of [
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.sources).gross.minorUnits = -1n;
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.salesSources).gst.minorUnits = -1n;
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.sources).gstCredit.minorUnits = 11001n;
    },
    (v: ReturnType<typeof bas>) => {
      v.workpaper.gstBasis = 0;
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.sources).gross.minorUnits = 9223372036854775808n;
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.salesSources).gst.currencyCode = "USD";
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.salesSources).sourceHash = new Uint8Array(31);
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.salesSources).invoiceId = "invalid";
    },
    (v: ReturnType<typeof bas>) => {
      v.workpaper.gstBasis = 3;
    },
    (v: ReturnType<typeof bas>) => {
      v.workpaper.createdAt.nanos = 1000000000;
    },
  ]) {
    const value = bas();
    change(value);
    assert.throws(() => renderBasCsv(value), /INVALID_REPORT_SNAPSHOT/);
  }
});

test("maximum BAS PDF renders all 1000 sales and 1000 purchases with intact dates and cents", async () => {
  const { renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const value = bas(1000);
  value.organisation.legalName = "Gamma Systems Pty Ltd";
  value.workpaper.salesSources.forEach((source, index) => {
    source.invoiceNumber = `SALE${String(index).padStart(4, "0")}`;
    source.customerName = "Customer";
  });
  value.workpaper.sources.forEach((source, index) => {
    source.invoiceNumber = `PURCHASE${String(index).padStart(4, "0")}`;
    source.supplierName = "Supplier";
  });
  const bytes = await renderBasPdf(value);
  const loading = getDocument({
    data: new Uint8Array(bytes),
    useSystemFonts: false,
    useWorkerFetch: false,
    verbosity: 0,
  });
  const document = await loading.promise;
  try {
    let content = "",
      dateCount = 0,
      salesAmountCount = 0;
    for (let number = 1; number <= document.numPages; number++) {
      const page = await document.getPage(number),
        text = await page.getTextContent();
      for (const item of text.items)
        if ("str" in item) {
          content += `${item.str}\n`;
          if (item.str === "2026-10-01") dateCount++;
          if (item.str === "55.00") salesAmountCount++;
        }
      page.cleanup();
    }
    assert.ok(document.numPages > 2);
    for (let index = 0; index < 1000; index++) {
      assert.ok(content.includes(`SALE${String(index).padStart(4, "0")}`), `missing sale ${index}`);
      assert.ok(
        content.includes(`PURCHASE${String(index).padStart(4, "0")}`),
        `missing purchase ${index}`,
      );
    }
    assert.ok(dateCount >= 2000, `${dateCount} intact source dates`);
    assert.equal(salesAmountCount, 1000);
    assert.ok((content.match(/Tax date/g) ?? []).length > 2);
    assert.ok((content.match(/Document date/g) ?? []).length > 2);
  } finally {
    await loading.destroy();
  }
});

test("BAS source dates and full int64 cents remain intact beside long unbroken party identities", async () => {
  const { renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  for (const [partyLength, gross] of [
    [30, 5500n],
    [200, 9223372036854775807n],
  ] as const) {
    const value = bas();
    value.organisation.legalName = "Gamma Systems Pty Ltd";
    const sale = first(value.workpaper.salesSources),
      purchase = first(value.workpaper.sources);
    sale.customerName = "W".repeat(partyLength);
    sale.taxDate.day = 2;
    sale.gross.minorUnits = gross;
    purchase.supplierName = "W".repeat(256);
    purchase.invoiceNumber = "P".repeat(128);
    purchase.documentDate.day = 3;
    purchase.gross.minorUnits = gross;
    if (gross === 9223372036854775807n) {
      sale.gst.minorUnits = gross;
      purchase.gstCredit.minorUnits = gross;
    }
    const loading = getDocument({
      data: new Uint8Array(await renderBasPdf(value)),
      useSystemFonts: false,
      useWorkerFetch: false,
      verbosity: 0,
    });
    const document = await loading.promise;
    try {
      const items: { text: string; x: number; y: number; width: number; page: number }[] = [];
      for (let number = 1; number <= document.numPages; number++) {
        const page = await document.getPage(number),
          text = await page.getTextContent();
        for (const item of text.items)
          if ("str" in item) {
            items.push({
              text: item.str,
              x: item.transform[4],
              y: item.transform[5],
              width: item.width,
              page: number,
            });
            if (item.str.trim()) {
              assert.ok(
                item.transform[4] >= 39.99,
                `text crosses the left page margin: ${item.str}`,
              );
              assert.ok(
                item.transform[4] + item.width <= 555.29,
                `text crosses the right page margin: ${item.str}`,
              );
            }
          }
        page.cleanup();
      }
      const strings = items.map((item) => item.text);
      assert.ok(
        strings.includes("2026-10-02"),
        `sales date split with ${partyLength} character party`,
      );
      assert.ok(
        strings.includes("2026-10-03"),
        `purchase date split with ${partyLength} character party`,
      );
      const exact = gross === 5500n ? "55.00" : "92233720368547758.07";
      const grossCells = items.filter((item) => item.text === exact);
      assert.equal(
        grossCells.length,
        gross === 5500n ? 2 : 4,
        `${exact} source amount cells split with ${partyLength} character party`,
      );
      for (const date of items.filter((item) => ["2026-10-02", "2026-10-03"].includes(item.text))) {
        const columns = items
          .filter((item) => item.page === date.page && item.y === date.y && item.text.trim())
          .sort((left, right) => left.x - right.x);
        assert.equal(columns.length, 3, "source date, gross and GST share one row");
        for (let column = 1; column < columns.length; column++) {
          const previous = columns[column - 1],
            current = columns[column];
          assert.ok(previous && current);
          assert.ok(
            previous.x + previous.width < current.x,
            "source numeric cells do not overlap adjacent columns",
          );
        }
      }
      if (gross === 5500n) assert.ok(strings.includes("5.00"), "GST must remain intact");
      assert.equal(
        strings
          .map((text) => text.replace(/^(Customer|Supplier): /u, ""))
          .filter((text) => /^W+$/u.test(text))
          .join("").length,
        partyLength + 256,
        "all party glyphs retained",
      );
      assert.equal(
        strings.filter((text) => /^P+$/u.test(text)).join("").length,
        128,
        "all invoice number glyphs retained",
      );
    } finally {
      await loading.destroy();
    }
  }
});

test("long organisation names keep PDF metric labels and exact amounts readable", async () => {
  const { renderIncomePdf, renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const summary = income(),
    workpaper = bas();
  for (const value of [summary, workpaper]) {
    value.organisation.legalName = "W".repeat(256);
    value.organisation.displayName = "M".repeat(256);
  }
  for (const bytes of [await renderIncomePdf(summary), await renderBasPdf(workpaper)]) {
    const loading = getDocument({
      data: new Uint8Array(bytes),
      verbosity: 0,
      useSystemFonts: false,
      useWorkerFetch: false,
    });
    const document = await loading.promise;
    try {
      assert.ok(
        document.numPages <= 3,
        "names must not squeeze metric labels into a vertical column",
      );
      const strings: string[] = [];
      for (let number = 1; number <= document.numPages; number++) {
        const page = await document.getPage(number),
          text = await page.getTextContent();
        for (const item of text.items)
          if ("str" in item) {
            strings.push(item.str);
            if (item.str.trim()) {
              assert.ok(item.transform[4] >= 39.99, "name context stays within the left margin");
              assert.ok(
                item.transform[4] + item.width <= 555.29,
                "name context stays within the right margin",
              );
            }
          }
        page.cleanup();
      }
      assert.ok(strings.includes("organisation abn"), "metric label remains on one line");
      assert.ok(strings.includes("51824753556"), "metric value remains on one line");
      assert.ok(
        strings.includes(strings.includes("Metric") ? "$90071992547409.93" : "$55.00"),
        "exact amount remains on one line",
      );
      assert.ok(strings.join("").includes("M".repeat(256)), "all display name glyphs retained");
    } finally {
      await loading.destroy();
    }
  }
});

test("each BAS source keeps its exact amount and retained identity together across page breaks", async () => {
  const { renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const value = bas(35);
  value.organisation.legalName = "Gamma Systems Pty Ltd";
  value.workpaper.salesSources.forEach((source, index) => {
    source.invoiceNumber = `SALESOURCE${String(index).padStart(4, "0")}`;
    source.gross.minorUnits = 10000n + BigInt(index);
  });
  value.workpaper.sources.forEach((source, index) => {
    source.invoiceNumber = `PURCHASESOURCE${String(index).padStart(4, "0")}`;
    source.gross.minorUnits = 20000n + BigInt(index);
  });
  const loading = getDocument({
    data: new Uint8Array(await renderBasPdf(value)),
    verbosity: 0,
    useSystemFonts: false,
    useWorkerFetch: false,
  });
  const document = await loading.promise;
  try {
    const pages: string[][] = [];
    for (let number = 1; number <= document.numPages; number++) {
      const page = await document.getPage(number),
        text = await page.getTextContent();
      pages.push(text.items.flatMap((item) => ("str" in item ? [item.str] : [])));
      page.cleanup();
    }
    for (const [prefix, major] of [
      ["SALESOURCE", "100"],
      ["PURCHASESOURCE", "200"],
    ] as const) {
      for (let index = 0; index < 35; index++) {
        const page = pages.find((items) =>
          items.includes(`${prefix}${String(index).padStart(4, "0")}`),
        );
        assert.ok(
          page?.includes(`${major}.${String(index).padStart(2, "0")}`),
          `${prefix}${index} identity separated from amount`,
        );
      }
    }
  } finally {
    await loading.destroy();
  }
});

test("PDF text preserves Latin ligature sequences in every retained identity and provenance field", async () => {
  const { renderIncomePdf, renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const identity = "018f0000-0000-7ff1-8000-000000000004";
  const sourceHash = "ff".repeat(32);
  const ordinary = "ff fi fl ffi ffl office affinity coffee";
  const summary = income(),
    workpaper = bas();
  for (const value of [summary, workpaper]) {
    value.organisation.id = identity;
    value.organisation.legalName = ordinary;
    value.organisation.displayName = ordinary;
  }
  summary.summary.organisationId = identity;
  workpaper.workpaper.organisationId = identity;
  workpaper.workpaper.id = "018f0000-0000-7ff1-8000-000000000005";
  workpaper.workpaper.sourceFingerprint = new Uint8Array(32).fill(255);
  const sale = first(workpaper.workpaper.salesSources),
    purchase = first(workpaper.workpaper.sources);
  sale.invoiceId = "018f0000-0000-7ff1-8000-000000000006";
  sale.paymentId = "018f0000-0000-7ff1-8000-000000000007";
  sale.sourceHash = new Uint8Array(32).fill(255);
  sale.customerName = ordinary;
  sale.invoiceNumber = ordinary;
  purchase.documentId = "018f0000-0000-7ff1-8000-000000000008";
  purchase.supplierName = ordinary;
  purchase.invoiceNumber = ordinary;
  workpaper.workpaper.reviewWarnings = [ordinary];
  for (const [kind, bytes] of [
    ["income", await renderIncomePdf(summary)],
    ["bas", await renderBasPdf(workpaper)],
  ] as const) {
    const loading = getDocument({
      data: new Uint8Array(bytes),
      verbosity: 0,
      useSystemFonts: false,
      useWorkerFetch: false,
    });
    const document = await loading.promise;
    try {
      let content = "";
      for (let number = 1; number <= document.numPages; number++) {
        const page = await document.getPage(number),
          text = await page.getTextContent();
        content += text.items.flatMap((item) => ("str" in item ? [item.str] : [])).join("\n");
        page.cleanup();
      }
      assert.ok(content.includes(identity), `${kind} retains the exact organisation identifier`);
      assert.ok(content.includes(ordinary), `${kind} retains every ordinary text glyph`);
      assert.equal(
        content.split(ordinary).length - 1,
        kind === "bas" ? 8 : 3,
        `${kind} retains every ordinary text field`,
      );
      if (kind === "income")
        assert.ok(content.includes("financial revision"), "financial revision label remains exact");
      if (kind === "bas") {
        for (const retained of [
          workpaper.workpaper.id,
          sale.invoiceId,
          sale.paymentId,
          purchase.documentId,
          sourceHash,
          "source fingerprint",
        ]) {
          assert.ok(content.includes(retained), `BAS retains exact provenance ${retained}`);
        }
      }
    } finally {
      await loading.destroy();
    }
  }
});

test("combining marks remain exact in CSV and fail visibly in PDF rather than losing characters", async () => {
  const { renderIncomePdf, renderBasPdf, renderIncomeCsv, renderBasCsv } = await renderer();
  const supported = "Cafe\u0301 Δοκιμη\u0301 Привет A\u0308 Ε\u0301 И\u0306";
  const summary = income(),
    workpaper = bas();
  for (const value of [summary, workpaper]) {
    value.organisation.legalName = supported;
    value.organisation.displayName = supported;
  }
  first(workpaper.workpaper.salesSources).customerName = supported;
  first(workpaper.workpaper.sources).supplierName = supported;
  workpaper.workpaper.reviewWarnings = [supported];
  assert.equal(
    csv(renderIncomeCsv(summary)).find((row) => row[0] === "organisation_legal_name")?.[1],
    supported,
  );
  assert.equal(
    csv(renderBasCsv(workpaper)).find((row) => row[1] === "organisation_legal_name")?.[2],
    supported,
  );
  await assert.rejects(() => renderIncomePdf(summary), /REPORT_PDF_UNSUPPORTED_TEXT/);
  await assert.rejects(() => renderBasPdf(workpaper), /REPORT_PDF_UNSUPPORTED_TEXT/);
  for (const mark of ["\u0301", "\u20dd", "\u034f", "\ufe00"]) {
    const value = income();
    value.organisation.legalName = `Retained ${mark}`;
    assert.equal(
      csv(renderIncomeCsv(value)).find((row) => row[0] === "organisation_legal_name")?.[1],
      `Retained ${mark}`,
    );
    await assert.rejects(() => renderIncomePdf(value), /REPORT_PDF_UNSUPPORTED_TEXT/);
  }
});

test("report fonts preserve precomposed Latin, Greek and Cyrillic text exactly", async () => {
  const { renderIncomePdf, renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const supported = "Élodie Δοκιμή Привет Ä Έ Й K K Ω Ω Å Å";
  const summary = income(),
    workpaper = bas();
  for (const value of [summary, workpaper]) {
    value.organisation.legalName = supported;
    value.organisation.displayName = supported;
  }
  first(workpaper.workpaper.salesSources).customerName = supported;
  first(workpaper.workpaper.sources).supplierName = supported;
  workpaper.workpaper.reviewWarnings = [supported];
  for (const bytes of [await renderIncomePdf(summary), await renderBasPdf(workpaper)]) {
    const loading = getDocument({
      data: new Uint8Array(bytes),
      verbosity: 0,
      useSystemFonts: false,
      useWorkerFetch: false,
    });
    const document = await loading.promise;
    try {
      let content = "";
      for (let number = 1; number <= document.numPages; number++) {
        const page = await document.getPage(number),
          text = await page.getTextContent({ disableNormalization: true });
        content += text.items.flatMap((item) => ("str" in item ? [item.str] : [])).join("\n");
        page.cleanup();
      }
      assert.ok(
        content.includes(supported),
        "every supported precomposed code point remains exact",
      );
    } finally {
      await loading.destroy();
    }
  }
});

test("format controls stay exact in CSV and fail visibly in PDF", async () => {
  const { renderIncomePdf, renderBasPdf, renderIncomeCsv, renderBasCsv } = await renderer();
  for (const character of ["\u200b", "\u200c", "\u200d", "\u2060", "\u2066", "\u2069", "\ufeff"]) {
    const retained = `BEFORE${character}AFTER`,
      summary = income(),
      workpaper = bas();
    summary.organisation.legalName = retained;
    const sale = first(workpaper.workpaper.salesSources);
    sale.customerName = retained;
    assert.equal(
      csv(renderIncomeCsv(summary)).find((row) => row[0] === "organisation_legal_name")?.[1],
      retained,
    );
    const records = csv(renderBasCsv(workpaper)),
      header = first(records);
    assert.equal(records.find((row) => row[0] === "sales")?.[header.indexOf("party")], retained);
    await assert.rejects(() => renderIncomePdf(summary), /REPORT_PDF_UNSUPPORTED_TEXT/);
    await assert.rejects(() => renderBasPdf(workpaper), /REPORT_PDF_UNSUPPORTED_TEXT/);
  }
});
