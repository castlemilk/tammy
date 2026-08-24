import { type ChildProcessWithoutNullStreams, execFile, spawn } from "node:child_process";
import { EventEmitter } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { Code, ConnectError } from "@connectrpc/connect";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

import { createCoreClient } from "./core-client";
import { authenticateCoreExecutable } from "./core-executable";
import { CoreProcess, type SpawnCoreProcess } from "./core-process";

const execFileAsync = promisify(execFile);
const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../..");
const DEFAULT_BUILD_TIMEOUT_MS = 120_000;
const MAXIMUM_BUILD_TIMEOUT_MS = 600_000;
const BUILD_HOOK_CLEANUP_HEADROOM_MS = 15_000;
const BUILD_TIMEOUT_OVERRIDE = "TAMMY_CORE_INTEGRATION_BUILD_TIMEOUT_MS";
const STOP_TIMEOUT_MS = 5_000;
const CLOSE_TIMEOUT_MS = 2_000;

interface IntegrationBuildTimeouts {
  readonly buildTimeoutMs: number;
  readonly hookTimeoutMs: number;
}

function resolveIntegrationBuildTimeoutMs(
  environment: Readonly<Record<string, string | undefined>>,
): IntegrationBuildTimeouts {
  const override = environment[BUILD_TIMEOUT_OVERRIDE];
  const buildTimeoutMs = override === undefined ? DEFAULT_BUILD_TIMEOUT_MS : Number(override);
  if (
    (override !== undefined && !/^[1-9][0-9]*$/u.test(override)) ||
    !Number.isSafeInteger(buildTimeoutMs) ||
    buildTimeoutMs < 1 ||
    buildTimeoutMs > MAXIMUM_BUILD_TIMEOUT_MS
  ) {
    throw new Error("Invalid core integration build timeout.");
  }
  return Object.freeze({
    buildTimeoutMs,
    hookTimeoutMs: buildTimeoutMs + BUILD_HOOK_CLEANUP_HEADROOM_MS,
  });
}

const { buildTimeoutMs: BUILD_TIMEOUT_MS, hookTimeoutMs: BUILD_HOOK_TIMEOUT_MS } =
  resolveIntegrationBuildTimeoutMs(process.env);

interface TimeoutTimers {
  setTimeout(callback: () => void, delay: number): unknown;
  clearTimeout(timer: unknown): void;
}

interface ScheduledTimeout {
  readonly callback: () => void;
  readonly delay: number;
  cancelled: boolean;
}

class FakeTimeoutTimers implements TimeoutTimers {
  readonly scheduled: ScheduledTimeout[] = [];

  setTimeout(callback: () => void, delay: number): ScheduledTimeout {
    const timer = { callback, delay, cancelled: false };
    this.scheduled.push(timer);
    return timer;
  }

  clearTimeout(timer: unknown): void {
    (timer as ScheduledTimeout).cancelled = true;
  }

  fire(delay: number): void {
    const timer = this.scheduled.find(
      (candidate) => candidate.delay === delay && !candidate.cancelled,
    );
    if (!timer) {
      throw new Error(`No active ${delay}ms timeout`);
    }
    timer.cancelled = true;
    timer.callback();
  }
}

interface ChildCloseObservation {
  readonly isClosed: () => boolean;
  readonly wait: Promise<void>;
}

interface CloseObservable {
  once(event: "close", listener: () => void): this;
}

interface CleanupChild {
  readonly pid?: number | undefined;
  kill(signal: NodeJS.Signals): boolean;
}

interface CleanupResources {
  readonly supervisor?: { stop(): Promise<void> } | undefined;
  readonly child?: CleanupChild | undefined;
  readonly childClose?: ChildCloseObservation | undefined;
  readonly temporaryDirectory?: string | undefined;
}

interface CleanupPrimitives {
  readonly isProcessAlive: (pid: number) => boolean;
  readonly removeDirectory: (directory: string) => Promise<void>;
  readonly timers: TimeoutTimers;
  readonly stopTimeoutMs: number;
  readonly closeTimeoutMs: number;
}

