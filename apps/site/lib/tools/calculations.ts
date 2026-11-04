/** AUD maths is done in integer cents. No floating-point money accumulators. */
export type GstMode = "add" | "remove";

export function parseAudCents(value: string): number | null {
  const cleaned = value.trim().replace(/,/g, "");
  if (!/^\d{1,9}(?:\.\d{1,2})?$/.test(cleaned)) return null;
  const [whole, decimal = ""] = cleaned.split(".");
  const cents = Number(whole) * 100 + Number(decimal.padEnd(2, "0"));
  return Number.isSafeInteger(cents) && cents <= 100_000_000_000 ? cents : null;
}

export function parseQuantityMilli(value: string): number | null {
  const trimmed = value.trim();
  if (!/^\d{1,4}(?:\.\d{1,3})?$/.test(trimmed)) return null;
  const [whole, fraction = ""] = trimmed.split(".");
  const milli = Number(whole) * 1000 + Number(fraction.padEnd(3, "0"));
  return Number.isSafeInteger(milli) && milli >= 1 && milli <= 1_000_000
    ? milli : null;
}

export function aud(cents: number): string {
  if (!Number.isSafeInteger(cents)) throw new Error("Invalid cent amount");
  const sign = cents < 0 ? "-" : "";
  const absolute = Math.abs(cents);
  return `${sign}$${Math.floor(absolute / 100).toLocaleString("en-AU")}.${String(absolute % 100).padStart(2, "0")}`;
}

/** Australian standard taxable supply: 10% of ex-GST, 1/11th of GST-inclusive.
 * All amounts are rounded to the nearest cent (half up).
 */
export function gstBreakdown(amountCents: number, mode: GstMode) {
  if (!Number.isSafeInteger(amountCents) || amountCents < 0)
    throw new Error("Enter a non-negative amount in cents");
  if (mode === "add") {
    const gstCents = Math.floor((amountCents + 5) / 10);
    return { exclusiveCents: amountCents, gstCents, inclusiveCents: amountCents + gstCents };
  }
  const gstCents = Math.floor((amountCents + 5) / 11);
  return { exclusiveCents: amountCents - gstCents, gstCents, inclusiveCents: amountCents };
}

export function addCalendarDays(dateIso: string, days: number): string | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(dateIso) || !Number.isSafeInteger(days) || days < 0 || days > 3650)
    return null;
  const date = new Date(`${dateIso}T00:00:00.000Z`);
  if (!Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== dateIso) return null;
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}

export function australianDate(dateIso: string): string {
  return new Intl.DateTimeFormat("en-AU", {
    weekday: "long", day: "numeric", month: "long", year: "numeric", timeZone: "UTC"
  }).format(new Date(`${dateIso}T00:00:00.000Z`));
}
