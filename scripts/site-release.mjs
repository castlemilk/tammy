import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmodSync,
  cpSync,
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { isDeepStrictEqual } from "node:util";

export const PROJECT_ID = "appgprj_6a93f33631748191b7c064c033f72b53";
export const CLOUDFLARE_WORKER_NAME = "tammy-site-direct";
export const TARGETS = ["sites", "cloudflare", "vercel"];
export const VERCEL_PROJECT_NAME = "tammy-site";
export const VERCEL_CLI_VERSION = "56.4.1";
// Routing that Vercel CLI 56.4.1 `vercel build` generates for a static folder with
// cleanUrls: true, trailingSlash: false and the two header rules below. Emitting it
// here keeps the deployed bytes exactly the verified build output.
export const VERCEL_ROUTES = [
  { src: "^/(?:(.+)/)?index(?:\\.html)?/?$", headers: { Location: "/$1" }, status: 308 },
  { src: "^/(.*)\\.html/?$", headers: { Location: "/$1" }, status: 308 },
  { src: "^/(.*)/$", headers: { Location: "/$1" }, status: 308 },
  { src: "^(?:/(.*))\\.rsc$", headers: { "Content-Type": "text/x-component" }, continue: true },
  {
    src: "^/_next/static(?:/(.*))$",
    headers: { "Cache-Control": "public, max-age=31536000, immutable" },
    continue: true,
  },
  { handle: "error" },
  { status: 404, src: "^(?!/api).*$", dest: "/404" },
];
export const SOURCE_PATHS = [
  "apps/site",
  "packages/invoices",
  "patches",
  "package.json",
  "pnpm-lock.yaml",
  "pnpm-workspace.yaml",
  ".npmrc",
  "tsconfig.base.json",
  "scripts/site-release.mjs",
  "scripts/site-release.test.mjs",
];
const SOURCE_RECORD = "source-provenance.json";
const BUILD_RECORD = "build-provenance.json";
const BUILD_SCRIPT = "node scripts/site-release.mjs build";
const hash = (value) => createHash("sha256").update(value).digest("hex");
const json = (file) => JSON.parse(readFileSync(file, "utf8"));
const writeJson = (file, value) => writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`);
const capture = (command, args, cwd, env = process.env) =>
  execFileSync(command, args, { cwd, env, encoding: "utf8", maxBuffer: 32 * 1024 * 1024 });
const run = (command, args, cwd) => execFileSync(command, args, { cwd, stdio: "inherit" });
const digest = (files) => hash(JSON.stringify(files));
function fail(message) {
  throw new Error(message);
}
function inside(parent, child) {
  const relative = path.relative(parent, child);
  return (
    relative === "" ||
    (!relative.startsWith(`..${path.sep}`) && relative !== ".." && !path.isAbsolute(relative))
  );
}

function regularFiles(root, skip = () => false, relative = "") {
  const files = [];
  for (const name of readdirSync(path.join(root, relative)).sort()) {
    const entry = relative ? `${relative}/${name}` : name;
    if (skip(entry)) continue;
    const stat = lstatSync(path.join(root, entry));
    if (stat.isDirectory()) files.push(...regularFiles(root, skip, entry));
    else if (stat.isFile()) files.push(entry);
    else fail(`Symlink or special file is not allowed: ${entry}`);
  }
  return files;
}
function inventory(root, names = regularFiles(root)) {
  return names.sort().map((name) => ({
    path: name,
    sha256: hash(readFileSync(path.join(root, name))),
    executable: Boolean(lstatSync(path.join(root, name)).mode & 0o111),
  }));
}
function assertHosting(manifest) {
  if (
    manifest.project_id !== PROJECT_ID ||
    manifest.d1 !== null ||
    manifest.r2 !== null ||
    manifest.static != null
  )
    fail("Unexpected Sites identity, binding or static configuration; review before release.");
}
function newOutput(root, output) {
  if (!output || !path.isAbsolute(output))
    fail("Use an absolute output path outside the source checkout.");
  const resolved = path.resolve(output);
  let ancestor = resolved;
  while (!existsSync(ancestor)) ancestor = path.dirname(ancestor);
  const canonical = path.resolve(realpathSync(ancestor), path.relative(ancestor, resolved));
  if (inside(realpathSync(root), canonical))
    fail("Output must be outside the source checkout, including through symlinks.");
  if (existsSync(resolved) && (!lstatSync(resolved).isDirectory() || readdirSync(resolved).length))
    fail("Output must be a new or empty directory; existing output is never overwritten.");
  mkdirSync(canonical, { recursive: true });
  return canonical;
}
function safeSourcePath(name) {
  if (
    name
      .split("/")
      .some(
        (part) =>
          part === ".." ||
          part === ".git" ||
          part === "node_modules" ||
          part === ".pnpm-store" ||
          part === "dist" ||
          part === ".wrangler" ||
          part.startsWith(".env") ||
          part.startsWith(".dev.vars") ||
          /\.(pem|key|p12)$/i.test(part),
      )
  )
    fail(`Unexpected generated or sensitive source path: ${name}`);
}
export function prepare(root, output, target = "sites") {
  if (!TARGETS.includes(target)) fail("Unknown release target.");
  root = realpathSync(root);
  if (capture("git", ["rev-parse", "--show-toplevel"], root).trim() !== root)
    fail("Prepare from the GitHub repository root.");
  if (capture("git", ["status", "--porcelain", "--untracked-files=all"], root).trim())
    fail("Commit or preserve changes first: preparation requires a clean GitHub checkout.");
  const sha = capture("git", ["rev-parse", "HEAD"], root).trim();
  const entries = capture("git", ["ls-tree", "-rz", sha, "--", ...SOURCE_PATHS], root)
    .split("\0")
    .filter(Boolean)
    .map((line) => {
      const match = /^(100644|100755) blob ([a-f0-9]+)\t(.+)$/.exec(line);
      if (!match) fail("Source closure contains a symlink, submodule or unsupported Git entry.");
      safeSourcePath(match[3]);
      return { mode: match[1], oid: match[2], path: match[3] };
    });
  for (const required of SOURCE_PATHS)
    if (!entries.some((entry) => entry.path === required || entry.path.startsWith(`${required}/`)))
      fail(`Missing source input: ${required}`);
  const hosting = JSON.parse(
    capture("git", ["show", `${sha}:apps/site/.openai/hosting.json`], root),
  );
  assertHosting(hosting);
  const out = newOutput(root, output);
  const original = [];
  for (const entry of entries) {
    const contents = execFileSync("git", ["cat-file", "blob", entry.oid], {
      cwd: root,
      maxBuffer: 32 * 1024 * 1024,
    });
    mkdirSync(path.dirname(path.join(out, entry.path)), { recursive: true });
    writeFileSync(path.join(out, entry.path), contents);
    chmodSync(path.join(out, entry.path), entry.mode === "100755" ? 0o755 : 0o644);
    original.push({
      path: entry.path,
      sha256: hash(contents),
      executable: entry.mode === "100755",
    });
  }
  const pkg = json(path.join(out, "package.json"));
  // Only scripts change. Dependency inputs stay byte-identical to the frozen lockfile.
  pkg.scripts = { build: BUILD_SCRIPT, "release:verify": "node scripts/site-release.mjs verify" };
  writeJson(path.join(out, "package.json"), pkg);
  writeJson(path.join(out, ".release-target.json"), { target });
  mkdirSync(path.join(out, ".openai"));
  cpSync(path.join(out, "apps/site/.openai/hosting.json"), path.join(out, ".openai/hosting.json"));
  writeFileSync(
    path.join(out, ".gitignore"),
    "node_modules/\n.pnpm-store/\ndist/\n.wrangler/\n.vite/\n.vinext/\n.next/\n.sites-runtime/\n*.tsbuildinfo\nnext-env.d.ts\nbuild-provenance.json\n",
  );
  const files = inventory(out);
  const provenance = {
    schema_version: 1,
    target,
    project_id: PROJECT_ID,
    github: { repository: "castlemilk/tammy", commit_sha: sha },
    sites: { commit_sha: null, saved_version: null, publication: "not-performed" },
    transformations: [
      "Root package scripts replaced with Sites build/verify wrappers; dependency declarations retained.",
      "Root hosting manifest copied from apps/site; export-local .gitignore added.",
    ],
    original_files: original.sort((a, b) => a.path.localeCompare(b.path)),
    files,
    source_sha256: digest(files),
  };
  writeJson(path.join(out, SOURCE_RECORD), provenance);
  verifySource(out);
  return provenance;
}
function ignoredGenerated(name) {
  const generatedRoots = [
    ".git",
    "node_modules",
    ".pnpm-store",
    "dist",
    ".sites-runtime",
    "apps/site/node_modules",
    "packages/invoices/node_modules",
    "apps/site/dist",
    "apps/site/.wrangler",
    "apps/site/.vite",
    "apps/site/.vinext",
    "apps/site/.next",
  ];
  return (
    [
      SOURCE_RECORD,
      BUILD_RECORD,
      "apps/site/tsconfig.tsbuildinfo",
      "apps/site/next-env.d.ts",
    ].includes(name) || generatedRoots.some((root) => name === root || name.startsWith(`${root}/`))
  );
}
export function verifySource(root) {
  const source = json(path.join(root, SOURCE_RECORD));
  if (
    source.schema_version !== 1 ||
    source.project_id !== PROJECT_ID ||
    !/^[a-f0-9]{40}$/.test(source.github?.commit_sha)
  )
    fail("Invalid source provenance.");
  if (
    !TARGETS.includes(source.target) ||
    json(path.join(root, ".release-target.json")).target !== source.target
  )
    fail("Release target differs from the prepared source.");
  const files = inventory(root, regularFiles(root, ignoredGenerated));
  if (digest(files) !== source.source_sha256 || digest(source.files) !== source.source_sha256)
    fail("Source differs from the prepared GitHub snapshot; prepare and build again.");
  assertHosting(json(path.join(root, ".openai/hosting.json")));
  if (
    hash(readFileSync(path.join(root, ".openai/hosting.json"))) !==
    hash(readFileSync(path.join(root, "apps/site/.openai/hosting.json")))
  )
    fail("Root and app hosting manifests differ.");
  const site = json(path.join(root, "apps/site/package.json"));
  const invoice = json(path.join(root, "packages/invoices/package.json"));
  if (
    site.dependencies?.["@tammy/invoices"] !== "workspace:*" ||
    invoice.name !== "@tammy/invoices"
  )
    fail("Invoice workspace dependency is missing or changed; review the source closure.");
  for (const pkgPath of [
    "package.json",
    "apps/site/package.json",
    "packages/invoices/package.json",
  ]) {
    const pkg = json(path.join(root, pkgPath));
    for (const [name, spec] of Object.entries({
      ...pkg.dependencies,
      ...pkg.devDependencies,
      ...pkg.optionalDependencies,
    })) {
      if (
        /^(workspace:|link:|file:)/.test(spec) &&
        !(
          pkgPath === "apps/site/package.json" &&
          name === "@tammy/invoices" &&
          spec === "workspace:*"
        )
      )
        fail(`Unreviewed local dependency ${pkgPath}: ${name}`);
    }
  }
  return source;
}
function rejectSecrets(names) {
  for (const name of names) {
    if (
      name
        .split("/")
        .some(
          (part) =>
            part.startsWith(".env") ||
            part.startsWith(".dev.vars") ||
            /\.(pem|key|p12)$/i.test(part),
        )
    ) {
      fail(`Unexpected sensitive artifact path: ${name}`);
    }
  }
}
export function assertCloudflareConfig(config) {
  const allowed = new Set([
    "$schema",
    "configPath",
    "userConfigPath",
    "definedEnvironments",
    "topLevelName",
    "dev",
    "name",
    "compatibility_date",
    "compatibility_flags",
    "workers_dev",
    "preview_urls",
    "assets",
    "observability",
    "main",
    "no_bundle",
    "jsx_factory",
    "jsx_fragment",
    "rules",
    "build",
    "legacy_env",
    "python_modules",
    "cloudchamber",
    "vars",
    "define",
    "durable_objects",
    "kv_namespaces",
    "queues",
    "r2_buckets",
    "d1_databases",
    "vectorize",
    "ai_search_namespaces",
    "ai_search",
    "hyperdrive",
    "workflows",
    "secrets_store_secrets",
    "artifacts",
    "services",
    "analytics_engine_datasets",
    "unsafe_hello_world",
    "flagship",
    "ratelimits",
    "worker_loaders",
    "migrations",
    "triggers",
    "dispatch_namespaces",
    "logfwdr",
    "send_email",
    "mtls_certificates",
    "pipelines",
    "vpc_services",
    "vpc_networks",
  ]);
  for (const key of Object.keys(config))
    if (!allowed.has(key)) fail(`Unreviewed Cloudflare configuration key: ${key}`);
  if (config.observability && !isDeepStrictEqual(config.observability, { enabled: false }))
    fail("Only disabled Cloudflare observability is reviewed for this target.");
  if (config.python_modules && !isDeepStrictEqual(config.python_modules, { exclude: ["**/*.pyc"] }))
    fail("Unexpected Python module configuration in the JavaScript Worker.");
  if (
    config.rules &&
    !isDeepStrictEqual(config.rules, [{ type: "ESModule", globs: ["**/*.js", "**/*.mjs"] }])
  )
    fail("Unreviewed Cloudflare module rules; modules must stay inside the verified output.");
  if (config.topLevelName && config.topLevelName !== CLOUDFLARE_WORKER_NAME)
    fail("Unexpected top-level Worker name.");
  if (
    config.name !== CLOUDFLARE_WORKER_NAME ||
    config.main !== "index.js" ||
    config.no_bundle !== true ||
    config.compatibility_date !== "2026-05-15" ||
    !isDeepStrictEqual(config.compatibility_flags, ["nodejs_compat"]) ||
    config.workers_dev !== true ||
    config.preview_urls !== false ||
    config.assets?.directory !== "../client" ||
    config.assets?.binding !== "ASSETS" ||
    config.assets?.not_found_handling !== "none" ||
    config.assets?.run_worker_first ||
    config.route != null ||
    config.routes != null ||
    config.account_id != null ||
    config.build?.command ||
    config.env ||
    config.site ||
    config.unsafe ||
    (config.assets?.html_handling && config.assets.html_handling !== "auto-trailing-slash")
  )
    fail("Unexpected direct Cloudflare Worker configuration; review target and routing.");
  for (const key of [
    "vars",
    "define",
    "d1_databases",
    "r2_buckets",
    "kv_namespaces",
    "services",
    "queues",
    "durable_objects",
    "workflows",
    "dispatch_namespaces",
    "containers",
    "ai",
    "ai_search",
    "ai_search_namespaces",
    "vectorize",
    "hyperdrive",
    "secrets_store_secrets",
    "analytics_engine_datasets",
    "send_email",
    "mtls_certificates",
    "pipelines",
    "vpc_services",
    "vpc_networks",
    "worker_loaders",
    "ratelimits",
    "logfwdr",
    "triggers",
    "migrations",
    "artifacts",
    "unsafe_hello_world",
    "flagship",
    "cloudchamber",
    "definedEnvironments",
  ]) {
    const value = config[key];
    const nonempty = (v) =>
      v &&
      (Array.isArray(v)
        ? v.length > 0
        : typeof v === "object"
          ? Object.values(v).some(nonempty)
          : true);
    if (nonempty(value)) fail(`Unreviewed Cloudflare binding or runtime setting: ${key}`);
  }
}
function workerFiles(root, target = "sites") {
  const dist = path.join(root, "dist");
  const files = inventory(dist);
  rejectSecrets(files.map((entry) => entry.path));
  if (target === "vercel") {
    // Static export: every route is prerendered HTML served from dist/client.
    for (const required of ["client/index.html", "client/404.html"])
      if (!files.some((entry) => entry.path === required))
        fail(`Missing static export output: ${required}`);
  } else
    for (const required of [
      "server/index.js",
      "server/wrangler.json",
      ...(target === "sites" ? [".openai/hosting.json"] : []),
    ])
      if (!files.some((entry) => entry.path === required))
        fail(`Missing Worker output: ${required}`);
  if (!files.some((entry) => entry.path.startsWith("client/") && entry.path.endsWith(".wasm")))
    fail("Missing invoice PDF WASM asset in client output.");
  if (target === "sites") assertHosting(json(path.join(dist, ".openai/hosting.json")));
  else if (target === "vercel") {
    if (existsSync(path.join(dist, ".openai")))
      fail("Vercel output must not contain Sites deployment metadata.");
    if (existsSync(path.join(dist, "server/wrangler.json")))
      fail("Vercel static output must not contain a Worker configuration.");
  } else {
    if (existsSync(path.join(dist, ".openai")))
      fail("Direct Cloudflare output must not contain Sites deployment metadata.");
    assertCloudflareConfig(json(path.join(dist, "server/wrangler.json")));
  }
  return files;
}
export function recordBuild(root, runtime) {
  const source = verifySource(root);
  const files = workerFiles(root, source.target);
  const record = {
    schema_version: 1,
    target: source.target,
    project_id: PROJECT_ID,
    github: source.github,
    source_sha256: source.source_sha256,
    runtime,
    files,
    build_sha256: digest(files),
  };
  writeJson(path.join(root, BUILD_RECORD), record);
  return record;
}
export function build(root) {
  const source = verifySource(root);
  const pkg = json(path.join(root, "package.json"));
  const pnpm = capture("pnpm", ["--version"], root, cloudflareEnvironment()).trim();
  if (process.versions.node !== pkg.engines.node || `pnpm@${pnpm}` !== pkg.packageManager)
    fail(`Use Node ${pkg.engines.node} and ${pkg.packageManager} for release builds.`);
  // These directories are generated exclusively by this release pipeline.
  for (const relative of ["dist", "apps/site/dist"]) {
    const filename = path.join(root, relative);
    if (existsSync(filename) && !lstatSync(filename).isDirectory())
      fail(`Refusing to replace a non-directory build output: ${relative}`);
    rmSync(filename, { recursive: true, force: true });
  }
  rmSync(path.join(root, BUILD_RECORD), { force: true });
  execFileSync("pnpm", ["--dir", "apps/site", "build"], {
    cwd: root,
    stdio: "inherit",
    env: { ...cloudflareEnvironment(), TAMMY_DEPLOY_TARGET: source.target },
  });
  regularFiles(path.join(root, "apps/site/dist"));
  cpSync(path.join(root, "apps/site/dist"), path.join(root, "dist"), {
    recursive: true,
    dereference: false,
  });
  return recordBuild(root, {
    node: process.versions.node,
    pnpm,
    platform: process.platform,
    arch: process.arch,
  });
}
export function verify(root) {
  const source = verifySource(root);
  const built = json(path.join(root, BUILD_RECORD));
  const files = workerFiles(root, source.target);
  if (
    built.target !== source.target ||
    built.source_sha256 !== source.source_sha256 ||
    built.github?.commit_sha !== source.github.commit_sha ||
    built.build_sha256 !== digest(files) ||
    digest(built.files) !== built.build_sha256
  )
    fail("Build provenance does not match the source and Worker output; rebuild before release.");
  return { source, built };
}
export function bundle(root, output) {
  const { source, built } = verify(root);
  const out = newOutput(root, output);
  const sourceArchive = path.join(out, "source.tar.gz");
  // Explicit file list excludes caches, credentials, dependencies and build output.
  run(
    "tar",
    ["-czf", sourceArchive, "--", ...source.files.map((entry) => entry.path), SOURCE_RECORD],
    root,
  );
  cpSync(path.join(root, "dist"), path.join(out, "dist"), { recursive: true });
  for (const name of [SOURCE_RECORD, BUILD_RECORD])
    cpSync(path.join(root, name), path.join(out, name));
  const manifest = {
    schema_version: 1,
    target: source.target,
    kind:
      source.target === "cloudflare"
        ? "cloudflare-worker-build"
        : source.target === "vercel"
          ? "vercel-static-build"
          : "github-source-and-worker-build",
    ...(source.target === "vercel"
      ? { vercel: { project: VERCEL_PROJECT_NAME, static_directory: "dist/client", url: null } }
      : {}),
    ...(source.target === "cloudflare"
      ? {
          cloudflare: {
            worker_name: CLOUDFLARE_WORKER_NAME,
            config: "dist/server/wrangler.json",
            version_id: null,
            url: null,
          },
        }
      : {}),
    project_id: PROJECT_ID,
    github: source.github,
    sites: source.sites,
    source_sha256: source.source_sha256,
    build_sha256: built.build_sha256,
    source_archive: { path: "source.tar.gz", sha256: hash(readFileSync(sourceArchive)) },
    publication: "not-performed",
  };
  writeJson(path.join(out, "release-manifest.json"), manifest);
  return manifest;
}
export function packageWorker(root, output, pluginRoot) {
  const { source, built } = verify(root);
  if (source.target !== "sites")
    fail("Sites packaging requires a Sites target; use cloudflare-dry-run for direct Workers.");
  if (!pluginRoot || !path.isAbsolute(pluginRoot))
    fail("Provide --sites-plugin with the absolute installed Sites plugin directory.");
  const helper = path.join(pluginRoot, "scripts/package-site.mjs");
  if (!existsSync(helper))
    fail("Installed Sites package-site.mjs helper is required; no custom publisher is used.");
  const out = newOutput(root, output);
  const archive = path.join(out, "worker.tar.gz");
  run(process.execPath, [helper, root, archive], root);
  const entries = capture("tar", ["-tzf", archive], root).trim().split("\n");
  if (entries.some((name) => !name.startsWith("dist/") || name.split("/").includes("..")))
    fail("Unexpected archive layout.");
  const stage = mkdtempSync(path.join(tmpdir(), "tammy-worker-verify-"));
  try {
    run("tar", ["-xzf", archive, "-C", stage], root);
    const packaged = workerFiles(stage);
    // The supported helper normalizes the hosting manifest; all Worker bytes must match.
    const content = (items) => items.filter((entry) => entry.path !== ".openai/hosting.json");
    if (digest(content(packaged)) !== digest(content(built.files)))
      fail("Packaged Worker differs from the verified build.");
    const expectedHosting = json(path.join(root, ".openai/hosting.json"));
    const builtHosting = json(path.join(root, "dist/.openai/hosting.json"));
    const attribution = builtHosting.artifact_metadata ?? expectedHosting.artifact_metadata;
    if (attribution != null) expectedHosting.artifact_metadata = attribution;
    if (!isDeepStrictEqual(json(path.join(stage, "dist/.openai/hosting.json")), expectedHosting))
      fail("Packaged Sites manifest differs from the source configuration.");
  } finally {
    rmSync(stage, { recursive: true, force: true });
  }
  verify(root);
  const manifest = {
    schema_version: 1,
    kind: "sites-worker-archive-not-saved",
    project_id: PROJECT_ID,
    github: source.github,
    sites: { commit_sha: null, saved_version: null, publication: "not-performed" },
    source_sha256: source.source_sha256,
    build_sha256: built.build_sha256,
    archive: {
      path: "worker.tar.gz",
      sha256: hash(readFileSync(archive)),
      bytes: lstatSync(archive).size,
    },
    packager: { name: "Sites scripts/package-site.mjs", sha256: hash(readFileSync(helper)) },
  };
  writeJson(path.join(out, "worker-manifest.json"), manifest);
  return manifest;
}
export function cloudflareEnvironment(credentials = false) {
  const env = Object.fromEntries(
    Object.entries(process.env).filter(
      ([key]) => !/^(CLOUDFLARE_|CF_|WRANGLER_|TAMMY_DEPLOY_TARGET$)/.test(key),
    ),
  );
  Object.assign(env, {
    WRANGLER_SEND_METRICS: "false",
    WRANGLER_WRITE_LOGS: "false",
    CLOUDFLARE_LOAD_DEV_VARS_FROM_DOT_ENV: "false",
    CLOUDFLARE_INCLUDE_PROCESS_ENV: "false",
    CI: "true",
  });
  if (credentials) {
    const { CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID } = process.env;
    if (
      !/^[a-f0-9]{32}$/.test(CLOUDFLARE_ACCOUNT_ID ?? "") ||
      (process.env.GITHUB_ACTIONS === "true" && !CLOUDFLARE_API_TOKEN)
    )
      fail(
        "Direct deployment requires a verified CLOUDFLARE_ACCOUNT_ID and existing authorized authentication; GitHub Actions also requires CLOUDFLARE_API_TOKEN. No login or credential creation is performed.",
      );
    env.CLOUDFLARE_ACCOUNT_ID = CLOUDFLARE_ACCOUNT_ID;
    if (CLOUDFLARE_API_TOKEN) env.CLOUDFLARE_API_TOKEN = CLOUDFLARE_API_TOKEN;
  }
  return env;
}
function directBuild(root) {
  const verified = verify(root);
  if (verified.source.target !== "cloudflare")
    fail("Direct deployment requires a cloudflare target, not a Sites artifact.");
  const pkg = json(path.join(root, "package.json"));
  if (
    process.versions.node !== pkg.engines.node ||
    capture("pnpm", ["--version"], root, cloudflareEnvironment()).trim() !==
      pkg.packageManager.replace("pnpm@", "")
  )
    fail("Use the pinned Node and pnpm versions for Cloudflare release operations.");
  const wranglerPackage = json(path.join(root, "apps/site/node_modules/wrangler/package.json"));
  if (wranglerPackage.version !== "4.92.0")
    fail("Use pinned Wrangler 4.92.0; do not download a newer CLI.");
  return verified;
}
function wrangler(root, args, env) {
  execFileSync(
    process.execPath,
    [
      path.join(root, "apps/site/node_modules/wrangler/bin/wrangler.js"),
      "deploy",
      "--config",
      path.join(root, "dist/server/wrangler.json"),
      ...args,
    ],
    { cwd: root, stdio: "inherit", env },
  );
}
export function cloudflareDryRun(root, output) {
  const { source, built } = directBuild(root);
  const out = newOutput(root, output);
  wrangler(root, ["--dry-run", "--outdir", path.join(out, "worker")], cloudflareEnvironment());
  verify(root);
  const files = inventory(out);
  rejectSecrets(files.map((entry) => entry.path));
  const record = {
    schema_version: 1,
    target: "cloudflare",
    worker_name: CLOUDFLARE_WORKER_NAME,
    github: source.github,
    source_sha256: source.source_sha256,
    build_sha256: built.build_sha256,
    wrangler: "4.92.0",
    files,
    publication: "not-performed",
  };
  writeJson(path.join(out, "dry-run-record.json"), record);
  return record;
}
export function cloudflareDeploymentResult(entries) {
  const result = entries.filter((entry) => entry.type === "deploy").at(-1);
  if (
    result?.version !== 1 ||
    result.worker_name !== CLOUDFLARE_WORKER_NAME ||
    result.worker_name_overridden ||
    !/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(
      result.version_id ?? "",
    ) ||
    result.targets?.length !== 1 ||
    typeof result.targets[0] !== "string"
  )
    fail("Wrangler deployment outcome is uncertain; inspect the existing Worker before retrying.");
  const url = new URL(result.targets[0]);
  if (
    url.protocol !== "https:" ||
    url.username ||
    url.password ||
    url.port ||
    url.search ||
    url.hash ||
    url.pathname !== "/" ||
    !new RegExp(`^${CLOUDFLARE_WORKER_NAME}\\.[a-z0-9-]+\\.workers\\.dev$`).test(url.hostname)
  )
    fail("Unexpected deployed route; reconcile the Worker outcome before retrying.");
  return { version_id: result.version_id, url: url.href };
}
const CLOUDFLARE_API = "https://api.cloudflare.com/client/v4";
const CLOUDFLARE_API_TIMEOUT_MS = 15_000;
const CLOUDFLARE_RETRY_DELAY_MS = 1_000;
// Used only when Cloudflare's Date header is absent or unparseable.
const CLOCK_FALLBACK_MS = 5_000;
const UUID_PATTERN = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Only numeric Cloudflare error codes leave this module; messages and bodies never do.
const cloudflareCodes = (body) =>
  Array.isArray(body?.errors)
    ? body.errors.map((error) => error?.code).filter(Number.isInteger)
    : [];

async function cloudflareAttempt(fetcher, token, accountId, route) {
  let response;
  try {
    response = await fetcher(`${CLOUDFLARE_API}/accounts/${accountId}/${route}`, {
      headers: { Authorization: `Bearer ${token}`, Accept: "application/json" },
      redirect: "error",
      signal: AbortSignal.timeout(CLOUDFLARE_API_TIMEOUT_MS),
    });
  } catch {
    return { ok: false, network: true, status: null, body: null, codes: [], date: null };
  }
  const body = await response.json().catch(() => null);
  const date = Date.parse(response.headers?.get("date") ?? "");
  return {
    ok: response.ok && body?.success === true,
    network: false,
    status: response.status,
    body,
    codes: cloudflareCodes(body),
    date: Number.isFinite(date) ? date : null,
  };
}

const retryable = (result) =>
  result.network || result.status === 429 || (result.status >= 500 && result.status <= 599);

// One retry, only for network/abort errors, 429 and 5xx. Auth and other 4xx errors are final.
async function cloudflareGet(fetcher, token, accountId, route, retryDelayMs) {
  const first = await cloudflareAttempt(fetcher, token, accountId, route);
  if (!retryable(first)) return first;
  await delay(retryDelayMs);
  return cloudflareAttempt(fetcher, token, accountId, route);
}

// Returns the Date of the first successful preflight read, or null when it had none.
async function preflightCloudflare(token, accountId, fetcher, retryDelayMs) {
  const checks = [
    ["account-subdomain", "workers/subdomain"],
    ["script-subdomain", `workers/scripts/${CLOUDFLARE_WORKER_NAME}/subdomain`],
  ];
  const failures = [];
  let serverTime;
  for (const [label, route] of checks) {
    const result = await cloudflareGet(fetcher, token, accountId, route, retryDelayMs);
    if (result.ok) {
      if (serverTime === undefined) serverTime = result.date;
    } else
      failures.push(
        `${label} HTTP ${result.status ?? "unavailable"}, Cloudflare codes ${result.codes.join(",") || "none"}`,
      );
  }
  if (failures.length)
    fail(
      `CLOUDFLARE_TOKEN_PREFLIGHT_FAILED: ${failures.join("; ")}. Wrangler was not run; see the direct Worker runbook for the required CI token access.`,
    );
  return serverTime ?? null;
}

// Pure classification of the deployments list read after a non-zero Wrangler exit.
// Cloudflare timestamps have one-second precision, so the start time is floored to a second.
export function classifyReadback(deployments, commitSha, startedAt) {
  const mismatch = { readback: "mismatch" };
  if (!deployments.length) return mismatch;
  const times = deployments.map((deployment) => Date.parse(deployment?.created_on));
  if (times.some((time) => !Number.isFinite(time))) return mismatch;
  const newest = times.indexOf(Math.max(...times));
  const deployment = deployments[newest];
  if (deployment.annotations?.["workers/message"] !== `GitHub ${commitSha}`) return mismatch;
  if (times[newest] < Math.floor(startedAt / 1000) * 1000) return mismatch;
  const versions = deployment.versions;
  if (!Array.isArray(versions) || versions.length !== 1) return mismatch;
  const [{ version_id: versionId, percentage }] = versions;
  if (percentage !== 100) return mismatch;
  if (typeof versionId !== "string" || !UUID_PATTERN.test(versionId)) return mismatch;
  return { readback: "matched", version_id: versionId };
}

async function readbackDeployment({
  fetcher,
  token,
  accountId,
  commitSha,
  startedAt,
  retryDelayMs,
}) {
  const result = await cloudflareGet(
    fetcher,
    token,
    accountId,
    `workers/scripts/${CLOUDFLARE_WORKER_NAME}/deployments`,
    retryDelayMs,
  );
  const deployments = result.ok ? result.body?.result?.deployments : undefined;
  if (!Array.isArray(deployments)) return { readback: "unavailable" };
  return classifyReadback(deployments, commitSha, startedAt);
}

async function reconcileFailedDeployment(record, options) {
  let outcome = { readback: "unavailable" };
  if (options.token && options.startedAt !== null) {
    try {
      outcome = await readbackDeployment(options);
    } catch {
      outcome = { readback: "unavailable" };
    }
  }
  record.readback = outcome.readback;
  if (outcome.readback === "matched") {
    record.status = "published-unconfirmed";
    record.version_id = outcome.version_id;
  } else record.status = "outcome-uncertain";
}

export async function deployCloudflare(root, output, options = {}) {
  const { fetcher = fetch, retryDelayMs = CLOUDFLARE_RETRY_DELAY_MS } = options;
  const { source, built } = directBuild(root);
  const env = cloudflareEnvironment(true);
  const out = newOutput(root, output);
  const record = {
    schema_version: 1,
    target: "cloudflare",
    worker_name: CLOUDFLARE_WORKER_NAME,
    github: source.github,
    source_sha256: source.source_sha256,
    build_sha256: built.build_sha256,
    account_id: env.CLOUDFLARE_ACCOUNT_ID,
    wrangler: "4.92.0",
    status: "started",
    live_verification: "pending",
    version_id: null,
    url: null,
  };
  const recordPath = path.join(out, "deployment-record.json");
  const outputPath = path.join(out, "wrangler-output.jsonl");
  writeJson(recordPath, record);
  // Local runs may rely on a cached Wrangler OAuth session without a token. Both the
  // preflight and the readback need the CI token, so both are skipped without it.
  const token = process.env.CLOUDFLARE_API_TOKEN;
  const accountId = env.CLOUDFLARE_ACCOUNT_ID;
  let startedAt = null;
  if (token) {
    try {
      const serverTime = await preflightCloudflare(token, accountId, fetcher, retryDelayMs);
      startedAt = serverTime ?? Date.now() - CLOCK_FALLBACK_MS;
    } catch (error) {
      Object.assign(record, { status: "preflight-failed", publication: "not-performed" });
      writeJson(recordPath, record);
      throw error;
    }
  }
  try {
    wrangler(root, ["--message", `GitHub ${source.github.commit_sha}`], {
      ...env,
      WRANGLER_OUTPUT_FILE_PATH: outputPath,
    });
  } catch (error) {
    await reconcileFailedDeployment(record, {
      token,
      accountId,
      fetcher,
      retryDelayMs,
      commitSha: source.github.commit_sha,
      startedAt,
    });
    writeJson(recordPath, record);
    throw error;
  }
  try {
    const result = cloudflareDeploymentResult(
      readFileSync(outputPath, "utf8")
        .trim()
        .split("\n")
        .map((line) => JSON.parse(line)),
    );
    Object.assign(record, result, { status: "published" });
    verify(root);
    writeJson(recordPath, record);
    return record;
  } catch (error) {
    record.status = "outcome-uncertain";
    writeJson(recordPath, record);
    throw error;
  }
}
function staticFiles(built) {
  // Hidden build metadata such as client/.vite stays out of the public deployment.
  return built.files
    .filter((entry) => entry.path.startsWith("client/"))
    .map((entry) => ({ ...entry, path: entry.path.slice("client/".length) }))
    .filter((entry) => !entry.path.split("/").some((part) => part.startsWith(".")));
}
function vercelConfig(files) {
  const overrides = {};
  for (const entry of files)
    if (entry.path.endsWith(".html")) overrides[entry.path] = { path: entry.path.slice(0, -5) };
  return { version: 3, routes: VERCEL_ROUTES, overrides };
}
function assertVercelOutput(out, expected) {
  const staticRoot = path.join(out, ".vercel/output/static");
  const deployed = inventory(staticRoot);
  if (digest(deployed) !== digest(expected))
    fail("Vercel static output differs from the verified build; regenerate it.");
  const configFile = path.join(out, ".vercel/output/config.json");
  if (!isDeepStrictEqual(json(configFile), vercelConfig(expected)))
    fail("Vercel routing configuration differs from the reviewed routes; regenerate it.");
  const extra = regularFiles(path.join(out, ".vercel/output")).filter(
    (name) => name !== "config.json" && !name.startsWith("static/"),
  );
  if (extra.length) fail(`Unexpected Vercel output files: ${extra.join(", ")}`);
  return { static_sha256: digest(deployed), config_sha256: hash(readFileSync(configFile)) };
}
export function vercelOutput(root, output) {
  const { source, built } = verify(root);
  if (source.target !== "vercel")
    fail("Vercel output requires a vercel target; prepare the source with --target vercel.");
  const files = staticFiles(built);
  const out = newOutput(root, output);
  const staticRoot = path.join(out, ".vercel/output/static");
  for (const entry of files) {
    const target = path.join(staticRoot, entry.path);
    mkdirSync(path.dirname(target), { recursive: true });
    cpSync(path.join(root, "dist/client", entry.path), target);
  }
  writeJson(path.join(out, ".vercel/output/config.json"), vercelConfig(files));
  const hashes = assertVercelOutput(out, files);
  const manifest = {
    schema_version: 1,
    target: "vercel",
    kind: "vercel-prebuilt-output",
    project: VERCEL_PROJECT_NAME,
    github: source.github,
    source_sha256: source.source_sha256,
    build_sha256: built.build_sha256,
    ...hashes,
    files: files.length,
    publication: "not-performed",
  };
  writeJson(path.join(out, "vercel-output-manifest.json"), manifest);
  return manifest;
}
// The Vercel CLI gets only what it needs to run npx, find its signed-in session or
// reach the network. Everything else, including other tokens and the variables it
// uses to detect AI agents, stays out of its environment.
const VERCEL_CLI_ENV = new Set([
  "PATH",
  "HOME",
  "USER",
  "LOGNAME",
  "USERPROFILE",
  "APPDATA",
  "LOCALAPPDATA",
  "SYSTEMROOT",
  "COMSPEC",
  "TMPDIR",
  "TMP",
  "TEMP",
  "LANG",
  "LC_ALL",
  "LC_CTYPE",
  "XDG_CACHE_HOME",
  "XDG_CONFIG_HOME",
  "XDG_DATA_HOME",
  "NODE_EXTRA_CA_CERTS",
  "HTTPS_PROXY",
  "HTTP_PROXY",
  "NO_PROXY",
  "https_proxy",
  "http_proxy",
  "no_proxy",
  "npm_config_cache",
  "npm_config_registry",
]);
export function vercelEnvironment() {
  const env = Object.fromEntries(
    Object.entries(process.env).filter(([key]) => VERCEL_CLI_ENV.has(key)),
  );
  const { VERCEL_TOKEN, VERCEL_ORG_ID, VERCEL_PROJECT_ID } = process.env;
  if (
    !/^team_[A-Za-z0-9]+$/.test(VERCEL_ORG_ID ?? "") ||
    !/^prj_[A-Za-z0-9]+$/.test(VERCEL_PROJECT_ID ?? "") ||
    (process.env.GITHUB_ACTIONS === "true" && !VERCEL_TOKEN)
  )
    fail(
      "Vercel deployment requires VERCEL_ORG_ID and VERCEL_PROJECT_ID, and existing authorized authentication; GitHub Actions also requires VERCEL_TOKEN. No login or credential creation is performed.",
    );
  Object.assign(env, {
    VERCEL_ORG_ID,
    VERCEL_PROJECT_ID,
    VERCEL_TELEMETRY_DISABLED: "1",
    CI: "true",
  });
  if (VERCEL_TOKEN) env.VERCEL_TOKEN = VERCEL_TOKEN;
  return env;
}
// `deploy --format=json` prints the deployment object; when the CLI decides it is
// running under an agent it wraps it as { status: "ok", deployment }. Accept either,
// and only a production deployment that is READY.
export function vercelDeploymentResult(stdout) {
  const text = String(stdout ?? "");
  const start = text.indexOf("{");
  const end = text.lastIndexOf("}");
  if (start < 0 || end < start) return null;
  let parsed;
  try {
    parsed = JSON.parse(text.slice(start, end + 1));
  } catch {
    return null;
  }
  if (parsed?.status !== undefined && parsed.status !== "ok") return null;
  const deployment = parsed?.deployment ?? parsed;
  if (
    !/^dpl_[A-Za-z0-9]+$/.test(deployment?.id ?? "") ||
    !/^https:\/\/[a-z0-9.-]+\.vercel\.app$/.test(deployment?.url ?? "") ||
    deployment.readyState !== "READY" ||
    deployment.target !== "production"
  )
    return null;
  return {
    deployment_id: deployment.id,
    url: deployment.url,
    ready_state: deployment.readyState,
    deployment_target: deployment.target,
  };
}
function outputTail(value, token) {
  const text = String(value ?? "").slice(-4000);
  return token ? text.split(token).join("[redacted]") : text;
}
export function deployVercel(root, output, runCli = capture) {
  const { source, built } = verify(root);
  if (source.target !== "vercel")
    fail("Vercel deployment requires a vercel target, not a Sites or Worker artifact.");
  if (
    !output ||
    !path.isAbsolute(output) ||
    !existsSync(path.join(output, "vercel-output-manifest.json"))
  )
    fail("Pass --output with the absolute directory written by vercel-output.");
  const out = realpathSync(output);
  if (inside(realpathSync(root), out)) fail("Vercel output must be outside the source checkout.");
  // The CLI reads local configuration such as vercel.json from its working directory.
  const unexpected = [
    ...readdirSync(out).filter(
      (name) =>
        ![".vercel", "vercel-output-manifest.json", "deployment-record.json"].includes(name),
    ),
    ...readdirSync(path.join(out, ".vercel"))
      .filter((name) => name !== "output")
      .map((name) => `.vercel/${name}`),
  ];
  if (unexpected.length)
    fail(`Unexpected files beside the Vercel output: ${unexpected.join(", ")}`);
  const manifest = json(path.join(out, "vercel-output-manifest.json"));
  const hashes = assertVercelOutput(out, staticFiles(built));
  if (
    manifest.github?.commit_sha !== source.github.commit_sha ||
    manifest.build_sha256 !== built.build_sha256 ||
    manifest.static_sha256 !== hashes.static_sha256 ||
    manifest.config_sha256 !== hashes.config_sha256
  )
    fail("Vercel output manifest does not match this verified build.");
  const recordPath = path.join(out, "deployment-record.json");
  if (existsSync(recordPath))
    fail("A deployment record already exists; inspect it before retrying.");
  const env = vercelEnvironment();
  const record = {
    schema_version: 1,
    target: "vercel",
    project: VERCEL_PROJECT_NAME,
    vercel: { org_id: env.VERCEL_ORG_ID, project_id: env.VERCEL_PROJECT_ID },
    cli: `vercel@${VERCEL_CLI_VERSION}`,
    github: source.github,
    source_sha256: source.source_sha256,
    build_sha256: built.build_sha256,
    ...hashes,
    status: "deploying",
    url: null,
    live_verification: "pending",
  };
  writeJson(recordPath, record);
  let stdout = "";
  try {
    stdout = runCli(
      "npx",
      [
        "--yes",
        `vercel@${VERCEL_CLI_VERSION}`,
        "deploy",
        "--prebuilt",
        "--prod",
        "--yes",
        "--format=json",
      ],
      out,
      env,
    );
    const result = vercelDeploymentResult(stdout);
    if (!result)
      throw new Error(
        "Vercel CLI did not report a READY production deployment; inspect the project.",
      );
    Object.assign(record, {
      status: "published",
      deployed_at: new Date().toISOString(),
      ...result,
    });
    writeJson(recordPath, record);
    return record;
  } catch (error) {
    record.status = "outcome-uncertain";
    record.cli_output = outputTail(stdout || error?.stdout, env.VERCEL_TOKEN);
    writeJson(recordPath, record);
    throw error;
  }
}
export const VERCEL_PRODUCTION_ORIGIN = "https://tammy.tax";
// Confirms the production domain serves this exact output: a READY deployment that
// did not take the domain alias would otherwise pass route and canonical checks.
export async function verifyVercelLive(
  output,
  { fetcher = fetch, attempts = 6, delayMs = 5000, origin = VERCEL_PRODUCTION_ORIGIN } = {},
) {
  if (
    !output ||
    !path.isAbsolute(output) ||
    !existsSync(path.join(output, "deployment-record.json"))
  )
    fail("Pass --output with the directory that holds a Vercel deployment record.");
  const out = realpathSync(output);
  const record = json(path.join(out, "deployment-record.json"));
  if (record.target !== "vercel" || record.status !== "published")
    fail("Only a published Vercel deployment record can be checked against the live domain.");
  const resultPath = path.join(out, "live-verification.json");
  if (existsSync(resultPath)) fail("A live verification already exists; keep it as evidence.");
  const staticRoot = path.join(out, ".vercel/output/static");
  const files = regularFiles(staticRoot);
  const asset = files.find((name) => name.startsWith("_next/static/") && name.endsWith(".js"));
  const probes = [
    ["/", "index.html"],
    ["/tools", "tools.html"],
    ["/sitemap.xml", "sitemap.xml"],
    ...(asset ? [[`/${asset}`, asset]] : []),
  ].filter(([, file]) => files.includes(file));
  if (!probes.some(([route]) => route === "/"))
    fail("The Vercel output has no index.html to compare.");
  let checks = [];
  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    checks = await Promise.all(
      probes.map(async ([route, file]) => {
        const expected = hash(readFileSync(path.join(staticRoot, file)));
        try {
          const response = await fetcher(origin + route, {
            redirect: "manual",
            headers: { "cache-control": "no-cache" },
          });
          const sha256 = hash(Buffer.from(await response.arrayBuffer()));
          return { route, status: response.status, sha256, expected_sha256: expected };
        } catch (error) {
          return {
            route,
            status: null,
            error: String(error?.message ?? error),
            expected_sha256: expected,
          };
        }
      }),
    );
    if (checks.every((check) => check.status === 200 && check.sha256 === check.expected_sha256))
      break;
    if (attempt < attempts) await new Promise((resolve) => setTimeout(resolve, delayMs));
  }
  const ok = checks.every(
    (check) => check.status === 200 && check.sha256 === check.expected_sha256,
  );
  const result = {
    schema_version: 1,
    kind: "vercel-live-verification",
    origin,
    github: record.github,
    deployment_id: record.deployment_id,
    static_sha256: record.static_sha256,
    checks,
    ok,
  };
  writeJson(resultPath, result);
  if (!ok)
    fail(
      `${origin} does not serve this exact build; inspect the project's domains and deployments.`,
    );
  return result;
}
async function main() {
  const [command, ...args] = process.argv.slice(2);
  const options = {};
  while (args.length) {
    const key = args.shift();
    if (!["--output", "--sites-plugin", "--target"].includes(key) || !args[0] || options[key])
      fail(
        "Usage: site-release.mjs prepare|build|verify|bundle|package|cloudflare-dry-run|cloudflare-deploy|vercel-output|vercel-deploy|vercel-verify-live [--target sites|cloudflare|vercel] [--output ABS_DIR] [--sites-plugin ABS_PLUGIN_ROOT]",
      );
    options[key] = args.shift();
  }
  const root = process.cwd();
  let result;
  if (options["--target"] && command !== "prepare")
    fail("Choose the target at preparation; build/deployment must use that recorded target.");
  if (command === "prepare") result = prepare(root, options["--output"], options["--target"]);
  else if (command === "build") result = build(root);
  else if (command === "verify") {
    const { source, built } = verify(root);
    result = {
      github: source.github,
      target: source.target,
      source_sha256: source.source_sha256,
      build_sha256: built.build_sha256,
      publication: "not-performed",
    };
  } else if (command === "bundle") result = bundle(root, options["--output"]);
  else if (command === "package")
    result = packageWorker(root, options["--output"], options["--sites-plugin"]);
  else if (command === "cloudflare-dry-run") result = cloudflareDryRun(root, options["--output"]);
  else if (command === "cloudflare-deploy")
    result = await deployCloudflare(root, options["--output"]);
  else if (command === "vercel-output") result = vercelOutput(root, options["--output"]);
  else if (command === "vercel-deploy") result = deployVercel(root, options["--output"]);
  else if (command === "vercel-verify-live") result = await verifyVercelLive(options["--output"]);
  else
    fail(
      "Expected prepare, build, verify, bundle, package, cloudflare-dry-run, cloudflare-deploy, vercel-output, vercel-deploy or vercel-verify-live.",
    );
  console.log(
    JSON.stringify(
      command === "prepare"
        ? {
            github: result.github,
            source_sha256: result.source_sha256,
            output: options["--output"],
          }
        : result,
      null,
      2,
    ),
  );
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    await main();
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
