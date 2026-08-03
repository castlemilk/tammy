import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readdir, readFile, rm, stat, symlink, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, it } from "node:test";

import {
  buildCore,
  cleanCoreResources,
  createBuildCache,
  createBuildPlan,
  loadSqlcipherBuildInput,
  parseCiTarget,
  resolveCoreBinary,
  selectBuildTarget,
  selectTarget,
} from "./build-core.mjs";

const temporaryDirectories = [];

afterEach(async () => {
  await Promise.all(
    temporaryDirectories
      .splice(0)
      .map((directory) =>
        import("node:fs/promises").then(({ rm }) =>
          rm(directory, { force: true, recursive: true }),
        ),
      ),
  );
});

async function temporaryRoot() {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-build-core-"));
  temporaryDirectories.push(root);
  await mkdir(path.join(root, "apps/desktop/resources/core"), { recursive: true });
  await writeFile(path.join(root, "apps/desktop/resources/core/.gitkeep"), "");
  return root;
}

async function authenticatedSqlcipherRoot(target = "darwin-arm64") {
  const root = await temporaryRoot();
  const resourceRoot = path.join(root, "apps/desktop/resources/sqlcipher");
  const targetRoot = path.join(resourceRoot, target);
  const license = "canonical sqlcipher license\n";
  const library = Buffer.from(`sqlcipher static ${target}`);
  const header = Buffer.from(`sqlcipher header ${target}`);
  const libraryHash = createHash("sha256").update(library).digest("hex");
  const headerHash = createHash("sha256").update(header).digest("hex");
  await Promise.all([
    mkdir(path.join(targetRoot, "include"), { recursive: true }),
    mkdir(path.join(targetRoot, "lib"), { recursive: true }),
    mkdir(path.join(root, "third_party/sqlcipher"), { recursive: true }),
  ]);
  await Promise.all([
    writeFile(path.join(resourceRoot, "VERSION"), "4.15.0\n"),
    writeFile(path.join(resourceRoot, "LICENSE"), license),
    writeFile(path.join(root, "third_party/sqlcipher/LICENSE"), license),
    writeFile(path.join(targetRoot, "include/sqlite3.h"), header),
    writeFile(path.join(targetRoot, "HEADER_SHA256"), `${headerHash}\n`),
    writeFile(path.join(targetRoot, "lib/libsqlite3.a"), library),
    writeFile(path.join(targetRoot, "LIBRARY_SHA256"), `${libraryHash}\n`),
  ]);
  if (target === "win32-x64") {
    const provider = Buffer.from("pinned openssl static archive");
    const providerHash = createHash("sha256").update(provider).digest("hex");
    await Promise.all([
      writeFile(path.join(targetRoot, "lib/libcrypto.a"), provider),
      writeFile(path.join(targetRoot, "OPENSSL_LIBRARY_SHA256"), `${providerHash}\n`),
      writeFile(path.join(resourceRoot, "OPENSSL_LICENSE"), "openssl license\n"),
      writeFile(path.join(root, "third_party/sqlcipher/OPENSSL_LICENSE"), "openssl license\n"),
    ]);
  }
  return { resourceRoot, root, targetRoot };
}

function sqlcipherInput(root, target = "darwin-arm64") {
  const targetRoot = path.join(root, `apps/desktop/resources/sqlcipher/${target}`);
  const input = {
    compiler: target === "darwin-arm64" ? "/usr/bin/clang" : "C:\\LLVM\\bin\\clang.exe",
    header: path.join(targetRoot, "include/sqlite3.h"),
    headerSha256: "c".repeat(64),
    include: path.join(targetRoot, "include"),
    library: path.join(targetRoot, "lib/libsqlite3.a"),
    librarySha256: "a".repeat(64),
    target,
    version: "4.15.0",
  };
  if (target === "win32-x64") {
    input.providerLibrary = path.join(targetRoot, "lib/libcrypto.a");
    input.providerLibrarySha256 = "b".repeat(64);
  }
  return input;
}

