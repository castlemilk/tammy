import { spawn } from "node:child_process";
import type { EventEmitter } from "node:events";
import path from "node:path";
import type { Readable, Writable } from "node:stream";

import { type CoreReadiness, parseReadiness } from "../shared/readiness";

const MAX_READINESS_BYTES = 65_536;
const MAX_STDERR_LINE_BYTES = 4_096;
const STOP_TIMEOUT_MS = 3_000;
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
  | "START_ABORTED";

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
  readonly stdout: Readable;
  readonly stderr: Readable;
  readonly stdin: Writable;
  kill(): boolean;
}

interface CoreSpawnOptions {
  readonly shell: false;
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
  #readinessBytes = Buffer.alloc(0);
  #stderrBytes = Buffer.alloc(0);
  #droppingStderrLine = false;
  #readinessTimer: unknown;
  #stopTimer: unknown;
  #startPromise: Promise<Readonly<CoreReadiness>> | undefined;
  #resolveStart: ((readiness: Readonly<CoreReadiness>) => void) | undefined;
  #rejectStart: ((error: CoreProcessError) => void) | undefined;
  #stopPromise: Promise<void> | undefined;
  #resolveStop: (() => void) | undefined;
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
    this.#clock.now();
    const startPromise = new Promise<Readonly<CoreReadiness>>((resolve, reject) => {
      this.#resolveStart = resolve;
      this.#rejectStart = reject;
    });
    this.#startPromise = startPromise;
    void startPromise.catch(() => undefined);

    try {
      this.#child = this.#spawn(this.#binaryPath, [], {
        shell: false,
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
    if (this.#state === "STOPPING" && this.#stopPromise) {
      return this.#stopPromise;
    }
    if (this.#state === "STOPPED" || this.#state === "FAILED") {
      return Promise.resolve();
    }
    if (this.#state === "IDLE") {
      this.#state = "STOPPED";
      return Promise.resolve();
    }

    const wasStarting = this.#state === "STARTING";
    this.#state = "STOPPING";
    this.#clearReadinessTimer();

    if (wasStarting) {
      this.#rejectPendingStart(new CoreProcessError("START_ABORTED"));
    }

    this.#stopPromise = new Promise((resolve) => {
      this.#resolveStop = resolve;
    });

    this.#stopTimer = this.#timers.setTimeout(() => {
      if (!this.#exitObserved) {
        try {
          this.#child?.kill();
        } catch {
          // Stop is bounded even if the native kill operation reports failure.
        }
      }
      this.#finishStop();
    }, STOP_TIMEOUT_MS);

    try {
      this.#child?.stdin.end();
    } catch {
      // A closed stdin already has the desired shutdown semantics.
    }

    return this.#stopPromise;
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
    this.#state = "READY";
    this.#clearReadinessTimer();
    const resolve = this.#resolveStart;
    this.#resolveStart = undefined;
    this.#rejectStart = undefined;
    this.#startPromise = undefined;
    resolve?.(readiness);
  };

  readonly #onStderr = (chunk: Buffer | Uint8Array | string): void => {
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
    this.#fail(new CoreProcessError("SPAWN_FAILED"));
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
    }
  };

  #attachListeners(child: CoreChildProcess): void {
    child.stdout.on("data", this.#onStdout);
    child.stderr.on("data", this.#onStderr);
    child.on("error", this.#onError);
    child.on("exit", this.#onExit);
  }

  #removeListeners(): void {
    const child = this.#child;
    if (!child) {
      return;
    }
    child.stdout.removeListener("data", this.#onStdout);
    child.stderr.removeListener("data", this.#onStderr);
    child.removeListener("error", this.#onError);
    child.removeListener("exit", this.#onExit);
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
      this.#logger.warn(message.slice(0, MAX_STDERR_LINE_BYTES));
    } catch {
      // Diagnostic sinks cannot be allowed to destabilize supervision.
    }
  }

  #fail(error: CoreProcessError): void {
    if (this.#state === "FAILED" || this.#state === "STOPPING" || this.#state === "STOPPED") {
      return;
    }
    this.#state = "FAILED";
    this.#failure = error;
    this.#readiness = undefined;
    this.#readinessBytes = Buffer.alloc(0);
    this.#stderrBytes = Buffer.alloc(0);
    this.#clearReadinessTimer();
    this.#clearStopTimer();
    this.#rejectPendingStart(error);
    this.#removeListeners();
    try {
      this.#child?.stdin.end();
    } catch {
      // Native shutdown details are deliberately not surfaced.
    }
    try {
      this.#child?.kill();
    } catch {
      // Native shutdown details are deliberately not surfaced.
    }
    this.#child = undefined;
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
    this.#readiness = undefined;
    this.#readinessBytes = Buffer.alloc(0);
    this.#stderrBytes = Buffer.alloc(0);
    this.#removeListeners();
    this.#child = undefined;
    this.#state = "STOPPED";
    const resolve = this.#resolveStop;
    this.#resolveStop = undefined;
    resolve?.();
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
}
