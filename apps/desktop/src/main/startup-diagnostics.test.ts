import { Code, ConnectError } from "@connectrpc/connect";
import { expect, it } from "vitest";

import { CoreProcessError, type CoreProcessErrorCode } from "./core-process";
import {
  type DesktopStartupStage,
  STARTUP_DIAGNOSTIC_MARKERS,
  safeDesktopStartupFailure,
} from "./startup-diagnostics";

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
