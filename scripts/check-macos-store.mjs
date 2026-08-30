import { execFile as nodeExecFile } from "node:child_process";
import { createHash } from "node:crypto";
import { access, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { isDeepStrictEqual, promisify } from "node:util";

import { evaluateReleaseState } from "./macos-release-state.mjs";

const execFile = promisify(nodeExecFile);
const PNG_SIGNATURE = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
const APP_BUNDLE_ID = "com.tammy.desktop";
const APP_CATEGORY = "public.app-category.finance";

function fail(code = "MACOS_STORE_REPOSITORY_INVALID") {
  throw new Error(code);
}

function required(environment, key) {
  const value = environment[key];
  if (typeof value !== "string" || value.length === 0 || value.trim() !== value) {
    fail("MACOS_RELEASE_INPUT_INVALID");
  }
  return value;
}

function requiredHttps(environment, key) {
  const value = required(environment, key);
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    fail("MACOS_RELEASE_INPUT_INVALID");
  }
  if (
    parsed.protocol !== "https:" ||
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.hash !== ""
  ) {
    fail("MACOS_RELEASE_INPUT_INVALID");
  }
  return parsed.href;
}

function matchesIdentity(value, certificateClass, teamID) {
  const prefix = `${certificateClass}: `;
  const suffix = ` (${teamID})`;
  return (
    value.startsWith(prefix) &&
    value.endsWith(suffix) &&
    value.length > prefix.length + suffix.length
  );
}

export function readPngDimensions(bytes) {
  if (
    !Buffer.isBuffer(bytes) ||
    bytes.length < 24 ||
    !bytes.subarray(0, PNG_SIGNATURE.length).equals(PNG_SIGNATURE) ||
    bytes.toString("ascii", 12, 16) !== "IHDR"
  ) {
    fail("MACOS_STORE_ICON_INVALID");
  }
  const width = bytes.readUInt32BE(16);
  const height = bytes.readUInt32BE(20);
  if (width === 0 || height === 0) fail("MACOS_STORE_ICON_INVALID");
  return { height, width };
}

export function validateMacOSReleaseEnvironment(environment) {
  const buildNumber = required(environment, "TAMMY_MACOS_BUILD_NUMBER");
  const exportCompliance = required(environment, "TAMMY_MACOS_EXPORT_COMPLIANCE");
  const provisioningProfile = required(environment, "TAMMY_MACOS_PROVISIONING_PROFILE");
  requiredHttps(environment, "TAMMY_MACOS_PRIVACY_POLICY_URL");
  const signingIdentity = required(environment, "TAMMY_MACOS_SIGNING_IDENTITY");
  const mode = required(environment, "TAMMY_MACOS_SIGNING_MODE");
  const teamID = required(environment, "TAMMY_MACOS_TEAM_ID");
  requiredHttps(environment, "TAMMY_MACOS_SUPPORT_URL");
  if (
    !/^[1-9][0-9]*$/.test(buildNumber) ||
    !["exempt", "non-exempt"].includes(exportCompliance) ||
    !["development", "distribution"].includes(mode) ||
    !path.isAbsolute(provisioningProfile) ||
    !/^[A-Z0-9]{10}$/.test(teamID)
  ) {
    fail("MACOS_RELEASE_INPUT_INVALID");
  }
  const installerIdentity =
    mode === "distribution" ? required(environment, "TAMMY_MACOS_INSTALLER_IDENTITY") : undefined;
  const signingCertificateClasses =
    mode === "distribution"
      ? ["Apple Distribution", "3rd Party Mac Developer Application"]
      : ["Apple Development"];
  if (
    !signingCertificateClasses.some((certificateClass) =>
      matchesIdentity(signingIdentity, certificateClass, teamID),
    ) ||
    (installerIdentity !== undefined &&
      !["Mac Installer Distribution", "3rd Party Mac Developer Installer"].some(
        (certificateClass) => matchesIdentity(installerIdentity, certificateClass, teamID),
      ))
  ) {
    fail("MACOS_RELEASE_INPUT_INVALID");
  }
  return { buildNumber, mode };
}

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

