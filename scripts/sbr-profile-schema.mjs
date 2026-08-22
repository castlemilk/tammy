import { createPublicKey, verify } from "node:crypto";
import { constants } from "node:fs";
import { open as openFile } from "node:fs/promises";

import canonicalize from "canonicalize";

export const MAX_PROFILE_BYTES = 64 * 1024;
export const MAX_SIGNATURE_BYTES = 128;
export const MAX_PUBLIC_KEY_BYTES = 4 * 1024;

const PROFILE_KEYS = [
  "component_manifest_sha256",
  "endpoint_profile_sha256",
  "environment",
  "expires_at",
  "helper_sha256",
  "issued_at",
  "registration_manifest_sha256",
  "schema_version",
  "target",
];
const LOWERCASE_SHA256 = /^[0-9a-f]{64}$/;
const MAX_JSON_NESTING_DEPTH = 32;
const CANONICAL_PUBLIC_KEY_PEM =
  /^-----BEGIN PUBLIC KEY-----\n(?:[A-Za-z0-9+/]{64}\n)*[A-Za-z0-9+/]{4,64}={0,2}\n-----END PUBLIC KEY-----\n$/;
const UTC_TIMESTAMP =
  /^(\d{4})-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T([01]\d|2[0-3]):([0-5]\d):([0-5]\d)Z$/;
const STRICT_BASE64_SIGNATURE = /^[A-Za-z0-9+/]{86}==\n$/;

function invalid(reason) {
  return new Error(`SBR_PROFILE_INVALID:${reason}`);
}

function decodeProfile(rawProfile) {
  if (typeof rawProfile === "string") {
    if (Buffer.byteLength(rawProfile) > MAX_PROFILE_BYTES) {
      throw invalid("PROFILE_TOO_LARGE");
    }
    return rawProfile;
  }
  if (!(rawProfile instanceof Uint8Array)) {
    throw invalid("PROFILE_INPUT");
  }
  if (rawProfile.byteLength > MAX_PROFILE_BYTES) {
    throw invalid("PROFILE_TOO_LARGE");
  }
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(rawProfile);
  } catch {
    throw invalid("PROFILE_UTF8");
  }
}

function assertUnicodeScalarString(value) {
  for (let index = 0; index < value.length; index += 1) {
    const codeUnit = value.charCodeAt(index);
    if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (next < 0xdc00 || next > 0xdfff) {
        throw invalid("UNICODE");
      }
      index += 1;
    } else if (codeUnit >= 0xdc00 && codeUnit <= 0xdfff) {
      throw invalid("UNICODE");
    }
  }
}

