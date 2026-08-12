import { spawn, spawnSync } from "node:child_process";
import { lstat, open, readdir, realpath } from "node:fs/promises";
import { createRequire } from "node:module";
import { arch } from "node:os";
import path from "node:path";

export const BASELINE_MESSAGE = "INITIAL_BASELINE_NOT_YET_ON_MASTER";

const MASTER_REFERENCES = Object.freeze(["refs/remotes/origin/master", "refs/heads/master"]);
const NATIVE_BUF_PACKAGES = Object.freeze({
  "darwin:arm64": "@bufbuild/buf-darwin-arm64",
  "darwin:x64": "@bufbuild/buf-darwin-x64",
  "linux:arm": "@bufbuild/buf-linux-armv7",
  "linux:arm64": "@bufbuild/buf-linux-aarch64",
  "linux:x64": "@bufbuild/buf-linux-x64",
  "win32:arm64": "@bufbuild/buf-win32-arm64",
  "win32:x64": "@bufbuild/buf-win32-x64",
});
const MAX_OUTPUT_BYTES = 1024 * 1024;
const MAX_CURRENT_ENTRIES = 4096;
const MAX_CURRENT_DEPTH = 32;
const ALLOWED_ENVIRONMENT_KEYS = new Set([
  "APPDATA",
  "HOME",
  "LANG",
  "LC_ALL",
  "LOCALAPPDATA",
  "PATH",
  "PATHEXT",
  "SYSTEMROOT",
  "TEMP",
  "TMP",
  "TMPDIR",
  "USERPROFILE",
  "WINDIR",
  "XDG_CONFIG_HOME",
]);

class ProtoBreakingError extends Error {
  constructor(message, exitCode = 1) {
    super(message);
    this.exitCode = exitCode;
  }
}

const requireFromBufPackage = createRequire(import.meta.resolve("@bufbuild/buf"));

function commandError(message) {
  return new ProtoBreakingError(message);
}

export function sanitizeCommandEnvironment(sourceEnvironment = process.env) {
  const environment = {};
  for (const [key, value] of Object.entries(sourceEnvironment)) {
    if (
      ALLOWED_ENVIRONMENT_KEYS.has(key.toUpperCase()) &&
      typeof value === "string" &&
      !value.includes("\0")
    ) {
      environment[key] = value;
    }
  }
  return environment;
}

async function validateCurrentInputs(root) {
  if (typeof root !== "string" || !path.isAbsolute(root)) {
    throw commandError("PROTO_BREAKING_CURRENT_INPUT_INVALID");
  }
  const physicalRoot = await realpath(root).catch(() => null);
  if (physicalRoot === null) throw commandError("PROTO_BREAKING_CURRENT_INPUT_INVALID");

  const configuration = await lstat(path.join(physicalRoot, "buf.yaml")).catch(() => null);
  const protoRoot = await lstat(path.join(physicalRoot, "proto")).catch(() => null);
  if (
    configuration === null ||
    !configuration.isFile() ||
    configuration.isSymbolicLink() ||
    configuration.size === 0 ||
    configuration.size > MAX_OUTPUT_BYTES ||
    protoRoot === null ||
    !protoRoot.isDirectory() ||
    protoRoot.isSymbolicLink()
  ) {
    throw commandError("PROTO_BREAKING_CURRENT_INPUT_INVALID");
  }

  let visited = 0;
  let hasContract = false;
  const visit = async (directory, depth) => {
    if (depth > MAX_CURRENT_DEPTH) throw commandError("PROTO_BREAKING_CURRENT_INPUT_INVALID");
    const entries = await readdir(directory, { withFileTypes: true }).catch(() => null);
    if (entries === null) throw commandError("PROTO_BREAKING_CURRENT_INPUT_INVALID");
    entries.sort((left, right) => left.name.localeCompare(right.name));
    for (const entry of entries) {
      visited += 1;
      if (visited > MAX_CURRENT_ENTRIES || entry.isSymbolicLink()) {
        throw commandError("PROTO_BREAKING_CURRENT_INPUT_INVALID");
      }
      const candidate = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        await visit(candidate, depth + 1);
      } else if (entry.isFile() && entry.name.endsWith(".proto")) {
        hasContract = true;
      }
    }
  };
  await visit(path.join(physicalRoot, "proto"), 0);
  if (!hasContract) throw commandError("PROTO_BREAKING_CURRENT_INPUT_INVALID");
  return physicalRoot;
}

