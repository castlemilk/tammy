import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import {
  createCandidateBuiltEvent,
  createProductSourceRecord,
  readProductSource,
  runProductSourceCommand,
  verifyFrozenProductSource,
} from "./macos-release-provenance.mjs";

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

async function createFrozenRepository(context) {
  const root = await createRepository(context);
  const productSource = await readProductSource(root);
  const candidateRoot = path.join(
    root,
    "docs/release/records/macos/0.1.0/build-1/evidence/candidate/018f3d8c-7b2a-7abc-8def-1234567890ab",
  );
  const screenshotRoot = path.join(root, "apps/desktop/release/macos/screenshots/en-AU");
  await mkdir(candidateRoot, { recursive: true });
  await mkdir(screenshotRoot, { recursive: true });
  await writeFile(
    path.join(candidateRoot, "candidate.json"),
    `${JSON.stringify({
      appSha256: "d".repeat(64),
      buildNumber: "1",
      packageSha256: "e".repeat(64),
      releaseVersion: "0.1.0",
      sourceCommit: productSource.productSourceCommit,
      sourceTree: productSource.productSourceTree,
    })}\n`,
  );
  for (const name of [
    "metadata-snapshot.json",
    "privacy-evidence.json",
    "runtime-egress.json",
    "screenshots.json",
  ]) {
    await writeFile(path.join(candidateRoot, name), "{}\n");
  }
  await writeFile(path.join(candidateRoot, "summary.md"), "# Candidate\n");
  await writeFile(path.join(screenshotRoot, "manifest.json"), "{}\n");
  await git(root, "add", ".");
  await git(
    root,
    "-c",
    "user.name=Tammy Tests",
    "-c",
    "user.email=tammy-tests@example.invalid",
    "commit",
    "-qm",
    "candidate bookkeeping",
  );
  return { productSource, root };
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

test("creates a redacted product-source record for one exact reservation", async (context) => {
  const root = await createRepository(context);
  const source = await readProductSource(root);
  const record = createProductSourceRecord(source, {
    buildNumber: "1",
    marketingVersion: "0.1.0",
  });
  assert.deepEqual(record, {
    buildNumber: "1",
    marketingVersion: "0.1.0",
    productSourceCommit: source.productSourceCommit,
    productSourceTree: source.productSourceTree,
    status: "frozen-source",
  });
  assert.equal(JSON.stringify(record).includes("ledger"), false);
  assert.equal(JSON.stringify(record).includes("reservedBy"), false);
});

test("rejects a product-source record for an unreserved build or mismatched version", async (context) => {
  const root = await createRepository(context);
  const source = await readProductSource(root);
  for (const requested of [
    { buildNumber: "2", marketingVersion: "0.1.0" },
    { buildNumber: "1", marketingVersion: "0.2.0" },
    { buildNumber: "01", marketingVersion: "0.1.0" },
  ]) {
    assert.throws(
      () => createProductSourceRecord(source, requested),
      /MACOS_PRODUCT_SOURCE_RESERVATION_INVALID/,
    );
  }
});

test("runs the read-only source command for an exact committed reservation", async (context) => {
  const root = await createRepository(context);
  const result = await runProductSourceCommand(root, [
    "--source",
    "--build",
    "1",
    "--version",
    "0.1.0",
  ]);
  assert.equal(result?.status, "frozen-source");
  assert.equal(result?.buildNumber, "1");
  assert.equal(result?.marketingVersion, "0.1.0");
  assert.deepEqual(Object.keys(result ?? {}).sort(), [
    "buildNumber",
    "marketingVersion",
    "productSourceCommit",
    "productSourceTree",
    "status",
  ]);
});

test("rejects ambiguous product-source command arguments", async (context) => {
  const root = await createRepository(context);
  for (const arguments_ of [
    ["--source", "--build", "1", "--version", "0.1.0", "extra"],
    ["--source", "--build", "1", "--build", "2", "--version", "0.1.0"],
    ["--source", "--build", "1"],
  ]) {
    await assert.rejects(
      async () => runProductSourceCommand(root, arguments_),
      /MACOS_PRODUCT_SOURCE_ARGUMENT_INVALID/,
    );
  }
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
    { marketingVersion: "01.2.3" },
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

test("verifies a frozen source with only canonical screenshots and exact build records", async (context) => {
  const { productSource, root } = await createFrozenRepository(context);
  const result = await verifyFrozenProductSource(root, {
    buildNumber: "1",
    marketingVersion: "0.1.0",
  });
  assert.equal(result.status, "frozen");
  assert.equal(result.productSourceCommit, productSource.productSourceCommit);
  assert.equal(result.productSourceTree, productSource.productSourceTree);
  assert.deepEqual(result.changedFiles, [
    "apps/desktop/release/macos/screenshots/en-AU/manifest.json",
    "docs/release/records/macos/0.1.0/build-1/evidence/candidate/018f3d8c-7b2a-7abc-8def-1234567890ab/candidate.json",
    "docs/release/records/macos/0.1.0/build-1/evidence/candidate/018f3d8c-7b2a-7abc-8def-1234567890ab/metadata-snapshot.json",
    "docs/release/records/macos/0.1.0/build-1/evidence/candidate/018f3d8c-7b2a-7abc-8def-1234567890ab/privacy-evidence.json",
    "docs/release/records/macos/0.1.0/build-1/evidence/candidate/018f3d8c-7b2a-7abc-8def-1234567890ab/runtime-egress.json",
    "docs/release/records/macos/0.1.0/build-1/evidence/candidate/018f3d8c-7b2a-7abc-8def-1234567890ab/screenshots.json",
    "docs/release/records/macos/0.1.0/build-1/evidence/candidate/018f3d8c-7b2a-7abc-8def-1234567890ab/summary.md",
  ]);
});

test("rejects product changes and unvalidated bookkeeping after the frozen commit", async (context) => {
  const first = await createFrozenRepository(context);
  await writeFile(
    path.join(first.root, "apps/desktop/package.json"),
    `${JSON.stringify({ name: "@tammy/desktop", version: "0.1.1" })}\n`,
  );
  await git(first.root, "add", ".");
  await git(
    first.root,
    "-c",
    "user.name=Tammy Tests",
    "-c",
    "user.email=tammy-tests@example.invalid",
    "commit",
    "-qm",
    "mutate product",
  );
  await assert.rejects(
    verifyFrozenProductSource(first.root, {
      buildNumber: "1",
      marketingVersion: "0.1.0",
    }),
    /MACOS_FROZEN_SOURCE_CHANGED/,
  );

  const second = await createFrozenRepository(context);
  const unexpected = path.join(
    second.root,
    "docs/release/records/macos/0.1.0/build-1/operator-notes.txt",
  );
  await writeFile(unexpected, "not validated\n");
  await git(second.root, "add", ".");
  await git(
    second.root,
    "-c",
    "user.name=Tammy Tests",
    "-c",
    "user.email=tammy-tests@example.invalid",
    "commit",
    "-qm",
    "unexpected record",
  );
  await assert.rejects(
    verifyFrozenProductSource(second.root, {
      buildNumber: "1",
      marketingVersion: "0.1.0",
    }),
    /MACOS_FROZEN_SOURCE_CHANGED/,
  );
});
