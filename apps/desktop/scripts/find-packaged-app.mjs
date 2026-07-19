import { createHash } from "node:crypto";
import { lstat, open, readdir, readFile } from "node:fs/promises";
import path from "node:path";

const HASH_PATTERN = /^[0-9a-f]{64}$/;
const MAX_ASAR_ENTRIES = 200_000;
const MAX_ASAR_HEADER_BYTES = 16 * 1024 * 1024;
const MAX_ASAR_PATH_DEPTH = 128;
const MAX_ASAR_PATH_LENGTH = 4096;

const TARGETS = Object.freeze({
  "darwin/arm64": Object.freeze({
    app: "out/Tammy-darwin-arm64/Tammy.app/Contents/MacOS/Tammy",
    asar: "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/app.asar",
    build: "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/build",
    core: "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/core",
    executable: "tammy-core",
    resourceBase: "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources",
    target: "darwin-arm64",
  }),
  "win32/x64": Object.freeze({
    app: "out/Tammy-win32-x64/Tammy.exe",
    asar: "out/Tammy-win32-x64/resources/app.asar",
    build: "out/Tammy-win32-x64/resources/build",
    core: "out/Tammy-win32-x64/resources/core",
    executable: "tammy-core.exe",
    resourceBase: "out/Tammy-win32-x64/resources",
    target: "win32-x64",
  }),
});

function selectTarget(platform, arch) {
  const selected = TARGETS[`${platform}/${arch}`];
  if (!selected) throw new Error("UNSUPPORTED_PACKAGE_TARGET");
  return selected;
}

function assertAbsoluteDirectoryPath(value) {
  if (
    typeof value !== "string" ||
    !path.isAbsolute(value) ||
    path.normalize(value) !== value ||
    value.split(path.sep).includes("..")
  ) {
    throw new Error("INVALID_DESKTOP_ROOT");
  }
}

function assertContained(parent, candidate, code) {
  const relative = path.relative(parent, candidate);
  if (
    relative === "" ||
    relative === ".." ||
    relative.startsWith(`..${path.sep}`) ||
    path.isAbsolute(relative)
  ) {
    throw new Error(code);
  }
}

export function resolvePackagedLayout({ desktopRoot, platform, arch }) {
  assertAbsoluteDirectoryPath(desktopRoot);
  const selected = selectTarget(platform, arch);
  const sourceCoreRoot = path.join(desktopRoot, "resources", "core");
  const sourceBuildRoot = path.join(desktopRoot, "resources", "build");
  const packagedCoreRoot = path.join(desktopRoot, selected.core);
  const packagedBuildRoot = path.join(desktopRoot, selected.build);
  const sourceCore = path.join(sourceCoreRoot, selected.target, selected.executable);
  const packagedCore = path.join(packagedCoreRoot, selected.target, selected.executable);
  const sourceManifest = path.join(sourceBuildRoot, "build-manifest.json");
  const packagedManifest = path.join(packagedBuildRoot, "build-manifest.json");
  const appExecutable = path.join(desktopRoot, selected.app);
  const appAsar = path.join(desktopRoot, selected.asar);
  for (const candidate of [
    appAsar,
    sourceCoreRoot,
    sourceBuildRoot,
    packagedCoreRoot,
    packagedBuildRoot,
    sourceCore,
    packagedCore,
    sourceManifest,
    packagedManifest,
    appExecutable,
  ]) {
    assertContained(desktopRoot, candidate, "PACKAGE_PATH_TRAVERSAL");
  }
  return {
    appAsar,
    appExecutable,
    packagedBuildRoot,
    packagedCore,
    packagedCoreRoot,
    packagedManifest,
    sourceBuildRoot,
    sourceCore,
    sourceCoreRoot,
    sourceManifest,
    target: selected.target,
  };
}

function isPlainRecord(value) {
  return (
    value !== null &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    Object.getPrototypeOf(value) === Object.prototype
  );
}

function assertAllowedKeys(value, allowed) {
  if (Object.keys(value).some((key) => !allowed.has(key))) {
    throw new Error("PACKAGE_ASAR_INVALID");
  }
}

async function readExactly(file, length, position) {
  const buffer = Buffer.alloc(length);
  let offset = 0;
  while (offset < length) {
    const { bytesRead } = await file.read(buffer, offset, length - offset, position + offset);
    if (bytesRead === 0) throw new Error("PACKAGE_ASAR_INVALID");
    offset += bytesRead;
  }
  return buffer;
}

