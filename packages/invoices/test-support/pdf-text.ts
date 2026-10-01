import { inflateSync } from "node:zlib";

export function pdfStreams(bytes: Uint8Array): string {
  const buffer = Buffer.from(bytes);
  const source = buffer.toString("latin1");
  const parts = [source];
  const objects = new Map<string, string>();
  const streams = new Map<string, string>();
  for (const match of source.matchAll(/(?:^|\n)(\d+) 0 obj\n([\s\S]*?)\nendobj/g)) {
    const id = match[1];
    const body = match[2];
    if (!id || !body) continue;
    objects.set(id, body);
    const marker = /stream\r?\n/.exec(body);
    if (!marker) continue;
    const start = marker.index + marker[0].length;
    const end = body.lastIndexOf("endstream");
    if (end < start) continue;
    let decoded = body.slice(start, end);
    if (body.slice(0, marker.index).includes("/FlateDecode")) {
      try {
        decoded = inflateSync(Buffer.from(decoded, "latin1")).toString("latin1");
      } catch {
        continue;
      }
    }
    streams.set(id, decoded);
    parts.push(decoded);
  }

  // Read the actual Forme PDF's font resources and ToUnicode CMaps. This is a
  // deliberately fixture-specific text extractor, not a general PDF parser.
  const fonts = new Map<string, Map<string, string>>();
  for (const match of source.matchAll(/\/(F\d+)\s+(\d+)\s+0\s+R/g)) {
    const name = match[1];
    const id = match[2];
    if (!name || !id) continue;
    const unicodeId = objects.get(id)?.match(/\/ToUnicode\s+(\d+)\s+0\s+R/)?.[1];
    if (!unicodeId) continue;
    const map = new Map<string, string>();
    for (const block of (streams.get(unicodeId) ?? "").matchAll(
      /beginbfchar([\s\S]*?)endbfchar/g,
    )) {
      for (const entry of (block[1] ?? "").matchAll(/<([\da-fA-F]+)>\s+<([\da-fA-F]+)>/g)) {
        if (entry[1] && entry[2])
          map.set(
            entry[1].toUpperCase(),
            Buffer.from(entry[2], "hex").swap16().toString("utf16le"),
          );
      }
    }
    fonts.set(name, map);
  }
  for (const stream of streams.values()) {
    let activeFont = "F0";
    for (const item of stream.matchAll(/\/(F\d+)\s+[\d.]+\s+Tf|<([\da-fA-F]+)>\s+Tj/g)) {
      if (item[1]) activeFont = item[1];
      else if (item[2]) {
        const map = fonts.get(activeFont);
        parts.push(
          map
            ? (item[2].match(/.{4}/g) ?? [])
                .map((code) => map.get(code.toUpperCase()) ?? "")
                .join("")
            : Buffer.from(item[2], "hex").toString("latin1"),
        );
      }
    }
  }
  return parts.join("\n");
}
