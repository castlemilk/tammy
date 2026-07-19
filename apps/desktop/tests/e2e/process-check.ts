import { execFile } from "node:child_process";
import path from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

export interface CoreProcessMatch {
  readonly executablePath: string;
  readonly processId: number;
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

function requireCanonicalCorePath(corePath: string): string {
  if (
    !path.isAbsolute(corePath) ||
    path.normalize(corePath) !== corePath ||
    !["tammy-core", "tammy-core.exe"].includes(path.basename(corePath))
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

async function queryMacOS(corePath: string): Promise<readonly CoreProcessMatch[]> {
  try {
    const { stdout } = await execFileAsync(
      "/usr/bin/pgrep",
      ["-f", "-x", escapeRegularExpression(corePath)],
      { encoding: "utf8", maxBuffer: 64 * 1024 },
    );
    return stdout
      .split(/\r?\n/u)
      .filter((line) => line.length > 0)
      .map((line) => ({ executablePath: corePath, processId: parseProcessId(line) }));
  } catch (error) {
    if (typeof error === "object" && error !== null && "code" in error && error.code === 1) {
      return [];
    }
    throw new Error("PROCESS_QUERY_FAILED");
  }
}

function windowsEnvironment(corePath: string): NodeJS.ProcessEnv {
  const environment: NodeJS.ProcessEnv = { TAMMY_EXPECTED_CORE: corePath };
  for (const name of ["SYSTEMROOT", "WINDIR", "TEMP", "TMP"] as const) {
    const value = process.env[name];
    if (value) environment[name] = value;
  }
  return environment;
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

async function queryWindows(corePath: string): Promise<readonly CoreProcessMatch[]> {
  try {
    const { stdout } = await execFileAsync("powershell.exe", [...WINDOWS_ARGUMENTS], {
      encoding: "utf8",
      env: windowsEnvironment(corePath),
      maxBuffer: 64 * 1024,
      windowsHide: true,
    });
    return stdout
      .split(/\r?\n/u)
      .filter((line) => line.length > 0)
      .map((line) => parseWindowsLine(line, corePath));
  } catch (error) {
    if (error instanceof Error && error.message === "INVALID_PROCESS_EVIDENCE") throw error;
    throw new Error("PROCESS_QUERY_FAILED");
  }
}

export async function findExactCoreProcesses(
  corePath: string,
  platform: NodeJS.Platform = process.platform,
): Promise<readonly CoreProcessMatch[]> {
  const expected = requireCanonicalCorePath(corePath);
  if (platform === "darwin") return queryMacOS(expected);
  if (platform === "win32") return queryWindows(expected);
  throw new Error("UNSUPPORTED_PROCESS_CHECK_PLATFORM");
}
