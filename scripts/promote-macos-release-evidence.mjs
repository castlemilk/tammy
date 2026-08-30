import { createHash } from "node:crypto";
import { constants as fsConstants } from "node:fs";
import { lstat, mkdir, open, readdir, realpath, rename, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { isDeepStrictEqual } from "node:util";

import { validateMacOSPrivacyEvidence } from "./collect-macos-privacy-evidence.mjs";
import { validateMacOSRuntimeEgressEvidence } from "./macos-runtime-egress.mjs";

const SHA256 = /^[0-9a-f]{64}$/u;
const SHA40 = /^[0-9a-f]{40}$/u;
const VERSION = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u;
const BUILD = /^[1-9]\d*$/u;
const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const UTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/u;
const SECRET_KEY =
  /(?:certificateBytes|credential|environment|password|privateKey|profileBytes|secret|token)/iu;
const SECRET_VALUE =
  /(?:-----BEGIN [A-Z ]*PRIVATE KEY-----|\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}\b)/u;
const INPUT_FILES = [
  "candidate.json",
  "metadata-snapshot.json",
  "privacy-evidence.json",
  "runtime-egress.json",
  "screenshots.json",
];
const OUTPUT_FILES = [...INPUT_FILES, "summary.md"];
const CANDIDATE_KEYS = [
  "appSha256",
  "buildNumber",
  "buildNumberReserved",
  "packageSha256",
  "privacyEvidencePassed",
  "publicUrlsMatch",
  "releaseVersion",
  "runtimeEgressEvidencePassed",
  "screenshotsLinked",
  "signingProfilePassed",
  "sourceCommit",
  "sourceTree",
];
const SCREENSHOT_KEYS = [
  "buildNumber",
  "candidateEventPath",
  "candidateEventSha256",
  "captureArtifactKind",
  "capturedAt",
  "developmentSignedAppSha256",
  "dimensions",
  "distributionPackageSha256",
  "fixturePath",
  "fixtureSha256",
  "images",
  "locale",
  "marketingVersion",
  "productSourceCommit",
  "productSourceTree",
  "schemaVersion",
  "unsignedContentManifestSha256",
];
const METADATA_KEYS = [
  "buildNumber",
  "exportCompliance",
  "marketingVersion",
  "metadataSha256",
  "policySha256",
  "productSourceCommit",
  "productSourceTree",
  "publicSiteDeploymentId",
  "publicSiteOrigin",
  "publicSiteVersion",
  "schemaVersion",
];

