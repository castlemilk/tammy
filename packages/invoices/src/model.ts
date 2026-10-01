export type InvoiceTaxTreatment = "taxable" | "gst-free" | "input-taxed" | "out-of-scope";

export interface InvoiceLineData {
  description: string;
  quantityMilli: number;
  unitPriceMinor: number;
  unitCostMinor?: number;
  taxTreatment: InvoiceTaxTreatment;
  sourceDocumentId?: string;
}

export interface InvoiceDraftData {
  number: string;
  issueDate: string;
  dueDate: string;
  customerName: string;
  customerAddress: string;
  customerAbn?: string;
  sellerAddress: string;
  gstRegistered: boolean;
  notes: string;
  paymentInstructions: string;
  lines: InvoiceLineData[];
}

export interface InvoiceLineTotals {
  netMinor: number;
  gstMinor: number;
  totalMinor: number;
  costMinor: number;
}

export interface InvoiceTotals {
  lines: InvoiceLineTotals[];
  subtotalMinor: number;
  gstMinor: number;
  totalMinor: number;
  costMinor: number;
  marginMinor: number;
}

export class InvoiceValidationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "InvoiceValidationError";
  }
}

const MAX_TOTAL = 1_000_000_000_000n;
const LINE_KEYS = new Set([
  "description",
  "quantityMilli",
  "unitPriceMinor",
  "unitCostMinor",
  "taxTreatment",
  "sourceDocumentId",
]);
const DRAFT_KEYS = new Set([
  "number",
  "issueDate",
  "dueDate",
  "customerName",
  "customerAddress",
  "customerAbn",
  "sellerAddress",
  "gstRegistered",
  "notes",
  "paymentInstructions",
  "lines",
]);
const TAX_TREATMENTS = new Set<InvoiceTaxTreatment>([
  "taxable",
  "gst-free",
  "input-taxed",
  "out-of-scope",
]);
const DOCUMENT_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
export const INVOICE_DRAFT_JSON_SCHEMA = {
  type: "object",
  additionalProperties: false,
  required: [
    "number",
    "issueDate",
    "dueDate",
    "customerName",
    "customerAddress",
    "sellerAddress",
    "gstRegistered",
    "notes",
    "paymentInstructions",
    "lines",
  ],
  properties: {
    number: { type: "string", maxLength: 40 },
    issueDate: {
      type: "string",
      pattern: "^(?:\\d{4}-\\d{2}-\\d{2})?$",
      description: "YYYY-MM-DD calendar date, or empty in an incomplete draft.",
    },
    dueDate: {
      type: "string",
      pattern: "^(?:\\d{4}-\\d{2}-\\d{2})?$",
      description: "YYYY-MM-DD date not before issueDate, or empty in an incomplete draft.",
    },
    customerName: { type: "string", maxLength: 200 },
    customerAddress: { type: "string", maxLength: 1000 },
    customerAbn: { type: "string", maxLength: 20 },
    sellerAddress: { type: "string", maxLength: 1000 },
    gstRegistered: {
      type: "boolean",
      description: "Seller GST-registration status; never inferred from an ABN.",
    },
    notes: { type: "string", maxLength: 2000 },
    paymentInstructions: { type: "string", maxLength: 1000 },
    lines: {
      type: "array",
      minItems: 1,
      maxItems: 50,
      items: {
        type: "object",
        additionalProperties: false,
        required: ["description", "quantityMilli", "unitPriceMinor", "taxTreatment"],
        properties: {
          description: { type: "string", minLength: 1, maxLength: 500 },
          quantityMilli: {
            type: "integer",
            minimum: 1,
            maximum: 1000000,
            description: "Quantity in thousandths: 1500 means 1.5 units.",
          },
          unitPriceMinor: {
            type: "integer",
            minimum: 0,
            maximum: 100000000,
            description: "Ex-GST selling price per unit, in AUD cents.",
          },
          unitCostMinor: {
            type: "integer",
            minimum: 0,
            maximum: 100000000,
            description: "Private per-unit cost in AUD cents; excluded from customer PDFs.",
          },
          taxTreatment: {
            type: "string",
            enum: ["taxable", "gst-free", "input-taxed", "out-of-scope"],
          },
          sourceDocumentId: {
            type: "string",
            pattern: DOCUMENT_ID.source,
            description:
              "Optional reviewed source document UUIDv7; application validates ownership.",
          },
        },
      },
    },
  },
};

function invalid(field: string, reason: string): never {
  throw new InvoiceValidationError(`${field}: ${reason}`);
}

