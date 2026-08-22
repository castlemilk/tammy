import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { lstat as lstatPath, open as openFile } from "node:fs/promises";
import { runInNewContext } from "node:vm";

import canonicalize from "canonicalize";

import { assertJsonStructure, assertUnicodeScalarString } from "./sbr-profile-schema.mjs";

// Execute the pinned RFC 8785 implementation with clean realm intrinsics so ambient prototype
// pollution cannot change its Object.keys(...).sort().reduce(...) behavior.
const isolatedCanonicalize = runInNewContext(`(${canonicalize.toString()})`);

export const MAX_COMPONENT_MANIFEST_BYTES = 256 * 1024;
export const MAX_COMPONENT_FILES = 256;
export const MAX_COMPONENT_FILE_BYTES = 64 * 1024 * 1024;
export const MAX_COMPONENT_BUNDLE_BYTES = 256 * 1024 * 1024;
export const MAX_COMPONENT_ENTRIES = 512;
export const MAX_COMPONENT_DEPTH = 16;
// Component/version identifiers are portable ASCII tokens, capped at 64 bytes.
export const MAX_COMPONENT_IDENTIFIER_BYTES = 64;
// Manifest paths are canonical NFC relative POSIX paths, capped at 1 KiB of UTF-8.
export const MAX_COMPONENT_PATH_BYTES = 1_024;

const MAX_JSON_NESTING_DEPTH = 32;
const MANIFEST_KEYS = ["component_name", "component_version", "files", "schema_version", "target"];
const FILE_KEYS = ["byte_length", "path", "sha256"];
const IDENTIFIER = /^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$/;
const LOWERCASE_SHA256 = /^[0-9a-f]{64}$/;

function invalid(reason) {
  return new Error(`SBR_COMPONENT_INVALID:${reason}`);
}

function hasControlCharacter(value) {
  for (const character of value) {
    const codePoint = character.codePointAt(0);
    if (codePoint <= 0x1f || (codePoint >= 0x7f && codePoint <= 0x9f)) {
      return true;
    }
  }
  return false;
}

function exactDataSnapshot(value, expectedKeys, fieldError) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw invalid(fieldError);
  }
  let actualKeys;
  try {
    actualKeys = Reflect.ownKeys(value);
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (actualKeys.length !== expectedKeys.length) {
    throw invalid(fieldError);
  }
  for (let index = 0; index < actualKeys.length; index += 1) {
    if (typeof actualKeys[index] !== "string") {
      throw invalid(fieldError);
    }
  }
  for (let index = 0; index < expectedKeys.length; index += 1) {
    if (!Object.hasOwn(value, expectedKeys[index])) {
      throw invalid(fieldError);
    }
  }
  try {
    if ("toJSON" in value) {
      throw invalid("CANONICALIZATION");
    }
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("SBR_COMPONENT_INVALID:")) {
      throw error;
    }
    throw invalid("CANONICALIZATION");
  }
  const snapshot = Object.create(null);
  for (const key of expectedKeys) {
    let descriptor;
    try {
      descriptor = Object.getOwnPropertyDescriptor(value, key);
    } catch {
      throw invalid("CANONICALIZATION");
    }
    if (!descriptor?.enumerable || !("value" in descriptor)) {
      throw invalid("CANONICALIZATION");
    }
    Object.defineProperty(snapshot, key, { enumerable: true, value: descriptor.value });
  }
  return snapshot;
}

function validateIdentifier(value, field) {
  if (typeof value === "string") {
    assertUnicodeScalarString(value, invalid);
  }
  if (
    typeof value !== "string" ||
    Buffer.byteLength(value, "utf8") > MAX_COMPONENT_IDENTIFIER_BYTES ||
    !IDENTIFIER.test(value)
  ) {
    throw invalid(field);
  }
}

function validateRelativePath(value) {
  if (typeof value !== "string") {
    throw invalid("FILE_PATH");
  }
  assertUnicodeScalarString(value, invalid);
  if (
    value.length === 0 ||
    value !== value.normalize("NFC") ||
    Buffer.byteLength(value, "utf8") > MAX_COMPONENT_PATH_BYTES ||
    value.startsWith("/") ||
    value.endsWith("/") ||
    value.includes("\\") ||
    hasControlCharacter(value)
  ) {
    throw invalid("FILE_PATH");
  }
  const segments = value.split("/");
  for (let index = 0; index < segments.length; index += 1) {
    if (segments[index] === "" || segments[index] === "." || segments[index] === "..") {
      throw invalid("FILE_PATH");
    }
  }
  return value;
}

