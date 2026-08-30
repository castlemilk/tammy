import { createHash, randomUUID } from "node:crypto";
import {
  cp,
  link,
  lstat,
  mkdir,
  open,
  readdir,
  readFile,
  readlink,
  realpath,
  rm,
} from "node:fs/promises";
import path from "node:path";
import { isDeepStrictEqual } from "node:util";

import plist from "plist";

const SHA40 = /^[0-9a-f]{40}$/;
const SHA256 = /^[0-9a-f]{64}$/;
const BUILD = /^[1-9][0-9]*$/;
const VERSION = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const MANIFEST_KEYS = [
  "buildNumber",
  "bundleIdentifiers",
  "entries",
  "marketingVersion",
  "productSourceCommit",
  "productSourceTree",
  "publicLinks",
  "schemaVersion",
  "stagingDirectorySha256",
];
const FILE_KEYS = ["byteSize", "executable", "kind", "path", "sha256"];
const SYMLINK_KEYS = ["executable", "kind", "linkTarget", "path"];
const EXPECTED_HELPERS = [
  "com.tammy.desktop.helper",
  "com.tammy.desktop.helper.GPU",
  "com.tammy.desktop.helper.Plugin",
  "com.tammy.desktop.helper.Renderer",
];
const HELPER_BUNDLES = [
  ["Tammy Helper.app", "Tammy Helper", "com.tammy.desktop.helper"],
  ["Tammy Helper (GPU).app", "Tammy Helper (GPU)", "com.tammy.desktop.helper.GPU"],
  ["Tammy Helper (Plugin).app", "Tammy Helper (Plugin)", "com.tammy.desktop.helper.Plugin"],
  ["Tammy Helper (Renderer).app", "Tammy Helper (Renderer)", "com.tammy.desktop.helper.Renderer"],
];
const SIGNING_ARTIFACT_FILES = new Set([
  "Contents/_CodeSignature/CodeResources",
  "Contents/embedded.provisionprofile",
  ...HELPER_BUNDLES.flatMap(([bundle]) => [
    `Contents/Frameworks/${bundle}/Contents/_CodeSignature/CodeResources`,
    `Contents/Frameworks/${bundle}/Contents/embedded.provisionprofile`,
  ]),
  ...[
    "Electron Framework.framework",
    "Mantle.framework",
    "ReactiveObjC.framework",
    "Squirrel.framework",
  ].map((bundle) => `Contents/Frameworks/${bundle}/Versions/A/_CodeSignature/CodeResources`),
]);
const STANDARD_FRAMEWORK_EXECUTABLES = [
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Electron Framework",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Helpers/chrome_crashpad_handler",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libEGL.dylib",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libGLESv2.dylib",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libffmpeg.dylib",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libvk_swiftshader.dylib",
  "Contents/Frameworks/Mantle.framework/Versions/A/Mantle",
  "Contents/Frameworks/ReactiveObjC.framework/Versions/A/ReactiveObjC",
  "Contents/Frameworks/Squirrel.framework/Versions/A/Resources/ShipIt",
  "Contents/Frameworks/Squirrel.framework/Versions/A/Squirrel",
];
const REQUIRED_RUNTIME_FILES = [
  ["Contents/Info.plist", false],
  ["Contents/MacOS/Tammy", true],
  ["Contents/Resources/PrivacyInfo.xcprivacy", false],
  ["Contents/Resources/app.asar", false],
  ["Contents/Resources/build/build-manifest.json", false],
  ["Contents/Resources/core/darwin-arm64/tammy-core", true],
  ["Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper", true],
  ["Contents/Resources/sbr/simulator/sbr-profile-v1.json", false],
  ["Contents/Resources/sbr/simulator/sbr-profile-v1.sig", false],
  ["Contents/Resources/sqlcipher/LICENSE", false],
  ["Contents/Resources/sqlcipher/VERSION", false],
  ["Contents/Resources/sqlcipher/darwin-arm64/HEADER_SHA256", false],
  ["Contents/Resources/sqlcipher/darwin-arm64/LIBRARY_SHA256", false],
  ["Contents/Resources/sqlcipher/darwin-arm64/include/sqlite3.h", false],
  ["Contents/Resources/sqlcipher/darwin-arm64/lib/libsqlite3.a", false],
  ...STANDARD_FRAMEWORK_EXECUTABLES.map((relativePath) => [relativePath, true]),
  ...HELPER_BUNDLES.flatMap(([bundle, executable]) => [
    [`Contents/Frameworks/${bundle}/Contents/Info.plist`, false],
    [`Contents/Frameworks/${bundle}/Contents/MacOS/${executable}`, true],
  ]),
];
const SIGNED_SEMANTIC_KEYS = [
  "buildNumber",
  "bundleIdentifier",
  "entitlementIntent",
  "marketingVersion",
  "publicLinks",
  "signingMode",
  "unsignedSha256",
];

