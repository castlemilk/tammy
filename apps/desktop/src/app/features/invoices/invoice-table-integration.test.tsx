import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { InvoiceLineTable } from "./invoice-line-table";
import { ReviewedExpenseTable } from "./reviewed-expense-table";

it("keeps issued lines read-only while their amounts remain keyboard reachable", async () => {
  const edit = vi.fn();
  render(
    <InvoiceLineTable
      lines={[
        {
          key: "line-1",
          description: "Consulting",
          quantity: "1",
          unitPrice: "1200.00",
          unitCost: "500.00",
          taxTreatment: "taxable",
        },
      ]}
      totals={{
        lines: [{ netMinor: 120000, gstMinor: 12000, totalMinor: 132000, costMinor: 50000 }],
        subtotalMinor: 120000,
        gstMinor: 12000,
        totalMinor: 132000,
        costMinor: 50000,
        marginMinor: 70000,
      }}
      gstRegistered
      busy={false}
      readOnly
      onEdit={edit}
      onAdd={vi.fn()}
      onRemove={vi.fn()}
    />,
  );
  const table = screen.getByRole("table", { name: "Invoice line items" });
  expect(within(table).getByText("$1,320.00")).toBeTruthy();
  expect(within(table).getByRole("columnheader", { name: "Unit cost Private" })).toBeTruthy();
  const description = screen.getByLabelText("Description 1") as HTMLInputElement;
  await userEvent.type(description, " changed");
  expect(description.value).toBe("Consulting");
  expect(edit).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "Add line item" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Remove line 1" })).toBeNull();
  const region = screen.getByRole("region", { name: "Invoice line items table" });
  region.focus();
  expect(document.activeElement).toBe(region);
});

it("excludes linked costs from selection and preserves explicit retry and pending gates", async () => {
  const refresh = vi.fn();
  const select = vi.fn();
  const props = {
    expenses: [
      { id: "available", description: "Supplies", subtotalMinor: 10000, gstMinor: 1000 },
      { id: "linked", description: "Equipment", subtotalMinor: 20000, gstMinor: 2000 },
    ],
    selectedIds: ["available", "linked"],
    linkedIds: new Set(["linked"]),
    loading: false,
    busy: false,
    error: "Expenses could not be refreshed.",
    markup: "20.00",
    onMarkup: vi.fn(),
    onSelect: select,
    onRefresh: refresh,
    onAdd: vi.fn(),
  };
  const view = render(<ReviewedExpenseTable {...props} />);
  expect(screen.getByTestId("selected-expense-summary").textContent).toContain(
    "1 selected · $100.00 net cost",
  );
  const linked = screen.getByRole("checkbox", { name: "Select Equipment" }) as HTMLInputElement;
  expect(linked.checked).toBe(false);
  expect(linked.disabled).toBe(true);
  await userEvent.click(linked);
  expect(select).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "Retry expenses" }));
  expect(refresh).toHaveBeenCalledOnce();
  fireEvent.change(screen.getByRole("searchbox", { name: "Search reviewed expenses" }), {
    target: { value: "Supplies" },
  });
  expect(screen.queryByText("Equipment")).toBeNull();
  expect(screen.getByTestId("selected-expense-summary").textContent).toContain("$100.00");
  view.rerender(<ReviewedExpenseTable {...props} loading busy />);
  expect(
    (screen.getByRole("button", { name: "Retry expenses" }) as HTMLButtonElement).disabled,
  ).toBe(true);
  expect(
    (screen.getByRole("button", { name: "Add selected expenses" }) as HTMLButtonElement).disabled,
  ).toBe(true);
});
