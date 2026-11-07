"use client";
import { useState } from "react";
import { aud, gstBreakdown, parseAudCents, type GstMode } from "../lib/tools/calculations";
import { SegmentedControl } from "./gamma/segmented-control";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

export function GstCalculator() {
  const [amount, setAmount] = useState("1000.00");
  const [mode, setMode] = useState<GstMode>("add");
  const cents = parseAudCents(amount);
  const result = cents === null ? null : gstBreakdown(cents, mode);
  return <div className="tools-calculator tools-gst-calculator">
    <div className="tools-calculator-input">
      <SegmentedControl label="GST calculation" value={mode} options={[
        { value: "add", label: "Add GST" },
        { value: "remove", label: "Extract GST" },
      ]} onChange={(value) => setMode(value as GstMode)} block />
      <div className="tools-field">
        <Label htmlFor="gst-amount">{mode === "add" ? "Amount excluding GST (AUD)" : "Amount including GST (AUD)"}</Label>
        <Input id="gst-amount" inputMode="decimal" autoComplete="off" value={amount}
          onChange={e => setAmount(e.target.value)} aria-invalid={cents === null}
          aria-describedby={cents === null ? "gst-help gst-error" : "gst-help"} />
      </div>
      {cents === null && <p id="gst-error" className="tools-error" role="alert">Enter a non-negative AUD amount using up to two decimal places.</p>}
      <p id="gst-help" className="tools-help">The standard GST rate is 10%. No amounts leave your browser.</p>
    </div>
    <div className="tools-calculator-result" aria-live="polite">
      <h2>Your GST breakdown</h2>
      <dl className="tools-result-list">
        <div><dt>Excluding GST</dt><dd>{result ? aud(result.exclusiveCents) : "–"}</dd></div>
        <div><dt>GST (10%)</dt><dd>{result ? aud(result.gstCents) : "–"}</dd></div>
        <div className="tools-result-total"><dt>Including GST</dt><dd>{result ? aud(result.inclusiveCents) : "–"}</dd></div>
      </dl>
      <p className="tools-help">{mode === "add" ? "GST is 10% of the exclusive price." : "GST is one-eleventh of a GST-inclusive price."}</p>
    </div>
  </div>;
}