function fail(code = "MACOS_UNSIGNED_CONTENT_INVALID") {
  throw new Error(code);
}

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function exactKeys(value, expected) {
  return (
    isRecord(value) &&
    Object.keys(value).length === expected.length &&
    expected.every((key) => Object.hasOwn(value, key))
  );
}

function safeRelativePath(value) {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.trim() === value &&
    !value.includes("\\") &&
    !value.includes("\0") &&
    !path.posix.isAbsolute(value) &&
    path.posix.normalize(value) === value &&
    !value.split("/").some((segment) => segment === "" || segment === "." || segment === "..")
  );
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

async function fileSha256(file) {
  const handle = await open(file, "r");
  const hash = createHash("sha256");
  try {
    const stream = handle.createReadStream({ autoClose: false });
    for await (const chunk of stream) hash.update(chunk);
    return hash.digest("hex");
  } finally {
    await handle.close();
  }
}

function manifestHash(entries) {
  return sha256(Buffer.from(JSON.stringify(entries), "utf8"));
}

async function assertRoot(root) {
  if (typeof root !== "string" || !path.isAbsolute(root)) fail();
  const status = await lstat(root).catch(() => fail());
  if (!status.isDirectory() || status.isSymbolicLink()) fail();
  return realpath(root).catch(() => fail());
}

async function collectEntries(stagingRoot) {
  const resolvedRoot = await assertRoot(stagingRoot);
  const entries = [];

  async function visit(directory) {
    const children = await readdir(directory, { withFileTypes: true }).catch(() => fail());
    children.sort((left, right) => left.name.localeCompare(right.name, "en", { numeric: false }));
    for (const child of children) {
      const candidate = path.join(directory, child.name);
      const relativePath = path.relative(stagingRoot, candidate).split(path.sep).join("/");
      if (!safeRelativePath(relativePath)) fail();
      const status = await lstat(candidate).catch(() => fail());
      if (status.isSymbolicLink()) {
        const linkTarget = await readlink(candidate);
        if (path.isAbsolute(linkTarget) || linkTarget.includes("\0") || linkTarget.includes("\\")) {
          fail();
        }
        const resolvedTarget = await realpath(candidate).catch(() => fail());
        if (
          resolvedTarget !== resolvedRoot &&
          !resolvedTarget.startsWith(`${resolvedRoot}${path.sep}`)
        ) {
          fail();
        }
        entries.push({
          executable: false,
          kind: "symlink",
          linkTarget,
          path: relativePath,
        });
      } else if (status.isDirectory()) {
        await visit(candidate);
      } else if (status.isFile()) {
        entries.push({
          byteSize: status.size,
          executable: (status.mode & 0o111) !== 0,
          kind: "file",
          path: relativePath,
          sha256: await fileSha256(candidate),
        });
      } else {
        fail();
      }
    }
  }

  await visit(stagingRoot);
  entries.sort((left, right) => Buffer.compare(Buffer.from(left.path), Buffer.from(right.path)));
  if (entries.length === 0) fail();
  return entries;
}

function validatePublicLinks(value) {
  if (
    !exactKeys(value, ["privacyPolicy", "support"]) ||
    value.privacyPolicy !== "https://tammy-accounting.castlemilk.chatgpt.site/privacy" ||
    value.support !== "https://tammy-accounting.castlemilk.chatgpt.site/support"
  ) {
    fail();
  }
}

function validateBundleIdentifiers(value) {
  if (
    !exactKeys(value, ["app", "helpers"]) ||
    value.app !== "com.tammy.desktop" ||
    !isDeepStrictEqual(value.helpers, EXPECTED_HELPERS)
  ) {
    fail();
  }
}

function validateRuntimeInventory(entries) {
  const byPath = new Map(entries.map((entry) => [entry.path, entry]));
  for (const [relativePath, executable] of REQUIRED_RUNTIME_FILES) {
    const entry = byPath.get(relativePath);
    if (entry?.kind !== "file" || entry.executable !== executable) fail();
  }
  const icons = entries.filter(
    (entry) =>
      entry.kind === "file" &&
      !entry.executable &&
      /^Contents\/Resources\/[^/]+\.icns$/u.test(entry.path),
  );
  if (icons.length !== 1) fail();
}