// This structural pass runs before JSON.parse so duplicate member names cannot be erased.
function assertJsonStructure(raw) {
  let cursor = 0;

  function skipWhitespace() {
    while (cursor < raw.length && /[\t\n\r ]/.test(raw[cursor])) {
      cursor += 1;
    }
  }

  function parseString() {
    if (raw[cursor] !== '"') {
      throw invalid("JSON");
    }
    const start = cursor;
    cursor += 1;
    while (cursor < raw.length) {
      const character = raw[cursor];
      if (character === '"') {
        cursor += 1;
        let decoded;
        try {
          decoded = JSON.parse(raw.slice(start, cursor));
        } catch {
          throw invalid("JSON");
        }
        assertUnicodeScalarString(decoded);
        return decoded;
      }
      if (character === "\\") {
        cursor += 1;
        const escapeCode = raw[cursor];
        if (escapeCode === "u") {
          if (!/^[0-9a-fA-F]{4}$/.test(raw.slice(cursor + 1, cursor + 5))) {
            throw invalid("JSON");
          }
          cursor += 5;
          continue;
        }
        if (!['"', "\\", "/", "b", "f", "n", "r", "t"].includes(escapeCode)) {
          throw invalid("JSON");
        }
        cursor += 1;
        continue;
      }
      if (character.charCodeAt(0) < 0x20) {
        throw invalid("JSON");
      }
      cursor += 1;
    }
    throw invalid("JSON");
  }

  function parsePrimitive() {
    const start = cursor;
    while (cursor < raw.length && !/[\t\n\r ,\]}]/.test(raw[cursor])) {
      cursor += 1;
    }
    const token = raw.slice(start, cursor);
    try {
      const value = JSON.parse(token);
      if (value !== null && typeof value === "object") {
        throw invalid("JSON");
      }
    } catch (error) {
      if (error instanceof Error && error.message.startsWith("SBR_PROFILE_INVALID:")) {
        throw error;
      }
      throw invalid("JSON");
    }
  }

  function parseArray(depth) {
    cursor += 1;
    skipWhitespace();
    if (raw[cursor] === "]") {
      cursor += 1;
      return;
    }
    while (cursor < raw.length) {
      parseValue(depth);
      skipWhitespace();
      if (raw[cursor] === "]") {
        cursor += 1;
        return;
      }
      if (raw[cursor] !== ",") {
        throw invalid("JSON");
      }
      cursor += 1;
      skipWhitespace();
    }
    throw invalid("JSON");
  }

  function parseObject(depth) {
    cursor += 1;
    const keys = new Set();
    skipWhitespace();
    if (raw[cursor] === "}") {
      cursor += 1;
      return;
    }
    while (cursor < raw.length) {
      const key = parseString();
      if (keys.has(key)) {
        throw invalid("DUPLICATE_KEY");
      }
      keys.add(key);
      skipWhitespace();
      if (raw[cursor] !== ":") {
        throw invalid("JSON");
      }
      cursor += 1;
      parseValue(depth);
      skipWhitespace();
      if (raw[cursor] === "}") {
        cursor += 1;
        return;
      }
      if (raw[cursor] !== ",") {
        throw invalid("JSON");
      }
      cursor += 1;
      skipWhitespace();
    }
    throw invalid("JSON");
  }

  function parseValue(depth = 0) {
    skipWhitespace();
    if (raw[cursor] === "{") {
      if (depth >= MAX_JSON_NESTING_DEPTH) {
        throw invalid("JSON_DEPTH");
      }
      parseObject(depth + 1);
    } else if (raw[cursor] === "[") {
      if (depth >= MAX_JSON_NESTING_DEPTH) {
        throw invalid("JSON_DEPTH");
      }
      parseArray(depth + 1);
    } else if (raw[cursor] === '"') {
      parseString();
    } else {
      parsePrimitive();
    }
  }

  parseValue();
  skipWhitespace();
  if (cursor !== raw.length) {
    throw invalid("TRAILING_DATA");
  }
}

function parseTimestamp(value, field) {
  if (typeof value !== "string" || !UTC_TIMESTAMP.test(value)) {
    throw invalid(`${field}_TIMESTAMP`);
  }
  const milliseconds = Date.parse(value);
  if (!Number.isFinite(milliseconds)) {
    throw invalid(`${field}_TIMESTAMP`);
  }
  const canonical = new Date(milliseconds).toISOString().replace(".000Z", "Z");
  if (canonical !== value) {
    throw invalid(`${field}_TIMESTAMP`);
  }
  return milliseconds;
}

function dataOnlyProfileSnapshot(profile) {
  if (profile === null || typeof profile !== "object" || Array.isArray(profile)) {
    throw invalid("ROOT");
  }
  let actual;
  try {
    actual = Reflect.ownKeys(profile);
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (actual.some((key) => typeof key !== "string")) {
    throw invalid("FIELDS");
  }
  actual.sort();
  if (actual.length !== PROFILE_KEYS.length) {
    throw invalid("FIELDS");
  }
  for (let index = 0; index < PROFILE_KEYS.length; index += 1) {
    if (actual[index] !== PROFILE_KEYS[index]) {
      throw invalid("FIELDS");
    }
  }
  try {
    if ("toJSON" in profile) {
      throw invalid("CANONICALIZATION");
    }
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("SBR_PROFILE_INVALID:")) {
      throw error;
    }
    throw invalid("CANONICALIZATION");
  }
  const snapshot = Object.create(null);
  for (const field of PROFILE_KEYS) {
    let descriptor;
    try {
      descriptor = Object.getOwnPropertyDescriptor(profile, field);
    } catch {
      throw invalid("CANONICALIZATION");
    }
    if (!descriptor?.enumerable || !("value" in descriptor)) {
      throw invalid("CANONICALIZATION");
    }
    Object.defineProperty(snapshot, field, {
      enumerable: true,
      value: descriptor.value,
    });
  }
  return snapshot;
}

