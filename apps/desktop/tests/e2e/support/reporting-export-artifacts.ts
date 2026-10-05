import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { parse } from "csv-parse/sync";
import { getDocument } from "pdfjs-dist/legacy/build/pdf.mjs";

const maximumArtifactBytes = 16 * 1024 * 1024;

export interface ParsedReportCsv {
  readonly header: readonly string[];
  readonly rows: readonly Readonly<Record<string, string>>[];
}

/** Acceptance uses an independent CSV parser, with no platform-specific command dependency. */
export async function parseReportCsv(file: string): Promise<ParsedReportCsv> {
  const bytes = await readFile(file);
  if (bytes.length === 0 || bytes.length > maximumArtifactBytes) {
    throw new Error("REPORTING_ACCEPTANCE_CSV_SIZE_INVALID");
  }
  const records = parse(bytes, { bom: true, relax_column_count: false }) as string[][];
  const header = records[0];
  if (!header?.length || header.some((value) => !value) || new Set(header).size !== header.length) {
    throw new Error("REPORTING_ACCEPTANCE_CSV_HEADER_INVALID");
  }
  return {
    header,
    rows: records
      .slice(1)
      .map((row) => Object.fromEntries(header.map((key, index) => [key, row[index] ?? ""]))),
  };
}

/** Mozilla PDF.js reads real saved pages independently of Forme's font/stream layout. */
export async function parseReportPdf(file: string): Promise<string> {
  const bytes = await readFile(file);
  if (
    bytes.length < 8 ||
    bytes.length > maximumArtifactBytes ||
    bytes.subarray(0, 5).toString("ascii") !== "%PDF-"
  ) {
    throw new Error("REPORTING_ACCEPTANCE_PDF_INVALID");
  }
  const loading = getDocument({
    data: Uint8Array.from(bytes),
    useSystemFonts: false,
    useWorkerFetch: false,
  });
  try {
    const document = await loading.promise;
    const pages: string[] = [];
    for (let number = 1; number <= document.numPages; number += 1) {
      const page = await document.getPage(number);
      const content = await page.getTextContent();
      pages.push(content.items.map((item) => ("str" in item ? item.str : "")).join(" "));
      page.cleanup();
    }
    const text = pages.join("\f");
    if (!text.trim()) throw new Error("REPORTING_ACCEPTANCE_PDF_TEXT_MISSING");
    return text;
  } finally {
    await loading.destroy();
  }
}

export function exactDecimal(minor: bigint | undefined): string {
  if (minor === undefined) throw new Error("REPORTING_ACCEPTANCE_AMOUNT_MISSING");
  const magnitude = minor < 0n ? -minor : minor;
  return `${minor < 0n ? "-" : ""}${magnitude / 100n}.${String(magnitude % 100n).padStart(2, "0")}`;
}

export async function reportArtifactHash(file: string): Promise<string> {
  return createHash("sha256")
    .update(await readFile(file))
    .digest("hex");
}
