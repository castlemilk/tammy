import { EventEmitter } from "node:events";

import { describe, expect, it, vi } from "vitest";

import {
  type HarnessChildProcess,
  HarnessProcessError,
  type HarnessProcessTimers,
  MAX_HARNESS_OUTPUT_BYTES,
  runHarnessProcess,
  selectHarnessLaunch,
  terminateHarnessProcess,
} from "../../tests/helpers/electron-harness-process";

interface ScheduledTimeout {
  readonly callback: () => void;
  readonly delay: number;
  cancelled: boolean;
}

class FakeTimers implements HarnessProcessTimers {
  readonly scheduled: ScheduledTimeout[] = [];

  setTimeout(callback: () => void, delay: number): ScheduledTimeout {
    const timeout = { callback, delay, cancelled: false };
    this.scheduled.push(timeout);
    return timeout;
  }

  clearTimeout(timer: unknown): void {
    (timer as ScheduledTimeout).cancelled = true;
  }

  fire(delay: number): void {
    const timeout = this.scheduled.find(
      (candidate) => candidate.delay === delay && !candidate.cancelled,
    );
    if (!timeout) {
      throw new Error(`No active ${delay}ms timeout.`);
    }
    timeout.cancelled = true;
    timeout.callback();
  }

  active(delay: number): number {
    return this.scheduled.filter((timeout) => timeout.delay === delay && !timeout.cancelled).length;
  }
}

class FakeReadable extends EventEmitter {
  write(chunk: Buffer | string): void {
    this.emit("data", chunk);
  }
}

class FakeChild extends EventEmitter implements HarnessChildProcess {
  readonly stderr = new FakeReadable();
  readonly stdout = new FakeReadable();
  readonly kill = vi.fn(() => true);
  readonly pid = 4321;
}

function harnessOptions(child: FakeChild, timers = new FakeTimers()) {
  return {
    options: {
      closeConfirmationTimeoutMs: 50,
      electronArguments: ["--enable-logging=stderr", "/fixtures"],
      environment: { DISPLAY: ":99" } as NodeJS.ProcessEnv,
      executable: "/electron",
      platform: "linux" as const,
      spawnProcess: vi.fn(() => child),
      terminateProcess: vi.fn(),
      timeoutMs: 100,
      timers,
    },
    timers,
  };
}

async function settledState(promise: Promise<unknown>): Promise<boolean> {
  let settled = false;
  void promise.then(
    () => {
      settled = true;
    },
    () => {
      settled = true;
    },
  );
  await Promise.resolve();
  return settled;
}

describe("Electron harness launch selection", () => {
  it.each([
    {
      display: ":99",
      expected: {
        args: ["--flag", "/fixtures"],
        command: "/electron",
        detached: true,
      },
      platform: "linux" as const,
    },
    {
      display: undefined,
      expected: {
        args: ["-a", "/electron", "--flag", "/fixtures"],
        command: "xvfb-run",
        detached: true,
      },
      platform: "linux" as const,
    },
    {
      display: ":0",
      expected: {
        args: ["--flag", "/fixtures"],
        command: "/electron",
        detached: true,
      },
      platform: "darwin" as const,
    },
    {
      display: undefined,
      expected: {
        args: ["--flag", "/fixtures"],
        command: "C:\\Electron.exe",
        detached: false,
      },
      executable: "C:\\Electron.exe",
      platform: "win32" as const,
    },
  ])("selects the expected $platform command with DISPLAY=$display", (testCase) => {
    expect(
      selectHarnessLaunch({
        display: testCase.display,
        electronArguments: ["--flag", "/fixtures"],
        executable: testCase.executable ?? "/electron",
        platform: testCase.platform,
      }),
    ).toEqual(testCase.expected);
  });
});

