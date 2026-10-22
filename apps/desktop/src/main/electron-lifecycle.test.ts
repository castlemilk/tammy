// @vitest-environment node

import type { ChildProcess } from "node:child_process";
import { EventEmitter } from "node:events";
import { join, resolve, sep } from "node:path";
import { PassThrough } from "node:stream";
import { describe, expect, it, vi } from "vitest";

import {
  assertOwnedStagedArtifact,
  closeAndReapElectron,
  type ElectronLifecycleOperations,
  pollForNoCoreProcesses,
  runElectronLifecycle,
  type StagedArtifact,
} from "../../tests/e2e/electron-lifecycle";
import {
  ElectronProcessDiagnostics,
  observeElectronProcessDiagnostics,
  safeElectronCloseFailureCode,
  safeFirstWindowFailureCode,
} from "../../tests/e2e/electron-process-diagnostics";

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
  finalizeFailure,
  setupFailure,
  useFailure,
}: {
  attachmentFailure?: StagedArtifact["kind"];
  cleanupFailure?: string;
  failed?: boolean;
  finalizeFailure?: boolean;
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
    finalize: async (_state, clean) => {
      calls.push(`finalize:${clean}`);
      if (finalizeFailure) throw failure("finalize");
    },
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
    expect(calls.at(-1)).toBe("finalize:true");
    expect(calls.indexOf("rawRm")).toBeLessThan(calls.indexOf("processQuery"));
    expect(calls.indexOf("processQuery")).toBeLessThan(
      calls.indexOf("delete:screenshot,trace,video"),
    );
    expect(calls.indexOf("delete:screenshot,trace,video")).toBeLessThan(
      calls.indexOf("finalize:true"),
    );
  });

  it("finalizes failure only after retained evidence and clean teardown attempts", async () => {
    const { calls, operations } = createOperations({ useFailure: true });

    await expect(runElectronLifecycle({}, operations)).rejects.toThrow("use");

    expect(calls.at(-1)).toBe("finalize:false");
    expect(calls.indexOf("attach:video")).toBeLessThan(calls.indexOf("finalize:false"));
  });

  it("refuses a clean lifecycle when the final result writer fails", async () => {
    const { calls, operations } = createOperations({ finalizeFailure: true });

    await expect(runElectronLifecycle({}, operations)).rejects.toThrow("finalize");

    expect(calls.at(-1)).toBe("finalize:true");
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
  const stagingRoot = resolve("owned", "evidence");

  it("accepts only the fixed filename for an artifact under the owned staging root", () => {
    expect(() =>
      assertOwnedStagedArtifact(stagingRoot, {
        kind: "trace",
        path: join(stagingRoot, "electron-trace.zip"),
      }),
    ).not.toThrow();
  });

  it.each([
    resolve("external", "electron-trace.zip"),
    `${stagingRoot}${sep}..${sep}external${sep}electron-trace.zip`,
    join(stagingRoot, "failure.webm"),
  ])("rejects unowned or mismatched artifact path %s", (artifactPath) => {
    expect(() =>
      assertOwnedStagedArtifact(stagingRoot, {
        kind: "trace",
        path: artifactPath,
      }),
    ).toThrow("UNOWNED_STAGED_ARTIFACT");
  });
});

