import { execFile as nodeExecFile } from "node:child_process";
import { createHash } from "node:crypto";
import {
  access,
  lstat,
  mkdtemp,
  readdir,
  readFile,
  realpath,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { isDeepStrictEqual, promisify } from "node:util";

import Ajv2020 from "ajv/dist/2020.js";

import { evaluateReleaseState, validateReleaseAttestation } from "./macos-release-state.mjs";
import {
  readMacOSLifecycleEvents,
  validateBuildLedger,
  validateConsumedBuildNumbers,
} from "./reserve-macos-build.mjs";

const execFile = promisify(nodeExecFile);
const PNG_SIGNATURE = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
const APP_BUNDLE_ID = "com.tammy.desktop";
const APP_CATEGORY = "public.app-category.finance";
const CANONICAL_METADATA_SHA256 =
  "346eb61962f8d2aa2b0ffe6144f6fbcd5825859f9525765ef129d1a970ca5f23";

function fail(code = "MACOS_STORE_REPOSITORY_INVALID") {
  throw new Error(code);
}

async function assertContainedPath(repositoryRoot, target, type, code) {
  const relative = path.relative(repositoryRoot, target);
  if (relative === "" || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    fail(code);
  }
  const rootStatus = await lstat(repositoryRoot).catch(() => fail(code));
  if (!rootStatus.isDirectory() || rootStatus.isSymbolicLink()) fail(code);
  let current = repositoryRoot;
  for (const segment of relative.split(path.sep)) {
    current = path.join(current, segment);
    const status = await lstat(current).catch(() => fail(code));
    if (status.isSymbolicLink()) fail(code);
  }
  const status = await lstat(target).catch(() => fail(code));
  if ((type === "file" && !status.isFile()) || (type === "directory" && !status.isDirectory())) {
    fail(code);
  }
  const resolvedRoot = await realpath(repositoryRoot).catch(() => fail(code));
  const resolvedTarget = await realpath(target).catch(() => fail(code));
  if (!resolvedTarget.startsWith(`${resolvedRoot}${path.sep}`)) fail(code);
}

async function readContainedFile(repositoryRoot, target, code) {
  await assertContainedPath(repositoryRoot, target, "file", code);
  return readFile(target);
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
  const target = required(environment, "TAMMY_MACOS_TARGET");
  requiredHttps(environment, "TAMMY_MACOS_SUPPORT_URL");
  if (
    !/^[1-9][0-9]*$/.test(buildNumber) ||
    !["exempt", "non-exempt"].includes(exportCompliance) ||
    !["development", "distribution"].includes(mode) ||
    !path.isAbsolute(provisioningProfile) ||
    !/^[A-Z0-9]{10}$/.test(teamID) ||
    target !== "mas/arm64"
  ) {
    fail("MACOS_RELEASE_INPUT_INVALID");
  }
  const installerIdentity =
    mode === "distribution" ? required(environment, "TAMMY_MACOS_INSTALLER_IDENTITY") : undefined;
  const signingCertificateClasses =
    mode === "distribution" ? ["Apple Distribution"] : ["Apple Development"];
  if (
    !signingCertificateClasses.some((certificateClass) =>
      matchesIdentity(signingIdentity, certificateClass, teamID),
    ) ||
    (installerIdentity !== undefined &&
      !["Mac Installer Distribution"].some((certificateClass) =>
        matchesIdentity(installerIdentity, certificateClass, teamID),
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
  "active-agreements",
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
  "active-agreements": "Active agreements",
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
  if (
    typeof source !== "string" ||
    createHash("sha256").update(source, "utf8").digest("hex") !== CANONICAL_METADATA_SHA256
  ) {
    fail();
  }
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

export function validateMacOSBuildReservation(ledger, releaseVersion, buildNumber) {
  try {
    validateBuildLedger(ledger);
  } catch {
    fail("MACOS_BUILD_NOT_RESERVED");
  }
  if (
    !ledger.entries.some(
      (entry) => entry.buildNumber === buildNumber && entry.marketingVersion === releaseVersion,
    )
  ) {
    fail("MACOS_BUILD_NOT_RESERVED");
  }
}

export function validateMacOSSellerEligibility(
  attestation,
  { buildNumber, releaseVersion, teamID },
) {
  try {
    validateReleaseAttestation(attestation);
  } catch {
    fail("MACOS_SELLER_ELIGIBILITY_MISSING");
  }
  if (
    attestation.kind !== "seller-eligibility" ||
    attestation.buildNumber !== buildNumber ||
    attestation.releaseVersion !== releaseVersion ||
    attestation.teamId !== teamID
  ) {
    fail("MACOS_SELLER_ELIGIBILITY_MISSING");
  }
}

export async function readValidatedMacOSReleaseFacts(root, environment, inspectedResult) {
  const result =
    inspectedResult ?? (await inspectMacOSStoreRepository(root, { repositoryTestsPassed: false }));
  if (result.blockers.includes("company-controller-attestation")) {
    fail("MACOS_STORE_COMPANY_AUTHORITY_MISSING");
  }
  if (!result.metadataComplete) fail("MACOS_RELEASE_METADATA_INCOMPLETE");
  const release = validateMacOSReleaseEnvironment(environment);
  if (
    !result.buildReservations.some(
      (entry) =>
        entry.buildNumber === release.buildNumber && entry.marketingVersion === result.version,
    )
  ) {
    fail("MACOS_BUILD_NOT_RESERVED");
  }
  if (result.consumedBuildNumbers.includes(release.buildNumber)) {
    fail("MACOS_BUILD_CONSUMED");
  }
  assertMacOSReleaseMetadata(result.metadata, environment);
  if (release.mode === "distribution") {
    const sellerPath = path.join(
      root,
      "docs",
      "release",
      "records",
      "macos",
      result.version,
      `build-${release.buildNumber}`,
      "attestations",
      "seller-eligibility.json",
    );
    let seller;
    try {
      seller = JSON.parse(
        (await readContainedFile(root, sellerPath, "MACOS_SELLER_ELIGIBILITY_MISSING")).toString(
          "utf8",
        ),
      );
    } catch {
      fail("MACOS_SELLER_ELIGIBILITY_MISSING");
    }
    const teamID = required(environment, "TAMMY_MACOS_TEAM_ID");
    validateMacOSSellerEligibility(seller, {
      buildNumber: release.buildNumber,
      releaseVersion: result.version,
      teamID,
    });
  }
  return {
    facts: {
      identity: {
        appStoreName: result.identity.appStoreName,
        architectures: result.identity.architectures,
        bundleIdentifier: result.identity.bundleIdentifier,
        copyright: result.identity.copyright,
        installedName: result.identity.installedName,
        minimumMacOSVersion: result.identity.minimumMacOSVersion,
        publisher: result.identity.publisher,
        supportEmail: result.identity.supportEmail,
      },
      marketingVersion: result.version,
      publicLinks: {
        privacyPolicy: result.publicSite.privacyPolicy,
        support: result.publicSite.support,
      },
      target: "mas/arm64",
    },
    release,
  };
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
    !deployment.versionId.startsWith(`${deployment.projectId}~appgver_`) ||
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

async function optionalDirectoryEntries(repositoryRoot, directory, code) {
  let status;
  try {
    status = await lstat(directory);
  } catch (error) {
    if (error?.code === "ENOENT") return [];
    fail(code);
  }
  if (!status.isDirectory() || status.isSymbolicLink()) fail(code);
  await assertContainedPath(repositoryRoot, directory, "directory", code);
  return readdir(directory, { withFileTypes: true });
}

async function readMacOSReleaseRecord(
  root,
  recordsRoot,
  releaseVersion,
  buildNumber,
  lifecycleRecords,
) {
  const code = "MACOS_RELEASE_RECORD_INVALID";
  const buildRoot = path.join(recordsRoot, releaseVersion, `build-${buildNumber}`);
  const attestations = [];
  const attestationRoot = path.join(buildRoot, "attestations");
  for (const entry of await optionalDirectoryEntries(root, attestationRoot, code)) {
    if (
      !entry.isFile() ||
      entry.isSymbolicLink() ||
      !entry.name.endsWith(".json") ||
      entry.name.endsWith(".example.json")
    ) {
      fail(code);
    }
    const attestation = JSON.parse(
      (await readContainedFile(root, path.join(attestationRoot, entry.name), code)).toString(
        "utf8",
      ),
    );
    try {
      validateReleaseAttestation(attestation);
    } catch {
      fail(code);
    }
    attestations.push(attestation);
  }

  const candidateMarkers = lifecycleRecords.filter(
    ({ event }) =>
      event.kind === "candidate-built" &&
      event.marketingVersion === releaseVersion &&
      event.buildNumber === buildNumber,
  );
  const candidates = [];
  const candidateRoot = path.join(buildRoot, "evidence", "candidate");
  for (const entry of await optionalDirectoryEntries(root, candidateRoot, code)) {
    if (!entry.isDirectory() || entry.isSymbolicLink()) fail(code);
    const candidatePath = path.join(candidateRoot, entry.name, "candidate.json");
    let candidateStatus;
    try {
      candidateStatus = await lstat(candidatePath);
    } catch (error) {
      if (error?.code === "ENOENT") continue;
      fail(code);
    }
    if (!candidateStatus.isFile() || candidateStatus.isSymbolicLink()) fail(code);
    const candidate = JSON.parse(
      (await readContainedFile(root, candidatePath, code)).toString("utf8"),
    );
    if (
      candidateMarkers.some(
        ({ event }) =>
          event.productSourceCommit === candidate.sourceCommit &&
          event.productSourceTree === candidate.sourceTree &&
          event.appSha256 === candidate.appSha256 &&
          event.packageSha256 === candidate.packageSha256,
      )
    ) {
      candidates.push(candidate);
    }
  }
  if (candidateMarkers.length !== candidates.length || candidates.length > 1) fail(code);
  return {
    attestations,
    candidate: candidates[0] ?? null,
    events: lifecycleRecords
      .map(({ event }) => event)
      .filter(
        (event) =>
          event.kind !== "candidate-built" &&
          event.releaseVersion === releaseVersion &&
          event.buildNumber === buildNumber,
      ),
  };
}

export async function inspectMacOSStoreRepository(
  root,
  { repositoryTestsPassed = false, screenshotDefinitionsPassed = false } = {},
) {
  if (!path.isAbsolute(root)) fail();
  const desktopRoot = path.join(root, "apps", "desktop");
  const releaseRoot = path.join(desktopRoot, "release", "macos");
  const paths = {
    appEntitlements: path.join(releaseRoot, "entitlements.mas.plist"),
    buildLedger: path.join(releaseRoot, "build-numbers.json"),
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
    releaseRecords: path.join(root, "docs", "release", "records", "macos"),
    runbook: path.join(root, "docs", "release", "macos-app-store.md"),
    techState: path.join(root, "docs", "development", "tech-state.md"),
    identity: path.join(releaseRoot, "store-identity.json"),
  };
  await access(paths.icns).catch(() => fail());
  const [
    appEntitlements,
    buildLedgerBytes,
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
    readContainedFile(root, paths.buildLedger, "MACOS_BUILD_LEDGER_INVALID"),
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
  ]).catch((error) => {
    if (error instanceof Error && /^MACOS_[A-Z0-9_]+$/.test(error.message)) throw error;
    fail();
  });

  const desktopPackage = JSON.parse(packageBytes.toString("utf8"));
  const buildLedger = validateBuildLedger(JSON.parse(buildLedgerBytes.toString("utf8")));
  await assertContainedPath(
    root,
    paths.releaseRecords,
    "directory",
    "MACOS_RELEASE_RECORD_INVALID",
  );
  const lifecycleRecords = await readMacOSLifecycleEvents(paths.releaseRecords);
  const consumedBuildNumbers = validateConsumedBuildNumbers(buildLedger, lifecycleRecords);
  const identity = validateMacOSStoreIdentity(JSON.parse(identityBytes.toString("utf8")));
  const publicSitePointer = JSON.parse(publicSiteCurrentBytes.toString("utf8"));
  const publicSiteRoot = path.dirname(paths.publicSiteCurrent);
  const repositoryStatus = await lstat(root).catch(() => fail("MACOS_PUBLIC_SITE_INVALID"));
  const publicSiteRootStatus = await lstat(publicSiteRoot).catch(() =>
    fail("MACOS_PUBLIC_SITE_INVALID"),
  );
  const resolvedRepositoryRoot = await realpath(root).catch(() =>
    fail("MACOS_PUBLIC_SITE_INVALID"),
  );
  const resolvedPublicSiteRoot = await realpath(publicSiteRoot);
  if (
    !repositoryStatus.isDirectory() ||
    repositoryStatus.isSymbolicLink() ||
    !publicSiteRootStatus.isDirectory() ||
    publicSiteRootStatus.isSymbolicLink() ||
    !resolvedPublicSiteRoot.startsWith(`${resolvedRepositoryRoot}${path.sep}`)
  ) {
    fail("MACOS_PUBLIC_SITE_INVALID");
  }
  const deploymentsDirectory = path.join(publicSiteRoot, "deployments");
  const deploymentsStatus = await lstat(deploymentsDirectory).catch(() =>
    fail("MACOS_PUBLIC_SITE_INVALID"),
  );
  if (!deploymentsStatus.isDirectory() || deploymentsStatus.isSymbolicLink()) {
    fail("MACOS_PUBLIC_SITE_INVALID");
  }
  const deploymentPath = path.resolve(publicSiteRoot, publicSitePointer.deploymentEvidence ?? "");
  if (!deploymentPath.startsWith(`${publicSiteRoot}${path.sep}`)) fail("MACOS_PUBLIC_SITE_INVALID");
  const deploymentStatus = await lstat(deploymentPath).catch(() =>
    fail("MACOS_PUBLIC_SITE_INVALID"),
  );
  const resolvedDeploymentPath = await realpath(deploymentPath).catch(() =>
    fail("MACOS_PUBLIC_SITE_INVALID"),
  );
  if (
    !deploymentStatus.isFile() ||
    deploymentStatus.isSymbolicLink() ||
    !resolvedDeploymentPath.startsWith(`${resolvedPublicSiteRoot}${path.sep}`)
  ) {
    fail("MACOS_PUBLIC_SITE_INVALID");
  }
  const publicSite = validateCurrentPublicSite(
    publicSitePointer,
    JSON.parse(await readFile(deploymentPath, "utf8")),
    privacyPolicyBytes,
  );
  const releaseStateSchema = JSON.parse(releaseStateSchemaBytes.toString("utf8"));
  try {
    new Ajv2020({ strict: true }).compile(releaseStateSchema);
  } catch {
    fail("MACOS_RELEASE_SCHEMA_INVALID");
  }
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
    !includesAll(profile, [
      APP_BUNDLE_ID,
      APP_CATEGORY,
      "TAMMY_MACOS_BUILD_NUMBER",
      'CFBundleDisplayName: "Tammy"',
      'LSMinimumSystemVersion: "14.0"',
      'NSHumanReadableCopyright: "© 2026 Gamma Systems Pty Ltd"',
    ]) ||
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

  const selectedReservation = buildLedger.entries
    .filter((entry) => entry.marketingVersion === desktopPackage.version)
    .at(-1);
  const selectedBuildNumber = selectedReservation?.buildNumber ?? "1";
  const releaseRecord = await readMacOSReleaseRecord(
    root,
    paths.releaseRecords,
    desktopPackage.version,
    selectedBuildNumber,
    lifecycleRecords,
  );
  const releaseState = evaluateReleaseState({
    releaseVersion: desktopPackage.version,
    buildNumber: selectedBuildNumber,
    repository: {
      storeIdentity: true,
      publicSite: true,
      metadata: true,
      platformIdentity: true,
      policy: true,
      schemas: true,
      screenshotDefinitions: screenshotDefinitionsPassed === true,
      tests: repositoryTestsPassed === true,
    },
    candidate: releaseRecord.candidate,
    attestations: releaseRecord.attestations,
    events: releaseRecord.events,
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
    buildReservations: buildLedger.entries.map(({ buildNumber, marketingVersion }) => ({
      buildNumber,
      marketingVersion,
    })),
    consumedBuildNumbers,
    category: APP_CATEGORY,
    icon,
    identity,
    metadataComplete: metadataStatus.complete,
    metadata: metadataStatus,
    publicSite,
    releaseState,
    selectedBuildNumber: selectedReservation?.buildNumber ?? null,
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

export function sanitizeMacOSStoreGitEnvironment(environment) {
  return {
    ...Object.fromEntries(Object.entries(environment).filter(([key]) => !key.startsWith("GIT_"))),
    LANG: "C",
    LC_ALL: "C",
  };
}

async function gitStatus(root, pathspec = []) {
  return execFile(
    "/usr/bin/git",
    ["status", "--porcelain=v1", "--untracked-files=all", ...pathspec],
    {
      cwd: root,
      env: sanitizeMacOSStoreGitEnvironment(process.env),
    },
  );
}

export async function assertMacOSReleaseTreeClean(root) {
  const { stdout } = await gitStatus(root);
  if (stdout !== "") fail("MACOS_RELEASE_TREE_DIRTY");
}

async function requireReleaseEvidenceClean(root, result, release) {
  const releaseRecord = path.join(
    "docs",
    "release",
    "records",
    "macos",
    result.version,
    `build-${release.buildNumber}`,
  );
  const evidencePaths = [
    path.join("apps", "desktop", "package.json"),
    path.join("apps", "desktop", "release", "macos", "build-numbers.json"),
    path.join("apps", "desktop", "release", "macos", "store-identity.json"),
    path.join("apps", "desktop", "release", "macos", "store-metadata.md"),
    path.join("docs", "release", "authority", "publisher-controller.json"),
    path.join("docs", "release", "public-site", "current.json"),
    path.join("docs", "release", "public-site", result.publicSite.deploymentEvidence),
    releaseRecord,
  ];
  const { stdout } = await gitStatus(root, ["--", ...evidencePaths]);
  if (stdout !== "") fail("MACOS_RELEASE_EVIDENCE_DIRTY");
}

export function assertMacOSRequiredState(state, requiredState) {
  if (state.buildNumber === null) fail("MACOS_BUILD_NOT_RESERVED");
  if (state.state !== requiredState || state.blockers.length > 0) {
    fail(`MACOS_RELEASE_STATE_GATE_FAILED:${requiredState}`);
  }
}

async function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const arguments_ = process.argv.slice(2);
  const release = isDeepStrictEqual(arguments_, ["--release"]);
  const profileFacts = isDeepStrictEqual(arguments_, ["--profile-facts"]);
  const runTests = isDeepStrictEqual(arguments_, ["--run-tests"]);
  const stateOnly = isDeepStrictEqual(arguments_, ["--state"]);
  const requiredState =
    arguments_.length === 2 &&
    arguments_[0] === "--require-state" &&
    ["PRE_UPLOAD_READY", "PRE_SUBMIT_READY"].includes(arguments_[1])
      ? arguments_[1]
      : undefined;
  if (
    arguments_.length > 0 &&
    !release &&
    !profileFacts &&
    !runTests &&
    !stateOnly &&
    requiredState === undefined
  ) {
    fail("MACOS_STORE_ARGUMENT_INVALID");
  }
  const verifyTests =
    runTests || release || profileFacts || stateOnly || requiredState !== undefined;
  if (verifyTests) {
    try {
      await execFile(
        process.execPath,
        ["--test", "scripts/check-macos-store.test.mjs", "scripts/macos-release-state.test.mjs"],
        { cwd: root, maxBuffer: 4 * 1024 * 1024 },
      );
    } catch {
      fail("MACOS_REPOSITORY_TESTS_FAILED");
    }
  }
  const result = await inspectMacOSStoreRepository(root, { repositoryTestsPassed: verifyTests });
  if (profileFacts) {
    const { facts, release: profileRelease } = await readValidatedMacOSReleaseFacts(
      root,
      process.env,
      result,
    );
    const provisioningProfile = required(process.env, "TAMMY_MACOS_PROVISIONING_PROFILE");
    await access(provisioningProfile);
    validateMacOSProvisioningProfile(await readMacOSProvisioningProfile(provisioningProfile), {
      mode: profileRelease.mode,
      teamID: required(process.env, "TAMMY_MACOS_TEAM_ID"),
    });
    await requireReleaseEvidenceClean(root, result, profileRelease);
    process.stdout.write(`${JSON.stringify(facts)}\n`);
    return;
  }
  if (stateOnly || requiredState !== undefined) {
    const state = {
      blockers: [
        ...result.releaseState.blockers,
        ...(result.selectedBuildNumber === null
          ? [
              {
                code: "BUILD_NUMBER_NOT_RESERVED",
                owner: "repository",
                remediation: "Reserve an explicit build number for the release.",
              },
            ]
          : []),
        ...(result.blockers.includes("company-controller-attestation")
          ? [
              {
                code: "COMPANY_CONTROLLER_ATTESTATION_MISSING",
                owner: "operator",
                remediation: "Record the confirmed company-controller authority attestation.",
              },
            ]
          : []),
      ],
      buildNumber: result.selectedBuildNumber,
      passed: result.passed,
      state: result.releaseState.state,
    };
    process.stdout.write(`${JSON.stringify(state)}\n`);
    if (requiredState !== undefined) assertMacOSRequiredState(state, requiredState);
    return;
  }
  let releaseInput;
  if (release) {
    ({ release: releaseInput } = await readValidatedMacOSReleaseFacts(root, process.env, result));
    const provisioningProfile = required(process.env, "TAMMY_MACOS_PROVISIONING_PROFILE");
    await access(provisioningProfile);
    validateMacOSProvisioningProfile(await readMacOSProvisioningProfile(provisioningProfile), {
      mode: releaseInput.mode,
      teamID: required(process.env, "TAMMY_MACOS_TEAM_ID"),
    });
    await assertMacOSReleaseTreeClean(root);
  }
  process.stdout.write(
    `${JSON.stringify({
      ...result,
      ...(releaseInput === undefined ? {} : { release: releaseInput }),
      status: releaseInput === undefined ? result.releaseState.state : "SIGNED_BUILD_INPUTS_READY",
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
