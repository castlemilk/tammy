import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, it } from "node:test";

import {
  authenticateCachedSource,
  hashSourceTree,
  SQLCIPHER_RELEASE,
  validateArchiveEntries,
  validateReleasePin,
} from "./vendor-sqlcipher.mjs";

const temporaryDirectories = [];

afterEach(async () => {
  await Promise.all(
    temporaryDirectories
      .splice(0)
      .map((directory) => rm(directory, { force: true, recursive: true })),
  );
});

const EXPECTED_RELEASE = Object.freeze({
  archiveName: "sqlcipher-v4.15.0.tar.gz",
  rootDirectory: "sqlcipher-4.15.0",
  sha256: "21f5dfb2558a2a87740bb060ba75aadfec2e6119e08a87c3546c54751395a28d",
  sourceTreeSha256: "ab920a951726ede8da090ad26874f094966de373e9ed566e6e6dc500541920be",
  url: "https://codeload.github.com/sqlcipher/sqlcipher/tar.gz/refs/tags/v4.15.0",
  version: "4.15.0",
});

describe("SQLCipher release pin", () => {
  it("accepts only the exact official 4.15.0 archive identity", () => {
    assert.deepEqual(SQLCIPHER_RELEASE, EXPECTED_RELEASE);
    assert.deepEqual(validateReleasePin(EXPECTED_RELEASE), EXPECTED_RELEASE);
  });

  for (const [field, value] of [
    ["version", "4.14.0"],
    ["url", "https://example.invalid/sqlcipher.tar.gz"],
    ["sha256", "0".repeat(64)],
    ["sourceTreeSha256", "0".repeat(64)],
    ["archiveName", "sqlcipher.tar.gz"],
    ["rootDirectory", "sqlcipher-main"],
  ]) {
    it(`rejects release drift in ${field}`, () => {
      assert.throws(
        () => validateReleasePin({ ...EXPECTED_RELEASE, [field]: value }),
        /SQLCIPHER_RELEASE_PIN_INVALID/,
      );
    });
  }
});

describe("SQLCipher archive boundary", () => {
  it("accepts regular files and directories only below the pinned root", () => {
    assert.deepEqual(
      validateArchiveEntries([
        { path: "sqlcipher-4.15.0/", size: 0, type: "Directory" },
        { path: "sqlcipher-4.15.0/LICENSE.md", size: 1463, type: "File" },
        { path: "sqlcipher-4.15.0/src/sqlite.h.in", size: 8192, type: "File" },
      ]),
      [
        { path: "sqlcipher-4.15.0/", size: 0, type: "Directory" },
        { path: "sqlcipher-4.15.0/LICENSE.md", size: 1463, type: "File" },
        { path: "sqlcipher-4.15.0/src/sqlite.h.in", size: 8192, type: "File" },
      ],
    );
  });

  for (const entry of [
    { path: "", size: 0, type: "File" },
    { path: "../escape", size: 1, type: "File" },
    { path: "/absolute", size: 1, type: "File" },
    { path: "C:\\escape", size: 1, type: "File" },
    { path: "\\\\server\\share\\escape", size: 1, type: "File" },
    { path: "sqlcipher-4.15.0\\..\\escape", size: 1, type: "File" },
    { path: "sqlcipher-4.15.0/NUL\0name", size: 1, type: "File" },
    { path: "sqlcipher-4.15.0/control\u001fname", size: 1, type: "File" },
    { path: "sqlcipher-4.15.0/../../escape", size: 1, type: "File" },
    { path: "other-root/file", size: 1, type: "File" },
    { path: "sqlcipher-4.15.0/link", size: 0, type: "SymbolicLink" },
    { path: "sqlcipher-4.15.0/hard", size: 0, type: "Link" },
    { path: "sqlcipher-4.15.0/device", size: 0, type: "CharacterDevice" },
  ]) {
    it(`rejects unsafe ${entry.type} entry ${entry.path}`, () => {
      assert.throws(() => validateArchiveEntries([entry]), /SQLCIPHER_ARCHIVE_INVALID/);
    });
  }

  it("rejects oversized and duplicate archive entries", () => {
    assert.throws(
      () =>
        validateArchiveEntries([
          { path: "sqlcipher-4.15.0/a", size: 1, type: "File" },
          { path: "sqlcipher-4.15.0/a", size: 1, type: "File" },
        ]),
      /SQLCIPHER_ARCHIVE_INVALID/,
    );
    assert.throws(
      () =>
        validateArchiveEntries([
          {
            path: "sqlcipher-4.15.0/huge",
            size: 512 * 1024 * 1024 + 1,
            type: "File",
          },
        ]),
      /SQLCIPHER_ARCHIVE_INVALID/,
    );
    assert.throws(
      () =>
        validateArchiveEntries([
          { path: "sqlcipher-4.15.0/a/../same", size: 1, type: "File" },
          { path: "sqlcipher-4.15.0/same", size: 1, type: "File" },
        ]),
      /SQLCIPHER_ARCHIVE_INVALID/,
    );
    assert.throws(
      () =>
        validateArchiveEntries([
          { path: "sqlcipher-4.15.0/file", size: 1, type: "File" },
          { path: "sqlcipher-4.15.0/file/child", size: 1, type: "File" },
        ]),
      /SQLCIPHER_ARCHIVE_INVALID/,
    );
    assert.throws(
      () =>
        validateArchiveEntries([
          { path: "sqlcipher-4.15.0/file/child", size: 1, type: "File" },
          { path: "sqlcipher-4.15.0/file", size: 1, type: "File" },
        ]),
      /SQLCIPHER_ARCHIVE_INVALID/,
    );
  });

  it("bounds entry count and total uncompressed size", () => {
    assert.throws(
      () =>
        validateArchiveEntries(
          Array.from({ length: 10_001 }, (_, index) => ({
            path: `sqlcipher-4.15.0/file-${index}`,
            size: 0,
            type: "File",
          })),
        ),
      /SQLCIPHER_ARCHIVE_INVALID/,
    );
    assert.throws(
      () =>
        validateArchiveEntries([
          { path: "sqlcipher-4.15.0/a", size: 300 * 1024 * 1024, type: "File" },
          { path: "sqlcipher-4.15.0/b", size: 300 * 1024 * 1024, type: "File" },
        ]),
      /SQLCIPHER_ARCHIVE_INVALID/,
    );
  });

  it("uses POSIX archive paths regardless of host path rules", () => {
    assert.throws(
      () =>
        validateArchiveEntries([
          { path: path.posix.join("sqlcipher-4.15.0", "..", "escape"), size: 1, type: "File" },
        ]),
      /SQLCIPHER_ARCHIVE_INVALID/,
    );
  });
});

describe("SQLCipher cached source authentication", () => {
  it("rejects tampering in a non-license source file", async () => {
    const source = await mkdtemp(path.join(os.tmpdir(), "tammy-sqlcipher-tree-test-"));
    temporaryDirectories.push(source);
    await mkdir(path.join(source, "src"));
    const license = Buffer.from("canonical license\n");
    await Promise.all([
      writeFile(path.join(source, "LICENSE.md"), license),
      writeFile(path.join(source, "VERSION"), "3.53.0\n"),
      writeFile(path.join(source, "src/sqlite.c"), "authenticated source\n"),
    ]);
    const expectedHash = await hashSourceTree(source);
    assert.equal(await authenticateCachedSource(source, license, expectedHash), true);
    await writeFile(path.join(source, "src/sqlite.c"), "tampered source\n");
    assert.equal(await authenticateCachedSource(source, license, expectedHash), false);
  });
});
