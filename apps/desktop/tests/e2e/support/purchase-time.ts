export function timestampUtcCivilDate(
  value: { readonly seconds: bigint; readonly nanos: number } | undefined,
): string {
  if (!value) throw new Error("PURCHASE_ACCEPTANCE_REVIEWED_AT_MISSING");
  const seconds = Number(value.seconds);
  if (
    !Number.isSafeInteger(seconds) ||
    !Number.isInteger(value.nanos) ||
    value.nanos < 0 ||
    value.nanos > 999_999_999
  )
    throw new Error("PURCHASE_ACCEPTANCE_REVIEWED_AT_INVALID");
  const instant = new Date(seconds * 1000 + Math.floor(value.nanos / 1_000_000));
  if (Number.isNaN(instant.getTime())) throw new Error("PURCHASE_ACCEPTANCE_REVIEWED_AT_INVALID");
  return instant.toISOString().slice(0, 10);
}
