import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { create, toBinary } from "@bufbuild/protobuf";
import { FileDescriptorSetSchema } from "@bufbuild/protobuf/wkt";

const DESCRIPTOR_BYTES = Buffer.from(
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
const STALE_BYTES = Buffer.from(
  toBinary(
    FileDescriptorSetSchema,
    create(FileDescriptorSetSchema, {
      file: [{ name: "tammy/v1/stale.proto", package: "tammy.v1" }],
    }),
  ),
);
const SOURCE_REVISION = "0123456789abcdef0123456789abcdef01234567";
const SUBJECT_REVISION = "abcdef0123456789abcdef0123456789abcdef01";
const OTHER_REVISION = "1111111111111111111111111111111111111111";
const NODE_EXECUTABLE = "/mise/installs/node/24.18.0/bin/node";
const BUF_ENTRY = "/workspace/tammy/node_modules/@bufbuild/buf/bin/buf";

function manifestFor(descriptorBytes, overrides = {}) {
  return {
    path: "descriptors.pb",
    byteLength: descriptorBytes.byteLength,
    sha256: createHash("sha256").update(descriptorBytes).digest("hex"),
    bufVersion: "1.72.0",
    module: "buf.build/tammyapp/tammy",
    gitRevision: SOURCE_REVISION,
    ...overrides,
  };
}

async function writeRetainedEvidence(root, descriptorBytes = DESCRIPTOR_BYTES) {
  const retainedDirectory = path.join(root, "compliance/contracts");
  await mkdir(retainedDirectory, { recursive: true });
  await writeFile(path.join(retainedDirectory, "descriptors.pb"), descriptorBytes);
  await writeFile(
    path.join(retainedDirectory, "descriptor-manifest.json"),
    `${JSON.stringify(manifestFor(descriptorBytes), null, 2)}\n`,
  );
}

function createRun({
  freshBytes = DESCRIPTOR_BYTES,
  sourceParent = SOURCE_REVISION,
  worktreeStatus = "",
} = {}) {
  const calls = [];
  const run = async (command, args, options) => {
    calls.push({ args, command, options });
    if (command === "git") {
      if (args[0] === "rev-parse" && args[2] === `${SUBJECT_REVISION}^{commit}`) {
        return { stdout: `${SUBJECT_REVISION}\n` };
      }
      if (args[0] === "cat-file") return { stdout: "" };
      if (args[0] === "rev-parse" && args[2] === `${SUBJECT_REVISION}^`) {
        return { stdout: `${sourceParent}\n` };
      }
      if (args[0] === "diff") {
        return { stdout: "compliance/contracts/descriptor-manifest.json\n" };
      }
      if (args[0] === "status") return { stdout: worktreeStatus };
    }
    if (command === NODE_EXECUTABLE && args[0] === BUF_ENTRY && args[1] === "build") {
      await writeFile(args.at(-1), freshBytes);
      return { stdout: "" };
    }
    if (command === NODE_EXECUTABLE && args[0] === BUF_ENTRY && args[1] === "--version") {
      return { stdout: "1.72.0\n" };
    }
    throw new Error(`unexpected command: ${command} ${args.join(" ")}`);
  };
  return { calls, run };
}

test("verifies retained evidence against a fresh canonical build without mutating it", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-retained-evidence-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const descriptorPath = path.join(root, "compliance/contracts/descriptors.pb");
  const manifestPath = path.join(root, "compliance/contracts/descriptor-manifest.json");
  const beforeDescriptor = await readFile(descriptorPath);
  const beforeManifest = await readFile(manifestPath);
  const { calls, run } = createRun({
    worktreeStatus: " M compliance/contracts/descriptor-manifest.json\n",
  });
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");

  await verifyRetainedDescriptorEvidence({
    bufEntry: BUF_ENTRY,
    nodeExecutable: NODE_EXECUTABLE,
    platform: "darwin",
    root,
    run,
    subjectRevision: SUBJECT_REVISION,
  });

  assert.deepEqual(await readFile(descriptorPath), beforeDescriptor);
  assert.deepEqual(await readFile(manifestPath), beforeManifest);
  assert.deepEqual(await readdir(path.join(root, ".tmp")), []);
  assert.equal(
    calls.some(
      ({ args, command, options }) =>
        command === NODE_EXECUTABLE &&
        args[0] === BUF_ENTRY &&
        args[1] === "build" &&
        options.shell === false,
    ),
    true,
  );
});

