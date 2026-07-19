// @vitest-environment node

import { describe, expect, it, vi } from "vitest";

import {
  closeAndReapElectron,
  type ElectronLifecycleOperations,
  pollForNoCoreProcesses,
  runElectronLifecycle,
} from "../../tests/e2e/electron-lifecycle";

interface TestState {
  application?: true;
  harness?: true;
  mainCloseObserved?: true;
  page?: true;
  traceStarted?: boolean;
}

function failure(message: string) {
  return new Error(message);
}

function createOperations({
  cleanupFailure,
  failed = true,
  setupFailure,
  useFailure,
}: {
  cleanupFailure?: string;
  failed?: boolean;
  setupFailure?: "firstWindow" | "traceStart";
  useFailure?: boolean;
} = {}) {
  const calls: string[] = [];
  const maybeFail = (step: string) => {
    if (cleanupFailure === step) throw failure(step);
  };
  const operations: ElectronLifecycleOperations<TestState, true> = {
    assertNoOrphan: async () => {
      calls.push("processQuery");
      maybeFail("processQuery");
    },
    attachTrace: async () => {
      calls.push("traceAttach");
      maybeFail("traceAttach");
    },
    closeAndReap: async () => {
      calls.push("close");
      maybeFail("close");
    },
    didTestFail: () => failed,
    handleVideo: async (_state, retained) => {
      calls.push(retained ? "videoRetain" : "videoDelete");
      maybeFail("video");
    },
    removeRawArtifacts: async () => {
      calls.push("rawRm");
      maybeFail("rawRm");
    },
    screenshot: async () => {
      calls.push("screenshot");
      maybeFail("screenshot");
    },
    setup: async (state) => {
      calls.push("launch");
      state.application = true;
      calls.push("observeMainClose");
      state.mainCloseObserved = true;
      calls.push("observePages");
      calls.push("firstWindow");
      if (setupFailure === "firstWindow") throw failure("firstWindow");
      state.page = true;
      calls.push("traceStart");
      if (setupFailure === "traceStart") throw failure("traceStart");
      state.traceStarted = true;
      state.harness = true;
      return true;
    },
    stopTrace: async (_state, retained) => {
      calls.push(retained ? "traceStopRetained" : "traceStopDiscarded");
      maybeFail("traceStop");
    },
    use: async () => {
      calls.push("use");
      if (useFailure) throw failure("use");
    },
  };
  return { calls, operations };
}

function errorMessages(error: unknown): string[] {
  if (error instanceof AggregateError) {
    return error.errors.map((child) => (child instanceof Error ? child.message : String(child)));
  }
  return [error instanceof Error ? error.message : String(error)];
}

describe("runElectronLifecycle", () => {
  it.each(["firstWindow", "traceStart"] as const)(
    "cleans every available stage when %s setup fails",
    async (setupFailure) => {
      const { calls, operations } = createOperations({ setupFailure });
      let caught: unknown;

      try {
        await runElectronLifecycle({}, operations);
      } catch (error) {
        caught = error;
      }

      expect(errorMessages(caught)).toEqual([setupFailure]);
      expect(calls).not.toContain("use");
      expect(calls).toEqual(
        expect.arrayContaining(["screenshot", "close", "videoRetain", "rawRm", "processQuery"]),
      );
      expect(calls.indexOf("close")).toBeLessThan(calls.indexOf("videoRetain"));
      expect(calls.indexOf("videoRetain")).toBeLessThan(calls.indexOf("rawRm"));
      expect(calls.indexOf("rawRm")).toBeLessThan(calls.indexOf("processQuery"));
    },
  );

  it.each(["screenshot", "traceStop", "traceAttach", "close", "video", "rawRm", "processQuery"])(
    "continues later cleanup when %s fails",
    async (cleanupFailure) => {
      const { calls, operations } = createOperations({
        cleanupFailure,
        useFailure: true,
      });
      let caught: unknown;

      try {
        await runElectronLifecycle({}, operations);
      } catch (error) {
        caught = error;
      }

      expect(errorMessages(caught)).toEqual(["use", cleanupFailure]);
      const expectedTail = [
        "screenshot",
        "traceStopRetained",
        "traceAttach",
        "close",
        "videoRetain",
        "rawRm",
        "processQuery",
      ];
      for (const step of expectedTail) expect(calls).toContain(step);
    },
  );

  it("discards trace and video artifacts on success without retaining attachments", async () => {
    const { calls, operations } = createOperations({ failed: false });

    await runElectronLifecycle({}, operations);

    expect(calls).toContain("traceStopDiscarded");
    expect(calls).not.toContain("traceAttach");
    expect(calls).toContain("videoDelete");
    expect(calls.at(-2)).toBe("rawRm");
    expect(calls.at(-1)).toBe("processQuery");
  });

  it("keeps the primary failure first while aggregating every cleanup failure", async () => {
    const calls: string[] = [];
    const operations = createOperations({ setupFailure: "firstWindow" }).operations;
    operations.screenshot = async () => {
      calls.push("screenshot");
      throw failure("screenshot");
    };
    operations.removeRawArtifacts = async () => {
      calls.push("rawRm");
      throw failure("rawRm");
    };
    operations.assertNoOrphan = async () => {
      calls.push("processQuery");
      throw failure("processQuery");
    };
    let caught: unknown;

    try {
      await runElectronLifecycle({}, operations);
    } catch (error) {
      caught = error;
    }

    expect(errorMessages(caught)).toEqual(["firstWindow", "screenshot", "rawRm", "processQuery"]);
    expect(calls).toEqual(["screenshot", "rawRm", "processQuery"]);
  });

  it("continues all cleanup when the test-status probe fails", async () => {
    const { calls, operations } = createOperations({ failed: false });
    operations.didTestFail = () => {
      throw failure("status");
    };
    let caught: unknown;

    try {
      await runElectronLifecycle({}, operations);
    } catch (error) {
      caught = error;
    }

    expect(errorMessages(caught)).toEqual(["status"]);
    expect(calls).toEqual(
      expect.arrayContaining([
        "screenshot",
        "traceStopRetained",
        "traceAttach",
        "close",
        "videoRetain",
        "rawRm",
        "processQuery",
      ]),
    );
  });

  it("force-reaps a graceful-close timeout before continuing artifact and orphan cleanup", async () => {
    const { calls, operations } = createOperations({ failed: true });
    let confirmClosed!: () => void;
    const mainClosed = new Promise<void>((resolve) => {
      confirmClosed = resolve;
    });
    operations.closeAndReap = async () => {
      calls.push("close");
      await closeAndReapElectron({
        forceKillMain: () => {
          calls.push("forceKill");
          confirmClosed();
        },
        gracefulClose: () => new Promise<void>(() => {}),
        mainClosed,
        timeoutMs: 5,
      });
    };
    let caught: unknown;

    try {
      await runElectronLifecycle({}, operations);
    } catch (error) {
      caught = error;
    }

    expect(errorMessages(caught)).toEqual(["ELECTRON_CLOSE_TIMEOUT"]);
    expect(calls.indexOf("close")).toBeLessThan(calls.indexOf("forceKill"));
    expect(calls.indexOf("forceKill")).toBeLessThan(calls.indexOf("videoRetain"));
    expect(calls.indexOf("videoRetain")).toBeLessThan(calls.indexOf("rawRm"));
    expect(calls.indexOf("rawRm")).toBeLessThan(calls.indexOf("processQuery"));
  });
});