function fail(code = "MACOS_RELEASE_EVIDENCE_INVALID") {
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

function assertRedacted(value, depth = 0) {
  if (depth > 16) fail();
  if (typeof value === "string") {
    if (value.length > 4096 || SECRET_VALUE.test(value)) fail();
    return;
  }
  if (Array.isArray(value)) {
    if (value.length > 20_000) fail();
    for (const child of value) assertRedacted(child, depth + 1);
    return;
  }
  if (!record(value)) return;
  for (const [key, child] of Object.entries(value)) {
    if (SECRET_KEY.test(key)) fail();
    assertRedacted(child, depth + 1);
  }
}

function sha256(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function jsonBytes(value) {
  return Buffer.from(`${JSON.stringify(value, null, 2)}\n`);
}

async function stableFileBytes(file, maximumBytes = 16 * 1024 * 1024) {
  let handle;
  try {
    handle = await open(file, fsConstants.O_RDONLY | fsConstants.O_NOFOLLOW);
    const before = await handle.stat({ bigint: true });
    if (
      !before.isFile() ||
      before.isSymbolicLink() ||
      before.nlink !== 1n ||
      before.size <= 0n ||
      before.size > BigInt(maximumBytes)
    ) {
      fail();
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
      fail();
    }
    return bytes;
  } catch (error) {
    if (error instanceof Error && error.message === "MACOS_RELEASE_EVIDENCE_INVALID") throw error;
    fail();
  } finally {
    await handle?.close().catch(() => undefined);
  }
}

async function readStableJson(file) {
  try {
    return JSON.parse((await stableFileBytes(file)).toString("utf8"));
  } catch {
    fail();
  }
}

function validateCandidate(candidate) {
  if (
    !exactKeys(candidate, CANDIDATE_KEYS) ||
    !VERSION.test(candidate.releaseVersion) ||
    !BUILD.test(candidate.buildNumber) ||
    !SHA40.test(candidate.sourceCommit) ||
    !SHA40.test(candidate.sourceTree) ||
    !SHA256.test(candidate.appSha256) ||
    !SHA256.test(candidate.packageSha256) ||
    ![
      candidate.buildNumberReserved,
      candidate.privacyEvidencePassed,
      candidate.publicUrlsMatch,
      candidate.runtimeEgressEvidencePassed,
      candidate.screenshotsLinked,
      candidate.signingProfilePassed,
    ].every((value) => value === true)
  ) {
    fail();
  }
  return candidate;
}

function validateScreenshotEvidence(value) {
  if (
    !exactKeys(value, SCREENSHOT_KEYS) ||
    value.schemaVersion !== 1 ||
    !VERSION.test(value.marketingVersion) ||
    !BUILD.test(value.buildNumber) ||
    !SHA40.test(value.productSourceCommit) ||
    !SHA40.test(value.productSourceTree) ||
    !SHA256.test(value.developmentSignedAppSha256) ||
    !SHA256.test(value.fixtureSha256) ||
    !SHA256.test(value.unsignedContentManifestSha256) ||
    value.captureArtifactKind !== "development-signed-app" ||
    value.locale !== "en-AU" ||
    !UTC.test(value.capturedAt) ||
    !Number.isFinite(Date.parse(value.capturedAt)) ||
    !isDeepStrictEqual(value.dimensions, { height: 900, width: 1440 }) ||
    value.fixturePath !== "apps/desktop/release/macos/screenshots/fixture.json" ||
    !Array.isArray(value.images) ||
    value.images.length !== 5
  ) {
    fail();
  }
  const filenames = new Set();
  const hashes = new Set();
  for (const image of value.images) {
    if (
      !exactKeys(image, [
        "accessibilitySnapshot",
        "accessibilitySnapshotSha256",
        "caption",
        "filename",
        "sha256",
      ]) ||
      !/^0[1-5]\.png$/u.test(image.filename) ||
      image.accessibilitySnapshot !== image.filename.replace(/\.png$/u, ".accessibility.txt") ||
      !SHA256.test(image.accessibilitySnapshotSha256) ||
      !SHA256.test(image.sha256) ||
      typeof image.caption !== "string" ||
      image.caption.length < 4 ||
      image.caption.length > 256 ||
      filenames.has(image.filename) ||
      hashes.has(image.sha256)
    ) {
      fail();
    }
    filenames.add(image.filename);
    hashes.add(image.sha256);
  }
  return value;
}

function validateMetadataSnapshot(value) {
  if (
    !exactKeys(value, METADATA_KEYS) ||
    value.schemaVersion !== 1 ||
    !VERSION.test(value.marketingVersion) ||
    !BUILD.test(value.buildNumber) ||
    !SHA40.test(value.productSourceCommit) ||
    !SHA40.test(value.productSourceTree) ||
    !SHA256.test(value.metadataSha256) ||
    !SHA256.test(value.policySha256) ||
    value.exportCompliance !== "exempt" ||
    value.publicSiteOrigin !== "https://tammy-accounting.castlemilk.chatgpt.site" ||
    !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/u.test(value.publicSiteDeploymentId) ||
    !/^[1-9]\d*$/u.test(value.publicSiteVersion)
  ) {
    fail();
  }
  return value;
}

async function validateEvidenceDirectory(repositoryRoot, evidenceDirectory) {
  if (
    typeof repositoryRoot !== "string" ||
    !path.isAbsolute(repositoryRoot) ||
    path.normalize(repositoryRoot) !== repositoryRoot ||
    typeof evidenceDirectory !== "string" ||
    !path.isAbsolute(evidenceDirectory) ||
    path.normalize(evidenceDirectory) !== evidenceDirectory
  ) {
    fail();
  }
  const [resolvedRoot, resolvedEvidence] = await Promise.all([
    realpath(repositoryRoot).catch(() => fail()),
    realpath(evidenceDirectory).catch(() => fail()),
  ]);
  const relative = path.relative(resolvedRoot, resolvedEvidence).split(path.sep).join("/");
  const match = relative.match(
    /^\.tmp\/macos-release\/((?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*))\/build-([1-9]\d*)\/evidence\/([0-9a-f-]+)$/u,
  );
  if (!match || !UUID_V7.test(match[3])) fail();
  const status = await lstat(evidenceDirectory).catch(() => fail());
  if (!status.isDirectory() || status.isSymbolicLink()) fail();
  const names = (await readdir(evidenceDirectory)).sort();
  if (!isDeepStrictEqual(names, [...INPUT_FILES].sort())) fail();
  return { buildNumber: match[2], eventId: match[3], releaseVersion: match[1] };
}

function stagedEvidenceIdentity(repositoryRoot, evidenceDirectory) {
  if (
    typeof repositoryRoot !== "string" ||
    !path.isAbsolute(repositoryRoot) ||
    path.normalize(repositoryRoot) !== repositoryRoot ||
    typeof evidenceDirectory !== "string" ||
    !path.isAbsolute(evidenceDirectory) ||
    path.normalize(evidenceDirectory) !== evidenceDirectory
  ) {
    fail();
  }
  const relative = path.relative(repositoryRoot, evidenceDirectory).split(path.sep).join("/");
  const match = relative.match(
    /^\.tmp\/macos-release\/((?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*))\/build-([1-9]\d*)\/evidence\/([0-9a-f-]+)$/u,
  );
  if (!match || !UUID_V7.test(match[3])) fail();
  return { buildNumber: match[2], eventId: match[3], releaseVersion: match[1] };
}

export async function stageMacOSReleaseEvidence({
  repositoryRoot,
  sourceDirectory,
  evidenceDirectory,
}) {
  const identity = stagedEvidenceIdentity(repositoryRoot, evidenceDirectory);
  if (
    typeof sourceDirectory !== "string" ||
    !path.isAbsolute(sourceDirectory) ||
    path.normalize(sourceDirectory) !== sourceDirectory ||
    sourceDirectory === evidenceDirectory
  ) {
    fail();
  }
  const sourceStatus = await lstat(sourceDirectory).catch(() => fail());
  if (!sourceStatus.isDirectory() || sourceStatus.isSymbolicLink()) fail();
  const sourceNames = (await readdir(sourceDirectory)).sort();
  if (!isDeepStrictEqual(sourceNames, [...INPUT_FILES].sort())) fail();
  const parent = path.dirname(evidenceDirectory);
  const temporary = path.join(parent, `.collect-${identity.eventId}`);
  for (const target of [evidenceDirectory, temporary]) {
    try {
      await lstat(target);
      fail("MACOS_RELEASE_EVIDENCE_EXISTS");
    } catch (error) {
      if (error instanceof Error && error.message === "MACOS_RELEASE_EVIDENCE_EXISTS") throw error;
      if (error?.code !== "ENOENT") fail();
    }
  }
  await mkdir(parent, { recursive: true, mode: 0o700 });
  await mkdir(temporary, { mode: 0o700 });
  let published = false;
  try {
    for (const name of INPUT_FILES) {
      await writeExclusive(
        path.join(temporary, name),
        await stableFileBytes(path.join(sourceDirectory, name)),
      );
    }
    await fsyncDirectory(temporary);
    await rename(temporary, evidenceDirectory);
    published = true;
    await fsyncDirectory(evidenceDirectory);
    await fsyncDirectory(parent);
    await validateStagedMacOSReleaseEvidence({ repositoryRoot, evidenceDirectory });
    return { evidenceDirectory, eventId: identity.eventId, outcome: "collected" };
  } catch (error) {
    await rm(temporary, { force: true, recursive: true }).catch(() => undefined);
    if (published) {
      await rm(evidenceDirectory, { force: true, recursive: true }).catch(() => undefined);
      await fsyncDirectory(parent).catch(() => undefined);
    }
    throw error;
  }
}

export async function validateStagedMacOSReleaseEvidence({ repositoryRoot, evidenceDirectory }) {
  const identity = await validateEvidenceDirectory(repositoryRoot, evidenceDirectory);
  const [candidateValue, metadataValue, privacyValue, runtimeValue, screenshotsValue] =
    await Promise.all(
      INPUT_FILES.map((name) => readStableJson(path.join(evidenceDirectory, name))),
    );
  for (const value of [
    candidateValue,
    metadataValue,
    privacyValue,
    runtimeValue,
    screenshotsValue,
  ]) {
    assertRedacted(value);
  }
  const candidate = validateCandidate(candidateValue);
  const metadata = validateMetadataSnapshot(metadataValue);
  const privacy = validateMacOSPrivacyEvidence(privacyValue);
  const runtimeEgress = validateMacOSRuntimeEgressEvidence(runtimeValue);
  const screenshots = validateScreenshotEvidence(screenshotsValue);
  if (
    candidate.releaseVersion !== identity.releaseVersion ||
    candidate.buildNumber !== identity.buildNumber ||
    privacy.marketingVersion !== candidate.releaseVersion ||
    privacy.buildNumber !== candidate.buildNumber ||
    privacy.productSourceCommit !== candidate.sourceCommit ||
    privacy.productSourceTree !== candidate.sourceTree ||
    privacy.distributionAppSha256 !== candidate.appSha256 ||
    privacy.packageSha256 !== candidate.packageSha256 ||
    runtimeEgress.marketingVersion !== candidate.releaseVersion ||
    runtimeEgress.buildNumber !== candidate.buildNumber ||
    runtimeEgress.productSourceCommit !== candidate.sourceCommit ||
    runtimeEgress.productSourceTree !== candidate.sourceTree ||
    runtimeEgress.appSha256 !== privacy.developmentAppSha256 ||
    screenshots.marketingVersion !== candidate.releaseVersion ||
    screenshots.buildNumber !== candidate.buildNumber ||
    screenshots.productSourceCommit !== candidate.sourceCommit ||
    screenshots.productSourceTree !== candidate.sourceTree ||
    screenshots.developmentSignedAppSha256 !== privacy.developmentAppSha256 ||
    screenshots.unsignedContentManifestSha256 !== privacy.unsignedContentManifestSha256 ||
    metadata.marketingVersion !== candidate.releaseVersion ||
    metadata.buildNumber !== candidate.buildNumber ||
    metadata.productSourceCommit !== candidate.sourceCommit ||
    metadata.productSourceTree !== candidate.sourceTree
  ) {
    fail();
  }
  const unlinked = [
    screenshots.candidateEventPath,
    screenshots.candidateEventSha256,
    screenshots.distributionPackageSha256,
  ].every((value) => value === null);
  if (!unlinked) fail();
  return {
    ...identity,
    candidate,
    metadata,
    privacy,
    runtimeEgress,
    screenshots,
  };
}

function candidateBuiltEvent(candidate, privacy) {
  return {
    appSha256: candidate.appSha256,
    buildNumber: candidate.buildNumber,
    kind: "candidate-built",
    marketingVersion: candidate.releaseVersion,
    packageSha256: candidate.packageSha256,
    productSourceCommit: candidate.sourceCommit,
    productSourceTree: candidate.sourceTree,
    unsignedContentManifestSha256: privacy.unsignedContentManifestSha256,
  };
}

function summary(evidence, eventPath) {
  return [
    "# Tammy macOS App Store candidate evidence",
    "",
    `- Version/build: ${evidence.releaseVersion} (${evidence.buildNumber})`,
    `- Product source commit: ${evidence.candidate.sourceCommit}`,
    `- Distribution app SHA-256: ${evidence.candidate.appSha256}`,
    `- Distribution package SHA-256: ${evidence.candidate.packageSha256}`,
    `- Candidate marker: ${eventPath}`,
    "- Screenshots: development-signed equivalent; five validated images",
    "- Privacy: no collected data, analytics, advertising, or tracking found",
    "- Runtime egress: active containment evidence passed",
    "",
    "Apple-controlled declarations and lifecycle outcomes remain operator-owned facts.",
    "",
  ].join("\n");
}

async function fsyncDirectory(directory) {
  const handle = await open(directory, fsConstants.O_RDONLY);
  try {
    await handle.sync();
  } finally {
    await handle.close();
  }
}

async function writeExclusive(file, bytes) {
  const handle = await open(file, "wx", 0o600).catch((error) => {
    if (error?.code === "EEXIST") fail("MACOS_RELEASE_EVIDENCE_EXISTS");
    throw error;
  });
  try {
    await handle.writeFile(bytes);
    await handle.sync();
  } finally {
    await handle.close();
  }
}

function eventFilename(now) {
  const value = now();
  if (!(value instanceof Date) || !Number.isFinite(value.valueOf())) fail();
  return `${value.toISOString().replaceAll(":", "-")}-candidate-built.json`;
}

export async function promoteMacOSReleaseEvidence({
  repositoryRoot,
  evidenceDirectory,
  mode,
  now = () => new Date(),
}) {
  if (!new Set(["check", "promote"]).has(mode) || typeof now !== "function") fail();
  const evidence = await validateStagedMacOSReleaseEvidence({ repositoryRoot, evidenceDirectory });
  const event = candidateBuiltEvent(evidence.candidate, evidence.privacy);
  const eventBytes = jsonBytes(event);
  const relativeEventPath = path.posix.join(
    "docs/release/records/macos",
    evidence.releaseVersion,
    `build-${evidence.buildNumber}`,
    "events",
    eventFilename(now),
  );
  const finalizedScreenshots = {
    ...evidence.screenshots,
    candidateEventPath: relativeEventPath,
    candidateEventSha256: sha256(eventBytes),
    distributionPackageSha256: evidence.candidate.packageSha256,
  };
  validateScreenshotEvidence(finalizedScreenshots);
  if (mode === "check") {
    return { destination: relativeEventPath, eventId: evidence.eventId, outcome: "validated" };
  }

  const buildRoot = path.join(
    repositoryRoot,
    "docs/release/records/macos",
    evidence.releaseVersion,
    `build-${evidence.buildNumber}`,
  );
  const candidateParent = path.join(buildRoot, "evidence/candidate");
  const destination = path.join(candidateParent, evidence.eventId);
  const temporary = path.join(candidateParent, `.promote-${evidence.eventId}`);
  const eventsDirectory = path.join(buildRoot, "events");
  const durableEventPath = path.join(repositoryRoot, ...relativeEventPath.split("/"));
  for (const target of [destination, temporary, durableEventPath]) {
    try {
      await lstat(target);
      fail("MACOS_RELEASE_EVIDENCE_EXISTS");
    } catch (error) {
      if (error instanceof Error && error.message === "MACOS_RELEASE_EVIDENCE_EXISTS") throw error;
      if (error?.code !== "ENOENT") fail();
    }
  }
  await mkdir(candidateParent, { recursive: true, mode: 0o700 });
  await mkdir(eventsDirectory, { recursive: true, mode: 0o700 });
  await mkdir(temporary, { mode: 0o700 });
  const values = {
    "candidate.json": evidence.candidate,
    "metadata-snapshot.json": evidence.metadata,
    "privacy-evidence.json": evidence.privacy,
    "runtime-egress.json": evidence.runtimeEgress,
    "screenshots.json": finalizedScreenshots,
    "summary.md": summary(evidence, relativeEventPath),
  };
  for (const name of OUTPUT_FILES) {
    const value = values[name];
    const bytes = typeof value === "string" ? Buffer.from(value) : jsonBytes(value);
    await writeExclusive(path.join(temporary, name), bytes);
  }
  await fsyncDirectory(temporary);
  await rename(temporary, destination);
  await fsyncDirectory(destination);
  await fsyncDirectory(candidateParent);
  await writeExclusive(durableEventPath, eventBytes);
  await fsyncDirectory(eventsDirectory);
  return { destination: relativeEventPath, eventId: evidence.eventId, outcome: "promoted" };
}

async function main() {
  const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const arguments_ = process.argv.slice(2);
  const modeIndex = arguments_.indexOf("--mode");
  const evidenceIndex = arguments_.indexOf("--evidence-dir");
  const sourceIndex = arguments_.indexOf("--source-dir");
  const mode = arguments_[modeIndex + 1];
  const evidenceDirectory = arguments_[evidenceIndex + 1];
  const collect = mode === "collect";
  if (
    modeIndex !== 0 ||
    evidenceIndex < 0 ||
    !path.isAbsolute(evidenceDirectory ?? "") ||
    arguments_.length !== 6 ||
    sourceIndex < 0 ||
    !path.isAbsolute(arguments_[sourceIndex + 1] ?? "") ||
    (!collect && !new Set(["check", "promote"]).has(mode))
  ) {
    fail("MACOS_RELEASE_EVIDENCE_ARGUMENT_INVALID");
  }
  const result = collect
    ? await stageMacOSReleaseEvidence({
        evidenceDirectory,
        repositoryRoot,
        sourceDirectory: arguments_[sourceIndex + 1],
      })
    : await promoteMacOSReleaseEvidence({ evidenceDirectory, mode, repositoryRoot });
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
