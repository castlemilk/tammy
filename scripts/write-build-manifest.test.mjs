import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { lstat, mkdir, mkdtemp, readdir, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";

import {
  collectBuildManifest,
  createBuildManifest,
  hashProtoTree,
  selectCiMode,
  writeBuildManifest,
} from "./write-build-manifest.mjs";

const hash = (value) => createHash("sha256").update(value).digest("hex");
const ZERO_HASH = "0".repeat(64);
const ONE_HASH = "1".repeat(64);

const versions = {
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

const lockfiles = {
  "pnpm-lock.yaml": ZERO_HASH,
  "services/core/go.sum": ONE_HASH,
};

function validInput(overrides = {}) {
  return {
    ciMode: false,
    coreSha256: "2".repeat(64),
    lockfiles,
    protobufTreeSha256: "3".repeat(64),
    sourceDirty: false,
    sourceRevision: "a".repeat(40),
    target: "darwin-arm64",
    versions,
    ...overrides,
  };
}

test("creates the exact stable manifest fields and ordering", () => {
  const manifest = createBuildManifest(validInput());
  assert.deepEqual(manifest, {
    schema: "tammy-build-manifest-v1",
    source_revision: "a".repeat(40),
    source_dirty: false,
    target: "darwin-arm64",
    versions,
    lockfiles,
    protobuf_tree_sha256: "3".repeat(64),
    core_sha256: "2".repeat(64),
    test_profile: "foundation-packaged-e2e",
    sbr_status: "SIMULATOR_NOT_IMPLEMENTED",
    signed: false,
  });
  assert.equal(
    `${JSON.stringify(manifest, null, 2)}\n`,
    `${JSON.stringify(
      {
        schema: "tammy-build-manifest-v1",
        source_revision: "a".repeat(40),
        source_dirty: false,
        target: "darwin-arm64",
        versions,
        lockfiles,
        protobuf_tree_sha256: "3".repeat(64),
        core_sha256: "2".repeat(64),
        test_profile: "foundation-packaged-e2e",
        sbr_status: "SIMULATOR_NOT_IMPLEMENTED",
        signed: false,
      },
      null,
      2,
    )}\n`,
  );
});

test("normalizes nested pin maps into stable key order", () => {
  const reverseVersions = Object.fromEntries(Object.entries(versions).reverse());
  const reverseLocks = Object.fromEntries(Object.entries(lockfiles).reverse());
  const manifest = createBuildManifest(
    validInput({ versions: reverseVersions, lockfiles: reverseLocks }),
  );
  assert.deepEqual(Object.keys(manifest.versions), Object.keys(versions));
  assert.deepEqual(Object.keys(manifest.lockfiles), Object.keys(lockfiles));
});

test("rejects dirty CI source and unsupported targets", () => {
  assert.throws(
    () => createBuildManifest(validInput({ ciMode: true, sourceDirty: true })),
    /DIRTY_SOURCE_IN_CI/,
  );
  for (const target of ["linux-x64", "darwin-x64", "win32-arm64", "../darwin-arm64"]) {
    assert.throws(() => createBuildManifest(validInput({ target })), /UNSUPPORTED_MANIFEST_TARGET/);
  }
});

test("selects CI mode from only the exact CI flag", () => {
  assert.equal(selectCiMode({ CI: "true", SECRET_TOKEN: "ignored" }), true);
  assert.equal(selectCiMode({ CI: "false" }), false);
  assert.equal(selectCiMode({}), false);
});

test("rejects malformed source revisions and hashes", () => {
  assert.throws(
    () => createBuildManifest(validInput({ sourceRevision: "abc" })),
    /INVALID_SOURCE_REVISION/,
  );
  for (const [field, value] of [
    ["coreSha256", "g".repeat(64)],
    ["protobufTreeSha256", "0".repeat(63)],
  ]) {
    assert.throws(
      () => createBuildManifest(validInput({ [field]: value })),
      /INVALID_MANIFEST_HASH/,
    );
  }
  assert.throws(
    () =>
      createBuildManifest(
        validInput({
          lockfiles: { ...lockfiles, "pnpm-lock.yaml": "bad" },
        }),
      ),
    /INVALID_MANIFEST_HASH/,
  );
});

test("rejects missing, extra, or unpinned version and lock entries", () => {
  const { node: _node, ...missingVersion } = versions;
  assert.throws(
    () => createBuildManifest(validInput({ versions: missingVersion })),
    /MANIFEST_PINS_INVALID/,
  );
  assert.throws(
    () =>
      createBuildManifest(
        validInput({ versions: { ...versions, experimental_runtime: "latest" } }),
      ),
    /MANIFEST_PINS_INVALID/,
  );
  assert.throws(
    () => createBuildManifest(validInput({ versions: { ...versions, node: "^24.18.0" } })),
    /MANIFEST_PINS_INVALID/,
  );
  assert.throws(
    () => createBuildManifest(validInput({ lockfiles: { "pnpm-lock.yaml": ZERO_HASH } })),
    /MANIFEST_LOCKFILES_INVALID/,
  );
});

test("rejects credential, secret, token, password, or environment fields", () => {
  for (const field of ["credentials", "api_secret", "access_token", "password", "environment"]) {
    assert.throws(
      () => createBuildManifest({ ...validInput(), [field]: "forbidden" }),
      /FORBIDDEN_MANIFEST_FIELD/,
    );
  }
  assert.throws(
    () =>
      createBuildManifest(validInput({ versions: { ...versions, credential_helper: "1.0.0" } })),
    /FORBIDDEN_MANIFEST_FIELD|MANIFEST_PINS_INVALID/,
  );
});

test("hashes a sorted protobuf tree with path boundaries", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-proto-hash-"));
  try {
    await mkdir(path.join(root, "tammy", "v1"), { recursive: true });
    await writeFile(path.join(root, "z.proto"), "z");
    await writeFile(path.join(root, "tammy", "v1", "a.proto"), "a");
    const expected = createHash("sha256")
      .update("tammy/v1/a.proto\0")
      .update("a")
      .update("\0")
      .update("z.proto\0")
      .update("z")
      .update("\0")
      .digest("hex");
    assert.equal(await hashProtoTree(root), expected);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("rejects symlinks and an empty protobuf tree", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-proto-invalid-"));
  try {
    await assert.rejects(hashProtoTree(root), /PROTOBUF_TREE_INVALID/);
    const outside = path.join(path.dirname(root), `${path.basename(root)}-outside.proto`);
    await writeFile(outside, "outside");
    await symlink(outside, path.join(root, "linked.proto"));
    await assert.rejects(hashProtoTree(root), /PROTOBUF_TREE_INVALID/);
    await rm(outside);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("cleans build staging and atomically writes canonical JSON", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-manifest-write-"));
  const buildRoot = path.join(root, "resources", "build");
  try {
    await mkdir(path.join(buildRoot, "stale", "nested"), { recursive: true });
    await writeFile(path.join(buildRoot, ".gitkeep"), "");
    await writeFile(path.join(buildRoot, "stale", "nested", "old.json"), "{}");
    await writeFile(path.join(buildRoot, "old-manifest.json"), "{}");
    const result = await writeBuildManifest({
      buildRoot,
      manifest: createBuildManifest(validInput()),
    });
    assert.equal(result, path.join(buildRoot, "build-manifest.json"));
    assert.deepEqual((await readdir(buildRoot)).sort(), [".gitkeep", "build-manifest.json"]);
    assert.equal(
      await readFile(result, "utf8"),
      `${JSON.stringify(createBuildManifest(validInput()), null, 2)}\n`,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("removes the temporary file when atomic rename fails", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-manifest-failure-"));
  const buildRoot = path.join(root, "resources", "build");
  try {
    await mkdir(buildRoot, { recursive: true });
    await writeFile(path.join(buildRoot, ".gitkeep"), "");
    await assert.rejects(
      writeBuildManifest({
        buildRoot,
        manifest: createBuildManifest(validInput()),
        renameFile: async () => {
          throw new Error("injected rename failure");
        },
      }),
      /MANIFEST_WRITE_FAILED/,
    );
    assert.deepEqual(await readdir(buildRoot), [".gitkeep"]);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("rejects an invalid build staging keep file", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-manifest-keep-"));
  const buildRoot = path.join(root, "resources", "build");
  try {
    await mkdir(buildRoot, { recursive: true });
    await writeFile(path.join(buildRoot, ".gitkeep"), "\n");
    assert.equal((await lstat(path.join(buildRoot, ".gitkeep"))).size, 1);
    await assert.rejects(
      writeBuildManifest({
        buildRoot,
        manifest: createBuildManifest(validInput()),
      }),
      /BUILD_STAGING_INVALID/,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("collects only committed pins, fixed git commands, and authenticated hashes", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-manifest-collect-"));
  const calls = [];
  try {
    await mkdir(path.join(root, "apps", "desktop", "resources", "core", "darwin-arm64"), {
      recursive: true,
    });
    await mkdir(path.join(root, "services", "core"), { recursive: true });
    await mkdir(path.join(root, "proto"), { recursive: true });
    await writeFile(
      path.join(root, "package.json"),
      JSON.stringify({
        packageManager: "pnpm@11.15.0",
        engines: { node: "24.18.0", pnpm: "11.15.0" },
        devDependencies: {
          "@bufbuild/buf": "1.72.0",
          "@bufbuild/protoc-gen-es": "2.12.1",
          typescript: "7.0.2",
        },
      }),
    );
    await writeFile(
      path.join(root, "apps", "desktop", "package.json"),
      JSON.stringify({
        dependencies: {
          "@connectrpc/connect": "2.1.2",
          "@bufbuild/protobuf": "2.12.1",
          react: "19.2.7",
        },
        devDependencies: {
          "@playwright/test": "1.61.1",
          electron: "43.1.1",
          shadcn: "4.13.1",
          tailwindcss: "4.3.3",
          vite: "8.1.5",
          vitest: "4.1.10",
        },
      }),
    );
    await writeFile(
      path.join(root, "services", "core", "go.mod"),
      "module example\n\ngo 1.26.4\n\nrequire (\n\tconnectrpc.com/connect v1.20.0\n\tgoogle.golang.org/protobuf v1.36.11\n)\n",
    );
    await writeFile(path.join(root, "pnpm-lock.yaml"), "pnpm lock");
    await writeFile(path.join(root, "services", "core", "go.sum"), "go sum");
    await writeFile(path.join(root, "proto", "system.proto"), 'syntax = "proto3";\n');
    await writeFile(
      path.join(root, "apps", "desktop", "resources", "core", "darwin-arm64", "tammy-core"),
      "core",
    );
    const commandRunner = async (command, args, options) => {
      calls.push({ command, args, options });
      return args[0] === "rev-parse" ? `${"b".repeat(40)}\n` : "";
    };
    const manifest = await collectBuildManifest({
      arch: "arm64",
      commandRunner,
      platform: "darwin",
      root,
    });
    assert.equal(manifest.source_revision, "b".repeat(40));
    assert.equal(manifest.source_dirty, false);
    assert.equal(manifest.core_sha256, hash("core"));
    assert.equal(manifest.lockfiles["pnpm-lock.yaml"], hash("pnpm lock"));
    assert.equal(manifest.lockfiles["services/core/go.sum"], hash("go sum"));
    assert.deepEqual(calls, [
      {
        command: "git",
        args: ["rev-parse", "HEAD"],
        options: { cwd: root, encoding: "utf8", shell: false, windowsHide: true },
      },
      {
        command: "git",
        args: ["status", "--porcelain", "--untracked-files=normal"],
        options: { cwd: root, encoding: "utf8", shell: false, windowsHide: true },
      },
    ]);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});
