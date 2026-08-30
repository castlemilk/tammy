import assert from "node:assert/strict";
import { execFile as nodeExecFile } from "node:child_process";
import { cp, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import {
  assertMacOSReleaseMetadata,
  inspectMacOSStoreRepository,
  readMacOSProvisioningProfilePlist,
  readMacOSRepositoryPlist,
  readPngDimensions,
  validateCompanyControllerAttestation,
  validateCurrentPublicSite,
  validateMacOSProvisioningProfile,
  validateMacOSReleaseEnvironment,
  validateMacOSStoreIdentity,
  validateMacOSStoreMetadata,
  validateMacOSStorePlists,
} from "./check-macos-store.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const execFile = promisify(nodeExecFile);
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
  const result = await inspectMacOSStoreRepository(root, { repositoryTestsPassed: true });

  assert.equal(result.appBundleId, "com.tammy.desktop");
  assert.equal(result.category, "public.app-category.finance");
  assert.equal(result.version, "0.1.0");
  assert.deepEqual(result.identity, validStoreIdentity);
  assert.equal(result.icon.width, 1024);
  assert.equal(result.icon.height, 1024);
  assert.deepEqual(result.operatorRequirements, [
    "certificates-and-profiles",
    "active-agreements",
    "age-rating",
    "app-store-warning-review",
    "export-compliance",
    "metadata-assets-entered",
    "pricing-availability",
    "privacy-answer",
    "processed-build",
    "seller-eligibility",
    "screenshots",
    "signed-build-privacy-report",
  ]);
  assert.deepEqual(result.passed, [
    "metadata",
    "policy",
    "public-site",
    "publisher-authority",
    "schemas",
    "store-identity",
    "tests",
  ]);
  assert.deepEqual(result.blockers, [
    "PLATFORM_IDENTITY_NOT_VERIFIED",
    "SCREENSHOT_DEFINITIONS_NOT_READY",
  ]);
  assert.equal(result.releaseState.state, "NOT_READY");
});

test("non-signing check reports repository blockers without release input leakage", async () => {
  const { stdout } = await execFile(process.execPath, ["scripts/check-macos-store.mjs"], {
    cwd: root,
  });
  const result = JSON.parse(stdout);

  assert.equal(result.status, "NOT_READY");
  assert.deepEqual(result.passed, [
    "metadata",
    "policy",
    "public-site",
    "publisher-authority",
    "schemas",
    "store-identity",
  ]);
  assert.deepEqual(result.blockers, [
    "PLATFORM_IDENTITY_NOT_VERIFIED",
    "REPOSITORY_TESTS_NOT_PASSED",
    "SCREENSHOT_DEFINITIONS_NOT_READY",
  ]);
  assert.deepEqual(result.identity, validStoreIdentity);
  assert.equal(stdout.includes("TAMMY_MACOS_"), false);
});

test("repository readiness binds the exact deployed public site to the canonical policy", async () => {
  const recordsRoot = path.join(root, "docs/release/public-site");
  const pointer = JSON.parse(await readFile(path.join(recordsRoot, "current.json"), "utf8"));
  const deployment = JSON.parse(
    await readFile(path.join(recordsRoot, pointer.deploymentEvidence), "utf8"),
  );
  const policy = await readFile(path.join(root, "PRIVACY.md"));
  assert.deepEqual(validateCurrentPublicSite(pointer, deployment, policy), {
    deploymentEvidence: pointer.deploymentEvidence,
    origin: "https://tammy-accounting.castlemilk.chatgpt.site",
    policySha256: deployment.policySha256,
    privacyPolicy: "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
    support: "https://tammy-accounting.castlemilk.chatgpt.site/support",
  });
  assert.throws(
    () => validateCurrentPublicSite(pointer, deployment, Buffer.from("changed policy\n")),
    /MACOS_PUBLIC_SITE_INVALID/,
  );
  assert.throws(
    () =>
      validateCurrentPublicSite(
        { ...pointer, deploymentEvidence: "../outside.json" },
        deployment,
        policy,
      ),
    /MACOS_PUBLIC_SITE_INVALID/,
  );
});

