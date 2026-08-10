import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createMacOSStoreBuildPlan } from "./package-macos-store.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function environment(mode = "distribution") {
  return {
    TAMMY_MACOS_BUILD_NUMBER: "42",
    TAMMY_MACOS_EXPORT_COMPLIANCE: "exempt",
    ...(mode === "distribution"
      ? {
          TAMMY_MACOS_INSTALLER_IDENTITY:
            "3rd Party Mac Developer Installer: Tammy Pty Ltd (ABCDE12345)",
        }
      : {}),
    TAMMY_MACOS_PROVISIONING_PROFILE: "/private/tmp/tammy.provisionprofile",
    TAMMY_MACOS_PRIVACY_POLICY_URL: "https://example.com/tammy/privacy",
    TAMMY_MACOS_SIGNING_IDENTITY: `${
      mode === "distribution" ? "Apple Distribution" : "Apple Development"
    }: Tammy Pty Ltd (ABCDE12345)`,
    TAMMY_MACOS_SIGNING_MODE: mode,
    TAMMY_MACOS_SUPPORT_URL: "https://example.com/tammy/support",
    TAMMY_MACOS_TEAM_ID: "ABCDE12345",
  };
}

test("distribution plan checks, builds, packages MAS and creates a signed flat package", () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  assert.equal(plan.app, path.join(root, "apps/desktop/out/Tammy-mas-arm64/Tammy.app"));
  assert.match(plan.pkg ?? "", /Tammy-0\.1\.0-build\.42\.pkg$/);
  assert.deepEqual(
    plan.commands.map(({ command, args }) => [command, ...args]),
    [
      [process.execPath, path.join(root, "scripts/check-macos-store.mjs"), "--release"],
      ["pnpm", "core:build"],
      ["pnpm", "build:manifest"],
      [
        "/usr/bin/codesign",
        "--force",
        "--sign",
        environment().TAMMY_MACOS_SIGNING_IDENTITY,
        "--entitlements",
        path.join(root, "apps/desktop/release/macos/entitlements.mas.core.plist"),
        "--timestamp=none",
        path.join(root, "apps/desktop/resources/core/darwin-arm64/tammy-core"),
      ],
      [
        "/usr/bin/codesign",
        "--verify",
        "--strict",
        path.join(root, "apps/desktop/resources/core/darwin-arm64/tammy-core"),
      ],
      [process.execPath, path.join(root, "scripts/write-build-manifest.mjs"), "--rehash-core"],
      ["pnpm", "--dir", "apps/desktop", "package", "--platform=mas", "--arch=arm64"],
      [
        process.execPath,
        path.join(root, "apps/desktop/scripts/find-packaged-app.mjs"),
        "--verify",
        "--platform",
        "mas",
        "--source-manifest",
        path.join(root, "apps/desktop/resources/build/build-manifest.json"),
      ],
      [
        "/usr/bin/productbuild",
        "--component",
        plan.app,
        "/Applications",
        "--sign",
        environment().TAMMY_MACOS_INSTALLER_IDENTITY,
        plan.pkg,
      ],
    ],
  );
  assert.equal(plan.environment.TAMMY_RELEASE_PROFILE, "mas");
  assert.equal(
    plan.environment.VITE_TAMMY_PRIVACY_POLICY_URL,
    environment().TAMMY_MACOS_PRIVACY_POLICY_URL,
  );
  assert.equal(plan.environment.VITE_TAMMY_SUPPORT_URL, environment().TAMMY_MACOS_SUPPORT_URL);
});

test("development signing stops at a locally runnable MAS app", () => {
  const plan = createMacOSStoreBuildPlan(root, environment("development"));
  assert.equal(plan.pkg, undefined);
  assert.equal(plan.commands.length, 8);
});
