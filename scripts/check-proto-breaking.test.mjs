import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { PassThrough } from "node:stream";
import { afterEach, test } from "node:test";
import { setTimeout as delay } from "node:timers/promises";

import {
  BASELINE_MESSAGE,
  checkProtoBreaking,
  killProcessTree,
  resolveNativeBufExecutable,
  runBoundedProcess,
  runToolPlan,
  sanitizeCommandEnvironment,
} from "./check-proto-breaking.mjs";

const roots = [];
const LOCAL_MASTER_OID = "b".repeat(40);
const REMOTE_MASTER_OID = "a".repeat(40);

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

function masterRunner({
  bufResult = commandResult(),
  entries = "",
  localOid = null,
  plans = [],
  remoteOid = REMOTE_MASTER_OID,
} = {}) {
  return async (plan) => {
    plans.push(plan);
    if (plan.tool === "buf") return bufResult;
    if (plan.args[0] === "rev-parse") {
      const reference = plan.args.at(-1).replace(/\^\{commit\}$/, "");
      const oid =
        reference === "refs/remotes/origin/master"
          ? remoteOid
          : reference === "refs/heads/master"
            ? localOid
            : null;
      return oid === null ? commandResult({ exitCode: 1 }) : commandResult({ stdout: `${oid}\n` });
    }
    if (plan.args[0] === "ls-tree") return commandResult({ stdout: entries });
    throw new Error(`unexpected git plan: ${plan.args.join(" ")}`);
  };
}

async function git(root, args) {
  const result = await runToolPlan({
    args,
    cwd: root,
    env: sanitizeCommandEnvironment(process.env),
    maxOutputBytes: 1024 * 1024,
    reapTimeoutMs: 1000,
    terminationGraceMs: 1000,
    timeoutMs: 10_000,
    tool: "git",
  });
  assert.equal(result.exitCode, 0, result.stderr);
  return result.stdout.trim();
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
    run: masterRunner({ localOid: LOCAL_MASTER_OID, plans, remoteOid: null }),
    sourceEnvironment: { HOME: "/safe/home", PATH: "/safe/bin", SECRET_TOKEN: "forbidden" },
    writeOutput: (value) => output.push(value),
  });

  assert.equal(status, "INITIAL_BASELINE");
  assert.deepEqual(output, [`${BASELINE_MESSAGE}\n`]);
  assert.equal(plans.length, 3);
  assert.deepEqual(plans[0].args, [
    "rev-parse",
    "--verify",
    "--quiet",
    "refs/remotes/origin/master^{commit}",
  ]);
  assert.deepEqual(plans[1].args, [
    "rev-parse",
    "--verify",
    "--quiet",
    "refs/heads/master^{commit}",
  ]);
  assert.deepEqual(plans[2].args, [
    "ls-tree",
    "-r",
    "--name-only",
    LOCAL_MASTER_OID,
    "--",
    "buf.yaml",
    "proto",
  ]);
  assert.deepEqual(plans[0].env, { HOME: "/safe/home", PATH: "/safe/bin" });
});

test("records an initial baseline from a real local-only master checkout", async () => {
  const root = await validRoot();
  await git(root, ["init", "--initial-branch=master"]);
  await git(root, ["config", "user.email", "ci@example.invalid"]);
  await git(root, ["config", "user.name", "Foundation CI"]);
  await git(root, ["commit", "--allow-empty", "-m", "empty baseline"]);

  const output = [];
  const status = await checkProtoBreaking({
    root,
    writeOutput: (value) => output.push(value),
  });

  assert.equal(status, "INITIAL_BASELINE");
  assert.deepEqual(output, [`${BASELINE_MESSAGE}\n`]);
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
        run: masterRunner({ entries: stdout }),
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
      run: async (plan) =>
        plan.args[0] === "rev-parse"
          ? commandResult({ stdout: `${REMOTE_MASTER_OID}\n` })
          : commandResult({ exitCode: 128, stderr: "fatal" }),
      writeError: () => {},
    }),
    /PROTO_BREAKING_GIT_INSPECTION_FAILED/,
  );
});

