const { execFile: nodeExecFile } = require("node:child_process");
const { promises: nodeFs } = require("node:fs");
const path = require("node:path");
const { promisify } = require("node:util");

const runNodeCommand = promisify(nodeExecFile);
const APP_ID = "com.tammy.desktop";
const COPYRIGHT = "© 2026 Gamma Systems Pty Ltd";
const MINIMUM_MACOS_VERSION = "14.0";
const UNUSED_MACOS_INFO_KEYS = Object.freeze([
  "NSAppTransportSecurity",
  "NSAudioCaptureUsageDescription",
  "NSBluetoothAlwaysUsageDescription",
  "NSBluetoothPeripheralUsageDescription",
  "NSCameraUsageDescription",
  "NSMicrophoneUsageDescription",
]);
const HELPER_BUNDLES = Object.freeze([
  ["Tammy Helper.app", `${APP_ID}.helper`],
  ["Tammy Helper (GPU).app", `${APP_ID}.helper.GPU`],
  ["Tammy Helper (Plugin).app", `${APP_ID}.helper.Plugin`],
  ["Tammy Helper (Renderer).app", `${APP_ID}.helper.Renderer`],
]);

function fail(code) {
  throw new Error(code);
}

function normalizeArchitecture(arch) {
  if (arch === "arm64" || arch === 3) return "arm64";
  if (arch === "x64" || arch === 1) return "x64";
  return undefined;
}

function isContained(root, candidate) {
  const relative = path.relative(root, candidate);
  return relative !== "" && !relative.startsWith("..") && !path.isAbsolute(relative);
}

function validateEnvironmentRoot(environment) {
  const root = environment?.TAMMY_ELECTRON_BUILDER_OUTPUT;
  if (typeof root !== "string" || !path.isAbsolute(root) || path.normalize(root) !== root) {
    fail("BUILDER_OUTPUT_ROOT_INVALID");
  }
  return root;
}

async function assertRealContainedDirectory(fs, root, candidate) {
  const rootStatus = await fs.lstat(root).catch(() => fail("BUILDER_OUTPUT_ROOT_INVALID"));
  if (!rootStatus.isDirectory() || rootStatus.isSymbolicLink()) fail("BUILDER_OUTPUT_ROOT_INVALID");
  const rootRealPath = await fs.realpath(root).catch(() => fail("BUILDER_OUTPUT_ROOT_INVALID"));
  if (rootRealPath !== root) fail("BUILDER_OUTPUT_ROOT_INVALID");
  if (!isContained(root, candidate)) fail("BUILDER_APP_OUT_DIR_INVALID");

  let cursor = candidate;
  while (true) {
    const status = await fs.lstat(cursor).catch(() => fail("BUILDER_APP_OUT_DIR_INVALID"));
    if (status.isSymbolicLink()) fail("BUILDER_APP_OUT_DIR_INVALID");
    if (cursor === root) break;
    const realPath = await fs.realpath(cursor).catch(() => fail("BUILDER_APP_OUT_DIR_INVALID"));
    if (realPath !== cursor) fail("BUILDER_APP_OUT_DIR_INVALID");
    const parent = path.dirname(cursor);
    if (parent === cursor || (!isContained(root, parent) && parent !== root)) {
      fail("BUILDER_APP_OUT_DIR_INVALID");
    }
    cursor = parent;
  }
  const candidateStatus = await fs.lstat(candidate);
  if (!candidateStatus.isDirectory()) fail("BUILDER_APP_OUT_DIR_INVALID");
}

async function assertRegularExecutable(fs, file, code) {
  const status = await fs.lstat(file).catch(() => fail(code));
  if (!status.isFile() || status.isSymbolicLink()) fail(code);
  await fs.chmod(file, 0o755).catch(() => fail(code));
}