function compareUtf8(left, right) {
  return Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8"));
}

function sortUtf8WithoutPrototypeMethods(values) {
  const sorted = new Array(values.length);
  for (let index = 0; index < values.length; index += 1) {
    Object.defineProperty(sorted, String(index), {
      configurable: true,
      enumerable: true,
      value: values[index],
      writable: true,
    });
  }
  for (let index = 1; index < sorted.length; index += 1) {
    const value = sorted[index];
    let cursor = index - 1;
    while (
      cursor >= 0 &&
      compareUtf8(sorted[cursor]?.name ?? sorted[cursor], value?.name ?? value) > 0
    ) {
      sorted[cursor + 1] = sorted[cursor];
      cursor -= 1;
    }
    sorted[cursor + 1] = value;
  }
  return sorted;
}

function snapshotArrayValues(value) {
  if (!Array.isArray(value)) {
    throw invalid("FILES");
  }
  const lengthDescriptor = Object.getOwnPropertyDescriptor(value, "length");
  if (
    !lengthDescriptor ||
    !("value" in lengthDescriptor) ||
    !Number.isSafeInteger(lengthDescriptor.value)
  ) {
    throw invalid("CANONICALIZATION");
  }
  const length = lengthDescriptor.value;
  if (length < 1 || length > MAX_COMPONENT_FILES) {
    throw invalid("FILE_COUNT");
  }
  let keys;
  try {
    keys = Reflect.ownKeys(value);
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (keys.length !== length + 1) {
    throw invalid("CANONICALIZATION");
  }
  const values = new Array(length);
  for (let index = 0; index < length; index += 1) {
    const key = String(index);
    let descriptor;
    try {
      descriptor = Object.getOwnPropertyDescriptor(value, key);
    } catch {
      throw invalid("CANONICALIZATION");
    }
    if (!descriptor?.enumerable || !("value" in descriptor)) {
      throw invalid("CANONICALIZATION");
    }
    Object.defineProperty(values, key, {
      configurable: true,
      enumerable: true,
      value: descriptor.value,
      writable: true,
    });
  }
  return values;
}

function createCanonicalArray(values) {
  const hardened = [];
  Object.setPrototypeOf(hardened, null);
  for (let index = 0; index < values.length; index += 1) {
    Object.defineProperty(hardened, String(index), {
      enumerable: true,
      value: values[index],
    });
  }
  Object.defineProperty(hardened, "reduce", {
    value: function reduce(callback, initialValue) {
      let accumulator = initialValue;
      for (let index = 0; index < this.length; index += 1) {
        accumulator = callback(accumulator, this[index], index, this);
      }
      return accumulator;
    },
  });
  return Object.freeze(hardened);
}

function createCanonicalGraph(manifest) {
  const canonicalFiles = new Array(manifest.files.length);
  for (let index = 0; index < manifest.files.length; index += 1) {
    const source = manifest.files[index];
    const file = Object.create(null);
    file.byte_length = source.byte_length;
    file.path = source.path;
    file.sha256 = source.sha256;
    Object.defineProperty(canonicalFiles, String(index), {
      configurable: true,
      enumerable: true,
      value: Object.freeze(file),
      writable: true,
    });
  }
  const root = Object.create(null);
  root.component_name = manifest.component_name;
  root.component_version = manifest.component_version;
  root.files = createCanonicalArray(canonicalFiles);
  root.schema_version = manifest.schema_version;
  root.target = manifest.target;
  return Object.freeze(root);
}

function validateManifestData(value) {
  const manifest = exactDataSnapshot(value, MANIFEST_KEYS, "FIELDS");
  if (manifest.schema_version !== 1) {
    throw invalid("SCHEMA_VERSION");
  }
  validateIdentifier(manifest.component_name, "COMPONENT_NAME");
  validateIdentifier(manifest.component_version, "COMPONENT_VERSION");
  if (manifest.target !== "darwin/arm64") {
    throw invalid("TARGET");
  }
  const sourceFiles = snapshotArrayValues(manifest.files);

  const files = new Array(sourceFiles.length);
  const normalizedPaths = new Set();
  let previousPath;
  let aggregateBytes = 0;
  for (let index = 0; index < sourceFiles.length; index += 1) {
    const file = sourceFiles[index];
    const snapshot = exactDataSnapshot(file, FILE_KEYS, "FILE_FIELDS");
    const normalizedPath = validateRelativePath(snapshot.path);
    if (normalizedPaths.has(normalizedPath)) {
      throw invalid("FILE_DUPLICATE");
    }
    if (previousPath !== undefined && compareUtf8(previousPath, normalizedPath) >= 0) {
      throw invalid("FILE_ORDER");
    }
    normalizedPaths.add(normalizedPath);
    previousPath = normalizedPath;
    if (
      !Number.isSafeInteger(snapshot.byte_length) ||
      snapshot.byte_length < 0 ||
      snapshot.byte_length > MAX_COMPONENT_FILE_BYTES
    ) {
      throw invalid("FILE_LENGTH");
    }
    aggregateBytes += snapshot.byte_length;
    if (!Number.isSafeInteger(aggregateBytes) || aggregateBytes > MAX_COMPONENT_BUNDLE_BYTES) {
      throw invalid("BUNDLE_SIZE");
    }
    if (typeof snapshot.sha256 !== "string" || !LOWERCASE_SHA256.test(snapshot.sha256)) {
      throw invalid("FILE_HASH");
    }
    Object.defineProperty(files, String(index), {
      configurable: true,
      enumerable: true,
      value: Object.freeze({
        byte_length: snapshot.byte_length,
        path: snapshot.path,
        sha256: snapshot.sha256,
      }),
      writable: true,
    });
  }

  return Object.freeze({
    component_name: manifest.component_name,
    component_version: manifest.component_version,
    files: Object.freeze(files),
    schema_version: manifest.schema_version,
    target: manifest.target,
  });
}

function canonicalManifestBytes(manifest) {
  try {
    const canonical = isolatedCanonicalize(createCanonicalGraph(manifest));
    if (typeof canonical !== "string") {
      throw invalid("CANONICALIZATION");
    }
    return Buffer.from(canonical, "utf8");
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("SBR_COMPONENT_INVALID:")) {
      throw error;
    }
    throw invalid("CANONICALIZATION");
  }
}

