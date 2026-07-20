import assert from "node:assert/strict";
import { test } from "node:test";

import {
  FOUNDATION_EVIDENCE_HEADER,
  REQUIRED_FOUNDATION_REQUIREMENTS,
  validateFoundationEvidence,
} from "./check-foundation-evidence.mjs";

const columns = [
  "source_requirement_id",
  "source_and_version",
  "applicability",
  "design_section",
  "implementation_component",
  "automated_test",
  "retained_evidence",
  "owner",
  "status",
  "dpo_confirmation_reference",
];

const completeFoundationRequirements = [
  "DESIGN-2.3",
  "DESIGN-2.4",
  "DESIGN-5.1",
  "DESIGN-5.2",
  "DESIGN-5.3",
  "DESIGN-6",
  "DESIGN-10",
  "DESIGN-11.1",
  "DESIGN-12.3",
  "DESIGN-13.3",
  "DESIGN-13.5",
  "DESIGN-14",
];

function evidenceRow(sourceRequirementId, overrides = {}) {
  return {
    source_requirement_id: sourceRequirementId,
    source_and_version: "Tammy design 2026-07-19",
    applicability: "Offline desktop foundation",
    design_section: `Section ${sourceRequirementId.replace("DESIGN-", "")}`,
    implementation_component: "foundation/component",
    automated_test: "foundation/component.test",
    retained_evidence: "LOCAL_TEST_COMMAND_PASS",
    owner: "Foundation Engineering Owner",
    status: "IMPLEMENTED_VERIFIED",
    dpo_confirmation_reference: "NOT_REQUIRED_FOUNDATION_ONLY",
    ...overrides,
  };
}

function encodeCell(value) {
  return /[",\n\r]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}

function matrix(rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) => evidenceRow(id))) {
  return `${FOUNDATION_EVIDENCE_HEADER}\n${rows
    .map((row) => columns.map((column) => encodeCell(row[column])).join(","))
    .join("\n")}\n`;
}

test("accepts the canonical fixed-header foundation matrix", () => {
  const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id, index) =>
    evidenceRow(id, {
      retained_evidence:
        index === 0 ? 'LOCAL_TEST_COMMAND_PASS; note "quoted" evidence' : "LOCAL_TEST_COMMAND_PASS",
    }),
  );

  assert.deepEqual(validateFoundationEvidence(matrix(rows)), {
    requirementIds: [...REQUIRED_FOUNDATION_REQUIREMENTS],
    rowCount: REQUIRED_FOUNDATION_REQUIREMENTS.length,
  });
});

test("requires the complete implemented Plan 1 foundation requirement set", () => {
  assert.deepEqual(REQUIRED_FOUNDATION_REQUIREMENTS, completeFoundationRequirements);
});

test("requires the exact committed header", () => {
  const wrongHeader = matrix().replace("source_requirement_id", "requirement_id");
  assert.throws(
    () => validateFoundationEvidence(wrongHeader),
    /FOUNDATION_EVIDENCE_HEADER_INVALID/,
  );
});

test("fails closed on malformed CSV quoting and row shapes", () => {
  const validRow = columns.map((column) => evidenceRow("DESIGN-5.1")[column]).join(",");
  for (const malformed of [
    `${FOUNDATION_EVIDENCE_HEADER}\n${validRow.replace(
      "Offline desktop foundation",
      '"unterminated',
    )}\n`,
    `${FOUNDATION_EVIDENCE_HEADER}\n${validRow.replace(
      "Offline desktop foundation",
      'bad"quote',
    )}\n`,
    `${FOUNDATION_EVIDENCE_HEADER}\n${validRow.replace(
      "Offline desktop foundation",
      '"closed"x',
    )}\n`,
    `${FOUNDATION_EVIDENCE_HEADER}\n${validRow},extra\n`,
    `${FOUNDATION_EVIDENCE_HEADER}\n${validRow}\n\n`,
  ]) {
    assert.throws(
      () => validateFoundationEvidence(malformed),
      /FOUNDATION_EVIDENCE_(?:CSV_MALFORMED|COLUMN_COUNT_INVALID|BLANK_ROW_INVALID)/,
      malformed,
    );
  }
});