const MACOS_STORE_IDENTITY = Object.freeze({
  schemaVersion: 1,
  appStoreName: "Tammy Accounting",
  installedName: "Tammy",
  bundleIdentifier: APP_BUNDLE_ID,
  publisher: "Gamma Systems Pty Ltd",
  supportEmail: "ben.ebsworth@gmail.com",
  locale: "en-AU",
  primaryCategory: "Finance",
  secondaryCategory: "Business",
  minimumMacOSVersion: "14.0",
  architectures: Object.freeze(["arm64"]),
  copyright: "© 2026 Gamma Systems Pty Ltd",
  capabilityBoundary: Object.freeze({
    reporting: "preparation-only",
    atoLodgement: "not-lodged",
  }),
});

export function validateMacOSStoreIdentity(value) {
  if (!isDeepStrictEqual(value, MACOS_STORE_IDENTITY)) {
    fail("MACOS_STORE_IDENTITY_INVALID");
  }
  return value;
}

const COMPANY_CONTROLLER_ATTESTATION = Object.freeze({
  accountablePerson: "Ben Ebsworth",
  company: "Gamma Systems Pty Ltd",
  controlsPrivacyPolicy: true,
  controlsSupportAddress: true,
  evidenceReference: "user-confirmation-in-task",
  kind: "publisher-controller-authority",
  schemaVersion: 1,
  supportEmail: "ben.ebsworth@gmail.com",
});
const COMPANY_CONTROLLER_ATTESTATION_KEYS = Object.freeze([
  "schemaVersion",
  "kind",
  "company",
  "accountablePerson",
  "controlsPrivacyPolicy",
  "controlsSupportAddress",
  "supportEmail",
  "confirmedAt",
  "evidenceReference",
]);
const SENSITIVE_KEY = /secret|token|password|credential/i;
const UTC_RFC3339 = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?Z$/;

function isValidUtcRfc3339(value) {
  const match = typeof value === "string" ? value.match(UTC_RFC3339) : undefined;
  if (match === null || match === undefined) return false;
  const date = new Date(value);
  return (
    Number.isFinite(date.getTime()) &&
    date.getUTCFullYear() === Number(match[1]) &&
    date.getUTCMonth() + 1 === Number(match[2]) &&
    date.getUTCDate() === Number(match[3]) &&
    date.getUTCHours() === Number(match[4]) &&
    date.getUTCMinutes() === Number(match[5]) &&
    date.getUTCSeconds() === Number(match[6])
  );
}

export function validateCompanyControllerAttestation(attestation) {
  if (
    !isRecord(attestation) ||
    Object.keys(attestation).length !== COMPANY_CONTROLLER_ATTESTATION_KEYS.length ||
    Object.keys(attestation).some(
      (key) => !COMPANY_CONTROLLER_ATTESTATION_KEYS.includes(key) || SENSITIVE_KEY.test(key),
    ) ||
    !COMPANY_CONTROLLER_ATTESTATION_KEYS.every((key) => Object.hasOwn(attestation, key)) ||
    Object.entries(COMPANY_CONTROLLER_ATTESTATION).some(
      ([key, value]) => attestation[key] !== value,
    ) ||
    !isValidUtcRfc3339(attestation.confirmedAt)
  ) {
    fail("MACOS_STORE_COMPANY_AUTHORITY_INVALID");
  }
}

