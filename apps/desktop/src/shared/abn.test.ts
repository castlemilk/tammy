import { expect, it } from "vitest";

import { abnLookupUrl, isValidAbn, normalizeAbnInput } from "./abn";

it("normalizes pasted whitespace and dash separators without truncating", () => {
  expect(normalizeAbnInput(" 51 824 753 556 ")).toBe("51824753556");
  expect(normalizeAbnInput("51‑824—753−556")).toBe("51824753556");
  expect(normalizeAbnInput("518247535560000")).toBe("518247535560000");
});

it("matches Core's exact 11 ASCII digit weighted mod-89 ABN check", () => {
  expect(isValidAbn("51824753556")).toBe(true);
  expect(isValidAbn("53004085616")).toBe(true);
  expect(isValidAbn("83914571673")).toBe(true);
  expect(isValidAbn("51 824 753 556")).toBe(true);
  expect(isValidAbn("52682863690")).toBe(true);
  expect(isValidAbn("52682863691")).toBe(false);
  expect(isValidAbn("51824753557")).toBe(false);
  expect(isValidAbn("٥١٨٢٤٧٥٣٥٥٦")).toBe(false);
});

it("does not discard letters or hide an invalid ABN in the lookup handoff", () => {
  const withLetter = "51A 824 753 556";
  expect(normalizeAbnInput(withLetter)).toBe("51A824753556");
  expect(isValidAbn(withLetter)).toBe(false);
  expect(abnLookupUrl(withLetter)).toBeUndefined();
  expect(abnLookupUrl("52682863691")).toBeUndefined();
});

it("hands only a valid canonical ABN to the official ABR page", () => {
  expect(abnLookupUrl("51 824 753 556")).toBe(
    "https://abr.business.gov.au/ABN/View?abn=51824753556",
  );
});
