import { lstat, readFile, realpath } from "node:fs/promises";
import path from "node:path";

export const FOUNDATION_EVIDENCE_HEADER =
  "source_requirement_id,source_and_version,applicability,design_section,implementation_component,automated_test,retained_evidence,owner,status,dpo_confirmation_reference";

export const REQUIRED_FOUNDATION_REQUIREMENTS = Object.freeze([
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
]);

const COLUMNS = Object.freeze(FOUNDATION_EVIDENCE_HEADER.split(","));
const MAX_EVIDENCE_BYTES = 1024 * 1024;
const MAX_EVIDENCE_ROWS = 256;
const PERMITTED_STATUSES = new Set([
  "IMPLEMENTED_VERIFIED",
  "IMPLEMENTED_PARTIAL_TARGET",
  "NOT_YET_VERIFIED",
  "PLANNED",
  "NOT_APPLICABLE",
]);

function evidenceError(code) {
  return new Error(code);
}

function normalizeLineEndings(csvText) {
  if (
    typeof csvText !== "string" ||
    csvText.length === 0 ||
    Buffer.byteLength(csvText, "utf8") > MAX_EVIDENCE_BYTES ||
    csvText.includes("\0") ||
    csvText.startsWith("\uFEFF")
  ) {
    throw evidenceError("FOUNDATION_EVIDENCE_CSV_MALFORMED");
  }
  for (let index = 0; index < csvText.length; index += 1) {
    if (csvText[index] === "\r" && csvText[index + 1] !== "\n") {
      throw evidenceError("FOUNDATION_EVIDENCE_CSV_MALFORMED");
    }
  }
  const normalized = csvText.replaceAll("\r\n", "\n");
  if (!normalized.endsWith("\n")) {
    throw evidenceError("FOUNDATION_EVIDENCE_CSV_MALFORMED");
  }
  return normalized;
}

function parseCsv(csvText) {
  const rows = [];
  let row = [];
  let field = "";
  let inQuotes = false;
  let closedQuote = false;

  const finishField = () => {
    row.push(field);
    field = "";
    closedQuote = false;
  };
  const finishRow = () => {
    finishField();
    rows.push(row);
    if (rows.length > MAX_EVIDENCE_ROWS + 1) {
      throw evidenceError("FOUNDATION_EVIDENCE_CSV_MALFORMED");
    }
    row = [];
  };

  for (let index = 0; index < csvText.length; index += 1) {
    const character = csvText[index];
    if (inQuotes) {
      if (character !== '"') {
        field += character;
        continue;
      }
      if (csvText[index + 1] === '"') {
        field += '"';
        index += 1;
        continue;
      }
      inQuotes = false;
      closedQuote = true;
      continue;
    }
    if (closedQuote) {
      if (character === ",") {
        finishField();
        continue;
      }
      if (character === "\n") {
        finishRow();
        continue;
      }
      throw evidenceError("FOUNDATION_EVIDENCE_CSV_MALFORMED");
    }
    if (character === '"') {
      if (field.length !== 0) throw evidenceError("FOUNDATION_EVIDENCE_CSV_MALFORMED");
      inQuotes = true;
      continue;
    }
    if (character === ",") {
      finishField();
      continue;
    }
    if (character === "\n") {
      finishRow();
      continue;
    }
    field += character;
  }

  if (inQuotes || closedQuote || field.length !== 0 || row.length !== 0) {
    throw evidenceError("FOUNDATION_EVIDENCE_CSV_MALFORMED");
  }
  return rows;
}

function assertWindowsEvidenceClassification(row) {
  const applicability = row.applicability.toUpperCase();
  const evidence = row.retained_evidence.toUpperCase();
  const referencesWindowsServer =
    evidence.includes("WINDOWS_SERVER_SMOKE_ONLY") || evidence.includes("WINDOWS SERVER");
  const referencesWindows11 =
    applicability.includes("WINDOWS 11") ||
    evidence.includes("WINDOWS11") ||
    evidence.includes("WINDOWS 11");
  if (!referencesWindowsServer || !referencesWindows11) return;

  const isExplicitlyAbsent = evidence.includes("NOT_PRODUCED") || evidence.includes("NOT PRODUCED");
  const isExplicitlyNotWindows11Evidence = evidence.includes("NOT WINDOWS 11 EVIDENCE");
  const isUnverifiedWindowsTarget = evidence.includes("WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED");
  const isVerifiedLocalPartialTarget =
    row.status === "IMPLEMENTED_PARTIAL_TARGET" &&
    evidence.includes("LOCAL_DARWIN_ARM64_PACKAGED_E2E PASSED") &&
    isUnverifiedWindowsTarget;
  if (row.status !== "NOT_YET_VERIFIED" && !isVerifiedLocalPartialTarget) {
    throw evidenceError(
      `FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${row.source_requirement_id}`,
    );
  }
  if (!isExplicitlyAbsent || !isExplicitlyNotWindows11Evidence) {
    throw evidenceError(
      `FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${row.source_requirement_id}`,
    );
  }
}

