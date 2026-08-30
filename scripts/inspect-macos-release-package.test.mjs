import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { inspectMacOSReleasePackage } from "./inspect-macos-release-package.mjs";

const sourceCommit = "a".repeat(40);
const sourceTree = "b".repeat(40);
const appSha256 = "c".repeat(64);

async function fixture(context) {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-package-inspection-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  const packagePath = path.join(root, "Tammy-0.1.0-build.42.pkg");
  const packageBytes = Buffer.from("synthetic immutable package\n");
  const packageSha256 = createHash("sha256").update(packageBytes).digest("hex");
  await writeFile(packagePath, packageBytes);
  const recordDirectory = path.join(root, "records/0.1.0/build-42");
  const eventId = "019fefd9-5d51-7c01-a20d-a43ad49f0afc";
  const candidateRoot = path.join(recordDirectory, "evidence/candidate", eventId);
  const events = path.join(recordDirectory, "events");
  await Promise.all([
    mkdir(candidateRoot, { recursive: true }),
    mkdir(events, { recursive: true }),
  ]);
  const candidate = {
    appSha256,
    buildNumber: "42",
    buildNumberReserved: true,
    packageSha256,
    privacyEvidencePassed: true,
    publicUrlsMatch: true,
    releaseVersion: "0.1.0",
    runtimeEgressEvidencePassed: true,
    screenshotsLinked: true,
    signingProfilePassed: true,
    sourceCommit,
    sourceTree,
  };
  await writeFile(path.join(candidateRoot, "candidate.json"), `${JSON.stringify(candidate)}\n`);
  await writeFile(
    path.join(events, "2026-08-31T05-00-00.000Z-candidate-built.json"),
    `${JSON.stringify({ appSha256, buildNumber: "42", kind: "candidate-built", marketingVersion: "0.1.0", packageSha256, productSourceCommit: sourceCommit, productSourceTree: sourceTree, unsignedContentManifestSha256: "d".repeat(64) })}\n`,
  );
  return { packagePath, packageSha256, recordDirectory, verifyDurability: async () => true };
}

test("authenticates an archived package against one exact durable candidate record", async (context) => {
  const input = await fixture(context);
  const result = await inspectMacOSReleasePackage({
    ...input,
    inspectArchive: async () => ({
      appBundleIdentifier: "com.tammy.desktop",
      appSha256,
      architectures: ["arm64"],
      buildNumber: "42",
      exportCompliance: "exempt",
      gatekeeper: "accepted",
      helperIdentifiers: [
        "com.tammy.desktop.helper",
        "com.tammy.desktop.helper.GPU",
        "com.tammy.desktop.helper.Plugin",
        "com.tammy.desktop.helper.Renderer",
      ],
      installerSignature: "valid",
      marketingVersion: "0.1.0",
      minimumMacOSVersion: "14.0",
      privacyPolicy: "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
      support: "https://tammy-accounting.castlemilk.chatgpt.site/support",
    }),
  });
  assert.equal(result.packageSha256, input.packageSha256);
  assert.equal(result.candidateEvent, "events/2026-08-31T05-00-00.000Z-candidate-built.json");
});

test("rejects an archive or inspection that disagrees with the durable record", async (context) => {
  const input = await fixture(context);
  await assert.rejects(
    inspectMacOSReleasePackage({
      ...input,
      inspectArchive: async () => ({}),
      verifyDurability: async () => false,
    }),
    /MACOS_RELEASE_PACKAGE_MISMATCH/,
  );
  await assert.rejects(
    inspectMacOSReleasePackage({
      ...input,
      packageSha256: "f".repeat(64),
      inspectArchive: async () => ({}),
    }),
    /MACOS_RELEASE_PACKAGE_MISMATCH/,
  );
  await assert.rejects(
    inspectMacOSReleasePackage({
      ...input,
      inspectArchive: async () => ({
        appBundleIdentifier: "com.attacker.app",
        appSha256,
        architectures: ["arm64"],
        buildNumber: "42",
        exportCompliance: "exempt",
        gatekeeper: "accepted",
        helperIdentifiers: [],
        installerSignature: "valid",
        marketingVersion: "0.1.0",
        minimumMacOSVersion: "14.0",
        privacyPolicy: "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
        support: "https://tammy-accounting.castlemilk.chatgpt.site/support",
      }),
    }),
    /MACOS_RELEASE_PACKAGE_MISMATCH/,
  );
});
