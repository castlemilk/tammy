import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import {
  appendFile,
  lstat,
  mkdir,
  mkdtemp,
  open,
  readFile,
  realpath,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { promisify } from "node:util";

import YAML from "yaml";

import * as foundationEvidence from "./check-foundation-evidence.mjs";
import {
  FOUNDATION_EVIDENCE_HEADER,
  REQUIRED_FOUNDATION_REQUIREMENTS,
  validateFoundationEvidence,
} from "./check-foundation-evidence.mjs";

const execFileAsync = promisify(execFile);

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

const currentTargetEvidence =
  "LOCAL_DARWIN_ARM64_PACKAGED_E2E_STATUS PASSED; LOCAL_DARWIN_ARM64_PACKAGED_E2E_COMMAND pnpm desktop:e2e; HOSTED_MACOS_TARGET_STATUS NOT_YET_VERIFIED; HOSTED_MACOS_PACKAGED_E2E_STATUS NOT_PRODUCED; HOSTED_MACOS_FAILURE_ARTIFACT_NAME macos14-arm64-foundation-failure-evidence; HOSTED_MACOS_FAILURE_ARTIFACT_STATUS NOT_PRODUCED; WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED; WINDOWS11_PACKAGED_E2E_STATUS NOT_PRODUCED; WINDOWS11_FOUNDATION_ARTIFACT_NAME windows11-x64-foundation-evidence; WINDOWS11_FOUNDATION_EVIDENCE_STATUS NOT_PRODUCED; WINDOWS_SERVER_SMOKE_ARTIFACT_NAME WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64; WINDOWS_SERVER_SMOKE_STATUS NOT_PRODUCED; WINDOWS_SERVER_SMOKE_CLASSIFICATION NOT_WINDOWS_11_EVIDENCE";

const verifiedTargetEvidence =
  "LOCAL_DARWIN_ARM64_PACKAGED_E2E_STATUS PASSED; LOCAL_DARWIN_ARM64_PACKAGED_E2E_COMMAND pnpm desktop:e2e; HOSTED_MACOS_TARGET_STATUS IMPLEMENTED_VERIFIED; HOSTED_MACOS_PACKAGED_E2E_STATUS PASSED; HOSTED_MACOS_FAILURE_ARTIFACT_NAME macos14-arm64-foundation-failure-evidence; HOSTED_MACOS_FAILURE_ARTIFACT_STATUS NOT_PRODUCED; WINDOWS11_TARGET_STATUS IMPLEMENTED_VERIFIED; WINDOWS11_PACKAGED_E2E_STATUS PASSED; WINDOWS11_FOUNDATION_ARTIFACT_NAME windows11-x64-foundation-evidence; WINDOWS11_FOUNDATION_EVIDENCE_STATUS PRODUCED; WINDOWS_SERVER_SMOKE_ARTIFACT_NAME WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64; WINDOWS_SERVER_SMOKE_STATUS NOT_PRODUCED; WINDOWS_SERVER_SMOKE_CLASSIFICATION NOT_WINDOWS_11_EVIDENCE";

const notYetVerifiedTargetEvidence =
  "LOCAL_DARWIN_ARM64_PACKAGED_E2E_STATUS NOT_YET_VERIFIED; HOSTED_MACOS_TARGET_STATUS NOT_YET_VERIFIED; HOSTED_MACOS_PACKAGED_E2E_STATUS NOT_PRODUCED; HOSTED_MACOS_FAILURE_ARTIFACT_NAME macos14-arm64-foundation-failure-evidence; HOSTED_MACOS_FAILURE_ARTIFACT_STATUS NOT_PRODUCED; WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED; WINDOWS11_PACKAGED_E2E_STATUS NOT_PRODUCED; WINDOWS11_FOUNDATION_ARTIFACT_NAME windows11-x64-foundation-evidence; WINDOWS11_FOUNDATION_EVIDENCE_STATUS NOT_PRODUCED; WINDOWS_SERVER_SMOKE_ARTIFACT_NAME WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64; WINDOWS_SERVER_SMOKE_STATUS NOT_PRODUCED; WINDOWS_SERVER_SMOKE_CLASSIFICATION NOT_WINDOWS_11_EVIDENCE";

const productBoundaryEvidence =
  "FOUNDATION_PRODUCT_BOUNDARY_STATUS FOUNDATION_ONLY; ACTIVITY_STATEMENT_IMPLEMENTATION_STATUS NOT_IMPLEMENTED; MACHINE_CREDENTIAL_IMPLEMENTATION_STATUS NOT_IMPLEMENTED; ATO_TRANSPORT_IMPLEMENTATION_STATUS NOT_IMPLEMENTED; SBR_APPROVAL_STATUS NOT_CLAIMED; LOCAL_TEST_COMMAND node --test scripts/check-foundation-evidence.test.mjs; COMPLIANCE_CHECK_COMMAND pnpm compliance:foundation; DPO_OSF_EVTE_CONFORMANCE_PVT_WHITELISTING_STATUS NOT_PRODUCED";

const CHECKOUT_ACTION = "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd";
const NODE_ACTION = "actions/setup-node@a0853c24544627f65ddf259abe73b1d18a591444";
const GO_ACTION = "actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16";
const TASK_INSTALL = "go install github.com/go-task/task/v3/cmd/task@v3.52.0";
const CONCURRENCY_GROUP = "foundation-$" + "{{ github.workflow }}-$" + "{{ github.ref }}";
const SUBJECT_REVISION = "$" + "{{ github.event.pull_request.head.sha || github.sha }}";
const WINDOWS_LLVM_AR = "$" + "{{ vars.TAMMY_WINDOWS_LLVM_AR }}";
const WINDOWS_NMAKE = "$" + "{{ vars.TAMMY_WINDOWS_NMAKE }}";
const WINDOWS_ARTIFACT_CONDITION = "$" + "{{ always() && steps.checkout.outcome == 'success' }}";

function requiredStep(job, name) {
  const matches = job.steps.filter((step) => step.name === name);
  assert.equal(matches.length, 1, `${job.name}: ${name} must occur exactly once`);
  return matches[0];
}

function assertSharedCiProvisioning(job, shell) {
  const checkout = requiredStep(job, "Check out the complete comparison history");
  assert.equal(checkout.uses, CHECKOUT_ACTION, `${job.name}: checkout pin`);
  assert.deepEqual(checkout.with, {
    clean: true,
    "fetch-depth": 0,
    "persist-credentials": false,
  });
  const node = requiredStep(job, "Set up pinned Node.js");
  assert.equal(node.uses, NODE_ACTION, `${job.name}: Node action pin`);
  assert.deepEqual(node.with, {
    "node-version": "24.18.0",
    "package-manager-cache": false,
  });
  const go = requiredStep(job, "Set up pinned Go");
  assert.equal(go.uses, GO_ACTION, `${job.name}: Go action pin`);
  assert.equal(go.with["go-version"], "1.26.4", `${job.name}: Go version`);
  assert.equal(
    requiredStep(job, "Activate pinned pnpm").run,
    "corepack enable pnpm\ncorepack prepare pnpm@11.15.0 --activate\n",
  );
  assert.equal(
    requiredStep(job, "Install frozen dependencies").run,
    "pnpm install --frozen-lockfile",
  );
  const taskInstall = requiredStep(job, "Install exact Task");
  assert.equal(taskInstall.shell, shell, `${job.name}: Task install shell`);
  assert.match(taskInstall.run, new RegExp(TASK_INSTALL.replaceAll(/[.*+?^${}()|[\]\\]/g, "\\$&")));
  const taskVersion = requiredStep(job, "Verify exact Task version");
  assert.equal(taskVersion.shell, shell, `${job.name}: Task version shell`);
  if (shell === "bash") {
    assert.match(taskInstall.run, /echo "\$\(go env GOPATH\)\/bin" >> "\$GITHUB_PATH"/);
    assert.equal(taskVersion.run, 'test "$(task --version)" = "3.52.0"');
    return;
  }
  assert.match(taskInstall.run, /Join-Path \(go env GOPATH\) "bin"[\s\S]*\$env:GITHUB_PATH/);
  assert.match(
    taskVersion.run,
    /\$version = task --version[\s\S]*\$LASTEXITCODE -ne 0 -or \$version -ne "3\.52\.0"[\s\S]*UNSUPPORTED_TASK_VERSION:\$version/,
  );
}

test("retains runner provisioning, evidence classification, and artifacts around canonical CI tasks", async () => {
  const foundationWorkflow = await readFile(
    new URL("../.github/workflows/foundation-ci.yml", import.meta.url),
    "utf8",
  );
  const windows11Workflow = await readFile(
    new URL("../.github/workflows/foundation-windows11-e2e.yml", import.meta.url),
    "utf8",
  );
  for (const workflow of [foundationWorkflow, windows11Workflow]) {
    assert.match(workflow, /permissions:\n {2}contents: read/);
    assert.match(
      workflow,
      /concurrency:\n {2}group: foundation-\$\{\{ github\.workflow \}\}-\$\{\{ github\.ref \}\}\n {2}cancel-in-progress: true/,
    );
    assert.match(
      workflow,
      /actions\/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd[\s\S]*clean: true[\s\S]*fetch-depth: 0[\s\S]*persist-credentials: false/,
    );
    assert.match(
      workflow,
      /actions\/setup-node@a0853c24544627f65ddf259abe73b1d18a591444[\s\S]*node-version: 24\.18\.0/,
    );
    assert.match(
      workflow,
      /actions\/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16[\s\S]*go-version: 1\.26\.4/,
    );
    assert.match(workflow, /corepack enable pnpm\n {10}corepack prepare pnpm@11\.15\.0 --activate/);
    assert.match(workflow, /pnpm install --frozen-lockfile/);
  }
  assert.match(foundationWorkflow, /runs-on: ubuntu-24\.04[\s\S]*timeout-minutes: 30/);
  assert.match(foundationWorkflow, /runs-on: macos-14[\s\S]*timeout-minutes: 30/);
  assert.match(foundationWorkflow, /runs-on: windows-2025[\s\S]*timeout-minutes: 30/);
  assert.match(foundationWorkflow, /name: Contracts and local foundation/);
  assert.match(foundationWorkflow, /name: macOS 14 arm64 packaged E2E/);
  assert.match(
    foundationWorkflow,
    /name: WINDOWS_SERVER_SMOKE_ONLY \/ Windows Server 2025 desktop checks/,
  );
  assert.match(foundationWorkflow, /Assert native Apple silicon[\s\S]*uname -m.*arm64/);
  assert.match(foundationWorkflow, /Verify virtual display availability[\s\S]*command -v xvfb-run/);
  assert.match(
    foundationWorkflow,
    /EVIDENCE_CLASSIFICATION: WINDOWS_SERVER_SMOKE_ONLY[\s\S]*WINDOWS_SERVER_SMOKE_ONLY_REQUIRES_AMD64[\s\S]*WINDOWS_SERVER_EVIDENCE_CLASSIFICATION_INVALID/,
  );
  assert.equal(
    (foundationWorkflow.match(/name: Verify exact Task version/g) ?? []).length,
    3,
    "every foundation workflow job verifies the installed Task version",
  );
  assert.match(
    foundationWorkflow,
    /go install github\.com\/go-task\/task\/v3\/cmd\/task@v3\.52\.0[\s\S]*echo "\$\(go env GOPATH\)\/bin" >> "\$GITHUB_PATH"[\s\S]*name: Verify exact Task version[\s\S]*test "\$\(task --version\)" = "3\.52\.0"/,
  );
  assert.match(
    foundationWorkflow,
    /go install github\.com\/go-task\/task\/v3\/cmd\/task@v3\.52\.0[\s\S]*Join-Path \(go env GOPATH\) "bin"[\s\S]*\$env:GITHUB_PATH[\s\S]*name: Verify exact Task version[\s\S]*\$version = task --version[\s\S]*\$LASTEXITCODE -ne 0 -or \$version -ne "3\.52\.0"[\s\S]*UNSUPPORTED_TASK_VERSION:\$version/,
  );
  assert.match(
    foundationWorkflow,
    /Run canonical Ubuntu CI scenarios[\s\S]*task ci:contracts[\s\S]*task ci:linux/,
  );
  assert.match(foundationWorkflow, /Run canonical macOS CI scenario[\s\S]*task ci:macos/);
  assert.match(
    foundationWorkflow,
    /Run canonical Windows Server smoke CI scenario[\s\S]*task ci:windows-smoke/,
  );
  assert.match(
    foundationWorkflow,
    /Retain packaged failure evidence[\s\S]*if: failure\(\)[\s\S]*macos14-arm64-foundation-failure-evidence[\s\S]*apps\/desktop\/test-results\/\*\*[\s\S]*apps\/desktop\/resources\/build\/build-manifest\.json[\s\S]*if-no-files-found: warn[\s\S]*retention-days: 30/,
  );
  assert.match(
    windows11Workflow,
    /runs-on: \[self-hosted, windows, x64, tammy-win11-23h2\][\s\S]*timeout-minutes: 60/,
  );
  assert.match(windows11Workflow, /name: Windows 11 23H2 x64 packaged E2E/);
  assert.match(
    windows11Workflow,
    /WINDOWS11_23H2_BUILD_REQUIRED[\s\S]*WINDOWS11_X64_REQUIRED[\s\S]*WINDOWS11_AMD64_PROCESS_REQUIRED/,
  );
  for (const exportName of ["TAMMY_SQLCIPHER_COMSPEC", "INCLUDE", "LIB"]) {
    assert.match(windows11Workflow, new RegExp(`"${exportName}=`));
  }
  assert.match(
    windows11Workflow,
    /TAMMY_SQLCIPHER_AR: \$\{\{ vars\.TAMMY_WINDOWS_LLVM_AR \}\}[\s\S]*TAMMY_SQLCIPHER_NMAKE: \$\{\{ vars\.TAMMY_WINDOWS_NMAKE \}\}[\s\S]*Verify the exact native SQLCipher tool paths[\s\S]*"LIB=\$lib"[\s\S]*Run canonical Windows 11 CI scenario[\s\S]*task ci:windows11/,
  );
  assert.equal(
    (windows11Workflow.match(/name: Verify exact Task version/g) ?? []).length,
    1,
    "the Windows 11 workflow verifies the installed Task version",
  );
  assert.match(
    windows11Workflow,
    /Upload Windows 11 manifest and results[\s\S]*if: \$\{\{ always\(\) && steps\.checkout\.outcome == 'success' \}\}[\s\S]*windows11-x64-foundation-evidence[\s\S]*apps\/desktop\/resources\/build\/build-manifest\.json[\s\S]*apps\/desktop\/test-results\/\*\*[\s\S]*if-no-files-found: error[\s\S]*retention-days: 30/,
  );
});

test("pins provisioning and evidence ownership independently for every CI job", async () => {
  const foundation = YAML.parse(
    await readFile(new URL("../.github/workflows/foundation-ci.yml", import.meta.url), "utf8"),
  );
  const windows11 = YAML.parse(
    await readFile(
      new URL("../.github/workflows/foundation-windows11-e2e.yml", import.meta.url),
      "utf8",
    ),
  );
  for (const workflow of [foundation, windows11]) {
    assert.deepEqual(workflow.permissions, { contents: "read" });
    assert.deepEqual(workflow.concurrency, {
      group: CONCURRENCY_GROUP,
      "cancel-in-progress": true,
    });
  }

  const contracts = foundation.jobs.contracts;
  assert.equal(contracts.name, "Contracts and local foundation");
  assert.equal(contracts["runs-on"], "ubuntu-24.04");
  assert.equal(contracts["timeout-minutes"], 30);
  assertSharedCiProvisioning(contracts, "bash");
  assert.equal(
    requiredStep(contracts, "Verify virtual display availability").run,
    "command -v xvfb-run",
  );
  const ubuntuTask = requiredStep(contracts, "Run canonical Ubuntu CI scenarios");
  assert.equal(ubuntuTask.env.TAMMY_EVIDENCE_SUBJECT_REVISION, SUBJECT_REVISION);
  assert.equal(ubuntuTask.run, "task ci:contracts\ntask ci:linux\n");

  const macos = foundation.jobs["macos14-arm64-packaged"];
  assert.equal(macos.name, "macOS 14 arm64 packaged E2E");
  assert.equal(macos["runs-on"], "macos-14");
  assert.equal(macos["timeout-minutes"], 30);
  assertSharedCiProvisioning(macos, "bash");
  assert.equal(
    requiredStep(macos, "Assert native Apple silicon").run,
    'test "$(uname -m)" = "arm64"',
  );
  assert.match(
    requiredStep(macos, "Prepare isolated test keychain").run,
    /security create-keychain[\s\S]*security default-keychain[\s\S]*security list-keychains -d user -s "\$keychain"[\s\S]*security unlock-keychain[\s\S]*security set-keychain-settings/,
  );
  assert.equal(requiredStep(macos, "Run canonical macOS CI scenario").run, "task ci:macos");
  const macosArtifact = requiredStep(macos, "Retain packaged failure evidence");
  assert.equal(macosArtifact.if, "failure()");
  assert.equal(
    macosArtifact.uses,
    "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a",
  );
  assert.equal(macosArtifact.with.name, "macos14-arm64-foundation-failure-evidence");
  assert.match(
    macosArtifact.with.path,
    /apps\/desktop\/test-results\/\*\*[\s\S]*apps\/desktop\/resources\/build\/build-manifest\.json/,
  );
  assert.equal(macosArtifact.with["if-no-files-found"], "warn");
  assert.equal(macosArtifact.with["retention-days"], 30);

  const smoke = foundation.jobs["windows-server-x64-package-smoke"];
  assert.equal(smoke.name, "WINDOWS_SERVER_SMOKE_ONLY / Windows Server 2025 desktop checks");
  assert.equal(smoke["runs-on"], "windows-2025");
  assert.equal(smoke["timeout-minutes"], 30);
  assert.equal(smoke.env.EVIDENCE_CLASSIFICATION, "WINDOWS_SERVER_SMOKE_ONLY");
  assertSharedCiProvisioning(smoke, "pwsh");
  assert.match(
    requiredStep(smoke, "Assert the Windows Server x64 smoke target").run,
    /WINDOWS_SERVER_SMOKE_ONLY_REQUIRES_AMD64[\s\S]*WINDOWS_SERVER_EVIDENCE_CLASSIFICATION_INVALID/,
  );
  const smokeTask = requiredStep(smoke, "Run canonical Windows Server smoke CI scenario");
  assert.equal(smokeTask.env.TAMMY_EVIDENCE_SUBJECT_REVISION, SUBJECT_REVISION);
  assert.equal(smokeTask.run, "task ci:windows-smoke");

  const releaseGate = windows11.jobs["windows11-23h2-x64-packaged-e2e"];
  assert.equal(releaseGate.name, "Windows 11 23H2 x64 packaged E2E");
  assert.deepEqual(releaseGate["runs-on"], ["self-hosted", "windows", "x64", "tammy-win11-23h2"]);
  assert.equal(releaseGate["timeout-minutes"], 60);
  assertSharedCiProvisioning(releaseGate, "pwsh");
  assert.match(
    requiredStep(releaseGate, "Verify Windows 11 23H2 or later x64").run,
    /WINDOWS11_23H2_BUILD_REQUIRED[\s\S]*WINDOWS11_X64_REQUIRED[\s\S]*WINDOWS11_AMD64_PROCESS_REQUIRED/,
  );
  const nativeTools = requiredStep(releaseGate, "Verify the exact native SQLCipher tool paths");
  assert.match(
    nativeTools.run,
    /TAMMY_SQLCIPHER_COMSPEC=\$comspec[\s\S]*INCLUDE=\$include[\s\S]*LIB=\$lib/,
  );
  assert.equal(releaseGate.env.TAMMY_SQLCIPHER_AR, WINDOWS_LLVM_AR);
  assert.equal(releaseGate.env.TAMMY_SQLCIPHER_NMAKE, WINDOWS_NMAKE);
  assert.equal(
    requiredStep(releaseGate, "Run canonical Windows 11 CI scenario").run,
    "task ci:windows11",
  );
  const windowsArtifact = requiredStep(releaseGate, "Upload Windows 11 manifest and results");
  assert.equal(windowsArtifact.if, WINDOWS_ARTIFACT_CONDITION);
  assert.equal(
    windowsArtifact.uses,
    "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a",
  );
  assert.equal(windowsArtifact.with.name, "windows11-x64-foundation-evidence");
  assert.match(
    windowsArtifact.with.path,
    /apps\/desktop\/resources\/build\/build-manifest\.json[\s\S]*apps\/desktop\/test-results\/\*\*/,
  );
  assert.equal(windowsArtifact.with["if-no-files-found"], "error");
  assert.equal(windowsArtifact.with["retention-days"], 30);
});

function evidenceRow(sourceRequirementId, overrides = {}) {
  const row = {
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
  };
  if (sourceRequirementId === "DESIGN-2.4" || sourceRequirementId === "DESIGN-13.5") {
    row.retained_evidence = currentTargetEvidence;
    row.status = "IMPLEMENTED_PARTIAL_TARGET";
  }
  if (sourceRequirementId === "DESIGN-13.3") {
    row.retained_evidence =
      "PROTO_BREAKING_BASELINE_STATUS INITIAL_BASELINE_NOT_YET_ON_MASTER; LOCAL_CONTRACT_TESTS PASSED";
    row.status = "IMPLEMENTED_PARTIAL_TARGET";
  }
  if (sourceRequirementId === "DESIGN-14") {
    row.retained_evidence = productBoundaryEvidence;
    row.status = "IMPLEMENTED_PARTIAL_TARGET";
  }
  return { ...row, ...overrides };
}

function encodeCell(value) {
  return /[",\n\r]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}

function matrix(rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) => evidenceRow(id))) {
  return `${FOUNDATION_EVIDENCE_HEADER}\n${rows
    .map((row) => columns.map((column) => encodeCell(row[column])).join(","))
    .join("\n")}\n`;
}

async function withTemporaryEvidence(run) {
  const root = await realpath(await mkdtemp(path.join(tmpdir(), "tammy-foundation-evidence-")));
  try {
    const evidencePath = path.join(root, "compliance/traceability/foundation.csv");
    await mkdir(path.dirname(evidencePath), { recursive: true });
    await writeFile(evidencePath, matrix(), { encoding: "utf8", mode: 0o600 });
    await run(root, evidencePath);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
}

test("accepts the canonical fixed-header foundation matrix", () => {
  const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id, index) =>
    index === 0
      ? evidenceRow(id, {
          retained_evidence: 'LOCAL_TEST_COMMAND_PASS; note "quoted" evidence',
        })
      : evidenceRow(id),
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

test("rejects control, bidi, and invisible audit-spoofing characters in every column", () => {
  for (const [name, character] of [
    ["C0", "\u0001"],
    ["ANSI escape", "\u001B"],
    ["C1", "\u0085"],
    ["soft hyphen", "\u00AD"],
    ["grapheme joiner", "\u034F"],
    ["zero-width space", "\u200B"],
    ["bidi override", "\u202E"],
    ["bidi isolate", "\u2066"],
    ["variation selector", "\uFE0F"],
  ]) {
    for (const column of columns) {
      const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
        evidenceRow(
          id,
          id === "DESIGN-5.1"
            ? {
                [column]:
                  column === "source_requirement_id"
                    ? `DESIGN-5.1${character}`
                    : `safe${character}value`,
              }
            : {},
        ),
      );
      assert.throws(
        () => validateFoundationEvidence(matrix(rows)),
        new RegExp(`FOUNDATION_EVIDENCE_CONTROL_INVALID:${column}`),
        `${name}:${column}`,
      );
    }
  }
});

test("allows a quoted newline without allowing other control characters", () => {
  const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
    evidenceRow(
      id,
      id === "DESIGN-5.1"
        ? { retained_evidence: "first retained evidence line\nsecond retained evidence line" }
        : {},
    ),
  );
  assert.equal(validateFoundationEvidence(matrix(rows)).rowCount, rows.length);
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

test("keys release-target classification to DESIGN-2.4 and DESIGN-13.5 semantics", () => {
  for (const requirementId of ["DESIGN-2.4", "DESIGN-13.5"]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(
        id,
        id === requirementId
          ? {
              applicability: "All desktop release targets",
              retained_evidence: "WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64 PASSED",
              status: "IMPLEMENTED_VERIFIED",
            }
          : {},
      ),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      new RegExp(`FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${requirementId}`),
      requirementId,
    );
  }
});

test("rejects target evidence that omits any required unverified platform marker", () => {
  for (const requirementId of ["DESIGN-2.4", "DESIGN-13.5"]) {
    for (const marker of [
      "LOCAL_DARWIN_ARM64_PACKAGED_E2E_STATUS PASSED; ",
      "HOSTED_MACOS_TARGET_STATUS NOT_YET_VERIFIED; ",
      "HOSTED_MACOS_PACKAGED_E2E_STATUS NOT_PRODUCED; ",
      "WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED; ",
      "WINDOWS11_PACKAGED_E2E_STATUS NOT_PRODUCED; ",
      "WINDOWS11_FOUNDATION_EVIDENCE_STATUS NOT_PRODUCED; ",
      "WINDOWS_SERVER_SMOKE_STATUS NOT_PRODUCED; ",
      "; WINDOWS_SERVER_SMOKE_CLASSIFICATION NOT_WINDOWS_11_EVIDENCE",
    ]) {
      const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
        evidenceRow(
          id,
          id === requirementId
            ? { retained_evidence: currentTargetEvidence.replace(marker, "") }
            : {},
        ),
      );
      assert.throws(
        () => validateFoundationEvidence(matrix(rows)),
        new RegExp(`FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${requirementId}`),
        `${requirementId}:${marker}`,
      );
    }
  }
});

test("does not let server smoke substitute for independent verified target evidence", () => {
  for (const requirementId of ["DESIGN-2.4", "DESIGN-13.5"]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(
        id,
        id === requirementId
          ? {
              retained_evidence:
                "LOCAL_DARWIN_ARM64_PACKAGED_E2E PASSED; HOSTED_MACOS_PACKAGED_E2E PASSED; WINDOWS11_TARGET_STATUS IMPLEMENTED_VERIFIED; WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64 PASSED and NOT WINDOWS 11 EVIDENCE",
              status: "IMPLEMENTED_VERIFIED",
            }
          : {},
      ),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      new RegExp(`FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${requirementId}`),
      requirementId,
    );
  }
});

