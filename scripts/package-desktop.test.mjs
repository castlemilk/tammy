import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { createDesktopPackagePlan, executeDesktopPackage } from "./package-desktop.mjs";
import { SBR_BUILD_LOCK_ENV } from "./sbr-build-lock.mjs";

test("ordinary packaging holds one owner across helper staging, manifest, package and verification", async () => {
  const root = path.resolve(import.meta.dirname, "..");
  const packageJson = JSON.parse(await readFile(path.join(root, "package.json"), "utf8"));
  assert.equal(packageJson.scripts["desktop:package"], "node scripts/package-desktop.mjs");
  assert.deepEqual(
    createDesktopPackagePlan(root).map(({ command, args }) => [command, ...args]),
    [
      ["pnpm", "core:build"],
      ["pnpm", "sbr-helper:build"],
      ["pnpm", "build:manifest"],
      ["pnpm", "--dir", "apps/desktop", "package"],
      [
        process.execPath,
        "--test",
        path.join(root, "apps/desktop/tests/e2e/package-signature.test.mjs"),
      ],
      [
        process.execPath,
        path.join(root, "apps/desktop/scripts/find-packaged-app.mjs"),
        "--verify",
        "--source-manifest",
        path.join(root, "apps/desktop/resources/build/build-manifest.json"),
      ],
    ],
  );
});

test("ordinary package execution keeps one inherited owner through every command", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-desktop-package-owner-"));
  const tokens = [];
  try {
    await executeDesktopPackage(root, async (_command, _args, options) => {
      const token = options.env[SBR_BUILD_LOCK_ENV];
      assert.equal(
        await readFile(path.join(root, ".tmp/sbr-build-owner/owner.lock"), "utf8"),
        `${token}\n`,
      );
      tokens.push(token);
    });
    assert.equal(tokens.length, 6);
    assert.equal(new Set(tokens).size, 1);
    await assert.rejects(readFile(path.join(root, ".tmp/sbr-build-owner/owner.lock")), /ENOENT/);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});
