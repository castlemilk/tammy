import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { EventEmitter } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { PassThrough } from "node:stream";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { launchLocalScenario, runDesktopScenarioOwner } from "./launch-local-scenario.mjs";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

class FakeChild extends EventEmitter {
  pid = 12_345;
  stdout = new PassThrough();
  stderr = new PassThrough();
  killCalls = [];

  kill(signal) {
    this.killCalls.push(signal);
    return true;
  }
}

function rig() {
  const child = new FakeChild();
  const calls = [];
  const signals = new EventEmitter();
  const stdout = [];
  const stderr = [];
  const timers = [];
  const groupKills = [];
  const dependencies = {
    cancelSchedule: (timer) => timers.push(["cancel", timer]),
    makeTemporaryDirectory: async (prefix) => {
      calls.push(["temporary-directory", prefix]);
      return "/private/tmp/tammy-accounting-fresh-fixed";
    },
    platform: "darwin",
    processRunner: (command, arguments_, options) => {
      calls.push(["spawn", command, arguments_, options]);
      return child;
    },
    schedule: (callback, milliseconds) => {
      const timer = { callback, unrefCalls: 0, unref: () => (timer.unrefCalls += 1) };
      timers.push(["schedule", milliseconds, timer]);
      return timer;
    },
    signalSource: signals,
    stderr: { write: (value) => stderr.push(String(value)) },
    stdout: { write: (value) => stdout.push(String(value)) },
    temporaryRoot: "/private/tmp",
    terminateProcessGroup: (pid, signal) => groupKills.push([pid, signal]),
  };
  return { calls, child, dependencies, groupKills, signals, stderr, stdout, timers };
}

test("accounting launches the narrow package owner with no scenario arguments", async () => {
  const harness = rig();
  const completion = launchLocalScenario("accounting", harness.dependencies);

  assert.deepEqual(harness.calls, [
    [
      "spawn",
      "mise",
      ["exec", "--", "pnpm", "desktop:start:scenario"],
      {
        cwd: repositoryRoot,
        detached: true,
        shell: false,
        stdio: ["ignore", "pipe", "pipe"],
      },
    ],
  ]);
  harness.child.emit("close", 0, null);

  await assert.doesNotReject(completion);
  assert.deepEqual(harness.stdout, []);
  assert.deepEqual(harness.stderr, []);
});

test("accounting-fresh creates and retains one unique root passed only as Electron user data", async () => {
  const harness = rig();
  const completion = launchLocalScenario("accounting-fresh", harness.dependencies);
  await Promise.resolve();

  assert.deepEqual(harness.calls, [
    ["temporary-directory", "/private/tmp/tammy-accounting-fresh-"],
    [
      "spawn",
      "mise",
      [
        "exec",
        "--",
        "pnpm",
        "desktop:start:scenario",
        "--",
        "--user-data-dir=/private/tmp/tammy-accounting-fresh-fixed",
        "--tammy-launch-scenario=accounting-fresh",
      ],
      {
        cwd: repositoryRoot,
        detached: true,
        shell: false,
        stdio: ["ignore", "pipe", "pipe"],
      },
    ],
  ]);
  harness.child.emit("close", 0, null);
  harness.child.emit("close", 0, null);

  await assert.doesNotReject(completion);
  assert.deepEqual(harness.stdout, [
    "LOCAL_SCENARIO_RETAINED_ROOT:/private/tmp/tammy-accounting-fresh-fixed\n",
  ]);
  assert.deepEqual(harness.stderr, []);
});

test("pins the child to the Tammy repository when invoked from a foreign cwd", async () => {
  const harness = rig();
  const originalDirectory = process.cwd();
  const foreignDirectory = await mkdtemp(path.join(tmpdir(), "tammy-foreign-cwd-"));
  try {
    process.chdir(foreignDirectory);
    const completion = launchLocalScenario("accounting", harness.dependencies);
    assert.equal(harness.calls[0][3].cwd, repositoryRoot);
    harness.child.emit("close", 0, null);
    await completion;
  } finally {
    process.chdir(originalDirectory);
    await rm(foreignDirectory, { force: true, recursive: true });
  }
});