test("allows explicitly absent Windows Server smoke without satisfying Windows 11", () => {
  const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
    evidenceRow(
      id,
      id === "DESIGN-13.5"
        ? {
            applicability: "Windows 11 23H2 x64 release gate remains required",
            retained_evidence: notYetVerifiedTargetEvidence,
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
            retained_evidence: currentTargetEvidence,
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

test("accepts one internally consistent target state for each implemented status", () => {
  for (const [status, retainedEvidence] of [
    ["IMPLEMENTED_PARTIAL_TARGET", currentTargetEvidence],
    ["IMPLEMENTED_VERIFIED", verifiedTargetEvidence],
    ["NOT_YET_VERIFIED", notYetVerifiedTargetEvidence],
  ]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(
        id,
        id === "DESIGN-2.4" || id === "DESIGN-13.5"
          ? { retained_evidence: retainedEvidence, status }
          : {},
      ),
    );
    assert.equal(validateFoundationEvidence(matrix(rows)).rowCount, rows.length, status);
  }
});

test("requires the three exact CI artifact identifiers exactly once", () => {
  for (const requirementId of ["DESIGN-2.4", "DESIGN-13.5"]) {
    for (const artifactToken of [
      "HOSTED_MACOS_FAILURE_ARTIFACT_NAME macos14-arm64-foundation-failure-evidence",
      "WINDOWS11_FOUNDATION_ARTIFACT_NAME windows11-x64-foundation-evidence",
      "WINDOWS_SERVER_SMOKE_ARTIFACT_NAME WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64",
    ]) {
      for (const retainedEvidence of [
        currentTargetEvidence
          .split(";")
          .filter((token) => token.trim() !== artifactToken)
          .join(";"),
        currentTargetEvidence.replace(artifactToken, `${artifactToken}-wrong`),
        `${currentTargetEvidence}; ${artifactToken}`,
      ]) {
        const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
          evidenceRow(id, id === requirementId ? { retained_evidence: retainedEvidence } : {}),
        );
        assert.throws(
          () => validateFoundationEvidence(matrix(rows)),
          new RegExp(`FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${requirementId}`),
          `${requirementId}:${artifactToken}`,
        );
      }
    }
  }
});

test("rejects contradictory values for every target evidence key", () => {
  for (const requirementId of ["DESIGN-2.4", "DESIGN-13.5"]) {
    for (const contradiction of [
      "LOCAL_DARWIN_ARM64_PACKAGED_E2E_STATUS NOT_YET_VERIFIED",
      "HOSTED_MACOS_TARGET_STATUS IMPLEMENTED_VERIFIED",
      "HOSTED_MACOS_PACKAGED_E2E_STATUS PASSED",
      "WINDOWS11_TARGET_STATUS IMPLEMENTED_VERIFIED",
      "WINDOWS11_PACKAGED_E2E_STATUS PASSED",
      "WINDOWS11_FOUNDATION_EVIDENCE_STATUS PRODUCED",
      "WINDOWS_SERVER_SMOKE_STATUS PASSED",
      "WINDOWS_SERVER_SMOKE_CLASSIFICATION WINDOWS_11_EVIDENCE",
    ]) {
      const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
        evidenceRow(
          id,
          id === requirementId
            ? { retained_evidence: `${currentTargetEvidence}; ${contradiction}` }
            : {},
        ),
      );
      assert.throws(
        () => validateFoundationEvidence(matrix(rows)),
        new RegExp(`FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${requirementId}`),
        `${requirementId}:${contradiction}`,
      );
    }
  }
});

test("rejects duplicate target status and provenance keys", () => {
  for (const duplicate of [
    "WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED",
    "LOCAL_DARWIN_ARM64_PACKAGED_E2E_COMMAND pnpm desktop:e2e",
  ]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(
        id,
        id === "DESIGN-2.4" ? { retained_evidence: `${currentTargetEvidence}; ${duplicate}` } : {},
      ),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      /FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:DESIGN-2\.4/,
      duplicate,
    );
  }
});

