import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createPrivateKey, createPublicKey, generateKeyPairSync, sign } from "node:crypto";
import { constants } from "node:fs";
import { chmod, mkdir, mkdtemp, open, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  authenticateSbrProfileBytes,
  canonicalizeSbrProfile,
  loadAuthenticatedSbrProfile,
  MAX_PROFILE_BYTES,
  parseAndValidateSbrProfile,
  verifySbrProfileSignature,
} from "./sbr-profile-schema.mjs";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const fixtureRoot = path.join(repositoryRoot, "test", "fixtures", "sbr");
const fixtureProfilePath = path.join(fixtureRoot, "sbr-profile-v1.example.json");
const fixtureSignaturePath = path.join(fixtureRoot, "sbr-profile-v1.example.sig");
// This repository key authenticates documentation fixtures only; runtime trust is core-owned.
const fixturePublicKeyPath = path.join(
  repositoryRoot,
  "config",
  "sbr",
  "simulator",
  "profile-public-key.pem",
);
const TEST_SEED = Buffer.from(
  "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60",
  "hex",
);
const TEST_PRIVATE_KEY = createPrivateKey({
  format: "der",
  key: Buffer.concat([Buffer.from("302e020100300506032b657004220420", "hex"), TEST_SEED]),
  type: "pkcs8",
});
const TEST_PUBLIC_KEY = createPublicKey(TEST_PRIVATE_KEY).export({ format: "pem", type: "spki" });
const NOW = new Date("2026-08-21T12:00:00Z");
const MAX_STRUCTURAL_NESTING = 32;

function simulatorProfile(overrides = {}) {
  return {
    schema_version: 1,
    environment: "SIMULATOR",
    target: "darwin/arm64",
    helper_sha256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    component_manifest_sha256: "NONE",
    registration_manifest_sha256: "NONE",
    endpoint_profile_sha256: "NONE",
    issued_at: "2026-01-01T00:00:00Z",
    expires_at: "2099-12-31T23:59:59Z",
    ...overrides,
  };
}

function encode(profile) {
  return Buffer.from(JSON.stringify(profile));
}

function signatureFor(profile) {
  return `${sign(null, canonicalizeSbrProfile(profile), TEST_PRIVATE_KEY).toString("base64")}\n`;
}

test("parses the exact simulator schema and emits RFC 8785 canonical bytes", () => {
  const profile = parseAndValidateSbrProfile(encode(simulatorProfile()), { now: NOW });

  assert.deepEqual(profile, simulatorProfile());
  assert.equal(
    canonicalizeSbrProfile(profile).toString(),
    '{"component_manifest_sha256":"NONE","endpoint_profile_sha256":"NONE","environment":"SIMULATOR","expires_at":"2099-12-31T23:59:59Z","helper_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","issued_at":"2026-01-01T00:00:00Z","registration_manifest_sha256":"NONE","schema_version":1,"target":"darwin/arm64"}',
  );
});

test("accepts the exact EVTE cross-hash shape", () => {
  const hash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789";
  const profile = parseAndValidateSbrProfile(
    encode(
      simulatorProfile({
        component_manifest_sha256: hash,
        endpoint_profile_sha256: hash,
        environment: "EVTE",
        registration_manifest_sha256: hash,
      }),
    ),
    { now: NOW },
  );
  assert.equal(profile.environment, "EVTE");
});