type BuildExecutor = (signal: AbortSignal) => Promise<void>;

interface IntegrationBuildExecOptions {
  readonly cwd: string;
  readonly env: NodeJS.ProcessEnv;
  readonly signal: AbortSignal;
  readonly windowsHide: boolean;
}

type IntegrationBuildExecFile = (
  command: string,
  args: readonly string[],
  options: IntegrationBuildExecOptions,
) => Promise<unknown>;

interface ExecuteIntegrationCoreBuildOptions {
  readonly binaryPath: string;
  readonly execFile?: IntegrationBuildExecFile | undefined;
  readonly signal: AbortSignal;
  readonly sourceEnvironment?: Readonly<NodeJS.ProcessEnv> | undefined;
  readonly temporaryDirectory: string;
}

const productionIntegrationBuildExecFile: IntegrationBuildExecFile = async (
  command,
  args,
  options,
) => {
  await execFileAsync(command, [...args], options);
};

async function executeIntegrationCoreBuild({
  binaryPath,
  execFile: execute = productionIntegrationBuildExecFile,
  signal,
  sourceEnvironment = process.env,
  temporaryDirectory,
}: ExecuteIntegrationCoreBuildOptions): Promise<void> {
  await execute("go", ["build", "-trimpath", "-o", binaryPath, "./services/core/cmd/tammy-core"], {
    cwd: repositoryRoot,
    env: {
      ...sourceEnvironment,
      GOCACHE: path.join(temporaryDirectory, "go-cache"),
    },
    signal,
    windowsHide: true,
  });
}

const productionTimeoutTimers: TimeoutTimers = {
  setTimeout: (callback, delay) => globalThis.setTimeout(callback, delay),
  clearTimeout: (timer) => globalThis.clearTimeout(timer as ReturnType<typeof setTimeout>),
};

async function runBoundedBuild(
  execute: BuildExecutor,
  timeoutMs: number,
  timers: TimeoutTimers,
): Promise<void> {
  const abortController = new AbortController();
  let timedOut = false;
  const timeout = timers.setTimeout(() => {
    timedOut = true;
    abortController.abort();
  }, timeoutMs);

  try {
    await execute(abortController.signal);
    if (timedOut) {
      throw new Error("Go core build timed out.");
    }
  } catch (error) {
    if (timedOut) {
      throw new Error("Go core build timed out.");
    }
    throw error;
  } finally {
    timers.clearTimeout(timeout);
  }
}

function observeChildClose(child: CloseObservable): ChildCloseObservation {
  let closed = false;
  const wait = new Promise<void>((resolve) => {
    child.once("close", () => {
      closed = true;
      resolve();
    });
  });
  return {
    isClosed: () => closed,
    wait,
  };
}

async function awaitWithTimeout(
  promise: Promise<void>,
  timeoutMs: number,
  message: string,
  timers: TimeoutTimers,
): Promise<void> {
  let timeout: unknown;
  const expiry = new Promise<never>((_resolve, reject) => {
    timeout = timers.setTimeout(() => reject(new Error(message)), timeoutMs);
  });
  try {
    await Promise.race([promise, expiry]);
  } finally {
    timers.clearTimeout(timeout);
  }
}

function combinedCleanupFailure(primary: unknown, secondary: unknown): unknown {
  if (primary === undefined) {
    return secondary;
  }
  return new AggregateError([primary, secondary], "Core integration cleanup failed.");
}

