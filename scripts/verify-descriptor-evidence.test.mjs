import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
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

async function writeRetainedEvidence(root, descriptorBytes = DESCRIPTOR_BYTES, overrides = {}) {
  const retainedDirectory = path.join(root, "compliance/contracts");
  await mkdir(retainedDirectory, { recursive: true });
  await writeFile(path.join(retainedDirectory, "descriptors.pb"), descriptorBytes);
  await writeFile(
    path.join(retainedDirectory, "descriptor-manifest.json"),
    `${JSON.stringify(manifestFor(descriptorBytes, overrides), null, 2)}\n`,
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
      if (args[0] === "show" && args.at(-1) === SUBJECT_REVISION) {
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

test("rejects duplicate or otherwise noncanonical raw manifest JSON", async (context) => {
  const canonicalManifest = `${JSON.stringify(manifestFor(DESCRIPTOR_BYTES), null, 2)}\n`;
  const cases = [
    canonicalManifest.replace(
      '  "path": "descriptors.pb",',
      '  "path": "descriptors.pb",\n  "path": "descriptors.pb",',
    ),
    JSON.stringify(manifestFor(DESCRIPTOR_BYTES)),
  ];
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");

  for (const [index, manifestSource] of cases.entries()) {
    const root = await mkdtemp(path.join(os.tmpdir(), "tammy-retained-evidence-"));
    context.after(() => rm(root, { force: true, recursive: true }));
    await writeRetainedEvidence(root);
    await writeFile(
      path.join(root, "compliance/contracts/descriptor-manifest.json"),
      manifestSource,
    );
    const { run } = createRun();

    await assert.rejects(
      verifyRetainedDescriptorEvidence({
        bufEntry: BUF_ENTRY,
        nodeExecutable: NODE_EXECUTABLE,
        platform: "darwin",
        root,
        run,
        subjectRevision: SUBJECT_REVISION,
      }),
      { message: "DESCRIPTOR_MANIFEST_CANONICAL_INVALID" },
      `case ${index + 1}`,
    );
  }
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

test("delegates non-mutating evidence verification to ordered Linux and Windows CI tasks", async () => {
  const packageJson = JSON.parse(
    await readFile(new URL("../package.json", import.meta.url), "utf8"),
  );
  const workflow = await readFile(
    new URL("../.github/workflows/foundation-ci.yml", import.meta.url),
    "utf8",
  );
  const attributes = await readFile(new URL("../.gitattributes", import.meta.url), "utf8");

  assert.equal(
    packageJson.scripts.contracts,
    "pnpm proto:format:check && pnpm proto:lint && pnpm proto:breaking && pnpm proto:generate && node scripts/check-contracts.mjs",
  );
  assert.equal(
    packageJson.scripts["proto:descriptors:verify"],
    "node scripts/verify-descriptor-evidence.mjs",
  );
  assert.match(attributes, /^compliance\/contracts\/descriptor-manifest\.json text eol=lf$/m);
  assert.equal((workflow.match(/run: pnpm proto:descriptors:verify/g) ?? []).length, 0);
  assert.equal(
    (
      workflow.match(
        /TAMMY_EVIDENCE_SUBJECT_REVISION: \$\{\{ github\.event\.pull_request\.head\.sha \|\| github\.sha \}\}/g,
      ) ?? []
    ).length,
    2,
  );
  assert.doesNotMatch(workflow, /run: pnpm proto:descriptors:evidence/);
  assert.match(
    workflow,
    /name: Run canonical Ubuntu CI scenarios[\s\S]*task ci:contracts[\s\S]*task ci:linux/,
  );
  const windowsJob = workflow.split("windows-server-x64-package-smoke:")[1];
  assert.match(
    windowsJob,
    /TAMMY_EVIDENCE_SUBJECT_REVISION: \$\{\{ github\.event\.pull_request\.head\.sha \|\| github\.sha \}\}[\s\S]*task ci:windows-smoke/,
  );
});

async function largePathHistory(context) {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-large-git-evidence-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  const git = (args, input) =>
    execFileSync("git", args, {
      cwd: root,
      encoding: "utf8",
      input,
      maxBuffer: 8 * 1024 * 1024,
      env: {
        ...process.env,
        GIT_AUTHOR_NAME: "Synthetic descriptor test",
        GIT_AUTHOR_EMAIL: "descriptor@example.invalid",
        GIT_COMMITTER_NAME: "Synthetic descriptor test",
        GIT_COMMITTER_EMAIL: "descriptor@example.invalid",
      },
    }).trim();
  git(["init", "--quiet", "--template="]);
  const blob = git(["hash-object", "-w", "--stdin"], "");
  const emptyTree = git(["mktree"], "");
  const sourceRevision = git(["commit-tree", emptyTree, "-m", "Synthetic source"]);
  const entries = Array.from(
    { length: 12_000 },
    (_, index) => `100644 blob ${blob}\t${String(index).padStart(5, "0")}-${"a".repeat(128)}\n`,
  ).join("");
  let tree = git(["mktree"], entries);
  for (const directory of ["00", "files", "v11", ".pnpm-store"]) {
    tree = git(["mktree"], `040000 tree ${tree}\t${directory}\n`);
  }
  const subjectRevision = git([
    "commit-tree",
    tree,
    "-p",
    sourceRevision,
    "-m",
    "Synthetic cache paths",
  ]);
  git(["update-ref", "HEAD", subjectRevision]);
  await writeRetainedEvidence(root, DESCRIPTOR_BYTES, { gitRevision: sourceRevision });
  return { git, root, sourceRevision, subjectRevision };
}

test("rejects a large real Git diff by provenance policy without overflowing stdout", async (context) => {
  const { git, root, sourceRevision, subjectRevision } = await largePathHistory(context);
  const output = git(["diff", "--name-only", sourceRevision, subjectRevision, "--"]);
  assert.ok(
    Buffer.byteLength(output) > 1024 * 1024,
    "fixture must exceed execFile's default limit",
  );
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");
  await assert.rejects(verifyRetainedDescriptorEvidence({ root, subjectRevision }), {
    message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH",
  });
});

test("rejects a large real Git status by dirty-tree policy without overflowing stdout", async (context) => {
  const { git, root, sourceRevision } = await largePathHistory(context);
  const output = git(["status", "--porcelain=v1", "--untracked-files=all"]);
  assert.ok(
    Buffer.byteLength(output) > 1024 * 1024,
    "fixture must exceed execFile's default limit",
  );
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");
  await assert.rejects(
    verifyRetainedDescriptorEvidence({ root, subjectRevision: sourceRevision }),
    {
      message: "DESCRIPTOR_EVIDENCE_DIRTY_TREE",
    },
  );
});

test("preserves a failing streamed Git command and its diagnostics", async (context) => {
  const { git, root, sourceRevision } = await largePathHistory(context);
  git(["config", "core.bare", "true"]);
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");
  await assert.rejects(
    verifyRetainedDescriptorEvidence({ root, subjectRevision: sourceRevision }),
    (error) => {
      assert.equal(error.code, 128);
      assert.match(error.cmd, /^git status /);
      assert.ok(error.stderr.length > 0);
      assert.ok(error.message.includes(error.stderr));
      return true;
    },
  );
});

test("rejects an internally consistent retained subset of the complete descriptor build", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-descriptor-subset-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const fullDescriptorSet = create(FileDescriptorSetSchema, {
    file: [
      {
        name: "tammy/v1/system.proto",
        package: "tammy.v1",
        service: [{ name: "SystemService", method: [{ name: "GetDiagnostics" }] }],
      },
      {
        name: "tammy/v1/banking.proto",
        package: "tammy.v1",
        service: [{ name: "BankingService", method: [{ name: "GetBankingSummary" }] }],
      },
    ],
  });
  const freshBytes = Buffer.from(toBinary(FileDescriptorSetSchema, fullDescriptorSet));
  const { run } = createRun({ freshBytes });
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
  assert.deepEqual(
    await readFile(path.join(root, "compliance/contracts/descriptors.pb")),
    DESCRIPTOR_BYTES,
  );
});

// Real Git objects exercise ancestry, parent order, tree identity and repository
// configuration. Only Buf is a fixture executable; existing checks still compare
// the retained bytes with the output of a separate build process.
async function mergeHistory(context, { retainedBytes = DESCRIPTOR_BYTES } = {}) {
  const directory = await mkdtemp(path.join(os.tmpdir(), "tammy-merge-evidence-"));
  context.after(() => rm(directory, { force: true, recursive: true }));
  const root = path.join(directory, "repository");
  await mkdir(root);
  const globalConfig = path.join(directory, "gitconfig");
  await writeFile(globalConfig, "");
  const git = (args, input) =>
    execFileSync("git", args, {
      cwd: root,
      encoding: "utf8",
      input,
      env: {
        ...process.env,
        GIT_CONFIG_NOSYSTEM: "1",
        GIT_CONFIG_GLOBAL: globalConfig,
        GIT_AUTHOR_NAME: "Synthetic descriptor test",
        GIT_AUTHOR_EMAIL: "descriptor@example.invalid",
        GIT_COMMITTER_NAME: "Synthetic descriptor test",
        GIT_COMMITTER_EMAIL: "descriptor@example.invalid",
      },
    }).trim();
  const commit = (tree, parents, message = "Synthetic commit") =>
    git(["commit-tree", tree, ...parents.flatMap((parent) => ["-p", parent]), "-m", message]);
  const tree = (revision) => git(["rev-parse", `${revision}^{tree}`]);
  const blob = (contents) => git(["hash-object", "-w", "--stdin"], contents);
  const treeFrom = (revision, entries) => {
    git(["read-tree", revision]);
    for (const [file, value] of entries) {
      if (value === null) git(["update-index", "--force-remove", file]);
      else
        git([
          "update-index",
          "--add",
          "--cacheinfo",
          `${value.mode || "100644"},${value.object},${file}`,
        ]);
    }
    return git(["write-tree"]);
  };
  const evidenceTree = (sourceRevision, entries = []) =>
    treeFrom(sourceRevision, [
      ["compliance/contracts/descriptors.pb", { object: blob(retainedBytes) }],
      [
        "compliance/contracts/descriptor-manifest.json",
        {
          object: blob(
            `${JSON.stringify(manifestFor(retainedBytes, { gitRevision: sourceRevision }), null, 2)}\n`,
          ),
        },
      ],
      ...entries,
    ]);
  const checkout = (revision) => git(["checkout", "--quiet", "--detach", "--force", revision]);
  git(["init", "--quiet", "--template="]);
  git(["config", "core.autocrlf", "false"]);
  git(["config", "core.filemode", "true"]);
  await writeFile(path.join(root, ".gitignore"), ".tmp/\n");
  await writeFile(path.join(root, "source.txt"), "base\n");
  await writeRetainedEvidence(root);
  git(["add", "."]);
  const baseRevision = commit(git(["write-tree"]), [], "Synthetic base");
  const sourceRevision = commit(
    treeFrom(baseRevision, [["source.txt", { object: blob("reviewed source\n") }]]),
    [baseRevision],
    "Synthetic source",
  );
  const evidenceRevision = commit(
    evidenceTree(sourceRevision),
    [sourceRevision],
    "Synthetic evidence",
  );
  const mergeRevision = commit(
    tree(evidenceRevision),
    [baseRevision, evidenceRevision],
    "Synthetic merge",
  );
  checkout(mergeRevision);
  const bufEntry = path.join(directory, "fixture-buf.mjs");
  await writeFile(
    bufEntry,
    `import { writeFile } from "node:fs/promises";
if (process.argv[2] === "--version") process.stdout.write("1.72.0\\n");
else if (process.argv[2] === "build") await writeFile(process.argv.at(-1), Buffer.from("${DESCRIPTOR_BYTES.toString("base64")}", "base64"));
else process.exitCode = 1;
`,
  );
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");
  const verify = (options = {}) => verifyRetainedDescriptorEvidence({ root, bufEntry, ...options });
  return {
    baseRevision,
    blob,
    checkout,
    commit,
    evidenceRevision,
    evidenceTree,
    git,
    mergeRevision,
    root,
    sourceRevision,
    tree,
    treeFrom,
    verify,
  };
}

test("accepts an unchanged-tree ordinary merge, its evidence head override and source precommit", async (context) => {
  const fixture = await mergeHistory(context);
  const { checkout, evidenceRevision, mergeRevision, root, sourceRevision, tree, verify } = fixture;
  assert.equal(tree(mergeRevision), tree(evidenceRevision));
  const before = await readFile(path.join(root, "compliance/contracts/descriptor-manifest.json"));
  assert.equal((await verify()).gitRevision, sourceRevision);
  assert.equal((await verify({ subjectRevision: evidenceRevision })).gitRevision, sourceRevision);
  assert.deepEqual(
    await readFile(path.join(root, "compliance/contracts/descriptor-manifest.json")),
    before,
  );
  checkout(evidenceRevision);
  assert.equal((await verify()).gitRevision, sourceRevision);
  checkout(sourceRevision);
  await writeRetainedEvidence(root, DESCRIPTOR_BYTES, { gitRevision: sourceRevision });
  assert.equal((await verify()).gitRevision, sourceRevision);
});

test("rejects unrelated and stale merge bases even when the entire merge tree matches", async (context) => {
  const fixture = await mergeHistory(context);
  const { baseRevision, checkout, commit, evidenceRevision, tree, verify } = fixture;
  for (const parents of [[], [baseRevision]]) {
    const otherBase = commit(tree(baseRevision), parents, "Unreviewed base");
    const merge = commit(tree(evidenceRevision), [otherBase, evidenceRevision]);
    checkout(merge);
    await assert.rejects(verify(), { message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH" });
  }
});

test("rejects changed merge trees including mode changes hidden by worktree configuration", async (context) => {
  const fixture = await mergeHistory(context);
  const { baseRevision, blob, checkout, commit, evidenceRevision, git, treeFrom, verify } = fixture;
  const sourceBlob = git(["rev-parse", `${evidenceRevision}:source.txt`]);
  for (const entry of [
    { object: blob("unreviewed resolution\n") },
    { mode: "100755", object: sourceBlob },
  ]) {
    const changedTree = treeFrom(evidenceRevision, [["source.txt", entry]]);
    const merge = commit(changedTree, [baseRevision, evidenceRevision]);
    checkout(merge);
    git(["config", "core.filemode", "false"]);
    assert.equal(git(["status", "--porcelain"]), "");
    await assert.rejects(verify(), { message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH" });
  }
});

test("rejects reversed, octopus and nested merge ancestry", async (context) => {
  const fixture = await mergeHistory(context);
  const { baseRevision, checkout, commit, evidenceRevision, mergeRevision, tree, verify } = fixture;
  const other = commit(tree(baseRevision), [], "Other root");
  for (const parents of [
    [evidenceRevision, baseRevision],
    [baseRevision, evidenceRevision, other],
    [baseRevision, mergeRevision],
  ]) {
    checkout(commit(tree(evidenceRevision), parents));
    await assert.rejects(verify(), { message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH" });
  }
});

test("rejects evidence commits with a wrong source or multiple parents", async (context) => {
  const fixture = await mergeHistory(context);
  const { baseRevision, checkout, commit, evidenceRevision, sourceRevision, tree, verify } =
    fixture;
  for (const parents of [[baseRevision], [sourceRevision, baseRevision]]) {
    const invalidEvidence = commit(tree(evidenceRevision), parents);
    checkout(commit(tree(invalidEvidence), [baseRevision, invalidEvidence]));
    await assert.rejects(verify(), { message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH" });
  }
});

test("rejects source edits in an evidence child and in its unchanged-tree merge", async (context) => {
  const fixture = await mergeHistory(context);
  const {
    baseRevision,
    blob,
    checkout,
    commit,
    evidenceRevision,
    sourceRevision,
    tree,
    treeFrom,
    verify,
  } = fixture;
  const invalidEvidence = commit(
    treeFrom(evidenceRevision, [["source.txt", { object: blob("unattested source\n") }]]),
    [sourceRevision],
  );
  for (const revision of [
    invalidEvidence,
    commit(tree(invalidEvidence), [baseRevision, invalidEvidence]),
  ]) {
    checkout(revision);
    await assert.rejects(verify(), { message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH" });
  }
});

test("rejects a different clean checkout when the subject is an otherwise valid merge", async (context) => {
  const fixture = await mergeHistory(context);
  const { blob, checkout, commit, evidenceRevision, git, mergeRevision, treeFrom, verify } =
    fixture;
  checkout(
    commit(treeFrom(evidenceRevision, [["source.txt", { object: blob("different checkout\n") }]]), [
      evidenceRevision,
    ]),
  );
  assert.equal(git(["status", "--porcelain"]), "");
  await assert.rejects(verify({ subjectRevision: mergeRevision }), {
    message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH",
  });
});

test("requires clean retained files when verifying a merge", async (context) => {
  const { root, sourceRevision, verify } = await mergeHistory(context);
  await writeRetainedEvidence(root, STALE_BYTES, { gitRevision: sourceRevision });
  await assert.rejects(verify(), { message: "DESCRIPTOR_EVIDENCE_DIRTY_TREE" });
});

test("still rejects stale descriptors after validating ordinary merge provenance", async (context) => {
  const { verify } = await mergeHistory(context, { retainedBytes: STALE_BYTES });
  await assert.rejects(verify(), { message: "DESCRIPTOR_RETAINED_BYTES_STALE" });
});

test("rejects a hidden gitlink change in the evidence commit", async (context) => {
  const fixture = await mergeHistory(context);
  const {
    baseRevision,
    checkout,
    commit,
    evidenceRevision,
    evidenceTree,
    git,
    sourceRevision,
    tree,
    treeFrom,
    verify,
  } = fixture;
  const source = commit(
    treeFrom(sourceRevision, [["vendor", { mode: "160000", object: baseRevision }]]),
    [baseRevision],
  );
  const evidence = commit(
    evidenceTree(source, [["vendor", { mode: "160000", object: evidenceRevision }]]),
    [source],
  );
  checkout(commit(tree(evidence), [baseRevision, evidence]));
  git(["config", "diff.ignoreSubmodules", "all"]);
  assert.equal(git(["diff", "--name-only", source, evidence, "--"]).includes("vendor"), false);
  await assert.rejects(verify(), { message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH" });
});

test("rejects a dirty checked-out submodule even when Git configuration ignores it", async (context) => {
  const fixture = await mergeHistory(context);
  const {
    baseRevision,
    checkout,
    commit,
    evidenceTree,
    git,
    root,
    sourceRevision,
    tree,
    treeFrom,
    verify,
  } = fixture;
  git(["clone", "--quiet", "--shared", "--no-checkout", ".", "vendor"]);
  git(["-C", "vendor", "checkout", "--quiet", "--detach", sourceRevision]);
  const source = commit(
    treeFrom(sourceRevision, [["vendor", { mode: "160000", object: sourceRevision }]]),
    [baseRevision],
  );
  const evidence = commit(evidenceTree(source), [source]);
  checkout(commit(tree(evidence), [baseRevision, evidence]));
  assert.equal(git(["status", "--porcelain"]), "");
  await writeFile(path.join(root, "vendor/source.txt"), "dirty submodule source\n");
  git(["config", "diff.ignoreSubmodules", "all"]);
  assert.equal(git(["status", "--porcelain"]), "");
  await assert.rejects(verify(), { message: "DESCRIPTOR_EVIDENCE_DIRTY_TREE" });
});

test("rejects a source path renamed into the retained descriptor path", async (context) => {
  const fixture = await mergeHistory(context);
  const {
    baseRevision,
    blob,
    checkout,
    commit,
    evidenceTree,
    git,
    sourceRevision,
    tree,
    treeFrom,
    verify,
  } = fixture;
  const source = commit(
    treeFrom(sourceRevision, [
      ["compliance/contracts/descriptors.pb", null],
      ["old-descriptors.pb", { object: blob(DESCRIPTOR_BYTES) }],
    ]),
    [baseRevision],
  );
  const evidence = commit(evidenceTree(source, [["old-descriptors.pb", null]]), [source]);
  checkout(commit(tree(evidence), [baseRevision, evidence]));
  git(["config", "diff.renames", "true"]);
  assert.equal(
    git(["diff", "--name-only", source, evidence, "--"]).includes("old-descriptors.pb"),
    false,
  );
  await assert.rejects(verify(), { message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH" });
});

test("rejects an empty evidence-child diff", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-empty-evidence-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await writeRetainedEvidence(root);
  const { run: baseRun } = createRun();
  const run = (command, args, options) =>
    command === "git" && args[0] === "diff" ? { stdout: "" } : baseRun(command, args, options);
  const { verifyRetainedDescriptorEvidence } = await import("./verify-descriptor-evidence.mjs");
  await assert.rejects(
    verifyRetainedDescriptorEvidence({ root, run, subjectRevision: SUBJECT_REVISION }),
    {
      message: "DESCRIPTOR_SOURCE_REVISION_MISMATCH",
    },
  );
});

test("preserves operational ancestry errors instead of treating them as non-ancestry", async (context) => {
  const fixture = await mergeHistory(context);
  const failure = Object.assign(new Error("synthetic Git failure"), { code: 128 });
  const run = async (command, args) => {
    assert.equal(command, "git");
    if (args[0] === "merge-base") throw failure;
    return { stdout: fixture.git(args) };
  };
  await assert.rejects(fixture.verify({ run }), (error) => error === failure);
});
