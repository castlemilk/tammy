import assert from "node:assert/strict";
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  assertMacOSReleaseMetadata,
  inspectMacOSStoreRepository,
  readMacOSProvisioningProfilePlist,
  readMacOSRepositoryPlist,
  readPngDimensions,
  validateMacOSProvisioningProfile,
  validateMacOSReleaseEnvironment,
  validateMacOSStoreMetadata,
  validateMacOSStorePlists,
  validateCompanyControllerAttestation,
  validateMacOSStoreIdentity,
} from "./check-macos-store.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const validStoreIdentity = {
  schemaVersion: 1,
  appStoreName: "Tammy Accounting",
  installedName: "Tammy",
  bundleIdentifier: "com.tammy.desktop",
  publisher: "Gamma Systems Pty Ltd",
  supportEmail: "ben.ebsworth@gmail.com",
  locale: "en-AU",
  primaryCategory: "Finance",
  secondaryCategory: "Business",
  minimumMacOSVersion: "14.0",
  architectures: ["arm64"],
  copyright: "© 2026 Gamma Systems Pty Ltd",
  capabilityBoundary: {
    reporting: "preparation-only",
    atoLodgement: "not-lodged",
  },
};

test("accepts the canonical Gamma Systems release identity", () => {
  assert.deepEqual(validateMacOSStoreIdentity(validStoreIdentity), validStoreIdentity);
});

test("release identity requires the exact shape and capability boundary", () => {
  for (const identity of [
    Object.fromEntries(Object.entries(validStoreIdentity).filter(([key]) => key !== "locale")),
    { ...validStoreIdentity, extra: true },
    {
      ...validStoreIdentity,
      capabilityBoundary: { reporting: "preparation-only" },
    },
    {
      ...validStoreIdentity,
      capabilityBoundary: { ...validStoreIdentity.capabilityBoundary, atoLodgement: "lodged" },
    },
    { ...validStoreIdentity, architectures: ["arm64", "x64"] },
  ]) {
    assert.throws(() => validateMacOSStoreIdentity(identity), /MACOS_STORE_IDENTITY_INVALID/);
  }
});

for (const [key, value] of [
  ["publisher", "Ben Ebsworth"],
  ["supportEmail", "support@example.com"],
  ["minimumMacOSVersion", "13.0"],
  ["architectures", ["x64"]],
]) {
  test(`rejects release identity drift in ${key}`, () => {
    assert.throws(
      () => validateMacOSStoreIdentity({ ...validStoreIdentity, [key]: value }),
      /MACOS_STORE_IDENTITY_INVALID/,
    );
  });
}

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
  assert.deepEqual(result.identity, validStoreIdentity);
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
  assert.deepEqual(result.blockers, []);
});

test("repository inspection rejects installed-name drift from the desktop product name", async () => {
  const fixtureRoot = await mkdtemp(path.join(tmpdir(), "tammy-macos-identity-test-"));
  try {
    await mkdir(path.join(fixtureRoot, "apps"), { recursive: true });
    await mkdir(path.join(fixtureRoot, "docs"), { recursive: true });
    await Promise.all([
      cp(path.join(root, "apps", "desktop"), path.join(fixtureRoot, "apps", "desktop"), {
        recursive: true,
      }),
      cp(path.join(root, "docs", "development"), path.join(fixtureRoot, "docs", "development"), {
        recursive: true,
      }),
      cp(path.join(root, "docs", "release"), path.join(fixtureRoot, "docs", "release"), {
        recursive: true,
      }),
      cp(path.join(root, "README.md"), path.join(fixtureRoot, "README.md")),
    ]);
    const packagePath = path.join(fixtureRoot, "apps", "desktop", "package.json");
    const desktopPackage = JSON.parse(await readFile(packagePath, "utf8"));
    await writeFile(packagePath, `${JSON.stringify({ ...desktopPackage, productName: "Tammy Drift" }, null, 2)}\n`);

    await assert.rejects(
      () => inspectMacOSStoreRepository(fixtureRoot),
      /MACOS_STORE_IDENTITY_MISMATCH/,
    );
  } finally {
    await rm(fixtureRoot, { force: true, recursive: true });
  }
});

