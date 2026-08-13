import { createHash } from "node:crypto";
import { lstat, mkdir, mkdtemp, open, readdir, readFile, rename, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { Readable, Transform } from "node:stream";
import { pipeline } from "node:stream/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { gunzipSync } from "node:zlib";

const MAX_ARCHIVE_BYTES = 64 * 1024 * 1024;
const MAX_ENTRY_BYTES = 512 * 1024 * 1024;
const MAX_ENTRY_COUNT = 10_000;
const MAX_EXPANDED_BYTES = 512 * 1024 * 1024;
const TAR_BLOCK = 512;
const PINNED_GLOBAL_PAX = "52 comment=0b4d16090a71579c4c88220b7fb67bb7396f1434\n";

export const SQLCIPHER_RELEASE = Object.freeze({
  archiveName: "sqlcipher-v4.15.0.tar.gz",
  rootDirectory: "sqlcipher-4.15.0",
  sha256: "21f5dfb2558a2a87740bb060ba75aadfec2e6119e08a87c3546c54751395a28d",
  sourceTreeSha256: "ab920a951726ede8da090ad26874f094966de373e9ed566e6e6dc500541920be",
  url: "https://codeload.github.com/sqlcipher/sqlcipher/tar.gz/refs/tags/v4.15.0",
  version: "4.15.0",
});

function fail(code = "SQLCIPHER_ARCHIVE_INVALID") {
  throw new Error(code);
}

export function validateReleasePin(pin) {
  if (
    !pin ||
    typeof pin !== "object" ||
    Object.keys(pin).length !== Object.keys(SQLCIPHER_RELEASE).length ||
    Object.entries(SQLCIPHER_RELEASE).some(([key, value]) => pin[key] !== value)
  ) {
    fail("SQLCIPHER_RELEASE_PIN_INVALID");
  }
  return SQLCIPHER_RELEASE;
}

function normalizedArchivePath(candidate) {
  const hasControlCharacter =
    typeof candidate === "string" &&
    [...candidate].some((character) => {
      const codePoint = character.codePointAt(0);
      return codePoint <= 0x1f || codePoint === 0x7f;
    });
  if (
    typeof candidate !== "string" ||
    candidate.length === 0 ||
    candidate.includes("\\") ||
    hasControlCharacter ||
    candidate.startsWith("/") ||
    /^[A-Za-z]:/.test(candidate)
  ) {
    fail();
  }
  const directory = candidate.endsWith("/");
  const withoutSlash = directory ? candidate.slice(0, -1) : candidate;
  const normalized = path.posix.normalize(withoutSlash);
  if (
    normalized === "." ||
    normalized === ".." ||
    normalized.startsWith("../") ||
    normalized !== withoutSlash
  ) {
    fail();
  }
  return directory ? `${normalized}/` : normalized;
}

export function validateArchiveEntries(
  entries,
  { rootDirectory = SQLCIPHER_RELEASE.rootDirectory } = {},
) {
  if (!Array.isArray(entries) || entries.length === 0 || entries.length > MAX_ENTRY_COUNT) fail();
  const seen = new Set();
  const regularFiles = new Set();
  let expandedBytes = 0;
  for (const entry of entries) {
    if (!entry || typeof entry !== "object") fail();
    const normalized = normalizedArchivePath(entry.path);
    const allowedType = entry.type === "File" || entry.type === "Directory";
    if (
      !allowedType ||
      !Number.isSafeInteger(entry.size) ||
      entry.size < 0 ||
      entry.size > MAX_ENTRY_BYTES ||
      (entry.type === "Directory" && (entry.size !== 0 || !normalized.endsWith("/"))) ||
      (entry.type === "File" && normalized.endsWith("/")) ||
      !(normalized === `${rootDirectory}/` || normalized.startsWith(`${rootDirectory}/`)) ||
      seen.has(normalized)
    ) {
      fail();
    }
    const parts = normalized.replace(/\/$/, "").split("/");
    for (let index = 1; index < parts.length; index += 1) {
      if (regularFiles.has(parts.slice(0, index).join("/"))) fail();
    }
    if (entry.type === "File") regularFiles.add(normalized);
    expandedBytes += entry.size;
    if (!Number.isSafeInteger(expandedBytes) || expandedBytes > MAX_EXPANDED_BYTES) fail();
    seen.add(normalized);
  }
  for (const file of regularFiles) {
    if ([...seen].some((candidate) => candidate.startsWith(`${file}/`))) fail();
  }
  return entries;
}

function readTarString(block, start, length) {
  const bytes = block.subarray(start, start + length);
  const end = bytes.indexOf(0);
  return bytes.subarray(0, end === -1 ? bytes.length : end).toString("utf8");
}

function readTarNumber(block, start, length) {
  const field = block.subarray(start, start + length);
  if ((field[0] & 0x80) !== 0) fail();
  const value = field.toString("ascii").replace(/\0.*$/s, "").trim();
  if (value === "") return 0;
  if (!/^[0-7]+$/.test(value)) fail();
  const parsed = Number.parseInt(value, 8);
  if (!Number.isSafeInteger(parsed)) fail();
  return parsed;
}

function assertTarChecksum(header) {
  const expected = readTarNumber(header, 148, 8);
  let actual = 0;
  for (let index = 0; index < TAR_BLOCK; index += 1) {
    actual += index >= 148 && index < 156 ? 0x20 : header[index];
  }
  if (actual !== expected) fail();
}

function parsePaxPath(contents) {
  let offset = 0;
  let result;
  while (offset < contents.length) {
    const space = contents.indexOf(0x20, offset);
    if (space <= offset) fail();
    const lengthText = contents.subarray(offset, space).toString("ascii");
    if (!/^[1-9][0-9]*$/.test(lengthText)) fail();
    const length = Number.parseInt(lengthText, 10);
    const end = offset + length;
    if (!Number.isSafeInteger(length) || end > contents.length || contents[end - 1] !== 0x0a)
      fail();
    const record = contents.subarray(space + 1, end - 1).toString("utf8");
    const equals = record.indexOf("=");
    if (equals <= 0) fail();
    if (record.slice(0, equals) === "path") result = record.slice(equals + 1);
    offset = end;
  }
  return result;
}

export function parseTarArchive(
  archive,
  { pinnedGlobalPax = PINNED_GLOBAL_PAX, rootDirectory = SQLCIPHER_RELEASE.rootDirectory } = {},
) {
  if (!Buffer.isBuffer(archive) || archive.length === 0 || archive.length > MAX_EXPANDED_BYTES)
    fail();
  const parsed = [];
  let offset = 0;
  let overridePath;
  let zeroBlocks = 0;
  while (offset + TAR_BLOCK <= archive.length) {
    const header = archive.subarray(offset, offset + TAR_BLOCK);
    offset += TAR_BLOCK;
    if (header.every((byte) => byte === 0)) {
      zeroBlocks += 1;
      if (zeroBlocks >= 2) {
        if (!archive.subarray(offset).every((byte) => byte === 0)) fail();
        break;
      }
      continue;
    }
    zeroBlocks = 0;
    assertTarChecksum(header);
    const magic = header.subarray(257, 263).toString("ascii");
    if (magic !== "ustar\0" && magic !== "ustar ") fail();
    const size = readTarNumber(header, 124, 12);
    const mode = readTarNumber(header, 100, 8);
    const paddedSize = Math.ceil(size / TAR_BLOCK) * TAR_BLOCK;
    if (size > MAX_ENTRY_BYTES || offset + paddedSize > archive.length) fail();
    const contents = archive.subarray(offset, offset + size);
    offset += paddedSize;
    const typeFlag = String.fromCharCode(header[156] || 0x30);
    const prefix = readTarString(header, 345, 155);
    const name = readTarString(header, 0, 100);
    const headerPath = prefix ? `${prefix}/${name}` : name;
    if (typeFlag === "g") {
      if (contents.toString("utf8") !== pinnedGlobalPax) fail();
      continue;
    }
    if (typeFlag === "x") {
      overridePath = parsePaxPath(contents) ?? overridePath;
      continue;
    }
    if (typeFlag === "L") {
      overridePath = contents.toString("utf8").replace(/\0+$/, "");
      continue;
    }
    const entryPath = overridePath ?? headerPath;
    overridePath = undefined;
    parsed.push({
      contents,
      mode,
      path: entryPath,
      size,
      type: typeFlag === "5" ? "Directory" : typeFlag === "0" ? "File" : `Type:${typeFlag}`,
    });
    if (parsed.length > MAX_ENTRY_COUNT) fail();
  }
  if (overridePath !== undefined || zeroBlocks < 2) fail();
  validateArchiveEntries(
    parsed.map(({ path: entryPath, size, type }) => ({ path: entryPath, size, type })),
    { rootDirectory },
  );
  return parsed;
}

async function extractValidated(entries, destination, rootDirectory) {
  await mkdir(destination, { recursive: false, mode: 0o700 });
  for (const entry of entries) {
    const relative = entry.path.slice(`${rootDirectory}/`.length);
    const output = path.join(destination, rootDirectory, ...relative.split("/"));
    const resolved = path.resolve(output);
    const expectedRoot = path.join(destination, rootDirectory);
    if (resolved !== expectedRoot && !resolved.startsWith(`${expectedRoot}${path.sep}`)) fail();
    if (entry.type === "Directory") {
      await mkdir(resolved, { recursive: true, mode: 0o700 });
      continue;
    }
    await mkdir(path.dirname(resolved), { recursive: true, mode: 0o700 });
    const safeMode = (entry.mode & 0o111) === 0 ? 0o600 : 0o700;
    const handle = await open(resolved, "wx", safeMode);
    try {
      await handle.writeFile(entry.contents);
    } finally {
      await handle.close();
    }
  }
}

async function downloadPinnedArchive(destination, fetchImpl, url) {
  const response = await fetchImpl(url, {
    headers: { accept: "application/gzip" },
    redirect: "error",
    signal: AbortSignal.timeout(120_000),
  });
  if (!response.ok || !response.body) fail("SQLCIPHER_DOWNLOAD_FAILED");
  const declaredLength = Number(response.headers.get("content-length"));
  if (Number.isFinite(declaredLength) && declaredLength > MAX_ARCHIVE_BYTES) {
    fail("SQLCIPHER_DOWNLOAD_FAILED");
  }
  const handle = await open(destination, "wx", 0o600);
  try {
    let received = 0;
    const limiter = new Transform({
      transform(chunk, _encoding, callback) {
        received += chunk.length;
        if (received > MAX_ARCHIVE_BYTES) callback(new Error("SQLCIPHER_DOWNLOAD_FAILED"));
        else callback(null, chunk);
      },
    });
    await pipeline(Readable.fromWeb(response.body), limiter, handle.createWriteStream());
  } finally {
    await handle.close().catch(() => undefined);
  }
  const stats = await lstat(destination);
  if (
    !stats.isFile() ||
    stats.isSymbolicLink() ||
    stats.size <= 0 ||
    stats.size > MAX_ARCHIVE_BYTES
  ) {
    fail("SQLCIPHER_DOWNLOAD_FAILED");
  }
}

export async function extractPinnedSource({ destination, fetchImpl = globalThis.fetch, pin }) {
  if (
    !pin ||
    typeof pin !== "object" ||
    typeof pin.archiveName !== "string" ||
    typeof pin.globalPax !== "string" ||
    typeof pin.rootDirectory !== "string" ||
    typeof pin.sha256 !== "string" ||
    typeof pin.url !== "string" ||
    !path.isAbsolute(destination) ||
    typeof fetchImpl !== "function"
  ) {
    fail("PINNED_SOURCE_INPUT_INVALID");
  }
  const parent = path.dirname(destination);
  const parentStats = await lstat(parent).catch(() => null);
  if (!parentStats?.isDirectory() || parentStats.isSymbolicLink()) {
    fail("PINNED_SOURCE_INPUT_INVALID");
  }
  if (await lstat(destination).catch(() => null)) fail("PINNED_SOURCE_INPUT_INVALID");
  const archive = path.join(parent, pin.archiveName);
  try {
    await downloadPinnedArchive(archive, fetchImpl, pin.url);
    const compressed = await readFile(archive);
    if (createHash("sha256").update(compressed).digest("hex") !== pin.sha256) {
      fail("PINNED_SOURCE_CHECKSUM_MISMATCH");
    }
    let expanded;
    try {
      expanded = gunzipSync(compressed, { maxOutputLength: MAX_EXPANDED_BYTES });
    } catch {
      fail();
    }
    const entries = parseTarArchive(expanded, {
      pinnedGlobalPax: pin.globalPax,
      rootDirectory: pin.rootDirectory,
    });
    await extractValidated(entries, destination, pin.rootDirectory);
    return path.join(destination, pin.rootDirectory);
  } finally {
    await rm(archive, { force: true });
  }
}

function validateCacheRoot(cacheRoot) {
  if (!path.isAbsolute(cacheRoot) || path.normalize(cacheRoot) !== cacheRoot) {
    fail("SQLCIPHER_CACHE_INVALID");
  }
  const allowedStandalone =
    path.dirname(cacheRoot) === path.resolve(os.tmpdir()) &&
    path.basename(cacheRoot) === "tammy-sqlcipher-cache-v1";
  const allowedWorkspaceCache =
    path.basename(cacheRoot) === "sqlcipher" && path.basename(path.dirname(cacheRoot)) === ".tmp";
  if (
    cacheRoot === path.parse(cacheRoot).root ||
    cacheRoot === os.homedir() ||
    cacheRoot === path.resolve(process.cwd()) ||
    (!allowedStandalone && !allowedWorkspaceCache)
  ) {
    fail("SQLCIPHER_CACHE_INVALID");
  }
}

function sameDirectory(stats, expected) {
  return Boolean(
    stats?.isDirectory() &&
      !stats.isSymbolicLink() &&
      stats.dev === expected.dev &&
      stats.ino === expected.ino,
  );
}

export async function hashSourceTree(sourceRoot) {
  const rootStats = await lstat(sourceRoot);
  if (!rootStats.isDirectory() || rootStats.isSymbolicLink()) fail("SQLCIPHER_SOURCE_INVALID");
  const entries = [];
  let totalBytes = 0;
  async function visit(directory, prefix) {
    const children = await readdir(directory, { withFileTypes: true });
    children.sort((left, right) => Buffer.compare(Buffer.from(left.name), Buffer.from(right.name)));
    for (const child of children) {
      const relative = prefix ? `${prefix}/${child.name}` : child.name;
      if (
        child.name === "" ||
        child.name.includes("\\") ||
        [...child.name].some((character) => {
          const codePoint = character.codePointAt(0);
          return codePoint <= 0x1f || codePoint === 0x7f;
        })
      ) {
        fail("SQLCIPHER_SOURCE_INVALID");
      }
      const absolute = path.join(directory, child.name);
      const stats = await lstat(absolute);
      if (stats.isSymbolicLink()) fail("SQLCIPHER_SOURCE_INVALID");
      if (stats.isDirectory()) {
        entries.push({ relative, type: "directory" });
        await visit(absolute, relative);
      } else if (stats.isFile()) {
        if (stats.size > MAX_ENTRY_BYTES) fail("SQLCIPHER_SOURCE_INVALID");
        totalBytes += stats.size;
        if (totalBytes > MAX_EXPANDED_BYTES) fail("SQLCIPHER_SOURCE_INVALID");
        entries.push({ absolute, relative, size: stats.size, type: "file" });
      } else {
        fail("SQLCIPHER_SOURCE_INVALID");
      }
      if (entries.length > MAX_ENTRY_COUNT) fail("SQLCIPHER_SOURCE_INVALID");
    }
  }
  await visit(sourceRoot, "");
  entries.sort((left, right) =>
    Buffer.compare(Buffer.from(left.relative), Buffer.from(right.relative)),
  );
  const digest = createHash("sha256");
  digest.update("tammy-sqlcipher-source-tree-v1\0");
  for (const entry of entries) {
    digest.update(`${entry.type}\0${Buffer.byteLength(entry.relative)}\0${entry.relative}\0`);
    if (entry.type === "file") {
      const contents = await readFile(entry.absolute);
      if (contents.length !== entry.size) fail("SQLCIPHER_SOURCE_INVALID");
      digest.update(`${contents.length}\0`);
      digest.update(contents);
    }
  }
  return digest.digest("hex");
}

export async function authenticateCachedSource(destination, pinnedLicense, expectedTreeHash) {
  const stats = await lstat(destination).catch(() => null);
  if (!stats?.isDirectory() || stats.isSymbolicLink()) return false;
  const licensePath = path.join(destination, "LICENSE.md");
  const versionPath = path.join(destination, "VERSION");
  const [licenseStats, versionStats] = await Promise.all([
    lstat(licensePath).catch(() => null),
    lstat(versionPath).catch(() => null),
  ]);
  if (
    !licenseStats?.isFile() ||
    licenseStats.isSymbolicLink() ||
    !versionStats?.isFile() ||
    versionStats.isSymbolicLink()
  ) {
    return false;
  }
  const [license, sqliteVersion, sourceTreeHash] = await Promise.all([
    readFile(licensePath),
    readFile(versionPath, "utf8"),
    hashSourceTree(destination).catch(() => ""),
  ]);
  return (
    license.equals(pinnedLicense) &&
    sqliteVersion === "3.53.0\n" &&
    sourceTreeHash === expectedTreeHash
  );
}

export async function vendorSqlcipher({
  cacheRoot = path.join(os.tmpdir(), "tammy-sqlcipher-cache-v1"),
  fetchImpl = globalThis.fetch,
} = {}) {
  validateCacheRoot(cacheRoot);
  if (typeof fetchImpl !== "function") fail("SQLCIPHER_CACHE_INVALID");
  await mkdir(cacheRoot, { recursive: true, mode: 0o700 });
  const rootStats = await lstat(cacheRoot, { bigint: true });
  if (!rootStats.isDirectory() || rootStats.isSymbolicLink()) fail("SQLCIPHER_CACHE_INVALID");
  const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const pinnedLicense = await readFile(path.join(repositoryRoot, "third_party/sqlcipher/LICENSE"));
  const destination = path.join(cacheRoot, SQLCIPHER_RELEASE.rootDirectory);
  if (
    await authenticateCachedSource(destination, pinnedLicense, SQLCIPHER_RELEASE.sourceTreeSha256)
  ) {
    return Object.freeze({
      archiveSha256: SQLCIPHER_RELEASE.sha256,
      sourceRoot: destination,
      sourceTreeSha256: SQLCIPHER_RELEASE.sourceTreeSha256,
      version: SQLCIPHER_RELEASE.version,
    });
  }
  if (await lstat(destination).catch(() => null)) fail("SQLCIPHER_CACHE_INVALID");
  const temporary = await mkdtemp(path.join(cacheRoot, ".vendor-"));
  const archivePath = path.join(temporary, SQLCIPHER_RELEASE.archiveName);
  const staged = path.join(temporary, "source");
  try {
    await downloadPinnedArchive(archivePath, fetchImpl, SQLCIPHER_RELEASE.url);
    const compressed = await readFile(archivePath);
    const digest = createHash("sha256").update(compressed).digest("hex");
    if (digest !== SQLCIPHER_RELEASE.sha256) fail("SQLCIPHER_CHECKSUM_MISMATCH");
    let expanded;
    try {
      expanded = gunzipSync(compressed, { maxOutputLength: MAX_EXPANDED_BYTES });
    } catch {
      fail();
    }
    const entries = parseTarArchive(expanded);
    await extractValidated(entries, staged, SQLCIPHER_RELEASE.rootDirectory);
    const sourceRoot = path.join(staged, SQLCIPHER_RELEASE.rootDirectory);
    const [license, sqliteVersion] = await Promise.all([
      readFile(path.join(sourceRoot, "LICENSE.md")),
      readFile(path.join(sourceRoot, "VERSION"), "utf8"),
    ]);
    if (!license.equals(pinnedLicense) || sqliteVersion !== "3.53.0\n")
      fail("SQLCIPHER_SOURCE_INVALID");
    if ((await hashSourceTree(sourceRoot)) !== SQLCIPHER_RELEASE.sourceTreeSha256) {
      fail("SQLCIPHER_SOURCE_INVALID");
    }
    const currentRoot = await lstat(cacheRoot, { bigint: true }).catch(() => null);
    if (!sameDirectory(currentRoot, rootStats)) fail("SQLCIPHER_CACHE_INVALID");
    await rename(sourceRoot, destination);
    const finalRoot = await lstat(cacheRoot, { bigint: true }).catch(() => null);
    if (!sameDirectory(finalRoot, rootStats)) fail("SQLCIPHER_CACHE_INVALID");
    return Object.freeze({
      archiveSha256: digest,
      sourceRoot: destination,
      sourceTreeSha256: SQLCIPHER_RELEASE.sourceTreeSha256,
      version: SQLCIPHER_RELEASE.version,
    });
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
}

async function main() {
  const result = await vendorSqlcipher();
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main().catch((error) => {
    process.stderr.write(
      `${JSON.stringify({ error: error instanceof Error ? error.message : "SQLCIPHER_VENDOR_FAILED" })}\n`,
    );
    process.exitCode = 1;
  });
}
