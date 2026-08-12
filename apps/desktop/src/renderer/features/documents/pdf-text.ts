const PDF_HEADER = "%PDF-";
const MAX_EXTRACTED_BYTES = 1024 * 1024;

// extractNativePdfText reads literal and hexadecimal text operators from
// native-text PDFs without leaving the renderer. Image-only PDFs intentionally
// return an empty string and remain available for human review.
export function extractNativePdfText(bytes: Uint8Array): string {
  const source = new TextDecoder("latin1").decode(bytes);
  if (!source.startsWith(PDF_HEADER)) return "";

  const fragments: string[] = [];
  collectLiteralOperators(source, fragments);
  collectArrayOperators(source, fragments);
  collectHexOperators(source, fragments);
  return fragments
    .map((value) => value.replace(/\s+/g, " ").trim())
    .filter(Boolean)
    .join("\n")
    .slice(0, MAX_EXTRACTED_BYTES);
}

function collectLiteralOperators(source: string, output: string[]): void {
  for (const match of source.matchAll(/\(((?:\\.|[^\\)])*)\)\s*(?:Tj|'|")/g)) {
    output.push(decodePdfLiteral(match[1] ?? ""));
  }
}

function collectArrayOperators(source: string, output: string[]): void {
  for (const array of source.matchAll(/\[([\s\S]*?)\]\s*TJ/g)) {
    const fragments: string[] = [];
    for (const literal of (array[1] ?? "").matchAll(/\(((?:\\.|[^\\)])*)\)/g)) {
      fragments.push(decodePdfLiteral(literal[1] ?? ""));
    }
    for (const hex of (array[1] ?? "").matchAll(/<([0-9a-fA-F\s]+)>/g)) {
      fragments.push(decodePdfHex(hex[1] ?? ""));
    }
    if (fragments.length > 0) output.push(fragments.join(""));
  }
}

function collectHexOperators(source: string, output: string[]): void {
  for (const match of source.matchAll(/<([0-9a-fA-F\s]+)>\s*Tj/g)) {
    output.push(decodePdfHex(match[1] ?? ""));
  }
}

function decodePdfLiteral(value: string): string {
  return value.replace(/\\([0-7]{1,3}|[nrtbf()\\])/g, (_match, escaped: string) => {
    if (/^[0-7]+$/.test(escaped)) return String.fromCharCode(Number.parseInt(escaped, 8));
    return (
      ({ n: "\n", r: "\r", t: "\t", b: "\b", f: "\f" } as Record<string, string>)[escaped] ??
      escaped
    );
  });
}

function decodePdfHex(value: string): string {
  const normalized = value.replace(/\s+/g, "");
  const even = normalized.length % 2 === 0 ? normalized : `${normalized}0`;
  const bytes = new Uint8Array(even.length / 2);
  for (let index = 0; index < bytes.length; index += 1) {
    bytes[index] = Number.parseInt(even.slice(index * 2, index * 2 + 2), 16);
  }
  return new TextDecoder("utf-8", { fatal: false }).decode(bytes);
}
