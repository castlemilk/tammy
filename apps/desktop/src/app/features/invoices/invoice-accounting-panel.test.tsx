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
    recordPayment: vi.fn(
      async (): Promise<InvoiceAccountingRecord> => ({
        ...posted,
        version: 2,
        receivedMinor: 5500,
        outstandingMinor: 5500,
      }),
    ),
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

it("keeps an unsubmitted receipt when changing views and cancels without a write", async () => {
  const api = fixture();
  const user = userEvent.setup();
  render(
    <InvoiceAccountingPanel
      api={api}
      invoiceId="invoice-1"
      invoiceVersion={3}
      issueDate="2026-09-30"
      incomeMinor={10000}
      gstMinor={1000}
      items={() => <p>Issued items</p>}
    />,
  );
  await screen.findByText("Cash basis · GST follows recorded payments");
  expect(screen.getByRole("button", { name: "Items" }).getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByRole("button", { name: "Record payment" })).toBeNull();
  const payments = screen.getByRole("button", { name: "Payments" });
  payments.focus();
  await user.keyboard(" ");
  await user.click(screen.getByRole("button", { name: "Record payment" }));
  expect(screen.getByLabelText("Payment date")).toBe(document.activeElement);
  await user.clear(screen.getByLabelText("Amount received (AUD)"));
  await user.type(screen.getByLabelText("Amount received (AUD)"), "25.00");
  await user.type(screen.getByLabelText("Payment reference (optional)"), "Review only");
  await user.click(screen.getByRole("button", { name: "Items" }));
  expect(screen.queryByRole("button", { name: "Save payment" })).toBeNull();
  await user.click(payments);
  expect(screen.getByLabelText("Amount received (AUD)")).toHaveProperty("value", "25.00");
  expect(screen.getByLabelText("Payment reference (optional)")).toHaveProperty(
    "value",
    "Review only",
  );
  expect(api.get).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("button", { name: "Cancel payment" }));
  expect(screen.getByRole("button", { name: "Record payment" })).toBe(document.activeElement);
  expect(api.recordPayment).not.toHaveBeenCalled();
});

it("blocks view changes and repeated submits while a receipt is pending", async () => {
  let confirm: ((value: InvoiceAccountingRecord) => void) | undefined;
  const api = fixture();
  api.recordPayment.mockImplementation(
    () =>
      new Promise((resolve) => {
        confirm = resolve;
      }),
  );
  const onBusyChange = vi.fn();
  render(
    <InvoiceAccountingPanel
      api={api}
      invoiceId="invoice-1"
      invoiceVersion={3}
      issueDate="2026-09-30"
      incomeMinor={10000}
      gstMinor={1000}
      items={() => <p>Issued items</p>}
      onBusyChange={onBusyChange}
    />,
  );
  await screen.findByText("Cash basis · GST follows recorded payments");
  await userEvent.click(screen.getByRole("button", { name: "Payments" }));
  await userEvent.click(screen.getByRole("button", { name: "Record payment" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Save payment" }).matches(":disabled")).toBe(false),
  );
  await userEvent.dblClick(screen.getByRole("button", { name: "Save payment" }));
  expect(api.recordPayment).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("button", { name: "Items" }).matches(":disabled")).toBe(true);
  expect(screen.getByRole("button", { name: "Cancel payment" }).matches(":disabled")).toBe(true);
  expect(onBusyChange).toHaveBeenLastCalledWith(true);
  confirm?.({ ...posted, version: 2, receivedMinor: 11000, outstandingMinor: 0 });
  await screen.findByText("Payment recorded.");
  expect(onBusyChange).toHaveBeenLastCalledWith(false);
  expect(screen.getByRole("button", { name: "Items" }).matches(":disabled")).toBe(false);
  expect(screen.getByTestId("invoice-outstanding").textContent).toContain("$0.00");
});
