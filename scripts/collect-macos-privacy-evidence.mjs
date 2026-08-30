import { execFile as nodeExecFile } from "node:child_process";
import { createHash } from "node:crypto";
import { constants as fsConstants } from "node:fs";
import { lstat, open, readdir, realpath } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { isDeepStrictEqual, promisify } from "node:util";

import plist from "plist";

import { hashAppBundle } from "./capture-app-store-screenshots.mjs";
import { validateUnsignedContentManifest } from "./macos-unsigned-content.mjs";

const execFile = promisify(nodeExecFile);
const require = createRequire(import.meta.url);

const SHA256 = /^[0-9a-f]{64}$/u;
const SHA40 = /^[0-9a-f]{40}$/u;
const VERSION = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u;
const PACKAGE_VERSION =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/u;
const BUILD = /^[1-9]\d*$/u;
const PRIVACY_URL = "https://tammy-accounting.castlemilk.chatgpt.site/privacy";
const SUPPORT_URL = "https://tammy-accounting.castlemilk.chatgpt.site/support";
const PUBLIC_HOSTNAME = "tammy-accounting.castlemilk.chatgpt.site";
const FORBIDDEN_PACKAGE =
  /(?:^|[/_@.-])(analytics?|advertis(?:e|ing)|crash(?:lytics|report(?:er|ing)?)?|fingerprint(?:ing)?|remote[-_]?code|sentry|telemetry|tracking|updat(?:e|er)|web[-_]?content)(?:$|[/_@.-])/iu;
const FORBIDDEN_HOSTNAME =
  /(?:^|\.)(?:ads?|analytics?|crash|fingerprint|metrics?|sentry|telemetry|track(?:er|ing)?|updates?)(?:\.|$)/iu;
const SECRET_KEY =
  /(?:accounting|certificate|credential|environment|password|private.?key|profile.?bytes|secret|token)/iu;
const SAFE_RELATIVE_PATH = /^(?!\/)(?!.*(?:^|\/)\.\.(?:\/|$))(?!.*\\)(?!.*\0).+/u;

const IDENTITY_KEYS = [
  "buildNumber",
  "developmentAppSha256",
  "distributionAppSha256",
  "marketingVersion",
  "packageSha256",
  "productSourceCommit",
  "productSourceTree",
  "unsignedContentManifestSha256",
];

function fail(code = "MACOS_PRIVACY_EVIDENCE_INVALID") {
  throw new Error(code);
}

