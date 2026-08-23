import { chmod, mkdir, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";

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
] as const;
const HASH = /^[0-9a-f]{64}$/;
const REVISION = /^[0-9a-f]{40}$/;
const MAX_TIMESTAMP_DRIFT_MS = 5 * 60 * 1000;

export interface SbrE2ePassedResult {
  readonly schema: "tammy-sbr-e2e-result-v1";
  readonly source_revision: string;
  readonly profile_sha256: string;
  readonly helper_sha256: string;
  readonly fixture_sha256: string;
  readonly socket_samples: number;
  readonly socket_violations: 0;
  readonly core_path_verified: true;
  readonly helper_path_verified: true;
  readonly core_orphans: 0;
  readonly helper_orphans: 0;
  readonly playwright_status: "PASSED";
  readonly recorded_at: string;
}

export async function removeSbrE2eResult(destination: string): Promise<void> {
  await rm(destination, { force: true });
}

function validResult(
  value: Readonly<Record<string, unknown>>,
  expectedRevision: string,
  now: Date,
): boolean {
  const recorded =
    typeof value.recorded_at === "string" ? new Date(value.recorded_at).getTime() : Number.NaN;
  return (
    Object.keys(value).length === RESULT_KEYS.length &&
    RESULT_KEYS.every((key) => Object.hasOwn(value, key)) &&
    value.schema === "tammy-sbr-e2e-result-v1" &&
    typeof value.source_revision === "string" &&
    REVISION.test(value.source_revision) &&
    value.source_revision === expectedRevision &&
    [value.profile_sha256, value.helper_sha256, value.fixture_sha256].every(
      (hash) => typeof hash === "string" && HASH.test(hash),
    ) &&
    Number.isSafeInteger(value.socket_samples) &&
    Number(value.socket_samples) >= 1 &&
    value.socket_violations === 0 &&
    value.core_path_verified === true &&
    value.helper_path_verified === true &&
    value.core_orphans === 0 &&
    value.helper_orphans === 0 &&
    value.playwright_status === "PASSED" &&
    Number.isFinite(recorded) &&
    value.recorded_at === new Date(recorded).toISOString() &&
    Math.abs(now.getTime() - recorded) <= MAX_TIMESTAMP_DRIFT_MS
  );
}

export async function writePassedSbrE2eResult(
  destination: string,
  value: Readonly<Record<string, unknown>>,
  options: { readonly expectedRevision: string; readonly now?: Date },
): Promise<void> {
  await removeSbrE2eResult(destination);
  const now = options.now ?? new Date();
  if (
    !REVISION.test(options.expectedRevision) ||
    !validResult(value, options.expectedRevision, now)
  ) {
    throw new Error("SBR_E2E_RESULT_INVALID");
  }
  await mkdir(path.dirname(destination), { recursive: true });
  const temporary = path.join(path.dirname(destination), `.result-${process.pid}.tmp`);
  try {
    await writeFile(temporary, `${JSON.stringify(value)}\n`, { flag: "wx", mode: 0o600 });
    await chmod(temporary, 0o600);
    await rename(temporary, destination);
  } catch (error) {
    await rm(temporary, { force: true });
    await removeSbrE2eResult(destination);
    throw error;
  }
}
