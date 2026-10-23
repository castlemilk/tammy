import type { InvoiceDraftData } from "@tammy/invoices/model";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import {
  type InvoiceExpense,
  type InvoiceRecord,
  type InvoicesApi,
  InvoicesScreen,
} from "./invoices-screen";

const printing: InvoiceExpense = {
  id: "018f0000-0000-7000-8000-000000000101",
  description: "Printing",
  subtotalMinor: 8000,
  gstMinor: 800,
};
const supplies: InvoiceExpense = {
  id: "018f0000-0000-7000-8000-000000000102",
  description: "Office supplies",
  subtotalMinor: 1234,
  gstMinor: 0,
};

function draft(): InvoiceDraftData {
  return {
    number: "INV-100",
    issueDate: "2026-09-30",
    dueDate: "2026-10-14",
    customerName: "Cambridge Services",
    customerAddress: "27 Cambridge Road, Mooroolbark VIC 3138",
    customerAbn: "",
    sellerAddress: "Melbourne VIC",
    gstRegistered: true,
    notes: "Thank you for your business.",
    paymentInstructions: "Pay by bank transfer.",
    lines: [
      {
        description: "Consulting",
        quantityMilli: 1000,
        unitPriceMinor: 10000,
        unitCostMinor: 6000,
        taxTreatment: "taxable",
      },
    ],
  };
}

function fixture(initial: InvoiceRecord[] = [], expenses: readonly InvoiceExpense[] = [printing]) {
  let records = structuredClone(initial);
  let nextId = 1;
  const calls: { saved: InvoiceDraftData[]; issued: string[]; exported: string[] } = {
    saved: [],
    issued: [],
    exported: [],
  };
  const api: InvoicesApi = {
    list: async () => structuredClone(records),
    listExpenses: async () => structuredClone(expenses),
    getProposal: async () => draft(),
    getTemplate: async () => ({
      version: 0,
      defaults: { sellerAddress: "", paymentInstructions: "", notes: "", paymentTermsDays: 14 },
    }),
    saveTemplate: async (defaults, expectedVersion) => ({ version: expectedVersion + 1, defaults }),
    uploadLogo: async () => "logo-1",
    getLogo: async (id) => ({ id, dataUrl: "data:image/png;base64,aGVsbG8=" }),
    save: async (value, id, expectedVersion) => {
      calls.saved.push(structuredClone(value));
      const existing = records.find((record) => record.id === id);
      if (existing && (existing.state !== "draft" || existing.version !== expectedVersion))
        throw new Error("conflict");
      const record: InvoiceRecord = {
        id: id ?? `invoice-${nextId++}`,
        version: (existing?.version ?? 0) + 1,
        state: "draft",
        sellerName: "Gamma Systems Pty Ltd",
        sellerAbn: "51824753556",
        draft: structuredClone(value),
      };
      records = [...records.filter((item) => item.id !== record.id), record];
      return structuredClone(record);
    },
    issue: async (id, expectedVersion) => {
      const existing = records.find((record) => record.id === id);
      if (!existing || existing.version !== expectedVersion) throw new Error("conflict");
      calls.issued.push(id);
      const record: InvoiceRecord = { ...existing, version: existing.version + 1, state: "issued" };
      records = records.map((item) => (item.id === id ? record : item));
      return structuredClone(record);
    },
    exportPdf: async (id) => {
      calls.exported.push(id);
      return { saved: true };
    },
  };
  return { api, calls };
}

async function useProposal() {
  await userEvent.click(screen.getByRole("button", { name: "Use MCP draft" }));
  await screen.findByDisplayValue("INV-100");
}