describe("core build target", () => {
  it("selects only the two supported target tables", () => {
    assert.deepEqual(
      [
        selectTarget("darwin", "arm64"),
        selectTarget("win32", "x64"),
        parseCiTarget("darwin/arm64"),
        parseCiTarget("win32/x64"),
      ],
      [
        { platform: "darwin", arch: "arm64", executable: "tammy-core" },
        { platform: "win32", arch: "x64", executable: "tammy-core.exe" },
        { platform: "darwin", arch: "arm64", executable: "tammy-core" },
        { platform: "win32", arch: "x64", executable: "tammy-core.exe" },
      ],
    );
  });

  for (const [platform, arch] of [
    ["linux", "x64"],
    ["darwin", "x64"],
    ["win32", "arm64"],
  ]) {
    it(`rejects unsupported ${platform}/${arch}`, () => {
      assert.throws(() => selectTarget(platform, arch), /UNSUPPORTED_CORE_TARGET/);
      assert.throws(() => parseCiTarget(`${platform}/${arch}`), /UNSUPPORTED_CORE_TARGET/);
    });
  }

  it("defaults only to a supported native target and permits an explicit supported CI target", () => {
    assert.deepEqual(selectBuildTarget({}, "darwin", "arm64"), {
      arch: "arm64",
      executable: "tammy-core",
      platform: "darwin",
    });
    assert.deepEqual(
      selectBuildTarget({ CI: "true", TAMMY_CORE_TARGET: "win32/x64" }, "win32", "x64"),
      {
        arch: "x64",
        executable: "tammy-core.exe",
        platform: "win32",
      },
    );
    assert.throws(
      () => selectBuildTarget({ CI: "true", TAMMY_CORE_TARGET: "win32/x64" }, "darwin", "arm64"),
      /CORE_CGO_CROSS_BUILD_UNSUPPORTED/,
    );
    assert.throws(
      () => selectBuildTarget({ TAMMY_CORE_TARGET: "win32/x64" }, "darwin", "arm64"),
      /CI_TARGET_REQUIRES_CI/,
    );
    assert.throws(() => selectBuildTarget({}, "linux", "x64"), /UNSUPPORTED_CORE_TARGET/);
  });
});

