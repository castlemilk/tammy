"use client";

import { useState } from "react";
import { aud, parseAudCents } from "../lib/tools/calculations";
import { calculateMargin, formatPercent } from "../lib/tools/margins";

export function ProfitMarginCalculator() {
  const [cost, setCost] = useState("80.00");
  const [selling, setSelling] = useState("120.00");
  const costCents = parseAudCents(cost);
  const sellingCents = parseAudCents(selling);
  const result = costCents !== null && sellingCents !== null ?
    calculateMargin(costCents, sellingCents) : null;

  return <section className="tools-calculator" aria-label="Profit margin and markup calculator">
    <div className="tools-calculator-input">
      <label className="tools-field" htmlFor="margin-cost"><span>Cost price, excluding GST (AUD)</span>
        <input id="margin-cost" type="text" inputMode="decimal" autoComplete="off"
          value={cost} onChange={event => setCost(event.target.value)} aria-invalid={costCents === null} />
      </label>
      <label className="tools-field" htmlFor="margin-sale"><span>Selling price, excluding GST (AUD)</span>
        <input id="margin-sale" type="text" inputMode="decimal" autoComplete="off"
          value={selling} onChange={event => setSelling(event.target.value)} aria-invalid={sellingCents === null} />
      </label>
      {result === null && <p className="tools-error" role="alert">Enter valid non-negative AUD amounts, with up to two decimals.</p>}
      <p className="tools-help">Compare prices on an ex-GST basis. Figures exclude other overheads and transaction fees unless you include them in your cost.</p>
    </div>
    <div className="tools-calculator-result" aria-live="polite">
      <p className="tools-overline">Per-unit results</p>
      <dl className="tools-result-list">
        <div><dt>Gross profit</dt><dd>{result ? aud(result.profitCents) : "–"}</dd></div>
        <div><dt>Markup on cost</dt><dd>{result ? formatPercent(result.markupPercent) : "–"}</dd></div>
        <div className="tools-result-total"><dt>Gross margin on sales</dt><dd>{result ? formatPercent(result.marginPercent) : "–"}</dd></div>
      </dl>
      <p className="tools-help">Margin uses the selling price as its denominator. Markup uses the cost price. Neither includes income tax or GST.</p>
    </div>
  </section>;
}
