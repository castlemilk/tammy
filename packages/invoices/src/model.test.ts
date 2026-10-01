import assert from "node:assert/strict";
import test from "node:test";
import {
  calculateInvoice,
  formatAud,
  formatQuantity,
  INVOICE_DRAFT_JSON_SCHEMA,
  validateInvoiceDraft,
  validateInvoiceForIssue,
} from "./model.ts";
import { draftFixture } from "./test-fixtures.ts";

test("calculates per-line half-up amounts, GST and private cost margin using minor units", () => {
  const result = calculateInvoice(validateInvoiceDraft(draftFixture()));
  assert.deepEqual(result, {
    lines: [{ netMinor: 15002, gstMinor: 1500, totalMinor: 16502, costMinor: 6000 }],
    subtotalMinor: 15002,
    gstMinor: 1500,
    totalMinor: 16502,
    costMinor: 6000,
    marginMinor: 9002,
  });
});

test("rounds GST per line and never taxes GST-free, input-taxed or out-of-scope rows", () => {
  const draft = draftFixture();
  const base = { description: "Small item", quantityMilli: 1000, unitPriceMinor: 5 };
  draft.lines = ["taxable", "taxable", "gst-free", "input-taxed", "out-of-scope"].map(
    (taxTreatment) => ({ ...base, unitCostMinor: 0, taxTreatment: taxTreatment as "taxable" }),
  );
  const result = calculateInvoice(validateInvoiceDraft(draft));
  assert.equal(result.subtotalMinor, 25);
  assert.equal(result.gstMinor, 2);
  assert.equal(result.totalMinor, 27);
});

test("permits incomplete customer and seller address in an editable draft, not in an issued invoice", () => {
  const draft = validateInvoiceDraft({
    ...draftFixture(),
    customerName: "",
    customerAddress: "",
    sellerAddress: "",
  });
  assert.equal(draft.customerName, "");
  assert.throws(
    () => validateInvoiceForIssue(draft, { name: "Seller Pty Ltd", abn: "51824753556" }),
    /customerName/,
  );
  assert.throws(
    () =>
      validateInvoiceForIssue(validateInvoiceDraft(draftFixture()), {
        name: "",
        abn: "51824753556",
      }),
    /seller.name/,
  );
  assert.throws(
    () =>
      validateInvoiceForIssue(validateInvoiceDraft(draftFixture()), {
        name: "Seller Pty Ltd",
        abn: "",
      }),
    /seller.abn/,
  );
  assert.doesNotThrow(() =>
    validateInvoiceForIssue(validateInvoiceDraft(draftFixture()), {
      name: "Seller Pty Ltd",
      abn: "51824753556",
    }),
  );
});

test("rejects unknown properties and identity or execution data in an MCP draft", () => {
  assert.throws(
    () => validateInvoiceDraft({ ...draftFixture(), organisationId: "secret" }),
    /organisationId/,
  );
  assert.throws(
    () =>
      validateInvoiceDraft({
        ...draftFixture(),
        lines: [{ ...draftFixture().lines[0], accountId: "secret" }],
      }),
    /accountId/,
  );
  assert.throws(() => validateInvoiceDraft(null), /object/);
  assert.throws(() => validateInvoiceDraft([]), /object/);
});

test("validates actual dates, due ordering, ABN checksums and UUIDv7 document references", () => {
  assert.throws(
    () => validateInvoiceDraft({ ...draftFixture(), issueDate: "2026-02-30" }),
    /issueDate/,
  );
  assert.throws(
    () => validateInvoiceDraft({ ...draftFixture(), dueDate: "2026-09-29" }),
    /dueDate/,
  );
  assert.throws(
    () => validateInvoiceDraft({ ...draftFixture(), customerAbn: "12345678901" }),
    /customerAbn/,
  );
  assert.throws(
    () =>
      validateInvoiceDraft({
        ...draftFixture(),
        lines: [{ ...draftFixture().lines[0], sourceDocumentId: "not-a-document" }],
      }),
    /sourceDocumentId/,
  );
  assert.doesNotThrow(() =>
    validateInvoiceDraft({
      ...draftFixture(),
      lines: [
        { ...draftFixture().lines[0], sourceDocumentId: "019fdab5-4704-7d72-8d37-0645f45b2ad3" },
      ],
    }),
  );
});

