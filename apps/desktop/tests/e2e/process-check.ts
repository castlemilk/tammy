import { type ExecFileException, execFile as nodeExecFile } from "node:child_process";
import path from "node:path";

const PROCESS_QUERY_TIMEOUT_MS = 5_000;

export interface CoreProcessMatch {
  readonly executablePath: string;
  readonly processId: number;
}

interface ProcessQueryOptions {
  readonly encoding: "utf8";
  readonly env?: NodeJS.ProcessEnv;
  readonly killSignal: "SIGKILL";
  readonly maxBuffer: number;
  readonly shell: false;
  readonly timeout: number;
  readonly windowsHide: true;
}

type ProcessQueryCallback = (
  error: ExecFileException | null,
  stdout: string,
  stderr: string,
) => void;

export type ProcessQueryExecFile = (
  command: string,
  arguments_: readonly string[],
  options: ProcessQueryOptions,
  callback: ProcessQueryCallback,
) => unknown;

export interface ProcessQueryDependencies {
  readonly environment?: NodeJS.ProcessEnv;
  readonly execFile?: ProcessQueryExecFile;
  readonly timeoutMs?: number;
}

const WINDOWS_PROCESS_QUERY = `
$ErrorActionPreference = "Stop"
$expected = [System.IO.Path]::GetFullPath($env:TAMMY_EXPECTED_CORE)
Get-CimInstance Win32_Process |
  Where-Object {
    $_.ExecutablePath -and
    [System.StringComparer]::OrdinalIgnoreCase.Equals(
      [System.IO.Path]::GetFullPath($_.ExecutablePath),
      $expected
    )
  } |
  ForEach-Object {
    [PSCustomObject]@{
      ProcessId = [int]$_.ProcessId
      ExecutablePath = [string]$_.ExecutablePath
    } | ConvertTo-Json -Compress
  }
`.trim();

const WINDOWS_ARGUMENTS = Object.freeze([
  "-NoLogo",
  "-NoProfile",
  "-NonInteractive",
  "-ExecutionPolicy",
  "Bypass",
  "-Command",
  WINDOWS_PROCESS_QUERY,
]);

const productionExecFile: ProcessQueryExecFile = (command, arguments_, options, callback) =>
  nodeExecFile(command, [...arguments_], options, callback);

function requireCanonicalCorePath(corePath: string, platform: NodeJS.Platform): string {
  const platformPath = platform === "win32" ? path.win32 : path.posix;
  const executable = platform === "win32" ? "tammy-core.exe" : "tammy-core";
  if (
    !platformPath.isAbsolute(corePath) ||
    platformPath.normalize(corePath) !== corePath ||
    platformPath.basename(corePath) !== executable
  ) {
    throw new Error("INVALID_EXPECTED_CORE_PATH");
  }
  return corePath;
}

function escapeRegularExpression(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function parseProcessId(value: string): number {
  if (!/^[1-9][0-9]*$/.test(value)) throw new Error("INVALID_PROCESS_EVIDENCE");
  const processId = Number(value);
  if (!Number.isSafeInteger(processId)) throw new Error("INVALID_PROCESS_EVIDENCE");
  return processId;
}

function runBoundedQuery(
  command: string,
  arguments_: readonly string[],
  {
    environment,
    execFile = productionExecFile,
    timeoutMs = PROCESS_QUERY_TIMEOUT_MS,
  }: ProcessQueryDependencies,
): Promise<string> {
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs <= 0) {
    throw new Error("INVALID_PROCESS_QUERY_TIMEOUT");
  }
  return new Promise((resolve, reject) => {
    try {
      execFile(
        command,
        arguments_,
        {
          encoding: "utf8",
          killSignal: "SIGKILL",
          maxBuffer: 64 * 1024,
          shell: false,
          timeout: timeoutMs,
          windowsHide: true,
          ...(environment ? { env: environment } : {}),
        },
        (error, stdout) => {
          if (error) {
            if (error.killed === true && error.signal === "SIGKILL") {
              reject(new Error("PROCESS_QUERY_TIMEOUT"));
            } else {
              reject(error);
            }
            return;
          }
          resolve(stdout);
        },
      );
    } catch (error) {
      reject(error);
    }
  });
}

function parseLines(stdout: string): string[] {
  return stdout.split(/\r?\n/u).filter((line) => line.length > 0);
}

