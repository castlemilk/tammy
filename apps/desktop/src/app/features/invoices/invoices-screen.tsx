import {
  calculateInvoice,
  formatAud,
  type InvoiceDraftData,
  validateInvoiceDraft,
  validateInvoiceForIssue,
} from "@tammy/invoices/model";
import { assertInvoicePdfReady } from "@tammy/invoices/pdf-policy";
import { Download, Plus } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import type {
  CompanyInvoiceTemplate,
  InvoiceLogoPreview,
  InvoiceTemplateDefaults,
} from "../../../shared/invoice-template";
import { WorkNotice } from "../../components/review-workspace";
import { Button } from "../../components/ui/button";
import { type InvoiceAccountingApi, InvoiceAccountingPanel } from "./invoice-accounting-panel";
import {
  appendExpenseLines,
  applyInvoiceTemplate,
  blankLine,
  type EditorDraft,
  type EditorLine,
  editorToDraft,
  type InvoiceExpense,
  toEditor,
} from "./invoice-editor";
import { InvoiceLineTable, invoiceFieldClass } from "./invoice-line-table";
import { InvoiceLogo } from "./invoice-logo";
import { InvoiceRecordList } from "./invoice-record-list";
import { InvoiceTemplatePanel } from "./invoice-template-panel";
import { ReviewedExpenseTable } from "./reviewed-expense-table";

export type { InvoiceExpense } from "./invoice-editor";

export interface InvoiceRecord {
  readonly id: string;
  readonly version: number;
  readonly state: "draft" | "issued";
  readonly sellerName: string;
  readonly sellerAbn: string;
  readonly draft: InvoiceDraftData;
  readonly logoAssetId?: string;
}

export interface InvoicesApi {
  readonly accounting?: InvoiceAccountingApi;
  list(): Promise<readonly InvoiceRecord[]>;
  save(
    draft: InvoiceDraftData,
    id?: string,
    expectedVersion?: number,
    logoAssetId?: string,
  ): Promise<InvoiceRecord>;
  issue(id: string, expectedVersion: number): Promise<InvoiceRecord>;
  exportPdf(id: string): Promise<{ readonly saved: boolean }>;
  listExpenses(): Promise<readonly InvoiceExpense[]>;
  getProposal(): Promise<InvoiceDraftData | undefined>;
  getTemplate(): Promise<CompanyInvoiceTemplate>;
  saveTemplate(
    defaults: InvoiceTemplateDefaults,
    expectedVersion: number,
  ): Promise<CompanyInvoiceTemplate>;
  uploadLogo(file: File): Promise<string>;
  getLogo(id: string): Promise<InvoiceLogoPreview>;
}

type Replacement = {
  readonly editor: EditorDraft;
  readonly record?: InvoiceRecord;
  readonly logoAssetId?: string;
};

const fieldClass = invoiceFieldClass;
const labelClass = "grid min-w-0 gap-2 text-[14px] font-medium text-foreground";

