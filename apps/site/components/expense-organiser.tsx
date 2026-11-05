"use client";

import { useRef, useState, type ChangeEvent, type FormEvent } from "react";
import { aud } from "../lib/tools/calculations";
import {
  EXPENSE_CATEGORIES, GST_EVIDENCE, createExpenseRecord, exportExpenseCSV,
  exportExpenseDraft, importExpenseDraft, summariseExpenseRecords,
  type ExpenseCategory, type ExpenseRecord, type GstEvidence,
} from "../lib/tools/expense-records";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "./ui/card";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Select } from "./ui/select";
import { Textarea } from "./ui/textarea";
import { GammaEmptyState } from "./gamma/empty-state";

const MAX_ATTACHMENT_SIZE = 5 * 1024 * 1024;
const ACCEPTED_TYPES = new Set(["application/pdf", "image/jpeg", "image/png"]);

function saveFile(name: string, value: string, type: string) {
  const url = URL.createObjectURL(new Blob([value], { type }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export function ExpenseOrganiser() {
  const [records, setRecords] = useState<ExpenseRecord[]>([]);
  const [attachments, setAttachments] = useState<Map<string, File>>(() => new Map());
  const [date, setDate] = useState("");
  const [supplier, setSupplier] = useState("");
  const [description, setDescription] = useState("");
  const [category, setCategory] = useState<ExpenseCategory>("Supplies");
  const [amount, setAmount] = useState("");
  const [businessUsePercent, setBusinessUsePercent] = useState("100");
  const [gstEvidence, setGstEvidence] = useState<GstEvidence>("Review treatment");
  const [receipt, setReceipt] = useState<File | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const sequence = useRef(0);
  const summary = summariseExpenseRecords(records);

  function pickReceipt(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0] ?? null;
    event.target.value = "";
    if (!file) return;
    if (!ACCEPTED_TYPES.has(file.type) || file.size > MAX_ATTACHMENT_SIZE || file.size === 0) {
      setReceipt(null);
      setError("Choose a non-empty PDF, JPEG or PNG receipt no larger than 5 MB.");
      return;
    }
    if (file.name.length > 180) {
      setReceipt(null);
      setError("Receipt filename is too long.");
      return;
    }
    setReceipt(file);
    setError("");
  }

  function addRecord(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    try {
      const next = createExpenseRecord({
        id: "expense-" + Date.now() + "-" + ++sequence.current,
        date, supplier, description, category, amount, gstEvidence,
        businessUsePercent: Number(businessUsePercent),
        sourceFileName: receipt?.name ?? "",
      });
      if (records.length >= 100) throw new Error("Limit 100 expenses per worksheet.");
      setRecords((current) => [...current, next]);
      if (receipt) setAttachments((current) => new Map(current).set(next.id, receipt));
      setSupplier("");
      setDescription("");
      setAmount("");
      setReceipt(null);
      setError("");
      setNotice("Expense recorded locally. No accounting entry was posted.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Check the expense details.");
    }
  }

  function removeRecord(id: string) {
    setRecords((current) => current.filter((item) => item.id !== id));
    setAttachments((current) => {
      const next = new Map(current);
      next.delete(id);
      return next;
    });
    setNotice("Record removed from this browser session.");
  }

  function exportCsv() {
    try {
      saveFile("tammy-expense-register.csv", exportExpenseCSV(records), "text/csv;charset=utf-8");
      setError("");
      setNotice("CSV prepared locally. Keep original receipt documents alongside it.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not prepare CSV.");
    }
  }

  function exportJson() {
    try {
      saveFile("tammy-expense-worksheet.json", exportExpenseDraft(records), "application/json");
      setError("");
      setNotice("Worksheet JSON saved to your device. It includes merchant details, but not receipt files and is not encrypted.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not prepare worksheet.");
    }
  }

  async function openJson(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    if (file.size > 2_000_000) {
      setError("Worksheet JSON must be smaller than 2 MB.");
      return;
    }
    if (records.length && !window.confirm("Replace all current rows? A saved JSON file does not include receipt attachments.")) {
      return;
    }
    try {
      const next = importExpenseDraft(await file.text());
      setRecords(next);
      setAttachments(new Map());
      setReceipt(null);
      setError("");
      setNotice("Worksheet imported. Source receipt files were not included. Recheck original documents.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not import worksheet.");
    }
  }

  function downloadSource(id: string) {
    const file = attachments.get(id);
    if (!file) return;
    const url = URL.createObjectURL(file);
    const link = document.createElement("a");
    link.href = url;
    link.download = file.name;
    document.body.appendChild(link);
    link.click();
    link.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  return <div className="mt-10 grid gap-7 lg:grid-cols-[minmax(0,0.92fr)_minmax(0,1.08fr)]">
    <Card className="self-start gap-5 rounded-xl border-border bg-card py-7">
      <CardHeader className="px-6 pt-0">
        <p className="tools-overline">Manual expense capture</p>
        <CardTitle className="text-3xl font-medium tracking-tight">Add an expense</CardTitle>
        <CardDescription>Enter what the receipt actually shows. Categories and GST treatment are provisional until reviewed.</CardDescription>
      </CardHeader>
      <CardContent className="px-6">
        <form onSubmit={addRecord} className="grid gap-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="expense-date">Transaction date *</Label>
              <Input id="expense-date" type="date" required value={date} onChange={(e) => setDate(e.target.value)}/>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="expense-amount">Paid amount (AUD, gross) *</Label>
              <Input id="expense-amount" inputMode="decimal" required placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)}/>
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="expense-supplier">Supplier *</Label>
            <Input id="expense-supplier" required maxLength={180} value={supplier} onChange={(e) => setSupplier(e.target.value)} placeholder="Business shown on receipt"/>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="expense-description">Purpose or note</Label>
            <Textarea id="expense-description" maxLength={500} value={description} onChange={(e) => setDescription(e.target.value)} rows={2} placeholder="What was purchased?"/>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="expense-category">Record category</Label>
              <Select id="expense-category" value={category} onChange={(e) => setCategory(e.target.value as ExpenseCategory)}>
                {EXPENSE_CATEGORIES.map((item) => <option value={item} key={item}>{item}</option>)}
              </Select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="expense-use">Estimated business use (%)</Label>
              <Input id="expense-use" inputMode="numeric" type="number" min={0} max={100} step={1} value={businessUsePercent} onChange={(e) => setBusinessUsePercent(e.target.value)}/>
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="expense-gst">GST evidence on the document</Label>
            <Select id="expense-gst" value={gstEvidence} onChange={(e) => setGstEvidence(e.target.value as GstEvidence)}>
              {GST_EVIDENCE.map((item) => <option value={item} key={item}>{item}</option>)}
            </Select>
            <p className="text-xs leading-relaxed text-muted-foreground">This describes a document; it does not determine whether you can claim GST credits.</p>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="expense-receipt">Receipt PDF, JPG or PNG (optional)</Label>
            <Input id="expense-receipt" type="file" accept=".pdf,.jpg,.jpeg,.png,application/pdf,image/jpeg,image/png" onChange={pickReceipt} className="cursor-pointer"/>
            <p className="text-xs text-muted-foreground">{receipt ? "Attached for this session: " + receipt.name : "Max 5 MB. Files stay in browser memory; exports do not include them."}</p>
          </div>
          <Button type="submit" disabled={records.length >= 100}>Add to worksheet</Button>
        </form>
      </CardContent>
    </Card>
    <div className="min-w-0 space-y-5">
      <Card className="gap-3 rounded-xl border-border bg-secondary py-6">
        <CardHeader className="px-6 pt-0">
          <p className="tools-overline">Working summary</p>
          <CardTitle className="text-3xl font-medium tracking-tight">{aud(summary.grossCents)}</CardTitle>
          <CardDescription>Gross payments across {summary.count} manually entered expense {summary.count === 1 ? "row" : "rows"}.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 px-6 sm:grid-cols-2">
          <div><p className="text-xs text-muted-foreground">Allocated business-use portion</p><strong className="text-xl tabular-nums">{aud(summary.businessPortionCents)}</strong></div>
          <div><p className="text-xs text-muted-foreground">Standard GST component indicated</p><strong className="text-xl tabular-nums">{aud(summary.standardGstIndicationCents)}</strong></div>
          <p className="col-span-full text-xs leading-relaxed text-muted-foreground">These are user-classified estimates, not deductible amounts or eligible GST credits. Expenses may require special treatment, apportionment or source checks.</p>
        </CardContent>
      </Card>

      <Card className="gap-4 rounded-xl border-border bg-card py-6">
        <CardHeader className="px-6 pt-0">
          <CardTitle className="text-2xl font-medium tracking-tight">Working register</CardTitle>
          <CardDescription>Review entries, export, or reopen a previously saved worksheet. Nothing is stored automatically.</CardDescription>
        </CardHeader>
        <CardContent className="px-6">
          {records.length === 0
            ? <GammaEmptyState title="No expenses recorded yet" description="Add a receipt row to create a private working register.">
                <span className="text-xs text-muted-foreground">No account, sync or automatic expense posting.</span>
              </GammaEmptyState>
            : <div className="max-w-full overflow-x-auto">
                <table className="w-full min-w-[490px] border-collapse text-sm">
                  <caption className="pb-3 text-left text-xs text-muted-foreground">Working expenses, AUD gross. All categories and tax labels require review.</caption>
                  <thead><tr className="border-b border-border text-left text-xs text-muted-foreground">
                    <th scope="col" className="pb-3 pr-3">Date / supplier</th>
                    <th scope="col" className="pb-3 pr-3">Purpose / evidence</th>
                    <th scope="col" className="pb-3 pr-3 text-right">Gross</th>
                    <th scope="col" className="pb-3 text-right">Action</th>
                  </tr></thead>
                  <tbody>{records.map((row) => <tr key={row.id} className="border-b border-border align-top">
                    <th scope="row" className="py-4 pr-3 text-left font-medium">
                      <span className="block tabular-nums text-muted-foreground">{row.date}</span>
                      <span className="block">{row.supplier}</span>
                    </th>
                    <td className="py-4 pr-3">
                      <span className="block">{row.description || row.category}</span>
                      <span className="block text-xs text-muted-foreground">{row.category} · {row.gstEvidence}</span>
                      {attachments.has(row.id)
                        ? <Button variant="ghost" size="sm" className="mt-1 px-0" onClick={() => downloadSource(row.id)}>Download attached receipt</Button>
                        : row.sourceFileName
                        ? <span className="block text-xs text-muted-foreground">Reference only: {row.sourceFileName}</span>
                        : null}
                    </td>
                    <td className="py-4 pr-3 text-right tabular-nums">{aud(row.grossCents)}</td>
                    <td className="py-3 text-right"><Button variant="ghost" size="sm" aria-label={"Remove expense from " + row.supplier} onClick={() => removeRecord(row.id)}>Remove</Button></td>
                  </tr>)}</tbody>
                </table>
              </div>}
          <div className="mt-6 flex flex-wrap gap-3">
            <Button onClick={exportCsv} disabled={!records.length}>Export CSV</Button>
            <Button onClick={exportJson} variant="outline" disabled={!records.length}>Save worksheet JSON</Button>
          </div>
          <div className="mt-6 grid gap-2">
            <Label htmlFor="expense-import">Reopen saved worksheet JSON</Label>
            <Input id="expense-import" type="file" accept=".json,application/json" onChange={(event) => void openJson(event)} className="cursor-pointer"/>
          </div>
          {error && <p role="alert" className="mt-4 text-sm font-semibold text-red-800">{error}</p>}
          {notice && <p role="status" className="mt-4 text-sm text-primary">{notice}</p>}
          <p className="mt-4 text-xs leading-relaxed text-muted-foreground">Exports include supplier details and are not encrypted. Keep them private. Receipt files are not bundled with CSV or JSON exports; retain original files separately. Reloading the page clears this worksheet.</p>
        </CardContent>
      </Card>
    </div>
  </div>;
}