test("rejects unknown target keys and status values", () => {
  for (const retainedEvidence of [
    currentTargetEvidence.replace(
      "WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED",
      "WINDOWS11_TARGET_STATUS UNKNOWN",
    ),
    `${currentTargetEvidence}; WINDOWS11_RELEASE_ASSERTION APPROVED`,
  ]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(id, id === "DESIGN-13.5" ? { retained_evidence: retainedEvidence } : {}),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      /FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:DESIGN-13\.5/,
      retainedEvidence,
    );
  }
});

test("requires local packaged command provenance as a separate exact token", () => {
  for (const requirementId of ["DESIGN-2.4", "DESIGN-13.5"]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(
        id,
        id === requirementId
          ? {
              retained_evidence: currentTargetEvidence.replace(
                "LOCAL_DARWIN_ARM64_PACKAGED_E2E_COMMAND pnpm desktop:e2e; ",
                "",
              ),
            }
          : {},
      ),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      new RegExp(`FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${requirementId}`),
      requirementId,
    );
  }
});

test("requires honest protobuf breaking baseline status for DESIGN-13.3", () => {
  const initialBaselineRows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
    evidenceRow(
      id,
      id === "DESIGN-13.3"
        ? {
            retained_evidence: "PROTO_BREAKING_BASELINE_STATUS INITIAL_BASELINE_NOT_YET_ON_MASTER",
            status: "IMPLEMENTED_VERIFIED",
          }
        : {},
    ),
  );
  assert.throws(
    () => validateFoundationEvidence(matrix(initialBaselineRows)),
    /FOUNDATION_EVIDENCE_PROTO_BASELINE_MISCLASSIFIED:DESIGN-13\.3/,
  );

  const missingBaselineRows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
    evidenceRow(
      id,
      id === "DESIGN-13.3"
        ? {
            retained_evidence: "LOCAL_CONTRACT_TESTS PASSED",
            status: "IMPLEMENTED_PARTIAL_TARGET",
          }
        : {},
    ),
  );
  assert.throws(
    () => validateFoundationEvidence(matrix(missingBaselineRows)),
    /FOUNDATION_EVIDENCE_PROTO_BASELINE_MISCLASSIFIED:DESIGN-13\.3/,
  );
});

