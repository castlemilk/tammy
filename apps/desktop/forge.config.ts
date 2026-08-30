import { execFile as nodeExecFile } from "node:child_process";
import { existsSync, lstatSync, realpathSync } from "node:fs";
import path from "node:path";
import { promisify } from "node:util";

import { FuseV1Options, FuseVersion } from "@electron/fuses";
import { MakerSquirrel } from "@electron-forge/maker-squirrel";
import { MakerZIP } from "@electron-forge/maker-zip";
import { FusesPlugin } from "@electron-forge/plugin-fuses";
import { VitePlugin } from "@electron-forge/plugin-vite";
import type { ForgeConfig } from "@electron-forge/shared-types";

import {
  createMacOSReleaseProfile,
  MACOS_APP_BUNDLE_ID,
  MACOS_APP_CATEGORY,
  normalizeMacOSPackagedResourcePermissions,
} from "./release/macos/profile";

const desktopRoot = import.meta.dirname;
const releaseProfile = createMacOSReleaseProfile(process.env, desktopRoot);
const execFile = promisify(nodeExecFile);
const isMacOSStoreProfile =
  releaseProfile.kind === "mas" || releaseProfile.kind === "mas-unsigned-staging";
let unsignedOutputRoot: string | undefined;

const UNUSED_MACOS_INFO_KEYS = Object.freeze([
  "NSAppTransportSecurity",
  "NSAudioCaptureUsageDescription",
  "NSBluetoothAlwaysUsageDescription",
  "NSBluetoothPeripheralUsageDescription",
  "NSCameraUsageDescription",
  "NSMicrophoneUsageDescription",
]);

type PackagerConfig = NonNullable<ForgeConfig["packagerConfig"]>;
type PackagerHook = NonNullable<PackagerConfig["afterCopyExtraResources"]>[number];

const removeUnusedMacOSInfoKeys: PackagerHook = (
  buildPath,
  _electronVersion,
  platform,
  _arch,
  callback,
) => {
  if (platform !== "darwin" && platform !== "mas") {
    callback();
    return;
  }
  const infoPlist = path.join(buildPath, "Tammy.app", "Contents", "Info.plist");
  void execFile("/usr/bin/plutil", ["-convert", "json", "-o", "-", infoPlist], {
    encoding: "utf8",
    maxBuffer: 1024 * 1024,
  })
    .then(async ({ stdout }) => {
      const info = JSON.parse(stdout);
      for (const key of UNUSED_MACOS_INFO_KEYS) {
        if (Object.hasOwn(info, key)) {
          await execFile("/usr/bin/plutil", ["-remove", key, infoPlist]);
        }
      }
    })
    .then(
      () => callback(),
      (error: unknown) =>
        callback(error instanceof Error ? error : new Error("MACOS_INFO_PLIST_INVALID")),
    );
};

const normalizePackagedResourcePermissions: PackagerHook = (
  buildPath,
  _electronVersion,
  platform,
  _arch,
  callback,
) => {
  if (platform !== "darwin" && platform !== "mas") {
    callback();
    return;
  }
  void normalizeMacOSPackagedResourcePermissions(buildPath).then(
    () => callback(),
    (error: unknown) =>
      callback(error instanceof Error ? error : new Error("MACOS_PACKAGED_RESOURCES_INVALID")),
  );
};

const packagedCoreSuffix = path.join("Contents", "Resources", "core", "darwin-arm64", "tammy-core");
const packagedSbrHelperSuffix = path.join(
  "Contents",
  "Resources",
  "sbr-helper",
  "darwin-arm64",
  "tammy-sbr-helper",
);

function isManifestBoundExecutable(file: string): boolean {
  return (
    path.isAbsolute(file) &&
    [packagedCoreSuffix, packagedSbrHelperSuffix].some((suffix) =>
      file.endsWith(`${path.sep}${suffix}`),
    )
  );
}

const developmentSign: NonNullable<ForgeConfig["packagerConfig"]>["osxSign"] = {
  identity: "-",
  identityValidation: false,
  ignore: isManifestBoundExecutable,
  optionsForFile: () => ({ hardenedRuntime: false, timestamp: "none" }),
};

