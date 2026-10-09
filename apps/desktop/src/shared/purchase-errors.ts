// Only these static correction identifiers cross the main/renderer boundary.
// Server messages, paths, metadata and arbitrary protobuf details stay in main.
export const purchaseCorrectionCodes = [
  "PURCHASE_STALE",
  "PURCHASE_PAYMENT_INVALID",
  "PURCHASE_DUPLICATE",
  "PURCHASE_PERIOD_CLOSED",
  "PURCHASE_GST_BASIS_CHANGED",
  "PURCHASE_EVIDENCE_UNSUPPORTED",
  "PURCHASE_CLAIM_TIMING_INVALID",
  "PURCHASE_CLAIM_BALANCE_UNAVAILABLE",
  "PURCHASE_ELIGIBILITY_REQUIRED",
  "PURCHASE_ACCOUNT_INVALID",
  "PURCHASE_PRIOR_ACCOUNTING",
  "PURCHASE_VOLUME_UNSUPPORTED",
  "PURCHASE_AUDIT_COVERAGE_UNAVAILABLE",
] as const;

export type PurchaseCorrectionCode = (typeof purchaseCorrectionCodes)[number];

export function isPurchaseCorrectionCode(value: unknown): value is PurchaseCorrectionCode {
  return typeof value === "string" && purchaseCorrectionCodes.some((code) => code === value);
}

export class PurchaseCoreCorrection extends Error {
  constructor(readonly code: PurchaseCorrectionCode) {
    super(code);
    this.name = "PurchaseCoreCorrection";
  }
}
