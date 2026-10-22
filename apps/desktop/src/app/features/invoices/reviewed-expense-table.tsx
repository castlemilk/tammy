import { formatAud } from "@tammy/invoices/model";
import { RefreshCw } from "lucide-react";
import { useState } from "react";
import { HorizontalScrollArea } from "../../components/horizontal-scroll-area";
import { Button } from "../../components/ui/button";
import type { InvoiceExpense } from "./invoice-editor";
import { invoiceFieldClass } from "./invoice-line-table";

export function ReviewedExpenseTable({
  expenses,
  selectedIds,
  linkedIds,
  loading,
  busy,
  error,
  markup,
  onMarkup,
  onSelect,
  onRefresh,
  onAdd,
}: {
  readonly expenses: readonly InvoiceExpense[];
  readonly selectedIds: readonly string[];
  readonly linkedIds: ReadonlySet<string>;
  readonly loading: boolean;
  readonly busy: boolean;
  readonly error: string | undefined;
  readonly markup: string;
  readonly onMarkup: (value: string) => void;
  readonly onSelect: (id: string, selected: boolean) => void;
  readonly onRefresh: () => void;
  readonly onAdd: () => void;
}) {
  const [search, setSearch] = useState("");
  const visible = expenses.filter((expense) =>
    expense.description
      .toLocaleLowerCase("en-AU")
      .includes(search.trim().toLocaleLowerCase("en-AU")),
  );
  const selected = expenses.filter(
    (expense) => selectedIds.includes(expense.id) && !linkedIds.has(expense.id),
  );
  const selectedCost = selected.reduce((sum, expense) => sum + expense.subtotalMinor, 0);
  return (
    <section
      className="grid min-w-0 gap-4 border-t border-border pt-6"
      aria-labelledby="reviewed-expenses-heading"
      aria-busy={loading}
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-[16px] font-semibold" id="reviewed-expenses-heading">
          Reviewed expenses
        </h2>
        <Button disabled={busy || loading} onClick={onRefresh} type="button" variant="ghost">
          <RefreshCw aria-hidden="true" className="size-3" />{" "}
          {error ? "Retry expenses" : "Refresh expenses"}
        </Button>
      </div>
      <label className="grid min-w-0 gap-1 text-[13px] font-medium">
        Search reviewed expenses
        <input
          className={`${invoiceFieldClass} max-w-[320px]`}
          onChange={(event) => setSearch(event.target.value)}
          type="search"
          value={search}
        />
      </label>
      {loading ? (
        <p className="text-[14px] text-muted-foreground" role="status">
          Loading reviewed expenses…
        </p>
      ) : null}
      {error ? (
        <p className="text-[14px] text-destructive" role="alert">
          {error}
        </p>
      ) : null}
      <HorizontalScrollArea
        label="Scrollable reviewed expenses"
        className="rounded-[20px] border border-border bg-surface"
      >
        <table
          className="w-full min-w-[540px] border-collapse text-[14px]"
          aria-label="Reviewed expenses"
        >
          <thead className="border-b border-border bg-muted/40 text-left text-[13px] text-muted-foreground">
            <tr>
              <th className="w-12 px-3 py-3 font-medium" scope="col">
                <span className="sr-only">Select</span>
              </th>
              <th className="px-3 py-3 font-medium" scope="col">
                Description
              </th>
              <th className="px-3 py-3 text-right font-medium" scope="col">
                Net cost
              </th>
              <th className="px-3 py-3 text-right font-medium" scope="col">
                Source GST
              </th>
              <th className="px-3 py-3 text-right font-medium" scope="col">
                Gross cost
              </th>
            </tr>
          </thead>
          <tbody>
            {visible.map((expense) => {
              const added = linkedIds.has(expense.id);
              return (
                <tr
                  className={`border-b border-border last:border-0 ${selectedIds.includes(expense.id) ? "bg-primary/5" : ""}`}
                  key={expense.id}
                >
                  <td className="px-3 py-3">
                    <input
                      aria-label={`Select ${expense.description}`}
                      checked={selectedIds.includes(expense.id) && !added}
                      className="focus-ring size-4 accent-primary"
                      disabled={busy || loading || added}
                      onChange={(event) => onSelect(expense.id, event.target.checked)}
                      type="checkbox"
                    />
                  </td>
                  <td className="px-3 py-3">
                    <span>{expense.description}</span>
                    {added ? (
                      <span className="ml-2 rounded border border-border px-1.5 py-0.5 text-[12px] text-muted-foreground">
                        Added
                      </span>
                    ) : null}
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-right tabular-nums">
                    {formatAud(expense.subtotalMinor)}
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-right tabular-nums">
                    {formatAud(expense.gstMinor)}
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-right tabular-nums">
                    {formatAud(expense.subtotalMinor + expense.gstMinor)}
                  </td>
                </tr>
              );
            })}
            {!visible.length && !loading && !error ? (
              <tr>
                <td className="px-3 py-5 text-muted-foreground" colSpan={5}>
                  {expenses.length
                    ? "No reviewed expenses match your search."
                    : "No reviewed expenses yet."}
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </HorizontalScrollArea>
      <div className="flex flex-wrap items-end justify-between gap-3">
        <label className="grid w-[160px] gap-2 text-[13px] font-medium">
          Markup percentage
          <input
            className={`${invoiceFieldClass} text-right tabular-nums`}
            disabled={busy || loading}
            inputMode="decimal"
            onChange={(event) => onMarkup(event.target.value)}
            value={markup}
          />
        </label>
        <div className="flex flex-wrap items-center gap-3">
          <p className="text-[13px] text-muted-foreground" data-testid="selected-expense-summary">
            {selected.length} selected ·{" "}
            <span className="tabular-nums">{formatAud(selectedCost)}</span> net cost
          </p>
          <Button
            disabled={busy || loading || !selected.length}
            onClick={onAdd}
            type="button"
            variant="outline"
          >
            Add selected expenses
          </Button>
        </div>
      </div>
    </section>
  );
}