describe("closeAndReapElectron", () => {
  it("force-kills and confirms main-process close after graceful close times out", async () => {
    let confirmClosed!: () => void;
    const mainClosed = new Promise<void>((resolve) => {
      confirmClosed = resolve;
    });
    const forceKillMain = vi.fn(() => {
      confirmClosed();
    });
    let caught: unknown;

    try {
      await closeAndReapElectron({
        forceKillMain,
        gracefulClose: () => new Promise<void>(() => {}),
        mainClosed,
        timeoutMs: 5,
      });
    } catch (error) {
      caught = error;
    }

    expect(forceKillMain).toHaveBeenCalledOnce();
    expect(errorMessages(caught)).toEqual(["ELECTRON_CLOSE_TIMEOUT"]);
  });

  it("aggregates graceful, force-kill, and reap failures in execution order", async () => {
    let caught: unknown;

    try {
      await closeAndReapElectron({
        forceKillMain: () => {
          throw failure("force");
        },
        gracefulClose: async () => {
          throw failure("graceful");
        },
        mainClosed: new Promise<void>(() => {}),
        timeoutMs: 5,
      });
    } catch (error) {
      caught = error;
    }

    expect(errorMessages(caught)).toEqual(["graceful", "force", "ELECTRON_REAP_TIMEOUT"]);
  });
});

describe("pollForNoCoreProcesses", () => {
  it("polls bounded exact-process evidence until no orphan remains", async () => {
    let now = 0;
    const query = vi
      .fn()
      .mockResolvedValueOnce([{ executablePath: "/Tammy/tammy-core", processId: 9 }])
      .mockResolvedValueOnce([{ executablePath: "/Tammy/tammy-core", processId: 9 }])
      .mockResolvedValueOnce([]);
    const sleep = vi.fn(async (milliseconds: number) => {
      now += milliseconds;
    });

    await pollForNoCoreProcesses({
      intervalMs: 10,
      now: () => now,
      query,
      sleep,
      timeoutMs: 50,
    });

    expect(query).toHaveBeenCalledTimes(3);
    expect(sleep).toHaveBeenCalledTimes(2);
  });

  it("fails with the remaining exact-process evidence at the deadline", async () => {
    let now = 0;
    const query = vi
      .fn()
      .mockResolvedValue([{ executablePath: "/Tammy/tammy-core", processId: 9 }]);

    await expect(
      pollForNoCoreProcesses({
        intervalMs: 10,
        now: () => now,
        query,
        sleep: async (milliseconds) => {
          now += milliseconds;
        },
        timeoutMs: 20,
      }),
    ).rejects.toThrow("CORE_PROCESS_ORPHAN");
    expect(query).toHaveBeenCalledTimes(3);
  });
});
