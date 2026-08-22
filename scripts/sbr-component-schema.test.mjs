import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { constants } from "node:fs";
import {
  link,
  lstat,
  mkdir,
  mkdtemp,
  open,
  readdir,
  readFile,
  rename,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import {
  MAX_COMPONENT_BUNDLE_BYTES,
  MAX_COMPONENT_DEPTH,
  MAX_COMPONENT_ENTRIES,
  MAX_COMPONENT_FILE_BYTES,
  MAX_COMPONENT_FILES,
  MAX_COMPONENT_MANIFEST_BYTES,
  parseAndValidateSbrComponentManifest,
  verifySbrComponentBundle as verifySbrComponentBundleRaw,
  verifySbrComponentCrossHashes,
} from "./sbr-component-schema.mjs";

const VALID_FILES = new Map([
  ["bin/helper", Buffer.from("helper fixture\n")],
  ["lib/config.json", Buffer.from('{"enabled":true}\n')],
]);

function sha256(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function manifestFor(files = VALID_FILES, overrides = {}) {
  return {
    schema_version: 1,
    component_name: "tammy-sbr-helper",
    component_version: "0.1.0-fixture",
    target: "darwin/arm64",
    files: [...files].map(([filePath, bytes]) => ({
      path: filePath,
      byte_length: bytes.byteLength,
      sha256: sha256(bytes),
    })),
    ...overrides,
  };
}

function encode(manifest) {
  return Buffer.from(JSON.stringify(manifest));
}

function manifestWithCanonicalSize(targetBytes) {
  const files = Array.from({ length: MAX_COMPONENT_FILES }, (_, index) => ({
    path: `file-${String(index).padStart(4, "0")}-`,
    byte_length: 0,
    sha256: sha256(Buffer.alloc(0)),
  }));
  const manifest = manifestFor(VALID_FILES, { files });
  let remaining = targetBytes - Buffer.byteLength(JSON.stringify(manifest));
  assert.ok(remaining >= 0);
  for (let index = 0; index < files.length && remaining > 0; index += 1) {
    const capacity = 1_024 - Buffer.byteLength(files[index].path);
    const addition = Math.min(capacity, remaining);
    files[index].path += "a".repeat(addition);
    remaining -= addition;
  }
  assert.equal(remaining, 0);
  assert.equal(Buffer.byteLength(JSON.stringify(manifest)), targetBytes);
  return manifest;
}

async function makeBundle(context, files = VALID_FILES) {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-sbr-component-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  for (const [relativePath, bytes] of files) {
    const destination = path.join(root, ...relativePath.split("/"));
    await mkdir(path.dirname(destination), { recursive: true });
    await writeFile(destination, bytes, { mode: 0o600 });
  }
  return root;
}

function verifySbrComponentBundle({ dependencies = {}, ...input }) {
  const pathsByHandle = new Map();
  const baseOpen = dependencies.open ?? open;
  const baseLstat = dependencies.lstat ?? lstat;
  const baseReaddir = dependencies.readdir ?? readdir;
  const rootOpen = async (entryPath, flags) => {
    const handle = await baseOpen(entryPath, flags);
    pathsByHandle.set(handle, entryPath);
    return handle;
  };
  const anchored = {
    lstat: async (parentHandle, childName, options) =>
      baseLstat(path.join(pathsByHandle.get(parentHandle), childName), options),
    open: async (parentHandle, childName, flags) => {
      const parentPath = pathsByHandle.get(parentHandle);
      dependencies.onOpenAt?.(parentPath, childName);
      const childPath = path.join(parentPath, childName);
      const handle = await baseOpen(childPath, flags);
      pathsByHandle.set(handle, childPath);
      return handle;
    },
    readdir: async (directoryHandle) =>
      baseReaddir(pathsByHandle.get(directoryHandle), { withFileTypes: true }),
  };
  return verifySbrComponentBundleRaw({
    ...input,
    dependencies: { ...dependencies, anchored, lstat: baseLstat, open: rootOpen },
  });
}

test("parses a deterministic nested manifest and exposes RFC 8785 bytes and hash", () => {
  const parsed = parseAndValidateSbrComponentManifest(encode(manifestFor()));

  assert.deepEqual(parsed.manifest, manifestFor());
  assert.equal(
    parsed.canonicalBytes.toString(),
    `{"component_name":"tammy-sbr-helper","component_version":"0.1.0-fixture","files":[{"byte_length":15,"path":"bin/helper","sha256":"${sha256(VALID_FILES.get("bin/helper"))}"},{"byte_length":17,"path":"lib/config.json","sha256":"${sha256(VALID_FILES.get("lib/config.json"))}"}],"schema_version":1,"target":"darwin/arm64"}`,
  );
  assert.equal(parsed.sha256, sha256(parsed.canonicalBytes));
});

for (const [property, pollutedValue] of [
  ["toJSON", () => "polluted"],
  ["reduce", () => "polluted"],
]) {
  test(`canonicalization is independent of Array.prototype.${property} pollution`, () => {
    const raw = encode(manifestFor());
    const baseline = parseAndValidateSbrComponentManifest(raw);
    const previous = Object.getOwnPropertyDescriptor(Array.prototype, property);
    try {
      Object.defineProperty(Array.prototype, property, {
        configurable: true,
        value: pollutedValue,
        writable: true,
      });
      const parsed = parseAndValidateSbrComponentManifest(raw);
      assert.deepEqual(parsed.canonicalBytes, baseline.canonicalBytes);
      assert.equal(parsed.sha256, baseline.sha256);
    } finally {
      if (previous) {
        Object.defineProperty(Array.prototype, property, previous);
      } else {
        delete Array.prototype[property];
      }
    }
  });
}

test("maps hostile manifest accessors to canonicalization errors", () => {
  const hostile = manifestFor();
  Object.defineProperty(hostile.files, "0", {
    enumerable: true,
    get: () => {
      throw new Error("sensitive canonicalizer detail");
    },
  });
  assert.throws(
    () =>
      verifySbrComponentCrossHashes({
        componentManifest: hostile,
        registrationManifest: { component_manifest_sha256: "a".repeat(64) },
        profile: { component_manifest_sha256: "a".repeat(64) },
      }),
    (error) => error?.message === "SBR_COMPONENT_INVALID:CANONICALIZATION",
  );
});

test("rejects missing, unknown, duplicate, and escaped-alias keys", () => {
  const valid = JSON.stringify(manifestFor());
  const missing = manifestFor();
  delete missing.component_name;
  const invalidInputs = [
    encode(missing),
    encode({ ...manifestFor(), extra: true }),
    valid.replace('{"schema_version":1', '{"schema_version":1,"schema_version":1'),
    valid.replace(
      '"target":"darwin/arm64"',
      '"target":"darwin/arm64","tar\\u0067et":"darwin/arm64"',
    ),
    valid.replace('"path":"bin/helper"', '"path":"bin/helper","pa\\u0074h":"bin/helper"'),
  ];

  for (const raw of invalidInputs) {
    assert.throws(
      () => parseAndValidateSbrComponentManifest(raw),
      /SBR_COMPONENT_INVALID:(?:FIELDS|FILE_FIELDS|DUPLICATE_KEY)/,
    );
  }
});

test("rejects invalid root field types and bounded identifiers", () => {
  const mutations = [
    { schema_version: 2 },
    { schema_version: "1" },
    { target: "linux/arm64" },
    { component_name: "" },
    { component_name: "../helper" },
    { component_name: "a".repeat(65) },
    { component_version: "version with spaces" },
    { component_version: "v\\1" },
    { component_version: "bad/name" },
    { component_version: "a".repeat(65) },
    { target: ["darwin/arm64"] },
    { files: {} },
    { files: [] },
  ];
  for (const mutation of mutations) {
    assert.throws(
      () => parseAndValidateSbrComponentManifest(encode(manifestFor(VALID_FILES, mutation))),
      /SBR_COMPONENT_INVALID/,
    );
  }
});

test("requires strict sorted normalized collision-free POSIX paths", () => {
  const empty = Buffer.alloc(0);
  const invalidPaths = [
    "",
    "/absolute",
    "relative/",
    "./relative",
    "relative/./file",
    "relative/../file",
    "relative//file",
    "relative\\file",
    "relative/\u0000file",
    "relative/\u001ffile",
    ".",
    "..",
  ];
  for (const invalidPath of invalidPaths) {
    const files = new Map([[invalidPath, empty]]);
    assert.throws(
      () => parseAndValidateSbrComponentManifest(encode(manifestFor(files))),
      /SBR_COMPONENT_INVALID:FILE_PATH/,
    );
  }

  const unsorted = new Map([
    ["z", empty],
    ["a", empty],
  ]);
  assert.throws(
    () => parseAndValidateSbrComponentManifest(encode(manifestFor(unsorted))),
    /SBR_COMPONENT_INVALID:FILE_ORDER/,
  );

  const duplicate = manifestFor(new Map([["same", empty]]));
  duplicate.files.push({ ...duplicate.files[0] });
  assert.throws(
    () => parseAndValidateSbrComponentManifest(encode(duplicate)),
    /SBR_COMPONENT_INVALID:FILE_(?:ORDER|DUPLICATE)/,
  );

  const nfc = "caf\u00e9";
  const nfd = "cafe\u0301";
  for (const paths of [[nfd], [nfc, nfd]]) {
    const files = paths.map((filePath) => ({
      path: filePath,
      byte_length: 0,
      sha256: sha256(empty),
    }));
    assert.throws(
      () => parseAndValidateSbrComponentManifest(encode(manifestFor(VALID_FILES, { files }))),
      /SBR_COMPONENT_INVALID:FILE_(?:PATH|DUPLICATE|ORDER)/,
    );
  }
});

test("allows dot-prefixed component path segments", async (context) => {
  const files = new Map([
    [".hidden", Buffer.from("hidden")],
    [".well-known/config", Buffer.from("config")],
  ]);
  const manifest = parseAndValidateSbrComponentManifest(encode(manifestFor(files)));
  const root = await makeBundle(context, files);
  const verified = await verifySbrComponentBundle({ componentRoot: root, manifest });
  assert.deepEqual(verified.files, [...files.keys()]);
});

test("uses raw UTF-8 byte ordering instead of locale ordering", () => {
  const bytes = Buffer.from("x");
  const correctlySorted = new Map([
    ["z", bytes],
    ["\u00e4", bytes],
  ]);
  assert.doesNotThrow(() =>
    parseAndValidateSbrComponentManifest(encode(manifestFor(correctlySorted))),
  );
  assert.throws(
    () =>
      parseAndValidateSbrComponentManifest(
        encode(manifestFor(new Map([...correctlySorted].reverse()))),
      ),
    /SBR_COMPONENT_INVALID:FILE_ORDER/,
  );
});

test("rejects invalid lengths, hashes, file count, and aggregate size", () => {
  const valid = manifestFor();
  const invalidFiles = [
    [null],
    [{ path: valid.files[0].path, byte_length: valid.files[0].byte_length }],
    [{ ...valid.files[0], extra: true }],
    [{ ...valid.files[0], path: 7 }],
    [{ ...valid.files[0], byte_length: -1 }],
    [{ ...valid.files[0], byte_length: 1.5 }],
    [{ ...valid.files[0], byte_length: "15" }],
    [{ ...valid.files[0], byte_length: MAX_COMPONENT_FILE_BYTES + 1 }],
    [{ ...valid.files[0], sha256: 7 }],
    [{ ...valid.files[0], sha256: "A".repeat(64) }],
    [{ ...valid.files[0], sha256: "a".repeat(63) }],
    [{ ...valid.files[0], sha256: "z".repeat(64) }],
  ];
  for (const files of invalidFiles) {
    assert.throws(
      () => parseAndValidateSbrComponentManifest(encode({ ...valid, files })),
      /SBR_COMPONENT_INVALID/,
    );
  }

  const tooMany = Array.from({ length: MAX_COMPONENT_FILES + 1 }, (_, index) => ({
    path: `file-${String(index).padStart(4, "0")}`,
    byte_length: 0,
    sha256: sha256(Buffer.alloc(0)),
  }));
  assert.throws(
    () => parseAndValidateSbrComponentManifest(encode({ ...valid, files: tooMany })),
    /SBR_COMPONENT_INVALID:FILE_COUNT/,
  );

  const aggregate = Array.from({ length: 5 }, (_, index) => ({
    path: String.fromCharCode(97 + index),
    byte_length: MAX_COMPONENT_FILE_BYTES,
    sha256: "a".repeat(64),
  }));
  assert.ok(
    aggregate.reduce((sum, file) => sum + file.byte_length, 0) > MAX_COMPONENT_BUNDLE_BYTES,
  );
  assert.throws(
    () => parseAndValidateSbrComponentManifest(encode({ ...valid, files: aggregate })),
    /SBR_COMPONENT_INVALID:BUNDLE_SIZE/,
  );
});

test("the documentation manifest is schema-valid but deliberately cannot verify", async (context) => {
  const raw = await readFile(
    new URL("../docs/development/sbr-component-manifest.example.json", import.meta.url),
  );
  const parsed = parseAndValidateSbrComponentManifest(raw);
  assert.match(parsed.manifest.component_name, /PLACEHOLDER-NOT-RUNNABLE/);
  assert.equal(parsed.manifest.files[0].sha256, "0".repeat(64));
  const root = await makeBundle(context, new Map());
  await assert.rejects(
    verifySbrComponentBundle({ componentRoot: root, manifest: parsed }),
    /SBR_COMPONENT_INVALID:DECLARED_FILE_MISSING/,
  );
});

test("bounds raw bytes, nesting, Unicode, and trailing JSON", () => {
  assert.throws(
    () => parseAndValidateSbrComponentManifest(Buffer.alloc(MAX_COMPONENT_MANIFEST_BYTES + 1)),
    /SBR_COMPONENT_INVALID:MANIFEST_TOO_LARGE/,
  );
  assert.throws(
    () => parseAndValidateSbrComponentManifest(Buffer.from([0xc3, 0x28])),
    /SBR_COMPONENT_INVALID:MANIFEST_UTF8/,
  );
  assert.throws(
    () => parseAndValidateSbrComponentManifest(`{"extra":${"[".repeat(33)}null${"]".repeat(33)}}`),
    /SBR_COMPONENT_INVALID:JSON_DEPTH/,
  );
  assert.throws(
    () =>
      parseAndValidateSbrComponentManifest(
        JSON.stringify({ ...manifestFor(), component_name: "\ud800" }),
      ),
    /SBR_COMPONENT_INVALID:UNICODE/,
  );
  assert.throws(
    () => parseAndValidateSbrComponentManifest(`${JSON.stringify(manifestFor())} true`),
    /SBR_COMPONENT_INVALID:TRAILING_DATA/,
  );
});

test("enforces canonical manifest bytes for direct objects and parsed wrappers", async () => {
  const oversized = manifestWithCanonicalSize(MAX_COMPONENT_MANIFEST_BYTES + 1);
  const matching = { component_manifest_sha256: "a".repeat(64) };
  assert.throws(
    () =>
      verifySbrComponentCrossHashes({
        componentManifest: oversized,
        registrationManifest: matching,
        profile: matching,
      }),
    (error) => error?.message === "SBR_COMPONENT_INVALID:MANIFEST_TOO_LARGE",
  );
  await assert.rejects(
    verifySbrComponentBundleRaw({
      componentRoot: "component-root",
      manifest: { manifest: oversized },
    }),
    (error) => error?.message === "SBR_COMPONENT_INVALID:MANIFEST_TOO_LARGE",
  );
});

test("accepts a canonical manifest exactly at the 256 KiB boundary", () => {
  const boundary = manifestWithCanonicalSize(MAX_COMPONENT_MANIFEST_BYTES);
  const parsed = parseAndValidateSbrComponentManifest(encode(boundary));
  const matching = { component_manifest_sha256: parsed.sha256 };
  assert.equal(parsed.canonicalBytes.byteLength, MAX_COMPONENT_MANIFEST_BYTES);
  assert.equal(
    verifySbrComponentCrossHashes({
      componentManifest: boundary,
      registrationManifest: matching,
      profile: matching,
    }),
    true,
  );
});

test("cross-hashes must be strict, equal, and bind exact canonical bytes", () => {
  const parsed = parseAndValidateSbrComponentManifest(encode(manifestFor()));
  const matching = { component_manifest_sha256: parsed.sha256 };
  assert.equal(
    verifySbrComponentCrossHashes({
      componentManifest: parsed,
      registrationManifest: matching,
      profile: matching,
    }),
    true,
  );

  for (const [registrationHash, profileHash, code] of [
    ["NONE", parsed.sha256, "REGISTRATION_HASH"],
    [parsed.sha256.toUpperCase(), parsed.sha256, "REGISTRATION_HASH"],
    [parsed.sha256, "NONE", "PROFILE_HASH"],
    [parsed.sha256, parsed.sha256.toUpperCase(), "PROFILE_HASH"],
    ["a".repeat(64), parsed.sha256, "CROSS_HASH_MISMATCH"],
    [parsed.sha256, "a".repeat(64), "CROSS_HASH_MISMATCH"],
    ["a".repeat(64), "a".repeat(64), "COMPONENT_HASH_MISMATCH"],
  ]) {
    assert.throws(
      () =>
        verifySbrComponentCrossHashes({
          componentManifest: parsed,
          registrationManifest: { component_manifest_sha256: registrationHash },
          profile: { component_manifest_sha256: profileHash },
        }),
      new RegExp(`SBR_COMPONENT_INVALID:${code}`),
    );
  }
});

test("maps accessor and proxy hazards to deterministic errors without leaking details", () => {
  const parsed = parseAndValidateSbrComponentManifest(encode(manifestFor()));
  const componentAccessor = {};
  Object.defineProperty(componentAccessor, "manifest", {
    get: () => {
      throw new Error("sensitive absolute path detail");
    },
  });
  const registrationAccessor = {};
  Object.defineProperty(registrationAccessor, "component_manifest_sha256", {
    enumerable: true,
    get: () => {
      throw new Error("sensitive registration detail");
    },
  });

  assert.throws(
    () =>
      verifySbrComponentCrossHashes({
        componentManifest: componentAccessor,
        registrationManifest: { component_manifest_sha256: parsed.sha256 },
        profile: { component_manifest_sha256: parsed.sha256 },
      }),
    (error) => error?.message === "SBR_COMPONENT_INVALID:CANONICALIZATION",
  );
  assert.throws(
    () =>
      verifySbrComponentCrossHashes({
        componentManifest: parsed,
        registrationManifest: registrationAccessor,
        profile: { component_manifest_sha256: parsed.sha256 },
      }),
    (error) => error?.message === "SBR_COMPONENT_INVALID:REGISTRATION_HASH",
  );
});

test("a descriptor-relative test adapter verifies nested files with relative evidence", async (context) => {
  const root = await makeBundle(context);
  const parsed = parseAndValidateSbrComponentManifest(encode(manifestFor()));

  assert.deepEqual(await verifySbrComponentBundle({ componentRoot: root, manifest: parsed }), {
    component_manifest_sha256: parsed.sha256,
    files: ["bin/helper", "lib/config.json"],
    total_byte_length: 32,
  });
});

test("default Node bundle verification fails closed before opening component input", async () => {
  let openCalls = 0;
  await assert.rejects(
    verifySbrComponentBundleRaw({
      componentRoot: "component-root",
      dependencies: {
        open: async () => {
          openCalls += 1;
          throw new Error("must not open");
        },
      },
      manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
    }),
    /SBR_COMPONENT_INVALID:ANCHORED_TRAVERSAL_UNAVAILABLE/,
  );
  assert.equal(openCalls, 0);
});

test("descriptor-adapter reflection hazards fail closed without leaking details", async () => {
  const anchored = new Proxy(
    {},
    {
      getOwnPropertyDescriptor: () => {
        throw new Error("sensitive adapter detail");
      },
    },
  );
  await assert.rejects(
    verifySbrComponentBundleRaw({
      componentRoot: "component-root",
      dependencies: { anchored },
      manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
    }),
    (error) => error?.message === "SBR_COMPONENT_INVALID:ANCHORED_TRAVERSAL_UNAVAILABLE",
  );
});

for (const [name, mutate, code] of [
  ["length mismatch", (bytes) => Buffer.concat([bytes, Buffer.from("x")]), "FILE_LENGTH_MISMATCH"],
  [
    "hash mismatch",
    (bytes) => Buffer.from(bytes.toString().replace("helper", "tamper")),
    "FILE_HASH_MISMATCH",
  ],
]) {
  test(`rejects ${name}`, async (context) => {
    const files = new Map(VALID_FILES);
    files.set("bin/helper", mutate(files.get("bin/helper")));
    const root = await makeBundle(context, files);
    await assert.rejects(
      verifySbrComponentBundle({
        componentRoot: root,
        manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
      }),
      new RegExp(`SBR_COMPONENT_INVALID:${code}`),
    );
  });
}

test("rejects missing and undeclared files or directory content", async (context) => {
  const cases = [
    new Map([["bin/helper", VALID_FILES.get("bin/helper")]]),
    new Map([...VALID_FILES, ["extra.txt", Buffer.from("extra")]]),
    new Map([...VALID_FILES, ["empty/.keep", Buffer.from("undeclared")]]),
  ];
  for (const files of cases) {
    const root = await makeBundle(context, files);
    await assert.rejects(
      verifySbrComponentBundle({
        componentRoot: root,
        manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
      }),
      /SBR_COMPONENT_INVALID:(?:DECLARED_FILE_MISSING|UNDECLARED_PATH)/,
    );
  }
});

test("rejects an empty undeclared directory", async (context) => {
  const root = await makeBundle(context);
  await mkdir(path.join(root, "empty"));
  await assert.rejects(
    verifySbrComponentBundle({
      componentRoot: root,
      manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
    }),
    /SBR_COMPONENT_INVALID:UNDECLARED_PATH/,
  );
});

for (const kind of ["file", "directory"]) {
  test(`rejects a symlink ${kind} without following it`, async (context) => {
    const root = await makeBundle(context);
    if (kind === "file") {
      await symlink(path.join(root, "bin", "helper"), path.join(root, "linked"));
    } else {
      await symlink(path.join(root, "bin"), path.join(root, "linked"));
    }
    await assert.rejects(
      verifySbrComponentBundle({
        componentRoot: root,
        manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
      }),
      /SBR_COMPONENT_INVALID:SYMLINK/,
    );
  });
}

test("rejects a symlink component root", async (context) => {
  const root = await makeBundle(context);
  const linkedRoot = `${root}-link`;
  context.after(() => rm(linkedRoot, { force: true }));
  await symlink(root, linkedRoot);
  await assert.rejects(
    verifySbrComponentBundle({
      componentRoot: linkedRoot,
      manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
    }),
    /SBR_COMPONENT_INVALID:ROOT_OPEN/,
  );
});

test("a descriptor-relative adapter never accepts a swapped outside directory", async (context) => {
  const expectedFiles = new Map([["sub/file", Buffer.from("expected")]]);
  const root = await makeBundle(context, expectedFiles);
  const outside = await mkdtemp(path.join(tmpdir(), "tammy-sbr-outside-"));
  context.after(() => rm(outside, { force: true, recursive: true }));
  await writeFile(path.join(outside, "file"), Buffer.from("expected"));
  const originalSubdirectory = path.join(root, "sub");
  const retainedSubdirectory = path.join(root, "sub-original");
  let swapped = false;
  let outsideOpened = false;

  await assert.rejects(
    verifySbrComponentBundle({
      componentRoot: root,
      dependencies: {
        lstat: async (entryPath, options) => {
          const metadata = await lstat(entryPath, options);
          if (!swapped && entryPath === originalSubdirectory) {
            swapped = true;
            await rename(originalSubdirectory, retainedSubdirectory);
            await symlink(outside, originalSubdirectory);
          }
          return metadata;
        },
        onOpenAt: (parentPath, childName) => {
          if (swapped && parentPath === originalSubdirectory && childName === "file") {
            outsideOpened = true;
          }
        },
      },
      manifest: parseAndValidateSbrComponentManifest(encode(manifestFor(expectedFiles))),
    }),
    /SBR_COMPONENT_INVALID:(?:PATH_SWAP|SYMLINK|IDENTITY_MISMATCH)/,
  );
  assert.equal(swapped, true);
  assert.equal(outsideOpened, false);
});

test("a descriptor-relative adapter rejects real hard-link aliases", async (context) => {
  const bytes = Buffer.from("same inode");
  const files = new Map([
    ["a", bytes],
    ["b", bytes],
  ]);
  const root = await makeBundle(context, new Map([["a", bytes]]));
  try {
    await link(path.join(root, "a"), path.join(root, "b"));
  } catch (error) {
    if (error?.code === "ENOTSUP" || error?.code === "EPERM") {
      context.skip("hard links are unavailable");
      return;
    }
    throw error;
  }
  await assert.rejects(
    verifySbrComponentBundle({
      componentRoot: root,
      manifest: parseAndValidateSbrComponentManifest(encode(manifestFor(files))),
    }),
    /SBR_COMPONENT_INVALID:PATH_ALIAS/,
  );
});

test("rejects a special file without blocking where supported", async (context) => {
  const root = await makeBundle(context);
  const specialPath = path.join(root, "pipe");
  const created = spawnSync("mkfifo", [specialPath]);
  if (created.status !== 0) {
    context.skip("mkfifo is unavailable");
    return;
  }
  await assert.rejects(
    verifySbrComponentBundle({
      componentRoot: root,
      manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
    }),
    /SBR_COMPONENT_INVALID:SPECIAL_FILE/,
  );
});

test("bounds actual entry count and directory depth", async (context) => {
  const root = await makeBundle(context);
  for (let index = 0; index <= MAX_COMPONENT_ENTRIES; index += 1) {
    await writeFile(path.join(root, `extra-${String(index).padStart(4, "0")}`), "x");
  }
  await assert.rejects(
    verifySbrComponentBundle({
      componentRoot: root,
      manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
    }),
    /SBR_COMPONENT_INVALID:ENTRY_COUNT/,
  );

  const boundaryFiles = new Map([
    [`${"d/".repeat(MAX_COMPONENT_DEPTH - 1)}file`, Buffer.from("x")],
  ]);
  const boundaryRoot = await makeBundle(context, boundaryFiles);
  await verifySbrComponentBundle({
    componentRoot: boundaryRoot,
    manifest: parseAndValidateSbrComponentManifest(encode(manifestFor(boundaryFiles))),
  });

  const deepFiles = new Map([[`${"d/".repeat(MAX_COMPONENT_DEPTH)}file`, Buffer.from("x")]]);
  const deepRoot = await makeBundle(context, deepFiles);
  await assert.rejects(
    verifySbrComponentBundle({
      componentRoot: deepRoot,
      manifest: parseAndValidateSbrComponentManifest(encode(manifestFor(deepFiles))),
    }),
    /SBR_COMPONENT_INVALID:DIRECTORY_DEPTH/,
  );
});

function memoryHandle(
  bytes,
  {
    dev = 1,
    isDirectory = false,
    ino = isDirectory ? 1 : 2,
    onClose = () => {},
    size = bytes.byteLength,
  } = {},
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
    stat: async () => ({
      dev,
      ino,
      isDirectory: () => isDirectory,
      isFile: () => !isDirectory,
      size,
    }),
  };
}

test("rejects repeated fake file identities and closes every descriptor", async () => {
  const bytes = Buffer.from("same identity");
  const parsed = parseAndValidateSbrComponentManifest(
    encode(
      manifestFor(
        new Map([
          ["a", bytes],
          ["b", bytes],
        ]),
      ),
    ),
  );
  let closed = 0;
  const rootHandle = memoryHandle(Buffer.alloc(0), {
    isDirectory: true,
    onClose: () => {
      closed += 1;
    },
  });
  const handles = new Map([
    ["component-root/a", memoryHandle(bytes, { dev: 7, ino: 9, onClose: () => (closed += 1) })],
    ["component-root/b", memoryHandle(bytes, { dev: 7, ino: 9, onClose: () => (closed += 1) })],
  ]);
  await assert.rejects(
    verifySbrComponentBundle({
      componentRoot: "component-root",
      dependencies: {
        lstat: async (entryPath) =>
          entryPath === "component-root"
            ? { dev: 1, ino: 1, isDirectory: () => true }
            : {
                dev: 7,
                ino: 9,
                isDirectory: () => false,
                isFile: () => true,
                isSymbolicLink: () => false,
              },
        open: async (entryPath) =>
          entryPath === "component-root" ? rootHandle : handles.get(entryPath),
        readdir: async () => [{ name: "a" }, { name: "b" }],
      },
      manifest: parsed,
    }),
    /SBR_COMPONENT_INVALID:PATH_ALIAS/,
  );
  assert.equal(closed, 3);
});

test("closes retained directory descriptors when a parent entry is swapped", async () => {
  let rootClosed = false;
  let childClosed = false;
  let subdirectoryStats = 0;
  const rootHandle = memoryHandle(Buffer.alloc(0), {
    isDirectory: true,
    onClose: () => {
      rootClosed = true;
    },
  });
  const childHandle = memoryHandle(Buffer.alloc(0), {
    dev: 1,
    ino: 2,
    isDirectory: true,
    onClose: () => {
      childClosed = true;
    },
  });
  await assert.rejects(
    verifySbrComponentBundleRaw({
      componentRoot: "component-root",
      dependencies: {
        anchored: {
          lstat: async () => {
            subdirectoryStats += 1;
            if (subdirectoryStats === 1) {
              return {
                dev: 1,
                ino: 2,
                isDirectory: () => true,
                isFile: () => false,
                isSymbolicLink: () => false,
              };
            }
            return {
              isDirectory: () => false,
              isFile: () => false,
              isSymbolicLink: () => true,
            };
          },
          open: async () => childHandle,
          readdir: async (handle) => (handle === rootHandle ? [{ name: "sub" }] : []),
        },
        lstat: async () => ({ dev: 1, ino: 1, isDirectory: () => true }),
        open: async () => rootHandle,
      },
      manifest: parseAndValidateSbrComponentManifest(
        encode(manifestFor(new Map([["sub/file", Buffer.from("missing")]]))),
      ),
    }),
    /SBR_COMPONENT_INVALID:PATH_SWAP/,
  );
  assert.equal(childClosed, true);
  assert.equal(rootClosed, true);
});

test("closes root and file descriptors when verification fails", async () => {
  let rootClosed = false;
  let fileClosed = false;
  const bytes = Buffer.from("wrong");
  const parsed = parseAndValidateSbrComponentManifest(
    encode(manifestFor(new Map([["file", Buffer.from("right")]]))),
  );
  const rootHandle = memoryHandle(Buffer.alloc(0), {
    isDirectory: true,
    onClose: () => {
      rootClosed = true;
    },
  });
  const fileHandle = memoryHandle(bytes, {
    onClose: () => {
      fileClosed = true;
    },
  });

  await assert.rejects(
    verifySbrComponentBundle({
      componentRoot: "component-root",
      dependencies: {
        lstat: async () => ({
          dev: 1,
          ino: 2,
          isDirectory: () => false,
          isFile: () => true,
          isSymbolicLink: () => false,
        }),
        open: async (filePath) => (filePath === "component-root" ? rootHandle : fileHandle),
        readdir: async () => [{ name: "file" }],
      },
      manifest: parsed,
    }),
    /SBR_COMPONENT_INVALID:FILE_(?:LENGTH|HASH)_MISMATCH/,
  );
  assert.equal(rootClosed, true);
  assert.equal(fileClosed, true);
});

test("closes the root descriptor when root stat or enumeration fails", async () => {
  for (const failurePoint of ["stat", "readdir"]) {
    let rootClosed = false;
    const rootHandle = {
      close: async () => {
        rootClosed = true;
      },
      stat: async () => {
        if (failurePoint === "stat") {
          throw new Error("stat detail");
        }
        return { dev: 1, ino: 1, isDirectory: () => true };
      },
    };
    await assert.rejects(
      verifySbrComponentBundle({
        componentRoot: "component-root",
        dependencies: {
          open: async () => rootHandle,
          readdir: async () => {
            throw new Error("readdir detail");
          },
        },
        manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
      }),
      new RegExp(
        `SBR_COMPONENT_INVALID:${failurePoint === "stat" ? "ROOT_STAT" : "DIRECTORY_READ"}`,
      ),
    );
    assert.equal(rootClosed, true);
  }
});

test("bounds reads if a declared file grows after its descriptor stat", async () => {
  let fileClosed = false;
  const expected = Buffer.from("right");
  const grown = Buffer.from("right-extra");
  const parsed = parseAndValidateSbrComponentManifest(
    encode(manifestFor(new Map([["file", expected]]))),
  );
  const rootHandle = memoryHandle(Buffer.alloc(0), { isDirectory: true });
  const fileHandle = memoryHandle(grown, {
    onClose: () => {
      fileClosed = true;
    },
    size: expected.byteLength,
  });
  await assert.rejects(
    verifySbrComponentBundle({
      componentRoot: "component-root",
      dependencies: {
        lstat: async () => ({
          dev: 1,
          ino: 2,
          isDirectory: () => false,
          isFile: () => true,
          isSymbolicLink: () => false,
        }),
        open: async (filePath) => (filePath === "component-root" ? rootHandle : fileHandle),
        readdir: async () => [{ name: "file" }],
      },
      manifest: parsed,
    }),
    /SBR_COMPONENT_INVALID:FILE_LENGTH_MISMATCH/,
  );
  assert.equal(fileClosed, true);
});

test("fails closed before opening when secure flags are missing, invalid, or substituted", async () => {
  for (const dependencies of [
    { noFollowFlag: 0 },
    { noFollowFlag: -1 },
    { noFollowFlag: 1.5 },
    { noFollowFlag: "nofollow" },
    { noFollowFlag: constants.O_NONBLOCK },
    { nonBlockFlag: 0 },
    { nonBlockFlag: -1 },
    { nonBlockFlag: 1.5 },
    { nonBlockFlag: "nonblock" },
    { nonBlockFlag: constants.O_NOFOLLOW },
  ]) {
    let opens = 0;
    await assert.rejects(
      verifySbrComponentBundle({
        componentRoot: "component-root",
        dependencies: {
          ...dependencies,
          open: async () => {
            opens += 1;
            throw new Error("must not open");
          },
        },
        manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
      }),
      /SBR_COMPONENT_INVALID:(?:NOFOLLOW|NONBLOCK)_UNAVAILABLE/,
    );
    assert.equal(opens, 0);
  }
});

test("uses the platform no-follow and nonblocking flags on root and declared files", async (context) => {
  if (!constants.O_NOFOLLOW || !constants.O_NONBLOCK) {
    context.skip("secure open flags are unavailable");
    return;
  }
  const root = await makeBundle(context);
  const observedFlags = [];
  await verifySbrComponentBundle({
    componentRoot: root,
    dependencies: {
      open: async (filePath, flags) => {
        observedFlags.push(flags);
        return open(filePath, flags);
      },
    },
    manifest: parseAndValidateSbrComponentManifest(encode(manifestFor())),
  });
  assert.equal(observedFlags.length, 5);
  for (const flags of observedFlags) {
    assert.equal((flags & constants.O_NOFOLLOW) === constants.O_NOFOLLOW, true);
    assert.equal((flags & constants.O_NONBLOCK) === constants.O_NONBLOCK, true);
  }
});