test("sbr-simulator launches the authenticated profile with isolated retained user data", async () => {
  const harness = rig();
  harness.dependencies.makeTemporaryDirectory = async (prefix) => {
    harness.calls.push(["temporary-directory", prefix]);
    return "/private/tmp/tammy-sbr-simulator-fixed";
  };

  const completion = launchLocalScenario("sbr-simulator", harness.dependencies);
  await Promise.resolve();

  assert.deepEqual(harness.calls, [
    ["temporary-directory", "/private/tmp/tammy-sbr-simulator-"],
    [
      "spawn",
      "mise",
      [
        "exec",
        "--",
        "pnpm",
        "desktop:start:scenario",
        "--",
        "--user-data-dir=/private/tmp/tammy-sbr-simulator-fixed",
        "--tammy-launch-scenario=sbr-simulator",
      ],
      {
        cwd: repositoryRoot,
        detached: true,
        shell: false,
        stdio: ["ignore", "pipe", "pipe"],
      },
    ],
  ]);
  harness.child.emit("close", 0, null);

  await assert.doesNotReject(completion);
  assert.deepEqual(harness.stdout, [
    "LOCAL_SCENARIO_RETAINED_ROOT:/private/tmp/tammy-sbr-simulator-fixed\n",
  ]);
});

test("sbr-evte stays fail-closed without creating a root or child", async () => {
  const harness = rig();

  await assert.rejects(
    launchLocalScenario("sbr-evte", harness.dependencies),
    new Error("SBR_IMPLEMENTATION_INCOMPLETE:sbr-evte"),
  );
  assert.deepEqual(harness.calls, []);
  assert.deepEqual(harness.stdout, []);
  assert.deepEqual(harness.stderr, []);
});

test("rejects extra or unknown inputs instead of forwarding them", async () => {
  const harness = rig();

  await assert.rejects(
    launchLocalScenario("accounting --credential-path=/tmp/key", harness.dependencies),
    new Error("LOCAL_SCENARIO_INVALID"),
  );
  assert.deepEqual(harness.calls, []);
});

test("forwards the first termination signal and finalizes once", async () => {
  const harness = rig();
  const completion = launchLocalScenario("accounting-fresh", harness.dependencies);
  await Promise.resolve();

  harness.signals.emit("SIGTERM");
  harness.signals.emit("SIGINT");
  assert.deepEqual(harness.groupKills, [[-12_345, "SIGTERM"]]);
  harness.child.emit("close", null, "SIGTERM");
  harness.child.emit("close", null, "SIGTERM");

  await assert.rejects(completion, new Error("LOCAL_SCENARIO_CHILD_SIGNAL:SIGTERM"));
  assert.deepEqual(harness.stdout, [
    "LOCAL_SCENARIO_RETAINED_ROOT:/private/tmp/tammy-accounting-fresh-fixed\n",
  ]);
  assert.equal(harness.signals.listenerCount("SIGINT"), 0);
  assert.equal(harness.signals.listenerCount("SIGTERM"), 0);
  assert.equal(harness.timers.length, 2);
  assert.deepEqual(harness.timers[0].slice(0, 2), ["schedule", 5_000]);
  assert.equal(harness.timers[1][0], "cancel");
  assert.equal(harness.timers[1][1], harness.timers[0][2]);
  assert.equal(harness.timers[0][2].unrefCalls, 1);
  harness.timers[0][2].callback();
  assert.deepEqual(harness.groupKills, [
    [-12_345, "SIGTERM"],
    [-12_345, "SIGKILL"],
  ]);
});

test("bounds child output and escalates an unresponsive process group", async () => {
  const harness = rig();
  const completion = launchLocalScenario("accounting", harness.dependencies);
  let settled = false;
  void completion.then(
    () => (settled = true),
    () => (settled = true),
  );
  harness.child.stdout.write("x".repeat(65_537));
  await Promise.resolve();
  assert.equal(settled, false);
  assert.deepEqual(harness.groupKills, [[-12_345, "SIGTERM"]]);
  assert.equal(harness.timers[0][2].unrefCalls, 1);
  harness.timers[0][2].callback();
  assert.deepEqual(harness.groupKills, [
    [-12_345, "SIGTERM"],
    [-12_345, "SIGKILL"],
  ]);
  await Promise.resolve();
  assert.equal(settled, false);
  harness.child.emit("close", null, "SIGTERM");

  await assert.rejects(completion, new Error("LOCAL_SCENARIO_OUTPUT_LIMIT_EXCEEDED"));
  assert.equal(harness.stdout.join("").length, 65_536);
});

test("keeps the retained-root marker on its own line after partial child stdout", async () => {
  const harness = rig();
  const completion = launchLocalScenario("accounting-fresh", harness.dependencies);
  await Promise.resolve();
  harness.child.stdout.write("partial child line");
  harness.child.emit("close", 0, null);

  await completion;
  assert.equal(
    harness.stdout.join(""),
    "partial child line\nLOCAL_SCENARIO_RETAINED_ROOT:/private/tmp/tammy-accounting-fresh-fixed\n",
  );
});