test("company-controller attestations require the exact redacted accountable record", () => {
  const valid = {
    schemaVersion: 1,
    kind: "publisher-controller-authority",
    company: "Gamma Systems Pty Ltd",
    accountablePerson: "Ben Ebsworth",
    controlsPrivacyPolicy: true,
    controlsSupportAddress: true,
    supportEmail: "ben.ebsworth@gmail.com",
    confirmedAt: "2026-08-30T00:00:00.000Z",
    evidenceReference: "user-confirmation-in-task",
  };

  assert.doesNotThrow(() => validateCompanyControllerAttestation(valid));

  for (const attestation of [
    Object.fromEntries(Object.entries(valid).filter(([key]) => key !== "confirmedAt")),
    { ...valid, controlsPrivacyPolicy: false },
    { ...valid, controlsSupportAddress: false },
    { ...valid, company: "Tammy Pty Ltd" },
    { ...valid, supportEmail: "support@example.com" },
    { ...valid, confirmedAt: "not-a-time" },
    { ...valid, confirmedAt: "2026-02-30T00:00:00Z" },
    { ...valid, extra: true },
    { ...valid, apiToken: "redacted" },
  ]) {
    assert.throws(
      () => validateCompanyControllerAttestation(attestation),
      /MACOS_STORE_COMPANY_AUTHORITY_INVALID/,
    );
  }
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
  assert.deepEqual(
    validateMacOSReleaseEnvironment({
      ...valid,
      TAMMY_MACOS_SIGNING_IDENTITY:
        "3rd Party Mac Developer Application: Tammy Pty Ltd (ABCDE12345)",
    }),
    { buildNumber: "42", mode: "distribution" },
  );

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
      "com.apple.security.application-groups": ["ABCDE12345.com.tammy.desktop"],
      "com.apple.developer.team-identifier": "ABCDE12345",
      "get-task-allow": true,
      "keychain-access-groups": ["LEGACY1234.com.tammy.desktop.sbr"],
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
  const appStore = {
    ...development,
    Entitlements: {
      "com.apple.application-identifier": "LEGACY1234.com.tammy.desktop",
      "com.apple.security.application-groups": ["ABCDE12345.com.tammy.desktop"],
      "com.apple.developer.team-identifier": "ABCDE12345",
      "keychain-access-groups": ["LEGACY1234.com.tammy.desktop.sbr"],
    },
    ProvisionedDevices: undefined,
  };
  assert.doesNotThrow(() =>
    validateMacOSProvisioningProfile(appStore, { ...options, mode: "distribution" }),
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
    {
      ...development,
      Entitlements: {
        ...development.Entitlements,
        "com.apple.security.application-groups": [],
      },
    },
    {
      ...development,
      Entitlements: {
        ...development.Entitlements,
        "com.apple.security.application-groups": ["ABCDE12345.com.other.app"],
      },
    },
    {
      ...development,
      Entitlements: {
        ...development.Entitlements,
        "com.apple.security.application-groups": [
          "ABCDE12345.com.tammy.desktop",
          "ABCDE12345.com.other.app",
        ],
      },
    },
    {
      ...development,
      Entitlements: Object.fromEntries(
        Object.entries(development.Entitlements).filter(
          ([key]) => key !== "keychain-access-groups",
        ),
      ),
    },
    {
      ...development,
      Entitlements: {
        ...development.Entitlements,
        "keychain-access-groups": ["LEGACY1234.com.other.app"],
      },
    },
    {
      ...development,
      Entitlements: {
        ...development.Entitlements,
        "keychain-access-groups": ["LEGACY1234.com.tammy.desktop.sbr", "LEGACY1234.com.other.app"],
      },
    },
  ]) {
    assert.throws(
      () => validateMacOSProvisioningProfile(profile, options),
      /MACOS_RELEASE_PROVISIONING_PROFILE_INVALID/,
    );
  }
});