it("starts on the invoice list and opens an explicit new draft without implied GST registration", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  expect(screen.getByRole("heading", { name: "Invoices" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Save draft" })).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  expect((screen.getByLabelText("Registered for GST") as HTMLInputElement).checked).toBe(false);
  expect(screen.queryByRole("table", { name: "Saved invoices" })).toBeNull();
  expect((screen.getByRole("button", { name: "Export PDF" }) as HTMLButtonElement).disabled).toBe(
    true,
  );
});

it("loads an MCP proposal for human editing without saving it", async () => {
  const { api, calls } = fixture();
  render(<InvoicesScreen api={api} />);
  await useProposal();
  expect(screen.getByDisplayValue("Cambridge Services")).toBeTruthy();
  expect(screen.getByTestId("invoice-subtotal").textContent).toContain("$100.00");
  expect(screen.getByTestId("invoice-gst").textContent).toContain("$10.00");
  expect(screen.getByTestId("invoice-total").textContent).toContain("$110.00");
  expect(screen.getByTestId("invoice-cost").textContent).toContain("$60.00");
  expect(screen.getByTestId("invoice-margin").textContent).toContain("$40.00");
  expect(calls.saved).toEqual([]);
});

it("normalizes a pasted customer ABN in the editor and saved invoice", async () => {
  const { api, calls } = fixture();
  render(<InvoicesScreen api={api} />);
  await useProposal();

  const customerAbn = screen.getByLabelText("Customer ABN (optional)") as HTMLInputElement;
  fireEvent.change(customerAbn, { target: { value: "52 682 863 690" } });
  expect(customerAbn.value).toBe("52682863690");

  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  expect(calls.saved[0]?.customerAbn).toBe("52682863690");

  fireEvent.change(customerAbn, {
    target: { value: "52A 682 863 690 ABCDEFGHIJK" },
  });
  expect(customerAbn.value).toBe("52A682863690ABCDEFGHIJK");
});

it("saves exact decimal amounts and disables export after unsaved edits", async () => {
  const { api, calls } = fixture();
  render(<InvoicesScreen api={api} />);
  await useProposal();
  fireEvent.change(screen.getByLabelText("Quantity 1"), { target: { value: "1.125" } });
  fireEvent.change(screen.getByLabelText("Unit price 1"), { target: { value: "123.45" } });
  fireEvent.change(screen.getByLabelText("Unit cost 1"), { target: { value: "80.05" } });
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  expect(calls.saved[0]?.lines[0]).toMatchObject({
    quantityMilli: 1125,
    unitPriceMinor: 12345,
    unitCostMinor: 8005,
  });
  await userEvent.click(screen.getByRole("button", { name: "Export PDF" }));
  await waitFor(() => expect(calls.exported).toEqual(["invoice-1"]));
  fireEvent.change(screen.getByLabelText("Customer name"), {
    target: { value: "Changed customer" },
  });
  expect((screen.getByRole("button", { name: "Export PDF" }) as HTMLButtonElement).disabled).toBe(
    true,
  );
  expect(screen.getByText("Save changes before exporting or issuing.")).toBeTruthy();
});

it("reenables saved export when a line edit is restored to its saved value", async () => {
  const { api } = fixture();
  render(<InvoicesScreen api={api} />);
  await useProposal();
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  fireEvent.change(screen.getByLabelText("Unit price 1"), { target: { value: "125.00" } });
  expect(screen.getByRole("button", { name: "Export PDF" }).matches(":disabled")).toBe(true);
  fireEvent.change(screen.getByLabelText("Unit price 1"), { target: { value: "100.00" } });
  expect(screen.getByRole("button", { name: "Export PDF" }).matches(":disabled")).toBe(false);
});

it("rejects excessive decimal precision without silently rounding or losing edits", async () => {
  const { api, calls } = fixture();
  render(<InvoicesScreen api={api} />);
  await useProposal();
  fireEvent.change(screen.getByLabelText("Unit price 1"), { target: { value: "12.345" } });
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("two decimal places"),
  );
  expect(calls.saved).toEqual([]);
  expect(screen.getByDisplayValue("12.345")).toBeTruthy();
  expect(document.activeElement).toBe(screen.getByRole("alert").parentElement);
});

it("imports a reviewed expense as a cost with an explicit markup", async () => {
  const { api, calls } = fixture();
  render(<InvoicesScreen api={api} />);
  await useProposal();
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  fireEvent.change(screen.getByLabelText("Markup percentage"), { target: { value: "25" } });
  await userEvent.click(screen.getByRole("button", { name: "Add selected expenses" }));
  expect(screen.getByDisplayValue("Printing")).toBeTruthy();
  expect((screen.getByLabelText("Unit price 2") as HTMLInputElement).value).toBe("100.00");
  expect((screen.getByLabelText("Unit cost 2") as HTMLInputElement).value).toBe("80.00");
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  expect(calls.saved[0]?.lines[1]).toMatchObject({
    sourceDocumentId: "018f0000-0000-7000-8000-000000000101",
    unitCostMinor: 8000,
    unitPriceMinor: 10000,
  });
});

it("removes GST from taxable lines when registration is switched off", async () => {
  const { api, calls } = fixture();
  render(<InvoicesScreen api={api} />);
  await useProposal();
  await userEvent.click(screen.getByLabelText("Registered for GST"));
  expect(screen.getByTestId("invoice-gst").textContent).toContain("$0.00");
  expect((screen.getByLabelText("GST treatment 1") as HTMLSelectElement).value).toBe(
    "out-of-scope",
  );
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  expect(calls.saved[0]).toMatchObject({
    gstRegistered: false,
    lines: [{ taxTreatment: "out-of-scope" }],
  });
});

it("confirms replacement of unsaved changes before using another MCP draft", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  fireEvent.change(screen.getByLabelText("Customer name"), {
    target: { value: "Keep this customer" },
  });
  await userEvent.click(screen.getByRole("button", { name: "Use MCP draft" }));
  expect(screen.getByDisplayValue("Keep this customer")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "Keep editing" }));
  expect(screen.getByDisplayValue("Keep this customer")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "Use MCP draft" }));
  await userEvent.click(screen.getByRole("button", { name: "Discard changes and continue" }));
  expect(await screen.findByDisplayValue("Cambridge Services")).toBeTruthy();
});

