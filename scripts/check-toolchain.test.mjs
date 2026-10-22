import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  createToolCommandPlan,
  executeToolCommandPlan,
  validateToolVersions,
} from "./check-toolchain.mjs";

function assertPlanUsesNoShellShims(plan) {
  for (const command of Object.values(plan)) {
    assert.equal("shell" in command, false);
    assert.doesNotMatch(command.file, /\.(?:cmd|bat)$/i);
    for (const argument of command.args) {
      assert.doesNotMatch(argument, /\.(?:cmd|bat)$/i);
    }
  }
}

test("imports without argv or tool execution", () => {
  const checkerUrl = new URL("./check-toolchain.mjs", import.meta.url).href;
  const result = spawnSync(
    process.execPath,
    ["--input-type=module", "-e", `await import(${JSON.stringify(checkerUrl)})`],
    { encoding: "utf8" },
  );

  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout, "");
  assert.equal(result.stderr, "");
});

test("creates a win32 command plan without cmd or bat shims", () => {
  const nodeExecutable = "C:\\mise\\node\\node.exe";
  const corepackEntry = "C:\\mise\\node\\node_modules\\corepack\\dist\\corepack.js";
  const bufEntry = "C:\\repo\\node_modules\\@bufbuild\\buf\\bin\\buf";
  const regularFiles = new Set([
    nodeExecutable,
    corepackEntry,
    bufEntry,
    "C:\\tools\\pnpm.cmd",
    "C:\\tools\\pnpm.bat",
  ]);

  for (const npmExecPath of ["C:\\tools\\pnpm.cmd", "C:\\tools\\pnpm.bat"]) {
    const plan = createToolCommandPlan({
      platform: "win32",
      nodeExecutable,
      npmExecPath,
      bufEntry,
      isRegularFile: (candidate) => regularFiles.has(candidate),
    });

    assert.deepEqual(plan, {
      node: { file: nodeExecutable, args: ["--version"] },
      pnpm: { file: nodeExecutable, args: [corepackEntry, "pnpm", "--version"] },
      go: { file: "go.exe", args: ["version"] },
      buf: { file: nodeExecutable, args: [bufEntry, "--version"] },
    });
    assertPlanUsesNoShellShims(plan);
  }
});

test("prefers an absolute pnpm JavaScript entry in a darwin command plan", () => {
  const nodeExecutable = "/mise/installs/node/24.18.0/bin/node";
  const npmExecPath = "/corepack/pnpm/11.15.0/dist/pnpm.mjs";
  const bufEntry = "/repo/node_modules/@bufbuild/buf/bin/buf";
  const regularFiles = new Set([nodeExecutable, npmExecPath, bufEntry]);
  const plan = createToolCommandPlan({
    platform: "darwin",
    nodeExecutable,
    npmExecPath,
    bufEntry,
    isRegularFile: (candidate) => regularFiles.has(candidate),
  });

  assert.deepEqual(plan, {
    node: { file: nodeExecutable, args: ["--version"] },
    pnpm: { file: nodeExecutable, args: [npmExecPath, "--version"] },
    go: { file: "go", args: ["version"] },
    buf: { file: nodeExecutable, args: [bufEntry, "--version"] },
  });
  assertPlanUsesNoShellShims(plan);
});

test("reports command execution failures without leaking the underlying error", () => {
  const secret = "DO_NOT_LEAK_ENVIRONMENT_VALUE";
  assert.throws(
    () =>
      executeToolCommandPlan(
        {
          pnpm: {
            file: "/mise/installs/node/24.18.0/bin/node",
            args: ["/corepack/pnpm/11.15.0/dist/pnpm.mjs", "--version"],
          },
        },
        () => {
          throw new Error(secret);
        },
      ),
    (error) => {
      assert.equal(error.message, "Unable to execute pnpm version check");
      assert.doesNotMatch(error.stack, new RegExp(secret));
      return true;
    },
  );
});

test("runs the current platform CLI without shell shims", () => {
  const checkerPath = fileURLToPath(new URL("./check-toolchain.mjs", import.meta.url));
  const result = spawnSync(process.execPath, [checkerPath], { encoding: "utf8" });

  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout, "toolchain ok\n");
  assert.equal(result.stderr, "");
});

test("accepts the exact pinned toolchain versions", () => {
  assert.deepEqual(
    validateToolVersions({
      node: "v24.18.0",
      pnpm: "11.15.0",
      go: "go version go1.26.4 darwin/arm64",
      buf: "1.72.0",
    }),
    [],
  );
});

test("reports every mismatched toolchain version", () => {
  assert.deepEqual(
    validateToolVersions({
      node: "v22.20.0",
      pnpm: "10.9.3",
      go: "go version go1.26.3 darwin/arm64",
      buf: "1.71.0",
    }),
    [
      "Node must be v24.18.0 (received v22.20.0)",
      "pnpm must be 11.15.0 (received 10.9.3)",
      "Go must be go1.26.4 (received go1.26.3)",
      "Buf must be 1.72.0 (received 1.71.0)",
    ],
  );
});
