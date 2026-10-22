import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmod, lstat, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";

import { resolvePackagedLayout, verifyPackagedLayout } from "./find-packaged-app.mjs";

const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const VERSION_PINS = {
  buf: "1.72.0",
  connect_es: "2.1.2",
  connect_go: "1.20.0",
  electron: "43.1.1",
  go: "1.26.4",
  node: "24.18.0",
  playwright: "1.61.1",
  pnpm: "11.15.0",
  protobuf_es: "2.12.1",
  protobuf_go: "1.36.11",
  react: "19.2.7",
  shadcn: "4.13.1",
  tailwindcss: "4.3.3",
  typescript: "7.0.2",
  vite: "8.1.5",
  vitest: "4.1.10",
};

function validManifest(target, coreSha256) {
  return {
    schema: "tammy-build-manifest-v1",
    source_revision: "a".repeat(40),
    source_dirty: false,
    target,
    versions: VERSION_PINS,
    lockfiles: {
      "pnpm-lock.yaml": "b".repeat(64),
      "services/core/go.sum": "c".repeat(64),
    },
    protobuf_tree_sha256: "d".repeat(64),
    core_sha256: coreSha256,
    sqlcipher: {
      library_sha256: "e".repeat(64),
      runtime_version: "4.15.0 community",
      version: "4.15.0",
    },
    test_profile: "foundation-packaged-e2e",
    sbr_status: "SIMULATOR_NOT_IMPLEMENTED",
    signed: false,
  };
}

function encodeAsar(header, payload = Buffer.alloc(0)) {
  const json = Buffer.from(JSON.stringify(header));
  const alignedJsonLength = Math.ceil(json.length / 4) * 4;
  const headerPayloadSize = 4 + alignedJsonLength;
  const headerPickle = Buffer.alloc(4 + headerPayloadSize);
  headerPickle.writeUInt32LE(headerPayloadSize, 0);
  headerPickle.writeInt32LE(json.length, 4);
  json.copy(headerPickle, 8);
  const sizePickle = Buffer.alloc(8);
  sizePickle.writeUInt32LE(4, 0);
  sizePickle.writeUInt32LE(headerPickle.length, 4);
  return Buffer.concat([sizePickle, headerPickle, payload]);
}

function coreAsarHeader(target, executable, unpacked) {
  const core = unpacked ? { size: 1, unpacked: true } : { offset: "0", size: 1 };
  return {
    files: {
      resources: {
        files: {
          core: {
            files: {
              [target]: {
                files: {
                  [executable]: core,
                },
              },
            },
          },
        },
      },
    },
  };
}

async function withFixture(platform, arch, callback) {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-package-layout-"));
  const desktopRoot = path.join(root, "apps", "desktop");
  const layout = resolvePackagedLayout({ arch, desktopRoot, platform });
  const coreBytes = Buffer.from(`core:${platform}/${arch}`);
  const manifestTarget = platform === "mas" ? `darwin-${arch}` : `${platform}-${arch}`;
  const manifest = validManifest(manifestTarget, sha256(coreBytes));
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
  await writeFile(
    layout.appAsar,
    encodeAsar(
      {
        files: {
          "package.json": { offset: "0", size: 2 },
        },
      },
      Buffer.from("{}"),
    ),
  );
  const manifestBytes = Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`);
  await writeFile(layout.sourceManifest, manifestBytes);
  await writeFile(layout.packagedManifest, manifestBytes);
  if (platform === "darwin" || platform === "mas") {
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
    appAsar: path.join(desktopRoot, "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/app.asar"),
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

test("resolves and verifies the exact Mac App Store arm64 package paths", async () => {
  const desktopRoot = path.resolve("/workspace/apps/desktop");
  const layout = resolvePackagedLayout({ desktopRoot, platform: "mas", arch: "arm64" });
  assert.equal(
    layout.appExecutable,
    path.join(desktopRoot, "out/Tammy-mas-arm64/Tammy.app/Contents/MacOS/Tammy"),
  );
  assert.equal(
    layout.packagedCore,
    path.join(
      desktopRoot,
      "out/Tammy-mas-arm64/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core",
    ),
  );
  assert.equal(layout.target, "darwin-arm64");

  await withFixture("mas", "arm64", async ({ desktopRoot: fixtureRoot, layout: fixture }) => {
    const result = await verifyPackagedLayout({
      arch: "arm64",
      desktopRoot: fixtureRoot,
      platform: "mas",
      sourceManifestPath: fixture.sourceManifest,
    });
    assert.equal(result.appExecutable, fixture.appExecutable);
    assert.equal(result.coreExecutable, fixture.packagedCore);
    assert.equal(result.target, "darwin-arm64");
  });
});

test("resolves the exact Windows x64 package paths", () => {
  const desktopRoot = path.resolve("/workspace/apps/desktop");
  const layout = resolvePackagedLayout({ desktopRoot, platform: "win32", arch: "x64" });
  assert.equal(layout.appExecutable, path.join(desktopRoot, "out/Tammy-win32-x64/Tammy.exe"));
  assert.equal(layout.appAsar, path.join(desktopRoot, "out/Tammy-win32-x64/resources/app.asar"));
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
});

test("rejects semantically invalid authenticated manifests", async () => {
  const mutations = [
    ["wrong target", (manifest) => ({ ...manifest, target: "win32-x64" })],
    ["signed", (manifest) => ({ ...manifest, signed: true })],
    ["unsupported SBR claim", (manifest) => ({ ...manifest, sbr_status: "SBR_APPROVED" })],
    ["extra field", (manifest) => ({ ...manifest, release_channel: "stable" })],
    ["credential field", (manifest) => ({ ...manifest, api_secret: "not-allowed" })],
    ["wrong type", (manifest) => ({ ...manifest, source_dirty: "false" })],
    [
      "missing version pin",
      (manifest) => {
        const { node: _node, ...versions } = manifest.versions;
        return { ...manifest, versions };
      },
    ],
    [
      "malformed lock hash",
      (manifest) => ({
        ...manifest,
        lockfiles: { ...manifest.lockfiles, "pnpm-lock.yaml": "bad" },
      }),
    ],
  ];
  for (const [name, mutate] of mutations) {
    await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
      const manifest = mutate(validManifest("darwin-arm64", sha256("core:darwin/arm64")));
      const bytes = Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`);
      await writeFile(layout.sourceManifest, bytes);
      await writeFile(layout.packagedManifest, bytes);
      await assert.rejects(
        verifyPackagedLayout({
          desktopRoot,
          platform: "darwin",
          arch: "arm64",
          sourceManifestPath: layout.sourceManifest,
        }),
        /SOURCE_MANIFEST_INVALID/,
        name,
      );
    });
  }
});

