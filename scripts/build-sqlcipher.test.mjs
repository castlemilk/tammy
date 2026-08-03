import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, it } from "node:test";

import {
  buildSqlcipher,
  buildWindowsProvider,
  createSqlcipherBuildPlan,
  selectSqlcipherTarget,
} from "./build-sqlcipher.mjs";

const temporaryDirectories = [];
const SOURCE_TREE_SHA256 = "ab920a951726ede8da090ad26874f094966de373e9ed566e6e6dc500541920be";

afterEach(async () => {
  await Promise.all(
    temporaryDirectories
      .splice(0)
      .map((directory) => rm(directory, { force: true, recursive: true })),
  );
});

async function fixtureRoot() {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-sqlcipher-build-test-"));
  temporaryDirectories.push(root);
  const sourceRoot = path.join(root, "fixture-source/sqlcipher-4.15.0");
  await mkdir(sourceRoot, { recursive: true });
  await Promise.all([
    writeFile(path.join(sourceRoot, "configure"), "#!/bin/sh\n", { mode: 0o700 }),
    writeFile(path.join(sourceRoot, "LICENSE.md"), "pinned license\n"),
    writeFile(path.join(sourceRoot, "VERSION"), "3.53.0\n"),
  ]);
  return { root, sourceRoot };
}

describe("SQLCipher build targets", () => {
  it("supports only native macOS arm64 and Windows x64", () => {
    assert.deepEqual(selectSqlcipherTarget("darwin", "arm64"), {
      arch: "arm64",
      goarch: "arm64",
      goos: "darwin",
      platform: "darwin",
      resourceTarget: "darwin-arm64",
    });
    assert.deepEqual(selectSqlcipherTarget("win32", "x64"), {
      arch: "x64",
      goarch: "amd64",
      goos: "windows",
      platform: "win32",
      resourceTarget: "win32-x64",
    });
  });

  for (const [platform, arch] of [
    ["linux", "x64"],
    ["darwin", "x64"],
    ["win32", "arm64"],
  ]) {
    it(`rejects unsupported ${platform}/${arch}`, () => {
      assert.throws(() => selectSqlcipherTarget(platform, arch), /UNSUPPORTED_SQLCIPHER_TARGET/);
    });
  }
});