function parseAsarHeader(buffer) {
  if (buffer.length < 12) throw new Error("PACKAGE_ASAR_INVALID");
  const payloadSize = buffer.readUInt32LE(0);
  if (payloadSize < 4 || payloadSize % 4 !== 0 || payloadSize + 4 !== buffer.length) {
    throw new Error("PACKAGE_ASAR_INVALID");
  }
  const jsonLength = buffer.readInt32LE(4);
  const alignedJsonLength = jsonLength >= 0 ? Math.ceil(jsonLength / 4) * 4 : -1;
  if (jsonLength < 0 || alignedJsonLength + 4 !== payloadSize || jsonLength > buffer.length - 8) {
    throw new Error("PACKAGE_ASAR_INVALID");
  }
  const jsonBytes = buffer.subarray(8, 8 + jsonLength);
  const padding = buffer.subarray(8 + jsonLength);
  if (padding.some((byte) => byte !== 0)) {
    throw new Error("PACKAGE_ASAR_INVALID");
  }
  const json = jsonBytes.toString("utf8");
  if (!Buffer.from(json, "utf8").equals(jsonBytes)) {
    throw new Error("PACKAGE_ASAR_INVALID");
  }
  try {
    return JSON.parse(json);
  } catch {
    throw new Error("PACKAGE_ASAR_INVALID");
  }
}

function enumerateAsarEntries(header, packedDataSize) {
  if (!isPlainRecord(header)) throw new Error("PACKAGE_ASAR_INVALID");
  assertAllowedKeys(header, new Set(["files"]));
  if (!isPlainRecord(header.files)) throw new Error("PACKAGE_ASAR_INVALID");
  const entries = [];

  function visit(files, parent, depth) {
    if (depth > MAX_ASAR_PATH_DEPTH || !isPlainRecord(files) || entries.length > MAX_ASAR_ENTRIES) {
      throw new Error("PACKAGE_ASAR_INVALID");
    }
    for (const [name, node] of Object.entries(files)) {
      if (
        name.length === 0 ||
        name === "." ||
        name === ".." ||
        name.includes("/") ||
        name.includes("\\") ||
        name.includes("\0") ||
        !isPlainRecord(node)
      ) {
        throw new Error("PACKAGE_ASAR_INVALID");
      }
      const entryPath = parent ? `${parent}/${name}` : name;
      if (entryPath.length > MAX_ASAR_PATH_LENGTH || entries.length >= MAX_ASAR_ENTRIES) {
        throw new Error("PACKAGE_ASAR_INVALID");
      }
      entries.push(entryPath);
      if (Object.hasOwn(node, "files")) {
        assertAllowedKeys(node, new Set(["files", "unpacked"]));
        if (
          !isPlainRecord(node.files) ||
          (Object.hasOwn(node, "unpacked") && typeof node.unpacked !== "boolean")
        ) {
          throw new Error("PACKAGE_ASAR_INVALID");
        }
        visit(node.files, entryPath, depth + 1);
      } else if (Object.hasOwn(node, "link")) {
        assertAllowedKeys(node, new Set(["link", "unpacked"]));
        if (
          typeof node.link !== "string" ||
          node.link.length === 0 ||
          node.link.length > MAX_ASAR_PATH_LENGTH ||
          node.link.includes("\0") ||
          (Object.hasOwn(node, "unpacked") && typeof node.unpacked !== "boolean")
        ) {
          throw new Error("PACKAGE_ASAR_INVALID");
        }
      } else {
        assertAllowedKeys(node, new Set(["executable", "integrity", "offset", "size", "unpacked"]));
        if (
          !Number.isSafeInteger(node.size) ||
          node.size < 0 ||
          node.size > 0xffffffff ||
          (Object.hasOwn(node, "executable") && typeof node.executable !== "boolean") ||
          (Object.hasOwn(node, "integrity") && !isPlainRecord(node.integrity)) ||
          (Object.hasOwn(node, "unpacked") && typeof node.unpacked !== "boolean")
        ) {
          throw new Error("PACKAGE_ASAR_INVALID");
        }
        if (node.unpacked === true) {
          if (Object.hasOwn(node, "offset")) {
            throw new Error("PACKAGE_ASAR_INVALID");
          }
        } else {
          if (typeof node.offset !== "string" || !/^(0|[1-9][0-9]*)$/.test(node.offset)) {
            throw new Error("PACKAGE_ASAR_INVALID");
          }
          const offset = BigInt(node.offset);
          if (offset + BigInt(node.size) > BigInt(packedDataSize)) {
            throw new Error("PACKAGE_ASAR_INVALID");
          }
        }
      }
    }
  }

  visit(header.files, "", 1);
  return entries;
}

