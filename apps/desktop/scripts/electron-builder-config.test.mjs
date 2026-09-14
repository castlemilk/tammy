import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { parse } from "yaml";

const root = path.resolve(import.meta.dirname, "..");
const configPath = path.join(root, "electron-builder.yml");

function resourceNames(resources) {
  return resources.map((resource) => ({ from: resource.from, to: resource.to }));
}

test("electron-builder configuration is a deterministic Tammy package contract", async () => {
  const config = parse(await readFile(configPath, "utf8"));

  assert.deepEqual(
    {
      appId: config.appId,
      category: config.category,
      productName: config.productName,
      copyright: config.copyright,
    },
    {
      appId: "com.tammy.desktop",
      category: "public.app-category.finance",
      productName: "Tammy",
      copyright: "© 2026 Gamma Systems Pty Ltd",
    },
  );
  assert.equal(config.directories.output, "${env.TAMMY_ELECTRON_BUILDER_OUTPUT}");
  assert.equal(config.directories.buildResources, "assets");
  assert.equal(config.npmRebuild, false);
  assert.equal(config.asar, true);
  assert.equal(config.afterPack, "scripts/electron-builder-after-pack.cjs");
  assert.deepEqual(config.files, [
    "out/**",
    "package.json",
    "!src/**",
    "!tests/**",
    "!*.config.*",
    "!electron-builder.yml",
    "!forge.config.ts",
  ]);
  assert.deepEqual(config.electronFuses, {
    runAsNode: false,
    enableCookieEncryption: true,
    enableNodeOptionsEnvironmentVariable: false,
    enableNodeCliInspectArguments: true,
    enableEmbeddedAsarIntegrityValidation: true,
    onlyLoadAppFromAsar: true,
  });

  assert.deepEqual(config.mac.target, [{ target: "dir", arch: ["arm64"] }]);
  assert.equal(config.mac.icon, "assets/icon.icns");
  assert.equal(config.mac.minimumSystemVersion, "14.0");
  assert.equal(config.mac.identity, "-");
  assert.equal(config.mac.hardenedRuntime, false);
  assert.equal(config.mac.helperBundleId, "com.tammy.desktop.helper");
  assert.deepEqual(config.mac.signIgnore, [
    "Contents/Resources/core/darwin-arm64/tammy-core$",
    "Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper$",
  ]);
  assert.deepEqual(resourceNames(config.mac.extraResources), [
    { from: "resources/build", to: "build" },
    { from: "resources/core", to: "core" },
    { from: "resources/sqlcipher", to: "sqlcipher" },
    { from: "resources/sbr", to: "sbr" },
    { from: "resources/sbr-helper", to: "sbr-helper" },
  ]);

  assert.deepEqual(config.win.target, [{ target: "dir", arch: ["x64"] }]);
  assert.deepEqual(resourceNames(config.win.extraResources), [
    { from: "resources/build", to: "build" },
    { from: "resources/core", to: "core" },
    { from: "resources/sqlcipher", to: "sqlcipher" },
  ]);

  assert.deepEqual(config.mas.target, [{ target: "dir", arch: ["arm64"] }]);
  assert.equal(config.mas.identity, null);
  assert.equal(config.mas.helperBundleId, "com.tammy.desktop.helper");
  assert.equal(config.mas.bundleVersion, "${env.TAMMY_MACOS_BUILD_NUMBER}");
  assert.equal(config.mas.minimumSystemVersion, "14.0");
  assert.deepEqual(config.mas.extendInfo, {
    ElectronTeamID: "${env.TAMMY_MACOS_TEAM_ID}",
    TammyPrivacyPolicyURL: "${env.TAMMY_MACOS_PRIVACY_POLICY_URL}",
    TammySupportURL: "${env.TAMMY_MACOS_SUPPORT_URL}",
  });
  assert.deepEqual(resourceNames(config.mas.extraResources), [
    { from: "resources/build", to: "build" },
    { from: "resources/core", to: "core" },
    { from: "resources/sqlcipher", to: "sqlcipher" },
    { from: "resources/sbr", to: "sbr" },
    { from: "resources/sbr-helper", to: "sbr-helper" },
    { from: "release/macos/PrivacyInfo.xcprivacy", to: "PrivacyInfo.xcprivacy" },
  ]);

  assert.equal(config.publish, undefined);
  assert.equal(config.dmg, undefined);
  assert.equal(config.linux, undefined);
});
