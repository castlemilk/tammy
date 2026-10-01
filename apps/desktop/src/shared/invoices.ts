import { create } from "@bufbuild/protobuf";
import { TaxTreatment } from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  type Invoice,
  InvoiceDraftSchema,
  InvoiceState,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import {
  calculateInvoice,
  type InvoiceDraftData,
  type InvoiceTaxTreatment,
  validateInvoiceDraft,
} from "@tammy/invoices/model";

const taxValues = {
  taxable: TaxTreatment.TAXABLE,
  "gst-free": TaxTreatment.GST_FREE,
  "input-taxed": TaxTreatment.INPUT_TAXED,
  "out-of-scope": TaxTreatment.OUT_OF_SCOPE,
} as const;

export function invoiceDraftToProto(value: InvoiceDraftData) {
  const draft = validateInvoiceDraft(value);
  return create(InvoiceDraftSchema, {
    ...draft,
    customerAbn: draft.customerAbn ?? "",
    lines: draft.lines.map((line) => ({
      ...line,
      unitPriceMinor: BigInt(line.unitPriceMinor),
      unitCostMinor: line.unitCostMinor === undefined ? undefined : BigInt(line.unitCostMinor),
      taxTreatment: taxValues[line.taxTreatment],
    })),
  });
}

export function invoiceToRecord(invoice: Invoice) {
  if (
    !invoice.draft ||
    ![InvoiceState.DRAFT, InvoiceState.ISSUED].includes(invoice.state) ||
    invoice.version < 1n ||
    invoice.version > BigInt(Number.MAX_SAFE_INTEGER)
  )
    throw new Error("INVALID_INVOICE");
  const draft = validateInvoiceDraft({
    number: invoice.draft.number,
    issueDate: invoice.draft.issueDate,
    dueDate: invoice.draft.dueDate,
    customerName: invoice.draft.customerName,
    customerAddress: invoice.draft.customerAddress,
    ...(invoice.draft.customerAbn ? { customerAbn: invoice.draft.customerAbn } : {}),
    sellerAddress: invoice.draft.sellerAddress,
    gstRegistered: invoice.draft.gstRegistered,
    notes: invoice.draft.notes,
    paymentInstructions: invoice.draft.paymentInstructions,
    lines: invoice.draft.lines.map((line) => ({
      description: line.description,
      quantityMilli: line.quantityMilli,
      unitPriceMinor: Number(line.unitPriceMinor),
      ...(line.unitCostMinor === undefined ? {} : { unitCostMinor: Number(line.unitCostMinor) }),
      taxTreatment: Object.entries(taxValues).find(
        ([, value]) => value === line.taxTreatment,
      )?.[0] as InvoiceTaxTreatment,
      ...(line.sourceDocumentId ? { sourceDocumentId: line.sourceDocumentId } : {}),
    })),
  });
  const totals = calculateInvoice(draft);
  if (
    BigInt(totals.subtotalMinor) !== invoice.subtotalMinor ||
    BigInt(totals.gstMinor) !== invoice.gstMinor ||
    BigInt(totals.totalMinor) !== invoice.totalMinor ||
    BigInt(totals.costMinor) !== invoice.costMinor
  )
    throw new Error("INVALID_INVOICE_TOTALS");
  return {
    id: invoice.id,
    version: Number(invoice.version),
    state: invoice.state === InvoiceState.ISSUED ? ("issued" as const) : ("draft" as const),
    sellerName: invoice.sellerName,
    sellerAbn: invoice.sellerAbn,
    draft,
  };
}
