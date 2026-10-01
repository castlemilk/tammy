import type { InvoiceDraftData } from "@tammy/invoices/model";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { type InvoiceRecord, type InvoicesApi, InvoicesScreen } from "./invoices-screen";

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

function fixture(initial: InvoiceRecord[] = []) {
  let records = structuredClone(initial);
  let nextId = 1;
  const calls: { saved: InvoiceDraftData[]; issued: string[]; exported: string[] } = {
    saved: [],
    issued: [],
    exported: [],
  };
  const api: InvoicesApi = {
    list: async () => structuredClone(records),
    listExpenses: async () => [
      {
        id: "018f0000-0000-7000-8000-000000000101",
        description: "Printing",
        subtotalMinor: 8000,
        gstMinor: 800,
      },
    ],
    getProposal: async () => draft(),
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

it("starts with an editable invoice and no implied GST registration", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  expect(screen.getByRole("heading", { name: "Invoices" })).toBeTruthy();
  expect((screen.getByLabelText("Registered for GST") as HTMLInputElement).checked).toBe(false);
  expect(await screen.findByText("No saved invoices yet.")).toBeTruthy();
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
});

it("imports a reviewed expense as a cost with an explicit markup", async () => {
  const { api, calls } = fixture();
  render(<InvoicesScreen api={api} />);
  await useProposal();
  await screen.findByRole("option", { name: /Printing/ });
  fireEvent.change(screen.getByLabelText("Reviewed expense"), {
    target: { value: "018f0000-0000-7000-8000-000000000101" },
  });
  fireEvent.change(screen.getByLabelText("Markup percentage"), { target: { value: "25" } });
  await userEvent.click(screen.getByRole("button", { name: "Add expense as line" }));
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
  expect(screen.getByText(/does not post to the ledger or send it/)).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "Confirm issue" }));
  await screen.findByText("Invoice issued.");
  expect(calls.issued).toEqual(["saved-1"]);
  expect(screen.getByLabelText("Customer name").matches(":disabled")).toBe(true);
  await userEvent.type(screen.getByLabelText("Customer name"), "cannot change");
  expect(screen.getByDisplayValue("Cambridge Services")).toBeTruthy();
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
  await userEvent.click(screen.getByRole("button", { name: "Add line item" }));
  const rows = screen.getAllByRole("group", { name: /Line item/ });
  expect(rows).toHaveLength(2);
  const firstRow = rows[0];
  if (!firstRow) throw new Error("expected a line item");
  await userEvent.click(within(firstRow).getByRole("button", { name: "Remove line 1" }));
  expect(screen.getAllByRole("group", { name: /Line item/ })).toHaveLength(1);
});

it("uses an expense as the first line instead of leaving an empty placeholder", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  await screen.findByRole("option", { name: /Printing/ });
  fireEvent.change(screen.getByLabelText("Reviewed expense"), {
    target: { value: "018f0000-0000-7000-8000-000000000101" },
  });
  await userEvent.click(screen.getByRole("button", { name: "Add expense as line" }));
  expect(screen.getAllByRole("group", { name: /Line item/ })).toHaveLength(1);
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
      "Add a cost to every line to calculate margin. Enter 0 only for a known zero cost.",
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
  expect(await screen.findByRole("option", { name: /Newly reviewed supplies/ })).toBeTruthy();
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