for (const [name, mutate] of [
  ["unknown field", (profile) => ({ ...profile, extra: true })],
  ["missing field", ({ target: _target, ...profile }) => profile],
  ["wrong schema version", (profile) => ({ ...profile, schema_version: 2 })],
  ["wrong target", (profile) => ({ ...profile, target: "linux/arm64" })],
  ["uppercase hash", (profile) => ({ ...profile, helper_sha256: "A".repeat(64) })],
  ["short hash", (profile) => ({ ...profile, helper_sha256: "a".repeat(63) })],
  ["malformed hash", (profile) => ({ ...profile, helper_sha256: "z".repeat(64) })],
  ["non-string hash", (profile) => ({ ...profile, helper_sha256: ["a".repeat(64)] })],
  [
    "simulator component hash",
    (profile) => ({ ...profile, component_manifest_sha256: "a".repeat(64) }),
  ],
  [
    "simulator registration hash",
    (profile) => ({ ...profile, registration_manifest_sha256: "a".repeat(64) }),
  ],
  [
    "simulator endpoint hash",
    (profile) => ({ ...profile, endpoint_profile_sha256: "a".repeat(64) }),
  ],
  [
    "EVTE NONE component hash",
    (profile) => ({
      ...profile,
      environment: "EVTE",
      endpoint_profile_sha256: "a".repeat(64),
      registration_manifest_sha256: "a".repeat(64),
    }),
  ],
  [
    "EVTE NONE registration hash",
    (profile) => ({
      ...profile,
      component_manifest_sha256: "a".repeat(64),
      endpoint_profile_sha256: "a".repeat(64),
      environment: "EVTE",
    }),
  ],
  [
    "EVTE NONE endpoint hash",
    (profile) => ({
      ...profile,
      component_manifest_sha256: "a".repeat(64),
      environment: "EVTE",
      registration_manifest_sha256: "a".repeat(64),
    }),
  ],
  [
    "EVTE non-string cross hash",
    (profile) => ({
      ...profile,
      component_manifest_sha256: ["a".repeat(64)],
      endpoint_profile_sha256: "a".repeat(64),
      environment: "EVTE",
      registration_manifest_sha256: "a".repeat(64),
    }),
  ],
  [
    "forbidden environment",
    (profile) => ({ ...profile, environment: ["PRO", "DUCTION"].join("") }),
  ],
]) {
  test(`rejects ${name}`, () => {
    assert.throws(
      () => parseAndValidateSbrProfile(encode(mutate(simulatorProfile())), { now: NOW }),
      /SBR_PROFILE_INVALID/,
    );
  });
}

test("rejects duplicate and escaped-alias keys before JSON parsing erases them", () => {
  const raw = JSON.stringify(simulatorProfile());
  const duplicates = [
    raw.replace('{"schema_version":1', '{"schema_version":1,"schema_version":1'),
    raw.replace(
      '"environment":"SIMULATOR"',
      '"environment":"SIMULATOR","environ\\u006dent":"SIMULATOR"',
    ),
  ];

  for (const duplicate of duplicates) {
    assert.throws(
      () => parseAndValidateSbrProfile(duplicate, { now: NOW }),
      /SBR_PROFILE_INVALID:DUPLICATE_KEY/,
    );
  }
});

test("rejects duplicate keys nested inside an otherwise unknown field", () => {
  const raw = JSON.stringify(simulatorProfile()).replace(
    "{",
    '{"extra":{"outer":{"member":1,"\\u006dember":2}},',
  );

  assert.throws(
    () => parseAndValidateSbrProfile(raw, { now: NOW }),
    /SBR_PROFILE_INVALID:DUPLICATE_KEY/,
  );
});

function nestedArrays(count) {
  let value = null;
  for (let index = 0; index < count; index += 1) {
    value = [value];
  }
  return value;
}

function nestedObjects(count) {
  let value = null;
  for (let index = 0; index < count; index += 1) {
    value = { value };
  }
  return value;
}

for (const [name, makeNested] of [
  ["array", nestedArrays],
  ["object", nestedObjects],
]) {
  test(`permits ${name} structure at the nesting boundary before schema rejection`, () => {
    const raw = JSON.stringify({
      ...simulatorProfile(),
      extra: makeNested(MAX_STRUCTURAL_NESTING - 1),
    });

    assert.throws(
      () => parseAndValidateSbrProfile(raw, { now: NOW }),
      (error) => error?.message === "SBR_PROFILE_INVALID:FIELDS",
    );
  });

  test(`rejects ${name} structure over the nesting boundary deterministically`, () => {
    const raw = JSON.stringify({
      ...simulatorProfile(),
      extra: makeNested(MAX_STRUCTURAL_NESTING),
    });

    assert.throws(
      () => parseAndValidateSbrProfile(raw, { now: NOW }),
      (error) => error?.message === "SBR_PROFILE_INVALID:JSON_DEPTH",
    );
  });
}

