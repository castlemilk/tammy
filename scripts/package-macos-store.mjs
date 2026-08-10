import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { validateMacOSReleaseEnvironment } from "./check-macos-store.mjs";

function command(command, args) {
  return Object.freeze({ args: Object.freeze(args), command });
}

export function createMacOSStoreBuildPlan(root, sourceEnvironment) {
  if (!path.isAbsolute(root)) throw new Error("MACOS_RELEASE_INPUT_INVALID");
  const release = validateMacOSReleaseEnvironment(sourceEnvironment);
  const desktopPackage = JSON.parse(
    readFileSync(path.join(root, "apps", "desktop", "package.json"), "utf8"),
  );
  if (
    typeof desktopPackage.version !== "string" ||
    !/^\d+\.\d+\.\d+$/.test(desktopPackage.version)
  ) {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
  const app = path.join(root, "apps", "desktop", "out", "Tammy-mas-arm64", "Tammy.app");
  const core = path.join(
    root,
    "apps",
    "desktop",
    "resources",
    "core",
    "darwin-arm64",
    "tammy-core",
  );
  const coreEntitlements = path.join(
    root,
    "apps",
    "desktop",
    "release",
    "macos",
    "entitlements.mas.core.plist",
  );
  const sourceManifest = path.join(
    root,
    "apps",
    "desktop",
    "resources",
    "build",
    "build-manifest.json",
  );
  const pkg =
    release.mode === "distribution"
      ? path.join(
          root,
          "apps",
          "desktop",
          "out",
          "make",
          "pkg",
          "arm64",
          `Tammy-${desktopPackage.version}-build.${release.buildNumber}.pkg`,
        )
      : undefined;
  const commands = [
    command(process.execPath, [path.join(root, "scripts", "check-macos-store.mjs"), "--release"]),
    command("pnpm", ["core:build"]),
    command("pnpm", ["build:manifest"]),
    command("/usr/bin/codesign", [
      "--force",
      "--sign",
      sourceEnvironment.TAMMY_MACOS_SIGNING_IDENTITY,
      "--entitlements",
      coreEntitlements,
      "--timestamp=none",
      core,
    ]),
    command("/usr/bin/codesign", ["--verify", "--strict", core]),
    command(process.execPath, [
      path.join(root, "scripts", "write-build-manifest.mjs"),
      "--rehash-core",
    ]),
    command("pnpm", ["--dir", "apps/desktop", "package", "--platform=mas", "--arch=arm64"]),
    command(process.execPath, [
      path.join(root, "apps", "desktop", "scripts", "find-packaged-app.mjs"),
      "--verify",
      "--platform",
      "mas",
      "--source-manifest",
      sourceManifest,
    ]),
  ];
  if (pkg !== undefined) {
    commands.push(
      command("/usr/bin/productbuild", [
        "--component",
        app,
        "/Applications",
        "--sign",
        sourceEnvironment.TAMMY_MACOS_INSTALLER_IDENTITY,
        pkg,
      ]),
    );
  }
  return Object.freeze({
    app,
    commands: Object.freeze(commands),
    environment: Object.freeze({
      ...sourceEnvironment,
      TAMMY_RELEASE_PROFILE: "mas",
      VITE_TAMMY_PRIVACY_POLICY_URL: sourceEnvironment.TAMMY_MACOS_PRIVACY_POLICY_URL,
      VITE_TAMMY_SUPPORT_URL: sourceEnvironment.TAMMY_MACOS_SUPPORT_URL,
    }),
    ...(pkg === undefined ? {} : { pkg }),
  });
}

async function run(commandSpec, options) {
  await new Promise((resolve, reject) => {
    const child = spawn(commandSpec.command, commandSpec.args, {
      cwd: options.cwd,
      env: options.environment,
      shell: false,
      stdio: "inherit",
    });
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      if (code === 0) resolve();
      else reject(new Error(`MACOS_STORE_COMMAND_FAILED:${code ?? signal ?? "unknown"}`));
    });
  });
}

async function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const plan = createMacOSStoreBuildPlan(root, process.env);
  if (plan.pkg !== undefined) await mkdir(path.dirname(plan.pkg), { recursive: true });
  for (const step of plan.commands) await run(step, { cwd: root, environment: plan.environment });
  process.stdout.write(
    `${JSON.stringify({ app: plan.app, ...(plan.pkg ? { pkg: plan.pkg } : {}) })}\n`,
  );
}

if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : "MACOS_STORE_BUILD_FAILED"}\n`,
    );
    process.exitCode = 1;
  });
}