export function InvoicesScreen({ api }: { readonly api: InvoicesApi }) {
  const [editor, setEditor] = useState<EditorDraft>(newEditor);
  const [logoAssetId, setLogoAssetId] = useState<string>();
  const [baseline, setBaseline] = useState(() => snapshot(editor));
  const untouchedInitialDraft = useRef(true);
  const [template, setTemplate] = useState<CompanyInvoiceTemplate>();
  const [templateLoading, setTemplateLoading] = useState(true);
  const [templateError, setTemplateError] = useState<string>();
  const [templateAttempt, setTemplateAttempt] = useState(0);
  const [current, setCurrent] = useState<InvoiceRecord>();
  const [records, setRecords] = useState<readonly InvoiceRecord[]>([]);
  const [recordsError, setRecordsError] = useState<string>();
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
  const dirty = snapshot(editor, logoAssetId) !== baseline;
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
    setRecordsError(undefined);
    void api
      .list()
      .then((value) => {
        if (active) setRecords(value);
      })
      .catch(() => {
        if (active) setRecordsError("Saved invoices could not be loaded. Try refreshing.");
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

  // biome-ignore lint/correctness/useExhaustiveDependencies: The retry counter deliberately reloads failed defaults.
  useEffect(() => {
    let active = true;
    setTemplateLoading(true);
    setTemplateError(undefined);
    void api
      .getTemplate()
      .then((value) => {
        if (!active) return;
        setTemplate(value);
        if (untouchedInitialDraft.current) {
          untouchedInitialDraft.current = false;
          const next = applyInvoiceTemplate(newEditor(), value.defaults);
          setEditor(next);
          setLogoAssetId(value.defaults.logoAssetId);
          setBaseline(snapshot(next, value.defaults.logoAssetId));
        }
      })
      .catch(() => {
        if (active)
          setTemplateError(
            "Company defaults could not be loaded. Your invoice edits are unchanged.",
          );
      })
      .finally(() => {
        if (active) setTemplateLoading(false);
      });
    return () => {
      active = false;
    };
  }, [api, templateAttempt]);

  useEffect(() => {
    const available = new Set(expenses.map((expense) => expense.id));
    setSelectedExpenseIds((previous) => {
      const next = previous.filter((id) => available.has(id) && !linkedExpenseIds.has(id));
      return next.length === previous.length ? previous : next;
    });
  }, [expenses, linkedExpenseIds]);

  function edit(value: Partial<EditorDraft>) {
    untouchedInitialDraft.current = false;
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
    untouchedInitialDraft.current = false;
    setEditor(next.editor);
    setLogoAssetId(next.logoAssetId ?? next.record?.logoAssetId);
    setCurrent(next.record);
    setBaseline(snapshot(next.editor, next.logoAssetId ?? next.record?.logoAssetId));
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
    untouchedInitialDraft.current = false;
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
    setRecordsError(undefined);
    try {
      setRecords(await api.list());
    } catch {
      setRecordsError("Saved invoices could not be loaded. Try refreshing.");
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
    untouchedInitialDraft.current = false;
    const next = toEditor(record.draft);
    setCurrent(record);
    setEditor(next);
    setLogoAssetId(record.logoAssetId);
    setBaseline(snapshot(next, record.logoAssetId));
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
      acceptRecord(await api.save(draft, current?.id, current?.version, logoAssetId ?? ""));
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

  function applyTemplate() {
    if (!template || busy || issued) return;
    try {
      edit(applyInvoiceTemplate(editor, template.defaults));
      setLogoAssetId(template.defaults.logoAssetId);
      setError(undefined);
      setNotice("Saved company template applied. Review this draft before saving.");
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : "Check the issue date before applying the template.",
      );
    }
  }

  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="work-eyebrow mb-2">Sales workspace</p>
          <h1 className="page-title">Invoices</h1>
          <p className="mt-2 max-w-[620px] text-[14px] leading-6 text-muted-foreground">
            Bring your work into a clear invoice. Review the details, issue it, then record receipts
            as they arrive.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button disabled={busy} onClick={loadProposal} type="button" variant="outline">
            Use MCP draft
          </Button>
          <Button
            disabled={busy}
            onClick={() =>
              requestReplacement({
                editor: template
                  ? applyInvoiceTemplate(newEditor(), template.defaults)
                  : newEditor(),
                ...(template?.defaults.logoAssetId
                  ? { logoAssetId: template.defaults.logoAssetId }
                  : {}),
              })
            }
            type="button"
            variant="outline"
          >
            <Plus aria-hidden="true" className="size-4" /> New invoice
          </Button>
        </div>
      </header>

      {replacement ? (
        <div
          className="flex flex-wrap items-center gap-3 rounded-[20px] border border-border bg-surface p-3 text-[14px]"
          role="alert"
        >
          <p className="mr-auto">
            Replace unsaved changes? Your current edits have not been saved.
          </p>
          <Button onClick={() => setReplacement(undefined)} type="button" variant="outline">
            Keep editing
          </Button>
          <Button onClick={() => replace(replacement)} type="button">
            Discard changes and continue
          </Button>
        </div>
      ) : null}
      {error ? <WorkNotice>{error}</WorkNotice> : null}
      {notice ? <WorkNotice success>{notice}</WorkNotice> : null}

      <InvoiceRecordList
        records={records}
        currentId={current?.id}
        loading={loading}
        error={recordsError}
        busy={busy}
        onRefresh={refresh}
        onOpen={(record) => requestReplacement({ editor: toEditor(record.draft), record })}
      />
      <div className="grid min-w-0 gap-6">
        <div className="grid min-w-0 content-start gap-5">
          {issued && current && api.accounting && totals ? (
            <InvoiceAccountingPanel
              api={api.accounting}
              gstMinor={totals.gstMinor}
              incomeMinor={totals.subtotalMinor}
              invoiceId={current.id}
              invoiceVersion={current.version}
              issueDate={current.draft.issueDate}
              key={current.id}
            />
          ) : null}
          <form className="work-surface grid min-w-0 gap-6 p-5 sm:p-6" onSubmit={save}>
            <fieldset className="grid min-w-0 gap-5" disabled={busy || issued}>
              <legend className="mb-4 text-[16px] font-semibold text-foreground">
                {issued ? "Issued invoice · read only" : "Invoice details"}
              </legend>
              {current ? (
                <p className="text-[14px] text-muted-foreground">
                  Seller: {current.sellerName} · ABN {current.sellerAbn}
                </p>
              ) : (
                <p className="text-[14px] text-muted-foreground">
                  Seller name and ABN come from your signed-in organisation when you save.
                </p>
              )}
              {logoAssetId ? (
                <div className="flex flex-wrap items-center gap-4">
                  <InvoiceLogo api={api} id={logoAssetId} label="Invoice logo" />
                  {!issued ? (
                    <Button
                      onClick={() => {
                        untouchedInitialDraft.current = false;
                        setLogoAssetId(undefined);
                        setConfirmIssue(false);
                        setNotice(undefined);
                      }}
                      type="button"
                      variant="outline"
                    >
                      Remove invoice logo
                    </Button>
                  ) : null}
                </div>
              ) : null}
              <div className="grid grid-cols-3 gap-4 max-[620px]:grid-cols-1">
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
              <div className="grid grid-cols-2 gap-4 max-[620px]:grid-cols-1">
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
                    className={`${fieldClass} min-h-24 max-h-80 field-sizing-content resize-none py-3`}
                    maxLength={1000}
                    onChange={(event) => edit({ customerAddress: event.target.value })}
                    value={editor.customerAddress}
                  />
                </label>
                <label className={labelClass}>
                  Seller address
                  <textarea
                    className={`${fieldClass} min-h-24 max-h-80 field-sizing-content resize-none py-3`}
                    maxLength={1000}
                    onChange={(event) => edit({ sellerAddress: event.target.value })}
                    value={editor.sellerAddress}
                  />
                </label>
              </div>
              <label className="flex min-h-11 items-center gap-3 text-[14px] text-foreground">
                <input
                  checked={editor.gstRegistered}
                  className="focus-ring size-4 accent-primary"
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
              <p className="text-[13px] leading-5 text-muted-foreground">
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
            <p className="text-[13px] leading-5 text-muted-foreground">
              Unit costs are private invoice estimates. Import uses the expense’s net cost; markup
              changes your selling price. Editing a cost leaves the original reviewed document and
              ledger unchanged.
            </p>

            <InvoiceTemplatePanel
              api={api}
              canApply={!issued}
              disabled={busy}
              error={templateError}
              loading={templateLoading}
              onApply={applyTemplate}
              onRetry={() => setTemplateAttempt((value) => value + 1)}
              onSaved={setTemplate}
              template={template}
            />

            <fieldset
              className="grid grid-cols-2 gap-4 max-[620px]:grid-cols-1"
              disabled={busy || issued}
            >
              <label className={labelClass}>
                Notes
                <textarea
                  className={`${fieldClass} min-h-24 max-h-80 field-sizing-content resize-none py-3`}
                  maxLength={2000}
                  onChange={(event) => edit({ notes: event.target.value })}
                  value={editor.notes}
                />
              </label>
              <label className={labelClass}>
                Payment instructions
                <textarea
                  className={`${fieldClass} min-h-24 max-h-80 field-sizing-content resize-none py-3`}
                  maxLength={1000}
                  onChange={(event) => edit({ paymentInstructions: event.target.value })}
                  value={editor.paymentInstructions}
                />
              </label>
            </fieldset>

            <section
              className="grid gap-6 border-t border-border pt-6 md:grid-cols-2"
              aria-label="Invoice totals"
            >
              <div className="grid content-start gap-3 rounded-2xl bg-muted/40 p-4">
                <h2 className="text-[14px] font-semibold">Internal · never on the PDF</h2>
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
                  <p className="text-[13px] leading-5 text-muted-foreground">
                    Add a cost to every line to calculate profit. Enter 0 only for a known zero
                    cost.
                  </p>
                ) : null}
              </div>
              <div className="grid content-start gap-3 p-4">
                <h2 className="text-[14px] font-semibold">Invoice totals · AUD</h2>
                <MoneyRow
                  label="Subtotal"
                  minor={totals?.subtotalMinor}
                  testId="invoice-subtotal"
                />
                <MoneyRow label="GST" minor={totals?.gstMinor} testId="invoice-gst" />
                <div className="border-t border-border pt-3">
                  <MoneyRow
                    label="Invoice total"
                    minor={totals?.totalMinor}
                    strong
                    testId="invoice-total"
                  />
                </div>
                {!totals ? (
                  <p className="text-[13px] leading-5 text-muted-foreground">
                    Enter valid quantities and amounts to calculate totals.
                  </p>
                ) : null}
              </div>
            </section>

            {confirmIssue ? (
              <div
                className="grid gap-3 rounded-[20px] border border-border bg-surface p-3 text-[14px]"
                role="alert"
              >
                <p>
                  Issuing locks this invoice, removes the draft watermark and records sales, GST and
                  the amount owing in the business ledger. Record actual receipts separately. Your
                  customer does not receive it automatically.
                </p>
                <div className="flex flex-wrap gap-2">
                  <Button disabled={busy} onClick={issue} type="button">
                    Confirm issue
                  </Button>
                  <Button
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
                <Button disabled={busy} type="submit">
                  {busy ? "Working…" : "Save draft"}
                </Button>
              ) : null}
              <Button
                disabled={!current || dirty || busy}
                onClick={exportPdf}
                type="button"
                variant="outline"
              >
                <Download aria-hidden="true" className="size-3" /> Export PDF
              </Button>
              {!issued ? (
                <Button
                  disabled={!current || dirty || busy}
                  onClick={requestIssue}
                  type="button"
                  variant="outline"
                >
                  Issue invoice
                </Button>
              ) : null}
              <p className="text-[13px] text-muted-foreground">
                {dirty && current
                  ? "Save changes before exporting or issuing."
                  : issued
                    ? "Issued invoice · immutable"
                    : "Draft PDFs are watermarked DRAFT."}
              </p>
            </div>
          </form>
        </div>
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
      className={`flex items-baseline justify-between gap-3 text-[14px] ${strong ? "text-[20px] font-semibold text-foreground" : "text-muted-foreground"}`}
      data-testid={testId}
    >
      <span>{label}</span>
      <span className="whitespace-nowrap tabular-nums">
        {minor === undefined ? "—" : formatAud(minor)}
      </span>
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

function snapshot(editor: EditorDraft, logoAssetId?: string): string {
  return JSON.stringify({ editor, logoAssetId });
}
