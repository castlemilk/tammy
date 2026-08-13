import { spawn } from "node:child_process";

export const MAX_HARNESS_OUTPUT_BYTES = 256 * 1024;
const TRUNCATION_MARKER = "\n[output truncated]";

export interface HarnessLaunchCommand {
  readonly args: readonly string[];
  readonly command: string;
  readonly detached: boolean;
}

export interface HarnessReadable {
  on(event: "data", listener: (chunk: Buffer | string) => void): this;
}

export interface HarnessChildProcess {
  readonly pid?: number | undefined;
  readonly stderr: HarnessReadable;
  readonly stdout: HarnessReadable;
  kill(signal: NodeJS.Signals): boolean;
  once(
    event: "close",
    listener: (code: number | null, signal: NodeJS.Signals | null) => void,
  ): this;
  once(event: "error", listener: (error: Error) => void): this;
  once(event: "exit", listener: (code: number | null, signal: NodeJS.Signals | null) => void): this;
}

export interface HarnessProcessTimers {
  clearTimeout(timer: unknown): void;
  setTimeout(callback: () => void, delay: number): unknown;
}

export interface HarnessSpawnOptions {
  readonly detached: boolean;
  readonly env: Readonly<NodeJS.ProcessEnv>;
  readonly shell: false;
  readonly stdio: readonly ["ignore", "pipe", "pipe"];
  readonly windowsHide: true;
}

export type SpawnHarnessProcess = (
  command: string,
  args: readonly string[],
  options: HarnessSpawnOptions,
) => HarnessChildProcess;

export interface RunHarnessProcessOptions {
  readonly closeConfirmationTimeoutMs: number;
  readonly electronArguments: readonly string[];
  readonly environment: Readonly<NodeJS.ProcessEnv>;
  readonly executable: string;
  readonly platform: NodeJS.Platform;
  readonly spawnProcess: SpawnHarnessProcess;
  readonly terminateProcess: (child: HarnessChildProcess, platform: NodeJS.Platform) => void;
  readonly timeoutMs: number;
  readonly timers: HarnessProcessTimers;
}

export interface HarnessProcessResult {
  readonly stderr: string;
  readonly stdout: string;
}

export class HarnessProcessError extends Error {
  public readonly processClosed: boolean;

  public constructor(message: string, processClosed: boolean) {
    super(message);
    this.name = "HarnessProcessError";
    this.processClosed = processClosed;
  }
}

export const productionHarnessTimers: HarnessProcessTimers = {
  clearTimeout: (timer) => globalThis.clearTimeout(timer as ReturnType<typeof setTimeout>),
  setTimeout: (callback, delay) => globalThis.setTimeout(callback, delay),
};

export const spawnHarnessProcess: SpawnHarnessProcess = (command, args, options) =>
  spawn(command, [...args], {
    detached: options.detached,
    env: options.env,
    shell: options.shell,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: options.windowsHide,
  }) as HarnessChildProcess;

class BoundedOutput {
  readonly #chunks: Buffer[] = [];
  #length = 0;
  #truncated = false;

  append(chunk: Buffer | string): void {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    const remaining = MAX_HARNESS_OUTPUT_BYTES - this.#length;
    if (remaining <= 0) {
      this.#truncated = true;
      return;
    }
    const retained = Buffer.from(bytes.subarray(0, remaining));
    this.#chunks.push(retained);
    this.#length += retained.byteLength;
    if (retained.byteLength < bytes.byteLength) {
      this.#truncated = true;
    }
  }

