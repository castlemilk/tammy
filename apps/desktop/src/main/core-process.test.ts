import { spawn as nodeSpawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { EventEmitter } from "node:events";
import { chmod, mkdtemp, rename, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { PassThrough } from "node:stream";

import { describe, expect, it, vi } from "vitest";
import { authenticateCoreExecutable } from "./core-executable";
import {
  CoreProcess,
  CoreProcessError,
  type CoreProcessTimers,
  type SpawnCoreProcess,
} from "./core-process";

const CERTIFICATE = `-----BEGIN CERTIFICATE-----
MIIDCTCCAfGgAwIBAgIUfyv/Dzl4yZ1jFPe6zuXvGRYDFlowDQYJKoZIhvcNAQEL
BQAwFDESMBAGA1UEAwwJbG9jYWxob3N0MB4XDTI2MDcxOTE0MzcxOFoXDTI2MDcy
MDE0MzcxOFowFDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEApx6O1eD2I7HXr/Qp6yO/90Zwk1OCGL3WaclJte+BZg+a
Ae4BLndHGc575pd96ntzWdk8rGpWjXQ/r/fYvLB9QAHW3xYP78QQcewGEXOaLIe7
sGDWATxxVaxO/3KRfjjuc6iAMt5erSENEafG8yNLuIweZuTa46VGQgWoI9C3PmG3
AB56sYA2ZPL2gW/QUcp6pcIi6TYFMqVffNprTaS8qhKUwiHeVrUR0gJYeaMv9dbZ
tTw7k3WUwK9+Xmyh6D1vN1YIpoaqcLge0/4tTXmoWcGPIRW+h6XwW84Qc0vELkTt
DEp4OVS46Wd24JnLR9/m/qEfMX08JpSknCHYKr582QIDAQABo1MwUTAdBgNVHQ4E
FgQUtQpIJKU696beoddFIu73TlTBC0UwHwYDVR0jBBgwFoAUtQpIJKU696beoddF
Iu73TlTBC0UwDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEABkh+
c1JTbRZzx+9vJZkLG3IqjE1na8+zgEcLt9AdVwPxfarpJAaiRruscZ3Sbyt8Yd57
cPE73Zf0fmDBg7gDzajkcfgLjwXNAeuZJs05Fdwl2WDSZIGwxIXCyjJ0w10Sz5jA
8IdN415Nvc0+WVNuEmS6VpeosQjq1JQlpq4h5BH37WgHeGdbip3m0hrP/+UVKW0s
+ZDK5DTeBRhMJ56u7r4JYy6abqAOlLQ6lry08pthjz20MhtC2oQU49Y5WL6j394S
LDDUIdLMdw4J/5IrjCErqL7ASHWXjWZwsS6JRHdCCI5yaFgDQidzEDdbnM7KvH3P
w33IoyLPX5HmoGJSBw==
-----END CERTIFICATE-----`;

const CAPABILITY = Buffer.alloc(32, 0xa5).toString("base64url");
const PORT = 54_321;
const TEST_BINARY_IDENTITY = Object.freeze({
  ctimeNs: 1n,
  dev: 2n,
  ino: 3n,
  mode: 0o100700n,
  mtimeNs: 4n,
  nlink: 1n,
  size: 5n,
});

function testBinary(executablePath = "/opt/tammy/bin/tammy-core") {
  return Object.freeze({
    executablePath,
    identity: TEST_BINARY_IDENTITY,
    sha256: "a".repeat(64),
  });
}

function readinessLine(): Buffer {
  return Buffer.from(
    `${JSON.stringify({
      protocol: "tammy-core-ready-v1",
      port: PORT,
      ca_pem: CERTIFICATE,
      capability: CAPABILITY,
    })}\n`,
  );
}

class FakeChild extends EventEmitter {
  pid: number | undefined = 12_345;
  readonly stdout = new PassThrough();
  readonly stderr = new PassThrough();
  readonly stdin = new PassThrough();
  readonly killSignals: NodeJS.Signals[] = [];
  killBehavior: (signal: NodeJS.Signals) => boolean = () => true;

  get killCalls(): number {
    return this.killSignals.length;
  }

  kill(signal: NodeJS.Signals): boolean {
    this.killSignals.push(signal);
    return this.killBehavior(signal);
  }
}

interface ScheduledTimer {
  readonly callback: () => void;
  readonly delay: number;
  cancelled: boolean;
}

class FakeTimers implements CoreProcessTimers {
  readonly scheduled: ScheduledTimer[] = [];

  setTimeout(callback: () => void, delay: number): ScheduledTimer {
    const timer = { callback, delay, cancelled: false };
    this.scheduled.push(timer);
    return timer;
  }

  clearTimeout(timer: unknown): void {
    (timer as ScheduledTimer).cancelled = true;
  }

  runNext(delay: number): void {
    const timer = this.scheduled.find(
      (candidate) => !candidate.cancelled && candidate.delay === delay,
    );
    if (!timer) {
      throw new Error(`No active ${delay}ms timer`);
    }
    timer.cancelled = true;
    timer.callback();
  }
}

function testRig(
  overrides: {
    readonly args?: readonly string[];
    readonly spawn?: SpawnCoreProcess;
    readonly readinessTimeoutMs?: number;
  } = {},
) {
  const child = new FakeChild();
  const spawnCalls: unknown[][] = [];
  const spawn: SpawnCoreProcess =
    overrides.spawn ??
    ((...args) => {
      spawnCalls.push(args);
      return child;
    });
  const timers = new FakeTimers();
  const warnings: string[] = [];
  const now = vi.fn(() => 42);
  const supervisor = new CoreProcess({
    binary: testBinary(),
    ...(overrides.args === undefined ? {} : { args: overrides.args }),
    spawn,
    clock: { now },
    timers,
    logger: { warn: (message) => warnings.push(message) },
    sourceEnvironment: {
      APPDATA: "C:\\Users\\test\\AppData\\Roaming",
      HOME: "/Users/test",
      PATH: "/secret/bin",
      NODE_OPTIONS: "--require=/secret/inject.js",
      DATABASE_PASSWORD: "do-not-copy",
      SYSTEMROOT: "C:\\Windows",
      WINDIR: "C:\\Windows",
      TEMP: "C:\\Temp",
      TMP: "C:\\Tmp",
      TMPDIR: "/tmp",
      LANG: "en_AU.UTF-8",
      USERPROFILE: "C:\\Users\\test",
    },
    readinessTimeoutMs: overrides.readinessTimeoutMs ?? 10_000,
    verifyBinary: () => undefined,
  });
  return {
    child,
    now,
    spawnCalls,
    supervisor,
    timers,
    warnings,
  };
}

async function expectCoreError(
  promise: Promise<unknown>,
  code: CoreProcessError["code"],
  message: string,
): Promise<void> {
  try {
    await promise;
    throw new Error("CoreProcess operation unexpectedly succeeded");
  } catch (error) {
    expect(error).toBeInstanceOf(CoreProcessError);
    expect(error).toMatchObject({ code, message });
    expect(Object.keys(error as object)).toEqual(["code", "name"]);
  }
}

describe("CoreProcess construction and spawning", () => {
  it.each(["tammy-core", "./tammy-core", "../bin/tammy-core", ""])(
    "requires an absolute binary path: %j",
    (binaryPath) => {
      expect(
        () =>
          new CoreProcess({
            binary: testBinary(binaryPath),
            verifyBinary: () => undefined,
          }),
      ).toThrowError(
        expect.objectContaining({
          code: "INVALID_BINARY_PATH",
          message: "Core binary path must be absolute.",
        }),
      );
    },
  );

  it("spawns the exact absolute path without a shell or inherited secrets", () => {
    const { now, spawnCalls, supervisor } = testRig();

    void supervisor.start();

    expect(spawnCalls).toEqual([
      [
        "/opt/tammy/bin/tammy-core",
        [],
        {
          shell: false,
          windowsHide: true,
          stdio: ["pipe", "pipe", "pipe"],
          env: {
            APPDATA: "C:\\Users\\test\\AppData\\Roaming",
            HOME: "/Users/test",
            SYSTEMROOT: "C:\\Windows",
            WINDIR: "C:\\Windows",
            TEMP: "C:\\Temp",
            TMP: "C:\\Tmp",
            TMPDIR: "/tmp",
            LANG: "en_AU.UTF-8",
            USERPROFILE: "C:\\Users\\test",
          },
        },
      ],
    ]);
    expect(now).not.toHaveBeenCalled();
  });

  it("rejects same-digest path substitution between resolution and spawn", async () => {
    const root = await mkdtemp(path.join(tmpdir(), "tammy-core-identity-"));
    try {
      const binaryPath = path.join(root, "tammy-core");
      const replacementPath = path.join(root, "replacement");
      const bytes = Buffer.from("authenticated-core");
      await writeFile(binaryPath, bytes, { mode: 0o700 });
      const binary = await authenticateCoreExecutable(binaryPath);
      const child = new FakeChild();
      const spawn = vi.fn<SpawnCoreProcess>(() => child);
      const supervisor = new CoreProcess({
        binary,
        spawn,
      });
      await writeFile(replacementPath, bytes, { mode: 0o700 });
      await chmod(replacementPath, 0o700);
      await rename(replacementPath, binaryPath);

      const start = supervisor.start();
      child.stdout.write(readinessLine());

      await expectCoreError(start, "SPAWN_FAILED", "Core process could not be started.");
      expect(spawn).not.toHaveBeenCalled();
    } finally {
      await rm(root, { force: true, recursive: true });
    }
  });

  it("passes only the explicitly owned local-data arguments", () => {
    const { spawnCalls, supervisor } = testRig({
      args: ["--data-root", "/Users/test/Library/Application Support/Tammy/local-core"],
    });

    void supervisor.start();

    expect(spawnCalls[0]?.[1]).toEqual([
      "--data-root",
      "/Users/test/Library/Application Support/Tammy/local-core",
    ]);
  });

  it("converts a synchronous native spawn throw to a stable redacted failure", async () => {
    const nativeText = "/secret/path EACCES DATABASE_PASSWORD=hunter2";
    const { supervisor } = testRig({
      spawn: () => {
        throw new Error(nativeText);
      },
    });

    const start = supervisor.start();

    await expectCoreError(start, "SPAWN_FAILED", "Core process could not be started.");
    expect(JSON.stringify(supervisor.getDiagnostic())).not.toContain(nativeText);
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "SPAWN_FAILED",
    });
  });

  it("cleans up an asynchronous spawn failure that never acquired a PID", async () => {
    const { child, supervisor, timers } = testRig();
    child.pid = undefined;
    const start = supervisor.start();

    child.emit("error", new Error("ENOENT /secret/missing-core"));

    await expectCoreError(start, "SPAWN_FAILED", "Core process could not be started.");
    expect(child.killSignals).toEqual([]);
    expect(timers.scheduled.filter((timer) => timer.delay === 3_000 && !timer.cancelled)).toEqual(
      [],
    );
    expect(child.listenerCount("error")).toBe(0);
    expect(child.listenerCount("exit")).toBe(0);
    await expect(supervisor.stop()).resolves.toBeUndefined();
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "SPAWN_FAILED",
    });
  });
});