function validateProfileData(profile, now) {
  if (profile.schema_version !== 1) {
    throw invalid("SCHEMA_VERSION");
  }
  if (profile.environment !== "SIMULATOR" && profile.environment !== "EVTE") {
    throw invalid("ENVIRONMENT");
  }
  if (profile.target !== "darwin/arm64") {
    throw invalid("TARGET");
  }
  if (typeof profile.helper_sha256 !== "string" || !LOWERCASE_SHA256.test(profile.helper_sha256)) {
    throw invalid("HELPER_HASH");
  }
  const crossHashFields = [
    "component_manifest_sha256",
    "registration_manifest_sha256",
    "endpoint_profile_sha256",
  ];
  for (const field of crossHashFields) {
    const expected =
      profile.environment === "SIMULATOR"
        ? profile[field] === "NONE"
        : typeof profile[field] === "string" && LOWERCASE_SHA256.test(profile[field]);
    if (!expected) {
      throw invalid("CROSS_HASH");
    }
  }
  const issuedAt = parseTimestamp(profile.issued_at, "ISSUED_AT");
  const expiresAt = parseTimestamp(profile.expires_at, "EXPIRES_AT");
  const nowMilliseconds = now instanceof Date ? now.getTime() : Number.NaN;
  if (!Number.isFinite(nowMilliseconds)) {
    throw invalid("CLOCK");
  }
  if (issuedAt > nowMilliseconds) {
    throw invalid("NOT_YET_VALID");
  }
  if (expiresAt <= nowMilliseconds) {
    throw invalid("EXPIRED");
  }
  if (expiresAt <= issuedAt) {
    throw invalid("VALIDITY_WINDOW");
  }
}

export function parseAndValidateSbrProfile(rawProfile, { now = new Date() } = {}) {
  const raw = decodeProfile(rawProfile);
  assertJsonStructure(raw);
  let profile;
  try {
    profile = JSON.parse(raw);
  } catch {
    throw invalid("JSON");
  }
  validateProfileData(dataOnlyProfileSnapshot(profile), now);
  return Object.freeze(profile);
}

export function canonicalizeSbrProfile(profile, { now = new Date() } = {}) {
  try {
    const snapshot = dataOnlyProfileSnapshot(profile);
    validateProfileData(snapshot, now);
    const canonical = canonicalize(snapshot);
    if (typeof canonical !== "string") {
      throw invalid("CANONICALIZATION");
    }
    return Buffer.from(canonical, "utf8");
  } catch {
    throw invalid("CANONICALIZATION");
  }
}

function decodePublicKey(publicKey) {
  if (typeof publicKey !== "string" && !(publicKey instanceof Uint8Array)) {
    throw invalid("PUBLIC_KEY_INPUT");
  }
  const bytes = typeof publicKey === "string" ? Buffer.from(publicKey) : Buffer.from(publicKey);
  if (bytes.byteLength > MAX_PUBLIC_KEY_BYTES) {
    throw invalid("PUBLIC_KEY_TOO_LARGE");
  }
  let pem;
  try {
    pem = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw invalid("PUBLIC_KEY_FORMAT");
  }
  if (!CANONICAL_PUBLIC_KEY_PEM.test(pem)) {
    throw invalid("PUBLIC_KEY_FORMAT");
  }
  try {
    const key = createPublicKey(pem);
    if (key.asymmetricKeyType !== "ed25519") {
      throw invalid("PUBLIC_KEY_TYPE");
    }
    if (key.export({ format: "pem", type: "spki" }) !== pem) {
      throw invalid("PUBLIC_KEY_FORMAT");
    }
    return key;
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("SBR_PROFILE_INVALID:")) {
      throw error;
    }
    throw invalid("PUBLIC_KEY_FORMAT");
  }
}

function decodeSignature(signature) {
  if (typeof signature !== "string" && !(signature instanceof Uint8Array)) {
    throw invalid("SIGNATURE_ENCODING");
  }
  const bytes = typeof signature === "string" ? Buffer.from(signature) : Buffer.from(signature);
  if (bytes.byteLength > MAX_SIGNATURE_BYTES) {
    throw invalid("SIGNATURE_TOO_LARGE");
  }
  if (bytes.some((byte) => byte > 0x7f)) {
    throw invalid("SIGNATURE_ENCODING");
  }
  const encoded = bytes.toString("ascii");
  // The sole on-disk encoding is 64 raw Ed25519 bytes as canonical base64 plus one LF.
  if (!STRICT_BASE64_SIGNATURE.test(encoded)) {
    throw invalid("SIGNATURE_ENCODING");
  }
  const decoded = Buffer.from(encoded.slice(0, -1), "base64");
  if (decoded.byteLength !== 64 || `${decoded.toString("base64")}\n` !== encoded) {
    throw invalid("SIGNATURE_ENCODING");
  }
  return decoded;
}

export function verifySbrProfileSignature({ now = new Date(), profile, publicKey, signature }) {
  const verified = verify(
    null,
    canonicalizeSbrProfile(profile, { now }),
    decodePublicKey(publicKey),
    decodeSignature(signature),
  );
  if (!verified) {
    throw invalid("SIGNATURE_MISMATCH");
  }
  return true;
}