for (const [name, nestedValue] of [
  ["array", `${"[".repeat(2_000)}null${"]".repeat(2_000)}`],
  ["object", `${'{"value":'.repeat(2_000)}null${"}".repeat(2_000)}`],
]) {
  test(`maps deeply nested ${name} input to the bounded schema error`, () => {
    const raw = `{"extra":${nestedValue}}`;
    assert.ok(Buffer.byteLength(raw) < MAX_PROFILE_BYTES);
    assert.throws(
      () => parseAndValidateSbrProfile(raw, { now: NOW }),
      (error) => error?.message === "SBR_PROFILE_INVALID:JSON_DEPTH",
    );
  });
}

for (const [name, raw] of [
  ["non-object root", "[]"],
  ["trailing data", `${JSON.stringify(simulatorProfile())} true`],
  ["invalid UTF-8", Buffer.from([0xc3, 0x28])],
  ["lone surrogate", JSON.stringify({ ...simulatorProfile(), environment: "\ud800" })],
]) {
  test(`rejects ${name}`, () => {
    assert.throws(() => parseAndValidateSbrProfile(raw, { now: NOW }), /SBR_PROFILE_INVALID/);
  });
}

for (const [name, overrides] of [
  ["non-canonical offset", { issued_at: "2026-01-01T11:00:00+11:00" }],
  ["fractional timestamp", { issued_at: "2026-01-01T00:00:00.000Z" }],
  ["invalid calendar timestamp", { issued_at: "2026-02-30T00:00:00Z" }],
  ["not-yet-valid profile", { issued_at: "2027-01-01T00:00:00Z" }],
  ["expired profile", { expires_at: "2026-08-21T11:59:59Z" }],
  [
    "non-increasing validity window",
    { expires_at: "2026-01-01T00:00:00Z", issued_at: "2026-01-01T00:00:00Z" },
  ],
]) {
  test(`rejects ${name}`, () => {
    assert.throws(
      () => parseAndValidateSbrProfile(encode(simulatorProfile(overrides)), { now: NOW }),
      /SBR_PROFILE_INVALID/,
    );
  });
}

test("bounds profile bytes before parsing", () => {
  assert.throws(
    () => parseAndValidateSbrProfile(Buffer.alloc(MAX_PROFILE_BYTES + 1, 0x20), { now: NOW }),
    /SBR_PROFILE_INVALID:PROFILE_TOO_LARGE/,
  );
});

test("bounds detached signature bytes before decoding", () => {
  assert.throws(
    () =>
      verifySbrProfileSignature({
        profile: simulatorProfile(),
        publicKey: TEST_PUBLIC_KEY,
        signature: "A".repeat(129),
      }),
    /SBR_PROFILE_INVALID:SIGNATURE_TOO_LARGE/,
  );
});

test("verifies only strict base64 detached Ed25519 signatures over canonical bytes", () => {
  const profile = parseAndValidateSbrProfile(encode(simulatorProfile()), { now: NOW });
  const signature = signatureFor(profile);
  const nonAsciiSignature = Buffer.from(signature);
  nonAsciiSignature[0] |= 0x80;

  assert.equal(verifySbrProfileSignature({ profile, publicKey: TEST_PUBLIC_KEY, signature }), true);
  for (const malformed of [`${signature}\n`, "not-base64", Buffer.alloc(64), nonAsciiSignature]) {
    assert.throws(
      () =>
        verifySbrProfileSignature({ profile, publicKey: TEST_PUBLIC_KEY, signature: malformed }),
      /SBR_PROFILE_INVALID:SIGNATURE_ENCODING/,
    );
  }
});

test("rejects a wrong signature, wrong public key, and oversized trust input", () => {
  const profile = simulatorProfile();
  const signature = signatureFor(profile);
  const otherSeed = Buffer.alloc(32, 7);
  const otherPrivateKey = createPrivateKey({
    format: "der",
    key: Buffer.concat([Buffer.from("302e020100300506032b657004220420", "hex"), otherSeed]),
    type: "pkcs8",
  });
  const otherPublicKey = createPublicKey(otherPrivateKey).export({ format: "pem", type: "spki" });

  const wrongSignatureBytes = Buffer.from(signature.trimEnd(), "base64");
  wrongSignatureBytes[0] ^= 1;
  assert.throws(
    () =>
      verifySbrProfileSignature({
        profile,
        publicKey: TEST_PUBLIC_KEY,
        signature: `${wrongSignatureBytes.toString("base64")}\n`,
      }),
    /SBR_PROFILE_INVALID:SIGNATURE_MISMATCH/,
  );
  assert.throws(
    () => verifySbrProfileSignature({ profile, publicKey: otherPublicKey, signature }),
    /SBR_PROFILE_INVALID:SIGNATURE_MISMATCH/,
  );
  assert.throws(
    () => verifySbrProfileSignature({ profile, publicKey: "x".repeat(4_097), signature }),
    /SBR_PROFILE_INVALID:PUBLIC_KEY_TOO_LARGE/,
  );
});

