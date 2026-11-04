"use client";

import { useMemo, useState } from "react";
import type { InvoiceDraftData, InvoiceTaxTreatment } from "@tammy/invoices/model";
import { calculateInvoice, formatAud, validateAbn, validateInvoiceDraft, validateInvoiceForIssue } from "@tammy/invoices/model";
import { parseAudCents, parseQuantityMilli } from "../lib/tools/calculations";

type EditableLine = {
  id: number;
  description: string;
  quantity: string;
  price: string;
  treatment: "taxable" | "gst-free";
};

const initialLine: EditableLine = {
  id: 1, description: "", quantity: "1", price: "0.00", treatment: "taxable"
};

function todayLocalIso() {
  const date = new Date();
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 10);
}

function formatAbn(input: string): string {
  return input.replace(/\s/g, "").replace(/^(\d{2})(\d{3})(\d{3})(\d{3})$/, "$1 $2 $3 $4");
}

function filename(number: string): string {
  const suffix = number.replace(/[^A-Za-z0-9_-]/g, "-").slice(0, 40);
  return `invoice-${suffix || "document"}.pdf`;
}

export function InvoiceGenerator() {
  const [sellerName, setSellerName] = useState("");
  const [sellerAbn, setSellerAbn] = useState("");
  const [sellerAddress, setSellerAddress] = useState("");
  const [customerName, setCustomerName] = useState("");
  const [customerAddress, setCustomerAddress] = useState("");
  const [customerAbn, setCustomerAbn] = useState("");
  const [number, setNumber] = useState("INV-001");
  const [issueDate, setIssueDate] = useState("");
  const [dueDate, setDueDate] = useState("");
  const [gstRegistered, setGstRegistered] = useState(false);
  const [notes, setNotes] = useState("");
  const [paymentInstructions, setPaymentInstructions] = useState("");
  const [lines, setLines] = useState<EditableLine[]>([initialLine]);
  const [pdfStatus, setPdfStatus] = useState<"idle" | "generating" | "ready">("idle");
  const [pdfMessage, setPdfMessage] = useState("");

  const compiled = useMemo(() => {
    try {
      const draft: InvoiceDraftData = {
        number, issueDate, dueDate, customerName, customerAddress,
        ...(customerAbn.trim() ? { customerAbn } : {}),
        sellerAddress, gstRegistered, notes, paymentInstructions,
        lines: lines.map((row, index) => {
          const quantityMilli = parseQuantityMilli(row.quantity);
          const unitPriceMinor = parseAudCents(row.price);
          if (quantityMilli === null) throw new Error(`Line ${index + 1}: enter a quantity between 0.001 and 1000.`);
          if (unitPriceMinor === null || unitPriceMinor > 100_000_000)
            throw new Error(`Line ${index + 1}: enter a unit price from $0 to $1,000,000.`);
          const treatment: InvoiceTaxTreatment = gstRegistered ? row.treatment : "out-of-scope";
          return { description: row.description, quantityMilli, unitPriceMinor, taxTreatment: treatment };
        }),
      };
      const validated = validateInvoiceDraft(draft);
      return { draft: validated, totals: calculateInvoice(validated), error: "" };
    } catch (error) {
      return { draft: null, totals: null, error: error instanceof Error ? error.message : "Check the invoice amounts." };
    }
  }, [number, issueDate, dueDate, customerName, customerAddress, customerAbn, sellerAddress, gstRegistered, notes, paymentInstructions, lines]);

  const updateLine = (id: number, patch: Partial<EditableLine>) => {
    setLines(rows => rows.map(row => row.id === id ? { ...row, ...patch } : row));
    setPdfMessage("");
  };

  const addLine = () => {
    setLines(rows => rows.length >= 20 ? rows : [...rows, {
      id: Math.max(...rows.map(row => row.id)) + 1,
      description: "", quantity: "1", price: "0.00", treatment: "taxable"
    }]);
  };

  async function downloadPdf() {
    setPdfMessage("");
    try {
      if (!compiled.draft) throw new Error(compiled.error || "Complete the line items first.");
      const normalizedAbn = validateAbn(sellerAbn, "seller ABN", true);
      if (!sellerName.trim()) throw new Error("Enter your business name.");
      const draft = validateInvoiceForIssue(compiled.draft, { name: sellerName, abn: normalizedAbn });
      if (compiled.totals && compiled.totals.totalMinor >= 100_000 && !draft.customerName.trim())
        throw new Error("Invoices of $1,000 or more must identify the customer.");
      setPdfStatus("generating");
      // Shared, pdfcn-adapted Forme renderer from the Tammy desktop invoice package.
      // Dynamic import keeps its WASM out of the initial page load.
      const { renderInvoicePdf } = await import("@tammy/invoices/pdf");
      const pdf = await renderInvoicePdf({
        ...draft,
        sellerName: sellerName.trim(),
        sellerAbn: normalizedAbn,
        state: "issued",
      });
      // Copy into an owned ArrayBuffer for browsers with strict BlobPart types.
      const buffer = new ArrayBuffer(pdf.byteLength);
      new Uint8Array(buffer).set(pdf);
      const url = URL.createObjectURL(new Blob([buffer], { type: "application/pdf" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = filename(number);
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      setPdfStatus("ready");
      setPdfMessage("Your PDF has been generated locally. It was not saved to Tammy or sent to your customer.");
    } catch (error) {
      setPdfStatus("idle");
      setPdfMessage(error instanceof Error ? error.message : "Unable to generate the invoice PDF.");
    }
  }

  return <div className="tools-invoice-grid">
    <div className="tools-invoice-form">
      <div className="tools-form-heading"><div><p className="tools-overline">Invoice details</p><h2>Make it yours.</h2></div><span className="tools-local-indicator">On-device only</span></div>
      <fieldset className="tools-form-section"><legend>From</legend>
        <div className="tools-form-two">
          <label className="tools-field"><span>Business name *</span><input value={sellerName} maxLength={256} onChange={e => setSellerName(e.target.value)} placeholder="Your business name" /></label>
          <label className="tools-field"><span>ABN *</span><input inputMode="numeric" value={sellerAbn} maxLength={20} onChange={e => setSellerAbn(e.target.value)} placeholder="11-digit ABN" /></label>
        </div>
        <label className="tools-field"><span>Business address *</span><textarea value={sellerAddress} maxLength={1000} rows={2} onChange={e => setSellerAddress(e.target.value)} placeholder="Street, suburb, state and postcode" /></label>
        <label className="tools-check"><input type="checkbox" checked={gstRegistered} onChange={e => setGstRegistered(e.target.checked)} /><span>My business is registered for GST</span></label>
      </fieldset>

      <fieldset className="tools-form-section"><legend>Bill to</legend>
        <div className="tools-form-two">
          <label className="tools-field"><span>Customer or business name *</span><input value={customerName} maxLength={200} onChange={e => setCustomerName(e.target.value)} /></label>
          <label className="tools-field"><span>Customer ABN (optional)</span><input inputMode="numeric" value={customerAbn} maxLength={20} onChange={e => setCustomerAbn(e.target.value)} /></label>
        </div>
        <label className="tools-field"><span>Customer address *</span><textarea rows={2} maxLength={1000} value={customerAddress} onChange={e => setCustomerAddress(e.target.value)} /></label>
      </fieldset>

      <fieldset className="tools-form-section"><legend>Invoice</legend>
        <div className="tools-form-three">
          <label className="tools-field"><span>Invoice number *</span><input maxLength={40} value={number} onChange={e => setNumber(e.target.value)} /></label>
          <label className="tools-field"><span>Issue date *</span><input type="date" value={issueDate} onChange={e => setIssueDate(e.target.value)} /><button className="tools-plain-action" type="button" onClick={() => setIssueDate(todayLocalIso())}>Use today</button></label>
          <label className="tools-field"><span>Due date *</span><input type="date" value={dueDate} min={issueDate || undefined} onChange={e => setDueDate(e.target.value)} /></label>
        </div>
      </fieldset>

      <fieldset className="tools-form-section"><legend>Items and amounts</legend>
        {lines.map((line, index) => <div className="tools-line-editor" key={line.id}>
          <div className="tools-line-head"><strong>Item {index + 1}</strong><button type="button" className="tools-plain-action" disabled={lines.length === 1} aria-label={`Remove item ${index + 1}`} onClick={() => setLines(rows => rows.filter(row => row.id !== line.id))}>Remove</button></div>
          <label className="tools-field"><span>Description</span><input maxLength={500} value={line.description} onChange={e => updateLine(line.id, { description: e.target.value })} placeholder="Design services, consultation, goods..." /></label>
          <div className="tools-form-three">
            <label className="tools-field"><span>Quantity</span><input inputMode="decimal" value={line.quantity} onChange={e => updateLine(line.id, { quantity: e.target.value })} /></label>
            <label className="tools-field"><span>Unit price (ex GST, AUD)</span><input inputMode="decimal" value={line.price} onChange={e => updateLine(line.id, { price: e.target.value })} /></label>
            {gstRegistered && <label className="tools-field"><span>GST treatment</span><select value={line.treatment} onChange={e => updateLine(line.id, { treatment: e.target.value as EditableLine["treatment"] })}><option value="taxable">Taxable (10%)</option><option value="gst-free">GST-free</option></select></label>}
          </div>
        </div>)}
        <button type="button" className="tools-add-item" disabled={lines.length >= 20} onClick={addLine}>+ Add line item</button>
      </fieldset>

      <fieldset className="tools-form-section"><legend>Payment details</legend>
        <label className="tools-field"><span>Payment instructions</span><textarea rows={2} value={paymentInstructions} maxLength={1000} onChange={e => setPaymentInstructions(e.target.value)} placeholder="Payment reference, method and account details" /></label>
        <label className="tools-field"><span>Additional notes (optional)</span><textarea rows={2} value={notes} maxLength={2000} onChange={e => setNotes(e.target.value)} /></label>
      </fieldset>

      <div className="tools-form-actions">
        <button type="button" className="tools-download" onClick={() => void downloadPdf()} disabled={pdfStatus === "generating"}>{pdfStatus === "generating" ? "Generating PDF..." : "Download invoice PDF"}</button>
        {compiled.error && <output className="tools-error">{compiled.error}</output>}
        {pdfMessage && <output className={pdfStatus === "ready" ? "tools-success" : "tools-error"}>{pdfMessage}</output>}
        <p className="tools-help">No login, no upload, no saved data. A PDF is created only after you supply valid invoice details. You are responsible for sending it to the customer.</p>
      </div>
    </div>

    <aside className="tools-invoice-preview" aria-label="Live invoice preview">
      <div className="tools-preview-toolbar"><span>Live document preview</span><span>Australian dollars</span></div>
      <article className="tools-invoice-paper">
        <div className="tools-paper-heading"><div><span className="tools-paper-mark" aria-hidden="true" /><strong>{sellerName || "Your business"}</strong></div><h2>{gstRegistered ? "Tax invoice" : "Invoice"}</h2></div>
        <dl className="tools-invoice-metadata">
          <div><dt>Number</dt><dd>{number || "Not set"}</dd></div>
          <div><dt>Issued</dt><dd>{issueDate || "Not set"}</dd></div>
          <div><dt>Due</dt><dd>{dueDate || "Not set"}</dd></div>
        </dl>
        <div className="tools-paper-parties"><div><span>From</span><strong>{sellerName || "Business name"}</strong><p>{sellerAddress || "Business address"}</p><small>{sellerAbn ? `ABN ${formatAbn(sellerAbn)}` : "ABN required for PDF"}</small></div><div><span>Billed to</span><strong>{customerName || "Customer name"}</strong><p>{customerAddress || "Customer address"}</p>{customerAbn && <small>ABN {formatAbn(customerAbn)}</small>}</div></div>
        <table className="tools-paper-items"><thead><tr><th scope="col">Description</th><th scope="col">Qty</th><th scope="col">Ex GST</th><th scope="col">GST</th><th scope="col">Total</th></tr></thead><tbody>
          {lines.map((row, index) => <tr key={row.id}><td>{row.description || "Item description"}</td><td>{row.quantity}</td><td>{compiled.totals && compiled.totals.lines[index] ? formatAud(compiled.totals.lines[index].netMinor) : "–"}</td><td>{compiled.totals && compiled.totals.lines[index] ? formatAud(compiled.totals.lines[index].gstMinor) : "–"}</td><td>{compiled.totals && compiled.totals.lines[index] ? formatAud(compiled.totals.lines[index].totalMinor) : "–"}</td></tr>)}
        </tbody></table>
        <dl className="tools-paper-totals"><div><dt>Subtotal (ex GST)</dt><dd>{compiled.totals ? formatAud(compiled.totals.subtotalMinor) : "–"}</dd></div><div><dt>GST</dt><dd>{compiled.totals ? formatAud(compiled.totals.gstMinor) : "–"}</dd></div><div className="tools-paper-total"><dt>Total due</dt><dd>{compiled.totals ? formatAud(compiled.totals.totalMinor) : "–"}</dd></div></dl>
        {paymentInstructions && <p className="tools-paper-note"><strong>Payment</strong> {paymentInstructions}</p>}
        {notes && <p className="tools-paper-note"><strong>Notes</strong> {notes}</p>}
        <p className="tools-paper-disclaimer">Preview only. A PDF is generated after validation. No invoice is issued in the desktop app.</p>
      </article>
    </aside>
  </div>;
}