test("rejects duplicate and missing required requirement IDs", () => {
  const duplicateRows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) => evidenceRow(id));
  duplicateRows.push(evidenceRow(REQUIRED_FOUNDATION_REQUIREMENTS[0]));
  assert.throws(
    () => validateFoundationEvidence(matrix(duplicateRows)),
    /FOUNDATION_EVIDENCE_REQUIREMENT_DUPLICATE:DESIGN-2\.3/,
  );

  const missingRows = REQUIRED_FOUNDATION_REQUIREMENTS.slice(1).map((id) => evidenceRow(id));
  assert.throws(
    () => validateFoundationEvidence(matrix(missingRows)),
    /FOUNDATION_EVIDENCE_REQUIREMENT_MISSING:DESIGN-2\.3/,
  );
});

test("requires every field including design, test, evidence, owner, and DPO disposition", () => {
  for (const field of columns.slice(1)) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(id, id === "DESIGN-5.1" ? { [field]: "" } : {}),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      new RegExp(`FOUNDATION_EVIDENCE_FIELD_REQUIRED:DESIGN-5\\.1:${field}`),
      field,
    );
  }
});

test("permits only the fixed evidence statuses", () => {
  for (const status of ["VERIFIED", "implemented_verified", "PASSED", "UNKNOWN"]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(id, id === "DESIGN-5.1" ? { status } : {}),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      /FOUNDATION_EVIDENCE_STATUS_INVALID:DESIGN-5\.1/,
      status,
    );
  }
});

test("rejects spreadsheet formula prefixes in evidence fields", () => {
  for (const prefix of ["=", "+", "-", "@"]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(id, id === "DESIGN-5.1" ? { owner: `${prefix}unsafe` } : {}),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      /FOUNDATION_EVIDENCE_FORMULA_INVALID:DESIGN-5\.1:owner/,
      prefix,
    );
  }
});

test("rejects Windows Server smoke presented as Windows 11 evidence", () => {
  const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
    evidenceRow(
      id,
      id === "DESIGN-13.5"
        ? {
            applicability: "Windows 11 23H2 x64 release gate",
            retained_evidence:
              "WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64 PASSED as Windows 11 evidence",
            status: "IMPLEMENTED_VERIFIED",
          }
        : {},
    ),
  );

  assert.throws(
    () => validateFoundationEvidence(matrix(rows)),
    /FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:DESIGN-13\.5/,
  );
});

test("allows explicitly absent Windows Server smoke without satisfying Windows 11", () => {
  const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
    evidenceRow(
      id,
      id === "DESIGN-13.5"
        ? {
            applicability: "Windows 11 23H2 x64 release gate remains required",
            retained_evidence:
              "WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64 NOT_PRODUCED and not Windows 11 evidence",
            status: "NOT_YET_VERIFIED",
          }
        : {},
    ),
  );

  assert.equal(validateFoundationEvidence(matrix(rows)).rowCount, rows.length);
});

test("allows a local packaged pass only when Windows 11 remains explicitly unverified", () => {
  const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
    evidenceRow(
      id,
      id === "DESIGN-13.5"
        ? {
            applicability:
              "Local darwin-arm64 packaged foundation passed; Windows 11 23H2 x64 remains required",
            retained_evidence:
              "LOCAL_DARWIN_ARM64_PACKAGED_E2E PASSED; WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED; WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64 NOT_PRODUCED and NOT WINDOWS 11 EVIDENCE",
            status: "IMPLEMENTED_PARTIAL_TARGET",
          }
        : {},
    ),
  );

  assert.equal(validateFoundationEvidence(matrix(rows)).rowCount, rows.length);

  rows.at(-2).retained_evidence = rows
    .at(-2)
    .retained_evidence.replace("WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED; ", "");
  assert.throws(
    () => validateFoundationEvidence(matrix(rows)),
    /FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:DESIGN-13\.5/,
  );
});
