import { calculateInvoice, type InvoiceDraftData } from "@tammy/invoices/model";
import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { InvoiceDraftPreview, invoicePreviewData } from "./invoice-draft-preview";

const draft: InvoiceDraftData = {
  number: "SYNTHETIC-001",
  issueDate: "2026-09-30",
  dueDate: "2026-10-30",
  customerName: "Synthetic customer",
  customerAddress: "1 Example Road",
  sellerAddress: "2 Example Road",
  gstRegistered: true,
  notes: "Synthetic note",
  paymentInstructions: "Synthetic payment instructions",
  lines: [
    {
      description: "Workshop",
      quantityMilli: 1250,
      unitPriceMinor: 10001,
      unitCostMinor: 7319,
      taxTreatment: "taxable",
      sourceDocumentId: "018f0000-0000-7000-8000-000000000101",
    },
    {
      description: "Materials",
      quantityMilli: 1000,
      unitPriceMinor: 2500,
      taxTreatment: "gst-free",
    },
  ],
};

it("projects only public fields and canonical rounded amounts into the on-screen preview", () => {
  const data = invoicePreviewData(draft, calculateInvoice(draft), ["line-1", "line-2"]);
  expect(JSON.stringify(data)).not.toMatch(
    /unitCost|costMinor|marginMinor|sourceDocumentId|7319|018f0000/,
  );
  render(<InvoiceDraftPreview data={data} seller={undefined} saved={false} />);
  const preview = screen.getByRole("region", { name: "Draft invoice preview" });
  expect(preview.textContent).toContain("Workshop");
  expect(preview.textContent).toContain("1.25 × $100.01");
  expect(preview.textContent).toContain("$12.50");
  expect(preview.textContent).toContain("$137.51");
  expect(preview.textContent).not.toContain("$73.19");
  expect(preview.textContent).not.toContain(draft.lines[0]?.sourceDocumentId);
  expect(preview.querySelector("input, textarea, select")).toBeNull();
  expect(screen.getByTestId("invoice-total").textContent).toContain("$162.51");
  expect(screen.getByText("Not saved")).toBeTruthy();
  expect(screen.getByText("Seller name and ABN appear after saving.")).toBeTruthy();
  expect(screen.queryByRole("button", { name: /Send|Reminder|Customer view/ })).toBeNull();
});

it("clears stale monetary values when the edited draft cannot be calculated", () => {
  const data = invoicePreviewData(draft, calculateInvoice(draft), ["line-1", "line-2"]);
  const view = render(
    <InvoiceDraftPreview
      data={data}
      seller={{ name: "Synthetic studio", abn: "51824753556" }}
      saved
    />,
  );
  expect(screen.getByTestId("invoice-total").textContent).toContain("$162.51");
  view.rerender(
    <InvoiceDraftPreview
      data={undefined}
      seller={{ name: "Synthetic studio", abn: "51824753556" }}
      saved={false}
    />,
  );
  expect(screen.getByTestId("invoice-total").textContent).toContain("—");
  expect(screen.queryByText("$162.51")).toBeNull();
  expect(screen.queryByRole("table", { name: "Draft preview items" })).toBeNull();
  expect(screen.getByText(/Check the invoice details to update this preview/)).toBeTruthy();
  expect(screen.getByText("Not saved")).toBeTruthy();
});
