import { createValidatedExport } from "./validated-export";
export interface InvoiceExport<Input = unknown> {
  export(request: Input): Promise<{ readonly saved: boolean }>;
}
/** The renderer supplies a persisted ID, never PDF bytes or a filesystem path. */
export function createInvoiceExport<Input, Invoice extends { readonly number: string }>(options: {
  readonly prepare: (
    request: Input,
  ) => Promise<{ readonly invoice: Invoice; readonly guard: () => void }>;
  readonly render: (invoice: Invoice) => Promise<Uint8Array>;
  readonly select: (
    defaultPath: string,
  ) => Promise<{ readonly canceled: boolean; readonly filePath?: string }>;
}): InvoiceExport<Input> {
  const failure = () => new Error("INVOICE_EXPORT_FAILED");
  return createValidatedExport({
    prepare: async (request: Input) => {
      const { invoice, guard } = await options.prepare(request);
      return {
        snapshot: invoice,
        guard,
        extension: "pdf" as const,
        filename: `${invoice.number.replace(/[^a-zA-Z0-9_-]/g, "_").slice(0, 80) || "invoice-draft"}.pdf`,
      };
    },
    render: options.render,
    select: options.select,
    validateBytes: (bytes) => {
      if (
        bytes.byteLength < 8 ||
        bytes.byteLength > 16 * 1024 * 1024 ||
        new TextDecoder().decode(bytes.subarray(0, 5)) !== "%PDF-"
      )
        throw failure();
    },
    renderError: failure,
    saveError: failure,
  });
}