test("accepts only one canonical Ed25519 SPKI public-key PEM", () => {
  const profile = simulatorProfile();
  const signature = signatureFor(profile);
  const privatePem = TEST_PRIVATE_KEY.export({ format: "pem", type: "pkcs8" });
  const { publicKey: wrongAlgorithmKey } = generateKeyPairSync("ec", { namedCurve: "P-256" });
  const wrongAlgorithmPem = wrongAlgorithmKey.export({ format: "pem", type: "spki" });
  const rejectedKeys = [
    privatePem,
    `${TEST_PUBLIC_KEY}${TEST_PUBLIC_KEY}`,
    ` ${TEST_PUBLIC_KEY}`,
    `${TEST_PUBLIC_KEY}garbage`,
    TEST_PUBLIC_KEY.slice(0, -1),
    TEST_PUBLIC_KEY.replace("BEGIN PUBLIC KEY", "BEGIN CERTIFICATE"),
    `-----BEGIN PUBLIC KEY-----\n${"A".repeat(60)}\n-----END PUBLIC KEY-----\n`,
    wrongAlgorithmPem,
  ];

  for (const publicKey of rejectedKeys) {
    assert.throws(
      () => verifySbrProfileSignature({ profile, publicKey, signature }),
      /SBR_PROFILE_INVALID:PUBLIC_KEY_(?:FORMAT|TYPE)/,
    );
  }
});

test("committed documentation fixture authenticates with its committed advisory test key", async () => {
  const [profileBytes, signatureBytes, publicKey] = await Promise.all([
    readFile(fixtureProfilePath),
    readFile(fixtureSignaturePath),
    readFile(fixturePublicKeyPath),
  ]);

  assert.deepEqual(publicKey, Buffer.from(TEST_PUBLIC_KEY));
  assert.deepEqual(
    authenticateSbrProfileBytes({
      now: NOW,
      profileBytes,
      publicKey,
      signatureBytes,
    }),
    simulatorProfile(),
  );
});

test("prototype toJSON pollution cannot authenticate a tampered profile", async () => {
  const [profileBytes, signatureBytes, publicKey] = await Promise.all([
    readFile(fixtureProfilePath),
    readFile(fixtureSignaturePath),
    readFile(fixturePublicKeyPath),
  ]);
  const tamperedBytes = Buffer.from(
    profileBytes
      .toString()
      .replace(
        simulatorProfile().helper_sha256,
        "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
      ),
  );
  const signedSnapshot = Object.assign(Object.create(null), simulatorProfile());
  const previous = Object.getOwnPropertyDescriptor(Object.prototype, "toJSON");
  try {
    Object.defineProperty(Object.prototype, "toJSON", {
      configurable: true,
      value: () => signedSnapshot,
    });
    assert.throws(
      () =>
        authenticateSbrProfileBytes({
          now: NOW,
          profileBytes: tamperedBytes,
          publicKey,
          signatureBytes,
        }),
      /SBR_PROFILE_INVALID:CANONICALIZATION/,
    );
  } finally {
    if (previous) {
      Object.defineProperty(Object.prototype, "toJSON", previous);
    } else {
      delete Object.prototype.toJSON;
    }
  }
});

test("canonicalization rejects accessors, toJSON hooks, and canonicalizer hazards deterministically", () => {
  const accessorProfile = simulatorProfile();
  let getterCalls = 0;
  Object.defineProperty(accessorProfile, "helper_sha256", {
    enumerable: true,
    get: () => {
      getterCalls += 1;
      throw new Error("getter detail");
    },
  });
  const ownToJsonProfile = { ...simulatorProfile(), toJSON: () => simulatorProfile() };
  const inheritedToJsonProfile = Object.assign(
    Object.create({ toJSON: () => simulatorProfile() }),
    simulatorProfile(),
  );
  const canonicalizerHazard = { ...simulatorProfile(), schema_version: 1n };

  for (const profile of [
    accessorProfile,
    ownToJsonProfile,
    inheritedToJsonProfile,
    canonicalizerHazard,
  ]) {
    assert.throws(
      () => canonicalizeSbrProfile(profile, { now: NOW }),
      /SBR_PROFILE_INVALID:CANONICALIZATION/,
    );
  }
  assert.equal(getterCalls, 0);
});

