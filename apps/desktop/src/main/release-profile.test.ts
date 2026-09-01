import { lstat, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { describe, expect, it } from "vitest";

import {
  createMacOSReleaseProfile,
  MACOS_APP_BUNDLE_ID,
  type MacOSReleaseFacts,
  normalizeMacOSPackagedResourcePermissions,
} from "../../release/macos/profile";

const desktopRoot = path.resolve(__dirname, "../..");
const provisioningProfile = path.join(desktopRoot, "test.provisionprofile");
const releaseFacts: MacOSReleaseFacts = {
  identity: {
    appStoreName: "Tammy Accounting",
    architectures: ["arm64"],
    bundleIdentifier: "com.tammy.desktop",
    copyright: "© 2026 Gamma Systems Pty Ltd",
    installedName: "Tammy",
    minimumMacOSVersion: "14.0",
    publisher: "Gamma Systems Pty Ltd",
    supportEmail: "ben.ebsworth@gmail.com",
  },
  marketingVersion: "0.1.0",
  publicLinks: {
    privacyPolicy: "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
    support: "https://tammy-accounting.castlemilk.chatgpt.site/support",
  },
  target: "mas/arm64",
};

function distributionEnvironment(): NodeJS.ProcessEnv {
  return {
    TAMMY_MACOS_BUILD_NUMBER: "42",
    TAMMY_MACOS_EXPORT_COMPLIANCE: "exempt",
    TAMMY_MACOS_INSTALLER_IDENTITY: "Mac Installer Distribution: Tammy Pty Ltd (ABCDE12345)",
    TAMMY_MACOS_PROVISIONING_PROFILE: provisioningProfile,
    TAMMY_MACOS_PRIVACY_POLICY_URL: "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
    TAMMY_MACOS_SIGNING_IDENTITY: "Apple Distribution: Tammy Pty Ltd (ABCDE12345)",
    TAMMY_MACOS_SIGNING_MODE: "distribution",
    TAMMY_MACOS_SUPPORT_URL: "https://tammy-accounting.castlemilk.chatgpt.site/support",
    TAMMY_MACOS_TARGET: "mas/arm64",
    TAMMY_MACOS_TEAM_ID: "ABCDE12345",
    TAMMY_RELEASE_PROFILE: "mas",
  };
}

function createTestProfile(
  environment: NodeJS.ProcessEnv,
  facts: MacOSReleaseFacts = releaseFacts,
) {
  return createMacOSReleaseProfile(environment, desktopRoot, facts);
}

describe("createMacOSReleaseProfile", () => {
  it("keeps ordinary development packaging ad-hoc and separate from MAS", () => {
    expect(createMacOSReleaseProfile({}, desktopRoot)).toEqual({ kind: "development" });
  });

  it.each(["", "store", "darwin", "MAS"])('rejects unknown release profile "%s"', (profile) => {
    expect(() =>
      createMacOSReleaseProfile({ TAMMY_RELEASE_PROFILE: profile }, desktopRoot),
    ).toThrow("MACOS_RELEASE_INPUT_INVALID");
  });

  it.each([
    "TAMMY_MACOS_BUILD_NUMBER",
    "TAMMY_MACOS_EXPORT_COMPLIANCE",
    "TAMMY_MACOS_INSTALLER_IDENTITY",
    "TAMMY_MACOS_PROVISIONING_PROFILE",
    "TAMMY_MACOS_PRIVACY_POLICY_URL",
    "TAMMY_MACOS_SIGNING_IDENTITY",
    "TAMMY_MACOS_SIGNING_MODE",
    "TAMMY_MACOS_SUPPORT_URL",
    "TAMMY_MACOS_TARGET",
    "TAMMY_MACOS_TEAM_ID",
  ])("rejects a MAS profile with missing %s", (key) => {
    const environment = distributionEnvironment();
    delete environment[key];
    expect(() => createMacOSReleaseProfile(environment, desktopRoot)).toThrow(
      "MACOS_RELEASE_INPUT_INVALID",
    );
  });

  it.each(["0", "01", "-1", "1.2", "build-42"])(
    "rejects invalid App Store build number %s",
    (buildNumber) => {
      expect(() =>
        createTestProfile({
          ...distributionEnvironment(),
          TAMMY_MACOS_BUILD_NUMBER: buildNumber,
        }),
      ).toThrow("MACOS_RELEASE_INPUT_INVALID");
    },
  );

  it("builds a pinned distribution profile with separate executable entitlements", () => {
    const profile = createTestProfile(distributionEnvironment());
    expect(profile.kind).toBe("mas");
    if (profile.kind !== "mas") throw new Error("expected MAS profile");

    expect(profile.appBundleId).toBe(MACOS_APP_BUNDLE_ID);
    expect(profile.buildVersion).toBe("42");
    expect(profile.category).toBe("public.app-category.finance");
    expect(profile.installerIdentity).toContain("Mac Installer Distribution");
    expect(profile.info).toEqual({
      CFBundleDisplayName: "Tammy",
      ElectronTeamID: "ABCDE12345",
      ITSAppUsesNonExemptEncryption: false,
      LSMinimumSystemVersion: "14.0",
      NSHumanReadableCopyright: "© 2026 Gamma Systems Pty Ltd",
      TammyPrivacyPolicyURL: "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
      TammySupportURL: "https://tammy-accounting.castlemilk.chatgpt.site/support",
    });
    expect(Object.isExtensible(profile.info)).toBe(true);
    expect(profile.publicLinks).toEqual({
      privacyPolicy: "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
      support: "https://tammy-accounting.castlemilk.chatgpt.site/support",
    });
    expect(profile.sign.type).toBe("distribution");
    expect(profile.sign.provisioningProfile).toBe(provisioningProfile);
    expect(profile.sign.entitlementsFor(path.join(desktopRoot, "Tammy.app"))).toMatch(
      /entitlements\.mas\.plist$/,
    );
    expect(
      profile.sign.entitlementsFor(
        path.join(
          desktopRoot,
          "Tammy.app",
          "Contents",
          "Resources",
          "core",
          "darwin-arm64",
          "tammy-core",
        ),
      ),
    ).toMatch(/entitlements\.mas\.core\.plist$/);
    expect(
      profile.sign.entitlementsFor(
        path.join(
          desktopRoot,
          "Tammy.app",
          "Contents",
          "Resources",
          "sbr-helper",
          "darwin-arm64",
          "tammy-sbr-helper",
        ),
      ),
    ).toMatch(/entitlements\.mas\.sbr-helper\.plist$/);
    expect(
      profile.sign.entitlementsFor(
        path.join(desktopRoot, "Tammy.app", "Contents", "Frameworks", "Tammy Helper.app"),
      ),
    ).toMatch(/entitlements\.mas\.child\.plist$/);
  });

  it("does not trust serialized release facts supplied through the environment", () => {
    expect(() =>
      createMacOSReleaseProfile(
        { ...distributionEnvironment(), TAMMY_MACOS_RELEASE_FACTS: "{}" },
        desktopRoot,
      ),
    ).toThrow("MACOS_RELEASE_INPUT_INVALID");
  });

  it("rejects legacy Mac App Distribution certificate identity aliases", () => {
    expect(() =>
      createTestProfile({
        ...distributionEnvironment(),
        TAMMY_MACOS_SIGNING_IDENTITY:
          "3rd Party Mac Developer Application: Tammy Pty Ltd (ABCDE12345)",
      }),
    ).toThrow("MACOS_RELEASE_INPUT_INVALID");
  });

  it("supports a locally runnable development-signed MAS profile without an installer identity", () => {
    const environment = distributionEnvironment();
    environment.TAMMY_MACOS_SIGNING_MODE = "development";
    environment.TAMMY_MACOS_SIGNING_IDENTITY = "Apple Development: Tammy Pty Ltd (ABCDE12345)";
    delete environment.TAMMY_MACOS_INSTALLER_IDENTITY;

    const profile = createTestProfile(environment);
    expect(profile.kind).toBe("mas");
    if (profile.kind !== "mas") throw new Error("expected MAS profile");
    expect(profile.installerIdentity).toBeUndefined();
    expect(profile.sign.type).toBe("development");
  });

  it("creates an unsigned MAS staging profile without accepting signing material", () => {
    const environment = distributionEnvironment();
    environment.TAMMY_MACOS_ARTIFACT_PHASE = "unsigned-staging";
    delete environment.TAMMY_MACOS_INSTALLER_IDENTITY;
    delete environment.TAMMY_MACOS_PROVISIONING_PROFILE;
    delete environment.TAMMY_MACOS_SIGNING_IDENTITY;
    delete environment.TAMMY_MACOS_SIGNING_MODE;

    const profile = createTestProfile(environment);
    expect(profile.kind).toBe("mas-unsigned-staging");
    if (profile.kind !== "mas-unsigned-staging") throw new Error("expected unsigned staging");
    expect(profile.buildVersion).toBe("42");
    expect(profile.info.ElectronTeamID).toBe("ABCDE12345");
    expect("sign" in profile).toBe(false);
    expect("installerIdentity" in profile).toBe(false);
  });

  it.each([
    "TAMMY_MACOS_INSTALLER_IDENTITY",
    "TAMMY_MACOS_PROVISIONING_PROFILE",
    "TAMMY_MACOS_SIGNING_IDENTITY",
    "TAMMY_MACOS_SIGNING_MODE",
  ])("rejects %s in unsigned staging", (key) => {
    expect(() =>
      createTestProfile({
        ...distributionEnvironment(),
        TAMMY_MACOS_ARTIFACT_PHASE: "unsigned-staging",
        [key]: "must-not-be-accepted",
      }),
    ).toThrow("MACOS_RELEASE_INPUT_INVALID");
  });

  it.each([
    {
      TAMMY_MACOS_SIGNING_IDENTITY: "Apple Development: Tammy Pty Ltd (ABCDE12345)",
    },
    {
      TAMMY_MACOS_SIGNING_IDENTITY: "Apple Distribution: Tammy Pty Ltd (OTHER12345)",
    },
    {
      TAMMY_MACOS_INSTALLER_IDENTITY: "Developer ID Installer: Tammy Pty Ltd (ABCDE12345)",
    },
    {
      TAMMY_MACOS_INSTALLER_IDENTITY:
        "3rd Party Mac Developer Installer: Tammy Pty Ltd (OTHER12345)",
    },
  ])("rejects signing identities not bound to the selected mode and Team ID", (change) => {
    expect(() => createTestProfile({ ...distributionEnvironment(), ...change })).toThrow(
      "MACOS_RELEASE_INPUT_INVALID",
    );
  });

  it("accepts Apple's legacy Mac Installer Distribution certificate name", () => {
    const profile = createTestProfile({
      ...distributionEnvironment(),
      TAMMY_MACOS_INSTALLER_IDENTITY:
        "3rd Party Mac Developer Installer: Tammy Pty Ltd (ABCDE12345)",
    });
    expect(profile.kind).toBe("mas");
    if (profile.kind !== "mas") throw new Error("expected MAS profile");
    expect(profile.installerIdentity).toBe(
      "3rd Party Mac Developer Installer: Tammy Pty Ltd (ABCDE12345)",
    );
  });

  it.each([
    { marketingVersion: "0.2.0" },
    { target: "mas/x64" },
    { publicLinks: { ...releaseFacts.publicLinks, support: "https://example.com/support" } },
  ])("rejects release fact drift %#", (change) => {
    expect(() =>
      createTestProfile(distributionEnvironment(), {
        ...releaseFacts,
        ...change,
      }),
    ).toThrow("MACOS_RELEASE_INPUT_INVALID");
  });
});

describe.skipIf(process.platform === "win32")("normalizeMacOSPackagedResourcePermissions", () => {
  it("makes packaged build and SQLCipher resources readable by App Store users", async () => {
    const fixture = await mkdtemp(path.join(os.tmpdir(), "tammy-mas-permissions-"));
    const resources = path.join(fixture, "Tammy.app", "Contents", "Resources");
    const build = path.join(resources, "build");
    const cipher = path.join(resources, "sqlcipher", "darwin-arm64");
    const helper = path.join(resources, "sbr-helper", "darwin-arm64", "tammy-sbr-helper");
    const sbrProfile = path.join(resources, "sbr", "simulator", "sbr-profile-v1.json");
    try {
      await mkdir(build, { mode: 0o700, recursive: true });
      await mkdir(cipher, { mode: 0o700, recursive: true });
      await writeFile(path.join(build, "build-manifest.json"), "{}\n", { mode: 0o600 });
      await writeFile(path.join(cipher, "LIBRARY_SHA256"), "hash\n", { mode: 0o600 });
      await mkdir(path.dirname(helper), { recursive: true });
      await mkdir(path.dirname(sbrProfile), { recursive: true });
      await writeFile(helper, "helper", { mode: 0o700 });
      await writeFile(sbrProfile, "{}\n", { mode: 0o600 });

      await normalizeMacOSPackagedResourcePermissions(fixture);

      expect((await lstat(path.join(resources, "sqlcipher"))).mode & 0o777).toBe(0o755);
      expect((await lstat(cipher)).mode & 0o777).toBe(0o755);
      expect((await lstat(path.join(build, "build-manifest.json"))).mode & 0o777).toBe(0o644);
      expect((await lstat(path.join(cipher, "LIBRARY_SHA256"))).mode & 0o777).toBe(0o644);
      expect((await lstat(helper)).mode & 0o777).toBe(0o500);
      expect((await lstat(sbrProfile)).mode & 0o777).toBe(0o444);
    } finally {
      await rm(fixture, { force: true, recursive: true });
    }
  });
});
