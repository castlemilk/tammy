import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  readPreparedTestElectron,
  resolveTestElectronPackageRoot,
} from "../apps/desktop/tests/helpers/prepared-electron.ts";
import { ELECTRON_TEST_SETUP_TIMEOUT_MS, prepareTestElectron } from "./prepare-test-electron.mjs";

async function fixture(context) {
  const desktopRoot = await realpath(
    await mkdtemp(path.join(os.tmpdir(), "tammy-electron-dependency-")),
  );
  context.after(() => rm(desktopRoot, { force: true, recursive: true }));
  const packageRoot = path.join(desktopRoot, "node_modules", "electron");
  await mkdir(packageRoot, { recursive: true });
  await writeFile(path.join(desktopRoot, "package.json"), '{"private":true}\n');
  await writeFile(
    path.join(packageRoot, "package.json"),
    '{"name":"electron","version":"43.1.1","main":"index.js"}\n',
  );
  // Reproduces the old test's unsafe entry point without a real download or network.
  await writeFile(path.join(packageRoot, "index.js"), 'throw Error("IMPLICIT_INSTALLER_ENTERED");');
  return { desktopRoot, packageRoot };
}

async function installed(packageRoot, platform = process.platform) {
  const relative =
    platform === "darwin"
      ? "Electron.app/Contents/MacOS/Electron"
      : platform === "win32"
        ? "electron.exe"
        : "electron";
  const executable = path.join(packageRoot, "dist", relative);
  await mkdir(path.dirname(executable), { recursive: true });
  await writeFile(executable, "synthetic executable");
  await chmod(executable, 0o755);
  await writeFile(path.join(packageRoot, "dist", "version"), "43.1.1");
  await writeFile(path.join(packageRoot, "path.txt"), relative);
  return executable;
}

test("the runtime dependency check never enters Electron's implicit installer", async (context) => {
  const { desktopRoot, packageRoot } = await fixture(context);
  const require = createRequire(path.join(desktopRoot, "package.json"));
  assert.throws(() => require("electron"), /IMPLICIT_INSTALLER_ENTERED/);
  assert.equal(await resolveTestElectronPackageRoot(desktopRoot), packageRoot);
  await assert.rejects(readPreparedTestElectron(packageRoot), /ELECTRON_TEST_DEPENDENCY_NOT_READY/);
  const executable = await installed(packageRoot);
  assert.equal(await readPreparedTestElectron(packageRoot), executable);
});

test("prepared binaries use the pinned native platform path", async (context) => {
  for (const platform of ["darwin", "linux", "win32"]) {
    const { packageRoot } = await fixture(context);
    assert.equal(await readPreparedTestElectron(packageRoot, platform).catch(() => null), null);
    const executable = await installed(packageRoot, platform);
    assert.equal(await readPreparedTestElectron(packageRoot, platform), executable);
    await writeFile(path.join(packageRoot, "dist", "version"), "43.1.0");
    await assert.rejects(readPreparedTestElectron(packageRoot, platform), /DEPENDENCY_NOT_READY/);
  }
});

test("an already prepared dependency does not invoke setup or change files", async (context) => {
  const { desktopRoot, packageRoot } = await fixture(context);
  const executable = await installed(packageRoot);
  assert.equal(
    await prepareTestElectron({
      desktopRoot,
      runInstaller: () => assert.fail("installer must not run for a prepared dependency"),
    }),
    executable,
  );
  assert.equal(await readFile(executable, "utf8"), "synthetic executable");
});

test("explicit setup uses the pinned installer with a separate finite deadline", async (context) => {
  const { desktopRoot, packageRoot } = await fixture(context);
  const executable = await prepareTestElectron({
    desktopRoot,
    runInstaller: async (command, args, options) => {
      assert.equal(command, process.execPath);
      assert.deepEqual(args, [path.join(packageRoot, "install.js")]);
      assert.equal(options.shell, false);
      assert.equal(options.timeout, 120_000);
      assert.equal(options.timeout, ELECTRON_TEST_SETUP_TIMEOUT_MS);
      assert.equal(options.maxBuffer, 256 * 1024);
      assert.equal(options.killSignal, "SIGKILL");
      assert.equal(options.env.ELECTRON_INSTALL_PLATFORM, process.platform);
      assert.equal(options.env.ELECTRON_INSTALL_ARCH, process.arch);
      await installed(packageRoot);
    },
  });
  assert.equal(executable, await readPreparedTestElectron(packageRoot));
});

