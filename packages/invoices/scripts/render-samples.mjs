import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { renderInvoicePdf } from "../src/pdf.ts";
import { draftFixture } from "../src/test-fixtures.ts";

const output = resolve(import.meta.dirname, "../../../.tmp/invoice-preview");
mkdirSync(output, { recursive: true });
const sample = {
  ...draftFixture(),
  sellerName: "Gamma Systems Pty Ltd",
  sellerAbn: "51824753556",
  state: "draft",
  lines: [
    {
      description: "Company EOFY accounting review — reconciliation, workpapers and preparation",
      quantityMilli: 1500,
      unitPriceMinor: 12500,
      unitCostMinor: 4000,
      taxTreatment: "taxable",
    },
    {
      description: "Reviewed expense reimbursement: business reference material",
      quantityMilli: 1000,
      unitPriceMinor: 5490,
      unitCostMinor: 4990,
      taxTreatment: "gst-free",
    },
    {
      description: "Reporting consultation",
      quantityMilli: 2250,
      unitPriceMinor: 8500,
      unitCostMinor: 3500,
      taxTreatment: "taxable",
    },
  ],
  paymentInstructions:
    "Bank transfer\nBSB: 000-000  Account: 123456789\nUse INV-2026-001 as your payment reference.",
};
writeFileSync(resolve(output, "mixed-gst-draft.pdf"), await renderInvoicePdf(sample));
writeFileSync(
  resolve(output, "mixed-gst-issued.pdf"),
  await renderInvoicePdf({ ...sample, state: "issued" }),
);
writeFileSync(
  resolve(output, "long-paginated.pdf"),
  await renderInvoicePdf({
    ...sample,
    lines: Array.from({ length: 50 }, (_, index) => ({
      description: `Service row ${index + 1}: detailed accounting review and document preparation for the company reporting period, including transaction reconciliation and review.`,
      quantityMilli: 1000,
      unitPriceMinor: 10000,
      unitCostMinor: 4000,
      taxTreatment: index % 2 ? "gst-free" : "taxable",
    })),
    notes:
      "Detailed terms for the accounting engagement, document review and completed deliverables. ".repeat(
        20,
      ),
  }),
);
console.log(output);
writeFileSync(
  resolve(output, "unicode-draft.pdf"),
  await renderInvoicePdf({
    ...sample,
    sellerName: "Zoë Núñez",
    customerName: "Εταιρεία — Компания",
  }),
);
