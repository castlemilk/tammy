import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { test } from "node:test";

import YAML from "yaml";

const workflowPath = path.resolve(
  import.meta.dirname,
  "../.github/workflows/foundation-windows11-e2e.yml",
);
const taskfilePath = path.resolve(import.meta.dirname, "../taskfiles/ci.yml");

function assertOrdered(command, expressions) {
  let previous = -1;
  for (const expression of expressions) {
    const index = command.indexOf(expression);
    assert.ok(index > previous, `${expression} must occur after the preceding Windows 11 handoff`);
    previous = index;
  }
}

function assertWindows11ResultJsonTail(command) {
  assert.match(
    command,
    /@\{ classification = "WINDOWS11_23H2_X64_RELEASE_GATE"; status = "PASSED"; run_id = \$env:GITHUB_RUN_ID; run_attempt = \$env:GITHUB_RUN_ATTEMPT \} \| ConvertTo-Json \| Set-Content -Encoding utf8 "apps\/desktop\/test-results\/windows11-result\.json" \}'$/,
  );
}

test("Windows SQLCipher workflow derives, validates, and exports exact COMSPEC", async () => {
  const workflow = await readFile(workflowPath, "utf8");
  assert.match(
    workflow,
    /\$comspec\s*=.*SystemRoot.*System32[\\/]cmd\.exe/i,
    "workflow must derive cmd.exe from the Windows system root",
  );
  assert.match(
    workflow,
    /Get-Item\s+-LiteralPath\s+\$comspec[\s\S]{0,600}ReparsePoint/i,
    "workflow must reject a missing, non-file, or reparse-point cmd.exe",
  );
  const exported = workflow.indexOf('"TAMMY_SQLCIPHER_COMSPEC=$comspec"');
  const build = workflow.indexOf("task ci:windows11");
  assert.ok(exported >= 0, "workflow must export TAMMY_SQLCIPHER_COMSPEC");
  assert.ok(
    build >= 0 && exported < build,
    "COMSPEC must be exported before SQLCipher build tests",
  );
});

test("Windows 11 workflow installs Task and delegates native evidence to one canonical scenario", async () => {
  const workflow = await readFile(workflowPath, "utf8");
  assert.match(workflow, /go install github\.com\/go-task\/task\/v3\/cmd\/task@v3\.52\.0/);
  assert.match(workflow, /Join-Path \(go env GOPATH\) "bin"[\s\S]{0,240}\$env:GITHUB_PATH/);
  assert.match(
    workflow,
    /name: Verify exact Task version[\s\S]*\$version = task --version[\s\S]*\$LASTEXITCODE -ne 0 -or \$version -ne "3\.52\.0"[\s\S]*UNSUPPORTED_TASK_VERSION:\$version/,
  );
  assert.match(workflow, /name: Run canonical Windows 11 CI scenario[\s\S]*run: task ci:windows11/);
  assert.doesNotMatch(
    workflow,
    /name: Build and prove the pinned Windows SQLCipher boundary before migrations/,
  );
  assert.doesNotMatch(workflow, /name: Run packaged Windows 11 offline acceptance/);
  assert.match(
    workflow,
    /name: Upload Windows 11 manifest and results[\s\S]*windows11-x64-foundation-evidence[\s\S]*apps\/desktop\/test-results\/\*\*/,
  );
});

test("Windows 11 Task handoff preserves the full guarded native release-gate sequence", async () => {
  const taskfile = YAML.parse(await readFile(taskfilePath, "utf8"));
  const command = taskfile.tasks.windows11.cmds.at(-1).cmd;
  assert.match(command, /^pwsh -NoProfile -NonInteractive -Command '& \{/);
  assertOrdered(command, [
    '$ErrorActionPreference = "Stop"',
    "pnpm sqlcipher:test",
    'throw "SQLCIPHER_NODE_TEST_FAILED"',
    "pnpm sqlcipher:build",
    'throw "SQLCIPHER_BUILD_FAILED"',
    'Resolve-Path ".tmp/sqlcipher/ordinary/win32-x64/ordinary-sqlite3.exe"',
    "$env:TAMMY_ORDINARY_SQLITE3 = $probe",
    "go test -race -tags tammy_sqlcipher ./services/core/internal/storage/sqlcipher/... -count=1",
    'throw "SQLCIPHER_GO_INTEGRATION_FAILED"',
    "pnpm desktop:e2e",
    'throw "WINDOWS11_PACKAGED_E2E_FAILED"',
    'New-Item -ItemType Directory -Force -Path "apps/desktop/test-results"',
    'classification = "WINDOWS11_23H2_X64_RELEASE_GATE"',
    'status = "PASSED"',
    "run_id = $env:GITHUB_RUN_ID",
    "run_attempt = $env:GITHUB_RUN_ATTEMPT",
    'Set-Content -Encoding utf8 "apps/desktop/test-results/windows11-result.json"',
  ]);
  for (const [commandExpression, guardExpression] of [
    ["pnpm sqlcipher:test", 'if ($LASTEXITCODE -ne 0) { throw "SQLCIPHER_NODE_TEST_FAILED" }'],
    ["pnpm sqlcipher:build", 'if ($LASTEXITCODE -ne 0) { throw "SQLCIPHER_BUILD_FAILED" }'],
    [
      "go test -race -tags tammy_sqlcipher ./services/core/internal/storage/sqlcipher/... -count=1",
      'if ($LASTEXITCODE -ne 0) { throw "SQLCIPHER_GO_INTEGRATION_FAILED" }',
    ],
    ["pnpm desktop:e2e", 'if ($LASTEXITCODE -ne 0) { throw "WINDOWS11_PACKAGED_E2E_FAILED" }'],
  ]) {
    assert.match(
      command,
      new RegExp(
        `${commandExpression.replaceAll(/[.*+?^${}()|[\]\\]/g, "\\$&")}; ${guardExpression.replaceAll(/[.*+?^${}()|[\]\\]/g, "\\$&")}`,
      ),
    );
  }
  assertWindows11ResultJsonTail(command);
  assert.throws(
    () => assertWindows11ResultJsonTail(command.replace(" | ConvertTo-Json", "")),
    assert.AssertionError,
    "the result handoff must not permit writing PowerShell text instead of JSON",
  );
});
