import { spawn } from "node:child_process";
import { lstat, readdir, realpath } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const BASELINE_MESSAGE = "INITIAL_BASELINE_NOT_YET_ON_MASTER";

const GIT_BASELINE_ARGS = Object.freeze([
  "ls-tree",
  "-r",
  "--name-only",
  "master",
  "--",
  "buf.yaml",
  "proto",
]);
const BUF_BREAKING_ARGS = Object.freeze(["breaking", "--against", ".git#branch=master"]);
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
  const gitResult = validateCommandResult(
    await run({
      ...commonPlan,
      args: [...GIT_BASELINE_ARGS],
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
      args: [...BUF_BREAKING_ARGS],
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

async function runToolPlan(plan) {
  if (plan.tool === "git") {
    return runBoundedProcess({ ...plan, file: "git" });
  }
  if (plan.tool === "buf") {
    const bufEntry = fileURLToPath(import.meta.resolve("@bufbuild/buf/bin/buf"));
    return runBoundedProcess({
      ...plan,
      args: [bufEntry, ...plan.args],
      file: process.execPath,
    });
  }
  throw commandError("PROTO_BREAKING_TOOL_INVALID");
}

export function runBoundedProcess(
  { args, cwd, env, file, maxOutputBytes, reapTimeoutMs, terminationGraceMs, timeoutMs },
  { clearTimer = clearTimeout, setTimer = setTimeout, spawnProcess = spawn } = {},
) {
  return new Promise((resolve, reject) => {
    let child;
    try {
      child = spawnProcess(file, args, {
        cwd,
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
    let graceTimer;
    let reapTimer;

    const clearAllTimers = () => {
      clearTimer(timeoutTimer);
      if (graceTimer !== undefined) clearTimer(graceTimer);
      if (reapTimer !== undefined) clearTimer(reapTimer);
    };
    const finishWithoutClose = () => {
      if (settled) return;
      settled = true;
      clearAllTimers();
      reject(commandError("COMMAND_REAP_FAILED"));
    };
    const terminate = (reason) => {
      if (terminalError !== null || settled) return;
      terminalError = commandError(reason);
      try {
        child.kill("SIGTERM");
      } catch {
        // The close event remains the settlement boundary for a started child.
      }
      graceTimer = setTimer(() => {
        try {
          child.kill("SIGKILL");
        } catch {
          // The bounded reap timer below handles a missing close event.
        }
        reapTimer = setTimer(finishWithoutClose, reapTimeoutMs);
      }, terminationGraceMs);
    };
    const collect = (chunks, chunk, currentBytes, setBytes) => {
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
    child.stdout.on("data", (chunk) =>
      collect(stdout, chunk, stdoutBytes, (value) => {
        stdoutBytes = value;
      }),
    );
    child.stderr.on("data", (chunk) =>
      collect(stderr, chunk, stderrBytes, (value) => {
        stderrBytes = value;
      }),
    );
    child.once("error", () => terminate("COMMAND_START_FAILED"));
    child.once("close", (exitCode, signal) => {
      if (settled) return;
      settled = true;
      clearAllTimers();
      if (terminalError !== null) {
        reject(terminalError);
        return;
      }
      let stdoutText;
      let stderrText;
      try {
        const decoder = new TextDecoder("utf-8", { fatal: true });
        stdoutText = decoder.decode(Buffer.concat(stdout));
        stderrText = decoder.decode(Buffer.concat(stderr));
      } catch {
        reject(commandError("COMMAND_OUTPUT_INVALID"));
        return;
      }
      resolve({
        exitCode: Number.isInteger(exitCode) && exitCode >= 0 ? exitCode : 1,
        signal: signal ?? null,
        stderr: stderrText,
        stdout: stdoutText,
      });
    });
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
