import { Code, ConnectError } from "@connectrpc/connect";
import { expect, it } from "vitest";

import { CoreProcessError, type CoreProcessErrorCode } from "./core-process";
import {
  CORE_RPC_DIAGNOSTIC_MARKERS,
  type DesktopStartupStage,
  STARTUP_DIAGNOSTIC_MARKERS,
  safeBasExportFailure,
  safeCoreRpcFailure,
  safeDesktopStartupFailure,
  safeWorkspaceCreateFailure,
  UNLOCK_FAILURE_DIAGNOSTIC_MARKERS,
} from "./startup-diagnostics";

it("classifies BAS export failures by fixed stage and concrete status only", () => {
  for (const stage of [
    "HISTORY_READ",
    "FROZEN_READ",
    "CALCULATION_READ",
    "PREPARATION_READ",
    "SNAPSHOT_VALIDATION",
    "EVENT_APPEND_STARTED",
    "EVENT_APPEND_FINISHED",
    "EXPORT_HANDLER",
  ] as const) {
    const failure = new ConnectError("private report path and evidence", Code.InvalidArgument);
    expect(safeBasExportFailure(stage, failure)).toBe(
      `LOCAL_ENGINE_BAS_EXPORT_${stage} LOCAL_ENGINE_CONNECT_INVALID_ARGUMENT`,
    );
    expect(safeBasExportFailure(stage, new Error("private"))).toBe(
      `LOCAL_ENGINE_BAS_EXPORT_${stage}`,
    );
  }
});

it("allowlists fixed unlock failure stages for packaged process diagnostics", () => {
  expect(UNLOCK_FAILURE_DIAGNOSTIC_MARKERS).toEqual([
    "LOCAL_ENGINE_UNLOCK_WORKSPACE_UNLOCK_FAILED",
    "LOCAL_ENGINE_UNLOCK_SIGN_IN_FAILED",
    "LOCAL_ENGINE_UNLOCK_CURRENT_USER_READ_FAILED",
    "LOCAL_ENGINE_UNLOCK_ORGANISATION_READ_FAILED",
    "LOCAL_ENGINE_UNLOCK_PROJECTION_VALIDATION_FAILED",
  ]);
  for (const marker of UNLOCK_FAILURE_DIAGNOSTIC_MARKERS) {
    expect(marker).toMatch(/^LOCAL_ENGINE_UNLOCK_[A-Z_]+_FAILED$/u);
  }
});

it("classifies only fixed Core RPC operations and actual Connect status codes", () => {
  const operations = [
    "INGEST_DOCUMENT",
    "LIST_DOCUMENTS",
    "LIST_ACCOUNTS",
    "CREATE_BAS_DRAFT",
    "GET_CURRENT_BAS_DRAFT",
    "ASSERT_TOTP",
    "UNLOCK_WORKSPACE",
    "SIGN_IN",
  ] as const;
  for (const operation of operations) {
    const marker = `LOCAL_ENGINE_CORE_RPC_${operation}_FAILED`;
    expect(
      safeCoreRpcFailure(
        operation,
        new ConnectError("private evidence path and credential", Code.InvalidArgument),
      ),
    ).toBe(`${marker} LOCAL_ENGINE_CONNECT_INVALID_ARGUMENT`);
    expect(safeCoreRpcFailure(operation, new Error("private"))).toBe(marker);
    expect(CORE_RPC_DIAGNOSTIC_MARKERS).toContain(marker);
  }
  expect(safeCoreRpcFailure("untrusted-operation" as never, new Error("private"))).toBe(
    "LOCAL_ENGINE_CORE_RPC_FAILED",
  );
  expect(CORE_RPC_DIAGNOSTIC_MARKERS).toContain("LOCAL_ENGINE_CORE_RPC_FAILED");
});

