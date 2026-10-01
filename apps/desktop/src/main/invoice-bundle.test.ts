// @vitest-environment node

import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";
import { build } from "vite";
import { expect, test } from "vitest";
import { draftFixture } from "../../../../packages/invoices/src/test-fixtures.ts";
import { pdfStreams } from "../../../../packages/invoices/test-support/pdf-text.ts";
import config from "../../electron.vite.config";

const execFileAsync = promisify(execFile);

test("the bundled invoice adapter renders draft and issued pages with the packaged Forme modules", async () => {
  const desktopRoot = process.cwd();
  const cache = path.join(desktopRoot, "node_modules/.cache");
  await mkdir(cache, { recursive: true });
  const directory = await mkdtemp(path.join(cache, "invoice-bundle-"));
  try {
    const result = await build({
      ...config.main,
      configFile: false,
      logLevel: "silent",
      build: {
        ...config.main?.build,
        lib: {
          entry: path.resolve(desktopRoot, "../../packages/invoices/src/pdf.ts"),
          formats: ["es"],
        },
        write: false,
      },
    });
    const bundle = Array.isArray(result) ? result[0] : result;
    if (!bundle || !("output" in bundle)) throw new Error("INVOICE_BUNDLE_MISSING");
    const chunk = bundle.output.find((entry) => entry.type === "chunk" && entry.isEntry);
    if (chunk?.type !== "chunk") throw new Error("INVOICE_BUNDLE_MISSING");
    const modulePath = path.join(directory, "invoice.mjs");
    await writeFile(modulePath, chunk.code);
    const invoice = {
      ...draftFixture(),
      sellerName: "Gamma Systems Pty Ltd",
      sellerAbn: "51824753556",
    };
    await execFileAsync(
      process.execPath,
      [
        "--input-type=module",
        "-e",
        `import { writeFile } from "node:fs/promises";
         import { join } from "node:path";
         import { pathToFileURL } from "node:url";
         const { renderInvoicePdf } = await import(pathToFileURL(process.argv[1]).href);
         const invoice = JSON.parse(process.argv[3]);
         for (const state of ["draft", "issued"]) {
           await writeFile(join(process.argv[2], state + ".pdf"), await renderInvoicePdf({ ...invoice, state }));
         }`,
        modulePath,
        directory,
        JSON.stringify(invoice),
      ],
      { timeout: 30_000 },
    );
    for (const state of ["draft", "issued"]) {
      const content = pdfStreams(await readFile(path.join(directory, `${state}.pdf`)));
      expect(content).toMatch(/\/Type\s*\/Pages\b[\s\S]*?\/Count\s+1\b/);
      expect(content).toContain(invoice.customerName);
      expect(content).toContain("165.02");
      expect(content.includes("DRAFT")).toBe(state === "draft");
    }
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}, 30_000);
