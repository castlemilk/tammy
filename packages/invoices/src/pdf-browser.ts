/** Browser-only entry for Forme PDF output in the public Tammy tools.
 * Keeps the desktop renderer and its integer-cent model unchanged.
 * Forme's worker target supports explicit WASM initialization without Vite's
 * unsupported bundler-target inline .wasm import.
 */
import { init } from "@formepdf/core/worker";
import wasmUrl from "@formepdf/core/pkg-web/forme_bg.wasm?url";
import type { InvoicePdfData } from "./pdf.ts";
import { renderInvoicePdf } from "./pdf.ts";

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
