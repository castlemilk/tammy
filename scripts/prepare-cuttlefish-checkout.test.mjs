import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath, pathToFileURL } from "node:url";

const script = fileURLToPath(new URL("./prepare-cuttlefish-checkout.mjs", import.meta.url));

function git(cwd, ...args) {
  return execFileSync("git", args, {
    cwd,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function fixture(t) {
  const root = mkdtempSync(path.join(tmpdir(), "tammy-cuttlefish-checkout-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const remote = path.join(root, "remote");
  git(root, "init", "--initial-branch=master", remote);
  git(remote, "config", "user.name", "CI Test");
  git(remote, "config", "user.email", "ci@example.test");
  writeFileSync(path.join(remote, "file"), "base\n");
  git(remote, "add", "file");
  git(remote, "commit", "-m", "base");
  const master = git(remote, "rev-parse", "HEAD");
  git(remote, "checkout", "-b", "feature");
  writeFileSync(path.join(remote, "file"), "feature\n");
  git(remote, "commit", "-am", "feature");
  const head = git(remote, "rev-parse", "HEAD");
  const checkout = path.join(root, "checkout");
  git(root, "clone", "--depth=1", "--branch=feature", pathToFileURL(remote).href, checkout);
  return { checkout, head, master };
}

function prepare(checkout, sha) {
  const env = { ...process.env };
  delete env.CUTTLE_GITHUB_SHA;
  if (sha !== undefined) env.CUTTLE_GITHUB_SHA = sha;
  return spawnSync(process.execPath, [script], { cwd: checkout, env, encoding: "utf8" });
}

test("Cuttlefish checkout preserves the dispatched commit and fetches full master history", (t) => {
  const { checkout, head, master } = fixture(t);
  assert.equal(git(checkout, "rev-parse", "--is-shallow-repository"), "true");
  const result = prepare(checkout, head);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(git(checkout, "rev-parse", "HEAD"), head);
  assert.equal(git(checkout, "rev-parse", "refs/remotes/origin/master"), master);
  assert.equal(git(checkout, "rev-parse", "--is-shallow-repository"), "false");
});

test("Cuttlefish checkout fails before fetching when its head differs from the dispatched commit", (t) => {
  const { checkout, master } = fixture(t);
  const result = prepare(checkout, master);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /CUTTLEFISH_CHECKOUT_REVISION_MISMATCH/);
  assert.equal(git(checkout, "rev-parse", "--is-shallow-repository"), "true");
});

test("manual runs can prepare an already complete checkout", (t) => {
  const { checkout, head, master } = fixture(t);
  assert.equal(prepare(checkout, head).status, 0);
  const result = prepare(checkout);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(git(checkout, "rev-parse", "HEAD"), head);
  assert.equal(git(checkout, "rev-parse", "refs/remotes/origin/master"), master);
});