export function validateMacOSProvisioningProfile(profile, { mode, teamID, now = new Date() }) {
  const entitlements = isRecord(profile) ? profile.Entitlements : undefined;
  const appIdentifierPrefixes = isRecord(profile) ? profile.ApplicationIdentifierPrefix : undefined;
  const appIdentifierPrefix =
    Array.isArray(appIdentifierPrefixes) && appIdentifierPrefixes.length === 1
      ? appIdentifierPrefixes[0]
      : undefined;
  const expiry = isRecord(profile) ? new Date(profile.ExpirationDate) : new Date(Number.NaN);
  const developmentDevices = isRecord(profile) ? profile.ProvisionedDevices : undefined;
  const validClass =
    mode === "development"
      ? entitlements?.["get-task-allow"] === true &&
        Array.isArray(developmentDevices) &&
        developmentDevices.length > 0
      : mode === "distribution"
        ? entitlements?.["get-task-allow"] !== true && developmentDevices === undefined
        : false;
  if (
    !isRecord(profile) ||
    !isRecord(entitlements) ||
    !Array.isArray(profile.TeamIdentifier) ||
    profile.TeamIdentifier.length !== 1 ||
    profile.TeamIdentifier[0] !== teamID ||
    typeof appIdentifierPrefix !== "string" ||
    !/^[A-Z0-9]{10}$/.test(appIdentifierPrefix) ||
    entitlements["com.apple.developer.team-identifier"] !== teamID ||
    entitlements["com.apple.application-identifier"] !==
      `${appIdentifierPrefix}.${APP_BUNDLE_ID}` ||
    !isDeepStrictEqual(entitlements["com.apple.security.application-groups"], [
      `${teamID}.${APP_BUNDLE_ID}`,
    ]) ||
    !isDeepStrictEqual(entitlements["keychain-access-groups"], [
      `${appIdentifierPrefix}.${APP_BUNDLE_ID}.sbr`,
    ]) ||
    profile.ProvisionsAllDevices === true ||
    !Number.isFinite(expiry.getTime()) ||
    expiry.getTime() <= now.getTime() ||
    !validClass
  ) {
    fail("MACOS_RELEASE_PROVISIONING_PROFILE_INVALID");
  }
}

async function extractProvisioningProfileValue(file, key, format) {
  const { stdout } = await execFile("/usr/bin/plutil", ["-extract", key, format, "-o", "-", file]);
  return format === "json" ? JSON.parse(stdout) : stdout.trim();
}

export async function readMacOSProvisioningProfilePlist(file) {
  try {
    const [ApplicationIdentifierPrefix, Entitlements, ExpirationDate, TeamIdentifier] =
      await Promise.all([
        extractProvisioningProfileValue(file, "ApplicationIdentifierPrefix", "json"),
        extractProvisioningProfileValue(file, "Entitlements", "json"),
        extractProvisioningProfileValue(file, "ExpirationDate", "raw"),
        extractProvisioningProfileValue(file, "TeamIdentifier", "json"),
      ]);
    const profile = { ApplicationIdentifierPrefix, Entitlements, ExpirationDate, TeamIdentifier };
    for (const [key, format] of [
      ["ProvisionedDevices", "json"],
      ["ProvisionsAllDevices", "raw"],
    ]) {
      try {
        const value = await extractProvisioningProfileValue(file, key, format);
        profile[key] = format === "raw" ? value === "true" : value;
      } catch {
        // Distribution profiles omit device-scoped fields.
      }
    }
    return profile;
  } catch {
    fail("MACOS_RELEASE_PROVISIONING_PROFILE_INVALID");
  }
}

async function readMacOSProvisioningProfile(file) {
  let temporaryRoot;
  try {
    const { stdout } = await execFile("/usr/bin/security", ["cms", "-D", "-i", file], {
      maxBuffer: 1024 * 1024,
    });
    temporaryRoot = await mkdtemp(path.join(tmpdir(), "tammy-macos-profile-"));
    const decoded = path.join(temporaryRoot, "profile.plist");
    await writeFile(decoded, stdout, { flag: "wx", mode: 0o600 });
    return await readMacOSProvisioningProfilePlist(decoded);
  } catch {
    fail("MACOS_RELEASE_PROVISIONING_PROFILE_INVALID");
  } finally {
    if (temporaryRoot !== undefined) await rm(temporaryRoot, { force: true, recursive: true });
  }
}

function includesAll(source, values) {
  return values.every((value) => source.includes(value));
}

