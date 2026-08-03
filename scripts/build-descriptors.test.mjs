import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { access, mkdir, mkdtemp, readdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { create, toBinary } from "@bufbuild/protobuf";
import { FileDescriptorSetSchema } from "@bufbuild/protobuf/wkt";

const DESCRIPTOR_BYTES = Buffer.from(
  toBinary(
    FileDescriptorSetSchema,
    create(FileDescriptorSetSchema, {
      file: [{ name: "tammy/v1/system.proto", package: "tammy.v1" }],
    }),
  ),
);
const NEXT_DESCRIPTOR_BYTES = Buffer.from(
  toBinary(
    FileDescriptorSetSchema,
    create(FileDescriptorSetSchema, {
      file: [
        {
          name: "tammy/v1/system.proto",
          package: "tammy.v1",
          service: [{ name: "SystemService", method: [{ name: "GetDiagnostics" }] }],
        },
      ],
    }),
  ),
);
const PARTIAL_DESCRIPTOR_BYTES = Buffer.from([0x0a, 0x05, 0x01]);
const REVISION = "0123456789abcdef0123456789abcdef01234567";
const NEXT_REVISION = "abcdef0123456789abcdef0123456789abcdef01";
const NODE_EXECUTABLE = "/mise/installs/node/24.18.0/bin/node";
const BUF_ENTRY = "/workspace/tammy/node_modules/@bufbuild/buf/bin/buf";

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

async function writeRetainedEvidence(root) {
  const retainedDirectory = path.join(root, "compliance/contracts");
  await mkdir(retainedDirectory, { recursive: true });
  await writeFile(path.join(retainedDirectory, "descriptors.pb"), DESCRIPTOR_BYTES);
  await writeFile(
    path.join(retainedDirectory, "descriptor-manifest.json"),
    `${JSON.stringify(validManifest(), null, 2)}\n`,
  );
}

async function retainedEvidence(root) {
  const retainedDirectory = path.join(root, "compliance/contracts");
  return {
    descriptor: await readFile(path.join(retainedDirectory, "descriptors.pb")),
    manifest: await readFile(path.join(retainedDirectory, "descriptor-manifest.json")),
  };
}

async function assertRetainedEvidenceUnchanged(root, before) {
  const after = await retainedEvidence(root);
  assert.deepEqual(after.descriptor, before.descriptor);
  assert.deepEqual(after.manifest, before.manifest);
  assert.deepEqual(await readdir(path.join(root, ".tmp")), []);
}

function createEvidenceRun({
  buildBytes = NEXT_DESCRIPTOR_BYTES,
  buildError,
  revisions = [REVISION, REVISION, REVISION],
  statuses = ["", "", ""],
  version = "1.72.0",
} = {}) {
  let revisionIndex = 0;
  let statusIndex = 0;
  const calls = [];
  const run = async (command, args, options) => {
    calls.push({ args, command, options });
    if (command === "git" && args[0] === "rev-parse") {
      const revision = revisions[Math.min(revisionIndex, revisions.length - 1)];
      revisionIndex += 1;
      return { stdout: `${revision}\n` };
    }
    if (command === "git" && args[0] === "status") {
      const status = statuses[Math.min(statusIndex, statuses.length - 1)];
      statusIndex += 1;
      return { stdout: status };
    }
    if (command === NODE_EXECUTABLE && args[0] === BUF_ENTRY && args[1] === "build") {
      if (buildBytes !== undefined) await writeFile(args.at(-1), buildBytes);
      if (buildError) throw buildError;
      return { stdout: "" };
    }
    if (command === NODE_EXECUTABLE && args[0] === BUF_ENTRY && args[1] === "--version") {
      return { stdout: `${version}\n` };
    }
    throw new Error(`unexpected command: ${command} ${args.join(" ")}`);
  };
  return { calls, run };
}

test("creates a Darwin Buf command plan through Node without a shell", async () => {
  const { createBufCommandPlan } = await import("./build-descriptors.mjs");
  const nodeExecutable = "/mise/installs/node/24.18.0/bin/node";
  const bufEntry = "/workspace/tammy/node_modules/@bufbuild/buf/bin/buf";

  assert.deepEqual(
    createBufCommandPlan({
      args: ["build", "--output", "/tmp/descriptors.pb"],
      bufEntry,
      nodeExecutable,
      platform: "darwin",
    }),
    {
      args: [bufEntry, "build", "--output", "/tmp/descriptors.pb"],
      command: nodeExecutable,
      shell: false,
    },
  );
});

test("creates a win32 Buf command plan without cmd or bat shims", async () => {
  const { createBufCommandPlan } = await import("./build-descriptors.mjs");
  const nodeExecutable = "C:\\mise\\node\\node.exe";
  const bufEntry = "C:\\repo\\node_modules\\@bufbuild\\buf\\bin\\buf";
  const plan = createBufCommandPlan({
    args: ["--version"],
    bufEntry,
    nodeExecutable,
    platform: "win32",
  });

  assert.deepEqual(plan, {
    args: [bufEntry, "--version"],
    command: nodeExecutable,
    shell: false,
  });
  assert.doesNotMatch(plan.command, /\.(?:cmd|bat)$/i);
  assert.equal(
    plan.args.some((argument) => /\.(?:cmd|bat)$/i.test(argument)),
    false,
  );
});

test("canonical descriptor command omits source information and retention options", async () => {
  const { createDescriptorBuildPlan } = await import("./build-descriptors.mjs");
  const root = path.resolve("/workspace/tammy");
  const nodeExecutable = "/mise/installs/node/24.18.0/bin/node";
  const bufEntry = "/workspace/tammy/node_modules/@bufbuild/buf/bin/buf";

  assert.deepEqual(
    createDescriptorBuildPlan({
      bufEntry,
      mode: "evidence",
      nodeExecutable,
      platform: "darwin",
      root,
    }),
    {
      args: [
        bufEntry,
        "build",
        "--as-file-descriptor-set",
        "--exclude-source-info",
        "--exclude-source-retention-options",
        "--output",
        path.join(root, "compliance/contracts/descriptors.pb"),
      ],
      command: nodeExecutable,
      outputDirectory: path.join(root, "compliance/contracts"),
      shell: false,
    },
  );
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

test("retained descriptor manifest enforces the exact six-field schema", async () => {
  const { validateDescriptorManifest } = await import("./build-descriptors.mjs");
  const invalidManifests = [
    { ...validManifest(), extra: true },
    Object.fromEntries(Object.entries(validManifest()).filter(([field]) => field !== "path")),
    validManifest({ byteLength: String(DESCRIPTOR_BYTES.byteLength) }),
    validManifest({ sha256: 42 }),
    validManifest({ bufVersion: null }),
    validManifest({ module: [] }),
    validManifest({ gitRevision: { revision: REVISION } }),
  ];

  for (const manifest of invalidManifests) {
    assert.throws(
      () =>
        validateDescriptorManifest({
          currentRevision: REVISION,
          descriptorBytes: DESCRIPTOR_BYTES,
          dirty: false,
          manifest,
          mode: "evidence",
        }),
      { message: "DESCRIPTOR_MANIFEST_SCHEMA_INVALID" },
    );
  }
});

test("retained descriptor manifest requires the canonical relative path", async () => {
  const { validateDescriptorManifest } = await import("./build-descriptors.mjs");

  assert.throws(
    () =>
      validateDescriptorManifest({
        currentRevision: REVISION,
        descriptorBytes: DESCRIPTOR_BYTES,
        dirty: false,
        manifest: validManifest({ path: "../descriptors.pb" }),
        mode: "evidence",
      }),
    { message: "DESCRIPTOR_MANIFEST_PATH_INVALID" },
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
  const nodeExecutable = "/mise/installs/node/24.18.0/bin/node";
  const bufEntry = "/workspace/tammy/node_modules/@bufbuild/buf/bin/buf";
  const calls = [];
  const run = async (command, args, options) => {
    calls.push({ args, command, options });
    if (args[1] === "build") {
      await writeFile(args.at(-1), DESCRIPTOR_BYTES);
      return { stdout: "" };
    }
    return { stdout: "1.72.0\n" };
  };
  const { buildDescriptors } = await import("./build-descriptors.mjs");

  await buildDescriptors({
    bufEntry,
    mode: "validation",
    nodeExecutable,
    platform: "darwin",
    root,
    run,
  });

  assert.deepEqual(
    calls.map(({ args, command, options }) => ({
      command,
      first: args[0],
      second: args[1],
      shell: options.shell,
    })),
    [
      { command: nodeExecutable, first: bufEntry, second: "build", shell: false },
      { command: nodeExecutable, first: bufEntry, second: "--version", shell: false },
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

test("Buf failure leaves retained evidence unchanged and removes staging", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-descriptors-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const before = await retainedEvidence(root);
  const { run } = createEvidenceRun({ buildError: new Error("BUF_BUILD_FAILED") });
  const { buildDescriptors } = await import("./build-descriptors.mjs");

  await assert.rejects(
    buildDescriptors({
      bufEntry: BUF_ENTRY,
      env: { TAMMY_SOURCE_REVISION: REVISION },
      mode: "evidence",
      nodeExecutable: NODE_EXECUTABLE,
      platform: "darwin",
      root,
      run,
    }),
    { message: "BUF_BUILD_FAILED" },
  );
  await assertRetainedEvidenceUnchanged(root, before);
});

test("partial staged descriptor output is rejected without changing retained evidence", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-descriptors-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const before = await retainedEvidence(root);
  const { run } = createEvidenceRun({ buildBytes: PARTIAL_DESCRIPTOR_BYTES });
  const { buildDescriptors } = await import("./build-descriptors.mjs");

  await assert.rejects(
    buildDescriptors({
      bufEntry: BUF_ENTRY,
      env: { TAMMY_SOURCE_REVISION: REVISION },
      mode: "evidence",
      nodeExecutable: NODE_EXECUTABLE,
      platform: "darwin",
      root,
      run,
    }),
    { message: "DESCRIPTOR_BUILD_OUTPUT_INVALID" },
  );
  await assertRetainedEvidenceUnchanged(root, before);
});

test("Buf version mismatch leaves retained evidence unchanged and removes staging", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-descriptors-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const before = await retainedEvidence(root);
  const { run } = createEvidenceRun({ version: "1.71.0" });
  const { buildDescriptors } = await import("./build-descriptors.mjs");

  await assert.rejects(
    buildDescriptors({
      bufEntry: BUF_ENTRY,
      env: { TAMMY_SOURCE_REVISION: REVISION },
      mode: "evidence",
      nodeExecutable: NODE_EXECUTABLE,
      platform: "darwin",
      root,
      run,
    }),
    { message: "DESCRIPTOR_MANIFEST_BUF_VERSION_MISMATCH" },
  );
  await assertRetainedEvidenceUnchanged(root, before);
});

test("source mutation after build leaves retained evidence unchanged", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-descriptors-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const before = await retainedEvidence(root);
  const { run } = createEvidenceRun({ revisions: [REVISION, NEXT_REVISION] });
  const { buildDescriptors } = await import("./build-descriptors.mjs");

  await assert.rejects(
    buildDescriptors({
      bufEntry: BUF_ENTRY,
      env: { TAMMY_SOURCE_REVISION: REVISION },
      mode: "evidence",
      nodeExecutable: NODE_EXECUTABLE,
      platform: "darwin",
      root,
      run,
    }),
    { message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH" },
  );
  await assertRetainedEvidenceUnchanged(root, before);
});

test("dirty mutation immediately before publish leaves retained evidence unchanged", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-descriptors-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const before = await retainedEvidence(root);
  const { run } = createEvidenceRun({ statuses: ["", "", " M proto/tammy/v1/system.proto\n"] });
  const { buildDescriptors } = await import("./build-descriptors.mjs");

  await assert.rejects(
    buildDescriptors({
      bufEntry: BUF_ENTRY,
      env: { TAMMY_SOURCE_REVISION: REVISION },
      mode: "evidence",
      nodeExecutable: NODE_EXECUTABLE,
      platform: "darwin",
      root,
      run,
    }),
    { message: "DESCRIPTOR_EVIDENCE_DIRTY_TREE" },
  );
  await assertRetainedEvidenceUnchanged(root, before);
});

test("publication failure rolls back the complete retained pair and removes staging", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-descriptors-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const before = await retainedEvidence(root);
  const { run } = createEvidenceRun();
  let renameCount = 0;
  const renamePath = async (source, destination) => {
    renameCount += 1;
    if (renameCount === 2) throw new Error("INJECTED_PUBLICATION_FAILURE");
    await rename(source, destination);
  };
  const { buildDescriptors } = await import("./build-descriptors.mjs");

  await assert.rejects(
    buildDescriptors({
      bufEntry: BUF_ENTRY,
      env: { TAMMY_SOURCE_REVISION: REVISION },
      mode: "evidence",
      nodeExecutable: NODE_EXECUTABLE,
      platform: "darwin",
      renamePath,
      root,
      run,
    }),
    (error) => {
      assert.equal(error.message, "DESCRIPTOR_EVIDENCE_PUBLISH_FAILED");
      assert.equal(error.cause?.message, "INJECTED_PUBLICATION_FAILURE");
      return true;
    },
  );
  assert.equal(renameCount, 3);
  await assertRetainedEvidenceUnchanged(root, before);
});

test("evidence publishes one validated pair after three clean source checks", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-descriptors-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const { calls, run } = createEvidenceRun();
  const { buildDescriptors } = await import("./build-descriptors.mjs");

  const manifest = await buildDescriptors({
    bufEntry: BUF_ENTRY,
    env: { TAMMY_SOURCE_REVISION: REVISION },
    mode: "evidence",
    nodeExecutable: NODE_EXECUTABLE,
    platform: "darwin",
    root,
    run,
  });

  assert.deepEqual(
    await readFile(path.join(root, "compliance/contracts/descriptors.pb")),
    NEXT_DESCRIPTOR_BYTES,
  );
  assert.deepEqual(
    JSON.parse(
      await readFile(path.join(root, "compliance/contracts/descriptor-manifest.json"), "utf8"),
    ),
    manifest,
  );
  assert.equal(
    calls.filter(({ command, args }) => command === "git" && args[0] === "rev-parse").length,
    3,
  );
  assert.equal(
    calls.filter(({ command, args }) => command === "git" && args[0] === "status").length,
    3,
  );
  const buildCall = calls.find(
    ({ command, args }) =>
      command === NODE_EXECUTABLE && args[0] === BUF_ENTRY && args[1] === "build",
  );
  const stagedOutputParts = path.relative(root, buildCall.args.at(-1)).split(path.sep);
  assert.deepEqual(stagedOutputParts.slice(0, 2), [".tmp", "contracts"]);
  assert.match(stagedOutputParts[2], /^descriptor-evidence-/);
  assert.deepEqual(stagedOutputParts.slice(3), ["next-contracts", "descriptors.pb"]);
  assert.deepEqual(await readdir(path.join(root, ".tmp")), []);
});

test("descriptor command accepts only validation and evidence modes", async () => {
  const { descriptorMode } = await import("./build-descriptors.mjs");

  assert.equal(descriptorMode(["--validation"]), "validation");
  assert.equal(descriptorMode(["--evidence"]), "evidence");
  assert.throws(() => descriptorMode([]), { message: "DESCRIPTOR_MODE_REQUIRED" });
});
