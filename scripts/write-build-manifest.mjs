import { execFile as nodeExecFile } from "node:child_process";
import { createHash } from "node:crypto";
import { constants, lstat, open, readdir, realpath, rename, rm } from "node:fs/promises";
import path from "node:path";

import {
  BUILD_MANIFEST_LOCKFILE_KEYS,
  BUILD_MANIFEST_VERSION_KEYS,
} from "./build-manifest-schema.mjs";
import { hashStableFile, readStableFileBytes } from "./stable-file.mjs";

const HASH_PATTERN = /^[0-9a-f]{64}$/;
const PROVENANCE_TIMEOUT_MS = 10_000;
const REVISION_PATTERN = /^[0-9a-f]{40}$/;
const VERSION_PATTERN = /^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$/;
const FORBIDDEN_FIELD_PATTERN = /credential|secret|token|password|environment|(^|_)env($|_)/i;

const REQUIRED_VERSION_KEYS = BUILD_MANIFEST_VERSION_KEYS;
const REQUIRED_LOCKFILE_KEYS = BUILD_MANIFEST_LOCKFILE_KEYS;
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
    Object.entries(value).sort(([left], [right]) =>
      Buffer.compare(Buffer.from(left), Buffer.from(right)),
    ),
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
  const stats = await lstat(root, { bigint: true }).catch(() => null);
  if (!stats?.isDirectory() || stats.isSymbolicLink()) throw new Error(code);
  const files = [];
  const identities = [{ identity: treeIdentity(stats), relative: "" }];
  async function visit(directory, prefix) {
    const entries = await readdir(directory, { withFileTypes: true });
    for (const entry of entries) {
      const absolute = path.join(directory, entry.name);
      const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
      const entryStats = await lstat(absolute, { bigint: true });
      if (entryStats.isSymbolicLink()) throw new Error(code);
      if (entryStats.isDirectory()) {
        identities.push({
          identity: treeIdentity(entryStats),
          relative: `${relative}/`,
        });
        await visit(absolute, relative);
      } else if (entryStats.isFile()) {
        const identity = treeIdentity(entryStats);
        files.push({
          absolute,
          identity,
          relative,
        });
        identities.push({ identity, relative });
      } else {
        throw new Error(code);
      }
    }
  }
  await visit(root, "");
  files.sort((left, right) =>
    Buffer.compare(Buffer.from(left.relative), Buffer.from(right.relative)),
  );
  identities.sort((left, right) =>
    Buffer.compare(Buffer.from(left.relative), Buffer.from(right.relative)),
  );
  return { files, identities };
}

function treeIdentity(stats) {
  return {
    ctimeNs: stats.ctimeNs,
    dev: stats.dev,
    ino: stats.ino,
    mode: stats.mode,
    mtimeNs: stats.mtimeNs,
    nlink: stats.nlink,
    size: stats.size,
  };
}

function sameFileList(left, right) {
  return (
    left.identities.length === right.identities.length &&
    left.identities.every((file, index) => {
      const other = right.identities[index];
      return (
        file.relative === other.relative &&
        Object.keys(file.identity).every((key) => file.identity[key] === other.identity[key])
      );
    })
  );
}

export async function hashProtoTree(protoRoot, { afterFileHashed } = {}) {
  const initial = await listFiles(protoRoot, "PROTOBUF_TREE_INVALID");
  const { files } = initial;
  if (files.length === 0) throw new Error("PROTOBUF_TREE_INVALID");
  const digest = createHash("sha256");
  digest.update("tammy-protobuf-tree-v1\0");
  const count = Buffer.alloc(4);
  count.writeUInt32BE(files.length);
  digest.update(count);
  for (const file of files) {
    const relative = Buffer.from(file.relative);
    const relativeLength = Buffer.alloc(4);
    relativeLength.writeUInt32BE(relative.length);
    const contents = await readStableFileBytes(file.absolute, {
      code: "PROTOBUF_TREE_CHANGED",
      maxBytes: 64 * 1024 * 1024,
    });
    const contentLength = Buffer.alloc(8);
    contentLength.writeBigUInt64BE(BigInt(contents.length));
    digest.update(relativeLength);
    digest.update(relative);
    digest.update(contentLength);
    digest.update(createHash("sha256").update(contents).digest());
    await afterFileHashed?.(file.relative);
  }
  if (!sameFileList(initial, await listFiles(protoRoot, "PROTOBUF_TREE_INVALID"))) {
    throw new Error("PROTOBUF_TREE_CHANGED");
  }
  return digest.digest("hex");
}

