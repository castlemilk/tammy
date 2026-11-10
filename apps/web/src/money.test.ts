import { describe, expect, it } from "vitest";
import { formatAmount, parseAmount, sumAmounts } from "./money";

describe("AUD amount entry", () => {
  it("converts signed decimal amounts to exact integer cents", () => {
    expect(parseAmount("290.09")).toBe(29009);
    expect(parseAmount("-0.10")).toBe(-10);
    expect(parseAmount("12")).toBe(1200);
  });

  it("rejects fractional cents, invalid amounts, and unsafe integers", () => {
    expect(parseAmount("1.234")).toBeUndefined();
    expect(parseAmount("not money")).toBeUndefined();
    expect(parseAmount("90071992547410.00")).toBeUndefined();
  });

  it("displays and adds large integer-cent balances without losing precision", () => {
    expect(formatAmount("9007199254740993")).toBe("$90,071,992,547,409.93");
    expect(formatAmount("-12345")).toBe("-$123.45");
    expect(formatAmount(sumAmounts(["9007199254740993", 7]))).toBe("$90,071,992,547,410.00");
  });
});
