"use client";

import { useState } from "react";
import { Card } from "./ui/card";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Select } from "./ui/select";
import { SegmentedControl } from "./gamma/segmented-control";
import { aud, parseAudCents } from "../lib/tools/calculations";
import { prepareSimpleBas, quarterlyBasReference, type BasQuarter } from "../lib/tools/simpler-bas";

const audWhole = (amount: number) =>
  new Intl.NumberFormat("en-AU", {
    style: "currency", currency: "AUD",
    maximumFractionDigits: 0, minimumFractionDigits: 0,
  }).format(amount);

const quarters = [
  { value: "1", label: "Q1", description: "July through September" },
  { value: "2", label: "Q2", description: "October through December" },
  { value: "3", label: "Q3", description: "January through March" },
  { value: "4", label: "Q4", description: "April through June" },
];

export function BasWorkpaper() {
  const [sales, setSales] = useState("22000.00");
  const [other, setOther] = useState("0.00");
  const [purchases, setPurchases] = useState("7150.00");
  const [year, setYear] = useState("2026");
  const [quarter, setQuarter] = useState<BasQuarter>(1);
  const values = [sales, other, purchases].map(parseAudCents);
  const hasValues = values.every((x) => x !== null);
  let result: ReturnType<typeof prepareSimpleBas> | null = null;
  let error = "";
  if (hasValues) {
    try {
      result = prepareSimpleBas({
        taxableSalesInclusiveCents: values[0]!,
        otherReportableSalesCents: values[1]!,
        creditablePurchasesInclusiveCents: values[2]!,
      });
    } catch (exception) {
      error = exception instanceof Error ? exception.message : "Invalid inputs.";
    }
  } else {
    error = "Use non-negative AUD amounts, with no more than two decimal places.";
  }
  const reference = quarterlyBasReference(Number(year), quarter);
  return <section className="tools-calculator" aria-label="Simpler BAS GST preparation workpaper">
    <Card className="tools-calculator-input rounded-none border-0 shadow-none">
      <p className="tools-overline">Illustrative GST-only workpaper</p>
      <div className="flex flex-col gap-2">
        <Label htmlFor="bas-taxable-sales">Taxable sales, including 10% GST (AUD)</Label>
        <Input id="bas-taxable-sales" inputMode="decimal" value={sales}
          aria-invalid={values[0] === null} onChange={(event) => setSales(event.target.value)} />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="bas-other-sales">Other sales you have confirmed belong at G1 (AUD)</Label>
        <Input id="bas-other-sales" inputMode="decimal" value={other}
          aria-invalid={values[1] === null} onChange={(event) => setOther(event.target.value)} />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="bas-creditable-purchases">Fully creditable purchases, including 10% GST (AUD)</Label>
        <Input id="bas-creditable-purchases" inputMode="decimal" value={purchases}
          aria-invalid={values[2] === null} onChange={(event) => setPurchases(event.target.value)} />
      </div>
      <div className="grid items-end gap-5 sm:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-2">
          <Label htmlFor="bas-year">Financial year beginning</Label>
          <Select id="bas-year" value={year} onChange={(event) => setYear(event.target.value)}>
            {Array.from({ length: 8 }, (_, i) => 2024 + i).map((y) =>
              <option key={y} value={y}>{y}–{y + 1}</option>)}
          </Select>
        </div>
        <div className="flex min-w-0 flex-col gap-2">
          <span className="text-sm font-semibold text-foreground">Quarter</span>
          <SegmentedControl
            label="Quarter"
            value={String(quarter)}
            onChange={(value) => setQuarter(Number(value) as BasQuarter)}
            options={quarters}
            block
          />
        </div>
      </div>
      {error && <p className="tools-error" role="alert">{error}</p>}
      <p className="tools-help">Enter only figures already classified for this reporting period and your GST accounting basis. This model assumes ordinary fully taxable sales and fully creditable purchases with no adjustments or special transactions.</p>
    </Card>
    <Card className="tools-calculator-result rounded-none border-0 shadow-none" aria-live="polite">
      <p className="tools-overline">Simpler BAS reference figures</p>
      <p className="tools-help">{reference.financialYear}, Q{quarter} ({reference.period})</p>
      {result && <dl className="tools-result-list">
        <div><dt>G1 · Total sales, whole dollars</dt><dd>{audWhole(result.entryG1Whole)}</dd></div>
        <div><dt>1A · GST on sales, whole dollars</dt><dd>{audWhole(result.entry1AWhole)}</dd></div>
        <div><dt>1B · Eligible GST on purchases, whole dollars</dt><dd>{audWhole(result.entry1BWhole)}</dd></div>
        <div className="tools-result-total"><dt>Difference, 1A less 1B</dt><dd>{audWhole(result.entryNetWhole)}</dd></div>
      </dl>}
      <p className="tools-help">Before dropping cents for the illustrated BAS labels: {result
        ? "G1 " + aud(result.totalSalesCents) + " · 1A " + aud(result.gstOnSalesCents) + " · 1B " + aud(result.eligibleGstCreditsCents)
        : "Complete the inputs."}</p>
      <p className="tools-help"><strong>Standard reference due date:</strong> {reference.standardDueDate}. {reference.note}</p>
      <p className="tools-help">These are NOT ready-to-lodge figures. Credit eligibility, cash/non-cash attribution, tax adjustments, PAYG and other BAS labels have not been verified. Review each amount against your own ATO statement.</p>
      <a href="https://www.ato.gov.au/" rel="noopener noreferrer" target="_blank" className="tools-external-link">Continue to the ATO website <span aria-hidden="true">↗</span></a>
    </Card>
  </section>;
}
