export const MAX_REPORT_BYTES = 16 * 1024 * 1024;
interface CivilDate {
  readonly year: number;
  readonly month: number;
  readonly day: number;
}
interface Money {
  readonly currencyCode: string;
  readonly minorUnits: bigint;
}
export interface ReportOrganisation {
  readonly id: string;
  readonly version: bigint;
  readonly legalName: string;
  readonly displayName: string;
  readonly abn: string;
}
interface Context {
  readonly organisation: ReportOrganisation;
  readonly acquiredAt: string;
}
interface RetainedSourceRef {
  readonly sourceId: string;
  readonly sourceVersion: bigint;
  readonly sourceHash: Uint8Array;
}
interface ClaimPeriod {
  readonly periodStart?: CivilDate | undefined;
  readonly periodEnd?: CivilDate | undefined;
}
interface LegacySourceRef {
  readonly source:
    | {
        readonly case: "document";
        readonly value: {
          readonly documentId: string;
          readonly reviewedVersion: bigint;
          readonly originalContentHash: Uint8Array;
          readonly reviewedSnapshotHash: Uint8Array;
        };
      }
    | {
        readonly case: "openingPayable";
        readonly value: {
          readonly openingPayableId: string;
          readonly sourceVersion: bigint;
          readonly sourceHash: Uint8Array;
        };
      }
    | { readonly case: undefined; readonly value?: undefined };
}
interface PurchaseClaimTimingReview {
  readonly reviewedAsOf?: CivilDate | undefined;
  readonly allowablePeriodConfirmed: boolean;
  readonly ordinaryTimeLimitConfirmed: boolean;
  readonly evidenceHeldConfirmed: boolean;
  readonly reason: string;
}
interface PurchaseSourcePortion {
  readonly source:
    | {
        readonly case: "posting";
        readonly value: {
          readonly postingId: string;
          readonly postingHash: Uint8Array;
          readonly allocationSequence: number;
        };
      }
    | {
        readonly case: "payment";
        readonly value: {
          readonly postingId: string;
          readonly postingHash: Uint8Array;
          readonly paymentId: string;
          readonly paymentHash: Uint8Array;
          readonly allocationSequence: number;
        };
      }
    | { readonly case: undefined; readonly value?: undefined };
  readonly normalEventDate?: CivilDate | undefined;
  readonly earliestOtherwiseEligiblePeriod?: ClaimPeriod | undefined;
  readonly requiredReturnDueDate?: CivilDate | undefined;
  readonly dueDateEvidence?: RetainedSourceRef | undefined;
  readonly ordinaryLastClaimDate?: CivilDate | undefined;
  readonly evidenceId: string;
  readonly evidenceHeldDate?: CivilDate | undefined;
}
interface PurchaseFutureClaim {
  readonly deferralId: string;
  readonly organisationId: string;
  readonly postingId: string;
  readonly documentId: string;
  readonly accountingVersion: bigint;
  readonly deferralHash: Uint8Array;
  readonly portion?: PurchaseSourcePortion | undefined;
  readonly affectedPeriod?: ClaimPeriod | undefined;
  readonly timingReview?: PurchaseClaimTimingReview | undefined;
  readonly allocatedDeferredGst?: Money | undefined;
  readonly reason: string;
  readonly actorUserId: string;
  readonly recordedAt?: { readonly seconds: bigint; readonly nanos: number } | undefined;
}
interface PurchaseLegacyReview {
  readonly id: string;
  readonly organisationId: string;
  readonly source?: LegacySourceRef | undefined;
  readonly affectedPeriod?: ClaimPeriod | undefined;
  readonly reportingBalanceDate?: CivilDate | undefined;
  readonly disposition: number;
  readonly previouslyClaimedPeriod?: ClaimPeriod | undefined;
  readonly claimEvidence?: RetainedSourceRef | undefined;
  readonly linkedPostingId?: string | undefined;
  readonly reason: string;
  readonly sourceRevision: bigint;
  readonly sourceHash: Uint8Array;
  readonly actorUserId: string;
  readonly recordedAt?: { readonly seconds: bigint; readonly nanos: number } | undefined;
}
interface BasPurchaseSourceLine {
  readonly postingId: string;
  readonly postingHash: Uint8Array;
  readonly documentId: string;
  readonly originalContentHash: Uint8Array;
  readonly reviewedSnapshotHash: Uint8Array;
  readonly paymentId?: string | undefined;
  readonly paymentHash?: Uint8Array | undefined;
  readonly attributionId?: string | undefined;
  readonly attributionHash?: Uint8Array | undefined;
  readonly allocationSequence: number;
  readonly accountId?: string | undefined;
  readonly accountVersion?: bigint | undefined;
  readonly supplierName?: string | undefined;
  readonly invoiceNumber?: string | undefined;
  readonly use: number;
  readonly basClassification: number;
  readonly cashFlowClassification: number;
  readonly invoiceDate?: CivilDate | undefined;
  readonly actualPaymentDate?: CivilDate | undefined;
  readonly normalTaxEventDate?: CivilDate | undefined;
  readonly releasePostingDate?: CivilDate | undefined;
  readonly claimPeriod?: ClaimPeriod | undefined;
  readonly allocatedGross?: Money | undefined;
  readonly supplierGst?: Money | undefined;
  readonly eligibleGst?: Money | undefined;
  readonly claimedGst?: Money | undefined;
  readonly evidence?: RetainedSourceRef | undefined;
  readonly sourceRevision: bigint;
  readonly sourceHash: Uint8Array;
}
interface BasPurchaseLegacySupport {
  readonly reviewId: string;
  readonly kind: number;
  readonly sequence: number;
  readonly source?: RetainedSourceRef | undefined;
}
interface BasPurchaseReview {
  readonly legacySource?: LegacySourceRef | undefined;
  readonly documentId?: string | undefined;
  readonly postingId?: string | undefined;
  readonly paymentId?: string | undefined;
  readonly deferralId?: string | undefined;
  readonly status: number;
  readonly affectedPeriod?: ClaimPeriod | undefined;
  readonly invoiceDate?: CivilDate | undefined;
  readonly actualPaymentDate?: CivilDate | undefined;
  readonly eligibleGst?: Money | undefined;
  readonly unclaimedGst?: Money | undefined;
  readonly legacyReview?: PurchaseLegacyReview | undefined;
  readonly futureClaimObligation?: PurchaseFutureClaim | undefined;
  readonly evidence?: RetainedSourceRef | undefined;
  readonly sourceRevision: bigint;
  readonly sourceHash: Uint8Array;
  readonly reason: string;
  readonly kind: number;
}
export interface IncomeSnapshot extends Context {
  readonly summary: {
    readonly organisationId: string;
    readonly periodStart?: CivilDate | undefined;
    readonly periodEnd?: CivilDate | undefined;
    readonly asOfDate?: CivilDate | undefined;
    readonly businessIncome?: Money | undefined;
    readonly invoiceIncome?: Money | undefined;
    readonly paymentsReceived?: Money | undefined;
    readonly outstanding?: Money | undefined;
    readonly invoiceGst?: Money | undefined;
    readonly unpostedIncome?: Money | undefined;
    readonly issuedCount: bigint;
    readonly unpostedIssuedCount: bigint;
    readonly gstBasis: number;
    readonly financialRevision: bigint;
  };
}
export interface BasSnapshot extends Context {
  readonly workpaper: {
    readonly id: string;
    readonly organisationId: string;
    readonly version: bigint;
    readonly periodStart?: CivilDate | undefined;
    readonly periodEnd?: CivilDate | undefined;
    readonly status: number;
    readonly salesG1?: Money | undefined;
    readonly gstOnSales1a?: Money | undefined;
    readonly gstCredits1b?: Money | undefined;
    readonly netGstPayable?: Money | undefined;
    readonly gstBasis: number;
    readonly createdAt?: { readonly seconds: bigint; readonly nanos: number } | undefined;
    readonly sourceFingerprint: Uint8Array;
    readonly sourcesChanged: boolean;
    readonly reviewWarnings: readonly string[];
    readonly excludedCashPurchaseCount: bigint;
    readonly salesSources: readonly {
      readonly invoiceId: string;
      readonly paymentId?: string | undefined;
      readonly invoiceNumber: string;
      readonly customerName: string;
      readonly taxDate?: CivilDate | undefined;
      readonly gross?: Money | undefined;
      readonly gst?: Money | undefined;
      readonly sourceRevision: bigint;
      readonly sourceHash: Uint8Array;
    }[];
    readonly sources: readonly {
      readonly documentId: string;
      readonly supplierName: string;
      readonly invoiceNumber: string;
      readonly documentDate?: CivilDate | undefined;
      readonly gross?: Money | undefined;
      readonly gstCredit?: Money | undefined;
    }[];
    readonly purchaseSourceFormat?: number | undefined;
    readonly legacyCandidateGst?: Money | undefined;
    readonly purchaseSources?: readonly BasPurchaseSourceLine[] | undefined;
    readonly purchaseReviews?: readonly BasPurchaseReview[] | undefined;
    readonly legacyPurchaseSupport?: readonly BasPurchaseLegacySupport[] | undefined;
  };
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
function invalid(): never {
  throw new Error("INVALID_REPORT_SNAPSHOT");
}
function text(value: string, maximum: number, required = false) {
  if (
    typeof value !== "string" ||
    !value.isWellFormed() ||
    [...value].length > maximum ||
    (required && !value.trim())
  )
    invalid();
}
function identity(value: string) {
  if (!uuid.test(value)) invalid();
}
function uint(value: bigint, positive = false) {
  if (typeof value !== "bigint" || value < (positive ? 1n : 0n) || value > 18446744073709551615n)
    invalid();
}
export function reportDate(value: CivilDate | undefined): string {
  if (
    !value ||
    !Number.isInteger(value.year) ||
    value.year < 1 ||
    value.year > 9999 ||
    !Number.isInteger(value.month) ||
    !Number.isInteger(value.day)
  )
    invalid();
  const date = new Date(0);
  date.setUTCFullYear(value.year, value.month - 1, value.day);
  if (
    date.getUTCFullYear() !== value.year ||
    date.getUTCMonth() + 1 !== value.month ||
    date.getUTCDate() !== value.day
  )
    invalid();
  return `${String(value.year).padStart(4, "0")}-${String(value.month).padStart(2, "0")}-${String(value.day).padStart(2, "0")}`;
}
export function exactDecimal(value: bigint): string {
  if (typeof value !== "bigint" || value < -9223372036854775808n || value > 9223372036854775807n)
    invalid();
  const magnitude = value < 0n ? -value : value;
  return `${value < 0n ? "-" : ""}${magnitude / 100n}.${String(magnitude % 100n).padStart(2, "0")}`;
}
function money(value: Money | undefined, signed = false) {
  if (value?.currencyCode !== "AUD") invalid();
  const amount = exactDecimal(value.minorUnits);
  if (!signed && value.minorUnits < 0n) invalid();
  return amount;
}
function requiredMoney(value: Money | undefined) {
  if (!value) invalid();
  money(value);
  return value;
}
function basis(value: number) {
  if (value === 1) return "Cash";
  if (value === 2) return "Non-cash";
  if (value === 0) return "Unconfigured";
  return invalid();
}
export function reportTimestamp(value: BasSnapshot["workpaper"]["createdAt"]) {
  if (
    !value ||
    typeof value.seconds !== "bigint" ||
    value.seconds < -62135596800n ||
    value.seconds > 253402300799n ||
    !Number.isInteger(value.nanos) ||
    value.nanos < 0 ||
    value.nanos > 999999999
  )
    invalid();
  const fraction =
    value.nanos === 0 ? "000" : String(value.nanos).padStart(9, "0").replace(/0+$/u, "");
  return new Date(Number(value.seconds) * 1000).toISOString().replace(/\.000Z$/u, `.${fraction}Z`);
}
function hash(value: Uint8Array, legacy = false) {
  if (!(value instanceof Uint8Array) || (value.length !== 32 && !(legacy && value.length === 0)))
    invalid();
  return [...value].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}
function context(value: Context) {
  identity(value.organisation.id);
  uint(value.organisation.version, true);
  text(value.organisation.legalName, 256, true);
  text(value.organisation.displayName, 256, true);
  if (!/^[0-9]{11}$/.test(value.organisation.abn)) invalid();
  if (
    typeof value.acquiredAt !== "string" ||
    value.acquiredAt.length > 32 ||
    !Number.isFinite(Date.parse(value.acquiredAt)) ||
    new Date(value.acquiredAt).toISOString() !== value.acquiredAt
  )
    invalid();
}
function period(start?: CivilDate, end?: CivilDate) {
  if (reportDate(start) > reportDate(end)) invalid();
}
const purchaseUses = { 1: "business", 2: "private" } as const;
const purchaseBasClassifications = { 1: "capital", 2: "non_capital", 3: "excluded" } as const;
const cashFlowClassifications = { 1: "operating", 2: "investing", 3: "financing" } as const;
const purchaseReviewStatuses = {
  1: "clear",
  2: "needs_evidence",
  3: "needs_timing_review",
  4: "unresolved_legacy",
  5: "unsupported",
  6: "deferred",
} as const;
const purchaseReviewKinds = {
  1: "unresolved_eligibility",
  2: "missing_evidence",
  3: "claim_timing",
  4: "excluded_unpaid",
  5: "excluded_private_or_noncreditable",
  6: "duplicate",
  7: "unsupported_settlement",
  8: "deferred_future_claim",
  9: "old_legacy_document",
  10: "undated_legacy_document",
  11: "opening_payable",
  12: "resolved_legacy",
} as const;
const legacyDispositions = {
  1: "confirmed_unpaid_through_balance",
  2: "already_claimed",
  3: "duplicate",
  4: "noncreditable",
  5: "evidence_only_linked_purchase",
  6: "unsupported_existing_settlement",
} as const;
const legacySupportKinds = { 1: "payment", 2: "evidence" } as const;
function enumLabel<T extends Record<number, string>>(value: number, values: T): T[keyof T] {
  if (!Number.isInteger(value) || !Object.hasOwn(values, value)) invalid();
  return values[value as keyof T];
}
function purchaseClaimPeriod(value: ClaimPeriod | undefined) {
  if (!value) invalid();
  period(value.periodStart, value.periodEnd);
  return [reportDate(value.periodStart), reportDate(value.periodEnd)] as const;
}
function retainedSource(value: RetainedSourceRef | undefined) {
  if (!value) invalid();
  identity(value.sourceId);
  uint(value.sourceVersion, true);
  hash(value.sourceHash);
  return value;
}
function legacySource(value: LegacySourceRef | undefined) {
  if (!value) invalid();
  switch (value.source.case) {
    case "document":
      identity(value.source.value.documentId);
      uint(value.source.value.reviewedVersion, true);
      hash(value.source.value.originalContentHash);
      hash(value.source.value.reviewedSnapshotHash);
      return {
        kind: "document",
        id: value.source.value.documentId,
        version: value.source.value.reviewedVersion,
        originalHash: hash(value.source.value.originalContentHash),
        reviewedHash: hash(value.source.value.reviewedSnapshotHash),
        hash: "",
      } as const;
    case "openingPayable":
      identity(value.source.value.openingPayableId);
      uint(value.source.value.sourceVersion, true);
      hash(value.source.value.sourceHash);
      return {
        kind: "opening_payable",
        id: value.source.value.openingPayableId,
        version: value.source.value.sourceVersion,
        originalHash: "",
        reviewedHash: "",
        hash: hash(value.source.value.sourceHash),
      } as const;
    default:
      return invalid();
  }
}
function claimPeriodMatchesReport(
  value: ClaimPeriod | undefined,
  start: CivilDate | undefined,
  end: CivilDate | undefined,
) {
  const [claimStart, claimEnd] = purchaseClaimPeriod(value);
  if (claimStart !== reportDate(start) || claimEnd !== reportDate(end)) invalid();
}
function sourceHash(value: Uint8Array) {
  return hash(value);
}
function timestamp(value: { readonly seconds: bigint; readonly nanos: number } | undefined) {
  return reportTimestamp(value);
}
function purchaseSourceLine(value: BasPurchaseSourceLine, report: BasSnapshot["workpaper"]) {
  identity(value.postingId);
  sourceHash(value.postingHash);
  identity(value.documentId);
  sourceHash(value.originalContentHash);
  sourceHash(value.reviewedSnapshotHash);
  if ((value.paymentId === undefined) !== (value.paymentHash === undefined)) invalid();
  if (value.paymentId !== undefined) identity(value.paymentId);
  if (value.paymentHash !== undefined) sourceHash(value.paymentHash);
  if ((value.attributionId === undefined) !== (value.attributionHash === undefined)) invalid();
  if (value.attributionId !== undefined) identity(value.attributionId);
  if (value.attributionHash !== undefined) sourceHash(value.attributionHash);
  if (
    !Number.isInteger(value.allocationSequence) ||
    value.allocationSequence < 1 ||
    value.allocationSequence > 0xffffffff
  )
    invalid();
  enumLabel(value.use, purchaseUses);
  enumLabel(value.basClassification, purchaseBasClassifications);
  enumLabel(value.cashFlowClassification, cashFlowClassifications);
  if ((value.accountId === undefined) !== (value.accountVersion === undefined)) invalid();
  if (value.accountId !== undefined) identity(value.accountId);
  if (value.accountVersion !== undefined) uint(value.accountVersion, true);
  if (value.supplierName !== undefined) text(value.supplierName, 256);
  if (value.invoiceNumber !== undefined) text(value.invoiceNumber, 128);
  const invoiceDate = reportDate(value.invoiceDate);
  const normalEvent = reportDate(value.normalTaxEventDate);
  const [claimStart, claimEnd] = purchaseClaimPeriod(value.claimPeriod);
  if (claimStart !== reportDate(report.periodStart) || claimEnd !== reportDate(report.periodEnd))
    invalid();
  if (invoiceDate > claimEnd || normalEvent > claimEnd) invalid();
  if ((value.paymentId === undefined) !== (value.actualPaymentDate === undefined)) invalid();
  const paymentDate = value.actualPaymentDate ? reportDate(value.actualPaymentDate) : undefined;
  if (paymentDate && paymentDate > claimEnd) invalid();
  if (report.gstBasis === 1 && (paymentDate === undefined || normalEvent !== paymentDate))
    invalid();
  if (report.gstBasis === 2 && normalEvent !== invoiceDate) invalid();
  if (value.releasePostingDate !== undefined) {
    const releaseDate = reportDate(value.releasePostingDate);
    if (releaseDate < normalEvent || releaseDate < claimStart || releaseDate > claimEnd) invalid();
  }
  const gross = requiredMoney(value.allocatedGross);
  const supplier = requiredMoney(value.supplierGst);
  const eligible = requiredMoney(value.eligibleGst);
  const claimed = requiredMoney(value.claimedGst);
  if (
    supplier.minorUnits > gross.minorUnits ||
    eligible.minorUnits > supplier.minorUnits ||
    claimed.minorUnits > eligible.minorUnits
  )
    invalid();
  if ((value.use === 2 || value.basClassification === 3) && claimed.minorUnits !== 0n) invalid();
  retainedSource(value.evidence);
  uint(value.sourceRevision, true);
  sourceHash(value.sourceHash);
  return value;
}
function purchaseTimingReview(value: PurchaseClaimTimingReview | undefined) {
  if (!value) invalid();
  reportDate(value.reviewedAsOf);
  if (
    value.allowablePeriodConfirmed !== true ||
    value.ordinaryTimeLimitConfirmed !== true ||
    value.evidenceHeldConfirmed !== true
  )
    invalid();
  text(value.reason, 512, true);
}
function futureClaim(
  value: PurchaseFutureClaim | undefined,
  review: BasPurchaseReview,
  orgId: string,
) {
  if (!value) invalid();
  identity(value.deferralId);
  if (value.organisationId !== orgId) invalid();
  identity(value.organisationId);
  identity(value.postingId);
  identity(value.documentId);
  uint(value.accountingVersion, true);
  sourceHash(value.deferralHash);
  if (value.deferralId !== review.deferralId || value.postingId !== review.postingId) invalid();
  if (review.paymentId !== undefined && value.portion?.source.case === "payment") {
    if (review.paymentId !== value.portion.source.value.paymentId) invalid();
  }
  const [, affectedEnd] = purchaseClaimPeriod(value.affectedPeriod);
  const portion = value.portion;
  if (!portion) invalid();
  switch (portion.source.case) {
    case "posting":
      identity(portion.source.value.postingId);
      sourceHash(portion.source.value.postingHash);
      if (portion.source.value.postingId !== value.postingId) invalid();
      break;
    case "payment":
      identity(portion.source.value.postingId);
      sourceHash(portion.source.value.postingHash);
      identity(portion.source.value.paymentId);
      sourceHash(portion.source.value.paymentHash);
      if (
        portion.source.value.postingId !== value.postingId ||
        portion.source.value.paymentId !== review.paymentId
      )
        invalid();
      break;
    default:
      invalid();
  }
  if (
    !Number.isInteger(portion.source.value.allocationSequence) ||
    portion.source.value.allocationSequence < 1 ||
    portion.source.value.allocationSequence > 0xffffffff
  )
    invalid();
  const eventDate = reportDate(portion.normalEventDate);
  const [earliestStart, earliestEnd] = purchaseClaimPeriod(portion.earliestOtherwiseEligiblePeriod);
  if (eventDate > earliestEnd || earliestStart > affectedEnd) invalid();
  const dueDate = reportDate(portion.requiredReturnDueDate);
  const ordinaryLastClaimDate = reportDate(portion.ordinaryLastClaimDate);
  if (ordinaryLastClaimDate < dueDate) invalid();
  retainedSource(portion.dueDateEvidence);
  identity(portion.evidenceId);
  const evidenceHeldDate = reportDate(portion.evidenceHeldDate);
  // The immutable deferral records its original evidence and reviewed scope.
  // The enclosing outcome separately retains the current evidence and period.
  if (evidenceHeldDate > affectedEnd) invalid();
  purchaseTimingReview(value.timingReview);
  const deferredAmount = requiredMoney(value.allocatedDeferredGst);
  const unclaimedAmount = requiredMoney(review.unclaimedGst);
  if (deferredAmount.minorUnits < unclaimedAmount.minorUnits) invalid();
  text(value.reason, 512, true);
  identity(value.actorUserId);
  timestamp(value.recordedAt);
  return value;
}
function purchaseReview(value: BasPurchaseReview, report: BasSnapshot["workpaper"], orgId: string) {
  const status = enumLabel(value.status, purchaseReviewStatuses);
  const kind = enumLabel(value.kind, purchaseReviewKinds);
  if (
    value.postingId === undefined &&
    value.paymentId === undefined &&
    value.deferralId === undefined &&
    value.legacySource === undefined
  )
    invalid();
  if (value.documentId !== undefined) identity(value.documentId);
  if (value.postingId !== undefined) identity(value.postingId);
  if (value.paymentId !== undefined) identity(value.paymentId);
  if (value.deferralId !== undefined) identity(value.deferralId);
  const hasFutureClaim = value.futureClaimObligation !== undefined;
  if (
    (value.deferralId === undefined) !== !hasFutureClaim ||
    (kind === "deferred_future_claim") !== hasFutureClaim
  )
    invalid();
  claimPeriodMatchesReport(value.affectedPeriod, report.periodStart, report.periodEnd);
  if (value.invoiceDate !== undefined) reportDate(value.invoiceDate);
  if (value.actualPaymentDate !== undefined) reportDate(value.actualPaymentDate);
  const eligibleAmount = requiredMoney(value.eligibleGst);
  const unclaimedAmount = requiredMoney(value.unclaimedGst);
  if (unclaimedAmount.minorUnits > eligibleAmount.minorUnits) invalid();
  if (value.evidence !== undefined) retainedSource(value.evidence);
  uint(value.sourceRevision, true);
  sourceHash(value.sourceHash);
  text(value.reason, 512, true);
  const source = value.legacySource === undefined ? undefined : legacySource(value.legacySource);
  if (value.legacyReview !== undefined) {
    const decision = value.legacyReview;
    identity(decision.id);
    if (decision.organisationId !== orgId) invalid();
    identity(decision.organisationId);
    const decisionSource = legacySource(decision.source);
    if (
      !source ||
      source.kind !== decisionSource.kind ||
      source.id !== decisionSource.id ||
      source.version !== decisionSource.version ||
      source.originalHash !== decisionSource.originalHash ||
      source.reviewedHash !== decisionSource.reviewedHash ||
      source.hash !== decisionSource.hash
    )
      invalid();
    const [, decisionEnd] = purchaseClaimPeriod(decision.affectedPeriod);
    if (reportDate(decision.reportingBalanceDate) < decisionEnd) invalid();
    enumLabel(decision.disposition, legacyDispositions);
    if (decision.previouslyClaimedPeriod !== undefined)
      purchaseClaimPeriod(decision.previouslyClaimedPeriod);
    if (decision.claimEvidence !== undefined) retainedSource(decision.claimEvidence);
    if (
      decision.disposition === 2 &&
      (!decision.previouslyClaimedPeriod || !decision.claimEvidence)
    )
      invalid();
    if (decision.linkedPostingId !== undefined) identity(decision.linkedPostingId);
    if (decision.disposition === 5 && decision.linkedPostingId === undefined) invalid();
    text(decision.reason, 512, true);
    uint(decision.sourceRevision, true);
    sourceHash(decision.sourceHash);
    identity(decision.actorUserId);
    timestamp(decision.recordedAt);
  }
  if (value.futureClaimObligation !== undefined) {
    if (kind !== "deferred_future_claim") invalid();
    futureClaim(value.futureClaimObligation, value, orgId);
  }
  return { ...value, status, kind, legacy: source };
}
function validatePurchaseSources(report: BasSnapshot["workpaper"], orgId: string) {
  const sourceFormat = report.purchaseSourceFormat;
  const sourceLines = report.purchaseSources;
  const purchaseReviews = report.purchaseReviews;
  const legacySupport = report.legacyPurchaseSupport;
  const sources = sourceLines ?? [];
  const reviews = purchaseReviews ?? [];
  const support = legacySupport ?? [];
  if (!Array.isArray(sources) || !Array.isArray(reviews) || !Array.isArray(support)) invalid();
  if (sources.length > 1000 || reviews.length > 1000 || support.length > 1000)
    throw new Error("REPORT_SOURCE_LIMIT");
  if (sourceFormat === undefined) {
    if (
      sources.length > 0 ||
      reviews.length > 0 ||
      support.length > 0 ||
      report.legacyCandidateGst !== undefined
    )
      invalid();
    return false;
  }
  if (sourceFormat !== 1) invalid();
  if (
    !Array.isArray(sourceLines) ||
    !Array.isArray(purchaseReviews) ||
    !Array.isArray(legacySupport)
  )
    invalid();
  if (report.legacyCandidateGst !== undefined) money(report.legacyCandidateGst);
  const sourceKeys = new Set<string>();
  let typedClaims = 0n;
  for (const source of sources) {
    purchaseSourceLine(source, report);
    const key = `${source.postingId}/${source.paymentId ?? ""}/${source.attributionId ?? ""}/${source.allocationSequence}`;
    if (sourceKeys.has(key)) invalid();
    sourceKeys.add(key);
    typedClaims += requiredMoney(source.claimedGst).minorUnits;
  }
  const reviewIds = new Set<string>();
  const postedDocuments = new Map<string, string>();
  for (const source of sources) {
    const documentId = postedDocuments.get(source.postingId);
    if (documentId !== undefined && documentId !== source.documentId) invalid();
    postedDocuments.set(source.postingId, source.documentId);
  }
  const legacyReviewIds = new Set<string>();
  for (const review of reviews) {
    const checked = purchaseReview(review, report, orgId);
    const key = `${checked.kind}/${review.postingId ?? ""}/${review.paymentId ?? ""}/${review.deferralId ?? ""}/${checked.legacy?.kind ?? ""}/${checked.legacy?.id ?? ""}/${review.legacyReview?.id ?? ""}`;
    if (reviewIds.has(key)) invalid();
    reviewIds.add(key);
    if (review.legacyReview) {
      if (legacyReviewIds.has(review.legacyReview.id)) invalid();
      legacyReviewIds.add(review.legacyReview.id);
    }
    if (
      review.documentId !== undefined &&
      review.futureClaimObligation &&
      review.documentId !== review.futureClaimObligation.documentId
    )
      invalid();
    if (review.postingId) {
      const documentId = postedDocuments.get(review.postingId);
      if (
        documentId !== undefined &&
        ((review.documentId !== undefined && documentId !== review.documentId) ||
          (review.futureClaimObligation && documentId !== review.futureClaimObligation.documentId))
      )
        invalid();
    }
  }
  const supportOrder = new Map<string, number>();
  const supportKeys = new Set<string>();
  for (const link of support) {
    identity(link.reviewId);
    enumLabel(link.kind, legacySupportKinds);
    if (!Number.isInteger(link.sequence) || link.sequence < 1 || link.sequence > 0xffffffff)
      invalid();
    if (!legacyReviewIds.has(link.reviewId)) invalid();
    const source = retainedSource(link.source);
    const key = `${link.reviewId}/${link.kind}/${link.sequence}/${source.sourceId}/${source.sourceVersion}`;
    if (supportKeys.has(key)) invalid();
    supportKeys.add(key);
    const orderKey = `${link.reviewId}/${link.kind}`;
    const expected = (supportOrder.get(orderKey) ?? 0) + 1;
    if (link.sequence !== expected) invalid();
    supportOrder.set(orderKey, expected);
  }
  let legacyDirectClaims = 0n;
  for (const source of report.sources)
    legacyDirectClaims += requiredMoney(source.gstCredit).minorUnits;
  const gstCredits = requiredMoney(report.gstCredits1b);
  if (
    legacyDirectClaims + typedClaims > 9223372036854775807n ||
    legacyDirectClaims + typedClaims !== gstCredits.minorUnits
  )
    invalid();
  return true;
}
export function validateIncomeSnapshot(value: IncomeSnapshot): IncomeSnapshot {
  context(value);
  const report = value.summary;
  if (report.organisationId !== value.organisation.id) invalid();
  period(report.periodStart, report.periodEnd);
  reportDate(report.asOfDate);
  basis(report.gstBasis);
  uint(report.financialRevision);
  uint(report.issuedCount);
  uint(report.unpostedIssuedCount);
  for (const key of [
    "businessIncome",
    "invoiceIncome",
    "paymentsReceived",
    "outstanding",
    "invoiceGst",
    "unpostedIncome",
  ] as const)
    money(report[key], key === "businessIncome");
  return value;
}
export function validateBasSnapshot(value: BasSnapshot): BasSnapshot {
  context(value);
  const report = value.workpaper;
  identity(report.id);
  if (report.organisationId !== value.organisation.id || report.status !== 1) invalid();
  uint(report.version, true);
  period(report.periodStart, report.periodEnd);
  if (report.gstBasis !== 1 && report.gstBasis !== 2) invalid();
  reportTimestamp(report.createdAt);
  hash(report.sourceFingerprint, true);
  uint(report.excludedCashPurchaseCount);
  if (
    typeof report.sourcesChanged !== "boolean" ||
    !Array.isArray(report.reviewWarnings) ||
    report.reviewWarnings.length > 10
  )
    invalid();
  for (const warning of report.reviewWarnings) text(warning, 500);
  if (!Array.isArray(report.sources) || !Array.isArray(report.salesSources)) invalid();
  if (report.sources.length > 1000 || report.salesSources.length > 1000)
    throw new Error("REPORT_SOURCE_LIMIT");
  for (const key of ["salesG1", "gstOnSales1a", "gstCredits1b", "netGstPayable"] as const)
    money(report[key], key === "netGstPayable");
  for (const source of report.sources) {
    identity(source.documentId);
    text(source.supplierName, 256);
    text(source.invoiceNumber, 128);
    reportDate(source.documentDate);
    money(source.gross);
    money(source.gstCredit);
    if (source.gstCredit && source.gross && source.gstCredit.minorUnits > source.gross.minorUnits)
      invalid();
  }
  for (const source of report.salesSources) {
    identity(source.invoiceId);
    if (source.paymentId !== undefined) identity(source.paymentId);
    text(source.customerName, 200);
    text(source.invoiceNumber, 40);
    reportDate(source.taxDate);
    money(source.gross);
    money(source.gst);
    if (source.gst && source.gross && source.gst.minorUnits > source.gross.minorUnits) invalid();
    uint(source.sourceRevision, true);
    hash(source.sourceHash);
  }
  validatePurchaseSources(report, value.organisation.id);
  return value;
}
export function boundedReportBytes(bytes: Uint8Array) {
  if (!(bytes instanceof Uint8Array) || bytes.length === 0) throw new Error("REPORT_RENDER_FAILED");
  if (bytes.length > MAX_REPORT_BYTES) throw new Error("REPORT_SIZE_LIMIT");
  return bytes;
}