export function validateMacOSStorePlists({
  appEntitlements,
  childEntitlements,
  coreEntitlements,
  sbrHelperEntitlements,
  privacy,
}) {
  const inherited = {
    "com.apple.security.app-sandbox": true,
    "com.apple.security.inherit": true,
  };
  if (
    !isDeepStrictEqual(appEntitlements, {
      "com.apple.security.app-sandbox": true,
      "com.apple.security.files.user-selected.read-only": true,
      "com.apple.security.application-groups": ["$(TeamIdentifierPrefix)com.tammy.desktop"],
      "keychain-access-groups": ["$(AppIdentifierPrefix)com.tammy.desktop.sbr"],
      "com.apple.security.network.client": true,
      "com.apple.security.network.server": true,
    }) ||
    !isDeepStrictEqual(childEntitlements, inherited) ||
    !isDeepStrictEqual(coreEntitlements, inherited) ||
    !isDeepStrictEqual(sbrHelperEntitlements, {
      "com.apple.security.app-sandbox": true,
      "com.apple.security.files.user-selected.read-only": true,
      "com.apple.security.application-groups": ["$(TeamIdentifierPrefix)com.tammy.desktop"],
      "keychain-access-groups": ["$(AppIdentifierPrefix)com.tammy.desktop.sbr"],
    }) ||
    !isDeepStrictEqual(privacy, {
      NSPrivacyAccessedAPITypes: [
        {
          NSPrivacyAccessedAPIType: "NSPrivacyAccessedAPICategoryFileTimestamp",
          NSPrivacyAccessedAPITypeReasons: ["C617.1", "3B52.1"],
        },
      ],
      NSPrivacyCollectedDataTypes: [],
      NSPrivacyTracking: false,
      NSPrivacyTrackingDomains: [],
    })
  ) {
    fail();
  }
}

const PUBLIC_ORIGIN = "https://tammy-accounting.castlemilk.chatgpt.site";
const METADATA_OPERATOR_CONFIRMATIONS = Object.freeze([
  "age-rating",
  "app-store-warning-review",
  "export-compliance",
  "metadata-assets-entered",
  "pricing-availability",
  "privacy-answer",
  "processed-build",
  "seller-eligibility",
]);
const METADATA_CONFIRMATION_LABELS = Object.freeze({
  "age-rating": "Age rating",
  "app-store-warning-review": "App Store warning review",
  "export-compliance": "Export compliance",
  "metadata-assets-entered": "Metadata and assets entered",
  "pricing-availability": "Pricing and availability",
  "privacy-answer": "App privacy answer",
  "processed-build": "Processed build selection",
  "seller-eligibility": "Seller eligibility",
});
const REQUIRED_METADATA_COPY = Object.freeze([
  "encrypted local workspace",
  "organisation and chart of accounts",
  "balanced journals",
  "trial balance",
  "upload and review source documents",
  "bank statement transactions",
  "GST and BAS drafts",
  "retained local activity",
  "does not require a cloud account",
  "advertising, analytics, tracking, in-app purchases, or ATO lodgement",
  "BAS draft — not lodged",
  "no remote account or demo credentials",
]);

function metadataField(source, label) {
  const escapedLabel = label.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  // biome-ignore lint/style/useTemplate: Backticks are literal Markdown delimiters in this matcher.
  return source.match(new RegExp("^- \\*\\*" + escapedLabel + ":\\*\\* `([^`]+)`$", "m"))?.[1];
}

