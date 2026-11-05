import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { InvoiceRecordList } from "./invoice-record-list";
import type { InvoiceRecord } from "./invoices-screen";

function record(index: number): InvoiceRecord {
  return {
    id: `invoice-${index}`,
    version: 1,
    state: index % 2 ? "draft" : "issued",
    sellerName: "Synthetic studio",
    sellerAbn: "51824753556",
    draft: {
      number: `INV-${index}`,
      issueDate: "2026-09-30",
      dueDate: "2026-10-30",
      customerName: index === 12 ? "Merri Studio" : "Example client",
      customerAddress: "Synthetic address",
      sellerAddress: "Synthetic address",
      gstRegistered: true,
      notes: "",
      paymentInstructions: "",
      lines: [
        {
          description: "Workshop",
          quantityMilli: 1000,
          unitPriceMinor: 120000,
          taxTreatment: "taxable",
        },
      ],
    },
  };
}
const records = Array.from({ length: 12 }, (_, i) => record(i + 1));
const props = {
  records,
  currentId: "invoice-12",
  loading: false,
  busy: false,
  onRefresh: vi.fn(),
  onOpen: vi.fn(),
};

it("keeps every saved invoice reachable through paging, customer search and status filters", async () => {
  const user = userEvent.setup();
  render(<InvoiceRecordList {...props} />);
  expect(
    within(screen.getByRole("table", { name: "Saved invoices" })).getAllByRole("row"),
  ).toHaveLength(11);
  expect(screen.queryByRole("button", { name: "Open INV-12" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Next invoice page" }));
  await user.click(screen.getByRole("button", { name: "Open INV-12" }));
  expect(props.onOpen).toHaveBeenCalledWith(records[11]);
  expect(screen.getByRole("button", { name: "Open INV-12" }).getAttribute("aria-pressed")).toBe(
    "true",
  );
  await user.type(screen.getByLabelText("Search saved invoices"), "mERRi");
  expect(screen.getByText("1–1 of 1 invoices")).toBeTruthy();
  expect(screen.getByRole("table").textContent).toContain("$1,320.00");
  await user.click(screen.getByRole("button", { name: /^Draft/ }));
  expect(screen.getByText("No matching invoices")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Reset invoice filters" }));
  expect(document.activeElement).toBe(screen.getByLabelText("Search saved invoices"));
  expect(screen.getByText("1–10 of 12 invoices")).toBeTruthy();
});

it("clamps a later page after refresh removes records and clears search with keyboard focus", async () => {
  const user = userEvent.setup();
  const view = render(<InvoiceRecordList {...props} />);
  await user.click(screen.getByRole("button", { name: "Next invoice page" }));
  view.rerender(<InvoiceRecordList {...props} records={records.slice(0, 1)} />);
  expect(screen.getByText("1–1 of 1 invoices")).toBeTruthy();
  expect(
    (screen.getByRole("button", { name: "Next invoice page" }) as HTMLButtonElement).disabled,
  ).toBe(true);
  await user.type(screen.getByLabelText("Search saved invoices"), "absent");
  await user.click(screen.getByRole("button", { name: "Clear invoice search" }));
  expect(document.activeElement).toBe(screen.getByLabelText("Search saved invoices"));
  expect(screen.getByRole("button", { name: "Open INV-1" })).toBeTruthy();
});

it("distinguishes a failed load from an empty workspace and retains cached records on refresh failure", () => {
  const error = "Saved invoices could not be loaded. Try refreshing.";
  const view = render(<InvoiceRecordList {...props} records={[]} error={error} />);
  expect(screen.getByRole("alert").textContent).toContain(error);
  expect(screen.getByText("Saved invoices unavailable")).toBeTruthy();
  expect(screen.queryByText("No saved invoices yet.")).toBeNull();
  view.rerender(<InvoiceRecordList {...props} error={error} />);
  expect(screen.getByRole("button", { name: "Open INV-1" })).toBeTruthy();
});
