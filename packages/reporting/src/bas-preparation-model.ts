import type {
  BasCalculation,
  BasFrozenBas,
  BasLabelTrace,
  BasPreparation,
  BasSourceCoverage,
  BasSourceIdentity,
  BasValidationFinding,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import type { SourceRef } from "@tammy/connect-client/tammy/v1/common_pb.js";
import type { ReportOrganisation } from "./model.ts";
import { type BasSnapshot, reportTimestamp, validateBasSnapshot } from "./model.ts";

export const BAS_PREPARATION_LABELS = [
  { code: 1, label: "G1" },
  { code: 2, label: "1A" },
  { code: 3, label: "1B" },
  { code: 4, label: "W1" },
  { code: 5, label: "W2" },
  { code: 6, label: "W3" },
  { code: 7, label: "W4" },
  { code: 8, label: "W5" },
  { code: 9, label: "4" },
  { code: 10, label: "T1" },
  { code: 11, label: "T2" },
  { code: 12, label: "T7" },
  { code: 13, label: "T11" },
  { code: 14, label: "5A" },
  { code: 15, label: "5B" },
  { code: 16, label: "8A" },
  { code: 17, label: "8B" },
  { code: 18, label: "9" },
] as const;

export interface BasPreparationReportSnapshot {
  readonly kind: "bas_preparation_report_v1";
  readonly target: {
    readonly kind: "calculation" | "frozen_bas";
    readonly id: string;
    readonly revision: bigint;
    readonly hash: Uint8Array;
  };
  readonly organisation: ReportOrganisation;
  readonly acquiredAt: string;
  readonly calculation: BasCalculation;
  readonly frozenBas?: BasFrozenBas | undefined;
  readonly preparation?: BasPreparation | undefined;
  readonly predecessor?:
    | {
        readonly preparation: BasPreparation;
        readonly calculation?: BasCalculation | undefined;
      }
    | undefined;
}

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

function id(value: string) {
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u.test(value))
    invalid();
}

function hash(value: Uint8Array, required = true) {
  if (
    !(value instanceof Uint8Array) ||
    (required && value.length !== 32) ||
    (!required && value.length !== 0 && value.length !== 32)
  )
    invalid();
}

function uint(value: bigint, positive = false) {
  if (typeof value !== "bigint" || value < (positive ? 1n : 0n) || value > 18446744073709551615n)
    invalid();
}

function rational(numerator: string, denominator: string, signedNumerator = false) {
  const integer = signedNumerator ? /^-?(?:0|[1-9][0-9]*)$/u : /^(?:0|[1-9][0-9]*)$/u;
  if (!integer.test(numerator) || !/^[1-9][0-9]*$/u.test(denominator)) invalid();
}

function ref(value: SourceRef | undefined, expectedType?: string) {
  if (!value) invalid();
  text(value.type, 128, true);
  id(value.id);
  uint(value.revision, true);
  hash(value.contentHash);
  if (expectedType && value.type !== expectedType) invalid();
}

function sourceIdentity(value: BasSourceIdentity) {
  if (!value || Boolean(value.canonicalSource) === Boolean(value.rawSource)) invalid();
  if (value.canonicalSource) ref(value.canonicalSource);
  if (value.rawSource) rawIdentity(value.rawSource);
}

function rawIdentity(value: NonNullable<BasSourceIdentity["rawSource"]>) {
  text(value.type, 128, true);
  text(value.id, 2048, true);
  if (value.originalRevision !== undefined) {
    if (value.originalRevision < 0n || value.originalRevision > 9223372036854775807n) invalid();
  }
  if (value.originalContentHash !== undefined) hash(value.originalContentHash);
  hash(value.computedProofHash);
  if (value.proofScope < 1 || value.proofScope > 3) invalid();
  if (value.componentSequence !== undefined) uint(value.componentSequence, true);
  if (value.relatedId !== undefined) text(value.relatedId, 2048, true);
  if (value.relatedSequence !== undefined) uint(value.relatedSequence, true);
  if (value.relatedSequence !== undefined && value.relatedId === undefined) invalid();
}

