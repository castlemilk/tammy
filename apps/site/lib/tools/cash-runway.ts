/** One-rate cash runway estimate based on recurring monthly inflows/outflows.
 * Does not forecast timing, seasonality, liabilities, or taxes separately.
 */
export type CashRunway =
  | { status: "decreasing"; monthlyNetCents: number; runwayTenths: number }
  | { status: "nondecreasing"; monthlyNetCents: number; runwayTenths: null };

export function estimateCashRunway(
  openingCashCents: number, monthlyInCents: number, monthlyOutCents: number,
): CashRunway {
  for (const amount of [openingCashCents, monthlyInCents, monthlyOutCents]) {
    if (!Number.isSafeInteger(amount) || amount < 0 || amount > 100_000_000_000) {
      throw new Error("Enter non-negative AUD amounts in whole cents.");
    }
  }
  const monthlyNetCents = monthlyInCents - monthlyOutCents;
  if (monthlyNetCents >= 0) {
    return { status: "nondecreasing", monthlyNetCents, runwayTenths: null };
  }
  const burn = BigInt(-monthlyNetCents);
  // Round half-up to one decimal month without converting cash to floating point.
  const runwayTenths = Number((BigInt(openingCashCents) * 10n + burn / 2n) / burn);
  return { status: "decreasing", monthlyNetCents, runwayTenths };
}

export function formatRunway(tenths: number): string {
  if (!Number.isSafeInteger(tenths) || tenths < 0) throw new Error("Invalid runway");
  return (tenths / 10).toFixed(1) + " months";
}