async function queryMacOS(
  corePath: string,
  dependencies: ProcessQueryDependencies,
): Promise<readonly CoreProcessMatch[]> {
  let stdout: string;
  try {
    stdout = await runBoundedQuery(
      "/usr/bin/pgrep",
      ["-f", "-x", escapeRegularExpression(corePath)],
      dependencies,
    );
  } catch (error) {
    if (error instanceof Error && error.message === "PROCESS_QUERY_TIMEOUT") throw error;
    if (typeof error === "object" && error !== null && "code" in error && error.code === 1) {
      return [];
    }
    throw new Error("PROCESS_QUERY_FAILED");
  }
  return parseLines(stdout).map((line) => ({
    executablePath: corePath,
    processId: parseProcessId(line),
  }));
}

function requireSystemRoot(sourceEnvironment: NodeJS.ProcessEnv): string {
  const upper = sourceEnvironment.SYSTEMROOT;
  const mixed = sourceEnvironment.SystemRoot;
  if (
    upper &&
    mixed &&
    path.win32.normalize(upper).toLowerCase() !== path.win32.normalize(mixed).toLowerCase()
  ) {
    throw new Error("INVALID_SYSTEM_ROOT");
  }
  const systemRoot = upper ?? mixed;
  if (
    typeof systemRoot !== "string" ||
    !path.win32.isAbsolute(systemRoot) ||
    path.win32.normalize(systemRoot) !== systemRoot ||
    !/^[A-Za-z]:\\Windows$/iu.test(systemRoot)
  ) {
    throw new Error("INVALID_SYSTEM_ROOT");
  }
  return systemRoot;
}

function windowsCommandAndEnvironment(
  corePath: string,
  sourceEnvironment: NodeJS.ProcessEnv,
): { readonly command: string; readonly environment: NodeJS.ProcessEnv } {
  const systemRoot = requireSystemRoot(sourceEnvironment);
  const command = path.win32.join(
    systemRoot,
    "System32",
    "WindowsPowerShell",
    "v1.0",
    "powershell.exe",
  );
  if (!path.win32.isAbsolute(command) || path.win32.normalize(command) !== command) {
    throw new Error("INVALID_SYSTEM_ROOT");
  }
  const environment: NodeJS.ProcessEnv = {
    SYSTEMROOT: systemRoot,
    TAMMY_EXPECTED_CORE: corePath,
  };
  for (const name of ["TEMP", "TMP"] as const) {
    const value = sourceEnvironment[name];
    if (value) environment[name] = value;
  }
  return { command, environment };
}

function parseWindowsLine(line: string, corePath: string): CoreProcessMatch {
  let value: unknown;
  try {
    value = JSON.parse(line);
  } catch {
    throw new Error("INVALID_PROCESS_EVIDENCE");
  }
  if (
    value === null ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    Object.keys(value).sort().join(",") !== "ExecutablePath,ProcessId"
  ) {
    throw new Error("INVALID_PROCESS_EVIDENCE");
  }
  const record = value as Record<string, unknown>;
  if (
    typeof record.ExecutablePath !== "string" ||
    typeof record.ProcessId !== "number" ||
    !Number.isSafeInteger(record.ProcessId) ||
    record.ProcessId <= 0 ||
    path.win32.normalize(record.ExecutablePath).toLowerCase() !==
      path.win32.normalize(corePath).toLowerCase()
  ) {
    throw new Error("INVALID_PROCESS_EVIDENCE");
  }
  return { executablePath: record.ExecutablePath, processId: record.ProcessId };
}

async function queryWindows(
  corePath: string,
  dependencies: ProcessQueryDependencies,
): Promise<readonly CoreProcessMatch[]> {
  const { command, environment } = windowsCommandAndEnvironment(
    corePath,
    dependencies.environment ?? process.env,
  );
  let stdout: string;
  try {
    stdout = await runBoundedQuery(command, WINDOWS_ARGUMENTS, {
      ...dependencies,
      environment,
    });
  } catch (error) {
    if (error instanceof Error && error.message === "PROCESS_QUERY_TIMEOUT") throw error;
    throw new Error("PROCESS_QUERY_FAILED");
  }
  return parseLines(stdout).map((line) => parseWindowsLine(line, corePath));
}

export async function findExactCoreProcesses(
  corePath: string,
  platform: NodeJS.Platform = process.platform,
  dependencies: ProcessQueryDependencies = {},
): Promise<readonly CoreProcessMatch[]> {
  const expected = requireCanonicalCorePath(corePath, platform);
  if (platform === "darwin") return queryMacOS(expected, dependencies);
  if (platform === "win32") return queryWindows(expected, dependencies);
  throw new Error("UNSUPPORTED_PROCESS_CHECK_PLATFORM");
}