test("rejects internally consistent stale retained evidence without repairing it", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-retained-evidence-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root, STALE_BYTES);
  const descriptorPath = path.join(root, "compliance/contracts/descriptors.pb");
  const manifestPath = path.join(root, "compliance/contracts/descriptor-manifest.json");
  const beforeDescriptor = await readFile(descriptorPath);
  const beforeManifest = await readFile(manifestPath);
  const { run } = createRun();
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");

  await assert.rejects(
    verifyRetainedDescriptorEvidence({
      bufEntry: BUF_ENTRY,
      nodeExecutable: NODE_EXECUTABLE,
      platform: "darwin",
      root,
      run,
      subjectRevision: SUBJECT_REVISION,
    }),
    { message: "DESCRIPTOR_RETAINED_BYTES_STALE" },
  );
  assert.deepEqual(await readFile(descriptorPath), beforeDescriptor);
  assert.deepEqual(await readFile(manifestPath), beforeManifest);
  assert.deepEqual(await readdir(path.join(root, ".tmp")), []);
});

test("rejects a retained source revision that is not a valid commit", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-retained-evidence-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const { run: baseRun } = createRun();
  const run = async (command, args, options) => {
    if (command === "git" && args[0] === "cat-file") {
      throw new Error("unknown revision details");
    }
    return baseRun(command, args, options);
  };
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");

  await assert.rejects(
    verifyRetainedDescriptorEvidence({
      bufEntry: BUF_ENTRY,
      nodeExecutable: NODE_EXECUTABLE,
      platform: "darwin",
      root,
      run,
      subjectRevision: SUBJECT_REVISION,
    }),
    (error) => {
      assert.equal(error.message, "DESCRIPTOR_SOURCE_REVISION_INVALID");
      assert.ok(error.cause instanceof Error);
      assert.notEqual(error.message, error.cause.message);
      return true;
    },
  );
});

test("rejects a subject that is not the source revision or its evidence-only child", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-retained-evidence-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const { run } = createRun({ sourceParent: OTHER_REVISION });
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");

  await assert.rejects(
    verifyRetainedDescriptorEvidence({
      bufEntry: BUF_ENTRY,
      nodeExecutable: NODE_EXECUTABLE,
      platform: "darwin",
      root,
      run,
      subjectRevision: SUBJECT_REVISION,
    }),
    { message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH" },
  );
});

test("rejects checkout source changes not represented by the retained revision", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-retained-evidence-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const { run } = createRun({ worktreeStatus: " M proto/tammy/v1/system.proto\n" });
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");

  await assert.rejects(
    verifyRetainedDescriptorEvidence({
      bufEntry: BUF_ENTRY,
      nodeExecutable: NODE_EXECUTABLE,
      platform: "darwin",
      root,
      run,
      subjectRevision: SUBJECT_REVISION,
    }),
    { message: "DESCRIPTOR_EVIDENCE_DIRTY_TREE" },
  );
});

test("wires non-mutating evidence verification into Linux and Windows CI", async () => {
  const packageJson = JSON.parse(
    await readFile(new URL("../package.json", import.meta.url), "utf8"),
  );
  const workflow = await readFile(
    new URL("../.github/workflows/foundation-ci.yml", import.meta.url),
    "utf8",
  );

  assert.equal(
    packageJson.scripts.contracts,
    "pnpm proto:format:check && pnpm proto:lint && pnpm proto:breaking && pnpm proto:generate && node scripts/check-contracts.mjs",
  );
  assert.equal(
    packageJson.scripts["proto:descriptors:verify"],
    "node scripts/verify-descriptor-evidence.mjs",
  );
  assert.equal((workflow.match(/run: pnpm proto:descriptors:verify/g) ?? []).length, 2);
  assert.equal(
    (
      workflow.match(
        /TAMMY_EVIDENCE_SUBJECT_REVISION: \$\{\{ github\.event\.pull_request\.head\.sha \|\| github\.sha \}\}/g,
      ) ?? []
    ).length,
    2,
  );
  assert.doesNotMatch(workflow, /run: pnpm proto:descriptors:evidence/);
  const windowsJob = workflow.split("windows-server-x64-package-smoke:")[1];
  assert.match(
    windowsJob,
    /name: Verify retained descriptor evidence[\s\S]*run: pnpm proto:descriptors:verify/,
  );
});
