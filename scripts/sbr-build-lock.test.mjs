import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { executeSbrHelperBuild } from "./build-sbr-helper.mjs";
import {
  acquireSbrBuildLock,
  enterSbrBuildOwnership,
  SBR_BUILD_LOCK_ENV,
} from "./sbr-build-lock.mjs";

test("ordinary helper builds reject a concurrent MAS owner and inherited children share ownership", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-sbr-owner-"));
  const owner = await acquireSbrBuildLock(root);
  try {
    await assert.rejects(
      executeSbrHelperBuild({ args: [], environment: {}, root }),
      /SBR_BUILD_LOCKED/,
    );
    const child = await enterSbrBuildOwnership(root, {
      [SBR_BUILD_LOCK_ENV]: owner.token,
    });
    await child.release();
    assert.equal(await readFile(owner.path, "utf8"), `${owner.token}\n`);
  } finally {
    await owner.release();
    await rm(root, { force: true, recursive: true });
  }
});

test("the exact owned lock directory is ignored by a real clean Git tree", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-sbr-clean-tree-"));
  try {
    const repositoryRoot = path.resolve(import.meta.dirname, "..");
    const repositoryIgnore = await readFile(path.join(repositoryRoot, ".gitignore"), "utf8");
    assert.equal(
      repositoryIgnore.split("\n").filter((line) => line === "/.tmp/sbr-build-owner/").length,
      1,
    );
    await writeFile(path.join(root, ".gitignore"), repositoryIgnore);
    const { execFile } = await import("node:child_process");
    const { promisify } = await import("node:util");
    const run = promisify(execFile);
    await run("git", ["init", "--quiet"], { cwd: root });
    await run("git", ["add", ".gitignore"], { cwd: root });
    await run(
      "git",
      [
        "-c",
        "user.name=Tammy Test",
        "-c",
        "user.email=test@tammy.invalid",
        "commit",
        "--quiet",
        "-m",
        "fixture",
      ],
      { cwd: root },
    );
    const owner = await acquireSbrBuildLock(root);
    try {
      const { stdout } = await run("git", ["status", "--porcelain", "--untracked-files=all"], {
        cwd: root,
      });
      assert.equal(stdout, "");
    } finally {
      await owner.release();
    }
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});