describe("SQLCipher static build plan", () => {
  it("creates an explicit CommonCrypto macOS arm64 plan and complete resources", () => {
    const root = path.resolve("/workspace/tammy");
    const sourceRoot = path.join(root, ".cache/sqlcipher/source/sqlcipher-4.15.0");
    const buildRoot = path.join(root, ".cache/sqlcipher/build/darwin-arm64");
    const plan = createSqlcipherBuildPlan({
      buildRoot,
      compiler: "/usr/bin/clang",
      root,
      sourceRoot,
      target: selectSqlcipherTarget("darwin", "arm64"),
    });

    assert.equal(plan.compiler, "/usr/bin/clang");
    assert.equal(plan.sourceRoot, sourceRoot);
    assert.equal(plan.library, path.join(buildRoot, "libsqlite3.a"));
    assert.deepEqual(plan.defines, [
      "SQLITE_HAS_CODEC",
      "SQLITE_EXTRA_INIT=sqlcipher_extra_init",
      "SQLITE_EXTRA_SHUTDOWN=sqlcipher_extra_shutdown",
      "SQLCIPHER_CRYPTO_CC",
      "SQLITE_TEMP_STORE=2",
      "SQLITE_THREADSAFE=1",
      "SQLITE_OMIT_LOAD_EXTENSION",
      "SQLITE_DQS=0",
    ]);
    assert.deepEqual(plan.frameworks, ["CoreFoundation", "Security"]);
    assert.deepEqual(plan.resources, {
      header: path.join(root, "apps/desktop/resources/sqlcipher/darwin-arm64/include/sqlite3.h"),
      headerSha256: path.join(root, "apps/desktop/resources/sqlcipher/darwin-arm64/HEADER_SHA256"),
      license: path.join(root, "apps/desktop/resources/sqlcipher/LICENSE"),
      library: path.join(root, "apps/desktop/resources/sqlcipher/darwin-arm64/lib/libsqlite3.a"),
      librarySha256: path.join(
        root,
        "apps/desktop/resources/sqlcipher/darwin-arm64/LIBRARY_SHA256",
      ),
      version: path.join(root, "apps/desktop/resources/sqlcipher/VERSION"),
    });
  });

  it("rejects a non-absolute compiler, roots, cross-build, and system SQLite input", () => {
    const root = path.resolve("/workspace/tammy");
    const base = {
      buildRoot: path.join(root, ".cache/sqlcipher/build/darwin-arm64"),
      compiler: "/usr/bin/clang",
      root,
      sourceRoot: path.join(root, ".cache/sqlcipher/source/sqlcipher-4.15.0"),
      target: selectSqlcipherTarget("darwin", "arm64"),
    };
    assert.throws(
      () => createSqlcipherBuildPlan({ ...base, compiler: "clang" }),
      /SQLCIPHER_BUILD_INPUT_INVALID/,
    );
    assert.throws(
      () => createSqlcipherBuildPlan({ ...base, sourceRoot: "/usr/local/include" }),
      /SQLCIPHER_BUILD_INPUT_INVALID/,
    );
    assert.throws(
      () => createSqlcipherBuildPlan({ ...base, hostPlatform: "win32" }),
      /SQLCIPHER_CROSS_BUILD_UNSUPPORTED/,
    );
  });

  it("pins every Windows generator, compiler, and OpenSSL input without PATH fallback", () => {
    const root = path.resolve("/workspace/tammy");
    const plan = createSqlcipherBuildPlan({
      archiver: "/llvm/bin/llvm-ar.exe",
      buildRoot: path.join(root, ".cache/sqlcipher/build/win32-x64"),
      compiler: "/llvm/bin/clang.exe",
      generatorCompiler: "/llvm/bin/clang-cl.exe",
      hostArch: "x64",
      hostPlatform: "win32",
      make: "/visual-studio/nmake.exe",
      librarian: "/llvm/bin/llvm-lib.exe",
      linker: "/llvm/bin/lld-link.exe",
      manifestTool: "/windows-sdk/mt.exe",
      opensslInclude: "/workspace/openssl/include",
      opensslLicense: "/workspace/openssl/LICENSE.txt",
      opensslLibrary: "/workspace/openssl/libcrypto.lib",
      opensslSourceSha256: "d71a811bfbd9153d7b30cbe476263302ee4b04a9a47ffea6e6a782326805c93f",
      opensslVersion: "3.5.7",
      resourceCompiler: "/windows-sdk/rc.exe",
      root,
      sourceRoot: path.join(root, ".cache/sqlcipher/source/sqlcipher-4.15.0"),
      target: selectSqlcipherTarget("win32", "x64"),
    });

    assert.equal(plan.compiler, "/llvm/bin/clang.exe");
    assert.equal(plan.generatorCompiler, "/llvm/bin/clang-cl.exe");
    assert.equal(plan.archiver, "/llvm/bin/llvm-ar.exe");
    assert.equal(plan.make, "/visual-studio/nmake.exe");
    assert.equal(plan.opensslLibrary, "/workspace/openssl/libcrypto.lib");
    assert.deepEqual(plan.defines.includes("SQLCIPHER_CRYPTO_OPENSSL"), true);
    assert.deepEqual(plan.frameworks, []);
    assert.equal(
      plan.resources.opensslLibrary,
      path.join(root, "apps/desktop/resources/sqlcipher/win32-x64/lib/libcrypto.a"),
    );
  });
});

