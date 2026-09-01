import assert from "node:assert/strict";
import test from "node:test";

import {
  electronBundleForExecutable,
  prepareElectronRuntime,
} from "./prepare-electron-runtime.mjs";

const executable = "/repo/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron";
const bundle = "/repo/node_modules/electron/dist/Electron.app";
const fileCheck = (candidate) => candidate === executable;
const directoryCheck = (candidate) => candidate === bundle;

test("derives only the exact Electron macOS bundle", () => {
  assert.equal(electronBundleForExecutable(executable, fileCheck, directoryCheck), bundle);
  assert.throws(
    () =>
      electronBundleForExecutable(
        "/repo/Other.app/Contents/MacOS/Other",
        fileCheck,
        directoryCheck,
      ),
    /ELECTRON_RUNTIME_INVALID/,
  );
});

test("does nothing on platforms that do not require macOS signing", () => {
  let calls = 0;
  assert.equal(
    prepareElectronRuntime({
      executable: "not-used",
      platform: "linux",
      run: () => {
        calls += 1;
        return { status: 0 };
      },
    }),
    "not-required",
  );
  assert.equal(calls, 0);
});

test("keeps a valid Electron runtime unchanged", () => {
  const calls = [];
  const result = prepareElectronRuntime({
    directoryCheck,
    executable,
    fileCheck,
    platform: "darwin",
    run: (command, args, options) => {
      calls.push({ args, command, options });
      return { status: 0 };
    },
  });

  assert.equal(result, "verified");
  assert.deepEqual(
    calls.map(({ args }) => args),
    [["--verify", "--deep", "--strict", bundle]],
  );
  assert.equal(calls[0].command, "/usr/bin/codesign");
  assert.equal(calls[0].options.shell, false);
});

test("ad-hoc signs an invalid Electron runtime and verifies the result", () => {
  const calls = [];
  const statuses = [1, 0, 0];
  const result = prepareElectronRuntime({
    directoryCheck,
    executable,
    fileCheck,
    platform: "darwin",
    run: (_command, args) => {
      calls.push(args);
      return { status: statuses.shift() };
    },
  });

  assert.equal(result, "repaired");
  assert.deepEqual(calls, [
    ["--verify", "--deep", "--strict", bundle],
    ["--force", "--deep", "--sign", "-", bundle],
    ["--verify", "--deep", "--strict", bundle],
  ]);
});

test("fails closed when the repaired runtime does not verify", () => {
  const statuses = [1, 0, 1];
  assert.throws(
    () =>
      prepareElectronRuntime({
        directoryCheck,
        executable,
        fileCheck,
        platform: "darwin",
        run: () => ({ status: statuses.shift() }),
      }),
    /ELECTRON_RUNTIME_SIGNING_FAILED/,
  );
});
