import type { InvoiceDraftData } from "./model.ts";
import { type InvoicePdfLogo, validateInvoicePdfLogo } from "./pdf-logo.ts";
import { assertInvoicePdfTextSupported } from "./pdfcn/font-coverage.ts";

export { type InvoicePdfLogo, validateInvoicePdfLogo } from "./pdf-logo.ts";
export { InvoicePdfUnsupportedTextError } from "./pdfcn/font-coverage.ts";

/** Renderer-safe font preflight shared by issue confirmation and PDF rendering.
 * General invoice/issue validation is separate; this checks only rendered text
 * against the exact packaged font coverage and does not alter invoice data.
 */
export function assertInvoicePdfReady(
  draft: InvoiceDraftData,
  sellerName: string,
  logo?: InvoicePdfLogo,
): void {
  if (logo !== undefined) validateInvoicePdfLogo(logo);
  assertInvoicePdfTextSupported(sellerName, "sellerName");
  for (const field of [
    "number",
    "sellerAddress",
    "customerName",
    "customerAddress",
    "notes",
    "paymentInstructions",
  ] as const) {
    assertInvoicePdfTextSupported(draft[field], field);
  }
  for (const [index, item] of draft.lines.entries())
    assertInvoicePdfTextSupported(item.description, `lines[${index}].description`);
}