async function readAsarEntries(archive) {
  const before = await lstat(archive).catch(() => null);
  if (!before?.isFile() || before.isSymbolicLink() || before.size < 20) {
    throw new Error("PACKAGE_ASAR_INVALID");
  }
  let file;
  try {
    file = await open(archive, "r");
    const opened = await file.stat();
    if (
      !opened.isFile() ||
      opened.dev !== before.dev ||
      opened.ino !== before.ino ||
      opened.size !== before.size
    ) {
      throw new Error("PACKAGE_ASAR_INVALID");
    }
    const sizePickle = await readExactly(file, 8, 0);
    if (sizePickle.readUInt32LE(0) !== 4) {
      throw new Error("PACKAGE_ASAR_INVALID");
    }
    const headerSize = sizePickle.readUInt32LE(4);
    if (headerSize < 12 || headerSize > MAX_ASAR_HEADER_BYTES || headerSize > opened.size - 8) {
      throw new Error("PACKAGE_ASAR_INVALID");
    }
    const header = parseAsarHeader(await readExactly(file, headerSize, 8));
    return enumerateAsarEntries(header, opened.size - 8 - headerSize);
  } catch (error) {
    if (error instanceof Error && error.message === "PACKAGE_ASAR_INVALID") {
      throw error;
    }
    throw new Error("PACKAGE_ASAR_INVALID");
  } finally {
    await file?.close().catch(() => {});
  }
}

async function assertCoreAbsentFromAsar(layout) {
  const executable = path.basename(layout.packagedCore);
  const selectedCoreSuffix = `core/${layout.target}/${executable}`;
  const entries = await readAsarEntries(layout.appAsar);
  if (
    entries.some(
      (entry) => entry === selectedCoreSuffix || entry.endsWith(`/${selectedCoreSuffix}`),
    )
  ) {
    throw new Error("PACKAGED_CORE_INSIDE_ASAR");
  }
}

async function enumerateTree(root, code) {
  const rootStats = await lstat(root).catch(() => null);
  if (!rootStats?.isDirectory() || rootStats.isSymbolicLink()) {
    throw new Error(code);
  }
  const entries = [];
  async function visit(directory, prefix) {
    const children = await readdir(directory, { withFileTypes: true });
    children.sort((left, right) => left.name.localeCompare(right.name));
    for (const child of children) {
      const relative = prefix ? `${prefix}/${child.name}` : child.name;
      const absolute = path.join(directory, child.name);
      const stats = await lstat(absolute);
      if (stats.isSymbolicLink()) throw new Error(code);
      if (stats.isDirectory()) {
        entries.push(`${relative}/`);
        await visit(absolute, relative);
      } else if (stats.isFile()) {
        entries.push(relative);
      } else {
        throw new Error(code);
      }
    }
  }
  await visit(root, "");
  return entries;
}

async function assertExactTree(root, expected, code) {
  const actual = await enumerateTree(root, code);
  if (
    actual.length !== expected.length ||
    actual.some((entry, index) => entry !== expected[index])
  ) {
    throw new Error(code);
  }
  const keep = await lstat(path.join(root, ".gitkeep")).catch(() => null);
  if (!keep?.isFile() || keep.isSymbolicLink() || keep.size !== 0) {
    throw new Error(code);
  }
}

async function assertRegularFile(file, code) {
  const stats = await lstat(file).catch(() => null);
  if (!stats?.isFile() || stats.isSymbolicLink()) throw new Error(code);
  return stats;
}

async function hashFile(file) {
  return createHash("sha256")
    .update(await readFile(file))
    .digest("hex");
}

