import { spawn } from "node:child_process";
import { lstat, mkdir, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { acquireSbrBuildLock, SBR_BUILD_LOCK_ENV } from "./sbr-build-lock.mjs";

export const ELECTRON_BUILDER_OUTPUT_ROOT = ".tmp/electron-builder/ordinary";

function assertContained(parent, candidate) {
  const relative = path.relative(parent, candidate);
  if (
    relative === "" ||
    relative === ".." ||
    relative.startsWith(`..${path.sep}`) ||
    path.isAbsolute(relative)
  ) {
    throw new Error("DESKTOP_PACKAGE_OUTPUT_INVALID");
  }
}

async function assertRealDirectory(directory, code) {
  const stats = await lstat(directory).catch(() => null);
  if (!stats?.isDirectory() || stats.isSymbolicLink()) throw new Error(code);
}

export function ordinaryBuilderOutputRoot(root) {
  if (typeof root !== "string" || !path.isAbsolute(root) || path.normalize(root) !== root) {
    throw new Error("DESKTOP_PACKAGE_INPUT_INVALID");
  }
  const output = path.join(root, ELECTRON_BUILDER_OUTPUT_ROOT);
  assertContained(root, output);
  return output;
}

async function prepareOrdinaryBuilderOutput(root) {
  const tmpRoot = path.join(root, ".tmp");
  await mkdir(tmpRoot, { recursive: true });
  await assertRealDirectory(tmpRoot, "DESKTOP_PACKAGE_OUTPUT_INVALID");
  const output = ordinaryBuilderOutputRoot(root);
  const existing = await lstat(output).catch(() => null);
  if (existing) {
    if (!existing.isDirectory() || existing.isSymbolicLink()) {
      throw new Error("DESKTOP_PACKAGE_OUTPUT_INVALID");
    }
    await rm(output, { force: true, recursive: true });
  }
  await mkdir(output, { recursive: true });
  await assertRealDirectory(output, "DESKTOP_PACKAGE_OUTPUT_INVALID");
  return output;
}

export function createDesktopPackagePlan(root) {
  if (!path.isAbsolute(root) || path.normalize(root) !== root) {
    throw new Error("DESKTOP_PACKAGE_INPUT_INVALID");
  }
  return [
    { args: ["core:build"], command: "pnpm" },
    { args: ["sbr-helper:build"], command: "pnpm" },
    { args: ["build:manifest"], command: "pnpm" },
    { args: ["--dir", "apps/desktop", "package"], command: "pnpm" },
    {
      args: ["--test", path.join(root, "apps/desktop/tests/e2e/package-signature.test.mjs")],
      command: process.execPath,
    },
    {
      args: [
        path.join(root, "apps/desktop/scripts/find-packaged-app.mjs"),
        "--verify",
        "--source-manifest",
        path.join(root, "apps/desktop/resources/build/build-manifest.json"),
      ],
      command: process.execPath,
    },
  ];
}

function run(command, args, options) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { ...options, shell: false, stdio: "inherit" });
    child.once("error", () => reject(new Error("DESKTOP_PACKAGE_COMMAND_FAILED")));
    child.once("close", (code, signal) => {
      if (code === 0 && signal === null) resolve();
      else reject(new Error("DESKTOP_PACKAGE_COMMAND_FAILED"));
    });
  });
}

export async function executeDesktopPackage(root, commandRunner = run) {
  const ownership = await acquireSbrBuildLock(root);
  try {
    const outputRoot = await prepareOrdinaryBuilderOutput(root);
    const environment = {
      ...process.env,
      [SBR_BUILD_LOCK_ENV]: ownership.token,
      TAMMY_ELECTRON_BUILDER_OUTPUT: outputRoot,
    };
    for (const { command, args } of createDesktopPackagePlan(root)) {
      await commandRunner(command, args, { cwd: root, env: environment });
    }
  } finally {
    await ownership.release();
  }
}

async function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  await executeDesktopPackage(root);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error instanceof Error ? error.message : "DESKTOP_PACKAGE_FAILED"}\n`);
    process.exitCode = 1;
  });
}
