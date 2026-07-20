import { constants } from "node:fs";
import { lstat, open, realpath } from "node:fs/promises";
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
const EVIDENCE_DIRECTORY_COMPONENTS = Object.freeze(["compliance", "traceability"]);
const EVIDENCE_FILE_NAME = "foundation.csv";
const EVIDENCE_OPEN_FLAGS =
  constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0) | (constants.O_NONBLOCK ?? 0);
const MAX_EVIDENCE_BYTES = 1024 * 1024;
const MAX_EVIDENCE_ROWS = 256;
const PERMITTED_STATUSES = new Set([
  "IMPLEMENTED_VERIFIED",
  "IMPLEMENTED_PARTIAL_TARGET",
  "NOT_YET_VERIFIED",
  "PLANNED",
  "NOT_APPLICABLE",
]);
const TARGET_REQUIREMENTS = new Set(["DESIGN-2.4", "DESIGN-13.5"]);
const LOCAL_PACKAGED_PASS = "LOCAL_DARWIN_ARM64_PACKAGED_E2E PASSED";
const LOCAL_PACKAGED_COMMAND = "LOCAL_DARWIN_ARM64_PACKAGED_E2E_COMMAND pnpm desktop:e2e";
const HOSTED_MACOS_UNVERIFIED = Object.freeze([
  "HOSTED_MACOS_TARGET_STATUS NOT_YET_VERIFIED",
  "macos14-arm64-foundation-failure-evidence NOT_PRODUCED",
]);
const WINDOWS11_UNVERIFIED = Object.freeze([
  "WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED",
  "windows11-x64-foundation-evidence NOT_PRODUCED",
]);
const VERIFIED_TARGET_EVIDENCE = Object.freeze([
  LOCAL_PACKAGED_PASS,
  LOCAL_PACKAGED_COMMAND,
  "HOSTED_MACOS_TARGET_STATUS IMPLEMENTED_VERIFIED",
  "HOSTED_MACOS_PACKAGED_E2E PASSED",
  "WINDOWS11_TARGET_STATUS IMPLEMENTED_VERIFIED",
  "WINDOWS11_PACKAGED_E2E PASSED",
  "windows11-x64-foundation-evidence PRODUCED",
]);
const SERVER_SMOKE_ARTIFACT = "WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64";
const SERVER_SMOKE_DISCLAIMER = "NOT WINDOWS 11 EVIDENCE";
const INITIAL_PROTO_BASELINE = "PROTO_BREAKING_BASELINE_STATUS INITIAL_BASELINE_NOT_YET_ON_MASTER";
const VERIFIED_PROTO_BASELINE = "PROTO_BREAKING_BASELINE_STATUS VERIFIED_AGAINST_MASTER";
const PRODUCT_BOUNDARY_MARKERS = Object.freeze([
  "FOUNDATION_PRODUCT_BOUNDARY",
  "NO_ACTIVITY_STATEMENT_IMPLEMENTATION",
  "NO_CREDENTIAL_IMPLEMENTATION",
  "NO_ATO_TRANSPORT_IMPLEMENTATION",
  "NO_APPROVAL_CLAIM",
]);
const FORBIDDEN_AUDIT_CHARACTER =
  /(?:\p{Cc}|\p{Cf}|\p{Zl}|\p{Zp}|\p{Variation_Selector}|\u034F|\u115F|\u1160|\u17B4|\u17B5|\u3164|\uFFA0)/u;

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

function hasEvidenceMarker(value, marker) {
  return value
    .split(";")
    .map((item) => item.trim())
    .some((item) => item === marker);
}

function containsEveryEvidenceMarker(value, markers) {
  return markers.every((marker) => hasEvidenceMarker(value, marker));
}

function hasExplicitServerSmokeClassification(evidence) {
  return (
    hasEvidenceMarker(
      evidence,
      `${SERVER_SMOKE_ARTIFACT} NOT_PRODUCED and ${SERVER_SMOKE_DISCLAIMER}`,
    ) ||
    hasEvidenceMarker(evidence, `${SERVER_SMOKE_ARTIFACT} PASSED and ${SERVER_SMOKE_DISCLAIMER}`)
  );
}