it("classifies unassigned Core operations with a generic marker and only Connect status", () => {
  const result = safeCoreRpcFailure(
    undefined,
    new ConnectError("private evidence /path token=secret", Code.DeadlineExceeded),
  );
  expect(result).toBe("LOCAL_ENGINE_CORE_RPC_FAILED LOCAL_ENGINE_CONNECT_DEADLINE_EXCEEDED");
  expect(result).not.toContain("private");
  expect(result).not.toContain("secret");
  expect(CORE_RPC_DIAGNOSTIC_MARKERS).toContain("LOCAL_ENGINE_CORE_RPC_FAILED");
});

const stages = [
  "REGISTER",
  "READY",
  "RUNTIME_SECURITY",
  "CORE_START",
  "CORE_DIAGNOSTICS",
  "CONVEYOR",
  "WINDOW",
  "WINDOW_SECURITY",
  "LOAD_WINDOW",
] as const satisfies readonly DesktopStartupStage[];

const coreErrorCodes = [
  "INVALID_BINARY_PATH",
  "INVALID_STATE",
  "SPAWN_FAILED",
  "READINESS_TIMEOUT",
  "READINESS_OVERFLOW",
  "READINESS_INVALID",
  "UNEXPECTED_STDOUT",
  "EXIT_BEFORE_READY",
  "CORE_EXITED",
  "START_ABORTED",
  "TERMINATION_FAILED",
] as const satisfies readonly CoreProcessErrorCode[];

const connectCodes = [
  [Code.Canceled, "CANCELED"],
  [Code.Unknown, "UNKNOWN"],
  [Code.InvalidArgument, "INVALID_ARGUMENT"],
  [Code.DeadlineExceeded, "DEADLINE_EXCEEDED"],
  [Code.NotFound, "NOT_FOUND"],
  [Code.AlreadyExists, "ALREADY_EXISTS"],
  [Code.PermissionDenied, "PERMISSION_DENIED"],
  [Code.ResourceExhausted, "RESOURCE_EXHAUSTED"],
  [Code.FailedPrecondition, "FAILED_PRECONDITION"],
  [Code.Aborted, "ABORTED"],
  [Code.OutOfRange, "OUT_OF_RANGE"],
  [Code.Unauthenticated, "UNAUTHENTICATED"],
  [Code.Internal, "INTERNAL"],
  [Code.Unavailable, "UNAVAILABLE"],
  [Code.DataLoss, "DATA_LOSS"],
  [Code.Unimplemented, "UNIMPLEMENTED"],
] as const;

it("emits only a fixed stage marker for generic, message-bearing failures", () => {
  const secret = new Error("/private/path token=secret password=hunter2");

  for (const stage of stages) {
    const result = safeDesktopStartupFailure(stage, secret);
    expect(result).toBe(`LOCAL_ENGINE_STARTUP_${stage}`);
    expect(result).not.toContain("/private");
    expect(result).not.toContain("secret");
    expect(STARTUP_DIAGNOSTIC_MARKERS).toContain(result);
  }
});

it("adds only allowlisted concrete Core process error codes", () => {
  for (const code of coreErrorCodes) {
    const failure = new CoreProcessError(code);
    const result = safeDesktopStartupFailure("CORE_START", failure);
    expect(result).toBe(`LOCAL_ENGINE_STARTUP_CORE_START LOCAL_ENGINE_CORE_${code}`);
    expect(STARTUP_DIAGNOSTIC_MARKERS).toContain(`LOCAL_ENGINE_CORE_${code}`);
  }

  const mutated = new CoreProcessError("READINESS_TIMEOUT");
  Object.defineProperty(mutated, "code", { value: "password=secret", configurable: true });
  expect(safeDesktopStartupFailure("CORE_START", mutated)).toBe("LOCAL_ENGINE_STARTUP_CORE_START");

  const spoofed = Object.create(CoreProcessError.prototype) as CoreProcessError;
  Object.defineProperties(spoofed, {
    name: { value: "CoreProcessError" },
    code: { value: "READINESS_TIMEOUT" },
  });
  expect(safeDesktopStartupFailure("CORE_START", spoofed)).toBe("LOCAL_ENGINE_STARTUP_CORE_START");
});

