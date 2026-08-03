import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, rename, rm, rmdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { fromBinary } from "@bufbuild/protobuf";
import { FileDescriptorSetSchema } from "@bufbuild/protobuf/wkt";

export const PINNED_BUF_VERSION = "1.72.0";
export const BUF_MODULE = "buf.build/tammyapp/tammy";
const execFileAsync = promisify(execFile);
const DEFAULT_BUF_ENTRY = fileURLToPath(import.meta.resolve("@bufbuild/buf/bin/buf"));

export function createBufCommandPlan({ args, bufEntry, nodeExecutable, platform }) {
  const pathApi = platform === "win32" ? path.win32 : path.posix;
  if (
    !pathApi.isAbsolute(nodeExecutable) ||
    !pathApi.isAbsolute(bufEntry) ||
    /\.(?:cmd|bat)$/i.test(nodeExecutable) ||
    /\.(?:cmd|bat)$/i.test(bufEntry)
  ) {
    throw new Error("DESCRIPTOR_BUF_COMMAND_INVALID");
  }
  return {
    args: [bufEntry, ...args],
    command: nodeExecutable,
    shell: false,
  };
}

export function createDescriptorBuildPlan({
  bufEntry = DEFAULT_BUF_ENTRY,
  mode,
  nodeExecutable = process.execPath,
  outputDirectory: requestedOutputDirectory,
  platform = process.platform,
  root,
}) {
  const outputDirectory =
    requestedOutputDirectory ??
    path.join(root, mode === "evidence" ? "compliance/contracts" : ".tmp/contracts");
  const commandPlan = createBufCommandPlan({
    args: [
      "build",
      "--as-file-descriptor-set",
      "--exclude-source-info",
      "--exclude-source-retention-options",
      "--output",
      path.join(outputDirectory, "descriptors.pb"),
    ],
    bufEntry,
    nodeExecutable,
    platform,
  });
  return {
    ...commandPlan,
    outputDirectory,
  };
}

export function validateDescriptorManifest({
  currentRevision,
  descriptorBytes,
  dirty,
  manifest,
  mode,
}) {
  if (manifest === null || typeof manifest !== "object" || Array.isArray(manifest)) {
    throw new Error("DESCRIPTOR_MANIFEST_SCHEMA_INVALID");
  }
  if (mode === "validation" && Object.hasOwn(manifest, "gitRevision")) {
    throw new Error("DESCRIPTOR_VALIDATION_REVISION_FORBIDDEN");
  }
  if (mode === "evidence" && !Object.hasOwn(manifest, "gitRevision")) {
    throw new Error("DESCRIPTOR_SOURCE_REVISION_REQUIRED");
  }
  const expectedFields = ["bufVersion", "byteLength", "module", "path", "sha256"];
  if (mode === "evidence") expectedFields.push("gitRevision");
  const actualFields = Object.keys(manifest).sort();
  expectedFields.sort();
  if (
    actualFields.length !== expectedFields.length ||
    actualFields.some((field, index) => field !== expectedFields[index]) ||
    typeof manifest.path !== "string" ||
    !Number.isSafeInteger(manifest.byteLength) ||
    manifest.byteLength < 0 ||
    typeof manifest.sha256 !== "string" ||
    typeof manifest.bufVersion !== "string" ||
    typeof manifest.module !== "string" ||
    (mode === "evidence" && typeof manifest.gitRevision !== "string")
  ) {
    throw new Error("DESCRIPTOR_MANIFEST_SCHEMA_INVALID");
  }
  if (manifest.path !== "descriptors.pb") {
    throw new Error("DESCRIPTOR_MANIFEST_PATH_INVALID");
  }
  if (manifest.byteLength !== descriptorBytes.byteLength) {
    throw new Error("DESCRIPTOR_MANIFEST_LENGTH_MISMATCH");
  }
  const sha256 = createHash("sha256").update(descriptorBytes).digest("hex");
  if (!/^[0-9a-f]{64}$/.test(manifest.sha256) || manifest.sha256 !== sha256) {
    throw new Error("DESCRIPTOR_MANIFEST_SHA256_INVALID");
  }
  if (manifest.bufVersion !== PINNED_BUF_VERSION) {
    throw new Error("DESCRIPTOR_MANIFEST_BUF_VERSION_MISMATCH");
  }
  if (manifest.module !== BUF_MODULE) {
    throw new Error("DESCRIPTOR_MANIFEST_MODULE_MISMATCH");
  }
  if (mode === "evidence" && !/^[0-9a-f]{40}$/.test(manifest.gitRevision)) {
    throw new Error("DESCRIPTOR_SOURCE_REVISION_INVALID");
  }
  if (mode === "evidence" && manifest.gitRevision !== currentRevision) {
    throw new Error("DESCRIPTOR_SOURCE_REVISION_MISMATCH");
  }
  if (mode === "evidence" && dirty) {
    throw new Error("DESCRIPTOR_EVIDENCE_DIRTY_TREE");
  }
}