test("decoded Apple profiles retain validation fields without converting certificate data", async () => {
  const temporaryRoot = await mkdtemp(path.join(tmpdir(), "tammy-profile-test-"));
  const profile = path.join(temporaryRoot, "profile.plist");
  try {
    await writeFile(
      profile,
      `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>ApplicationIdentifierPrefix</key><array><string>WFTX6CN23F</string></array>
<key>DeveloperCertificates</key><array><data>AQID</data></array>
<key>Entitlements</key><dict>
<key>com.apple.application-identifier</key><string>WFTX6CN23F.com.tammy.desktop</string>
<key>com.apple.security.application-groups</key><array><string>WFTX6CN23F.com.tammy.desktop</string></array>
<key>com.apple.developer.team-identifier</key><string>WFTX6CN23F</string>
<key>keychain-access-groups</key><array><string>WFTX6CN23F.com.tammy.desktop.sbr</string></array>
</dict>
<key>ExpirationDate</key><date>2027-05-13T11:58:20Z</date>
<key>TeamIdentifier</key><array><string>WFTX6CN23F</string></array>
</dict></plist>`,
      { mode: 0o600 },
    );

    assert.deepEqual(await readMacOSProvisioningProfilePlist(profile), {
      ApplicationIdentifierPrefix: ["WFTX6CN23F"],
      Entitlements: {
        "com.apple.application-identifier": "WFTX6CN23F.com.tammy.desktop",
        "com.apple.security.application-groups": ["WFTX6CN23F.com.tammy.desktop"],
        "com.apple.developer.team-identifier": "WFTX6CN23F",
        "keychain-access-groups": ["WFTX6CN23F.com.tammy.desktop.sbr"],
      },
      ExpirationDate: "2027-05-13T11:58:20Z",
      TeamIdentifier: ["WFTX6CN23F"],
    });
  } finally {
    await rm(temporaryRoot, { force: true, recursive: true });
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

test("store metadata is complete and still rejects operator placeholders", async () => {
  const metadata = await readFile(
    path.join(root, "apps/desktop/release/macos/store-metadata.md"),
    "utf8",
  );
  assert.deepEqual(validateMacOSStoreMetadata(metadata), {
    complete: true,
    privacyPolicy: "https://github.com/castlemilk/tammy/blob/master/PRIVACY.md",
    support: "https://github.com/castlemilk/tammy/issues",
  });
  assert.deepEqual(
    validateMacOSStoreMetadata(
      metadata.replace(
        "**Privacy policy URL:** `https://github.com/castlemilk/tammy/blob/master/PRIVACY.md`",
        "**Privacy policy URL:** `OPERATOR_REQUIRED`",
      ),
    ),
    { complete: false },
  );
});

test("public privacy policy states the shipped offline data lifecycle", async () => {
  const privacy = await readFile(path.join(root, "PRIVACY.md"), "utf8");
  for (const statement of [
    "does not collect or transmit",
    "encrypted workspace",
    "macOS Keychain",
    "Files you choose to import",
    "deleting the workspace",
    "GitHub Issues",
  ]) {
    assert.match(privacy, new RegExp(statement, "i"));
  }
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
      "com.apple.security.application-groups": ["$(TeamIdentifierPrefix)com.tammy.desktop"],
      "keychain-access-groups": ["$(AppIdentifierPrefix)com.tammy.desktop.sbr"],
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
    sbrHelperEntitlements: {
      "com.apple.security.app-sandbox": true,
      "com.apple.security.files.user-selected.read-only": true,
      "com.apple.security.application-groups": ["$(TeamIdentifierPrefix)com.tammy.desktop"],
      "keychain-access-groups": ["$(AppIdentifierPrefix)com.tammy.desktop.sbr"],
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
  for (const sbrHelperEntitlements of [
    { ...valid.sbrHelperEntitlements, "com.apple.security.app-sandbox": false },
    {
      ...valid.sbrHelperEntitlements,
      "com.apple.security.application-groups": [
        "$(TeamIdentifierPrefix)com.tammy.desktop",
        "$(TeamIdentifierPrefix)com.other.app",
      ],
    },
    { ...valid.sbrHelperEntitlements, "com.apple.security.network.client": true },
    { ...valid.sbrHelperEntitlements, "com.apple.security.cs.allow-jit": true },
  ]) {
    assert.throws(
      () => validateMacOSStorePlists({ ...valid, sbrHelperEntitlements }),
      /MACOS_STORE_REPOSITORY_INVALID/,
    );
  }
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
