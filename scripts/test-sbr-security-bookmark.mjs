import { execFileSync } from "node:child_process";
import { copyFileSync, lstatSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const REQUIRED_SECURITY_BOOKMARK_ENVIRONMENT = Object.freeze([
  "TAMMY_MACOS_SIGNING_IDENTITY",
  "TAMMY_MACOS_PROVISIONING_PROFILE",
  "TAMMY_MACOS_TEAM_ID",
  "TAMMY_SBR_BOOKMARK_TEST_CREDENTIAL",
]);

const APP_BUNDLE_ID = "com.tammy.desktop";

function fail(code) {
  throw new Error(code);
}

function required(environment, name) {
  const value = environment[name];
  if (typeof value !== "string" || value.length === 0 || value.trim() !== value) {
    fail("SBR_SECURITY_BOOKMARK_RELEASE_INPUTS_REQUIRED");
  }
  return value;
}

function bundlePaths(temporaryDirectory, name) {
  const bundle = path.join(temporaryDirectory, `${name}.app`);
  return Object.freeze({
    bundle,
    executable: path.join(bundle, "Contents", "MacOS", name),
    profile: path.join(bundle, "Contents", "embedded.provisionprofile"),
  });
}

export function createSecurityBookmarkTestPlan(options) {
  if (options.platform !== "darwin" || options.arch !== "arm64") {
    fail("SBR_SECURITY_BOOKMARK_HOST_UNSUPPORTED");
  }
  const environment = options.environment;
  const identity = required(environment, "TAMMY_MACOS_SIGNING_IDENTITY");
  const provisioningProfile = required(environment, "TAMMY_MACOS_PROVISIONING_PROFILE");
  const teamID = required(environment, "TAMMY_MACOS_TEAM_ID");
  const credentialPath = required(environment, "TAMMY_SBR_BOOKMARK_TEST_CREDENTIAL");
  if (
    !/^[A-Z0-9]{10}$/.test(teamID) ||
    !path.isAbsolute(provisioningProfile) ||
    !path.isAbsolute(credentialPath) ||
    !/^(?:Apple Development|Apple Distribution|3rd Party Mac Developer Application): .+ \([A-Z0-9]{10}\)$/.test(
      identity,
    ) ||
    !identity.endsWith(` (${teamID})`)
  ) {
    fail("SBR_SECURITY_BOOKMARK_RELEASE_INPUTS_REQUIRED");
  }
  if (!path.isAbsolute(options.repositoryRoot) || !path.isAbsolute(options.temporaryDirectory)) {
    fail("SBR_SECURITY_BOOKMARK_RELEASE_INPUTS_REQUIRED");
  }

  const source = path.join(
    options.repositoryRoot,
    "test",
    "fixtures",
    "sbr",
    "create-security-bookmark.swift",
  );
  const host = bundlePaths(options.temporaryDirectory, "BookmarkHost");
  const helper = bundlePaths(options.temporaryDirectory, "BookmarkHelper");
  const hostEntitlements = path.join(options.temporaryDirectory, "host-entitlements.plist");
  const helperEntitlements = path.join(options.temporaryDirectory, "helper-entitlements.plist");
  const commands = [
    {
      file: "/usr/bin/xcrun",
      args: ["swiftc", source, "-framework", "AppKit", "-o", host.executable],
    },
    {
      file: "/usr/bin/xcrun",
      args: ["swiftc", source, "-framework", "AppKit", "-o", helper.executable],
    },
    {
      file: "/usr/bin/codesign",
      args: [
        "--force",
        "--options",
        "runtime",
        "--entitlements",
        hostEntitlements,
        "--sign",
        identity,
        host.bundle,
      ],
    },
    {
      file: "/usr/bin/codesign",
      args: [
        "--force",
        "--options",
        "runtime",
        "--entitlements",
        helperEntitlements,
        "--sign",
        identity,
        helper.bundle,
      ],
    },
    { file: "/usr/bin/codesign", args: ["--verify", "--strict", "--deep", host.bundle] },
    { file: "/usr/bin/codesign", args: ["--verify", "--strict", "--deep", helper.bundle] },
    {
      file: host.executable,
      args: ["--helper-path", helper.executable, "--expected-path", credentialPath],
    },
  ];
  return Object.freeze({
    appGroup: `${teamID}.${APP_BUNDLE_ID}`,
    commands: Object.freeze(commands.map((command) => Object.freeze(command))),
    credentialPath,
    helperBundle: helper.bundle,
    helperEntitlements,
    helperExecutable: helper.executable,
    hostBundle: host.bundle,
    hostEntitlements,
    hostExecutable: host.executable,
    identity,
    keychainGroup: `${teamID}.${APP_BUNDLE_ID}.sbr`,
    provisioningProfile,
    securityScopedBookmark: true,
    source,
    teamID,
    userSelectedReadOnly: true,
  });
}

function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isExactSingleton(value, expected) {
  return Array.isArray(value) && value.length === 1 && value[0] === expected;
}

export function validateSecurityBookmarkProvisioningProfile(profile, plan) {
  const entitlements = isRecord(profile) ? profile.Entitlements : undefined;
  if (
    !isRecord(entitlements) ||
    !isExactSingleton(profile.TeamIdentifier, plan.teamID) ||
    !isExactSingleton(profile.ApplicationIdentifierPrefix, plan.teamID) ||
    entitlements["application-identifier"] !== `${plan.teamID}.${APP_BUNDLE_ID}` ||
    !isExactSingleton(entitlements["com.apple.security.application-groups"], plan.appGroup) ||
    !isExactSingleton(entitlements["keychain-access-groups"], plan.keychainGroup)
  ) {
    fail("SBR_SECURITY_BOOKMARK_PROFILE_INVALID");
  }
}

function plist(bundleIdentifier) {
  return `<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0"><dict>\n<key>CFBundleExecutable</key><string>${path.basename(bundleIdentifier)}</string>\n<key>CFBundleIdentifier</key><string>${APP_BUNDLE_ID}</string>\n<key>CFBundleName</key><string>${bundleIdentifier}</string>\n<key>CFBundlePackageType</key><string>APPL</string>\n<key>CFBundleVersion</key><string>1</string>\n<key>CFBundleShortVersionString</key><string>1.0</string>\n</dict></plist>\n`;
}

function entitlements(plan) {
  return `<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0"><dict>\n<key>com.apple.security.app-sandbox</key><true/>\n<key>com.apple.security.files.user-selected.read-only</key><true/>\n<key>com.apple.security.application-groups</key><array><string>${plan.appGroup}</string></array>\n<key>keychain-access-groups</key><array><string>${plan.keychainGroup}</string></array>\n</dict></plist>\n`;
}

function prepareBundle(bundle, executable, profile, name, plan) {
  mkdirSync(path.dirname(executable), { recursive: true });
  writeFileSync(path.join(bundle, "Contents", "Info.plist"), plist(name), { mode: 0o600 });
  copyFileSync(plan.provisioningProfile, profile);
}

function run(plan) {
  for (const candidate of [plan.provisioningProfile, plan.credentialPath]) {
    const stats = lstatSync(candidate);
    if (stats.isSymbolicLink() || !stats.isFile())
      fail("SBR_SECURITY_BOOKMARK_RELEASE_INPUTS_REQUIRED");
  }
  const credentialStats = lstatSync(plan.credentialPath);
  if (credentialStats.size < 1 || credentialStats.size > 4 * 1024 * 1024) {
    fail("SBR_SECURITY_BOOKMARK_RELEASE_INPUTS_REQUIRED");
  }
  prepareBundle(
    plan.hostBundle,
    plan.hostExecutable,
    path.join(plan.hostBundle, "Contents", "embedded.provisionprofile"),
    "BookmarkHost",
    plan,
  );
  prepareBundle(
    plan.helperBundle,
    plan.helperExecutable,
    path.join(plan.helperBundle, "Contents", "embedded.provisionprofile"),
    "BookmarkHelper",
    plan,
  );
  writeFileSync(plan.hostEntitlements, entitlements(plan), { mode: 0o600 });
  writeFileSync(plan.helperEntitlements, entitlements(plan), { mode: 0o600 });

  const decodedProfile = execFileSync(
    "/usr/bin/security",
    ["cms", "-D", "-i", plan.provisioningProfile],
    {
      encoding: "utf8",
      maxBuffer: 1024 * 1024,
    },
  );
  const profile = JSON.parse(
    execFileSync("/usr/bin/plutil", ["-convert", "json", "-o", "-", "-"], {
      encoding: "utf8",
      input: decodedProfile,
      maxBuffer: 1024 * 1024,
    }),
  );
  validateSecurityBookmarkProvisioningProfile(profile, plan);
  const identities = execFileSync(
    "/usr/bin/security",
    ["find-identity", "-v", "-p", "codesigning"],
    {
      encoding: "utf8",
      maxBuffer: 1024 * 1024,
    },
  );
  if (!identities.includes(`"${plan.identity}"`))
    fail("SBR_SECURITY_BOOKMARK_IDENTITY_UNAVAILABLE");

  for (const command of plan.commands) {
    execFileSync(command.file, command.args, { stdio: "inherit" });
  }
}

const invokedPath = process.argv[1] === undefined ? "" : path.resolve(process.argv[1]);
if (invokedPath === fileURLToPath(import.meta.url)) {
  let temporaryDirectory;
  try {
    temporaryDirectory = mkdtempSync(path.join(os.tmpdir(), "tammy-sbr-bookmark-"));
    const plan = createSecurityBookmarkTestPlan({
      arch: process.arch,
      environment: process.env,
      platform: process.platform,
      repositoryRoot: path.resolve(import.meta.dirname, ".."),
      temporaryDirectory,
    });
    run(plan);
  } catch (error) {
    const message = error instanceof Error ? error.message : "SBR_SECURITY_BOOKMARK_FAILED";
    process.stderr.write(
      `${message.startsWith("SBR_SECURITY_BOOKMARK_") ? message : "SBR_SECURITY_BOOKMARK_FAILED"}\n`,
    );
    process.exitCode = 1;
  } finally {
    if (temporaryDirectory !== undefined)
      rmSync(temporaryDirectory, { force: true, recursive: true });
  }
}
