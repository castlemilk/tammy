/** Profit, margin and markup in integer AUD cents with cent-accurate inputs.
 * Margin = profit / selling price; markup = profit / cost.
 */
export type MarginResult = { profitCents: number; marginPercent: number | null; markupPercent: number | null };

function percentToTwoDecimals(numerator: number, denominator: number): number | null {
  if (denominator === 0) return null;
  const absNumerator = BigInt(Math.abs(numerator));
  const absDenominator = BigInt(Math.abs(denominator));
  // Percentage represented to two decimal places (basis points), half-up.
  const scaled = (absNumerator * 10000n * 2n + absDenominator) / (absDenominator * 2n);
  const sign = numerator < 0 !== denominator < 0 ? -1 : 1;
  return sign * Number(scaled) / 100;
}

export function calculateMargin(costCents: number, saleCents: number): MarginResult {
  if (!Number.isSafeInteger(costCents) || !Number.isSafeInteger(saleCents) ||
      costCents < 0 || saleCents < 0 || costCents > 100_000_000_000 || saleCents > 100_000_000_000) {
    throw new Error("Cost and selling price must be valid non-negative AUD amounts.");
  }
  const profitCents = saleCents - costCents;
  return {
    profitCents,
    marginPercent: percentToTwoDecimals(profitCents, saleCents),
    markupPercent: percentToTwoDecimals(profitCents, costCents),
  };
}

export function formatPercent(percent: number | null): string {
  return percent === null ? "Not defined" :
    `${new Intl.NumberFormat("en-AU", { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(percent)}%`;
}
