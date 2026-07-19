import { execFileSync } from "node:child_process";
import { statSync } from "node:fs";
import { posix, win32 } from "node:path";
import { fileURLToPath } from "node:url";

function isJavaScriptEntry(candidate) {
  return /\.[cm]?js$/i.test(candidate);
}

function isShellShim(candidate) {
  return /\.(?:cmd|bat)$/i.test(candidate);
}

function requireRegularAbsoluteFile(label, candidate, pathApi, isRegularFile) {
  if (!pathApi.isAbsolute(candidate) || isShellShim(candidate) || !isRegularFile(candidate)) {
    throw new Error(`${label} is unavailable`);
  }
}

export function createToolCommandPlan({
  platform,
  nodeExecutable,
  npmExecPath,
  bufEntry,
  isRegularFile,
}) {
  const pathApi = platform === "win32" ? win32 : posix;
  requireRegularAbsoluteFile("Node executable", nodeExecutable, pathApi, isRegularFile);
  requireRegularAbsoluteFile("Buf entry point", bufEntry, pathApi, isRegularFile);

  const canUseLifecyclePnpm =
    typeof npmExecPath === "string" &&
    isJavaScriptEntry(npmExecPath) &&
    pathApi.isAbsolute(npmExecPath) &&
    !isShellShim(npmExecPath) &&
    isRegularFile(npmExecPath);
  const corepackEntry =
    platform === "win32"
      ? pathApi.join(
          pathApi.dirname(nodeExecutable),
          "node_modules",
          "corepack",
          "dist",
          "corepack.js",
        )
      : pathApi.resolve(
          pathApi.dirname(nodeExecutable),
          "..",
          "lib",
          "node_modules",
          "corepack",
          "dist",
          "corepack.js",
        );
  const pnpmEntry = canUseLifecyclePnpm ? npmExecPath : corepackEntry;

  requireRegularAbsoluteFile("pnpm entry point", pnpmEntry, pathApi, isRegularFile);
  if (!isJavaScriptEntry(pnpmEntry)) {
    throw new Error("pnpm entry point is unavailable");
  }

  return {
    node: { file: nodeExecutable, args: ["--version"] },
    pnpm: {
      file: nodeExecutable,
      args: canUseLifecyclePnpm ? [pnpmEntry, "--version"] : [pnpmEntry, "pnpm", "--version"],
    },
    go: { file: platform === "win32" ? "go.exe" : "go", args: ["version"] },
    buf: { file: nodeExecutable, args: [bufEntry, "--version"] },
  };
}

export function executeToolCommandPlan(plan, execute = execFileSync) {
  const outputs = {};

  for (const [tool, command] of Object.entries(plan)) {
    try {
      outputs[tool] = execute(command.file, command.args, { encoding: "utf8" }).trim();
    } catch {
      throw new Error(`Unable to execute ${tool} version check`);
    }
  }

  return outputs;
}

export function validateToolVersions(outputs) {
  const errors = [];
  const goVersion = outputs.go.trim().split(/\s+/)[2] ?? outputs.go.trim();

  if (outputs.node !== "v24.18.0") {
    errors.push(`Node must be v24.18.0 (received ${outputs.node})`);
  }
  if (outputs.pnpm !== "11.15.0") {
    errors.push(`pnpm must be 11.15.0 (received ${outputs.pnpm})`);
  }
  if (goVersion !== "go1.26.4") {
    errors.push(`Go must be go1.26.4 (received ${goVersion})`);
  }
  if (outputs.buf !== "1.72.0") {
    errors.push(`Buf must be 1.72.0 (received ${outputs.buf})`);
  }

  return errors;
}

if (import.meta.main) {
  try {
    const bufEntry = fileURLToPath(import.meta.resolve("@bufbuild/buf/bin/buf"));
    const plan = createToolCommandPlan({
      platform: process.platform,
      nodeExecutable: process.execPath,
      npmExecPath: process.env.npm_execpath,
      bufEntry,
      isRegularFile: (candidate) => {
        try {
          return statSync(candidate).isFile();
        } catch {
          return false;
        }
      },
    });
    const errors = validateToolVersions(executeToolCommandPlan(plan));

    if (errors.length > 0) {
      for (const error of errors) {
        console.error(error);
      }
      process.exitCode = 1;
    } else {
      console.log("toolchain ok");
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown toolchain error";
    console.error(`Toolchain check failed: ${message}`);
    process.exitCode = 1;
  }
}