test("requires the complete no-claim product boundary in DESIGN-14", () => {
  for (const marker of [
    "FOUNDATION_PRODUCT_BOUNDARY_STATUS FOUNDATION_ONLY; ",
    "ACTIVITY_STATEMENT_IMPLEMENTATION_STATUS NOT_IMPLEMENTED; ",
    "MACHINE_CREDENTIAL_IMPLEMENTATION_STATUS NOT_IMPLEMENTED; ",
    "ATO_TRANSPORT_IMPLEMENTATION_STATUS NOT_IMPLEMENTED; ",
    "SBR_APPROVAL_STATUS NOT_CLAIMED; ",
    "LOCAL_TEST_COMMAND node --test scripts/check-foundation-evidence.test.mjs; ",
    "COMPLIANCE_CHECK_COMMAND pnpm compliance:foundation; ",
    "; DPO_OSF_EVTE_CONFORMANCE_PVT_WHITELISTING_STATUS NOT_PRODUCED",
  ]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(
        id,
        id === "DESIGN-14"
          ? { retained_evidence: productBoundaryEvidence.replace(marker, "") }
          : {},
      ),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      /FOUNDATION_EVIDENCE_PRODUCT_BOUNDARY_MISSING:DESIGN-14/,
      marker,
    );
  }
});