describe("authenticated SQLCipher inputs", () => {
  it("authenticates the complete macOS resource set", async () => {
    const { root } = await authenticatedSqlcipherRoot();
    const input = await loadSqlcipherBuildInput(root, selectTarget("darwin", "arm64"));
    assert.equal(input.version, "4.15.0");
    assert.equal(input.target, "darwin-arm64");
    assert.match(input.librarySha256, /^[a-f0-9]{64}$/);
    assert.equal(input.compiler, "/usr/bin/clang");
  });

  for (const relative of [
    "VERSION",
    "LICENSE",
    "darwin-arm64/LIBRARY_SHA256",
    "darwin-arm64/HEADER_SHA256",
    "darwin-arm64/include/sqlite3.h",
    "darwin-arm64/lib/libsqlite3.a",
  ]) {
    it(`rejects a missing ${relative}`, async () => {
      const { resourceRoot, root } = await authenticatedSqlcipherRoot();
      await rm(path.join(resourceRoot, relative), { force: true });
      await assert.rejects(
        loadSqlcipherBuildInput(root, selectTarget("darwin", "arm64")),
        /SQLCIPHER_/,
      );
    });

    it(`rejects a symlinked ${relative}`, async () => {
      const { resourceRoot, root } = await authenticatedSqlcipherRoot();
      const candidate = path.join(resourceRoot, relative);
      const outside = path.join(root, `outside-${relative.replaceAll("/", "-")}`);
      await writeFile(outside, "outside");
      await rm(candidate, { force: true });
      await symlink(outside, candidate);
      await assert.rejects(
        loadSqlcipherBuildInput(root, selectTarget("darwin", "arm64")),
        /SQLCIPHER_/,
      );
    });
  }

  it("rejects version, library hash, canonical licence, and compiler drift", async () => {
    for (const mutation of [
      async ({ resourceRoot }) => writeFile(path.join(resourceRoot, "VERSION"), "4.14.0\n"),
      async ({ targetRoot }) =>
        writeFile(path.join(targetRoot, "LIBRARY_SHA256"), `${"0".repeat(64)}\n`),
      async ({ resourceRoot }) => writeFile(path.join(resourceRoot, "LICENSE"), "changed\n"),
      async ({ targetRoot }) => writeFile(path.join(targetRoot, "include/sqlite3.h"), "changed\n"),
    ]) {
      const fixture = await authenticatedSqlcipherRoot();
      await mutation(fixture);
      await assert.rejects(
        loadSqlcipherBuildInput(fixture.root, selectTarget("darwin", "arm64")),
        /SQLCIPHER_RESOURCE_AUTHENTICATION_FAILED/,
      );
    }
    const { root } = await authenticatedSqlcipherRoot("win32-x64");
    await assert.rejects(
      loadSqlcipherBuildInput(root, selectTarget("win32", "x64"), {
        TAMMY_SQLCIPHER_CC: path.join(root, "missing-clang.exe"),
      }),
      /SQLCIPHER_/,
    );
  });

  it("authenticates and rejects drift in the Windows provider archive and licence", async () => {
    const fixture = await authenticatedSqlcipherRoot("win32-x64");
    const input = await loadSqlcipherBuildInput(fixture.root, selectTarget("win32", "x64"), {
      TAMMY_SQLCIPHER_CC: "/usr/bin/clang",
    });
    assert.match(input.providerLibrarySha256, /^[a-f0-9]{64}$/);
    for (const relative of ["lib/libcrypto.a", "OPENSSL_LIBRARY_SHA256"]) {
      const changed = await authenticatedSqlcipherRoot("win32-x64");
      await writeFile(path.join(changed.targetRoot, relative), "drift");
      await assert.rejects(
        loadSqlcipherBuildInput(changed.root, selectTarget("win32", "x64"), {
          TAMMY_SQLCIPHER_CC: "/usr/bin/clang",
        }),
        /SQLCIPHER_OPENSSL_/,
      );
    }
    const changedLicense = await authenticatedSqlcipherRoot("win32-x64");
    await rm(path.join(changedLicense.resourceRoot, "OPENSSL_LICENSE"));
    await assert.rejects(
      loadSqlcipherBuildInput(changedLicense.root, selectTarget("win32", "x64"), {
        TAMMY_SQLCIPHER_CC: "/usr/bin/clang",
      }),
      /SQLCIPHER_OPENSSL_MISSING/,
    );
    for (const replacement of ["modified\n", null]) {
      const changed = await authenticatedSqlcipherRoot("win32-x64");
      const packaged = path.join(changed.resourceRoot, "OPENSSL_LICENSE");
      if (replacement === null) {
        const outside = path.join(changed.root, "outside-openssl-license");
        await writeFile(outside, "openssl license\n");
        await rm(packaged);
        await symlink(outside, packaged);
      } else {
        await writeFile(packaged, replacement);
      }
      await assert.rejects(
        loadSqlcipherBuildInput(changed.root, selectTarget("win32", "x64"), {
          TAMMY_SQLCIPHER_CC: "/usr/bin/clang",
        }),
        /SQLCIPHER_OPENSSL_/,
      );
    }
  });
});