function optionalRefs(values: readonly SourceRef[], maximum: number) {
  if (!Array.isArray(values) || values.length > maximum) invalid();
  for (const value of values) ref(value);
}

function labelTrace(value: BasLabelTrace) {
  if (!value || value.label < 1 || value.label > 18 || value.operation < 1 || value.operation > 6)
    invalid();
  if (value.state < 1 || value.state > 5) invalid();
  text(value.ruleId, 128, true);
  if (!Array.isArray(value.operands) || value.operands.length > 8) invalid();
  if (!Array.isArray(value.evidence) || value.evidence.length > 20) invalid();
  for (const evidence of value.evidence) ref(evidence);
  for (const operand of value.operands) {
    if (operand.label < 1 || operand.label > 18 || operand.state < 1 || operand.state > 5)
      invalid();
    text(operand.exactCentsNumerator ?? "", 80, true);
    text(operand.exactCentsDenominator ?? "", 80, true);
    if (operand.exactRateNumerator !== undefined || operand.exactRateDenominator !== undefined) {
      text(operand.exactRateNumerator ?? "", 80, true);
      text(operand.exactRateDenominator ?? "", 80, true);
      rational(operand.exactRateNumerator!, operand.exactRateDenominator!);
    }
    rational(operand.exactCentsNumerator, operand.exactCentsDenominator);
  }
  const numeric = [
    value.exactCentsNumerator,
    value.exactCentsDenominator,
    value.roundingDifferenceCentsNumerator,
    value.roundingDifferenceCentsDenominator,
    value.exactRateNumerator,
    value.exactRateDenominator,
  ];
  if (value.reportedWholeDollars !== undefined && value.reportedWholeDollars < 0n) invalid();
  if (value.state === 4 || value.state === 5) {
    if (value.label === 11) {
      text(value.exactRateNumerator ?? "", 80, true);
      text(value.exactRateDenominator ?? "", 80, true);
      rational(value.exactRateNumerator!, value.exactRateDenominator!);
    } else {
      text(value.exactCentsNumerator ?? "", 80, true);
      text(value.exactCentsDenominator ?? "", 80, true);
      if (value.reportedWholeDollars === undefined) invalid();
      text(value.roundingDifferenceCentsNumerator ?? "", 80, true);
      text(value.roundingDifferenceCentsDenominator ?? "", 80, true);
      rational(value.exactCentsNumerator!, value.exactCentsDenominator!);
      rational(
        value.roundingDifferenceCentsNumerator!,
        value.roundingDifferenceCentsDenominator!,
        true,
      );
    }
  } else if (
    numeric.some((item) => item !== undefined) ||
    value.reportedWholeDollars !== undefined
  ) {
    invalid();
  }
}

function coverage(value: BasSourceCoverage) {
  if (value.source && value.rawSource) invalid();
  if (value.source) ref(value.source);
  if (value.rawSource) rawIdentity(value.rawSource);
  if (
    value.candidateKind < 1 ||
    value.candidateKind > 7 ||
    value.disposition < 1 ||
    value.disposition > 6
  )
    invalid();
  if (
    value.eligibleAmount &&
    (value.eligibleAmount.currencyCode !== "AUD" || value.eligibleAmount.minorUnits < 0n)
  )
    invalid();
  if (value.taxDate) {
    const { year, month, day } = value.taxDate;
    if (
      !Number.isInteger(year) ||
      year < 1 ||
      year > 9999 ||
      !Number.isInteger(month) ||
      !Number.isInteger(day)
    )
      invalid();
    const date = new Date(0);
    date.setUTCFullYear(year, month - 1, day);
    if (
      date.getUTCFullYear() !== year ||
      date.getUTCMonth() + 1 !== month ||
      date.getUTCDate() !== day
    )
      invalid();
  }
  text(value.classificationCode, 96, true);
  text(value.reason, 512, true);
  if (
    value.lineage.length > 20 ||
    value.evidence.length > 20 ||
    value.lineageIdentities.length > 20 ||
    value.evidenceIdentities.length > 20
  )
    invalid();
  optionalRefs(value.lineage, 20);
  optionalRefs(value.evidence, 20);
  for (const identity of value.lineageIdentities) sourceIdentity(identity);
  for (const identity of value.evidenceIdentities) sourceIdentity(identity);
}

