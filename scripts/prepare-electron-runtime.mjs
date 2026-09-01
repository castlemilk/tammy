import { spawnSync } from "node:child_process";
import { lstatSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const CODESIGN = "/usr/bin/codesign";

function isFile(candidate) {
  try {
    return lstatSync(candidate).isFile();
  } catch {
    return false;
  }
}

function isDirectory(candidate) {
  try {
    return lstatSync(candidate).isDirectory();
  } catch {
    return false;
  }
}

export function electronBundleForExecutable(
  executable,
  fileCheck = isFile,
  directoryCheck = isDirectory,
) {
  if (
    typeof executable !== "string" ||
    !path.isAbsolute(executable) ||
    path.basename(executable) !== "Electron" ||
    path.basename(path.dirname(executable)) !== "MacOS" ||
    path.basename(path.dirname(path.dirname(executable))) !== "Contents" ||
    !fileCheck(executable)
  ) {
    throw new Error("ELECTRON_RUNTIME_INVALID");
  }
  const bundle = path.dirname(path.dirname(path.dirname(executable)));
  if (path.basename(bundle) !== "Electron.app" || !directoryCheck(bundle)) {
    throw new Error("ELECTRON_RUNTIME_INVALID");
  }
  return bundle;
}

export function prepareElectronRuntime({
  platform,
  executable,
  run = spawnSync,
  fileCheck = isFile,
  directoryCheck = isDirectory,
}) {
  if (platform !== "darwin") return "not-required";
  const bundle = electronBundleForExecutable(executable, fileCheck, directoryCheck);
  const execute = (args) =>
    run(CODESIGN, args, {
      encoding: "utf8",
      shell: false,
      stdio: "ignore",
    });
  if (execute(["--verify", "--deep", "--strict", bundle]).status === 0) return "verified";
  if (execute(["--force", "--deep", "--sign", "-", bundle]).status !== 0) {
    throw new Error("ELECTRON_RUNTIME_SIGNING_FAILED");
  }
  if (execute(["--verify", "--deep", "--strict", bundle]).status !== 0) {
    throw new Error("ELECTRON_RUNTIME_SIGNING_FAILED");
  }
  return "repaired";
}

function main() {
  const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const desktopRequire = createRequire(
    path.join(repositoryRoot, "apps", "desktop", "package.json"),
  );
  const executable = desktopRequire("electron");
  const result = prepareElectronRuntime({ executable, platform: process.platform });
  process.stdout.write(`electron runtime ${result}\n`);
}

if (import.meta.main) {
  try {
    main();
  } catch (error) {
    process.stderr.write(
      `${error instanceof Error ? error.message : "ELECTRON_RUNTIME_SIGNING_FAILED"}\n`,
    );
    process.exitCode = 1;
  }
}
