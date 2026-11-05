"use client";
import { useState } from "react";
import { addCalendarDays, australianDate } from "../lib/tools/calculations";

export function InvoiceDueDateCalculator() {
  const [issue, setIssue] = useState("");
  const [terms, setTerms] = useState(30);
  const due = addCalendarDays(issue, terms);
  return <div className="tools-calculator">
    <div className="tools-calculator-input">
      <label className="tools-field"><span>Invoice issue date</span><input type="date" value={issue} onChange={e => setIssue(e.target.value)} min="1900-01-01" max="9990-01-01" /></label>
      <label className="tools-field"><span>Payment terms (calendar days)</span><select value={terms} onChange={e => setTerms(Number(e.target.value))}>
        {[7, 14, 30, 45, 60].map(days => <option key={days} value={days}>{days} days</option>)}
      </select></label>
      <p className="tools-help">The count begins the day after the invoice date. Calendar days include weekends and public holidays.</p>
    </div>
    <div className="tools-calculator-result" aria-live="polite">
      <p className="tools-overline">Calculated payment date</p>
      {due ? <><p className="tools-date-result">{australianDate(due)}</p><p className="tools-date-iso">{due}</p><p className="tools-help">Based on {terms} calendar days after the issue date.</p></> : <p className="tools-date-placeholder">Choose a valid invoice date to calculate when payment is due.</p>}
    </div>
  </div>;
}