function finding(value: BasValidationFinding) {
  if (value.severity < 1 || value.severity > 4) invalid();
  id(value.findingId);
  text(value.stableCode, 128, true);
  text(value.safeMessage, 512, true);
  if (value.sources.length > 20 || value.sourceIdentities.length > 20) invalid();
  optionalRefs(value.sources, 20);
  for (const identity of value.sourceIdentities) sourceIdentity(identity);
}

function calculation(value: BasCalculation, organisationId: string) {
  id(value.calculationId);
  id(value.organisationId);
  if (value.organisationId !== organisationId) invalid();
  uint(value.version, true);
  hash(value.contentHash);
  ref(value.preparation, "bas_preparation");
  ref(value.family, "bas_family");
  ref(value.period, "bas_period");
  ref(value.profile, "bas_obligations_profile");
  ref(value.centWorkpaper, "bas_workpaper");
  hash(value.sourceSnapshotHash);
  hash(value.coverageManifestHash);
  hash(value.revisionVectorHash);
  hash(value.ruleHash);
  hash(value.calculatorHash);
  hash(value.formHash);
  hash(value.schemaHash);
  hash(value.descriptorHash);
  hash(value.organisationSnapshotHash);
  for (const field of [
    value.ruleVersion,
    value.calculatorVersion,
    value.formVersion,
    value.schemaVersion,
    value.descriptorVersion,
  ])
    text(field, 128, true);
  if (!value.createdAt) invalid();
  reportTimestamp(value.createdAt);
  uint(value.inputRevision, true);
  hash(value.inputHash);
  if (
    !value.result ||
    value.result.outcome < 1 ||
    value.result.outcome > 3 ||
    value.result.label9Direction < 0 ||
    value.result.label9Direction > 3
  )
    invalid();
  if (value.result.labels.length !== BAS_PREPARATION_LABELS.length) invalid();
  const labels = new Set<number>();
  for (const trace of value.result.labels) {
    labelTrace(trace);
    if (labels.has(trace.label)) invalid();
    labels.add(trace.label);
  }
  if (
    labels.size !== BAS_PREPARATION_LABELS.length ||
    BAS_PREPARATION_LABELS.some((label) => !labels.has(label.code))
  )
    invalid();
  if (value.coverage.length > 1000 || value.findings.length > 200) invalid();
  for (const row of value.coverage) coverage(row);
  for (const row of value.findings) finding(row);
  if (
    !value.periodSnapshot ||
    !value.profileSnapshot ||
    !value.paygwInputs ||
    !value.paygiInputs ||
    !value.organisationSnapshot ||
    !value.revisionVector ||
    !value.workpaperSnapshot
  )
    invalid();
  if (value.organisationSnapshot.id !== organisationId) invalid();
  if (!value.organisationIdentity) invalid();
  id(value.organisationIdentity.id);
  ref(value.organisationIdentity, "organisation");
  if (value.revisionVector.financialRevision < 0n) invalid();
  const legacy: BasSnapshot = {
    organisation: {
      id: organisationId,
      version: value.organisationSnapshot.version,
      legalName: value.organisationSnapshot.legalName,
      displayName: value.organisationSnapshot.displayName,
      abn: value.organisationSnapshot.abn,
    },
    workpaper: value.workpaperSnapshot as unknown as BasSnapshot["workpaper"],
    acquiredAt: new Date(0).toISOString(),
  };
  validateBasSnapshot(legacy);
}

function sameRef(
  value: SourceRef | undefined,
  idValue: string,
  hashValue: Uint8Array,
  revision?: bigint,
) {
  return (
    value?.id === idValue &&
    (revision === undefined || value.revision === revision) &&
    value.contentHash.length === hashValue.length &&
    value.contentHash.every((byte, index) => byte === hashValue[index])
  );
}

function sameIdentity(
  idValue: string,
  hashValue: Uint8Array,
  expectedId: string,
  expectedHash: Uint8Array,
) {
  return (
    idValue === expectedId &&
    hashValue.length === expectedHash.length &&
    hashValue.every((byte, index) => byte === expectedHash[index])
  );
}

