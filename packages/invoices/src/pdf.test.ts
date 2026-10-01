import assert from "node:assert/strict";
import test from "node:test";
import { extractData } from "@formepdf/core";
import { pdfStreams } from "../test-support/pdf-text.ts";
import { renderInvoicePdf } from "./pdf.ts";
import { draftFixture } from "./test-fixtures.ts";

const pdfInput = () => ({
  ...draftFixture(),
  sellerName: "Gamma Systems Pty Ltd",
  sellerAbn: "51824753556",
  state: "draft" as const,
});

test("renders an actual offline A4 pdfcn invoice with draft label, AUD and GST totals", async () => {
  const bytes = await renderInvoicePdf(pdfInput());
  assert.equal(Buffer.from(bytes).subarray(0, 5).toString(), "%PDF-");
  assert.ok(bytes.length > 1000);
  const content = pdfStreams(bytes);
  assert.match(content, /DRAFT/);
  assert.match(content, /Tax Invoice/);
  assert.match(content, /165\.02/);
  assert.match(content, /AUD/);
  assert.match(content, /Page 1 of 1/);
  assert.equal(extractData(bytes), null);
});

test("customer PDF excludes source document references, unit costs and margins", async () => {
  const input = pdfInput();
  const reference = "019fdab5-4704-7d72-8d37-0645f45b2ad3";
  const first = input.lines[0];
  assert.ok(first);
  const bytes = await renderInvoicePdf({
    ...input,
    lines: [{ ...first, unitCostMinor: 777777, sourceDocumentId: reference }],
  });
  const content = pdfStreams(bytes);
  assert.equal(content.includes(reference), false);
  assert.equal(content.includes("7,777.77"), false);
  assert.equal(content.includes("unitCostMinor"), false);
  assert.equal(content.includes("marginMinor"), false);
  assert.equal(extractData(bytes), null);
});

test("long descriptions, fifty rows and notes paginate with actual page counts and repeated headers", async () => {
  const input = pdfInput();
  const bytes = await renderInvoicePdf({
    ...input,
    lines: Array.from({ length: 50 }, (_, index) => ({
      description: `Service row ${index + 1}: detailed accounting review and document preparation for the company reporting period, including transaction reconciliation and review.`,
      quantityMilli: 1000,
      unitPriceMinor: 10000,
      taxTreatment: index % 2 ? "gst-free" : "taxable",
    })),
    notes: "Detailed terms for the accounting engagement. ".repeat(40),
  });
  const content = pdfStreams(bytes);
  const count = [...content.matchAll(/\/Type\s*\/Pages\b[\s\S]*?\/Count\s+(\d+)/g)]
    .map((match) => Number(match[1]))
    .sort((a, b) => b - a)[0];
  assert.ok(count && count > 1, "expected more than one actual PDF page");
  assert.match(content, new RegExp(`Page ${count} of ${count}`));
  assert.equal(content.includes("{{pageNumber}}"), false);
  assert.equal(content.includes("{{totalPages}}"), false);
  assert.ok(
    (content.match(/Description/g) ?? []).length > 1,
    "table header must repeat on table continuation pages (notes-only pages need no table header)",
  );
  assert.match(content, /Service row 50/);
});

test("non-GST-registered sellers produce an Invoice, never a Tax Invoice", async () => {
  const input = pdfInput();
  const first = input.lines[0];
  assert.ok(first);
  const content = pdfStreams(
    await renderInvoicePdf({
      ...input,
      gstRegistered: false,
      lines: [{ ...first, taxTreatment: "out-of-scope" }],
    }),
  );
  assert.equal(content.includes("Tax Invoice"), false);
  assert.match(content, /Not registered for GST/);
  assert.match(content, /150\.02/);
});

test("issued PDF validates identities and omits draft marking", async () => {
  const input = pdfInput();
  await assert.rejects(
    renderInvoicePdf({ ...input, state: "issued", customerName: "" }),
    /customerName/,
  );
  const content = pdfStreams(await renderInvoicePdf({ ...input, state: "issued" }));
  assert.equal(content.includes("DRAFT"), false);
  assert.match(content, /INV-2026-001/);
});

test("PDF seller identity accepts 256 characters and rejects excessive text or controls", async () => {
  const input = pdfInput();
  await assert.doesNotReject(renderInvoicePdf({ ...input, sellerName: "x".repeat(256) }));
  await assert.rejects(renderInvoicePdf({ ...input, sellerName: "x".repeat(257) }), /sellerName/);
  await assert.rejects(
    renderInvoicePdf({ ...input, sellerName: "Seller\u0000Name" }),
    /sellerName/,
  );
  await assert.doesNotReject(renderInvoicePdf({ ...input, sellerName: "é".repeat(256) }));
  await assert.rejects(renderInvoicePdf({ ...input, sellerName: "Broken\ud800" }), /sellerName/);
});

test("fails clearly on unsupported font characters rather than silently altering invoice identities", async () => {
  const input = pdfInput();
  await assert.rejects(renderInvoicePdf({ ...input, sellerName: "公司" }), /sellerName.*U\+516C/);
  await assert.rejects(
    renderInvoicePdf({ ...input, customerName: "Customer 😀" }),
    /customerName.*U\+1F600/,
  );
  await assert.rejects(renderInvoicePdf({ ...input, notes: "Payment 中" }), /notes.*U\+4E2D/);
});

test("embeds its packaged Noto Sans font for Latin accents, Greek and Cyrillic without system fonts", async () => {
  const input = pdfInput();
  const content = pdfStreams(
    await renderInvoicePdf({
      ...input,
      sellerName: "Zoë Núñez",
      customerName: "Εταιρεία — Компания",
    }),
  );
  assert.match(content, /NotoSans/);
  assert.match(content, /FontFile2/);
  assert.match(content, /ToUnicode/);
  assert.match(content, /Zoë Núñez/);
  assert.match(content, /Εταιρεία — Компания/);
});

test("keeps a standard three-line mixed-GST invoice and its payment instructions on one page", async () => {
  const input = pdfInput();
  const content = pdfStreams(
    await renderInvoicePdf({
      ...input,
      lines: [
        {
          description:
            "Company EOFY accounting review — reconciliation, workpapers and preparation",
          quantityMilli: 1500,
          unitPriceMinor: 12500,
          taxTreatment: "taxable",
        },
        {
          description: "Reviewed expense reimbursement: business reference material",
          quantityMilli: 1000,
          unitPriceMinor: 5490,
          taxTreatment: "gst-free",
        },
        {
          description: "Reporting consultation",
          quantityMilli: 2250,
          unitPriceMinor: 8500,
          taxTreatment: "taxable",
        },
      ],
      paymentInstructions:
        "Bank transfer\nBSB: 000-000  Account: 123456789\nUse INV-2026-001 as your payment reference.",
    }),
  );
  assert.match(content, /\/Type\s*\/Pages\b[\s\S]*?\/Count\s+1\b/);
  assert.match(content, /Thank you for your business\./);
  assert.match(content, /471\.53/);
});
