/** Quotes are proposals, not issued tax invoices or accounting entries.
 * Reuse Tammy's validated invoice line arithmetic without posting anything.
 */
import {
  calculateInvoice, validateAbn, validateInvoiceDraft,
  type InvoiceTaxTreatment,
} from "@tammy/invoices/model";

export interface QuoteLineInput {
  readonly description: string;
  readonly quantity: string;
  readonly unitPrice: string;
  readonly treatment: "taxable" | "gst-free";
}
export interface QuoteInput {
  readonly sellerName: string;
  readonly sellerAbn: string;
  readonly sellerAddress: string;
  readonly customerName: string;
  readonly customerAddress: string;
  readonly reference: string;
  readonly quoteDate: string;
  readonly validUntil: string;
  readonly gstRegistered: boolean;
  readonly notes: string;
  readonly lines: readonly QuoteLineInput[];
}
const MAX_LINES = 20;
const max = (s: string, n: number) => s.isWellFormed() && Array.from(s).length <= n &&
  !Array.from(s).some((char) => {
    const code = char.charCodeAt(0);
    return (code < 32 && code !== 9 && code !== 10 && code !== 13) || code === 127;
  });
function text(value: string, label: string, length: number, required = true) {
  if (!max(value, length) || (required && !value.trim())) {
    throw new Error(label + ": enter valid text of at most " + length + " characters.");
  }
  return value.trim();
}
function minor(value: string) {
  const cleaned = value.trim().replace(/,/g, "");
  if (!/^\d{1,7}(?:\.\d{1,2})?$/.test(cleaned)) return null;
  const [whole, decimals = ""] = cleaned.split(".");
  const cents = Number(whole) * 100 + Number(decimals.padEnd(2, "0"));
  return Number.isSafeInteger(cents) && cents <= 100_000_000 ? cents : null;
}
function milli(value: string) {
  if (!/^\d{1,4}(?:\.\d{1,3})?$/.test(value.trim())) return null;
  const [whole, fraction = ""] = value.trim().split(".");
  const units = Number(whole) * 1000 + Number(fraction.padEnd(3, "0"));
  return Number.isSafeInteger(units) && units >= 1 && units <= 1_000_000 ? units : null;
}

export function compileQuote(input: QuoteInput) {
  if (!Array.isArray(input.lines) || !input.lines.length || input.lines.length > MAX_LINES) {
    throw new Error("Add between 1 and 20 quote line items.");
  }
  const businessName = text(input.sellerName, "Business name", 256);
  const businessAbn = validateAbn(input.sellerAbn, "business ABN", false);
  const reference = text(input.reference, "Quote reference", 40);
  const recipientName = text(input.customerName, "Customer name", 200);
  const lines = input.lines.map((line, index) => {
    const qty = milli(line.quantity);
    const price = minor(line.unitPrice);
    if (qty === null) throw new Error("Line " + (index + 1) + ": enter a positive quantity up to 1000.");
    if (price === null) throw new Error("Line " + (index + 1) + ": enter a unit price up to $1,000,000.");
    const treatment: InvoiceTaxTreatment = input.gstRegistered ? line.treatment : "out-of-scope";
    return {
      description: text(line.description, "Line " + (index + 1), 500),
      quantityMilli: qty, unitPriceMinor: price, taxTreatment: treatment,
    };
  });
  const draft = validateInvoiceDraft({
    number: reference,
    issueDate: input.quoteDate,
    dueDate: input.validUntil,
    customerName: recipientName,
    customerAddress: text(input.customerAddress, "Customer address", 1000, false),
    sellerAddress: text(input.sellerAddress, "Business address", 1000, false),
    gstRegistered: input.gstRegistered,
    notes: text(input.notes, "Notes", 2000, false),
    paymentInstructions: "",
    lines,
  });
  if (!draft.issueDate || !draft.dueDate) throw new Error("Enter a quote date and valid-until date.");
  return {
    sellerName: businessName,
    sellerAbn: businessAbn,
    reference,
    draft,
    totals: calculateInvoice(draft),
  };
}