describe("CoreProcess real subprocess lifecycle", () => {
  it("cleans up a production-spawn ENOENT without waiting for termination timers", async () => {
    const missingPath = path.join(tmpdir(), `tammy-core-guaranteed-missing-${randomUUID()}`);
    const supervisor = new CoreProcess({
      binary: testBinary(missingPath),
      verifyBinary: () => undefined,
    });
    const startedAt = Date.now();

    const error = await supervisor.start().catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(CoreProcessError);
    expect(error).toMatchObject({
      code: "SPAWN_FAILED",
      message: "Core process could not be started.",
    });
    expect(`${String(error)} ${JSON.stringify(error)}`).not.toMatch(
      /ENOENT|tammy-core-guaranteed-missing/i,
    );
    await expect(supervisor.stop()).resolves.toBeUndefined();
    expect(Date.now() - startedAt).toBeLessThan(1_500);
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "SPAWN_FAILED",
    });
    await expect(supervisor.stop()).resolves.toBeUndefined();
  }, 7_000);

  it("waits for real exit after the graceful window and SIGKILL", async () => {
    const signals: NodeJS.Signals[] = [];
    let exitObservedAt: number | undefined;
    const script = [
      `process.stdout.write(Buffer.from("${readinessLine().toString("base64")}","base64"));`,
      'process.on("SIGTERM",()=>{});',
      "process.stdin.resume();",
      "setInterval(()=>{},1000);",
    ].join("");
    const spawn: SpawnCoreProcess = (_binaryPath, args, options) => {
      expect(args).toEqual([]);
      const child = nodeSpawn(process.execPath, ["-e", script], {
        shell: options.shell,
        windowsHide: options.windowsHide,
        stdio: ["pipe", "pipe", "pipe"],
        env: options.env,
      });
      const nativeKill = child.kill.bind(child);
      child.kill = ((signal: NodeJS.Signals) => {
        signals.push(signal);
        return nativeKill(signal);
      }) as typeof child.kill;
      child.once("exit", () => {
        exitObservedAt = Date.now();
      });
      return child;
    };
    const supervisor = new CoreProcess({
      binary: testBinary(process.execPath),
      spawn,
      verifyBinary: () => undefined,
    });
    await supervisor.start();
    const stopStartedAt = Date.now();

    await supervisor.stop();
    const stopResolvedAt = Date.now();

    expect(signals).toEqual(["SIGKILL"]);
    expect(stopResolvedAt - stopStartedAt).toBeGreaterThanOrEqual(2_800);
    expect(stopResolvedAt - stopStartedAt).toBeLessThan(5_000);
    expect(exitObservedAt).toBeDefined();
    expect(stopResolvedAt).toBeGreaterThanOrEqual(exitObservedAt ?? Infinity);
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPED" });
  }, 7_000);
});