test("a successful installer cannot hide missing executable output", async (context) => {
  const { desktopRoot } = await fixture(context);
  await assert.rejects(
    prepareTestElectron({ desktopRoot, runInstaller: async () => ({ stdout: "", stderr: "" }) }),
    (error) => {
      assert.equal(error.message, "ELECTRON_TEST_DEPENDENCY_SETUP_FAILED");
      assert.match(error.cause.message, /ELECTRON_TEST_DEPENDENCY_NOT_READY/);
      return true;
    },
  );
});

test("a real setup child completes acquisition before runtime can resolve the binary", async (context) => {
  const { desktopRoot, packageRoot } = await fixture(context);
  await writeFile(
    path.join(packageRoot, "install.js"),
    `const fs = require("node:fs");
const path = require("node:path");
setTimeout(() => {
  const relative = process.platform === "darwin" ? "Electron.app/Contents/MacOS/Electron" : process.platform === "win32" ? "electron.exe" : "electron";
  const executable = path.join(__dirname, "dist", relative);
  fs.mkdirSync(path.dirname(executable), {recursive:true});
  fs.writeFileSync(executable, "synthetic executable", {mode:0o755});
  fs.writeFileSync(path.join(__dirname, "dist/version"), "43.1.1");
  fs.writeFileSync(path.join(__dirname, "path.txt"), relative);
}, 80);
`,
  );
  await assert.rejects(readPreparedTestElectron(packageRoot), /DEPENDENCY_NOT_READY/);
  const executable = await prepareTestElectron({ desktopRoot, timeoutMs: 3000 });
  assert.equal(executable, await readPreparedTestElectron(packageRoot));
});

test("setup rejects a different package pin without running any installer", async (context) => {
  const { desktopRoot, packageRoot } = await fixture(context);
  await writeFile(path.join(packageRoot, "package.json"), '{"name":"electron","version":"43.0.0"}');
  await assert.rejects(
    prepareTestElectron({ desktopRoot, runInstaller: () => assert.fail("wrong pin") }),
    /ELECTRON_TEST_PACKAGE_PIN_MISMATCH/,
  );
});

test("a hanging real setup child is terminated at its finite deadline", {
  timeout: 5000,
}, async (context) => {
  const { desktopRoot, packageRoot } = await fixture(context);
  await writeFile(path.join(packageRoot, "install.js"), "setInterval(() => {}, 1000);");
  await assert.rejects(prepareTestElectron({ desktopRoot, timeoutMs: 200 }), (error) => {
    assert.equal(error.message, "ELECTRON_TEST_DEPENDENCY_SETUP_FAILED");
    assert.equal(error.cause.killed, true);
    return true;
  });
});

test("a failing real setup child preserves its exit code and diagnostics", async (context) => {
  const { desktopRoot, packageRoot } = await fixture(context);
  await writeFile(
    path.join(packageRoot, "install.js"),
    'process.stderr.write("synthetic installer failure"); process.exit(17);',
  );
  await assert.rejects(prepareTestElectron({ desktopRoot }), (error) => {
    assert.equal(error.message, "ELECTRON_TEST_DEPENDENCY_SETUP_FAILED");
    assert.equal(error.cause.code, 17);
    assert.match(error.cause.stderr, /synthetic installer failure/);
    return true;
  });
});

test("the desktop front door prepares dependencies before entering Vitest", async () => {
  const manifest = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  assert.equal(
    manifest.scripts["desktop:test"],
    "node scripts/prepare-test-electron.mjs && pnpm --dir apps/desktop test",
  );
});

test("setup cannot disable or expand its maximum failure deadline", async () => {
  for (const timeoutMs of [0, -1, Number.POSITIVE_INFINITY, 120_001]) {
    await assert.rejects(prepareTestElectron({ timeoutMs }), /ELECTRON_TEST_SETUP_TIMEOUT_INVALID/);
  }
});
