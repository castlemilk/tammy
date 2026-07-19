import { spawn } from "node:child_process";
import type { EventEmitter } from "node:events";
import path from "node:path";
import type { Readable, Writable } from "node:stream";

import { type CoreReadiness, parseReadiness } from "../shared/readiness";

const MAX_READINESS_BYTES = 65_536;
const MAX_STDERR_LINE_BYTES = 4_096;
const STOP_TIMEOUT_MS = 3_000;
const FORCE_CONFIRMATION_TIMEOUT_MS = 1_000;
const DEFAULT_READINESS_TIMEOUT_MS = 10_000;
const ALLOWED_ENVIRONMENT_KEYS = ["SYSTEMROOT", "WINDIR", "TEMP", "TMP", "TMPDIR", "LANG"] as const;

export type CoreProcessState = "IDLE" | "STARTING" | "READY" | "STOPPING" | "STOPPED" | "FAILED";

export type CoreProcessErrorCode =
  | "INVALID_BINARY_PATH"
  | "INVALID_STATE"
  | "SPAWN_FAILED"
  | "READINESS_TIMEOUT"
  | "READINESS_OVERFLOW"
  | "READINESS_INVALID"
  | "UNEXPECTED_STDOUT"
  | "EXIT_BEFORE_READY"
  | "CORE_EXITED"
  | "START_ABORTED"
  | "TERMINATION_FAILED";

const ERROR_MESSAGES: Readonly<Record<CoreProcessErrorCode, string>> = {
  INVALID_BINARY_PATH: "Core binary path must be absolute.",
  INVALID_STATE: "Core process cannot be started in its current state.",
  SPAWN_FAILED: "Core process could not be started.",
  READINESS_TIMEOUT: "Core process readiness timed out.",
  READINESS_OVERFLOW: "Core process readiness exceeded its limit.",
  READINESS_INVALID: "Core process sent invalid readiness.",
  UNEXPECTED_STDOUT: "Core process wrote unexpected output.",
  EXIT_BEFORE_READY: "Core process exited before readiness.",
  CORE_EXITED: "Core process exited unexpectedly.",
  START_ABORTED: "Core process start was stopped.",
  TERMINATION_FAILED: "Core process termination was not confirmed.",
};

export class CoreProcessError extends Error {
  public readonly code: CoreProcessErrorCode;

  public constructor(code: CoreProcessErrorCode) {
    super(ERROR_MESSAGES[code]);
    this.name = "CoreProcessError";
    this.code = code;
  }
}

export interface CoreChildProcess extends EventEmitter {
  readonly pid?: number | undefined;
  readonly stdout: Readable;
  readonly stderr: Readable;
  readonly stdin: Writable;
  kill(signal: NodeJS.Signals): boolean;
}

interface CoreSpawnOptions {
  readonly shell: false;
  readonly windowsHide: true;
  readonly stdio: readonly ["pipe", "pipe", "pipe"];
  readonly env: Readonly<Record<string, string>>;
}

export type SpawnCoreProcess = (
  binaryPath: string,
  args: string[],
  options: CoreSpawnOptions,
) => CoreChildProcess;

export interface CoreProcessTimers {
  setTimeout(callback: () => void, delay: number): unknown;
  clearTimeout(timer: unknown): void;
}

export interface CoreProcessLogger {
  warn(message: string): void;
}

export interface CoreProcessClock {
  now(): number;
}

export interface CoreProcessOptions {
  readonly binaryPath: string;
  readonly spawn?: SpawnCoreProcess;
  readonly clock?: CoreProcessClock;
  readonly timers?: CoreProcessTimers;
  readonly logger?: CoreProcessLogger;
  readonly sourceEnvironment?: Readonly<NodeJS.ProcessEnv>;
  readonly readinessTimeoutMs?: number;
}

export interface CoreProcessDiagnostic {
  readonly state: CoreProcessState;
  readonly errorCode?: CoreProcessErrorCode;
}

const productionTimers: CoreProcessTimers = {
  setTimeout: (callback, delay) => globalThis.setTimeout(callback, delay),
  clearTimeout: (timer) => globalThis.clearTimeout(timer as ReturnType<typeof setTimeout>),
};

const productionClock: CoreProcessClock = {
  now: () => Date.now(),
};

const silentLogger: CoreProcessLogger = {
  warn: () => undefined,
};

const productionSpawn: SpawnCoreProcess = (binaryPath, args, options) =>
  spawn(binaryPath, args, {
    shell: options.shell,
    windowsHide: options.windowsHide,
    stdio: ["pipe", "pipe", "pipe"],
    env: options.env,
  });