function record(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function exactKeys(value, keys) {
  return (
    record(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}

function sortedUniqueStrings(value, { allowEmpty = false, maximum = 512, pattern } = {}) {
  return (
    Array.isArray(value) &&
    (allowEmpty || value.length > 0) &&
    value.length <= maximum &&
    new Set(value).size === value.length &&
    value.every(
      (item, index) =>
        typeof item === "string" &&
        item.length > 0 &&
        item.length <= 2048 &&
        (!pattern || pattern.test(item)) &&
        (index === 0 || Buffer.compare(Buffer.from(value[index - 1]), Buffer.from(item)) < 0),
    )
  );
}

function validateIdentity(value) {
  if (
    !record(value) ||
    !BUILD.test(value.buildNumber) ||
    !SHA256.test(value.developmentAppSha256) ||
    !SHA256.test(value.distributionAppSha256) ||
    !VERSION.test(value.marketingVersion) ||
    !SHA256.test(value.packageSha256) ||
    !SHA40.test(value.productSourceCommit) ||
    !SHA40.test(value.productSourceTree) ||
    !SHA256.test(value.unsignedContentManifestSha256)
  ) {
    fail();
  }
}

function assertNoSecretShape(value, path = "evidence", depth = 0) {
  if (depth > 16) fail();
  if (Array.isArray(value)) {
    if (value.length > 20_000) fail();
    for (const item of value) assertNoSecretShape(item, path, depth + 1);
    return;
  }
  if (!record(value)) return;
  for (const [key, child] of Object.entries(value)) {
    if (SECRET_KEY.test(key)) fail();
    assertNoSecretShape(child, `${path}.${key}`, depth + 1);
  }
}

function validateAccessedApiReasons(value) {
  if (!Array.isArray(value) || value.length === 0 || value.length > 128) fail();
  let prior = "";
  for (const entry of value) {
    if (
      !exactKeys(entry, ["category", "manifestPath", "reasons"]) ||
      !/^NSPrivacyAccessedAPICategory[A-Za-z]+$/u.test(entry.category) ||
      !SAFE_RELATIVE_PATH.test(entry.manifestPath) ||
      !sortedUniqueStrings(entry.reasons, { maximum: 64, pattern: /^[A-Z0-9]{4}\.\d+$/u })
    ) {
      fail();
    }
    const order = `${entry.manifestPath}\0${entry.category}`;
    if (Buffer.compare(Buffer.from(prior), Buffer.from(order)) >= 0) fail();
    prior = order;
  }
}

function validateEntitlements(value) {
  if (!Array.isArray(value) || value.length === 0 || value.length > 128) fail();
  let prior = "";
  for (const entry of value) {
    if (
      !exactKeys(entry, ["keys", "path", "sha256"]) ||
      !SAFE_RELATIVE_PATH.test(entry.path) ||
      !SHA256.test(entry.sha256) ||
      !sortedUniqueStrings(entry.keys, { maximum: 256, pattern: /^[a-zA-Z0-9.-]+$/u }) ||
      Buffer.compare(Buffer.from(prior), Buffer.from(entry.path)) >= 0
    ) {
      fail();
    }
    prior = entry.path;
  }
}

function validateNativePayloads(value) {
  const kinds = new Set(["executable", "framework", "dylib", "native-module"]);
  if (!Array.isArray(value) || value.length === 0 || value.length > 4096) fail();
  let prior = "";
  for (const entry of value) {
    if (
      !exactKeys(entry, ["architectures", "kind", "path", "sha256"]) ||
      !sortedUniqueStrings(entry.architectures, { maximum: 8, pattern: /^(?:arm64|x86_64)$/u }) ||
      !isDeepStrictEqual(entry.architectures, ["arm64"]) ||
      !kinds.has(entry.kind) ||
      !SAFE_RELATIVE_PATH.test(entry.path) ||
      !SHA256.test(entry.sha256) ||
      Buffer.compare(Buffer.from(prior), Buffer.from(entry.path)) >= 0
    ) {
      fail();
    }
    prior = entry.path;
  }
}

function validatePrivacyManifests(value) {
  if (!Array.isArray(value) || value.length === 0 || value.length > 256) fail();
  let prior = "";
  for (const entry of value) {
    if (
      !exactKeys(entry, [
        "accessedApiCategories",
        "collectedDataTypeCount",
        "path",
        "sha256",
        "tracking",
        "trackingDomains",
      ]) ||
      !sortedUniqueStrings(entry.accessedApiCategories, {
        maximum: 128,
        pattern: /^NSPrivacyAccessedAPICategory[A-Za-z]+$/u,
      }) ||
      entry.collectedDataTypeCount !== 0 ||
      !SAFE_RELATIVE_PATH.test(entry.path) ||
      !entry.path.endsWith("PrivacyInfo.xcprivacy") ||
      !SHA256.test(entry.sha256) ||
      entry.tracking !== false ||
      !sortedUniqueStrings(entry.trackingDomains, { allowEmpty: true, maximum: 0 }) ||
      Buffer.compare(Buffer.from(prior), Buffer.from(entry.path)) >= 0
    ) {
      fail();
    }
    prior = entry.path;
  }
}

function validatePackages(value) {
  if (!Array.isArray(value) || value.length === 0 || value.length > 10_000) fail();
  let prior = "";
  for (const entry of value) {
    if (
      !exactKeys(entry, ["license", "name", "representedPaths", "version"]) ||
      typeof entry.name !== "string" ||
      !/^(@[a-z0-9._-]+\/)?[a-z0-9._-]+$/u.test(entry.name) ||
      FORBIDDEN_PACKAGE.test(entry.name) ||
      typeof entry.license !== "string" ||
      !/^[A-Za-z0-9().+\- ]{1,128}$/u.test(entry.license) ||
      !PACKAGE_VERSION.test(entry.version) ||
      !sortedUniqueStrings(entry.representedPaths, { maximum: 4096 }) ||
      Buffer.compare(Buffer.from(prior), Buffer.from(`${entry.name}@${entry.version}`)) >= 0
    ) {
      fail();
    }
    prior = `${entry.name}@${entry.version}`;
  }
}

function validateSupplementalReport(value) {
  if (
    !exactKeys(value, ["checkedTool", "status", "toolVersion"]) ||
    value.checkedTool !== "/usr/bin/xcodebuild" ||
    value.status !== "not-supported-by-detected-toolchain" ||
    typeof value.toolVersion !== "string" ||
    value.toolVersion.length === 0 ||
    value.toolVersion.length > 256
  ) {
    fail();
  }
}

export function validateMacOSPrivacyEvidence(value) {
  const topLevelKeys = [
    ...IDENTITY_KEYS,
    "accessedApiReasons",
    "embeddedHostnames",
    "embeddedPublicUrls",
    "entitlements",
    "nativePayloads",
    "privacyManifests",
    "productionPackages",
    "schemaVersion",
    "supplementalPrivacyReport",
  ];
  if (!exactKeys(value, topLevelKeys) || value.schemaVersion !== 1) fail();
  assertNoSecretShape(value);
  validateIdentity(value);
  validateAccessedApiReasons(value.accessedApiReasons);
  if (
    !sortedUniqueStrings(value.embeddedHostnames, { maximum: 256 }) ||
    value.embeddedHostnames.some(
      (hostname) => hostname !== hostname.toLowerCase() || FORBIDDEN_HOSTNAME.test(hostname),
    ) ||
    !value.embeddedHostnames.includes(PUBLIC_HOSTNAME) ||
    !isDeepStrictEqual(value.embeddedPublicUrls, [PRIVACY_URL, SUPPORT_URL])
  ) {
    fail();
  }
  validateEntitlements(value.entitlements);
  validateNativePayloads(value.nativePayloads);
  validatePrivacyManifests(value.privacyManifests);
  validatePackages(value.productionPackages);
  validateSupplementalReport(value.supplementalPrivacyReport);
  return value;
}

function validateCollectionInput(value) {
  const pathKeys = [
    "developmentApp",
    "distributionApp",
    "lockfilePath",
    "packagePath",
    "unsignedContentManifestPath",
  ];
  if (!exactKeys(value, [...IDENTITY_KEYS, ...pathKeys])) fail("MACOS_PRIVACY_INPUT_INVALID");
  validateIdentity(value);
  for (const key of pathKeys) {
    if (
      typeof value[key] !== "string" ||
      !path.isAbsolute(value[key]) ||
      path.normalize(value[key]) !== value[key] ||
      value[key].includes("\0")
    ) {
      fail("MACOS_PRIVACY_INPUT_INVALID");
    }
  }
}

function identityOf(value) {
  return Object.fromEntries(IDENTITY_KEYS.map((key) => [key, value[key]]));
}

function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (record(value)) {
    return Object.fromEntries(
      Object.keys(value)
        .sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)))
        .map((key) => [key, canonical(value[key])]),
    );
  }
  return value;
}