async function cleanupIntegrationResources(
  resources: CleanupResources,
  primitives: CleanupPrimitives,
): Promise<void> {
  let deferredFailure: unknown;
  if (resources.supervisor) {
    try {
      await awaitWithTimeout(
        resources.supervisor.stop(),
        primitives.stopTimeoutMs,
        "Core supervisor stop timed out.",
        primitives.timers,
      );
    } catch (error) {
      deferredFailure = error;
    }
  }

  const child = resources.child;
  const pid = child?.pid;
  let alive = pid !== undefined && primitives.isProcessAlive(pid);
  if (pid !== undefined && alive) {
    if (!child || !resources.childClose) {
      throw combinedCleanupFailure(
        deferredFailure,
        new Error("Core child termination could not be confirmed."),
      );
    }

    try {
      const signalSent = child.kill("SIGKILL");
      if (!signalSent && primitives.isProcessAlive(pid)) {
        throw new Error("Core child could not be force-killed.");
      }
    } catch (error) {
      throw combinedCleanupFailure(deferredFailure, error);
    }
  }

  if (resources.childClose && !resources.childClose.isClosed()) {
    try {
      await awaitWithTimeout(
        resources.childClose.wait,
        primitives.closeTimeoutMs,
        "Core child close confirmation timed out.",
        primitives.timers,
      );
    } catch (error) {
      throw combinedCleanupFailure(deferredFailure, error);
    }
  }

  alive = pid !== undefined && primitives.isProcessAlive(pid);
  if (alive) {
    throw combinedCleanupFailure(
      deferredFailure,
      new Error("Core child remained alive after cleanup."),
    );
  }

  if (resources.temporaryDirectory) {
    try {
      await primitives.removeDirectory(resources.temporaryDirectory);
    } catch (error) {
      throw combinedCleanupFailure(deferredFailure, error);
    }
  }

  if (deferredFailure !== undefined) {
    throw deferredFailure;
  }
}

function processIsAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    if (
      error instanceof Error &&
      "code" in error &&
      (error as NodeJS.ErrnoException).code === "ESRCH"
    ) {
      return false;
    }
    throw error;
  }
}

