import { lstat, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { describe, expect, it } from "vitest";

import {
  createMacOSReleaseProfile,
  MACOS_APP_BUNDLE_ID,
  normalizeMacOSPackagedResourcePermissions,
} from "../../release/macos/profile";

const desktopRoot = path.resolve(__dirname, "../..");
const provisioningProfile = path.join(desktopRoot, "test.provisionprofile");

function distributionEnvironment(): NodeJS.ProcessEnv {
  return {
    TAMMY_MACOS_BUILD_NUMBER: "42",
    TAMMY_MACOS_EXPORT_COMPLIANCE: "exempt",
    TAMMY_MACOS_INSTALLER_IDENTITY: "3rd Party Mac Developer Installer: Tammy Pty Ltd (ABCDE12345)",
    TAMMY_MACOS_PROVISIONING_PROFILE: provisioningProfile,
    TAMMY_MACOS_PRIVACY_POLICY_URL: "https://example.com/tammy/privacy",
    TAMMY_MACOS_SIGNING_IDENTITY: "Apple Distribution: Tammy Pty Ltd (ABCDE12345)",
    TAMMY_MACOS_SIGNING_MODE: "distribution",
    TAMMY_MACOS_SUPPORT_URL: "https://example.com/tammy/support",
    TAMMY_MACOS_TEAM_ID: "ABCDE12345",
    TAMMY_RELEASE_PROFILE: "mas",
  };
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
        createMacOSReleaseProfile(
          { ...distributionEnvironment(), TAMMY_MACOS_BUILD_NUMBER: buildNumber },
          desktopRoot,
        ),
      ).toThrow("MACOS_RELEASE_INPUT_INVALID");
    },
  );

  it("builds a pinned distribution profile with separate executable entitlements", () => {
    const profile = createMacOSReleaseProfile(distributionEnvironment(), desktopRoot);
    expect(profile.kind).toBe("mas");
    if (profile.kind !== "mas") throw new Error("expected MAS profile");

    expect(profile.appBundleId).toBe(MACOS_APP_BUNDLE_ID);
    expect(profile.buildVersion).toBe("42");
    expect(profile.category).toBe("public.app-category.finance");
    expect(profile.installerIdentity).toContain("3rd Party Mac Developer Installer");
    expect(profile.info).toEqual({
      ElectronTeamID: "ABCDE12345",
      ITSAppUsesNonExemptEncryption: false,
    });
    expect(Object.isExtensible(profile.info)).toBe(true);
    expect(profile.publicLinks).toEqual({
      privacyPolicy: "https://example.com/tammy/privacy",
      support: "https://example.com/tammy/support",
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

  it("accepts Apple's Mac App Distribution certificate identity", () => {
    const profile = createMacOSReleaseProfile(
      {
        ...distributionEnvironment(),
        TAMMY_MACOS_SIGNING_IDENTITY:
          "3rd Party Mac Developer Application: Tammy Pty Ltd (ABCDE12345)",
      },
      desktopRoot,
    );

    expect(profile.kind).toBe("mas");
  });

  it("supports a locally runnable development-signed MAS profile without an installer identity", () => {
    const environment = distributionEnvironment();
    environment.TAMMY_MACOS_SIGNING_MODE = "development";
    environment.TAMMY_MACOS_SIGNING_IDENTITY = "Apple Development: Tammy Pty Ltd (ABCDE12345)";
    delete environment.TAMMY_MACOS_INSTALLER_IDENTITY;

    const profile = createMacOSReleaseProfile(environment, desktopRoot);
    expect(profile.kind).toBe("mas");
    if (profile.kind !== "mas") throw new Error("expected MAS profile");
    expect(profile.installerIdentity).toBeUndefined();
    expect(profile.sign.type).toBe("development");
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
    expect(() =>
      createMacOSReleaseProfile({ ...distributionEnvironment(), ...change }, desktopRoot),
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