test("fails closed when no fixed master reference can be resolved", async () => {
  const root = await validRoot();
  await assert.rejects(
    checkProtoBreaking({
      root,
      run: masterRunner({ localOid: null, remoteOid: null }),
    }),
    /PROTO_BREAKING_MASTER_REF_MISSING/,
  );
});

test("fails closed when a fixed master reference resolves to malformed output", async () => {
  const root = await validRoot();
  await assert.rejects(
    checkProtoBreaking({
      root,
      run: async () => commandResult({ stdout: "master^{commit}\n" }),
    }),
    /PROTO_BREAKING_MASTER_REF_MALFORMED/,
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

test("prefers a remote-only origin/master and runs Buf against its exact commit", async () => {
  const root = await validRoot();
  const plans = [];

  const status = await checkProtoBreaking({
    root,
    run: masterRunner({
      entries: "buf.yaml\nproto/tammy/v1/system.proto\n",
      localOid: LOCAL_MASTER_OID,
      plans,
    }),
  });

  assert.equal(status, "VERIFIED");
  assert.equal(plans.length, 3);
  assert.deepEqual(plans[0].args, [
    "rev-parse",
    "--verify",
    "--quiet",
    "refs/remotes/origin/master^{commit}",
  ]);
  assert.deepEqual(plans[1].args, [
    "ls-tree",
    "-r",
    "--name-only",
    REMOTE_MASTER_OID,
    "--",
    "buf.yaml",
    "proto",
  ]);
  assert.equal(plans[2].tool, "buf");
  assert.deepEqual(plans[2].args, ["breaking", "--against", `.git#ref=${REMOTE_MASTER_OID}`]);
});

test("verifies a real checkout with only refs/remotes/origin/master", async () => {
  const root = await validRoot();
  await git(root, ["init", "--initial-branch=checkout"]);
  await git(root, ["config", "user.email", "ci@example.invalid"]);
  await git(root, ["config", "user.name", "Foundation CI"]);
  await git(root, ["add", "buf.yaml", "proto"]);
  await git(root, ["commit", "-m", "baseline"]);
  const oid = await git(root, ["rev-parse", "HEAD"]);
  await git(root, ["update-ref", "refs/remotes/origin/master", oid]);

  const bufPlans = [];
  const status = await checkProtoBreaking({
    root,
    run: async (plan) => {
      if (plan.tool === "buf") bufPlans.push(plan);
      return runToolPlan(plan);
    },
  });

  assert.equal(status, "VERIFIED");
  assert.equal(bufPlans.length, 1);
  assert.deepEqual(bufPlans[0].args, ["breaking", "--against", `.git#ref=${oid}`]);
});

test("propagates an established Buf breaking failure", async () => {
  const root = await validRoot();
  const errors = [];

  await assert.rejects(
    checkProtoBreaking({
      root,
      run: masterRunner({
        bufResult: commandResult({ exitCode: 23, stderr: "breaking change\n" }),
        entries: "buf.yaml\nproto/tammy/v1/system.proto\n",
      }),
      writeError: (value) => errors.push(value),
    }),
    (error) => error.message === "PROTO_BREAKING_FAILED" && error.exitCode === 23,
  );
  assert.deepEqual(errors, ["breaking change\n"]);
});

test("runs the pinned native Buf executable directly without a JavaScript shim", async () => {
  const executions = [];
  const result = await runToolPlan(
    {
      args: ["breaking", "--against", `.git#ref=${REMOTE_MASTER_OID}`],
      cwd: "/safe/root",
      env: { PATH: "/safe/bin" },
      maxOutputBytes: 1024,
      reapTimeoutMs: 10,
      terminationGraceMs: 10,
      timeoutMs: 50,
      tool: "buf",
    },
    {
      resolveBufExecutable: async () => "/safe/@bufbuild/buf-linux-x64/bin/buf",
      runProcess: async (plan) => {
        executions.push(plan);
        return commandResult();
      },
    },
  );

  assert.deepEqual(result, commandResult());
  assert.equal(executions.length, 1);
  assert.equal(executions[0].file, "/safe/@bufbuild/buf-linux-x64/bin/buf");
  assert.deepEqual(executions[0].args, ["breaking", "--against", `.git#ref=${REMOTE_MASTER_OID}`]);
});

test("rejects a JavaScript launcher masquerading as the native Buf executable", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-native-buf-"));
  roots.push(root);
  const packageRoot = path.join(root, "@bufbuild/buf-linux-x64");
  const manifest = path.join(packageRoot, "package.json");
  const executable = path.join(packageRoot, "bin/buf");
  await mkdir(path.dirname(executable), { recursive: true });
  await writeFile(manifest, '{"name":"@bufbuild/buf-linux-x64"}\n');
  await writeFile(executable, "#!/usr/bin/env node\nsetInterval(() => {}, 1000);\n");
  await chmod(executable, 0o755);

  await assert.rejects(
    resolveNativeBufExecutable({
      architecture: "x64",
      packageResolve: (specifier) => (specifier.endsWith("/package.json") ? manifest : executable),
      platform: "linux",
    }),
    /PROTO_BREAKING_BUF_EXECUTABLE_INVALID/,
  );
});

test("resolves the installed platform package to a native Buf executable", async () => {
  const executable = await resolveNativeBufExecutable();
  assert.equal(path.basename(executable), process.platform === "win32" ? "buf.exe" : "buf");
  assert.match(executable, /@bufbuild[+/]buf-(?:darwin|linux|win32)-/);
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

test("Windows process-tree teardown forces descendants before the parent can exit", () => {
  const child = new FakeChild();
  child.pid = 42;
  const calls = [];
  const spawnProcessSync = (...args) => {
    calls.push(args);
    return { error: undefined, status: 0 };
  };
  const options = {
    env: { PATH: "C:\\Windows\\System32" },
    platform: "win32",
    spawnProcessSync,
  };

  killProcessTree(child, "SIGTERM", options);

  assert.deepEqual(child.kills, []);
  assert.deepEqual(calls[0][0], "taskkill.exe");
  assert.deepEqual(calls[0][1], ["/PID", "42", "/T", "/F"]);
  assert.deepEqual(calls[0][2], {
    env: { PATH: "C:\\Windows\\System32" },
    shell: false,
    stdio: "ignore",
    timeout: 5000,
    windowsHide: true,
  });
});

test("a failed Windows tree kill cannot report descendants as reaped", () => {
  const child = new FakeChild();
  child.pid = 42;

  const reaped = killProcessTree(child, "SIGKILL", {
    env: { PATH: "C:\\Windows\\System32" },
    platform: "win32",
    spawnProcessSync: () => ({ error: undefined, status: 1 }),
  });

  assert.equal(reaped, false);
  assert.deepEqual(child.kills, ["SIGKILL"]);
});

test("bounded teardown uses the injected Windows platform", async () => {
  const child = new FakeChild();
  child.pid = 42;
  const timers = [];
  const treeKills = [];
  const promise = runBoundedProcess(
    {
      args: ["status", "--porcelain=v1"],
      cwd: "C:\\safe\\root",
      env: { PATH: "C:\\Windows\\System32" },
      file: "git.exe",
      maxOutputBytes: 1024,
      reapTimeoutMs: 10,
      terminationGraceMs: 10,
      timeoutMs: 50,
    },
    {
      clearTimer: (timer) => {
        timer.cleared = true;
      },
      platform: "win32",
      setTimer: (callback, milliseconds) => {
        const timer = { callback, cleared: false, milliseconds };
        timers.push(timer);
        return timer;
      },
      spawnProcess: () => child,
      spawnProcessSync: (...args) => {
        treeKills.push(args);
        return { error: undefined, status: 0 };
      },
    },
  );

  timers.find((timer) => timer.milliseconds === 50).callback();
  assert.deepEqual(child.kills, []);
  assert.deepEqual(treeKills[0].slice(0, 2), ["taskkill.exe", ["/PID", "42", "/T", "/F"]]);

  child.emit("close", null, "SIGTERM");
  await assert.rejects(promise, /COMMAND_TIMED_OUT/);
});

test("a direct child close cannot cancel forced tree escalation", async () => {
  const child = new FakeChild();
  const timers = [];
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
      platform: "linux",
      setTimer: (callback, milliseconds) => {
        const timer = { callback, cleared: false, milliseconds };
        timers.push(timer);
        return timer;
      },
      spawnProcess: () => child,
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
  child.emit("close", null, "SIGTERM");
  await Promise.resolve();
  assert.equal(settled, false);
  assert.deepEqual(child.kills, ["SIGTERM"]);

  timers.find((timer) => timer.milliseconds === 10 && !timer.cleared).callback();
  const error = await observed;
  assert.equal(error.message, "COMMAND_TIMED_OUT");
  assert.deepEqual(child.kills, ["SIGTERM", "SIGKILL"]);
  assert.equal(
    timers.every((timer) => timer.cleared),
    true,
  );
  assert.equal(child.listenerCount("close"), 0);
  assert.equal(child.listenerCount("error"), 0);
  assert.equal(child.stdout.listenerCount("data"), 0);
  assert.equal(child.stderr.listenerCount("data"), 0);
});

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
      platform: "linux",
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
    detached: true,
    env: { PATH: "/safe/bin" },
    shell: false,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });
});