export function validateDescriptorOutput(descriptorBytes) {
  try {
    const descriptorSet = fromBinary(FileDescriptorSetSchema, descriptorBytes);
    if (descriptorSet.file.length === 0) throw new Error("empty descriptor set");
  } catch (cause) {
    throw new Error("DESCRIPTOR_BUILD_OUTPUT_INVALID", { cause });
  }
}

async function buildDescriptorIntoDirectory({
  bufEntry,
  mode,
  nodeExecutable,
  outputDirectory,
  platform,
  root,
  run,
}) {
  const plan = createDescriptorBuildPlan({
    bufEntry,
    mode,
    nodeExecutable,
    outputDirectory,
    platform,
    root,
  });
  await mkdir(plan.outputDirectory, { recursive: true });
  await run(plan.command, plan.args, { cwd: root, shell: plan.shell });
  let descriptorBytes;
  try {
    descriptorBytes = await readFile(path.join(plan.outputDirectory, "descriptors.pb"));
  } catch (cause) {
    throw new Error("DESCRIPTOR_BUILD_OUTPUT_INVALID", { cause });
  }
  validateDescriptorOutput(descriptorBytes);
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
  return { bufVersion: String(stdout).trim(), descriptorBytes };
}

function createDescriptorManifest({ bufVersion, descriptorBytes, gitRevision }) {
  const manifest = {
    path: "descriptors.pb",
    byteLength: descriptorBytes.byteLength,
    sha256: createHash("sha256").update(descriptorBytes).digest("hex"),
    bufVersion,
    module: BUF_MODULE,
  };
  if (gitRevision !== undefined) manifest.gitRevision = gitRevision;
  return manifest;
}

async function assertEvidenceSourceState({ expectedRevision, root, run }) {
  const revisionResult = await run("git", ["rev-parse", "HEAD"], {
    cwd: root,
    shell: false,
  });
  if (String(revisionResult.stdout).trim() !== expectedRevision) {
    throw new Error("DESCRIPTOR_SOURCE_REVISION_MISMATCH");
  }
  const statusResult = await run("git", ["status", "--porcelain=v1"], {
    cwd: root,
    shell: false,
  });
  if (String(statusResult.stdout).trim().length > 0) {
    throw new Error("DESCRIPTOR_EVIDENCE_DIRTY_TREE");
  }
}

async function publishDescriptorEvidence({
  backupDirectory,
  retainedDirectory,
  renamePath,
  stagingDirectory,
}) {
  try {
    await renamePath(retainedDirectory, backupDirectory);
  } catch (cause) {
    throw new Error("DESCRIPTOR_EVIDENCE_PUBLISH_FAILED", { cause });
  }
  try {
    await renamePath(stagingDirectory, retainedDirectory);
  } catch (cause) {
    try {
      await renamePath(backupDirectory, retainedDirectory);
    } catch (rollbackCause) {
      throw new Error("DESCRIPTOR_EVIDENCE_ROLLBACK_FAILED", {
        cause: new AggregateError([cause, rollbackCause]),
      });
    }
    throw new Error("DESCRIPTOR_EVIDENCE_PUBLISH_FAILED", { cause });
  }
}

