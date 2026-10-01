import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import type { InvoiceAccountingRecord } from "../../../shared/invoice-accounting";
import { InvoiceAccountingPanel } from "./invoice-accounting-panel";

const posted = {
  invoiceId: "invoice-1",
  organisationId: "organisation-1",
  version: 1,
  posted: true,
  gstBasis: "cash" as const,
  receivedMinor: 0,
  outstandingMinor: 11000,
  revenueAccountId: "sales-1",
  journalId: "journal-1",
  payments: [],
  paymentCount: 0,
};

function fixture(value: InvoiceAccountingRecord = posted) {
  return {
    get: vi.fn(async () => value),
    post: vi.fn(async () => posted),
    recordPayment: vi.fn(async () => ({
      ...posted,
      version: 2,
      receivedMinor: 5500,
      outstandingMinor: 5500,
    })),
    listBankAccounts: vi.fn(async () => [{ id: "bank-1", code: "1000", name: "Business bank" }]),
  };
}

it("records an actual dated payment against the current accounting version", async () => {
  const api = fixture();
  render(
    <InvoiceAccountingPanel
      api={api}
      invoiceId="invoice-1"
      invoiceVersion={3}
      issueDate="2026-09-30"
      incomeMinor={10000}
      gstMinor={1000}
    />,
  );
  expect(await screen.findByText("Cash basis · GST follows recorded payments")).toBeTruthy();
  const section = screen.getByRole("region", { name: "Invoice accounting" });
  expect(section.textContent).toContain("$110.00");
  await userEvent.click(screen.getByRole("button", { name: "Record payment" }));
  fireEvent.change(screen.getByLabelText("Payment date"), { target: { value: "2026-10-02" } });
  fireEvent.change(screen.getByLabelText("Amount received (AUD)"), { target: { value: "55.00" } });
  fireEvent.change(screen.getByLabelText("Payment reference (optional)"), {
    target: { value: "Bank receipt 123" },
  });
  await userEvent.click(screen.getByRole("button", { name: "Save payment" }));
  await waitFor(() =>
    expect(api.recordPayment).toHaveBeenCalledWith("invoice-1", 1, {
      date: "2026-10-02",
      amountMinor: 5500,
      bankAccountId: "bank-1",
      reference: "Bank receipt 123",
    }),
  );
  expect(await screen.findByText("Payment recorded.")).toBeTruthy();
  expect(section.textContent).toContain("$55.00");
});

it("requires legacy duplicate review before adding an issued invoice to accounts", async () => {
  const { journalId: _journalId, ...unposted } = posted;
  const api = fixture({ ...unposted, posted: false, version: 0 });
  render(
    <InvoiceAccountingPanel
      api={api}
      invoiceId="invoice-1"
      invoiceVersion={3}
      issueDate="2026-09-30"
      incomeMinor={10000}
      gstMinor={1000}
    />,
  );
  await screen.findByText("Issued invoice needs accounting review");
  const button = screen.getByRole("button", { name: "Add to accounts" });
  expect(button.matches(":disabled")).toBe(true);
  await userEvent.click(
    screen.getByRole("checkbox", {
      name: "I checked this invoice has not already been entered manually.",
    }),
  );
  await userEvent.click(button);
  await waitFor(() => expect(api.post).toHaveBeenCalledWith("invoice-1", 3));
  expect(await screen.findByText("Invoice added to accounts.")).toBeTruthy();
});

it("rejects overpayment and pre-issue dates before calling accounting", async () => {
  const api = fixture();
  render(
    <InvoiceAccountingPanel
      api={api}
      invoiceId="invoice-1"
      invoiceVersion={3}
      issueDate="2026-09-30"
      incomeMinor={10000}
      gstMinor={1000}
    />,
  );
  await userEvent.click(await screen.findByRole("button", { name: "Record payment" }));
  fireEvent.change(screen.getByLabelText("Payment date"), { target: { value: "2026-09-29" } });
  await userEvent.click(screen.getByRole("button", { name: "Save payment" }));
  expect(screen.getByRole("alert").textContent).toContain("on or after");
  fireEvent.change(screen.getByLabelText("Payment date"), { target: { value: "2026-10-02" } });
  fireEvent.change(screen.getByLabelText("Amount received (AUD)"), { target: { value: "110.01" } });
  await userEvent.click(screen.getByRole("button", { name: "Save payment" }));
  expect(screen.getByRole("alert").textContent).toContain("outstanding");
  expect(api.recordPayment).not.toHaveBeenCalled();
});
