// @vitest-environment node

import type { ChildProcess } from "node:child_process";
import { EventEmitter } from "node:events";
import { describe, expect, it, vi } from "vitest";

import {
  createElectronLaunchArguments,
  observeMainExit,
  requestGracefulElectronQuit,
  shouldRecordElectronVideo,
} from "../../tests/e2e/fixtures";
import { findExactCoreProcesses } from "../../tests/e2e/process-check";

interface QueryOptions {
  readonly encoding: "utf8";
  readonly env?: NodeJS.ProcessEnv;
  readonly killSignal: "SIGKILL";
  readonly maxBuffer: number;
  readonly shell: false;
  readonly timeout: number;
  readonly windowsHide: true;
}

type QueryCallback = (
  error: (Error & { code?: number | string; killed?: boolean; signal?: string }) | null,
  stdout: string,
  stderr: string,
) => void;

type QueryRunner = (
  command: string,
  arguments_: readonly string[],
  options: QueryOptions,
  callback: QueryCallback,
) => unknown;

type FindProcesses = (
  corePath: string,
  platform: NodeJS.Platform,
  dependencies: {
    readonly environment?: NodeJS.ProcessEnv;
    readonly execFile: QueryRunner;
    readonly timeoutMs?: number;
  },
) => Promise<readonly { readonly executablePath: string; readonly processId: number }[]>;

const findProcesses = findExactCoreProcesses as FindProcesses;

function resultRunner(
  error: Parameters<QueryCallback>[0],
  stdout = "",
): ReturnType<typeof vi.fn<QueryRunner>> {
  return vi.fn<QueryRunner>((_command, _arguments, _options, callback) => {
    callback(error, stdout, "");
    return {};
  });
}

describe("findExactCoreProcesses", () => {
  it("runs bounded exact macOS pgrep without a shell and parses multiple process IDs", async () => {
    const runner = resultRunner(null, "41\n42\n");
    const corePath = "/Applications/Tammy (Test)/tammy-core";

    await expect(
      findProcesses(corePath, "darwin", {
        execFile: runner,
        timeoutMs: 1_234,
      }),
    ).resolves.toEqual([
      { executablePath: corePath, processId: 41 },
      { executablePath: corePath, processId: 42 },
    ]);
    expect(runner).toHaveBeenCalledWith(
      "/usr/bin/pgrep",
      ["-f", "-x", "^/Applications/Tammy \\(Test\\)/tammy-core( .*)?$"],
      expect.objectContaining({
        encoding: "utf8",
        killSignal: "SIGKILL",
        shell: false,
        timeout: 1_234,
        windowsHide: true,
      }),
      expect.any(Function),
    );
  });

  it("accepts only pgrep exit code 1 as no match", async () => {
    const noMatch = resultRunner(Object.assign(new Error("no match"), { code: 1 }));
    const failed = resultRunner(Object.assign(new Error("failed"), { code: 2 }));

    await expect(
      findProcesses("/Applications/Tammy/tammy-core", "darwin", { execFile: noMatch }),
    ).resolves.toEqual([]);
    await expect(
      findProcesses("/Applications/Tammy/tammy-core", "darwin", { execFile: failed }),
    ).rejects.toThrow("PROCESS_QUERY_FAILED");
  });

  it("waits for callback settlement and reports a killed query as a timeout", async () => {
    let callback: QueryCallback | undefined;
    const runner = vi.fn<QueryRunner>((_command, _arguments, _options, queryCallback) => {
      callback = queryCallback;
      return {};
    });
    const query = findProcesses("/Applications/Tammy/tammy-core", "darwin", {
      execFile: runner,
      timeoutMs: 50,
    });
    let settled = false;
    void query.then(
      () => {
        settled = true;
      },
      () => {
        settled = true;
      },
    );

    await Promise.resolve();
    expect(settled).toBe(false);
    callback?.(
      Object.assign(new Error("timed out"), {
        killed: true,
        signal: "SIGKILL",
      }),
      "",
      "",
    );
    await expect(query).rejects.toThrow("PROCESS_QUERY_TIMEOUT");
  });

  it("uses an absolute PowerShell under a validated SystemRoot with a minimal environment", async () => {
    const corePath = String.raw`C:\Program Files\Tammy\tammy-core.exe`;
    const runner = resultRunner(
      null,
      `${JSON.stringify({
        ProcessId: 17,
        ExecutablePath: String.raw`C:\PROGRAM FILES\TAMMY\TAMMY-CORE.EXE`,
      })}\n${JSON.stringify({
        ProcessId: 18,
        ExecutablePath: corePath,
      })}\n`,
    );

    await expect(
      findProcesses(corePath, "win32", {
        environment: {
          PATH: String.raw`C:\untrusted`,
          SYSTEMROOT: String.raw`C:\Windows`,
          TEMP: String.raw`C:\Temp`,
        },
        execFile: runner,
        timeoutMs: 2_000,
      }),
    ).resolves.toEqual([
      {
        executablePath: String.raw`C:\PROGRAM FILES\TAMMY\TAMMY-CORE.EXE`,
        processId: 17,
      },
      { executablePath: corePath, processId: 18 },
    ]);

    const [command, arguments_, options] = runner.mock.calls[0] ?? [];
    expect(command).toBe(String.raw`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`);
    expect(arguments_).toEqual([
      "-NoLogo",
      "-NoProfile",
      "-NonInteractive",
      "-ExecutionPolicy",
      "Bypass",
      "-Command",
      expect.stringContaining("Get-CimInstance Win32_Process"),
    ]);
    expect(arguments_?.at(-1)).toContain("$env:TAMMY_EXPECTED_CORE");
    expect(options).toEqual(
      expect.objectContaining({
        encoding: "utf8",
        env: {
          SYSTEMROOT: String.raw`C:\Windows`,
          TAMMY_EXPECTED_CORE: corePath,
          TEMP: String.raw`C:\Temp`,
        },
        killSignal: "SIGKILL",
        shell: false,
        timeout: 2_000,
        windowsHide: true,
      }),
    );
  });

  it.each([
    ["relative", "Windows"],
    ["non-canonical", String.raw`C:\Windows\..\Windows`],
    ["unexpected directory", String.raw`C:\Untrusted`],
    ["missing", undefined],
  ])("rejects a %s SystemRoot before spawning PowerShell", async (_name, systemRoot) => {
    const runner = resultRunner(null);

    await expect(
      findProcesses(String.raw`C:\Tammy\tammy-core.exe`, "win32", {
        environment: systemRoot === undefined ? {} : { SYSTEMROOT: systemRoot },
        execFile: runner,
      }),
    ).rejects.toThrow("INVALID_SYSTEM_ROOT");
    expect(runner).not.toHaveBeenCalled();
  });

  it.each([
    ["not JSON"],
    [
      JSON.stringify({
        ExecutablePath: String.raw`C:\Tammy\tammy-core.exe`,
        ProcessId: 4,
        Extra: 1,
      }),
    ],
    [JSON.stringify({ ExecutablePath: String.raw`C:\Other\tammy-core.exe`, ProcessId: 4 })],
  ])("rejects malformed Windows process evidence", async (stdout) => {
    await expect(
      findProcesses(String.raw`C:\Tammy\tammy-core.exe`, "win32", {
        environment: { SYSTEMROOT: String.raw`C:\Windows` },
        execFile: resultRunner(null, `${stdout}\n`),
      }),
    ).rejects.toThrow("INVALID_PROCESS_EVIDENCE");
  });
});

