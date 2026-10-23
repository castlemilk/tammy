import { expect, it } from "vitest";

import { getNewCredentialFeedback, newCredentialValuesMatch } from "./new-credential-policy";

it("checks the backend's 15–128 NFC code-point bounds", () => {
  expect(getNewCredentialFeedback("a".repeat(14)).withinCodePointRange).toBe(false);
  expect(getNewCredentialFeedback("a".repeat(15)).valid).toBe(true);
  expect(getNewCredentialFeedback("a".repeat(128)).valid).toBe(true);
  expect(getNewCredentialFeedback("a".repeat(129)).withinCodePointRange).toBe(false);
});

it("counts normalized Unicode code points, preserves spaces, and reports UTF-8 bytes", () => {
  const composed = getNewCredentialFeedback("é".repeat(15));
  const decomposed = getNewCredentialFeedback("e\u0301".repeat(15));
  expect(composed.codePointCount).toBe(15);
  expect(decomposed.codePointCount).toBe(15);
  expect(decomposed.utf8ByteCount).toBe(45);
  expect(decomposed.normalizedUtf8ByteCount).toBe(30);
  expect(getNewCredentialFeedback(` ${"a".repeat(13)} `).valid).toBe(true);
});

it("enforces the UTF-8 byte cap and printable Unicode rule", () => {
  expect(getNewCredentialFeedback("界".repeat(342)).withinUtf8Limit).toBe(false);
  expect(getNewCredentialFeedback(`${"a".repeat(15)}\n`).printableUnicode).toBe(false);
  expect(getNewCredentialFeedback(`${"a".repeat(15)}\u200b`).printableUnicode).toBe(false);
  expect(getNewCredentialFeedback(`${"a".repeat(14)} `).valid).toBe(true);
  expect(getNewCredentialFeedback(`${"a".repeat(14)}\u00a0`).printableUnicode).toBe(false);
  expect(getNewCredentialFeedback(`${"a".repeat(14)}\u2003`).printableUnicode).toBe(false);
});

it("rejects malformed surrogate input rather than encoding replacement characters", () => {
  expect(getNewCredentialFeedback(`${"a".repeat(15)}\ud800`).wellFormedUnicode).toBe(false);
  expect(getNewCredentialFeedback(`${"a".repeat(15)}\udc00`).valid).toBe(false);
});

it("matches confirmation after NFC normalization without trimming", () => {
  expect(newCredentialValuesMatch("é".repeat(15), "e\u0301".repeat(15))).toBe(true);
  expect(newCredentialValuesMatch(` ${"a".repeat(13)} `, "a".repeat(15))).toBe(false);
  expect(newCredentialValuesMatch("a".repeat(15), "a".repeat(14))).toBe(false);
  expect(newCredentialValuesMatch("", "")).toBe(false);
});