function object(value: unknown, field: string, keys: ReadonlySet<string>): Record<string, unknown> {
  if (
    value === null ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    Object.getPrototypeOf(value) !== Object.prototype
  ) {
    invalid(field, "must be an object");
  }
  for (const key of Reflect.ownKeys(value)) {
    if (typeof key !== "string" || !keys.has(key))
      invalid(`${field}.${String(key)}`, "is not supported");
  }
  return value as Record<string, unknown>;
}

function text(value: unknown, field: string, max: number, required = false): string {
  if (
    typeof value !== "string" ||
    !value.isWellFormed() ||
    Array.from(value).length > max ||
    Array.from(value).some((character) => {
      const code = character.charCodeAt(0);
      return (code < 32 && code !== 9 && code !== 10 && code !== 13) || code === 127;
    })
  ) {
    invalid(field, `must be text of at most ${max} characters without control characters`);
  }
  const normalized = value.trim();
  if (required && normalized.length === 0) invalid(field, "is required");
  return normalized;
}

function integer(value: unknown, field: string, min: number, max: number): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < min || value > max) {
    invalid(field, `must be an integer from ${min} to ${max}`);
  }
  return value;
}

function date(value: unknown, field: string): string {
  if (value === "") return "";
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value))
    invalid(field, "must be a YYYY-MM-DD date");
  const parsed = new Date(`${value}T00:00:00.000Z`);
  if (
    !Number.isFinite(parsed.getTime()) ||
    parsed.toISOString().slice(0, 10) !== value ||
    value < "1900-01-01" ||
    value > "9999-12-31"
  ) {
    invalid(field, "must be a valid calendar date from 1900 onwards");
  }
  return value;
}

/** Syntax/checksum validation only. This does not verify registration or GST status. */
export function validateAbn(value: unknown, field = "abn", required = false): string {
  const normalized = text(value, field, 20).replace(/ /g, "");
  if (normalized.length === 0 && !required) return "";
  if (!/^\d{11}$/.test(normalized)) invalid(field, "must contain a valid 11-digit ABN");
  const weights = [10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19];
  let checksum = 0;
  for (const [index, weight] of weights.entries()) {
    checksum += (Number(normalized[index]) - (index === 0 ? 1 : 0)) * weight;
  }
  if (checksum % 89 !== 0) invalid(field, "has an invalid ABN checksum");
  return normalized;
}

function line(value: unknown, index: number, gstRegistered: boolean): InvoiceLineData {
  const field = `lines[${index}]`;
  const input = object(value, field, LINE_KEYS);
  if (
    typeof input.taxTreatment !== "string" ||
    !TAX_TREATMENTS.has(input.taxTreatment as InvoiceTaxTreatment)
  ) {
    invalid(`${field}.taxTreatment`, "must be taxable, gst-free, input-taxed or out-of-scope");
  }
  if (!gstRegistered && input.taxTreatment === "taxable")
    invalid("gstRegistered", "must be true to charge GST on taxable lines");
  const result: InvoiceLineData = {
    description: text(input.description, `${field}.description`, 500, true),
    quantityMilli: integer(input.quantityMilli, `${field}.quantityMilli`, 1, 1_000_000),
    unitPriceMinor: integer(input.unitPriceMinor, `${field}.unitPriceMinor`, 0, 100_000_000),
    taxTreatment: input.taxTreatment as InvoiceTaxTreatment,
  };
  if (input.unitCostMinor !== undefined)
    result.unitCostMinor = integer(input.unitCostMinor, `${field}.unitCostMinor`, 0, 100_000_000);
  if (input.sourceDocumentId !== undefined) {
    const reference = text(input.sourceDocumentId, `${field}.sourceDocumentId`, 36);
    if (!DOCUMENT_ID.test(reference)) invalid(`${field}.sourceDocumentId`, "must be a UUIDv7");
    result.sourceDocumentId = reference.toLowerCase();
  }
  return result;
}

