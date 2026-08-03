import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

export const PINNED_BUF_VERSION = "1.72.0";
export const BUF_MODULE = "buf.build/tammyapp/tammy";
const execFileAsync = promisify(execFile);

export function createDescriptorBuildPlan({ mode, root }) {
  const outputDirectory = path.join(
    root,
    mode === "evidence" ? "compliance/contracts" : ".tmp/contracts",
  );
  return {
    args: [
      "build",
      "--as-file-descriptor-set",
      "--exclude-source-info",
      "--exclude-source-retention-options",
      "--output",
      path.join(outputDirectory, "descriptors.pb"),
    ],
    command: "buf",
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
  if (mode === "validation" && Object.hasOwn(manifest, "gitRevision")) {
    throw new Error("DESCRIPTOR_VALIDATION_REVISION_FORBIDDEN");
  }
  if (manifest.byteLength !== descriptorBytes.byteLength) {
    throw new Error("DESCRIPTOR_MANIFEST_LENGTH_MISMATCH");
  }
  const sha256 = createHash("sha256").update(descriptorBytes).digest("hex");
  if (manifest.sha256 !== sha256) {
    throw new Error("DESCRIPTOR_MANIFEST_SHA256_INVALID");
  }
  if (manifest.bufVersion !== PINNED_BUF_VERSION) {
    throw new Error("DESCRIPTOR_MANIFEST_BUF_VERSION_MISMATCH");
  }
  if (manifest.module !== BUF_MODULE) {
    throw new Error("DESCRIPTOR_MANIFEST_MODULE_MISMATCH");
  }
  if (mode === "evidence" && typeof manifest.gitRevision !== "string") {
    throw new Error("DESCRIPTOR_SOURCE_REVISION_REQUIRED");
  }
  if (mode === "evidence" && manifest.gitRevision !== currentRevision) {
    throw new Error("DESCRIPTOR_SOURCE_REVISION_MISMATCH");
  }
  if (mode === "evidence" && dirty) {
    throw new Error("DESCRIPTOR_EVIDENCE_DIRTY_TREE");
  }
}

export async function buildDescriptors({
  env = process.env,
  mode,
  root = process.cwd(),
  run = execFileAsync,
}) {
  let currentRevision;
  let dirty = false;
  if (mode === "evidence") {
    const revisionResult = await run("git", ["rev-parse", "HEAD"], {
      cwd: root,
      shell: false,
    });
    currentRevision = String(revisionResult.stdout).trim();
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
    dirty = String(statusResult.stdout).trim().length > 0;
    if (dirty) {
      throw new Error("DESCRIPTOR_EVIDENCE_DIRTY_TREE");
    }
  }
  const plan = createDescriptorBuildPlan({ mode, root });
  await mkdir(plan.outputDirectory, { recursive: true });
  await run(plan.command, plan.args, { cwd: root, shell: false });
  const descriptorPath = path.join(plan.outputDirectory, "descriptors.pb");
  const descriptorBytes = await readFile(descriptorPath);
  const { stdout } = await run("buf", ["--version"], {
    cwd: root,
    shell: false,
  });
  const manifest = {
    path: "descriptors.pb",
    byteLength: descriptorBytes.byteLength,
    sha256: createHash("sha256").update(descriptorBytes).digest("hex"),
    bufVersion: String(stdout).trim(),
    module: BUF_MODULE,
  };
  if (mode === "evidence") {
    manifest.gitRevision = env.TAMMY_SOURCE_REVISION;
  }
  validateDescriptorManifest({
    currentRevision,
    descriptorBytes,
    dirty,
    manifest,
    mode,
  });
  await writeFile(
    path.join(plan.outputDirectory, "descriptor-manifest.json"),
    `${JSON.stringify(manifest, null, 2)}\n`,
    "utf8",
  );
  return manifest;
}

export function descriptorMode(args) {
  if (args.length === 1 && args[0] === "--validation") return "validation";
  if (args.length === 1 && args[0] === "--evidence") return "evidence";
  throw new Error("DESCRIPTOR_MODE_REQUIRED");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await buildDescriptors({ mode: descriptorMode(process.argv.slice(2)) });
}