test("binds the foundation-only product boundary to partial-target status", () => {
  for (const status of ["IMPLEMENTED_VERIFIED", "NOT_YET_VERIFIED", "PLANNED", "NOT_APPLICABLE"]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(id, id === "DESIGN-14" ? { status } : {}),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      /FOUNDATION_EVIDENCE_PRODUCT_BOUNDARY_MISSING:DESIGN-14/,
      status,
    );
  }
});

test("rejects contradictory product-boundary values for every protected capability", () => {
  for (const contradiction of [
    "ACTIVITY_STATEMENT_IMPLEMENTATION_STATUS IMPLEMENTED",
    "MACHINE_CREDENTIAL_IMPLEMENTATION_STATUS IMPLEMENTED",
    "ATO_TRANSPORT_IMPLEMENTATION_STATUS IMPLEMENTED",
    "SBR_APPROVAL_STATUS APPROVAL_GRANTED",
  ]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(
        id,
        id === "DESIGN-14"
          ? { retained_evidence: `${productBoundaryEvidence}; ${contradiction}` }
          : {},
      ),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      /FOUNDATION_EVIDENCE_PRODUCT_BOUNDARY_MISSING:DESIGN-14/,
      contradiction,
    );
  }
});

test("rejects duplicate, unknown, and legacy product-boundary assertions", () => {
  for (const assertion of [
    "LOCAL_TEST_COMMAND node --test scripts/check-foundation-evidence.test.mjs",
    "SBR_APPROVAL_STATUS UNKNOWN",
    "SIGNED_RELEASE_STATUS APPROVED",
    "NO_APPROVAL_CLAIM",
    "APPROVAL_GRANTED",
  ]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(
        id,
        id === "DESIGN-14" ? { retained_evidence: `${productBoundaryEvidence}; ${assertion}` } : {},
      ),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      /FOUNDATION_EVIDENCE_PRODUCT_BOUNDARY_MISSING:DESIGN-14/,
      assertion,
    );
  }
});