describe("core build plan", () => {
  it("uses exact safe Go arguments and environment", () => {
    const root = path.resolve("/workspace/tammy");
    const plan = createBuildPlan({
      root,
      sqlcipher: sqlcipherInput(root, "win32-x64"),
      target: selectTarget("win32", "x64"),
      version: "0.1.0-beta.2",
    });

    assert.deepEqual(plan.args, [
      "build",
      "-trimpath",
      "-buildvcs=true",
      "-tags=tammy_sqlcipher",
      `-ldflags=-s -w -X github.com/tammyapp/tammy/services/core/internal/buildinfo.version=0.1.0-beta.2 -X github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher.linkedLibrarySHA256=${"a".repeat(64)}`,
      "-o",
      path.join(root, "apps/desktop/resources/core/win32-x64/tammy-core.exe"),
      "./services/core/cmd/tammy-core",
    ]);
    assert.equal(plan.command, "go");
    assert.equal(plan.options.cwd, root);
    assert.equal(plan.options.shell, false);
    assert.equal(plan.options.env.CGO_ENABLED, "1");
    assert.equal(plan.options.env.CC, "C:\\LLVM\\bin\\clang.exe");
    assert.equal(
      plan.options.env.CGO_CFLAGS,
      `-DSQLITE_HAS_CODEC -I${path.join(root, "apps/desktop/resources/sqlcipher/win32-x64/include")}`,
    );
    assert.equal(
      plan.options.env.CGO_LDFLAGS,
      `${path.join(root, "apps/desktop/resources/sqlcipher/win32-x64/lib/libsqlite3.a")} ${path.join(root, "apps/desktop/resources/sqlcipher/win32-x64/lib/libcrypto.a")} -lcrypt32 -luser32 -lws2_32`,
    );
    assert.equal(plan.options.env.GOOS, "windows");
    assert.equal(plan.options.env.GOARCH, "amd64");
  });

  for (const version of ["", "1.0.0 evil", "1.0.0\n-X evil=value", "../1.0.0"]) {
    it(`rejects unsafe version ${JSON.stringify(version)}`, () => {
      assert.throws(
        () =>
          createBuildPlan({
            root: path.resolve("/workspace/tammy"),
            sqlcipher: sqlcipherInput(path.resolve("/workspace/tammy"), "darwin-arm64"),
            target: selectTarget("darwin", "arm64"),
            version,
          }),
        /INVALID_DESKTOP_VERSION/,
      );
    });
  }

  it("rejects path traversal and roots that are not absolute", () => {
    assert.throws(
      () =>
        createBuildPlan({
          root: ".",
          sqlcipher: sqlcipherInput(path.resolve("/workspace/tammy"), "darwin-arm64"),
          target: selectTarget("darwin", "arm64"),
          version: "0.1.0",
        }),
      /INVALID_PROJECT_ROOT/,
    );
    assert.throws(
      () =>
        resolveCoreBinary(path.resolve("/workspace/tammy/apps/desktop/resources/core"), {
          platform: "../darwin",
          arch: "arm64",
          executable: "tammy-core",
        }),
      /INVALID_CORE_PATH|UNSUPPORTED_CORE_TARGET/,
    );
  });

  it("rejects absent, wrong-target, and malformed SQLCipher inputs", () => {
    const root = path.resolve("/workspace/tammy");
    const target = selectTarget("darwin", "arm64");
    assert.throws(
      () => createBuildPlan({ root, target, version: "0.1.0" }),
      /SQLCIPHER_BUILD_INPUT_INVALID/,
    );
    assert.throws(
      () =>
        createBuildPlan({
          root,
          sqlcipher: sqlcipherInput(root, "win32-x64"),
          target,
          version: "0.1.0",
        }),
      /SQLCIPHER_TARGET_MISMATCH/,
    );
    assert.throws(
      () =>
        createBuildPlan({
          root,
          sqlcipher: { ...sqlcipherInput(root), librarySha256: "bad" },
          target,
          version: "0.1.0",
        }),
      /SQLCIPHER_BUILD_INPUT_INVALID/,
    );
  });
});

