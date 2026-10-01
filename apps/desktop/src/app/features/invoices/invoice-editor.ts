import type { InvoiceDraftData } from "@tammy/invoices/model";
import type { InvoiceTemplateDefaults } from "../../../shared/invoice-template";

export interface InvoiceExpense {
  readonly id: string;
  readonly description: string;
  readonly subtotalMinor: number;
  readonly gstMinor: number;
}

export interface EditorLine {
  readonly key: string;
  readonly description: string;
  readonly quantity: string;
  readonly unitPrice: string;
  readonly unitCost: string;
  readonly taxTreatment: InvoiceDraftData["lines"][number]["taxTreatment"];
  readonly sourceDocumentId?: string;
  readonly untouched?: boolean;
}

export type EditorDraft = Omit<InvoiceDraftData, "lines"> & {
  readonly lines: readonly EditorLine[];
};

export function applyInvoiceTemplate(
  editor: EditorDraft,
  defaults: InvoiceTemplateDefaults,
): EditorDraft {
  const due = new Date(`${editor.issueDate}T00:00:00Z`);
  if (!Number.isFinite(due.getTime()) || due.toISOString().slice(0, 10) !== editor.issueDate)
    throw new Error("Enter a valid issue date before applying the template.");
  due.setUTCDate(due.getUTCDate() + defaults.paymentTermsDays);
  return {
    ...editor,
    sellerAddress: defaults.sellerAddress,
    notes: defaults.notes,
    paymentInstructions: defaults.paymentInstructions,
    dueDate: due.toISOString().slice(0, 10),
  };
}

export function blankLine(gstRegistered: boolean): EditorLine {
  return {
    key: crypto.randomUUID(),
    description: "",
    quantity: "1",
    unitPrice: "0.00",
    unitCost: "",
    taxTreatment: gstRegistered ? "taxable" : "out-of-scope",
    untouched: true,
  };
}

export function toEditor(draft: InvoiceDraftData): EditorDraft {
  return {
    ...draft,
    lines: draft.lines.map((line) => ({
      key: crypto.randomUUID(),
      description: line.description,
      quantity: decimalText(line.quantityMilli, 3).replace(/\.?0+$/, ""),
      unitPrice: decimalText(line.unitPriceMinor, 2),
      unitCost: line.unitCostMinor === undefined ? "" : decimalText(line.unitCostMinor, 2),
      taxTreatment: line.taxTreatment,
      ...(line.sourceDocumentId ? { sourceDocumentId: line.sourceDocumentId } : {}),
    })),
  };
}

export function editorToDraft(editor: EditorDraft): InvoiceDraftData {
  return {
    ...editor,
    lines: editor.lines.map((line, index) => {
      const quantityMilli = decimalToInteger(line.quantity, 3, `Quantity ${index + 1}`, 1_000_000);
      if (quantityMilli < 1) throw new Error(`Quantity ${index + 1} must be greater than zero.`);
      return {
        description: line.description,
        quantityMilli,
        unitPriceMinor: decimalToInteger(line.unitPrice, 2, `Unit price ${index + 1}`, 100_000_000),
        ...(line.unitCost.trim()
          ? {
              unitCostMinor: decimalToInteger(
                line.unitCost,
                2,
                `Unit cost ${index + 1}`,
                100_000_000,
              ),
            }
          : {}),
        taxTreatment: line.taxTreatment,
        ...(line.sourceDocumentId ? { sourceDocumentId: line.sourceDocumentId } : {}),
      };
    }),
  };
}

export function appendExpenseLines(
  editor: EditorDraft,
  selected: readonly InvoiceExpense[],
  markup: string,
): readonly EditorLine[] {
  const percent = decimalToInteger(markup, 2, "Markup", 100_000);
  const linked = new Set(editor.lines.map((line) => line.sourceDocumentId));
  const first = editor.lines[0];
  const emptyPlaceholder =
    editor.lines.length === 1 &&
    first?.untouched &&
    !first.description &&
    first.quantity === "1" &&
    first.unitPrice === "0.00" &&
    first.unitCost === "" &&
    !first.sourceDocumentId;
  const existing = emptyPlaceholder ? [] : editor.lines;
  if (existing.length + selected.length > 50)
    throw new Error(
      "An invoice can contain at most 50 line items. Remove a line or select fewer expenses.",
    );
  const additions = selected.map((expense): EditorLine => {
    if (linked.has(expense.id))
      throw new Error("This expense has already been added to the invoice.");
    linked.add(expense.id);
    if (
      !Number.isSafeInteger(expense.subtotalMinor) ||
      expense.subtotalMinor < 0 ||
      expense.subtotalMinor > 100_000_000
    )
      throw new Error("Expense cost is invalid or too large.");
    const price =
      expense.subtotalMinor + Math.floor((expense.subtotalMinor * percent + 5_000) / 10_000);
    if (price > 100_000_000) throw new Error("The marked-up unit price is too large.");
    return {
      ...blankLine(editor.gstRegistered),
      description: expense.description,
      unitPrice: decimalText(price, 2),
      unitCost: decimalText(expense.subtotalMinor, 2),
      sourceDocumentId: expense.id,
      untouched: false,
    };
  });
  return [...existing, ...additions];
}

function decimalText(value: number, precision: number): string {
  const scale = 10 ** precision;
  return `${Math.floor(value / scale)}.${String(value % scale).padStart(precision, "0")}`;
}

function decimalToInteger(
  value: string,
  precision: number,
  label: string,
  maximum: number,
): number {
  const clean = value.trim();
  if (!new RegExp(`^(?:\\d+(?:\\.\\d{0,${precision}})?|\\.\\d{1,${precision}})$`).test(clean))
    throw new Error(
      `${label} must be a non-negative number with at most ${precision === 2 ? "two" : "three"} decimal places.`,
    );
  const [whole = "0", fraction = ""] = clean.split(".");
  const integer = Number(`${whole || "0"}${fraction.padEnd(precision, "0")}`);
  if (!Number.isSafeInteger(integer) || integer > maximum)
    throw new Error(`${label} is too large.`);
  return integer;
}