test("repository readiness rejects deployment evidence through a symlinked ancestor", async () => {
  const fixtureRoot = await mkdtemp(path.join(tmpdir(), "tammy-macos-public-site-symlink-"));
  const outside = await mkdtemp(path.join(tmpdir(), "tammy-macos-public-site-outside-"));
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
      cp(path.join(root, "PRIVACY.md"), path.join(fixtureRoot, "PRIVACY.md")),
    ]);
    const publicSiteRoot = path.join(fixtureRoot, "docs/release/public-site");
    const pointer = JSON.parse(await readFile(path.join(publicSiteRoot, "current.json"), "utf8"));
    await cp(
      path.join(publicSiteRoot, pointer.deploymentEvidence),
      path.join(outside, path.basename(pointer.deploymentEvidence)),
    );
    await rm(path.join(publicSiteRoot, "deployments"), { recursive: true });
    await symlink(outside, path.join(publicSiteRoot, "deployments"));
    await assert.rejects(
      inspectMacOSStoreRepository(fixtureRoot, { repositoryTestsPassed: true }),
      /MACOS_PUBLIC_SITE_INVALID/,
    );
  } finally {
    await rm(fixtureRoot, { force: true, recursive: true });
    await rm(outside, { force: true, recursive: true });
  }
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
      cp(path.join(root, "PRIVACY.md"), path.join(fixtureRoot, "PRIVACY.md")),
    ]);
    const packagePath = path.join(fixtureRoot, "apps", "desktop", "package.json");
    const desktopPackage = JSON.parse(await readFile(packagePath, "utf8"));
    await writeFile(
      packagePath,
      `${JSON.stringify({ ...desktopPackage, productName: "Tammy Drift" }, null, 2)}\n`,
    );

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

test("store metadata is truthful, public-site bound, and leaves Apple facts operator-owned", async () => {
  const metadata = await readFile(
    path.join(root, "apps/desktop/release/macos/store-metadata.md"),
    "utf8",
  );
  assert.deepEqual(validateMacOSStoreMetadata(metadata), {
    complete: true,
    marketingVersion: "0.1.0",
    operatorConfirmations: [
      "active-agreements",
      "age-rating",
      "app-store-warning-review",
      "export-compliance",
      "metadata-assets-entered",
      "pricing-availability",
      "privacy-answer",
      "processed-build",
      "seller-eligibility",
    ],
    privacyPolicy: "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
    publisher: "Gamma Systems Pty Ltd",
    support: "https://tammy-accounting.castlemilk.chatgpt.site/support",
    supportEmail: "ben.ebsworth@gmail.com",
  });
  for (const drift of [
    metadata.replace(
      "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
      "https://github.com/castlemilk/tammy/blob/master/PRIVACY.md",
    ),
    metadata.replace("- **Publisher:** `Gamma Systems Pty Ltd`", "- **Publisher:** `Ben Ebsworth`"),
    metadata.replace("ben.ebsworth@gmail.com", "support@example.com"),
    metadata.replaceAll("BAS draft — not lodged", "BAS report"),
    metadata.replace("Apple silicon", "Intel"),
    metadata.replace(
      "- **Seller eligibility:** `OPERATOR_CONFIRMATION_REQUIRED`",
      "- **Seller eligibility:** `Complete`",
    ),
  ]) {
    assert.throws(() => validateMacOSStoreMetadata(drift), /MACOS_STORE_REPOSITORY_INVALID/);
  }
  for (const prohibited of [
    "TestFlight invitation",
    "Production SBR submission is supported.",
    "Tammy submits company tax returns.",
    "Tammy lodges reports with the ATO.",
    "Tammy submits BAS directly to the ATO.",
    "Active agreements are confirmed.",
    "App Privacy is completed.",
    "The processed build is selected.",
  ]) {
    assert.throws(
      () => validateMacOSStoreMetadata(`${metadata}\n${prohibited}\n`),
      /MACOS_STORE_REPOSITORY_INVALID/,
    );
  }
});

test("public privacy policy states the shipped app, site, support, and cleanup boundaries", async () => {
  const privacy = await readFile(path.join(root, "PRIVACY.md"), "utf8");
  for (const statement of [
    "does not transmit your accounting records",
    "encrypted workspace",
    "macOS Keychain",
    "Files you choose to import",
    "no Gamma-owned analytics, cookies, accounts, or forms",
    "Sending an email is user initiated",
    "Removing the Tammy app alone does not promise deletion",
    "machine credentials",
    "/support",
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
