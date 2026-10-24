import { calculateInvoice } from "@tammy/invoices/model";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { InvoiceIssuedItems } from "./invoice-issued-items";
import type { InvoiceRecord } from "./invoices-screen";

const record: InvoiceRecord = {
  id: "invoice-issued",
  version: 2,
  state: "issued",
  sellerName: "Synthetic studio",
  sellerAbn: "51824753556",
  draft: {
    number: "SYNTHETIC-001",
    customerName: "Synthetic customer",
    customerAddress: "1 Example Road",
    sellerAddress: "2 Example Road",
    customerAbn: "",
    issueDate: "2026-09-30",
    dueDate: "2026-10-30",
    gstRegistered: true,
    notes: "Synthetic note",
    paymentInstructions: "Synthetic bank instructions",
    lines: [
      {
        description: "Workshop",
        quantityMilli: 1250,
        unitPriceMinor: 10001,
        unitCostMinor: 4000,
        taxTreatment: "taxable",
        sourceDocumentId: "018f0000-0000-7000-8000-000000000101",
      },
      {
        description: "GST-free materials",
        quantityMilli: 1000,
        unitPriceMinor: 2500,
        taxTreatment: "gst-free",
      },
    ],
  },
};

it("presents immutable customer amounts while retaining private estimates under disclosure", async () => {
  render(<InvoiceIssuedItems record={record} totals={calculateInvoice(record.draft)} />);
  const table = screen.getByRole("table", { name: "Invoice line items" });
  const row = within(table).getByRole("row", { name: /Workshop/ });
  expect(row.textContent).toContain("1.25");
  expect(row.textContent).toContain("$100.01");
  expect(row.textContent).toContain("$12.50");
  expect(row.textContent).toContain("$137.51");
  expect(table.querySelector("input, select, textarea")).toBeNull();
  expect(table.textContent).not.toContain("$40.00");
  expect(table.textContent).not.toContain(record.draft.lines[0]?.sourceDocumentId);
  expect(screen.getByTestId("invoice-total").textContent).toContain("$162.51");
  expect(screen.queryByTestId("invoice-received")).toBeNull();
  expect(screen.queryByTestId("invoice-outstanding")).toBeNull();

  const disclosure = screen.getByText("Invoice information & private estimates");
  await userEvent.click(disclosure);
  expect(disclosure.closest("details")?.open).toBe(true);
  expect(screen.getByText("Synthetic bank instructions")).toBeTruthy();
  expect(disclosure.closest("details")?.textContent).toContain("1 Example Road");
  expect(screen.getByTestId("invoice-cost").textContent).toContain("$50.00");
  expect(screen.getByTestId("invoice-margin").textContent).toContain("—");
  expect(screen.getByRole("table", { name: "Private invoice estimates" }).textContent).toContain(
    "$40.00",
  );
});

it("uses confirmed receipt totals without interpreting them as another lifecycle state", () => {
  render(
    <InvoiceIssuedItems
      record={record}
      totals={calculateInvoice(record.draft)}
      accounting={{
        invoiceId: record.id,
        version: 1,
        posted: true,
        gstBasis: "non-cash",
        receivedMinor: 5000,
        outstandingMinor: 11251,
        payments: [],
        paymentCount: 0,
      }}
    />,
  );
  expect(screen.getByTestId("invoice-received").textContent).toContain("$50.00");
  expect(screen.getByTestId("invoice-outstanding").textContent).toContain("$112.51");
  expect(screen.queryByText(/Part-paid|Overdue|Paid in full/)).toBeNull();
});
