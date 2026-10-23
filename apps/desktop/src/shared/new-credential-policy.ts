const minimumCodePoints = 15;
const maximumCodePoints = 128;
const maximumUtf8Bytes = 1_024;
const printableCodePoint = /^[\p{L}\p{M}\p{N}\p{P}\p{S} ]$/u;

export interface NewCredentialFeedback {
  readonly wellFormedUnicode: boolean;
  readonly codePointCount: number;
  readonly utf8ByteCount: number;
  readonly normalizedUtf8ByteCount: number;
  readonly printableUnicode: boolean;
  readonly withinCodePointRange: boolean;
  readonly withinUtf8Limit: boolean;
  readonly valid: boolean;
}

function hasWellFormedUnicode(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const unit = value.charCodeAt(index);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
      index += 1;
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      return false;
    }
  }
  return true;
}

function utf8ByteLength(value: string): number {
  const encoded = new TextEncoder().encode(value);
  const length = encoded.length;
  encoded.fill(0);
  return length;
}

/** Mirror the backend's format checks without trimming or retaining encoded bytes. */
export function getNewCredentialFeedback(value: string): NewCredentialFeedback {
  const wellFormedUnicode = hasWellFormedUnicode(value);
  if (!wellFormedUnicode) {
    return {
      wellFormedUnicode: false,
      codePointCount: 0,
      utf8ByteCount: 0,
      normalizedUtf8ByteCount: 0,
      printableUnicode: false,
      withinCodePointRange: false,
      withinUtf8Limit: false,
      valid: false,
    };
  }

  const normalized = value.normalize("NFC");
  const codePointCount = Array.from(normalized).length;
  const utf8ByteCount = utf8ByteLength(value);
  const normalizedUtf8ByteCount = utf8ByteLength(normalized);
  const printableUnicode = Array.from(normalized).every((character) =>
    printableCodePoint.test(character),
  );
  const withinCodePointRange =
    codePointCount >= minimumCodePoints && codePointCount <= maximumCodePoints;
  const withinUtf8Limit =
    utf8ByteCount <= maximumUtf8Bytes && normalizedUtf8ByteCount <= maximumUtf8Bytes;

  return {
    wellFormedUnicode,
    codePointCount,
    utf8ByteCount,
    normalizedUtf8ByteCount,
    printableUnicode,
    withinCodePointRange,
    withinUtf8Limit,
    valid: value.length > 0 && withinCodePointRange && withinUtf8Limit && printableUnicode,
  };
}

/** Confirmation follows backend NFC normalization and deliberately preserves spaces. */
export function newCredentialValuesMatch(value: string, confirmation: string): boolean {
  return (
    value.length > 0 &&
    confirmation.length > 0 &&
    hasWellFormedUnicode(value) &&
    hasWellFormedUnicode(confirmation) &&
    value.normalize("NFC") === confirmation.normalize("NFC")
  );
}
