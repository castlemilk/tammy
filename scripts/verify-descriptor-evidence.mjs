import { execFile, spawn } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import {
  createBufCommandPlan,
  createDescriptorBuildPlan,
  serializeDescriptorManifest,
  validateDescriptorManifest,
  validateDescriptorOutput,
} from "./build-descriptors.mjs";

const execFileAsync = promisify(execFile);
const DEFAULT_BUF_ENTRY = fileURLToPath(import.meta.resolve("@bufbuild/buf/bin/buf"));
const RETAINED_PATHS = new Set([
  "compliance/contracts/descriptor-manifest.json",
  "compliance/contracts/descriptors.pb",
]);

const MAX_OUTPUT_LINE_CHARACTERS = 1024 * 1024;
const MAX_STDERR_CHARACTERS = 32 * 1024;

function runVerificationCommand(command, args, { onStdoutLine, ...options }) {
  if (!onStdoutLine) return execFileAsync(command, args, options);
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      ...options,
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });
    let pending = "";
    let stderr = "";
    let stderrTruncated = false;
    let streamError;
    let spawnError;
    const consume = (line) => {
      if (line.endsWith("\r")) line = line.slice(0, -1);
      if (line.length > MAX_OUTPUT_LINE_CHARACTERS) {
        throw new Error("DESCRIPTOR_GIT_OUTPUT_LINE_TOO_LONG");
      }
      onStdoutLine(line);
    };
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      if (streamError) return;
      try {
        const lines = (pending + chunk).split("\n");
        pending = lines.pop();
        for (const line of lines) consume(line);
        if (pending.length > MAX_OUTPUT_LINE_CHARACTERS) {
          throw new Error("DESCRIPTOR_GIT_OUTPUT_LINE_TOO_LONG");
        }
      } catch (error) {
        streamError = error;
        pending = "";
      }
    });
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk) => {
      const remaining = MAX_STDERR_CHARACTERS - stderr.length;
      stderr += chunk.slice(0, remaining);
      if (chunk.length > remaining) stderrTruncated = true;
    });
    child.stdout.once("error", (error) => {
      streamError ??= error;
    });
    child.stderr.once("error", (error) => {
      streamError ??= error;
    });
    child.once("error", (error) => {
      spawnError = error;
    });
    child.once("close", (code, signal) => {
      if (stderrTruncated) stderr += "\n[output truncated]";
      if (spawnError) return reject(spawnError);
      if (code !== 0) {
        return reject(
          Object.assign(new Error(`${command} failed (${signal ?? code}): ${stderr}`), {
            code,
            signal,
            cmd: `${command} ${args.join(" ")}`,
            stderr,
          }),
        );
      }
      try {
        if (streamError) throw streamError;
        if (pending) consume(pending);
        resolve({ stdout: "", stderr });
      } catch (error) {
        reject(error);
      }
    });
  });
}

async function forEachGitOutputLine(run, root, args, consume) {
  const { stdout } = await run("git", args, {
    cwd: root,
    shell: false,
    onStdoutLine: consume,
  });
  // Injected test runners may return buffered output rather than invoke the consumer.
  if (stdout) for (const line of String(stdout).split(/\r?\n/)) consume(line);
}

