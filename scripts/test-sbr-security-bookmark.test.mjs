import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

import {
  createSecurityBookmarkTestPlan,
  REQUIRED_SECURITY_BOOKMARK_ENVIRONMENT,
  validateSecurityBookmarkProvisioningProfile,
} from "./test-sbr-security-bookmark.mjs";

const repositoryRoot = path.resolve(import.meta.dirname, "..");

function releaseEnvironment() {
  return {
    TAMMY_MACOS_PROVISIONING_PROFILE: "/private/tmp/tammy.provisionprofile",
    TAMMY_MACOS_SIGNING_IDENTITY: "Apple Development: Tammy Pty Ltd (ABCDE12345)",
    TAMMY_MACOS_TEAM_ID: "ABCDE12345",
    TAMMY_SBR_BOOKMARK_TEST_CREDENTIAL: "/private/tmp/synthetic-machine-credential.p12",
  };
}

test("fails closed for every missing native release input", () => {
  assert.deepEqual(REQUIRED_SECURITY_BOOKMARK_ENVIRONMENT, [
    "TAMMY_MACOS_SIGNING_IDENTITY",
    "TAMMY_MACOS_PROVISIONING_PROFILE",
    "TAMMY_MACOS_TEAM_ID",
    "TAMMY_SBR_BOOKMARK_TEST_CREDENTIAL",
  ]);
  for (const name of REQUIRED_SECURITY_BOOKMARK_ENVIRONMENT) {
    const environment = releaseEnvironment();
    delete environment[name];
    assert.throws(
      () =>
        createSecurityBookmarkTestPlan({
          arch: "arm64",
          environment,
          platform: "darwin",
          repositoryRoot,
          temporaryDirectory: "/private/tmp/tammy-bookmark-test",
        }),
      { message: "SBR_SECURITY_BOOKMARK_RELEASE_INPUTS_REQUIRED" },
    );
  }
});

test("unconfigured CLI exits closed before signing or showing a chooser", () => {
  const result = spawnSync(
    process.execPath,
    [path.join(repositoryRoot, "scripts/test-sbr-security-bookmark.mjs")],
    {
      encoding: "utf8",
      env: {},
    },
  );
  assert.equal(result.status, 1);
  assert.equal(result.stdout, "");
  assert.equal(result.stderr, "SBR_SECURITY_BOOKMARK_RELEASE_INPUTS_REQUIRED\n");
});

test("rejects unsupported hosts, ad-hoc identities, relative inputs, and identity/team mismatch", () => {
  const base = {
    arch: "arm64",
    environment: releaseEnvironment(),
    platform: "darwin",
    repositoryRoot,
    temporaryDirectory: "/private/tmp/tammy-bookmark-test",
  };
  for (const changed of [
    { platform: "linux" },
    { arch: "x64" },
    { environment: { ...releaseEnvironment(), TAMMY_MACOS_SIGNING_IDENTITY: "-" } },
    {
      environment: {
        ...releaseEnvironment(),
        TAMMY_MACOS_SIGNING_IDENTITY: "Apple Development: Tammy Pty Ltd (OTHER12345)",
      },
    },
    {
      environment: {
        ...releaseEnvironment(),
        TAMMY_MACOS_PROVISIONING_PROFILE: "relative.provisionprofile",
      },
    },
  ]) {
    assert.throws(
      () => createSecurityBookmarkTestPlan({ ...base, ...changed }),
      /SBR_SECURITY_BOOKMARK_(?:HOST_UNSUPPORTED|RELEASE_INPUTS_REQUIRED)/,
    );
  }
});

