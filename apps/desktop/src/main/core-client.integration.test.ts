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
import { CoreProcess, type SpawnCoreProcess } from "./core-process";

const execFileAsync = promisify(execFile);
const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../..");
const BUILD_TIMEOUT_MS = 45_000;
const STOP_TIMEOUT_MS = 5_000;
const CLOSE_TIMEOUT_MS = 2_000;

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
      async (signal) => {
        // execFile's callback settles after child termination, so an aborted build is reaped
        // before the rejection reaches beforeAll and cleanup can remove the executable.
        await execFileAsync(
          "go",
          ["build", "-trimpath", "-o", binaryPath, "./services/core/cmd/tammy-core"],
          {
            cwd: repositoryRoot,
            windowsHide: true,
            signal,
          },
        );
      },
      BUILD_TIMEOUT_MS,
      productionTimeoutTimers,
    );
  }, 60_000);

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
      binaryPath,
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
