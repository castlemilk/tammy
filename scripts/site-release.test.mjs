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
  assertCloudflareConfig,
  build,
  bundle,
  CLOUDFLARE_WORKER_NAME,
  classifyReadback,
  cloudflareDeploymentResult,
  cloudflareDryRun,
  cloudflareEnvironment,
  deployCloudflare,
  deployVercel,
  PROJECT_ID,
  packageWorker,
  prepare,
  recordBuild,
  VERCEL_CLI_VERSION,
  VERCEL_ROUTES,
  vercelEnvironment,
  vercelOutput,
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

function prepared(t, target = "sites") {
  const state = fixture(t);
  state.provenance = prepare(state.root, state.snapshot, target);
  return state;
}

function workerFixture(root, target = "sites") {
  if (target === "vercel") {
    // Static export: prerendered pages, RSC payloads, hashed assets and a hidden Vite manifest.
    write(root, "dist/client/index.html", "<h1>Tammy</h1>\n");
    write(root, "dist/client/index.rsc", "0:fixture\n");
    write(root, "dist/client/404.html", "<h1>Not found</h1>\n");
    write(root, "dist/client/tools.html", "<h1>Tools</h1>\n");
    write(root, "dist/client/tools/gst-calculator.html", "<h1>GST</h1>\n");
    write(root, "dist/client/_next/static/chunks/app.js", 'console.log("fixture");\n');
    write(root, "dist/client/assets/pdf.wasm", "\0asm\x01\0\0\0");
    write(root, "dist/client/.vite/manifest.json", "{}\n");
    write(root, "dist/server/index.js", "export default {};\n");
    return;
  }
  if (target === "sites") write(root, "dist/.openai/hosting.json", hosting);
  write(
    root,
    "dist/server/index.js",
    'export default { fetch() { return new Response("Tammy fixture"); } };\n',
  );
  write(
    root,
    "dist/server/wrangler.json",
    target === "cloudflare"
      ? cloudflareConfig()
      : {
          main: "index.js",
          assets: { directory: "../client" },
        },
  );
  write(root, "dist/client/assets/pdf.wasm", "\0asm\x01\0\0\0");
  write(root, "dist/client/assets/site.js", 'console.log("fixture");\n');
}

function cloudflareConfig() {
  return {
    name: "tammy-site-direct",
    main: "index.js",
    no_bundle: true,
    compatibility_date: "2026-05-15",
    compatibility_flags: ["nodejs_compat"],
    workers_dev: true,
    preview_urls: false,
    assets: {
      directory: "../client",
      binding: "ASSETS",
      not_found_handling: "none",
    },
  };
}

