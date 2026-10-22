// These identifiers are the only BAS Core statuses allowed through the
// Conveyor boundary. Core messages, metadata, and protobuf details stay private.
export const basPreparationCorrectionCodes = [
  "BAS_SOURCE_CAPACITY",
  "BAS_CONFLICT",
  "BAS_IDEMPOTENCY_CONFLICT",
  "BAS_PERMISSION_DENIED",
  "BAS_AUTHENTICATION_REQUIRED",
  "BAS_REQUEST_TIMEOUT",
] as const;

export type BasPreparationCorrectionCode = (typeof basPreparationCorrectionCodes)[number];

export function isBasPreparationCorrectionCode(
  value: unknown,
): value is BasPreparationCorrectionCode {
  return typeof value === "string" && basPreparationCorrectionCodes.some((code) => code === value);
}

export class BasCoreCorrection extends Error {
  constructor(readonly code: BasPreparationCorrectionCode) {
    super(code);
    this.name = "BasCoreCorrection";
  }
}
