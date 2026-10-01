import {
  calculateInvoice,
  formatAud,
  type InvoiceDraftData,
  validateInvoiceDraft,
  validateInvoiceForIssue,
} from "@tammy/invoices/model";
import { assertInvoicePdfReady } from "@tammy/invoices/pdf-policy";
import { Download, Plus, RefreshCw } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useState } from "react";

import { Button } from "../../components/ui/button";
import {
  appendExpenseLines,
  blankLine,
  type EditorDraft,
  type EditorLine,
  editorToDraft,
  type InvoiceExpense,
  toEditor,
} from "./invoice-editor";
import { InvoiceLineTable, invoiceFieldClass } from "./invoice-line-table";
import { ReviewedExpenseTable } from "./reviewed-expense-table";

export type { InvoiceExpense } from "./invoice-editor";

export interface InvoiceRecord {
  readonly id: string;
  readonly version: number;
  readonly state: "draft" | "issued";
  readonly sellerName: string;
  readonly sellerAbn: string;
  readonly draft: InvoiceDraftData;
}

export interface InvoicesApi {
  list(): Promise<readonly InvoiceRecord[]>;
  save(draft: InvoiceDraftData, id?: string, expectedVersion?: number): Promise<InvoiceRecord>;
  issue(id: string, expectedVersion: number): Promise<InvoiceRecord>;
  exportPdf(id: string): Promise<{ readonly saved: boolean }>;
  listExpenses(): Promise<readonly InvoiceExpense[]>;
  getProposal(): Promise<InvoiceDraftData | undefined>;
}

type Replacement = { readonly editor: EditorDraft; readonly record?: InvoiceRecord };

const fieldClass = invoiceFieldClass;
const labelClass = "grid min-w-0 gap-1 text-[10px] font-medium text-foreground";