const packagerConfig: PackagerConfig = {
  afterCopyExtraResources: [removeUnusedMacOSInfoKeys, normalizePackagedResourcePermissions],
  asar: true,
  executableName: "Tammy",
  extraResource: [
    "resources/core",
    "resources/build",
    "resources/sqlcipher",
    ...(process.platform === "darwin" ? ["resources/sbr-helper", "resources/sbr"] : []),
    ...(isMacOSStoreProfile ? [releaseProfile.privacyManifest] : []),
  ],
  osxSign: developmentSign,
};

if (process.platform === "darwin") {
  packagerConfig.appBundleId = MACOS_APP_BUNDLE_ID;
  packagerConfig.appCategoryType = MACOS_APP_CATEGORY;
  packagerConfig.helperBundleId = `${MACOS_APP_BUNDLE_ID}.helper`;
  packagerConfig.icon = path.join(desktopRoot, "assets", "icon.icns");
}

if (isMacOSStoreProfile) {
  packagerConfig.appBundleId = releaseProfile.appBundleId;
  packagerConfig.appCategoryType = releaseProfile.category;
  packagerConfig.buildVersion = releaseProfile.buildVersion;
  packagerConfig.extendInfo = releaseProfile.info;
  packagerConfig.helperBundleId = `${releaseProfile.appBundleId}.helper`;
  packagerConfig.icon = releaseProfile.icon;
  if (releaseProfile.kind === "mas") {
    packagerConfig.osxSign = {
      identity: releaseProfile.sign.identity,
      identityValidation: true,
      // Core and the SBR helper are signed before their hashes are written into
      // authenticated manifests. Forge must preserve those exact bytes.
      ignore: isManifestBoundExecutable,
      optionsForFile: (file: string) => ({
        entitlements: releaseProfile.sign.entitlementsFor(file),
      }),
      provisioningProfile: releaseProfile.sign.provisioningProfile,
      type: releaseProfile.sign.type,
    };
  } else {
    const outputRoot = process.env.TAMMY_MACOS_UNSIGNED_OUTPUT_ROOT;
    const repositoryRoot = path.resolve(desktopRoot, "../..");
    const expectedRoot = path.join(
      repositoryRoot,
      ".tmp",
      "macos-release",
      "0.1.0",
      `build-${releaseProfile.buildVersion}`,
      ".forge-unsigned",
    );
    if (outputRoot !== expectedRoot) throw new Error("MACOS_RELEASE_INPUT_INVALID");
    const outputParent = path.dirname(expectedRoot);
    const outputParentStatus = lstatSync(outputParent);
    if (
      !outputParentStatus.isDirectory() ||
      outputParentStatus.isSymbolicLink() ||
      realpathSync.native(outputParent) !== outputParent ||
      existsSync(expectedRoot)
    ) {
      throw new Error("MACOS_RELEASE_INPUT_INVALID");
    }
    unsignedOutputRoot = outputRoot;
    delete packagerConfig.osxSign;
  }
}

const config: ForgeConfig = {
  ...(unsignedOutputRoot === undefined ? {} : { outDir: unsignedOutputRoot }),
  packagerConfig,
  makers: [
    new MakerSquirrel(
      { authors: "Tammy", description: "Local-first Australian accounting software" },
      ["win32"],
    ),
    new MakerZIP({}, ["darwin"]),
  ],
  plugins: [
    new VitePlugin({
      concurrent: 2,
      build: [
        { entry: "src/main/index.ts", config: "vite.main.config.ts" },
        { entry: "src/preload/index.ts", config: "vite.preload.config.ts" },
      ],
      renderer: [{ name: "main_window", config: "vite.renderer.config.ts" }],
    }),
    new FusesPlugin({
      version: FuseVersion.V1,
      [FuseV1Options.RunAsNode]: false,
      [FuseV1Options.EnableCookieEncryption]: true,
      [FuseV1Options.EnableNodeOptionsEnvironmentVariable]: false,
      [FuseV1Options.EnableNodeCliInspectArguments]: true,
      [FuseV1Options.EnableEmbeddedAsarIntegrityValidation]: true,
      [FuseV1Options.OnlyLoadAppFromAsar]: true,
    }),
  ],
};

export default config;