function hashFile(file, code, maxBytes = 512 * 1024 * 1024) {
  return hashStableFile(file, { code, maxBytes });
}

function sameDirectoryIdentity(stats, expected) {
  return Boolean(
    stats?.isDirectory() &&
      !stats.isSymbolicLink() &&
      stats.dev === expected?.dev &&
      stats.ino === expected?.ino,
  );
}

async function assertBuildRootIdentity(buildRoot, expected) {
  const stats = await lstat(buildRoot, { bigint: true }).catch(() => null);
  if (!sameDirectoryIdentity(stats, expected)) {
    throw new Error("BUILD_STAGING_INVALID");
  }
  return stats;
}

async function removeOwnedBuildFile(buildRoot, expectedRoot, file, expectedFile) {
  if (!expectedRoot || !expectedFile) return;
  const currentRoot = await lstat(buildRoot, { bigint: true }).catch(() => null);
  if (!sameDirectoryIdentity(currentRoot, expectedRoot)) return;
  const stats = await lstat(file, { bigint: true }).catch(() => null);
  if (
    !stats?.isFile() ||
    stats.isSymbolicLink() ||
    stats.dev !== expectedFile.dev ||
    stats.ino !== expectedFile.ino
  ) {
    return;
  }
  const revalidatedRoot = await lstat(buildRoot, { bigint: true }).catch(() => null);
  if (!sameDirectoryIdentity(revalidatedRoot, expectedRoot)) return;
  await rm(file, { force: true });
}

async function cleanBuildRoot(buildRoot, expectedRoot) {
  const rootStats = await assertBuildRootIdentity(buildRoot, expectedRoot);
  const keep = path.join(buildRoot, ".gitkeep");
  const keepStats = await lstat(keep, { bigint: true }).catch(() => null);
  if (!keepStats?.isFile() || keepStats.isSymbolicLink() || keepStats.size !== 0n) {
    throw new Error("BUILD_STAGING_INVALID");
  }
  for (const entry of await readdir(buildRoot)) {
    if (entry !== ".gitkeep") {
      await assertBuildRootIdentity(buildRoot, expectedRoot);
      await rm(path.join(buildRoot, entry), { force: true, recursive: true });
    }
  }
  return {
    keep: treeIdentity(keepStats),
    root: treeIdentity(rootStats),
  };
}

async function validateBuildRoot(buildRoot, expected) {
  const [rootStats, keepStats] = await Promise.all([
    lstat(buildRoot, { bigint: true }).catch(() => null),
    lstat(path.join(buildRoot, ".gitkeep"), { bigint: true }).catch(() => null),
  ]);
  if (
    !rootStats?.isDirectory() ||
    rootStats.isSymbolicLink() ||
    !keepStats?.isFile() ||
    keepStats.isSymbolicLink() ||
    keepStats.size !== 0n ||
    rootStats.dev !== expected.root.dev ||
    rootStats.ino !== expected.root.ino ||
    rootStats.mode !== expected.root.mode ||
    !sameFileList(
      { identities: [{ identity: expected.keep, relative: ".gitkeep" }] },
      {
        identities: [
          {
            identity: treeIdentity(keepStats),
            relative: ".gitkeep",
          },
        ],
      },
    )
  ) {
    throw new Error("BUILD_STAGING_INVALID");
  }
}