export function InvoicesScreen({ api }: { readonly api: InvoicesApi }) {
  const [editor, setEditor] = useState<EditorDraft>(newEditor);
  const [baseline, setBaseline] = useState(() => JSON.stringify(editor));
  const [current, setCurrent] = useState<InvoiceRecord>();
  const [records, setRecords] = useState<readonly InvoiceRecord[]>([]);
  const [expenses, setExpenses] = useState<readonly InvoiceExpense[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [selectedExpenseIds, setSelectedExpenseIds] = useState<readonly string[]>([]);
  const [expensesLoading, setExpensesLoading] = useState(true);
  const [expensesError, setExpensesError] = useState<string>();
  const [markup, setMarkup] = useState("0");
  const [replacement, setReplacement] = useState<Replacement>();
  const [confirmIssue, setConfirmIssue] = useState(false);
  const dirty = JSON.stringify(editor) !== baseline;
  const issued = current?.state === "issued";
  const fullyCosted = editor.lines.every((line) => line.unitCost.trim().length > 0);
  const linkedExpenseIds = useMemo(
    () =>
      new Set(
        editor.lines.flatMap((line) => (line.sourceDocumentId ? [line.sourceDocumentId] : [])),
      ),
    [editor.lines],
  );
  const totals = useMemo(() => {
    try {
      return calculateInvoice(editorToDraft(editor));
    } catch {
      return undefined;
    }
  }, [editor]);

  useEffect(() => {
    let active = true;
    setLoading(true);
    void api
      .list()
      .then((value) => {
        if (active) setRecords(value);
      })
      .catch(() => {
        if (active) setError("Saved invoices could not be loaded. Try refreshing.");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    setExpensesLoading(true);
    setExpensesError(undefined);
    void api
      .listExpenses()
      .then((value) => {
        if (active) setExpenses(value);
      })
      .catch(() => {
        if (active)
          setExpensesError(
            "Reviewed expenses could not be loaded. Retry to refresh the list. Manual line items are still available.",
          );
      })
      .finally(() => {
        if (active) setExpensesLoading(false);
      });
    return () => {
      active = false;
    };
  }, [api]);

  useEffect(() => {
    const available = new Set(expenses.map((expense) => expense.id));
    setSelectedExpenseIds((previous) => {
      const next = previous.filter((id) => available.has(id) && !linkedExpenseIds.has(id));
      return next.length === previous.length ? previous : next;
    });
  }, [expenses, linkedExpenseIds]);

  function edit(value: Partial<EditorDraft>) {
    setEditor((previous) => ({ ...previous, ...value }));
    setConfirmIssue(false);
    setNotice(undefined);
  }

  function editLine(key: string, value: Partial<EditorLine>) {
    edit({
      lines: editor.lines.map((line) =>
        line.key === key
          ? { ...line, ...value, ...(line.untouched ? { untouched: false } : {}) }
          : line,
      ),
    });
  }

  function replace(next: Replacement) {
    setEditor(next.editor);
    setCurrent(next.record);
    setBaseline(JSON.stringify(next.editor));
    setReplacement(undefined);
    setConfirmIssue(false);
    setError(undefined);
    setNotice(next.record ? undefined : "Review this draft before saving.");
    setSelectedExpenseIds([]);
  }

  function requestReplacement(next: Replacement) {
    if (dirty) setReplacement(next);
    else replace(next);
  }

  async function loadProposal() {
    setBusy(true);
    setError(undefined);
    try {
      const proposal = await api.getProposal();
      if (proposal) requestReplacement({ editor: toEditor(validateInvoiceDraft(proposal)) });
      else
        setNotice(
          "No MCP draft is waiting. Ask your connected MCP client to propose an invoice first.",
        );
    } catch {
      setError("The MCP draft could not be loaded. Your current invoice is unchanged.");
    } finally {
      setBusy(false);
    }
  }

  async function refresh() {
    setBusy(true);
    setError(undefined);
    try {
      setRecords(await api.list());
    } catch {
      setError("Saved invoices could not be loaded. Try refreshing.");
    } finally {
      setBusy(false);
    }
  }

  async function refreshExpenses() {
    if (expensesLoading || busy) return;
    setExpensesLoading(true);
    setExpensesError(undefined);
    try {
      setExpenses(await api.listExpenses());
    } catch {
      setExpensesError(
        "Reviewed expenses could not be loaded. Retry to refresh the list. Your invoice edits and previous expenses are still here.",
      );
    } finally {
      setExpensesLoading(false);
    }
  }

  function acceptRecord(record: InvoiceRecord) {
    const next = toEditor(record.draft);
    setCurrent(record);
    setEditor(next);
    setBaseline(JSON.stringify(next));
    setRecords((previous) => [record, ...previous.filter((item) => item.id !== record.id)]);
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    if (issued || busy) return;
    setError(undefined);
    setNotice(undefined);
    let draft: InvoiceDraftData;
    try {
      draft = validateInvoiceDraft(editorToDraft(editor));
    } catch (failure) {
      setError(
        failure instanceof Error ? failure.message : "Check the invoice fields before saving.",
      );
      return;
    }
    setBusy(true);
    try {
      acceptRecord(await api.save(draft, current?.id, current?.version));
      setNotice("Draft saved.");
    } catch {
      setError(
        "The draft could not be saved. Your changes are still here. Refresh saved invoices and try again.",
      );
    } finally {
      setBusy(false);
    }
  }

  async function issue() {
    if (!current || dirty || busy || issued) return;
    setBusy(true);
    setError(undefined);
    try {
      acceptRecord(await api.issue(current.id, current.version));
      setConfirmIssue(false);
      setNotice("Invoice issued.");
    } catch {
      setError(
        "The invoice could not be issued. Check the seller, customer, dates and line items, then save and try again.",
      );
    } finally {
      setBusy(false);
    }
  }

  function requestIssue() {
    if (!current || dirty || busy || issued) return;
    setError(undefined);
    try {
      const draft = validateInvoiceForIssue(editorToDraft(editor), {
        name: current.sellerName,
        abn: current.sellerAbn,
      });
      assertInvoicePdfReady(draft, current.sellerName);
      setConfirmIssue(true);
    } catch (failure) {
      setError(
        failure instanceof Error ? failure.message : "Check the invoice fields before issuing.",
      );
    }
  }

  async function exportPdf() {
    if (!current || dirty || busy) return;
    setBusy(true);
    setError(undefined);
    setNotice(undefined);
    try {
      const result = await api.exportPdf(current.id);
      setNotice(result.saved ? "Invoice PDF saved." : "PDF export cancelled. No file was saved.");
    } catch {
      setError("The PDF could not be exported. Your saved invoice is unchanged.");
    } finally {
      setBusy(false);
    }
  }

  function addExpenses() {
    if (busy || issued || expensesLoading) return;
    const selected = expenses.filter((expense) => selectedExpenseIds.includes(expense.id));
    if (!selected.length) return;
    try {
      edit({ lines: appendExpenseLines(editor, selected, markup) });
      setSelectedExpenseIds([]);
      setError(undefined);
      setNotice("Expenses added as billable lines. Review the selling price and GST treatment.");
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Check the expenses and markup.");
    }
  }

  return (
    <div className="mx-auto grid w-full max-w-[1200px] gap-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[18px] font-semibold tracking-[-0.02em] text-foreground">Invoices</h1>
          <p className="mt-1 text-[11px] leading-5 text-muted-foreground">
            Build an AUD invoice from your work, costs and reviewed expenses.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            className="h-8 text-[10px]"
            disabled={busy}
            onClick={loadProposal}
            type="button"
            variant="outline"
          >
            Use MCP draft
          </Button>
          <Button
            className="h-8 text-[10px]"
            disabled={busy}
            onClick={() => requestReplacement({ editor: newEditor() })}
            type="button"
            variant="outline"
          >
            <Plus aria-hidden="true" className="size-3" /> New invoice
          </Button>
        </div>
      </header>

      {replacement ? (
        <div
          className="flex flex-wrap items-center gap-3 rounded-[6px] border border-border bg-surface p-3 text-[11px]"
          role="alert"
        >
          <p className="mr-auto">
            Replace unsaved changes? Your current edits have not been saved.
          </p>
          <Button
            className="h-8 text-[10px]"
            onClick={() => setReplacement(undefined)}
            type="button"
            variant="outline"
          >
            Keep editing
          </Button>
          <Button className="h-8 text-[10px]" onClick={() => replace(replacement)} type="button">
            Discard changes and continue
          </Button>
        </div>
      ) : null}
      {error ? (
        <p className="text-[11px] text-destructive" role="alert">
          {error}
        </p>
      ) : null}
      {notice ? (
        <p className="text-[11px] text-muted-foreground" role="status">
          {notice}
        </p>
      ) : null}

      <div className="grid grid-cols-[minmax(0,1fr)_220px] gap-5 max-[1180px]:grid-cols-1">
        <form className="grid min-w-0 gap-5" onSubmit={save}>
          <fieldset
            className="grid gap-4 rounded-[6px] border border-border bg-surface p-4"
            disabled={busy || issued}
          >
            <legend className="px-1 text-[11px] font-semibold text-foreground">
              {issued ? "Issued invoice · read only" : "Invoice details"}
            </legend>
            {current ? (
              <p className="text-[11px] text-muted-foreground">
                Seller: {current.sellerName} · ABN {current.sellerAbn}
              </p>
            ) : (
              <p className="text-[11px] text-muted-foreground">
                Seller name and ABN come from your signed-in organisation when you save.
              </p>
            )}
            <div className="grid grid-cols-3 gap-3 max-[620px]:grid-cols-1">
              <label className={labelClass}>
                Invoice number
                <input
                  className={fieldClass}
                  maxLength={40}
                  onChange={(event) => edit({ number: event.target.value })}
                  value={editor.number}
                />
              </label>
              <label className={labelClass}>
                Issue date
                <input
                  className={fieldClass}
                  onChange={(event) => edit({ issueDate: event.target.value })}
                  type="date"
                  value={editor.issueDate}
                />
              </label>
              <label className={labelClass}>
                Due date
                <input
                  className={fieldClass}
                  onChange={(event) => edit({ dueDate: event.target.value })}
                  type="date"
                  value={editor.dueDate}
                />
              </label>
            </div>
            <div className="grid grid-cols-2 gap-3 max-[620px]:grid-cols-1">
              <label className={labelClass}>
                Customer name
                <input
                  className={fieldClass}
                  maxLength={200}
                  onChange={(event) => edit({ customerName: event.target.value })}
                  value={editor.customerName}
                />
              </label>
              <label className={labelClass}>
                Customer ABN (optional)
                <input
                  className={fieldClass}
                  inputMode="numeric"
                  maxLength={20}
                  onChange={(event) => edit({ customerAbn: event.target.value })}
                  value={editor.customerAbn ?? ""}
                />
              </label>
              <label className={labelClass}>
                Customer address
                <textarea
                  className={`${fieldClass} h-16 py-2`}
                  maxLength={1000}
                  onChange={(event) => edit({ customerAddress: event.target.value })}
                  value={editor.customerAddress}
                />
              </label>
              <label className={labelClass}>
                Seller address
                <textarea
                  className={`${fieldClass} h-16 py-2`}
                  maxLength={1000}
                  onChange={(event) => edit({ sellerAddress: event.target.value })}
                  value={editor.sellerAddress}
                />
              </label>
            </div>
            <label className="flex items-center gap-2 text-[11px] text-foreground">
              <input
                checked={editor.gstRegistered}
                onChange={(event) =>
                  edit({
                    gstRegistered: event.target.checked,
                    lines: editor.lines.map((line) =>
                      !event.target.checked && line.taxTreatment === "taxable"
                        ? { ...line, taxTreatment: "out-of-scope" }
                        : line,
                    ),
                  })
                }
                type="checkbox"
              />{" "}
              Registered for GST
            </label>
            <p className="text-[10px] leading-5 text-muted-foreground">
              Only enable GST if this business is registered. Selling prices are exclusive of GST;
              review the treatment on each line.
            </p>
          </fieldset>

          <InvoiceLineTable
            busy={busy}
            gstRegistered={editor.gstRegistered}
            lines={editor.lines}
            onAdd={() => edit({ lines: [...editor.lines, blankLine(editor.gstRegistered)] })}
            onEdit={editLine}
            onRemove={(key) => edit({ lines: editor.lines.filter((line) => line.key !== key) })}
            readOnly={Boolean(issued)}
            totals={totals}
          />
          {!issued ? (
            <ReviewedExpenseTable
              busy={busy}
              error={expensesError}
              expenses={expenses}
              linkedIds={linkedExpenseIds}
              loading={expensesLoading}
              markup={markup}
              onAdd={addExpenses}
              onMarkup={setMarkup}
              onRefresh={refreshExpenses}
              onSelect={(id, selected) =>
                setSelectedExpenseIds((previous) =>
                  selected
                    ? [...previous.filter((value) => value !== id), id]
                    : previous.filter((value) => value !== id),
                )
              }
              selectedIds={selectedExpenseIds}
            />
          ) : null}
          <p className="text-[10px] leading-5 text-muted-foreground">
            Unit costs are private invoice estimates. Import uses the expense’s net cost; markup
            changes your selling price. Editing a cost leaves the original reviewed document and
            ledger unchanged.
          </p>

          <fieldset
            className="grid grid-cols-2 gap-3 max-[620px]:grid-cols-1"
            disabled={busy || issued}
          >
            <label className={labelClass}>
              Notes
              <textarea
                className={`${fieldClass} h-20 py-2`}
                maxLength={2000}
                onChange={(event) => edit({ notes: event.target.value })}
                value={editor.notes}
              />
            </label>
            <label className={labelClass}>
              Payment instructions
              <textarea
                className={`${fieldClass} h-20 py-2`}
                maxLength={1000}
                onChange={(event) => edit({ paymentInstructions: event.target.value })}
                value={editor.paymentInstructions}
              />
            </label>
          </fieldset>

          {confirmIssue ? (
            <div
              className="grid gap-3 rounded-[6px] border border-border bg-surface p-3 text-[11px]"
              role="alert"
            >
              <p>
                Issuing locks this invoice and removes the draft watermark. It does not post to the
                ledger or send it to your customer.
              </p>
              <div className="flex gap-2">
                <Button className="h-8 text-[10px]" disabled={busy} onClick={issue} type="button">
                  Confirm issue
                </Button>
                <Button
                  className="h-8 text-[10px]"
                  disabled={busy}
                  onClick={() => setConfirmIssue(false)}
                  type="button"
                  variant="outline"
                >
                  Keep as draft
                </Button>
              </div>
            </div>
          ) : null}
          <div className="flex flex-wrap items-center gap-2 border-t border-border pt-4">
            {!issued ? (
              <Button className="h-8 text-[11px]" disabled={busy} type="submit">
                {busy ? "Working…" : "Save draft"}
              </Button>
            ) : null}
            <Button
              className="h-8 text-[10px]"
              disabled={!current || dirty || busy}
              onClick={exportPdf}
              type="button"
              variant="outline"
            >
              <Download aria-hidden="true" className="size-3" /> Export PDF
            </Button>
            {!issued ? (
              <Button
                className="h-8 text-[10px]"
                disabled={!current || dirty || busy}
                onClick={requestIssue}
                type="button"
                variant="outline"
              >
                Issue invoice
              </Button>
            ) : null}
            <p className="text-[10px] text-muted-foreground">
              {dirty && current
                ? "Save changes before exporting or issuing."
                : issued
                  ? "Issued invoice · immutable"
                  : "Draft PDFs are watermarked DRAFT."}
            </p>
          </div>
        </form>

        <aside className="grid content-start gap-5">
          <section
            className="grid gap-3 rounded-[6px] border border-border bg-surface p-4"
            aria-label="Invoice totals"
          >
            <h2 className="text-[11px] font-semibold">Totals · AUD</h2>
            <MoneyRow label="Subtotal" minor={totals?.subtotalMinor} testId="invoice-subtotal" />
            <MoneyRow label="GST" minor={totals?.gstMinor} testId="invoice-gst" />
            <div className="border-t border-border pt-3">
              <MoneyRow
                label="Total due"
                minor={totals?.totalMinor}
                strong
                testId="invoice-total"
              />
            </div>
            {!totals ? (
              <p className="text-[10px] text-muted-foreground">
                Enter valid quantities and amounts to calculate totals.
              </p>
            ) : null}
            <div className="grid gap-2 border-t border-border pt-3">
              <h3 className="text-[10px] font-semibold">Internal · never on the PDF</h3>
              <MoneyRow
                label={fullyCosted ? "Private cost" : "Known private cost"}
                minor={totals?.costMinor}
                testId="invoice-cost"
              />
              <MoneyRow
                label="Profit (ex GST)"
                minor={fullyCosted ? totals?.marginMinor : undefined}
                testId="invoice-margin"
              />
              {!fullyCosted ? (
                <p className="text-[10px] leading-5 text-muted-foreground">
                  Add a cost to every line to calculate profit. Enter 0 only for a known zero cost.
                </p>
              ) : null}
            </div>
          </section>
          <section className="grid gap-3" aria-label="Saved invoices" aria-busy={loading}>
            <div className="flex items-center justify-between">
              <h2 className="text-[11px] font-semibold">Saved invoices</h2>
              <Button
                aria-label="Refresh saved invoices"
                className="h-7 px-2"
                disabled={busy || loading}
                onClick={refresh}
                type="button"
                variant="ghost"
              >
                <RefreshCw aria-hidden="true" className="size-3" />
              </Button>
            </div>
            {loading ? (
              <p className="text-[11px] text-muted-foreground">Loading invoices…</p>
            ) : records.length ? (
              <ul className="grid gap-2">
                {records.map((record) => (
                  <li key={record.id}>
                    <button
                      aria-label={`Open ${record.draft.number || "unnumbered invoice"}`}
                      className="focus-ring grid w-full gap-1 rounded-[4px] border border-border bg-surface px-3 py-2 text-left disabled:opacity-50"
                      disabled={busy}
                      onClick={() => requestReplacement({ editor: toEditor(record.draft), record })}
                      type="button"
                    >
                      <span className="flex justify-between gap-2 text-[11px] font-medium">
                        <span>{record.draft.number || "Unnumbered"}</span>
                        <span className="text-[9px] uppercase text-muted-foreground">
                          {record.state}
                        </span>
                      </span>
                      <span className="truncate text-[10px] text-muted-foreground">
                        {record.draft.customerName || "Customer not set"}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-[11px] text-muted-foreground">No saved invoices yet.</p>
            )}
          </section>
        </aside>
      </div>
    </div>
  );
}

function MoneyRow({
  label,
  minor,
  strong = false,
  testId,
}: {
  readonly label: string;
  readonly minor: number | undefined;
  readonly strong?: boolean;
  readonly testId: string;
}) {
  return (
    <p
      className={`flex items-baseline justify-between gap-3 text-[11px] ${strong ? "font-semibold text-foreground" : "text-muted-foreground"}`}
      data-testid={testId}
    >
      <span>{label}</span>
      <span className="tabular-nums">{minor === undefined ? "—" : formatAud(minor)}</span>
    </p>
  );
}

function newEditor(): EditorDraft {
  const issueDate = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Australia/Melbourne",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());
  const due = new Date(`${issueDate}T12:00:00Z`);
  due.setUTCDate(due.getUTCDate() + 14);
  return {
    number: "",
    issueDate,
    dueDate: due.toISOString().slice(0, 10),
    customerName: "",
    customerAddress: "",
    customerAbn: "",
    sellerAddress: "",
    gstRegistered: false,
    notes: "",
    paymentInstructions: "",
    lines: [blankLine(false)],
  };
}