function sha256(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

async function stableFileBytes(file, maximumBytes) {
  let handle;
  try {
    handle = await open(file, fsConstants.O_RDONLY | fsConstants.O_NOFOLLOW);
    const before = await handle.stat({ bigint: true });
    if (
      !before.isFile() ||
      before.isSymbolicLink() ||
      before.nlink !== 1n ||
      before.size < 0n ||
      before.size > BigInt(maximumBytes)
    ) {
      fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
    }
    const bytes = await handle.readFile();
    const after = await handle.stat({ bigint: true });
    if (
      before.dev !== after.dev ||
      before.ino !== after.ino ||
      before.mode !== after.mode ||
      before.nlink !== after.nlink ||
      before.size !== after.size ||
      before.mtimeNs !== after.mtimeNs ||
      before.ctimeNs !== after.ctimeNs ||
      bytes.byteLength !== Number(before.size)
    ) {
      fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
    }
    return bytes;
  } catch (error) {
    if (error instanceof Error && error.message === "MACOS_PRIVACY_ARTIFACT_CHANGED") throw error;
    fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
  } finally {
    await handle?.close().catch(() => undefined);
  }
}

async function stableFileHash(file, maximumBytes = 2 * 1024 * 1024 * 1024) {
  return sha256(await stableFileBytes(file, maximumBytes));
}

async function stableFilePrefix(file, maximumFileBytes = 2 * 1024 * 1024 * 1024) {
  let handle;
  try {
    handle = await open(file, fsConstants.O_RDONLY | fsConstants.O_NOFOLLOW);
    const before = await handle.stat({ bigint: true });
    if (
      !before.isFile() ||
      before.isSymbolicLink() ||
      before.nlink !== 1n ||
      before.size < 4n ||
      before.size > BigInt(maximumFileBytes)
    ) {
      fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
    }
    const bytes = Buffer.alloc(4);
    const { bytesRead } = await handle.read(bytes, 0, bytes.length, 0);
    const after = await handle.stat({ bigint: true });
    if (
      bytesRead !== 4 ||
      before.dev !== after.dev ||
      before.ino !== after.ino ||
      before.mode !== after.mode ||
      before.nlink !== after.nlink ||
      before.size !== after.size ||
      before.mtimeNs !== after.mtimeNs ||
      before.ctimeNs !== after.ctimeNs
    ) {
      fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
    }
    return bytes;
  } catch (error) {
    if (error instanceof Error && error.message === "MACOS_PRIVACY_ARTIFACT_CHANGED") throw error;
    fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
  } finally {
    await handle?.close().catch(() => undefined);
  }
}

async function requireRealDirectory(directory) {
  const status = await lstat(directory).catch(() => fail("MACOS_PRIVACY_ARTIFACT_CHANGED"));
  if (
    !status.isDirectory() ||
    status.isSymbolicLink() ||
    (await realpath(directory).catch(() => fail("MACOS_PRIVACY_ARTIFACT_CHANGED"))) !== directory
  ) {
    fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
  }
}

async function authenticateMacOSPrivacyArtifacts(input) {
  await Promise.all([
    requireRealDirectory(input.developmentApp),
    requireRealDirectory(input.distributionApp),
  ]);
  const [developmentAppSha256, distributionAppSha256, packageSha256, unsignedBytes] =
    await Promise.all([
      hashAppBundle(input.developmentApp),
      hashAppBundle(input.distributionApp),
      stableFileHash(input.packagePath),
      stableFileBytes(input.unsignedContentManifestPath, 32 * 1024 * 1024),
    ]);
  let unsignedManifest;
  try {
    unsignedManifest = validateUnsignedContentManifest(JSON.parse(unsignedBytes.toString("utf8")));
  } catch {
    fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
  }
  const authenticated = {
    buildNumber: unsignedManifest.buildNumber,
    developmentAppSha256,
    distributionAppSha256,
    marketingVersion: unsignedManifest.marketingVersion,
    packageSha256,
    productSourceCommit: unsignedManifest.productSourceCommit,
    productSourceTree: unsignedManifest.productSourceTree,
    unsignedContentManifestSha256: sha256(unsignedBytes),
  };
  if (!isDeepStrictEqual(authenticated, identityOf(input))) {
    fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
  }
  return authenticated;
}

async function collectBundleFiles(app) {
  const result = [];
  let count = 0;
  let bytes = 0;
  const visit = async (directory, depth) => {
    if (depth > 32) fail("MACOS_PRIVACY_INSPECTION_LIMIT_EXCEEDED");
    const names = (await readdir(directory)).sort((left, right) =>
      Buffer.compare(Buffer.from(left), Buffer.from(right)),
    );
    for (const name of names) {
      count += 1;
      if (count > 40_000) fail("MACOS_PRIVACY_INSPECTION_LIMIT_EXCEEDED");
      const absolutePath = path.join(directory, name);
      const status = await lstat(absolutePath);
      if (status.isSymbolicLink()) continue;
      if (status.isDirectory()) {
        await visit(absolutePath, depth + 1);
        continue;
      }
      if (!status.isFile() || status.nlink !== 1) fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
      bytes += status.size;
      if (bytes > 2 * 1024 * 1024 * 1024) fail("MACOS_PRIVACY_INSPECTION_LIMIT_EXCEEDED");
      result.push({
        absolutePath,
        executable: (status.mode & 0o111) !== 0,
        path: path.relative(app, absolutePath).split(path.sep).join("/"),
        size: status.size,
      });
    }
  };
  await visit(app, 0);
  return result;
}

function parsePrivacyManifest(bytes, relativePath) {
  let manifest;
  try {
    manifest = plist.parse(bytes.toString("utf8"));
  } catch {
    fail("MACOS_PRIVACY_MANIFEST_INVALID");
  }
  if (!record(manifest)) fail("MACOS_PRIVACY_MANIFEST_INVALID");
  const accessed = manifest.NSPrivacyAccessedAPITypes;
  const collected = manifest.NSPrivacyCollectedDataTypes;
  const domains = manifest.NSPrivacyTrackingDomains;
  if (!Array.isArray(accessed) || !Array.isArray(collected) || !Array.isArray(domains)) {
    fail("MACOS_PRIVACY_MANIFEST_INVALID");
  }
  const reasons = accessed.map((entry) => {
    if (
      !record(entry) ||
      typeof entry.NSPrivacyAccessedAPIType !== "string" ||
      !Array.isArray(entry.NSPrivacyAccessedAPITypeReasons)
    ) {
      fail("MACOS_PRIVACY_MANIFEST_INVALID");
    }
    return {
      category: entry.NSPrivacyAccessedAPIType,
      manifestPath: relativePath,
      reasons: [...entry.NSPrivacyAccessedAPITypeReasons].sort(),
    };
  });
  reasons.sort((left, right) =>
    Buffer.compare(
      Buffer.from(`${left.manifestPath}\0${left.category}`),
      Buffer.from(`${right.manifestPath}\0${right.category}`),
    ),
  );
  return {
    reasons,
    summary: {
      accessedApiCategories: reasons.map(({ category }) => category).sort(),
      collectedDataTypeCount: collected.length,
      path: relativePath,
      sha256: sha256(bytes),
      tracking: manifest.NSPrivacyTracking,
      trackingDomains: [...domains].sort(),
    },
  };
}

function looksLikeMachO(bytes) {
  if (bytes.length < 4) return false;
  return new Set([
    0xfeedface, 0xfeedfacf, 0xcefaedfe, 0xcffaedfe, 0xcafebabe, 0xbebafeca, 0xcafebabf,
  ]).has(bytes.readUInt32BE(0));
}

async function nativePayload(file) {
  const prefix = await stableFilePrefix(file.absolutePath);
  if (!looksLikeMachO(prefix)) return undefined;
  const { stdout } = await execFile("/usr/bin/lipo", ["-archs", file.absolutePath], {
    encoding: "utf8",
    killSignal: "SIGKILL",
    maxBuffer: 16 * 1024,
    timeout: 5_000,
  }).catch(() => fail("MACOS_PRIVACY_NATIVE_PAYLOAD_INVALID"));
  const architectures = stdout.trim().split(/\s+/u).sort();
  const kind = file.path.includes(".framework/")
    ? "framework"
    : file.path.endsWith(".dylib")
      ? "dylib"
      : file.path.endsWith(".node")
        ? "native-module"
        : "executable";
  return {
    architectures,
    kind,
    path: file.path,
    sha256: await stableFileHash(file.absolutePath),
  };
}

function shouldInspectEntitlements(payload) {
  return (
    payload.kind === "executable" &&
    (payload.path === "Contents/MacOS/Tammy" ||
      /^Contents\/Frameworks\/Tammy Helper(?: \([A-Za-z]+\))?\.app\/Contents\/MacOS\//u.test(
        payload.path,
      ) ||
      payload.path.startsWith("Contents/Resources/core/") ||
      payload.path.startsWith("Contents/Resources/sbr-helper/"))
  );
}