describe("generated resources", () => {
  it("removes every stale entry and symlink but preserves only zero-byte .gitkeep", async () => {
    const root = await temporaryRoot();
    const resources = path.join(root, "apps/desktop/resources/core");
    const outside = path.join(root, "outside");
    await mkdir(path.join(resources, "stale/sub"), { recursive: true });
    await mkdir(outside);
    await writeFile(path.join(resources, "stale/sub/tammy-core"), "old");
    await writeFile(path.join(resources, "arbitrary.txt"), "old");
    await writeFile(path.join(outside, "untouched"), "safe");
    await symlink(outside, path.join(resources, "link"));

    await cleanCoreResources(resources);

    assert.deepEqual(await readdir(resources), [".gitkeep"]);
    assert.equal(await readFile(path.join(outside, "untouched"), "utf8"), "safe");
    assert.equal((await stat(path.join(resources, ".gitkeep"))).size, 0);
  });

  it("rejects a symlink resource root and a non-empty .gitkeep", async () => {
    const root = await temporaryRoot();
    const resources = path.join(root, "apps/desktop/resources/core");
    const linked = path.join(root, "linked-core");
    await mkdir(linked);
    await writeFile(path.join(resources, ".gitkeep"), "not empty");
    await assert.rejects(cleanCoreResources(resources), /INVALID_CORE_RESOURCES/);
    await import("node:fs/promises").then(({ rm }) =>
      rm(path.join(resources, ".gitkeep"), { force: true }),
    );
    await symlink(linked, path.join(root, "core-link"));
    await assert.rejects(
      cleanCoreResources(path.join(root, "core-link")),
      /INVALID_CORE_RESOURCES/,
    );
  });

  it("normalizes the repository's historical newline-only .gitkeep", async () => {
    const root = await temporaryRoot();
    const resources = path.join(root, "apps/desktop/resources/core");
    await writeFile(path.join(resources, ".gitkeep"), "\n");

    await cleanCoreResources(resources);

    assert.equal((await stat(path.join(resources, ".gitkeep"))).size, 0);
  });
});

