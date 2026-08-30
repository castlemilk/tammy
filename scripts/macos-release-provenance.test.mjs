import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import { createCandidateBuiltEvent, readProductSource } from "./macos-release-provenance.mjs";

const execFileAsync = promisify(execFile);

async function git(root, ...args) {
  return execFileAsync("git", args, { cwd: root, encoding: "utf8" });
}

async function createRepository(context) {
  const root = await mkdtemp(path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-product-source-"));
  context.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(path.join(root, "apps/desktop/release/macos"), { recursive: true });
  await writeFile(
    path.join(root, "apps/desktop/package.json"),
    `${JSON.stringify({ name: "@tammy/desktop", version: "0.1.0" }, null, 2)}\n`,
  );
  await writeFile(
    path.join(root, "apps/desktop/release/macos/build-numbers.json"),
    `${JSON.stringify(
      {
        schemaVersion: 1,
        entries: [
          {
            buildNumber: "1",
            marketingVersion: "0.1.0",
            reservedAt: "2026-08-30T00:00:00.000Z",
            reservedBy: "Ben Ebsworth",
            state: "reserved",
          },
        ],
      },
      null,
      2,
    )}\n`,
  );
  await git(root, "init", "-q");
  await git(root, "add", ".");
  await git(
    root,
    "-c",
    "user.name=Tammy Tests",
    "-c",
    "user.email=tammy-tests@example.invalid",
    "commit",
    "-qm",
    "fixture",
  );
  return root;
}

test("reads a clean committed product source and ledger reservation", async (context) => {
  const root = await createRepository(context);
  const source = await readProductSource(root);
  assert.match(source.productSourceCommit, /^[0-9a-f]{40}$/);
  assert.match(source.productSourceTree, /^[0-9a-f]{40}$/);
  assert.equal(source.marketingVersion, "0.1.0");
  assert.equal(source.ledger.entries[0].buildNumber, "1");
  assert.equal(JSON.stringify(source).includes("reservedBy"), true);
});

test("rejects dirty or uncommitted product-source facts", async (context) => {
  const root = await createRepository(context);
  await writeFile(path.join(root, "dirty.txt"), "dirty\n");
  await assert.rejects(readProductSource(root), /MACOS_PRODUCT_SOURCE_DIRTY/);
});

test("ignores inherited Git repository overrides and verifies the supplied root", async (context) => {
  const suppliedRoot = await createRepository(context);
  const foreignRoot = await createRepository(context);
  await writeFile(path.join(suppliedRoot, "dirty.txt"), "dirty\n");
  const previousGitDir = process.env.GIT_DIR;
  const previousGitWorkTree = process.env.GIT_WORK_TREE;
  process.env.GIT_DIR = path.join(foreignRoot, ".git");
  process.env.GIT_WORK_TREE = foreignRoot;
  try {
    await assert.rejects(readProductSource(suppliedRoot), /MACOS_PRODUCT_SOURCE_DIRTY/);
  } finally {
    if (previousGitDir === undefined) delete process.env.GIT_DIR;
    else process.env.GIT_DIR = previousGitDir;
    if (previousGitWorkTree === undefined) delete process.env.GIT_WORK_TREE;
    else process.env.GIT_WORK_TREE = previousGitWorkTree;
  }
});

test("creates an exact phase-two event without mutating the phase-one ledger", async (context) => {
  const root = await createRepository(context);
  const source = await readProductSource(root);
  const ledgerPath = path.join(root, "apps/desktop/release/macos/build-numbers.json");
  const before = await readFile(ledgerPath, "utf8");
  const event = createCandidateBuiltEvent({
    productSource: source,
    buildNumber: "1",
    marketingVersion: "0.1.0",
    unsignedContentManifestSha256: "c".repeat(64),
    appSha256: "d".repeat(64),
    packageSha256: "e".repeat(64),
  });
  assert.deepEqual(event, {
    kind: "candidate-built",
    buildNumber: "1",
    marketingVersion: "0.1.0",
    productSourceCommit: source.productSourceCommit,
    productSourceTree: source.productSourceTree,
    unsignedContentManifestSha256: "c".repeat(64),
    appSha256: "d".repeat(64),
    packageSha256: "e".repeat(64),
  });
  assert.equal(await readFile(ledgerPath, "utf8"), before);
  assert.equal((await git(root, "status", "--porcelain")).stdout, "");

  for (const invalid of [
    { buildNumber: "2" },
    { marketingVersion: "0.2.0" },
    { packageSha256: "not-a-hash" },
    { apiToken: "redacted" },
  ]) {
    assert.throws(
      () =>
        createCandidateBuiltEvent({
          productSource: source,
          buildNumber: "1",
          marketingVersion: "0.1.0",
          unsignedContentManifestSha256: "c".repeat(64),
          appSha256: "d".repeat(64),
          packageSha256: "e".repeat(64),
          ...invalid,
        }),
      /MACOS_CANDIDATE_PROVENANCE_INVALID/,
    );
  }
});
