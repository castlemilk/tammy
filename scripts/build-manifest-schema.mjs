export const BUILD_MANIFEST_KEYS = Object.freeze([
  "schema",
  "source_revision",
  "source_dirty",
  "target",
  "versions",
  "lockfiles",
  "protobuf_tree_sha256",
  "core_sha256",
  "test_profile",
  "sbr_status",
  "signed",
]);

export const BUILD_MANIFEST_VERSION_KEYS = Object.freeze([
  "buf",
  "connect_es",
  "connect_go",
  "electron",
  "go",
  "node",
  "playwright",
  "pnpm",
  "protobuf_es",
  "protobuf_go",
  "react",
  "shadcn",
  "tailwindcss",
  "typescript",
  "vite",
  "vitest",
]);

export const BUILD_MANIFEST_LOCKFILE_KEYS = Object.freeze([
  "pnpm-lock.yaml",
  "services/core/go.sum",
]);

const FORBIDDEN_FIELD_PATTERN = /credential|secret|token|password|environment|(^|_)env($|_)/i;
const HASH_PATTERN = /^[0-9a-f]{64}$/;
const REVISION_PATTERN = /^[0-9a-f]{40}$/;
const VERSION_PATTERN = /^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$/;

function fail() {
  throw new Error("BUILD_MANIFEST_SCHEMA_INVALID");
}

function isPlainRecord(value) {
  return (
    value !== null &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    Object.getPrototypeOf(value) === Object.prototype
  );
}

function hasExactOrderedKeys(value, expected) {
  if (!isPlainRecord(value)) return false;
  const keys = Object.keys(value);
  return keys.length === expected.length && keys.every((key, index) => key === expected[index]);
}

function assertNoForbiddenFields(value) {
  if (!isPlainRecord(value)) return;
  for (const [key, child] of Object.entries(value)) {
    if (FORBIDDEN_FIELD_PATTERN.test(key)) fail();
    assertNoForbiddenFields(child);
  }
}

export function validateBuildManifest(manifest, { expectedTarget, requireClean }) {
  assertNoForbiddenFields(manifest);
  if (
    !hasExactOrderedKeys(manifest, BUILD_MANIFEST_KEYS) ||
    manifest.schema !== "tammy-build-manifest-v1" ||
    typeof manifest.source_revision !== "string" ||
    !REVISION_PATTERN.test(manifest.source_revision) ||
    typeof manifest.source_dirty !== "boolean" ||
    (requireClean && manifest.source_dirty !== false) ||
    manifest.target !== expectedTarget ||
    !hasExactOrderedKeys(manifest.versions, BUILD_MANIFEST_VERSION_KEYS) ||
    Object.values(manifest.versions).some(
      (version) => typeof version !== "string" || !VERSION_PATTERN.test(version),
    ) ||
    !hasExactOrderedKeys(manifest.lockfiles, BUILD_MANIFEST_LOCKFILE_KEYS) ||
    Object.values(manifest.lockfiles).some(
      (hash) => typeof hash !== "string" || !HASH_PATTERN.test(hash),
    ) ||
    typeof manifest.protobuf_tree_sha256 !== "string" ||
    !HASH_PATTERN.test(manifest.protobuf_tree_sha256) ||
    typeof manifest.core_sha256 !== "string" ||
    !HASH_PATTERN.test(manifest.core_sha256) ||
    manifest.test_profile !== "foundation-packaged-e2e" ||
    manifest.sbr_status !== "SIMULATOR_NOT_IMPLEMENTED" ||
    manifest.signed !== false
  ) {
    fail();
  }
  return manifest;
}

export function parseCanonicalBuildManifest(bytes, expectedTarget) {
  if (!Buffer.isBuffer(bytes)) fail();
  const json = bytes.toString("utf8");
  if (!Buffer.from(json, "utf8").equals(bytes)) fail();
  let manifest;
  try {
    manifest = JSON.parse(json);
  } catch {
    fail();
  }
  validateBuildManifest(manifest, {
    expectedTarget,
    requireClean: true,
  });
  const canonical = Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`);
  if (!canonical.equals(bytes)) fail();
  return manifest;
}