function validateCommandResult(result) {
  if (
    result === null ||
    typeof result !== "object" ||
    !Number.isInteger(result.exitCode) ||
    result.exitCode < 0 ||
    typeof result.stdout !== "string" ||
    typeof result.stderr !== "string" ||
    (result.signal !== null && typeof result.signal !== "string")
  ) {
    throw commandError("PROTO_BREAKING_COMMAND_RESULT_INVALID");
  }
  return result;
}

function inspectMasterEntries(stdout) {
  if (stdout.includes("\0") || stdout.includes("\r")) {
    throw commandError("PROTO_BREAKING_MASTER_BASELINE_MALFORMED");
  }
  const entries = stdout.endsWith("\n") ? stdout.slice(0, -1).split("\n") : stdout.split("\n");
  if (entries.length === 1 && entries[0] === "") return { hasBuf: false, hasProto: false };

  const unique = new Set();
  for (const entry of entries) {
    if (
      entry.length === 0 ||
      entry.startsWith("/") ||
      entry.startsWith("../") ||
      path.posix.normalize(entry) !== entry ||
      (entry !== "buf.yaml" && !entry.startsWith("proto/")) ||
      unique.has(entry)
    ) {
      throw commandError("PROTO_BREAKING_MASTER_BASELINE_MALFORMED");
    }
    unique.add(entry);
  }
  return {
    hasBuf: unique.has("buf.yaml"),
    hasProto: [...unique].some((entry) => /^proto\/.+\.proto$/.test(entry)),
  };
}

function inspectMasterOid(stdout) {
  if (!/^(?:[0-9a-f]{40}|[0-9a-f]{64})\n$/.test(stdout)) {
    throw commandError("PROTO_BREAKING_MASTER_REF_MALFORMED");
  }
  return stdout.slice(0, -1);
}

function hasNativeExecutableHeader(header, platform) {
  if (platform === "linux") {
    return header.equals(Buffer.from([0x7f, 0x45, 0x4c, 0x46]));
  }
  if (platform === "win32") {
    return header[0] === 0x4d && header[1] === 0x5a;
  }
  if (platform === "darwin") {
    const magic = header.readUInt32BE(0);
    return new Set([
      0xbebafeca, 0xbfbafeca, 0xcafebabe, 0xcafebabf, 0xcefaedfe, 0xcffaedfe, 0xfeedface,
      0xfeedfacf,
    ]).has(magic);
  }
  return false;
}

async function resolveMasterOid({ commonPlan, run, writeError }) {
  for (const reference of MASTER_REFERENCES) {
    const result = validateCommandResult(
      await run({
        ...commonPlan,
        args: ["rev-parse", "--verify", "--quiet", `${reference}^{commit}`],
        timeoutMs: 10_000,
        tool: "git",
      }),
    );
    if (result.exitCode === 1 && result.signal === null && !result.stdout && !result.stderr) {
      continue;
    }
    if (result.exitCode !== 0) {
      if (result.stderr) writeError(result.stderr);
      throw commandError("PROTO_BREAKING_GIT_INSPECTION_FAILED");
    }

    return inspectMasterOid(result.stdout);
  }
  throw commandError("PROTO_BREAKING_MASTER_REF_MISSING");
}

