import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  assertMacOSReleaseMetadata,
  inspectMacOSStoreRepository,
  readMacOSRepositoryPlist,
  readPngDimensions,
  validateMacOSProvisioningProfile,
  validateMacOSReleaseEnvironment,
  validateMacOSStoreMetadata,
  validateMacOSStorePlists,
} from "./check-macos-store.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

test("repository plist reader is portable and never shells out to plutil", async () => {
  let reads = 0;
  const privacy = await readMacOSRepositoryPlist(
    "/portable/PrivacyInfo.xcprivacy",
    async (file, encoding) => {
      reads += 1;
      assert.equal(file, "/portable/PrivacyInfo.xcprivacy");
      assert.equal(encoding, "utf8");
      return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>NSPrivacyTracking</key><false/><key>NSPrivacyCollectedDataTypes</key><array/></dict></plist>`;
    },
  );

  assert.equal(reads, 1);
  assert.deepEqual(privacy.NSPrivacyTracking, false);
  assert.deepEqual(privacy.NSPrivacyCollectedDataTypes, []);
});

test("repository inspection binds Tammy identity, store resources and operator gates", async () => {
  const result = await inspectMacOSStoreRepository(root);

  assert.equal(result.appBundleId, "com.tammy.desktop");
  assert.equal(result.category, "public.app-category.finance");
  assert.equal(result.version, "0.1.0");
  assert.equal(result.icon.width, 1024);
  assert.equal(result.icon.height, 1024);
  assert.deepEqual(result.operatorRequirements, [
    "app-store-connect-record",
    "certificates-and-profiles",
    "export-compliance",
    "legal-and-commercial-metadata",
    "privacy-and-support-urls",
    "signed-build-privacy-report",
    "screenshots",
  ]);
});

test("PNG dimension reader rejects malformed and non-square app icons", async () => {
  const icon = await readFile(path.join(root, "apps/desktop/assets/icon-source.png"));
  assert.deepEqual(readPngDimensions(icon), { height: 1024, width: 1024 });
  assert.throws(() => readPngDimensions(Buffer.from("not a png")), /MACOS_STORE_ICON_INVALID/);

  const changed = Buffer.from(icon);
  changed.writeUInt32BE(1000, 20);
  assert.deepEqual(readPngDimensions(changed), { height: 1000, width: 1024 });
});

test("distribution inputs require absolute paths, explicit compliance and a positive build", () => {
  const valid = {
    TAMMY_MACOS_BUILD_NUMBER: "42",
    TAMMY_MACOS_EXPORT_COMPLIANCE: "exempt",
    TAMMY_MACOS_INSTALLER_IDENTITY: "Mac Installer Distribution: Tammy Pty Ltd (ABCDE12345)",
    TAMMY_MACOS_PROVISIONING_PROFILE: "/private/tmp/tammy.provisionprofile",
    TAMMY_MACOS_PRIVACY_POLICY_URL: "https://example.com/tammy/privacy",
    TAMMY_MACOS_SIGNING_IDENTITY: "Apple Distribution: Tammy Pty Ltd (ABCDE12345)",
    TAMMY_MACOS_SIGNING_MODE: "distribution",
    TAMMY_MACOS_SUPPORT_URL: "https://example.com/tammy/support",
    TAMMY_MACOS_TEAM_ID: "ABCDE12345",
  };
  assert.deepEqual(validateMacOSReleaseEnvironment(valid), {
    buildNumber: "42",
    mode: "distribution",
  });

  for (const environment of [
    { ...valid, TAMMY_MACOS_BUILD_NUMBER: "0" },
    { ...valid, TAMMY_MACOS_PROVISIONING_PROFILE: "relative.provisionprofile" },
    { ...valid, TAMMY_MACOS_EXPORT_COMPLIANCE: "unknown" },
    { ...valid, TAMMY_MACOS_INSTALLER_IDENTITY: "" },
    { ...valid, TAMMY_MACOS_SIGNING_IDENTITY: "Apple Development: Tammy Pty Ltd (ABCDE12345)" },
    { ...valid, TAMMY_MACOS_SIGNING_IDENTITY: "Apple Distribution: Tammy Pty Ltd (OTHER12345)" },
    {
      ...valid,
      TAMMY_MACOS_INSTALLER_IDENTITY: "Developer ID Installer: Tammy Pty Ltd (ABCDE12345)",
    },
  ]) {
    assert.throws(
      () => validateMacOSReleaseEnvironment(environment),
      /MACOS_RELEASE_INPUT_INVALID/,
    );
  }
});

test("provisioning profiles bind the team, app identifier, mode, and expiry", () => {
  const development = {
    ApplicationIdentifierPrefix: ["LEGACY1234"],
    Entitlements: {
      "com.apple.application-identifier": "LEGACY1234.com.tammy.desktop",
      "com.apple.developer.team-identifier": "ABCDE12345",
      "get-task-allow": true,
    },
    ExpirationDate: "2027-08-11T00:00:00.000Z",
    ProvisionedDevices: ["device-id"],
    TeamIdentifier: ["ABCDE12345"],
  };
  const options = {
    mode: "development",
    now: new Date("2026-08-11T00:00:00.000Z"),
    teamID: "ABCDE12345",
  };
  assert.doesNotThrow(() => validateMacOSProvisioningProfile(development, options));
  assert.doesNotThrow(() =>
    validateMacOSProvisioningProfile(
      {
        ...development,
        Entitlements: { ...development.Entitlements, "get-task-allow": false },
        ProvisionedDevices: undefined,
      },
      { ...options, mode: "distribution" },
    ),
  );
  for (const profile of [
    { ...development, TeamIdentifier: ["OTHER12345"] },
    {
      ...development,
      Entitlements: {
        ...development.Entitlements,
        "com.apple.application-identifier": "LEGACY1234.com.other.app",
      },
    },
    { ...development, ApplicationIdentifierPrefix: ["OTHER12345"] },
    { ...development, ExpirationDate: "2026-08-10T00:00:00.000Z" },
    { ...development, ProvisionedDevices: undefined },
    { ...development, ProvisionsAllDevices: true },
  ]) {
    assert.throws(
      () => validateMacOSProvisioningProfile(profile, options),
      /MACOS_RELEASE_PROVISIONING_PROFILE_INVALID/,
    );
  }
});

test("development signing does not require an installer identity", () => {
  assert.deepEqual(
    validateMacOSReleaseEnvironment({
      TAMMY_MACOS_BUILD_NUMBER: "7",
      TAMMY_MACOS_EXPORT_COMPLIANCE: "non-exempt",
      TAMMY_MACOS_PROVISIONING_PROFILE: "/private/tmp/tammy.provisionprofile",
      TAMMY_MACOS_PRIVACY_POLICY_URL: "https://example.com/tammy/privacy",
      TAMMY_MACOS_SIGNING_IDENTITY: "Apple Development: Tammy Pty Ltd (ABCDE12345)",
      TAMMY_MACOS_SIGNING_MODE: "development",
      TAMMY_MACOS_SUPPORT_URL: "https://example.com/tammy/support",
      TAMMY_MACOS_TEAM_ID: "ABCDE12345",
    }),
    { buildNumber: "7", mode: "development" },
  );
});

test("store metadata distinguishes the repository template from a release candidate", async () => {
  const template = await readFile(
    path.join(root, "apps/desktop/release/macos/store-metadata.md"),
    "utf8",
  );
  assert.deepEqual(validateMacOSStoreMetadata(template), { complete: false });
  const complete = template
    .replaceAll("OPERATOR_REQUIRED", "approved-value")
    .replace(
      "**Privacy policy URL:** `approved-value`",
      "**Privacy policy URL:** `https://example.com/tammy/privacy`",
    )
    .replace(
      "**Support URL:** `approved-value`",
      "**Support URL:** `https://example.com/tammy/support`",
    );
  assert.deepEqual(validateMacOSStoreMetadata(complete), {
    complete: true,
    privacyPolicy: "https://example.com/tammy/privacy",
    support: "https://example.com/tammy/support",
  });
  assert.throws(
    () => validateMacOSStoreMetadata(template.replaceAll("OPERATOR_REQUIRED", "approved-value")),
    /MACOS_STORE_REPOSITORY_INVALID/,
  );
});

test("release metadata URLs must exactly match the configured public links", () => {
  const metadata = {
    complete: true,
    privacyPolicy: "https://example.com/tammy/privacy",
    support: "https://example.com/tammy/support",
  };
  assert.doesNotThrow(() =>
    assertMacOSReleaseMetadata(metadata, {
      TAMMY_MACOS_PRIVACY_POLICY_URL: metadata.privacyPolicy,
      TAMMY_MACOS_SUPPORT_URL: metadata.support,
    }),
  );
  assert.throws(
    () =>
      assertMacOSReleaseMetadata(metadata, {
        TAMMY_MACOS_PRIVACY_POLICY_URL: "https://example.com/other",
        TAMMY_MACOS_SUPPORT_URL: metadata.support,
      }),
    /MACOS_RELEASE_METADATA_MISMATCH/,
  );
});

test("store plist validation requires exact sandbox inheritance and privacy values", () => {
  const valid = {
    appEntitlements: {
      "com.apple.security.app-sandbox": true,
      "com.apple.security.files.user-selected.read-only": true,
      "com.apple.security.network.client": true,
      "com.apple.security.network.server": true,
    },
    childEntitlements: {
      "com.apple.security.app-sandbox": true,
      "com.apple.security.inherit": true,
    },
    coreEntitlements: {
      "com.apple.security.app-sandbox": true,
      "com.apple.security.inherit": true,
    },
    privacy: {
      NSPrivacyAccessedAPITypes: [
        {
          NSPrivacyAccessedAPIType: "NSPrivacyAccessedAPICategoryFileTimestamp",
          NSPrivacyAccessedAPITypeReasons: ["C617.1", "3B52.1"],
        },
      ],
      NSPrivacyCollectedDataTypes: [],
      NSPrivacyTracking: false,
      NSPrivacyTrackingDomains: [],
    },
  };

  assert.doesNotThrow(() => validateMacOSStorePlists(valid));
  assert.throws(
    () =>
      validateMacOSStorePlists({
        ...valid,
        coreEntitlements: { ...valid.coreEntitlements, "com.apple.security.network.server": true },
      }),
    /MACOS_STORE_REPOSITORY_INVALID/,
  );
  assert.throws(
    () =>
      validateMacOSStorePlists({
        ...valid,
        appEntitlements: { ...valid.appEntitlements, "com.apple.security.network.server": false },
      }),
    /MACOS_STORE_REPOSITORY_INVALID/,
  );
  assert.throws(
    () =>
      validateMacOSStorePlists({
        ...valid,
        privacy: { ...valid.privacy, NSPrivacyTracking: true },
      }),
    /MACOS_STORE_REPOSITORY_INVALID/,
  );
});
