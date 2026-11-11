import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import YAML from "yaml";

const TARGET = Object.freeze(["self-hosted", "linux", "ARM64", "tammy-linux-ci"]);
const workflowPaths = [
  [".github/workflows/foundation-ci.yml", "contracts"],
  [".github/workflows/tammy-site.yml", "verify-site"],
  [".github/workflows/xero-roadmap-ci.yml", "validate"],
];
const read = async (path) =>
  YAML.parse(await readFile(new URL(`../${path}`, import.meta.url), "utf8"));

test("foundation, site and roadmap Linux jobs require a Cuttlefish repo-scoped runner", async () => {
  for (const [path, name] of workflowPaths) {
    const document = await read(path);
    const job = document.jobs[name];
    assert.ok(job, `${path}: missing job ${name}`);
    assert.deepEqual(job["runs-on"], TARGET, `${path}: no GitHub-hosted fallback`);
    assert.match(job.if, /github\.event_name != 'pull_request'/, `${path}: trusted branch guard`);
    assert.match(
      job.if,
      /github\.event\.pull_request\.head\.repo\.full_name == github\.repository/,
      `${path}: block fork PRs`,
    );
    assert.equal(document.permissions.contents, "read", `${path}: minimal token`);
    assert.equal(job.steps[0].name, "Verify Cuttlefish Linux runner identity", path);
    const verify = job.steps[0].run;
    assert.match(verify, /RUNNER_NAME/, path);
    assert.match(verify, /cf-vm-/, path);
    assert.match(verify, /RUNNER_ARCH/, path);
    assert.match(verify, /aarch64/, path);
  }
});

test("native release-platform gates are not silently replaced by Linux", async () => {
  const foundation = await read(".github/workflows/foundation-ci.yml");
  assert.equal(foundation.jobs["macos14-arm64-packaged"]["runs-on"], "macos-15");
  assert.equal(foundation.jobs["macos14-arm64-packaged"].name, "macOS 15 arm64 packaged E2E");
  assert.equal(foundation.jobs["windows-server-x64-package-smoke"]["runs-on"], "windows-2025");
  const windows = await read(".github/workflows/foundation-windows11-e2e.yml");
  assert.deepEqual(windows.jobs["windows11-23h2-x64-packaged-e2e"]["runs-on"], [
    "self-hosted",
    "windows",
    "x64",
    "tammy-win11-23h2",
  ]);
});

test("Cuttlefish readiness probe cannot be satisfied by a GitHub-hosted machine", async () => {
  const wf = await read(".github/workflows/cuttlefish-runner-readiness.yml");
  assert.equal(wf.permissions.contents, "read");
  assert.deepEqual(wf.jobs["verify-cuttlefish"]["runs-on"], TARGET);
  const probe = wf.jobs["verify-cuttlefish"].steps[0].run;
  assert.match(probe, /RUNNER_NOT_CUTTLEFISH/);
  assert.ok(probe.includes("castlemilk/tammy"));
  assert.match(probe, /RUNNER_ARCH/);
  assert.match(probe, /cf-vm-/);
});