test("uses direct child termination for supported Windows accounting", async () => {
  const harness = rig();
  harness.dependencies.platform = "win32";
  const completion = launchLocalScenario("accounting", harness.dependencies);
  harness.signals.emit("SIGINT");
  assert.deepEqual(harness.groupKills, []);
  assert.deepEqual(harness.child.killCalls, ["SIGINT"]);
  harness.child.emit("close", null, "SIGINT");
  await assert.rejects(completion, new Error("LOCAL_SCENARIO_CHILD_SIGNAL:SIGINT"));
  assert.deepEqual(harness.child.killCalls, ["SIGINT", "SIGKILL"]);
});

test("treats an already-exited macOS process group as a safe termination race", async () => {
  const harness = rig();
  harness.dependencies.terminateProcessGroup = () => {
    const error = new Error("gone");
    error.code = "ESRCH";
    throw error;
  };
  const completion = launchLocalScenario("accounting", harness.dependencies);
  harness.signals.emit("SIGTERM");
  harness.child.emit("close", null, "SIGTERM");
  await assert.rejects(completion, new Error("LOCAL_SCENARIO_CHILD_SIGNAL:SIGTERM"));
  assert.deepEqual(harness.child.killCalls, []);
});

test("an error followed by close finalizes and reports a retained root exactly once", async () => {
  const harness = rig();
  const completion = launchLocalScenario("accounting-fresh", harness.dependencies);
  await Promise.resolve();
  harness.child.emit("error", new Error("host detail"));
  harness.child.emit("close", 1, null);
  await assert.rejects(completion, new Error("LOCAL_SCENARIO_START_FAILED"));
  assert.deepEqual(harness.stdout, [
    "LOCAL_SCENARIO_RETAINED_ROOT:/private/tmp/tammy-accounting-fresh-fixed\n",
  ]);
  assert.equal(harness.signals.listenerCount("SIGINT"), 0);
  assert.equal(harness.signals.listenerCount("SIGTERM"), 0);
});

test("desktop package owner forwards only validated Electron Forge arguments", async () => {
  const children = [new FakeChild(), new FakeChild()];
  const calls = [];
  const processRunner = (command, arguments_, options) => {
    calls.push([command, arguments_, options]);
    const child = children.shift();
    queueMicrotask(() => child.emit("close", 0, null));
    return child;
  };

  await runDesktopScenarioOwner(
    [
      "--",
      "--user-data-dir=/private/tmp/tammy-accounting-fresh-fixed",
      "--tammy-launch-scenario=accounting-fresh",
    ],
    {
      processRunner,
      temporaryRoot: "/private/tmp",
    },
  );
  assert.deepEqual(calls, [
    ["pnpm", ["core:build"], { cwd: repositoryRoot, shell: false, stdio: "inherit" }],
    [
      "pnpm",
      [
        "--dir",
        "apps/desktop",
        "start",
        "--",
        "--user-data-dir=/private/tmp/tammy-accounting-fresh-fixed",
        "--tammy-launch-scenario=accounting-fresh",
      ],
      { cwd: repositoryRoot, shell: false, stdio: "inherit" },
    ],
  ]);
});

test("desktop package owner accepts only the owned SBR simulator root and explicit authority", async () => {
  const children = [new FakeChild(), new FakeChild()];
  const calls = [];
  const processRunner = (command, arguments_, options) => {
    calls.push([command, arguments_, options]);
    const child = children.shift();
    queueMicrotask(() => child.emit("close", 0, null));
    return child;
  };

  await runDesktopScenarioOwner(
    [
      "--",
      "--user-data-dir=/private/tmp/tammy-sbr-simulator-fixed",
      "--tammy-launch-scenario=sbr-simulator",
    ],
    {
      processRunner,
      temporaryRoot: "/private/tmp",
    },
  );

  assert.equal(calls.length, 2);
  assert.deepEqual(calls[1][1], [
    "--dir",
    "apps/desktop",
    "start",
    "--",
    "--user-data-dir=/private/tmp/tammy-sbr-simulator-fixed",
    "--tammy-launch-scenario=sbr-simulator",
  ]);
});

test("desktop package owner rejects arbitrary arguments before starting a child", async () => {
  for (const arguments_ of [
    ["--inspect=9229"],
    ["--user-data-dir=relative"],
    ["--user-data-dir=/private/tmp/arbitrary"],
    ["--user-data-dir=/private/tmp/one", "--user-data-dir=/private/tmp/two"],
    [
      "--user-data-dir=/private/tmp/tammy-accounting-fresh-fixed",
      "--tammy-launch-scenario=sbr-simulator",
    ],
    [
      "--user-data-dir=/private/tmp/tammy-sbr-simulator-fixed",
      "--tammy-launch-scenario=accounting-fresh",
    ],
    ["--sbr-profile=/private/tmp/profile.json"],
  ]) {
    const calls = [];
    await assert.rejects(
      runDesktopScenarioOwner(arguments_, {
        processRunner: (...runnerArguments) => calls.push(runnerArguments),
        temporaryRoot: "/private/tmp",
      }),
      new Error("LOCAL_SCENARIO_OWNER_ARGUMENTS_INVALID"),
    );
    assert.deepEqual(calls, []);
  }
});