describe("CoreProcess readiness", () => {
  it("incrementally resolves readiness exactly once without logging it", async () => {
    const { child, supervisor, warnings } = testRig();
    const line = readinessLine();
    const firstStart = supervisor.start();
    const concurrentStart = supervisor.start();

    expect(concurrentStart).toBe(firstStart);
    child.stdout.write(line.subarray(0, 17));
    child.stdout.write(line.subarray(17, 777));
    child.stdout.write(line.subarray(777));

    const readiness = await firstStart;
    expect(readiness).toEqual({
      protocol: "tammy-core-ready-v1",
      port: PORT,
      caPem: CERTIFICATE,
      capability: CAPABILITY,
    });
    expect(await supervisor.start()).toBe(readiness);
    expect(supervisor.getDiagnostic()).toEqual({ state: "READY" });
    expect(warnings).toEqual([]);
  });

  it("does not leak the readiness timer when listener attachment receives data", async () => {
    const { child, supervisor, timers } = testRig();
    const nativeOn = child.stdout.on.bind(child.stdout);
    child.stdout.on = ((event: string, listener: (...args: unknown[]) => void) => {
      const result = nativeOn(event, listener);
      if (event === "data") {
        listener(readinessLine());
      }
      return result;
    }) as typeof child.stdout.on;

    await expect(supervisor.start()).resolves.toMatchObject({ port: PORT });

    expect(timers.scheduled.filter((timer) => timer.delay === 10_000 && !timer.cancelled)).toEqual(
      [],
    );
  });

  it("times out with an injected deterministic timer", async () => {
    const { supervisor, timers } = testRig({ readinessTimeoutMs: 1_234 });
    const start = supervisor.start();

    timers.runNext(1_234);

    await expectCoreError(start, "READINESS_TIMEOUT", "Core process readiness timed out.");
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "READINESS_TIMEOUT",
    });
  });

  it.each([
    ["malformed JSON", Buffer.from("{nope}\n"), "READINESS_INVALID"],
    [
      "extra bytes in the readiness chunk",
      Buffer.concat([readinessLine(), Buffer.from("x")]),
      "READINESS_INVALID",
    ],
    ["overflow before a newline", Buffer.alloc(65_537, 0x61), "READINESS_OVERFLOW"],
  ] as const)("rejects %s with a stable failure", async (_name, chunk, code) => {
    const { child, supervisor } = testRig();
    const start = supervisor.start();

    child.stdout.write(chunk);

    await expectCoreError(
      start,
      code,
      code === "READINESS_OVERFLOW"
        ? "Core process readiness exceeded its limit."
        : "Core process sent invalid readiness.",
    );
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: code,
    });
  });

  it("fails if any stdout byte arrives after readiness", async () => {
    const { child, supervisor } = testRig();
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;

    child.stdout.write(Buffer.from("later"));

    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "UNEXPECTED_STDOUT",
    });
  });

  it.each([
    ["an asynchronous spawn error", "error"],
    ["an early exit", "exit"],
  ] as const)("rejects %s without exposing native details", async (_name, event) => {
    const { child, supervisor } = testRig();
    const start = supervisor.start();

    if (event === "error") {
      child.emit("error", new Error("EACCES /secret/path token=very-secret"));
    } else {
      child.emit("exit", 137, "SIGKILL");
    }

    await expectCoreError(
      start,
      event === "error" ? "SPAWN_FAILED" : "EXIT_BEFORE_READY",
      event === "error"
        ? "Core process could not be started."
        : "Core process exited before readiness.",
    );
    const diagnostic = JSON.stringify(supervisor.getDiagnostic());
    expect(diagnostic).not.toMatch(/137|SIGKILL|secret|EACCES/i);
  });

  it("uses stable secret-free public error strings for malformed records", async () => {
    const { child, supervisor } = testRig();
    const start = supervisor.start();
    child.stdout.write(
      `${JSON.stringify({
        protocol: "tammy-core-ready-v1",
        port: PORT,
        ca_pem: CERTIFICATE,
        capability: `bad-${CAPABILITY}`,
      })}\n`,
    );

    await expectCoreError(start, "READINESS_INVALID", "Core process sent invalid readiness.");
    try {
      await start;
    } catch (error) {
      const text = `${String(error)} ${JSON.stringify(error)}`;
      expect(text).not.toContain(CAPABILITY);
      expect(text).not.toContain(String(PORT));
      expect(text).not.toContain("BEGIN CERTIFICATE");
    }
  });
});