export function validateBasPreparationReportSnapshot(value: BasPreparationReportSnapshot) {
  if (!value || value.kind !== "bas_preparation_report_v1") invalid();
  id(value.organisation.id);
  uint(value.organisation.version, true);
  text(value.organisation.legalName, 256, true);
  text(value.organisation.displayName, 256, true);
  text(value.organisation.abn, 32, true);
  if (!Number.isFinite(Date.parse(value.acquiredAt))) invalid();
  id(value.target.id);
  uint(value.target.revision, true);
  hash(value.target.hash);
  calculation(value.calculation, value.organisation.id);
  if (value.target.kind === "calculation") {
    if (
      value.frozenBas ||
      value.calculation.calculationId !== value.target.id ||
      value.calculation.version !== value.target.revision ||
      !sameIdentity(
        value.calculation.calculationId,
        value.calculation.contentHash,
        value.target.id,
        value.target.hash,
      )
    )
      invalid();
  } else if (value.target.kind === "frozen_bas") {
    const frozen = value.frozenBas;
    if (
      !frozen ||
      frozen.frozenId !== value.target.id ||
      frozen.organisationId !== value.organisation.id ||
      value.target.revision !== 1n ||
      !sameIdentity(frozen.frozenId, frozen.contentHash, value.target.id, value.target.hash)
    )
      invalid();
    if (!frozen.calculation) invalid();
    const frozenCalculation = frozen.calculation;
    if (
      !sameRef(
        frozenCalculation,
        value.calculation.calculationId,
        value.calculation.contentHash,
        value.calculation.version,
      ) ||
      !frozen.calculationSnapshot ||
      frozen.calculationSnapshot.calculationId !== value.calculation.calculationId ||
      frozen.calculationSnapshot.version !== frozenCalculation.revision ||
      !sameIdentity(
        frozen.calculationSnapshot.calculationId,
        frozen.calculationSnapshot.contentHash,
        value.calculation.calculationId,
        value.calculation.contentHash,
      )
    )
      invalid();
    ref(frozen.preparation, "bas_preparation");
    ref(frozen.validation, "bas_validation");
    hash(frozen.approvedSnapshotHash);
    id(frozen.approvalActorUserId);
    id(frozen.freshFactorAssertionId);
    if (!frozen.approvedAt) invalid();
    reportTimestamp(frozen.approvedAt);
    text(frozen.approvalReason, 512, true);
    if (frozen.acknowledgements.length > 200) invalid();
    for (const acknowledgement of frozen.acknowledgements) {
      id(acknowledgement.acknowledgementId);
      ref(acknowledgement.validation, "bas_validation");
      ref(acknowledgement.finding, "bas_validation_finding");
      hash(acknowledgement.sourceSnapshotHash);
      id(acknowledgement.actorUserId);
      if (!acknowledgement.acknowledgedAt) invalid();
      reportTimestamp(acknowledgement.acknowledgedAt);
    }
  } else invalid();
  if (value.preparation) {
    id(value.preparation.preparationId);
    if (
      value.preparation.organisationId !== value.organisation.id ||
      value.preparation.contentHash.length !== 32
    )
      invalid();
    text(value.preparation.creationReason, 512);
    ref(value.preparation.family, "bas_family");
    ref(value.preparation.period, "bas_period");
  }
  if (value.predecessor) {
    id(value.predecessor.preparation.preparationId);
    if (
      value.predecessor.preparation.organisationId !== value.organisation.id ||
      value.predecessor.preparation.contentHash.length !== 32
    )
      invalid();
    text(value.predecessor.preparation.creationReason, 512);
    if (value.predecessor.calculation)
      calculation(value.predecessor.calculation, value.organisation.id);
  }
  return value;
}

export function reportOrganisationFromCalculation(value: BasCalculation): ReportOrganisation {
  const organisation = value.organisationSnapshot;
  if (!organisation) invalid();
  return {
    id: organisation.id,
    version: organisation.version,
    legalName: organisation.legalName,
    displayName: organisation.displayName,
    abn: organisation.abn,
  };
}