describe("createElectronLaunchArguments", () => {
  it("disables GPU only for hosted macOS evidence", () => {
    const userData = "/private/tmp/tammy-e2e/user-data";

    expect(createElectronLaunchArguments(userData, "darwin-arm64", true)).toEqual([
      `--user-data-dir=${userData}`,
      "--disable-gpu",
    ]);
    expect(createElectronLaunchArguments(userData, "darwin-arm64", false)).toEqual([
      `--user-data-dir=${userData}`,
    ]);
    expect(createElectronLaunchArguments(userData, "win32-x64", true)).toEqual([
      `--user-data-dir=${userData}`,
    ]);
  });

  it("skips video only on hosted macOS", () => {
    expect(shouldRecordElectronVideo("darwin-arm64", true)).toBe(false);
    expect(shouldRecordElectronVideo("darwin-arm64", false)).toBe(true);
    expect(shouldRecordElectronVideo("win32-x64", true)).toBe(true);
  });
});

describe("packaged Electron main-process observation", () => {
  it("confirms OS process exit without waiting for inherited stdio close", async () => {
    const mainProcess = new EventEmitter() as ChildProcess;
    Object.assign(mainProcess, { exitCode: null, signalCode: null });
    const exited = observeMainExit(mainProcess);

    Object.assign(mainProcess, { exitCode: 0 });
    mainProcess.emit("exit", 0, null);

    await expect(exited).resolves.toBeUndefined();
  });

  it("requests native app quit instead of closing the automation transport", async () => {
    const quit = vi.fn();
    const evaluate = vi.fn(async (callback: (electron: { app: { quit(): void } }) => void) => {
      callback({ app: { quit } });
    });

    await requestGracefulElectronQuit({ evaluate } as never);

    expect(quit).toHaveBeenCalledOnce();
  });
});
