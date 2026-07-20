import assert from "node:assert/strict";
import { constants } from "node:fs";
import { appendFile, lstat, mkdtemp, open, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";

import * as foundationEvidence from "./check-foundation-evidence.mjs";
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

const currentTargetEvidence =
  "LOCAL_DARWIN_ARM64_PACKAGED_E2E PASSED; HOSTED_MACOS_TARGET_STATUS NOT_YET_VERIFIED; macos14-arm64-foundation-failure-evidence NOT_PRODUCED; WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED; windows11-x64-foundation-evidence NOT_PRODUCED; WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64 NOT_PRODUCED and NOT WINDOWS 11 EVIDENCE";

const productBoundaryEvidence =
  "FOUNDATION_PRODUCT_BOUNDARY; NO_ACTIVITY_STATEMENT_IMPLEMENTATION; NO_CREDENTIAL_IMPLEMENTATION; NO_ATO_TRANSPORT_IMPLEMENTATION; NO_APPROVAL_CLAIM";

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
  const root = await mkdtemp(path.join(tmpdir(), "tammy-foundation-evidence-"));
  try {
    const evidencePath = path.join(root, "foundation.csv");
    await writeFile(evidencePath, matrix(), { encoding: "utf8", mode: 0o600 });
    await run(evidencePath);
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
      "LOCAL_DARWIN_ARM64_PACKAGED_E2E PASSED; ",
      "HOSTED_MACOS_TARGET_STATUS NOT_YET_VERIFIED; ",
      "macos14-arm64-foundation-failure-evidence NOT_PRODUCED; ",
      "WINDOWS11_TARGET_STATUS NOT_YET_VERIFIED; ",
      "windows11-x64-foundation-evidence NOT_PRODUCED; ",
      "; WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64 NOT_PRODUCED and NOT WINDOWS 11 EVIDENCE",
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
            retained_evidence: currentTargetEvidence.replace(
              "LOCAL_DARWIN_ARM64_PACKAGED_E2E PASSED; ",
              "",
            ),
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
    "FOUNDATION_PRODUCT_BOUNDARY; ",
    "NO_ACTIVITY_STATEMENT_IMPLEMENTATION; ",
    "NO_CREDENTIAL_IMPLEMENTATION; ",
    "NO_ATO_TRANSPORT_IMPLEMENTATION; ",
    "; NO_APPROVAL_CLAIM",
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

test("rejects suffixed lookalikes for target, baseline, and product-boundary markers", () => {
  for (const [requirementId, marker, error] of [
    [
      "DESIGN-2.4",
      "LOCAL_DARWIN_ARM64_PACKAGED_E2E PASSED",
      /FOUNDATION_EVIDENCE_WINDOWS_TARGET_MISCLASSIFIED:DESIGN-2\.4/,
    ],
    [
      "DESIGN-13.3",
      "INITIAL_BASELINE_NOT_YET_ON_MASTER",
      /FOUNDATION_EVIDENCE_PROTO_BASELINE_MISCLASSIFIED:DESIGN-13\.3/,
    ],
    ["DESIGN-14", "NO_APPROVAL_CLAIM", /FOUNDATION_EVIDENCE_PRODUCT_BOUNDARY_MISSING:DESIGN-14/],
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

test("reads the evidence through one bounded no-follow file handle", async () => {
  await withTemporaryEvidence(async (evidencePath) => {
    let openedFlags;
    const csv = await foundationEvidence.readStableEvidenceFile(evidencePath, {
      openFile: async (file, flags) => {
        openedFlags = flags;
        return open(file, flags);
      },
    });

    assert.equal(csv, matrix());
    assert.equal(openedFlags, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
  });
});

test("rejects oversized, growing, and truncated evidence without an unbounded read", async () => {
  await withTemporaryEvidence(async (evidencePath) => {
    await writeFile(evidencePath, Buffer.alloc(1024 * 1024 + 1, 0x61));
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(evidencePath),
      /FOUNDATION_EVIDENCE_FILE_INVALID/,
    );
  });

  await withTemporaryEvidence(async (evidencePath) => {
    let firstRead = true;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(evidencePath, {
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

  await withTemporaryEvidence(async (evidencePath) => {
    let firstRead = true;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(evidencePath, {
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
  await withTemporaryEvidence(async (evidencePath) => {
    let statCalls = 0;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(evidencePath, {
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

  await withTemporaryEvidence(async (evidencePath) => {
    let lstatCalls = 0;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(evidencePath, {
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

test("rejects a symlinked path on platforms without no-follow support and always closes", async () => {
  await withTemporaryEvidence(async (evidencePath) => {
    let closed = false;
    await assert.rejects(
      foundationEvidence.readStableEvidenceFile(evidencePath, {
        lstatPath: async (file) => {
          const stats = await lstat(file, { bigint: true });
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
    assert.equal(closed, true);
  });
});

test("documents exact-path and PID-scoped orphan recovery for macOS and Windows", async () => {
  const developmentGuide = await readFile(
    path.resolve(import.meta.dirname, "../docs/development/foundation.md"),
    "utf8",
  );
  for (const requiredCommand of [
    "out/Tammy-darwin-arm64/Tammy.app/Contents/MacOS/Tammy",
    "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core",
    "/bin/ps -axo pid=,ppid=,comm=",
    '/bin/kill -TERM "$pid"',
    "out\\Tammy-win32-x64\\Tammy.exe",
    "out\\Tammy-win32-x64\\resources\\core\\win32-x64\\tammy-core.exe",
    "Get-CimInstance Win32_Process",
    "ParentProcessId",
    "& $Taskkill /PID $process.ProcessId /T",
    "ORPHAN_RECOVERY_CONFIRMED",
  ]) {
    assert.equal(developmentGuide.includes(requiredCommand), true, requiredCommand);
  }

  const commandBlocks = [...developmentGuide.matchAll(/```(?:sh|zsh|powershell)\n([\s\S]*?)```/gu)]
    .map((match) => match[1])
    .join("\n");
  assert.doesNotMatch(
    commandBlocks,
    /\b(?:killall|pkill)\b|taskkill(?:\.exe)?\s+\/IM|Stop-Process[^\n]*-Name/iu,
  );
});