describe("closeAndReapElectron", () => {
  it("labels an internal SBR restart close timeout with its exact lifecycle stage", async () => {
    let confirmExited!: () => void;
    const mainClosed = new Promise<void>((resolve) => {
      confirmExited = resolve;
    });
    let caught: unknown;

    try {
      await closeAndReapElectron({
        forceKillMain: confirmExited,
        gracefulClose: () => new Promise<void>(() => {}),
        mainClosed,
        stage: "sbr-helper-death-recovery",
        timeoutMs: 5,
      });
    } catch (error) {
      caught = error;
    }

    expect(errorMessages(caught)).toEqual([
      "ELECTRON_CLOSE_TIMEOUT [stage=sbr-helper-death-recovery]",
    ]);
  });

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

describe("ElectronProcessDiagnostics", () => {
  it("retains workspace creation status and timing markers without error data", () => {
    const diagnostics = new ElectronProcessDiagnostics();
    diagnostics.observeStderr(
      "LOCAL_ENGINE_WORKSPACE_CREATE_FAILED LOCAL_ENGINE_CONNECT_DEADLINE_EXCEEDED LOCAL_ENGINE_WORKSPACE_CREATE_DURATION_5_TO_10_SECONDS private=secret",
    );
    const summary = diagnostics.formatSummary();
    expect(summary).toContain("LOCAL_ENGINE_WORKSPACE_CREATE_FAILED");
    expect(summary).toContain("LOCAL_ENGINE_CONNECT_DEADLINE_EXCEEDED");
    expect(summary).toContain("LOCAL_ENGINE_WORKSPACE_CREATE_DURATION_5_TO_10_SECONDS");
    expect(summary).not.toContain("secret");
  });
  it("retains the fixed startup stage and error code without adjacent private text", () => {
    const diagnostics = new ElectronProcessDiagnostics();
    diagnostics.observeStderr(
      "LOCAL_ENGINE_STARTUP_CORE_START LOCAL_ENGINE_CORE_READINESS_TIMEOUT path=/private token=private-secret",
    );
    const summary = diagnostics.formatSummary();
    expect(summary).toContain("LOCAL_ENGINE_STARTUP_CORE_START");
    expect(summary).toContain("LOCAL_ENGINE_CORE_READINESS_TIMEOUT");
    expect(summary).not.toContain("/private");
    expect(summary).not.toContain("private-secret");
  });

  it("retains only allowlisted markers from stderr, never raw text", () => {
    const diagnostics = new ElectronProcessDiagnostics();
    diagnostics.observeStderr(
      "path=/private/workspace password=do-not-retain token=not-a-token LOCAL_ENGINE_UNAVAILABLE",
    );

    expect(diagnostics.formatSummary()).toBe(
      "electronProcess=RUNNING;stderrMarkers=LOCAL_ENGINE_UNAVAILABLE",
    );
    expect(diagnostics.formatSummary()).not.toContain("/private/workspace");
    expect(diagnostics.formatSummary()).not.toContain("do-not-retain");
    expect(diagnostics.formatSummary()).not.toContain("not-a-token");
  });

  it("recognizes an allowlisted marker split across byte chunks", () => {
    const diagnostics = new ElectronProcessDiagnostics();
    diagnostics.observeStderr(Buffer.from("LOCAL_ENGINE_SHUTDOWN_"));
    diagnostics.observeStderr(Buffer.from("FAILED"));

    expect(diagnostics.formatSummary()).toBe(
      "electronProcess=RUNNING;stderrMarkers=LOCAL_ENGINE_SHUTDOWN_FAILED",
    );
  });

  it("does not retain arbitrary error, credential, or path text", () => {
    const diagnostics = new ElectronProcessDiagnostics();
    diagnostics.observeStderr("startup failed at /secret/path with capability=private-value");
    diagnostics.observeStderr("LOCAL_ENGINE_OTHER PRIVATE_TOKEN=secret");

    expect(diagnostics.formatSummary()).toBe("electronProcess=RUNNING;stderrMarkers=none");
  });

  it("records a normalized process exit code and signal", () => {
    const diagnostics = new ElectronProcessDiagnostics();
    diagnostics.observeExit(1, null);

    expect(diagnostics.formatSummary()).toBe(
      "electronProcess=EXIT(code=1,signal=none);stderrMarkers=none",
    );
  });

  it("removes the stderr observer when the child process closes", () => {
    const diagnostics = new ElectronProcessDiagnostics();
    const stderr = new PassThrough();
    const events = new EventEmitter();
    const process = Object.assign(events, {
      exitCode: null,
      signalCode: null,
      stderr,
    }) as unknown as ChildProcess;

    observeElectronProcessDiagnostics(process, diagnostics);
    expect(stderr.listenerCount("data")).toBe(1);

    stderr.emit("data", Buffer.from("LOCAL_ENGINE_UNAVAILABLE"));
    events.emit("exit", 0, null);
    events.emit("close", 0, null);

    expect(stderr.listenerCount("data")).toBe(0);
    expect(events.listenerCount("exit")).toBe(0);
    expect(events.listenerCount("close")).toBe(0);
    expect(diagnostics.formatSummary(process)).toBe(
      "electronProcess=EXIT(code=0,signal=none);stderrMarkers=LOCAL_ENGINE_UNAVAILABLE",
    );
  });

  it("preserves process status and leaves no listeners when close preceded observation", () => {
    const diagnostics = new ElectronProcessDiagnostics();
    const events = new EventEmitter();
    const process = Object.assign(events, {
      exitCode: 0,
      signalCode: null,
      stderr: null,
    }) as unknown as ChildProcess;

    events.emit("exit", 0, null);
    events.emit("close", 0, null);
    observeElectronProcessDiagnostics(process, diagnostics);

    expect(events.listenerCount("exit")).toBe(0);
    expect(events.listenerCount("close")).toBe(0);
    expect(diagnostics.formatSummary(process)).toBe(
      "electronProcess=EXIT(code=0,signal=none);stderrMarkers=none",
    );
  });

  it.each(["destroyed", "readableEnded", "closed"] as const)(
    "skips an unavailable stderr stream when it is %s",
    (state) => {
      const diagnostics = new ElectronProcessDiagnostics();
      const stderr = Object.assign(new EventEmitter(), {
        closed: state === "closed",
        destroyed: state === "destroyed",
        readableEnded: state === "readableEnded",
      });
      const events = new EventEmitter();
      const process = Object.assign(events, {
        exitCode: null,
        signalCode: null,
        stderr,
      }) as unknown as ChildProcess;

      observeElectronProcessDiagnostics(process, diagnostics);
      expect(stderr.listenerCount("data")).toBe(0);

      events.emit("exit", 1, null);
      events.emit("close", 1, null);

      expect(events.listenerCount("exit")).toBe(0);
      expect(events.listenerCount("close")).toBe(0);
      expect(diagnostics.formatSummary(process)).toBe(
        "electronProcess=EXIT(code=1,signal=none);stderrMarkers=none",
      );
    },
  );

  it("normalizes invalid process status fields instead of retaining arbitrary strings", () => {
    const diagnostics = new ElectronProcessDiagnostics();
    diagnostics.observeExit(999, "PRIVATE_SIGNAL" as NodeJS.Signals);

    expect(diagnostics.formatSummary()).toBe(
      "electronProcess=EXIT(code=unknown,signal=none);stderrMarkers=none",
    );
  });

  it("uses valid ChildProcess exit fields when the exit event has not arrived yet", () => {
    const diagnostics = new ElectronProcessDiagnostics();

    expect(diagnostics.formatSummary({ exitCode: null, signalCode: null })).toBe(
      "electronProcess=RUNNING;stderrMarkers=none",
    );
    expect(diagnostics.formatSummary({ exitCode: null, signalCode: "SIGKILL" })).toBe(
      "electronProcess=EXIT(code=unknown,signal=SIGKILL);stderrMarkers=none",
    );
  });

  it("reduces errors to fixed diagnostic enums without preserving exception text", () => {
    expect(
      safeFirstWindowFailureCode(
        new Error("Target page, context or browser has been closed: /secret token=private"),
      ),
    ).toBe("ELECTRON_TARGET_CLOSED");
    expect(safeFirstWindowFailureCode(new Error("passphrase=private"))).toBe(
      "ELECTRON_FIRST_WINDOW_FAILED",
    );
    expect(
      safeElectronCloseFailureCode(new Error("ELECTRON_CLOSE_TIMEOUT [stage=final-test-teardown]")),
    ).toBe("ELECTRON_CLOSE_TIMEOUT");
    expect(safeElectronCloseFailureCode(new Error("ELECTRON_CLOSE_FAILED token=private"))).toBe(
      "ELECTRON_CLOSE_FAILED",
    );
  });

  it("preserves only allowlisted causes from aggregated close failures", () => {
    const failure = new AggregateError(
      [
        new Error("graceful shutdown failed at /private/path"),
        new Error("ELECTRON_FORCE_KILL_FAILED token=private"),
        new Error("ELECTRON_REAP_TIMEOUT [stage=final-test-teardown]"),
      ],
      "ELECTRON_CLOSE_FAILED",
    );

    expect(safeElectronCloseFailureCode(failure)).toBe(
      "ELECTRON_CLOSE_FAILED(causes=ELECTRON_FORCE_KILL_FAILED,ELECTRON_REAP_TIMEOUT)",
    );
    expect(safeElectronCloseFailureCode(failure)).not.toContain("/private/path");
    expect(safeElectronCloseFailureCode(failure)).not.toContain("token=private");
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