function assertTargetEvidenceClassification(row) {
  if (!TARGET_REQUIREMENTS.has(row.source_requirement_id)) return;
  const evidence = row.retained_evidence;
  const hasUnverifiedTargets =
    containsEveryEvidenceMarker(evidence, HOSTED_MACOS_UNVERIFIED) &&
    containsEveryEvidenceMarker(evidence, WINDOWS11_UNVERIFIED) &&
    hasExplicitServerSmokeClassification(evidence);
  const valid =
    (row.status === "IMPLEMENTED_PARTIAL_TARGET" &&
      hasEvidenceMarker(evidence, LOCAL_PACKAGED_PASS) &&
      hasEvidenceMarker(evidence, LOCAL_PACKAGED_COMMAND) &&
      hasUnverifiedTargets) ||
    (row.status === "NOT_YET_VERIFIED" && hasUnverifiedTargets) ||
    (row.status === "IMPLEMENTED_VERIFIED" &&
      containsEveryEvidenceMarker(evidence, VERIFIED_TARGET_EVIDENCE) &&
      hasExplicitServerSmokeClassification(evidence));
  if (!valid) {
    throw evidenceError(
      `FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${row.source_requirement_id}`,
    );
  }
}

function assertProtoBaselineClassification(row) {
  if (row.source_requirement_id !== "DESIGN-13.3") return;
  const evidence = row.retained_evidence;
  const valid =
    (row.status === "IMPLEMENTED_PARTIAL_TARGET" &&
      hasEvidenceMarker(evidence, INITIAL_PROTO_BASELINE) &&
      !hasEvidenceMarker(evidence, VERIFIED_PROTO_BASELINE)) ||
    (row.status === "IMPLEMENTED_VERIFIED" &&
      hasEvidenceMarker(evidence, VERIFIED_PROTO_BASELINE) &&
      !hasEvidenceMarker(evidence, INITIAL_PROTO_BASELINE));
  if (!valid) {
    throw evidenceError(
      `FOUNDATION_EVIDENCE_PROTO_BASELINE_MISCLASSIFIED:${row.source_requirement_id}`,
    );
  }
}

function assertProductBoundary(row) {
  if (
    row.source_requirement_id === "DESIGN-14" &&
    !containsEveryEvidenceMarker(row.retained_evidence, PRODUCT_BOUNDARY_MARKERS)
  ) {
    throw evidenceError(
      `FOUNDATION_EVIDENCE_PRODUCT_BOUNDARY_MISSING:${row.source_requirement_id}`,
    );
  }
}

function isStableRegularFile(stats) {
  return (
    stats?.isFile() === true &&
    !stats.isSymbolicLink() &&
    stats.size > 0n &&
    stats.size <= BigInt(MAX_EVIDENCE_BYTES)
  );
}

function isStableDirectory(stats) {
  return stats?.isDirectory() === true && !stats.isSymbolicLink() && stats.nlink > 0n;
}

function hasSameNodeIdentity(left, right) {
  return (
    left.dev === right.dev &&
    left.ino === right.ino &&
    left.mode === right.mode &&
    left.nlink === right.nlink &&
    left.size === right.size &&
    left.mtimeNs === right.mtimeNs &&
    left.ctimeNs === right.ctimeNs
  );
}

function hasSamePathSnapshot(left, right) {
  return (
    left.nodes.length === right.nodes.length &&
    left.nodes.every(
      (node, index) =>
        node.path === right.nodes[index].path &&
        hasSameNodeIdentity(node.stats, right.nodes[index].stats),
    )
  );
}