test("desktop:start:scenario reaches the rejecting owner instead of a generic shell", async () => {
  const child = spawn(
    "mise",
    ["exec", "--", "pnpm", "desktop:start:scenario", "--", "--inspect=9229"],
    {
      cwd: repositoryRoot,
      shell: false,
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  let output = "";
  child.stdout.on("data", (chunk) => (output += chunk));
  child.stderr.on("data", (chunk) => (output += chunk));
  const result = await new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("close", (code, signal) => resolve({ code, signal }));
  });

  assert.notEqual(result.code, 0);
  assert.equal(result.signal, null);
  assert.match(output, /LOCAL_SCENARIO_OWNER_ARGUMENTS_INVALID/u);
  assert.doesNotMatch(output, /core:build/u);
});

async function waitForProcessExit(pid) {
  const deadline = Date.now() + 3_000;
  while (Date.now() < deadline) {
    try {
      process.kill(pid, 0);
    } catch (error) {
      if (error?.code === "ESRCH") return;
      throw error;
    }
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
  assert.fail(`descendant process ${pid} survived termination`);
}

for (const signal of ["SIGINT", "SIGTERM"]) {
  test(`macOS ${signal} forwarding terminates the complete descendant process group`, {
    skip: process.platform !== "darwin",
    timeout: 10_000,
  }, async () => {
    const signalSource = new EventEmitter();
    let parentPid;
    let grandchildPid;
    let output = "";
    let ready;
    const grandchildReady = new Promise((resolve) => (ready = resolve));
    const parentSource = [
      'const { spawn } = require("node:child_process")',
      'const child = spawn(process.execPath, ["-e", "process.on(\\"SIGINT\\", () => {}); process.on(\\"SIGTERM\\", () => {}); setInterval(() => {}, 1000)"], { stdio: "ignore" })',
      "process.stdout.write('GRANDCHILD:' + child.pid + '\\n')",
      "setInterval(() => {}, 1000)",
    ].join(";");
    const completion = launchLocalScenario("accounting", {
      processRunner: (_command, _arguments, options) => {
        const child = spawn(process.execPath, ["-e", parentSource], options);
        parentPid = child.pid;
        return child;
      },
      signalSource,
      stderr: { write: () => undefined },
      stdout: {
        write: (chunk) => {
          output += String(chunk);
          const match = output.match(/GRANDCHILD:(\d+)/u);
          if (match && grandchildPid === undefined) {
            grandchildPid = Number(match[1]);
            ready();
          }
        },
      },
    });

    try {
      await grandchildReady;
      signalSource.emit(signal);
      await assert.rejects(completion, new Error(`LOCAL_SCENARIO_CHILD_SIGNAL:${signal}`));
      await waitForProcessExit(grandchildPid);
    } finally {
      if (parentPid !== undefined) {
        try {
          process.kill(-parentPid, "SIGKILL");
        } catch {
          // Best-effort cleanup; the survival assertion above owns orphan evidence.
        }
      }
    }
  });
}

test("maps spawn and nonzero child exits to deterministic errors", async (context) => {
  await context.test("temporary-directory failure", async () => {
    const harness = rig();
    harness.dependencies.makeTemporaryDirectory = async () => {
      throw new Error("host detail");
    };
    await assert.rejects(
      launchLocalScenario("accounting-fresh", harness.dependencies),
      new Error("LOCAL_SCENARIO_TEMPORARY_ROOT_FAILED"),
    );
    assert.deepEqual(harness.calls, []);
  });

  await context.test("synchronous spawn failure", async () => {
    const harness = rig();
    harness.dependencies.processRunner = () => {
      throw new Error("host detail");
    };
    await assert.rejects(
      launchLocalScenario("accounting", harness.dependencies),
      new Error("LOCAL_SCENARIO_START_FAILED"),
    );
  });

  await context.test("spawn failure", async () => {
    const harness = rig();
    const completion = launchLocalScenario("accounting", harness.dependencies);
    harness.child.emit("error", new Error("host detail"));
    await assert.rejects(completion, new Error("LOCAL_SCENARIO_START_FAILED"));
  });

  await context.test("child failure", async () => {
    const harness = rig();
    const completion = launchLocalScenario("accounting", harness.dependencies);
    harness.child.emit("close", 7, null);
    await assert.rejects(completion, new Error("LOCAL_SCENARIO_CHILD_EXIT:7"));
  });
});
