import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import {
  bundle,
  PROJECT_ID,
  packageWorker,
  prepare,
  recordBuild,
  verify,
  verifySource,
} from "./site-release.mjs";

const hosting = { project_id: PROJECT_ID, d1: null, r2: null };
const runtime = { node: "24.18.0", pnpm: "11.15.0", platform: "fixture", arch: "fixture" };
const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const readJson = (filename) => JSON.parse(readFileSync(filename, "utf8"));

function write(root, filename, contents) {
  mkdirSync(path.dirname(path.join(root, filename)), { recursive: true });
  writeFileSync(
    path.join(root, filename),
    typeof contents === "string" ? contents : `${JSON.stringify(contents, null, 2)}\n`,
  );
}

function git(root, ...args) {
  return execFileSync("git", args, {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function commit(root) {
  git(root, "add", "--all");
  git(
    root,
    "-c",
    "user.name=Release contract tests",
    "-c",
    "user.email=release-tests@example.invalid",
    "-c",
    "commit.gpgsign=false",
    "commit",
    "-m",
    "Fixture source",
  );
  return git(root, "rev-parse", "HEAD");
}

function fixture(t) {
  const base = realpathSync(mkdtempSync(path.join(tmpdir(), "tammy-release-test-")));
  t.after(() => rmSync(base, { recursive: true, force: true }));
  const root = path.join(base, "github");
  const snapshot = path.join(base, "snapshot");
  mkdirSync(root);
  git(root, "init", "--initial-branch=master");
  const rootPackage = {
    name: "tammy",
    private: true,
    type: "module",
    packageManager: "pnpm@11.15.0",
    engines: { node: "24.18.0" },
    scripts: { unrelated: "do-not-run" },
    devDependencies: { typescript: "7.0.2" },
  };
  write(root, "package.json", rootPackage);
  write(
    root,
    "pnpm-lock.yaml",
    'lockfileVersion: "9.0"\nimporters:\n  apps/site: {}\n  packages/invoices: {}\n',
  );
  write(root, "pnpm-workspace.yaml", "packages:\n  - apps/*\n  - packages/*\n");
  write(root, ".npmrc", "strict-peer-dependencies=true\n");
  write(root, "tsconfig.base.json", { compilerOptions: { strict: true } });
  write(root, ".gitignore", "node_modules/\n.pnpm-store/\n.env*\n");
  write(root, "apps/site/.openai/hosting.json", hosting);
  write(root, "apps/site/package.json", {
    name: "sites-project",
    dependencies: { "@tammy/invoices": "workspace:*" },
  });
  write(root, "apps/site/app/page.tsx", 'export default function Page() { return "Tammy"; }\n');
  write(root, "packages/invoices/package.json", {
    name: "@tammy/invoices",
    dependencies: { react: "19.2.7" },
  });
  write(root, "packages/invoices/tsconfig.json", { extends: "../../tsconfig.base.json" });
  write(root, "packages/invoices/src/model.ts", "export const total = 110;\n");
  write(root, "packages/invoices/test-support/logo.png", "fixture PNG bytes");
  write(root, "patches/electron-conveyor@0.4.0.patch", "fixture tracked patch\n");
  write(root, "scripts/site-release.mjs", "// Fixture release script\n");
  write(root, "scripts/site-release.test.mjs", "// Fixture release tests\n");
  write(root, "apps/desktop/unrelated.ts", "This unrelated app must not be exported.\n");
  const commitSha = commit(root);
  return { base, root, snapshot, commitSha, rootPackage };
}

function prepared(t) {
  const state = fixture(t);
  state.provenance = prepare(state.root, state.snapshot);
  return state;
}

function workerFixture(root) {
  write(root, "dist/.openai/hosting.json", hosting);
  write(
    root,
    "dist/server/index.js",
    'export default { fetch() { return new Response("Tammy fixture"); } };\n',
  );
  write(root, "dist/server/wrangler.json", {
    main: "index.js",
    assets: { directory: "../client" },
  });
  write(root, "dist/client/assets/pdf.wasm", "\0asm\x01\0\0\0");
  write(root, "dist/client/assets/site.js", 'console.log("fixture");\n');
}

function built(t) {
  const state = prepared(t);
  workerFixture(state.snapshot);
  state.build = recordBuild(state.snapshot, runtime);
  return state;
}

test("exports the complete invoice workspace and immutable dependency inputs from the exact GitHub commit", (t) => {
  const { root, snapshot, commitSha, rootPackage, provenance } = prepared(t);
  assert.equal(provenance.github.commit_sha, commitSha);
  assert.equal(provenance.github.repository, "castlemilk/tammy");
  assert.deepEqual(provenance.sites, {
    commit_sha: null,
    saved_version: null,
    publication: "not-performed",
  });
  for (const filename of [
    "pnpm-lock.yaml",
    "pnpm-workspace.yaml",
    ".npmrc",
    "tsconfig.base.json",
    "patches/electron-conveyor@0.4.0.patch",
    "packages/invoices/package.json",
    "packages/invoices/tsconfig.json",
    "packages/invoices/src/model.ts",
    "packages/invoices/test-support/logo.png",
  ]) {
    assert.deepEqual(
      readFileSync(path.join(snapshot, filename)),
      readFileSync(path.join(root, filename)),
      filename,
    );
  }
  const exportedPackage = readJson(path.join(snapshot, "package.json"));
  assert.deepEqual(exportedPackage.devDependencies, rootPackage.devDependencies);
  assert.equal(exportedPackage.packageManager, rootPackage.packageManager);
  assert.equal(exportedPackage.scripts.build, "node scripts/site-release.mjs build");
  assert.equal(exportedPackage.scripts.unrelated, undefined);
  assert.deepEqual(readJson(path.join(snapshot, ".openai/hosting.json")), hosting);
  assert.deepEqual(verifySource(snapshot), provenance);
  assert.equal(existsSync(path.join(snapshot, ".git")), false);
  assert.equal(existsSync(path.join(snapshot, "apps/desktop")), false);
});

test("ignored local credentials and dependency stores never enter the source export", (t) => {
  const { root, snapshot } = fixture(t);
  write(root, "apps/site/.env.local", "TEST_ONLY_SECRET=must-not-be-exported\n");
  write(root, "node_modules/local-package/index.js", "ignored package\n");
  write(root, ".pnpm-store/private-cache", "ignored cache\n");
  const source = prepare(root, snapshot);
  assert.equal(
    source.files.some((file) => /\.env|node_modules|\.pnpm-store/.test(file.path)),
    false,
  );
  assert.equal(existsSync(path.join(snapshot, "apps/site/.env.local")), false);
});

for (const kind of ["tracked", "untracked"]) {
  test(`refuses a ${kind} dirty source tree before creating output`, (t) => {
    const { root, snapshot } = fixture(t);
    write(
      root,
      kind === "tracked" ? "apps/site/app/page.tsx" : "new-release-note.txt",
      "uncommitted content\n",
    );
    assert.throws(() => prepare(root, snapshot), /clean GitHub checkout/);
    assert.equal(existsSync(snapshot), false);
  });
}

test("refuses a missing invoice workspace and a newly introduced local workspace dependency", async (t) => {
  await t.test("missing invoice source", (t) => {
    const { root, snapshot } = fixture(t);
    rmSync(path.join(root, "packages/invoices"), { recursive: true });
    commit(root);
    assert.throws(() => prepare(root, snapshot), /Missing source input: packages\/invoices/);
  });
  await t.test("new dependency must be added to the reviewed closure", (t) => {
    const { root, snapshot } = fixture(t);
    write(root, "packages/invoices/package.json", {
      name: "@tammy/invoices",
      dependencies: { "@tammy/new-package": "workspace:*" },
    });
    commit(root);
    assert.throws(() => prepare(root, snapshot), /Unreviewed local dependency/);
  });
});

test("refuses tracked credentials and symlinks inside the source closure", async (t) => {
  await t.test("tracked credential file", (t) => {
    const { root, snapshot } = fixture(t);
    write(root, "apps/site/.env.production", "TEST_ONLY_SECRET=never-export\n");
    git(root, "add", "--force", "apps/site/.env.production");
    commit(root);
    assert.throws(() => prepare(root, snapshot), /generated or sensitive source path/);
    assert.equal(existsSync(snapshot), false);
  });
  await t.test("tracked symlink", (t) => {
    const { root, snapshot } = fixture(t);
    symlinkSync("../../../package.json", path.join(root, "apps/site/linked.json"));
    commit(root);
    assert.throws(() => prepare(root, snapshot), /symlink, submodule or unsupported Git entry/);
    assert.equal(existsSync(snapshot), false);
  });
});

test("never overwrites an existing output or writes inside its GitHub checkout", async (t) => {
  await t.test("nonempty output", (t) => {
    const { root, snapshot } = fixture(t);
    write(snapshot, "keep.txt", "existing work");
    assert.throws(() => prepare(root, snapshot), /never overwritten/);
    assert.equal(readFileSync(path.join(snapshot, "keep.txt"), "utf8"), "existing work");
    assert.deepEqual(readdirSync(snapshot), ["keep.txt"]);
  });
  await t.test("inside source checkout", (t) => {
    const { root } = fixture(t);
    assert.throws(() => prepare(root, path.join(root, "output")), /outside the source checkout/);
    assert.equal(existsSync(path.join(root, "output")), false);
  });
  await t.test("symlinked output parent cannot bypass source containment", (t) => {
    const { base, root } = fixture(t);
    symlinkSync(root, path.join(base, "linked-output"));
    assert.throws(
      () => prepare(root, path.join(base, "linked-output/snapshot")),
      /outside the source checkout|must not traverse symlinks/,
    );
    assert.equal(existsSync(path.join(root, "snapshot")), false);
  });
  await t.test("an existing output symlink is never replaced", (t) => {
    const { base, root } = fixture(t);
    mkdirSync(path.join(base, "real-output"));
    symlinkSync(path.join(base, "real-output"), path.join(base, "linked-output"));
    assert.throws(
      () => prepare(root, path.join(base, "linked-output")),
      /symlink|never overwritten/,
    );
    assert.deepEqual(readdirSync(path.join(base, "real-output")), []);
  });
});

test("source provenance detects changed bytes, new source files, deleted dependencies and executable-mode changes", async (t) => {
  for (const mutation of ["changed", "new", "nested generated-name route", "deleted", "mode"]) {
    await t.test(mutation, (t) => {
      const { snapshot } = prepared(t);
      if (mutation === "changed")
        write(snapshot, "packages/invoices/src/model.ts", "export const total = 999;\n");
      if (mutation === "new") write(snapshot, "apps/site/app/new-page.tsx", "unexpected route\n");
      if (mutation === "nested generated-name route")
        write(snapshot, "apps/site/app/dist/page.tsx", "unexpected compilable route\n");
      if (mutation === "deleted") rmSync(path.join(snapshot, "pnpm-lock.yaml"));
      if (mutation === "mode") chmodSync(path.join(snapshot, "apps/site/app/page.tsx"), 0o755);
      assert.throws(() => verifySource(snapshot), /Source differs/);
    });
  }
});

test("preserves executable source bits and ignores expected build/tool caches when checking source", (t) => {
  const { root, snapshot } = fixture(t);
  chmodSync(path.join(root, "scripts/site-release.mjs"), 0o755);
  commit(root);
  const source = prepare(root, snapshot);
  assert.equal(
    source.files.find((file) => file.path === "scripts/site-release.mjs").executable,
    true,
  );
  write(snapshot, "node_modules/tool/index.js", "installed dependency");
  write(snapshot, "apps/site/.wrangler/cache.json", "tool state");
  write(snapshot, "apps/site/tsconfig.tsbuildinfo", "typescript cache");
  write(snapshot, "apps/site/next-env.d.ts", "generated types");
  assert.deepEqual(verifySource(snapshot), source);
});

test("links build provenance to source and validates required Worker, manifest and PDF WASM output", async (t) => {
  await t.test("valid build", (t) => {
    const { snapshot, provenance, build } = built(t);
    assert.equal(build.github.commit_sha, provenance.github.commit_sha);
    assert.equal(build.source_sha256, provenance.source_sha256);
    assert.deepEqual(build.runtime, runtime);
    assert.equal(verify(snapshot).built.build_sha256, build.build_sha256);
  });
  for (const filename of ["server/index.js", ".openai/hosting.json", "client/assets/pdf.wasm"]) {
    await t.test(`missing ${filename}`, (t) => {
      const { snapshot } = prepared(t);
      workerFixture(snapshot);
      rmSync(path.join(snapshot, "dist", filename));
      assert.throws(
        () => recordBuild(snapshot, runtime),
        /Missing Worker output|Missing invoice PDF WASM/,
      );
      assert.equal(existsSync(path.join(snapshot, "build-provenance.json")), false);
    });
  }
});

test("rejects stale or tampered build provenance and symlinks in Worker output", async (t) => {
  for (const mutation of ["worker bytes", "source commit", "source digest", "symlink"]) {
    await t.test(mutation, (t) => {
      const { snapshot } = built(t);
      if (mutation === "worker bytes") write(snapshot, "dist/server/index.js", "changed Worker");
      if (mutation === "source commit" || mutation === "source digest") {
        const record = readJson(path.join(snapshot, "build-provenance.json"));
        if (mutation === "source commit") record.github.commit_sha = "f".repeat(40);
        else record.source_sha256 = "0".repeat(64);
        write(snapshot, "build-provenance.json", record);
      }
      if (mutation === "symlink")
        symlinkSync("../server/index.js", path.join(snapshot, "dist/client/unsafe.js"));
      assert.throws(
        () => verify(snapshot),
        /Build provenance does not match|Symlink or special file/,
      );
    });
  }
});

test("source bundle round-trips the prepared closure and keeps GitHub provenance distinct from Sites publication", (t) => {
  const { base, snapshot, provenance, build } = built(t);
  const output = path.join(base, "artifacts");
  const manifest = bundle(snapshot, output);
  const archive = path.join(output, "source.tar.gz");
  assert.equal(manifest.source_archive.sha256, sha256(readFileSync(archive)));
  assert.equal(manifest.source_sha256, provenance.source_sha256);
  assert.equal(manifest.build_sha256, build.build_sha256);
  assert.equal(manifest.publication, "not-performed");
  assert.equal(manifest.sites.commit_sha, null);
  assert.equal(manifest.sites.saved_version, null);
  const extracted = path.join(base, "extracted");
  mkdirSync(extracted);
  execFileSync("tar", ["-xzf", archive, "-C", extracted]);
  assert.deepEqual(verifySource(extracted), provenance);
  assert.equal(existsSync(path.join(extracted, "dist")), false);
  assert.equal(existsSync(path.join(extracted, "node_modules")), false);
  assert.deepEqual(
    readFileSync(path.join(output, "dist/server/index.js")),
    readFileSync(path.join(snapshot, "dist/server/index.js")),
  );
  assert.deepEqual(readJson(path.join(output, "release-manifest.json")), manifest);
});

test("packaging requires the installed supported helper and rejects a stale build before producing output", (t) => {
  const { base, snapshot } = built(t);
  const output = path.join(base, "package");
  assert.throws(() => packageWorker(snapshot, output), /absolute installed Sites plugin directory/);
  assert.throws(
    () => packageWorker(snapshot, output, path.join(base, "missing-plugin")),
    /Installed Sites package-site.mjs helper is required/,
  );
  assert.equal(existsSync(output), false);
  write(snapshot, "dist/server/index.js", "stale build");
  assert.throws(
    () => packageWorker(snapshot, output, path.join(base, "missing-plugin")),
    /Build provenance does not match/,
  );
  assert.equal(existsSync(output), false);
});