export function validateMacOSStoreMetadata(source) {
  if (typeof source !== "string") fail();
  const exactFields = {
    "App Store Connect ID": "6800226692",
    "Apple Developer identifier ID": "DXP9QHD7JH",
    "Bundle identifier": "com.tammy.desktop",
    Name: "Tammy Accounting",
    "Installed name": "Tammy",
    Version: "0.1.0",
    "Minimum macOS version": "14.0 or later",
    Architectures: "Apple silicon (arm64)",
    Publisher: "Gamma Systems Pty Ltd",
    Subtitle: "Local accounting for Australia",
    "Primary category": "Finance",
    "Secondary category": "Business",
    "Default language": "English (Australia) — en-AU",
    SKU: "tammy-macos-001",
    "Release method": "Manual",
    "Privacy policy URL": `${PUBLIC_ORIGIN}/privacy`,
    "Support URL": `${PUBLIC_ORIGIN}/support`,
    Copyright: "© 2026 Gamma Systems Pty Ltd",
    "Intended price and availability": "Free — Australia",
    "Review support email": "ben.ebsworth@gmail.com",
  };
  if (
    Object.entries(exactFields).some(
      ([label, expected]) => metadataField(source, label) !== expected,
    ) ||
    REQUIRED_METADATA_COPY.some((copy) => !source.includes(copy)) ||
    source.includes("OPERATOR_REQUIRED") ||
    /\[[ xX]\]/.test(source) ||
    /github\.com\/castlemilk\/tammy|Ben Ebsworth — individual|TestFlight invitation|production SBR enabled|company tax return submission enabled/i.test(
      source,
    ) ||
    METADATA_OPERATOR_CONFIRMATIONS.some(
      (kind) =>
        metadataField(source, METADATA_CONFIRMATION_LABELS[kind]) !==
        "OPERATOR_CONFIRMATION_REQUIRED",
    )
  ) {
    fail();
  }
  return {
    complete: true,
    marketingVersion: exactFields.Version,
    operatorConfirmations: [...METADATA_OPERATOR_CONFIRMATIONS],
    privacyPolicy: exactFields["Privacy policy URL"],
    publisher: exactFields.Publisher,
    support: exactFields["Support URL"],
    supportEmail: exactFields["Review support email"],
  };
}

export function assertMacOSReleaseMetadata(metadata, environment) {
  if (
    metadata?.complete !== true ||
    metadata.privacyPolicy !== requiredHttps(environment, "TAMMY_MACOS_PRIVACY_POLICY_URL") ||
    metadata.support !== requiredHttps(environment, "TAMMY_MACOS_SUPPORT_URL")
  ) {
    fail("MACOS_RELEASE_METADATA_MISMATCH");
  }
}

function decodePlistText(value) {
  if (typeof value !== "string" || value.includes("<")) fail();
  const decoded = value.replace(/&(amp|apos|gt|lt|quot);/g, (_match, entity) => {
    return { amp: "&", apos: "'", gt: ">", lt: "<", quot: '"' }[entity];
  });
  if (decoded.includes("&")) fail();
  return decoded;
}

export function parseMacOSRepositoryPlist(source) {
  if (typeof source !== "string") fail();
  const opening = source.indexOf('<plist version="1.0">');
  const closing = source.lastIndexOf("</plist>");
  if (
    opening < 0 ||
    closing < opening ||
    !/^<\?xml[\s\S]*\?>\s*<!DOCTYPE plist[\s\S]*>$/.test(source.slice(0, opening).trim()) ||
    source.slice(closing + "</plist>".length).trim() !== ""
  ) {
    fail();
  }
  const tokens = (
    source.slice(opening + '<plist version="1.0">'.length, closing).match(/<[^>]*>|[^<]+/g) ?? []
  ).filter((token) => !/^\s+$/.test(token));
  let index = 0;
  const take = (token) => {
    if (tokens[index] !== token) fail();
    index += 1;
  };
  const parseText = (tag) => {
    take(`<${tag}>`);
    const text = tokens[index];
    if (typeof text !== "string" || text.startsWith("<")) fail();
    index += 1;
    take(`</${tag}>`);
    return decodePlistText(text);
  };
  const parseValue = () => {
    const token = tokens[index];
    if (token === "<true/>") {
      index += 1;
      return true;
    }
    if (token === "<false/>") {
      index += 1;
      return false;
    }
    if (token === "<string>") return parseText("string");
    if (token === "<array/>") {
      index += 1;
      return [];
    }
    if (token === "<array>") {
      index += 1;
      const values = [];
      while (tokens[index] !== "</array>") values.push(parseValue());
      take("</array>");
      return values;
    }
    if (token === "<dict>") {
      index += 1;
      const dictionary = {};
      while (tokens[index] !== "</dict>") {
        const key = parseText("key");
        if (Object.hasOwn(dictionary, key)) fail();
        Object.defineProperty(dictionary, key, {
          configurable: true,
          enumerable: true,
          value: parseValue(),
          writable: true,
        });
      }
      take("</dict>");
      return dictionary;
    }
    fail();
  };
  const parsed = parseValue();
  if (index !== tokens.length) fail();
  return parsed;
}

