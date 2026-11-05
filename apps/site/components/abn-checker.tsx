"use client";

import { useState } from "react";
import { checkAbn, formattedAbn } from "../lib/tools/abn";

export function AbnChecker() {
  const [value, setValue] = useState("");
  const result = checkAbn(value);
  return <section className="tools-calculator" aria-label="ABN checksum checker">
    <div className="tools-calculator-input">
      <label className="tools-field" htmlFor="abn-entry"><span>Australian business number (ABN)</span>
        <input id="abn-entry" type="text" inputMode="numeric" autoComplete="off"
          placeholder="11-digit ABN" maxLength={24} value={value}
          onChange={event => setValue(event.target.value)}
          aria-invalid={result.status === "invalid"} />
      </label>
      <p className="tools-help">Spaces are optional. Your ABN stays in this browser tab. The check does not contact the Australian Business Register.</p>
      <a className="tools-external-link" href="https://abr.business.gov.au/" target="_blank" rel="noopener noreferrer">
        Look up registration details on ABN Lookup <span aria-hidden="true">↗</span>
      </a>
    </div>
    <div className="tools-calculator-result" aria-live="polite">
      <p className="tools-overline">Checksum result</p>
      <p className={result.status === "valid" ? "tools-check-result tools-check-valid" :
        result.status === "invalid" ? "tools-check-result tools-check-invalid" : "tools-check-result"}>
        {result.status === "valid" ? "Valid format" : result.status === "invalid" ? "Invalid ABN" : "Ready to check"}
      </p>
      {result.status === "valid" && <p className="tools-abn-number">{formattedAbn(result.digits)}</p>}
      <p className="tools-help">{result.explanation}</p>
      {result.status === "valid" && <p className="tools-help"><strong>Next:</strong> Check the official ABN Lookup for registration, business identity and GST status.</p>}
    </div>
  </section>;
}
