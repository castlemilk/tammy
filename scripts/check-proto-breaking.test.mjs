import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { PassThrough } from "node:stream";
import { afterEach, test } from "node:test";

import {
  BASELINE_MESSAGE,
  checkProtoBreaking,
  runBoundedProcess,
} from "./check-proto-breaking.mjs";

const roots = [];

async function validRoot() {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-proto-breaking-"));
  roots.push(root);
  await mkdir(path.join(root, "proto/tammy/v1"), { recursive: true });
  await writeFile(path.join(root, "buf.yaml"), "version: v2\nmodules:\n  - path: proto\n");
  await writeFile(path.join(root, "proto/tammy/v1/system.proto"), 'syntax = "proto3";\n');
  return root;
}

function commandResult({ exitCode = 0, stderr = "", stdout = "" } = {}) {
  return { exitCode, signal: null, stderr, stdout };
}

afterEach(async () => {
  await Promise.all(roots.splice(0).map((root) => rm(root, { force: true, recursive: true })));
});

test("records an explicit initial baseline when master has neither contract input", async () => {
  const root = await validRoot();
  const plans = [];
  const output = [];

  const status = await checkProtoBreaking({
    root,
    run: async (plan) => {
      plans.push(plan);
      return commandResult();
    },
    sourceEnvironment: { HOME: "/safe/home", PATH: "/safe/bin", SECRET_TOKEN: "forbidden" },
    writeOutput: (value) => output.push(value),
  });

  assert.equal(status, "INITIAL_BASELINE");
  assert.deepEqual(output, [`${BASELINE_MESSAGE}\n`]);
  assert.equal(plans.length, 1);
  assert.equal(plans[0].tool, "git");
  assert.deepEqual(plans[0].args, [
    "ls-tree",
    "-r",
    "--name-only",
    "master",
    "--",
    "buf.yaml",
    "proto",
  ]);
  assert.deepEqual(plans[0].env, { HOME: "/safe/home", PATH: "/safe/bin" });
});

test("fails closed for a partial or malformed master baseline", async () => {
  for (const stdout of [
    "buf.yaml\n",
    "proto/tammy/v1/system.proto\n",
    "buf.yaml\n../proto/tammy/v1/system.proto\n",
    "buf.yaml\nproto/tammy/v1/system.proto\nproto/tammy/v1/system.proto\n",
  ]) {
    const root = await validRoot();
    await assert.rejects(
      checkProtoBreaking({
        root,
        run: async () => commandResult({ stdout }),
      }),
      /PROTO_BREAKING_MASTER_BASELINE_(?:PARTIAL|MALFORMED)/,
      stdout,
    );
  }
});

test("fails closed when git inspection fails", async () => {
  const root = await validRoot();
  await assert.rejects(
    checkProtoBreaking({
      root,
      run: async () => commandResult({ exitCode: 128, stderr: "fatal" }),
      writeError: () => {},
    }),
    /PROTO_BREAKING_GIT_INSPECTION_FAILED/,
  );
});

test("requires the current Buf configuration and at least one current contract", async () => {
  const missingConfig = await validRoot();
  await rm(path.join(missingConfig, "buf.yaml"));
  await assert.rejects(
    checkProtoBreaking({ root: missingConfig, run: async () => commandResult() }),
    /PROTO_BREAKING_CURRENT_INPUT_INVALID/,
  );

  const missingContract = await validRoot();
  await rm(path.join(missingContract, "proto"), { recursive: true });
  await mkdir(path.join(missingContract, "proto"));
  await assert.rejects(
    checkProtoBreaking({ root: missingContract, run: async () => commandResult() }),
    /PROTO_BREAKING_CURRENT_INPUT_INVALID/,
  );
});

test("runs the exact Buf breaking arguments for an established master baseline", async () => {
  const root = await validRoot();
  const plans = [];

  const status = await checkProtoBreaking({
    root,
    run: async (plan) => {
      plans.push(plan);
      if (plan.tool === "git") {
        return commandResult({ stdout: "buf.yaml\nproto/tammy/v1/system.proto\n" });
      }
      return commandResult();
    },
  });

  assert.equal(status, "VERIFIED");
  assert.equal(plans.length, 2);
  assert.equal(plans[1].tool, "buf");
  assert.deepEqual(plans[1].args, ["breaking", "--against", ".git#branch=master"]);
});

test("propagates an established Buf breaking failure", async () => {
  const root = await validRoot();
  const errors = [];

  await assert.rejects(
    checkProtoBreaking({
      root,
      run: async (plan) =>
        plan.tool === "git"
          ? commandResult({ stdout: "buf.yaml\nproto/tammy/v1/system.proto\n" })
          : commandResult({ exitCode: 23, stderr: "breaking change\n" }),
      writeError: (value) => errors.push(value),
    }),
    (error) => error.message === "PROTO_BREAKING_FAILED" && error.exitCode === 23,
  );
  assert.deepEqual(errors, ["breaking change\n"]);
});

class FakeChild extends EventEmitter {
  stdout = new PassThrough();
  stderr = new PassThrough();
  kills = [];

  kill(signal) {
    this.kills.push(signal);
    return true;
  }
}

test("a timed-out command escalates and settles only after the child closes", async () => {
  const child = new FakeChild();
  const timers = [];
  const spawnCalls = [];
  const promise = runBoundedProcess(
    {
      args: ["status", "--porcelain=v1"],
      cwd: "/safe/root",
      env: { PATH: "/safe/bin" },
      file: "git",
      maxOutputBytes: 1024,
      reapTimeoutMs: 10,
      terminationGraceMs: 10,
      timeoutMs: 50,
    },
    {
      clearTimer: (timer) => {
        timer.cleared = true;
      },
      setTimer: (callback, milliseconds) => {
        const timer = { callback, cleared: false, milliseconds };
        timers.push(timer);
        return timer;
      },
      spawnProcess: (...args) => {
        spawnCalls.push(args);
        return child;
      },
    },
  );
  let settled = false;
  const observed = promise.then(
    () => {
      settled = true;
      return null;
    },
    (error) => {
      settled = true;
      return error;
    },
  );

  timers.find((timer) => timer.milliseconds === 50).callback();
  await Promise.resolve();
  assert.equal(settled, false);
  assert.deepEqual(child.kills, ["SIGTERM"]);

  timers.find((timer) => timer.milliseconds === 10).callback();
  await Promise.resolve();
  assert.equal(settled, false);
  assert.deepEqual(child.kills, ["SIGTERM", "SIGKILL"]);

  child.emit("close", null, "SIGKILL");
  const error = await observed;
  assert.equal(error.message, "COMMAND_TIMED_OUT");
  assert.deepEqual(spawnCalls[0][2], {
    cwd: "/safe/root",
    env: { PATH: "/safe/bin" },
    shell: false,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });
});
