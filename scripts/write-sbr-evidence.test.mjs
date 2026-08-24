import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { chmod, lstat, mkdir, mkdtemp, readFile, symlink, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { consumePassedSbrEvidence } from "./write-sbr-evidence.mjs";

const realRepositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const HASH = "a".repeat(64);
const REVISION = "b".repeat(40);

function validResult(overrides = {}) {
  return {
    schema: "tammy-sbr-e2e-result-v1",
    source_revision: REVISION,
    profile_sha256: HASH,
    helper_sha256: HASH,
    fixture_sha256: HASH,
    socket_samples: 3,
    socket_violations: 0,
    core_path_verified: true,
    helper_path_verified: true,
    core_orphans: 0,
    helper_orphans: 0,
    playwright_status: "PASSED",
    recorded_at: "2026-08-24T02:00:00.000Z",
    ...overrides,
  };
}

async function installResult(root, result = validResult()) {
  const directory = path.join(root, ".tmp/sbr-e2e/latest");
  await mkdir(directory, { recursive: true });
  const destination = path.join(directory, "result.json");
  await writeFile(destination, `${JSON.stringify(result)}\n`, { mode: 0o600 });
  await chmod(destination, 0o600);
  return destination;
}

test("consumes the existing exact-revision 0600 PASSED result into redacted evidence", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-evidence-"));
  await installResult(root);

  const bundle = await consumePassedSbrEvidence({
    currentRevision: async () => REVISION,
    now: new Date("2026-08-25T03:04:05.000Z"),
    repositoryRoot: root,
  });
  assert.deepEqual(bundle, {
    schema: "tammy-sbr-evidence-bundle-v1",
    source_revision: REVISION,
    profile_sha256: HASH,
    helper_sha256: HASH,
    fixture_sha256: HASH,
    zero_socket_observation: { samples: 3, violations: 0 },
    process_observation: {
      core_path_verified: true,
      helper_path_verified: true,
      core_orphans: 0,
      helper_orphans: 0,
    },
    playwright_status: "PASSED",
    recorded_at: "2026-08-24T02:00:00.000Z",
    exported_at: "2026-08-25T03:04:05.000Z",
    environment: "SIMULATOR",
    ato_lodgment: "UNAVAILABLE",
  });
  const evidencePath = path.join(root, ".tmp/sbr-e2e/latest/evidence.json");
  assert.deepEqual(JSON.parse(await readFile(evidencePath, "utf8")), bundle);
  assert.equal((await lstat(evidencePath)).mode & 0o777, 0o600);
  assert.doesNotMatch(JSON.stringify(bundle), /(?:\/Users\/|password|credential|product.?id)/iu);
});

test("fails closed for stale, malformed, insecure, or symlinked results and removes stale evidence", async () => {
  for (const kind of ["revision", "status", "mode", "symlink"]) {
    const root = await mkdtemp(path.join(os.tmpdir(), "tammy-evidence-"));
    const result =
      kind === "revision"
        ? validResult({ source_revision: "c".repeat(40) })
        : kind === "status"
          ? validResult({ playwright_status: "FAILED" })
          : validResult();
    const resultPath = await installResult(root, result);
    if (kind === "mode") await chmod(resultPath, 0o644);
    if (kind === "symlink") {
      const target = `${resultPath}.target`;
      await writeFile(target, `${JSON.stringify(result)}\n`, { mode: 0o600 });
      await import("node:fs/promises").then(({ rm }) => rm(resultPath));
      await symlink(target, resultPath);
    }
    const evidencePath = path.join(root, ".tmp/sbr-e2e/latest/evidence.json");
    await writeFile(evidencePath, "stale", { mode: 0o600 });

    await assert.rejects(
      consumePassedSbrEvidence({ currentRevision: async () => REVISION, repositoryRoot: root }),
      new Error("SBR_EVIDENCE_RESULT_INVALID"),
    );
    await assert.rejects(readFile(evidencePath));
  }
});

test("CLI accepts no inputs and does not invoke Playwright", async () => {
  const source = await readFile(path.join(realRepositoryRoot, "scripts/write-sbr-evidence.mjs"), "utf8");
  assert.doesNotMatch(source, /(?:pnpm|npx).*playwright|test:e2e:packaged/iu);
  const child = spawn(
    "mise",
    ["exec", "--", "node", "scripts/write-sbr-evidence.mjs", "--result=/private/tmp/result.json"],
    { cwd: realRepositoryRoot, shell: false, stdio: ["ignore", "pipe", "pipe"] },
  );
  let output = "";
  child.stderr.on("data", (chunk) => (output += chunk));
  const code = await new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("close", resolve);
  });
  assert.notEqual(code, 0);
  assert.equal(output, "SBR_EVIDENCE_ARGUMENTS_INVALID\n");
});