async function readPlistJson(runCommand, plistPath) {
  let result;
  try {
    result = await runCommand("/usr/bin/plutil", ["-convert", "json", "-o", "-", plistPath]);
  } catch {
    fail("BUILDER_INFO_PLIST_INVALID");
  }
  try {
    return JSON.parse(String(result?.stdout ?? result ?? ""));
  } catch {
    fail("BUILDER_INFO_PLIST_INVALID");
  }
}

async function replacePlistValue(runCommand, plistPath, key, type, value) {
  try {
    await runCommand("/usr/bin/plutil", ["-replace", key, `-${type}`, String(value), plistPath]);
  } catch {
    fail("BUILDER_INFO_PLIST_INVALID");
  }
}

async function normalizeMacOSPlists(fs, runCommand, appOutDir, masEnvironment) {
  const infoPlist = path.join(appOutDir, "Contents", "Info.plist");
  const info = await readPlistJson(runCommand, infoPlist);
  for (const key of UNUSED_MACOS_INFO_KEYS) {
    if (!Object.hasOwn(info, key)) continue;
    try {
      await runCommand("/usr/bin/plutil", ["-remove", key, infoPlist]);
    } catch {
      fail("BUILDER_INFO_PLIST_INVALID");
    }
  }

  const frameworkRoot = path.join(appOutDir, "Contents", "Frameworks");
  for (const [bundleName, identifier] of HELPER_BUNDLES) {
    const helperPlist = path.join(frameworkRoot, bundleName, "Contents", "Info.plist");
    await replacePlistValue(runCommand, helperPlist, "CFBundleIdentifier", "string", identifier);
    const helperInfo = await readPlistJson(runCommand, helperPlist);
    if (helperInfo.CFBundleIdentifier !== identifier) fail("BUILDER_HELPER_IDENTIFIERS_INVALID");
  }

  if (masEnvironment) {
    await writeMasInfo(fs, runCommand, infoPlist, masEnvironment);
  }
}

function requireMasEnvironment(environment) {
  const required = [
    "TAMMY_MACOS_ARTIFACT_PHASE",
    "TAMMY_MACOS_BUILD_NUMBER",
    "TAMMY_MACOS_EXPORT_COMPLIANCE",
    "TAMMY_MACOS_PRIVACY_POLICY_URL",
    "TAMMY_MACOS_SUPPORT_URL",
    "TAMMY_MACOS_TEAM_ID",
    "TAMMY_MACOS_TARGET",
  ];
  if (
    required.some((name) => typeof environment?.[name] !== "string" || !environment[name].trim())
  ) {
    fail("BUILDER_MAS_ENV_INVALID");
  }
  if (
    environment.TAMMY_MACOS_ARTIFACT_PHASE !== "unsigned-staging" ||
    environment.TAMMY_MACOS_TARGET !== "mas/arm64" ||
    !/^[1-9][0-9]*$/.test(environment.TAMMY_MACOS_BUILD_NUMBER) ||
    !["exempt", "non-exempt"].includes(environment.TAMMY_MACOS_EXPORT_COMPLIANCE) ||
    !/^[A-Z0-9]{10}$/.test(environment.TAMMY_MACOS_TEAM_ID) ||
    !isHttpsUrl(environment.TAMMY_MACOS_PRIVACY_POLICY_URL) ||
    !isHttpsUrl(environment.TAMMY_MACOS_SUPPORT_URL)
  ) {
    fail("BUILDER_MAS_ENV_INVALID");
  }
  return environment;
}

function isHttpsUrl(value) {
  try {
    const parsed = new URL(value);
    if (parsed.protocol !== "https:") return false;
    for (const character of value) {
      const code = character.charCodeAt(0);
      if (code <= 0x1f || code === 0x7f) return false;
    }
    return true;
  } catch {
    return false;
  }
}