describe("build execution", () => {
  it("cleans, builds once, requires a regular output, and returns its SHA-256", async () => {
    const root = await temporaryRoot();
    const cacheRoot = path.join(root, "cache");
    await mkdir(cacheRoot);
    const calls = [];
    const sourceEnvironment = {
      GOCACHE: path.join(root, "shared-cache"),
      PATH: "/tools",
      TAMMY_BUILD_SECRET: "must-not-be-printed",
    };
    const originalEnvironment = { ...sourceEnvironment };
    const result = await buildCore({
      root,
      sqlcipher: sqlcipherInput(root),
      sourceEnvironment,
      target: selectTarget("darwin", "arm64"),
      temporaryRoot: cacheRoot,
      version: "0.1.0",
      execFile: async (command, args, options) => {
        calls.push({ command, args, options });
        const output = args[args.indexOf("-o") + 1];
        await writeFile(output, "binary");
      },
    });

    assert.equal(calls.length, 1);
    assert.equal(calls[0].options.killSignal, "SIGKILL");
    assert.equal(calls[0].options.shell, false);
    assert.equal(calls[0].options.timeout, 120_000);
    assert.ok(calls[0].options.signal instanceof AbortSignal);
    assert.equal(calls[0].options.env.TAMMY_BUILD_SECRET, "must-not-be-printed");
    assert.equal(calls[0].options.env.PATH, "/tools");
    assert.notEqual(calls[0].options.env.GOCACHE, originalEnvironment.GOCACHE);
    assert.equal(path.relative(cacheRoot, calls[0].options.env.GOCACHE).startsWith(".."), false);
    assert.deepEqual(sourceEnvironment, originalEnvironment);
    assert.deepEqual(await readdir(cacheRoot), []);
    assert.equal(
      result.path,
      path.join(root, "apps/desktop/resources/core/darwin-arm64/tammy-core"),
    );
    assert.match(result.sha256, /^[a-f0-9]{64}$/);
    assert.equal(result.sha256, "9a3a45d01531a20e89ac6ae10b0b0beb0492acd7216a368aa062d1a5fecaf9cd");
  });

  it("rejects a build that does not produce the binary", async () => {
    const root = await temporaryRoot();
    const cacheRoot = path.join(root, "cache");
    await mkdir(cacheRoot);
    await assert.rejects(
      buildCore({
        root,
        sqlcipher: sqlcipherInput(root),
        target: selectTarget("darwin", "arm64"),
        temporaryRoot: cacheRoot,
        version: "0.1.0",
        execFile: async () => undefined,
      }),
      /CORE_BINARY_MISSING/,
    );
    assert.deepEqual(await readdir(cacheRoot), []);
  });

  it("aborts a bounded build and cleans its partial output", async () => {
    const root = await temporaryRoot();
    const resources = path.join(root, "apps/desktop/resources/core");
    const cacheRoot = path.join(root, "cache");
    await mkdir(cacheRoot);
    await assert.rejects(
      buildCore({
        root,
        sqlcipher: sqlcipherInput(root),
        target: selectTarget("darwin", "arm64"),
        temporaryRoot: cacheRoot,
        timeoutMs: 1,
        version: "0.1.0",
        execFile: async (_command, args, options) =>
          new Promise((resolve, reject) => {
            const output = args[args.indexOf("-o") + 1];
            void writeFile(output, "partial").then(() => {
              if (options.signal.aborted) {
                reject(new Error("aborted"));
                return;
              }
              options.signal.addEventListener("abort", () => reject(new Error("aborted")), {
                once: true,
              });
            }, reject);
            void resolve;
          }),
      }),
      /CORE_BUILD_TIMEOUT/,
    );
    assert.deepEqual(await readdir(resources), [".gitkeep"]);
    assert.deepEqual(await readdir(cacheRoot), []);
  });

  it("cleans the isolated cache and partial output after a build failure", async () => {
    const root = await temporaryRoot();
    const resources = path.join(root, "apps/desktop/resources/core");
    const cacheRoot = path.join(root, "cache");
    await mkdir(cacheRoot);
    await assert.rejects(
      buildCore({
        root,
        sqlcipher: sqlcipherInput(root),
        target: selectTarget("darwin", "arm64"),
        temporaryRoot: cacheRoot,
        version: "0.1.0",
        execFile: async (_command, args, options) => {
          assert.equal(path.relative(cacheRoot, options.env.GOCACHE).startsWith(".."), false);
          await writeFile(path.join(options.env.GOCACHE, "cache-entry"), "cache");
          const output = args[args.indexOf("-o") + 1];
          await writeFile(output, "partial");
          throw new Error("injected failure");
        },
      }),
      /CORE_BUILD_FAILED/,
    );
    assert.deepEqual(await readdir(cacheRoot), []);
    assert.deepEqual(await readdir(resources), [".gitkeep"]);
  });

  it("rejects a cache directory outside the configured temporary root", async () => {
    const root = await temporaryRoot();
    const cacheRoot = path.join(root, "cache");
    const outside = path.join(root, "outside-cache");
    await mkdir(cacheRoot);
    await mkdir(outside);
    await assert.rejects(
      createBuildCache({
        makeDirectory: async () => outside,
        temporaryRoot: cacheRoot,
      }),
      /INVALID_BUILD_CACHE/,
    );
  });

  it("fails closed and removes the binary when cache cleanup fails", async () => {
    const root = await temporaryRoot();
    const resources = path.join(root, "apps/desktop/resources/core");
    const cacheRoot = path.join(root, "cache");
    await mkdir(cacheRoot);
    await assert.rejects(
      buildCore({
        root,
        sqlcipher: sqlcipherInput(root),
        target: selectTarget("darwin", "arm64"),
        temporaryRoot: cacheRoot,
        version: "0.1.0",
        execFile: async (_command, args) => {
          await writeFile(args[args.indexOf("-o") + 1], "binary");
        },
        removeCacheDirectory: async () => {
          throw new Error("injected cleanup failure");
        },
      }),
      /CORE_BUILD_CACHE_CLEANUP_FAILED/,
    );
    assert.deepEqual(await readdir(resources), [".gitkeep"]);
  });
});