export function validateFoundationEvidence(csvText) {
  const normalized = normalizeLineEndings(csvText);
  const firstNewline = normalized.indexOf("\n");
  if (normalized.slice(0, firstNewline) !== FOUNDATION_EVIDENCE_HEADER) {
    throw evidenceError("FOUNDATION_EVIDENCE_HEADER_INVALID");
  }

  const parsedRows = parseCsv(normalized);
  if (parsedRows.length < 2 || parsedRows[0].join(",") !== FOUNDATION_EVIDENCE_HEADER) {
    throw evidenceError("FOUNDATION_EVIDENCE_HEADER_INVALID");
  }

  const seen = new Set();
  const requirementIds = [];
  for (const values of parsedRows.slice(1)) {
    if (values.length === 1 && values[0] === "") {
      throw evidenceError("FOUNDATION_EVIDENCE_BLANK_ROW_INVALID");
    }
    if (values.length !== COLUMNS.length) {
      throw evidenceError("FOUNDATION_EVIDENCE_COLUMN_COUNT_INVALID");
    }
    const row = Object.fromEntries(COLUMNS.map((column, index) => [column, values[index]]));
    const requirementId = row.source_requirement_id;
    if (!/^DESIGN-\d+(?:\.\d+)?$/.test(requirementId)) {
      throw evidenceError("FOUNDATION_EVIDENCE_REQUIREMENT_ID_INVALID");
    }
    if (seen.has(requirementId)) {
      throw evidenceError(`FOUNDATION_EVIDENCE_REQUIREMENT_DUPLICATE:${requirementId}`);
    }
    seen.add(requirementId);
    requirementIds.push(requirementId);

    for (const column of COLUMNS.slice(1)) {
      if (row[column].length === 0 || row[column] !== row[column].trim()) {
        throw evidenceError(`FOUNDATION_EVIDENCE_FIELD_REQUIRED:${requirementId}:${column}`);
      }
      if (/^[=+\-@]/.test(row[column])) {
        throw evidenceError(`FOUNDATION_EVIDENCE_FORMULA_INVALID:${requirementId}:${column}`);
      }
    }
    if (!PERMITTED_STATUSES.has(row.status)) {
      throw evidenceError(`FOUNDATION_EVIDENCE_STATUS_INVALID:${requirementId}`);
    }
    assertWindowsEvidenceClassification(row);
  }

  for (const requirementId of REQUIRED_FOUNDATION_REQUIREMENTS) {
    if (!seen.has(requirementId)) {
      throw evidenceError(`FOUNDATION_EVIDENCE_REQUIREMENT_MISSING:${requirementId}`);
    }
  }
  return { requirementIds, rowCount: requirementIds.length };
}

async function readCommittedEvidence() {
  const root = await realpath(path.resolve(import.meta.dirname, ".."));
  const evidencePath = path.join(root, "compliance/traceability/foundation.csv");
  const metadata = await lstat(evidencePath).catch(() => null);
  if (
    metadata === null ||
    !metadata.isFile() ||
    metadata.isSymbolicLink() ||
    metadata.size === 0 ||
    metadata.size > MAX_EVIDENCE_BYTES
  ) {
    throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
  }
  const bytes = await readFile(evidencePath);
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
  }
}

async function main() {
  if (process.argv.length !== 2) throw evidenceError("FOUNDATION_EVIDENCE_ARGUMENT_INVALID");
  validateFoundationEvidence(await readCommittedEvidence());
  process.stdout.write("foundation evidence ok\n");
}

if (process.argv[1] && path.resolve(process.argv[1]) === import.meta.filename) {
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : "FOUNDATION_EVIDENCE_INVALID"}\n`,
    );
    process.exitCode = 1;
  });
}
