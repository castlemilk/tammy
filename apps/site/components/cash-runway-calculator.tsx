"use client";
import { useState } from "react";
import { aud, parseAudCents } from "../lib/tools/calculations";
import { estimateCashRunway, formatRunway } from "../lib/tools/cash-runway";

export function CashRunwayCalculator() {
  const [cash, setCash] = useState("24000.00");
  const [incoming, setIncoming] = useState("6000.00");
  const [outgoing, setOutgoing] = useState("10000.00");
  const values = [cash, incoming, outgoing].map(parseAudCents);
  const valid = values.every((value) => value !== null);
  const result = valid ? estimateCashRunway(values[0]!, values[1]!, values[2]!) : null;

  return <section className="tools-calculator" aria-label="Cash runway calculator">
    <div className="tools-calculator-input">
      <label className="tools-field" htmlFor="runway-cash"><span>Available cash balance (AUD)</span>
        <input id="runway-cash" inputMode="decimal" type="text" value={cash}
          aria-invalid={values[0] === null} onChange={(event) => setCash(event.target.value)} />
      </label>
      <label className="tools-field" htmlFor="runway-income"><span>Expected cash receipts per month (AUD)</span>
        <input id="runway-income" inputMode="decimal" type="text" value={incoming}
          aria-invalid={values[1] === null} onChange={(event) => setIncoming(event.target.value)} />
      </label>
      <label className="tools-field" htmlFor="runway-outgoing"><span>Expected cash payments per month (AUD)</span>
        <input id="runway-outgoing" inputMode="decimal" type="text" value={outgoing}
          aria-invalid={values[2] === null} onChange={(event) => setOutgoing(event.target.value)} />
      </label>
      {!valid && <p className="tools-error" role="alert">Enter non-negative AUD amounts with up to two decimal places.</p>}
      <p className="tools-help">Include GST and tax payments consistently in your cash estimates. All calculations stay in this browser tab.</p>
    </div>
    <div className="tools-calculator-result" aria-live="polite">
      <p className="tools-overline">Current-rate estimate</p>
      {result && <>
        <dl className="tools-result-list">
          <div><dt>Monthly net cash flow</dt><dd>{aud(result.monthlyNetCents)}</dd></div>
          <div className="tools-result-total">
            <dt>Estimated runway</dt>
            <dd>{result.status === "decreasing" ? formatRunway(result.runwayTenths) : "No net burn"}</dd>
          </div>
        </dl>
        <p className="tools-help">{result.status === "decreasing"
          ? "Estimated months before the opening balance is used at an unchanged monthly net burn. Real cash shortfalls can arrive earlier."
          : "At these constant monthly rates, incoming cash covers outgoing payments. This does not guarantee future liquidity."}</p>
      </>}
      {!result && <p className="tools-help">Enter your cash balance and monthly cash flows to estimate runway.</p>}
    </div>
  </section>;
}