async function removeEmptyDirectory(directory) {
  try {
    await rmdir(directory);
  } catch (error) {
    if (!error || !["ENOENT", "ENOTEMPTY", "EEXIST"].includes(error.code)) throw error;
  }
}

export async function buildDescriptors({
  bufEntry = DEFAULT_BUF_ENTRY,
  env = process.env,
  mode,
  nodeExecutable = process.execPath,
  platform = process.platform,
  renamePath = rename,
  root = process.cwd(),
  run = execFileAsync,
}) {
  if (mode === "validation") {
    const outputDirectory = path.join(root, ".tmp/contracts");
    const { bufVersion, descriptorBytes } = await buildDescriptorIntoDirectory({
      bufEntry,
      mode,
      nodeExecutable,
      outputDirectory,
      platform,
      root,
      run,
    });
    const manifest = createDescriptorManifest({ bufVersion, descriptorBytes });
    validateDescriptorManifest({ descriptorBytes, manifest, mode });
    await writeFile(
      path.join(outputDirectory, "descriptor-manifest.json"),
      `${JSON.stringify(manifest, null, 2)}\n`,
      "utf8",
    );
    return manifest;
  }

  const revisionResult = await run("git", ["rev-parse", "HEAD"], {
    cwd: root,
    shell: false,
  });
  const currentRevision = String(revisionResult.stdout).trim();
  if (typeof env.TAMMY_SOURCE_REVISION !== "string" || env.TAMMY_SOURCE_REVISION === "") {
    throw new Error("DESCRIPTOR_SOURCE_REVISION_REQUIRED");
  }
  if (env.TAMMY_SOURCE_REVISION !== currentRevision) {
    throw new Error("DESCRIPTOR_SOURCE_REVISION_MISMATCH");
  }
  const statusResult = await run("git", ["status", "--porcelain=v1"], {
    cwd: root,
    shell: false,
  });
  if (String(statusResult.stdout).trim().length > 0) {
    throw new Error("DESCRIPTOR_EVIDENCE_DIRTY_TREE");
  }

  const temporaryRoot = path.join(root, ".tmp/contracts");
  await mkdir(temporaryRoot, { recursive: true });
  const transactionDirectory = await mkdtemp(path.join(temporaryRoot, "descriptor-evidence-"));
  const stagingDirectory = path.join(transactionDirectory, "next-contracts");
  const backupDirectory = path.join(transactionDirectory, "previous-contracts");
  const retainedDirectory = path.join(root, "compliance/contracts");
  try {
    const { bufVersion, descriptorBytes } = await buildDescriptorIntoDirectory({
      bufEntry,
      mode,
      nodeExecutable,
      outputDirectory: stagingDirectory,
      platform,
      root,
      run,
    });
    const manifest = createDescriptorManifest({
      bufVersion,
      descriptorBytes,
      gitRevision: env.TAMMY_SOURCE_REVISION,
    });
    validateDescriptorManifest({
      currentRevision,
      descriptorBytes,
      dirty: false,
      manifest,
      mode,
    });
    await assertEvidenceSourceState({ expectedRevision: currentRevision, root, run });
    await writeFile(
      path.join(stagingDirectory, "descriptor-manifest.json"),
      `${JSON.stringify(manifest, null, 2)}\n`,
      "utf8",
    );
    await assertEvidenceSourceState({ expectedRevision: currentRevision, root, run });
    await publishDescriptorEvidence({
      backupDirectory,
      retainedDirectory,
      renamePath,
      stagingDirectory,
    });
    return manifest;
  } finally {
    try {
      await rm(transactionDirectory, { force: true, recursive: true });
    } finally {
      await removeEmptyDirectory(temporaryRoot);
    }
  }
}

export function descriptorMode(args) {
  if (args.length === 1 && args[0] === "--validation") return "validation";
  if (args.length === 1 && args[0] === "--evidence") return "evidence";
  throw new Error("DESCRIPTOR_MODE_REQUIRED");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await buildDescriptors({ mode: descriptorMode(process.argv.slice(2)) });
}
