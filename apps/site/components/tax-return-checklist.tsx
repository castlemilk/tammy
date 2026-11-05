"use client";
import { useState } from "react";
import { Button } from "./ui/button";
import { Card } from "./ui/card";
import { Checkbox } from "./ui/checkbox";
import { Label } from "./ui/label";
import { Select } from "./ui/select";

const tasks = [
  { id: "tax-year", title: "Confirm the income year and who is lodging", detail: "A sole trader generally includes business income in an individual tax return. Companies use their own income-tax return process." },
  { id: "income", title: "Reconcile all business income sources", detail: "Check invoices, cash sales, refunds, platform payments and bank receipts. Keep loan proceeds and owner transfers distinct from sales." },
  { id: "expenses", title: "Review deductible expenses and private use", detail: "Match expense categories to receipts, review business-use percentages, and separate assets/depreciation items as required." },
  { id: "gst", title: "Check GST amounts and any BAS records", detail: "Where registered for GST, use the correct income-tax treatment of GST amounts and reconcile GST workpapers to the records." },
  { id: "other", title: "Review other personal income and deductions", detail: "Employment, interest, dividends and other applicable income are separate from the business schedule; verify prefilled information." },
  { id: "payg", title: "Check PAYG instalments and prior adjustments", detail: "Review tax payments/instalments, prior losses and any business asset disposal or carry-forward items that apply." },
  { id: "docs", title: "Keep evidence and backup or export your work", detail: "Retain invoices, bank statements, workpapers and receipts. Preserve enough records to explain each figure if queried." },
  { id: "review", title: "Review the complete return in the ATO service", detail: "Confirm all relevant myTax fields and the ATO's final review screens before choosing Lodge. A checklist does not submit anything." },
] as const;

export function TaxReturnChecklist() {
  const [checked, setChecked] = useState<readonly string[]>([]);
  const [year, setYear] = useState("2025–26");
  function toggle(id: string, on: boolean) {
    setChecked((previous) => on ? [...previous.filter((x) => x !== id), id] : previous.filter((x) => x !== id));
  }
  return <Card className="tools-checker" aria-labelledby="sole-trader-checklist-heading">
    <div className="tools-checker-header">
      <div><p className="tools-overline">Local reading checklist</p><h2 id="sole-trader-checklist-heading">Prepare the records you need.</h2></div>
      <div className="flex min-w-[190px] flex-col gap-2">
        <Label htmlFor="sole-trader-income-year">Income year</Label>
        <Select id="sole-trader-income-year" value={year} onChange={(event) => setYear(event.target.value)}>
          <option>2025–26</option><option>2024–25</option><option>2026–27</option>
        </Select>
      </div>
    </div>
    <p className="tools-help">This is for a sole trader preparing their own individual return, not a company tax return or a BAS. The selected year is only a checklist label; it does not change tax rates or calculate tax.</p>
    <output className="tools-checker-progress" aria-live="polite">{checked.length} of {tasks.length} checks reviewed · {year}</output>
    <div className="tools-checker-items">
      {tasks.map((task) => <Label className="tools-checker-item" key={task.id} htmlFor={"tax-check-" + task.id}>
        <Checkbox id={"tax-check-" + task.id} aria-label={task.title}
          checked={checked.includes(task.id)} onChange={(event) => toggle(task.id, event.target.checked)} />
        <span><strong>{task.title}</strong><small>{task.detail}</small></span>
      </Label>)}
    </div>
    <div className="tools-checker-actions">
      <Button onClick={() => window.print()}>Print checklist</Button>
      <Button variant="outline" disabled={checked.length === 0} onClick={() => setChecked([])}>Reset checks</Button>
      <a href="https://www.ato.gov.au/" target="_blank" rel="noopener noreferrer">Open the ATO website <span aria-hidden="true">↗</span></a>
    </div>
    <p className="tools-help">No names, incomes, ABNs, amounts or selections are saved or transmitted. Checking a task is for your own preparation, not proof of eligibility or completion of lodgement.</p>
  </Card>;
}