function built(t, target = "sites") {
  const state = prepared(t, target);
  workerFixture(state.snapshot, target);
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

test("direct Cloudflare configuration fixes Worker identity and asset routing without account or resource bindings", () => {
  assert.equal(CLOUDFLARE_WORKER_NAME, "tammy-site-direct");
  assert.doesNotThrow(() => assertCloudflareConfig(cloudflareConfig()));
});

test("direct Cloudflare configuration rejects changes that could select another deployment or bind resources", async (t) => {
  const invalid = [
    [
      "different Worker",
      (config) => {
        config.name = "another-worker";
      },
    ],
    [
      "different entrypoint",
      (config) => {
        config.main = "../../another.js";
      },
    ],
    [
      "unexpected rebundling",
      (config) => {
        config.no_bundle = false;
      },
    ],
    [
      "different compatibility date",
      (config) => {
        config.compatibility_date = "2026-01-01";
      },
    ],
    [
      "missing Node compatibility",
      (config) => {
        config.compatibility_flags = [];
      },
    ],
    [
      "workers.dev disabled",
      (config) => {
        config.workers_dev = false;
      },
    ],
    [
      "preview URLs enabled",
      (config) => {
        config.preview_urls = true;
      },
    ],
    [
      "asset path outside selected client tree",
      (config) => {
        config.assets.directory = "../../other-assets";
      },
    ],
    [
      "different assets binding",
      (config) => {
        config.assets.binding = "OTHER";
      },
    ],
    [
      "SPA fallback",
      (config) => {
        config.assets.not_found_handling = "single-page-application";
      },
    ],
    [
      "account pinned in source",
      (config) => {
        config.account_id = "0".repeat(32);
      },
    ],
    [
      "custom route",
      (config) => {
        config.routes = [{ pattern: "tammy.tax/*", zone_name: "tammy.tax" }];
      },
    ],
    [
      "legacy route",
      (config) => {
        config.route = "tammy.tax/*";
      },
    ],
    [
      "environment variables",
      (config) => {
        config.vars = { SHOULD_NOT_SHIP: "test-only" };
      },
    ],
    [
      "database binding",
      (config) => {
        config.d1_databases = [{ binding: "DB", database_id: "fixture" }];
      },
    ],
    [
      "bucket binding",
      (config) => {
        config.r2_buckets = [{ binding: "BUCKET", bucket_name: "fixture" }];
      },
    ],
    [
      "key-value binding",
      (config) => {
        config.kv_namespaces = [{ binding: "KV", id: "fixture" }];
      },
    ],
    [
      "service binding",
      (config) => {
        config.services = [{ binding: "SERVICE", service: "fixture" }];
      },
    ],
    [
      "durable object binding",
      (config) => {
        config.durable_objects = { bindings: [{ name: "OBJECT", class_name: "Fixture" }] };
      },
    ],
    [
      "queue producer",
      (config) => {
        config.queues = { producers: [{ binding: "QUEUE", queue: "fixture" }] };
      },
    ],
    [
      "scheduled work",
      (config) => {
        config.triggers = { crons: ["0 * * * *"] };
      },
    ],
  ];
  for (const [label, change] of invalid) {
    await t.test(label, () => {
      const config = cloudflareConfig();
      change(config);
      assert.throws(() => assertCloudflareConfig(config));
    });
  }
});

test("Cloudflare preparation preserves the GitHub workspace and identifies its deployment target", (t) => {
  const { root, snapshot, commitSha, provenance } = prepared(t, "cloudflare");
  assert.equal(provenance.target, "cloudflare");
  assert.equal(provenance.github.commit_sha, commitSha);
  for (const filename of [
    "pnpm-lock.yaml",
    "packages/invoices/src/model.ts",
    "apps/site/.openai/hosting.json",
  ]) {
    assert.deepEqual(
      readFileSync(path.join(snapshot, filename)),
      readFileSync(path.join(root, filename)),
    );
  }
  assert.equal(verifySource(snapshot).target, "cloudflare");
});

test("unknown deployment targets are rejected instead of falling back to Sites", (t) => {
  const { root, snapshot } = fixture(t);
  assert.throws(() => prepare(root, snapshot, "cloudflare-typo"));
  assert.equal(existsSync(snapshot), false);
});

test("direct Worker build and bundle preserve target and exact source/build provenance", (t) => {
  const { base, snapshot, provenance, build } = built(t, "cloudflare");
  assert.equal(build.target, "cloudflare");
  assert.equal(build.github.commit_sha, provenance.github.commit_sha);
  assert.equal(build.source_sha256, provenance.source_sha256);
  assert.equal(verify(snapshot).built.target, "cloudflare");
  const output = path.join(base, "direct-bundle");
  const manifest = bundle(snapshot, output);
  assert.equal(manifest.target, "cloudflare");
  assert.equal(manifest.source_sha256, provenance.source_sha256);
  assert.equal(manifest.build_sha256, build.build_sha256);
  assert.equal(manifest.publication, "not-performed");
  assert.equal(
    manifest.source_archive.sha256,
    sha256(readFileSync(path.join(output, "source.tar.gz"))),
  );
  assert.equal(existsSync(path.join(output, "dist/.openai")), false);
  assert.deepEqual(readJson(path.join(output, "dist/server/wrangler.json")), cloudflareConfig());
  const extracted = path.join(base, "direct-source");
  mkdirSync(extracted);
  execFileSync("tar", ["-xzf", path.join(output, "source.tar.gz"), "-C", extracted]);
  assert.equal(verifySource(extracted).target, "cloudflare");
});

test("Sites and direct Cloudflare artifacts cannot be relabeled interchangeably", async (t) => {
  for (const target of ["sites", "cloudflare"]) {
    await t.test(`${target} build record target mismatch`, (t) => {
      const { snapshot } = built(t, target);
      const record = readJson(path.join(snapshot, "build-provenance.json"));
      record.target = target === "sites" ? "cloudflare" : "sites";
      write(snapshot, "build-provenance.json", record);
      assert.throws(() => verify(snapshot), /target|provenance/i);
    });
  }
  await t.test("unknown source provenance target", (t) => {
    const { snapshot } = prepared(t, "cloudflare");
    const source = readJson(path.join(snapshot, "source-provenance.json"));
    source.target = "typo";
    write(snapshot, "source-provenance.json", source);
    assert.throws(() => verifySource(snapshot), /target|provenance/i);
  });
  await t.test("Sites packager refuses a direct artifact before resolving its helper", (t) => {
    const { base, snapshot } = built(t, "cloudflare");
    const output = path.join(base, "wrong-package");
    assert.throws(() => packageWorker(snapshot, output), /target|Cloudflare|Sites artifact/i);
    assert.equal(existsSync(output), false);
  });
});

test("direct builds reject Sites metadata and incomplete Worker/PDF output", async (t) => {
  await t.test("Sites hosting metadata cannot ship in the direct target", (t) => {
    const { snapshot } = prepared(t, "cloudflare");
    workerFixture(snapshot, "cloudflare");
    write(snapshot, "dist/.openai/hosting.json", hosting);
    assert.throws(() => recordBuild(snapshot, runtime), /Sites|openai|metadata/i);
    assert.equal(existsSync(path.join(snapshot, "build-provenance.json")), false);
  });
  for (const filename of ["server/index.js", "server/wrangler.json", "client/assets/pdf.wasm"]) {
    await t.test(`missing ${filename}`, (t) => {
      const { snapshot } = prepared(t, "cloudflare");
      workerFixture(snapshot, "cloudflare");
      rmSync(path.join(snapshot, "dist", filename));
      assert.throws(
        () => recordBuild(snapshot, runtime),
        /Missing Worker output|Missing invoice PDF WASM/,
      );
      assert.equal(existsSync(path.join(snapshot, "build-provenance.json")), false);
    });
  }
});

test("tracked Wrangler dev variables are rejected for every release target", async (t) => {
  for (const target of ["sites", "cloudflare", "vercel"]) {
    await t.test(target, (t) => {
      const { root, snapshot } = fixture(t);
      write(root, "apps/site/.dev.vars.production", "TEST_ONLY_SECRET=never-export\n");
      commit(root);
      assert.throws(() => prepare(root, snapshot, target), /sensitive|credential|secret/i);
      assert.equal(existsSync(snapshot), false);
    });
  }
});

test("credentials cannot enter any target's built inventory or retained artifact", async (t) => {
  for (const target of ["sites", "cloudflare", "vercel"]) {
    for (const filename of [
      "server/.dev.vars",
      "client/.dev.vars.preview",
      "client/.env.production",
    ]) {
      await t.test(`${target}: ${filename}`, (t) => {
        const { base, snapshot } = built(t, target);
        write(snapshot, `dist/${filename}`, "TEST_ONLY_SECRET=never-package\n");
        assert.throws(() => verify(snapshot), /sensitive|credential|secret/i);
        const output = path.join(base, "unsafe-bundle");
        assert.throws(() => bundle(snapshot, output), /sensitive|credential|secret/i);
        assert.equal(existsSync(output), false);
      });
    }
  }
});

const testToken = "fixture-token-not-a-real-credential";
const testAccount = "0123456789abcdef0123456789abcdef";
const testVersion = "00112233-4455-4677-8899-aabbccddeeff";
const cloudflareEnvKey = /^(CLOUDFLARE_|CF_|WRANGLER_|TAMMY_DEPLOY_TARGET$)/;

function deploymentReceipt() {
  return {
    type: "deploy",
    version: 1,
    worker_name: "tammy-site-direct",
    worker_name_overridden: false,
    version_id: testVersion,
    targets: ["https://tammy-site-direct.example.workers.dev"],
  };
}

function withEnvironment(overrides, action) {
  const original = { ...process.env };
  for (const key of Object.keys(process.env))
    if (cloudflareEnvKey.test(key)) delete process.env[key];
  for (const [key, value] of Object.entries(overrides)) {
    if (value === undefined) delete process.env[key];
    else process.env[key] = value;
  }
  const restore = () => {
    for (const key of Object.keys(process.env)) if (!(key in original)) delete process.env[key];
    for (const [key, value] of Object.entries(original)) process.env[key] = value;
  };
  let result;
  try {
    result = action();
  } catch (error) {
    restore();
    throw error;
  }
  if (result && typeof result.then === "function") return result.finally(restore);
  restore();
  return result;
}

// A deployment as the Cloudflare deployments endpoint reports it; created one minute in the future by default.
function fixtureDeployment(commitSha, overrides = {}) {
  return {
    id: "fixture-deployment",
    created_on: new Date(Date.now() + 60_000).toISOString(),
    annotations: { "workers/message": `GitHub ${commitSha}` },
    versions: [{ version_id: testVersion, percentage: 100 }],
    ...overrides,
  };
}

// In-process stand-in for api.cloudflare.com. Routes are keyed by the exact URL the release
// script must use; any other URL fails the call so an unexpected endpoint is visible.
function cloudflareApi(state, overrides = {}) {
  const base = `https://api.cloudflare.com/client/v4/accounts/${testAccount}`;
  const deployments = [fixtureDeployment(state.provenance.github.commit_sha)];
  const ok = (result) => ({
    status: 200,
    body: { success: true, errors: [], messages: [], result },
  });
  const routes = {
    [`${base}/workers/subdomain`]:
      overrides.accountSubdomain ?? ok({ subdomain: "fixture-subdomain" }),
    [`${base}/workers/scripts/tammy-site-direct/subdomain`]:
      overrides.scriptSubdomain ?? ok({ enabled: true, previews_enabled: false }),
    [`${base}/workers/scripts/tammy-site-direct/deployments`]:
      overrides.readback ?? ok({ deployments: overrides.deployments ?? deployments }),
  };
  // An array in overrides is a sequence: each call consumes one entry, the last entry repeats.
  const queues = Object.fromEntries(
    Object.entries(routes).map(([url, value]) => [url, Array.isArray(value) ? [...value] : null]),
  );
  const date = (overrides.date ?? new Date()).toUTCString();
  const calls = [];
  const fetcher = async (url, init) => {
    calls.push({
      url,
      authorization: init.headers.Authorization,
      signal: init.signal,
      redirect: init.redirect,
    });
    const queue = queues[url];
    const route = queue ? (queue.length > 1 ? queue.shift() : queue[0]) : routes[url];
    if (!route) throw new Error("unexpected fixture route");
    if (route.throws) throw new Error("fixture network failure");
    return new Response(JSON.stringify(route.body), {
      status: route.status,
      headers: { date },
    });
  };
  return { fetcher, calls };
}

function assertNoTokenLeak(output, error) {
  assert.equal(String(error?.message ?? error).includes(testToken), false);
  for (const name of readdirSync(output)) {
    assert.equal(readFileSync(path.join(output, name), "utf8").includes(testToken), false, name);
  }
}

const isWranglerCall = (entry) => entry.command === "wrangler.js";
// Tests never sleep between retries, and a call without a stub must not reach the network.
const deployOptions = (api) => ({ fetcher: api.fetcher, retryDelayMs: 0 });
const neverFetch = () => {
  throw new Error("fixture: unexpected Cloudflare network call");
};

function poisonedEnvironment() {
  return {
    GITHUB_ACTIONS: "true",
    CLOUDFLARE_API_TOKEN: testToken,
    CLOUDFLARE_ACCOUNT_ID: testAccount,
    CLOUDFLARE_API_BASE_URL: "https://invalid.example.test/spoofed",
    CF_API_BASE_URL: "https://invalid.example.test/spoofed",
    CF_API_TOKEN: "fixture-legacy-token",
    WRANGLER_API_BASE_URL: "https://invalid.example.test/spoofed",
    WRANGLER_NAME: "another-worker",
    WRANGLER_ENV: "production-override",
    WRANGLER_OUTPUT_FILE_PATH: "/invalid/fixture-output.jsonl",
    WRANGLER_SEND_METRICS: "true",
    CLOUDFLARE_LOAD_DEV_VARS_FROM_DOT_ENV: "true",
    CLOUDFLARE_INCLUDE_PROCESS_ENV: "true",
    TAMMY_DEPLOY_TARGET: "sites",
  };
}

function wrapperFixture(t, scenario = {}) {
  const state = built(t, "cloudflare");
  const observation = path.join(state.base, "command-observations.jsonl");
  const bin = path.join(state.base, "bin");
  mkdirSync(bin);
  const common = String.raw`
const fs = require("node:fs");
const path = require("node:path");
const args = process.argv.slice(2);
fs.appendFileSync(${JSON.stringify(observation)}, JSON.stringify({
  command: path.basename(process.argv[1]), args,
  environmentKeys: Object.keys(process.env).filter((key) => /^(CLOUDFLARE_|CF_|WRANGLER_|TAMMY_DEPLOY_TARGET$)/.test(key)).sort(),
  tokenPresent: Boolean(process.env.CLOUDFLARE_API_TOKEN),
  accountPresent: Boolean(process.env.CLOUDFLARE_ACCOUNT_ID),
  target: process.env.TAMMY_DEPLOY_TARGET,
  loadEnv: process.env.CLOUDFLARE_LOAD_DEV_VARS_FROM_DOT_ENV,
  includeEnv: process.env.CLOUDFLARE_INCLUDE_PROCESS_ENV,
  metrics: process.env.WRANGLER_SEND_METRICS,
}) + "\n");
`;
  const pnpmStub = String.raw`#!/usr/bin/env node
${common}
if (args.length === 1 && args[0] === "--version") process.stdout.write("11.15.0\n");
else if (JSON.stringify(args) === JSON.stringify(["--dir", "apps/site", "build"])) {
  const output = path.join(process.cwd(), "apps/site/dist");
  fs.mkdirSync(path.join(output, "server"), { recursive: true });
  fs.mkdirSync(path.join(output, "client/assets"), { recursive: true });
  fs.writeFileSync(path.join(output, "server/index.js"), "export default {};\n");
  fs.writeFileSync(path.join(output, "server/wrangler.json"), ${JSON.stringify(JSON.stringify(cloudflareConfig()))});
  fs.writeFileSync(path.join(output, "client/assets/pdf.wasm"), "fixture-wasm");
} else { process.stderr.write("Unexpected fixture pnpm invocation"); process.exit(91); }
`;
  writeFileSync(path.join(bin, "pnpm"), pnpmStub, { mode: 0o755 });
  write(state.snapshot, "apps/site/node_modules/wrangler/package.json", { version: "4.92.0" });
  const wranglerStub = String.raw`${common}
const scenario = ${JSON.stringify(scenario)};
if (args[0] !== "deploy" || args[1] !== "--config") process.exit(92);
if (scenario.exitCode) process.exit(scenario.exitCode);
if (args.includes("--dry-run")) {
  const outdir = args[args.indexOf("--outdir") + 1];
  fs.mkdirSync(outdir, { recursive: true });
  fs.writeFileSync(path.join(outdir, "index.js"), "export default {};\n");
  if (scenario.secretOutput) fs.writeFileSync(path.join(outdir, ".dev.vars"), "TEST_ONLY_SECRET=never-retain");
} else if (!scenario.noReceipt) {
  const output = process.env.WRANGLER_OUTPUT_FILE_PATH;
  if (!output) process.exit(93);
  fs.writeFileSync(output, scenario.malformedReceipt ? "not-json\n" : JSON.stringify(scenario.receipt || ${JSON.stringify(deploymentReceipt())}) + "\n");
}
`;
  write(state.snapshot, "apps/site/node_modules/wrangler/bin/wrangler.js", wranglerStub);
  state.environment = {
    ...poisonedEnvironment(),
    PATH: `${bin}${path.delimiter}${process.env.PATH}`,
  };
  state.observations = () =>
    existsSync(observation)
      ? readFileSync(observation, "utf8")
          .trim()
          .split("\n")
          .filter(Boolean)
          .map((line) => JSON.parse(line))
      : [];
  return state;
}

function assertNoCredentialsOrOverrides(observation, allowed = []) {
  assert.equal(observation.tokenPresent, allowed.includes("CLOUDFLARE_API_TOKEN"));
  assert.equal(observation.accountPresent, allowed.includes("CLOUDFLARE_ACCOUNT_ID"));
  const expected = [
    "WRANGLER_SEND_METRICS",
    "WRANGLER_WRITE_LOGS",
    "CLOUDFLARE_LOAD_DEV_VARS_FROM_DOT_ENV",
    "CLOUDFLARE_INCLUDE_PROCESS_ENV",
    ...allowed,
  ].sort();
  assert.deepEqual(observation.environmentKeys, expected);
  assert.equal(observation.loadEnv, "false");
  assert.equal(observation.includeEnv, "false");
  assert.equal(observation.metrics, "false");
}

test("Cloudflare environment isolates build and dry-run subprocesses from credentials and deployment overrides", () => {
  withEnvironment(poisonedEnvironment(), () => {
    const env = cloudflareEnvironment();
    assert.deepEqual(
      Object.keys(env)
        .filter((key) => cloudflareEnvKey.test(key))
        .sort(),
      [
        "CLOUDFLARE_INCLUDE_PROCESS_ENV",
        "CLOUDFLARE_LOAD_DEV_VARS_FROM_DOT_ENV",
        "WRANGLER_SEND_METRICS",
        "WRANGLER_WRITE_LOGS",
      ],
    );
    assert.equal(env.CLOUDFLARE_INCLUDE_PROCESS_ENV, "false");
    assert.equal(env.CLOUDFLARE_LOAD_DEV_VARS_FROM_DOT_ENV, "false");
    assert.equal(env.WRANGLER_SEND_METRICS, "false");
    assert.equal(env.WRANGLER_WRITE_LOGS, "false");
    assert.equal(env.CI, "true");
    assert.equal(env.PATH, process.env.PATH);
    assert.equal(process.env.CLOUDFLARE_API_TOKEN, testToken);
  });
});

test("deployment environment admits only the explicit existing token/account and rejects missing or malformed credentials", async (t) => {
  await t.test("authorized authentication fields only", () => {
    withEnvironment(poisonedEnvironment(), () => {
      const env = cloudflareEnvironment(true);
      assert.equal(env.CLOUDFLARE_API_TOKEN, testToken);
      assert.equal(env.CLOUDFLARE_ACCOUNT_ID, testAccount);
      for (const key of [
        "CF_API_TOKEN",
        "CF_API_BASE_URL",
        "CLOUDFLARE_API_BASE_URL",
        "WRANGLER_API_BASE_URL",
        "WRANGLER_NAME",
        "WRANGLER_ENV",
        "WRANGLER_OUTPUT_FILE_PATH",
        "TAMMY_DEPLOY_TARGET",
      ])
        assert.equal(env[key], undefined, key);
    });
  });
  for (const [label, overrides] of [
    ["missing token", { CLOUDFLARE_API_TOKEN: undefined }],
    ["empty token", { CLOUDFLARE_API_TOKEN: "" }],
    ["missing account", { CLOUDFLARE_ACCOUNT_ID: undefined }],
    ["short account", { CLOUDFLARE_ACCOUNT_ID: "0123" }],
    ["non-hex account", { CLOUDFLARE_ACCOUNT_ID: "g".repeat(32) }],
  ]) {
    await t.test(label, () => {
      withEnvironment({ ...poisonedEnvironment(), ...overrides }, () => {
        assert.throws(() => cloudflareEnvironment(true), /existing authorized/);
      });
    });
  }
});

test("local deployment environment permits an existing cached Wrangler session without minting a token", () => {
  withEnvironment(
    { ...poisonedEnvironment(), GITHUB_ACTIONS: undefined, CLOUDFLARE_API_TOKEN: undefined },
    () => {
      const env = cloudflareEnvironment(true);
      assert.equal(env.CLOUDFLARE_ACCOUNT_ID, testAccount);
      assert.equal(env.CLOUDFLARE_API_TOKEN, undefined);
      assert.equal(env.CI, "true");
      assert.equal(env.CLOUDFLARE_API_BASE_URL, undefined);
      assert.equal(env.WRANGLER_ENV, undefined);
    },
  );
});

test("Wrangler receipt validation accepts the pinned deploy schema and one intended workers.dev URL", () => {
  const result = cloudflareDeploymentResult([
    { type: "unrelated-event", version: 1 },
    deploymentReceipt(),
  ]);
  assert.deepEqual(result, {
    version_id: testVersion,
    url: "https://tammy-site-direct.example.workers.dev/",
  });
});

test("Wrangler receipt validation refuses to claim success for missing, malformed or redirected deployment evidence", async (t) => {
  await t.test("no deployment receipt", () =>
    assert.throws(() => cloudflareDeploymentResult([]), /uncertain/),
  );
  const invalid = [
    [
      "unknown receipt schema",
      (receipt) => {
        receipt.version = 2;
      },
    ],
    [
      "missing receipt schema",
      (receipt) => {
        delete receipt.version;
      },
    ],
    [
      "another Worker",
      (receipt) => {
        receipt.worker_name = "another-worker";
      },
    ],
    [
      "overridden Worker",
      (receipt) => {
        receipt.worker_name_overridden = true;
      },
    ],
    [
      "missing version ID",
      (receipt) => {
        delete receipt.version_id;
      },
    ],
    [
      "invalid version ID",
      (receipt) => {
        receipt.version_id = "-".repeat(36);
      },
    ],
    [
      "no targets",
      (receipt) => {
        receipt.targets = [];
      },
    ],
    [
      "multiple targets",
      (receipt) => {
        receipt.targets.push("https://tammy.tax");
      },
    ],
    [
      "custom domain",
      (receipt) => {
        receipt.targets = ["https://tammy.tax"];
      },
    ],
    [
      "non-HTTPS",
      (receipt) => {
        receipt.targets = ["http://tammy-site-direct.example.workers.dev"];
      },
    ],
    [
      "credentials in URL",
      (receipt) => {
        receipt.targets = ["https://fixture@tammy-site-direct.example.workers.dev"];
      },
    ],
    [
      "unexpected port",
      (receipt) => {
        receipt.targets = ["https://tammy-site-direct.example.workers.dev:8443"];
      },
    ],
    [
      "unexpected path",
      (receipt) => {
        receipt.targets = ["https://tammy-site-direct.example.workers.dev/admin"];
      },
    ],
    [
      "unexpected query",
      (receipt) => {
        receipt.targets = ["https://tammy-site-direct.example.workers.dev/?x=1"];
      },
    ],
    [
      "unexpected fragment",
      (receipt) => {
        receipt.targets = ["https://tammy-site-direct.example.workers.dev/#x"];
      },
    ],
    [
      "spoofed workers.dev suffix",
      (receipt) => {
        receipt.targets = ["https://tammy-site-direct.example.workers.dev.invalid.example"];
      },
    ],
  ];
  for (const [label, mutate] of invalid) {
    await t.test(label, () => {
      const receipt = deploymentReceipt();
      mutate(receipt);
      assert.throws(() => cloudflareDeploymentResult([receipt]));
    });
  }
});

test("build and dry-run wrappers strip authentication even when the calling environment contains it", async (t) => {
  await t.test("build subprocesses", (t) => {
    const state = wrapperFixture(t);
    withEnvironment(state.environment, () => {
      const record = build(state.snapshot);
      assert.equal(record.target, "cloudflare");
    });
    const observations = state.observations();
    assert.equal(observations.length, 2);
    assert.deepEqual(observations[0].args, ["--version"]);
    assertNoCredentialsOrOverrides(observations[0]);
    assert.deepEqual(observations[1].args, ["--dir", "apps/site", "build"]);
    assertNoCredentialsOrOverrides(observations[1], ["TAMMY_DEPLOY_TARGET"]);
    assert.equal(observations[1].target, "cloudflare");
  });
  await t.test("Wrangler dry-run", (t) => {
    const state = wrapperFixture(t);
    const output = path.join(state.base, "dry-run");
    const record = withEnvironment(state.environment, () =>
      cloudflareDryRun(state.snapshot, output),
    );
    assert.equal(record.publication, "not-performed");
    assert.equal(record.build_sha256, state.build.build_sha256);
    assert.equal(
      record.files.some((entry) => entry.path === "worker/index.js"),
      true,
    );
    const observations = state.observations();
    assert.equal(observations.length, 2);
    for (const observation of observations) assertNoCredentialsOrOverrides(observation);
    assert.deepEqual(observations[1].args, [
      "deploy",
      "--config",
      path.join(state.snapshot, "dist/server/wrangler.json"),
      "--dry-run",
      "--outdir",
      path.join(output, "worker"),
    ]);
  });
  await t.test("dry-run refuses retained secret files", (t) => {
    const state = wrapperFixture(t, { secretOutput: true });
    const output = path.join(state.base, "dry-run");
    withEnvironment(state.environment, () =>
      assert.throws(() => cloudflareDryRun(state.snapshot, output), /sensitive artifact path/),
    );
    assert.equal(existsSync(path.join(output, "dry-run-record.json")), false);
  });
});

test("deployment wrapper records only an authenticated simulated receipt and never passes endpoint/name overrides", async (t) => {
  const state = wrapperFixture(t);
  const output = path.join(state.base, "deployment");
  const api = cloudflareApi(state);
  const result = await withEnvironment(state.environment, () =>
    deployCloudflare(state.snapshot, output, deployOptions(api)),
  );
  assert.equal(result.status, "published");
  assert.deepEqual(
    api.calls.map((call) => call.url),
    [
      `https://api.cloudflare.com/client/v4/accounts/${testAccount}/workers/subdomain`,
      `https://api.cloudflare.com/client/v4/accounts/${testAccount}/workers/scripts/tammy-site-direct/subdomain`,
    ],
  );
  for (const call of api.calls) {
    assert.equal(call.authorization, `Bearer ${testToken}`);
    assert.ok(call.signal instanceof AbortSignal);
    assert.equal(call.redirect, "error");
  }
  assert.equal(result.live_verification, "pending");
  assert.equal(result.version_id, testVersion);
  assert.equal(result.url, "https://tammy-site-direct.example.workers.dev/");
  assert.equal(result.github.commit_sha, state.provenance.github.commit_sha);
  assert.equal(result.build_sha256, state.build.build_sha256);
  assert.deepEqual(readJson(path.join(output, "deployment-record.json")), result);
  assert.equal(
    readFileSync(path.join(output, "deployment-record.json"), "utf8").includes(testToken),
    false,
  );
  const observations = state.observations();
  assert.equal(observations.length, 2);
  assertNoCredentialsOrOverrides(observations[0]);
  assertNoCredentialsOrOverrides(observations[1], [
    "CLOUDFLARE_API_TOKEN",
    "CLOUDFLARE_ACCOUNT_ID",
    "WRANGLER_OUTPUT_FILE_PATH",
  ]);
  assert.deepEqual(observations[1].args, [
    "deploy",
    "--config",
    path.join(state.snapshot, "dist/server/wrangler.json"),
    "--message",
    `GitHub ${state.provenance.github.commit_sha}`,
  ]);
});

test("deployment wrapper marks an uncertain outcome rather than success when Wrangler output is missing or invalid", async (t) => {
  for (const [label, scenario] of [
    ["no receipt despite exit zero", { noReceipt: true }],
    ["malformed JSONL", { malformedReceipt: true }],
    [
      "wrong Worker receipt",
      { receipt: { ...deploymentReceipt(), worker_name: "another-worker" } },
    ],
    ["Wrangler failure without matching deployment", { exitCode: 27 }],
  ]) {
    await t.test(label, async (t) => {
      const state = wrapperFixture(t, scenario);
      const output = path.join(state.base, "deployment");
      const api = cloudflareApi(state, { deployments: [] });
      await withEnvironment(state.environment, () =>
        assert.rejects(deployCloudflare(state.snapshot, output, deployOptions(api))),
      );
      const record = readJson(path.join(output, "deployment-record.json"));
      assert.equal(record.status, "outcome-uncertain");
      assert.equal(record.live_verification, "pending");
      assert.equal(record.version_id, null);
      assert.equal(record.url, null);
      assert.equal(
        state.observations().filter((entry) => entry.command === "wrangler.js").length,
        1,
      );
    });
  }
});

test("deployment wrapper fails before invoking Wrangler when credentials or pinned CLI are unavailable", async (t) => {
  await t.test("missing existing token", async (t) => {
    const state = wrapperFixture(t);
    const output = path.join(state.base, "deployment");
    await withEnvironment({ ...state.environment, CLOUDFLARE_API_TOKEN: undefined }, () =>
      assert.rejects(
        deployCloudflare(state.snapshot, output, { fetcher: neverFetch }),
        /existing authorized/,
      ),
    );
    assert.equal(existsSync(output), false);
    assert.equal(
      state.observations().some((entry) => entry.command === "wrangler.js"),
      false,
    );
  });
  await t.test("unexpected Wrangler version", async (t) => {
    const state = wrapperFixture(t);
    write(state.snapshot, "apps/site/node_modules/wrangler/package.json", { version: "4.93.0" });
    const output = path.join(state.base, "deployment");
    await withEnvironment(state.environment, () =>
      assert.rejects(
        deployCloudflare(state.snapshot, output, { fetcher: neverFetch }),
        /pinned Wrangler 4.92.0/,
      ),
    );
    assert.equal(existsSync(output), false);
    assert.equal(
      state.observations().some((entry) => entry.command === "wrangler.js"),
      false,
    );
  });
});

test("Cloudflare credential preflight stops before Wrangler and names the failing endpoint with codes only", async (t) => {
  const denied = {
    status: 403,
    body: {
      success: false,
      errors: [{ code: 10000, message: "Authentication error" }],
      messages: [],
      result: null,
    },
  };
  for (const [label, override, expected] of [
    [
      "account-subdomain",
      { accountSubdomain: denied },
      /^CLOUDFLARE_TOKEN_PREFLIGHT_FAILED: account-subdomain HTTP 403, Cloudflare codes 10000\./,
    ],
    [
      "script-subdomain",
      { scriptSubdomain: denied },
      /^CLOUDFLARE_TOKEN_PREFLIGHT_FAILED: script-subdomain HTTP 403, Cloudflare codes 10000\./,
    ],
  ]) {
    await t.test(label, async (t) => {
      const state = wrapperFixture(t);
      const output = path.join(state.base, "deployment");
      const api = cloudflareApi(state, override);
      await withEnvironment(state.environment, () =>
        assert.rejects(deployCloudflare(state.snapshot, output, deployOptions(api)), (error) => {
          assert.match(error.message, expected);
          assert.doesNotMatch(error.message, /Authentication error/);
          assertNoTokenLeak(output, error);
          return true;
        }),
      );
      assert.equal(state.observations().some(isWranglerCall), false);
      // A 403 is an authorization answer, not a transient failure: each read happens once.
      assert.equal(api.calls.length, 2);
      assert.equal(new Set(api.calls.map((call) => call.url)).size, 2);
      const record = readJson(path.join(output, "deployment-record.json"));
      assert.equal(record.status, "preflight-failed");
      assert.equal(record.publication, "not-performed");
      assert.equal(record.version_id, null);
      assert.equal(record.url, null);
    });
  }
});

test("Cloudflare credential preflight reads both pinned subdomain endpoints with the token before Wrangler runs", async (t) => {
  const state = wrapperFixture(t);
  const output = path.join(state.base, "deployment");
  const api = cloudflareApi(state);
  const result = await withEnvironment(state.environment, () =>
    deployCloudflare(state.snapshot, output, deployOptions(api)),
  );
  assert.equal(result.status, "published");
  assert.equal(state.observations().filter(isWranglerCall).length, 1);
  assert.equal(api.calls.length, 2);
  for (const call of api.calls)
    assert.match(call.url, /^https:\/\/api\.cloudflare\.com\/client\/v4\/accounts\/[a-f0-9]{32}\//);
  assert.equal(api.calls[0].url.endsWith(`/accounts/${testAccount}/workers/subdomain`), true);
  assert.equal(
    api.calls[1].url.endsWith(
      `/accounts/${testAccount}/workers/scripts/tammy-site-direct/subdomain`,
    ),
    true,
  );
  assertNoTokenLeak(output);
});

test("failed Wrangler exit reconciles to published-unconfirmed when the newest deployment matches this commit", async (t) => {
  const state = wrapperFixture(t, { exitCode: 27 });
  const output = path.join(state.base, "deployment");
  const api = cloudflareApi(state);
  await withEnvironment(state.environment, () =>
    assert.rejects(deployCloudflare(state.snapshot, output, deployOptions(api)), (error) => {
      assert.match(error.message, /Command failed/);
      assertNoTokenLeak(output, error);
      return true;
    }),
  );
  const record = readJson(path.join(output, "deployment-record.json"));
  assert.equal(record.status, "published-unconfirmed");
  assert.equal(record.readback, "matched");
  assert.equal(record.version_id, testVersion);
  assert.equal(record.url, null);
  assert.equal(record.live_verification, "pending");
  assert.equal(state.observations().filter(isWranglerCall).length, 1);
  const readback = api.calls.at(-1);
  assert.equal(
    readback.url,
    `https://api.cloudflare.com/client/v4/accounts/${testAccount}/workers/scripts/tammy-site-direct/deployments`,
  );
  assert.equal(readback.authorization, `Bearer ${testToken}`);
});

test("failed Wrangler exit stays outcome-uncertain when readback cannot prove this deployment", async (t) => {
  const older = (state) =>
    fixtureDeployment(state.provenance.github.commit_sha, {
      id: "older",
      created_on: new Date(Date.now() + 30_000).toISOString(),
    });
  const newerOtherCommit = () =>
    fixtureDeployment("8bb2c6d56393c55fc3d88fd527709f0f6f2688fa", {
      id: "newer",
      created_on: new Date(Date.now() + 90_000).toISOString(),
    });
  const denied = {
    status: 500,
    body: { success: false, errors: [{ code: 10000 }], messages: [], result: null },
  };
  for (const [label, overrides, readback] of [
    [
      "message from another commit",
      () => ({
        deployments: [fixtureDeployment("8bb2c6d56393c55fc3d88fd527709f0f6f2688fa")],
      }),
      "mismatch",
    ],
    [
      "deployment created before this run",
      (state) => ({
        deployments: [
          fixtureDeployment(state.provenance.github.commit_sha, {
            created_on: new Date(Date.now() - 3_600_000).toISOString(),
          }),
        ],
      }),
      "mismatch",
    ],
    [
      "newest deployment is another commit",
      (state) => ({ deployments: [newerOtherCommit(), older(state)] }),
      "mismatch",
    ],
    ["no deployments listed", () => ({ deployments: [] }), "mismatch"],
    ["readback HTTP error", () => ({ readback: denied }), "unavailable"],
    ["readback network failure", () => ({ readback: { throws: true } }), "unavailable"],
  ]) {
    await t.test(label, async (t) => {
      const state = wrapperFixture(t, { exitCode: 27 });
      const output = path.join(state.base, "deployment");
      const api = cloudflareApi(state, overrides(state));
      await withEnvironment(state.environment, () =>
        assert.rejects(deployCloudflare(state.snapshot, output, deployOptions(api)), (error) => {
          assert.match(error.message, /Command failed/);
          assertNoTokenLeak(output, error);
          return true;
        }),
      );
      const record = readJson(path.join(output, "deployment-record.json"));
      assert.equal(record.status, "outcome-uncertain");
      assert.equal(record.readback, readback);
      assert.equal(record.version_id, null);
      assert.equal(record.url, null);
      assert.equal(state.observations().filter(isWranglerCall).length, 1);
    });
  }
});

const reconcileCommit = "fa5ade8a0ea33e455740bec24074d1d3b4e12ac6";
const unavailable = {
  status: 503,
  body: { success: false, errors: [], messages: [], result: null },
};
const healthy = (result) => ({
  status: 200,
  body: { success: true, errors: [], messages: [], result },
});

test("Cloudflare preflight network failure names the endpoint, retries once and stops before Wrangler", async (t) => {
  const state = wrapperFixture(t);
  const output = path.join(state.base, "deployment");
  const api = cloudflareApi(state, { accountSubdomain: { throws: true } });
  await withEnvironment(state.environment, () =>
    assert.rejects(deployCloudflare(state.snapshot, output, deployOptions(api)), (error) => {
      assert.match(
        error.message,
        /^CLOUDFLARE_TOKEN_PREFLIGHT_FAILED: account-subdomain HTTP unavailable, Cloudflare codes none\./,
      );
      assertNoTokenLeak(output, error);
      return true;
    }),
  );
  const accountCalls = api.calls.filter((call) => call.url.endsWith("/workers/subdomain"));
  assert.equal(accountCalls.length, 2);
  assert.equal(state.observations().some(isWranglerCall), false);
});

test("Cloudflare preflight retries a 503 once and proceeds when the retry succeeds", async (t) => {
  const state = wrapperFixture(t);
  const output = path.join(state.base, "deployment");
  const api = cloudflareApi(state, {
    accountSubdomain: [unavailable, healthy({ subdomain: "fixture-subdomain" })],
  });
  const result = await withEnvironment(state.environment, () =>
    deployCloudflare(state.snapshot, output, deployOptions(api)),
  );
  assert.equal(result.status, "published");
  assert.equal(api.calls.filter((call) => call.url.endsWith("/workers/subdomain")).length, 2);
  assert.equal(state.observations().filter(isWranglerCall).length, 1);
});

test("failed Wrangler readback retries a 503 once before reconciling", async (t) => {
  const state = wrapperFixture(t, { exitCode: 27 });
  const output = path.join(state.base, "deployment");
  const api = cloudflareApi(state, {
    readback: [
      unavailable,
      healthy({ deployments: [fixtureDeployment(state.provenance.github.commit_sha)] }),
    ],
  });
  await withEnvironment(state.environment, () =>
    assert.rejects(deployCloudflare(state.snapshot, output, deployOptions(api)), /Command failed/),
  );
  const record = readJson(path.join(output, "deployment-record.json"));
  assert.equal(record.status, "published-unconfirmed");
  assert.equal(record.readback, "matched");
  assert.equal(api.calls.filter((call) => call.url.endsWith("/deployments")).length, 2);
});

test("readback start time comes from Cloudflare's Date header rather than the local clock", async (t) => {
  const state = wrapperFixture(t, { exitCode: 27 });
  const output = path.join(state.base, "deployment");
  // The fixture deployment is one minute ahead of the local clock but an hour behind this header.
  const api = cloudflareApi(state, { date: new Date(Date.now() + 3_600_000) });
  await withEnvironment(state.environment, () =>
    assert.rejects(deployCloudflare(state.snapshot, output, deployOptions(api)), /Command failed/),
  );
  const record = readJson(path.join(output, "deployment-record.json"));
  assert.equal(record.status, "outcome-uncertain");
  assert.equal(record.readback, "mismatch");
});

test("readback floors the start time to one second for created_on values at -0.5 s, +0.5 s and -1.5 s", () => {
  const start = Date.parse("2026-11-11T05:38:36Z") + 500;
  const at = (ms) => [
    fixtureDeployment(reconcileCommit, { created_on: new Date(ms).toISOString() }),
  ];
  assert.deepEqual(classifyReadback(at(start - 500), reconcileCommit, start), {
    readback: "matched",
    version_id: testVersion,
  });
  assert.equal(classifyReadback(at(start + 500), reconcileCommit, start).readback, "matched");
  assert.equal(classifyReadback(at(start - 1500), reconcileCommit, start).readback, "mismatch");
});

test("readback refuses deployments that cannot prove this run", () => {
  const cases = [
    ["missing commit annotation", { annotations: {} }],
    ["unparseable created_on", { created_on: "not-a-date" }],
    ["non-UUID version id", { versions: [{ version_id: "not-a-uuid", percentage: 100 }] }],
    ["version percentage below 100", { versions: [{ version_id: testVersion, percentage: 50 }] }],
    [
      "more than one version",
      {
        versions: [
          { version_id: testVersion, percentage: 50 },
          { version_id: "00112233-4455-4677-8899-aabbccddef00", percentage: 50 },
        ],
      },
    ],
  ];
  for (const [label, patch] of cases) {
    assert.equal(
      classifyReadback(
        [fixtureDeployment(reconcileCommit, patch)],
        reconcileCommit,
        Date.now() - 5000,
      ).readback,
      "mismatch",
      label,
    );
  }
});

const vercelIds = { VERCEL_ORG_ID: "team_fixture0org", VERCEL_PROJECT_ID: "prj_fixture0project" };

function withEnv(t, values) {
  const saved = Object.fromEntries(
    Object.keys(process.env)
      .filter((key) => /^(VERCEL_|GITHUB_ACTIONS$)/.test(key))
      .map((key) => [key, process.env[key]]),
  );
  for (const key of Object.keys(saved)) delete process.env[key];
  Object.assign(process.env, values);
  t.after(() => {
    for (const key of Object.keys(process.env))
      if (/^(VERCEL_|GITHUB_ACTIONS$)/.test(key)) delete process.env[key];
    Object.assign(process.env, saved);
  });
}

function vercelBuilt(t) {
  const state = built(t, "vercel");
  state.output = path.join(state.base, "vercel-output");
  state.manifest = vercelOutput(state.snapshot, state.output);
  return state;
}

test("a Vercel static export becomes reviewed prebuilt output with clean URLs", (t) => {
  const { base, snapshot, output, manifest, commitSha, build: record } = vercelBuilt(t);
  assert.equal(record.target, "vercel");
  assert.equal(manifest.github.commit_sha, commitSha);
  assert.equal(manifest.build_sha256, record.build_sha256);
  assert.equal(manifest.publication, "not-performed");
  const staticRoot = path.join(output, ".vercel/output/static");
  const listed = execFileSync("find", [".", "-type", "f"], { cwd: staticRoot, encoding: "utf8" })
    .trim()
    .split("\n")
    .map((name) => name.slice(2))
    .sort();
  assert.deepEqual(listed, [
    "404.html",
    "_next/static/chunks/app.js",
    "assets/pdf.wasm",
    "index.html",
    "index.rsc",
    "tools.html",
    "tools/gst-calculator.html",
  ]);
  assert.equal(manifest.files, listed.length);
  for (const name of listed)
    assert.equal(
      sha256(readFileSync(path.join(staticRoot, name))),
      sha256(readFileSync(path.join(snapshot, "dist/client", name))),
    );
  const config = readJson(path.join(output, ".vercel/output/config.json"));
  assert.equal(config.version, 3);
  assert.deepEqual(config.routes, VERCEL_ROUTES);
  assert.deepEqual(config.overrides, {
    "404.html": { path: "404" },
    "index.html": { path: "index" },
    "tools.html": { path: "tools" },
    "tools/gst-calculator.html": { path: "tools/gst-calculator" },
  });
  assert.equal(
    manifest.config_sha256,
    sha256(readFileSync(path.join(output, ".vercel/output/config.json"))),
  );
  const bundled = bundle(snapshot, path.join(base, "vercel-bundle"));
  assert.equal(bundled.kind, "vercel-static-build");
  assert.equal(bundled.vercel.project, "tammy-site");
});

test("Vercel builds reject Sites metadata, Worker configuration and incomplete exports", async (t) => {
  await t.test("Sites hosting metadata", (t) => {
    const { snapshot } = prepared(t, "vercel");
    workerFixture(snapshot, "vercel");
    write(snapshot, "dist/.openai/hosting.json", hosting);
    assert.throws(() => recordBuild(snapshot, runtime), /Sites deployment metadata/);
  });
  await t.test("Worker configuration", (t) => {
    const { snapshot } = prepared(t, "vercel");
    workerFixture(snapshot, "vercel");
    write(snapshot, "dist/server/wrangler.json", cloudflareConfig());
    assert.throws(() => recordBuild(snapshot, runtime), /Worker configuration/);
  });
  for (const filename of ["client/index.html", "client/404.html", "client/assets/pdf.wasm"]) {
    await t.test(`missing ${filename}`, (t) => {
      const { snapshot } = prepared(t, "vercel");
      workerFixture(snapshot, "vercel");
      rmSync(path.join(snapshot, "dist", filename));
      assert.throws(
        () => recordBuild(snapshot, runtime),
        /Missing static export output|Missing invoice PDF WASM/,
      );
      assert.equal(existsSync(path.join(snapshot, "build-provenance.json")), false);
    });
  }
});

test("Vercel output refuses other targets and tampered or extra deployment files", async (t) => {
  await t.test("a direct Cloudflare build cannot become Vercel output", (t) => {
    const { base, snapshot } = built(t, "cloudflare");
    const output = path.join(base, "wrong-vercel");
    assert.throws(() => vercelOutput(snapshot, output), /vercel target/);
    assert.equal(existsSync(output), false);
  });
  const deployNever = () => assert.fail("The CLI must not run for rejected output");
  for (const [name, tamper, message] of [
    [
      "changed page",
      (o) => write(o, ".vercel/output/static/tools.html", "<h1>Edited</h1>\n"),
      /differs from the verified build/,
    ],
    [
      "extra static file",
      (o) => write(o, ".vercel/output/static/extra.html", "x"),
      /differs from the verified build/,
    ],
    [
      "changed routing",
      (o) => {
        const c = readJson(path.join(o, ".vercel/output/config.json"));
        c.routes = [];
        write(o, ".vercel/output/config.json", c);
      },
      /routing configuration differs/,
    ],
    [
      "extra output file",
      (o) => write(o, ".vercel/output/functions/x.func/index.js", "x"),
      /Unexpected Vercel output files/,
    ],
  ]) {
    await t.test(name, (t) => {
      withEnv(t, vercelIds);
      const { snapshot, output } = vercelBuilt(t);
      tamper(output);
      assert.throws(() => deployVercel(snapshot, output, deployNever), message);
      assert.equal(existsSync(path.join(output, "deployment-record.json")), false);
    });
  }
});

test("Vercel deployment takes credentials only from the environment and records the URL", async (t) => {
  await t.test("missing project identity stops before the CLI", (t) => {
    withEnv(t, { VERCEL_TOKEN: testToken });
    const { snapshot, output } = vercelBuilt(t);
    assert.throws(
      () => deployVercel(snapshot, output, () => assert.fail("CLI must not run")),
      /VERCEL_ORG_ID and VERCEL_PROJECT_ID/,
    );
    assert.equal(existsSync(path.join(output, "deployment-record.json")), false);
  });
  await t.test("GitHub Actions requires a token", (t) => {
    withEnv(t, { ...vercelIds, GITHUB_ACTIONS: "true" });
    assert.throws(() => vercelEnvironment(), /VERCEL_TOKEN/);
  });
  await t.test("published deployment", (t) => {
    withEnv(t, { ...vercelIds, VERCEL_TOKEN: testToken, VERCEL_SCOPE_EXTRA: "dropped" });
    const { snapshot, output, manifest, commitSha } = vercelBuilt(t);
    const calls = [];
    const record = deployVercel(snapshot, output, (command, args, cwd, env) => {
      calls.push({ command, args, cwd, env });
      return "Vercel CLI notice\nhttps://tammy-site-abc123-gamma.vercel.app\n";
    });
    assert.equal(calls.length, 1);
    const [call] = calls;
    assert.equal(call.command, "npx");
    assert.deepEqual(call.args, [
      "--yes",
      `vercel@${VERCEL_CLI_VERSION}`,
      "deploy",
      "--prebuilt",
      "--prod",
      "--yes",
    ]);
    assert.equal(call.cwd, realpathSync(output));
    assert.ok(!call.args.join(" ").includes(testToken), "token must not be a CLI argument");
    assert.equal(call.env.VERCEL_TOKEN, testToken);
    assert.equal(call.env.VERCEL_ORG_ID, vercelIds.VERCEL_ORG_ID);
    assert.equal(call.env.VERCEL_PROJECT_ID, vercelIds.VERCEL_PROJECT_ID);
    assert.equal(call.env.VERCEL_SCOPE_EXTRA, undefined);
    assert.equal(record.status, "published");
    assert.equal(record.url, "https://tammy-site-abc123-gamma.vercel.app");
    assert.equal(record.github.commit_sha, commitSha);
    assert.equal(record.static_sha256, manifest.static_sha256);
    const saved = readJson(path.join(output, "deployment-record.json"));
    assert.deepEqual(saved, record);
    assert.ok(!JSON.stringify(saved).includes(testToken));
    assert.throws(
      () => deployVercel(snapshot, output, () => assert.fail("no second deploy")),
      /already exists/,
    );
  });
  for (const [name, cli] of [
    [
      "CLI failure",
      () => {
        throw new Error("fixture CLI failure");
      },
    ],
    ["no deployment URL", () => "Error: something went wrong\n"],
  ]) {
    await t.test(name, (t) => {
      withEnv(t, { ...vercelIds, VERCEL_TOKEN: testToken });
      const { snapshot, output } = vercelBuilt(t);
      assert.throws(() => deployVercel(snapshot, output, cli));
      assert.equal(
        readJson(path.join(output, "deployment-record.json")).status,
        "outcome-uncertain",
      );
    });
  }
});
