import type { ChildProcess } from "node:child_process";
import { constants as osConstants } from "node:os";
import {
  BAS_EXPORT_DIAGNOSTIC_MARKERS,
  CORE_RPC_DIAGNOSTIC_MARKERS,
  STARTUP_DIAGNOSTIC_MARKERS,
  WORKSPACE_CREATE_DIAGNOSTIC_MARKERS,
} from "../../src/main/startup-diagnostics";

const SAFE_STDERR_MARKERS = [
  "LOCAL_ENGINE_UNAVAILABLE",
  "LOCAL_ENGINE_SHUTDOWN_FAILED",
  ...STARTUP_DIAGNOSTIC_MARKERS,
  ...CORE_RPC_DIAGNOSTIC_MARKERS,
  ...WORKSPACE_CREATE_DIAGNOSTIC_MARKERS,
  ...BAS_EXPORT_DIAGNOSTIC_MARKERS,
] as const;

const SAFE_CLOSE_FAILURE_CODES = [
  "ELECTRON_CLOSE_TIMEOUT",
  "ELECTRON_REAP_TIMEOUT",
  "ELECTRON_FORCE_KILL_FAILED",
  "ELECTRON_MAIN_PROCESS_MISSING",
  "ELECTRON_CLOSE_FAILED",
] as const;

type SafeStderrMarker = (typeof SAFE_STDERR_MARKERS)[number];
type SafeCloseFailureCode = (typeof SAFE_CLOSE_FAILURE_CODES)[number];
type SafeCloseFailureCauseCode = Exclude<SafeCloseFailureCode, "ELECTRON_CLOSE_FAILED">;
type SafeCloseFailureSummary = SafeCloseFailureCode | `ELECTRON_CLOSE_FAILED(causes=${string})`;

interface ProcessExitStatus {
  readonly code: number | null;
  readonly signal: string | null;
}

interface ProcessStatusFallback {
  readonly exitCode: ChildProcess["exitCode"];
  readonly signalCode: ChildProcess["signalCode"];
}

function prefixTable(pattern: string): readonly number[] {
  const table = Array<number>(pattern.length).fill(0);
  let prefixLength = 0;
  for (let index = 1; index < pattern.length; index += 1) {
    while (prefixLength > 0 && pattern[index] !== pattern[prefixLength]) {
      prefixLength = table[prefixLength - 1] ?? 0;
    }
    if (pattern[index] === pattern[prefixLength]) prefixLength += 1;
    table[index] = prefixLength;
  }
  return table;
}

const SAFE_STDERR_PREFIX_TABLES = SAFE_STDERR_MARKERS.map(prefixTable);
const KNOWN_SIGNALS = new Set(Object.keys(osConstants.signals));

/** Retains only fixed diagnostic markers and normalized process exit status. */
export class ElectronProcessDiagnostics {
  readonly #matchedMarkers = new Set<SafeStderrMarker>();
  readonly #prefixLengths = SAFE_STDERR_MARKERS.map(() => 0);
  #exitStatus: ProcessExitStatus | undefined;

  public observeStderr(chunk: string | Uint8Array): void {
    for (let index = 0; index < chunk.length; index += 1) {
      const code = typeof chunk === "string" ? chunk.charCodeAt(index) : (chunk[index] ?? -1);
      for (let markerIndex = 0; markerIndex < SAFE_STDERR_MARKERS.length; markerIndex += 1) {
        const marker = SAFE_STDERR_MARKERS[markerIndex];
        const table = SAFE_STDERR_PREFIX_TABLES[markerIndex];
        if (!marker || !table) continue;

        let prefixLength = this.#prefixLengths[markerIndex] ?? 0;
        while (prefixLength > 0 && marker.charCodeAt(prefixLength) !== code) {
          prefixLength = table[prefixLength - 1] ?? 0;
        }
        if (marker.charCodeAt(prefixLength) === code) prefixLength += 1;
        if (prefixLength === marker.length) {
          this.#matchedMarkers.add(marker);
          prefixLength = table[marker.length - 1] ?? 0;
        }
        this.#prefixLengths[markerIndex] = prefixLength;
      }
    }
  }

  public observeExit(code: number | null, signal: NodeJS.Signals | null): void {
    this.#exitStatus = {
      code: normalizeExitCode(code),
      signal: normalizeSignal(signal),
    };
  }

  public formatSummary(fallback?: ProcessStatusFallback): string {
    const exitStatus = this.#exitStatus ?? exitStatusFromFallback(fallback);
    const processStatus = exitStatus
      ? `EXIT(code=${exitStatus.code ?? "unknown"},signal=${exitStatus.signal ?? "none"})`
      : "RUNNING";
    const markers = SAFE_STDERR_MARKERS.filter((marker) => this.#matchedMarkers.has(marker));
    return `electronProcess=${processStatus};stderrMarkers=${markers.join(",") || "none"}`;
  }
}

