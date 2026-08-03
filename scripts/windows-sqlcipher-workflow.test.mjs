import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { test } from "node:test";

const workflowPath = path.resolve(
  import.meta.dirname,
  "../.github/workflows/foundation-windows11-e2e.yml",
);

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
  const build = workflow.indexOf("pnpm sqlcipher:test");
  assert.ok(exported >= 0, "workflow must export TAMMY_SQLCIPHER_COMSPEC");
  assert.ok(
    build >= 0 && exported < build,
    "COMSPEC must be exported before SQLCipher build tests",
  );
});