export async function verifyPackagedLayout({ desktopRoot, platform, arch, sourceManifestPath }) {
  const layout = resolvePackagedLayout({ desktopRoot, platform, arch });
  if (
    typeof sourceManifestPath !== "string" ||
    path.resolve(sourceManifestPath) !== layout.sourceManifest
  ) {
    throw new Error("SOURCE_MANIFEST_PATH_INVALID");
  }
  const executable = path.basename(layout.sourceCore);
  const targetDirectory = path.basename(path.dirname(layout.sourceCore));
  const coreAllowlist = [".gitkeep", `${targetDirectory}/`, `${targetDirectory}/${executable}`];
  await assertExactTree(layout.sourceCoreRoot, coreAllowlist, "SOURCE_CORE_LAYOUT_INVALID");
  await assertExactTree(
    layout.sourceBuildRoot,
    [".gitkeep", "build-manifest.json"],
    "SOURCE_BUILD_LAYOUT_INVALID",
  );
  await assertExactTree(layout.packagedCoreRoot, coreAllowlist, "PACKAGED_CORE_LAYOUT_INVALID");
  await assertExactTree(
    layout.packagedBuildRoot,
    [".gitkeep", "build-manifest.json"],
    "PACKAGED_BUILD_LAYOUT_INVALID",
  );

  const appStats = await assertRegularFile(layout.appExecutable, "PACKAGE_APP_INVALID");
  await assertCoreAbsentFromAsar(layout);
  const sourceCoreStats = await assertRegularFile(layout.sourceCore, "SOURCE_CORE_LAYOUT_INVALID");
  const packagedCoreStats = await assertRegularFile(
    layout.packagedCore,
    "PACKAGED_CORE_LAYOUT_INVALID",
  );
  await assertRegularFile(layout.sourceManifest, "SOURCE_BUILD_LAYOUT_INVALID");
  await assertRegularFile(layout.packagedManifest, "PACKAGED_BUILD_LAYOUT_INVALID");
  if (platform === "darwin") {
    if ((appStats.mode & 0o111) === 0) throw new Error("PACKAGE_APP_NOT_EXECUTABLE");
    if ((sourceCoreStats.mode & 0o111) === 0) {
      throw new Error("SOURCE_CORE_NOT_EXECUTABLE");
    }
    if ((packagedCoreStats.mode & 0o111) === 0) {
      throw new Error("PACKAGED_CORE_NOT_EXECUTABLE");
    }
  }

  const sourceManifest = await readFile(layout.sourceManifest);
  const packagedManifest = await readFile(layout.packagedManifest);
  if (!sourceManifest.equals(packagedManifest)) {
    throw new Error("PACKAGED_MANIFEST_MISMATCH");
  }
  let manifest;
  try {
    manifest = JSON.parse(sourceManifest.toString("utf8"));
  } catch {
    throw new Error("SOURCE_MANIFEST_INVALID");
  }
  if (
    manifest?.schema !== "tammy-build-manifest-v1" ||
    typeof manifest.core_sha256 !== "string" ||
    !HASH_PATTERN.test(manifest.core_sha256)
  ) {
    throw new Error("SOURCE_MANIFEST_INVALID");
  }
  const sourceCoreHash = await hashFile(layout.sourceCore);
  if (sourceCoreHash !== manifest.core_sha256) {
    throw new Error("SOURCE_CORE_HASH_MISMATCH");
  }
  const packagedCoreHash = await hashFile(layout.packagedCore);
  if (packagedCoreHash !== manifest.core_sha256) {
    throw new Error("PACKAGED_CORE_HASH_MISMATCH");
  }
  return {
    appExecutable: layout.appExecutable,
    appSha256: await hashFile(layout.appExecutable),
    coreExecutable: layout.packagedCore,
    coreSha256: packagedCoreHash,
    manifest: layout.packagedManifest,
    manifestSha256: createHash("sha256").update(sourceManifest).digest("hex"),
    target: layout.target,
  };
}

function parseArguments(argv) {
  let sourceManifestPath;
  let verify = false;
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--verify") {
      verify = true;
    } else if (argument === "--source-manifest" && index + 1 < argv.length) {
      sourceManifestPath = path.resolve(argv[index + 1]);
      index += 1;
    } else {
      throw new Error("INVALID_PACKAGE_ARGUMENTS");
    }
  }
  if (!verify || !sourceManifestPath) throw new Error("INVALID_PACKAGE_ARGUMENTS");
  return sourceManifestPath;
}

async function main() {
  const desktopRoot = path.resolve(import.meta.dirname, "..");
  const sourceManifestPath = parseArguments(process.argv.slice(2));
  const result = await verifyPackagedLayout({
    desktopRoot,
    platform: process.platform,
    arch: process.arch,
    sourceManifestPath,
  });
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === import.meta.filename) {
  main().catch((error) => {
    const code = error instanceof Error ? error.message : "PACKAGE_VERIFICATION_FAILED";
    process.stderr.write(`${JSON.stringify({ error: code })}\n`);
    process.exitCode = 1;
  });
}
