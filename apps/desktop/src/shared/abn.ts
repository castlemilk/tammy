const separators = /[\p{White_Space}\p{Dash_Punctuation}\u2212]/gu;
const canonicalAbnPattern = /^[0-9]{11}$/;
const weights = [10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19] as const;

/** Remove presentation separators while preserving all other input for validation. */
export function normalizeAbnInput(value: string): string {
  return value.replace(separators, "");
}

/** Match Core's canonical 11-digit ABN weighted mod-89 check. */
export function isValidAbn(value: string): boolean {
  const canonical = normalizeAbnInput(value);
  if (!canonicalAbnPattern.test(canonical)) return false;

  let total = 0;
  for (let index = 0; index < weights.length; index += 1) {
    const digit = Number(canonical[index]) - (index === 0 ? 1 : 0);
    total += digit * (weights[index] ?? 0);
  }
  return total % 89 === 0;
}

/** Build the official ABR handoff URL only for a checksum-valid ABN. */
export function abnLookupUrl(value: string): string | undefined {
  const canonical = normalizeAbnInput(value);
  if (!isValidAbn(canonical)) return undefined;
  return `https://abr.business.gov.au/ABN/View?abn=${canonical}`;
}
