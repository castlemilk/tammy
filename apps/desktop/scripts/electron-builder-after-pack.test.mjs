import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { chmod, lstat, mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";

const require = createRequire(import.meta.url);
const { createAfterPackHook } = require("./electron-builder-after-pack.cjs");
const execFileAsync = promisify(execFile);
const PLIST_HEADER = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>`;
const PLIST_FOOTER = "</dict></plist>\n";

function plist(values) {
  const entries = Object.entries(values)
    .map(([key, value]) => {
      if (typeof value === "boolean") return `<key>${key}</key><${value ? "true" : "false"}/>`;
      return `<key>${key}</key><string>${value}</string>`;
    })
    .join("");
  return `${PLIST_HEADER}${entries}${PLIST_FOOTER}`;
}

async function createAppRoot(prefix = "tammy-builder-hook-") {
  const outputRoot = await mkdtemp(path.join(os.tmpdir(), prefix));
  const appOutDir = path.join(outputRoot, "mac-arm64");
  const appBundle = path.join(appOutDir, "Tammy.app");
  const contents = path.join(appBundle, "Contents");
  const resources = path.join(contents, "Resources");
  const frameworks = path.join(contents, "Frameworks");
  await mkdir(path.join(resources, "core", "darwin-arm64"), { recursive: true });
  await mkdir(path.join(resources, "sbr-helper", "darwin-arm64"), { recursive: true });
  await mkdir(frameworks, { recursive: true });
  await writeFile(
    path.join(contents, "Info.plist"),
    plist({
      CFBundleIdentifier: "com.tammy.desktop",
      NSAppTransportSecurity: "unused",
      NSCameraUsageDescription: "unused",
    }),
  );
  for (const name of [
    "Tammy Helper.app",
    "Tammy Helper (GPU).app",
    "Tammy Helper (Plugin).app",
    "Tammy Helper (Renderer).app",
  ]) {
    const helperContents = path.join(frameworks, name, "Contents");
    await mkdir(helperContents, { recursive: true });
    await writeFile(path.join(helperContents, "Info.plist"), plist({ CFBundleIdentifier: "old" }));
  }
  await writeFile(path.join(resources, "core", "darwin-arm64", "tammy-core"), "core");
  await writeFile(path.join(resources, "sbr-helper", "darwin-arm64", "tammy-sbr-helper"), "helper");
  await chmod(path.join(resources, "core", "darwin-arm64", "tammy-core"), 0o644);
  await chmod(path.join(resources, "sbr-helper", "darwin-arm64", "tammy-sbr-helper"), 0o644);
  return { appBundle, appOutDir, outputRoot };
}

async function readPlist(file) {
  const { stdout } = await execFileAsync("/usr/bin/plutil", ["-convert", "json", "-o", "-", file]);
  return JSON.parse(stdout);
}

test("normalizes an ordinary macOS app without signing or leaking macOS usage keys", async (context) => {
  const { appBundle, appOutDir, outputRoot } = await createAppRoot();
  context.after(() => rm(outputRoot, { force: true, recursive: true }));
  await createAfterPackHook({
    environment: { TAMMY_ELECTRON_BUILDER_OUTPUT: outputRoot },
  })({ appOutDir, arch: "arm64", electronPlatformName: "darwin" });

  const info = await readPlist(path.join(appBundle, "Contents", "Info.plist"));
  assert.equal(info.NSAppTransportSecurity, undefined);
  assert.equal(info.NSCameraUsageDescription, undefined);
  assert.equal(info.CFBundleIdentifier, "com.tammy.desktop");
  for (const [name, identifier] of [
    ["Tammy Helper.app", "com.tammy.desktop.helper"],
    ["Tammy Helper (GPU).app", "com.tammy.desktop.helper.GPU"],
    ["Tammy Helper (Plugin).app", "com.tammy.desktop.helper.Plugin"],
    ["Tammy Helper (Renderer).app", "com.tammy.desktop.helper.Renderer"],
  ]) {
    const helper = await readPlist(
      path.join(appBundle, "Contents", "Frameworks", name, "Contents", "Info.plist"),
    );
    assert.equal(helper.CFBundleIdentifier, identifier);
  }
  for (const file of [
    path.join(appBundle, "Contents", "Resources", "core", "darwin-arm64", "tammy-core"),
    path.join(appBundle, "Contents", "Resources", "sbr-helper", "darwin-arm64", "tammy-sbr-helper"),
  ]) {
    assert.equal((await lstat(file)).mode & 0o777, 0o755);
  }
});

test("writes the exact unsigned MAS release profile and export-compliance boolean", async (context) => {
  const { appBundle, appOutDir, outputRoot } = await createAppRoot("tammy-builder-mas-");
  context.after(() => rm(outputRoot, { force: true, recursive: true }));
  await createAfterPackHook({
    environment: {
      TAMMY_ELECTRON_BUILDER_OUTPUT: outputRoot,
      TAMMY_MACOS_ARTIFACT_PHASE: "unsigned-staging",
      TAMMY_MACOS_BUILD_NUMBER: "42",
      TAMMY_MACOS_EXPORT_COMPLIANCE: "exempt",
      TAMMY_MACOS_PRIVACY_POLICY_URL: "https://example.com/privacy",
      TAMMY_MACOS_SUPPORT_URL: "https://example.com/support",
      TAMMY_MACOS_TARGET: "mas/arm64",
      TAMMY_MACOS_TEAM_ID: "ABCDE12345",
    },
  })({ appOutDir, arch: "arm64", electronPlatformName: "mas" });

  const info = await readPlist(path.join(appBundle, "Contents", "Info.plist"));
  assert.deepEqual(
    {
      CFBundleVersion: info.CFBundleVersion,
      ElectronTeamID: info.ElectronTeamID,
      ITSAppUsesNonExemptEncryption: info.ITSAppUsesNonExemptEncryption,
      LSMinimumSystemVersion: info.LSMinimumSystemVersion,
      NSHumanReadableCopyright: info.NSHumanReadableCopyright,
      TammyPrivacyPolicyURL: info.TammyPrivacyPolicyURL,
      TammySupportURL: info.TammySupportURL,
    },
    {
      CFBundleVersion: "42",
      ElectronTeamID: "ABCDE12345",
      ITSAppUsesNonExemptEncryption: false,
      LSMinimumSystemVersion: "14.0",
      NSHumanReadableCopyright: "© 2026 Gamma Systems Pty Ltd",
      TammyPrivacyPolicyURL: "https://example.com/privacy",
      TammySupportURL: "https://example.com/support",
    },
  );
});

test("does not invoke macOS plist tooling for Windows and rejects unsupported targets", async (context) => {
  const outputRoot = await mkdtemp(path.join(os.tmpdir(), "tammy-builder-win-"));
  context.after(() => rm(outputRoot, { force: true, recursive: true }));
  const appOutDir = path.join(outputRoot, "win-unpacked");
  const core = path.join(appOutDir, "resources", "core", "win32-x64", "tammy-core.exe");
  await mkdir(path.dirname(core), { recursive: true });
  await writeFile(core, "core");
  const commands = [];
  await createAfterPackHook({
    environment: { TAMMY_ELECTRON_BUILDER_OUTPUT: outputRoot },
    runCommand: async (...args) => commands.push(args),
  })({ appOutDir, arch: "x64", electronPlatformName: "win32" });
  assert.deepEqual(commands, []);

  await assert.rejects(
    createAfterPackHook({ environment: { TAMMY_ELECTRON_BUILDER_OUTPUT: outputRoot } })({
      appOutDir,
      arch: "x64",
      electronPlatformName: "darwin",
    }),
    { message: "BUILDER_TARGET_INVALID" },
  );
});

test("rejects every ASCII control character in MAS public URLs", async (context) => {
  const { appOutDir, outputRoot } = await createAppRoot("tammy-builder-url-");
  context.after(() => rm(outputRoot, { force: true, recursive: true }));
  const environment = {
    TAMMY_ELECTRON_BUILDER_OUTPUT: outputRoot,
    TAMMY_MACOS_ARTIFACT_PHASE: "unsigned-staging",
    TAMMY_MACOS_BUILD_NUMBER: "42",
    TAMMY_MACOS_EXPORT_COMPLIANCE: "exempt",
    TAMMY_MACOS_PRIVACY_POLICY_URL: "https://example.com/privacy",
    TAMMY_MACOS_SUPPORT_URL: "https://example.com/support",
    TAMMY_MACOS_TARGET: "mas/arm64",
    TAMMY_MACOS_TEAM_ID: "ABCDE12345",
  };
  for (const key of ["TAMMY_MACOS_PRIVACY_POLICY_URL", "TAMMY_MACOS_SUPPORT_URL"]) {
    for (const code of [...Array.from({ length: 32 }, (_, index) => index), 127]) {
      await assert.rejects(
        createAfterPackHook({
          environment: {
            ...environment,
            [key]: `https://example.com/a${String.fromCharCode(code)}b`,
          },
        })({ appOutDir, arch: "arm64", electronPlatformName: "mas" }),
        { message: "BUILDER_MAS_ENV_INVALID" },
      );
    }
  }
});

test("rejects symlinked output app roots and resources", async (context) => {
  const { appBundle, appOutDir, outputRoot } = await createAppRoot("tammy-builder-symlink-");
  context.after(() => rm(outputRoot, { force: true, recursive: true }));
  const linkRoot = path.join(outputRoot, "link");
  await symlink(appOutDir, linkRoot);
  await assert.rejects(
    createAfterPackHook({
      environment: { TAMMY_ELECTRON_BUILDER_OUTPUT: outputRoot },
    })({ appOutDir: linkRoot, arch: "arm64", electronPlatformName: "darwin" }),
    { message: "BUILDER_APP_OUT_DIR_INVALID" },
  );
  const core = path.join(appBundle, "Contents", "Resources", "core", "darwin-arm64", "tammy-core");
  const replacement = `${core}.real`;
  await symlink(core, replacement);
  await rm(core);
  await symlink(replacement, core);
  await assert.rejects(
    createAfterPackHook({
      environment: { TAMMY_ELECTRON_BUILDER_OUTPUT: outputRoot },
    })({ appOutDir, arch: "arm64", electronPlatformName: "darwin" }),
    { message: "BUILDER_CORE_RESOURCE_INVALID" },
  );
});