export async function verifyRetainedDescriptorEvidence({
  bufEntry = DEFAULT_BUF_ENTRY,
  nodeExecutable = process.execPath,
  platform = process.platform,
  root = process.cwd(),
  run = runVerificationCommand,
  subjectRevision = "HEAD",
}) {
  const retainedDirectory = path.join(root, "compliance/contracts");
  const descriptorBytes = await readFile(path.join(retainedDirectory, "descriptors.pb"));
  let manifestSource;
  let manifest;
  try {
    manifestSource = await readFile(
      path.join(retainedDirectory, "descriptor-manifest.json"),
      "utf8",
    );
    manifest = JSON.parse(manifestSource);
  } catch (cause) {
    throw new Error("DESCRIPTOR_MANIFEST_SCHEMA_INVALID", { cause });
  }
  validateDescriptorOutput(descriptorBytes);
  validateDescriptorManifest({
    currentRevision: manifest.gitRevision,
    descriptorBytes,
    dirty: false,
    manifest,
    mode: "evidence",
  });
  if (manifestSource !== serializeDescriptorManifest(manifest)) {
    throw new Error("DESCRIPTOR_MANIFEST_CANONICAL_INVALID");
  }

  let resolvedSubjectRevision;
  try {
    const { stdout } = await run("git", ["rev-parse", "--verify", `${subjectRevision}^{commit}`], {
      cwd: root,
      shell: false,
    });
    resolvedSubjectRevision = String(stdout).trim();
    await run("git", ["cat-file", "-e", `${manifest.gitRevision}^{commit}`], {
      cwd: root,
      shell: false,
    });
  } catch (cause) {
    throw new Error("DESCRIPTOR_SOURCE_REVISION_INVALID", { cause });
  }
  if (manifest.gitRevision !== resolvedSubjectRevision) {
    const { stdout: parentStdout } = await run(
      "git",
      ["rev-parse", "--verify", `${resolvedSubjectRevision}^`],
      { cwd: root, shell: false },
    );
    let changedPathCount = 0;
    let invalidChangedPath = false;
    await forEachGitOutputLine(
      run,
      root,
      ["diff", "--name-only", manifest.gitRevision, resolvedSubjectRevision, "--"],
      (line) => {
        if (!line) return;
        changedPathCount += 1;
        if (!RETAINED_PATHS.has(line)) invalidChangedPath = true;
      },
    );
    if (
      String(parentStdout).trim() !== manifest.gitRevision ||
      changedPathCount === 0 ||
      invalidChangedPath
    ) {
      throw new Error("DESCRIPTOR_SOURCE_REVISION_MISMATCH");
    }
  }
  let invalidDirtyPath = false;
  await forEachGitOutputLine(
    run,
    root,
    ["status", "--porcelain=v1", "--untracked-files=all"],
    (line) => {
      if (!line) return;
      const dirtyPath = line.slice(3).split(" -> ").at(-1);
      if (!RETAINED_PATHS.has(dirtyPath)) invalidDirtyPath = true;
    },
  );
  if (invalidDirtyPath) {
    throw new Error("DESCRIPTOR_EVIDENCE_DIRTY_TREE");
  }

  const temporaryRoot = path.join(root, ".tmp");
  await mkdir(temporaryRoot, { recursive: true });
  const stagingDirectory = await mkdtemp(path.join(temporaryRoot, "descriptor-verify-"));
  try {
    const plan = createDescriptorBuildPlan({
      bufEntry,
      mode: "validation",
      nodeExecutable,
      outputDirectory: stagingDirectory,
      platform,
      root,
    });
    await run(plan.command, plan.args, { cwd: root, shell: plan.shell });
    const freshDescriptorBytes = await readFile(path.join(stagingDirectory, "descriptors.pb"));
    validateDescriptorOutput(freshDescriptorBytes);
    const versionPlan = createBufCommandPlan({
      args: ["--version"],
      bufEntry,
      nodeExecutable,
      platform,
    });
    const { stdout } = await run(versionPlan.command, versionPlan.args, {
      cwd: root,
      shell: versionPlan.shell,
    });
    if (String(stdout).trim() !== manifest.bufVersion) {
      throw new Error("DESCRIPTOR_MANIFEST_BUF_VERSION_MISMATCH");
    }
    if (!descriptorBytes.equals(freshDescriptorBytes)) {
      throw new Error("DESCRIPTOR_RETAINED_BYTES_STALE");
    }
    return manifest;
  } finally {
    await rm(stagingDirectory, { force: true, recursive: true });
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await verifyRetainedDescriptorEvidence({
    subjectRevision: process.env.TAMMY_EVIDENCE_SUBJECT_REVISION || "HEAD",
  });
}