test("rejects suffixed lookalikes for target, baseline, and product-boundary markers", () => {
  for (const [requirementId, marker, error] of [
    [
      "DESIGN-2.4",
      "LOCAL_DARWIN_ARM64_PACKAGED_E2E_STATUS PASSED",
      /FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:DESIGN-2\.4/,
    ],
    [
      "DESIGN-13.3",
      "INITIAL_BASELINE_NOT_YET_ON_MASTER",
      /FOUNDATION_EVIDENCE_PROTO_BASELINE_MISCLASSIFIED:DESIGN-13\.3/,
    ],
    [
      "DESIGN-14",
      "SBR_APPROVAL_STATUS NOT_CLAIMED",
      /FOUNDATION_EVIDENCE_PRODUCT_BOUNDARY_MISSING:DESIGN-14/,
    ],
  ]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) => {
      const row = evidenceRow(id);
      return id === requirementId
        ? {
            ...row,
            retained_evidence: row.retained_evidence.replace(marker, `${marker}_FORGED`),
          }
        : row;
    });
    assert.throws(() => validateFoundationEvidence(matrix(rows)), error, requirementId);
  }
});

test("rejects contradictory via suffixes on every mandatory verified target marker", () => {
  for (const requirementId of ["DESIGN-2.4", "DESIGN-13.5"]) {
    for (const marker of [
      "LOCAL_DARWIN_ARM64_PACKAGED_E2E_STATUS PASSED",
      "HOSTED_MACOS_TARGET_STATUS IMPLEMENTED_VERIFIED",
      "HOSTED_MACOS_PACKAGED_E2E_STATUS PASSED",
      "WINDOWS11_TARGET_STATUS IMPLEMENTED_VERIFIED",
      "WINDOWS11_PACKAGED_E2E_STATUS PASSED",
      "WINDOWS11_FOUNDATION_EVIDENCE_STATUS PRODUCED",
    ]) {
      const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
        evidenceRow(
          id,
          id === requirementId
            ? {
                retained_evidence: verifiedTargetEvidence.replace(
                  marker,
                  `${marker} via NOT_PRODUCED`,
                ),
                status: "IMPLEMENTED_VERIFIED",
              }
            : {},
        ),
      );
      assert.throws(
        () => validateFoundationEvidence(matrix(rows)),
        new RegExp(`FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${requirementId}`),
        `${requirementId}:${marker}`,
      );
    }
  }
});

