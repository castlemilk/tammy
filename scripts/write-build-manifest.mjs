import { execFile as nodeExecFile } from "node:child_process";
import { createHash } from "node:crypto";
import { lstat, readdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";

const HASH_PATTERN = /^[0-9a-f]{64}$/;
const REVISION_PATTERN = /^[0-9a-f]{40}$/;
const VERSION_PATTERN = /^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$/;
const FORBIDDEN_FIELD_PATTERN = /credential|secret|token|password|environment|(^|_)env($|_)/i;

const REQUIRED_VERSION_KEYS = Object.freeze([
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
const REQUIRED_LOCKFILE_KEYS = Object.freeze(["pnpm-lock.yaml", "services/core/go.sum"]);
const CREATE_INPUT_KEYS = Object.freeze([
  "ciMode",
  "coreSha256",
  "lockfiles",
  "protobufTreeSha256",
  "sourceDirty",
  "sourceRevision",
  "target",
  "versions",
]);

const TARGETS = Object.freeze({
  "darwin/arm64": Object.freeze({
    binary: "apps/desktop/resources/core/darwin-arm64/tammy-core",
    target: "darwin-arm64",
  }),
  "win32/x64": Object.freeze({
    binary: "apps/desktop/resources/core/win32-x64/tammy-core.exe",
    target: "win32-x64",
  }),
});

function assertExactKeys(value, expected, code) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(code);
  }
  const keys = Object.keys(value).sort();
  if (keys.length !== expected.length || keys.some((key, index) => key !== expected[index])) {
    throw new Error(code);
  }
}

function assertNoForbiddenKeys(value) {
  if (!value || typeof value !== "object") return;
  for (const [key, child] of Object.entries(value)) {
    if (FORBIDDEN_FIELD_PATTERN.test(key)) {
      throw new Error("FORBIDDEN_MANIFEST_FIELD");
    }
    assertNoForbiddenKeys(child);
  }
}

function sortRecord(value) {
  return Object.fromEntries(
    Object.entries(value).sort(([left], [right]) => left.localeCompare(right)),
  );
}

function assertHash(value) {
  if (typeof value !== "string" || !HASH_PATTERN.test(value)) {
    throw new Error("INVALID_MANIFEST_HASH");
  }
}

function selectTarget(platform, arch) {
  const selected = TARGETS[`${platform}/${arch}`];
  if (!selected) throw new Error("UNSUPPORTED_MANIFEST_TARGET");
  return selected;
}

export function selectCiMode(environment) {
  return environment?.CI === "true";
}

export function createBuildManifest(input) {
  assertNoForbiddenKeys(input);
  assertExactKeys(input, CREATE_INPUT_KEYS, "MANIFEST_INPUT_INVALID");
  const {
    ciMode,
    coreSha256,
    lockfiles,
    protobufTreeSha256,
    sourceDirty,
    sourceRevision,
    target,
    versions,
  } = input;
  if (!["darwin-arm64", "win32-x64"].includes(target)) {
    throw new Error("UNSUPPORTED_MANIFEST_TARGET");
  }
  if (typeof sourceRevision !== "string" || !REVISION_PATTERN.test(sourceRevision)) {
    throw new Error("INVALID_SOURCE_REVISION");
  }
  if (typeof sourceDirty !== "boolean" || typeof ciMode !== "boolean") {
    throw new Error("MANIFEST_INPUT_INVALID");
  }
  if (ciMode && sourceDirty) throw new Error("DIRTY_SOURCE_IN_CI");
  assertHash(coreSha256);
  assertHash(protobufTreeSha256);
  assertExactKeys(versions, REQUIRED_VERSION_KEYS, "MANIFEST_PINS_INVALID");
  for (const version of Object.values(versions)) {
    if (typeof version !== "string" || !VERSION_PATTERN.test(version)) {
      throw new Error("MANIFEST_PINS_INVALID");
    }
  }
  assertExactKeys(lockfiles, REQUIRED_LOCKFILE_KEYS, "MANIFEST_LOCKFILES_INVALID");
  for (const lockHash of Object.values(lockfiles)) assertHash(lockHash);

  return {
    schema: "tammy-build-manifest-v1",
    source_revision: sourceRevision,
    source_dirty: sourceDirty,
    target,
    versions: sortRecord(versions),
    lockfiles: sortRecord(lockfiles),
    protobuf_tree_sha256: protobufTreeSha256,
    core_sha256: coreSha256,
    test_profile: "foundation-packaged-e2e",
    sbr_status: "SIMULATOR_NOT_IMPLEMENTED",
    signed: false,
  };
}

async function listFiles(root, code) {
  const stats = await lstat(root).catch(() => null);
  if (!stats?.isDirectory() || stats.isSymbolicLink()) throw new Error(code);
  const result = [];
  async function visit(directory, prefix) {
    const entries = await readdir(directory, { withFileTypes: true });
    entries.sort((left, right) => left.name.localeCompare(right.name));
    for (const entry of entries) {
      const absolute = path.join(directory, entry.name);
      const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
      const entryStats = await lstat(absolute);
      if (entryStats.isSymbolicLink()) throw new Error(code);
      if (entryStats.isDirectory()) {
        await visit(absolute, relative);
      } else if (entryStats.isFile()) {
        result.push({ absolute, relative });
      } else {
        throw new Error(code);
      }
    }
  }
  await visit(root, "");
  return result;
}

export async function hashProtoTree(protoRoot) {
  const files = await listFiles(protoRoot, "PROTOBUF_TREE_INVALID");
  if (files.length === 0) throw new Error("PROTOBUF_TREE_INVALID");
  const digest = createHash("sha256");
  for (const file of files) {
    digest.update(`${file.relative}\0`);
    digest.update(await readFile(file.absolute));
    digest.update("\0");
  }
  return digest.digest("hex");
}

async function hashFile(file, code) {
  const stats = await lstat(file).catch(() => null);
  if (!stats?.isFile() || stats.isSymbolicLink()) throw new Error(code);
  return createHash("sha256")
    .update(await readFile(file))
    .digest("hex");
}

async function cleanBuildRoot(buildRoot) {
  const rootStats = await lstat(buildRoot).catch(() => null);
  if (!rootStats?.isDirectory() || rootStats.isSymbolicLink()) {
    throw new Error("BUILD_STAGING_INVALID");
  }
  const keep = path.join(buildRoot, ".gitkeep");
  const keepStats = await lstat(keep).catch(() => null);
  if (!keepStats?.isFile() || keepStats.isSymbolicLink() || keepStats.size !== 0) {
    throw new Error("BUILD_STAGING_INVALID");
  }
  for (const entry of await readdir(buildRoot)) {
    if (entry !== ".gitkeep") {
      await rm(path.join(buildRoot, entry), { force: true, recursive: true });
    }
  }
}

export async function writeBuildManifest({ buildRoot, manifest, renameFile = rename }) {
  if (!path.isAbsolute(buildRoot)) throw new Error("BUILD_STAGING_INVALID");
  await cleanBuildRoot(buildRoot);
  const destination = path.join(buildRoot, "build-manifest.json");
  const temporary = path.join(buildRoot, ".build-manifest.json.tmp");
  try {
    await writeFile(temporary, `${JSON.stringify(manifest, null, 2)}\n`, {
      encoding: "utf8",
      flag: "wx",
      mode: 0o600,
    });
    await renameFile(temporary, destination);
  } catch {
    await rm(temporary, { force: true });
    await rm(destination, { force: true });
    throw new Error("MANIFEST_WRITE_FAILED");
  }
  const remaining = (await readdir(buildRoot)).sort();
  if (
    remaining.length !== 2 ||
    remaining[0] !== ".gitkeep" ||
    remaining[1] !== "build-manifest.json"
  ) {
    await rm(destination, { force: true });
    throw new Error("MANIFEST_WRITE_FAILED");
  }
  return destination;
}

function productionCommandRunner(command, args, options) {
  return new Promise((resolve, reject) => {
    nodeExecFile(command, args, options, (error, stdout) => {
      if (error) reject(error);
      else resolve(stdout);
    });
  });
}

function parseJson(bytes, code) {
  try {
    return JSON.parse(bytes.toString("utf8"));
  } catch {
    throw new Error(code);
  }
}

function extractGoVersion(goMod, moduleName) {
  const escaped = moduleName.replaceAll(".", "\\.").replaceAll("/", "\\/");
  const match = goMod.match(new RegExp(`(?:^|\\n)\\s*${escaped}\\s+v([^\\s]+)`));
  if (!match) throw new Error("MANIFEST_PINS_INVALID");
  return match[1];
}

function readPinnedVersions(rootPackage, desktopPackage, goMod) {
  const goMatch = goMod.match(/(?:^|\n)go ([^\s]+)/);
  if (!goMatch) throw new Error("MANIFEST_PINS_INVALID");
  const versions = {
    buf: rootPackage.devDependencies?.["@bufbuild/buf"],
    connect_es: desktopPackage.dependencies?.["@connectrpc/connect"],
    connect_go: extractGoVersion(goMod, "connectrpc.com/connect"),
    electron: desktopPackage.devDependencies?.electron,
    go: goMatch[1],
    node: rootPackage.engines?.node,
    playwright: desktopPackage.devDependencies?.["@playwright/test"],
    pnpm: rootPackage.engines?.pnpm,
    protobuf_es: rootPackage.devDependencies?.["@bufbuild/protoc-gen-es"],
    protobuf_go: extractGoVersion(goMod, "google.golang.org/protobuf"),
    react: desktopPackage.dependencies?.react,
    shadcn: desktopPackage.devDependencies?.shadcn,
    tailwindcss: desktopPackage.devDependencies?.tailwindcss,
    typescript: rootPackage.devDependencies?.typescript,
    vite: desktopPackage.devDependencies?.vite,
    vitest: desktopPackage.devDependencies?.vitest,
  };
  if (rootPackage.packageManager !== `pnpm@${versions.pnpm}`) {
    throw new Error("MANIFEST_PINS_INVALID");
  }
  return versions;
}

export async function collectBuildManifest({
  root,
  platform,
  arch,
  ciMode = false,
  commandRunner = productionCommandRunner,
}) {
  if (!path.isAbsolute(root) || path.normalize(root) !== root) {
    throw new Error("INVALID_PROJECT_ROOT");
  }
  const selected = selectTarget(platform, arch);
  const rootPackage = parseJson(
    await readFile(path.join(root, "package.json")),
    "MANIFEST_PINS_INVALID",
  );
  const desktopPackage = parseJson(
    await readFile(path.join(root, "apps/desktop/package.json")),
    "MANIFEST_PINS_INVALID",
  );
  const goMod = await readFile(path.join(root, "services/core/go.mod"), "utf8");
  const commandOptions = {
    cwd: root,
    encoding: "utf8",
    shell: false,
    windowsHide: true,
  };
  const sourceRevision = (await commandRunner("git", ["rev-parse", "HEAD"], commandOptions)).trim();
  const sourceStatus = await commandRunner(
    "git",
    ["status", "--porcelain", "--untracked-files=normal"],
    commandOptions,
  );
  const lockfiles = {};
  for (const relative of REQUIRED_LOCKFILE_KEYS) {
    lockfiles[relative] = await hashFile(
      path.join(root, ...relative.split("/")),
      "MANIFEST_LOCKFILES_INVALID",
    );
  }
  return createBuildManifest({
    ciMode,
    coreSha256: await hashFile(
      path.join(root, ...selected.binary.split("/")),
      "CORE_BINARY_INVALID",
    ),
    lockfiles,
    protobufTreeSha256: await hashProtoTree(path.join(root, "proto")),
    sourceDirty: sourceStatus.trim().length !== 0,
    sourceRevision,
    target: selected.target,
    versions: readPinnedVersions(rootPackage, desktopPackage, goMod),
  });
}

async function main() {
  const root = path.resolve(import.meta.dirname, "..");
  const unexpected = process.argv.slice(2).filter((argument) => argument !== "--ci");
  if (unexpected.length > 0) throw new Error("INVALID_MANIFEST_ARGUMENTS");
  const manifest = await collectBuildManifest({
    root,
    platform: process.platform,
    arch: process.arch,
    ciMode: process.argv.includes("--ci") || selectCiMode(process.env),
  });
  const destination = await writeBuildManifest({
    buildRoot: path.join(root, "apps/desktop/resources/build"),
    manifest,
  });
  process.stdout.write(
    `${JSON.stringify({
      manifest: destination,
      manifestSha256: await hashFile(destination, "MANIFEST_WRITE_FAILED"),
      target: manifest.target,
    })}\n`,
  );
}

if (process.argv[1] && path.resolve(process.argv[1]) === import.meta.filename) {
  main().catch((error) => {
    const code = error instanceof Error ? error.message : "BUILD_MANIFEST_FAILED";
    process.stderr.write(`${JSON.stringify({ error: code })}\n`);
    process.exitCode = 1;
  });
}