async function writeMasInfo(_fs, runCommand, infoPlist, environment) {
  const exportValue = environment.TAMMY_MACOS_EXPORT_COMPLIANCE === "non-exempt";
  const values = [
    ["CFBundleVersion", "string", environment.TAMMY_MACOS_BUILD_NUMBER],
    ["ElectronTeamID", "string", environment.TAMMY_MACOS_TEAM_ID],
    ["ITSAppUsesNonExemptEncryption", "bool", exportValue],
    ["LSMinimumSystemVersion", "string", MINIMUM_MACOS_VERSION],
    ["NSHumanReadableCopyright", "string", COPYRIGHT],
    ["TammyPrivacyPolicyURL", "string", environment.TAMMY_MACOS_PRIVACY_POLICY_URL],
    ["TammySupportURL", "string", environment.TAMMY_MACOS_SUPPORT_URL],
  ];
  for (const [key, type, value] of values) {
    await replacePlistValue(runCommand, infoPlist, key, type, value);
  }
  const info = await readPlistJson(runCommand, infoPlist);
  if (
    info.CFBundleVersion !== environment.TAMMY_MACOS_BUILD_NUMBER ||
    info.ElectronTeamID !== environment.TAMMY_MACOS_TEAM_ID ||
    info.ITSAppUsesNonExemptEncryption !== exportValue ||
    info.LSMinimumSystemVersion !== MINIMUM_MACOS_VERSION ||
    info.NSHumanReadableCopyright !== COPYRIGHT ||
    info.TammyPrivacyPolicyURL !== environment.TAMMY_MACOS_PRIVACY_POLICY_URL ||
    info.TammySupportURL !== environment.TAMMY_MACOS_SUPPORT_URL
  ) {
    fail("BUILDER_MAS_INFO_INVALID");
  }
}

function createAfterPackHook({
  environment = process.env,
  fs = nodeFs,
  runCommand = runNodeCommand,
} = {}) {
  return async function afterPack(context) {
    const platform = context?.electronPlatformName;
    const architecture = normalizeArchitecture(context?.arch);
    if (!platform || !architecture || !["darwin", "mas", "win32"].includes(platform)) {
      fail("BUILDER_TARGET_INVALID");
    }
    if (
      (platform === "darwin" && architecture !== "arm64") ||
      (platform === "mas" && architecture !== "arm64") ||
      (platform === "win32" && architecture !== "x64")
    ) {
      fail("BUILDER_TARGET_INVALID");
    }
    const outputRoot = validateEnvironmentRoot(environment);
    const appOutDir = context?.appOutDir;
    if (
      typeof appOutDir !== "string" ||
      !path.isAbsolute(appOutDir) ||
      path.normalize(appOutDir) !== appOutDir
    ) {
      fail("BUILDER_APP_OUT_DIR_INVALID");
    }
    await assertRealContainedDirectory(fs, outputRoot, appOutDir);

    if (platform === "win32") {
      await assertRegularExecutable(
        fs,
        path.join(appOutDir, "resources", "core", "win32-x64", "tammy-core.exe"),
        "BUILDER_CORE_RESOURCE_INVALID",
      );
      return;
    }

    const appBundle = path.join(appOutDir, "Tammy.app");
    await assertRealContainedDirectory(fs, outputRoot, appBundle);
    const masEnvironment = platform === "mas" ? requireMasEnvironment(environment) : undefined;
    await normalizeMacOSPlists(fs, runCommand, appBundle, masEnvironment);
    await assertRegularExecutable(
      fs,
      path.join(appBundle, "Contents", "Resources", "core", "darwin-arm64", "tammy-core"),
      "BUILDER_CORE_RESOURCE_INVALID",
    );
    await assertRegularExecutable(
      fs,
      path.join(
        appBundle,
        "Contents",
        "Resources",
        "sbr-helper",
        "darwin-arm64",
        "tammy-sbr-helper",
      ),
      "BUILDER_SBR_HELPER_RESOURCE_INVALID",
    );
  };
}

module.exports = createAfterPackHook();
module.exports.createAfterPackHook = createAfterPackHook;