test("loads owned private regular profile inputs using no-follow opens", async (context) => {
  if (typeof process.getuid !== "function") {
    context.skip("POSIX ownership and mode checks are unavailable");
    return;
  }
  const directory = await mkdtemp(path.join(tmpdir(), "tammy-sbr-profile-"));
  context.after(() => rm(directory, { force: true, recursive: true }));
  const profilePath = path.join(directory, "profile.json");
  const signaturePath = path.join(directory, "profile.sig");
  const profile = simulatorProfile();
  await writeFile(profilePath, encode(profile), { mode: 0o600 });
  await writeFile(signaturePath, signatureFor(profile), { mode: 0o600 });

  assert.deepEqual(
    await loadAuthenticatedSbrProfile({
      now: NOW,
      profilePath,
      publicKey: TEST_PUBLIC_KEY,
      signaturePath,
    }),
    profile,
  );
});

for (const input of ["profile", "signature"]) {
  test(`rejects a symlink ${input} file`, async (context) => {
    const directory = await mkdtemp(path.join(tmpdir(), "tammy-sbr-symlink-"));
    context.after(() => rm(directory, { force: true, recursive: true }));
    const realProfile = path.join(directory, "real-profile.json");
    const realSignature = path.join(directory, "real-profile.sig");
    const linkedProfile = path.join(directory, "linked-profile.json");
    const linkedSignature = path.join(directory, "linked-profile.sig");
    const profile = simulatorProfile();
    await writeFile(realProfile, encode(profile), { mode: 0o600 });
    await writeFile(realSignature, signatureFor(profile), { mode: 0o600 });
    await symlink(realProfile, linkedProfile);
    await symlink(realSignature, linkedSignature);

    await assert.rejects(
      loadAuthenticatedSbrProfile({
        now: NOW,
        profilePath: input === "profile" ? linkedProfile : realProfile,
        publicKey: TEST_PUBLIC_KEY,
        signaturePath: input === "signature" ? linkedSignature : realSignature,
      }),
      /SBR_PROFILE_INVALID:FILE_OPEN/,
    );
  });
}

for (const input of ["profile", "signature"]) {
  test(`rejects a group/world-writable ${input} file`, async (context) => {
    if (typeof process.getuid !== "function") {
      context.skip("POSIX ownership and mode checks are unavailable");
      return;
    }
    const directory = await mkdtemp(path.join(tmpdir(), "tammy-sbr-mode-"));
    context.after(() => rm(directory, { force: true, recursive: true }));
    const profilePath = path.join(directory, "profile.json");
    const signaturePath = path.join(directory, "profile.sig");
    const profile = simulatorProfile();
    await writeFile(profilePath, encode(profile), { mode: 0o600 });
    await writeFile(signaturePath, signatureFor(profile), { mode: 0o600 });
    await chmod(input === "profile" ? profilePath : signaturePath, 0o622);

    await assert.rejects(
      loadAuthenticatedSbrProfile({
        now: NOW,
        profilePath,
        publicKey: TEST_PUBLIC_KEY,
        signaturePath,
      }),
      /SBR_PROFILE_INVALID:FILE_MODE/,
    );
  });
}

test("rejects non-regular input files", async (context) => {
  const directory = await mkdtemp(path.join(tmpdir(), "tammy-sbr-regular-"));
  context.after(() => rm(directory, { force: true, recursive: true }));
  const profileDirectory = path.join(directory, "profile-directory");
  const signaturePath = path.join(directory, "profile.sig");
  await mkdir(profileDirectory);
  await writeFile(signaturePath, "A".repeat(88), { mode: 0o600 });

  await assert.rejects(
    loadAuthenticatedSbrProfile({
      now: NOW,
      profilePath: profileDirectory,
      publicKey: TEST_PUBLIC_KEY,
      signaturePath,
    }),
    /SBR_PROFILE_INVALID:FILE_NOT_REGULAR/,
  );
});