test("build plan signs native host and helper with the configured identity and shared authority", () => {
  const plan = createSecurityBookmarkTestPlan({
    arch: "arm64",
    environment: releaseEnvironment(),
    platform: "darwin",
    repositoryRoot,
    temporaryDirectory: "/private/tmp/tammy-bookmark-test",
  });

  assert.equal(plan.identity, releaseEnvironment().TAMMY_MACOS_SIGNING_IDENTITY);
  assert.equal(plan.teamID, "ABCDE12345");
  assert.equal(plan.appGroup, "ABCDE12345.com.tammy.desktop");
  assert.equal(plan.keychainGroup, "ABCDE12345.com.tammy.desktop.sbr");
  assert.equal(plan.securityScopedBookmark, true);
  assert.equal(plan.userSelectedReadOnly, true);
  assert.match(plan.hostExecutable, /BookmarkHost\.app\/Contents\/MacOS\/BookmarkHost$/);
  assert.match(plan.helperExecutable, /BookmarkHelper\.app\/Contents\/MacOS\/BookmarkHelper$/);
  assert.ok(
    plan.commands.some(
      ({ file, args }) =>
        file === "/usr/bin/xcrun" && args[0] === "swiftc" && args.includes("-framework"),
    ),
  );
  assert.deepEqual(plan.commands.at(-1)?.args, [
    "--helper-path",
    plan.helperExecutable,
    "--expected-path",
    releaseEnvironment().TAMMY_SBR_BOOKMARK_TEST_CREDENTIAL,
  ]);
  assert.equal(
    plan.commands.filter(
      ({ file, args }) => file === "/usr/bin/codesign" && args.includes("--sign"),
    ).length,
    2,
  );
  assert.ok(
    plan.commands
      .filter(({ file, args }) => file === "/usr/bin/codesign" && args.includes("--sign"))
      .every(({ args }) => !args.includes("-") && args.includes(plan.identity)),
  );
});

test("validates shared provisioning authority as exact plist fields, not text fragments", () => {
  const plan = createSecurityBookmarkTestPlan({
    arch: "arm64",
    environment: releaseEnvironment(),
    platform: "darwin",
    repositoryRoot,
    temporaryDirectory: "/private/tmp/tammy-bookmark-test",
  });
  const valid = {
    ApplicationIdentifierPrefix: ["ABCDE12345"],
    Entitlements: {
      "application-identifier": "ABCDE12345.com.tammy.desktop",
      "com.apple.security.application-groups": [plan.appGroup],
      "keychain-access-groups": [plan.keychainGroup],
    },
    TeamIdentifier: [plan.teamID],
  };
  assert.doesNotThrow(() => validateSecurityBookmarkProvisioningProfile(valid, plan));
  for (const profile of [
    { note: JSON.stringify(valid) },
    { ...valid, TeamIdentifier: ["OTHER12345"] },
    {
      ...valid,
      Entitlements: { ...valid.Entitlements, "com.apple.security.application-groups": [] },
    },
    {
      ...valid,
      Entitlements: { ...valid.Entitlements, "keychain-access-groups": ["OTHER12345.fake"] },
    },
  ]) {
    assert.throws(() => validateSecurityBookmarkProvisioningProfile(profile, plan), {
      message: "SBR_SECURITY_BOOKMARK_PROFILE_INVALID",
    });
  }
});

test("Swift fixture keeps file reading in the helper and uses scoped resolution/read/stop", async () => {
  const source = await readFile(
    path.join(repositoryRoot, "test/fixtures/sbr/create-security-bookmark.swift"),
    "utf8",
  );

  assert.match(source, /NSOpenPanel/);
  assert.match(source, /--expected-path/);
  assert.match(source, /bookmarkData\(\s*options: \.withSecurityScope/);
  assert.match(source, /resolvingBookmarkData:[\s\S]*\.withSecurityScope/);
  assert.match(source, /startAccessingSecurityScopedResource\(\)/);
  assert.match(source, /stopAccessingSecurityScopedResource\(\)/);
  assert.match(source, /FileHandle\(forReadingFrom:/);
  assert.match(source, /if CommandLine\.arguments\.contains\("--helper"\)/);
  assert.match(source, /standardInput/);
  assert.match(source, /JSONEncoder/);
  assert.doesNotMatch(source, /helper\.arguments\s*=\s*\[[\s\S]*--bookmark/);
  assert.doesNotMatch(source, /bookmarkData\(\s*options: \[\]/);
  assert.doesNotMatch(source, /addRecentDocument/);
});

test("MAS parent and SBR helper declare the same pinned app-group and Keychain authority", async () => {
  const [parent, helper] = await Promise.all([
    readFile(
      path.join(repositoryRoot, "apps/desktop/release/macos/entitlements.mas.plist"),
      "utf8",
    ),
    readFile(
      path.join(repositoryRoot, "apps/desktop/release/macos/entitlements.mas.sbr-helper.plist"),
      "utf8",
    ),
  ]);
  for (const authority of [
    "$(TeamIdentifierPrefix)com.tammy.desktop",
    "$(AppIdentifierPrefix)com.tammy.desktop.sbr",
  ]) {
    assert.match(parent, new RegExp(authority.replace(/[().$]/g, "\\$&")));
    assert.match(helper, new RegExp(authority.replace(/[().$]/g, "\\$&")));
  }
});