function unwrapParsedManifest(value) {
  if (value === null || typeof value !== "object") {
    return value;
  }
  let descriptor;
  try {
    descriptor = Object.getOwnPropertyDescriptor(value, "manifest");
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (!descriptor) {
    return value;
  }
  if (!("value" in descriptor) || !descriptor.enumerable) {
    throw invalid("CANONICALIZATION");
  }
  return descriptor.value;
}

function parsedManifestResult(value) {
  const manifest = validateManifestData(unwrapParsedManifest(value));
  const canonicalBytes = canonicalManifestBytes(manifest);
  if (canonicalBytes.byteLength > MAX_COMPONENT_MANIFEST_BYTES) {
    throw invalid("MANIFEST_TOO_LARGE");
  }
  return Object.freeze({
    canonicalBytes,
    manifest,
    sha256: createHash("sha256").update(canonicalBytes).digest("hex"),
  });
}

function readCrossHash(source, errorCode) {
  if (source === null || typeof source !== "object") {
    throw invalid(errorCode);
  }
  let descriptor;
  try {
    descriptor = Object.getOwnPropertyDescriptor(source, "component_manifest_sha256");
  } catch {
    throw invalid(errorCode);
  }
  if (!descriptor?.enumerable || !("value" in descriptor)) {
    throw invalid(errorCode);
  }
  return descriptor.value;
}

function decodeManifest(rawManifest) {
  if (typeof rawManifest === "string") {
    if (Buffer.byteLength(rawManifest, "utf8") > MAX_COMPONENT_MANIFEST_BYTES) {
      throw invalid("MANIFEST_TOO_LARGE");
    }
    return rawManifest;
  }
  if (!(rawManifest instanceof Uint8Array)) {
    throw invalid("MANIFEST_INPUT");
  }
  if (rawManifest.byteLength > MAX_COMPONENT_MANIFEST_BYTES) {
    throw invalid("MANIFEST_TOO_LARGE");
  }
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(rawManifest);
  } catch {
    throw invalid("MANIFEST_UTF8");
  }
}

