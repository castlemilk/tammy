import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import {
  createBufCommandPlan,
  createDescriptorBuildPlan,
  validateDescriptorManifest,
  validateDescriptorOutput,
} from "./build-descriptors.mjs";

const execFileAsync = promisify(execFile);
const DEFAULT_BUF_ENTRY = fileURLToPath(import.meta.resolve("@bufbuild/buf/bin/buf"));
const RETAINED_PATHS = new Set([
  "compliance/contracts/descriptor-manifest.json",
  "compliance/contracts/descriptors.pb",
]);

export async function verifyRetainedDescriptorEvidence({
  bufEntry = DEFAULT_BUF_ENTRY,
  nodeExecutable = process.execPath,
  platform = process.platform,
  root = process.cwd(),
  run = execFileAsync,
  subjectRevision = "HEAD",
}) {
  const retainedDirectory = path.join(root, "compliance/contracts");
  const descriptorBytes = await readFile(path.join(retainedDirectory, "descriptors.pb"));
  let manifest;
  try {
    manifest = JSON.parse(
      await readFile(path.join(retainedDirectory, "descriptor-manifest.json"), "utf8"),
    );
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
    const { stdout: changedStdout } = await run(
      "git",
      ["diff", "--name-only", manifest.gitRevision, resolvedSubjectRevision, "--"],
      { cwd: root, shell: false },
    );
    const changedPaths = String(changedStdout).trim().split(/\r?\n/).filter(Boolean);
    if (
      String(parentStdout).trim() !== manifest.gitRevision ||
      changedPaths.length === 0 ||
      changedPaths.some((changedPath) => !RETAINED_PATHS.has(changedPath))
    ) {
      throw new Error("DESCRIPTOR_SOURCE_REVISION_MISMATCH");
    }
  }
  const { stdout: statusStdout } = await run(
    "git",
    ["status", "--porcelain=v1", "--untracked-files=all"],
    { cwd: root, shell: false },
  );
  const dirtyPaths = String(statusStdout)
    .split(/\r?\n/)
    .filter(Boolean)
    .map((line) => line.slice(3).split(" -> ").at(-1));
  if (dirtyPaths.some((dirtyPath) => !RETAINED_PATHS.has(dirtyPath))) {
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
