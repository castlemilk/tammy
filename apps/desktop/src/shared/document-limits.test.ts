import { describe, expect, it } from "vitest";

import {
  documentIngestPayloadFits,
  MAX_DOCUMENT_INGEST_REQUEST_BYTES,
  MAX_DOCUMENT_ORIGINAL_BYTES,
  MAX_DOCUMENT_TEXT_BYTES,
  utf8ByteLength,
} from "./document-limits";

describe("document ingest limits", () => {
  it("keeps the worst-case request inside the 1 MiB core transport cap", () => {
    expect(MAX_DOCUMENT_ORIGINAL_BYTES).toBe(800 * 1024);
    expect(MAX_DOCUMENT_TEXT_BYTES).toBe(128 * 1024);
    expect(MAX_DOCUMENT_INGEST_REQUEST_BYTES).toBeLessThan(1024 * 1024);
    expect(MAX_DOCUMENT_INGEST_REQUEST_BYTES).toBeGreaterThan(
      MAX_DOCUMENT_ORIGINAL_BYTES + MAX_DOCUMENT_TEXT_BYTES,
    );
  });

  it("accepts exactly the original-byte limit and rejects one byte more", () => {
    const text = "GST $29.00";
    expect(documentIngestPayloadFits(new Uint8Array(MAX_DOCUMENT_ORIGINAL_BYTES), text)).toBe(true);
    expect(documentIngestPayloadFits(new Uint8Array(MAX_DOCUMENT_ORIGINAL_BYTES + 1), text)).toBe(
      false,
    );
  });

  it("measures extracted text in UTF-8 bytes, not UTF-16 units", () => {
    // "é" is 2 UTF-8 bytes and "€" is 3, so the unit count and byte count differ.
    expect(utf8ByteLength("é")).toBe(2);
    expect(utf8ByteLength("€")).toBe(3);
    const exactly = "a".repeat(MAX_DOCUMENT_TEXT_BYTES);
    const one = new Uint8Array(1);
    expect(documentIngestPayloadFits(one, exactly)).toBe(true);
    expect(documentIngestPayloadFits(one, `${exactly}a`)).toBe(false);
    // 65,536 euro signs are 196,608 bytes: over the text budget despite a small unit count.
    expect(documentIngestPayloadFits(one, "€".repeat(65_536))).toBe(false);
  });
});