export function parseAndValidateSbrComponentManifest(rawManifest) {
  const raw = decodeManifest(rawManifest);
  assertJsonStructure(raw, { makeInvalid: invalid, maximumDepth: MAX_JSON_NESTING_DEPTH });
  let manifest;
  try {
    manifest = JSON.parse(raw);
  } catch {
    throw invalid("JSON");
  }
  return parsedManifestResult(manifest);
}

export function verifySbrComponentCrossHashes({
  componentManifest,
  profile,
  registrationManifest,
}) {
  const parsed = parsedManifestResult(componentManifest);
  const registrationHash = readCrossHash(registrationManifest, "REGISTRATION_HASH");
  const profileHash = readCrossHash(profile, "PROFILE_HASH");
  if (typeof registrationHash !== "string" || !LOWERCASE_SHA256.test(registrationHash)) {
    throw invalid("REGISTRATION_HASH");
  }
  if (typeof profileHash !== "string" || !LOWERCASE_SHA256.test(profileHash)) {
    throw invalid("PROFILE_HASH");
  }
  if (registrationHash !== profileHash) {
    throw invalid("CROSS_HASH_MISMATCH");
  }
  if (registrationHash !== parsed.sha256) {
    throw invalid("COMPONENT_HASH_MISMATCH");
  }
  return true;
}

function secureOpenFlags(dependencies) {
  const noFollowFlag = dependencies.noFollowFlag ?? constants.O_NOFOLLOW;
  if (
    !Number.isSafeInteger(noFollowFlag) ||
    noFollowFlag <= 0 ||
    noFollowFlag !== constants.O_NOFOLLOW
  ) {
    throw invalid("NOFOLLOW_UNAVAILABLE");
  }
  const nonBlockFlag = dependencies.nonBlockFlag ?? constants.O_NONBLOCK;
  if (
    !Number.isSafeInteger(nonBlockFlag) ||
    nonBlockFlag <= 0 ||
    nonBlockFlag !== constants.O_NONBLOCK
  ) {
    throw invalid("NONBLOCK_UNAVAILABLE");
  }
  return constants.O_RDONLY | noFollowFlag | nonBlockFlag;
}

async function closeHandle(handle, failure) {
  try {
    await handle.close();
  } catch {
    return failure ?? invalid("FILE_CLOSE");
  }
  return failure;
}

function descriptorRelativeAdapter(dependencies) {
  let descriptor;
  try {
    descriptor = Object.getOwnPropertyDescriptor(dependencies, "anchored");
  } catch {
    throw invalid("ANCHORED_TRAVERSAL_UNAVAILABLE");
  }
  if (!descriptor?.enumerable || !("value" in descriptor)) {
    throw invalid("ANCHORED_TRAVERSAL_UNAVAILABLE");
  }
  const adapter = descriptor.value;
  if (adapter === null || typeof adapter !== "object") {
    throw invalid("ANCHORED_TRAVERSAL_UNAVAILABLE");
  }
  const methods = Object.create(null);
  for (const name of ["lstat", "open", "readdir"]) {
    let method;
    try {
      method = Object.getOwnPropertyDescriptor(adapter, name);
    } catch {
      throw invalid("ANCHORED_TRAVERSAL_UNAVAILABLE");
    }
    if (!method?.enumerable || !("value" in method) || typeof method.value !== "function") {
      throw invalid("ANCHORED_TRAVERSAL_UNAVAILABLE");
    }
    methods[name] = method.value;
  }
  return Object.freeze(methods);
}

function metadataIdentity(metadata, expectedKind, errorCode = "IDENTITY_UNAVAILABLE") {
  const matchesKind =
    expectedKind === "directory" ? metadata?.isDirectory?.() : metadata?.isFile?.();
  if (!matchesKind) {
    throw invalid(errorCode);
  }
  const validIdentityPart = (value) =>
    (typeof value === "bigint" && value >= 0n) || (Number.isSafeInteger(value) && value >= 0);
  if (!validIdentityPart(metadata.dev) || !validIdentityPart(metadata.ino)) {
    throw invalid("IDENTITY_UNAVAILABLE");
  }
  return `${metadata.dev.toString()}:${metadata.ino.toString()}`;
}

