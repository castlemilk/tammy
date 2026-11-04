"use client";
import { useState } from "react";
import { aud, parseAudCents } from "../lib/tools/calculations";
import { calculateBreakEven, formatAudBigInt } from "../lib/tools/break-even";

export function BreakEvenCalculator() {
  const [fixed, setFixed] = useState("3000.00");
  const [price, setPrice] = useState("120.00");
  const [variable, setVariable] = useState("80.00");
  const values = [fixed, price, variable].map(parseAudCents);
  const valid = values.every((value) => value !== null);
  const result = valid
    ? calculateBreakEven(values[0]!, values[1]!, values[2]!)
    : null;
  return <section className="tools-calculator" aria-label="Break-even calculator">
    <div className="tools-calculator-input">
      <label className="tools-field" htmlFor="break-fixed"><span>Fixed costs for the period (AUD)</span>
        <input id="break-fixed" type="text" inputMode="decimal" value={fixed}
          aria-invalid={values[0] === null} onChange={(event) => setFixed(event.target.value)} />
      </label>
      <label className="tools-field" htmlFor="break-price"><span>Selling price per unit, ex GST (AUD)</span>
        <input id="break-price" type="text" inputMode="decimal" value={price}
          aria-invalid={values[1] === null} onChange={(event) => setPrice(event.target.value)} />
      </label>
      <label className="tools-field" htmlFor="break-variable"><span>Variable cost per unit, ex GST (AUD)</span>
        <input id="break-variable" type="text" inputMode="decimal" value={variable}
          aria-invalid={values[2] === null} onChange={(event) => setVariable(event.target.value)} />
      </label>
      {!valid && <p className="tools-error" role="alert">Enter non-negative AUD amounts with up to two decimal places.</p>}
      <p className="tools-help">Use matching periods, such as monthly fixed costs and expected units sold that month. This is a single-product model.</p>
    </div>
    <div className="tools-calculator-result" aria-live="polite">
      <p className="tools-overline">Break-even estimate</p>
      {result && <>
        <dl className="tools-result-list">
          <div><dt>Contribution per unit</dt><dd>{aud(result.contributionCents)}</dd></div>
          <div className="tools-result-total">
            <dt>Units to cover fixed costs</dt>
            <dd>{result.status === "possible" ? result.units.toLocaleString("en-AU") : "Not reachable"}</dd>
          </div>
          <div><dt>Sales at break-even</dt><dd>{result.status === "possible" ? formatAudBigInt(result.revenueCents) : "Not reachable"}</dd></div>
        </dl>
        <p className="tools-help">{result.status === "unreachable"
          ? "The unit price does not exceed the variable cost. Selling more units cannot cover positive fixed costs at these prices."
          : result.units === 0n && result.contributionCents <= 0
            ? "Fixed costs are zero, but each positive sale may still lose money. Check the unit contribution before setting a sales target."
            : "Whole units are rounded up. This estimate excludes finance costs, taxes and demand constraints."}</p>
      </>}
      {!result && <p className="tools-help">Enter three valid amounts to see the break-even units.</p>}
    </div>
  </section>;
}
