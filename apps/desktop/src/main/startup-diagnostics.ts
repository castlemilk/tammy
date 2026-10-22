import { isNativeError } from "node:util/types";

import { Code, ConnectError } from "@connectrpc/connect";

import { CoreProcessError, type CoreProcessErrorCode } from "./core-process";

export type DesktopStartupStage =
  | "REGISTER"
  | "READY"
  | "RUNTIME_SECURITY"
  | "CORE_START"
  | "CORE_DIAGNOSTICS"
  | "CONVEYOR"
  | "WINDOW"
  | "WINDOW_SECURITY"
  | "LOAD_WINDOW";

const stageMarkers = {
  REGISTER: "LOCAL_ENGINE_STARTUP_REGISTER",
  READY: "LOCAL_ENGINE_STARTUP_READY",
  RUNTIME_SECURITY: "LOCAL_ENGINE_STARTUP_RUNTIME_SECURITY",
  CORE_START: "LOCAL_ENGINE_STARTUP_CORE_START",
  CORE_DIAGNOSTICS: "LOCAL_ENGINE_STARTUP_CORE_DIAGNOSTICS",
  CONVEYOR: "LOCAL_ENGINE_STARTUP_CONVEYOR",
  WINDOW: "LOCAL_ENGINE_STARTUP_WINDOW",
  WINDOW_SECURITY: "LOCAL_ENGINE_STARTUP_WINDOW_SECURITY",
  LOAD_WINDOW: "LOCAL_ENGINE_STARTUP_LOAD_WINDOW",
} as const satisfies Readonly<Record<DesktopStartupStage, string>>;

const coreErrorMarkers = {
  INVALID_BINARY_PATH: "LOCAL_ENGINE_CORE_INVALID_BINARY_PATH",
  INVALID_STATE: "LOCAL_ENGINE_CORE_INVALID_STATE",
  SPAWN_FAILED: "LOCAL_ENGINE_CORE_SPAWN_FAILED",
  READINESS_TIMEOUT: "LOCAL_ENGINE_CORE_READINESS_TIMEOUT",
  READINESS_OVERFLOW: "LOCAL_ENGINE_CORE_READINESS_OVERFLOW",
  READINESS_INVALID: "LOCAL_ENGINE_CORE_READINESS_INVALID",
  UNEXPECTED_STDOUT: "LOCAL_ENGINE_CORE_UNEXPECTED_STDOUT",
  EXIT_BEFORE_READY: "LOCAL_ENGINE_CORE_EXIT_BEFORE_READY",
  CORE_EXITED: "LOCAL_ENGINE_CORE_CORE_EXITED",
  START_ABORTED: "LOCAL_ENGINE_CORE_START_ABORTED",
  TERMINATION_FAILED: "LOCAL_ENGINE_CORE_TERMINATION_FAILED",
} as const satisfies Readonly<Record<CoreProcessErrorCode, string>>;

const connectCodeMarkers = {
  [Code.Canceled]: "LOCAL_ENGINE_CONNECT_CANCELED",
  [Code.Unknown]: "LOCAL_ENGINE_CONNECT_UNKNOWN",
  [Code.InvalidArgument]: "LOCAL_ENGINE_CONNECT_INVALID_ARGUMENT",
  [Code.DeadlineExceeded]: "LOCAL_ENGINE_CONNECT_DEADLINE_EXCEEDED",
  [Code.NotFound]: "LOCAL_ENGINE_CONNECT_NOT_FOUND",
  [Code.AlreadyExists]: "LOCAL_ENGINE_CONNECT_ALREADY_EXISTS",
  [Code.PermissionDenied]: "LOCAL_ENGINE_CONNECT_PERMISSION_DENIED",
  [Code.ResourceExhausted]: "LOCAL_ENGINE_CONNECT_RESOURCE_EXHAUSTED",
  [Code.FailedPrecondition]: "LOCAL_ENGINE_CONNECT_FAILED_PRECONDITION",
  [Code.Aborted]: "LOCAL_ENGINE_CONNECT_ABORTED",
  [Code.OutOfRange]: "LOCAL_ENGINE_CONNECT_OUT_OF_RANGE",
  [Code.Unauthenticated]: "LOCAL_ENGINE_CONNECT_UNAUTHENTICATED",
  [Code.Internal]: "LOCAL_ENGINE_CONNECT_INTERNAL",
  [Code.Unavailable]: "LOCAL_ENGINE_CONNECT_UNAVAILABLE",
  [Code.DataLoss]: "LOCAL_ENGINE_CONNECT_DATA_LOSS",
  [Code.Unimplemented]: "LOCAL_ENGINE_CONNECT_UNIMPLEMENTED",
} as const satisfies Readonly<Record<Code, string>>;

