import { type ExecFileException, execFile as nodeExecFile } from "node:child_process";
import { createHash } from "node:crypto";
import { constants as fsConstants } from "node:fs";
import { open } from "node:fs/promises";
import path from "node:path";

const PROCESS_QUERY_TIMEOUT_MS = 5_000;
const verifiedPackagedAuthorities = new WeakSet<object>();

export interface CoreProcessMatch {
  readonly executablePath: string;
  readonly processId: number;
}

export interface HelperSocketSample {
  readonly processIds: readonly number[];
  readonly samples: number;
  readonly violations: number;
}

export interface StagedHelperAuthority {
  readonly helperSha256: string;
  readonly packagedExecutable: string;
  readonly trustedRuntimeBases: readonly string[];
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

function requireCanonicalPackagedHelperPath(helperPath: string): string {
  if (
    !path.posix.isAbsolute(helperPath) ||
    path.posix.normalize(helperPath) !== helperPath ||
    path.posix.basename(helperPath) !== "tammy-sbr-helper" ||
    !helperPath.endsWith("/sbr-helper/darwin-arm64/tammy-sbr-helper")
  ) {
    throw new Error("INVALID_EXPECTED_HELPER_PATH");
  }
  return helperPath;
}

function requireStagedHelperAuthority(authority: StagedHelperAuthority): StagedHelperAuthority {
  requireCanonicalPackagedHelperPath(authority.packagedExecutable);
  if (
    !/^[0-9a-f]{64}$/.test(authority.helperSha256) ||
    authority.trustedRuntimeBases.length < 1 ||
    authority.trustedRuntimeBases.length > 2 ||
    new Set(authority.trustedRuntimeBases).size !== authority.trustedRuntimeBases.length
  ) {
    throw new Error("INVALID_STAGED_HELPER_AUTHORITY");
  }
  for (const base of authority.trustedRuntimeBases) {
    if (
      !path.posix.isAbsolute(base) ||
      path.posix.normalize(base) !== base ||
      !base.endsWith("/local-core/core/sbr-runtime")
    ) {
      throw new Error("INVALID_STAGED_HELPER_AUTHORITY");
    }
  }
  return authority;
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

async function queryMacOSExecutableImage(
  processId: number,
  expectedExecutablePath: string,
  dependencies: ProcessQueryDependencies,
): Promise<string | undefined> {
  let stdout: string;
  try {
    stdout = await runBoundedQuery(
      "/usr/sbin/lsof",
      ["-nP", "-a", "-p", String(processId), "-d", "txt", "-Fn"],
      dependencies,
    );
  } catch (error) {
    if (typeof error === "object" && error !== null && "code" in error && error.code === 1) {
      return undefined;
    }
    throw new Error("PROCESS_QUERY_FAILED");
  }
  const lines = parseLines(stdout);
  if (lines[0] !== `p${processId}`) {
    throw new Error("INVALID_PROCESS_EVIDENCE");
  }
  const images: string[] = [];
  for (let index = 1; index < lines.length; index += 2) {
    const descriptor = lines[index];
    const name = lines[index + 1];
    if (
      descriptor !== "ftxt" ||
      name === undefined ||
      !name.startsWith("n") ||
      !path.posix.isAbsolute(name.slice(1))
    ) {
      throw new Error("INVALID_PROCESS_EVIDENCE");
    }
    images.push(name.slice(1));
  }
  const expectedMatches = images.filter((image) => image === expectedExecutablePath);
  if (expectedMatches.length > 1 || images.length === 0) {
    throw new Error("INVALID_PROCESS_EVIDENCE");
  }
  return expectedMatches.length === 1 ? expectedExecutablePath : images[0];
}

function sameFileIdentity(
  left: Awaited<ReturnType<Awaited<ReturnType<typeof open>>["stat"]>>,
  right: Awaited<ReturnType<Awaited<ReturnType<typeof open>>["stat"]>>,
): boolean {
  return (
    left.dev === right.dev &&
    left.ino === right.ino &&
    left.mode === right.mode &&
    left.nlink === right.nlink &&
    left.size === right.size &&
    left.mtimeMs === right.mtimeMs &&
    left.ctimeMs === right.ctimeMs
  );
}

async function authenticatedFileDigest(filePath: string): Promise<string> {
  let handle: Awaited<ReturnType<typeof open>> | undefined;
  try {
    handle = await open(filePath, fsConstants.O_RDONLY | fsConstants.O_NOFOLLOW);
    const before = await handle.stat();
    if (!before.isFile() || before.isSymbolicLink() || before.nlink !== 1 || before.size <= 0) {
      throw new Error("UNAUTHENTICATED_STAGED_HELPER");
    }
    const bytes = await handle.readFile();
    const after = await handle.stat();
    if (!sameFileIdentity(before, after) || bytes.byteLength !== before.size) {
      throw new Error("UNAUTHENTICATED_STAGED_HELPER");
    }
    return createHash("sha256").update(bytes).digest("hex");
  } catch (error) {
    if (error instanceof Error && error.message === "UNAUTHENTICATED_STAGED_HELPER") throw error;
    throw new Error("UNAUTHENTICATED_STAGED_HELPER");
  } finally {
    await handle?.close().catch(() => undefined);
  }
}

function stagedHelperPatternSource(authority: StagedHelperAuthority): string {
  const bases = authority.trustedRuntimeBases.map(escapeRegularExpression).join("|");
  return `^(${bases})/tammy-sbr-runtime-[0-9a-f]{24}/sbr-helper$`;
}

async function queryAuthenticatedStagedHelpers(
  authority: StagedHelperAuthority,
  pinned: Map<number, string>,
  dependencies: ProcessQueryDependencies,
): Promise<readonly CoreProcessMatch[]> {
  const trusted = requireStagedHelperAuthority(authority);
  if (!verifiedPackagedAuthorities.has(trusted)) {
    if ((await authenticatedFileDigest(trusted.packagedExecutable)) !== trusted.helperSha256) {
      throw new Error("UNAUTHENTICATED_PACKAGED_HELPER");
    }
    verifiedPackagedAuthorities.add(trusted);
  }
  const patternSource = stagedHelperPatternSource(trusted);
  const pattern = new RegExp(patternSource, "u");
  let stdout: string;
  try {
    stdout = await runBoundedQuery("/usr/bin/pgrep", ["-f", "-x", patternSource], dependencies);
  } catch (error) {
    if (error instanceof Error && error.message === "PROCESS_QUERY_TIMEOUT") throw error;
    if (typeof error === "object" && error !== null && "code" in error && error.code === 1) {
      return [];
    }
    throw new Error("PROCESS_QUERY_FAILED");
  }
  const matches: CoreProcessMatch[] = [];
  for (const line of parseLines(stdout)) {
    const processId = parseProcessId(line);
    let command: string;
    try {
      command = await runBoundedQuery(
        "/bin/ps",
        ["-p", String(processId), "-o", "command="],
        dependencies,
      );
    } catch {
      throw new Error("PROCESS_QUERY_FAILED");
    }
    const commands = parseLines(command);
    const executablePath = commands.length === 1 ? commands[0] : undefined;
    if (!executablePath || !pattern.test(executablePath)) {
      throw new Error("UNAUTHENTICATED_STAGED_HELPER");
    }
    const executableImage = await queryMacOSExecutableImage(
      processId,
      executablePath,
      dependencies,
    );
    if (executableImage === undefined) continue;
    if (executableImage !== executablePath) {
      throw new Error("UNAUTHENTICATED_STAGED_HELPER");
    }
    const prior = pinned.get(processId);
    if (prior !== undefined && prior !== executablePath) {
      throw new Error("STAGED_HELPER_PATH_CHANGED");
    }
    if ((await authenticatedFileDigest(executablePath)) !== trusted.helperSha256) {
      throw new Error("UNAUTHENTICATED_STAGED_HELPER");
    }
    pinned.set(processId, executablePath);
    matches.push({ executablePath, processId });
  }
  return matches;
}

async function queryMacOS(
  corePath: string,
  dependencies: ProcessQueryDependencies,
): Promise<readonly CoreProcessMatch[]> {
  let stdout: string;
  try {
    stdout = await runBoundedQuery(
      "/usr/bin/pgrep",
      ["-f", "-x", `^${escapeRegularExpression(corePath)}( .*)?$`],
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

export async function findAuthenticatedStagedHelperProcesses(
  authority: StagedHelperAuthority,
  pinned: Map<number, string>,
  dependencies: ProcessQueryDependencies = {},
): Promise<readonly CoreProcessMatch[]> {
  return queryAuthenticatedStagedHelpers(authority, pinned, dependencies);
}

export async function sampleAuthenticatedStagedHelperSockets(
  authority: StagedHelperAuthority,
  pinned: Map<number, string>,
  dependencies: ProcessQueryDependencies = {},
): Promise<HelperSocketSample> {
  const processes = await findAuthenticatedStagedHelperProcesses(authority, pinned, dependencies);
  let samples = 0;
  let violations = 0;
  for (const process of processes) {
    samples += 1;
    try {
      const stdout = await runBoundedQuery(
        "/usr/sbin/lsof",
        ["-nP", "-a", "-p", String(process.processId), "-iTCP", "-iUDP"],
        dependencies,
      );
      if (parseLines(stdout).length > 0) violations += 1;
    } catch (error) {
      if (typeof error === "object" && error !== null && "code" in error && error.code === 1) {
        continue;
      }
      throw new Error("HELPER_SOCKET_QUERY_FAILED");
    }
  }
  return {
    processIds: processes.map((process) => process.processId),
    samples,
    violations,
  };
}