  toString(): string {
    const output = Buffer.concat(this.#chunks, this.#length).toString("utf8");
    return this.#truncated ? `${output}${TRUNCATION_MARKER}` : output;
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Unknown process error.";
}

function diagnostics(stdout: BoundedOutput, stderr: BoundedOutput): string {
  return ` stdout=${JSON.stringify(stdout.toString())} stderr=${JSON.stringify(stderr.toString())}`;
}

export function selectHarnessLaunch(options: {
  readonly display: string | undefined;
  readonly electronArguments: readonly string[];
  readonly executable: string;
  readonly platform: NodeJS.Platform;
}): HarnessLaunchCommand {
  const detached = options.platform !== "win32";
  const needsVirtualDisplay =
    options.platform === "linux" &&
    (options.display === undefined || options.display.trim() === "");
  return needsVirtualDisplay
    ? {
        args: ["-a", options.executable, ...options.electronArguments],
        command: "xvfb-run",
        detached,
      }
    : {
        args: [...options.electronArguments],
        command: options.executable,
        detached,
      };
}

export function terminateHarnessProcess(
  child: HarnessChildProcess,
  platform: NodeJS.Platform,
  killProcess: (pid: number, signal: NodeJS.Signals) => boolean = process.kill,
): void {
  if (platform === "win32") {
    if (!child.kill("SIGKILL")) {
      throw new Error("Electron ASAR harness force termination was not requested.");
    }
    return;
  }

  if (child.pid === undefined || !Number.isInteger(child.pid) || child.pid <= 0) {
    throw new Error("Electron ASAR harness process group identifier is unavailable.");
  }
  killProcess(-child.pid, "SIGKILL");
}

export async function runHarnessProcess(
  options: RunHarnessProcessOptions,
): Promise<HarnessProcessResult> {
  const launch = selectHarnessLaunch({
    display: options.environment.DISPLAY,
    electronArguments: options.electronArguments,
    executable: options.executable,
    platform: options.platform,
  });
  let child: HarnessChildProcess;
  try {
    child = options.spawnProcess(launch.command, launch.args, {
      detached: launch.detached,
      env: options.environment,
      shell: false,
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });
  } catch (error) {
    throw new HarnessProcessError(
      `Electron ASAR harness could not be spawned: ${errorMessage(error)}`,
      true,
    );
  }

  return new Promise<HarnessProcessResult>((resolve, reject) => {
    const stdout = new BoundedOutput();
    const stderr = new BoundedOutput();
    let settled = false;
    let timedOut = false;
    let spawnError: Error | undefined;
    let terminationError: unknown;
    let closeConfirmationTimer: unknown;
    let executionTimer: unknown;

    const clearTimers = (): void => {
      if (executionTimer !== undefined) {
        options.timers.clearTimeout(executionTimer);
        executionTimer = undefined;
      }
      if (closeConfirmationTimer !== undefined) {
        options.timers.clearTimeout(closeConfirmationTimer);
        closeConfirmationTimer = undefined;
      }
    };

    const rejectOnce = (message: string, processClosed: boolean): void => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimers();
      reject(new HarnessProcessError(message, processClosed));
    };

    const resolveOnce = (result: HarnessProcessResult): void => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimers();
      resolve(result);
    };

    const startCloseConfirmation = (reason: "spawn" | "timeout"): void => {
      if (closeConfirmationTimer !== undefined || settled) {
        return;
      }
      closeConfirmationTimer = options.timers.setTimeout(() => {
        const reasonMessage =
          reason === "timeout"
            ? `Electron ASAR harness timed out after ${options.timeoutMs}ms and termination was not confirmed within ${options.closeConfirmationTimeoutMs}ms.`
            : `Electron ASAR harness spawn failed and close was not confirmed within ${options.closeConfirmationTimeoutMs}ms: ${errorMessage(spawnError)}.`;
        const terminationMessage =
          terminationError === undefined
            ? ""
            : ` Force termination also failed: ${errorMessage(terminationError)}.`;
        rejectOnce(`${reasonMessage}${terminationMessage}`, false);
      }, options.closeConfirmationTimeoutMs);
    };

    child.stdout.on("data", (chunk) => stdout.append(chunk));
    child.stderr.on("data", (chunk) => stderr.append(chunk));
    child.once("error", (error) => {
      if (settled) {
        return;
      }
      spawnError ??= error;
      if (!timedOut) {
        if (executionTimer !== undefined) {
          options.timers.clearTimeout(executionTimer);
          executionTimer = undefined;
        }
        startCloseConfirmation("spawn");
      }
    });
    child.once("close", (code, signal) => {
      if (settled) {
        return;
      }
      const output = {
        stderr: stderr.toString(),
        stdout: stdout.toString(),
      };
      if (spawnError) {
        const virtualDisplayHint =
          launch.command === "xvfb-run" && "code" in spawnError && spawnError.code === "ENOENT"
            ? " xvfb-run is unavailable; install Xvfb for headless Linux Electron tests."
            : "";
        rejectOnce(
          `Electron ASAR harness failed to launch: ${errorMessage(spawnError)}.${virtualDisplayHint}${diagnostics(stdout, stderr)}`,
          true,
        );
        return;
      }
      if (timedOut) {
        const terminationMessage =
          terminationError === undefined
            ? ""
            : ` Force termination failed: ${errorMessage(terminationError)}.`;
        rejectOnce(
          `Electron ASAR harness timed out after ${options.timeoutMs}ms.${terminationMessage}${diagnostics(stdout, stderr)}`,
          true,
        );
        return;
      }
      if (code !== 0) {
        rejectOnce(
          `Electron ASAR harness failed (${code ?? signal ?? "unknown"}).${diagnostics(stdout, stderr)}`,
          true,
        );
        return;
      }
      resolveOnce(output);
    });

    executionTimer = options.timers.setTimeout(() => {
      if (settled) {
        return;
      }
      timedOut = true;
      startCloseConfirmation("timeout");
      try {
        options.terminateProcess(child, options.platform);
      } catch (error) {
        terminationError = error;
      }
    }, options.timeoutMs);
  });
}