function deferred(): {
  readonly promise: Promise<void>;
  readonly resolve: () => void;
} {
  let resolve = (): void => undefined;
  const promise = new Promise<void>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

describe("integration build timeout", () => {
  it("defaults to the production core build bound with hook cleanup headroom", () => {
    expect(resolveIntegrationBuildTimeoutMs({})).toEqual({
      buildTimeoutMs: 120_000,
      hookTimeoutMs: 135_000,
    });
  });

  it("accepts a bounded explicit override", () => {
    expect(
      resolveIntegrationBuildTimeoutMs({
        TAMMY_CORE_INTEGRATION_BUILD_TIMEOUT_MS: "180000",
      }),
    ).toEqual({
      buildTimeoutMs: 180_000,
      hookTimeoutMs: 195_000,
    });
  });

  it.each(["0", "-1", "120000.5", " 120000", "600001", "unbounded"])(
    "rejects invalid override %j",
    (override) => {
      expect(() =>
        resolveIntegrationBuildTimeoutMs({
          TAMMY_CORE_INTEGRATION_BUILD_TIMEOUT_MS: override,
        }),
      ).toThrow("Invalid core integration build timeout.");
    },
  );
});

describe("integration build environment", () => {
  it("gives execFile a contained local Go cache without mutating inherited environment", async () => {
    const temporaryRoot = path.join(tmpdir(), "tammy-integration-environment");
    const binary = path.join(
      temporaryRoot,
      process.platform === "win32" ? "tammy-core.exe" : "tammy-core",
    );
    const sourceEnvironment = Object.freeze({
      GOCACHE: "/shared/go-cache",
      PATH: "/toolchain/bin",
      SystemRoot: "C:\\Windows",
      TAMMY_SECRET_SENTINEL: "must-not-be-printed",
    });
    const abortController = new AbortController();
    const calls: unknown[][] = [];

    await executeIntegrationCoreBuild({
      binaryPath: binary,
      execFile: async (...call: unknown[]) => {
        calls.push(call);
      },
      signal: abortController.signal,
      sourceEnvironment,
      temporaryDirectory: temporaryRoot,
    });

    expect(calls).toHaveLength(1);
    const [command, args, rawOptions] = calls[0] ?? [];
    const options = rawOptions as {
      readonly cwd: string;
      readonly env: Readonly<Record<string, string | undefined>>;
      readonly signal: AbortSignal;
      readonly windowsHide: boolean;
    };
    expect(command).toBe("go");
    expect(args).toEqual(["build", "-trimpath", "-o", binary, "./services/core/cmd/tammy-core"]);
    expect(options).toMatchObject({
      cwd: repositoryRoot,
      signal: abortController.signal,
      windowsHide: true,
    });
    expect(options.env).not.toBe(sourceEnvironment);
    expect(options.env).toMatchObject({
      ...sourceEnvironment,
      GOCACHE: path.join(temporaryRoot, "go-cache"),
    });
    expect(path.relative(temporaryRoot, options.env.GOCACHE ?? "")).toBe("go-cache");
    expect(sourceEnvironment.GOCACHE).toBe("/shared/go-cache");
  });
});

describe("bounded integration lifecycle helpers", () => {
  it("aborts a build executor that otherwise never settles", async () => {
    const timers = new FakeTimeoutTimers();
    let observedAbort = false;
    const execute: BuildExecutor = (signal) =>
      new Promise((_resolve, reject) => {
        signal.addEventListener(
          "abort",
          () => {
            observedAbort = true;
            reject(signal.reason);
          },
          { once: true },
        );
      });
    const build = runBoundedBuild(execute, 50, timers);

    timers.fire(50);

    await expect(build).rejects.toThrow("Go core build timed out.");
    expect(observedAbort).toBe(true);
  });

  it("observes close before cleanup begins without missing the event", async () => {
    const child = new EventEmitter();
    const observation = observeChildClose(child);

    child.emit("close");

    expect(observation.isClosed()).toBe(true);
    await expect(observation.wait).resolves.toBeUndefined();
  });

  it("force-kills after rejected stop, awaits close, removes, then reports the stop error", async () => {
    const stopError = new Error("supervisor stop failed");
    const close = deferred();
    const timers = new FakeTimeoutTimers();
    const kill = vi.fn(() => true);
    const removeDirectory = vi.fn(async () => undefined);
    let alive = true;
    let closed = false;
    const cleanup = cleanupIntegrationResources(
      {
        supervisor: { stop: () => Promise.reject(stopError) },
        child: { pid: 12_345, kill },
        childClose: {
          isClosed: () => closed,
          wait: close.promise,
        },
        temporaryDirectory: "/tmp/tammy-core-test",
      },
      {
        isProcessAlive: () => alive,
        removeDirectory,
        timers,
        stopTimeoutMs: 25,
        closeTimeoutMs: 50,
      },
    );

    await vi.waitFor(() => expect(kill).toHaveBeenCalledWith("SIGKILL"));
    expect(removeDirectory).not.toHaveBeenCalled();
    alive = false;
    closed = true;
    close.resolve();

    await expect(cleanup).rejects.toBe(stopError);
    expect(removeDirectory).toHaveBeenCalledWith("/tmp/tammy-core-test");
  });

  it("bounds force-kill close confirmation and never removes a possibly live executable", async () => {
    const timers = new FakeTimeoutTimers();
    const kill = vi.fn(() => true);
    const removeDirectory = vi.fn(async () => undefined);
    let alive = true;
    const cleanup = cleanupIntegrationResources(
      {
        supervisor: { stop: () => Promise.reject(new Error("stop failed")) },
        child: { pid: 12_345, kill },
        childClose: {
          isClosed: () => false,
          wait: new Promise(() => undefined),
        },
        temporaryDirectory: "C:\\Temp\\tammy-core-test",
      },
      {
        isProcessAlive: () => alive,
        removeDirectory,
        timers,
        stopTimeoutMs: 25,
        closeTimeoutMs: 50,
      },
    );

    await vi.waitFor(() => expect(kill).toHaveBeenCalledWith("SIGKILL"));
    alive = false;
    timers.fire(50);

    const error = await cleanup.catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(AggregateError);
    expect((error as AggregateError).errors).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          message: "Core child close confirmation timed out.",
        }),
      ]),
    );
    expect(removeDirectory).not.toHaveBeenCalled();
  });

  it("surfaces directory-removal failures", async () => {
    const removalError = new Error("remove failed");
    const removeDirectory = vi.fn(() => Promise.reject(removalError));

    await expect(
      cleanupIntegrationResources(
        { temporaryDirectory: "/tmp/tammy-core-test" },
        {
          isProcessAlive: () => false,
          removeDirectory,
          timers: new FakeTimeoutTimers(),
          stopTimeoutMs: 25,
          closeTimeoutMs: 50,
        },
      ),
    ).rejects.toBe(removalError);
  });
});

