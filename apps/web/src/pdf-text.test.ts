import { expect, it } from "vitest";
import { extractNativePdfText } from "./pdf-text";

it("extracts supported PDF text operators locally", () => {
  const source = `%PDF-1.4\nBT (Officeworks\\040Ltd) Tj\n[(Invoice ) 10 (INV-029847)] TJ\n<475354202432392e3030> Tj ET`;
  expect(extractNativePdfText(new TextEncoder().encode(source))).toBe(
    "Officeworks Ltd\nInvoice INV-029847\nGST $29.00",
  );
});

it("does bounded work on malformed input with unmatched opening delimiters", () => {
  const source = new TextEncoder().encode(`%PDF-1.4\n${"(".repeat(512 * 1024 - 16)}`);
  const start = performance.now();
  expect(extractNativePdfText(source)).toBe("");
  expect(performance.now() - start).toBeLessThan(500);
});