it("adds fixed Connect code tokens only for actual, well-formed ConnectErrors", () => {
  for (const [code, name] of connectCodes) {
    const error = new ConnectError("/private/path token=secret", code);
    const result = safeDesktopStartupFailure("CONVEYOR", error);
    expect(result).toBe(`LOCAL_ENGINE_STARTUP_CONVEYOR LOCAL_ENGINE_CONNECT_${name}`);
    expect(result).not.toContain("/private");
    expect(result).not.toContain("secret");
    expect(STARTUP_DIAGNOSTIC_MARKERS).toContain(`LOCAL_ENGINE_CONNECT_${name}`);
  }

  const spoofed = Object.create(ConnectError.prototype) as ConnectError;
  Object.defineProperties(spoofed, {
    name: { value: "ConnectError" },
    code: { value: Code.Unavailable },
    rawMessage: { value: "password=secret" },
    metadata: { value: new Headers() },
    details: { value: [] },
    cause: { value: undefined },
  });
  expect(safeDesktopStartupFailure("CONVEYOR", spoofed)).toBe("LOCAL_ENGINE_STARTUP_CONVEYOR");
  expect(
    safeDesktopStartupFailure("CONVEYOR", {
      name: "ConnectError",
      code: Code.Unavailable,
      message: "password=secret",
    }),
  ).toBe("LOCAL_ENGINE_STARTUP_CONVEYOR");
});

it("returns a bounded ASCII marker even when hostile objects throw on inspection", () => {
  const hostile = new Proxy(
    {},
    {
      getPrototypeOf() {
        throw new Error("secret path /private");
      },
    },
  );

  const result = safeDesktopStartupFailure("WINDOW", hostile);
  expect(result).toBe("LOCAL_ENGINE_STARTUP_WINDOW");
  expect(result).toMatch(/^[A-Z0-9_ ]+$/u);
  expect(result.length).toBeLessThanOrEqual(96);
  expect(STARTUP_DIAGNOSTIC_MARKERS).toContain(result);
});

it("records only a fixed workspace operation, concrete status and elapsed bucket", () => {
  const error = new ConnectError("password=secret /private/path", Code.DeadlineExceeded, {
    "private-token": "secret",
  });
  expect(safeWorkspaceCreateFailure(error, 5_005)).toBe(
    "LOCAL_ENGINE_WORKSPACE_CREATE_FAILED LOCAL_ENGINE_CONNECT_DEADLINE_EXCEEDED LOCAL_ENGINE_WORKSPACE_CREATE_DURATION_5_TO_10_SECONDS",
  );
});

it("bounds workspace creation timings without retaining arbitrary errors", () => {
  const cases = [
    [0, "UNDER_1_SECOND"],
    [999, "UNDER_1_SECOND"],
    [1_000, "1_TO_5_SECONDS"],
    [4_999, "1_TO_5_SECONDS"],
    [5_000, "5_TO_10_SECONDS"],
    [9_999, "5_TO_10_SECONDS"],
    [10_000, "10_SECONDS_OR_MORE"],
    [Number.NaN, "UNKNOWN"],
    [-1, "UNKNOWN"],
    [Number.POSITIVE_INFINITY, "UNKNOWN"],
  ] as const;
  for (const [elapsed, bucket] of cases) {
    const result = safeWorkspaceCreateFailure(new Error("private secret"), elapsed);
    expect(result).toBe(
      `LOCAL_ENGINE_WORKSPACE_CREATE_FAILED LOCAL_ENGINE_WORKSPACE_CREATE_DURATION_${bucket}`,
    );
    expect(result).toMatch(/^[A-Z0-9_ ]+$/u);
    expect(result.length).toBeLessThan(192);
  }
});

it("does not inspect hostile workspace creation errors", () => {
  const hostile = new Proxy(
    {},
    {
      getPrototypeOf() {
        throw new Error("secret");
      },
    },
  );
  expect(safeWorkspaceCreateFailure(hostile, 1)).toBe(
    "LOCAL_ENGINE_WORKSPACE_CREATE_FAILED LOCAL_ENGINE_WORKSPACE_CREATE_DURATION_UNDER_1_SECOND",
  );
});
