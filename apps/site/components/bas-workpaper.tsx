"use client";

import { useState } from "react";
import { aud, parseAudCents } from "../lib/tools/calculations";
import { prepareSimpleBas, quarterlyBasReference, type BasQuarter } from "../lib/tools/simpler-bas";

const audWhole = (amount: number) =>
  new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD",
    maximumFractionDigits: 0, minimumFractionDigits: 0 }).format(amount);

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
    try { result = prepareSimpleBas({
      taxableSalesInclusiveCents: values[0]!, otherReportableSalesCents: values[1]!,
      creditablePurchasesInclusiveCents: values[2]!,
    }); } catch (exception) { error = exception instanceof Error ? exception.message : "Invalid inputs."; }
  } else error = "Use non-negative AUD amounts, with no more than two decimal places.";
  const reference = quarterlyBasReference(Number(year), quarter);

  return <section className="tools-calculator" aria-label="Simpler BAS GST preparation workpaper">
    <div className="tools-calculator-input">
      <p className="tools-overline">Illustrative GST-only workpaper</p>
      <label className="tools-field" htmlFor="bas-taxable-sales"><span>Taxable sales, including 10% GST (AUD)</span>
        <input id="bas-taxable-sales" inputMode="decimal" type="text" value={sales}
          aria-invalid={values[0] === null} onChange={(e) => setSales(e.target.value)} />
      </label>
      <label className="tools-field" htmlFor="bas-other-sales"><span>Other sales you have confirmed belong at G1 (AUD)</span>
        <input id="bas-other-sales" inputMode="decimal" type="text" value={other}
          aria-invalid={values[1] === null} onChange={(e) => setOther(e.target.value)} />
      </label>
      <label className="tools-field" htmlFor="bas-creditable-purchases"><span>Fully creditable purchases, including 10% GST (AUD)</span>
        <input id="bas-creditable-purchases" inputMode="decimal" type="text" value={purchases}
          aria-invalid={values[2] === null} onChange={(e) => setPurchases(e.target.value)} />
      </label>
      <div className="tools-form-two">
        <label className="tools-field" htmlFor="bas-year"><span>Financial year beginning</span>
          <select id="bas-year" value={year} onChange={(e) => setYear(e.target.value)}>
            {Array.from({length:8},(_,i)=>2024+i).map(y=><option key={y} value={y}>{y}–{y+1}</option>)}
          </select>
        </label>
        <label className="tools-field" htmlFor="bas-quarter"><span>Quarter</span>
          <select id="bas-quarter" value={quarter} onChange={(e)=>setQuarter(Number(e.target.value) as BasQuarter)}>
            <option value="1">Q1 · Jul–Sep</option><option value="2">Q2 · Oct–Dec</option>
            <option value="3">Q3 · Jan–Mar</option><option value="4">Q4 · Apr–Jun</option>
          </select>
        </label>
      </div>
      {error && <p className="tools-error" role="alert">{error}</p>}
      <p className="tools-help">Enter only figures already classified for this reporting period and your GST accounting basis. This model assumes ordinary fully taxable sales and fully creditable purchases with no adjustments or special transactions.</p>
    </div>
    <div className="tools-calculator-result" aria-live="polite">
      <p className="tools-overline">Simpler BAS reference figures</p>
      <p className="tools-help">{reference.financialYear}, Q{quarter} ({reference.period})</p>
      {result && <dl className="tools-result-list">
        <div><dt>G1 · Total sales, whole dollars</dt><dd>{audWhole(result.entryG1Whole)}</dd></div>
        <div><dt>1A · GST on sales, whole dollars</dt><dd>{audWhole(result.entry1AWhole)}</dd></div>
        <div><dt>1B · Eligible GST on purchases, whole dollars</dt><dd>{audWhole(result.entry1BWhole)}</dd></div>
        <div className="tools-result-total"><dt>Difference, 1A less 1B</dt><dd>{audWhole(result.entryNetWhole)}</dd></div>
      </dl>}
      <p className="tools-help">Before dropping cents for the illustrated BAS labels: {result ?
        "G1 " + aud(result.totalSalesCents) + " · 1A " + aud(result.gstOnSalesCents) + " · 1B " + aud(result.eligibleGstCreditsCents) : "Complete the inputs."}</p>
      <p className="tools-help"><strong>Standard reference due date:</strong> {reference.standardDueDate}. {reference.note}</p>
      <p className="tools-help">These are NOT ready-to-lodge figures. Credit eligibility, cash/non-cash attribution, tax adjustments, PAYG and other BAS labels have not been verified. Review each amount against your own ATO statement.</p>
      <a href="https://www.ato.gov.au/" rel="noopener noreferrer" target="_blank" className="tools-external-link">Continue to the ATO website <span aria-hidden="true">↗</span></a>
    </div>
  </section>;
}