/** Observe bounded process diagnostics and release listeners after child shutdown. */
export function observeElectronProcessDiagnostics(
  mainProcess: ChildProcess,
  diagnostics: ElectronProcessDiagnostics,
): void {
  const stderr = mainProcess.stderr;
  const onStderrData = (chunk: Buffer | string) => diagnostics.observeStderr(chunk);
  const onExit = (code: number | null, signal: NodeJS.Signals | null) =>
    diagnostics.observeExit(code, signal);
  let cleanedUp = false;
  const cleanup = () => {
    if (cleanedUp) return;
    cleanedUp = true;
    stderr?.removeListener("data", onStderrData);
    mainProcess.removeListener("exit", onExit);
    mainProcess.removeListener("close", cleanup);
  };

  const canReadStderr =
    stderr !== null && !stderr.destroyed && !stderr.readableEnded && !stderr.closed;
  if (canReadStderr) stderr.on("data", onStderrData);
  mainProcess.once("exit", onExit);
  mainProcess.once("close", cleanup);

  // Process status fields expose an exit that happened just before listener
  // registration. Keep reading live stderr until close; if the stream cannot
  // deliver more data, clean up immediately when an exit is already known.
  if (mainProcess.exitCode !== null || mainProcess.signalCode !== null) {
    diagnostics.observeExit(mainProcess.exitCode, mainProcess.signalCode);
    if (!canReadStderr) cleanup();
  }
}

export function safeFirstWindowFailureCode(
  error: unknown,
): "ELECTRON_FIRST_WINDOW_FAILED" | "ELECTRON_TARGET_CLOSED" {
  if (
    error instanceof Error &&
    error.message.includes("Target page, context or browser has been closed")
  ) {
    return "ELECTRON_TARGET_CLOSED";
  }
  return "ELECTRON_FIRST_WINDOW_FAILED";
}

export function safeElectronCloseFailureCode(error: unknown): SafeCloseFailureSummary {
  const code = closeFailureCode(error);
  if (code !== "ELECTRON_CLOSE_FAILED") return code ?? "ELECTRON_CLOSE_FAILED";

  const causes = error instanceof AggregateError ? collectSafeCloseFailureCauses(error) : [];
  return causes.length > 0
    ? `ELECTRON_CLOSE_FAILED(causes=${causes.join(",")})`
    : "ELECTRON_CLOSE_FAILED";
}

function closeFailureCode(error: unknown): SafeCloseFailureCode | undefined {
  if (!(error instanceof Error)) return undefined;
  for (const code of SAFE_CLOSE_FAILURE_CODES) {
    if (
      error.message === code ||
      error.message.startsWith(`${code} `) ||
      error.message.startsWith(`${code}[`)
    ) {
      return code;
    }
  }
  return undefined;
}

function collectSafeCloseFailureCauses(error: AggregateError): SafeCloseFailureCauseCode[] {
  const causes: SafeCloseFailureCauseCode[] = [];
  const visit = (candidate: unknown, depth: number): void => {
    if (depth > 4 || causes.length >= 8 || !(candidate instanceof Error)) return;
    const code = closeFailureCode(candidate);
    if (code && code !== "ELECTRON_CLOSE_FAILED" && !causes.includes(code)) causes.push(code);
    if (candidate instanceof AggregateError) {
      for (const nested of candidate.errors) visit(nested, depth + 1);
    }
    if (candidate.cause !== undefined) visit(candidate.cause, depth + 1);
  };

  for (const nested of error.errors) visit(nested, 1);
  if (error.cause !== undefined) visit(error.cause, 1);
  return causes;
}

function normalizeExitCode(code: number | null): number | null {
  return typeof code === "number" && Number.isSafeInteger(code) && code >= 0 && code <= 255
    ? code
    : null;
}

function normalizeSignal(signal: NodeJS.Signals | null): string | null {
  return typeof signal === "string" && KNOWN_SIGNALS.has(signal) ? signal : null;
}

function exitStatusFromFallback(fallback?: ProcessStatusFallback): ProcessExitStatus | undefined {
  if (
    !fallback ||
    (fallback.exitCode === null && fallback.signalCode === null) ||
    (fallback.exitCode === null && fallback.signalCode === undefined)
  ) {
    return undefined;
  }
  return {
    code: normalizeExitCode(fallback.exitCode),
    signal: normalizeSignal(fallback.signalCode),
  };
}
