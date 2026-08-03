import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { access, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

const DESCRIPTOR_BYTES = Buffer.from("descriptor bytes");
const REVISION = "0123456789abcdef0123456789abcdef01234567";

function validManifest(overrides = {}) {
  return {
    path: "descriptors.pb",
    byteLength: DESCRIPTOR_BYTES.byteLength,
    sha256: createHash("sha256").update(DESCRIPTOR_BYTES).digest("hex"),
    bufVersion: "1.72.0",
    module: "buf.build/tammyapp/tammy",
    gitRevision: REVISION,
    ...overrides,
  };
}

test("canonical descriptor command omits source information and retention options", async () => {
  const { createDescriptorBuildPlan } = await import("./build-descriptors.mjs");
  const root = path.resolve("/workspace/tammy");

  assert.deepEqual(createDescriptorBuildPlan({ mode: "evidence", root }), {
    args: [
      "build",
      "--as-file-descriptor-set",
      "--exclude-source-info",
      "--exclude-source-retention-options",
      "--output",
      path.join(root, "compliance/contracts/descriptors.pb"),
    ],
    command: "buf",
    outputDirectory: path.join(root, "compliance/contracts"),
  });
});

test("validation mode omits source revision", async () => {
  const { validateDescriptorManifest } = await import("./build-descriptors.mjs");

  assert.throws(
    () =>
      validateDescriptorManifest({
        descriptorBytes: DESCRIPTOR_BYTES,
        manifest: validManifest(),
        mode: "validation",
      }),
    { message: "DESCRIPTOR_VALIDATION_REVISION_FORBIDDEN" },
  );
});

test("real descriptor byte length", async () => {
  const { validateDescriptorManifest } = await import("./build-descriptors.mjs");
  const { gitRevision: _gitRevision, ...manifest } = validManifest({
    byteLength: DESCRIPTOR_BYTES.byteLength + 1,
  });

  assert.throws(
    () =>
      validateDescriptorManifest({
        descriptorBytes: DESCRIPTOR_BYTES,
        manifest,
        mode: "validation",
      }),
    { message: "DESCRIPTOR_MANIFEST_LENGTH_MISMATCH" },
  );
});

test("lowercase descriptor sha256", async () => {
  const { validateDescriptorManifest } = await import("./build-descriptors.mjs");
  const { gitRevision: _gitRevision, ...manifest } = validManifest({
    sha256: validManifest().sha256.toUpperCase(),
  });

  assert.throws(
    () =>
      validateDescriptorManifest({
        descriptorBytes: DESCRIPTOR_BYTES,
        manifest,
        mode: "validation",
      }),
    { message: "DESCRIPTOR_MANIFEST_SHA256_INVALID" },
  );
});

test("pinned buf version", async () => {
  const { validateDescriptorManifest } = await import("./build-descriptors.mjs");
  const { gitRevision: _gitRevision, ...manifest } = validManifest({
    bufVersion: "1.71.0",
  });

  assert.throws(
    () =>
      validateDescriptorManifest({
        descriptorBytes: DESCRIPTOR_BYTES,
        manifest,
        mode: "validation",
      }),
    { message: "DESCRIPTOR_MANIFEST_BUF_VERSION_MISMATCH" },
  );
});

test("exact buf module", async () => {
  const { validateDescriptorManifest } = await import("./build-descriptors.mjs");
  const { gitRevision: _gitRevision, ...manifest } = validManifest({
    module: "buf.build/tammyapp/other",
  });

  assert.throws(
    () =>
      validateDescriptorManifest({
        descriptorBytes: DESCRIPTOR_BYTES,
        manifest,
        mode: "validation",
      }),
    { message: "DESCRIPTOR_MANIFEST_MODULE_MISMATCH" },
  );
});

test("missing source revision", async () => {
  const { validateDescriptorManifest } = await import("./build-descriptors.mjs");
  const { gitRevision: _gitRevision, ...manifest } = validManifest();

  assert.throws(
    () =>
      validateDescriptorManifest({
        currentRevision: REVISION,
        descriptorBytes: DESCRIPTOR_BYTES,
        dirty: false,
        manifest,
        mode: "evidence",
      }),
    { message: "DESCRIPTOR_SOURCE_REVISION_REQUIRED" },
  );
});

test("mismatched source revision", async () => {
  const { validateDescriptorManifest } = await import("./build-descriptors.mjs");

  assert.throws(
    () =>
      validateDescriptorManifest({
        currentRevision: "abcdef0123456789abcdef0123456789abcdef01",
        descriptorBytes: DESCRIPTOR_BYTES,
        dirty: false,
        manifest: validManifest(),
        mode: "evidence",
      }),
    { message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH" },
  );
});

test("dirty evidence tree", async () => {
  const { validateDescriptorManifest } = await import("./build-descriptors.mjs");

  assert.throws(
    () =>
      validateDescriptorManifest({
        currentRevision: REVISION,
        descriptorBytes: DESCRIPTOR_BYTES,
        dirty: true,
        manifest: validManifest(),
        mode: "evidence",
      }),
    { message: "DESCRIPTOR_EVIDENCE_DIRTY_TREE" },
  );
});

test("validation build writes temporary descriptor and manifest without Git", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-descriptors-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  const calls = [];
  const run = async (command, args) => {
    calls.push([command, ...args]);
    if (args[0] === "build") {
      await writeFile(args.at(-1), DESCRIPTOR_BYTES);
      return { stdout: "" };
    }
    return { stdout: "1.72.0\n" };
  };
  const { buildDescriptors } = await import("./build-descriptors.mjs");

  await buildDescriptors({ mode: "validation", root, run });

  assert.deepEqual(
    calls.map(([command, first]) => [command, first]),
    [
      ["buf", "build"],
      ["buf", "--version"],
    ],
  );
  assert.deepEqual(
    JSON.parse(await readFile(path.join(root, ".tmp/contracts/descriptor-manifest.json"), "utf8")),
    {
      path: "descriptors.pb",
      byteLength: DESCRIPTOR_BYTES.byteLength,
      sha256: createHash("sha256").update(DESCRIPTOR_BYTES).digest("hex"),
      bufVersion: "1.72.0",
      module: "buf.build/tammyapp/tammy",
    },
  );
});

test("evidence rejects a dirty tree before writing output", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-descriptors-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  const calls = [];
  const run = async (command, args) => {
    calls.push([command, ...args]);
    if (args[0] === "rev-parse") return { stdout: `${REVISION}\n` };
    if (args[0] === "status") return { stdout: " M proto/tammy/v1/system.proto\n" };
    throw new Error("unexpected build");
  };
  const { buildDescriptors } = await import("./build-descriptors.mjs");

  await assert.rejects(
    buildDescriptors({
      env: { TAMMY_SOURCE_REVISION: REVISION },
      mode: "evidence",
      root,
      run,
    }),
    { message: "DESCRIPTOR_EVIDENCE_DIRTY_TREE" },
  );
  assert.deepEqual(
    calls.map(([command, first]) => [command, first]),
    [
      ["git", "rev-parse"],
      ["git", "status"],
    ],
  );
  await assert.rejects(access(path.join(root, "compliance/contracts/descriptors.pb")));
});

test("descriptor command accepts only validation and evidence modes", async () => {
  const { descriptorMode } = await import("./build-descriptors.mjs");

  assert.equal(descriptorMode(["--validation"]), "validation");
  assert.equal(descriptorMode(["--evidence"]), "evidence");
  assert.throws(() => descriptorMode([]), { message: "DESCRIPTOR_MODE_REQUIRED" });
});