export async function checkProtoBreaking({
  root,
  run = runToolPlan,
  sourceEnvironment = process.env,
  writeError = (value) => process.stderr.write(value),
  writeOutput = (value) => process.stdout.write(value),
} = {}) {
  const physicalRoot = await validateCurrentInputs(root);
  const commonPlan = {
    cwd: physicalRoot,
    env: sanitizeCommandEnvironment(sourceEnvironment),
    maxOutputBytes: MAX_OUTPUT_BYTES,
    reapTimeoutMs: 1000,
    terminationGraceMs: 1000,
  };
  const masterOid = await resolveMasterOid({ commonPlan, run, writeError });
  const gitResult = validateCommandResult(
    await run({
      ...commonPlan,
      args: ["ls-tree", "-r", "--name-only", masterOid, "--", "buf.yaml", "proto"],
      timeoutMs: 10_000,
      tool: "git",
    }),
  );
  if (gitResult.exitCode !== 0) {
    if (gitResult.stderr) writeError(gitResult.stderr);
    throw commandError("PROTO_BREAKING_GIT_INSPECTION_FAILED");
  }

  const baseline = inspectMasterEntries(gitResult.stdout);
  if (!baseline.hasBuf && !baseline.hasProto) {
    writeOutput(`${BASELINE_MESSAGE}\n`);
    return "INITIAL_BASELINE";
  }
  if (!baseline.hasBuf || !baseline.hasProto) {
    throw commandError("PROTO_BREAKING_MASTER_BASELINE_PARTIAL");
  }

  const bufResult = validateCommandResult(
    await run({
      ...commonPlan,
      args: ["breaking", "--against", `.git#ref=${masterOid}`],
      timeoutMs: 120_000,
      tool: "buf",
    }),
  );
  if (bufResult.stdout) writeOutput(bufResult.stdout);
  if (bufResult.stderr) writeError(bufResult.stderr);
  if (bufResult.exitCode !== 0) {
    throw new ProtoBreakingError("PROTO_BREAKING_FAILED", bufResult.exitCode);
  }
  return "VERIFIED";
}

export async function resolveNativeBufExecutable({
  architecture = arch(),
  packageResolve = (specifier) => requireFromBufPackage.resolve(specifier),
  platform = process.platform,
} = {}) {
  const packageName = NATIVE_BUF_PACKAGES[`${platform}:${architecture}`];
  if (packageName === undefined) {
    throw commandError("PROTO_BREAKING_BUF_PLATFORM_UNSUPPORTED");
  }
  const binaryName = platform === "win32" ? "buf.exe" : "buf";

  let manifestPath;
  let executablePath;
  try {
    [manifestPath, executablePath] = await Promise.all([
      realpath(packageResolve(`${packageName}/package.json`)),
      realpath(packageResolve(`${packageName}/bin/${binaryName}`)),
    ]);
  } catch {
    throw commandError("PROTO_BREAKING_BUF_EXECUTABLE_INVALID");
  }

  const packageRoot = path.dirname(manifestPath);
  if (path.relative(packageRoot, executablePath) !== path.join("bin", binaryName)) {
    throw commandError("PROTO_BREAKING_BUF_EXECUTABLE_INVALID");
  }
  const executable = await lstat(executablePath).catch(() => null);
  if (
    executable === null ||
    !executable.isFile() ||
    executable.isSymbolicLink() ||
    executable.size === 0 ||
    (platform !== "win32" && (executable.mode & 0o111) === 0)
  ) {
    throw commandError("PROTO_BREAKING_BUF_EXECUTABLE_INVALID");
  }

  const handle = await open(executablePath, "r").catch(() => null);
  if (handle === null) throw commandError("PROTO_BREAKING_BUF_EXECUTABLE_INVALID");
  const header = Buffer.alloc(4);
  let bytesRead = 0;
  try {
    ({ bytesRead } = await handle.read(header, 0, header.length, 0));
  } catch {
    throw commandError("PROTO_BREAKING_BUF_EXECUTABLE_INVALID");
  } finally {
    await handle.close().catch(() => {});
  }
  if (bytesRead !== header.length || !hasNativeExecutableHeader(header, platform)) {
    throw commandError("PROTO_BREAKING_BUF_EXECUTABLE_INVALID");
  }
  return executablePath;
}

