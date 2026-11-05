"use client";
import { useMemo, useState, type ChangeEvent } from "react";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Checkbox } from "./ui/checkbox";
import { Select } from "./ui/select";
import { Textarea } from "./ui/textarea";
import { serializeQuoteDraft, parseQuoteDraft } from "../lib/tools/local-quote-draft";
import type { QuoteInput } from "../lib/tools/quote";
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
  const [pdfBusy, setPdfBusy] = useState(false);

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


  function currentDraft(): QuoteInput {
    return {sellerName,sellerAbn,sellerAddress,customerName,customerAddress,reference,
      quoteDate,validUntil,gstRegistered,notes,lines};
  }

  function saveDraft() {
    try {
      const json=serializeQuoteDraft(currentDraft());
      const url=URL.createObjectURL(new Blob([json],{type:"application/json"}));
      const link=document.createElement("a");
      link.href=url;
      link.download="tammy-quote-"+(reference.replace(/[^a-zA-Z0-9_-]/g,"-").slice(0,40)||"draft")+".json";
      document.body.appendChild(link);link.click();link.remove();
      window.setTimeout(()=>URL.revokeObjectURL(url),1000);
      setMessage("Draft saved to your device as JSON. It remains your responsibility to keep the file private.");
    }catch(e){setMessage(e instanceof Error?e.message:"Unable to export quote draft.");}
  }
  async function loadDraft(event: ChangeEvent<HTMLInputElement>) {
    const file=event.target.files?.[0];
    if(!file)return;
    try{
      if(file.size>50000)throw new Error("Quote draft exceeds the 50 KB limit.");
      const data=parseQuoteDraft(await file.text());
      setSellerName(data.sellerName);setSellerAbn(data.sellerAbn);
      setSellerAddress(data.sellerAddress);setCustomerName(data.customerName);
      setCustomerAddress(data.customerAddress);setReference(data.reference);
      setQuoteDate(data.quoteDate);setValidUntil(data.validUntil);
      setGstRegistered(data.gstRegistered);setNotes(data.notes);
      setLines([...data.lines]);
      setMessage("Draft loaded from your local file. Review it before exporting a quote.");
    }catch(e){setMessage(e instanceof Error?e.message:"Unable to open quote draft.");}
    finally{event.target.value="";}
  }

  async function downloadQuotePdf() {
    setMessage("");
    if(!compiled.result){setMessage(compiled.error||"Complete valid quote details first.");return;}
    setPdfBusy(true);
    try{
      // Exactly the same local WASM renderer as invoices, with a quote-only template.
      const { renderBrowserQuotePdf }=await import("@tammy/invoices/pdf-browser");
      const pdf=await renderBrowserQuotePdf({
        draft:compiled.result.draft,
        sellerName:compiled.result.sellerName,
        sellerAbn:compiled.result.sellerAbn,
      });
      const buffer=new ArrayBuffer(pdf.byteLength);
      new Uint8Array(buffer).set(pdf);
      const url=URL.createObjectURL(new Blob([buffer],{type:"application/pdf"}));
      const link=document.createElement("a");
      link.href=url;
      link.download="quote-"+(reference.replace(/[^a-zA-Z0-9_-]/g,"-").slice(0,40)||"estimate")+".pdf";
      document.body.appendChild(link);link.click();link.remove();
      window.setTimeout(()=>URL.revokeObjectURL(url),1000);
      setMessage("Quote PDF generated locally. This has not issued an invoice or saved an accounting entry.");
    }catch(e){setMessage(e instanceof Error?e.message:"Unable to generate the quote PDF.");}
    finally{setPdfBusy(false);}
  }

  return <div className="tools-quote-layout">
    <section className="tools-quote-form" aria-labelledby="quote-editor-heading">
      <div className="tools-form-heading">
        <div><p className="tools-overline">Quote editor</p><h2 id="quote-editor-heading">Prepare an estimate.</h2></div>
        <span className="tools-local-indicator">Browser only</span>
      </div>
      <fieldset className="tools-form-section"><legend>From</legend>
        <div className="tools-form-two">
          <label className="tools-field" htmlFor="quote-seller-name"><span>Business name *</span><Input id="quote-seller-name" value={sellerName} maxLength={256} onChange={(event) => setSellerName(event.target.value)} /></label>
          <label className="tools-field" htmlFor="quote-seller-abn"><span>ABN (optional)</span><Input id="quote-seller-abn" inputMode="numeric" maxLength={20} value={sellerAbn} onChange={(event) => setSellerAbn(event.target.value)} /></label>
        </div>
        <label className="tools-field" htmlFor="quote-seller-address"><span>Business address</span><Textarea id="quote-seller-address" value={sellerAddress} rows={2} maxLength={1000} onChange={(event) => setSellerAddress(event.target.value)} /></label>
        <label className="tools-check" htmlFor="quote-gst-registered"><Checkbox id="quote-gst-registered" checked={gstRegistered} onChange={(event) => setGstRegistered(event.target.checked)} /><span>My business is registered for GST</span></label>
      </fieldset>
      <fieldset className="tools-form-section"><legend>Customer</legend>
        <div className="tools-form-two">
          <label className="tools-field" htmlFor="quote-customer-name"><span>Customer name *</span><Input id="quote-customer-name" value={customerName} maxLength={200} onChange={(event) => setCustomerName(event.target.value)} /></label>
          <label className="tools-field" htmlFor="quote-customer-address"><span>Customer address</span><Input id="quote-customer-address" value={customerAddress} maxLength={1000} onChange={(event) => setCustomerAddress(event.target.value)} /></label>
        </div>
      </fieldset>
      <fieldset className="tools-form-section"><legend>Quote details</legend>
        <div className="tools-form-three">
          <label className="tools-field" htmlFor="quote-reference"><span>Reference *</span><Input id="quote-reference" value={reference} maxLength={40} onChange={(event) => setReference(event.target.value)} /></label>
          <label className="tools-field" htmlFor="quote-date"><span>Quote date *</span><Input id="quote-date" type="date" value={quoteDate} onChange={(event) => setQuoteDate(event.target.value)} /></label>
          <label className="tools-field" htmlFor="quote-valid-until"><span>Valid until *</span><Input id="quote-valid-until" type="date" min={quoteDate || undefined} value={validUntil} onChange={(event) => setValidUntil(event.target.value)} /></label>
        </div>
      </fieldset>
      <fieldset className="tools-form-section"><legend>Quoted work</legend>
        {lines.map((line, i) => <div className="tools-quote-line" key={i}>
          <label className="tools-field" htmlFor={"quote-line-description-"+i}><span>Line {i + 1} description *</span><Input id={"quote-line-description-"+i} maxLength={500} value={line.description} onChange={(e) => updateLine(i, { description: e.target.value })} /></label>
          <div className="tools-form-three">
            <label className="tools-field" htmlFor={"quote-line-quantity-"+i}><span>Quantity</span><Input id={"quote-line-quantity-"+i} inputMode="decimal" value={line.quantity} onChange={(e) => updateLine(i, { quantity: e.target.value })}/></label>
            <label className="tools-field" htmlFor={"quote-line-price-"+i}><span>Unit price (ex GST, AUD)</span><Input id={"quote-line-price-"+i} inputMode="decimal" value={line.unitPrice} onChange={(e) => updateLine(i, { unitPrice: e.target.value })}/></label>
            {gstRegistered && <label className="tools-field" htmlFor={"quote-line-treatment-"+i}><span>Tax treatment</span><Select id={"quote-line-treatment-"+i} value={line.treatment} onChange={(e) => updateLine(i, { treatment: e.target.value as QuoteLineInput["treatment"] })}><option value="taxable">Taxable (10%)</option><option value="gst-free">GST-free</option></Select></label>}
          </div>
          {lines.length > 1 && <Button variant="outline" size="sm" className="tools-quote-remove" type="button" onClick={() => setLines((previous) => previous.filter((_, j) => i !== j))}>Remove line {i + 1}</Button>}
        </div>)}
        <Button variant="outline" className="tools-add-item" type="button" disabled={lines.length >= 20} onClick={() => setLines((previous) => [...previous, emptyLine()])}>+ Add line item</Button>
      </fieldset>
      <label className="tools-field tools-quote-notes" htmlFor="quote-notes"><span>Notes and assumptions</span><Textarea id="quote-notes" rows={3} maxLength={2000} value={notes} onChange={(e) => setNotes(e.target.value)}/></label>
      <div className="tools-form-actions">
        <div className="flex flex-wrap items-center gap-2">
          <Button loading={pdfBusy} onClick={()=>void downloadQuotePdf()}>{pdfBusy?"Generating PDF...":"Download quote PDF"}</Button>
          <Button variant="outline" onClick={print}>Print or save quote as PDF</Button>
          <Button variant="secondary" onClick={saveDraft}>Download draft JSON</Button>
        </div>
        <label htmlFor="quote-draft-upload" className="mt-3 block text-sm font-semibold text-foreground">Import a saved quote draft</label>
        <Input id="quote-draft-upload" type="file" accept=".json,application/json"
          aria-label="Import quote draft JSON" onChange={(event)=>void loadDraft(event)} className="mt-1 cursor-pointer" />
        {message && <output className="tools-error">{message}</output>}
        <p className="tools-help">The PDF and optional JSON draft are created locally. The JSON draft contains the customer details you entered and is not encrypted; keep it in a private location. No account, upload, invoicing or delivery occurs.</p>
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