describe("CoreProcess and Connect-ES interoperability", () => {
  let temporaryDirectory: string;
  let binaryPath: string;
  let supervisor: CoreProcess | undefined;
  let child: ChildProcessWithoutNullStreams | undefined;
  let childClose: ChildCloseObservation | undefined;

  beforeAll(async () => {
    temporaryDirectory = await mkdtemp(path.join(tmpdir(), "tammy-core-test-"));
    binaryPath = path.join(
      temporaryDirectory,
      process.platform === "win32" ? "tammy-core.exe" : "tammy-core",
    );
    await runBoundedBuild(
      (signal) =>
        // execFile's callback settles after child termination, so an aborted build is reaped
        // before the rejection reaches beforeAll and cleanup can remove the executable.
        executeIntegrationCoreBuild({
          binaryPath,
          signal,
          temporaryDirectory,
        }),
      BUILD_TIMEOUT_MS,
      productionTimeoutTimers,
    );
  }, BUILD_HOOK_TIMEOUT_MS);

  afterAll(async () => {
    await cleanupIntegrationResources(
      {
        supervisor,
        child,
        childClose,
        temporaryDirectory,
      },
      {
        isProcessAlive: processIsAlive,
        removeDirectory: (directory) => rm(directory, { recursive: true, force: true }),
        timers: productionTimeoutTimers,
        stopTimeoutMs: STOP_TIMEOUT_MS,
        closeTimeoutMs: CLOSE_TIMEOUT_MS,
      },
    );
  }, 15_000);

  it("calls the real offline core, rejects a changed capability, and leaves no child", async () => {
    const spawnCore: SpawnCoreProcess = (absoluteBinaryPath, args, options) => {
      child = spawn(absoluteBinaryPath, args, {
        env: options.env,
        shell: options.shell,
        stdio: ["pipe", "pipe", "pipe"],
        windowsHide: options.windowsHide,
      });
      childClose = observeChildClose(child);
      return child;
    };
    supervisor = new CoreProcess({
      binary: await authenticateCoreExecutable(binaryPath),
      spawn: spawnCore,
      readinessTimeoutMs: 10_000,
    });
    const readiness = await supervisor.start();
    const pid = child?.pid;
    expect(pid).toBeTypeOf("number");
    if (pid === undefined) {
      throw new Error("Core child did not expose a process identifier.");
    }

    await expect(createCoreClient(readiness).getDiagnostics()).resolves.toEqual({
      apiVersion: "tammy.v1",
      coreVersion: "dev",
      runtimeMode: "offline",
      networkRequired: false,
    });

    const changedCapability = Buffer.alloc(32, 0x52).toString("base64url");
    expect(changedCapability).not.toBe(readiness.capability);
    const authenticationError = await createCoreClient({
      ...readiness,
      capability: changedCapability,
    })
      .getDiagnostics()
      .catch((caught: unknown) => caught);
    expect(authenticationError).toBeInstanceOf(ConnectError);
    expect(authenticationError).toMatchObject({
      code: Code.Unauthenticated,
      rawMessage: "Core request failed.",
    });
    expect(`${String(authenticationError)} ${JSON.stringify(authenticationError)}`).not.toContain(
      changedCapability,
    );

    await supervisor.stop();

    expect(child?.exitCode).toBe(0);
    expect(child?.signalCode).toBeNull();
    expect(processIsAlive(pid)).toBe(false);
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPED" });
  }, 30_000);
});