export async function runToolPlan(
  plan,
  { resolveBufExecutable = resolveNativeBufExecutable, runProcess = runBoundedProcess } = {},
) {
  if (plan.tool === "git") {
    return runProcess({ ...plan, file: "git" });
  }
  if (plan.tool === "buf") {
    const executable = await resolveBufExecutable();
    if (
      typeof executable !== "string" ||
      !path.isAbsolute(executable) ||
      executable.includes("\0")
    ) {
      throw commandError("PROTO_BREAKING_BUF_EXECUTABLE_INVALID");
    }
    return runProcess({ ...plan, file: executable });
  }
  throw commandError("PROTO_BREAKING_TOOL_INVALID");
}

export function killProcessTree(
  child,
  signal,
  { env = process.env, platform = process.platform, spawnProcessSync = spawnSync } = {},
) {
  if (platform === "win32" && Number.isInteger(child.pid) && child.pid > 0) {
    // Windows has no durable process-group identity, so force the tree before its parent PID exits.
    const args = ["/PID", String(child.pid), "/T", "/F"];
    const result = spawnProcessSync("taskkill.exe", args, {
      env,
      shell: false,
      stdio: "ignore",
      timeout: 5000,
      windowsHide: true,
    });
    if (result.error === undefined && result.status === 0) return true;
    if (signal === "SIGKILL") child.kill(signal);
    return false;
  }
  if (platform !== "win32" && Number.isInteger(child.pid) && child.pid > 0) {
    try {
      process.kill(-child.pid, signal);
      return true;
    } catch {
      // Fall through when the process group no longer exists.
    }
  }
  return child.kill(signal);
}