test("rejects an oversized profile file before parsing", async (context) => {
  const directory = await mkdtemp(path.join(tmpdir(), "tammy-sbr-size-"));
  context.after(() => rm(directory, { force: true, recursive: true }));
  const profilePath = path.join(directory, "profile.json");
  const signaturePath = path.join(directory, "profile.sig");
  await writeFile(profilePath, Buffer.alloc(MAX_PROFILE_BYTES + 1, 0x20), { mode: 0o600 });
  await writeFile(signaturePath, `${"A".repeat(86)}==\n`, { mode: 0o600 });

  await assert.rejects(
    loadAuthenticatedSbrProfile({
      now: NOW,
      profilePath,
      publicKey: TEST_PUBLIC_KEY,
      signaturePath,
    }),
    /SBR_PROFILE_INVALID:FILE_TOO_LARGE/,
  );
});

test("validates file ownership through injectable open handles", async () => {
  const handle = {
    close: async () => {},
    readFile: async () => encode(simulatorProfile()),
    stat: async () => ({ isFile: () => true, mode: 0o100600, size: 100, uid: 501 }),
  };

  await assert.rejects(
    loadAuthenticatedSbrProfile({
      dependencies: { getuid: () => 502, open: async () => handle },
      now: NOW,
      profilePath: "profile",
      publicKey: TEST_PUBLIC_KEY,
      signaturePath: "signature",
    }),
    /SBR_PROFILE_INVALID:FILE_OWNER/,
  );
});

test("fails closed before opening when no-follow semantics are unavailable or invalid", async () => {
  for (const noFollowFlag of [0, -1, 1.5, 2 ** 40, "nofollow"]) {
    let openCalls = 0;
    await assert.rejects(
      loadAuthenticatedSbrProfile({
        dependencies: {
          noFollowFlag,
          open: async () => {
            openCalls += 1;
            throw new Error("must not open");
          },
        },
        now: NOW,
        profilePath: "profile",
        publicKey: TEST_PUBLIC_KEY,
        signaturePath: "signature",
      }),
      /SBR_PROFILE_INVALID:NOFOLLOW_UNAVAILABLE/,
    );
    assert.equal(openCalls, 0);
  }
});

test("fails closed before opening when nonblocking semantics are unavailable or invalid", async () => {
  for (const nonBlockFlag of [0, -1, 1.5, 2 ** 40, "nonblock"]) {
    let openCalls = 0;
    await assert.rejects(
      loadAuthenticatedSbrProfile({
        dependencies: {
          nonBlockFlag,
          open: async () => {
            openCalls += 1;
            throw new Error("must not open");
          },
        },
        now: NOW,
        profilePath: "profile",
        publicKey: TEST_PUBLIC_KEY,
        signaturePath: "signature",
      }),
      /SBR_PROFILE_INVALID:NONBLOCK_UNAVAILABLE/,
    );
    assert.equal(openCalls, 0);
  }
});

test("cannot substitute the secure-open flags", async (context) => {
  if (
    !Number.isSafeInteger(constants.O_NOFOLLOW) ||
    !Number.isSafeInteger(constants.O_NONBLOCK) ||
    constants.O_NOFOLLOW === constants.O_NONBLOCK
  ) {
    context.skip("distinct POSIX secure-open flags are unavailable");
    return;
  }
  const directory = await mkdtemp(path.join(tmpdir(), "tammy-sbr-flag-substitution-"));
  context.after(() => rm(directory, { force: true, recursive: true }));
  const realProfilePath = path.join(directory, "profile.json");
  const linkedProfilePath = path.join(directory, "linked-profile.json");
  const signaturePath = path.join(directory, "profile.sig");
  const profile = simulatorProfile();
  await writeFile(realProfilePath, encode(profile), { mode: 0o600 });
  await writeFile(signaturePath, signatureFor(profile), { mode: 0o600 });
  await symlink(realProfilePath, linkedProfilePath);
  let openCalls = 0;
  const countedOpen = async (...arguments_) => {
    openCalls += 1;
    return open(...arguments_);
  };

  await assert.rejects(
    loadAuthenticatedSbrProfile({
      dependencies: {
        noFollowFlag: constants.O_NONBLOCK,
        open: countedOpen,
      },
      now: NOW,
      profilePath: linkedProfilePath,
      publicKey: TEST_PUBLIC_KEY,
      signaturePath,
    }),
    /SBR_PROFILE_INVALID:NOFOLLOW_UNAVAILABLE/,
  );
  assert.equal(openCalls, 0);

  await assert.rejects(
    loadAuthenticatedSbrProfile({
      dependencies: {
        nonBlockFlag: constants.O_NOFOLLOW,
        open: countedOpen,
      },
      now: NOW,
      profilePath: realProfilePath,
      publicKey: TEST_PUBLIC_KEY,
      signaturePath,
    }),
    /SBR_PROFILE_INVALID:NONBLOCK_UNAVAILABLE/,
  );
  assert.equal(openCalls, 0);
});

