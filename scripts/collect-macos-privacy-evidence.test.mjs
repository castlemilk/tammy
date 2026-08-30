import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import {
  collectMacOSPrivacyEvidence,
  inspectBundledProductionPackages,
  validateMacOSPrivacyEvidence,
} from "./collect-macos-privacy-evidence.mjs";

const require = createRequire(import.meta.url);

const sha = (digit) => digit.repeat(64);
const identity = {
  buildNumber: "42",
  developmentAppSha256: sha("a"),
  distributionAppSha256: sha("b"),
  marketingVersion: "0.1.0",
  packageSha256: sha("c"),
  productSourceCommit: "d".repeat(40),
  productSourceTree: "e".repeat(40),
  unsignedContentManifestSha256: sha("f"),
};

function evidence() {
  return {
    ...identity,
    accessedApiReasons: [
      {
        category: "NSPrivacyAccessedAPICategoryFileTimestamp",
        manifestPath: "Contents/Resources/PrivacyInfo.xcprivacy",
        reasons: ["3B52.1", "C617.1"],
      },
    ],
    embeddedHostnames: ["tammy-accounting.castlemilk.chatgpt.site"],
    embeddedPublicUrls: [
      "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
      "https://tammy-accounting.castlemilk.chatgpt.site/support",
    ],
    entitlements: [
      {
        keys: ["com.apple.security.app-sandbox", "com.apple.security.network.client"],
        path: "Contents/MacOS/Tammy",
        sha256: sha("1"),
      },
    ],
    nativePayloads: [
      {
        architectures: ["arm64"],
        kind: "executable",
        path: "Contents/MacOS/Tammy",
        sha256: sha("2"),
      },
    ],
    privacyManifests: [
      {
        accessedApiCategories: ["NSPrivacyAccessedAPICategoryFileTimestamp"],
        collectedDataTypeCount: 0,
        path: "Contents/Resources/PrivacyInfo.xcprivacy",
        sha256: sha("3"),
        tracking: false,
        trackingDomains: [],
      },
    ],
    productionPackages: [
      {
        license: "MIT",
        name: "react",
        representedPaths: ["/node_modules/react/index.js"],
        version: "19.2.7",
      },
    ],
    schemaVersion: 1,
    supplementalPrivacyReport: {
      checkedTool: "/usr/bin/xcodebuild",
      status: "not-supported-by-detected-toolchain",
      toolVersion: "Xcode 26.0",
    },
  };
}

test("accepts only complete candidate-bound redacted privacy evidence", () => {
  assert.deepEqual(validateMacOSPrivacyEvidence(evidence()), evidence());
  for (const mutate of [
    (value) => ({ ...value, packageSha256: "stale" }),
    (value) => ({ ...value, environment: { PATH: "/secret" } }),
    (value) => ({
      ...value,
      productionPackages: [{ ...value.productionPackages[0], name: "@sentry/electron" }],
    }),
    (value) => ({ ...value, privacyManifests: [] }),
    (value) => ({ ...value, embeddedHostnames: ["analytics.example.com"] }),
  ]) {
    assert.throws(
      () => validateMacOSPrivacyEvidence(mutate(evidence())),
      /MACOS_PRIVACY_EVIDENCE_INVALID/,
    );
  }
});

test("collects bounded exact-artifact facts and rejects resource drift", async () => {
  const calls = [];
  const collected = await collectMacOSPrivacyEvidence(
    {
      ...identity,
      developmentApp: "/release/development/Tammy.app",
      distributionApp: "/release/distribution/Tammy.app",
      lockfilePath: "/repo/pnpm-lock.yaml",
      packagePath: "/release/Tammy.pkg",
      unsignedContentManifestPath: "/release/unsigned-content.json",
    },
    {
      authenticateArtifacts: async (input) => {
        calls.push(["authenticate", input]);
        return identity;
      },
      inspectArtifacts: async () => evidence(),
    },
  );
  assert.equal(calls.length, 1);
  assert.deepEqual(collected, evidence());

  await assert.rejects(
    collectMacOSPrivacyEvidence(
      {
        ...identity,
        developmentApp: "/release/development/Tammy.app",
        distributionApp: "/release/distribution/Tammy.app",
        lockfilePath: "/repo/pnpm-lock.yaml",
        packagePath: "/release/Tammy.pkg",
        unsignedContentManifestPath: "/release/unsigned-content.json",
      },
      {
        authenticateArtifacts: async () => ({ ...identity, distributionAppSha256: sha("9") }),
        inspectArtifacts: async () => evidence(),
      },
    ),
    /MACOS_PRIVACY_ARTIFACT_CHANGED/,
  );
});

test("binds a bundled ASAR to the pinned production manifest and license metadata", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "tammy-privacy-packages-"));
  try {
    const source = path.join(directory, "source");
    const lockfilePath = path.join(directory, "pnpm-lock.yaml");
    const desktop = path.join(directory, "apps/desktop");
    await Promise.all([
      mkdir(path.join(source, ".vite/build"), { recursive: true }),
      mkdir(path.join(source, ".vite/renderer"), { recursive: true }),
      mkdir(path.join(desktop, "node_modules/react"), { recursive: true }),
    ]);
    await Promise.all([
      writeFile(path.join(source, ".vite/build/main.js"), "export const react = true;\n"),
      writeFile(path.join(source, ".vite/renderer/index.js"), "export const ui = true;\n"),
      writeFile(path.join(source, "package.json"), '{"name":"tammy-fixture"}\n'),
      writeFile(path.join(desktop, "package.json"), '{"dependencies":{"react":"19.2.7"}}\n'),
      writeFile(
        path.join(desktop, "node_modules/react/package.json"),
        '{"name":"react","version":"19.2.7","license":"MIT"}\n',
      ),
      writeFile(
        lockfilePath,
        "importers:\n  apps/desktop:\n    dependencies:\n      react:\n        specifier: 19.2.7\n        version: 19.2.7\n",
      ),
    ]);
    const asarPath = path.join(directory, "app.asar");
    const forgeRequire = createRequire(
      require.resolve("@electron-forge/cli/package.json", { paths: ["./apps/desktop"] }),
    );
    await forgeRequire("@electron/asar").createPackage(source, asarPath);

    assert.deepEqual(await inspectBundledProductionPackages({ asarPath, lockfilePath }), [
      {
        license: "MIT",
        name: "react",
        representedPaths: ["/.vite/build/main.js", "/.vite/renderer/index.js"],
        version: "19.2.7",
      },
    ]);
  } finally {
    await rm(directory, { force: true, recursive: true });
  }
});
