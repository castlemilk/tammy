import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmod, lstat, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";

import { resolvePackagedLayout, verifyPackagedLayout } from "./find-packaged-app.mjs";

const sha256 = (value) => createHash("sha256").update(value).digest("hex");

async function withFixture(platform, arch, callback) {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-package-layout-"));
  const desktopRoot = path.join(root, "apps", "desktop");
  const layout = resolvePackagedLayout({ arch, desktopRoot, platform });
  const coreBytes = Buffer.from(`core:${platform}/${arch}`);
  const manifest = {
    schema: "tammy-build-manifest-v1",
    core_sha256: sha256(coreBytes),
  };
  await mkdir(path.dirname(layout.sourceCore), { recursive: true });
  await mkdir(path.dirname(layout.sourceManifest), { recursive: true });
  await mkdir(path.dirname(layout.appExecutable), { recursive: true });
  await mkdir(path.dirname(layout.packagedCore), { recursive: true });
  await mkdir(path.dirname(layout.packagedManifest), { recursive: true });
  await writeFile(path.join(layout.sourceCoreRoot, ".gitkeep"), "");
  await writeFile(path.join(layout.sourceBuildRoot, ".gitkeep"), "");
  await writeFile(path.join(layout.packagedCoreRoot, ".gitkeep"), "");
  await writeFile(path.join(layout.packagedBuildRoot, ".gitkeep"), "");
  await writeFile(layout.sourceCore, coreBytes);
  await writeFile(layout.packagedCore, coreBytes);
  await writeFile(layout.appExecutable, "application");
  const manifestBytes = Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`);
  await writeFile(layout.sourceManifest, manifestBytes);
  await writeFile(layout.packagedManifest, manifestBytes);
  if (platform === "darwin") {
    await chmod(layout.appExecutable, 0o755);
    await chmod(layout.sourceCore, 0o755);
    await chmod(layout.packagedCore, 0o755);
  }
  try {
    await callback({ desktopRoot, layout, manifestBytes });
  } finally {
    await rm(root, { force: true, recursive: true });
  }
}

test("resolves the exact macOS arm64 package paths", () => {
  const desktopRoot = path.resolve("/workspace/apps/desktop");
  assert.deepEqual(resolvePackagedLayout({ desktopRoot, platform: "darwin", arch: "arm64" }), {
    appExecutable: path.join(desktopRoot, "out/Tammy-darwin-arm64/Tammy.app/Contents/MacOS/Tammy"),
    packagedBuildRoot: path.join(
      desktopRoot,
      "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/build",
    ),
    packagedCore: path.join(
      desktopRoot,
      "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core",
    ),
    packagedCoreRoot: path.join(
      desktopRoot,
      "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/core",
    ),
    packagedManifest: path.join(
      desktopRoot,
      "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/build/build-manifest.json",
    ),
    sourceBuildRoot: path.join(desktopRoot, "resources/build"),
    sourceCore: path.join(desktopRoot, "resources/core/darwin-arm64/tammy-core"),
    sourceCoreRoot: path.join(desktopRoot, "resources/core"),
    sourceManifest: path.join(desktopRoot, "resources/build/build-manifest.json"),
    target: "darwin-arm64",
  });
});

test("resolves the exact Windows x64 package paths", () => {
  const desktopRoot = path.resolve("/workspace/apps/desktop");
  const layout = resolvePackagedLayout({ desktopRoot, platform: "win32", arch: "x64" });
  assert.equal(layout.appExecutable, path.join(desktopRoot, "out/Tammy-win32-x64/Tammy.exe"));
  assert.equal(
    layout.packagedCore,
    path.join(desktopRoot, "out/Tammy-win32-x64/resources/core/win32-x64/tammy-core.exe"),
  );
  assert.equal(
    layout.packagedManifest,
    path.join(desktopRoot, "out/Tammy-win32-x64/resources/build/build-manifest.json"),
  );
});

test("rejects unsupported targets, traversal tokens, and relative roots", () => {
  const desktopRoot = path.resolve("/workspace/apps/desktop");
  for (const [platform, arch] of [
    ["linux", "x64"],
    ["darwin", "x64"],
    ["win32", "arm64"],
    ["../darwin", "arm64"],
    ["darwin", "../arm64"],
  ]) {
    assert.throws(
      () => resolvePackagedLayout({ desktopRoot, platform, arch }),
      /UNSUPPORTED_PACKAGE_TARGET/,
    );
  }
  assert.throws(
    () => resolvePackagedLayout({ desktopRoot: "../desktop", platform: "darwin", arch: "arm64" }),
    /INVALID_DESKTOP_ROOT/,
  );
});

for (const [platform, arch] of [
  ["darwin", "arm64"],
  ["win32", "x64"],
]) {
  test(`verifies the exact ${platform}/${arch} source and packaged layout`, async () => {
    await withFixture(platform, arch, async ({ layout }) => {
      const result = await verifyPackagedLayout({
        arch,
        desktopRoot: path.dirname(path.dirname(layout.sourceCoreRoot)),
        platform,
        sourceManifestPath: layout.sourceManifest,
      });
      assert.deepEqual(result, {
        appExecutable: layout.appExecutable,
        appSha256: sha256("application"),
        coreExecutable: layout.packagedCore,
        coreSha256: sha256(`core:${platform}/${arch}`),
        manifest: layout.packagedManifest,
        manifestSha256: sha256(await readFile(layout.sourceManifest)),
        target: `${platform}-${arch}`,
      });
    });
  });
}

test("rejects missing files and a source manifest outside the exact build path", async () => {
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await rm(layout.appExecutable);
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGE_APP_INVALID/,
    );
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: path.join(desktopRoot, "../build-manifest.json"),
      }),
      /SOURCE_MANIFEST_PATH_INVALID/,
    );
  });
});

test("rejects extra and stale recursive resource entries", async () => {
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await mkdir(path.join(layout.sourceCoreRoot, "stale", "nested"), { recursive: true });
    await writeFile(path.join(layout.sourceCoreRoot, "stale", "nested", "core"), "stale");
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /SOURCE_CORE_LAYOUT_INVALID/,
    );
  });
  await withFixture("win32", "x64", async ({ desktopRoot, layout }) => {
    await writeFile(path.join(layout.packagedBuildRoot, "old-manifest.json"), "{}");
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "win32",
        arch: "x64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGED_BUILD_LAYOUT_INVALID/,
    );
  });
});

test("rejects symlinks, non-zero keep files, and non-regular executables", async () => {
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await rm(layout.packagedCore);
    await symlink(layout.sourceCore, layout.packagedCore);
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGED_CORE_LAYOUT_INVALID/,
    );
  });
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await writeFile(path.join(layout.sourceBuildRoot, ".gitkeep"), "\n");
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /SOURCE_BUILD_LAYOUT_INVALID/,
    );
  });
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await rm(layout.appExecutable);
    await mkdir(layout.appExecutable);
    assert.equal((await lstat(layout.appExecutable)).isDirectory(), true);
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGE_APP_INVALID/,
    );
  });
});

test("rejects manifest byte mismatch, core hash mismatch, and ASAR-contained core", async () => {
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await writeFile(layout.packagedManifest, "{}\n");
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGED_MANIFEST_MISMATCH/,
    );
  });
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await writeFile(layout.packagedCore, "tampered");
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGED_CORE_HASH_MISMATCH/,
    );
  });
  const desktopRoot = path.resolve("/workspace/apps/desktop");
  const layout = resolvePackagedLayout({ desktopRoot, platform: "darwin", arch: "arm64" });
  assert.equal(layout.packagedCore.includes(`${path.sep}app.asar${path.sep}`), false);
});

test("rejects a lost executable bit on macOS", async () => {
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await chmod(layout.packagedCore, 0o644);
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGED_CORE_NOT_EXECUTABLE/,
    );
  });
});