export const STARTUP_DIAGNOSTIC_MARKERS = Object.freeze([
  ...Object.values(stageMarkers),
  ...Object.values(coreErrorMarkers),
  ...Object.values(connectCodeMarkers),
]);

const workspaceDurationMarkers = {
  underOne: "LOCAL_ENGINE_WORKSPACE_CREATE_DURATION_UNDER_1_SECOND",
  oneToFive: "LOCAL_ENGINE_WORKSPACE_CREATE_DURATION_1_TO_5_SECONDS",
  fiveToTen: "LOCAL_ENGINE_WORKSPACE_CREATE_DURATION_5_TO_10_SECONDS",
  tenOrMore: "LOCAL_ENGINE_WORKSPACE_CREATE_DURATION_10_SECONDS_OR_MORE",
  unknown: "LOCAL_ENGINE_WORKSPACE_CREATE_DURATION_UNKNOWN",
} as const;

export const WORKSPACE_CREATE_DIAGNOSTIC_MARKERS = Object.freeze([
  "LOCAL_ENGINE_WORKSPACE_CREATE_FAILED",
  ...Object.values(workspaceDurationMarkers),
]);

const basExportStageMarkers = {
  HISTORY_READ: "LOCAL_ENGINE_BAS_EXPORT_HISTORY_READ",
  FROZEN_READ: "LOCAL_ENGINE_BAS_EXPORT_FROZEN_READ",
  CALCULATION_READ: "LOCAL_ENGINE_BAS_EXPORT_CALCULATION_READ",
  PREPARATION_READ: "LOCAL_ENGINE_BAS_EXPORT_PREPARATION_READ",
  SNAPSHOT_VALIDATION: "LOCAL_ENGINE_BAS_EXPORT_SNAPSHOT_VALIDATION",
  EVENT_APPEND_STARTED: "LOCAL_ENGINE_BAS_EXPORT_EVENT_APPEND_STARTED",
  EVENT_APPEND_FINISHED: "LOCAL_ENGINE_BAS_EXPORT_EVENT_APPEND_FINISHED",
  EXPORT_HANDLER: "LOCAL_ENGINE_BAS_EXPORT_EXPORT_HANDLER",
} as const;

export type BasExportDiagnosticStage = keyof typeof basExportStageMarkers;
export const BAS_EXPORT_DIAGNOSTIC_MARKERS = Object.freeze(Object.values(basExportStageMarkers));

const coreRpcOperationMarkers = {
  INGEST_DOCUMENT: "LOCAL_ENGINE_CORE_RPC_INGEST_DOCUMENT_FAILED",
  LIST_DOCUMENTS: "LOCAL_ENGINE_CORE_RPC_LIST_DOCUMENTS_FAILED",
  LIST_ACCOUNTS: "LOCAL_ENGINE_CORE_RPC_LIST_ACCOUNTS_FAILED",
  CREATE_BAS_DRAFT: "LOCAL_ENGINE_CORE_RPC_CREATE_BAS_DRAFT_FAILED",
  UNLOCK_WORKSPACE: "LOCAL_ENGINE_CORE_RPC_UNLOCK_WORKSPACE_FAILED",
  SIGN_IN: "LOCAL_ENGINE_CORE_RPC_SIGN_IN_FAILED",
} as const;