/** Strict data-only draft validation for UI, API and MCP proposal inputs. */
export function validateInvoiceDraft(value: unknown): InvoiceDraftData {
  const input = object(value, "invoice", DRAFT_KEYS);
  if (typeof input.gstRegistered !== "boolean") invalid("gstRegistered", "must be a boolean");
  if (!Array.isArray(input.lines) || input.lines.length < 1 || input.lines.length > 50)
    invalid("lines", "must contain 1 to 50 line items");
  const result: InvoiceDraftData = {
    number: text(input.number, "number", 40),
    issueDate: date(input.issueDate, "issueDate"),
    dueDate: date(input.dueDate, "dueDate"),
    customerName: text(input.customerName, "customerName", 200),
    customerAddress: text(input.customerAddress, "customerAddress", 1000),
    sellerAddress: text(input.sellerAddress, "sellerAddress", 1000),
    gstRegistered: input.gstRegistered,
    notes: text(input.notes, "notes", 2000),
    paymentInstructions: text(input.paymentInstructions, "paymentInstructions", 1000),
    lines: input.lines.map((value, index) => line(value, index, input.gstRegistered as boolean)),
  };
  if (result.issueDate && result.dueDate && result.dueDate < result.issueDate)
    invalid("dueDate", "cannot precede issueDate");
  if (input.customerAbn !== undefined)
    result.customerAbn = validateAbn(input.customerAbn, "customerAbn");
  calculateInvoice(result);
  return result;
}

const halfUp = (numerator: bigint, divisor: bigint): bigint => (numerator + divisor / 2n) / divisor;

/** Monetary multiplication uses integers; GST rounds half-up on each line's net cents. */
export function calculateInvoice(draft: InvoiceDraftData): InvoiceTotals {
  if (!Array.isArray(draft.lines) || draft.lines.length < 1 || draft.lines.length > 50)
    invalid("lines", "must contain 1 to 50 line items");
  let subtotal = 0n;
  let gst = 0n;
  let cost = 0n;
  const lines = draft.lines.map((item, index): InvoiceLineTotals => {
    const quantity = BigInt(
      integer(item.quantityMilli, `lines[${index}].quantityMilli`, 1, 1_000_000),
    );
    const price = BigInt(
      integer(item.unitPriceMinor, `lines[${index}].unitPriceMinor`, 0, 100_000_000),
    );
    const unitCost = BigInt(
      integer(item.unitCostMinor ?? 0, `lines[${index}].unitCostMinor`, 0, 100_000_000),
    );
    if (!TAX_TREATMENTS.has(item.taxTreatment))
      invalid(`lines[${index}].taxTreatment`, "is not supported");
    if (!draft.gstRegistered && item.taxTreatment === "taxable")
      invalid("gstRegistered", "must be true to charge GST on taxable lines");
    const netMinor = halfUp(quantity * price, 1000n);
    const gstMinor = item.taxTreatment === "taxable" ? halfUp(netMinor, 10n) : 0n;
    const costMinor = halfUp(quantity * unitCost, 1000n);
    subtotal += netMinor;
    gst += gstMinor;
    cost += costMinor;
    if (subtotal + gst > MAX_TOTAL || cost > MAX_TOTAL)
      invalid("total", "cannot exceed $10,000,000,000.00");
    return {
      netMinor: Number(netMinor),
      gstMinor: Number(gstMinor),
      totalMinor: Number(netMinor + gstMinor),
      costMinor: Number(costMinor),
    };
  });
  return {
    lines,
    subtotalMinor: Number(subtotal),
    gstMinor: Number(gst),
    totalMinor: Number(subtotal + gst),
    costMinor: Number(cost),
    marginMinor: Number(subtotal - cost),
  };
}

/** An issued customer-facing invoice must have the identities and addresses needed for review. */
export function validateInvoiceForIssue(
  draft: InvoiceDraftData,
  seller: { name: string; abn: string },
): InvoiceDraftData {
  const validated = validateInvoiceDraft(draft);
  text(validated.number, "number", 40, true);
  text(validated.issueDate, "issueDate", 10, true);
  text(validated.dueDate, "dueDate", 10, true);
  text(validated.customerName, "customerName", 200, true);
  text(validated.customerAddress, "customerAddress", 1000, true);
  text(validated.sellerAddress, "sellerAddress", 1000, true);
  text(seller.name, "seller.name", 256, true);
  validateAbn(seller.abn, "seller.abn", true);
  return validated;
}

export function formatAud(minor: number): string {
  if (!Number.isSafeInteger(minor)) invalid("amount", "must be safe integer cents");
  const amount = Math.abs(minor);
  return `${minor < 0 ? "-" : ""}$${Math.floor(amount / 100).toLocaleString("en-AU")}.${String(amount % 100).padStart(2, "0")}`;
}

export function formatQuantity(quantityMilli: number): string {
  integer(quantityMilli, "quantityMilli", 1, 1_000_000);
  const decimal = String(quantityMilli % 1000)
    .padStart(3, "0")
    .replace(/0+$/, "");
  return `${Math.floor(quantityMilli / 1000)}${decimal.length ? `.${decimal}` : ""}`;
}
