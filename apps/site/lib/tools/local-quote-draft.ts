import type { QuoteInput } from "./quote";

const MAX_BYTES = 50_000;
const TEXT_LIMITS: Record<keyof Omit<QuoteInput,"gstRegistered"|"lines">,number> = {
  sellerName:256,sellerAbn:20,sellerAddress:1000,customerName:200,
  customerAddress:1000,reference:40,quoteDate:10,validUntil:10,notes:2000,
};
const keys = [...Object.keys(TEXT_LIMITS),"gstRegistered","lines"];
const lineKeys = ["description","quantity","unitPrice","treatment"];
export const QUOTE_DRAFT_KIND = "tammy-quote-draft" as const;

function plain(value: unknown): value is Record<string,unknown> {
  return Boolean(value && typeof value==="object" && !Array.isArray(value) &&
    Object.getPrototypeOf(value)===Object.prototype);
}
function exactKeys(obj: Record<string,unknown>, allow: readonly string[]): boolean {
  return Object.keys(obj).length===allow.length && Object.keys(obj).every(k=>allow.includes(k));
}
function checkedQuote(value: unknown): QuoteInput {
  if (!plain(value) || !exactKeys(value,keys) || typeof value.gstRegistered!=="boolean")
    throw new Error("Unrecognised Tammy draft format.");
  for(const [name,limit] of Object.entries(TEXT_LIMITS)){
    const v=value[name];
    if(typeof v!=="string" || !v.isWellFormed() || Array.from(v).length>limit)
      throw new Error("Draft contains invalid " + name + " text.");
  }
  if (!Array.isArray(value.lines) || !value.lines.length || value.lines.length>20)
    throw new Error("Draft must have 1–20 line items.");
  for(const [i,line] of value.lines.entries()) {
    if (!plain(line) || !exactKeys(line,lineKeys) || lineKeys.slice(0,3).some(k=>
      typeof line[k]!=="string" || (line[k] as string).length>500) ||
      !["taxable","gst-free"].includes(String(line.treatment))) {
      throw new Error("Draft line "+(i+1)+" has unsupported contents.");
    }
  }
  return value as unknown as QuoteInput;
}
export function serializeQuoteDraft(value: QuoteInput): string {
  const draft = checkedQuote(value);
  const serialized = JSON.stringify({format:QUOTE_DRAFT_KIND,version:1,draft},null,2);
  if(new TextEncoder().encode(serialized).byteLength>MAX_BYTES)
    throw new Error("Draft exceeds the 50 KB limit.");
  return serialized;
}
export function parseQuoteDraft(serialized: string): QuoteInput {
  if(new TextEncoder().encode(serialized).byteLength>MAX_BYTES)
    throw new Error("Draft exceeds the 50 KB limit.");
  let envelope:unknown;
  try {envelope=JSON.parse(serialized);}catch{throw new Error("Choose a valid Tammy JSON draft.");}
  if(!plain(envelope) || !exactKeys(envelope,["format","version","draft"]) ||
     envelope.format!==QUOTE_DRAFT_KIND || envelope.version!==1)
    throw new Error("Unsupported quote draft version or format.");
  return checkedQuote(envelope.draft);
}