test("a timed-out command leaves no stubborn grandchild", async () => {
  const root = await validRoot();
  const pidFile = path.join(root, "grandchild.pid");
  const stubbornScript = path.join(root, "stubborn-grandchild.mjs");
  await writeFile(
    stubbornScript,
    [
      'import { writeFileSync } from "node:fs";',
      'process.on("SIGTERM", () => {});',
      "writeFileSync(process.argv[2], String(process.pid));",
      "setInterval(() => {}, 1000);",
    ].join("\n"),
  );
  const parentScript = [
    'const { spawn } = require("node:child_process");',
    "const child = spawn(process.execPath, [process.argv[2], process.argv[1]],",
    '  { stdio: "ignore" });',
    'require("node:fs").writeFileSync(process.argv[1], String(child.pid));',
    "setInterval(() => {}, 1000);",
  ].join("\n");
  let grandchildPid;

  try {
    await assert.rejects(
      runBoundedProcess({
        args: ["-e", parentScript, pidFile, stubbornScript],
        cwd: root,
        env: { PATH: process.env.PATH },
        file: process.execPath,
        maxOutputBytes: 1024,
        reapTimeoutMs: 1000,
        terminationGraceMs: 50,
        timeoutMs: 500,
      }),
      /COMMAND_TIMED_OUT/,
    );
    grandchildPid = Number.parseInt(await readFile(pidFile, "utf8"), 10);
    assert.ok(Number.isInteger(grandchildPid) && grandchildPid > 0);

    let alive = true;
    for (let attempt = 0; attempt < 100 && alive; attempt += 1) {
      try {
        process.kill(grandchildPid, 0);
        await delay(10);
      } catch (error) {
        if (error?.code !== "ESRCH") throw error;
        alive = false;
      }
    }
    assert.equal(alive, false, `grandchild ${grandchildPid} survived timeout teardown`);
  } finally {
    if (grandchildPid !== undefined) {
      try {
        process.kill(grandchildPid, "SIGKILL");
      } catch {
        // It is expected to have been reaped by the process-group teardown.
      }
    }
  }
});
