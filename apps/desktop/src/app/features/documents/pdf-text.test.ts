import { describe, expect, it } from "vitest";

import { MAX_DOCUMENT_TEXT_BYTES } from "../../../shared/document-limits";
import { extractNativePdfText } from "./pdf-text";

describe("extractNativePdfText", () => {
  it("extracts literal, array, escaped, and hexadecimal native PDF text locally", () => {
    const source = `%PDF-1.4
      BT (Officeworks\\040Ltd) Tj
      [(Invoice ) 10 (INV-029847)] TJ
      <475354202432392e3030> Tj ET
      %%EOF`;
    expect(extractNativePdfText(new TextEncoder().encode(source))).toBe(
      "Officeworks Ltd\nInvoice INV-029847\nGST $29.00",
    );
  });

  it("returns no candidate text for non-PDF and image-only PDF input", () => {
    expect(extractNativePdfText(new TextEncoder().encode("not a pdf"))).toBe("");
    expect(extractNativePdfText(new TextEncoder().encode("%PDF-1.7\n/image data\n%%EOF"))).toBe("");
  });

  it("truncates extracted text to the UTF-8 byte budget without splitting a code point", () => {
    // Hex text operators carry UTF-8 bytes. Each euro sign (E2 82 AC) is 3 bytes, so 50,000 of
    // them is 150,000 bytes. 131,072 is not a multiple of 3, so the cut must back off to 131,070.
    const euroHex = "e282ac".repeat(50_000);
    const source = `%PDF-1.4\n<${euroHex}> Tj\n%%EOF`;
    const text = extractNativePdfText(new TextEncoder().encode(source));

    expect(new TextEncoder().encode(text).byteLength).toBe(131_070);
    expect(text).toBe("€".repeat(43_690));
    expect(text).not.toContain("\uFFFD");
  });

  it("keeps text exactly at the byte budget unchanged", () => {
    const text = "a".repeat(MAX_DOCUMENT_TEXT_BYTES);
    const source = `%PDF-1.4\n(${text}) Tj\n%%EOF`;
    expect(extractNativePdfText(new TextEncoder().encode(source))).toBe(text);
  });
});
