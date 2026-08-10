import path from "node:path";

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
} from "./release/macos/profile";

const desktopRoot = import.meta.dirname;
const releaseProfile = createMacOSReleaseProfile(process.env, desktopRoot);

const packagedCoreSuffix = path.join("Contents", "Resources", "core", "darwin-arm64", "tammy-core");

function isManifestBoundCore(file: string): boolean {
  return path.isAbsolute(file) && file.endsWith(`${path.sep}${packagedCoreSuffix}`);
}

const developmentSign: NonNullable<ForgeConfig["packagerConfig"]>["osxSign"] = {
  identity: "-",
  identityValidation: false,
  ignore: isManifestBoundCore,
  optionsForFile: () => ({ hardenedRuntime: false, timestamp: "none" }),
};

const packagerConfig: NonNullable<ForgeConfig["packagerConfig"]> = {
  asar: true,
  executableName: "Tammy",
  extraResource: [
    "resources/core",
    "resources/build",
    "resources/sqlcipher",
    ...(releaseProfile.kind === "mas" ? [releaseProfile.privacyManifest] : []),
  ],
  osxSign: developmentSign,
};

if (process.platform === "darwin") {
  packagerConfig.appBundleId = MACOS_APP_BUNDLE_ID;
  packagerConfig.appCategoryType = MACOS_APP_CATEGORY;
  packagerConfig.helperBundleId = `${MACOS_APP_BUNDLE_ID}.helper`;
  packagerConfig.icon = path.join(desktopRoot, "assets", "icon.icns");
}

if (releaseProfile.kind === "mas") {
  packagerConfig.appBundleId = releaseProfile.appBundleId;
  packagerConfig.appCategoryType = releaseProfile.category;
  packagerConfig.buildVersion = releaseProfile.buildVersion;
  packagerConfig.extendInfo = releaseProfile.info;
  packagerConfig.helperBundleId = `${releaseProfile.appBundleId}.helper`;
  packagerConfig.icon = releaseProfile.icon;
  packagerConfig.osxSign = {
    identity: releaseProfile.sign.identity,
    identityValidation: true,
    ignore: isManifestBoundCore,
    optionsForFile: (file: string) => ({
      entitlements: releaseProfile.sign.entitlementsFor(file),
    }),
    provisioningProfile: releaseProfile.sign.provisioningProfile,
    type: releaseProfile.sign.type,
  };
}

const config: ForgeConfig = {
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
