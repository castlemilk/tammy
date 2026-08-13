import path from "node:path";

export const MACOS_APP_BUNDLE_ID = "com.tammy.desktop";
export const MACOS_APP_CATEGORY = "public.app-category.finance";

type SigningMode = "development" | "distribution";

export interface DevelopmentProfile {
  readonly kind: "development";
}

export interface MacOSStoreProfile {
  readonly appBundleId: typeof MACOS_APP_BUNDLE_ID;
  readonly buildVersion: string;
  readonly category: typeof MACOS_APP_CATEGORY;
  readonly icon: string;
  readonly info: Readonly<{
    ElectronTeamID: string;
    ITSAppUsesNonExemptEncryption: boolean;
  }>;
  readonly installerIdentity?: string;
  readonly kind: "mas";
  readonly privacyManifest: string;
  readonly publicLinks: Readonly<{
    privacyPolicy: string;
    support: string;
  }>;
  readonly sign: Readonly<{
    entitlementsFor: (file: string) => string;
    identity: string;
    provisioningProfile: string;
    type: SigningMode;
  }>;
}

export type MacOSReleaseProfile = DevelopmentProfile | MacOSStoreProfile;

function required(environment: NodeJS.ProcessEnv, key: string): string {
  const value = environment[key];
  if (typeof value !== "string" || value.length === 0 || value.trim() !== value) {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
  return value;
}

function isAbsoluteFileInput(value: string): boolean {
  return path.isAbsolute(value) && path.basename(value) !== "." && path.basename(value) !== "..";
}

function requiredHttpsUrl(environment: NodeJS.ProcessEnv, key: string): string {
  const value = required(environment, key);
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
  if (
    parsed.protocol !== "https:" ||
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.hash !== ""
  ) {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
  return parsed.href;
}

function matchesIdentity(value: string, certificateClass: string, teamID: string): boolean {
  const prefix = `${certificateClass}: `;
  const suffix = ` (${teamID})`;
  return (
    value.startsWith(prefix) &&
    value.endsWith(suffix) &&
    value.length > prefix.length + suffix.length
  );
}

export function createMacOSReleaseProfile(
  environment: NodeJS.ProcessEnv,
  desktopRoot: string,
): MacOSReleaseProfile {
  if (environment.TAMMY_RELEASE_PROFILE === undefined) {
    return Object.freeze({ kind: "development" });
  }
  if (environment.TAMMY_RELEASE_PROFILE !== "mas") {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
  if (!path.isAbsolute(desktopRoot)) throw new Error("MACOS_RELEASE_INPUT_INVALID");

  const buildVersion = required(environment, "TAMMY_MACOS_BUILD_NUMBER");
  const exportCompliance = required(environment, "TAMMY_MACOS_EXPORT_COMPLIANCE");
  const provisioningProfile = required(environment, "TAMMY_MACOS_PROVISIONING_PROFILE");
  const privacyPolicy = requiredHttpsUrl(environment, "TAMMY_MACOS_PRIVACY_POLICY_URL");
  const identity = required(environment, "TAMMY_MACOS_SIGNING_IDENTITY");
  const signingMode = required(environment, "TAMMY_MACOS_SIGNING_MODE");
  const teamID = required(environment, "TAMMY_MACOS_TEAM_ID");
  const support = requiredHttpsUrl(environment, "TAMMY_MACOS_SUPPORT_URL");

  if (
    !/^[1-9][0-9]*$/.test(buildVersion) ||
    !["exempt", "non-exempt"].includes(exportCompliance) ||
    !["development", "distribution"].includes(signingMode) ||
    !/^[A-Z0-9]{10}$/.test(teamID) ||
    !isAbsoluteFileInput(provisioningProfile)
  ) {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }

  const type = signingMode as SigningMode;
  const installerIdentity =
    type === "distribution" ? required(environment, "TAMMY_MACOS_INSTALLER_IDENTITY") : undefined;
  const signingCertificateClasses =
    type === "distribution"
      ? ["Apple Distribution", "3rd Party Mac Developer Application"]
      : ["Apple Development"];
  if (
    !signingCertificateClasses.some((certificateClass) =>
      matchesIdentity(identity, certificateClass, teamID),
    ) ||
    (installerIdentity !== undefined &&
      !["Mac Installer Distribution", "3rd Party Mac Developer Installer"].some(
        (certificateClass) => matchesIdentity(installerIdentity, certificateClass, teamID),
      ))
  ) {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
  const releaseRoot = path.join(desktopRoot, "release", "macos");
  const mainEntitlements = path.join(releaseRoot, "entitlements.mas.plist");
  const childEntitlements = path.join(releaseRoot, "entitlements.mas.child.plist");
  const coreEntitlements = path.join(releaseRoot, "entitlements.mas.core.plist");
  const coreSuffix = path.join("Contents", "Resources", "core", "darwin-arm64", "tammy-core");

  return Object.freeze({
    appBundleId: MACOS_APP_BUNDLE_ID,
    buildVersion,
    category: MACOS_APP_CATEGORY,
    icon: path.join(desktopRoot, "assets", "icon.icns"),
    info: Object.freeze({
      ElectronTeamID: teamID,
      ITSAppUsesNonExemptEncryption: exportCompliance === "non-exempt",
    }),
    ...(installerIdentity === undefined ? {} : { installerIdentity }),
    kind: "mas",
    privacyManifest: path.join(releaseRoot, "PrivacyInfo.xcprivacy"),
    publicLinks: Object.freeze({ privacyPolicy, support }),
    sign: Object.freeze({
      entitlementsFor(file: string): string {
        if (file.endsWith(coreSuffix)) return coreEntitlements;
        if (
          file.endsWith(`${path.sep}Tammy.app`) &&
          !file.includes(`${path.sep}Frameworks${path.sep}`)
        ) {
          return mainEntitlements;
        }
        return childEntitlements;
      },
      identity,
      provisioningProfile,
      type,
    }),
  });
}
