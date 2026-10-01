// @vitest-environment node
import { mkdtemp, readFile, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createInvoiceExport } from "./invoice-export";

const directories: string[] = [];
afterEach(async () => {
  await Promise.all(
    directories.splice(0).map((directory) => rm(directory, { recursive: true, force: true })),
  );
});
async function fixture() {
  const directory = await mkdtemp(path.join(tmpdir(), "tammy-invoice-export-"));
  directories.push(directory);
  const filePath = path.join(directory, "invoice.pdf");
  const guard = vi.fn();
  const render = vi.fn(async () => new TextEncoder().encode("%PDF-1.7\nfixture"));
  const select = vi.fn(async () => ({ canceled: false, filePath }));
  const invoice = { number: "INV-001" };
  const exporter = createInvoiceExport({
    prepare: async () => ({ invoice, guard }),
    render,
    select,
  });
  return { directory, filePath, exporter, select, render, guard };
}
describe("native invoice PDF export", () => {
  it("writes only the persisted invoice to the native-selected private file", async () => {
    const { exporter, filePath, guard } = await fixture();
    expect(await exporter.export({})).toEqual({ saved: true });
    expect(await readFile(filePath, "utf8")).toContain("%PDF");
    expect(guard).toHaveBeenCalled();
  });
  it("cancellation writes nothing and returns no local path", async () => {
    const { exporter, select, filePath } = await fixture();
    select.mockResolvedValue({ canceled: true, filePath });
    expect(await exporter.export({})).toEqual({ saved: false });
    await expect(readFile(filePath)).rejects.toMatchObject({ code: "ENOENT" });
  });
  it("does not export after a session change during native selection", async () => {
    const { exporter, guard, filePath } = await fixture();
    guard.mockImplementation(() => {
      throw new Error("LOCAL_SESSION_REQUIRED");
    });
    await expect(exporter.export({})).rejects.toThrow();
    await expect(readFile(filePath)).rejects.toMatchObject({ code: "ENOENT" });
  });
  it("refuses a symlink destination", async () => {
    const { exporter, directory, filePath } = await fixture();
    await symlink(path.join(directory, "elsewhere"), filePath);
    await expect(exporter.export({})).rejects.toThrow("INVOICE_EXPORT_FAILED");
  });
});