async function inspectEntitlements(app, payload) {
  const absolutePath = path.join(app, ...payload.path.split("/"));
  const { stdout } = await execFile(
    "/usr/bin/codesign",
    ["-d", "--entitlements", ":-", absolutePath],
    {
      encoding: "utf8",
      killSignal: "SIGKILL",
      maxBuffer: 1024 * 1024,
      timeout: 5_000,
    },
  ).catch(() => fail("MACOS_PRIVACY_ENTITLEMENTS_INVALID"));
  let value;
  try {
    value = canonical(plist.parse(stdout));
  } catch {
    fail("MACOS_PRIVACY_ENTITLEMENTS_INVALID");
  }
  if (!record(value) || Object.keys(value).length === 0) {
    fail("MACOS_PRIVACY_ENTITLEMENTS_INVALID");
  }
  return {
    keys: Object.keys(value).sort(),
    path: payload.path,
    sha256: sha256(Buffer.from(JSON.stringify(value))),
  };
}

function asarLibrary() {
  const forgeRequire = createRequire(
    require.resolve("@electron-forge/cli/package.json", { paths: ["./apps/desktop"] }),
  );
  return forgeRequire("@electron/asar");
}

function packageRootForManifest(manifestPath) {
  const match = manifestPath.match(
    /^(.*\/node_modules\/(?:@[a-z0-9._-]+\/)?[a-z0-9._-]+)\/package\.json$/u,
  );
  return match?.[1];
}

