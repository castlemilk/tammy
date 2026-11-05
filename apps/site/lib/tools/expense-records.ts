import { parseAudCents } from "./calculations";

/**
 * An unauthenticated, in-memory record organiser.
 * Entries are NOT bookkeeping postings, tax deductions, or GST credit claims.
 */
export const EXPENSE_CATEGORIES = [
  "Supplies", "Travel", "Software", "Professional fees", "Equipment", "Other",
] as const;
export const GST_EVIDENCE = [
  "Standard GST shown", "GST-free", "No GST shown", "Review treatment",
] as const;
export type ExpenseCategory = (typeof EXPENSE_CATEGORIES)[number];
export type GstEvidence = (typeof GST_EVIDENCE)[number];

export interface ExpenseRecord {
  readonly id: string;
  readonly date: string;
  readonly supplier: string;
  readonly description: string;
  readonly category: ExpenseCategory;
  readonly grossCents: number;
  readonly businessUsePercent: number;
  readonly gstEvidence: GstEvidence;
  readonly sourceFileName: string;
}
export type ExpenseInput = Omit<ExpenseRecord, "grossCents"> & { readonly amount: string };

const MAX_ROWS = 100;
const MAX_AMOUNT_CENTS = 100_000_000_000;
const MAX_JSON_LENGTH = 2_000_000;
const MAX_FILENAME = 180;
const textLimits = { id: 80, supplier: 180, description: 500 };
function isRecord(x: unknown): x is Record<string, unknown> {
  return !!x && typeof x === "object" && !Array.isArray(x) &&
    Object.getPrototypeOf(x) === Object.prototype;
}
function exactKeys(record: Record<string, unknown>, keys: readonly string[]) {
  return Object.keys(record).length === keys.length &&
    Object.keys(record).every((key) => keys.includes(key));
}
function safeText(value: unknown, label: string, max: number, required = true): string {
  const hasControl = typeof value === "string" &&
    Array.from(value).some((char) => {
      const point = char.charCodeAt(0);
      return (point < 32 && point !== 9 && point !== 10 && point !== 13) || point === 127;
    });
  if (typeof value !== "string" || value.length > max ||
    (required && !value.trim()) || hasControl) {
    throw new Error(label + " is missing, too long, or contains unsupported characters.");
  }
  return value.trim();
}
function dateOK(value: unknown): value is string {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const date = new Date(value + "T00:00:00.000Z");
  return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value;
}
function oneOf<T extends string>(value: unknown, values: readonly T[], label: string): T {
  if (typeof value !== "string" || !values.includes(value as T)) {
    throw new Error(label + " must use a supported option.");
  }
  return value as T;
}
function percent(value: unknown): number {
  if (typeof value !== "number" || !Number.isInteger(value) ||
    value < 0 || value > 100) {
    throw new Error("Business-use percentage must be a whole number from 0 to 100.");
  }
  return value;
}
function cents(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) ||
    value <= 0 || value > MAX_AMOUNT_CENTS) {
    throw new Error("Expense amount must be a positive valid AUD cent amount.");
  }
  return value;
}
function validate(record: unknown): ExpenseRecord {
  if (!isRecord(record) || !exactKeys(record, [
    "id", "date", "supplier", "description", "category", "grossCents",
    "businessUsePercent", "gstEvidence", "sourceFileName",
  ])) throw new Error("Expense record has missing or unexpected fields.");
  if (!dateOK(record.date)) throw new Error("Choose a valid transaction date.");
  return Object.freeze({
    id: safeText(record.id, "Record ID", textLimits.id),
    date: record.date,
    supplier: safeText(record.supplier, "Supplier", textLimits.supplier),
    description: safeText(record.description, "Description", textLimits.description, false),
    category: oneOf(record.category, EXPENSE_CATEGORIES, "Category"),
    grossCents: cents(record.grossCents),
    businessUsePercent: percent(record.businessUsePercent),
    gstEvidence: oneOf(record.gstEvidence, GST_EVIDENCE, "GST evidence"),
    sourceFileName: safeText(record.sourceFileName, "Receipt filename", MAX_FILENAME, false),
  });
}

