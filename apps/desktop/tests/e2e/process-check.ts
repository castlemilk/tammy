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

export interface PackagedCoreAuthority {
  readonly coreSha256: string;
  readonly packagedExecutable: string;
}

export interface CoreProcessInstancePin {
  readonly executablePath: string;
  readonly instanceToken: string;
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

const WINDOWS_AUTH_PROCESS_QUERY = `
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
      CreationDate = [string]$_.CreationDate
    } | ConvertTo-Json -Compress
  }
`.trim();

const WINDOWS_AUTH_ARGUMENTS = Object.freeze([
  "-NoLogo",
  "-NoProfile",
  "-NonInteractive",
  "-ExecutionPolicy",
  "Bypass",
  "-Command",
  WINDOWS_AUTH_PROCESS_QUERY,
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

function requirePackagedCoreAuthority(
  authority: PackagedCoreAuthority,
  platform: NodeJS.Platform,
): PackagedCoreAuthority {
  requireCanonicalCorePath(authority.packagedExecutable, platform);
  if (!/^[0-9a-f]{64}$/.test(authority.coreSha256)) {
    throw new Error("INVALID_PACKAGED_CORE_AUTHORITY");
  }
  return authority;
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
  if (lines.length === 0) return undefined;
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

async function authenticatedCoreFileEvidence(
  filePath: string,
): Promise<{ readonly dev: bigint; readonly ino: bigint; readonly sha256: string }> {
  let handle: Awaited<ReturnType<typeof open>> | undefined;
  try {
    handle = await open(filePath, fsConstants.O_RDONLY | fsConstants.O_NOFOLLOW);
    const before = await handle.stat({ bigint: true });
    if (!before.isFile() || before.isSymbolicLink() || before.nlink !== 1n || before.size <= 0n) {
      throw new Error();
    }
    const digest = createHash("sha256");
    const buffer = Buffer.alloc(Math.min(1024 * 1024, Number(before.size)));
    let position = 0;
    while (position < Number(before.size)) {
      const { bytesRead } = await handle.read(
        buffer,
        0,
        Math.min(buffer.length, Number(before.size) - position),
        position,
      );
      if (bytesRead === 0) throw new Error();
      digest.update(buffer.subarray(0, bytesRead));
      position += bytesRead;
    }
    const after = await handle.stat({ bigint: true });
    if (
      before.dev !== after.dev ||
      before.ino !== after.ino ||
      before.mode !== after.mode ||
      before.nlink !== after.nlink ||
      before.size !== after.size ||
      before.mtimeNs !== after.mtimeNs ||
      before.ctimeNs !== after.ctimeNs
    ) {
      throw new Error();
    }
    return { dev: before.dev, ino: before.ino, sha256: digest.digest("hex") };
  } catch {
    throw new Error("UNAUTHENTICATED_CORE_PROCESS");
  } finally {
    await handle?.close().catch(() => undefined);
  }
}

interface MacOSExecutableImageEvidence {
  readonly dev: bigint;
  readonly executablePath: string;
  readonly ino: bigint;
}

async function queryMacOSCoreImage(
  processId: number,
  expectedExecutablePath: string,
  dependencies: ProcessQueryDependencies,
): Promise<MacOSExecutableImageEvidence | undefined> {
  let stdout: string;
  try {
    stdout = await runBoundedQuery(
      "/usr/sbin/lsof",
      ["-nP", "-a", "-p", String(processId), "-d", "txt", "-FDin"],
      dependencies,
    );
  } catch (error) {
    if (typeof error === "object" && error !== null && "code" in error && error.code === 1) {
      return undefined;
    }
    throw new Error("PROCESS_QUERY_FAILED");
  }
  const lines = parseLines(stdout);
  if (lines.length < 5 || lines[0] !== `p${processId}`) {
    throw new Error("INVALID_PROCESS_EVIDENCE");
  }
  const images: MacOSExecutableImageEvidence[] = [];
  for (let index = 1; index < lines.length; ) {
    if (lines[index] !== "ftxt") throw new Error("INVALID_PROCESS_EVIDENCE");
    const fields = lines.slice(index + 1, index + 4);
    const deviceField = fields.find((value) => value.startsWith("D"));
    const inodeField = fields.find((value) => value.startsWith("i"));
    const nameField = fields.find((value) => value.startsWith("n"));
    if (
      fields.length !== 3 ||
      deviceField === undefined ||
      inodeField === undefined ||
      nameField === undefined ||
      !/^D(?:0x[0-9a-f]+|[0-9]+)$/iu.test(deviceField) ||
      !/^i[1-9][0-9]*$/u.test(inodeField) ||
      !path.posix.isAbsolute(nameField.slice(1))
    ) {
      throw new Error("INVALID_PROCESS_EVIDENCE");
    }
    images.push({
      dev: BigInt(deviceField.slice(1)),
      executablePath: nameField.slice(1),
      ino: BigInt(inodeField.slice(1)),
    });
    index += 4;
  }
  const matches = images.filter((image) => image.executablePath === expectedExecutablePath);
  if (matches.length !== 1) throw new Error("UNAUTHENTICATED_CORE_PROCESS");
  return matches[0];
}

async function queryMacOSInstanceToken(
  processId: number,
  dependencies: ProcessQueryDependencies,
): Promise<string | undefined> {
  let stdout: string;
  try {
    stdout = await runBoundedQuery(
      "/bin/ps",
      ["-p", String(processId), "-o", "lstart="],
      dependencies,
    );
  } catch (error) {
    if (typeof error === "object" && error !== null && "code" in error && error.code === 1) {
      return undefined;
    }
    throw new Error("PROCESS_QUERY_FAILED");
  }
  const tokens = parseLines(stdout).map((line) => line.trim());
  if (tokens.length === 0) return undefined;
  const token = tokens[0];
  if (tokens.length !== 1 || token === undefined || token.length === 0 || token.length > 128) {
    throw new Error("INVALID_PROCESS_EVIDENCE");
  }
  return token;
}

async function queryAuthenticatedMacOSCore(
  authority: PackagedCoreAuthority,
  pinned: Map<number, CoreProcessInstancePin>,
  dependencies: ProcessQueryDependencies,
): Promise<readonly CoreProcessMatch[]> {
  const processIds = await queryMacOS(authority.packagedExecutable, dependencies);
  const matches: CoreProcessMatch[] = [];
  for (const { processId } of processIds) {
    const instanceBefore = await queryMacOSInstanceToken(processId, dependencies);
    if (instanceBefore === undefined) continue;
    const image = await queryMacOSCoreImage(processId, authority.packagedExecutable, dependencies);
    if (image === undefined) continue;
    const instanceAfter = await queryMacOSInstanceToken(processId, dependencies);
    if (instanceAfter === undefined || instanceAfter !== instanceBefore) continue;
    const prior = pinned.get(processId);
    if (
      prior !== undefined &&
      (prior.instanceToken !== instanceBefore ||
        prior.executablePath !== authority.packagedExecutable)
    ) {
      throw new Error("CORE_PROCESS_INSTANCE_CHANGED");
    }
    if (image.executablePath !== authority.packagedExecutable) {
      throw new Error("UNAUTHENTICATED_CORE_PROCESS");
    }
    const file = await authenticatedCoreFileEvidence(image.executablePath);
    if (file.dev !== image.dev || file.ino !== image.ino || file.sha256 !== authority.coreSha256) {
      throw new Error("UNAUTHENTICATED_CORE_PROCESS");
    }
    pinned.set(processId, {
      executablePath: image.executablePath,
      instanceToken: instanceBefore,
    });
    matches.push({ executablePath: image.executablePath, processId });
  }
  return matches;
}

function stagedHelperPatternSource(authority: StagedHelperAuthority): string {
  const bases = authority.trustedRuntimeBases.map(escapeRegularExpression).join("|");
  return `^(${bases})/tammy-sbr-runtime-[0-9a-f]{24}/sbr-helper$`;
}

async function queryExactStagedHelperProcessIds(
  patternSource: string,
  dependencies: ProcessQueryDependencies,
): Promise<readonly number[]> {
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
  return parseLines(stdout).map(parseProcessId);
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
  const matches: CoreProcessMatch[] = [];
  for (const processId of await queryExactStagedHelperProcessIds(patternSource, dependencies)) {
    let command: string;
    try {
      command = await runBoundedQuery(
        "/bin/ps",
        ["-p", String(processId), "-o", "command="],
        dependencies,
      );
    } catch (error) {
      if (typeof error === "object" && error !== null && "code" in error && error.code === 1) {
        continue;
      }
      throw new Error("PROCESS_QUERY_FAILED");
    }
    const commands = parseLines(command);
    if (commands.length === 0) continue;
    const executablePath = commands.length === 1 ? commands[0] : undefined;
    if (!executablePath || !pattern.test(executablePath)) {
      const stillMatches = await queryExactStagedHelperProcessIds(patternSource, dependencies);
      if (!stillMatches.includes(processId)) continue;
      throw new Error("UNAUTHENTICATED_STAGED_HELPER");
    }
    const executableImage = await queryMacOSExecutableImage(
      processId,
      executablePath,
      dependencies,
    );
    if (executableImage === undefined) continue;
    if (executableImage !== executablePath) {
      const stillMatches = await queryExactStagedHelperProcessIds(patternSource, dependencies);
      if (!stillMatches.includes(processId)) continue;
      throw new Error("UNAUTHENTICATED_STAGED_HELPER");
    }
    const prior = pinned.get(processId);
    if (prior !== undefined && prior !== executablePath) {
      throw new Error("STAGED_HELPER_PATH_CHANGED");
    }
    let stagedDigest: string;
    try {
      stagedDigest = await authenticatedFileDigest(executablePath);
    } catch (error) {
      if (!(error instanceof Error) || error.message !== "UNAUTHENTICATED_STAGED_HELPER") {
        throw error;
      }
      const stillMatches = await queryExactStagedHelperProcessIds(patternSource, dependencies);
      if (!stillMatches.includes(processId)) continue;
      throw error;
    }
    if (stagedDigest !== trusted.helperSha256) {
      const stillMatches = await queryExactStagedHelperProcessIds(patternSource, dependencies);
      if (!stillMatches.includes(processId)) continue;
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

function parseAuthenticatedWindowsLine(
  line: string,
  authority: PackagedCoreAuthority,
): CoreProcessMatch & { readonly instanceToken: string } {
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
    Object.keys(value).sort().join(",") !== "CreationDate,ExecutablePath,ProcessId"
  ) {
    throw new Error("INVALID_PROCESS_EVIDENCE");
  }
  const record = value as Record<string, unknown>;
  if (
    typeof record.ExecutablePath !== "string" ||
    typeof record.CreationDate !== "string" ||
    record.CreationDate.length === 0 ||
    record.CreationDate.length > 128 ||
    typeof record.ProcessId !== "number" ||
    !Number.isSafeInteger(record.ProcessId) ||
    record.ProcessId <= 0 ||
    path.win32.normalize(record.ExecutablePath).toLowerCase() !==
      path.win32.normalize(authority.packagedExecutable).toLowerCase()
  ) {
    throw new Error("INVALID_PROCESS_EVIDENCE");
  }
  return {
    executablePath: record.ExecutablePath,
    instanceToken: record.CreationDate,
    processId: record.ProcessId,
  };
}

async function queryAuthenticatedWindowsCore(
  authority: PackagedCoreAuthority,
  pinned: Map<number, CoreProcessInstancePin>,
  dependencies: ProcessQueryDependencies,
): Promise<readonly CoreProcessMatch[]> {
  const { command, environment } = windowsCommandAndEnvironment(
    authority.packagedExecutable,
    dependencies.environment ?? process.env,
  );
  let stdout: string;
  try {
    stdout = await runBoundedQuery(command, WINDOWS_AUTH_ARGUMENTS, {
      ...dependencies,
      environment,
    });
  } catch {
    throw new Error("PROCESS_QUERY_FAILED");
  }
  const file = await authenticatedCoreFileEvidence(authority.packagedExecutable);
  if (file.sha256 !== authority.coreSha256) throw new Error("UNAUTHENTICATED_CORE_PROCESS");
  const matches = parseLines(stdout).map((line) => parseAuthenticatedWindowsLine(line, authority));
  for (const match of matches) {
    const prior = pinned.get(match.processId);
    if (
      prior !== undefined &&
      (prior.executablePath.toLowerCase() !== match.executablePath.toLowerCase() ||
        prior.instanceToken !== match.instanceToken)
    ) {
      throw new Error("CORE_PROCESS_INSTANCE_CHANGED");
    }
    pinned.set(match.processId, {
      executablePath: match.executablePath,
      instanceToken: match.instanceToken,
    });
  }
  return matches.map(({ executablePath, processId }) => ({ executablePath, processId }));
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

export async function findAuthenticatedCoreProcesses(
  authority: PackagedCoreAuthority,
  pinned: Map<number, CoreProcessInstancePin>,
  platform: NodeJS.Platform = process.platform,
  dependencies: ProcessQueryDependencies = {},
): Promise<readonly CoreProcessMatch[]> {
  const trusted = requirePackagedCoreAuthority(authority, platform);
  if (platform === "darwin") return queryAuthenticatedMacOSCore(trusted, pinned, dependencies);
  if (platform === "win32") return queryAuthenticatedWindowsCore(trusted, pinned, dependencies);
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