async function inspectAsarPackages(asarPath, lockfile, lockfilePath) {
  const asar = asarLibrary();
  const entries = asar
    .listPackage(asarPath)
    .sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
  if (entries.length === 0 || entries.length > 100_000) {
    fail("MACOS_PRIVACY_INSPECTION_LIMIT_EXCEEDED");
  }
  const packages = new Map();
  for (const manifestPath of entries.filter((entry) => entry.endsWith("/package.json"))) {
    const packageRoot = packageRootForManifest(manifestPath);
    if (!packageRoot) continue;
    let metadata;
    try {
      const bytes = asar.extractFile(asarPath, manifestPath.slice(1));
      if (!Buffer.isBuffer(bytes) || bytes.length > 128 * 1024) throw new Error();
      metadata = JSON.parse(bytes.toString("utf8"));
    } catch {
      fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
    }
    if (
      !record(metadata) ||
      typeof metadata.name !== "string" ||
      typeof metadata.version !== "string" ||
      typeof metadata.license !== "string" ||
      !lockfile.includes(`${metadata.name}@${metadata.version}`)
    ) {
      fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
    }
    const representedPaths = entries.filter(
      (entry) => entry === packageRoot || entry.startsWith(`${packageRoot}/`),
    );
    if (representedPaths.length > 4096) fail("MACOS_PRIVACY_INSPECTION_LIMIT_EXCEEDED");
    const key = `${metadata.name}@${metadata.version}`;
    const existing = packages.get(key);
    if (existing && existing.license !== metadata.license) {
      fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
    }
    packages.set(key, {
      license: metadata.license,
      name: metadata.name,
      representedPaths: [...new Set([...(existing?.representedPaths ?? []), ...representedPaths])]
        .sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)))
        .slice(0, 4097),
      version: metadata.version,
    });
  }
  if (packages.size === 0) {
    const repositoryRoot = path.dirname(lockfilePath);
    const desktopPackagePath = path.join(repositoryRoot, "apps/desktop/package.json");
    let desktopPackage;
    try {
      desktopPackage = JSON.parse(
        (await stableFileBytes(desktopPackagePath, 128 * 1024)).toString("utf8"),
      );
    } catch {
      fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
    }
    if (!record(desktopPackage) || !record(desktopPackage.dependencies)) {
      fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
    }
    const representedPaths = entries.filter(
      (entry) => !entry.endsWith("/") && /\.(?:c?js|mjs)$/u.test(entry),
    );
    if (representedPaths.length === 0 || representedPaths.length > 4096) {
      fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
    }
    for (const [name, declaredVersion] of Object.entries(desktopPackage.dependencies)) {
      if (typeof declaredVersion !== "string") fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
      const packagePath = path.join(
        repositoryRoot,
        "apps/desktop/node_modules",
        ...name.split("/"),
        "package.json",
      );
      let metadata;
      try {
        metadata = JSON.parse((await stableFileBytes(packagePath, 128 * 1024)).toString("utf8"));
      } catch {
        fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
      }
      if (
        !record(metadata) ||
        metadata.name !== name ||
        typeof metadata.version !== "string" ||
        (declaredVersion !== "workspace:*" && metadata.version !== declaredVersion) ||
        (declaredVersion !== "workspace:*" &&
          !lockfile.includes(`'${name}':`) &&
          !lockfile.includes(`  ${name}:`)) ||
        (declaredVersion !== "workspace:*" && !lockfile.includes(`specifier: ${metadata.version}`))
      ) {
        fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
      }
      const license =
        typeof metadata.license === "string" && metadata.license.length > 0
          ? metadata.license
          : metadata.private === true
            ? "PROPRIETARY"
            : undefined;
      if (!license) fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
      packages.set(`${name}@${metadata.version}`, {
        license,
        name,
        representedPaths,
        version: metadata.version,
      });
    }
  }
  const packageList = [...packages.values()].sort((left, right) =>
    Buffer.compare(
      Buffer.from(`${left.name}@${left.version}`),
      Buffer.from(`${right.name}@${right.version}`),
    ),
  );
  if (packageList.some(({ representedPaths }) => representedPaths.length > 4096)) {
    fail("MACOS_PRIVACY_INSPECTION_LIMIT_EXCEEDED");
  }
  return { asar, entries, packages: packageList };
}