export async function writeBuildManifest({
  beforeCleanup,
  buildRoot,
  manifest,
  renameFile = rename,
}) {
  if (!path.isAbsolute(buildRoot)) throw new Error("BUILD_STAGING_INVALID");
  const resourcesRoot = path.dirname(buildRoot);
  const lock = path.join(resourcesRoot, ".build-manifest.lock");
  const destination = path.join(buildRoot, "build-manifest.json");
  const temporary = path.join(buildRoot, ".build-manifest.json.tmp");
  let lockHandle;
  let lockIdentity;
  let resourcesIdentity;
  let stagingIdentity;
  let originalRootIdentity;
  let manifestFileIdentity;
  const bytes = Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`);
  try {
    const [rootStats, resourcesStats] = await Promise.all([
      lstat(buildRoot, { bigint: true }).catch(() => null),
      lstat(resourcesRoot, { bigint: true }).catch(() => null),
    ]);
    if (
      !rootStats?.isDirectory() ||
      rootStats.isSymbolicLink() ||
      !resourcesStats?.isDirectory() ||
      resourcesStats.isSymbolicLink()
    ) {
      throw new Error("BUILD_STAGING_INVALID");
    }
    originalRootIdentity = { dev: rootStats.dev, ino: rootStats.ino };
    resourcesIdentity = { dev: resourcesStats.dev, ino: resourcesStats.ino };
    await assertBuildRootIdentity(buildRoot, originalRootIdentity);
    try {
      lockHandle = await open(
        lock,
        constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | (constants.O_NOFOLLOW ?? 0),
        0o600,
      );
    } catch {
      throw new Error("BUILD_STAGING_LOCKED");
    }
    const openedLock = await lockHandle.stat({ bigint: true });
    lockIdentity = { dev: openedLock.dev, ino: openedLock.ino };
    await lockHandle.writeFile("tammy-build-manifest-v1\n");
    await lockHandle.sync();
    await beforeCleanup?.();
    stagingIdentity = await cleanBuildRoot(buildRoot, originalRootIdentity);
    const currentResources = await lstat(resourcesRoot, {
      bigint: true,
    });
    if (
      currentResources.dev !== resourcesStats.dev ||
      currentResources.ino !== resourcesStats.ino
    ) {
      throw new Error("BUILD_STAGING_INVALID");
    }
    await assertBuildRootIdentity(buildRoot, originalRootIdentity);
    const temporaryHandle = await open(
      temporary,
      constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | (constants.O_NOFOLLOW ?? 0),
      0o600,
    );
    try {
      const openedTemporary = await temporaryHandle.stat({ bigint: true });
      if (!openedTemporary.isFile()) throw new Error("MANIFEST_WRITE_FAILED");
      manifestFileIdentity = { dev: openedTemporary.dev, ino: openedTemporary.ino };
      await temporaryHandle.writeFile(bytes);
      await temporaryHandle.sync();
    } finally {
      await temporaryHandle.close();
    }
    await assertBuildRootIdentity(buildRoot, originalRootIdentity);
    await renameFile(temporary, destination);
    const published = await readStableFileBytes(destination, {
      code: "MANIFEST_WRITE_FAILED",
      maxBytes: 1024 * 1024,
    });
    if (!published.equals(bytes)) throw new Error("MANIFEST_WRITE_FAILED");
    await validateBuildRoot(buildRoot, stagingIdentity);
  } catch (error) {
    await removeOwnedBuildFile(buildRoot, originalRootIdentity, temporary, manifestFileIdentity);
    await removeOwnedBuildFile(buildRoot, originalRootIdentity, destination, manifestFileIdentity);
    if (
      error instanceof Error &&
      ["BUILD_STAGING_INVALID", "BUILD_STAGING_LOCKED"].includes(error.message)
    ) {
      throw error;
    }
    throw new Error("MANIFEST_WRITE_FAILED");
  } finally {
    await lockHandle?.close().catch(() => {});
    if (lockIdentity && resourcesIdentity) {
      const [currentResources, lexicalLock] = await Promise.all([
        lstat(resourcesRoot, { bigint: true }).catch(() => null),
        lstat(lock, { bigint: true }).catch(() => null),
      ]);
      if (
        sameDirectoryIdentity(currentResources, resourcesIdentity) &&
        lexicalLock?.isFile() &&
        !lexicalLock.isSymbolicLink() &&
        lexicalLock.dev === lockIdentity.dev &&
        lexicalLock.ino === lockIdentity.ino
      ) {
        const [revalidatedResources, revalidatedLock] = await Promise.all([
          lstat(resourcesRoot, { bigint: true }).catch(() => null),
          lstat(lock, { bigint: true }).catch(() => null),
        ]);
        if (
          sameDirectoryIdentity(revalidatedResources, resourcesIdentity) &&
          revalidatedLock?.isFile() &&
          !revalidatedLock.isSymbolicLink() &&
          revalidatedLock.dev === lockIdentity.dev &&
          revalidatedLock.ino === lockIdentity.ino
        ) {
          await rm(lock, { force: true });
        }
      }
    }
  }
  try {
    const remaining = (await readdir(buildRoot)).sort();
    if (
      remaining.length !== 2 ||
      remaining[0] !== ".gitkeep" ||
      remaining[1] !== "build-manifest.json"
    ) {
      throw new Error("MANIFEST_WRITE_FAILED");
    }
    await validateBuildRoot(buildRoot, stagingIdentity);
  } catch {
    await removeOwnedBuildFile(buildRoot, originalRootIdentity, destination, manifestFileIdentity);
    throw new Error("MANIFEST_WRITE_FAILED");
  }
  return destination;
}

export function sanitizeProvenanceEnvironment(sourceEnvironment) {
  const allowed = [
    "COMSPEC",
    "HOME",
    "PATH",
    "PATHEXT",
    "SYSTEMROOT",
    "SystemRoot",
    "TEMP",
    "TMP",
    "TMPDIR",
    "WINDIR",
  ];
  const sanitized = {};
  for (const key of allowed) {
    if (typeof sourceEnvironment?.[key] === "string") {
      sanitized[key] = sourceEnvironment[key];
    }
  }
  sanitized.LANG = "C";
  sanitized.LC_ALL = "C";
  return sanitized;
}

export function runBoundedCommand(
  command,
  args,
  options,
  { execFile = nodeExecFile, timeoutMs = PROVENANCE_TIMEOUT_MS } = {},
) {
  return new Promise((resolve, reject) => {
    const controller = new AbortController();
    let timedOut = false;
    const timeout = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, timeoutMs);
    try {
      execFile(
        command,
        args,
        {
          ...options,
          killSignal: "SIGKILL",
          signal: controller.signal,
          timeout: timeoutMs,
        },
        (error, stdout) => {
          clearTimeout(timeout);
          if (error) {
            reject(
              new Error(timedOut ? "PROVENANCE_COMMAND_TIMEOUT" : "PROVENANCE_COMMAND_FAILED"),
            );
          } else {
            resolve(stdout);
          }
        },
      );
    } catch {
      clearTimeout(timeout);
      reject(new Error("PROVENANCE_COMMAND_FAILED"));
    }
  });
}

function productionCommandRunner(command, args, options) {
  return runBoundedCommand(command, args, options);
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
  sourceEnvironment = process.env,
}) {
  if (!path.isAbsolute(root) || path.normalize(root) !== root) {
    throw new Error("INVALID_PROJECT_ROOT");
  }
  const selected = selectTarget(platform, arch);
  const physicalRoot = await realpath(root).catch(() => null);
  if (physicalRoot !== root) throw new Error("INVALID_PROJECT_ROOT");
  const commandOptions = {
    cwd: physicalRoot,
    encoding: "utf8",
    env: sanitizeProvenanceEnvironment(sourceEnvironment),
    shell: false,
    windowsHide: true,
  };
  const repositoryRoot = (
    await commandRunner("git", ["rev-parse", "--show-toplevel"], commandOptions)
  ).trim();
  if (path.resolve(repositoryRoot) !== physicalRoot) {
    throw new Error("GIT_REPOSITORY_MISMATCH");
  }
  const sourceRevision = (await commandRunner("git", ["rev-parse", "HEAD"], commandOptions)).trim();
  const sourceStatus = await commandRunner(
    "git",
    ["status", "--porcelain", "--untracked-files=normal"],
    commandOptions,
  );
  const rootPackage = parseJson(
    await readStableFileBytes(path.join(root, "package.json"), {
      code: "MANIFEST_PINS_INVALID",
      maxBytes: 1024 * 1024,
    }),
    "MANIFEST_PINS_INVALID",
  );
  const desktopPackage = parseJson(
    await readStableFileBytes(path.join(root, "apps/desktop/package.json"), {
      code: "MANIFEST_PINS_INVALID",
      maxBytes: 1024 * 1024,
    }),
    "MANIFEST_PINS_INVALID",
  );
  const goMod = (
    await readStableFileBytes(path.join(root, "services/core/go.mod"), {
      code: "MANIFEST_PINS_INVALID",
      maxBytes: 1024 * 1024,
    })
  ).toString("utf8");
  const lockfiles = {};
  for (const relative of REQUIRED_LOCKFILE_KEYS) {
    lockfiles[relative] = await hashFile(
      path.join(root, ...relative.split("/")),
      "MANIFEST_LOCKFILES_INVALID",
    );
  }
  const manifestInput = {
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
  };
  const finalRevision = (await commandRunner("git", ["rev-parse", "HEAD"], commandOptions)).trim();
  const finalStatus = await commandRunner(
    "git",
    ["status", "--porcelain", "--untracked-files=normal"],
    commandOptions,
  );
  if (sourceRevision !== finalRevision || sourceStatus !== finalStatus) {
    throw new Error("SOURCE_CHANGED_DURING_MANIFEST");
  }
  return createBuildManifest(manifestInput);
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