async function snapshotEvidencePath(root, { lstatPath, realpathPath }) {
  const nodes = [];
  let currentPath = root;
  for (const component of [undefined, ...EVIDENCE_DIRECTORY_COMPONENTS]) {
    if (component !== undefined) currentPath = path.join(currentPath, component);
    const stats = await lstatPath(currentPath, { bigint: true });
    const physicalPath = await realpathPath(currentPath);
    if (!isStableDirectory(stats) || physicalPath !== currentPath) {
      throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
    }
    nodes.push({ path: currentPath, stats });
  }

  const evidencePath = path.join(currentPath, EVIDENCE_FILE_NAME);
  const expectedRelativePath = path.join(...EVIDENCE_DIRECTORY_COMPONENTS, EVIDENCE_FILE_NAME);
  const relativePath = path.relative(root, evidencePath);
  const fileStats = await lstatPath(evidencePath, { bigint: true });
  const physicalEvidencePath = await realpathPath(evidencePath);
  if (
    relativePath !== expectedRelativePath ||
    path.isAbsolute(relativePath) ||
    !isStableRegularFile(fileStats) ||
    physicalEvidencePath !== evidencePath
  ) {
    throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
  }
  nodes.push({ path: evidencePath, stats: fileStats });
  return { evidencePath, fileStats, nodes };
}

async function readBoundedHandle(handle, expectedBytes) {
  const bytes = Buffer.alloc(expectedBytes + 1);
  let offset = 0;
  let reachedEof = false;
  while (offset < bytes.length) {
    const result = await handle.read(bytes, offset, bytes.length - offset, null);
    if (
      result === null ||
      typeof result !== "object" ||
      !Number.isSafeInteger(result.bytesRead) ||
      result.bytesRead < 0 ||
      result.bytesRead > bytes.length - offset
    ) {
      throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
    }
    if (result.bytesRead === 0) {
      reachedEof = true;
      break;
    }
    offset += result.bytesRead;
  }
  if (!reachedEof || offset !== expectedBytes) {
    throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
  }
  return bytes.subarray(0, expectedBytes);
}

export async function readStableEvidenceFile(
  root,
  { lstatPath = lstat, openFile = open, realpathPath = realpath } = {},
) {
  if (typeof root !== "string" || !path.isAbsolute(root) || path.normalize(root) !== root) {
    throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
  }

  let handle;
  let bytes;
  let failed = false;
  try {
    if ((await realpathPath(root)) !== root) {
      throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
    }
    const initialPathSnapshot = await snapshotEvidencePath(root, {
      lstatPath,
      realpathPath,
    });
    handle = await openFile(initialPathSnapshot.evidencePath, EVIDENCE_OPEN_FLAGS);
    const initialHandleStats = await handle.stat({ bigint: true });
    const openedPathSnapshot = await snapshotEvidencePath(root, {
      lstatPath,
      realpathPath,
    });
    if (
      !isStableRegularFile(initialHandleStats) ||
      !hasSamePathSnapshot(initialPathSnapshot, openedPathSnapshot) ||
      !hasSameNodeIdentity(initialHandleStats, initialPathSnapshot.fileStats) ||
      !hasSameNodeIdentity(initialHandleStats, openedPathSnapshot.fileStats)
    ) {
      throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
    }

    bytes = await readBoundedHandle(handle, Number(initialHandleStats.size));

    const finalHandleStats = await handle.stat({ bigint: true });
    const finalPathSnapshot = await snapshotEvidencePath(root, {
      lstatPath,
      realpathPath,
    });
    if (
      !isStableRegularFile(finalHandleStats) ||
      !hasSamePathSnapshot(initialPathSnapshot, finalPathSnapshot) ||
      !hasSameNodeIdentity(initialHandleStats, finalHandleStats) ||
      !hasSameNodeIdentity(initialHandleStats, finalPathSnapshot.fileStats)
    ) {
      throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
    }
  } catch {
    failed = true;
  }
  if (handle !== undefined) {
    try {
      await handle.close();
    } catch {
      failed = true;
    }
  }
  if (failed || bytes === undefined) {
    throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
  }
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw evidenceError("FOUNDATION_EVIDENCE_FILE_INVALID");
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
    for (const column of COLUMNS) {
      if (FORBIDDEN_AUDIT_CHARACTER.test(row[column].replaceAll("\n", ""))) {
        throw evidenceError(`FOUNDATION_EVIDENCE_CONTROL_INVALID:${column}`);
      }
    }
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
    assertTargetEvidenceClassification(row);
    assertProtoBaselineClassification(row);
    assertProductBoundary(row);
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
  return readStableEvidenceFile(root);
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
