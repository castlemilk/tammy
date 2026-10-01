import assert from "node:assert/strict";
import { lstat, mkdtemp, readFile, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { parse } from "yaml";

import {
  createDesktopPackagePlan,
  executeDesktopPackage,
  ordinaryBuilderOutputRoot,
} from "./package-desktop.mjs";
import { SBR_BUILD_LOCK_ENV } from "./sbr-build-lock.mjs";

test("ordinary packaging holds one owner across helper staging, manifest, package and verification", async () => {
  const root = path.resolve(import.meta.dirname, "..");
  const packageJson = JSON.parse(await readFile(path.join(root, "package.json"), "utf8"));
  const builderConfig = parse(
    await readFile(path.join(root, "apps/desktop/electron-builder.yml"), "utf8"),
  );
  assert.equal(packageJson.scripts["desktop:package"], "node scripts/package-desktop.mjs");
  assert.equal(builderConfig.directories.output, "${env.TAMMY_ELECTRON_BUILDER_OUTPUT}");
  assert.deepEqual(builderConfig.mac.target, [{ target: "dir", arch: ["arm64"] }]);
  assert.deepEqual(builderConfig.win.target, [{ target: "dir", arch: ["x64"] }]);
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
      assert.equal(options.env.TAMMY_ELECTRON_BUILDER_OUTPUT, ordinaryBuilderOutputRoot(root));
      tokens.push(token);
    });
    assert.equal(tokens.length, 6);
    assert.equal(new Set(tokens).size, 1);
    await assert.rejects(readFile(path.join(root, ".tmp/sbr-build-owner/owner.lock")), /ENOENT/);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("ordinary output is an exact real directory", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-desktop-package-output-"));
  try {
    const output = ordinaryBuilderOutputRoot(root);
    await executeDesktopPackage(root, async () => {});
    const outputStats = await lstat(output);
    assert.equal(outputStats.isDirectory(), true);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("ordinary output rejects a symlink before Builder can overwrite it", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-desktop-package-output-link-"));
  const external = await mkdtemp(path.join(tmpdir(), "tammy-desktop-package-external-"));
  try {
    const output = ordinaryBuilderOutputRoot(root);
    await import("node:fs/promises").then(({ mkdir }) =>
      mkdir(path.dirname(output), { recursive: true }),
    );
    await symlink(external, output);
    await assert.rejects(
      executeDesktopPackage(root, async () => {}),
      /DESKTOP_PACKAGE_OUTPUT_INVALID/,
    );
    assert.equal((await lstat(output)).isSymbolicLink(), true);
    assert.equal((await lstat(external)).isDirectory(), true);
  } finally {
    await rm(root, { force: true, recursive: true });
    await rm(external, { force: true, recursive: true });
  }
});