function memoryHandle(
  bytes,
  { claimedSize = bytes.byteLength, onClose = () => {}, uid = 501 } = {},
) {
  let position = 0;
  return {
    close: async () => onClose(),
    read: async (buffer, offset, length) => {
      const bytesRead = Math.min(length, bytes.byteLength - position);
      bytes.copy(buffer, offset, position, position + bytesRead);
      position += bytesRead;
      return { buffer, bytesRead };
    },
    stat: async () => ({ isFile: () => true, mode: 0o100600, size: claimedSize, uid }),
  };
}

for (const failurePoint of ["open", "stat", "read"]) {
  test(`closes every opened descriptor when the signature ${failurePoint} fails`, async () => {
    let profileClosed = false;
    let signatureClosed = false;
    const profileHandle = memoryHandle(encode(simulatorProfile()), {
      onClose: () => {
        profileClosed = true;
      },
      uid: 501,
    });
    const signatureHandle = {
      close: async () => {
        signatureClosed = true;
      },
      read: async () => {
        if (failurePoint === "read") {
          throw new Error("signature read detail");
        }
        return { bytesRead: 0 };
      },
      stat: async () => {
        if (failurePoint === "stat") {
          throw new Error("signature stat detail");
        }
        return { isFile: () => true, mode: 0o100600, size: 1, uid: 501 };
      },
    };

    await assert.rejects(
      loadAuthenticatedSbrProfile({
        dependencies: {
          getuid: () => 501,
          open: async (filePath) => {
            if (filePath === "signature") {
              if (failurePoint === "open") {
                throw new Error("signature open detail");
              }
              return signatureHandle;
            }
            return profileHandle;
          },
        },
        now: NOW,
        profilePath: "profile",
        publicKey: TEST_PUBLIC_KEY,
        signaturePath: "signature",
      }),
      new RegExp(`SBR_PROFILE_INVALID:FILE_${failurePoint.toUpperCase()}`),
    );
    assert.equal(profileClosed, true);
    assert.equal(signatureClosed, failurePoint !== "open");
  });
}

test("bounds descriptor reads when a file grows after fstat", async () => {
  const profile = simulatorProfile();
  const files = new Map([
    ["profile", memoryHandle(Buffer.alloc(MAX_PROFILE_BYTES + 1, 0x20), { claimedSize: 1 })],
    ["signature", memoryHandle(Buffer.from(signatureFor(profile)))],
  ]);

  await assert.rejects(
    loadAuthenticatedSbrProfile({
      dependencies: {
        getuid: () => 501,
        open: async (filePath) => files.get(filePath),
      },
      now: NOW,
      profilePath: "profile",
      publicKey: TEST_PUBLIC_KEY,
      signaturePath: "signature",
    }),
    /SBR_PROFILE_INVALID:FILE_TOO_LARGE/,
  );
});

test("does not return while a concurrently opened signature handle remains pending", async () => {
  let releaseSignatureRead;
  const signatureRead = new Promise((resolve) => {
    releaseSignatureRead = resolve;
  });
  let signatureOpened = false;
  let signatureClosed = false;
  let resolveSignatureClosed;
  const signatureClosedPromise = new Promise((resolve) => {
    resolveSignatureClosed = resolve;
  });
  const profileHandle = {
    close: async () => {},
    stat: async () => ({ isFile: () => false, mode: 0o100600, size: 1, uid: 501 }),
  };
  const signatureHandle = {
    close: async () => {
      signatureClosed = true;
      resolveSignatureClosed();
    },
    read: async () => signatureRead,
    stat: async () => ({ isFile: () => true, mode: 0o100600, size: 1, uid: 501 }),
  };

  try {
    await assert.rejects(
      loadAuthenticatedSbrProfile({
        dependencies: {
          getuid: () => 501,
          open: async (filePath) => {
            if (filePath === "signature") {
              signatureOpened = true;
              return signatureHandle;
            }
            return profileHandle;
          },
        },
        now: NOW,
        profilePath: "profile",
        publicKey: TEST_PUBLIC_KEY,
        signaturePath: "signature",
      }),
      /SBR_PROFILE_INVALID:FILE_NOT_REGULAR/,
    );
    assert.equal(signatureOpened && !signatureClosed, false);
  } finally {
    if (signatureOpened && !signatureClosed) {
      releaseSignatureRead({ bytesRead: 0 });
      await signatureClosedPromise;
    }
  }
});

