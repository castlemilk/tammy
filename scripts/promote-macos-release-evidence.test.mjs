import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import {
  promoteMacOSReleaseEvidence,
  stageMacOSReleaseEvidence,
  validateStagedMacOSReleaseEvidence,
} from "./promote-macos-release-evidence.mjs";

const sha = (digit) => digit.repeat(64);
const sourceCommit = "a".repeat(40);
const sourceTree = "b".repeat(40);
const eventId = "019fefd9-5d51-7c01-a20d-a43ad49f0afc";
const privacyUrl = "https://tammy-accounting.castlemilk.chatgpt.site/privacy";
const supportUrl = "https://tammy-accounting.castlemilk.chatgpt.site/support";

function candidate() {
  return {
    appSha256: sha("1"),
    buildNumber: "42",
    buildNumberReserved: true,
    packageSha256: sha("2"),
    privacyEvidencePassed: true,
    publicUrlsMatch: true,
    releaseVersion: "0.1.0",
    runtimeEgressEvidencePassed: true,
    screenshotsLinked: true,
    signingProfilePassed: true,
    sourceCommit,
    sourceTree,
  };
}

function privacyEvidence() {
  return {
    accessedApiReasons: [
      {
        category: "NSPrivacyAccessedAPICategoryFileTimestamp",
        manifestPath: "Contents/Resources/PrivacyInfo.xcprivacy",
        reasons: ["3B52.1", "C617.1"],
      },
    ],
    buildNumber: "42",
    developmentAppSha256: sha("3"),
    distributionAppSha256: sha("1"),
    embeddedHostnames: ["tammy-accounting.castlemilk.chatgpt.site"],
    embeddedPublicUrls: [privacyUrl, supportUrl],
    entitlements: [
      {
        keys: ["com.apple.security.app-sandbox", "com.apple.security.network.client"],
        path: "Contents/MacOS/Tammy",
        sha256: sha("4"),
      },
    ],
    marketingVersion: "0.1.0",
    nativePayloads: [
      {
        architectures: ["arm64"],
        kind: "executable",
        path: "Contents/MacOS/Tammy",
        sha256: sha("5"),
      },
    ],
    packageSha256: sha("2"),
    privacyManifests: [
      {
        accessedApiCategories: ["NSPrivacyAccessedAPICategoryFileTimestamp"],
        collectedDataTypeCount: 0,
        path: "Contents/Resources/PrivacyInfo.xcprivacy",
        sha256: sha("6"),
        tracking: false,
        trackingDomains: [],
      },
    ],
    productionPackages: [
      { license: "MIT", name: "react", representedPaths: ["/.vite/ui.js"], version: "19.2.7" },
    ],
    productSourceCommit: sourceCommit,
    productSourceTree: sourceTree,
    schemaVersion: 1,
    supplementalPrivacyReport: {
      checkedTool: "/usr/bin/xcodebuild",
      status: "not-supported-by-detected-toolchain",
      toolVersion: "Xcode 26.0",
    },
    unsignedContentManifestSha256: sha("7"),
  };
}

function runtimeEvidence() {
  return {
    appSha256: sha("3"),
    auditSamples: 4,
    buildNumber: "42",
    coreSha256: sha("8"),
    deniedDnsAttempts: 0,
    deniedNonLoopbackAttempts: 0,
    handoffs: [
      { occurredAt: "2026-08-31T00:00:01.000Z", url: privacyUrl, userGesture: true },
      { occurredAt: "2026-08-31T00:00:02.000Z", url: supportUrl, userGesture: true },
    ],
    helperSha256: sha("9"),
    listeners: [{ address: "127.0.0.1", owner: "authenticated-core", port: 43123 }],
    marketingVersion: "0.1.0",
    observationSamples: 10,
    observedNonLoopbackConnections: 0,
    processPaths: [
      "/Applications/Tammy.app/Contents/MacOS/Tammy",
      "/Applications/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core",
    ],
    productSourceCommit: sourceCommit,
    productSourceTree: sourceTree,
    schemaVersion: 1,
  };
}

function screenshots() {
  return {
    buildNumber: "42",
    candidateEventPath: null,
    candidateEventSha256: null,
    captureArtifactKind: "development-signed-app",
    capturedAt: "2026-08-31T00:00:03.000Z",
    developmentSignedAppSha256: sha("3"),
    dimensions: { height: 900, width: 1440 },
    distributionPackageSha256: null,
    fixturePath: "apps/desktop/release/macos/screenshots/fixture.json",
    fixtureSha256: sha("a"),
    images: Array.from({ length: 5 }, (_, index) => ({
      accessibilitySnapshot: `0${index + 1}.accessibility.txt`,
      accessibilitySnapshotSha256: String(index + 1).repeat(64),
      caption: `Tammy screen ${index + 1}`,
      filename: `0${index + 1}.png`,
      sha256: String(index + 2).repeat(64),
    })),
    locale: "en-AU",
    marketingVersion: "0.1.0",
    productSourceCommit: sourceCommit,
    productSourceTree: sourceTree,
    schemaVersion: 1,
    unsignedContentManifestSha256: sha("7"),
  };
}