function metadataSize(metadata) {
  if (typeof metadata.size === "bigint" && metadata.size >= 0n) {
    return metadata.size;
  }
  if (Number.isSafeInteger(metadata.size) && metadata.size >= 0) {
    return BigInt(metadata.size);
  }
  throw invalid("FILE_STAT");
}

async function handleMetadata(handle, errorCode) {
  try {
    return await handle.stat({ bigint: true });
  } catch {
    throw invalid(errorCode);
  }
}

async function pathMetadata(entryPath, filesystem, errorCode) {
  try {
    return await filesystem.lstat(entryPath, { bigint: true });
  } catch {
    throw invalid(errorCode);
  }
}

async function openAndCheckDirectory(componentRoot, filesystem, flags) {
  let handle;
  try {
    handle = await filesystem.open(componentRoot, flags);
  } catch {
    throw invalid("ROOT_OPEN");
  }
  let failure;
  let identity;
  try {
    const metadata = await handleMetadata(handle, "ROOT_STAT");
    identity = metadataIdentity(metadata, "directory", "ROOT_NOT_DIRECTORY");
  } catch (error) {
    failure =
      error instanceof Error && error.message.startsWith("SBR_COMPONENT_INVALID:")
        ? error
        : invalid("ROOT_STAT");
  }
  if (failure) {
    throw await closeHandle(handle, failure);
  }
  return { handle, identity };
}

async function openAnchoredEntry(
  parentHandle,
  childName,
  expectedMetadata,
  expectedKind,
  anchored,
  flags,
) {
  let handle;
  try {
    handle = await anchored.open(parentHandle, childName, flags);
  } catch {
    throw invalid("PATH_SWAP");
  }
  let failure;
  let identity;
  try {
    const expectedIdentity = metadataIdentity(expectedMetadata, expectedKind, "PATH_SWAP");
    const metadata = await handleMetadata(handle, "ENTRY_STAT");
    identity = metadataIdentity(metadata, expectedKind, "PATH_SWAP");
    if (identity !== expectedIdentity) {
      throw invalid("IDENTITY_MISMATCH");
    }
  } catch (error) {
    failure =
      error instanceof Error && error.message.startsWith("SBR_COMPONENT_INVALID:")
        ? error
        : invalid("ENTRY_STAT");
  }
  if (failure) {
    throw await closeHandle(handle, failure);
  }
  return { handle, identity };
}

async function verifyOpenedFile({
  anchored,
  childName,
  declaration,
  handle,
  identity,
  parentHandle,
}) {
  let failure;
  try {
    const firstMetadata = await handleMetadata(handle, "FILE_STAT");
    if (metadataIdentity(firstMetadata, "file", "FILE_NOT_REGULAR") !== identity) {
      throw invalid("IDENTITY_MISMATCH");
    }
    if (metadataSize(firstMetadata) !== BigInt(declaration.byte_length)) {
      throw invalid("FILE_LENGTH_MISMATCH");
    }
    const maximumRead = declaration.byte_length + 1;
    const bytes = Buffer.alloc(maximumRead);
    let total = 0;
    while (total < maximumRead) {
      let result;
      try {
        result = await handle.read(bytes, total, maximumRead - total, null);
      } catch {
        throw invalid("FILE_READ");
      }
      if (
        !Number.isSafeInteger(result?.bytesRead) ||
        result.bytesRead < 0 ||
        result.bytesRead > maximumRead - total
      ) {
        throw invalid("FILE_READ");
      }
      if (result.bytesRead === 0) {
        break;
      }
      total += result.bytesRead;
    }
    if (total !== declaration.byte_length) {
      throw invalid("FILE_LENGTH_MISMATCH");
    }
    const digest = createHash("sha256").update(bytes.subarray(0, total)).digest("hex");
    if (digest !== declaration.sha256) {
      throw invalid("FILE_HASH_MISMATCH");
    }
    const finalMetadata = await handleMetadata(handle, "FILE_STAT");
    if (
      metadataIdentity(finalMetadata, "file", "FILE_NOT_REGULAR") !== identity ||
      metadataSize(finalMetadata) !== BigInt(declaration.byte_length)
    ) {
      throw invalid("IDENTITY_MISMATCH");
    }
    let finalPathMetadata;
    try {
      finalPathMetadata = await anchored.lstat(parentHandle, childName, { bigint: true });
    } catch {
      throw invalid("PATH_SWAP");
    }
    if (metadataIdentity(finalPathMetadata, "file", "PATH_SWAP") !== identity) {
      throw invalid("PATH_SWAP");
    }
  } catch (error) {
    failure =
      error instanceof Error && error.message.startsWith("SBR_COMPONENT_INVALID:")
        ? error
        : invalid("FILE_READ");
  }
  failure = await closeHandle(handle, failure);
  if (failure) {
    throw failure;
  }
  return identity;
}

