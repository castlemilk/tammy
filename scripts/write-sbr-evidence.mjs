import { execFile } from "node:child_process";
import { chmod, lstat, readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const RESULT_KEYS = [
  "schema",
  "source_revision",
  "profile_sha256",
  "helper_sha256",
  "fixture_sha256",
  "socket_samples",
  "socket_violations",
  "core_path_verified",
  "helper_path_verified",
  "core_orphans",
  "helper_orphans",
  "playwright_status",
  "recorded_at",
];
const HASH = /^[0-9a-f]{64}$/u;
const REVISION = /^[0-9a-f]{40}$/u;
const MAX_RESULT_BYTES = 4_096;
const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

async function gitRevision(root) {
  const { stdout } = await execFileAsync("git", ["rev-parse", "HEAD"], {
    cwd: root,
    encoding: "utf8",
    maxBuffer: 128,
  });
  return stdout.trim();
}

function validResult(value, expectedRevision) {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const keys = Object.keys(value);
  const recorded =
    typeof value.recorded_at === "string" ? new Date(value.recorded_at).getTime() : Number.NaN;
  return (
    keys.length === RESULT_KEYS.length &&
    RESULT_KEYS.every((key) => Object.hasOwn(value, key)) &&
    value.schema === "tammy-sbr-e2e-result-v1" &&
    typeof value.source_revision === "string" &&
    REVISION.test(value.source_revision) &&
    value.source_revision === expectedRevision &&
    [value.profile_sha256, value.helper_sha256, value.fixture_sha256].every(
      (hash) => typeof hash === "string" && HASH.test(hash),
    ) &&
    Number.isSafeInteger(value.socket_samples) &&
    value.socket_samples >= 1 &&
    value.socket_violations === 0 &&
    value.core_path_verified === true &&
    value.helper_path_verified === true &&
    value.core_orphans === 0 &&
    value.helper_orphans === 0 &&
    value.playwright_status === "PASSED" &&
    Number.isFinite(recorded) &&
    value.recorded_at === new Date(recorded).toISOString()
  );
}

function evidenceBundle(result, exportedAt) {
  return Object.freeze({
    schema: "tammy-sbr-evidence-bundle-v1",
    source_revision: result.source_revision,
    profile_sha256: result.profile_sha256,
    helper_sha256: result.helper_sha256,
    fixture_sha256: result.fixture_sha256,
    zero_socket_observation: Object.freeze({
      samples: result.socket_samples,
      violations: result.socket_violations,
    }),
    process_observation: Object.freeze({
      core_path_verified: result.core_path_verified,
      helper_path_verified: result.helper_path_verified,
      core_orphans: result.core_orphans,
      helper_orphans: result.helper_orphans,
    }),
    playwright_status: result.playwright_status,
    recorded_at: result.recorded_at,
    exported_at: exportedAt,
    environment: "SIMULATOR",
    ato_lodgment: "UNAVAILABLE",
  });
}

export async function consumePassedSbrEvidence({
  currentRevision,
  now = new Date(),
  repositoryRoot: root = repositoryRoot,
} = {}) {
  const resultPath = path.join(root, ".tmp", "sbr-e2e", "latest", "result.json");
  const evidencePath = path.join(root, ".tmp", "sbr-e2e", "latest", "evidence.json");
  await rm(evidencePath, { force: true });
  let temporary;
  try {
    if (!path.isAbsolute(root)) throw new Error("invalid root");
    const metadata = await lstat(resultPath);
    if (
      !metadata.isFile() ||
      metadata.isSymbolicLink() ||
      (metadata.mode & 0o777) !== 0o600 ||
      metadata.size < 1 ||
      metadata.size > MAX_RESULT_BYTES
    ) {
      throw new Error("invalid result");
    }
    const raw = await readFile(resultPath, "utf8");
    if (Buffer.byteLength(raw) !== metadata.size) throw new Error("changed result");
    let result;
    try {
      result = JSON.parse(raw);
    } catch {
      throw new Error("invalid JSON");
    }
    const revision = await (currentRevision ?? (() => gitRevision(root)))();
    if (!REVISION.test(revision) || !validResult(result, revision)) {
      throw new Error("invalid result");
    }
    const exportedAt = now.toISOString();
    if (exportedAt !== new Date(exportedAt).toISOString()) throw new Error("invalid clock");
    const bundle = evidenceBundle(result, exportedAt);
    const output = `${JSON.stringify(bundle)}\n`;
    if (Buffer.byteLength(output) >= MAX_RESULT_BYTES) throw new Error("invalid output");
    temporary = path.join(path.dirname(evidencePath), `.evidence-${process.pid}.tmp`);
    await rm(temporary, { force: true });
    await writeFile(temporary, output, { flag: "wx", mode: 0o600 });
    await chmod(temporary, 0o600);
    await rename(temporary, evidencePath);
    return bundle;
  } catch {
    if (temporary) await rm(temporary, { force: true });
    await rm(evidencePath, { force: true });
    throw new Error("SBR_EVIDENCE_RESULT_INVALID");
  }
}

async function main() {
  if (process.argv.length !== 2) throw new Error("SBR_EVIDENCE_ARGUMENTS_INVALID");
  const bundle = await consumePassedSbrEvidence();
  process.stdout.write(`${JSON.stringify(bundle)}\n`);
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  main().catch((error) => {
    process.stderr.write(`${error instanceof Error ? error.message : "SBR_EVIDENCE_FAILED"}\n`);
    process.exitCode = 1;
  });
}