test("rejects duplicate-key and noncanonical manifest JSON", async () => {
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    const manifest = validManifest("darwin-arm64", sha256("core:darwin/arm64"));
    const canonical = JSON.stringify(manifest, null, 2);
    const duplicate = Buffer.from(
      `${canonical.replace(
        '  "schema": "tammy-build-manifest-v1",',
        '  "schema": "tammy-build-manifest-v1",\n  "schema": "tammy-build-manifest-v1",',
      )}\n`,
    );
    await writeFile(layout.sourceManifest, duplicate);
    await writeFile(layout.packagedManifest, duplicate);
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /SOURCE_MANIFEST_INVALID/,
    );
  });
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    const noncanonical = Buffer.from(
      JSON.stringify(validManifest("darwin-arm64", sha256("core:darwin/arm64"))),
    );
    await writeFile(layout.sourceManifest, noncanonical);
    await writeFile(layout.packagedManifest, noncanonical);
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /SOURCE_MANIFEST_INVALID/,
    );
  });
});

for (const [platform, arch, executable] of [
  ["darwin", "arm64", "tammy-core"],
  ["win32", "x64", "tammy-core.exe"],
]) {
  for (const unpacked of [false, true]) {
    test(`rejects ${unpacked ? "unpacked" : "packed"} ${platform}/${arch} core metadata inside app.asar`, async () => {
      await withFixture(platform, arch, async ({ desktopRoot, layout }) => {
        await writeFile(
          layout.appAsar,
          encodeAsar(
            coreAsarHeader(`${platform}-${arch}`, executable, unpacked),
            unpacked ? Buffer.alloc(0) : Buffer.from("x"),
          ),
        );
        await assert.rejects(
          verifyPackagedLayout({
            desktopRoot,
            platform,
            arch,
            sourceManifestPath: layout.sourceManifest,
          }),
          /PACKAGED_CORE_INSIDE_ASAR/,
        );
      });
    });
  }
}

test("rejects malformed, truncated, and oversized ASAR headers", async () => {
  for (const bytes of [
    Buffer.from("not-an-asar"),
    Buffer.from([4, 0, 0, 0, 64, 0, 0, 0]),
    Buffer.from([4, 0, 0, 0, 255, 255, 255, 127]),
  ]) {
    await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
      await writeFile(layout.appAsar, bytes);
      await assert.rejects(
        verifyPackagedLayout({
          desktopRoot,
          platform: "darwin",
          arch: "arm64",
          sourceManifestPath: layout.sourceManifest,
        }),
        /PACKAGE_ASAR_INVALID/,
      );
    });
  }
});

test("rejects a non-regular or symlinked app.asar", async () => {
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await rm(layout.appAsar);
    await mkdir(layout.appAsar);
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGE_ASAR_INVALID/,
    );
  });
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    const target = path.join(path.dirname(layout.appAsar), "valid.asar");
    await writeFile(target, encodeAsar({ files: {} }));
    await rm(layout.appAsar);
    await symlink(target, layout.appAsar);
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGE_ASAR_INVALID/,
    );
  });
});

test("rejects noncanonical or escaping ASAR link targets", async () => {
  for (const link of [
    "/absolute/target",
    String.raw`..\windows\escape`,
    "../parent",
    "directory/../escape",
    "./relative",
    "directory/./file",
  ]) {
    await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
      await writeFile(
        layout.appAsar,
        encodeAsar({
          files: {
            linked: { link },
          },
        }),
      );
      await assert.rejects(
        verifyPackagedLayout({
          desktopRoot,
          platform: "darwin",
          arch: "arm64",
          sourceManifestPath: layout.sourceManifest,
        }),
        /PACKAGE_ASAR_INVALID/,
      );
    });
  }
});

test("rejects an abusive ASAR packed offset without unbounded integer parsing", async () => {
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await writeFile(
      layout.appAsar,
      encodeAsar({
        files: {
          abusive: { offset: "9".repeat(100_000), size: 0 },
        },
      }),
    );
    await assert.rejects(
      verifyPackagedLayout({
        desktopRoot,
        platform: "darwin",
        arch: "arm64",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGE_ASAR_INVALID/,
    );
  });
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

test("rejects a resource tree changed before final identity revalidation", async () => {
  await withFixture("darwin", "arm64", async ({ desktopRoot, layout }) => {
    await assert.rejects(
      verifyPackagedLayout({
        arch: "arm64",
        beforeTreeRevalidation: async () => {
          await writeFile(path.join(layout.packagedCoreRoot, "late-entry"), "late");
        },
        desktopRoot,
        platform: "darwin",
        sourceManifestPath: layout.sourceManifest,
      }),
      /PACKAGED_CORE_LAYOUT_CHANGED/,
    );
  });
});