export function createExpenseRecord(input: ExpenseInput): ExpenseRecord {
  const grossCents = parseAudCents(input.amount);
  if (grossCents === null || grossCents === 0 || grossCents > MAX_AMOUNT_CENTS)
    throw new Error("Enter a positive AUD amount using up to two decimal places.");
  return validate({
    id: input.id, date: input.date, supplier: input.supplier,
    description: input.description, category: input.category,
    grossCents, businessUsePercent: input.businessUsePercent,
    gstEvidence: input.gstEvidence, sourceFileName: input.sourceFileName,
  });
}

export function summariseExpenseRecords(records: readonly ExpenseRecord[]) {
  if (records.length > MAX_ROWS) throw new Error("Maximum 100 expense records per worksheet.");
  const unique = new Set<string>();
  let grossCents = 0;
  let businessPortionCents = 0;
  let standardGstIndicationCents = 0;
  let referenceCount = 0;
  for (const entry of records) {
    const row = validate(entry);
    if (unique.has(row.id)) throw new Error("Duplicate expense record identifier.");
    unique.add(row.id);
    grossCents += row.grossCents;
    // This is a user-entered business-use allocation, NOT a deductible amount.
    businessPortionCents += Math.floor((row.grossCents * row.businessUsePercent + 50) / 100);
    if (row.gstEvidence === "Standard GST shown") {
      standardGstIndicationCents += Math.floor((row.grossCents + 5) / 11);
    }
    if (row.sourceFileName) referenceCount++;
  }
  if (![grossCents,businessPortionCents,standardGstIndicationCents].every(Number.isSafeInteger)) {
    throw new Error("Expense totals exceed the supported range.");
  }
  return Object.freeze({
    count: records.length, grossCents, businessPortionCents,
    standardGstIndicationCents, referenceCount,
  });
}
function safeCsv(value: string) {
  // A spreadsheet must not execute attacker-controlled supplier/receipt names.
  const protectedValue = /^\s*[=+\-@]/.test(value) ? "'" + value : value;
  return '"' + protectedValue.replaceAll('"', '""') + '"';
}
export function exportExpenseCSV(rows: readonly ExpenseRecord[]) {
  summariseExpenseRecords(rows);
  const header = [
    "Date","Supplier","Description","Category","Amount AUD gross",
    "Business-use percent (self-classified)","GST evidence (self-classified)",
    "Receipt file reference (not attached)",
  ];
  const contents = rows.map((row) => [
    row.date, row.supplier, row.description, row.category,
    (row.grossCents / 100).toFixed(2), String(row.businessUsePercent),
    row.gstEvidence, row.sourceFileName,
  ]);
  return [header, ...contents].map((items)=>items.map(safeCsv).join(",")).join("\r\n") + "\r\n";
}
export function exportExpenseDraft(rows: readonly ExpenseRecord[]) {
  summariseExpenseRecords(rows);
  return JSON.stringify({
    schemaVersion: 1,
    disclaimer: "Receipt files are not bundled; keep original documents separately.",
    records: rows,
  }, null, 2) + "\n";
}
export function importExpenseDraft(source: string): ExpenseRecord[] {
  if (typeof source !== "string" || source.length > MAX_JSON_LENGTH) {
    throw new Error("Expense worksheet file exceeds the 2 MB limit.");
  }
  let data: unknown;
  try { data = JSON.parse(source); }
  catch { throw new Error("This is not a valid expense worksheet JSON file."); }
  if (!isRecord(data) || !exactKeys(data,["schemaVersion","disclaimer","records"]) ||
    data.schemaVersion !== 1 || typeof data.disclaimer !== "string" ||
    !Array.isArray(data.records) || data.records.length > MAX_ROWS) {
    throw new Error("Unsupported or modified expense worksheet format.");
  }
  const result = data.records.map(validate);
  summariseExpenseRecords(result);
  return result;
}