test("rejects invalid quantities, prices, tax treatment and unregistered GST", () => {
  for (const quantityMilli of [0, -1, 1000001, 1.5, Number.NaN]) {
    assert.throws(
      () =>
        validateInvoiceDraft({
          ...draftFixture(),
          lines: [{ ...draftFixture().lines[0], quantityMilli }],
        }),
      /quantityMilli/,
    );
  }
  for (const unitPriceMinor of [-1, 100000001, 1.5, Number.POSITIVE_INFINITY]) {
    assert.throws(
      () =>
        validateInvoiceDraft({
          ...draftFixture(),
          lines: [{ ...draftFixture().lines[0], unitPriceMinor }],
        }),
      /unitPriceMinor/,
    );
  }
  assert.throws(
    () =>
      validateInvoiceDraft({
        ...draftFixture(),
        lines: [{ ...draftFixture().lines[0], taxTreatment: "free-form" }],
      }),
    /taxTreatment/,
  );
  assert.throws(
    () => validateInvoiceDraft({ ...draftFixture(), gstRegistered: false }),
    /gstRegistered/,
  );
});

test("bounds line count, text size and invoice totals", () => {
  assert.throws(() => validateInvoiceDraft({ ...draftFixture(), lines: [] }), /lines/);
  assert.throws(
    () =>
      validateInvoiceDraft({
        ...draftFixture(),
        lines: Array.from({ length: 51 }, () => draftFixture().lines[0]),
      }),
    /lines/,
  );
  assert.throws(
    () => validateInvoiceDraft({ ...draftFixture(), notes: "x".repeat(2001) }),
    /notes/,
  );
  assert.throws(
    () => validateInvoiceDraft({ ...draftFixture(), paymentInstructions: "x".repeat(1001) }),
    /paymentInstructions/,
  );
  assert.throws(
    () => validateInvoiceDraft({ ...draftFixture(), number: "x".repeat(41) }),
    /number/,
  );
  const maximum = {
    description: "Large item",
    quantityMilli: 1000000,
    unitPriceMinor: 100000000,
    taxTreatment: "taxable",
  };
  assert.equal(
    calculateInvoice(validateInvoiceDraft({ ...draftFixture(), lines: [maximum] })).totalMinor,
    110000000000,
  );
  assert.throws(
    () =>
      validateInvoiceDraft({ ...draftFixture(), lines: Array.from({ length: 10 }, () => maximum) }),
    /total/,
  );
});

test("allows blank draft dates for editing but requires calendar dates before issue", () => {
  const draft = validateInvoiceDraft({ ...draftFixture(), issueDate: "", dueDate: "" });
  assert.equal(draft.issueDate, "");
  assert.throws(
    () => validateInvoiceForIssue(draft, { name: "Seller Pty Ltd", abn: "51824753556" }),
    /issueDate/,
  );
  assert.doesNotThrow(() =>
    validateInvoiceDraft({ ...draftFixture(), issueDate: "", dueDate: "2026-10-30" }),
  );
});

test("counts Unicode characters consistently with Core and rejects malformed Unicode", () => {
  assert.equal(
    validateInvoiceDraft({ ...draftFixture(), customerName: "😀".repeat(200) }).customerName,
    "😀".repeat(200),
  );
  assert.throws(
    () => validateInvoiceDraft({ ...draftFixture(), customerName: "😀".repeat(201) }),
    /customerName/,
  );
  assert.throws(
    () => validateInvoiceDraft({ ...draftFixture(), customerName: "Broken\ud800" }),
    /customerName/,
  );
});

test("returns an independent normalized draft without mutating callers", () => {
  const input = draftFixture();
  const result = validateInvoiceDraft(input);
  const first = input.lines[0];
  assert.ok(first);
  first.description = "Changed after validation";
  assert.equal(result.lines[0]?.description, "Accounting services");
  assert.equal(
    validateInvoiceDraft({ ...draftFixture(), customerAbn: "51 824 753 556" }).customerAbn,
    "51824753556",
  );
});

test("formats AUD and thousandth quantities without exposing floating-point artifacts", () => {
  assert.equal(formatAud(16502), "$165.02");
  assert.equal(formatAud(-123456), "-$1,234.56");
  assert.equal(formatQuantity(1000), "1");
  assert.equal(formatQuantity(1500), "1.5");
  assert.equal(formatQuantity(1), "0.001");
});

test("publishes a strict bounded draft schema for MCP without exposing application identity fields", () => {
  assert.equal(INVOICE_DRAFT_JSON_SCHEMA.additionalProperties, false);
  assert.equal(INVOICE_DRAFT_JSON_SCHEMA.properties.lines.maxItems, 50);
  assert.equal(INVOICE_DRAFT_JSON_SCHEMA.properties.lines.items.additionalProperties, false);
  assert.equal(
    INVOICE_DRAFT_JSON_SCHEMA.properties.lines.items.properties.quantityMilli.maximum,
    1000000,
  );
  assert.equal("organisationId" in INVOICE_DRAFT_JSON_SCHEMA.properties, false);
});
