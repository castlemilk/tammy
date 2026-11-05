"use client";
import { useMemo, useState } from "react";
import { formatAud } from "@tammy/invoices/model";
import { compileQuote, type QuoteLineInput } from "../lib/tools/quote";

const emptyLine = (): QuoteLineInput => ({
  description: "", quantity: "1", unitPrice: "0.00", treatment: "taxable",
});
export function QuoteGenerator() {
  const [sellerName, setSellerName] = useState("");
  const [sellerAbn, setSellerAbn] = useState("");
  const [sellerAddress, setSellerAddress] = useState("");
  const [customerName, setCustomerName] = useState("");
  const [customerAddress, setCustomerAddress] = useState("");
  const [reference, setReference] = useState("QUO-001");
  const [quoteDate, setQuoteDate] = useState("");
  const [validUntil, setValidUntil] = useState("");
  const [gstRegistered, setGstRegistered] = useState(false);
  const [notes, setNotes] = useState("");
  const [lines, setLines] = useState<QuoteLineInput[]>([emptyLine()]);
  const [message, setMessage] = useState("");

  const compiled = useMemo(() => {
    try {
      const result = compileQuote({
        sellerName, sellerAbn, sellerAddress, customerName, customerAddress,
        reference, quoteDate, validUntil, gstRegistered, notes, lines,
      });
      return { result, error: "" };
    } catch (error) {
      return { result: null, error: error instanceof Error ? error.message : "Check the quote details." };
    }
  }, [sellerName, sellerAbn, sellerAddress, customerName, customerAddress, reference, quoteDate, validUntil, gstRegistered, notes, lines]);

  const updateLine = (index: number, patch: Partial<QuoteLineInput>) => {
    setLines((previous) => previous.map((line, i) => i === index ? { ...line, ...patch } : line));
    setMessage("");
  };
  function print() {
    try {
      if (!compiled.result) throw new Error(compiled.error || "Complete the quote before printing.");
      setMessage("");
      window.print();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Unable to print. Check your quote.");
    }
  }

  return <div className="tools-quote-layout">
    <section className="tools-quote-form" aria-labelledby="quote-editor-heading">
      <div className="tools-form-heading">
        <div><p className="tools-overline">Quote editor</p><h2 id="quote-editor-heading">Prepare an estimate.</h2></div>
        <span className="tools-local-indicator">Browser only</span>
      </div>
      <fieldset className="tools-form-section"><legend>From</legend>
        <div className="tools-form-two">
          <label className="tools-field"><span>Business name *</span><input value={sellerName} maxLength={256} onChange={(event) => setSellerName(event.target.value)} /></label>
          <label className="tools-field"><span>ABN (optional)</span><input inputMode="numeric" maxLength={20} value={sellerAbn} onChange={(event) => setSellerAbn(event.target.value)} /></label>
        </div>
        <label className="tools-field"><span>Business address</span><textarea value={sellerAddress} rows={2} maxLength={1000} onChange={(event) => setSellerAddress(event.target.value)} /></label>
        <label className="tools-check"><input type="checkbox" checked={gstRegistered} onChange={(event) => setGstRegistered(event.target.checked)} /><span>My business is registered for GST</span></label>
      </fieldset>
      <fieldset className="tools-form-section"><legend>Customer</legend>
        <div className="tools-form-two">
          <label className="tools-field"><span>Customer name *</span><input value={customerName} maxLength={200} onChange={(event) => setCustomerName(event.target.value)} /></label>
          <label className="tools-field"><span>Customer address</span><input value={customerAddress} maxLength={1000} onChange={(event) => setCustomerAddress(event.target.value)} /></label>
        </div>
      </fieldset>
      <fieldset className="tools-form-section"><legend>Quote details</legend>
        <div className="tools-form-three">
          <label className="tools-field"><span>Reference *</span><input value={reference} maxLength={40} onChange={(event) => setReference(event.target.value)} /></label>
          <label className="tools-field"><span>Quote date *</span><input type="date" value={quoteDate} onChange={(event) => setQuoteDate(event.target.value)} /></label>
          <label className="tools-field"><span>Valid until *</span><input type="date" min={quoteDate || undefined} value={validUntil} onChange={(event) => setValidUntil(event.target.value)} /></label>
        </div>
      </fieldset>
      <fieldset className="tools-form-section"><legend>Quoted work</legend>
        {lines.map((line, i) => <div className="tools-quote-line" key={i}>
          <label className="tools-field"><span>Line {i + 1} description *</span><input maxLength={500} value={line.description} onChange={(e) => updateLine(i, { description: e.target.value })} /></label>
          <div className="tools-form-three">
            <label className="tools-field"><span>Quantity</span><input inputMode="decimal" value={line.quantity} onChange={(e) => updateLine(i, { quantity: e.target.value })}/></label>
            <label className="tools-field"><span>Unit price (ex GST, AUD)</span><input inputMode="decimal" value={line.unitPrice} onChange={(e) => updateLine(i, { unitPrice: e.target.value })}/></label>
            {gstRegistered && <label className="tools-field"><span>Tax treatment</span><select value={line.treatment} onChange={(e) => updateLine(i, { treatment: e.target.value as QuoteLineInput["treatment"] })}><option value="taxable">Taxable (10%)</option><option value="gst-free">GST-free</option></select></label>}
          </div>
          {lines.length > 1 && <button className="tools-quote-remove" type="button" onClick={() => setLines((previous) => previous.filter((_, j) => i !== j))}>Remove line {i + 1}</button>}
        </div>)}
        <button className="tools-add-item" type="button" disabled={lines.length >= 20} onClick={() => setLines((previous) => [...previous, emptyLine()])}>+ Add line item</button>
      </fieldset>
      <label className="tools-field tools-quote-notes"><span>Notes and assumptions</span><textarea rows={3} maxLength={2000} value={notes} onChange={(e) => setNotes(e.target.value)}/></label>
      <div className="tools-form-actions">
        <button className="tools-download" type="button" onClick={print}>Print or save quote as PDF</button>
        {message && <output className="tools-error">{message}</output>}
        <p className="tools-help">Printing opens your browser's print dialog; choose Save as PDF to create a PDF. No account, upload, issue posting or delivery occurs.</p>
      </div>
    </section>
    <aside className="tools-quote-preview" aria-label="Live quote preview">
      <div className="tools-preview-toolbar"><span>Live estimate preview</span><span>Not an invoice</span></div>
      <article className="tools-quote-paper">
        <div className="tools-quote-paper-header"><div><p className="tools-overline">Estimate</p><h2>QUOTE</h2></div><p>{sellerName || "Your business"}</p></div>
        <div className="tools-quote-meta"><span>Reference <strong>{reference || "—"}</strong></span><span>Date <strong>{quoteDate || "—"}</strong></span><span>Valid until <strong>{validUntil || "—"}</strong></span></div>
        <div className="tools-quote-parties"><div><small>Prepared by</small><strong>{sellerName || "Business name"}</strong><p>{sellerAddress}</p>{sellerAbn && <p>ABN {sellerAbn}</p>}</div><div><small>Prepared for</small><strong>{customerName || "Customer name"}</strong><p>{customerAddress}</p></div></div>
        <table className="tools-paper-items">
          <thead><tr><th scope="col">Description</th><th scope="col">Qty</th><th scope="col">Ex GST</th><th scope="col">GST</th><th scope="col">Total</th></tr></thead>
          <tbody>{lines.map((line, i) => <tr key={i}>
            <td>{line.description || "Work description"}</td><td>{line.quantity}</td>
            <td>{compiled.result?.totals.lines[i] ? formatAud(compiled.result.totals.lines[i].netMinor) : "—"}</td>
            <td>{compiled.result?.totals.lines[i] ? formatAud(compiled.result.totals.lines[i].gstMinor) : "—"}</td>
            <td>{compiled.result?.totals.lines[i] ? formatAud(compiled.result.totals.lines[i].totalMinor) : "—"}</td>
          </tr>)}</tbody>
        </table>
        <dl className="tools-paper-totals">
          <div><dt>Subtotal (ex GST)</dt><dd>{compiled.result ? formatAud(compiled.result.totals.subtotalMinor) : "—"}</dd></div>
          <div><dt>GST</dt><dd>{compiled.result ? formatAud(compiled.result.totals.gstMinor) : "—"}</dd></div>
          <div className="tools-paper-total"><dt>Estimated total</dt><dd>{compiled.result ? formatAud(compiled.result.totals.totalMinor) : "—"}</dd></div>
        </dl>
        {notes && <p className="tools-quote-paper-note"><strong>Assumptions:</strong> {notes}</p>}
        <p className="tools-quote-disclaimer">Quote only. Not a tax invoice or payment request. Acceptance, scheduling and variations must be agreed separately.</p>
      </article>
    </aside>
  </div>;
}