export async function readMacOSRepositoryPlist(file, read = readFile) {
  try {
    return parseMacOSRepositoryPlist(await read(file, "utf8"));
  } catch {
    fail();
  }
}

function exactKeys(value, expected) {
  return (
    isRecord(value) &&
    Object.keys(value).length === expected.length &&
    expected.every((key) => Object.hasOwn(value, key))
  );
}

export function validateCurrentPublicSite(pointer, deployment, policyBytes) {
  const deploymentKeys = [
    "schemaVersion",
    "provider",
    "access",
    "projectId",
    "versionId",
    "deploymentId",
    "origin",
    "deployedAt",
    "sourceCommit",
    "policySha256",
    "routes",
  ];
  const policySha256 = createHash("sha256").update(policyBytes).digest("hex");
  if (
    !exactKeys(pointer, ["schemaVersion", "deploymentEvidence"]) ||
    pointer.schemaVersion !== 1 ||
    !/^deployments\/appgdep_[a-z0-9]+\.json$/.test(pointer.deploymentEvidence) ||
    !exactKeys(deployment, deploymentKeys) ||
    deployment.schemaVersion !== 1 ||
    deployment.provider !== "OpenAI Sites" ||
    deployment.access !== "public" ||
    deployment.origin !== PUBLIC_ORIGIN ||
    deployment.policySha256 !== policySha256 ||
    pointer.deploymentEvidence !== `deployments/${deployment.deploymentId}.json` ||
    !/^appgprj_[a-z0-9]+$/.test(deployment.projectId) ||
    !/^appgprj_[a-z0-9]+~appgver_[a-z0-9]+$/.test(deployment.versionId) ||
    !/^appgdep_[a-z0-9]+$/.test(deployment.deploymentId) ||
    !/^[0-9a-f]{40}$/.test(deployment.sourceCommit) ||
    !isValidUtcRfc3339(deployment.deployedAt) ||
    !isDeepStrictEqual(deployment.routes, [
      { path: "/", status: 200, contentType: "text/html", check: "passed" },
      { path: "/privacy", status: 200, contentType: "text/html", check: "passed" },
      { path: "/support", status: 200, contentType: "text/html", check: "passed" },
    ])
  ) {
    fail("MACOS_PUBLIC_SITE_INVALID");
  }
  return {
    deploymentEvidence: pointer.deploymentEvidence,
    origin: deployment.origin,
    policySha256,
    privacyPolicy: `${deployment.origin}/privacy`,
    support: `${deployment.origin}/support`,
  };
}