function metadataSnapshot() {
  return {
    buildNumber: "42",
    exportCompliance: "exempt",
    marketingVersion: "0.1.0",
    metadataSha256: sha("b"),
    policySha256: sha("c"),
    productSourceCommit: sourceCommit,
    productSourceTree: sourceTree,
    publicSiteDeploymentId: "tammy-site-2026-08-31",
    publicSiteOrigin: "https://tammy-accounting.castlemilk.chatgpt.site",
    publicSiteVersion: "1",
    schemaVersion: 1,
  };
}

async function fixture(context) {
  const repositoryRoot = await mkdtemp(path.join(tmpdir(), "tammy-promote-repository-"));
  context.after(() => rm(repositoryRoot, { force: true, recursive: true }));
  const evidenceDirectory = path.join(
    repositoryRoot,
    ".tmp/macos-release/0.1.0/build-42/evidence",
    eventId,
  );
  await mkdir(evidenceDirectory, { recursive: true });
  const files = {
    "candidate.json": candidate(),
    "metadata-snapshot.json": metadataSnapshot(),
    "privacy-evidence.json": privacyEvidence(),
    "runtime-egress.json": runtimeEvidence(),
    "screenshots.json": screenshots(),
  };
  await Promise.all(
    Object.entries(files).map(([name, value]) =>
      writeFile(path.join(evidenceDirectory, name), `${JSON.stringify(value, null, 2)}\n`),
    ),
  );
  return { evidenceDirectory, repositoryRoot };
}

test("validates one exact candidate-bound staged evidence set", async (context) => {
  const input = await fixture(context);
  const result = await validateStagedMacOSReleaseEvidence(input);
  assert.equal(result.candidate.releaseVersion, "0.1.0");
  assert.equal(result.eventId, eventId);

  await writeFile(
    path.join(input.evidenceDirectory, "candidate.json"),
    `${JSON.stringify({ ...candidate(), packageSha256: sha("f") })}\n`,
  );
  await assert.rejects(validateStagedMacOSReleaseEvidence(input), /MACOS_RELEASE_EVIDENCE_INVALID/);
});

test("collects one exact source set into an exclusive durable local staging directory", async (context) => {
  const input = await fixture(context);
  const evidenceDirectory = path.join(
    input.repositoryRoot,
    ".tmp/macos-release/0.1.0/build-42/evidence/019fefd9-5d51-7c02-a20d-a43ad49f0afc",
  );
  const result = await stageMacOSReleaseEvidence({
    evidenceDirectory,
    repositoryRoot: input.repositoryRoot,
    sourceDirectory: input.evidenceDirectory,
  });
  assert.equal(result.outcome, "collected");
  assert.equal(
    (
      await validateStagedMacOSReleaseEvidence({
        evidenceDirectory,
        repositoryRoot: input.repositoryRoot,
      })
    ).candidate.packageSha256,
    sha("2"),
  );
  await assert.rejects(
    stageMacOSReleaseEvidence({
      evidenceDirectory,
      repositoryRoot: input.repositoryRoot,
      sourceDirectory: input.evidenceDirectory,
    }),
    /MACOS_RELEASE_EVIDENCE_EXISTS/,
  );
});

test("promotes atomically with the exclusive candidate event last", async (context) => {
  const input = await fixture(context);
  const checked = await promoteMacOSReleaseEvidence({
    ...input,
    mode: "check",
    now: () => new Date("2026-08-31T04:05:06.789Z"),
  });
  assert.equal(checked.outcome, "validated");

  const promoted = await promoteMacOSReleaseEvidence({
    ...input,
    mode: "promote",
    now: () => new Date("2026-08-31T04:05:06.789Z"),
  });
  assert.equal(promoted.outcome, "promoted");
  const evidenceRoot = path.join(
    input.repositoryRoot,
    "docs/release/records/macos/0.1.0/build-42/evidence/candidate",
    eventId,
  );
  const eventPath = path.join(
    input.repositoryRoot,
    "docs/release/records/macos/0.1.0/build-42/events/2026-08-31T04-05-06.789Z-candidate-built.json",
  );
  const event = JSON.parse(await readFile(eventPath, "utf8"));
  assert.equal(event.packageSha256, sha("2"));
  assert.equal(
    JSON.parse(
      await readFile(path.join(evidenceRoot, "screenshots.json"), "utf8"),
    ).candidateEventPath.endsWith("candidate-built.json"),
    true,
  );
  await assert.rejects(
    promoteMacOSReleaseEvidence({ ...input, mode: "promote" }),
    /MACOS_RELEASE_EVIDENCE_EXISTS/,
  );
});
