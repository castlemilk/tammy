import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

const root = path.resolve(import.meta.dirname, "..");

test("mise pins Task 3.52.0 while preserving Node and Go pins", () => {
  const mise = readFileSync(path.join(root, "mise.toml"), "utf8");

  assert.match(mise, /^node = "24\.18\.0"$/m);
  assert.match(mise, /^go = "1\.26\.4"$/m);
  assert.match(mise, /^task = "3\.52\.0"$/m);
});

test("clean tree guard accepts only an empty successful porcelain status", async () => {
  const calls = [];
  const run = async (command, args, options) => {
    calls.push({ command, args, options });
    return { stdout: "" };
  };
  const { checkCleanTree } = await import("./check-clean-tree.mjs");

  await checkCleanTree(run);
  assert.deepEqual(calls, [
    {
      command: "git",
      args: ["status", "--porcelain=v1", "--untracked-files=all"],
      options: { cwd: process.cwd(), shell: false },
    },
  ]);
});

test("clean tree guard reports the exact changed paths", async () => {
  const { checkCleanTree } = await import("./check-clean-tree.mjs");

  await assert.rejects(
    checkCleanTree(async () => ({
      stdout: " M README.md\n?? docs/superpowers/plans/plan.md\n",
    })),
    (error) => {
      assert.equal(error.message, "DIRTY_WORKTREE\nREADME.md\ndocs/superpowers/plans/plan.md");
      assert.equal(error.code, "DIRTY_WORKTREE");
      assert.deepEqual(error.paths, ["README.md", "docs/superpowers/plans/plan.md"]);
      return true;
    },
  );
});

test("clean tree guard propagates git spawn and non-zero failures", async () => {
  const { checkCleanTree } = await import("./check-clean-tree.mjs");
  const spawnFailure = new Error("spawn git ENOENT");
  const statusFailure = new Error("git exited with status 1");
  statusFailure.code = 1;

  await assert.rejects(checkCleanTree(async () => { throw spawnFailure; }), spawnFailure);
  await assert.rejects(checkCleanTree(async () => { throw statusFailure; }), statusFailure);
});

test("zero-argument CLI checks the inherited cwd without mutating files", (context) => {
  const fixture = mkdtempSync(path.join(os.tmpdir(), "tammy-clean-tree-"));
  context.after(() => rmSync(fixture, { force: true, recursive: true }));
  execFileSync("git", ["init", "--quiet"], { cwd: fixture });
  writeFileSync(path.join(fixture, "sentinel.txt"), "preserve me\n");
  writeFileSync(path.join(fixture, ".git/info/exclude"), "sentinel.txt\n");

  const result = spawnSync(process.execPath, [path.join(root, "scripts/check-clean-tree.mjs")], {
    cwd: fixture,
    encoding: "utf8",
  });

  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stderr, "");
  assert.equal(readFileSync(path.join(fixture, "sentinel.txt"), "utf8"), "preserve me\n");
  assert.equal(
    execFileSync("git", ["status", "--porcelain=v1"], { cwd: fixture, encoding: "utf8" }),
    "",
  );
});

test("zero-argument CLI reports untracked files hidden by Git configuration", (context) => {
  const fixture = mkdtempSync(path.join(os.tmpdir(), "tammy-clean-tree-untracked-"));
  context.after(() => rmSync(fixture, { force: true, recursive: true }));
  execFileSync("git", ["init", "--quiet"], { cwd: fixture });
  execFileSync("git", ["config", "status.showUntrackedFiles", "no"], { cwd: fixture });
  writeFileSync(path.join(fixture, "untracked.txt"), "must remain\n");

  const result = spawnSync(process.execPath, [path.join(root, "scripts/check-clean-tree.mjs")], {
    cwd: fixture,
    encoding: "utf8",
  });

  assert.equal(result.status, 1);
  assert.equal(result.stderr, "DIRTY_WORKTREE\nuntracked.txt\n");
  assert.equal(readFileSync(path.join(fixture, "untracked.txt"), "utf8"), "must remain\n");
});