export async function inspectMacOSStoreRepository(root) {
  if (!path.isAbsolute(root)) fail();
  const desktopRoot = path.join(root, "apps", "desktop");
  const releaseRoot = path.join(desktopRoot, "release", "macos");
  const paths = {
    appEntitlements: path.join(releaseRoot, "entitlements.mas.plist"),
    companyControllerAttestation: path.join(
      root,
      "docs",
      "release",
      "authority",
      "publisher-controller.json",
    ),
    childEntitlements: path.join(releaseRoot, "entitlements.mas.child.plist"),
    coreEntitlements: path.join(releaseRoot, "entitlements.mas.core.plist"),
    sbrHelperEntitlements: path.join(releaseRoot, "entitlements.mas.sbr-helper.plist"),
    forge: path.join(desktopRoot, "forge.config.ts"),
    icon: path.join(desktopRoot, "assets", "icon-source.png"),
    icns: path.join(desktopRoot, "assets", "icon.icns"),
    metadata: path.join(releaseRoot, "store-metadata.md"),
    package: path.join(desktopRoot, "package.json"),
    privacy: path.join(releaseRoot, "PrivacyInfo.xcprivacy"),
    privacyPolicy: path.join(root, "PRIVACY.md"),
    profile: path.join(releaseRoot, "profile.ts"),
    publicSiteCurrent: path.join(root, "docs", "release", "public-site", "current.json"),
    readme: path.join(root, "README.md"),
    releaseStateSchema: path.join(releaseRoot, "release-state.schema.json"),
    runbook: path.join(root, "docs", "release", "macos-app-store.md"),
    techState: path.join(root, "docs", "development", "tech-state.md"),
    identity: path.join(releaseRoot, "store-identity.json"),
  };
  await access(paths.icns).catch(() => fail());
  const [
    appEntitlements,
    childEntitlements,
    coreEntitlements,
    sbrHelperEntitlements,
    forge,
    iconBytes,
    identityBytes,
    metadata,
    packageBytes,
    privacy,
    privacyPolicyBytes,
    profile,
    publicSiteCurrentBytes,
    readme,
    releaseStateSchemaBytes,
    runbook,
    techState,
  ] = await Promise.all([
    readMacOSRepositoryPlist(paths.appEntitlements),
    readMacOSRepositoryPlist(paths.childEntitlements),
    readMacOSRepositoryPlist(paths.coreEntitlements),
    readMacOSRepositoryPlist(paths.sbrHelperEntitlements),
    readFile(paths.forge, "utf8"),
    readFile(paths.icon),
    readFile(paths.identity),
    readFile(paths.metadata, "utf8"),
    readFile(paths.package),
    readMacOSRepositoryPlist(paths.privacy),
    readFile(paths.privacyPolicy),
    readFile(paths.profile, "utf8"),
    readFile(paths.publicSiteCurrent),
    readFile(paths.readme, "utf8"),
    readFile(paths.releaseStateSchema),
    readFile(paths.runbook, "utf8"),
    readFile(paths.techState, "utf8"),
  ]).catch(() => fail());

  const desktopPackage = JSON.parse(packageBytes.toString("utf8"));
  const identity = validateMacOSStoreIdentity(JSON.parse(identityBytes.toString("utf8")));
  const publicSitePointer = JSON.parse(publicSiteCurrentBytes.toString("utf8"));
  const publicSiteRoot = path.dirname(paths.publicSiteCurrent);
  const deploymentPath = path.resolve(publicSiteRoot, publicSitePointer.deploymentEvidence ?? "");
  if (!deploymentPath.startsWith(`${publicSiteRoot}${path.sep}`)) fail("MACOS_PUBLIC_SITE_INVALID");
  const publicSite = validateCurrentPublicSite(
    publicSitePointer,
    JSON.parse(await readFile(deploymentPath, "utf8")),
    privacyPolicyBytes,
  );
  const releaseStateSchema = JSON.parse(releaseStateSchemaBytes.toString("utf8"));
  let companyControllerAttestationValid = false;
  try {
    validateCompanyControllerAttestation(
      JSON.parse(await readFile(paths.companyControllerAttestation, "utf8")),
    );
    companyControllerAttestationValid = true;
  } catch {
    // This gate deliberately reports only the blocker, never record contents.
  }
  const icon = readPngDimensions(iconBytes);
  const metadataStatus = validateMacOSStoreMetadata(metadata);
  validateMacOSStorePlists({
    appEntitlements,
    childEntitlements,
    coreEntitlements,
    sbrHelperEntitlements,
    privacy,
  });
  if (
    identity.installedName !== desktopPackage.productName ||
    identity.bundleIdentifier !== APP_BUNDLE_ID ||
    identity.minimumMacOSVersion !== "14.0" ||
    identity.architectures.join(",") !== "arm64"
  ) {
    fail("MACOS_STORE_IDENTITY_MISMATCH");
  }
  if (
    metadataStatus.marketingVersion !== desktopPackage.version ||
    metadataStatus.publisher !== identity.publisher ||
    metadataStatus.supportEmail !== identity.supportEmail ||
    metadataStatus.privacyPolicy !== publicSite.privacyPolicy ||
    metadataStatus.support !== publicSite.support ||
    releaseStateSchema?.$schema !== "https://json-schema.org/draft/2020-12/schema" ||
    !Array.isArray(releaseStateSchema?.oneOf)
  ) {
    fail("MACOS_STORE_METADATA_OWNER_MISMATCH");
  }
  if (
    desktopPackage?.productName !== "Tammy" ||
    typeof desktopPackage?.version !== "string" ||
    !/^[0-9]+\.[0-9]+\.[0-9]+$/.test(desktopPackage.version) ||
    icon.width !== 1024 ||
    icon.height !== 1024 ||
    !includesAll(profile, [APP_BUNDLE_ID, APP_CATEGORY, "TAMMY_MACOS_BUILD_NUMBER"]) ||
    !includesAll(forge, [
      "createMacOSReleaseProfile",
      '"darwin-arm64"',
      "ignore: isManifestBoundExecutable",
      "packagedSbrHelperSuffix",
      "releaseProfile.privacyManifest",
    ]) ||
    forge.includes("process.arch") ||
    !readme.includes("docs/release/macos-app-store.md") ||
    !["pnpm check:macos-store", "task release:check"].some((command) => readme.includes(command)) ||
    !includesAll(runbook, [
      "TAMMY_MACOS_BUILD_NUMBER",
      "Apple Development",
      "Apple Distribution",
      "/usr/bin/productbuild",
      "codesign",
      "Transporter",
    ]) ||
    !techState.includes("../release/macos-app-store.md")
  ) {
    fail();
  }

  const releaseState = evaluateReleaseState({
    releaseVersion: desktopPackage.version,
    buildNumber: "1",
    repository: {
      storeIdentity: true,
      publicSite: true,
      metadata: true,
      platformIdentity: false,
      policy: true,
      schemas: true,
      screenshotDefinitions: false,
      tests: true,
    },
    candidate: null,
    attestations: [],
    events: [],
  });
  const blockers = [
    ...releaseState.blockers.map(({ code }) => code),
    ...(companyControllerAttestationValid ? [] : ["company-controller-attestation"]),
  ].sort();
  const passed = [
    ...releaseState.passed,
    ...(companyControllerAttestationValid ? ["publisher-authority"] : []),
  ].sort();

  return {
    appBundleId: APP_BUNDLE_ID,
    category: APP_CATEGORY,
    icon,
    identity,
    metadataComplete: metadataStatus.complete,
    metadata: metadataStatus,
    publicSite,
    releaseState,
    blockers,
    operatorRequirements: [
      "certificates-and-profiles",
      ...METADATA_OPERATOR_CONFIRMATIONS,
      "screenshots",
      "signed-build-privacy-report",
    ],
    passed,
    version: desktopPackage.version,
  };
}

