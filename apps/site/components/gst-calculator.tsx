"use client";
import { useState } from "react";
import { aud, gstBreakdown, parseAudCents, type GstMode } from "../lib/tools/calculations";

export function GstCalculator() {
  const [amount, setAmount] = useState("1000.00");
  const [mode, setMode] = useState<GstMode>("add");
  const cents = parseAudCents(amount);
  const result = cents === null ? null : gstBreakdown(cents, mode);
  return <div className="tools-calculator">
    <div className="tools-calculator-input">
      <fieldset className="tools-segmented"><legend>Price includes GST?</legend>
        <label><input type="radio" name="gst-mode" checked={mode === "add"} onChange={() => setMode("add")} /> Add GST</label>
        <label><input type="radio" name="gst-mode" checked={mode === "remove"} onChange={() => setMode("remove")} /> Extract GST</label>
      </fieldset>
      <label className="tools-field"><span>{mode === "add" ? "Amount excluding GST (AUD)" : "Amount including GST (AUD)"}</span><input inputMode="decimal" autoComplete="off" value={amount} onChange={e => setAmount(e.target.value)} aria-invalid={cents === null} /></label>
      {cents === null && <p className="tools-error" role="alert">Enter a non-negative AUD amount using up to two decimal places.</p>}
      <p className="tools-help">The standard GST rate is 10%. No amounts leave your browser.</p>
    </div>
    <div className="tools-calculator-result" aria-live="polite">
      <p className="tools-overline">Your GST breakdown</p>
      <dl className="tools-result-list">
        <div><dt>Excluding GST</dt><dd>{result ? aud(result.exclusiveCents) : "–"}</dd></div>
        <div><dt>GST (10%)</dt><dd>{result ? aud(result.gstCents) : "–"}</dd></div>
        <div className="tools-result-total"><dt>Including GST</dt><dd>{result ? aud(result.inclusiveCents) : "–"}</dd></div>
      </dl>
      <p className="tools-help">{mode === "add" ? "GST is 10% of the exclusive price." : "GST is one-eleventh of a GST-inclusive price."}</p>
    </div>
  </div>;
}