describe("SQLCipher build execution", () => {
  it("stages the static library, header, license, version, and authenticated hash", async () => {
    const { root, sourceRoot } = await fixtureRoot();
    const staleProvider = path.join(
      root,
      "apps/desktop/resources/sqlcipher/win32-x64/lib/libcrypto.a",
    );
    await mkdir(path.dirname(staleProvider), { recursive: true });
    await writeFile(staleProvider, "stale provider");
    const calls = [];
    const result = await buildSqlcipher({
      arch: "arm64",
      platform: "darwin",
      root,
      vendor: async ({ cacheRoot }) => {
        assert.equal(cacheRoot, path.join(root, ".tmp/sqlcipher"));
        return { sourceRoot, sourceTreeSha256: SOURCE_TREE_SHA256 };
      },
      sourceTreeHasher: async () => SOURCE_TREE_SHA256,
      execFile: async (command, args, options) => {
        calls.push({ args, command, options });
        if (command === "/usr/bin/make") {
          await Promise.all([
            writeFile(path.join(options.cwd, "sqlite3.c"), "sqlcipher source"),
            writeFile(path.join(options.cwd, "sqlite3.h"), "sqlcipher header"),
          ]);
        }
        const outputIndex = args.indexOf("-o");
        if (command === "/usr/bin/clang" && outputIndex >= 0) {
          await writeFile(args[outputIndex + 1], "object");
        }
        if (command === "/usr/bin/libtool") {
          await writeFile(args[args.indexOf("-o") + 1], "static sqlcipher library");
        }
        return { stderr: "", stdout: "" };
      },
    });

    assert.equal(calls.length, 4);
    assert.deepEqual(
      calls.map(({ command }) => command),
      ["/bin/sh", "/usr/bin/make", "/usr/bin/clang", "/usr/bin/libtool"],
    );
    assert.equal(await readFile(result.library, "utf8"), "static sqlcipher library");
    assert.equal(
      result.librarySha256,
      createHash("sha256").update("static sqlcipher library").digest("hex"),
    );
    const resourceRoot = path.join(root, "apps/desktop/resources/sqlcipher");
    assert.equal(await readFile(path.join(resourceRoot, "VERSION"), "utf8"), "4.15.0\n");
    assert.equal(await readFile(path.join(resourceRoot, "LICENSE"), "utf8"), "pinned license\n");
    assert.equal(
      await readFile(path.join(resourceRoot, "darwin-arm64/include/sqlite3.h"), "utf8"),
      "sqlcipher header",
    );
    assert.equal(
      await readFile(path.join(resourceRoot, "darwin-arm64/LIBRARY_SHA256"), "utf8"),
      `${result.librarySha256}\n`,
    );
    assert.equal(
      await readFile(path.join(resourceRoot, "darwin-arm64/HEADER_SHA256"), "utf8"),
      `${createHash("sha256").update("sqlcipher header").digest("hex")}\n`,
    );
    await assert.rejects(readFile(staleProvider), { code: "ENOENT" });
  });

  it("fails closed when a successful tool invocation omits the static library", async () => {
    const { root, sourceRoot } = await fixtureRoot();
    await assert.rejects(
      buildSqlcipher({
        arch: "arm64",
        platform: "darwin",
        root,
        vendor: async () => ({ sourceRoot, sourceTreeSha256: SOURCE_TREE_SHA256 }),
        sourceTreeHasher: async () => SOURCE_TREE_SHA256,
        execFile: async (command, _args, options) => {
          if (command === "/usr/bin/make") {
            await Promise.all([
              writeFile(path.join(options.cwd, "sqlite3.c"), "source"),
              writeFile(path.join(options.cwd, "sqlite3.h"), "header"),
            ]);
          }
          return { stderr: "", stdout: "" };
        },
      }),
      /SQLCIPHER_LIBRARY_MISSING/,
    );
  });

  it("rejects a private source copy whose full-tree hash changes before configure", async () => {
    const { root, sourceRoot } = await fixtureRoot();
    await assert.rejects(
      buildSqlcipher({
        arch: "arm64",
        platform: "darwin",
        root,
        sourceTreeHasher: async (copiedSource) => {
          await writeFile(path.join(copiedSource, "non-license-source.c"), "tampered\n");
          return "0".repeat(64);
        },
        vendor: async () => ({ sourceRoot, sourceTreeSha256: SOURCE_TREE_SHA256 }),
        execFile: async () => {
          assert.fail("configure must not run for a tampered private source copy");
        },
      }),
      /SQLCIPHER_SOURCE_INVALID/,
    );
  });

  it("builds Windows only with exact generators and stages the authenticated provider", async () => {
    const { root, sourceRoot } = await fixtureRoot();
    const staleMacLibrary = path.join(
      root,
      "apps/desktop/resources/sqlcipher/darwin-arm64/lib/libsqlite3.a",
    );
    await mkdir(path.dirname(staleMacLibrary), { recursive: true });
    await writeFile(staleMacLibrary, "stale mac archive");
    const toolsRoot = path.join(root, "tools");
    const opensslRoot = path.join(root, "openssl");
    const paths = {
      ar: path.join(toolsRoot, "llvm-ar.exe"),
      cc: path.join(toolsRoot, "clang.exe"),
      clangCl: path.join(toolsRoot, "clang-cl.exe"),
      include: path.join(opensslRoot, "include"),
      library: path.join(opensslRoot, "libcrypto.lib"),
      license: path.join(opensslRoot, "LICENSE.txt"),
      nmake: path.join(toolsRoot, "nmake.exe"),
      librarian: path.join(toolsRoot, "llvm-lib.exe"),
      linker: path.join(toolsRoot, "lld-link.exe"),
      manifestTool: path.join(toolsRoot, "mt.exe"),
      resourceCompiler: path.join(toolsRoot, "rc.exe"),
    };
    await Promise.all([
      mkdir(toolsRoot),
      mkdir(paths.include, { recursive: true }),
      mkdir(path.join(root, "scripts")),
    ]);
    await Promise.all([
      ...[
        paths.ar,
        paths.cc,
        paths.clangCl,
        paths.nmake,
        paths.librarian,
        paths.linker,
        paths.manifestTool,
        paths.resourceCompiler,
      ].map((candidate) => writeFile(candidate, "tool")),
      writeFile(paths.library, "pinned provider archive"),
      writeFile(path.join(root, "scripts/ordinary-sqlite-probe.c"), "probe"),
      writeFile(
        paths.license,
        await readFile(
          path.resolve(import.meta.dirname, "../third_party/sqlcipher/OPENSSL_LICENSE"),
        ),
      ),
    ]);
    const calls = [];
    const result = await buildSqlcipher({
      arch: "x64",
      environment: {
        TAMMY_SQLCIPHER_AR: paths.ar,
        TAMMY_SQLCIPHER_CC: paths.cc,
        TAMMY_SQLCIPHER_NMAKE: paths.nmake,
        TAMMY_SQLCIPHER_NMAKE_CC: paths.clangCl,
        TAMMY_SQLCIPHER_LIB: paths.librarian,
        TAMMY_SQLCIPHER_LINK: paths.linker,
        TAMMY_SQLCIPHER_MT: paths.manifestTool,
        TAMMY_SQLCIPHER_RC: paths.resourceCompiler,
      },
      platform: "win32",
      providerBuilder: async ({ buildRoot, repositoryRoot }) => {
        assert.ok(buildRoot.startsWith(path.join(root, ".tmp/sqlcipher")));
        assert.equal(repositoryRoot, root);
        return { include: paths.include, library: paths.library, license: paths.license };
      },
      root,
      vendor: async () => ({ sourceRoot, sourceTreeSha256: SOURCE_TREE_SHA256 }),
      sourceTreeHasher: async () => SOURCE_TREE_SHA256,
      execFile: async (command, args, options) => {
        calls.push({ args, command });
        if (command === paths.nmake) {
          await Promise.all([
            writeFile(path.join(options.cwd, "sqlite3.c"), "source"),
            writeFile(path.join(options.cwd, "sqlite3.h"), "header"),
          ]);
        } else if (command === paths.cc) {
          await writeFile(args[args.indexOf("-o") + 1], "object");
        } else if (command === paths.ar) {
          await writeFile(args[1], "sqlcipher archive");
        }
        return { stderr: "", stdout: "" };
      },
    });
    const generator = calls.find(({ command }) => command === paths.nmake);
    assert.ok(generator.args.includes(`CC=${paths.clangCl}`));
    assert.ok(generator.args.includes(`NCC=${paths.clangCl}`));
    const ordinaryReaderCompile = calls.find(
      ({ args, command }) =>
        command === paths.cc &&
        args.some((argument) => argument.endsWith("ordinary-sqlite-probe.c")),
    );
    assert.ok(ordinaryReaderCompile);
    assert.ok(
      !ordinaryReaderCompile.args.some((argument) => argument.includes("SQLITE_HAS_CODEC")),
    );
    assert.ok(
      !ordinaryReaderCompile.args.some((argument) => argument.includes("SQLCIPHER_CRYPTO")),
    );
    assert.equal(
      result.ordinaryReader,
      path.join(root, ".tmp/sqlcipher/ordinary/win32-x64/ordinary-sqlite3.exe"),
    );
    assert.equal(
      await readFile(
        path.join(root, "apps/desktop/resources/sqlcipher/win32-x64/lib/libcrypto.a"),
        "utf8",
      ),
      "pinned provider archive",
    );
    assert.match(result.opensslLibrarySha256, /^[a-f0-9]{64}$/);
    await assert.rejects(readFile(staleMacLibrary), { code: "ENOENT" });
  });
});

