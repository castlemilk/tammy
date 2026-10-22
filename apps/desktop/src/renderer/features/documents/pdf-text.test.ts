import { describe, expect, it } from "vitest";

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
});
