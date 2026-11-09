import { expect, it } from "vitest";
import { parseStatementRows } from "./statement";

it("reads signed statement rows without rounding or losing commas in descriptions", () => {
  expect(
    parseStatementRows("2024-05-12,Officeworks, Melbourne,-319.00\n2024-05-13,Deposit,150.25"),
  ).toEqual([
    { date: "2024-05-12", description: "Officeworks, Melbourne", amountMinor: -31900 },
    { date: "2024-05-13", description: "Deposit", amountMinor: 15025 },
  ]);
  expect(() => parseStatementRows("2024-05-12,Receipt,1.234")).toThrow();
});
