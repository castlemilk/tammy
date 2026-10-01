/** Adapted from pdfcn's Forme invoice-modern template. MIT, Copyright (c) 2026 Shadcn Labs. */

import { renderDocument } from "@formepdf/core";
import { Document, Page, View, Watermark } from "@formepdf/react";
import { createElement as h } from "react";
import type { InvoiceDraftData, InvoiceTaxTreatment } from "./model.ts";
import {
  calculateInvoice,
  formatAud,
  formatQuantity,
  InvoiceValidationError,
  validateAbn,
  validateInvoiceDraft,
  validateInvoiceForIssue,
} from "./model.ts";
import { type InvoicePdfLogo, validateInvoicePdfLogo } from "./pdf-logo.ts";
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

export { type InvoicePdfLogo, validateInvoicePdfLogo } from "./pdf-logo.ts";
export { InvoicePdfUnsupportedTextError } from "./pdf-policy.ts";

export interface InvoicePdfData extends InvoiceDraftData {
  id?: string;
  sellerName: string;
  sellerAbn: string;
  state: "draft" | "issued";
  logo?: InvoicePdfLogo;
}

function metadata(label: string, value: string, width?: number) {
  return h(
    View,
    { style: { ...(width === undefined ? { flex: 1 } : { width }), paddingRight: 12 } },
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
    h(Text, { style: { fontSize: 10, marginBottom: 0 } }, value),
  );
}

const treatmentLabels: Record<InvoiceTaxTreatment, string> = {
  taxable: "10% GST",
  "gst-free": "GST-free",
  "input-taxed": "Input-taxed",
  "out-of-scope": "No GST",
};

function validatePdfInput(value: InvoicePdfData): InvoicePdfData {
  const { id, sellerName, sellerAbn, state, logo, ...draftValue } = value;
  if (state !== "draft" && state !== "issued")
    throw new InvoiceValidationError("state: must be draft or issued");
  if (
    typeof sellerName !== "string" ||
    sellerName.trim().length === 0 ||
    !sellerName.isWellFormed() ||
    Array.from(sellerName).length > 256 ||
    Array.from(sellerName).some((character) => {
      const code = character.charCodeAt(0);
      return (code < 32 && code !== 9 && code !== 10 && code !== 13) || code === 127;
    })
  )
    throw new InvoiceValidationError(
      "sellerName: is required and must be at most 256 characters without control characters",
    );
  const abn = validateAbn(sellerAbn, "sellerAbn", true);
  const draft =
    state === "issued"
      ? validateInvoiceForIssue(validateInvoiceDraft(draftValue), { name: sellerName, abn })
      : validateInvoiceDraft(draftValue);
  return {
    ...draft,
    ...(id === undefined ? {} : { id }),
    sellerName: sellerName.trim(),
    sellerAbn: abn,
    state,
    ...(logo === undefined ? {} : { logo: validateInvoicePdfLogo(logo) }),
  };
}

/** Customer-facing projection deliberately never renders cost, margin, source IDs or embedded input JSON. */
export async function renderInvoicePdf(value: InvoicePdfData): Promise<Uint8Array> {
  const data = validatePdfInput(value);
  assertInvoicePdfReady(data, data.sellerName, data.logo);
  const totals = calculateInvoice(data);
  const invoiceLabel = data.gstRegistered ? "Tax Invoice" : "Invoice";
  const title = `${data.state === "draft" ? "DRAFT · " : ""}${invoiceLabel}`;
  const rows = data.lines.map((item, index) => {
    const lineTotals = totals.lines[index];
    if (!lineTotals) throw new Error("Missing invoice line totals");
    return h(
      TableRow,
      { key: `line-${index}`, stripe: index % 2 === 1 },
      h(
        TableCell,
        { key: "description" },
        `${item.description}\n${treatmentLabels[item.taxTreatment]}`,
      ),
      h(TableCell, { key: "quantity", align: "center" }, formatQuantity(item.quantityMilli)),
      h(TableCell, { key: "price", align: "right" }, formatAud(item.unitPriceMinor)),
      h(TableCell, { key: "gst", align: "right" }, formatAud(lineTotals.gstMinor)),
      h(TableCell, { key: "amount", align: "right" }, formatAud(lineTotals.totalMinor)),
    );
  });
  const document = h(
    Document,
    {
      title: `${title} ${data.number || "unnumbered"}`,
      author: data.sellerName,
      creator: "Tammy · pdfcn / Forme",
      lang: "en-AU",
      tagged: true,
      style: { fontFamily: "Noto Sans", fontSize: 10, color: theme.colors.foreground },
    },
    h(
      Page,
      { size: "A4", margin: { top: 40, left: 40, right: 40, bottom: 36 } },
      h(PageFooter, {
        leftText: `${data.number || "Unnumbered draft"} · AUD${data.state === "draft" ? " · DRAFT — not issued" : ""}`,
      }),
      data.state === "draft"
        ? h(Watermark, { text: "DRAFT", fontSize: 72, color: "rgba(32,78,67,0.06)", angle: -35 })
        : null,
      h(PageHeader, {
        title,
        subtitle: data.sellerName,
        rightText: `ABN ${data.sellerAbn} · All amounts in Australian dollars (AUD)`,
        ...(data.logo ? { logo: data.logo } : {}),
      }),
      h(
        View,
        { style: { flexDirection: "row", marginBottom: theme.spacing.sectionGap } },
        metadata("INVOICE NUMBER", data.number || "Unnumbered draft"),
        metadata("ISSUE DATE", data.issueDate || "To be completed"),
        metadata("DUE DATE", data.dueDate || "To be completed"),
      ),
      h(
        View,
        { style: { flexDirection: "row", marginBottom: theme.spacing.sectionGap } },
        metadata("FROM", `${data.sellerName}\n${data.sellerAddress || "Address to be completed"}`),
        metadata(
          "BILLED TO",
          `${data.customerName || "Customer to be completed"}\n${data.customerAddress || "Address to be completed"}${data.customerAbn ? `\nABN ${data.customerAbn}` : ""}`,
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
          h(TableCell, { key: "amount", header: true, align: "right" }, "Amount incl GST"),
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
            { key: "Total due (AUD)", value: formatAud(totals.totalMinor), prominent: true },
          ],
        }),
      ),
      h(
        Text,
        { style: { fontSize: 8, color: theme.colors.mutedForeground } },
        data.gstRegistered
          ? "GST is calculated at 10% on taxable line amounts and rounded per line. Other tax treatments are shown alongside each item."
          : "Not registered for GST. No GST has been charged.",
      ),
      data.paymentInstructions
        ? h(
            View,
            { style: { marginTop: 16 } },
            h(Text, { style: { fontSize: 9, fontWeight: 700 } }, "PAYMENT INSTRUCTIONS"),
            h(Text, {}, data.paymentInstructions),
          )
        : null,
      data.notes
        ? h(
            View,
            { style: { marginTop: 14 } },
            h(Text, { style: { fontSize: 9, fontWeight: 700 } }, "NOTES"),
            h(Text, {}, data.notes),
          )
        : null,
    ),
  );
  const bytes = await renderDocument(document);
  // Forme may silently omit undecodable raster data. A requested logo must be
  // represented by an actual PDF image object before export or issue succeeds.
  if (data.logo && !/\/Subtype\s*\/Image\b/.test(new TextDecoder("latin1").decode(bytes)))
    throw new InvoiceValidationError("logo: image could not be rendered");
  return bytes;
}