test("profile failure has fixed precedence and prevents a failing signature open", async () => {
  let signatureOpenCalls = 0;
  const profileHandle = {
    close: async () => {},
    stat: async () => {
      await Promise.resolve();
      return { isFile: () => false, mode: 0o100600, size: 1, uid: 501 };
    },
  };

  await assert.rejects(
    loadAuthenticatedSbrProfile({
      dependencies: {
        getuid: () => 501,
        open: async (filePath) => {
          if (filePath === "signature") {
            signatureOpenCalls += 1;
            throw new Error("signature open detail");
          }
          return profileHandle;
        },
      },
      now: NOW,
      profilePath: "profile",
      publicKey: TEST_PUBLIC_KEY,
      signaturePath: "signature",
    }),
    /SBR_PROFILE_INVALID:FILE_NOT_REGULAR/,
  );
  assert.equal(signatureOpenCalls, 0);
});

for (const failurePoint of ["stat", "read"]) {
  test(`closes the profile descriptor after a ${failurePoint} failure`, async () => {
    let closed = false;
    const handle = {
      close: async () => {
        closed = true;
      },
      read: async () => {
        throw new Error("read detail");
      },
      stat: async () => {
        if (failurePoint === "stat") {
          throw new Error("stat detail");
        }
        return { isFile: () => true, mode: 0o100600, size: 1, uid: 501 };
      },
    };

    await assert.rejects(
      loadAuthenticatedSbrProfile({
        dependencies: { getuid: () => 501, open: async () => handle },
        now: NOW,
        profilePath: "profile",
        publicKey: TEST_PUBLIC_KEY,
        signaturePath: "signature",
      }),
      new RegExp(`SBR_PROFILE_INVALID:FILE_${failurePoint.toUpperCase()}`),
    );
    assert.equal(closed, true);
  });
}

test("rejects a FIFO without blocking on supported POSIX hosts", {
  skip: process.platform === "win32",
  timeout: 3_000,
}, async (context) => {
  const directory = await mkdtemp(path.join(tmpdir(), "tammy-sbr-fifo-"));
  context.after(() => rm(directory, { force: true, recursive: true }));
  const fifoPath = path.join(directory, "profile.fifo");
  const mkfifo = spawnSync("mkfifo", [fifoPath]);
  if (mkfifo.error?.code === "ENOENT") {
    context.skip("mkfifo is unavailable");
    return;
  }
  assert.equal(mkfifo.status, 0);
  const moduleUrl = new URL("./sbr-profile-schema.mjs", import.meta.url).href;
  const childSource = `
      import { readFile } from "node:fs/promises";
      import { loadAuthenticatedSbrProfile } from ${JSON.stringify(moduleUrl)};
      const publicKey = await readFile(${JSON.stringify(fixturePublicKeyPath)});
      try {
        await loadAuthenticatedSbrProfile({
          now: new Date(${JSON.stringify(NOW.toISOString())}),
          profilePath: process.argv[1],
          publicKey,
          signaturePath: ${JSON.stringify(fixtureSignaturePath)},
        });
      } catch (error) {
        process.stdout.write(error.message + "\\n");
      }
    `;
  const child = spawnSync(
    process.execPath,
    ["--input-type=module", "--eval", childSource, fifoPath],
    { encoding: "utf8", timeout: 750 },
  );

  assert.equal(child.error, undefined);
  assert.equal(child.status, 0);
  assert.equal(child.stdout, "SBR_PROFILE_INVALID:FILE_NOT_REGULAR\n");
});