export async function inspectBundledProductionPackages({ asarPath, lockfilePath }) {
  if (
    typeof asarPath !== "string" ||
    !path.isAbsolute(asarPath) ||
    path.normalize(asarPath) !== asarPath ||
    typeof lockfilePath !== "string" ||
    !path.isAbsolute(lockfilePath) ||
    path.normalize(lockfilePath) !== lockfilePath
  ) {
    fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
  }
  const lockfile = await stableFileBytes(lockfilePath, 32 * 1024 * 1024);
  return (await inspectAsarPackages(asarPath, lockfile.toString("utf8"), lockfilePath)).packages;
}

function collectFirstPartyNetworkStrings(asar, asarPath, entries, externalFiles) {
  const urls = new Set();
  const hostnames = new Set();
  let totalBytes = 0;
  const inspect = (bytes) => {
    totalBytes += bytes.length;
    if (totalBytes > 128 * 1024 * 1024) fail("MACOS_PRIVACY_INSPECTION_LIMIT_EXCEEDED");
    const text = bytes.toString("utf8");
    for (const match of text.matchAll(
      /https:\/\/[a-z0-9.-]+(?::[0-9]+)?(?:\/[A-Za-z0-9._~!$&'()*+,;=:@%/?#-]*)?/giu,
    )) {
      try {
        const parsed = new URL(match[0]);
        hostnames.add(parsed.hostname.toLowerCase());
        if (match[0] === PRIVACY_URL || match[0] === SUPPORT_URL) urls.add(match[0]);
      } catch {
        // A regex candidate is not evidence unless URL parsing accepts it.
      }
    }
  };
  const firstPartyEntries = entries.filter(
    (entry) =>
      !entry.includes("/node_modules/") &&
      /(?:^|\/)(?:\.vite|package\.json)(?:\/|$)|\.(?:c?js|json|mjs)$/u.test(entry),
  );
  if (firstPartyEntries.length > 2048) fail("MACOS_PRIVACY_INSPECTION_LIMIT_EXCEEDED");
  for (const entry of firstPartyEntries) {
    const bytes = asar.extractFile(asarPath, entry.slice(1));
    if (!Buffer.isBuffer(bytes) || bytes.length > 8 * 1024 * 1024) {
      fail("MACOS_PRIVACY_INSPECTION_LIMIT_EXCEEDED");
    }
    inspect(bytes);
  }
  for (const bytes of externalFiles) inspect(bytes);
  return {
    embeddedHostnames: [...hostnames].sort(),
    embeddedPublicUrls: [...urls].sort(),
  };
}

async function supplementalPrivacyReport() {
  const { stdout } = await execFile("/usr/bin/xcodebuild", ["-version"], {
    encoding: "utf8",
    killSignal: "SIGKILL",
    maxBuffer: 16 * 1024,
    timeout: 5_000,
  }).catch(() => fail("MACOS_PRIVACY_XCODE_INSPECTION_FAILED"));
  const toolVersion = stdout.trim().split(/\r?\n/u).join("; ");
  if (toolVersion.length === 0 || toolVersion.length > 256) {
    fail("MACOS_PRIVACY_XCODE_INSPECTION_FAILED");
  }
  return {
    checkedTool: "/usr/bin/xcodebuild",
    status: "not-supported-by-detected-toolchain",
    toolVersion,
  };
}

async function inspectMacOSPrivacyArtifacts(input, authenticated) {
  const [files, lockfile] = await Promise.all([
    collectBundleFiles(input.distributionApp),
    stableFileBytes(input.lockfilePath, 32 * 1024 * 1024),
  ]);
  const manifestFiles = files.filter(({ path: relativePath }) =>
    relativePath.endsWith("PrivacyInfo.xcprivacy"),
  );
  if (manifestFiles.length === 0) fail("MACOS_PRIVACY_MANIFEST_INVALID");
  const parsedManifests = [];
  for (const file of manifestFiles) {
    parsedManifests.push(
      parsePrivacyManifest(await stableFileBytes(file.absolutePath, 1024 * 1024), file.path),
    );
  }
  const nativePayloads = (
    await Promise.all(
      files
        .filter((file) => file.executable || /\.(?:dylib|node)$/u.test(file.path))
        .map(nativePayload),
    )
  )
    .filter(Boolean)
    .sort((left, right) => Buffer.compare(Buffer.from(left.path), Buffer.from(right.path)));
  const entitlements = (
    await Promise.all(
      nativePayloads
        .filter(shouldInspectEntitlements)
        .map((payload) => inspectEntitlements(input.distributionApp, payload)),
    )
  ).sort((left, right) => Buffer.compare(Buffer.from(left.path), Buffer.from(right.path)));
  const asarFile = files.find(
    ({ path: relativePath }) => relativePath === "Contents/Resources/app.asar",
  );
  if (!asarFile) fail("MACOS_PRIVACY_PACKAGE_INVENTORY_INVALID");
  const inventory = await inspectAsarPackages(
    asarFile.absolutePath,
    lockfile.toString("utf8"),
    input.lockfilePath,
  );
  const externalNetworkFiles = await Promise.all(
    files
      .filter(
        ({ path: relativePath, size }) =>
          size <= 1024 * 1024 &&
          (relativePath === "Contents/Info.plist" ||
            relativePath.startsWith("Contents/Resources/build/")),
      )
      .map(({ absolutePath }) => stableFileBytes(absolutePath, 1024 * 1024)),
  );
  const network = collectFirstPartyNetworkStrings(
    inventory.asar,
    asarFile.absolutePath,
    inventory.entries,
    externalNetworkFiles,
  );
  return {
    ...authenticated,
    accessedApiReasons: parsedManifests.flatMap(({ reasons }) => reasons),
    ...network,
    entitlements,
    nativePayloads,
    privacyManifests: parsedManifests.map(({ summary }) => summary),
    productionPackages: inventory.packages,
    schemaVersion: 1,
    supplementalPrivacyReport: await supplementalPrivacyReport(),
  };
}

export async function collectMacOSPrivacyEvidence(
  input,
  {
    authenticateArtifacts = authenticateMacOSPrivacyArtifacts,
    inspectArtifacts = inspectMacOSPrivacyArtifacts,
  } = {},
) {
  validateCollectionInput(input);
  if (typeof authenticateArtifacts !== "function" || typeof inspectArtifacts !== "function") fail();
  const snapshot = Object.freeze({ ...input });
  const authenticated = await authenticateArtifacts(snapshot);
  if (!isDeepStrictEqual(authenticated, identityOf(snapshot))) {
    fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
  }
  const evidence = validateMacOSPrivacyEvidence(await inspectArtifacts(snapshot, authenticated));
  if (!isDeepStrictEqual(identityOf(evidence), authenticated)) {
    fail("MACOS_PRIVACY_ARTIFACT_CHANGED");
  }
  return evidence;
}