test("rejects contradictory via suffixes on baseline and product-boundary markers", () => {
  const baselineRows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
    evidenceRow(
      id,
      id === "DESIGN-13.3"
        ? {
            retained_evidence:
              "PROTO_BREAKING_BASELINE_STATUS VERIFIED_AGAINST_MASTER via NOT_PRODUCED",
            status: "IMPLEMENTED_VERIFIED",
          }
        : {},
    ),
  );
  assert.throws(
    () => validateFoundationEvidence(matrix(baselineRows)),
    /FOUNDATION_EVIDENCE_PROTO_BASELINE_MISCLASSIFIED:DESIGN-13\.3/,
  );

  const boundaryRows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
    evidenceRow(
      id,
      id === "DESIGN-14"
        ? {
            retained_evidence: productBoundaryEvidence.replace(
              "SBR_APPROVAL_STATUS NOT_CLAIMED",
              "SBR_APPROVAL_STATUS NOT_CLAIMED via APPROVAL_GRANTED",
            ),
          }
        : {},
    ),
  );
  assert.throws(
    () => validateFoundationEvidence(matrix(boundaryRows)),
    /FOUNDATION_EVIDENCE_PRODUCT_BOUNDARY_MISSING:DESIGN-14/,
  );
});

test("rejects verified target evidence that also retains exact unverified states", () => {
  const contradictoryEvidence = `${verifiedTargetEvidence}; HOSTED_MACOS_TARGET_STATUS NOT_YET_VERIFIED; HOSTED_MACOS_PACKAGED_E2E_STATUS NOT_PRODUCED; WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED; WINDOWS11_PACKAGED_E2E_STATUS NOT_PRODUCED; WINDOWS11_FOUNDATION_EVIDENCE_STATUS NOT_PRODUCED`;
  for (const requirementId of ["DESIGN-2.4", "DESIGN-13.5"]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(
        id,
        id === requirementId
          ? {
              retained_evidence: contradictoryEvidence,
              status: "IMPLEMENTED_VERIFIED",
            }
          : {},
      ),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      new RegExp(`FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:${requirementId}`),
      requirementId,
    );
  }
});

test("rejects an approval grant alongside the exact no-approval claim", () => {
  const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
    evidenceRow(
      id,
      id === "DESIGN-14"
        ? {
            retained_evidence: `${productBoundaryEvidence}; SBR_APPROVAL_STATUS APPROVAL_GRANTED`,
          }
        : {},
    ),
  );
  assert.throws(
    () => validateFoundationEvidence(matrix(rows)),
    /FOUNDATION_EVIDENCE_PRODUCT_BOUNDARY_MISSING:DESIGN-14/,
  );
});

test("rejects case and Unicode-lookalike mandatory markers", () => {
  for (const forged of [
    "local_darwin_arm64_packaged_e2e_status passed",
    "LOCAL_DARWIN_ARM64_PACKAGED_E2E_STATUS P\u0410SSED",
    "LOCAL_DARWIN_ARM64_PACKAGED_E2E_STATUS PASSED_NOT",
  ]) {
    const rows = REQUIRED_FOUNDATION_REQUIREMENTS.map((id) =>
      evidenceRow(
        id,
        id === "DESIGN-2.4"
          ? {
              retained_evidence: currentTargetEvidence.replace(
                "LOCAL_DARWIN_ARM64_PACKAGED_E2E_STATUS PASSED",
                forged,
              ),
            }
          : {},
      ),
    );
    assert.throws(
      () => validateFoundationEvidence(matrix(rows)),
      /FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:DESIGN-2\.4/,
      forged,
    );
  }
});

test("reads the evidence through one bounded no-follow file handle", async () => {
  await withTemporaryEvidence(async (root) => {
    let openedFlags;
    const csv = await foundationEvidence.readStableEvidenceFile(root, {
      openFile: async (file, flags) => {
        openedFlags = flags;
        return open(file, flags);
      },
    });

    assert.equal(csv, matrix());
    assert.equal(
      openedFlags,
      constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0) | (constants.O_NONBLOCK ?? 0),
    );
  });
});

test("rejects oversized, growing, and truncated evidence without an unbounded read", async () => {
  await withTemporaryEvidence(async (root, evidencePath) => {
    await writeFile(evidencePath, Buffer.alloc(1024 * 1024 + 1, 0x61));
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(root),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );
  });

  await withTemporaryEvidence(async (root, evidencePath) => {
    let firstRead = true;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(root, {
        openFile: async (file, flags) => {
          const handle = await open(file, flags);
          return {
            close: () => handle.close(),
            read: async (...arguments_) => {
              if (firstRead) {
                firstRead = false;
                await appendFile(evidencePath, "X");
              }
              return handle.read(...arguments_);
            },
            stat: (options) => handle.stat(options),
          };
        },
      }),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );
  });

  await withTemporaryEvidence(async (root) => {
    let firstRead = true;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(root, {
        openFile: async (file, flags) => {
          const handle = await open(file, flags);
          return {
            close: () => handle.close(),
            read: async (...arguments_) => {
              if (firstRead) {
                firstRead = false;
                await handle.truncate(0);
              }
              return handle.read(...arguments_);
            },
            stat: (options) => handle.stat(options),
          };
        },
      }),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );
  });
});

