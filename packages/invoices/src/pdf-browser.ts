/** Browser-only entry for Forme PDF output in the public Tammy tools.
 * Keeps the desktop renderer and its integer-cent model unchanged.
 * Forme's worker target supports explicit WASM initialization without Vite's
 * unsupported bundler-target inline .wasm import.
 */

import wasmUrl from "@formepdf/core/pkg-web/forme_bg.wasm?url";
import { init } from "@formepdf/core/worker";
import type { InvoicePdfData } from "./pdf.ts";
import { renderInvoicePdf } from "./pdf.ts";
import { type QuotePdfData, renderQuotePdf } from "./quote-pdf.ts";

let initPromise: Promise<void> | undefined;

export async function renderBrowserInvoicePdf(data: InvoicePdfData): Promise<Uint8Array> {
  initPromise ??= (async () => {
    const response = await fetch(wasmUrl);
    if (!response.ok) throw new Error("Unable to load the local PDF engine. Please retry.");
    await init(await response.arrayBuffer());
  })();
  try {
    await initPromise;
  } catch (error) {
    initPromise = undefined;
    throw error;
  }
  return renderInvoicePdf(data);
}

/** Browser-only quote PDF, sharing the exact local WASM engine with invoices. */
export async function renderBrowserQuotePdf(data: QuotePdfData): Promise<Uint8Array> {
  initPromise ??= (async () => {
    const response = await fetch(wasmUrl);
    if (!response.ok) throw new Error("Unable to load the local PDF engine. Please retry.");
    await init(await response.arrayBuffer());
  })();
  try {
    await initPromise;
  } catch (error) {
    initPromise = undefined;
    throw error;
  }
  return renderQuotePdf(data);
}
