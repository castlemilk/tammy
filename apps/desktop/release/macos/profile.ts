import { execFileSync } from "node:child_process";
import { chmod, lstat, readdir } from "node:fs/promises";
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
    CFBundleDisplayName: "Tammy";
    ElectronTeamID: string;
    ITSAppUsesNonExemptEncryption: boolean;
    LSMinimumSystemVersion: "14.0";
    NSHumanReadableCopyright: "© 2026 Gamma Systems Pty Ltd";
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

export interface MacOSReleaseFacts {
  readonly identity: Readonly<{
    appStoreName: "Tammy Accounting";
    architectures: readonly ["arm64"];
    bundleIdentifier: "com.tammy.desktop";
    copyright: "© 2026 Gamma Systems Pty Ltd";
    installedName: "Tammy";
    minimumMacOSVersion: "14.0";
    publisher: "Gamma Systems Pty Ltd";
    supportEmail: "ben.ebsworth@gmail.com";
  }>;
  readonly marketingVersion: string;
  readonly publicLinks: Readonly<{ privacyPolicy: string; support: string }>;
  readonly target: string;
}

const PUBLIC_ORIGIN = "https://tammy-accounting.castlemilk.chatgpt.site";

function exactKeys(value: unknown, expected: readonly string[]): value is Record<string, unknown> {
  return (
    value !== null &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    Object.keys(value).length === expected.length &&
    expected.every((key) => Object.hasOwn(value, key))
  );
}