async function parsePlist(file) {
  try {
    const parsed = plist.parse(await readFile(file, "utf8"));
    if (!isRecord(parsed)) fail();
    return parsed;
  } catch {
    fail();
  }
}

async function validatePackagedBundleFacts(stagingRoot, input, entries) {
  const app = await parsePlist(path.join(stagingRoot, "Contents", "Info.plist"));
  if (
    app.CFBundleIdentifier !== input.bundleIdentifiers.app ||
    app.CFBundleExecutable !== "Tammy" ||
    app.CFBundleShortVersionString !== input.marketingVersion ||
    app.CFBundleVersion !== input.buildNumber ||
    app.TammyPrivacyPolicyURL !== input.publicLinks.privacyPolicy ||
    app.TammySupportURL !== input.publicLinks.support ||
    typeof app.CFBundleIconFile !== "string" ||
    !entries.some(
      (entry) =>
        entry.kind === "file" && entry.path === `Contents/Resources/${app.CFBundleIconFile}`,
    )
  ) {
    fail();
  }
  for (const [index, [bundle, executable]] of HELPER_BUNDLES.entries()) {
    const helper = await parsePlist(
      path.join(stagingRoot, "Contents", "Frameworks", bundle, "Contents", "Info.plist"),
    );
    if (
      helper.CFBundleIdentifier !== input.bundleIdentifiers.helpers[index] ||
      helper.CFBundleExecutable !== executable ||
      helper.CFBundleShortVersionString !== input.marketingVersion ||
      helper.CFBundleVersion !== input.buildNumber
    ) {
      fail();
    }
  }
}

export function validateUnsignedContentManifest(manifest) {
  if (
    !exactKeys(manifest, MANIFEST_KEYS) ||
    manifest.schemaVersion !== 1 ||
    !VERSION.test(manifest.marketingVersion) ||
    !BUILD.test(manifest.buildNumber) ||
    !SHA40.test(manifest.productSourceCommit) ||
    !SHA40.test(manifest.productSourceTree) ||
    !SHA256.test(manifest.stagingDirectorySha256) ||
    !Array.isArray(manifest.entries) ||
    manifest.entries.length === 0
  ) {
    fail();
  }
  validatePublicLinks(manifest.publicLinks);
  validateBundleIdentifiers(manifest.bundleIdentifiers);
  let prior = "";
  for (const entry of manifest.entries) {
    const expectedKeys = entry?.kind === "file" ? FILE_KEYS : SYMLINK_KEYS;
    if (
      !exactKeys(entry, expectedKeys) ||
      !safeRelativePath(entry.path) ||
      entry.path <= prior ||
      typeof entry.executable !== "boolean"
    ) {
      fail();
    }
    prior = entry.path;
    if (entry.kind === "file") {
      if (
        !Number.isSafeInteger(entry.byteSize) ||
        entry.byteSize < 0 ||
        !SHA256.test(entry.sha256)
      ) {
        fail();
      }
    } else if (
      entry.kind !== "symlink" ||
      entry.executable !== false ||
      typeof entry.linkTarget !== "string" ||
      entry.linkTarget.length === 0 ||
      path.isAbsolute(entry.linkTarget) ||
      entry.linkTarget.includes("\\") ||
      entry.linkTarget.includes("\0")
    ) {
      fail();
    }
  }
  if (manifestHash(manifest.entries) !== manifest.stagingDirectorySha256) fail();
  validateRuntimeInventory(manifest.entries);
  return manifest;
}

export async function createUnsignedContentManifest(input) {
  if (
    !exactKeys(input, [
      "buildNumber",
      "bundleIdentifiers",
      "marketingVersion",
      "productSourceCommit",
      "productSourceTree",
      "publicLinks",
      "stagingRoot",
    ])
  ) {
    fail();
  }
  const entries = await collectEntries(input.stagingRoot);
  const manifest = validateUnsignedContentManifest({
    buildNumber: input.buildNumber,
    bundleIdentifiers: input.bundleIdentifiers,
    entries,
    marketingVersion: input.marketingVersion,
    productSourceCommit: input.productSourceCommit,
    productSourceTree: input.productSourceTree,
    publicLinks: input.publicLinks,
    schemaVersion: 1,
    stagingDirectorySha256: manifestHash(entries),
  });
  await validatePackagedBundleFacts(input.stagingRoot, input, entries);
  return manifest;
}