export type CoreRpcDiagnosticOperation = keyof typeof coreRpcOperationMarkers;
export const CORE_RPC_DIAGNOSTIC_MARKERS = Object.freeze([
  "LOCAL_ENGINE_CORE_RPC_FAILED",
  ...Object.values(coreRpcOperationMarkers),
]);

function ownDataValue(value: object, name: string): unknown {
  const descriptor = Object.getOwnPropertyDescriptor(value, name);
  return descriptor && "value" in descriptor ? descriptor.value : undefined;
}

function coreProcessMarker(error: unknown): string | undefined {
  try {
    if (
      !isNativeError(error) ||
      !(error instanceof CoreProcessError) ||
      Object.getPrototypeOf(error) !== CoreProcessError.prototype
    )
      return undefined;
    if (ownDataValue(error, "name") !== "CoreProcessError") return undefined;
    const code = ownDataValue(error, "code");
    if (typeof code !== "string" || !Object.hasOwn(coreErrorMarkers, code)) return undefined;
    return coreErrorMarkers[code as CoreProcessErrorCode];
  } catch {
    return undefined;
  }
}

function connectMarker(error: unknown): string | undefined {
  try {
    if (
      (typeof error !== "object" && typeof error !== "function") ||
      error === null ||
      !isNativeError(error) ||
      Object.getPrototypeOf(error) !== ConnectError.prototype
    )
      return undefined;

    const name = ownDataValue(error, "name");
    const code = ownDataValue(error, "code");
    const rawMessage = ownDataValue(error, "rawMessage");
    const metadata = ownDataValue(error, "metadata");
    const details = ownDataValue(error, "details");
    if (
      name !== "ConnectError" ||
      typeof code !== "number" ||
      !Number.isInteger(code) ||
      typeof rawMessage !== "string" ||
      typeof Headers === "undefined" ||
      !(metadata instanceof Headers) ||
      !Array.isArray(details) ||
      !Object.hasOwn(error, "cause") ||
      !Object.hasOwn(connectCodeMarkers, code)
    )
      return undefined;

    return connectCodeMarkers[code as Code];
  } catch {
    return undefined;
  }
}

export function safeDesktopStartupFailure(stage: DesktopStartupStage, error: unknown): string {
  const stageMarker =
    typeof stage === "string" && Object.hasOwn(stageMarkers, stage)
      ? stageMarkers[stage as DesktopStartupStage]
      : stageMarkers.REGISTER;
  const codeMarker = coreProcessMarker(error) ?? connectMarker(error);
  return codeMarker ? `${stageMarker} ${codeMarker}` : stageMarker;
}

export function safeWorkspaceCreateFailure(error: unknown, elapsedMs: number): string {
  const duration =
    !Number.isFinite(elapsedMs) || elapsedMs < 0
      ? workspaceDurationMarkers.unknown
      : elapsedMs < 1_000
        ? workspaceDurationMarkers.underOne
        : elapsedMs < 5_000
          ? workspaceDurationMarkers.oneToFive
          : elapsedMs < 10_000
            ? workspaceDurationMarkers.fiveToTen
            : workspaceDurationMarkers.tenOrMore;
  const code = connectMarker(error);
  return ["LOCAL_ENGINE_WORKSPACE_CREATE_FAILED", ...(code ? [code] : []), duration].join(" ");
}

export function safeBasExportFailure(stage: BasExportDiagnosticStage, error: unknown): string {
  const marker =
    typeof stage === "string" && Object.hasOwn(basExportStageMarkers, stage)
      ? basExportStageMarkers[stage]
      : basExportStageMarkers.EXPORT_HANDLER;
  const code = connectMarker(error);
  return code ? `${marker} ${code}` : marker;
}

export function safeCoreRpcFailure(operation: CoreRpcDiagnosticOperation, error: unknown): string {
  const marker =
    typeof operation === "string" && Object.hasOwn(coreRpcOperationMarkers, operation)
      ? coreRpcOperationMarkers[operation]
      : "LOCAL_ENGINE_CORE_RPC_FAILED";
  const code = connectMarker(error);
  return code ? `${marker} ${code}` : marker;
}
