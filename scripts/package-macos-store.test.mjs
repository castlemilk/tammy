import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash, createPrivateKey, sign } from "node:crypto";
import {
  chmod,
  cp,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  symlink,
  unlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import plist from "plist";

import * as packageMacOSStore from "./package-macos-store.mjs";
import { SBR_BUILD_LOCK_ENV } from "./sbr-build-lock.mjs";
import { canonicalizeSbrProfile } from "./sbr-profile-schema.mjs";

const execFileAsync = promisify(execFile);
const {
  createMacOSStoreBuildPlan,
  normalizeCodeSignedExecutable,
  prepareMacOSEntitlements,
  prepareSignedMacOSCopy,
  publishUnsignedMacOSStaging,
  validateSignedModeBoundResources,
  verifySignedCopyEquivalence,
} = packageMacOSStore;

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const standardFrameworkExecutables = [
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Electron Framework",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Helpers/chrome_crashpad_handler",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libEGL.dylib",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libGLESv2.dylib",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libffmpeg.dylib",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libvk_swiftshader.dylib",
  "Contents/Frameworks/Mantle.framework/Versions/A/Mantle",
  "Contents/Frameworks/ReactiveObjC.framework/Versions/A/ReactiveObjC",
  "Contents/Frameworks/Squirrel.framework/Versions/A/Resources/ShipIt",
  "Contents/Frameworks/Squirrel.framework/Versions/A/Squirrel",
];

async function executeMacOSStoreBuild(plan, options = {}) {
  const normalizedPlan = plan.stageCommands
    ? plan
    : {
        ...plan,
        finalCommands: [],
        forgeOutputRoot: path.join(plan.root, ".tmp/macos-release/test/build-1/.forge-unsigned"),
        releaseRoot: path.join(plan.root, ".tmp/macos-release/test/build-1"),
        resourceSignCommands: [],
        signingApp: plan.app,
        stageCommands: plan.commands,
      };
  return packageMacOSStore.executeMacOSStoreBuild(normalizedPlan, {
    appSigner: async () => {},
    entitlementPreparer: async () => ({ cleanup: async () => {}, files: {} }),
    equivalenceVerifier: async () => {},
    signedCopyPreparer: async () => ({
      app: normalizedPlan.signingApp,
      cleanup: async () => {},
      publish: async () => {},
    }),
    unsignedInputVerifier: async () => {},
    unsignedStager: async () => ({}),
    ...options,
  });
}

function environment(mode = "distribution") {
  const privacyPolicy = "https://tammy-accounting.castlemilk.chatgpt.site/privacy";
  const support = "https://tammy-accounting.castlemilk.chatgpt.site/support";
  return {
    TAMMY_MACOS_BUILD_NUMBER: "42",
    TAMMY_MACOS_EXPORT_COMPLIANCE: "exempt",
    ...(mode === "distribution"
      ? {
          TAMMY_MACOS_INSTALLER_IDENTITY: "Mac Installer Distribution: Tammy Pty Ltd (ABCDE12345)",
        }
      : {}),
    TAMMY_MACOS_PROVISIONING_PROFILE: "/private/tmp/tammy.provisionprofile",
    TAMMY_MACOS_PRIVACY_POLICY_URL: privacyPolicy,
    TAMMY_MACOS_SIGNING_IDENTITY: `${
      mode === "distribution" ? "Apple Distribution" : "Apple Development"
    }: Tammy Pty Ltd (ABCDE12345)`,
    TAMMY_MACOS_SIGNING_MODE: mode,
    TAMMY_MACOS_SUPPORT_URL: support,
    TAMMY_MACOS_TARGET: "mas/arm64",
    TAMMY_MACOS_TEAM_ID: "ABCDE12345",
  };
}

function fixturePlist({ bundleIdentifier, executable, helper = false }) {
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>${bundleIdentifier}</string>
<key>CFBundleExecutable</key><string>${executable}</string>
<key>CFBundleShortVersionString</key><string>0.1.0</string>
<key>CFBundleVersion</key><string>1</string>
${
  helper
    ? ""
    : `<key>CFBundleIconFile</key><string>icon.icns</string>
<key>TammyPrivacyPolicyURL</key><string>https://tammy-accounting.castlemilk.chatgpt.site/privacy</string>
<key>TammySupportURL</key><string>https://tammy-accounting.castlemilk.chatgpt.site/support</string>`
}
</dict></plist>\n`;
}

async function createUnsignedStagingRepository(context) {
  const fixtureRoot = await mkdtemp(path.join(tmpdir(), "tammy-macos-unsigned-stage-"));
  context.after(() => rm(fixtureRoot, { force: true, recursive: true }));
  const resources = path.join(fixtureRoot, "apps/desktop/resources");
  await Promise.all([
    mkdir(path.join(fixtureRoot, "apps/desktop/release/macos"), { recursive: true }),
    mkdir(path.join(fixtureRoot, "config/sbr/simulator"), { recursive: true }),
    mkdir(path.join(resources, "build"), { recursive: true }),
    mkdir(path.join(resources, "core/darwin-arm64"), { recursive: true }),
    mkdir(path.join(resources, "sbr/simulator"), { recursive: true }),
    mkdir(path.join(resources, "sbr-helper/darwin-arm64"), { recursive: true }),
    mkdir(path.join(resources, "sqlcipher/darwin-arm64/include"), { recursive: true }),
    mkdir(path.join(resources, "sqlcipher/darwin-arm64/lib"), { recursive: true }),
    mkdir(path.join(fixtureRoot, "test/fixtures/sbr"), { recursive: true }),
  ]);
  await Promise.all([
    writeFile(path.join(fixtureRoot, ".gitignore"), ".tmp/\n"),
    writeFile(
      path.join(fixtureRoot, "apps/desktop/package.json"),
      `${JSON.stringify({ name: "@tammy/desktop", version: "0.1.0" }, null, 2)}\n`,
    ),
    writeFile(
      path.join(fixtureRoot, "apps/desktop/release/macos/build-numbers.json"),
      `${JSON.stringify(
        {
          entries: [
            {
              buildNumber: "1",
              marketingVersion: "0.1.0",
              reservedAt: "2026-08-30T00:00:00.000Z",
              reservedBy: "Tammy Release Test",
              state: "reserved",
            },
          ],
          schemaVersion: 1,
        },
        null,
        2,
      )}\n`,
    ),
    writeFile(path.join(resources, "build/build-manifest.json"), '{"phase":"unsigned"}\n'),
    writeFile(path.join(resources, "core/darwin-arm64/tammy-core"), "unsigned core\n"),
    writeFile(path.join(resources, "sbr/simulator/sbr-profile-v1.json"), '{"phase":"unsigned"}\n'),
    writeFile(path.join(resources, "sbr/simulator/sbr-profile-v1.sig"), "unsigned signature\n"),
    writeFile(
      path.join(resources, "sbr-helper/darwin-arm64/tammy-sbr-helper"),
      "unsigned helper\n",
    ),
    writeFile(path.join(resources, "sqlcipher/LICENSE"), "license\n"),
    writeFile(path.join(resources, "sqlcipher/VERSION"), "4.15.0\n"),
    writeFile(path.join(resources, "sqlcipher/darwin-arm64/HEADER_SHA256"), "hash\n"),
    writeFile(path.join(resources, "sqlcipher/darwin-arm64/LIBRARY_SHA256"), "hash\n"),
    writeFile(path.join(resources, "sqlcipher/darwin-arm64/include/sqlite3.h"), "header\n"),
    writeFile(path.join(resources, "sqlcipher/darwin-arm64/lib/libsqlite3.a"), "library\n"),
    writeFile(
      path.join(fixtureRoot, "config/sbr/simulator/profile-public-key.pem"),
      await readFile(path.join(root, "config/sbr/simulator/profile-public-key.pem")),
    ),
    writeFile(
      path.join(fixtureRoot, "test/fixtures/sbr/simulator-profile-private-key.pem"),
      await readFile(path.join(root, "test/fixtures/sbr/simulator-profile-private-key.pem")),
    ),
  ]);
  await execFileAsync("git", ["init", "-q"], { cwd: fixtureRoot });
  await execFileAsync("git", ["add", "."], { cwd: fixtureRoot });
  await execFileAsync(
    "git",
    [
      "-c",
      "user.name=Tammy Tests",
      "-c",
      "user.email=tammy-tests@example.invalid",
      "commit",
      "-qm",
      "fixture",
    ],
    { cwd: fixtureRoot },
  );
  const plan = createMacOSStoreBuildPlan(fixtureRoot, {
    ...environment("development"),
    TAMMY_MACOS_BUILD_NUMBER: "1",
  });
  const forgeResources = path.join(plan.forgeApp, "Contents/Resources");
  await mkdir(path.join(plan.forgeApp, "Contents/MacOS"), { recursive: true });
  await cp(resources, forgeResources, { recursive: true });
  for (const [bundle, executable, bundleIdentifier] of [
    ["Tammy Helper.app", "Tammy Helper", "com.tammy.desktop.helper"],
    ["Tammy Helper (GPU).app", "Tammy Helper (GPU)", "com.tammy.desktop.helper.GPU"],
    ["Tammy Helper (Plugin).app", "Tammy Helper (Plugin)", "com.tammy.desktop.helper.Plugin"],
    ["Tammy Helper (Renderer).app", "Tammy Helper (Renderer)", "com.tammy.desktop.helper.Renderer"],
  ]) {
    const contents = path.join(plan.forgeApp, "Contents/Frameworks", bundle, "Contents");
    await mkdir(path.join(contents, "MacOS"), { recursive: true });
    await writeFile(
      path.join(contents, "Info.plist"),
      fixturePlist({ bundleIdentifier, executable, helper: true }),
    );
    await writeFile(path.join(contents, "MacOS", executable), `${executable}\n`);
    await chmod(path.join(contents, "MacOS", executable), 0o755);
  }
  for (const relativePath of standardFrameworkExecutables) {
    const file = path.join(plan.forgeApp, ...relativePath.split("/"));
    await mkdir(path.dirname(file), { recursive: true });
    await writeFile(file, `${path.basename(file)}\n`);
    await chmod(file, 0o755);
  }
  await Promise.all([
    writeFile(
      path.join(plan.forgeApp, "Contents/Info.plist"),
      fixturePlist({ bundleIdentifier: "com.tammy.desktop", executable: "Tammy" }),
    ),
    writeFile(path.join(plan.forgeApp, "Contents/MacOS/Tammy"), "unsigned app\n"),
    writeFile(path.join(forgeResources, "app.asar"), "unsigned asar\n"),
    writeFile(path.join(forgeResources, "PrivacyInfo.xcprivacy"), "privacy\n"),
    writeFile(path.join(forgeResources, "icon.icns"), "icon\n"),
  ]);
  await Promise.all([
    chmod(path.join(plan.forgeApp, "Contents/MacOS/Tammy"), 0o755),
    chmod(path.join(forgeResources, "core/darwin-arm64/tammy-core"), 0o755),
    chmod(path.join(forgeResources, "sbr-helper/darwin-arm64/tammy-sbr-helper"), 0o755),
  ]);
  return { plan, resources };
}

test("exposes an injectable local package execution seam", () => {
  assert.equal(typeof packageMacOSStore.executeMacOSStoreBuild, "function");
});

test("distribution plan checks, builds, packages MAS and creates a signed flat package", () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const commands = plan.commands.map(({ command, args }) => [command, ...args]);
  assert.equal(
    plan.app,
    path.join(root, ".tmp/macos-release/0.1.0/build-42/distribution/Tammy.app"),
  );
  assert.match(plan.pkg ?? "", /Tammy-0\.1\.0-build\.42\.pkg$/);
  assert.deepEqual(
    plan.stageCommands.map(({ command, args }) => [command, ...args]),
    [
      [process.execPath, path.join(root, "scripts/check-macos-store.mjs"), "--release"],
      [process.execPath, path.join(root, "scripts/build-sbr-helper.mjs"), "--mas-raw"],
      [process.execPath, path.join(root, "scripts/build-sbr-helper.mjs"), "--mas-profile-unsigned"],
      ["pnpm", "core:build"],
      ["pnpm", "build:manifest"],
      ["pnpm", "--dir", "apps/desktop", "package", "--platform=mas", "--arch=arm64"],
    ],
  );
  assert.deepEqual(commands, [
    [process.execPath, path.join(root, "scripts/check-macos-store.mjs"), "--release"],
    [process.execPath, path.join(root, "scripts/build-sbr-helper.mjs"), "--mas-raw"],
    [process.execPath, path.join(root, "scripts/build-sbr-helper.mjs"), "--mas-profile-unsigned"],
    ["pnpm", "core:build"],
    ["pnpm", "build:manifest"],
    ["pnpm", "--dir", "apps/desktop", "package", "--platform=mas", "--arch=arm64"],
    [
      "/usr/bin/codesign",
      "--force",
      "--sign",
      environment().TAMMY_MACOS_SIGNING_IDENTITY,
      "--entitlements",
      path.join(root, "apps/desktop/release/macos/entitlements.mas.sbr-helper.plist"),
      "--identifier",
      "com.tammy.desktop.sbr-helper",
      "--timestamp",
      path.join(root, "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper"),
    ],
    [
      "/usr/bin/codesign",
      "--verify",
      "--strict",
      "-R",
      '=identifier "com.tammy.desktop.sbr-helper" and anchor apple generic and certificate leaf[subject.OU] = "ABCDE12345"',
      path.join(root, "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper"),
    ],
    [process.execPath, path.join(root, "scripts/build-sbr-helper.mjs"), "--mas-profile"],
    [
      "/usr/bin/codesign",
      "--force",
      "--sign",
      environment().TAMMY_MACOS_SIGNING_IDENTITY,
      "--entitlements",
      path.join(root, "apps/desktop/release/macos/entitlements.mas.core.plist"),
      "--identifier",
      "com.tammy.desktop.core",
      "--timestamp=none",
      path.join(root, "apps/desktop/resources/core/darwin-arm64/tammy-core"),
    ],
    [
      "/usr/bin/codesign",
      "--verify",
      "--strict",
      "-R",
      '=identifier "com.tammy.desktop.core" and anchor apple generic and certificate leaf[subject.OU] = "ABCDE12345"',
      path.join(root, "apps/desktop/resources/core/darwin-arm64/tammy-core"),
    ],
    ["pnpm", "build:manifest"],
    [
      "/usr/bin/codesign",
      "--verify",
      "--deep",
      "--strict",
      "-R",
      '=identifier "com.tammy.desktop" and anchor apple generic and certificate leaf[subject.OU] = "ABCDE12345"',
      plan.signingApp,
    ],
    [
      "/usr/bin/productbuild",
      "--component",
      plan.signingApp,
      "/Applications",
      "--sign",
      environment().TAMMY_MACOS_INSTALLER_IDENTITY,
      plan.signingPkg,
    ],
  ]);
  assert.equal(plan.environment.TAMMY_RELEASE_PROFILE, "mas");
  assert.equal(plan.environment.TAMMY_MACOS_TARGET, "mas/arm64");
  assert.equal(plan.unsignedEnvironment.TAMMY_MACOS_ARTIFACT_PHASE, "unsigned-staging");
  for (const key of [
    "TAMMY_MACOS_INSTALLER_IDENTITY",
    "TAMMY_MACOS_PROVISIONING_PROFILE",
    "TAMMY_MACOS_SIGNING_IDENTITY",
    "TAMMY_MACOS_SIGNING_MODE",
  ]) {
    assert.equal(Object.hasOwn(plan.unsignedEnvironment, key), false);
  }
  assert.equal(
    plan.environment.VITE_TAMMY_PRIVACY_POLICY_URL,
    environment().TAMMY_MACOS_PRIVACY_POLICY_URL,
  );
  assert.equal(plan.environment.VITE_TAMMY_SUPPORT_URL, environment().TAMMY_MACOS_SUPPORT_URL);
  const helperPath = path.join(
    root,
    "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
  );
  const helperSignIndex = commands.findIndex(
    ([command, ...args]) => command === "/usr/bin/codesign" && args.at(-1) === helperPath,
  );
  const profileIndex = commands.findIndex((command) => command.at(-1) === "--mas-profile");
  const manifestIndex = commands.findLastIndex(
    ([command, ...args]) => command === "pnpm" && args.join(" ") === "build:manifest",
  );
  const packageIndex = commands.findIndex(
    ([command, ...args]) => command === "pnpm" && args.includes("--platform=mas"),
  );
  assert.ok(packageIndex > 0 && packageIndex < helperSignIndex);
  assert.ok(helperSignIndex < profileIndex && profileIndex < manifestIndex);
  assert.equal(
    commands
      .slice(profileIndex + 1)
      .filter(
        ([command, ...args]) =>
          command === "/usr/bin/codesign" && args.includes("--sign") && args.at(-1) === helperPath,
      ).length,
    0,
    "the final helper bytes must never be signed after profile generation",
  );
});

test("development signing stops at a locally runnable MAS app", () => {
  const plan = createMacOSStoreBuildPlan(root, environment("development"));
  assert.equal(plan.pkg, undefined);
  assert.equal(plan.commands.length, 13);
});

test("materializes exact Team-bound MAS entitlements without unresolved placeholders", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment("development"));
  const material = await prepareMacOSEntitlements(plan);
  const mainPath = material.files["entitlements.mas.plist"];
  try {
    const mainBytes = await readFile(mainPath, "utf8");
    const helperBytes = await readFile(material.files["entitlements.mas.sbr-helper.plist"], "utf8");
    assert.equal(mainBytes.includes("$("), false);
    assert.equal(helperBytes.includes("$("), false);
    const main = plist.parse(mainBytes);
    const helper = plist.parse(helperBytes);
    assert.equal(main["com.apple.application-identifier"], "ABCDE12345.com.tammy.desktop");
    assert.equal(main["com.apple.developer.team-identifier"], "ABCDE12345");
    assert.deepEqual(main["com.apple.security.application-groups"], [
      "ABCDE12345.com.tammy.desktop",
    ]);
    assert.deepEqual(helper["keychain-access-groups"], ["ABCDE12345.com.tammy.desktop.sbr"]);
  } finally {
    await material.cleanup();
  }
  await assert.rejects(readFile(mainPath), /ENOENT/u);
});

test("release output rejects a symlinked ignored ancestor before locking or cleanup", async (context) => {
  const fixtureRoot = await mkdtemp(path.join(tmpdir(), "tammy-macos-output-root-"));
  const external = await mkdtemp(path.join(tmpdir(), "tammy-macos-output-external-"));
  context.after(async () => {
    await rm(fixtureRoot, { force: true, recursive: true });
    await rm(external, { force: true, recursive: true });
  });
  await writeFile(path.join(external, "preserve.txt"), "external data\n");
  await symlink(external, path.join(fixtureRoot, ".tmp"));
  let commandRan = false;
  await assert.rejects(
    packageMacOSStore.executeMacOSStoreBuild(
      {
        environment: {},
        releaseRoot: path.join(fixtureRoot, ".tmp/macos-release/0.1.0/build-1"),
        root: fixtureRoot,
      },
      {
        commandRunner: async () => {
          commandRan = true;
        },
      },
    ),
    /MACOS_RELEASE_OUTPUT_INVALID/,
  );
  assert.equal(commandRan, false);
  assert.equal(await readFile(path.join(external, "preserve.txt"), "utf8"), "external data\n");
});

test("release output rejects a direct Forge output symlink before Forge can overwrite it", async (context) => {
  const fixtureRoot = await mkdtemp(path.join(tmpdir(), "tammy-macos-forge-root-"));
  const external = await mkdtemp(path.join(tmpdir(), "tammy-macos-forge-external-"));
  context.after(async () => {
    await rm(fixtureRoot, { force: true, recursive: true });
    await rm(external, { force: true, recursive: true });
  });
  const releaseRoot = path.join(fixtureRoot, ".tmp/macos-release/0.1.0/build-1");
  const forgeOutputRoot = path.join(releaseRoot, ".forge-unsigned");
  await mkdir(releaseRoot, { recursive: true });
  await writeFile(path.join(external, "preserve.txt"), "external Forge data\n");
  await symlink(external, forgeOutputRoot);
  let commandRan = false;
  await assert.rejects(
    packageMacOSStore.executeMacOSStoreBuild(
      { environment: {}, forgeOutputRoot, releaseRoot, root: fixtureRoot },
      {
        commandRunner: async () => {
          commandRan = true;
        },
      },
    ),
    /MACOS_RELEASE_OUTPUT_INVALID/,
  );
  assert.equal(commandRan, false);
  assert.equal(
    await readFile(path.join(external, "preserve.txt"), "utf8"),
    "external Forge data\n",
  );
});

test("code-signature normalization preserves the Mach-O payload and detects signed drift", {
  skip: process.platform !== "darwin",
}, async (context) => {
  const temporary = await mkdtemp(path.join(tmpdir(), "tammy-codesign-normalization-"));
  context.after(() => rm(temporary, { force: true, recursive: true }));
  const candidate = path.join(temporary, "candidate");
  const tampered = path.join(temporary, "tampered");
  await Promise.all([cp("/usr/bin/true", candidate), cp("/usr/bin/false", tampered)]);
  await execFileAsync("/usr/bin/codesign", [
    "--force",
    "--sign",
    "-",
    "--identifier",
    "com.tammy.normalization-test",
    "--timestamp=none",
    candidate,
  ]);
  await execFileAsync("/usr/bin/codesign", [
    "--force",
    "--sign",
    "-",
    "--identifier",
    "com.tammy.normalization-test",
    "--timestamp=none",
    tampered,
  ]);
  const sourceHash = await normalizeCodeSignedExecutable("/usr/bin/true");
  assert.equal(await normalizeCodeSignedExecutable(candidate), sourceHash);
  assert.notEqual(await normalizeCodeSignedExecutable(tampered), sourceHash);
});

test("publishes one authenticated unsigned payload and transactionally derives a signed copy", async (context) => {
  const { plan, resources } = await createUnsignedStagingRepository(context);
  const first = await publishUnsignedMacOSStaging(plan);
  assert.equal(first.marketingVersion, "0.1.0");
  assert.equal(first.buildNumber, "1");
  assert.match(first.productSourceCommit, /^[0-9a-f]{40}$/u);
  assert.deepEqual(JSON.parse(await readFile(plan.unsignedManifest, "utf8")), first);

  await mkdir(path.dirname(plan.forgeApp), { recursive: true });
  await cp(plan.unsignedApp, plan.forgeApp, { recursive: true });
  assert.deepEqual(await publishUnsignedMacOSStaging(plan), first);

  await writeFile(path.join(plan.root, "release-note.txt"), "new committed source\n");
  await execFileAsync("git", ["add", "release-note.txt"], { cwd: plan.root });
  await execFileAsync(
    "git",
    [
      "-c",
      "user.name=Tammy Tests",
      "-c",
      "user.email=tammy-tests@example.invalid",
      "commit",
      "-qm",
      "new source",
    ],
    { cwd: plan.root },
  );
  await mkdir(path.dirname(plan.forgeApp), { recursive: true });
  await cp(plan.unsignedApp, plan.forgeApp, { recursive: true });
  await assert.rejects(publishUnsignedMacOSStaging(plan), /MACOS_UNSIGNED_STAGING_CONFLICT/);

  await mkdir(path.dirname(plan.forgeApp), { recursive: true });
  await cp(plan.unsignedApp, plan.forgeApp, { recursive: true });
  await writeFile(path.join(plan.forgeApp, "Contents/Resources/app.asar"), "payload drift\n");
  await assert.rejects(publishUnsignedMacOSStaging(plan), /MACOS_UNSIGNED_STAGING_CONFLICT/);

  await Promise.all([
    writeFile(path.join(resources, "build/build-manifest.json"), '{"phase":"development"}\n'),
    writeFile(path.join(resources, "core/darwin-arm64/tammy-core"), "signed core\n"),
    writeFile(path.join(resources, "sbr-helper/darwin-arm64/tammy-sbr-helper"), "signed helper\n"),
  ]);
  const signedCopy = await prepareSignedMacOSCopy(plan, first);
  assert.equal(
    await readFile(
      path.join(signedCopy.app, "Contents/Resources/core/darwin-arm64/tammy-core"),
      "utf8",
    ),
    "signed core\n",
  );
  assert.equal(
    await readFile(path.join(signedCopy.app, "Contents/Resources/app.asar"), "utf8"),
    "unsigned asar\n",
  );
  await signedCopy.publish();
  assert.equal(
    await readFile(path.join(plan.app, "Contents/MacOS/Tammy"), "utf8"),
    "unsigned app\n",
  );
  await assert.rejects(prepareSignedMacOSCopy(plan, first), /MACOS_SIGNED_COPY_EXISTS/);
});

test("mode-bound build and SBR manifests authenticate the exact packaged signed resources", async (context) => {
  const { plan, resources } = await createUnsignedStagingRepository(context);
  const manifest = await publishUnsignedMacOSStaging(plan);
  const corePath = path.join(resources, "core/darwin-arm64/tammy-core");
  const helperPath = path.join(resources, "sbr-helper/darwin-arm64/tammy-sbr-helper");
  const profilePath = path.join(resources, "sbr/simulator/sbr-profile-v1.json");
  const signaturePath = path.join(resources, "sbr/simulator/sbr-profile-v1.sig");
  const sqlcipherPath = path.join(resources, "sqlcipher/darwin-arm64/lib/libsqlite3.a");
  const hash = (value) => createHash("sha256").update(value).digest("hex");
  const [coreBytes, helperBytes, sqlcipherBytes] = await Promise.all([
    readFile(corePath),
    readFile(helperPath),
    readFile(sqlcipherPath),
  ]);
  const profile = {
    component_manifest_sha256: "NONE",
    endpoint_profile_sha256: "NONE",
    environment: "SIMULATOR",
    expires_at: "2030-01-01T00:00:00Z",
    helper_sha256: hash(helperBytes),
    issued_at: "2026-08-01T00:00:00Z",
    registration_manifest_sha256: "NONE",
    schema_version: 1,
    target: "darwin/arm64",
  };
  const profileBytes = Buffer.from(`${JSON.stringify(profile, null, 2)}\n`);
  const privateKey = createPrivateKey(
    await readFile(path.join(plan.root, "test/fixtures/sbr/simulator-profile-private-key.pem")),
  );
  const signatureBytes = Buffer.from(
    `${sign(
      null,
      canonicalizeSbrProfile(profile, { now: new Date("2026-08-01T00:00:00Z") }),
      privateKey,
    ).toString("base64")}\n`,
  );
  await Promise.all([
    writeFile(profilePath, profileBytes),
    writeFile(signaturePath, signatureBytes),
  ]);
  const build = JSON.parse(
    await readFile(path.join(root, "apps/desktop/resources/build/build-manifest.json"), "utf8"),
  );
  build.source_revision = manifest.productSourceCommit;
  build.core_sha256 = hash(coreBytes);
  build.sqlcipher.library_sha256 = hash(sqlcipherBytes);
  build.sbr.helper_sha256 = hash(helperBytes);
  build.sbr.profile_sha256 = hash(profileBytes);
  build.sbr.profile_signature_sha256 = hash(signatureBytes);
  await writeFile(
    path.join(resources, "build/build-manifest.json"),
    `${JSON.stringify(build, null, 2)}\n`,
  );

  const signedCopy = await prepareSignedMacOSCopy(plan, manifest);
  await assert.doesNotReject(
    validateSignedModeBoundResources({ app: signedCopy.app, plan, unsignedManifest: manifest }),
  );
  await writeFile(
    path.join(signedCopy.app, "Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper"),
    "dishonest helper replacement\n",
  );
  await assert.rejects(
    validateSignedModeBoundResources({ app: signedCopy.app, plan, unsignedManifest: manifest }),
    /MACOS_SIGNED_COPY_MISMATCH/,
  );
  await signedCopy.cleanup();
});

test("distribution promotion requires a verified development-signed counterpart", async (context) => {
  const { plan: developmentPlan } = await createUnsignedStagingRepository(context);
  const manifest = await publishUnsignedMacOSStaging(developmentPlan);
  const distributionPlan = createMacOSStoreBuildPlan(developmentPlan.root, {
    ...environment("distribution"),
    TAMMY_MACOS_BUILD_NUMBER: "1",
  });
  await assert.rejects(
    verifySignedCopyEquivalence(distributionPlan, manifest, distributionPlan.signingApp),
    /MACOS_SIGNED_COPY_COUNTERPART_MISSING/,
  );
});

test("development single-copy verification rejects unsigned resource drift", async (context) => {
  const { plan } = await createUnsignedStagingRepository(context);
  const manifest = await publishUnsignedMacOSStaging(plan);
  const signedCopy = await prepareSignedMacOSCopy(plan, manifest);
  await writeFile(
    path.join(signedCopy.app, "Contents/Resources/app.asar"),
    "changed after unsigned authentication\n",
  );
  await assert.rejects(
    verifySignedCopyEquivalence(plan, manifest, signedCopy.app),
    /MACOS_SIGNED_COPY_MISMATCH/,
  );
  await signedCopy.cleanup();
});

test("execution authenticates one unsigned stage before signing and publishing its copy", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment("development"));
  const events = [];
  await executeMacOSStoreBuild(plan, {
    appSigner: async () => events.push("app-sign"),
    commandRunner: async ({ command, environment }, options) => {
      events.push(command === "pnpm" && environment === "unsigned" ? "forge-unsigned" : command);
      if (environment === "unsigned") {
        assert.equal(options.environment.TAMMY_MACOS_SIGNING_IDENTITY, undefined);
      }
      return undefined;
    },
    equivalenceVerifier: async () => events.push("equivalence"),
    signedCopyPreparer: async () => ({
      app: plan.signingApp,
      cleanup: async () => events.push("cleanup"),
      publish: async () => events.push("publish"),
    }),
    unsignedStager: async () => {
      events.push("unsigned-manifest");
      return {};
    },
    write: () => {},
  });
  assert.ok(events.indexOf("forge-unsigned") < events.indexOf("unsigned-manifest"));
  assert.ok(events.indexOf("unsigned-manifest") < events.indexOf("app-sign"));
  assert.ok(events.indexOf("app-sign") < events.indexOf("equivalence"));
  assert.ok(events.indexOf("equivalence") < events.indexOf("publish"));
  assert.equal(events.includes("cleanup"), false);
});

test("equivalence failure removes only the candidate staging copy before promotion", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment("development"));
  const events = [];
  await assert.rejects(
    executeMacOSStoreBuild(plan, {
      commandRunner: async () => undefined,
      equivalenceVerifier: async () => {
        events.push("equivalence");
        throw new Error("mode-independent resource drift");
      },
      signedCopyPreparer: async () => ({
        app: plan.signingApp,
        cleanup: async () => events.push("cleanup"),
        publish: async () => events.push("publish"),
      }),
      write: () => {},
    }),
    /mode-independent resource drift/,
  );
  assert.deepEqual(events, ["equivalence", "cleanup"]);
});

function successfulRunner(events, assessment = { stderr: "accepted\nsource=Mac App Store\n" }) {
  return async (commandSpec) => {
    events.push(commandSpec.command);
    if (commandSpec.command === "/usr/sbin/spctl") {
      return { exitCode: 0, signal: null, ...assessment };
    }
    return undefined;
  };
}

test("development execution stops at the app without package validation or package output", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment("development"));
  const events = [];
  const output = [];

  const result = await executeMacOSStoreBuild(plan, {
    commandRunner: successfulRunner(events),
    packageHasher: async () => {
      throw new Error("package hash should not run");
    },
    write: (line) => output.push(line),
  });

  assert.deepEqual(
    events,
    plan.commands.map((step) => step.command),
  );
  assert.deepEqual(result, { app: plan.app });
  assert.deepEqual(output, [`{"app":"${plan.app}"}\n`]);
});

test("outer MAS app seal failure stops before productbuild", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const commands = [];
  await assert.rejects(
    executeMacOSStoreBuild(plan, {
      commandRunner: async (commandSpec) => {
        commands.push(commandSpec);
        if (commandSpec.command === "/usr/bin/codesign" && commandSpec.args.includes("--deep")) {
          throw new Error("invalid outer seal");
        }
        return undefined;
      },
      write: () => {},
    }),
    /MACOS_STORE_COMMAND_FAILED/,
  );
  assert.equal(
    commands.some(({ command }) => command === "/usr/bin/productbuild"),
    false,
  );
});

test("build lock rejects concurrent owners, cleans failures, and never removes a foreign lock", async () => {
  const temporary = await mkdtemp(path.join(tmpdir(), "tammy-macos-package-lock-"));
  const lockPath = path.join(temporary, ".tmp/sbr-build-owner/owner.lock");
  const plan = {
    app: path.join(temporary, "Tammy.app"),
    commands: [{ args: [], command: "build" }],
    environment: {},
    root: temporary,
  };
  let unblock;
  const blocked = new Promise((resolve) => {
    unblock = resolve;
  });
  let started;
  const entered = new Promise((resolve) => {
    started = resolve;
  });
  try {
    const first = executeMacOSStoreBuild(plan, {
      commandRunner: async () => {
        started();
        await blocked;
      },
      write: () => {},
    });
    await entered;
    await assert.rejects(
      executeMacOSStoreBuild(plan, {
        commandRunner: async () => {},
        write: () => {},
      }),
      /SBR_BUILD_LOCKED/,
    );
    unblock();
    await first;
    await assert.rejects(readFile(lockPath), /ENOENT/);

    await assert.rejects(
      executeMacOSStoreBuild(plan, {
        commandRunner: async () => {
          throw new Error("build failed");
        },
        write: () => {},
      }),
      /MACOS_STORE_COMMAND_FAILED/,
    );
    await assert.rejects(readFile(lockPath), /ENOENT/);

    await executeMacOSStoreBuild(plan, {
      commandRunner: async () => {
        await unlink(lockPath);
        await writeFile(lockPath, "foreign-owner\n", { mode: 0o600 });
      },
      write: () => {},
    });
    assert.equal(await readFile(lockPath, "utf8"), "foreign-owner\n");
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("distribution execution validates its package in the required order and emits stable JSON", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const events = [];
  const output = [];
  const validationSteps = [];
  const hash = "a".repeat(64);

  const result = await executeMacOSStoreBuild(plan, {
    commandRunner: async (commandSpec, options) => {
      events.push(commandSpec.command);
      if (commandSpec.command.startsWith("/usr/sbin/")) {
        validationSteps.push({ args: commandSpec.args, command: commandSpec.command, options });
      }
      return commandSpec.command === "/usr/sbin/spctl"
        ? { exitCode: 0, signal: null, stderr: "accepted\nsource=Mac App Store\n" }
        : undefined;
    },
    packageHasher: async (pkg) => {
      assert.equal(pkg, plan.signingPkg);
      events.push("sha256");
      return hash;
    },
    write: (line) => {
      events.push("json");
      output.push(line);
    },
  });

  assert.deepEqual(events, [
    ...plan.commands.map((step) => step.command),
    "/usr/sbin/pkgutil",
    "sha256",
    "/usr/sbin/spctl",
    "json",
  ]);
  const inheritedToken = validationSteps[0].options.environment[SBR_BUILD_LOCK_ENV];
  assert.match(inheritedToken, /^[0-9a-f]{64}$/);
  assert.deepEqual(validationSteps, [
    {
      command: "/usr/sbin/pkgutil",
      args: ["--check-signature", plan.signingPkg],
      options: {
        captureOutput: true,
        cwd: root,
        environment: { ...plan.environment, [SBR_BUILD_LOCK_ENV]: inheritedToken },
      },
    },
    {
      command: "/usr/sbin/spctl",
      args: ["--assess", "--type", "install", "--verbose=4", plan.signingPkg],
      options: {
        allowNonZero: true,
        captureOutput: true,
        cwd: root,
        environment: { ...plan.environment, [SBR_BUILD_LOCK_ENV]: inheritedToken },
      },
    },
  ]);
  assert.deepEqual(result, {
    app: plan.app,
    pkg: plan.pkg,
    pkgSha256: hash,
    gatekeeperAssessment: "accepted",
  });
  assert.deepEqual(output, [
    `${JSON.stringify({
      app: plan.app,
      pkg: plan.pkg,
      pkgSha256: hash,
      gatekeeperAssessment: "accepted",
    })}\n`,
  ]);
});

test("distribution execution fails closed when a command runner cannot spawn", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const secret = "DO_NOT_DISCLOSE";

  await assert.rejects(
    () =>
      executeMacOSStoreBuild(plan, {
        commandRunner: async () => {
          throw new Error(secret);
        },
        packageHasher: async () => "a".repeat(64),
        write: () => {},
      }),
    (error) => error.message === "MACOS_STORE_COMMAND_FAILED" && !error.message.includes(secret),
  );
});

test("distribution execution treats an invalid package signature as fatal", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const events = [];

  await assert.rejects(
    () =>
      executeMacOSStoreBuild(plan, {
        commandRunner: async (commandSpec) => {
          if (commandSpec.command === "/usr/sbin/pkgutil") throw new Error("nonzero");
          return commandSpec.command === "/usr/sbin/spctl" ? { stderr: "accepted" } : undefined;
        },
        packageHasher: async () => "a".repeat(64),
        signedCopyPreparer: async () => ({
          app: plan.signingApp,
          cleanup: async () => events.push("cleanup"),
          publish: async () => events.push("publish"),
        }),
        write: () => {},
      }),
    /MACOS_STORE_PACKAGE_SIGNATURE_INVALID/,
  );
  assert.deepEqual(events, ["cleanup"]);
});

test("distribution execution fails closed when Gatekeeper produces no verdict", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());

  await assert.rejects(
    () =>
      executeMacOSStoreBuild(plan, {
        commandRunner: successfulRunner([], {}),
        packageHasher: async () => "a".repeat(64),
        write: () => {},
      }),
    /MACOS_STORE_GATEKEEPER_OUTPUT_MISSING/,
  );
});

test("distribution execution fails closed for an unclassifiable Gatekeeper verdict", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const secret = "DO_NOT_DISCLOSE";

  await assert.rejects(
    () =>
      executeMacOSStoreBuild(plan, {
        commandRunner: successfulRunner([], { stderr: `ambiguous ${secret}` }),
        packageHasher: async () => "a".repeat(64),
        write: () => {},
      }),
    (error) =>
      error.message === "MACOS_STORE_GATEKEEPER_OUTPUT_UNCLASSIFIABLE" &&
      !error.message.includes(secret),
  );
});

test("distribution execution records an ordinary pre-App-Store Gatekeeper rejection without failing", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const output = [];

  const result = await executeMacOSStoreBuild(plan, {
    commandRunner: successfulRunner([], {
      exitCode: 3,
      stderr: "Tammy.pkg: rejected\nsource=Developer ID\n",
    }),
    packageHasher: async () => "b".repeat(64),
    write: (line) => output.push(line),
  });

  assert.equal(result.gatekeeperAssessment, "rejected");
  assert.match(output[0], /"gatekeeperAssessment":"rejected"/);
});

test("distribution execution fails closed for unexpected Gatekeeper status and verdict pairs", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const invalidAssessments = [
    { exitCode: 1, stderr: "Tammy.pkg: rejected\n" },
    { exitCode: 2, stderr: "Tammy.pkg: rejected\n" },
    { exitCode: 4, stderr: "Tammy.pkg: rejected\n" },
    { exitCode: 3, stderr: "Tammy.pkg: accepted\n" },
    { exitCode: 0, stderr: "Tammy.pkg: rejected\n" },
  ];

  for (const assessment of invalidAssessments) {
    await assert.rejects(
      () =>
        executeMacOSStoreBuild(plan, {
          commandRunner: successfulRunner([], assessment),
          packageHasher: async () => "c".repeat(64),
          write: () => {},
        }),
      /MACOS_STORE_GATEKEEPER_ASSESSMENT_INVALID/,
    );
  }
});

test("distribution execution preserves bounded validation-command output failures", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());

  for (const command of ["/usr/sbin/pkgutil", "/usr/sbin/spctl"]) {
    await assert.rejects(
      () =>
        executeMacOSStoreBuild(plan, {
          commandRunner: async (commandSpec) => {
            if (commandSpec.command === command) {
              throw new Error("MACOS_STORE_COMMAND_OUTPUT_INVALID");
            }
            return commandSpec.command === "/usr/sbin/spctl"
              ? { exitCode: 0, signal: null, stderr: "accepted" }
              : undefined;
          },
          packageHasher: async () => "c".repeat(64),
          write: () => {},
        }),
      /MACOS_STORE_COMMAND_OUTPUT_INVALID/,
    );
  }
});

test("distribution execution fails closed when Gatekeeper is terminated after a partial rejection", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());

  await assert.rejects(
    () =>
      executeMacOSStoreBuild(plan, {
        commandRunner: successfulRunner([], {
          signal: "SIGTERM",
          stderr: "Tammy.pkg: rejected\n",
        }),
        packageHasher: async () => "c".repeat(64),
        write: () => {},
      }),
    /MACOS_STORE_GATEKEEPER_ASSESSMENT_INVALID/,
  );
});
