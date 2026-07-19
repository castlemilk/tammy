import assert from "node:assert/strict";
import { mkdir, mkdtemp, readdir, readFile, stat, symlink, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, it } from "node:test";

import {
  buildCore,
  cleanCoreResources,
  createBuildCache,
  createBuildPlan,
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
      selectBuildTarget({ CI: "true", TAMMY_CORE_TARGET: "win32/x64" }, "darwin", "arm64"),
      {
        arch: "x64",
        executable: "tammy-core.exe",
        platform: "win32",
      },
    );
    assert.throws(
      () => selectBuildTarget({ TAMMY_CORE_TARGET: "win32/x64" }, "darwin", "arm64"),
      /CI_TARGET_REQUIRES_CI/,
    );
    assert.throws(() => selectBuildTarget({}, "linux", "x64"), /UNSUPPORTED_CORE_TARGET/);
  });
});

describe("core build plan", () => {
  it("uses exact safe Go arguments and environment", () => {
    const root = path.resolve("/workspace/tammy");
    const plan = createBuildPlan({
      root,
      target: selectTarget("win32", "x64"),
      version: "0.1.0-beta.2",
    });

    assert.deepEqual(plan.args, [
      "build",
      "-trimpath",
      "-buildvcs=true",
      "-ldflags=-s -w -X github.com/tammyapp/tammy/services/core/internal/buildinfo.version=0.1.0-beta.2",
      "-o",
      path.join(root, "apps/desktop/resources/core/win32-x64/tammy-core.exe"),
      "./services/core/cmd/tammy-core",
    ]);
    assert.equal(plan.command, "go");
    assert.equal(plan.options.cwd, root);
    assert.equal(plan.options.shell, false);
    assert.equal(plan.options.env.CGO_ENABLED, "0");
    assert.equal(plan.options.env.GOOS, "windows");
    assert.equal(plan.options.env.GOARCH, "amd64");
  });

  for (const version of ["", "1.0.0 evil", "1.0.0\n-X evil=value", "../1.0.0"]) {
    it(`rejects unsafe version ${JSON.stringify(version)}`, () => {
      assert.throws(
        () =>
          createBuildPlan({
            root: path.resolve("/workspace/tammy"),
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
