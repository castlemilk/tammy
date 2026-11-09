const MAX_TEXT_UNITS = 8192;

interface ParsedText {
  value: string;
  end: number;
  closed: boolean;
}

// This deliberately handles only uncompressed native PDF text operators.
// Each parser advances monotonically so malformed input cannot cause repeated
// scans of the same bytes on the browser's main thread.
export function extractNativePdfText(bytes: Uint8Array): string {
  const source = new TextDecoder("latin1").decode(bytes);
  if (!source.startsWith("%PDF-")) return "";
  const fragments: string[] = [];
  let index = 0;
  let collected = 0;
  while (index < source.length && collected < MAX_TEXT_UNITS) {
    const current = source[index];
    if (current === "(") {
      const parsed = literal(source, index);
      index = parsed.end;
      const operator = skipWhitespace(source, index);
      if (
        parsed.closed &&
        (source.startsWith("Tj", operator) || source[operator] === "'" || source[operator] === '"')
      ) {
        fragments.push(parsed.value);
        collected += parsed.value.length;
      }
    } else if (current === "[") {
      const parsed = array(source, index);
      index = parsed.end;
      const operator = skipWhitespace(source, index);
      if (parsed.closed && source.startsWith("TJ", operator)) {
        fragments.push(parsed.value);
        collected += parsed.value.length;
      }
    } else if (current === "<" && source[index + 1] !== "<") {
      const parsed = hex(source, index);
      index = parsed.end;
      const operator = skipWhitespace(source, index);
      if (parsed.closed && source.startsWith("Tj", operator)) {
        fragments.push(parsed.value);
        collected += parsed.value.length;
      }
    } else index += 1;
  }
  return fragments
    .map((value) => value.replace(/\s+/g, " ").trim())
    .filter(Boolean)
    .join("\n")
    .slice(0, MAX_TEXT_UNITS);
}

function skipWhitespace(source: string, start: number): number {
  let index = start;
  while (index < source.length && /\s/.test(source[index] ?? "")) index += 1;
  return index;
}

function literal(source: string, start: number): ParsedText {
  let index = start + 1;
  let depth = 1;
  let value = "";
  while (index < source.length) {
    const char = source[index++] ?? "";
    if (char === "\\") {
      const escaped = source[index++] ?? "";
      if (/[0-7]/.test(escaped)) {
        let octal = escaped;
        while (octal.length < 3 && index < source.length && /[0-7]/.test(source[index] ?? ""))
          octal += source[index++];
        value += String.fromCharCode(Number.parseInt(octal, 8));
      } else
        value +=
          ({ n: "\n", r: "\r", t: "\t", b: "\b", f: "\f" } as Record<string, string>)[escaped] ??
          escaped;
    } else if (char === "(") {
      depth += 1;
      value += char;
    } else if (char === ")") {
      depth -= 1;
      if (depth === 0) return { value, end: index, closed: true };
      value += char;
    } else value += char;
  }
  return { value: "", end: index, closed: false };
}

function hex(source: string, start: number): ParsedText {
  let index = start + 1;
  let digits = "";
  let valid = true;
  while (index < source.length && source[index] !== ">") {
    const char = source[index++] ?? "";
    if (/[0-9a-f]/i.test(char)) digits += char;
    else if (!/\s/.test(char)) valid = false;
  }
  if (index >= source.length) return { value: "", end: index, closed: false };
  index += 1;
  if (!valid) return { value: "", end: index, closed: true };
  if (digits.length % 2) digits += "0";
  const bytes = new Uint8Array(digits.length / 2);
  for (let i = 0; i < bytes.length; i += 1)
    bytes[i] = Number.parseInt(digits.slice(i * 2, i * 2 + 2), 16);
  return { value: new TextDecoder("utf-8").decode(bytes), end: index, closed: true };
}

function array(source: string, start: number): ParsedText {
  let index = start + 1;
  const values: string[] = [];
  while (index < source.length) {
    if (source[index] === "]") return { value: values.join(""), end: index + 1, closed: true };
    if (source[index] === "(") {
      const parsed = literal(source, index);
      index = parsed.end;
      if (!parsed.closed) return { value: "", end: index, closed: false };
      values.push(parsed.value);
    } else if (source[index] === "<" && source[index + 1] !== "<") {
      const parsed = hex(source, index);
      index = parsed.end;
      if (!parsed.closed) return { value: "", end: index, closed: false };
      values.push(parsed.value);
    } else index += 1;
  }
  return { value: "", end: index, closed: false };
}