async function requireCleanTree(root) {
  const { stdout } = await execFile("git", ["status", "--porcelain"], { cwd: root });
  if (stdout !== "") fail("MACOS_RELEASE_TREE_DIRTY");
}

async function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const release = process.argv.slice(2);
  if (release.some((argument) => argument !== "--release") || release.length > 1) {
    fail("MACOS_STORE_ARGUMENT_INVALID");
  }
  const result = await inspectMacOSStoreRepository(root);
  let releaseInput;
  if (release[0] === "--release") {
    if (result.blockers.includes("company-controller-attestation")) {
      fail("MACOS_STORE_COMPANY_AUTHORITY_MISSING");
    }
    if (!result.metadataComplete) fail("MACOS_RELEASE_METADATA_INCOMPLETE");
    releaseInput = validateMacOSReleaseEnvironment(process.env);
    assertMacOSReleaseMetadata(result.metadata, process.env);
    const provisioningProfile = required(process.env, "TAMMY_MACOS_PROVISIONING_PROFILE");
    await access(provisioningProfile);
    validateMacOSProvisioningProfile(await readMacOSProvisioningProfile(provisioningProfile), {
      mode: releaseInput.mode,
      teamID: required(process.env, "TAMMY_MACOS_TEAM_ID"),
    });
    await requireCleanTree(root);
  }
  process.stdout.write(
    `${JSON.stringify({
      ...result,
      ...(releaseInput === undefined ? {} : { release: releaseInput }),
      status: releaseInput === undefined ? "NOT_READY" : "SIGNED_BUILD_INPUTS_READY",
    })}\n`,
  );
}

if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : "MACOS_STORE_CHECK_FAILED"}\n`,
    );
    process.exitCode = 1;
  });
}