export async function authenticateUnsignedContentManifest(stagingRoot, manifest) {
  validateUnsignedContentManifest(manifest);
  const current = await createUnsignedContentManifest({
    buildNumber: manifest.buildNumber,
    bundleIdentifiers: manifest.bundleIdentifiers,
    marketingVersion: manifest.marketingVersion,
    productSourceCommit: manifest.productSourceCommit,
    productSourceTree: manifest.productSourceTree,
    publicLinks: manifest.publicLinks,
    stagingRoot,
  });
  if (!isDeepStrictEqual(current, manifest)) fail();
  return manifest;
}

export async function writeUnsignedContentManifest(target, manifest) {
  validateUnsignedContentManifest(manifest);
  if (typeof target !== "string" || !path.isAbsolute(target)) fail();
  await mkdir(path.dirname(target), { recursive: true });
  const temporary = `${target}.${randomUUID()}.tmp`;
  let handle;
  try {
    handle = await open(temporary, "wx", 0o600);
    await handle.writeFile(`${JSON.stringify(manifest, null, 2)}\n`, "utf8");
    await handle.sync();
    await handle.close();
    handle = undefined;
    await link(temporary, target);
    await rm(temporary);
    const directory = await open(path.dirname(target), "r");
    try {
      await directory.sync();
    } finally {
      await directory.close();
    }
  } catch (error) {
    await handle?.close().catch(() => {});
    await rm(temporary, { force: true }).catch(() => {});
    if (error?.code === "EEXIST") fail("MACOS_UNSIGNED_CONTENT_DESTINATION_EXISTS");
    fail("MACOS_UNSIGNED_CONTENT_WRITE_FAILED");
  }
}

export async function cloneUnsignedStaging({ destination, manifest, source }) {
  if (
    ![source, destination].every((value) => typeof value === "string" && path.isAbsolute(value))
  ) {
    fail();
  }
  await lstat(destination)
    .then(() => fail("MACOS_UNSIGNED_CONTENT_DESTINATION_EXISTS"))
    .catch((error) => {
      if (error?.code !== "ENOENT") throw error;
    });
  await authenticateUnsignedContentManifest(source, manifest);
  await mkdir(path.dirname(destination), { recursive: true });
  try {
    await cp(source, destination, {
      dereference: false,
      errorOnExist: true,
      force: false,
      preserveTimestamps: false,
      recursive: true,
      verbatimSymlinks: true,
    });
    await Promise.all([
      authenticateUnsignedContentManifest(source, manifest),
      authenticateUnsignedContentManifest(destination, manifest),
    ]);
  } catch (error) {
    await rm(destination, { force: true, recursive: true }).catch(() => {});
    if (error instanceof Error && error.message === "MACOS_UNSIGNED_CONTENT_DESTINATION_EXISTS") {
      throw error;
    }
    fail();
  }
}

function allowedSigningArtifact(relativePath) {
  return SIGNING_ARTIFACT_FILES.has(relativePath);
}

async function collectSignedCopyEntries(app) {
  return collectEntries(app);
}

function validateSignedSemantics(value, mode, unsignedEntry, manifest) {
  if (
    !exactKeys(value, SIGNED_SEMANTIC_KEYS) ||
    value.signingMode !== mode ||
    value.marketingVersion !== manifest.marketingVersion ||
    value.buildNumber !== manifest.buildNumber ||
    typeof value.bundleIdentifier !== "string" ||
    !/^[A-Za-z0-9.-]+$/.test(value.bundleIdentifier) ||
    !Array.isArray(value.entitlementIntent) ||
    value.entitlementIntent.length === 0 ||
    new Set(value.entitlementIntent).size !== value.entitlementIntent.length ||
    !value.entitlementIntent.every(
      (item, index) =>
        typeof item === "string" && (index === 0 || value.entitlementIntent[index - 1] < item),
    ) ||
    value.unsignedSha256 !== unsignedEntry.sha256
  ) {
    fail("MACOS_SIGNED_COPY_MISMATCH");
  }
  validatePublicLinks(value.publicLinks);
  return { ...value, signingMode: undefined };
}