export function runBoundedProcess(
  { args, cwd, env, file, maxOutputBytes, reapTimeoutMs, terminationGraceMs, timeoutMs },
  {
    clearTimer = clearTimeout,
    platform = process.platform,
    setTimer = setTimeout,
    spawnProcess = spawn,
    spawnProcessSync = spawnSync,
  } = {},
) {
  return new Promise((resolve, reject) => {
    let child;
    try {
      child = spawnProcess(file, args, {
        cwd,
        detached: platform !== "win32",
        env,
        shell: false,
        stdio: ["ignore", "pipe", "pipe"],
        windowsHide: true,
      });
    } catch {
      reject(commandError("COMMAND_START_FAILED"));
      return;
    }

    const stdout = [];
    const stderr = [];
    let stdoutBytes = 0;
    let stderrBytes = 0;
    let terminalError = null;
    let settled = false;
    let directChildClosed = false;
    let treeCleanupComplete = false;
    let graceTimer;
    let reapTimer;
    let verificationTimer;

    const clearAllTimers = () => {
      clearTimer(timeoutTimer);
      if (graceTimer !== undefined) clearTimer(graceTimer);
      if (reapTimer !== undefined) clearTimer(reapTimer);
      if (verificationTimer !== undefined) clearTimer(verificationTimer);
    };
    const removeAllListeners = () => {
      child.stdout.off("data", onStdout);
      child.stderr.off("data", onStderr);
      child.off("error", onError);
      child.off("close", onClose);
    };
    const finishRejected = (error) => {
      if (settled) return;
      settled = true;
      clearAllTimers();
      removeAllListeners();
      reject(error);
    };
    const finishTerminatedIfReady = () => {
      if (directChildClosed && treeCleanupComplete) finishRejected(terminalError);
    };
    const finishWithoutReap = () => {
      if (settled) return;
      try {
        killProcessTree(child, "SIGKILL", { env, platform, spawnProcessSync });
      } catch {
        // The bounded cleanup has already exhausted both termination phases.
      }
      finishRejected(commandError("COMMAND_REAP_FAILED"));
    };
    const processGroupExists = () => {
      try {
        process.kill(-child.pid, 0);
        return true;
      } catch (error) {
        return error?.code !== "ESRCH";
      }
    };
    const verifyUnixTreeReaped = () => {
      if (settled) return;
      if (!processGroupExists()) {
        treeCleanupComplete = true;
        finishTerminatedIfReady();
        return;
      }
      const pollMilliseconds = Math.max(1, Math.min(10, Math.floor(reapTimeoutMs / 4)));
      verificationTimer = setTimer(verifyUnixTreeReaped, pollMilliseconds);
    };
    const startReapDeadline = () => {
      if (reapTimer === undefined) {
        reapTimer = setTimer(finishWithoutReap, reapTimeoutMs);
      }
    };
    const forceTreeCleanup = () => {
      if (settled) return;
      startReapDeadline();
      let forceIssued = false;
      try {
        forceIssued = killProcessTree(child, "SIGKILL", { env, platform, spawnProcessSync });
      } catch {
        // The reap deadline below is the fail-closed settlement boundary.
      }
      if (platform === "win32") {
        treeCleanupComplete = forceIssued === true;
        finishTerminatedIfReady();
        return;
      }
      if (Number.isInteger(child.pid) && child.pid > 0) {
        verifyUnixTreeReaped();
        return;
      }
      treeCleanupComplete = true;
      finishTerminatedIfReady();
    };
    const terminate = (reason) => {
      if (terminalError !== null || settled) return;
      terminalError = commandError(reason);
      clearTimer(timeoutTimer);
      if (platform === "win32") {
        let forcedTree = false;
        try {
          forcedTree = killProcessTree(child, "SIGTERM", { env, platform, spawnProcessSync });
        } catch {
          // Retain the parent PID for the bounded forced retry below.
        }
        if (forcedTree === true) {
          treeCleanupComplete = true;
          startReapDeadline();
          finishTerminatedIfReady();
          return;
        }
        graceTimer = setTimer(forceTreeCleanup, terminationGraceMs);
        return;
      }
      try {
        killProcessTree(child, "SIGTERM", { env, platform, spawnProcessSync });
      } catch {
        // Forced process-group cleanup still runs after the grace period.
      }
      graceTimer = setTimer(forceTreeCleanup, terminationGraceMs);
    };
    const collect = (chunks, chunk, currentBytes, setBytes) => {
      if (terminalError !== null || settled) return;
      const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
      const nextBytes = currentBytes + bytes.length;
      setBytes(nextBytes);
      if (nextBytes > maxOutputBytes) {
        terminate("COMMAND_OUTPUT_LIMIT_EXCEEDED");
        return;
      }
      chunks.push(bytes);
    };

    const timeoutTimer = setTimer(() => terminate("COMMAND_TIMED_OUT"), timeoutMs);
    const onStdout = (chunk) =>
      collect(stdout, chunk, stdoutBytes, (value) => {
        stdoutBytes = value;
      });
    const onStderr = (chunk) =>
      collect(stderr, chunk, stderrBytes, (value) => {
        stderrBytes = value;
      });
    const onError = () => terminate("COMMAND_START_FAILED");
    const onClose = (exitCode, signal) => {
      if (settled) return;
      if (terminalError !== null) {
        directChildClosed = true;
        finishTerminatedIfReady();
        return;
      }
      let stdoutText;
      let stderrText;
      try {
        const decoder = new TextDecoder("utf-8", { fatal: true });
        stdoutText = decoder.decode(Buffer.concat(stdout));
        stderrText = decoder.decode(Buffer.concat(stderr));
      } catch {
        finishRejected(commandError("COMMAND_OUTPUT_INVALID"));
        return;
      }
      settled = true;
      clearAllTimers();
      removeAllListeners();
      resolve({
        exitCode: Number.isInteger(exitCode) && exitCode >= 0 ? exitCode : 1,
        signal: signal ?? null,
        stderr: stderrText,
        stdout: stdoutText,
      });
    };
    child.stdout.on("data", onStdout);
    child.stderr.on("data", onStderr);
    child.once("error", onError);
    child.once("close", onClose);
  });
}

async function main() {
  const root = await realpath(path.resolve(import.meta.dirname, ".."));
  await checkProtoBreaking({ root });
}

if (process.argv[1] && path.resolve(process.argv[1]) === import.meta.filename) {
  main().catch((error) => {
    const message = error instanceof Error ? error.message : "PROTO_BREAKING_FAILED";
    process.stderr.write(`${message}\n`);
    process.exitCode =
      error instanceof ProtoBreakingError && error.exitCode >= 1 && error.exitCode <= 255
        ? error.exitCode
        : 1;
  });
}
