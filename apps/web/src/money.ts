export function parseAmount(value: string): number | undefined {
  const match = /^(-?)(\d+)(?:\.(\d{1,2}))?$/.exec(value.trim());
  if (!match) return undefined;
  const magnitude = BigInt(match[2] ?? "0") * 100n + BigInt((match[3] ?? "").padEnd(2, "0"));
  if (magnitude > BigInt(Number.MAX_SAFE_INTEGER)) return undefined;
  const amount = Number(magnitude);
  return match[1] === "-" && amount !== 0 ? -amount : amount;
}

export type MinorUnits = number | string;

function integerAmount(value: MinorUnits | null | undefined): bigint | undefined {
  if (typeof value === "number") return Number.isSafeInteger(value) ? BigInt(value) : undefined;
  if (typeof value === "string" && /^-?\d+$/.test(value)) return BigInt(value);
  return undefined;
}

export function sumAmounts(values: MinorUnits[]): string | undefined {
  let total = 0n;
  for (const value of values) {
    const amount = integerAmount(value);
    if (amount === undefined) return undefined;
    total += amount;
  }
  return total.toString();
}

export function formatAmount(value: MinorUnits | null | undefined): string {
  const amount = integerAmount(value);
  if (amount === undefined) return "—";
  const magnitude = amount < 0n ? -amount : amount;
  const dollars = (magnitude / 100n).toLocaleString("en-AU");
  const cents = (magnitude % 100n).toString().padStart(2, "0");
  return `${amount < 0n ? "-" : ""}$${dollars}.${cents}`;
}

export function amountInput(value: MinorUnits | null | undefined): string {
  const amount = integerAmount(value);
  if (amount === undefined) return "";
  const magnitude = amount < 0n ? -amount : amount;
  return `${amount < 0n ? "-" : ""}${magnitude / 100n}.${(magnitude % 100n).toString().padStart(2, "0")}`;
}
