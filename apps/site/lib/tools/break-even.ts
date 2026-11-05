/** Simple single-product break-even model, in AUD cents.
 * Deliberately excludes GST, income tax, financing and capacity constraints.
 */
export type BreakEvenResult =
  | { status: "possible"; contributionCents: number; units: bigint; revenueCents: bigint }
  | { status: "unreachable"; contributionCents: number; units: null; revenueCents: null };

const MAX = 100_000_000_000;
function valid(value: number): boolean {
  return Number.isSafeInteger(value) && value >= 0 && value <= MAX;
}

export function calculateBreakEven(
  fixedCostsCents: number, unitPriceCents: number, variableCostCents: number,
): BreakEvenResult {
  if (![fixedCostsCents, unitPriceCents, variableCostCents].every(valid)) {
    throw new Error("Enter valid non-negative amounts in AUD cents.");
  }
  const contributionCents = unitPriceCents - variableCostCents;
  if (contributionCents <= 0 && fixedCostsCents > 0) {
    return { status: "unreachable", contributionCents, units: null, revenueCents: null };
  }
  if (fixedCostsCents === 0) {
    return { status: "possible", contributionCents, units: 0n, revenueCents: 0n };
  }
  const units = (BigInt(fixedCostsCents) + BigInt(contributionCents) - 1n) /
    BigInt(contributionCents);
  return {
    status: "possible", contributionCents,
    units, revenueCents: units * BigInt(unitPriceCents),
  };
}

export function formatAudBigInt(cents: bigint): string {
  const sign = cents < 0n ? "-" : "";
  const magnitude = cents < 0n ? -cents : cents;
  const dollars = (magnitude / 100n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return sign + "$" + dollars + "." + (magnitude % 100n).toString().padStart(2, "0");
}