function allowedEnvironment(source: Readonly<NodeJS.ProcessEnv>): Record<string, string> {
  const allowed: Record<string, string> = {};
  for (const key of ALLOWED_ENVIRONMENT_KEYS) {
    const value = source[key];
    if (value !== undefined) {
      allowed[key] = value;
    }
  }
  return allowed;
}

export class CoreProcess {
  readonly #binaryPath: string;
  readonly #spawn: SpawnCoreProcess;
  readonly #clock: CoreProcessClock;
  readonly #timers: CoreProcessTimers;
  readonly #logger: CoreProcessLogger;
  readonly #environment: Readonly<Record<string, string>>;
  readonly #readinessTimeoutMs: number;

  #state: CoreProcessState = "IDLE";
  #failure: CoreProcessError | undefined;
  #child: CoreChildProcess | undefined;
  #readiness: Readonly<CoreReadiness> | undefined;
  #readinessAccepted = false;
  #readinessBytes = Buffer.alloc(0);
  #stderrBytes = Buffer.alloc(0);
  #droppingStderrLine = false;
  #readinessTimer: unknown;
  #stopTimer: unknown;
  #forceConfirmationTimer: unknown;
  #startPromise: Promise<Readonly<CoreReadiness>> | undefined;
  #resolveStart: ((readiness: Readonly<CoreReadiness>) => void) | undefined;
  #rejectStart: ((error: CoreProcessError) => void) | undefined;
  #stopPromise: Promise<void> | undefined;
  #resolveStop: (() => void) | undefined;
  #rejectStop: ((error: CoreProcessError) => void) | undefined;
  #exitObserved = false;

  public constructor(options: CoreProcessOptions) {
    if (!path.isAbsolute(options.binaryPath)) {
      throw new CoreProcessError("INVALID_BINARY_PATH");
    }
    if (
      options.readinessTimeoutMs !== undefined &&
      (!Number.isFinite(options.readinessTimeoutMs) || options.readinessTimeoutMs < 0)
    ) {
      throw new CoreProcessError("INVALID_STATE");
    }

    this.#binaryPath = options.binaryPath;
    this.#spawn = options.spawn ?? productionSpawn;
    this.#clock = options.clock ?? productionClock;
    this.#timers = options.timers ?? productionTimers;
    this.#logger = options.logger ?? silentLogger;
    this.#environment = Object.freeze(allowedEnvironment(options.sourceEnvironment ?? process.env));
    this.#readinessTimeoutMs = options.readinessTimeoutMs ?? DEFAULT_READINESS_TIMEOUT_MS;
  }