describe("CoreProcess stderr handling", () => {
  it("suppresses all startup stderr and logs only bounded, timestamped, redacted READY lines", async () => {
    const { child, now, supervisor, warnings } = testRig();
    const start = supervisor.start();

    child.stderr.write(`${CAPABILITY} ${PORT} ${CERTIFICATE.split("\n")[1]}`);
    child.stderr.write("x".repeat(100_000));
    expect(warnings).toEqual([]);

    child.stdout.write(readinessLine());
    await start;
    child.stderr.write("\n");
    child.stderr.write(
      `${CAPABILITY} ${PORT} ${CERTIFICATE.split("\n")[1]} password=bad secret:also-bad\n`,
    );

    expect(warnings.length).toBeGreaterThanOrEqual(2);
    for (const warning of warnings) {
      expect(warning.length).toBeLessThanOrEqual(4_096);
      expect(warning).toMatch(/^\[42\] /);
    }
    const logged = warnings.join("\n");
    expect(logged).not.toContain(CAPABILITY);
    expect(logged).not.toContain(String(PORT));
    expect(logged).not.toContain(CERTIFICATE.split("\n")[1]);
    expect(logged).not.toMatch(/password=bad|also-bad/);
    expect(logged).toContain("[REDACTED]");
    expect(now).toHaveBeenCalledTimes(warnings.length);
  });

  it("suppresses stderr while shutdown is in progress without retaining readiness", async () => {
    const { child, supervisor, warnings } = testRig();
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;

    const warningCount = warnings.length;
    const stop = supervisor.stop();
    child.stderr.write(`${CAPABILITY} ${PORT} ${CERTIFICATE.split("\n")[1]}\n`);

    expect(warnings).toHaveLength(warningCount);
    child.emit("exit", 0, null);
    await stop;
  });
});

