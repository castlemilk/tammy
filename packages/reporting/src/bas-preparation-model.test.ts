import assert from "node:assert/strict";
import test from "node:test";
import type {
  BasLabelTrace,
  BasTraceOperand,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import type { BasPreparationReportSnapshot } from "./bas-preparation-model.ts";
import { validateBasPreparationReportSnapshot } from "./bas-preparation-model.ts";

const organisationId = "018f0000-0000-7000-8000-000000000004";
const id = (index: number) => `018f0000-0000-7000-8000-${String(index).padStart(12, "0")}`;
const hash = (value: number) => new Uint8Array(32).fill(value);
const periodStart = { year: 2026, month: 10, day: 1 };
const periodEnd = { year: 2026, month: 10, day: 31 };

function ref(type: string, index: number, contentHash = hash(index)) {
  return { type, id: id(index), revision: 1n, contentHash };
}

function rateTraceSnapshot(): BasPreparationReportSnapshot {
  const preparationRef = ref("bas_preparation", 20);
  const familyRef = ref("bas_family", 21);
  const periodRef = ref("bas_period", 22);
  const profileRef = ref("bas_obligations_profile", 23);
  const workpaperRef = ref("bas_workpaper", 24);
  const calculationId = id(25);
  const calculationHash = hash(25);
  const timestamp = { seconds: 1_790_000_000n, nanos: 0 };
  const labels: BasLabelTrace[] = Array.from({ length: 18 }, (_, index) => ({
    $typeName: "tammy.v1.BasLabelTrace",
    label: index + 1,
    operation: index + 1 === 18 ? 6 : 1,
    state: 2,
    ruleId: `FIXTURE_LABEL_${index + 1}`,
    evidence: [],
    operands: [],
  }));
  labels[10] = {
    $typeName: "tammy.v1.BasLabelTrace",
    label: 11,
    operation: 1,
    state: 5,
    ruleId: "PAYGI_T2_RATE",
    exactRateNumerator: "17",
    exactRateDenominator: "10",
    evidence: [],
    operands: [],
  };
  labels[12] = {
    $typeName: "tammy.v1.BasLabelTrace",
    label: 13,
    operation: 5,
    state: 5,
    ruleId: "PAYGI_T11_RATE",
    exactCentsNumerator: "2091",
    exactCentsDenominator: "10",
    reportedWholeDollars: 2n,
    roundingDifferenceCentsNumerator: "91",
    roundingDifferenceCentsDenominator: "10",
    evidence: [],
    operands: [
      {
        $typeName: "tammy.v1.BasTraceOperand",
        label: 10,
        state: 5,
        exactCentsNumerator: "12345",
        exactCentsDenominator: "1",
        reportedWholeDollars: 123n,
      },
      {
        $typeName: "tammy.v1.BasTraceOperand",
        label: 11,
        state: 5,
        exactCentsNumerator: "",
        exactCentsDenominator: "",
        exactRateNumerator: "17",
        exactRateDenominator: "10",
        reportedWholeDollars: 0n,
      },
    ] satisfies BasTraceOperand[],
  };

  const calculation = {
    calculationId,
    organisationId,
    version: 1n,
    contentHash: calculationHash,
    preparation: preparationRef,
    family: familyRef,
    period: periodRef,
    profile: profileRef,
    centWorkpaper: workpaperRef,
    sourceSnapshotHash: hash(26),
    coverageManifestHash: hash(27),
    revisionVectorHash: hash(28),
    ruleVersion: "fixture-rule-v1",
    ruleHash: hash(29),
    calculatorVersion: "fixture-calculator-v1",
    calculatorHash: hash(30),
    formVersion: "fixture-form-v1",
    formHash: hash(31),
    schemaVersion: "fixture-schema-v1",
    schemaHash: hash(32),
    descriptorVersion: "fixture-descriptor-v1",
    descriptorHash: hash(33),
    createdAt: timestamp,
    inputRevision: 1n,
    inputHash: hash(34),
    result: { outcome: 2, label9Direction: 0, labels },
    coverage: [],
    findings: [],
    periodSnapshot: {},
    profileSnapshot: {},
    paygwInputs: {},
    paygiInputs: {},
    organisationSnapshot: {
      id: organisationId,
      version: 1n,
      legalName: "Fixture Ltd",
      displayName: "Fixture",
      abn: "51824753556",
    },
    organisationIdentity: ref("organisation", 4),
    organisationSnapshotHash: hash(36),
    revisionVector: { financialRevision: 0n },
    workpaperSnapshot: {
      id: id(24),
      organisationId,
      version: 1n,
      periodStart,
      periodEnd,
      status: 1,
      gstBasis: 2,
      createdAt: timestamp,
      sourceFingerprint: hash(35),
      sourcesChanged: false,
      reviewWarnings: [],
      excludedCashPurchaseCount: 0n,
      salesG1: { currencyCode: "AUD", minorUnits: 0n },
      gstOnSales1a: { currencyCode: "AUD", minorUnits: 0n },
      gstCredits1b: { currencyCode: "AUD", minorUnits: 0n },
      netGstPayable: { currencyCode: "AUD", minorUnits: 0n },
      sources: [],
      salesSources: [],
    },
  };
  const frozenId = id(40);
  const frozenHash = hash(40);
  const frozen = {
    frozenId,
    organisationId,
    contentHash: frozenHash,
    preparation: preparationRef,
    family: familyRef,
    period: periodRef,
    profile: profileRef,
    calculation: ref("bas_calculation", 25, calculationHash),
    validation: ref("bas_validation", 41),
    calculationSnapshot: calculation,
    acknowledgements: [],
    approvedSnapshotHash: hash(42),
    approvalActorUserId: id(43),
    freshFactorAssertionId: id(44),
    approvedAt: timestamp,
    approvalReason: "Synthetic frozen report fixture.",
  };

  return {
    kind: "bas_preparation_report_v1",
    target: { kind: "frozen_bas", id: frozenId, revision: 1n, hash: frozenHash },
    organisation: {
      id: organisationId,
      version: 1n,
      legalName: "Fixture Ltd",
      displayName: "Fixture",
      abn: "51824753556",
    },
    acquiredAt: "2026-10-22T00:00:00.000Z",
    calculation,
    frozenBas: frozen,
    preparation: {
      preparationId: preparationRef.id,
      organisationId,
      contentHash: preparationRef.contentHash,
      creationReason: "Synthetic frozen report fixture.",
      family: familyRef,
      period: periodRef,
    },
  } as unknown as BasPreparationReportSnapshot;
}

test("accepts the frozen PAYGI rate operand produced without currency fields", () => {
  assert.doesNotThrow(() => validateBasPreparationReportSnapshot(rateTraceSnapshot()));
});

test("rejects partial, mixed-unit and non-known trace operands", () => {
  const mutations: [number, (operand: Record<string, unknown>) => void][] = [
    [
      1,
      (operand) => {
        delete operand.exactRateDenominator;
      },
    ],
    [
      1,
      (operand) => {
        delete operand.exactRateNumerator;
      },
    ],
    [
      1,
      (operand) => {
        operand.exactCentsNumerator = "0";
        operand.exactCentsDenominator = "1";
      },
    ],
    [
      1,
      (operand) => {
        operand.state = 2;
      },
    ],
    [
      1,
      (operand) => {
        operand.reportedWholeDollars = 1n;
      },
    ],
    [
      0,
      (operand) => {
        delete operand.reportedWholeDollars;
      },
    ],
    [
      0,
      (operand) => {
        operand.exactRateNumerator = "1";
        operand.exactRateDenominator = "1";
      },
    ],
    [
      0,
      (operand) => {
        delete operand.exactCentsDenominator;
      },
    ],
    [
      1,
      (operand) => {
        operand.exactRateNumerator = "0";
      },
    ],
    [
      0,
      (operand) => {
        operand.state = 4;
      },
    ],
  ];

  for (const [index, mutate] of mutations) {
    const value = structuredClone(rateTraceSnapshot());
    const operands = value.calculation.result?.labels.find((trace) => trace.label === 13)?.operands;
    assert.ok(operands);
    mutate(operands[index] as unknown as Record<string, unknown>);
    assert.throws(() => validateBasPreparationReportSnapshot(value), /INVALID_REPORT_SNAPSHOT/);
  }
});

test("accepts known-zero PAYGI rate operands with the source state", () => {
  const value = structuredClone(rateTraceSnapshot());
  const operand = value.calculation.result?.labels.find((trace) => trace.label === 13)?.operands[1];
  assert.ok(operand);
  operand.exactRateNumerator = "0";
  operand.exactRateDenominator = "1";
  operand.state = 4;
  const t2 = value.calculation.result?.labels.find((trace) => trace.label === 11);
  assert.ok(t2);
  t2.state = 4;
  t2.exactRateNumerator = "0";
  t2.exactRateDenominator = "1";
  assert.doesNotThrow(() => validateBasPreparationReportSnapshot(value));
});

test("rejects T2 traces with money fields or a rate inconsistent with the known state", () => {
  const mutations: ((trace: BasLabelTrace) => void)[] = [
    (trace) => {
      trace.exactCentsNumerator = "0";
      trace.exactCentsDenominator = "1";
    },
    (trace) => {
      trace.reportedWholeDollars = 0n;
    },
    (trace) => {
      trace.roundingDifferenceCentsNumerator = "0";
      trace.roundingDifferenceCentsDenominator = "1";
    },
    (trace) => {
      trace.state = 4;
    },
    (trace) => {
      trace.exactRateNumerator = "0";
      trace.exactRateDenominator = "1";
    },
  ];
  for (const mutate of mutations) {
    const value = structuredClone(rateTraceSnapshot());
    const trace = value.calculation.result?.labels.find((item) => item.label === 11);
    assert.ok(trace);
    mutate(trace);
    assert.throws(() => validateBasPreparationReportSnapshot(value), /INVALID_REPORT_SNAPSHOT/);
  }
});

test("accepts the 79-digit rational boundary defined by the protobuf pattern", () => {
  const value = structuredClone(rateTraceSnapshot());
  const numerator = "9".repeat(79);
  const denominator = "8".repeat(79);
  const operand = value.calculation.result?.labels.find((trace) => trace.label === 13)?.operands[1];
  const t2 = value.calculation.result?.labels.find((trace) => trace.label === 11);
  assert.ok(operand);
  assert.ok(t2);
  operand.exactRateNumerator = numerator;
  operand.exactRateDenominator = denominator;
  t2.exactRateNumerator = numerator;
  t2.exactRateDenominator = denominator;
  assert.doesNotThrow(() => validateBasPreparationReportSnapshot(value));
});

test("keeps exact rationals and reported dollars within their protobuf bounds", () => {
  const invalidValues = [
    (value: BasPreparationReportSnapshot) => {
      const trace = value.calculation.result?.labels.find((item) => item.label === 13);
      assert.ok(trace);
      const rate = trace.operands[1];
      assert.ok(rate);
      rate.exactRateNumerator = "1".repeat(80);
    },
    (value: BasPreparationReportSnapshot) => {
      const trace = value.calculation.result?.labels.find((item) => item.label === 13);
      assert.ok(trace);
      const money = trace.operands[0];
      assert.ok(money);
      money.state = 4;
      money.exactCentsNumerator = "0";
      money.exactCentsDenominator = "1";
      money.reportedWholeDollars = 1n;
    },
    (value: BasPreparationReportSnapshot) => {
      const trace = value.calculation.result?.labels.find((item) => item.label === 13);
      assert.ok(trace);
      const rate = trace.operands[1];
      assert.ok(rate);
      rate.exactRateDenominator = "1".repeat(80);
    },
    (value: BasPreparationReportSnapshot) => {
      const trace = value.calculation.result?.labels.find((item) => item.label === 13);
      assert.ok(trace);
      const money = trace.operands[0];
      assert.ok(money);
      money.reportedWholeDollars = 9_223_372_036_854_775_808n;
    },
    (value: BasPreparationReportSnapshot) => {
      const trace = value.calculation.result?.labels.find((item) => item.label === 13);
      assert.ok(trace);
      trace.roundingDifferenceCentsNumerator = "-0";
    },
  ];

  for (const mutate of invalidValues) {
    const value = structuredClone(rateTraceSnapshot());
    mutate(value);
    assert.throws(() => validateBasPreparationReportSnapshot(value), /INVALID_REPORT_SNAPSHOT/);
  }
});