it("requires an explicit issue confirmation and keeps issued invoices immutable", async () => {
  const record: InvoiceRecord = {
    id: "saved-1",
    version: 3,
    state: "draft",
    sellerName: "Gamma Systems Pty Ltd",
    sellerAbn: "51824753556",
    draft: draft(),
  };
  const { api, calls } = fixture([record]);
  render(<InvoicesScreen api={api} />);
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-100" }));
  await userEvent.click(screen.getByRole("button", { name: "Issue invoice" }));
  expect(calls.issued).toEqual([]);
  expect(
    screen.getByText(/records sales, GST and the amount owing in the business ledger/),
  ).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "Confirm issue" }));
  await screen.findByText("Invoice issued.");
  expect(calls.issued).toEqual(["saved-1"]);
  expect(screen.queryByLabelText("Customer name")).toBeNull();
  const detail = screen.getByRole("region", { name: "Issued invoice items" });
  expect(detail.querySelector("input, select, textarea")).toBeNull();
  expect(detail.textContent).toContain("Cambridge Services");
  expect(calls.saved).toEqual([]);
  expect(screen.queryByRole("button", { name: "Save draft" })).toBeNull();
  expect((screen.getByRole("button", { name: "Export PDF" }) as HTMLButtonElement).disabled).toBe(
    false,
  );
});

it("preserves draft input if saving fails", async () => {
  const { api } = fixture();
  render(
    <InvoicesScreen
      api={{
        ...api,
        save: async () => {
          throw new Error("unsafe internal detail");
        },
      }}
    />,
  );
  await useProposal();
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toContain("Your changes are still here");
  expect(alert.textContent).not.toContain("unsafe internal detail");
  expect(screen.getByDisplayValue("Cambridge Services")).toBeTruthy();
});

it("adds and removes manual line items", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await userEvent.click(screen.getByRole("button", { name: "Add line item" }));
  const table = screen.getByRole("table", { name: "Invoice line items" });
  expect(within(table).getAllByRole("textbox", { name: /Description/ })).toHaveLength(2);
  await userEvent.click(within(table).getByRole("button", { name: "Remove line 1" }));
  expect(within(table).getAllByRole("textbox", { name: /Description/ })).toHaveLength(1);
  expect(screen.getByRole("button", { name: "Remove line 1" }).matches(":disabled")).toBe(true);
});

it("uses an expense as the first line instead of leaving an empty placeholder", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  await userEvent.click(screen.getByRole("button", { name: "Add selected expenses" }));
  expect(screen.getAllByRole("textbox", { name: /Description/ })).toHaveLength(1);
  expect((screen.getByLabelText("Description 1") as HTMLInputElement).value).toBe("Printing");
  expect((screen.getByLabelText("GST treatment 1") as HTMLSelectElement).value).toBe(
    "out-of-scope",
  );
});

it("reports a cancelled PDF dialog without claiming a file was saved", async () => {
  const record: InvoiceRecord = {
    id: "saved-1",
    version: 3,
    state: "draft",
    sellerName: "Gamma Systems Pty Ltd",
    sellerAbn: "51824753556",
    draft: draft(),
  };
  const { api } = fixture([record]);
  render(<InvoicesScreen api={{ ...api, exportPdf: async () => ({ saved: false }) }} />);
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-100" }));
  await userEvent.click(screen.getByRole("button", { name: "Export PDF" }));
  expect(await screen.findByText("PDF export cancelled. No file was saved.")).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
});

