// @vitest-environment node

import { describe, expect, it, vi } from "vitest";

import {
  assertOwnedStagedArtifact,
  closeAndReapElectron,
  type ElectronLifecycleOperations,
  pollForNoCoreProcesses,
  runElectronLifecycle,
  type StagedArtifact,
} from "../../tests/e2e/electron-lifecycle";

interface TestState {
  application?: true;
  harness?: true;
  mainCloseObserved?: true;
  page?: true;
  traceStarted?: boolean;
}

const stagedArtifacts = {
  screenshot: { kind: "screenshot", path: "/owned/evidence/screenshot.png" },
  trace: { kind: "trace", path: "/owned/evidence/trace.zip" },
  video: { kind: "video", path: "/owned/evidence/video.webm" },
} as const satisfies Record<string, StagedArtifact>;

function failure(message: string) {
  return new Error(message);
}

function createOperations({
  attachmentFailure,
  cleanupFailure,
  failed = false,
  setupFailure,
  useFailure,
}: {
  attachmentFailure?: StagedArtifact["kind"];
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
    attachArtifact: async (_state, artifact) => {
      const step = `attach:${artifact.kind}`;
      calls.push(step);
      if (attachmentFailure === artifact.kind) throw failure(step);
    },
    closeAndReap: async () => {
      calls.push("close");
      maybeFail("close");
    },
    deleteStagedArtifacts: async (_state, artifacts) => {
      calls.push(`delete:${artifacts.map((artifact) => artifact.kind).join(",")}`);
      maybeFail("delete");
    },
    didTestFail: () => failed,
    removeRawArtifacts: async () => {
      calls.push("rawRm");
      maybeFail("rawRm");
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
    stageScreenshot: async (state) => {
      calls.push("stage:screenshot");
      maybeFail("screenshot");
      return state.page ? stagedArtifacts.screenshot : undefined;
    },
    stageVideo: async (state) => {
      calls.push("stage:video");
      maybeFail("video");
      return state.page ? stagedArtifacts.video : undefined;
    },
    stopAndStageTrace: async (state) => {
      calls.push("stage:trace");
      maybeFail("traceStop");
      return state.traceStarted ? stagedArtifacts.trace : undefined;
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
    "cleans every available stage and retains staged evidence when %s setup fails",
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
        expect.arrayContaining([
          "stage:screenshot",
          "close",
          "stage:video",
          "rawRm",
          "processQuery",
        ]),
      );
      expect(calls.some((call) => call.startsWith("delete:"))).toBe(false);
      expect(calls.indexOf("close")).toBeLessThan(calls.indexOf("stage:video"));
      expect(calls.indexOf("stage:video")).toBeLessThan(calls.indexOf("rawRm"));
      expect(calls.indexOf("rawRm")).toBeLessThan(calls.indexOf("processQuery"));
    },
  );

  it.each(["screenshot", "traceStop", "close", "video", "rawRm", "processQuery"])(
    "retains all successfully staged evidence when otherwise-successful %s cleanup fails",
    async (cleanupFailure) => {
      const { calls, operations } = createOperations({ cleanupFailure });
      let caught: unknown;

      try {
        await runElectronLifecycle({}, operations);
      } catch (error) {
        caught = error;
      }

      expect(errorMessages(caught)).toEqual([cleanupFailure]);
      expect(calls.some((call) => call.startsWith("delete:"))).toBe(false);
      const expectedStages = [
        "stage:screenshot",
        "stage:trace",
        "close",
        "stage:video",
        "rawRm",
        "processQuery",
      ];
      for (const step of expectedStages) expect(calls).toContain(step);
      const failedKind =
        cleanupFailure === "screenshot"
          ? "screenshot"
          : cleanupFailure === "traceStop"
            ? "trace"
            : cleanupFailure === "video"
              ? "video"
              : undefined;
      for (const kind of ["screenshot", "trace", "video"] as const) {
        expect(calls.includes(`attach:${kind}`)).toBe(kind !== failedKind);
      }
    },
  );

  it.each(["screenshot", "trace", "video"] as const)(
    "continues attaching every staged artifact when %s attachment fails",
    async (attachmentFailure) => {
      const { calls, operations } = createOperations({
        attachmentFailure,
        cleanupFailure: "rawRm",
      });
      let caught: unknown;

      try {
        await runElectronLifecycle({}, operations);
      } catch (error) {
        caught = error;
      }

      expect(errorMessages(caught)).toEqual(["rawRm", `attach:${attachmentFailure}`]);
      expect(calls).toEqual(
        expect.arrayContaining(["attach:screenshot", "attach:trace", "attach:video"]),
      );
      expect(calls.some((call) => call.startsWith("delete:"))).toBe(false);
    },
  );

  it("turns staged-artifact deletion failure into retention for every artifact", async () => {
    const { calls, operations } = createOperations({ cleanupFailure: "delete" });
    let caught: unknown;

    try {
      await runElectronLifecycle({}, operations);
    } catch (error) {
      caught = error;
    }

    expect(errorMessages(caught)).toEqual(["delete"]);
    expect(calls).toContain("delete:screenshot,trace,video");
    expect(calls).toEqual(
      expect.arrayContaining(["attach:screenshot", "attach:trace", "attach:video"]),
    );
  });

  it("deletes staged artifacts only after every lifecycle stage succeeds", async () => {
    const { calls, operations } = createOperations();

    await runElectronLifecycle({}, operations);

    expect(calls).not.toContain("attach:screenshot");
    expect(calls).not.toContain("attach:trace");
    expect(calls).not.toContain("attach:video");
    expect(calls.at(-1)).toBe("delete:screenshot,trace,video");
    expect(calls.indexOf("rawRm")).toBeLessThan(calls.indexOf("processQuery"));
    expect(calls.indexOf("processQuery")).toBeLessThan(
      calls.indexOf("delete:screenshot,trace,video"),
    );
  });

  it("keeps the primary failure first while aggregating cleanup and attachment failures", async () => {
    const { calls, operations } = createOperations({
      attachmentFailure: "trace",
      useFailure: true,
    });
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

    expect(errorMessages(caught)).toEqual(["use", "rawRm", "processQuery", "attach:trace"]);
    expect(calls).toEqual(
      expect.arrayContaining(["attach:screenshot", "attach:trace", "attach:video"]),
    );
  });

  it("continues all cleanup and retains evidence when the test-status probe fails", async () => {
    const { calls, operations } = createOperations();
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
        "stage:screenshot",
        "stage:trace",
        "close",
        "stage:video",
        "rawRm",
        "processQuery",
        "attach:screenshot",
        "attach:trace",
        "attach:video",
      ]),
    );
    expect(calls.some((call) => call.startsWith("delete:"))).toBe(false);
  });

  it("force-reaps a close timeout before staging video and retaining all evidence", async () => {
    const { calls, operations } = createOperations();
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
    expect(calls.indexOf("forceKill")).toBeLessThan(calls.indexOf("stage:video"));
    expect(calls.indexOf("stage:video")).toBeLessThan(calls.indexOf("rawRm"));
    expect(calls.indexOf("rawRm")).toBeLessThan(calls.indexOf("processQuery"));
    expect(calls).toEqual(
      expect.arrayContaining(["attach:screenshot", "attach:trace", "attach:video"]),
    );
  });
});

describe("assertOwnedStagedArtifact", () => {
  it("accepts only the fixed filename for an artifact under the owned staging root", () => {
    expect(() =>
      assertOwnedStagedArtifact("/owned/evidence", {
        kind: "trace",
        path: "/owned/evidence/electron-trace.zip",
      }),
    ).not.toThrow();
  });

  it.each([
    "/external/electron-trace.zip",
    "/owned/evidence/../external/electron-trace.zip",
    "/owned/evidence/failure.webm",
  ])("rejects unowned or mismatched artifact path %s", (artifactPath) => {
    expect(() =>
      assertOwnedStagedArtifact("/owned/evidence", {
        kind: "trace",
        path: artifactPath,
      }),
    ).toThrow("UNOWNED_STAGED_ARTIFACT");
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