  public start(): Promise<Readonly<CoreReadiness>> {
    if (this.#state === "STARTING" && this.#startPromise) {
      return this.#startPromise;
    }
    if (this.#state === "READY" && this.#readiness) {
      return Promise.resolve(this.#readiness);
    }
    if (this.#state === "FAILED" && this.#failure) {
      return Promise.reject(this.#failure);
    }
    if (this.#state !== "IDLE") {
      return Promise.reject(new CoreProcessError("INVALID_STATE"));
    }

    this.#state = "STARTING";
    this.#readinessAccepted = false;
    this.#exitObserved = false;
    const startPromise = new Promise<Readonly<CoreReadiness>>((resolve, reject) => {
      this.#resolveStart = resolve;
      this.#rejectStart = reject;
    });
    this.#startPromise = startPromise;
    void startPromise.catch(() => undefined);

    try {
      this.#child = this.#spawn(this.#binaryPath, [], {
        shell: false,
        windowsHide: true,
        stdio: ["pipe", "pipe", "pipe"],
        env: this.#environment,
      });
    } catch {
      this.#fail(new CoreProcessError("SPAWN_FAILED"));
      return startPromise;
    }

    this.#readinessTimer = this.#timers.setTimeout(
      () => this.#fail(new CoreProcessError("READINESS_TIMEOUT")),
      this.#readinessTimeoutMs,
    );
    this.#attachListeners(this.#child);
    return startPromise;
  }

  public stop(): Promise<void> {
    if (this.#stopPromise) {
      return this.#stopPromise;
    }
    if (this.#state === "FAILED") {
      if (!this.#child) {
        return Promise.resolve();
      }
      const retryPromise = this.#createStopPromise();
      if (this.#stopTimer === undefined) {
        this.#requestForceTermination("RETRY");
      }
      return retryPromise;
    }
    if (this.#state === "STOPPED") {
      return Promise.resolve();
    }
    if (this.#state === "IDLE") {
      this.#state = "STOPPED";
      return Promise.resolve();
    }

    const wasStarting = this.#state === "STARTING";
    this.#readiness = undefined;
    this.#readinessBytes = Buffer.alloc(0);
    this.#stderrBytes = Buffer.alloc(0);
    this.#droppingStderrLine = false;
    this.#state = "STOPPING";
    this.#clearReadinessTimer();

    if (wasStarting) {
      this.#rejectPendingStart(new CoreProcessError("START_ABORTED"));
    }

    const stopPromise = this.#createStopPromise();

    this.#startGracefulTerminationTimer();

    try {
      this.#child?.stdin.end();
    } catch {
      // A closed stdin already has the desired shutdown semantics.
    }

    return stopPromise;
  }

  public getDiagnostic(): Readonly<CoreProcessDiagnostic> {
    if (this.#state === "FAILED" && this.#failure) {
      return Object.freeze({
        state: this.#state,
        errorCode: this.#failure.code,
      });
    }
    return Object.freeze({ state: this.#state });
  }

  readonly #onStdout = (chunk: Buffer | Uint8Array | string): void => {
    const bytes =
      typeof chunk === "string"
        ? Buffer.from(chunk)
        : Buffer.isBuffer(chunk)
          ? chunk
          : Buffer.from(chunk);

    if (bytes.byteLength === 0) {
      return;
    }
    if (this.#state === "READY") {
      this.#fail(new CoreProcessError("UNEXPECTED_STDOUT"));
      return;
    }
    if (this.#state === "STOPPING") {
      if (this.#readinessAccepted) {
        this.#fail(new CoreProcessError("UNEXPECTED_STDOUT"));
      }
      return;
    }
    if (this.#state !== "STARTING") {
      return;
    }
    if (this.#readinessBytes.byteLength + bytes.byteLength > MAX_READINESS_BYTES) {
      this.#fail(new CoreProcessError("READINESS_OVERFLOW"));
      return;
    }

    this.#readinessBytes = Buffer.concat([this.#readinessBytes, bytes]);
    if (!bytes.includes(0x0a)) {
      return;
    }

    let readiness: Readonly<CoreReadiness>;
    try {
      readiness = parseReadiness(this.#readinessBytes);
    } catch {
      this.#fail(new CoreProcessError("READINESS_INVALID"));
      return;
    }

    this.#readinessBytes = Buffer.alloc(0);
    this.#readiness = readiness;
    this.#readinessAccepted = true;
    this.#state = "READY";
    this.#clearReadinessTimer();
    const resolve = this.#resolveStart;
    this.#resolveStart = undefined;
    this.#rejectStart = undefined;
    this.#startPromise = undefined;
    resolve?.(readiness);
  };

  readonly #onStderr = (chunk: Buffer | Uint8Array | string): void => {
    if (this.#state !== "READY") {
      return;
    }

    let bytes =
      typeof chunk === "string"
        ? Buffer.from(chunk)
        : Buffer.isBuffer(chunk)
          ? chunk
          : Buffer.from(chunk);

    if (this.#droppingStderrLine) {
      const newline = bytes.indexOf(0x0a);
      if (newline === -1) {
        return;
      }
      this.#droppingStderrLine = false;
      bytes = bytes.subarray(newline + 1);
    }

    let offset = 0;
    while (offset < bytes.byteLength) {
      const newline = bytes.indexOf(0x0a, offset);
      const end = newline === -1 ? bytes.byteLength : newline;
      const segment = bytes.subarray(offset, end);
      const available = MAX_STDERR_LINE_BYTES - this.#stderrBytes.byteLength;

      if (segment.byteLength > available) {
        if (available > 0) {
          this.#stderrBytes = Buffer.concat([this.#stderrBytes, segment.subarray(0, available)]);
        }
        this.#logStderr(this.#stderrBytes, true);
        this.#stderrBytes = Buffer.alloc(0);
        if (newline === -1) {
          this.#droppingStderrLine = true;
          return;
        }
      } else {
        this.#stderrBytes = Buffer.concat([this.#stderrBytes, segment]);
        if (newline !== -1) {
          this.#logStderr(this.#stderrBytes, false);
          this.#stderrBytes = Buffer.alloc(0);
        }
      }

      if (newline === -1) {
        return;
      }
      offset = newline + 1;
    }
  };

  readonly #onError = (): void => {
    if (this.#state === "STOPPING" || this.#state === "STOPPED") {
      return;
    }
    const child = this.#child;
    const neverSpawned = this.#state === "STARTING" && child?.pid === undefined;
    this.#fail(new CoreProcessError("SPAWN_FAILED"));
    if (neverSpawned && this.#state === "FAILED" && this.#child === child) {
      this.#finishFailedChildExit();
    }
  };

  readonly #onExit = (): void => {
    this.#exitObserved = true;
    if (this.#state === "STOPPING") {
      this.#finishStop();
      return;
    }
    if (this.#state === "STARTING") {
      this.#fail(new CoreProcessError("EXIT_BEFORE_READY"));
      return;
    }
    if (this.#state === "READY") {
      this.#fail(new CoreProcessError("CORE_EXITED"));
      return;
    }
    if (this.#state === "FAILED") {
      this.#finishFailedChildExit();
    }
  };

  #attachListeners(child: CoreChildProcess): void {
    child.stdout.on("data", this.#onStdout);
    child.stderr.on("data", this.#onStderr);
    child.on("error", this.#onError);
    child.on("exit", this.#onExit);
  }

  #removeStreamListeners(): void {
    const child = this.#child;
    if (!child) {
      return;
    }
    child.stdout.removeListener("data", this.#onStdout);
    child.stderr.removeListener("data", this.#onStderr);
  }

  #removeLifecycleListeners(): void {
    const child = this.#child;
    if (!child) {
      return;
    }
    child.removeListener("error", this.#onError);
    child.removeListener("exit", this.#onExit);
  }

  #removeListeners(): void {
    this.#removeStreamListeners();
    this.#removeLifecycleListeners();
  }

  #logStderr(bytes: Buffer, truncated: boolean): void {
    let message = new TextDecoder("utf-8", { fatal: false }).decode(bytes).replace(/\r$/, "");
    message = message.replace(
      /\b(capability|authorization|token|secret|password|api[_-]?key)\b["']?\s*[:=].*$/gi,
      "$1=[REDACTED]",
    );

    const readiness = this.#readiness;
    if (readiness) {
      message = message.split(readiness.capability).join("[REDACTED]");
      message = message.replace(new RegExp(`\\b${readiness.port}\\b`, "g"), "[REDACTED]");
      for (const certificateLine of readiness.caPem.split("\n")) {
        if (certificateLine.length > 0) {
          message = message.split(certificateLine).join("[REDACTED]");
        }
      }
    }

    if (truncated) {
      message = `${message.slice(0, MAX_STDERR_LINE_BYTES - 12)}[TRUNCATED]`;
    }
    try {
      const now = this.#clock.now();
      const timestamp = Number.isFinite(now) ? Math.trunc(now) : 0;
      const prefix = `[${timestamp}] `;
      this.#logger.warn(`${prefix}${message}`.slice(0, MAX_STDERR_LINE_BYTES));
    } catch {
      // Diagnostic sinks cannot be allowed to destabilize supervision.
    }
  }

  #fail(error: CoreProcessError): void {
    if (this.#state === "FAILED" || this.#state === "STOPPED") {
      return;
    }
    if (this.#state === "STOPPING" && error.code !== "UNEXPECTED_STDOUT") {
      return;
    }
    this.#state = "FAILED";
    this.#failure = error;
    this.#readiness = undefined;
    this.#readinessAccepted = false;
    this.#readinessBytes = Buffer.alloc(0);
    this.#stderrBytes = Buffer.alloc(0);
    this.#droppingStderrLine = false;
    this.#clearReadinessTimer();
    this.#rejectPendingStart(error);
    this.#removeStreamListeners();
    try {
      this.#child?.stdin.end();
    } catch {
      // Native shutdown details are deliberately not surfaced.
    }
    this.#rejectPendingStop(error);
    if (this.#exitObserved || !this.#child) {
      this.#finishFailedChildExit();
      return;
    }
    if (this.#forceConfirmationTimer === undefined) {
      this.#startGracefulTerminationTimer();
    }
  }

  #rejectPendingStart(error: CoreProcessError): void {
    const reject = this.#rejectStart;
    this.#resolveStart = undefined;
    this.#rejectStart = undefined;
    this.#startPromise = undefined;
    reject?.(error);
  }

  #finishStop(): void {
    if (this.#state !== "STOPPING") {
      return;
    }
    this.#clearStopTimer();
    this.#clearForceConfirmationTimer();
    this.#readiness = undefined;
    this.#readinessAccepted = false;
    this.#readinessBytes = Buffer.alloc(0);
    this.#stderrBytes = Buffer.alloc(0);
    this.#droppingStderrLine = false;
    this.#removeListeners();
    this.#child = undefined;
    this.#state = "STOPPED";
    this.#resolvePendingStop();
  }

  #finishFailedChildExit(): void {
    this.#clearStopTimer();
    this.#clearForceConfirmationTimer();
    this.#removeListeners();
    this.#child = undefined;
    this.#readiness = undefined;
    this.#readinessAccepted = false;
    this.#resolvePendingStop();
  }

  #createStopPromise(): Promise<void> {
    const stopPromise = new Promise<void>((resolve, reject) => {
      this.#resolveStop = resolve;
      this.#rejectStop = reject;
    });
    this.#stopPromise = stopPromise;
    void stopPromise.catch(() => undefined);
    return stopPromise;
  }

  #resolvePendingStop(): void {
    const resolve = this.#resolveStop;
    this.#resolveStop = undefined;
    this.#rejectStop = undefined;
    this.#stopPromise = undefined;
    resolve?.();
  }

  #rejectPendingStop(error: CoreProcessError): void {
    const reject = this.#rejectStop;
    this.#resolveStop = undefined;
    this.#rejectStop = undefined;
    this.#stopPromise = undefined;
    reject?.(error);
  }

  #requestForceTermination(mode: "STOP" | "FAILURE" | "RETRY"): void {
    this.#clearForceConfirmationTimer();
    const child = this.#child;
    if (!child) {
      if (mode === "STOP") {
        this.#terminationNotConfirmed(mode);
      } else if (mode === "RETRY") {
        this.#resolvePendingStop();
      }
      return;
    }

    let signalSent = false;
    try {
      signalSent = child.kill("SIGKILL");
    } catch {
      this.#terminationNotConfirmed(mode);
      return;
    }

    if (this.#child !== child || this.#exitObserved) {
      return;
    }
    if (!signalSent) {
      this.#terminationNotConfirmed(mode);
      return;
    }

    let firedSynchronously = false;
    const timer = this.#timers.setTimeout(() => {
      firedSynchronously = true;
      this.#forceConfirmationTimer = undefined;
      if (this.#child === child && !this.#exitObserved) {
        this.#terminationNotConfirmed(mode);
      }
    }, FORCE_CONFIRMATION_TIMEOUT_MS);
    if (firedSynchronously) {
      this.#timers.clearTimeout(timer);
    } else {
      this.#forceConfirmationTimer = timer;
    }
  }

  #startGracefulTerminationTimer(): void {
    if (this.#stopTimer !== undefined) {
      return;
    }
    this.#stopTimer = this.#timers.setTimeout(() => {
      this.#stopTimer = undefined;
      if (this.#exitObserved) {
        return;
      }
      if (this.#state === "STOPPING") {
        this.#requestForceTermination("STOP");
      } else if (this.#state === "FAILED") {
        this.#requestForceTermination(this.#stopPromise ? "RETRY" : "FAILURE");
      }
    }, STOP_TIMEOUT_MS);
  }

  #terminationNotConfirmed(mode: "STOP" | "FAILURE" | "RETRY"): void {
    this.#clearForceConfirmationTimer();
    this.#removeStreamListeners();
    if (mode === "STOP") {
      if (this.#state !== "STOPPING") {
        return;
      }
      const error = new CoreProcessError("TERMINATION_FAILED");
      this.#state = "FAILED";
      this.#failure = error;
      this.#readiness = undefined;
      this.#readinessAccepted = false;
      this.#rejectPendingStop(error);
      return;
    }
    if (mode === "RETRY") {
      this.#rejectPendingStop(new CoreProcessError("TERMINATION_FAILED"));
    }
  }

  #clearReadinessTimer(): void {
    if (this.#readinessTimer !== undefined) {
      this.#timers.clearTimeout(this.#readinessTimer);
      this.#readinessTimer = undefined;
    }
  }

  #clearStopTimer(): void {
    if (this.#stopTimer !== undefined) {
      this.#timers.clearTimeout(this.#stopTimer);
      this.#stopTimer = undefined;
    }
  }

  #clearForceConfirmationTimer(): void {
    if (this.#forceConfirmationTimer !== undefined) {
      this.#timers.clearTimeout(this.#forceConfirmationTimer);
      this.#forceConfirmationTimer = undefined;
    }
  }
}
