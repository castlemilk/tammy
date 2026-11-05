/** A customer-facing estimate PDF. Never call the issued invoice renderer:
 * a quote isn't a tax invoice, receivable, or accounting entry.
 */
import { renderDocument } from "@formepdf/core";
import { Document, Page, View } from "@formepdf/react";
import { createElement as h } from "react";
import {
  calculateInvoice,
  formatAud,
  formatQuantity,
  type InvoiceDraftData,
  type InvoiceTaxTreatment,
  InvoiceValidationError,
  validateAbn,
  validateInvoiceDraft,
} from "./model.ts";
import { assertInvoicePdfReady } from "./pdf-policy.ts";
import {
  KeyValue,
  PageFooter,
  PageHeader,
  Table,
  TableCell,
  TableRow,
  Text,
  theme,
} from "./pdfcn/components.ts";

export interface QuotePdfData {
  readonly draft: InvoiceDraftData;
  readonly sellerName: string;
  readonly sellerAbn: string;
}
const taxLabel: Record<InvoiceTaxTreatment, string> = {
  taxable: "10% GST",
  "gst-free": "GST-free",
  "input-taxed": "Input-taxed",
  "out-of-scope": "No GST",
};
function note(label: string, value: string) {
  return h(
    View,
    { style: { flex: 1, paddingRight: 12 } },
    h(
      Text,
      {
        style: {
          fontSize: 8,
          fontWeight: 700,
          color: theme.colors.mutedForeground,
          marginBottom: 4,
        },
      },
      label,
    ),
    h(Text, { style: { fontSize: 10 } }, value),
  );
}
/** Reuses invoice cent arithmetic and font policy but never issues or posts. */
export async function renderQuotePdf(value: QuotePdfData): Promise<Uint8Array> {
  const draft = validateInvoiceDraft(value.draft);
  const name = typeof value.sellerName === "string" ? value.sellerName.trim() : "";
  if (!name || !draft.number || !draft.issueDate || !draft.dueDate || !draft.customerName) {
    throw new InvoiceValidationError("Quote needs a seller, customer, reference and valid dates.");
  }
  const abn = validateAbn(value.sellerAbn, "sellerAbn", false);
  assertInvoicePdfReady(draft, name);
  const totals = calculateInvoice(draft);
  const rows = draft.lines.map((item, i) => {
    const total = totals.lines[i];
    if (!total) throw new Error("Missing quote line total");
    return h(
      TableRow,
      { key: "line-" + i, stripe: i % 2 === 1 },
      h(TableCell, { key: "description" }, item.description + "\n" + taxLabel[item.taxTreatment]),
      h(TableCell, { key: "quantity", align: "center" }, formatQuantity(item.quantityMilli)),
      h(TableCell, { key: "price", align: "right" }, formatAud(item.unitPriceMinor)),
      h(TableCell, { key: "gst", align: "right" }, formatAud(total.gstMinor)),
      h(TableCell, { key: "amount", align: "right" }, formatAud(total.totalMinor)),
    );
  });
  const pdf = h(
    Document,
    {
      title: "QUOTE " + draft.number,
      author: name,
      creator: "Tammy · pdfcn / Forme",
      lang: "en-AU",
      tagged: true,
      style: { fontFamily: "Noto Sans", fontSize: 10, color: theme.colors.foreground },
    },
    h(
      Page,
      { size: "A4", margin: { top: 40, left: 40, right: 40, bottom: 36 } },
      h(PageFooter, { leftText: "QUOTE " + draft.number + " · AUD · NOT AN INVOICE" }),
      h(PageHeader, {
        title: "QUOTE",
        subtitle: name,
        rightText: (abn ? "ABN " + abn + " · " : "") + "All amounts in Australian dollars (AUD)",
      }),
      h(
        View,
        { style: { flexDirection: "row", marginBottom: theme.spacing.sectionGap } },
        note("QUOTE REFERENCE", draft.number),
        note("PREPARED", draft.issueDate),
        note("VALID UNTIL", draft.dueDate),
      ),
      h(
        View,
        { style: { flexDirection: "row", marginBottom: theme.spacing.sectionGap } },
        note("PREPARED BY", name + (draft.sellerAddress ? "\n" + draft.sellerAddress : "")),
        note(
          "PREPARED FOR",
          draft.customerName + (draft.customerAddress ? "\n" + draft.customerAddress : ""),
        ),
      ),
      h(
        Table,
        {},
        h(
          TableRow,
          { key: "header", header: true },
          h(TableCell, { key: "description", header: true }, "Description"),
          h(TableCell, { key: "quantity", header: true, align: "center" }, "Qty"),
          h(TableCell, { key: "price", header: true, align: "right" }, "Unit ex GST"),
          h(TableCell, { key: "gst", header: true, align: "right" }, "GST"),
          h(TableCell, { key: "amount", header: true, align: "right" }, "Estimated total"),
        ),
        ...rows,
      ),
      h(
        View,
        {
          wrap: false,
          style: {
            flexDirection: "row",
            justifyContent: "flex-end",
            marginTop: 18,
            marginBottom: 20,
          },
        },
        h(KeyValue, {
          items: [
            { key: "Subtotal (ex GST)", value: formatAud(totals.subtotalMinor) },
            { key: "GST", value: formatAud(totals.gstMinor) },
            { key: "Quoted total (AUD)", value: formatAud(totals.totalMinor), prominent: true },
          ],
        }),
      ),
      h(
        Text,
        { style: { fontSize: 9, color: theme.colors.mutedForeground, marginTop: 12 } },
        "Estimate only. Not a tax invoice or a request for payment. No goods or services have been issued by this document.",
      ),
      draft.notes
        ? h(
            View,
            { style: { marginTop: 16 } },
            h(Text, { style: { fontSize: 9, fontWeight: 700 } }, "SCOPE AND ASSUMPTIONS"),
            h(Text, {}, draft.notes),
          )
        : null,
      h(
        Text,
        { style: { fontSize: 8, color: theme.colors.mutedForeground, marginTop: 13 } },
        draft.gstRegistered
          ? "GST is estimated at 10% on the taxable quoted lines; the final invoice may differ if the scope or tax treatment changes."
          : "Seller is not GST registered. No GST has been charged in this estimate.",
      ),
    ),
  );
  return renderDocument(pdf);
}