describe("official Windows OpenSSL provider build", () => {
  async function providerFixture() {
    const root = await mkdtemp(path.join(os.tmpdir(), "tammy-openssl-build-test-"));
    temporaryDirectories.push(root);
    const buildRoot = path.join(root, "build");
    const toolsRoot = path.join(root, "tools");
    await Promise.all([mkdir(buildRoot), mkdir(toolsRoot)]);
    const environment = Object.fromEntries(
      [
        ["TAMMY_SQLCIPHER_LIB", "llvm-lib.exe"],
        ["TAMMY_SQLCIPHER_LINK", "lld-link.exe"],
        ["TAMMY_SQLCIPHER_MT", "mt.exe"],
        ["TAMMY_SQLCIPHER_NMAKE", "nmake.exe"],
        ["TAMMY_SQLCIPHER_NMAKE_CC", "clang-cl.exe"],
        ["TAMMY_SQLCIPHER_PERL", "perl.exe"],
        ["TAMMY_SQLCIPHER_RC", "rc.exe"],
      ].map(([key, name]) => [key, path.join(toolsRoot, name)]),
    );
    await Promise.all(Object.values(environment).map((candidate) => writeFile(candidate, "tool")));
    const extractSource = async ({ destination, pin }) => {
      assert.deepEqual(pin, {
        archiveName: "openssl-3.5.7.tar.gz",
        globalPax: "52 comment=8cf17aaeb4599f8af87fefd810b5b5fee90fe69e\n",
        rootDirectory: "openssl-openssl-3.5.7",
        sha256: "d71a811bfbd9153d7b30cbe476263302ee4b04a9a47ffea6e6a782326805c93f",
        url: "https://codeload.github.com/openssl/openssl/tar.gz/refs/tags/openssl-3.5.7",
      });
      const source = path.join(destination, pin.rootDirectory);
      await mkdir(source, { recursive: true });
      await Promise.all([
        writeFile(path.join(source, "Configure"), "configure"),
        writeFile(
          path.join(source, "LICENSE.txt"),
          await readFile(
            path.resolve(import.meta.dirname, "../third_party/sqlcipher/OPENSSL_LICENSE"),
          ),
        ),
      ]);
      return source;
    };
    return { buildRoot, environment, extractSource, root };
  }

  it("pins source, licence, configuration, tools, environment, version, and one archive", async () => {
    const fixture = await providerFixture();
    const calls = [];
    const execFile = async (command, args, options) => {
      calls.push({ args, command, options });
      if (command === fixture.environment.TAMMY_SQLCIPHER_PERL) {
        await writeFile(
          path.join(options.cwd, "makefile"),
          [
            `CC="${fixture.environment.TAMMY_SQLCIPHER_NMAKE_CC}"`,
            `AR = ${fixture.environment.TAMMY_SQLCIPHER_LIB}`,
            `LD := "${fixture.environment.TAMMY_SQLCIPHER_LINK}"`,
            `MT=${fixture.environment.TAMMY_SQLCIPHER_MT}`,
            `RC = "${fixture.environment.TAMMY_SQLCIPHER_RC}"`,
          ].join("\n"),
        );
      }
      if (args.includes("install_dev")) {
        const configure = calls.find(
          ({ command: item }) => item === fixture.environment.TAMMY_SQLCIPHER_PERL,
        );
        const prefix = configure.args.find((argument) => argument.startsWith("--prefix=")).slice(9);
        await mkdir(path.join(prefix, "include/openssl"), { recursive: true });
        await mkdir(path.join(prefix, "lib/VC/x64/MD"), { recursive: true });
        await Promise.all([
          writeFile(
            path.join(prefix, "include/openssl/opensslv.h"),
            '#define OPENSSL_VERSION_STR "3.5.7"\n',
          ),
          writeFile(path.join(prefix, "lib/VC/x64/MD/libcrypto.lib"), "provider"),
        ]);
      }
      return { stderr: "", stdout: "" };
    };
    const result = await buildWindowsProvider({
      buildRoot: fixture.buildRoot,
      environment: fixture.environment,
      execFile,
      extractSource: fixture.extractSource,
      repositoryRoot: path.resolve(import.meta.dirname, ".."),
    });
    assert.equal(await readFile(result.library, "utf8"), "provider");
    const configure = calls[0];
    assert.equal(configure.command, fixture.environment.TAMMY_SQLCIPHER_PERL);
    assert.ok(configure.args.includes("VC-WIN64A"));
    assert.ok(configure.args.includes("no-shared"));
    assert.ok(configure.args.includes("no-tests"));
    assert.equal(configure.options.env.SOURCE_DATE_EPOCH, "1781042040");
    assert.equal(configure.options.env.TZ, "UTC");
    for (const call of calls.slice(1)) {
      assert.ok(call.args.includes(`CC=${fixture.environment.TAMMY_SQLCIPHER_NMAKE_CC}`));
      assert.ok(call.args.includes(`AR=${fixture.environment.TAMMY_SQLCIPHER_LIB}`));
      assert.ok(call.args.includes(`LD=${fixture.environment.TAMMY_SQLCIPHER_LINK}`));
    }
  });

  it("rejects generated toolchain drift before nmake", async () => {
    const fixture = await providerFixture();
    await assert.rejects(
      buildWindowsProvider({
        buildRoot: fixture.buildRoot,
        environment: fixture.environment,
        extractSource: fixture.extractSource,
        repositoryRoot: path.resolve(import.meta.dirname, ".."),
        execFile: async (_command, _args, options) => {
          await writeFile(path.join(options.cwd, "makefile"), "implicit tools\n");
          return { stderr: "", stdout: "" };
        },
      }),
      /SQLCIPHER_OPENSSL_TOOLCHAIN_DRIFT/,
    );
  });

  it("rejects pinned paths that appear only in comments", async () => {
    const fixture = await providerFixture();
    const comments = [
      fixture.environment.TAMMY_SQLCIPHER_NMAKE_CC,
      fixture.environment.TAMMY_SQLCIPHER_LIB,
      fixture.environment.TAMMY_SQLCIPHER_LINK,
      fixture.environment.TAMMY_SQLCIPHER_MT,
      fixture.environment.TAMMY_SQLCIPHER_RC,
    ].map((tool) => `# expected tool: ${tool}`);
    await assert.rejects(
      buildWindowsProvider({
        buildRoot: fixture.buildRoot,
        environment: fixture.environment,
        extractSource: fixture.extractSource,
        repositoryRoot: path.resolve(import.meta.dirname, ".."),
        execFile: async (_command, _args, options) => {
          await writeFile(
            path.join(options.cwd, "makefile"),
            [...comments, "CC=cl", "AR=lib", "LD=link", "MT=mt", "RC=rc"].join("\n"),
          );
          return { stderr: "", stdout: "" };
        },
      }),
      /SQLCIPHER_OPENSSL_TOOLCHAIN_DRIFT/,
    );
  });
});

it("rebuilds the real macOS arm64 static archive byte-for-byte reproducibly", {
  skip:
    process.platform !== "darwin" ||
    process.arch !== "arm64" ||
    process.env.TAMMY_SQLCIPHER_REPRODUCIBILITY !== "1",
}, async () => {
  const first = await buildSqlcipher();
  const firstBytes = await readFile(first.library);
  const second = await buildSqlcipher();
  const secondBytes = await readFile(second.library);
  assert.equal(second.librarySha256, first.librarySha256);
  assert.deepEqual(secondBytes, firstBytes);
});