export function authenticateSbrProfileBytes({
  now = new Date(),
  profileBytes,
  publicKey,
  signatureBytes,
}) {
  const profile = parseAndValidateSbrProfile(profileBytes, { now });
  verifySbrProfileSignature({ now, profile, publicKey, signature: signatureBytes });
  return profile;
}

async function readOwnedRegularFile(filePath, maximumBytes, dependencies) {
  const noFollowFlag = dependencies.noFollowFlag ?? constants.O_NOFOLLOW;
  if (
    !Number.isSafeInteger(noFollowFlag) ||
    noFollowFlag <= 0 ||
    noFollowFlag > 0x7fff_ffff ||
    noFollowFlag !== constants.O_NOFOLLOW
  ) {
    throw invalid("NOFOLLOW_UNAVAILABLE");
  }
  const nonBlockFlag = dependencies.nonBlockFlag ?? constants.O_NONBLOCK;
  if (
    !Number.isSafeInteger(nonBlockFlag) ||
    nonBlockFlag <= 0 ||
    nonBlockFlag > 0x7fff_ffff ||
    nonBlockFlag !== constants.O_NONBLOCK
  ) {
    throw invalid("NONBLOCK_UNAVAILABLE");
  }
  let handle;
  try {
    handle = await dependencies.open(filePath, constants.O_RDONLY | noFollowFlag | nonBlockFlag);
  } catch {
    throw invalid("FILE_OPEN");
  }
  let bytes;
  let failure;
  try {
    let metadata;
    try {
      metadata = await handle.stat();
    } catch {
      throw invalid("FILE_STAT");
    }
    if (!metadata.isFile()) {
      throw invalid("FILE_NOT_REGULAR");
    }
    if (!Number.isSafeInteger(metadata.size) || metadata.size < 0) {
      throw invalid("FILE_STAT");
    }
    if (!Number.isSafeInteger(metadata.mode)) {
      throw invalid("FILE_STAT");
    }
    if (typeof dependencies.getuid !== "function") {
      throw invalid("FILE_OWNER_UNAVAILABLE");
    }
    let currentUser;
    try {
      currentUser = dependencies.getuid();
    } catch {
      throw invalid("FILE_OWNER_UNAVAILABLE");
    }
    if (!Number.isSafeInteger(currentUser) || metadata.uid !== currentUser) {
      throw invalid("FILE_OWNER");
    }
    if ((metadata.mode & 0o022) !== 0) {
      throw invalid("FILE_MODE");
    }
    if (metadata.size > maximumBytes) {
      throw invalid("FILE_TOO_LARGE");
    }
    const buffer = Buffer.alloc(maximumBytes + 1);
    let total = 0;
    while (total < buffer.byteLength) {
      const requested = buffer.byteLength - total;
      let result;
      try {
        result = await handle.read(buffer, total, requested, null);
      } catch {
        throw invalid("FILE_READ");
      }
      if (
        !Number.isSafeInteger(result?.bytesRead) ||
        result.bytesRead < 0 ||
        result.bytesRead > requested
      ) {
        throw invalid("FILE_READ");
      }
      if (result.bytesRead === 0) {
        break;
      }
      total += result.bytesRead;
    }
    if (total > maximumBytes) {
      throw invalid("FILE_TOO_LARGE");
    }
    bytes = buffer.subarray(0, total);
  } catch (error) {
    failure =
      error instanceof Error && error.message.startsWith("SBR_PROFILE_INVALID:")
        ? error
        : invalid("FILE_READ");
  }
  try {
    await handle.close();
  } catch {
    failure ??= invalid("FILE_CLOSE");
  }
  if (failure) {
    throw failure;
  }
  return bytes;
}

export async function loadAuthenticatedSbrProfile({
  dependencies = {},
  now = new Date(),
  profilePath,
  publicKey,
  signaturePath,
}) {
  const filesystem = {
    getuid: typeof process.getuid === "function" ? () => process.getuid() : undefined,
    noFollowFlag: constants.O_NOFOLLOW,
    nonBlockFlag: constants.O_NONBLOCK,
    open: openFile,
    ...dependencies,
  };
  const profileBytes = await readOwnedRegularFile(profilePath, MAX_PROFILE_BYTES, filesystem);
  const signatureBytes = await readOwnedRegularFile(signaturePath, MAX_SIGNATURE_BYTES, filesystem);
  return authenticateSbrProfileBytes({ now, profileBytes, publicKey, signatureBytes });
}