describe("Electron harness process lifecycle", () => {
  it("waits for close and includes output drained after exit", async () => {
    const child = new FakeChild();
    const { options } = harnessOptions(child);
    const result = runHarnessProcess(options);

    child.emit("exit", 0, null);
    child.stdout.write("drained-after-exit");
    expect(await settledState(result)).toBe(false);
    child.emit("close", 0, null);

    await expect(result).resolves.toEqual({
      stderr: "",
      stdout: "drained-after-exit",
    });
  });

  it("retains a spawn error until close and reports drained diagnostics", async () => {
    const child = new FakeChild();
    const { options } = harnessOptions(child);
    const result = runHarnessProcess(options);

    child.emit("error", new Error("spawn denied"));
    child.stderr.write("late diagnostic");
    expect(await settledState(result)).toBe(false);
    child.emit("close", 1, null);

    const failure = await result.catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(HarnessProcessError);
    expect(failure).toMatchObject({ processClosed: true });
    expect(String(failure)).toContain("spawn denied");
    expect(String(failure)).toContain("late diagnostic");
  });

  it("waits for close after a timeout requests force termination", async () => {
    const child = new FakeChild();
    const { options, timers } = harnessOptions(child);
    const result = runHarnessProcess(options);

    timers.fire(100);
    expect(options.terminateProcess).toHaveBeenCalledWith(child, "linux");
    expect(await settledState(result)).toBe(false);
    expect(timers.active(50)).toBe(1);
    child.emit("close", null, "SIGKILL");

    const failure = await result.catch((error: unknown) => error);
    expect(failure).toMatchObject({ processClosed: true });
    expect(String(failure)).toContain("timed out");
    expect(timers.active(50)).toBe(0);
  });

  it("handles a synchronous close emitted while force termination runs", async () => {
    const child = new FakeChild();
    const { options, timers } = harnessOptions(child);
    options.terminateProcess.mockImplementation(() => {
      child.emit("close", null, "SIGKILL");
    });
    const result = runHarnessProcess(options);

    timers.fire(100);

    await expect(result).rejects.toMatchObject({ processClosed: true });
    expect(options.terminateProcess).toHaveBeenCalledTimes(1);
    expect(timers.active(50)).toBe(0);
  });

  it("settles once when terminal events race", async () => {
    const child = new FakeChild();
    const { options } = harnessOptions(child);
    let settlements = 0;
    const result = runHarnessProcess(options);
    void result.then(
      () => {
        settlements += 1;
      },
      () => {
        settlements += 1;
      },
    );

    child.emit("close", 0, null);
    child.emit("close", 1, null);
    child.emit("error", new Error("too late"));
    await result;
    await Promise.resolve();

    expect(settlements).toBe(1);
  });

  it("rejects safely when timeout termination is never confirmed by close", async () => {
    const child = new FakeChild();
    const { options, timers } = harnessOptions(child);
    const result = runHarnessProcess(options);

    timers.fire(100);
    timers.fire(50);

    await expect(result).rejects.toMatchObject({ processClosed: false });
    expect(options.terminateProcess).toHaveBeenCalledTimes(1);
  });

  it("bounds waiting for close after a spawn error", async () => {
    const child = new FakeChild();
    const { options, timers } = harnessOptions(child);
    const result = runHarnessProcess(options);

    child.emit("error", new Error("spawn failed"));
    expect(timers.active(50)).toBe(1);
    timers.fire(50);

    const failure = await result.catch((error: unknown) => error);
    expect(failure).toMatchObject({ processClosed: false });
    expect(String(failure)).toContain("spawn failed");
  });

  it("marks a synchronous spawn failure safe for cleanup", async () => {
    const child = new FakeChild();
    const { options } = harnessOptions(child);
    options.spawnProcess.mockImplementation(() => {
      throw new Error("synchronous spawn failure");
    });

    await expect(runHarnessProcess(options)).rejects.toMatchObject({
      processClosed: true,
    });
  });

  it("caps captured stdout and stderr", async () => {
    const child = new FakeChild();
    const { options } = harnessOptions(child);
    const result = runHarnessProcess(options);
    const oversized = Buffer.alloc(MAX_HARNESS_OUTPUT_BYTES + 1024, "x");

    child.stdout.write(oversized);
    child.stderr.write(oversized);
    child.emit("close", 0, null);

    const output = await result;
    expect(Buffer.byteLength(output.stdout)).toBeLessThanOrEqual(
      MAX_HARNESS_OUTPUT_BYTES + Buffer.byteLength("\n[output truncated]"),
    );
    expect(Buffer.byteLength(output.stderr)).toBeLessThanOrEqual(
      MAX_HARNESS_OUTPUT_BYTES + Buffer.byteLength("\n[output truncated]"),
    );
    expect(output.stdout).toContain("[output truncated]");
    expect(output.stderr).toContain("[output truncated]");
  });

  it("copies a retained prefix instead of retaining an oversized backing buffer", async () => {
    const child = new FakeChild();
    const { options } = harnessOptions(child);
    const result = runHarnessProcess(options);
    const oversized = Buffer.alloc(MAX_HARNESS_OUTPUT_BYTES + 1024, "x");

    child.stdout.write(oversized);
    oversized.fill("y");
    child.emit("close", 0, null);

    expect((await result).stdout.startsWith("x")).toBe(true);
  });

  it("explains when headless Linux cannot launch xvfb-run", async () => {
    const child = new FakeChild();
    const { options } = harnessOptions(child);
    options.environment = {};
    const result = runHarnessProcess(options);

    child.emit("error", Object.assign(new Error("spawn xvfb-run ENOENT"), { code: "ENOENT" }));
    child.emit("close", -2, null);

    await expect(result).rejects.toThrow(/xvfb-run.*Xvfb/i);
  });
});

describe("Electron harness force termination", () => {
  it("kills the detached POSIX process group", () => {
    const child = new FakeChild();
    const killProcess = vi.fn(() => true);

    terminateHarnessProcess(child, "linux", killProcess);

    expect(killProcess).toHaveBeenCalledWith(-child.pid, "SIGKILL");
    expect(child.kill).not.toHaveBeenCalled();
  });

  it("force-kills the direct Windows child", () => {
    const child = new FakeChild();
    const killProcess = vi.fn(() => true);

    terminateHarnessProcess(child, "win32", killProcess);

    expect(child.kill).toHaveBeenCalledWith("SIGKILL");
    expect(killProcess).not.toHaveBeenCalled();
  });
});
