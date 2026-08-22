import assert from "node:assert/strict";
import test from "node:test";

import { validateBuildManifest } from "./build-manifest-schema.mjs";

const hashes = {
  helper_sha256: "2".repeat(64),
  profile_sha256: "3".repeat(64),
  profile_signature_sha256: "4".repeat(64),
  source_tree_sha256: "5".repeat(64),
};

function manifest(target = "darwin-arm64") {
  return {
    schema: "tammy-build-manifest-v1",
    source_revision: "a".repeat(40),
    source_dirty: false,
    target,
    versions: {
      buf: "1.72.0",
      connect_es: "2.1.2",
      connect_go: "1.20.0",
      electron: "43.1.1",
      go: "1.26.4",
      node: "24.18.0",
      playwright: "1.61.1",
      pnpm: "11.15.0",
      protobuf_es: "2.12.1",
      protobuf_go: "1.36.11",
      react: "19.2.7",
      shadcn: "4.13.1",
      tailwindcss: "4.3.3",
      typescript: "7.0.2",
      vite: "8.1.5",
      vitest: "4.1.10",
    },
    lockfiles: { "pnpm-lock.yaml": "b".repeat(64), "services/core/go.sum": "c".repeat(64) },
    protobuf_tree_sha256: "d".repeat(64),
    core_sha256: "e".repeat(64),
    sqlcipher: {
      library_sha256: "f".repeat(64),
      runtime_version: "4.15.0 community",
      version: "4.15.0",
    },
    test_profile: "foundation-packaged-e2e",
    sbr_status: target === "darwin-arm64" ? "SIMULATOR_ENABLED" : "SBR_UNAVAILABLE_ON_TARGET",
    sbr:
      target === "darwin-arm64"
        ? hashes
        : Object.fromEntries(Object.keys(hashes).map((key) => [key, null])),
    signed: false,
  };
}

test("accepts exact Darwin SBR provenance and exact Windows unavailability", () => {
  assert.equal(
    validateBuildManifest(manifest(), { expectedTarget: "darwin-arm64", requireClean: true }).sbr
      .helper_sha256,
    hashes.helper_sha256,
  );
  assert.equal(
    validateBuildManifest(manifest("win32-x64"), {
      expectedTarget: "win32-x64",
      requireClean: true,
    }).sbr_status,
    "SBR_UNAVAILABLE_ON_TARGET",
  );
});

test("rejects authorization claims, unknown fields, secrets, and mismatched status", () => {
  for (const mutate of [
    (value) => {
      value.sbr_status = "SBR_APPROVED";
    },
    (value) => {
      value.sbr.authorized = true;
    },
    (value) => {
      value.sbr.helper_sha256 = null;
    },
    (value) => {
      value.sbr.helper_raw_sha256 = "1".repeat(64);
    },
    (value) => {
      value.machine_credential = "secret";
    },
  ]) {
    const value = manifest();
    mutate(value);
    assert.throws(
      () => validateBuildManifest(value, { expectedTarget: "darwin-arm64", requireClean: true }),
      /BUILD_MANIFEST_SCHEMA_INVALID/,
    );
  }
});
