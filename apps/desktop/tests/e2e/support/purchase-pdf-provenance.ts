const CANONICAL_UUID = /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/u;
const SHA256_HEX = /^[0-9a-f]{64}$/u;
const IDENTIFIER_BOUNDARY = "[\\p{L}\\p{N}_-]";
const REGEXP_SPECIAL_CHARACTERS = new Set(".*+?^$(){}|[]\\");

function isOpaqueIdentifier(expected: string): boolean {
  return CANONICAL_UUID.test(expected) || SHA256_HEX.test(expected);
}

function escapeRegExp(value: string): string {
  return Array.from(value)
    .map((character) => (REGEXP_SPECIAL_CHARACTERS.has(character) ? `\\${character}` : character))
    .join("");
}

function opaqueCharacters(expected: string): string {
  return expected.split("").join("\\s*");
}

function exactTokenPattern(token: string): RegExp {
  return new RegExp(`(?<!${IDENTIFIER_BOUNDARY})${token}(?!${IDENTIFIER_BOUNDARY})`, "u");
}

export function pdfRetainsMetadata(normalized: string, expected: string): boolean {
  if (isOpaqueIdentifier(expected)) {
    return exactTokenPattern(opaqueCharacters(expected)).test(normalized);
  }
  return normalized.includes(expected);
}

export function pdfRetainsReviewDocument(normalized: string, expected: string): boolean {
  const labelPattern = "Review\\s+document";
  const expectedPattern = isOpaqueIdentifier(expected)
    ? opaqueCharacters(expected)
    : escapeRegExp(expected);
  return exactTokenPattern(`${labelPattern}\\s+${expectedPattern}`).test(normalized);
}