export async function compareSignedCopies({
  developmentApp,
  distributionApp,
  inspectSignedFile,
  normalizeFile = undefined,
  normalizedPaths = [],
  signedPaths,
  unsignedManifest,
}) {
  const development = await validateSignedCopyAgainstUnsignedManifest({
    app: developmentApp,
    inspectSignedFile,
    mode: "development",
    normalizeFile,
    normalizedPaths,
    signedPaths,
    unsignedManifest,
  });
  const distribution = await validateSignedCopyAgainstUnsignedManifest({
    app: distributionApp,
    inspectSignedFile,
    mode: "distribution",
    normalizeFile,
    normalizedPaths,
    signedPaths,
    unsignedManifest,
  });
  for (const relativePath of normalizedPaths) {
    if (
      !isDeepStrictEqual(
        development.normalizedSemantics.get(relativePath),
        distribution.normalizedSemantics.get(relativePath),
      )
    ) {
      fail("MACOS_SIGNED_COPY_MISMATCH");
    }
  }
  for (const relativePath of signedPaths) {
    if (
      !isDeepStrictEqual(
        development.signedSemantics.get(relativePath),
        distribution.signedSemantics.get(relativePath),
      )
    ) {
      fail("MACOS_SIGNED_COPY_MISMATCH");
    }
  }
  return {
    developmentApp,
    distributionApp,
    stagingDirectorySha256: unsignedManifest.stagingDirectorySha256,
  };
}

export async function validateSignedCopyAgainstUnsignedManifest({
  app,
  inspectSignedFile,
  mode,
  normalizeFile = undefined,
  normalizedPaths = [],
  signedPaths,
  unsignedManifest,
}) {
  validateUnsignedContentManifest(unsignedManifest);
  if (
    typeof app !== "string" ||
    !path.isAbsolute(app) ||
    !["development", "distribution"].includes(mode) ||
    typeof inspectSignedFile !== "function" ||
    (normalizeFile !== undefined && typeof normalizeFile !== "function") ||
    !Array.isArray(signedPaths) ||
    signedPaths.length === 0 ||
    signedPaths.some(
      (value, index) => !safeRelativePath(value) || (index > 0 && signedPaths[index - 1] >= value),
    )
  ) {
    fail("MACOS_SIGNED_COPY_MISMATCH");
  }
  if (
    !Array.isArray(normalizedPaths) ||
    normalizedPaths.some(
      (value, index) =>
        !safeRelativePath(value) || (index > 0 && normalizedPaths[index - 1] >= value),
    ) ||
    (normalizedPaths.length > 0 && normalizeFile === undefined)
  ) {
    fail("MACOS_SIGNED_COPY_MISMATCH");
  }
  const signed = new Set(signedPaths);
  const normalized = new Set(normalizedPaths);
  const expected = new Map(unsignedManifest.entries.map((entry) => [entry.path, entry]));
  for (const relativePath of signed) {
    if (expected.get(relativePath)?.kind !== "file") fail("MACOS_SIGNED_COPY_MISMATCH");
  }
  for (const relativePath of normalized) {
    if (expected.get(relativePath)?.kind !== "file" || signed.has(relativePath)) {
      fail("MACOS_SIGNED_COPY_MISMATCH");
    }
  }
  const entries = await collectSignedCopyEntries(app);
  const copy = new Map(entries.map((entry) => [entry.path, entry]));
  for (const entry of entries) {
    if (!expected.has(entry.path) && !allowedSigningArtifact(entry.path)) {
      fail("MACOS_SIGNED_COPY_MISMATCH");
    }
    if (allowedSigningArtifact(entry.path) && entry.kind !== "file") {
      fail("MACOS_SIGNED_COPY_MISMATCH");
    }
  }
  const normalizedSemantics = new Map();
  const signedSemantics = new Map();
  for (const unsignedEntry of unsignedManifest.entries) {
    const copyEntry = copy.get(unsignedEntry.path);
    if (allowedSigningArtifact(unsignedEntry.path)) continue;
    if (signed.has(unsignedEntry.path) || normalized.has(unsignedEntry.path)) {
      if (copyEntry?.kind !== "file") fail("MACOS_SIGNED_COPY_MISMATCH");
      continue;
    }
    if (!isDeepStrictEqual(copyEntry, unsignedEntry)) {
      fail("MACOS_SIGNED_COPY_MISMATCH");
    }
  }
  for (const relativePath of normalizedPaths) {
    normalizedSemantics.set(relativePath, await normalizeFile(app, relativePath, mode));
  }
  for (const relativePath of signedPaths) {
    const unsignedEntry = expected.get(relativePath);
    signedSemantics.set(
      relativePath,
      validateSignedSemantics(
        await inspectSignedFile(app, relativePath, mode),
        mode,
        unsignedEntry,
        unsignedManifest,
      ),
    );
  }
  return {
    app,
    normalizedSemantics,
    signedSemantics,
    stagingDirectorySha256: unsignedManifest.stagingDirectorySha256,
  };
}
