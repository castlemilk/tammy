import assert from "node:assert/strict";
import { createHash, createPrivateKey, sign } from "node:crypto";
import {
  lstat,
  mkdir,
  mkdtemp,
  readdir,
  readFile,
  realpath,
  rename,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { hashSbrHelperSourceTree } from "./build-sbr-helper.mjs";
import { canonicalizeSbrProfile } from "./sbr-profile-schema.mjs";
import {
  collectBuildManifest,
  createBuildManifest,
  hashProtoTree,
  rehashSignedBuildManifest,
  runBoundedCommand,
  sanitizeProvenanceEnvironment,
  selectCiMode,
  writeBuildManifest,
} from "./write-build-manifest.mjs";

const hash = (value) => createHash("sha256").update(value).digest("hex");
const ZERO_HASH = "0".repeat(64);
const ONE_HASH = "1".repeat(64);
const sbr = {
  helper_sha256: "6".repeat(64),
  profile_sha256: "7".repeat(64),
  profile_signature_sha256: "8".repeat(64),
  source_tree_sha256: "9".repeat(64),
};

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

function expectedTreeHash(entries) {
  const ordered = [...entries].sort(([left], [right]) =>
    Buffer.compare(Buffer.from(left), Buffer.from(right)),
  );
  const digest = createHash("sha256");
  digest.update("tammy-protobuf-tree-v1\0");
  const count = Buffer.alloc(4);
  count.writeUInt32BE(ordered.length);
  digest.update(count);
  for (const [name, contents] of ordered) {
    const nameBytes = Buffer.from(name);
    const nameLength = Buffer.alloc(4);
    nameLength.writeUInt32BE(nameBytes.length);
    const contentBytes = Buffer.from(contents);
    const contentLength = Buffer.alloc(8);
    contentLength.writeBigUInt64BE(BigInt(contentBytes.length));
    digest.update(nameLength);
    digest.update(nameBytes);
    digest.update(contentLength);
    digest.update(createHash("sha256").update(contentBytes).digest());
  }
  return digest.digest("hex");
}

function validInput(overrides = {}) {
  return {
    ciMode: false,
    coreSha256: "2".repeat(64),
    lockfiles,
    protobufTreeSha256: "3".repeat(64),
    sourceDirty: false,
    sourceRevision: "a".repeat(40),
    sbr,
    sqlcipher: {
      librarySha256: "4".repeat(64),
      runtimeVersion: "4.15.0 community",
      version: "4.15.0",
    },
    target: "darwin-arm64",
    versions,
    ...overrides,
  };
}

async function writeSqlcipherManifestFixture(root, target = "darwin-arm64") {
  const resourceRoot = path.join(root, "apps/desktop/resources/sqlcipher");
  const library = Buffer.from(`sqlcipher static ${target}`);
  const librarySha256 = hash(library);
  const header = Buffer.from(`sqlcipher header ${target}`);
  const headerSha256 = hash(header);
  const license = "pinned sqlcipher license\n";
  await Promise.all([
    mkdir(path.join(resourceRoot, target, "lib"), { recursive: true }),
    mkdir(path.join(resourceRoot, target, "include"), { recursive: true }),
  ]);
  await mkdir(path.join(root, "third_party/sqlcipher"), { recursive: true });
  await Promise.all([
    writeFile(path.join(resourceRoot, "VERSION"), "4.15.0\n"),
    writeFile(path.join(resourceRoot, "LICENSE"), license),
    writeFile(path.join(root, "third_party/sqlcipher/LICENSE"), license),
    writeFile(path.join(resourceRoot, target, "lib/libsqlite3.a"), library),
    writeFile(path.join(resourceRoot, target, "include/sqlite3.h"), header),
    writeFile(path.join(resourceRoot, target, "HEADER_SHA256"), `${headerSha256}\n`),
    writeFile(path.join(resourceRoot, target, "LIBRARY_SHA256"), `${librarySha256}\n`),
  ]);
  return librarySha256;
}

async function writeSbrManifestFixture(root, sourceRevision = "a".repeat(40)) {
  const helper = path.join(root, "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper");
  const profilePath = path.join(root, "apps/desktop/resources/sbr/simulator/sbr-profile-v1.json");
  const signaturePath = path.join(root, "apps/desktop/resources/sbr/simulator/sbr-profile-v1.sig");
  const publicKeyPath = path.join(root, "config/sbr/simulator/profile-public-key.pem");
  const source = path.join(root, "services/sbr-helper/main.go");
  const helperBytes = Buffer.from("helper");
  const profile = {
    component_manifest_sha256: "NONE",
    endpoint_profile_sha256: "NONE",
    environment: "SIMULATOR",
    expires_at: "2030-01-01T00:00:00Z",
    helper_sha256: hash(helperBytes),
    issued_at: "2026-08-01T00:00:00Z",
    registration_manifest_sha256: "NONE",
    schema_version: 1,
    target: "darwin/arm64",
  };
  const privateKey = createPrivateKey({
    format: "der",
    key: Buffer.from(
      "302e020100300506032b6570042204209d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60",
      "hex",
    ),
    type: "pkcs8",
  });
  await Promise.all([
    mkdir(path.dirname(helper), { recursive: true }),
    mkdir(path.dirname(profilePath), { recursive: true }),
    mkdir(path.dirname(publicKeyPath), { recursive: true }),
    mkdir(path.dirname(source), { recursive: true }),
    mkdir(path.join(root, ".tmp/sbr-helper-build"), { recursive: true }),
  ]);
  await Promise.all([
    writeFile(helper, helperBytes),
    writeFile(source, "package main\n"),
    writeFile(profilePath, `${JSON.stringify(profile, null, 2)}\n`),
    writeFile(
      signaturePath,
      `${sign(null, canonicalizeSbrProfile(profile, { now: new Date("2026-08-01T00:00:00Z") }), privateKey).toString("base64")}\n`,
    ),
    writeFile(publicKeyPath, await readFile("config/sbr/simulator/profile-public-key.pem")),
  ]);
  const provenance = {
    helper_raw_sha256: "5".repeat(64),
    helper_sha256: hash(helperBytes),
    profile_sha256: hash(await readFile(profilePath)),
    profile_signature_sha256: hash(await readFile(signaturePath)),
    session_nonce: "2".repeat(32),
    source_revision: sourceRevision,
    source_tree_sha256: await hashSbrHelperSourceTree(path.join(root, "services/sbr-helper")),
    status: "SIMULATOR_ENABLED",
    target: "darwin-arm64",
  };
  await writeFile(
    path.join(root, ".tmp/sbr-helper-build/provenance.json"),
    `${JSON.stringify(provenance, null, 2)}\n`,
  );
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
    sqlcipher: {
      library_sha256: "4".repeat(64),
      runtime_version: "4.15.0 community",
      version: "4.15.0",
    },
    test_profile: "foundation-packaged-e2e",
    sbr_status: "SIMULATOR_ENABLED",
    sbr,
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
        sqlcipher: {
          library_sha256: "4".repeat(64),
          runtime_version: "4.15.0 community",
          version: "4.15.0",
        },
        test_profile: "foundation-packaged-e2e",
        sbr_status: "SIMULATOR_ENABLED",
        sbr,
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

test("rehashes a signed core without executing it or changing authenticated provenance", async () => {
  const root = await realpath(await mkdtemp(path.join(tmpdir(), "tammy-signed-manifest-")));
  try {
    const buildRoot = path.join(root, "apps/desktop/resources/build");
    const core = path.join(root, "apps/desktop/resources/core/darwin-arm64/tammy-core");
    await mkdir(buildRoot, { recursive: true });
    await mkdir(path.dirname(core), { recursive: true });
    await writeFile(path.join(buildRoot, ".gitkeep"), "");
    await writeFile(core, "signed-core");
    await writeFile(
      path.join(buildRoot, "build-manifest.json"),
      `${JSON.stringify(createBuildManifest(validInput()), null, 2)}\n`,
    );
    const calls = [];
    const manifest = await rehashSignedBuildManifest({
      arch: "arm64",
      commandRunner: async (command, args) => {
        calls.push([command, ...args]);
        if (args[1] === "--show-toplevel") return `${root}\n`;
        if (args[0] === "rev-parse") return `${"a".repeat(40)}\n`;
        if (args[0] === "status") return "";
        throw new Error("unexpected command");
      },
      platform: "darwin",
      root,
    });
    assert.equal(manifest.core_sha256, hash("signed-core"));
    assert.deepEqual(manifest.sqlcipher, createBuildManifest(validInput()).sqlcipher);
    assert.equal(
      calls.some((call) => call.includes("--sqlcipher-status")),
      false,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
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

test("rejects SQLCipher version, runtime, hash, or shape drift", () => {
  for (const sqlcipher of [
    { librarySha256: "bad", runtimeVersion: "4.15.0 community", version: "4.15.0" },
    { librarySha256: "4".repeat(64), runtimeVersion: "4.15.0", version: "4.15.0" },
    { librarySha256: "4".repeat(64), runtimeVersion: "4.15.0 community", version: "4.14.0" },
    {
      extra: true,
      librarySha256: "4".repeat(64),
      runtimeVersion: "4.15.0 community",
      version: "4.15.0",
    },
  ]) {
    assert.throws(
      () => createBuildManifest(validInput({ sqlcipher })),
      /MANIFEST_SQLCIPHER_INVALID|INVALID_MANIFEST_HASH/,
    );
  }
});

test("selects CI mode from only the exact CI flag", () => {
  assert.equal(selectCiMode({ CI: "true", SECRET_TOKEN: "ignored" }), true);
  assert.equal(selectCiMode({ CI: "false" }), false);
  assert.equal(selectCiMode({}), false);
});

test("sanitizes Git redirection and stabilizes command locale", () => {
  assert.deepEqual(
    sanitizeProvenanceEnvironment({
      GIT_COMMON_DIR: "/redirect/common",
      GIT_DIR: "/redirect/git",
      GIT_WORK_TREE: "/redirect/worktree",
      HOME: "/home/tammy",
      PATH: "/tools",
      SECRET_TOKEN: "must-not-be-forwarded",
      SYSTEMROOT: "C:\\Windows",
    }),
    {
      HOME: "/home/tammy",
      LANG: "C",
      LC_ALL: "C",
      PATH: "/tools",
      SYSTEMROOT: "C:\\Windows",
    },
  );
});

test("bounds command execution and settles only after the child callback", async () => {
  let callbacks = 0;
  const execFile = (_command, _args, options, callback) => {
    options.signal.addEventListener(
      "abort",
      () => {
        setImmediate(() => {
          callbacks += 1;
          callback(new Error("aborted"), "", "");
        });
      },
      { once: true },
    );
  };
  await assert.rejects(
    runBoundedCommand(
      "git",
      ["status"],
      {
        cwd: path.resolve("/workspace"),
        encoding: "utf8",
        env: { LANG: "C", LC_ALL: "C" },
        shell: false,
        windowsHide: true,
      },
      { execFile, timeoutMs: 5 },
    ),
    /PROVENANCE_COMMAND_TIMEOUT/,
  );
  assert.equal(callbacks, 1);
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
    const expected = expectedTreeHash([
      ["tammy/v1/a.proto", "a"],
      ["z.proto", "z"],
    ]);
    assert.equal(await hashProtoTree(root), expected);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("does not collide when content contains the former path delimiter encoding", async () => {
  const first = await mkdtemp(path.join(tmpdir(), "tammy-proto-collision-a-"));
  const second = await mkdtemp(path.join(tmpdir(), "tammy-proto-collision-b-"));
  try {
    await writeFile(path.join(first, "a.proto"), "x\0b.proto\0");
    await writeFile(path.join(second, "a.proto"), "x");
    await writeFile(path.join(second, "b.proto"), "");
    assert.notEqual(await hashProtoTree(first), await hashProtoTree(second));
  } finally {
    await rm(first, { force: true, recursive: true });
    await rm(second, { force: true, recursive: true });
  }
});

test("orders non-ASCII protobuf paths by UTF-8 bytes independent of locale", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-proto-utf8-"));
  try {
    const entries = [
      ["é.proto", "e-acute"],
      ["z.proto", "zed"],
      ["ä.proto", "a-umlaut"],
      ["a.proto", "ascii"],
    ];
    for (const [name, contents] of entries) {
      await writeFile(path.join(root, name), contents);
    }
    assert.equal(await hashProtoTree(root), expectedTreeHash(entries));
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("rejects a protobuf tree changed after a file is hashed", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-proto-swap-"));
  try {
    const file = path.join(root, "system.proto");
    await writeFile(file, "before");
    await assert.rejects(
      hashProtoTree(root, {
        afterFileHashed: async () => {
          await writeFile(file, "after!");
        },
      }),
      /PROTOBUF_TREE_CHANGED/,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("rejects a protobuf root replacement even when the file identity is preserved", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-proto-root-swap-"));
  const moved = `${root}-moved`;
  try {
    await writeFile(path.join(root, "system.proto"), "unchanged");
    await assert.rejects(
      hashProtoTree(root, {
        afterFileHashed: async () => {
          await rename(root, moved);
          await mkdir(root);
          await rename(path.join(moved, "system.proto"), path.join(root, "system.proto"));
        },
      }),
      /PROTOBUF_TREE_CHANGED/,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
    await rm(moved, { force: true, recursive: true });
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
    await writeFile(path.join(buildRoot, ".build-manifest.json.tmp"), "stale");
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

test("excludes a concurrent manifest publisher with a bounded staging lock", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-manifest-lock-"));
  const buildRoot = path.join(root, "resources", "build");
  let enteredRename;
  let releaseRename;
  const renameEntered = new Promise((resolve) => {
    enteredRename = resolve;
  });
  const renameReleased = new Promise((resolve) => {
    releaseRename = resolve;
  });
  try {
    await mkdir(buildRoot, { recursive: true });
    await writeFile(path.join(buildRoot, ".gitkeep"), "");
    const first = writeBuildManifest({
      buildRoot,
      manifest: createBuildManifest(validInput()),
      renameFile: async (source, destination) => {
        enteredRename();
        await renameReleased;
        await rename(source, destination);
      },
    });
    await renameEntered;
    await assert.rejects(
      writeBuildManifest({
        buildRoot,
        manifest: createBuildManifest(validInput()),
      }),
      /BUILD_STAGING_LOCKED/,
    );
    releaseRename();
    await first;
    assert.deepEqual((await readdir(buildRoot)).sort(), [".gitkeep", "build-manifest.json"]);
  } finally {
    releaseRename?.();
    await rm(root, { force: true, recursive: true });
  }
});

test("does not clean a replacement build root after taking the staging lock", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-manifest-root-swap-"));
  const buildRoot = path.join(root, "resources", "build");
  const moved = `${buildRoot}-moved`;
  try {
    await mkdir(buildRoot, { recursive: true });
    await writeFile(path.join(buildRoot, ".gitkeep"), "");
    await assert.rejects(
      writeBuildManifest({
        beforeCleanup: async () => {
          await rename(buildRoot, moved);
          await mkdir(buildRoot);
          await writeFile(path.join(buildRoot, ".gitkeep"), "");
          await writeFile(path.join(buildRoot, "must-survive"), "evidence");
        },
        buildRoot,
        manifest: createBuildManifest(validInput()),
      }),
      /BUILD_STAGING_INVALID/,
    );
    assert.equal(await readFile(path.join(buildRoot, "must-survive"), "utf8"), "evidence");
  } finally {
    await rm(root, { force: true, recursive: true });
    await rm(moved, { force: true, recursive: true });
  }
});

test("does not traverse a symlink replacement while cleaning failed staging", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-manifest-symlink-swap-"));
  const buildRoot = path.join(root, "resources", "build");
  const moved = `${buildRoot}-moved`;
  const external = path.join(root, "external");
  const externalFiles = new Map([
    [".build-manifest.json.tmp", Buffer.from("external temporary bytes\0")],
    ["build-manifest.json", Buffer.from('{"external":"manifest"}\n')],
    [".build-manifest.lock", Buffer.from("external lock bytes\n")],
    ["build-manifest.lock", Buffer.from("another lock-named file\n")],
  ]);
  try {
    await mkdir(buildRoot, { recursive: true });
    await mkdir(external);
    await writeFile(path.join(buildRoot, ".gitkeep"), "");
    for (const [name, bytes] of externalFiles) {
      await writeFile(path.join(external, name), bytes);
    }
    await assert.rejects(
      writeBuildManifest({
        beforeCleanup: async () => {
          await rename(buildRoot, moved);
          await symlink(external, buildRoot, "dir");
        },
        buildRoot,
        manifest: createBuildManifest(validInput()),
      }),
      /BUILD_STAGING_INVALID/,
    );
    assert.equal((await lstat(buildRoot)).isSymbolicLink(), true);
    assert.deepEqual((await readdir(external)).sort(), [...externalFiles.keys()].sort());
    for (const [name, bytes] of externalFiles) {
      assert.deepEqual(await readFile(path.join(external, name)), bytes);
    }
    const siblingLock = path.join(root, "resources", ".build-manifest.lock");
    assert.equal(
      await lstat(siblingLock).then(
        () => true,
        () => false,
      ),
      false,
    );
    await rm(buildRoot);
    await rename(moved, buildRoot);
    await writeBuildManifest({
      buildRoot,
      manifest: createBuildManifest(validInput()),
    });
    assert.deepEqual((await readdir(buildRoot)).sort(), [".gitkeep", "build-manifest.json"]);
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
  const root = await realpath(await mkdtemp(path.join(tmpdir(), "tammy-manifest-collect-")));
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
    await writeSbrManifestFixture(root, "b".repeat(40));
    const sqlcipherLibrarySha256 = await writeSqlcipherManifestFixture(root);
    const commandRunner = async (command, args, options) => {
      calls.push({ command, args, options });
      if (args[0] === "rev-parse" && args[1] === "--show-toplevel") {
        return `${root}\n`;
      }
      if (args[0] === "--sqlcipher-status") {
        return `${JSON.stringify({
          library_sha256: sqlcipherLibrarySha256,
          ordinary_sqlite_fallback: false,
          runtime_version: "4.15.0 community",
          version: "4.15.0",
        })}\n`;
      }
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
    assert.equal(Object.hasOwn(manifest.sbr, "helper_raw_sha256"), false);
    assert.equal(manifest.lockfiles["pnpm-lock.yaml"], hash("pnpm lock"));
    assert.equal(manifest.lockfiles["services/core/go.sum"], hash("go sum"));
    assert.deepEqual(
      calls.map(({ command, args, options }) => ({
        command,
        args,
        options: {
          cwd: options.cwd,
          encoding: options.encoding,
          shell: options.shell,
          windowsHide: options.windowsHide,
        },
      })),
      [
        {
          command: "git",
          args: ["rev-parse", "--show-toplevel"],
          options: { cwd: root, encoding: "utf8", shell: false, windowsHide: true },
        },
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
        {
          command: path.join(root, "apps/desktop/resources/core/darwin-arm64/tammy-core"),
          args: ["--sqlcipher-status"],
          options: { cwd: root, encoding: "utf8", shell: false, windowsHide: true },
        },
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
      ],
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("rejects redirected repositories and concurrent Git observations", async () => {
  const fixture = await realpath(await mkdtemp(path.join(tmpdir(), "tammy-git-change-")));
  try {
    await mkdir(path.join(fixture, "apps/desktop/resources/core/darwin-arm64"), {
      recursive: true,
    });
    await mkdir(path.join(fixture, "services/core"), { recursive: true });
    await mkdir(path.join(fixture, "proto"), { recursive: true });
    await writeFile(
      path.join(fixture, "package.json"),
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
      path.join(fixture, "apps/desktop/package.json"),
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
      path.join(fixture, "services/core/go.mod"),
      "module example\n\ngo 1.26.4\n\nrequire (\n\tconnectrpc.com/connect v1.20.0\n\tgoogle.golang.org/protobuf v1.36.11\n)\n",
    );
    await writeFile(path.join(fixture, "pnpm-lock.yaml"), "lock");
    await writeFile(path.join(fixture, "services/core/go.sum"), "sum");
    await writeFile(path.join(fixture, "proto/system.proto"), "proto");
    await writeFile(
      path.join(fixture, "apps/desktop/resources/core/darwin-arm64/tammy-core"),
      "core",
    );
    await writeSbrManifestFixture(fixture);
    const sqlcipherLibrarySha256 = await writeSqlcipherManifestFixture(fixture);
    await assert.rejects(
      collectBuildManifest({
        arch: "arm64",
        commandRunner: async (_command, args) =>
          args[0] === "--sqlcipher-status"
            ? `${JSON.stringify({
                library_sha256: sqlcipherLibrarySha256,
                ordinary_sqlite_fallback: false,
                runtime_version: "4.15.0 community",
                version: "4.15.0",
              })}\n`
            : args[1] === "--show-toplevel"
              ? `${path.dirname(fixture)}\n`
              : args[0] === "rev-parse"
                ? `${"a".repeat(40)}\n`
                : "",
        platform: "darwin",
        root: fixture,
      }),
      /GIT_REPOSITORY_MISMATCH/,
    );
    let headReads = 0;
    await assert.rejects(
      collectBuildManifest({
        arch: "arm64",
        commandRunner: async (_command, args) => {
          if (args[0] === "--sqlcipher-status") {
            return `${JSON.stringify({
              library_sha256: sqlcipherLibrarySha256,
              ordinary_sqlite_fallback: false,
              runtime_version: "4.15.0 community",
              version: "4.15.0",
            })}\n`;
          }
          if (args[1] === "--show-toplevel") return `${fixture}\n`;
          if (args[0] === "rev-parse") {
            headReads += 1;
            return `${(headReads === 1 ? "a" : "b").repeat(40)}\n`;
          }
          return "";
        },
        platform: "darwin",
        root: fixture,
      }),
      /SOURCE_CHANGED_DURING_MANIFEST/,
    );
  } finally {
    await rm(fixture, { force: true, recursive: true });
  }
});
