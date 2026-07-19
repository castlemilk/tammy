import { execFile as nodeExecFile } from "node:child_process";
import { createHash } from "node:crypto";
import { lstat, mkdir, readdir, readFile, rm, truncate } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const BUILD_TIMEOUT_MS = 120_000;
const CORE_PACKAGE = "./services/core/cmd/tammy-core";
const VERSION_SYMBOL = "github.com/tammyapp/tammy/services/core/internal/buildinfo.version";
const VERSION_PATTERN = /^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$/;

const targets = Object.freeze({
  "darwin/arm64": Object.freeze({
    arch: "arm64",
    executable: "tammy-core",
    goarch: "arm64",
    goos: "darwin",
    platform: "darwin",
  }),
  "win32/x64": Object.freeze({
    arch: "x64",
    executable: "tammy-core.exe",
    goarch: "amd64",
    goos: "windows",
    platform: "win32",
  }),
});

export function selectTarget(platform, arch) {
  const target = targets[`${platform}/${arch}`];
  if (!target) {
    throw new Error("UNSUPPORTED_CORE_TARGET");
  }
  return Object.freeze({
    arch: target.arch,
    executable: target.executable,
    platform: target.platform,
  });
}

export function parseCiTarget(value) {
  if (typeof value !== "string" || !/^[a-z0-9]+\/[a-z0-9]+$/.test(value)) {
    throw new Error("UNSUPPORTED_CORE_TARGET");
  }
  const [platform, arch] = value.split("/");
  return selectTarget(platform, arch);
}

function targetDetails(target) {
  const selected = selectTarget(target?.platform, target?.arch);
  if (selected.executable !== target?.executable) {
    throw new Error("INVALID_CORE_PATH");
  }
  return targets[`${selected.platform}/${selected.arch}`];
}

function assertContained(parent, candidate) {
  const relative = path.relative(parent, candidate);
  if (
    relative === "" ||
    relative.startsWith(`..${path.sep}`) ||
    relative === ".." ||
    path.isAbsolute(relative)
  ) {
    throw new Error("INVALID_CORE_PATH");
  }
}

export function resolveCoreBinary(resourcesRoot, target) {
  if (!path.isAbsolute(resourcesRoot)) {
    throw new Error("INVALID_CORE_PATH");
  }
  const details = targetDetails(target);
  const result = path.resolve(
    resourcesRoot,
    `${details.platform}-${details.arch}`,
    details.executable,
  );
  assertContained(resourcesRoot, result);
  return result;
}

export function createBuildPlan({ root, target, version, sourceEnvironment = process.env }) {
  if (!path.isAbsolute(root)) {
    throw new Error("INVALID_PROJECT_ROOT");
  }
  if (typeof version !== "string" || !VERSION_PATTERN.test(version)) {
    throw new Error("INVALID_DESKTOP_VERSION");
  }
  const details = targetDetails(target);
  const resourcesRoot = path.join(root, "apps/desktop/resources/core");
  const output = resolveCoreBinary(resourcesRoot, target);
  return Object.freeze({
    args: Object.freeze([
      "build",
      "-trimpath",
      "-buildvcs=true",
      `-ldflags=-s -w -X ${VERSION_SYMBOL}=${version}`,
      "-o",
      output,
      CORE_PACKAGE,
    ]),
    command: "go",
    options: Object.freeze({
      cwd: root,
      env: Object.freeze({
        ...sourceEnvironment,
        CGO_ENABLED: "0",
        GOARCH: details.goarch,
        GOOS: details.goos,
      }),
      shell: false,
      windowsHide: true,
    }),
    output,
    resourcesRoot,
  });
}

export async function cleanCoreResources(resourcesRoot) {
  const rootStats = await lstat(resourcesRoot).catch(() => null);
  if (!rootStats?.isDirectory() || rootStats.isSymbolicLink()) {
    throw new Error("INVALID_CORE_RESOURCES");
  }
  const keepPath = path.join(resourcesRoot, ".gitkeep");
  let keepStats = await lstat(keepPath).catch(() => null);
  if (!keepStats?.isFile() || keepStats.isSymbolicLink()) {
    throw new Error("INVALID_CORE_RESOURCES");
  }
  if (keepStats.size === 1 && (await readFile(keepPath, "utf8")) === "\n") {
    await truncate(keepPath, 0);
    keepStats = await lstat(keepPath);
  }
  if (keepStats.size !== 0) throw new Error("INVALID_CORE_RESOURCES");
  for (const entry of await readdir(resourcesRoot, { withFileTypes: true })) {
    if (entry.name === ".gitkeep") {
      continue;
    }
    await rm(path.join(resourcesRoot, entry.name), { force: true, recursive: true });
  }
}

function productionExecFile(command, args, options) {
  return new Promise((resolve, reject) => {
    nodeExecFile(command, args, options, (error) => {
      if (error) {
        reject(error);
      } else {
        resolve();
      }
    });
  });
}

export async function buildCore({
  root,
  target,
  version,
  execFile = productionExecFile,
  timeoutMs = BUILD_TIMEOUT_MS,
  sourceEnvironment,
}) {
  const plan = createBuildPlan({ root, target, version, sourceEnvironment });
  await cleanCoreResources(plan.resourcesRoot);
  await mkdir(path.dirname(plan.output), { recursive: true });
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    await execFile(plan.command, plan.args, {
      ...plan.options,
      killSignal: "SIGKILL",
      signal: controller.signal,
      timeout: timeoutMs,
    });
  } catch {
    await cleanCoreResources(plan.resourcesRoot);
    throw new Error(controller.signal.aborted ? "CORE_BUILD_TIMEOUT" : "CORE_BUILD_FAILED");
  } finally {
    clearTimeout(timeout);
  }
  const outputStats = await lstat(plan.output).catch(() => null);
  if (!outputStats?.isFile() || outputStats.isSymbolicLink()) {
    await cleanCoreResources(plan.resourcesRoot);
    throw new Error("CORE_BINARY_MISSING");
  }
  const sha256 = createHash("sha256")
    .update(await readFile(plan.output))
    .digest("hex");
  return Object.freeze({ path: plan.output, sha256 });
}

export function selectBuildTarget(environment, platform, arch) {
  if (environment.TAMMY_CORE_TARGET !== undefined) {
    if (environment.CI !== "true") {
      throw new Error("CI_TARGET_REQUIRES_CI");
    }
    return parseCiTarget(environment.TAMMY_CORE_TARGET);
  }
  return selectTarget(platform, arch);
}

async function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const desktopPackage = JSON.parse(
    await readFile(path.join(root, "apps/desktop/package.json"), "utf8"),
  );
  const result = await buildCore({
    root,
    target: selectBuildTarget(process.env, process.platform, process.arch),
    version: desktopPackage.version,
  });
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main().catch((error) => {
    const code = error instanceof Error ? error.message : "CORE_BUILD_FAILED";
    process.stderr.write(`${JSON.stringify({ error: code })}\n`);
    process.exitCode = 1;
  });
}
