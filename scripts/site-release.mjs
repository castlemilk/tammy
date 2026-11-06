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
const capture = (command, args, cwd) =>
  execFileSync(command, args, { cwd, encoding: "utf8", maxBuffer: 32 * 1024 * 1024 });
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
          /\.(pem|key|p12)$/i.test(part),
      )
  )
    fail(`Unexpected generated or sensitive source path: ${name}`);
}
export function prepare(root, output) {
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
  mkdirSync(path.join(out, ".openai"));
  cpSync(path.join(out, "apps/site/.openai/hosting.json"), path.join(out, ".openai/hosting.json"));
  writeFileSync(
    path.join(out, ".gitignore"),
    "node_modules/\n.pnpm-store/\ndist/\n.wrangler/\n.vite/\n.vinext/\n.next/\n.sites-runtime/\n*.tsbuildinfo\nnext-env.d.ts\nbuild-provenance.json\n",
  );
  const files = inventory(out);
  const provenance = {
    schema_version: 1,
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
function workerFiles(root) {
  const dist = path.join(root, "dist");
  const files = inventory(dist);
  for (const required of ["server/index.js", "server/wrangler.json", ".openai/hosting.json"])
    if (!files.some((entry) => entry.path === required)) fail(`Missing Worker output: ${required}`);
  if (!files.some((entry) => entry.path.startsWith("client/") && entry.path.endsWith(".wasm")))
    fail("Missing invoice PDF WASM asset in client output.");
  assertHosting(json(path.join(dist, ".openai/hosting.json")));
  return files;
}
export function recordBuild(root, runtime) {
  const source = verifySource(root);
  const files = workerFiles(root);
  const record = {
    schema_version: 1,
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
  verifySource(root);
  const pkg = json(path.join(root, "package.json"));
  const pnpm = capture("pnpm", ["--version"], root).trim();
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
  run("pnpm", ["--dir", "apps/site", "build"], root);
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
  const files = workerFiles(root);
  if (
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
    kind: "github-source-and-worker-build",
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
function main() {
  const [command, ...args] = process.argv.slice(2);
  const options = {};
  while (args.length) {
    const key = args.shift();
    if (!["--output", "--sites-plugin"].includes(key) || !args[0] || options[key])
      fail(
        "Usage: site-release.mjs prepare|build|verify|bundle|package [--output ABS_DIR] [--sites-plugin ABS_PLUGIN_ROOT]",
      );
    options[key] = args.shift();
  }
  const root = process.cwd();
  let result;
  if (command === "prepare") result = prepare(root, options["--output"]);
  else if (command === "build") result = build(root);
  else if (command === "verify") {
    const { source, built } = verify(root);
    result = {
      github: source.github,
      source_sha256: source.source_sha256,
      build_sha256: built.build_sha256,
      publication: "not-performed",
    };
  } else if (command === "bundle") result = bundle(root, options["--output"]);
  else if (command === "package")
    result = packageWorker(root, options["--output"], options["--sites-plugin"]);
  else fail("Expected prepare, build, verify, bundle or package.");
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
    main();
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