describe("CoreProcess stopping and diagnostics", () => {
  it("closes stdin and exits without killing when the child exits within 3 seconds", async () => {
    const { child, supervisor, timers } = testRig();
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;

    const firstStop = supervisor.stop();
    const concurrentStop = supervisor.stop();

    expect(concurrentStop).toBe(firstStop);
    expect(child.stdin.writableEnded).toBe(true);
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPING" });
    expect(timers.scheduled.some((timer) => timer.delay === 3_000 && !timer.cancelled)).toBe(true);
    child.emit("exit", 0, null);
    await firstStop;
    expect(child.killCalls).toBe(0);
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPED" });
    await expect(supervisor.stop()).resolves.toBeUndefined();
  });

  it("does not leak a stop timer when closing stdin synchronously observes exit", async () => {
    const { child, supervisor, timers } = testRig();
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;
    const nativeEnd = child.stdin.end.bind(child.stdin);
    child.stdin.end = ((...args: Parameters<typeof child.stdin.end>) => {
      const result = nativeEnd(...args);
      child.emit("exit", 0, null);
      return result;
    }) as typeof child.stdin.end;

    await supervisor.stop();

    expect(child.killCalls).toBe(0);
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPED" });
    expect(timers.scheduled.filter((timer) => timer.delay === 3_000 && !timer.cancelled)).toEqual(
      [],
    );
  });

  it("kills only after exactly 3 seconds if exit has not been observed", async () => {
    const { child, supervisor, timers } = testRig();
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;

    const stop = supervisor.stop();
    expect(child.killCalls).toBe(0);
    timers.runNext(3_000);

    expect(child.killSignals).toEqual(["SIGKILL"]);
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPING" });
    child.emit("exit", 0, null);
    await stop;
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPED" });
  });

  it("rejects after force termination is not exit-confirmed and permits a retry", async () => {
    const { child, supervisor, timers } = testRig();
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;

    const stop = supervisor.stop();
    let settled = false;
    void stop.then(
      () => {
        settled = true;
      },
      () => {
        settled = true;
      },
    );
    timers.runNext(3_000);
    await Promise.resolve();
    expect(child.killSignals).toEqual(["SIGKILL"]);
    expect(settled).toBe(false);
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPING" });

    timers.runNext(1_000);
    await expectCoreError(
      stop,
      "TERMINATION_FAILED",
      "Core process termination was not confirmed.",
    );
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "TERMINATION_FAILED",
    });
    expect(child.listenerCount("exit")).toBe(1);
    expect(child.listenerCount("error")).toBe(1);

    child.killBehavior = () => {
      child.emit("exit", 0, null);
      return true;
    };
    await expect(supervisor.stop()).resolves.toBeUndefined();
    expect(child.killSignals).toEqual(["SIGKILL", "SIGKILL"]);
    expect(child.listenerCount("exit")).toBe(0);
    expect(child.listenerCount("error")).toBe(0);
  });

  it.each([
    [
      "kill returns false",
      (child: FakeChild) => {
        child.killBehavior = () => false;
      },
    ],
    [
      "kill throws",
      (child: FakeChild) => {
        child.killBehavior = () => {
          throw new Error("/secret/native kill failure");
        };
      },
    ],
  ])("retains a live child when %s", async (_name, configure) => {
    const { child, supervisor, timers } = testRig();
    configure(child);
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;

    const stop = supervisor.stop();
    timers.runNext(3_000);

    await expectCoreError(
      stop,
      "TERMINATION_FAILED",
      "Core process termination was not confirmed.",
    );
    expect(child.killSignals).toEqual(["SIGKILL"]);
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "TERMINATION_FAILED",
    });
    expect(child.listenerCount("exit")).toBe(1);
    expect(child.listenerCount("error")).toBe(1);
    child.emit("exit", 0, null);
    expect(child.listenerCount("exit")).toBe(0);
    expect(child.listenerCount("error")).toBe(0);
  });

  it("handles synchronous exit during SIGKILL without leaking a confirmation timer", async () => {
    const { child, supervisor, timers } = testRig();
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;
    child.killBehavior = () => {
      child.emit("exit", 0, null);
      return true;
    };

    const stop = supervisor.stop();
    timers.runNext(3_000);
    await stop;

    expect(child.killSignals).toEqual(["SIGKILL"]);
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPED" });
    expect(timers.scheduled.filter((timer) => timer.delay === 1_000 && !timer.cancelled)).toEqual(
      [],
    );
    expect(child.listenerCount("exit")).toBe(0);
  });

  it("fatally rejects shutdown if stdout arrives after readiness", async () => {
    const { child, supervisor, timers } = testRig();
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;

    const stop = supervisor.stop();
    const concurrentStop = supervisor.stop();
    child.stdout.write(Buffer.from(CAPABILITY));

    expect(concurrentStop).toBe(stop);
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "UNEXPECTED_STDOUT",
    });
    await expectCoreError(stop, "UNEXPECTED_STDOUT", "Core process wrote unexpected output.");
    const publicText = JSON.stringify(supervisor.getDiagnostic());
    expect(publicText).not.toContain(CAPABILITY);
    expect(publicText).not.toContain(String(PORT));
    expect(publicText).not.toContain("BEGIN CERTIFICATE");
    expect(child.killSignals).toEqual([]);
    expect(child.stdin.writableEnded).toBe(true);
    expect(child.listenerCount("error")).toBe(1);
    expect(child.listenerCount("exit")).toBe(1);
    expect(child.stdout.listenerCount("data")).toBe(0);
    expect(timers.scheduled.filter((timer) => timer.delay === 3_000 && !timer.cancelled)).toEqual([
      expect.objectContaining({ delay: 3_000, cancelled: false }),
    ]);
    timers.runNext(3_000);
    expect(child.killSignals).toEqual(["SIGKILL"]);
    child.emit("exit", 0, null);
    expect(child.listenerCount("error")).toBe(0);
    expect(child.listenerCount("exit")).toBe(0);
  });

  it("preserves the stdout failure during force-confirmation ordering", async () => {
    const { child, supervisor, timers } = testRig();
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;

    const stop = supervisor.stop();
    timers.runNext(3_000);
    child.stdout.write(Buffer.from("x"));

    await expectCoreError(stop, "UNEXPECTED_STDOUT", "Core process wrote unexpected output.");
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "UNEXPECTED_STDOUT",
    });
    expect(timers.scheduled.filter((timer) => timer.delay === 3_000 && !timer.cancelled)).toEqual(
      [],
    );
    timers.runNext(1_000);
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "UNEXPECTED_STDOUT",
    });
    child.emit("exit", 0, null);
  });

  it("deterministically aborts a concurrent start and deduplicates stop", async () => {
    const { child, supervisor, timers } = testRig();
    const start = supervisor.start();
    const firstStop = supervisor.stop();
    const concurrentStop = supervisor.stop();

    expect(concurrentStop).toBe(firstStop);
    await expectCoreError(start, "START_ABORTED", "Core process start was stopped.");
    child.stdout.write(readinessLine());
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPING" });
    timers.runNext(3_000);
    expect(child.killSignals).toEqual(["SIGKILL"]);
    child.emit("exit", 0, null);
    await firstStop;
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPED" });
  });

  it("clears readiness on stop and failure and exposes only safe diagnostics", async () => {
    const { child, supervisor } = testRig();
    const start = supervisor.start();
    child.stdout.write(readinessLine());
    await start;

    const readyProjection = JSON.stringify(supervisor.getDiagnostic());
    expect(readyProjection).toBe('{"state":"READY"}');
    expect(readyProjection).not.toMatch(
      new RegExp(`${CAPABILITY}|${PORT}|BEGIN CERTIFICATE|authorization`, "i"),
    );

    const stop = supervisor.stop();
    child.emit("exit", 0, null);
    await stop;

    await expectCoreError(
      supervisor.start(),
      "INVALID_STATE",
      "Core process cannot be started in its current state.",
    );
    const stoppedProjection = JSON.stringify(supervisor.getDiagnostic());
    expect(stoppedProjection).toBe('{"state":"STOPPED"}');
    expect(stoppedProjection).not.toMatch(
      new RegExp(`${CAPABILITY}|${PORT}|BEGIN CERTIFICATE|authorization`, "i"),
    );
  });

  it("retains safe lifecycle listeners after failure and retries cleanup", async () => {
    const { child, supervisor, timers } = testRig();
    const start = supervisor.start();
    child.emit("error", new Error("native"));
    await expectCoreError(start, "SPAWN_FAILED", "Core process could not be started.");

    expect(child.killSignals).toEqual([]);
    expect(child.listenerCount("error")).toBe(1);
    expect(child.listenerCount("exit")).toBe(1);
    expect(child.stdout.listenerCount("data")).toBe(0);
    expect(child.stderr.listenerCount("data")).toBe(0);
    timers.runNext(3_000);
    expect(child.killSignals).toEqual(["SIGKILL"]);
    timers.runNext(1_000);
    expect(supervisor.getDiagnostic()).toEqual({
      state: "FAILED",
      errorCode: "SPAWN_FAILED",
    });
    child.killBehavior = () => {
      child.emit("exit", 0, null);
      return true;
    };
    await supervisor.stop();
    expect(child.killSignals).toEqual(["SIGKILL", "SIGKILL"]);
    expect(child.listenerCount("error")).toBe(0);
    expect(child.listenerCount("exit")).toBe(0);
  });
});
