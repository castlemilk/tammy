import { randomUUID } from "node:crypto";
import { lstat, open, rename, unlink } from "node:fs/promises";
import path from "node:path";

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
  return {
    export: async (request) => {
      const { invoice, guard } = await options.prepare(request);
      guard();
      const filename = `${invoice.number.replace(/[^a-zA-Z0-9_-]/g, "_").slice(0, 80) || "invoice-draft"}.pdf`;
      const selected = await options.select(filename);
      guard();
      if (selected.canceled) return { saved: false };
      const target = selected.filePath;
      if (!target || !path.isAbsolute(target) || !target.toLowerCase().endsWith(".pdf"))
        throw new Error("INVOICE_EXPORT_FAILED");
      try {
        const state = await lstat(target).catch((error: NodeJS.ErrnoException) => {
          if (error.code === "ENOENT") return undefined;
          throw error;
        });
        if (state && (!state.isFile() || state.isSymbolicLink()))
          throw new Error("INVOICE_EXPORT_FAILED");
        const bytes = await options.render(invoice);
        guard();
        if (
          bytes.byteLength < 8 ||
          bytes.byteLength > 16 * 1024 * 1024 ||
          new TextDecoder().decode(bytes.subarray(0, 5)) !== "%PDF-"
        )
          throw new Error("INVOICE_EXPORT_FAILED");
        const temporary = path.join(path.dirname(target), `.tammy-invoice-${randomUUID()}.pdf`);
        try {
          const file = await open(temporary, "wx", 0o600);
          try {
            await file.writeFile(bytes);
            await file.sync();
          } finally {
            await file.close();
          }
          guard();
          await rename(temporary, target);
        } finally {
          await unlink(temporary).catch(() => undefined);
        }
        return { saved: true };
      } catch {
        throw new Error("INVOICE_EXPORT_FAILED");
      }
    },
  };
}