test("rejects file or committed-path identity changes during the bounded read", async () => {
  await withTemporaryEvidence(async (root) => {
    let statCalls = 0;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(root, {
        openFile: async (file, flags) => {
          const handle = await open(file, flags);
          return {
            close: () => handle.close(),
            read: (...arguments_) => handle.read(...arguments_),
            stat: async (options) => {
              const stats = await handle.stat(options);
              statCalls += 1;
              if (statCalls === 1) return stats;
              return new Proxy(stats, {
                get(target, property) {
                  if (property === "ino") return target.ino + 1n;
                  const value = Reflect.get(target, property);
                  return typeof value === "function" ? value.bind(target) : value;
                },
              });
            },
          };
        },
      }),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );
  });

  await withTemporaryEvidence(async (root) => {
    let lstatCalls = 0;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(root, {
        lstatPath: async (file) => {
          const stats = await lstat(file, { bigint: true });
          lstatCalls += 1;
          if (lstatCalls === 1) return stats;
          return new Proxy(stats, {
            get(target, property) {
              if (property === "ino") return target.ino + 1n;
              const value = Reflect.get(target, property);
              return typeof value === "function" ? value.bind(target) : value;
            },
          });
        },
      }),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );
  });
});

test("rejects a symlinked final path before opening it", async () => {
  await withTemporaryEvidence(async (root, evidencePath) => {
    let opened = false;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(root, {
        lstatPath: async (file) => {
          const stats = await lstat(file, { bigint: true });
          if (file !== evidencePath) return stats;
          return new Proxy(stats, {
            get(target, property) {
              if (property === "isFile") return () => false;
              if (property === "isSymbolicLink") return () => true;
              const value = Reflect.get(target, property);
              return typeof value === "function" ? value.bind(target) : value;
            },
          });
        },
        openFile: async (file, flags) => {
          opened = true;
          return open(file, flags);
        },
      }),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );
    assert.equal(opened, false);
  });
});

test("accepts only a trusted physical root and the fixed evidence-relative path", async () => {
  await withTemporaryEvidence(async (root, evidencePath) => {
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(evidencePath),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );

    const rootAlias = `${root}-alias`;
    try {
      await symlink(root, rootAlias, "dir");
    } catch (error) {
      if (["EACCES", "EPERM", "ENOTSUP"].includes(error?.code)) return;
      throw error;
    }
    try {
      await assert.rejects(
        foundationEvidence.readStableEvidenceFile(rootAlias),
        /FOUNDATION_EVIDENCE_FILE_INVALID/,
      );
    } finally {
      await rm(rootAlias, { force: true });
    }
  });
});

test("rejects a real symlinked parent directory", async (context) => {
  const root = await realpath(await mkdtemp(path.join(tmpdir(), "tammy-foundation-parent-")));
  const outside = await realpath(await mkdtemp(path.join(tmpdir(), "tammy-foundation-outside-")));
  try {
    await mkdir(path.join(root, "compliance"), { recursive: true });
    await mkdir(path.join(outside, "traceability"), { recursive: true });
    await writeFile(path.join(outside, "traceability/foundation.csv"), matrix(), {
      encoding: "utf8",
      mode: 0o600,
    });
    try {
      await symlink(
        path.join(outside, "traceability"),
        path.join(root, "compliance/traceability"),
        "dir",
      );
    } catch (error) {
      if (["EACCES", "EPERM", "ENOTSUP"].includes(error?.code)) {
        context.skip(`directory symlinks unavailable: ${error.code}`);
        return;
      }
      throw error;
    }

    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(root),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
    await rm(outside, { force: true, recursive: true });
  }
});

test("fails closed when an ancestor identity changes during validation", async () => {
  await withTemporaryEvidence(async (root) => {
    const traceabilityPath = path.join(root, "compliance/traceability");
    let ancestorSnapshots = 0;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(root, {
        lstatPath: async (file, options) => {
          const stats = await lstat(file, options);
          if (file !== traceabilityPath) return stats;
          ancestorSnapshots += 1;
          if (ancestorSnapshots === 1) return stats;
          return new Proxy(stats, {
            get(target, property) {
              if (property === "ino") return target.ino + 1n;
              const value = Reflect.get(target, property);
              return typeof value === "function" ? value.bind(target) : value;
            },
          });
        },
      }),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );
    assert.ok(ancestorSnapshots >= 2);
  });
});

test("rejects a real FIFO replacement without blocking and closes its handle", {
  timeout: 3_000,
}, async (context) => {
  if (constants.O_NONBLOCK === undefined || process.platform === "win32") {
    context.skip("nonblocking FIFO open is unavailable on this platform");
    return;
  }
  const probeRoot = await mkdtemp(path.join(tmpdir(), "tammy-mkfifo-probe-"));
  try {
    await execFileAsync("mkfifo", ["-m", "600", path.join(probeRoot, "fifo")]);
  } catch (error) {
    if (["ENOENT", "EACCES", "EPERM", "ENOTSUP"].includes(error?.code)) {
      context.skip(`mkfifo unavailable: ${error.code}`);
      return;
    }
    throw error;
  } finally {
    await rm(probeRoot, { force: true, recursive: true });
  }

  await withTemporaryEvidence(async (root, evidencePath) => {
    let openedFifo = false;
    let closed = false;
    const startedAt = Date.now();
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(root, {
        openFile: async (file, flags) => {
          assert.equal(file, evidencePath);
          assert.notEqual(flags & constants.O_NONBLOCK, 0);
          await rm(file);
          await execFileAsync("mkfifo", ["-m", "600", file]);
          openedFifo = true;
          const handle = await open(file, flags);
          return {
            close: async () => {
              closed = true;
              await handle.close();
            },
            read: (...arguments_) => handle.read(...arguments_),
            stat: (options) => handle.stat(options),
          };
        },
      }),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );
    assert.equal(openedFifo, true);
    assert.equal(closed, true);
    assert.ok(Date.now() - startedAt < 2_000);
  });
});

test("documents exact-path and PID-scoped orphan recovery without broad process-name kills", async () => {
  const developmentGuide = await readFile(
    path.resolve(import.meta.dirname, "../docs/development/foundation.md"),
    "utf8",
  );
  for (const requiredGuidance of [
    "retain its exact path and PID diagnostics",
    "Do not use a broad process-name kill",
  ]) {
    assert.equal(developmentGuide.includes(requiredGuidance), true, requiredGuidance);
  }

  const commandBlocks = [...developmentGuide.matchAll(/```(?:sh|zsh|powershell)\n([\s\S]*?)```/gu)]
    .map((match) => match[1])
    .join("\n");
  assert.doesNotMatch(
    commandBlocks,
    /\b(?:killall|pkill)\b|taskkill(?:\.exe)?\s+\/IM|Stop-Process[^\n]*-Name/iu,
  );
});