function validateEntryName(name) {
  if (
    typeof name !== "string" ||
    name.length === 0 ||
    name === "." ||
    name === ".." ||
    name !== name.normalize("NFC") ||
    name.includes("/") ||
    name.includes("\\") ||
    hasControlCharacter(name)
  ) {
    throw invalid("PATH_ALIAS");
  }
  assertUnicodeScalarString(name, invalid);
}

// This bounded Node verifier is advisory preflight. The later Go core remains authoritative.
export async function verifySbrComponentBundle({ componentRoot, dependencies = {}, manifest }) {
  if (typeof componentRoot !== "string" || componentRoot.length === 0) {
    throw invalid("COMPONENT_ROOT");
  }
  const parsed = parsedManifestResult(manifest);
  const declarations = new Map();
  for (let index = 0; index < parsed.manifest.files.length; index += 1) {
    const file = parsed.manifest.files[index];
    declarations.set(file.path, file);
  }
  const filesystem = {
    lstat: lstatPath,
    open: openFile,
    ...dependencies,
  };
  const flags = secureOpenFlags(filesystem);
  // Node/Darwin has no public openat/fstatat/readdir-at API and /dev/fd directory traversal is
  // unavailable. Callers must supply the later Go-owned descriptor-relative boundary; otherwise
  // this advisory layer fails before opening or reading any component input.
  const anchored = descriptorRelativeAdapter(dependencies);
  const root = await openAndCheckDirectory(componentRoot, filesystem, flags);
  const actualFiles = new Set();
  const fileIdentities = new Map();
  let entryCount = 0;
  let failure;

  function hasDeclaredDescendant(prefix) {
    for (const declaredPath of declarations.keys()) {
      if (declaredPath.startsWith(prefix)) {
        return true;
      }
    }
    return false;
  }

  async function enumerate(directory, relativeDirectory, depth) {
    const beforeMetadata = await handleMetadata(directory.handle, "DIRECTORY_STAT");
    if (metadataIdentity(beforeMetadata, "directory", "PATH_SWAP") !== directory.identity) {
      throw invalid("IDENTITY_MISMATCH");
    }
    let entries;
    try {
      entries = await anchored.readdir(directory.handle);
    } catch {
      throw invalid("DIRECTORY_READ");
    }
    if (!Array.isArray(entries)) {
      throw invalid("DIRECTORY_READ");
    }
    entryCount += entries.length;
    if (entryCount > MAX_COMPONENT_ENTRIES) {
      throw invalid("ENTRY_COUNT");
    }
    const names = new Set();
    const sortedEntries = sortUtf8WithoutPrototypeMethods(entries);
    for (const entry of sortedEntries) {
      validateEntryName(entry?.name);
      if (names.has(entry.name)) {
        throw invalid("PATH_ALIAS");
      }
      names.add(entry.name);
      const relativePath = relativeDirectory ? `${relativeDirectory}/${entry.name}` : entry.name;
      validateRelativePath(relativePath);
      if (depth > MAX_COMPONENT_DEPTH) {
        throw invalid("DIRECTORY_DEPTH");
      }
      let metadata;
      try {
        metadata = await anchored.lstat(directory.handle, entry.name, { bigint: true });
      } catch {
        throw invalid("ENTRY_STAT");
      }
      if (metadata?.isSymbolicLink?.()) {
        throw invalid("SYMLINK");
      }
      if (metadata?.isDirectory?.()) {
        const prefix = `${relativePath}/`;
        if (!hasDeclaredDescendant(prefix)) {
          throw invalid("UNDECLARED_PATH");
        }
        const child = await openAnchoredEntry(
          directory.handle,
          entry.name,
          metadata,
          "directory",
          anchored,
          flags,
        );
        let childFailure;
        try {
          await enumerate(child, relativePath, depth + 1);
          const finalChildMetadata = await handleMetadata(child.handle, "DIRECTORY_STAT");
          if (metadataIdentity(finalChildMetadata, "directory", "PATH_SWAP") !== child.identity) {
            throw invalid("IDENTITY_MISMATCH");
          }
          let finalPathMetadata;
          try {
            finalPathMetadata = await anchored.lstat(directory.handle, entry.name, {
              bigint: true,
            });
          } catch {
            throw invalid("PATH_SWAP");
          }
          if (metadataIdentity(finalPathMetadata, "directory", "PATH_SWAP") !== child.identity) {
            throw invalid("PATH_SWAP");
          }
        } catch (error) {
          childFailure =
            error instanceof Error && error.message.startsWith("SBR_COMPONENT_INVALID:")
              ? error
              : invalid("BUNDLE_READ");
        }
        childFailure = await closeHandle(child.handle, childFailure);
        if (childFailure) {
          throw childFailure;
        }
      } else if (metadata?.isFile?.()) {
        const declaration = declarations.get(relativePath);
        if (!declaration) {
          throw invalid("UNDECLARED_PATH");
        }
        if (actualFiles.has(relativePath)) {
          throw invalid("PATH_ALIAS");
        }
        const opened = await openAnchoredEntry(
          directory.handle,
          entry.name,
          metadata,
          "file",
          anchored,
          flags,
        );
        const identity = await verifyOpenedFile({
          anchored,
          childName: entry.name,
          declaration,
          handle: opened.handle,
          identity: opened.identity,
          parentHandle: directory.handle,
        });
        if (fileIdentities.has(identity)) {
          throw invalid("PATH_ALIAS");
        }
        fileIdentities.set(identity, relativePath);
        actualFiles.add(relativePath);
      } else {
        throw invalid("SPECIAL_FILE");
      }
    }
    const afterMetadata = await handleMetadata(directory.handle, "DIRECTORY_STAT");
    if (metadataIdentity(afterMetadata, "directory", "PATH_SWAP") !== directory.identity) {
      throw invalid("IDENTITY_MISMATCH");
    }
  }

  try {
    await enumerate(root, "", 1);
    let declaredFileMissing = actualFiles.size !== declarations.size;
    for (const declaredPath of declarations.keys()) {
      if (!actualFiles.has(declaredPath)) {
        declaredFileMissing = true;
      }
    }
    if (declaredFileMissing) {
      throw invalid("DECLARED_FILE_MISSING");
    }
    const finalRootMetadata = await pathMetadata(componentRoot, filesystem, "PATH_SWAP");
    if (metadataIdentity(finalRootMetadata, "directory", "PATH_SWAP") !== root.identity) {
      throw invalid("PATH_SWAP");
    }
  } catch (error) {
    failure =
      error instanceof Error && error.message.startsWith("SBR_COMPONENT_INVALID:")
        ? error
        : invalid("BUNDLE_READ");
  }
  failure = await closeHandle(root.handle, failure);
  if (failure) {
    throw failure;
  }

  let totalByteLength = 0;
  for (let index = 0; index < parsed.manifest.files.length; index += 1) {
    totalByteLength += parsed.manifest.files[index].byte_length;
  }
  const resultFiles = new Array(actualFiles.size);
  let resultIndex = 0;
  for (const relativePath of actualFiles) {
    resultFiles[resultIndex] = relativePath;
    resultIndex += 1;
  }
  const sortedResultFiles = sortUtf8WithoutPrototypeMethods(resultFiles);
  return Object.freeze({
    component_manifest_sha256: parsed.sha256,
    files: Object.freeze(sortedResultFiles),
    total_byte_length: totalByteLength,
  });
}