function validateReleaseFacts(value: unknown): MacOSReleaseFacts {
  if (
    !exactKeys(value, ["identity", "marketingVersion", "publicLinks", "target"]) ||
    !exactKeys(value.identity, [
      "appStoreName",
      "architectures",
      "bundleIdentifier",
      "copyright",
      "installedName",
      "minimumMacOSVersion",
      "publisher",
      "supportEmail",
    ]) ||
    !exactKeys(value.publicLinks, ["privacyPolicy", "support"])
  ) {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
  return value as unknown as MacOSReleaseFacts;
}

function releaseFactsFromChecker(
  environment: NodeJS.ProcessEnv,
  desktopRoot: string,
): MacOSReleaseFacts {
  try {
    const repositoryRoot = path.resolve(desktopRoot, "../..");
    const checker = path.join(repositoryRoot, "scripts", "check-macos-store.mjs");
    return validateReleaseFacts(
      JSON.parse(
        execFileSync(process.execPath, [checker, "--profile-facts"], {
          cwd: repositoryRoot,
          encoding: "utf8",
          env: environment,
          maxBuffer: 1024 * 1024,
          stdio: ["ignore", "pipe", "ignore"],
        }),
      ),
    );
  } catch {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
}

export async function normalizeMacOSPackagedResourcePermissions(buildPath: string): Promise<void> {
  if (!path.isAbsolute(buildPath)) throw new Error("MACOS_PACKAGED_RESOURCES_INVALID");
  const resources = path.join(buildPath, "Tammy.app", "Contents", "Resources");

  async function normalize(candidate: string): Promise<void> {
    const stats = await lstat(candidate);
    if (stats.isSymbolicLink()) throw new Error("MACOS_PACKAGED_RESOURCES_INVALID");
    if (stats.isFile()) {
      await chmod(candidate, 0o644);
      return;
    }
    if (!stats.isDirectory()) throw new Error("MACOS_PACKAGED_RESOURCES_INVALID");
    await chmod(candidate, 0o755);
    for (const entry of await readdir(candidate)) await normalize(path.join(candidate, entry));
  }

  await normalize(path.join(resources, "build"));
  await normalize(path.join(resources, "sqlcipher"));
  const helper = path.join(resources, "sbr-helper", "darwin-arm64", "tammy-sbr-helper");
  const helperStats = await lstat(helper);
  if (!helperStats.isFile() || helperStats.isSymbolicLink()) {
    throw new Error("MACOS_PACKAGED_RESOURCES_INVALID");
  }
  await chmod(helper, 0o500);
  const sbrRoot = path.join(resources, "sbr");
  await normalize(sbrRoot);
  async function makeReadOnly(directory: string): Promise<void> {
    for (const entry of await readdir(directory)) {
      const candidate = path.join(directory, entry);
      const stats = await lstat(candidate);
      if (stats.isSymbolicLink()) throw new Error("MACOS_PACKAGED_RESOURCES_INVALID");
      if (stats.isDirectory()) await makeReadOnly(candidate);
      else if (stats.isFile()) await chmod(candidate, 0o444);
      else throw new Error("MACOS_PACKAGED_RESOURCES_INVALID");
    }
  }
  await makeReadOnly(sbrRoot);
}

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
  suppliedFacts?: MacOSReleaseFacts,
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
  const target = required(environment, "TAMMY_MACOS_TARGET");

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
  const facts = validateReleaseFacts(
    suppliedFacts ?? releaseFactsFromChecker(environment, desktopRoot),
  );
  if (
    facts.marketingVersion !== "0.1.0" ||
    target !== "mas/arm64" ||
    facts.target !== target ||
    facts.identity.appStoreName !== "Tammy Accounting" ||
    facts.identity.installedName !== "Tammy" ||
    facts.identity.bundleIdentifier !== MACOS_APP_BUNDLE_ID ||
    facts.identity.publisher !== "Gamma Systems Pty Ltd" ||
    facts.identity.supportEmail !== "ben.ebsworth@gmail.com" ||
    facts.identity.minimumMacOSVersion !== "14.0" ||
    facts.identity.copyright !== "© 2026 Gamma Systems Pty Ltd" ||
    facts.identity.architectures.length !== 1 ||
    facts.identity.architectures[0] !== "arm64" ||
    facts.publicLinks.privacyPolicy !== `${PUBLIC_ORIGIN}/privacy` ||
    facts.publicLinks.support !== `${PUBLIC_ORIGIN}/support` ||
    privacyPolicy !== facts.publicLinks.privacyPolicy ||
    support !== facts.publicLinks.support
  ) {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
  const installerIdentity =
    type === "distribution" ? required(environment, "TAMMY_MACOS_INSTALLER_IDENTITY") : undefined;
  const signingCertificateClasses =
    type === "distribution" ? ["Apple Distribution"] : ["Apple Development"];
  if (
    !signingCertificateClasses.some((certificateClass) =>
      matchesIdentity(identity, certificateClass, teamID),
    ) ||
    (installerIdentity !== undefined &&
      !["Mac Installer Distribution"].some((certificateClass) =>
        matchesIdentity(installerIdentity, certificateClass, teamID),
      ))
  ) {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
  const releaseRoot = path.join(desktopRoot, "release", "macos");
  const mainEntitlements = path.join(releaseRoot, "entitlements.mas.plist");
  const childEntitlements = path.join(releaseRoot, "entitlements.mas.child.plist");
  const coreEntitlements = path.join(releaseRoot, "entitlements.mas.core.plist");
  const sbrHelperEntitlements = path.join(releaseRoot, "entitlements.mas.sbr-helper.plist");
  const coreSuffix = path.join("Contents", "Resources", "core", "darwin-arm64", "tammy-core");
  const sbrHelperSuffix = path.join(
    "Contents",
    "Resources",
    "sbr-helper",
    "darwin-arm64",
    "tammy-sbr-helper",
  );

  return Object.freeze({
    appBundleId: MACOS_APP_BUNDLE_ID,
    buildVersion,
    category: MACOS_APP_CATEGORY,
    icon: path.join(desktopRoot, "assets", "icon.icns"),
    info: {
      CFBundleDisplayName: "Tammy",
      ElectronTeamID: teamID,
      ITSAppUsesNonExemptEncryption: exportCompliance === "non-exempt",
      LSMinimumSystemVersion: "14.0",
      NSHumanReadableCopyright: "© 2026 Gamma Systems Pty Ltd",
    } as const,
    ...(installerIdentity === undefined ? {} : { installerIdentity }),
    kind: "mas",
    privacyManifest: path.join(releaseRoot, "PrivacyInfo.xcprivacy"),
    publicLinks: Object.freeze({ privacyPolicy, support }),
    sign: Object.freeze({
      entitlementsFor(file: string): string {
        if (file.endsWith(coreSuffix)) return coreEntitlements;
        if (file.endsWith(sbrHelperSuffix)) return sbrHelperEntitlements;
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