it("reports a successful PDF export", async () => {
  const record: InvoiceRecord = {
    id: "saved-1",
    version: 3,
    state: "draft",
    sellerName: "Gamma Systems Pty Ltd",
    sellerAbn: "51824753556",
    draft: draft(),
  };
  const { api } = fixture([record]);
  render(<InvoicesScreen api={api} />);
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-100" }));
  await userEvent.click(screen.getByRole("button", { name: "Export PDF" }));
  expect(await screen.findByText("Invoice PDF saved.")).toBeTruthy();
});

it("does not present unknown costs as a known margin", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  await useProposal();
  fireEvent.change(screen.getByLabelText("Unit cost 1"), { target: { value: "" } });
  expect(screen.getByTestId("invoice-margin").textContent).toContain("—");
  expect(
    screen.getByText(
      "Add a cost to every line to calculate profit. Enter 0 only for a known zero cost.",
    ),
  ).toBeTruthy();
});

it("checks issue-only identity fields before offering issue confirmation", async () => {
  const incomplete = draft();
  incomplete.customerName = "";
  const record: InvoiceRecord = {
    id: "saved-1",
    version: 3,
    state: "draft",
    sellerName: "Gamma Systems Pty Ltd",
    sellerAbn: "51824753556",
    draft: incomplete,
  };
  const { api, calls } = fixture([record]);
  render(<InvoicesScreen api={api} />);
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-100" }));
  await userEvent.click(screen.getByRole("button", { name: "Issue invoice" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("customerName"),
  );
  expect(screen.queryByRole("button", { name: "Confirm issue" })).toBeNull();
  expect(calls.issued).toEqual([]);
});

it("keeps text field bounds aligned with persisted invoices", () => {
  render(<InvoicesScreen api={fixture().api} />);
  expect((screen.getByLabelText("Invoice number") as HTMLInputElement).maxLength).toBe(40);
  expect((screen.getByLabelText("Description 1") as HTMLInputElement).maxLength).toBe(500);
  expect((screen.getByLabelText("Notes") as HTMLTextAreaElement).maxLength).toBe(2000);
  expect((screen.getByLabelText("Payment instructions") as HTMLTextAreaElement).maxLength).toBe(
    1000,
  );
});

it("refreshes reviewed expenses without replacing unsaved invoice fields", async () => {
  const { api } = fixture();
  let refreshed = false;
  render(
    <InvoicesScreen
      api={{
        ...api,
        listExpenses: async () =>
          refreshed
            ? [
                {
                  id: "018f0000-0000-7000-8000-000000000111",
                  description: "Newly reviewed supplies",
                  subtotalMinor: 1000,
                  gstMinor: 100,
                },
              ]
            : [],
      }}
    />,
  );
  await useProposal();
  refreshed = true;
  await userEvent.click(screen.getByRole("button", { name: "Refresh expenses" }));
  expect(
    await screen.findByRole("checkbox", { name: "Select Newly reviewed supplies" }),
  ).toBeTruthy();
  expect(screen.getByDisplayValue("Cambridge Services")).toBeTruthy();
  expect(screen.getByDisplayValue("Consulting")).toBeTruthy();
});

it("keeps unsupported PDF text editable instead of confirming an irreversible issue", async () => {
  const unsupported = draft();
  unsupported.customerName = "公司服务";
  const record: InvoiceRecord = {
    id: "saved-1",
    version: 3,
    state: "draft",
    sellerName: "Gamma Systems Pty Ltd",
    sellerAbn: "51824753556",
    draft: unsupported,
  };
  const { api, calls } = fixture([record]);
  render(<InvoicesScreen api={api} />);
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-100" }));
  await userEvent.click(screen.getByRole("button", { name: "Issue invoice" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("customerName"),
  );
  expect(screen.queryByRole("button", { name: "Confirm issue" })).toBeNull();
  expect(screen.getByRole("textbox", { name: "Customer name" }).matches(":disabled")).toBe(false);
  expect(calls.issued).toEqual([]);
});

it("presents editable semantic tables with private costs and explicit GST column labels", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  const table = screen.getByRole("table", { name: "Invoice line items" });
  expect(within(table).getByRole("columnheader", { name: "Unit cost Private" })).toBeTruthy();
  expect(within(table).getByRole("columnheader", { name: "Selling price Ex GST" })).toBeTruthy();
  expect(within(table).getByRole("columnheader", { name: "Line total Incl GST" })).toBeTruthy();
  expect(within(table).getByText("Manual line")).toBeTruthy();
  for (const name of ["Quantity 1", "Unit cost 1", "Unit price 1"]) {
    const input = within(table).getByRole("textbox", { name }) as HTMLInputElement;
    expect(input.type).toBe("text");
    expect(input.inputMode).toBe("decimal");
  }
  const expenses = screen.getByRole("table", { name: "Reviewed expenses" });
  const row = (await within(expenses).findByText("Printing")).closest("tr");
  if (!row) throw new Error("expected expense row");
  expect(within(row).getByText("$80.00")).toBeTruthy();
  expect(within(row).getByText("$8.00")).toBeTruthy();
  expect(within(row).getByText("$88.00")).toBeTruthy();
});

it("imports multiple selections with exact markup and clears selection", async () => {
  const { api, calls } = fixture([], [printing, supplies]);
  render(<InvoicesScreen api={api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  await userEvent.click(screen.getByRole("checkbox", { name: "Select Office supplies" }));
  expect(screen.getByTestId("selected-expense-summary").textContent).toContain("2 selected");
  expect(screen.getByTestId("selected-expense-summary").textContent).toContain("$92.34");
  fireEvent.change(screen.getByLabelText("Markup percentage"), { target: { value: "12.50" } });
  await userEvent.click(screen.getByRole("button", { name: "Add selected expenses" }));
  expect((screen.getByLabelText("Unit price 1") as HTMLInputElement).value).toBe("90.00");
  expect((screen.getByLabelText("Unit price 2") as HTMLInputElement).value).toBe("13.88");
  expect((screen.getByLabelText("Quantity 2") as HTMLInputElement).value).toBe("1");
  expect((screen.getByLabelText("GST treatment 2") as HTMLSelectElement).value).toBe(
    "out-of-scope",
  );
  expect(screen.getByTestId("selected-expense-summary").textContent).toContain("0 selected");
  expect(screen.getAllByText("Linked reviewed expense · internal only")).toHaveLength(2);
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  expect(calls.saved[0]?.lines).toMatchObject([
    { sourceDocumentId: printing.id, unitCostMinor: 8000, unitPriceMinor: 9000 },
    { sourceDocumentId: supplies.id, unitCostMinor: 1234, unitPriceMinor: 1388 },
  ]);
});

it("rounds markup half-up in cents and uses registration rather than source GST for a sale", async () => {
  const { api } = fixture([], [{ ...printing, subtotalMinor: 4, gstMinor: 0 }]);
  render(<InvoicesScreen api={api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await userEvent.click(screen.getByLabelText("Registered for GST"));
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  fireEvent.change(screen.getByLabelText("Markup percentage"), { target: { value: "12.50" } });
  await userEvent.click(screen.getByRole("button", { name: "Add selected expenses" }));
  expect((screen.getByLabelText("Unit price 1") as HTMLInputElement).value).toBe("0.05");
  expect((screen.getByLabelText("GST treatment 1") as HTMLSelectElement).value).toBe("taxable");
  expect(screen.getByTestId("invoice-total").textContent).toContain("$0.06");
  expect(screen.getByRole("status").textContent).toContain("Review the selling price and GST");
});

it("keeps selected expenses across search and prevents duplicate links until a line is removed", async () => {
  render(<InvoicesScreen api={fixture([], [printing, supplies]).api} />);
  await useProposal();
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  fireEvent.change(screen.getByLabelText("Search reviewed expenses"), {
    target: { value: "office" },
  });
  expect(screen.queryByRole("checkbox", { name: "Select Printing" })).toBeNull();
  await userEvent.click(screen.getByRole("checkbox", { name: "Select Office supplies" }));
  expect(screen.getByTestId("selected-expense-summary").textContent).toContain("2 selected");
  await userEvent.click(screen.getByRole("button", { name: "Add selected expenses" }));
  fireEvent.change(screen.getByLabelText("Search reviewed expenses"), { target: { value: "" } });
  const checkbox = screen.getByRole("checkbox", { name: "Select Printing" });
  expect(checkbox.matches(":disabled")).toBe(true);
  expect(checkbox.closest("tr")?.textContent).toContain("Added");
  await userEvent.click(checkbox);
  expect(screen.getByRole("button", { name: "Add selected expenses" }).matches(":disabled")).toBe(
    true,
  );
  await userEvent.click(screen.getByRole("button", { name: "Remove line 2" }));
  expect(screen.getByRole("checkbox", { name: "Select Printing" }).matches(":disabled")).toBe(
    false,
  );
});

it("preserves a previously edited blank line when importing an expense", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  fireEvent.change(screen.getByLabelText("Description 1"), { target: { value: "Work" } });
  fireEvent.change(screen.getByLabelText("Description 1"), { target: { value: "" } });
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  await userEvent.click(screen.getByRole("button", { name: "Add selected expenses" }));
  expect((screen.getByLabelText("Description 1") as HTMLInputElement).value).toBe("");
  expect((screen.getByLabelText("Description 2") as HTMLInputElement).value).toBe("Printing");
});

it("rejects invalid markup atomically while retaining all selected expenses", async () => {
  render(<InvoicesScreen api={fixture([], [printing, supplies]).api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  await userEvent.click(screen.getByRole("checkbox", { name: "Select Office supplies" }));
  fireEvent.change(screen.getByLabelText("Markup percentage"), { target: { value: "12.345" } });
  await userEvent.click(screen.getByRole("button", { name: "Add selected expenses" }));
  expect(screen.getByRole("alert").textContent).toContain("two decimal places");
  expect(screen.getAllByRole("textbox", { name: /Description/ })).toHaveLength(1);
  expect(
    (screen.getByRole("checkbox", { name: "Select Printing" }) as HTMLInputElement).checked,
  ).toBe(true);
  expect(
    (screen.getByRole("checkbox", { name: "Select Office supplies" }) as HTMLInputElement).checked,
  ).toBe(true);
});

it("rejects a batch that exceeds fifty rows without inserting any selected expense", async () => {
  const { api } = fixture([], [printing, supplies]);
  const proposal = draft();
  const sourceLine = proposal.lines[0];
  if (!sourceLine) throw new Error("expected a proposal line");
  proposal.lines = Array.from({ length: 49 }, (_, index) => ({
    ...sourceLine,
    description: `Work ${index + 1}`,
  }));
  render(<InvoicesScreen api={{ ...api, getProposal: async () => proposal }} />);
  await useProposal();
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  await userEvent.click(screen.getByRole("checkbox", { name: "Select Office supplies" }));
  await userEvent.click(screen.getByRole("button", { name: "Add selected expenses" }));
  expect(screen.getByRole("alert").textContent).toContain("50 line items");
  expect(screen.getAllByRole("textbox", { name: /Description/ })).toHaveLength(49);
  expect(screen.getByTestId("selected-expense-summary").textContent).toContain("2 selected");
  await userEvent.click(screen.getByRole("button", { name: "Remove line 49" }));
  await userEvent.click(screen.getByRole("button", { name: "Add selected expenses" }));
  expect(screen.getAllByRole("textbox", { name: /Description/ })).toHaveLength(50);
  expect(screen.getByRole("button", { name: "Add line item" }).matches(":disabled")).toBe(true);
});

it("rejects a batch containing an excessive marked-up price without partial insertion", async () => {
  render(
    <InvoicesScreen
      api={fixture([], [printing, { ...supplies, subtotalMinor: 100_000_000 }]).api}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  await userEvent.click(screen.getByRole("checkbox", { name: "Select Office supplies" }));
  fireEvent.change(screen.getByLabelText("Markup percentage"), { target: { value: "1" } });
  await userEvent.click(screen.getByRole("button", { name: "Add selected expenses" }));
  expect(screen.getByRole("alert").textContent).toContain("too large");
  expect(screen.getAllByRole("textbox", { name: /Description/ })).toHaveLength(1);
  expect(screen.getByTestId("selected-expense-summary").textContent).toContain("2 selected");
});

it("updates inclusive row totals, private cost and profit while preserving partial decimal input", async () => {
  const { api, calls } = fixture();
  render(<InvoicesScreen api={api} />);
  await useProposal();
  fireEvent.change(screen.getByLabelText("Quantity 1"), { target: { value: "1.125" } });
  fireEvent.change(screen.getByLabelText("Unit price 1"), { target: { value: "123.45" } });
  fireEvent.change(screen.getByLabelText("Unit cost 1"), { target: { value: "80.05" } });
  expect(
    within(screen.getByRole("table", { name: "Invoice line items" })).getByText("$152.77"),
  ).toBeTruthy();
  expect(screen.getByTestId("invoice-cost").textContent).toContain("$90.06");
  expect(screen.getByTestId("invoice-margin").textContent).toContain("$48.82");
  fireEvent.change(screen.getByLabelText("Unit price 1"), { target: { value: "." } });
  expect((screen.getByLabelText("Unit price 1") as HTMLInputElement).value).toBe(".");
  expect(
    within(screen.getByRole("table", { name: "Invoice line items" })).getByText("—"),
  ).toBeTruthy();
  expect(screen.getByTestId("invoice-total").textContent).toContain("—");
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  expect(screen.getByRole("alert").textContent).toContain("Unit price 1");
  expect(calls.saved).toEqual([]);
});

it("recognises a known zero cost and keeps profit unknown if another line is uncosted", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  await useProposal();
  fireEvent.change(screen.getByLabelText("Unit cost 1"), { target: { value: "0" } });
  expect(screen.getByTestId("invoice-margin").textContent).toContain("$100.00");
  await userEvent.click(screen.getByRole("button", { name: "Add line item" }));
  expect(screen.getByTestId("invoice-margin").textContent).toContain("—");
});

it("distinguishes an empty reviewed list from a search with no matching expenses", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await screen.findByRole("checkbox", { name: "Select Printing" });
  fireEvent.change(screen.getByLabelText("Search reviewed expenses"), {
    target: { value: "unknown" },
  });
  expect(screen.getByText("No reviewed expenses match your search.")).toBeTruthy();
  expect(screen.queryByText("No reviewed expenses yet.")).toBeNull();
});

it("shows a truthful reviewed expense empty state only after successful loading", async () => {
  render(<InvoicesScreen api={fixture([], []).api} />);
  expect(await screen.findByText("No reviewed expenses yet.")).toBeTruthy();
});

it("retains prior rows and manual editing during refresh, then reconciles selected IDs", async () => {
  const { api } = fixture([], [printing, supplies]);
  let completeRefresh: ((value: readonly InvoiceExpense[]) => void) | undefined;
  let request = 0;
  render(
    <InvoicesScreen
      api={{
        ...api,
        listExpenses: () => {
          request += 1;
          return request === 1
            ? api.listExpenses()
            : new Promise((resolve) => {
                completeRefresh = resolve;
              });
        },
      }}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  await userEvent.click(screen.getByRole("checkbox", { name: "Select Office supplies" }));
  await userEvent.click(screen.getByRole("button", { name: "Refresh expenses" }));
  expect(screen.getByText("Loading reviewed expenses…")).toBeTruthy();
  expect(screen.getByLabelText("Description 1").matches(":disabled")).toBe(false);
  fireEvent.change(screen.getByLabelText("Description 1"), { target: { value: "Still editing" } });
  expect(screen.getByRole("checkbox", { name: "Select Printing" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Add selected expenses" }).matches(":disabled")).toBe(
    true,
  );
  completeRefresh?.([{ ...printing, subtotalMinor: 9000 }]);
  await waitFor(() => expect(screen.queryByText("Loading reviewed expenses…")).toBeNull());
  expect(screen.queryByRole("checkbox", { name: "Select Office supplies" })).toBeNull();
  expect(
    (screen.getByRole("checkbox", { name: "Select Printing" }) as HTMLInputElement).checked,
  ).toBe(true);
  expect(screen.getByTestId("selected-expense-summary").textContent).toContain("1 selected");
  expect(screen.getByTestId("selected-expense-summary").textContent).toContain("$90.00");
  expect(screen.getByDisplayValue("Still editing")).toBeTruthy();
});

it("retains rows and selection after refresh failure and exposes a retry without an empty state", async () => {
  const { api } = fixture();
  let request = 0;
  render(
    <InvoicesScreen
      api={{
        ...api,
        listExpenses: async () => {
          request += 1;
          if (request === 2) throw new Error("failure");
          return api.listExpenses();
        },
      }}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await userEvent.click(await screen.findByRole("checkbox", { name: "Select Printing" }));
  await userEvent.click(screen.getByRole("button", { name: "Refresh expenses" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("Reviewed expenses could not be loaded"),
  );
  expect(
    (screen.getByRole("checkbox", { name: "Select Printing" }) as HTMLInputElement).checked,
  ).toBe(true);
  expect(screen.queryByText("No reviewed expenses yet.")).toBeNull();
  expect(screen.getByLabelText("Description 1").matches(":disabled")).toBe(false);
  await userEvent.click(screen.getByRole("button", { name: "Retry expenses" }));
  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
});

it("shows initial expense loading and failure without claiming a successfully empty list", async () => {
  const { api } = fixture();
  let failLoad: ((reason: Error) => void) | undefined;
  render(
    <InvoicesScreen
      api={{
        ...api,
        listExpenses: () =>
          new Promise((_, reject) => {
            failLoad = reject;
          }),
      }}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  expect(screen.getByText("Loading reviewed expenses…")).toBeTruthy();
  expect(screen.queryByText("No reviewed expenses yet.")).toBeNull();
  failLoad?.(new Error("unavailable"));
  await screen.findByRole("alert");
  expect(screen.queryByText("No reviewed expenses yet.")).toBeNull();
  expect(screen.getByRole("button", { name: "Retry expenses" })).toBeTruthy();
});

it("shows issued line values without editor fields or expense import", async () => {
  const record: InvoiceRecord = {
    id: "issued-1",
    version: 2,
    state: "issued",
    sellerName: "Gamma Systems Pty Ltd",
    sellerAbn: "51824753556",
    draft: draft(),
  };
  render(<InvoicesScreen api={fixture([record]).api} />);
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-100" }));
  const table = screen.getByRole("table", { name: "Invoice line items" });
  expect(table.querySelector("input, select, textarea")).toBeNull();
  expect(table.textContent).toContain("Consulting");
  expect(table.textContent).toContain("$100.00");
  expect(table.textContent).toContain("$10.00");
  expect(table.textContent).toContain("$110.00");
  expect(screen.queryByRole("button", { name: "Add line item" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Remove line 1" })).toBeNull();
  expect(screen.queryByRole("table", { name: "Reviewed expenses" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Add selected expenses" })).toBeNull();
});

it("keeps list search and keyboard focus when returning from an invoice", async () => {
  const record: InvoiceRecord = {
    id: "invoice-existing",
    version: 1,
    state: "draft",
    sellerName: "Synthetic studio",
    sellerAbn: "51824753556",
    draft: draft(),
  };
  const { api } = fixture([record]);
  render(<InvoicesScreen api={api} />);
  await screen.findByRole("button", { name: "Open INV-100" });
  expect(screen.queryByRole("button", { name: "Save draft" })).toBeNull();
  await userEvent.type(screen.getByLabelText("Search saved invoices"), "Cambridge");
  await userEvent.click(screen.getByRole("button", { name: "Open INV-100" }));
  expect(screen.queryByRole("table", { name: "Saved invoices" })).toBeNull();
  expect(screen.getByRole("heading", { name: "INV-100" })).toBe(document.activeElement);
  await userEvent.click(screen.getByRole("button", { name: "Back to invoices" }));
  expect(screen.getByLabelText("Search saved invoices")).toHaveProperty("value", "Cambridge");
  expect(screen.getByRole("button", { name: "Open INV-100" })).toBe(document.activeElement);
});

it("keeps unsaved changes on Back until discard is explicit, without writing", async () => {
  const record: InvoiceRecord = {
    id: "invoice-existing",
    version: 1,
    state: "draft",
    sellerName: "Synthetic studio",
    sellerAbn: "51824753556",
    draft: draft(),
  };
  const { api, calls } = fixture([record]);
  render(<InvoicesScreen api={api} />);
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-100" }));
  fireEvent.change(screen.getByLabelText("Customer name"), {
    target: { value: "Unsaved customer" },
  });
  await userEvent.click(screen.getByRole("button", { name: "Back to invoices" }));
  await userEvent.click(screen.getByRole("button", { name: "Keep editing" }));
  expect(screen.getByLabelText("Customer name")).toHaveProperty("value", "Unsaved customer");
  await userEvent.click(screen.getByRole("button", { name: "Back to invoices" }));
  await userEvent.click(screen.getByRole("button", { name: "Discard changes and continue" }));
  expect(screen.getByRole("table", { name: "Saved invoices" })).toBeTruthy();
  expect(calls.saved).toEqual([]);
  await userEvent.click(screen.getByRole("button", { name: "Open INV-100" }));
  expect(screen.getByLabelText("Customer name")).toHaveProperty("value", "Cambridge Services");
});
