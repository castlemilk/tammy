import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { mkdir, open } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { validateMacOSReleaseEnvironment } from "./check-macos-store.mjs";

const MAX_CAPTURED_OUTPUT_BYTES = 16 * 1024;

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
    root,
    ...(pkg === undefined ? {} : { pkg }),
  });
}

async function run(commandSpec, options) {
  return new Promise((resolve, reject) => {
    const captureOutput = options.captureOutput === true;
    const output = { stderr: "", stdout: "" };
    let capturedOutputBytes = 0;
    let outputOverflowed = false;
    const child = spawn(commandSpec.command, commandSpec.args, {
      cwd: options.cwd,
      env: options.environment,
      shell: false,
      stdio: captureOutput ? ["ignore", "pipe", "pipe"] : "inherit",
    });
    if (captureOutput) {
      const capture = (stream) => (chunk) => {
        if (outputOverflowed) return;
        const chunkBytes = Buffer.byteLength(chunk);
        if (capturedOutputBytes + chunkBytes > MAX_CAPTURED_OUTPUT_BYTES) {
          outputOverflowed = true;
          child.kill();
          return;
        }
        capturedOutputBytes += chunkBytes;
        output[stream] += chunk;
      };
      child.stdout.on("data", capture("stdout"));
      child.stderr.on("data", capture("stderr"));
    }
    child.once("error", () => reject(new Error("MACOS_STORE_COMMAND_FAILED")));
    child.once("close", (code, signal) => {
      if (outputOverflowed) {
        reject(new Error("MACOS_STORE_COMMAND_OUTPUT_INVALID"));
      } else if (signal !== null || !Number.isInteger(code)) {
        reject(new Error("MACOS_STORE_COMMAND_FAILED"));
      } else if (code === 0 || options.allowNonZero === true) {
        resolve({ ...output, exitCode: code, signal });
      } else reject(new Error(`MACOS_STORE_COMMAND_FAILED:${code ?? signal ?? "unknown"}`));
    });
  });
}

async function hashPackage(pkg) {
  const hash = createHash("sha256");
  let file;
  try {
    file = await open(pkg, "r");
    const stream = file.createReadStream();
    for await (const chunk of stream) hash.update(chunk);
    return hash.digest("hex");
  } catch {
    throw new Error("MACOS_STORE_PACKAGE_HASH_INVALID");
  } finally {
    await file?.close().catch(() => {});
  }
}

function classifyGatekeeperAssessment(result) {
  if (!Number.isInteger(result?.exitCode) || result.signal !== null) {
    throw new Error("MACOS_STORE_GATEKEEPER_ASSESSMENT_INVALID");
  }
  const output = [result?.stdout, result?.stderr]
    .filter((value) => typeof value === "string")
    .join("\n");
  if (output.trim().length === 0) throw new Error("MACOS_STORE_GATEKEEPER_OUTPUT_MISSING");
  const accepted = /(?:^|\W)accepted(?:\W|$)/iu.test(output);
  const rejected = /(?:^|\W)rejected(?:\W|$)/iu.test(output);
  if (accepted === rejected) throw new Error("MACOS_STORE_GATEKEEPER_OUTPUT_UNCLASSIFIABLE");
  if (result.exitCode === 0 && accepted) return "accepted";
  if (result.exitCode === 3 && rejected) return "rejected";
  throw new Error("MACOS_STORE_GATEKEEPER_ASSESSMENT_INVALID");
}

async function runCommand(commandRunner, commandSpec, options, failure) {
  try {
    return await commandRunner(commandSpec, options);
  } catch (error) {
    if (error instanceof Error && error.message === "MACOS_STORE_COMMAND_OUTPUT_INVALID") {
      throw error;
    }
    throw new Error(failure);
  }
}

export async function executeMacOSStoreBuild(
  plan,
  {
    commandRunner = run,
    packageHasher = hashPackage,
    write = (line) => process.stdout.write(line),
  } = {},
) {
  const commandOptions = { cwd: plan.root, environment: plan.environment };
  for (const step of plan.commands) {
    await runCommand(commandRunner, step, commandOptions, "MACOS_STORE_COMMAND_FAILED");
  }
  if (plan.pkg === undefined) {
    const result = { app: plan.app };
    write(`${JSON.stringify(result)}\n`);
    return result;
  }

  await runCommand(
    commandRunner,
    command("/usr/sbin/pkgutil", ["--check-signature", plan.pkg]),
    { ...commandOptions, captureOutput: true },
    "MACOS_STORE_PACKAGE_SIGNATURE_INVALID",
  );
  let pkgSha256;
  try {
    pkgSha256 = await packageHasher(plan.pkg);
  } catch {
    throw new Error("MACOS_STORE_PACKAGE_HASH_INVALID");
  }
  if (typeof pkgSha256 !== "string" || !/^[a-f0-9]{64}$/u.test(pkgSha256)) {
    throw new Error("MACOS_STORE_PACKAGE_HASH_INVALID");
  }
  const gatekeeperOutput = await runCommand(
    commandRunner,
    command("/usr/sbin/spctl", ["--assess", "--type", "install", "--verbose=4", plan.pkg]),
    { ...commandOptions, allowNonZero: true, captureOutput: true },
    "MACOS_STORE_GATEKEEPER_ASSESSMENT_INVALID",
  );
  const result = {
    app: plan.app,
    pkg: plan.pkg,
    pkgSha256,
    gatekeeperAssessment: classifyGatekeeperAssessment(gatekeeperOutput),
  };
  write(`${JSON.stringify(result)}\n`);
  return result;
}

async function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const plan = createMacOSStoreBuildPlan(root, process.env);
  if (plan.pkg !== undefined) await mkdir(path.dirname(plan.pkg), { recursive: true });
  await executeMacOSStoreBuild(plan);
}

if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : "MACOS_STORE_BUILD_FAILED"}\n`,
    );
    process.exitCode = 1;
  });
}
