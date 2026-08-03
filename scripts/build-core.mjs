import { execFile as nodeExecFile } from "node:child_process";
import { createHash } from "node:crypto";
import { lstat, mkdir, mkdtemp, readdir, readFile, rm, truncate } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const BUILD_TIMEOUT_MS = 120_000;
const CORE_PACKAGE = "./services/core/cmd/tammy-core";
const VERSION_SYMBOL = "github.com/tammyapp/tammy/services/core/internal/buildinfo.version";
const SQLCIPHER_HASH_SYMBOL =
  "github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher.linkedLibrarySHA256";
const VERSION_PATTERN = /^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$/;
const HASH_PATTERN = /^[a-f0-9]{64}$/;
const SQLCIPHER_VERSION = "4.15.0";

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

async function readRegularFile(candidate, code) {
  const stats = await lstat(candidate).catch(() => null);
  if (!stats?.isFile() || stats.isSymbolicLink()) throw new Error(code);
  return readFile(candidate);
}

async function requireRegularDirectory(candidate, code) {
  const stats = await lstat(candidate).catch(() => null);
  if (!stats?.isDirectory() || stats.isSymbolicLink()) throw new Error(code);
}

export async function loadSqlcipherBuildInput(root, target, sourceEnvironment = process.env) {
  if (!path.isAbsolute(root)) throw new Error("INVALID_PROJECT_ROOT");
  const details = targetDetails(target);
  const targetName = `${details.platform}-${details.arch}`;
  const resourceRoot = path.join(root, "apps/desktop/resources/sqlcipher");
  const targetRoot = path.join(resourceRoot, targetName);
  const include = path.join(targetRoot, "include");
  const library = path.join(targetRoot, "lib/libsqlite3.a");
  const header = path.join(include, "sqlite3.h");
  await Promise.all([
    requireRegularDirectory(resourceRoot, "SQLCIPHER_RESOURCE_INVALID"),
    requireRegularDirectory(targetRoot, "SQLCIPHER_RESOURCE_INVALID"),
    requireRegularDirectory(include, "SQLCIPHER_RESOURCE_INVALID"),
    requireRegularDirectory(path.dirname(library), "SQLCIPHER_RESOURCE_INVALID"),
  ]);
  const [
    versionBytes,
    expectedHashBytes,
    libraryBytes,
    headerBytes,
    expectedHeaderHashBytes,
    licenseBytes,
    pinnedLicenseBytes,
  ] = await Promise.all([
    readRegularFile(path.join(resourceRoot, "VERSION"), "SQLCIPHER_VERSION_MISSING"),
    readRegularFile(path.join(targetRoot, "LIBRARY_SHA256"), "SQLCIPHER_HASH_MISSING"),
    readRegularFile(library, "SQLCIPHER_LIBRARY_MISSING"),
    readRegularFile(header, "SQLCIPHER_HEADER_MISSING"),
    readRegularFile(path.join(targetRoot, "HEADER_SHA256"), "SQLCIPHER_HEADER_HASH_MISSING"),
    readRegularFile(path.join(resourceRoot, "LICENSE"), "SQLCIPHER_LICENSE_MISSING"),
    readRegularFile(
      path.join(root, "third_party/sqlcipher/LICENSE"),
      "SQLCIPHER_PINNED_LICENSE_MISSING",
    ),
  ]);
  const version = versionBytes.toString("utf8").replace(/\n$/, "");
  const expectedHash = expectedHashBytes.toString("utf8").replace(/\n$/, "");
  const expectedHeaderHash = expectedHeaderHashBytes.toString("utf8").replace(/\n$/, "");
  const actualHash = createHash("sha256").update(libraryBytes).digest("hex");
  const actualHeaderHash = createHash("sha256").update(headerBytes).digest("hex");
  if (
    version !== SQLCIPHER_VERSION ||
    expectedHash !== actualHash ||
    expectedHeaderHash !== actualHeaderHash ||
    !HASH_PATTERN.test(actualHash) ||
    !HASH_PATTERN.test(actualHeaderHash) ||
    !licenseBytes.equals(pinnedLicenseBytes)
  ) {
    throw new Error("SQLCIPHER_RESOURCE_AUTHENTICATION_FAILED");
  }
  const compiler =
    details.platform === "darwin" ? "/usr/bin/clang" : sourceEnvironment.TAMMY_SQLCIPHER_CC;
  if (typeof compiler !== "string" || !path.isAbsolute(compiler)) {
    throw new Error("SQLCIPHER_COMPILER_MISSING");
  }
  await readRegularFile(compiler, "SQLCIPHER_COMPILER_MISSING");
  const input = {
    compiler,
    header,
    headerSha256: actualHeaderHash,
    include,
    library,
    librarySha256: actualHash,
    target: targetName,
    version,
  };
  if (details.platform === "win32") {
    const providerLibrary = path.join(targetRoot, "lib/libcrypto.a");
    const [providerBytes, providerHashBytes, providerLicense, pinnedProviderLicense] =
      await Promise.all([
        readRegularFile(providerLibrary, "SQLCIPHER_OPENSSL_MISSING"),
        readRegularFile(
          path.join(targetRoot, "OPENSSL_LIBRARY_SHA256"),
          "SQLCIPHER_OPENSSL_MISSING",
        ),
        readRegularFile(path.join(resourceRoot, "OPENSSL_LICENSE"), "SQLCIPHER_OPENSSL_MISSING"),
        readRegularFile(
          path.join(root, "third_party/sqlcipher/OPENSSL_LICENSE"),
          "SQLCIPHER_OPENSSL_MISSING",
        ),
      ]);
    const expectedProviderHash = providerHashBytes.toString("utf8").replace(/\n$/, "");
    const providerLibrarySha256 = createHash("sha256").update(providerBytes).digest("hex");
    if (
      expectedProviderHash !== providerLibrarySha256 ||
      !HASH_PATTERN.test(providerLibrarySha256) ||
      !providerLicense.equals(pinnedProviderLicense)
    ) {
      throw new Error("SQLCIPHER_OPENSSL_AUTHENTICATION_FAILED");
    }
    input.providerLibrary = providerLibrary;
    input.providerLibrarySha256 = providerLibrarySha256;
  }
  return Object.freeze(input);
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

function validateSqlcipherInput(root, details, input) {
  const expectedTarget = `${details.platform}-${details.arch}`;
  const expectedRoot = path.join(root, "apps/desktop/resources/sqlcipher", expectedTarget);
  const expected = {
    header: path.join(expectedRoot, "include/sqlite3.h"),
    include: path.join(expectedRoot, "include"),
    library: path.join(expectedRoot, "lib/libsqlite3.a"),
  };
  const compilerIsAbsolute =
    details.platform === "win32"
      ? path.win32.isAbsolute(input?.compiler ?? "")
      : path.posix.isAbsolute(input?.compiler ?? "");
  if (!input || typeof input !== "object") throw new Error("SQLCIPHER_BUILD_INPUT_INVALID");
  if (input.target !== expectedTarget) throw new Error("SQLCIPHER_TARGET_MISMATCH");
  if (
    input.version !== SQLCIPHER_VERSION ||
    input.header !== expected.header ||
    !HASH_PATTERN.test(input.headerSha256) ||
    input.include !== expected.include ||
    input.library !== expected.library ||
    !compilerIsAbsolute ||
    !HASH_PATTERN.test(input.librarySha256)
  ) {
    throw new Error("SQLCIPHER_BUILD_INPUT_INVALID");
  }
  if (details.platform === "win32") {
    if (
      input.providerLibrary !== path.join(expectedRoot, "lib/libcrypto.a") ||
      !HASH_PATTERN.test(input.providerLibrarySha256)
    ) {
      throw new Error("SQLCIPHER_BUILD_INPUT_INVALID");
    }
  } else if (input.providerLibrary !== undefined || input.providerLibrarySha256 !== undefined) {
    throw new Error("SQLCIPHER_BUILD_INPUT_INVALID");
  }
  return input;
}

export function createBuildPlan({
  root,
  sqlcipher,
  target,
  version,
  sourceEnvironment = process.env,
}) {
  if (!path.isAbsolute(root)) {
    throw new Error("INVALID_PROJECT_ROOT");
  }
  if (typeof version !== "string" || !VERSION_PATTERN.test(version)) {
    throw new Error("INVALID_DESKTOP_VERSION");
  }
  const details = targetDetails(target);
  const cipher = validateSqlcipherInput(root, details, sqlcipher);
  const resourcesRoot = path.join(root, "apps/desktop/resources/core");
  const output = resolveCoreBinary(resourcesRoot, target);
  return Object.freeze({
    args: Object.freeze([
      "build",
      "-trimpath",
      "-buildvcs=true",
      "-tags=tammy_sqlcipher",
      `-ldflags=-s -w -X ${VERSION_SYMBOL}=${version} -X ${SQLCIPHER_HASH_SYMBOL}=${cipher.librarySha256}`,
      "-o",
      output,
      CORE_PACKAGE,
    ]),
    command: "go",
    options: Object.freeze({
      cwd: root,
      env: Object.freeze({
        ...sourceEnvironment,
        CC: cipher.compiler,
        CGO_CFLAGS: `-DSQLITE_HAS_CODEC -I${cipher.include}`,
        CGO_ENABLED: "1",
        CGO_LDFLAGS:
          details.platform === "darwin"
            ? `${cipher.library} -framework CoreFoundation -framework Security`
            : `${cipher.library} ${cipher.providerLibrary} -lcrypt32 -luser32 -lws2_32`,
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

export async function createBuildCache({
  temporaryRoot = os.tmpdir(),
  makeDirectory = mkdtemp,
} = {}) {
  if (
    typeof temporaryRoot !== "string" ||
    !path.isAbsolute(temporaryRoot) ||
    path.normalize(temporaryRoot) !== temporaryRoot
  ) {
    throw new Error("INVALID_BUILD_CACHE");
  }
  const rootStats = await lstat(temporaryRoot).catch(() => null);
  if (!rootStats?.isDirectory() || rootStats.isSymbolicLink()) {
    throw new Error("INVALID_BUILD_CACHE");
  }
  const cache = await makeDirectory(path.join(temporaryRoot, "tammy-go-build-cache-")).catch(() => {
    throw new Error("INVALID_BUILD_CACHE");
  });
  const relative = path.relative(temporaryRoot, cache);
  const cacheStats = await lstat(cache).catch(() => null);
  if (
    typeof cache !== "string" ||
    !path.isAbsolute(cache) ||
    path.normalize(cache) !== cache ||
    relative === "" ||
    relative === ".." ||
    relative.startsWith(`..${path.sep}`) ||
    path.isAbsolute(relative) ||
    !cacheStats?.isDirectory() ||
    cacheStats.isSymbolicLink()
  ) {
    throw new Error("INVALID_BUILD_CACHE");
  }
  return cache;
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
  sqlcipher,
  target,
  version,
  execFile = productionExecFile,
  timeoutMs = BUILD_TIMEOUT_MS,
  sourceEnvironment,
  temporaryRoot = os.tmpdir(),
  hostPlatform = process.platform,
  hostArch = process.arch,
  makeCacheDirectory = mkdtemp,
  removeCacheDirectory = (directory) => rm(directory, { force: true, recursive: true }),
}) {
  const nativeTarget = selectTarget(hostPlatform, hostArch);
  if (nativeTarget.platform !== target?.platform || nativeTarget.arch !== target?.arch) {
    throw new Error("CORE_CGO_CROSS_BUILD_UNSUPPORTED");
  }
  const plan = createBuildPlan({ root, sqlcipher, target, version, sourceEnvironment });
  await cleanCoreResources(plan.resourcesRoot);
  await mkdir(path.dirname(plan.output), { recursive: true });
  let cacheDirectory;
  let buildError;
  let result;
  try {
    cacheDirectory = await createBuildCache({
      makeDirectory: makeCacheDirectory,
      temporaryRoot,
    });
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), timeoutMs);
    try {
      await execFile(plan.command, plan.args, {
        ...plan.options,
        env: Object.freeze({
          ...plan.options.env,
          GOCACHE: cacheDirectory,
        }),
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
    result = Object.freeze({ path: plan.output, sha256 });
  } catch (error) {
    buildError = error;
  }
  if (cacheDirectory) {
    try {
      await removeCacheDirectory(cacheDirectory);
    } catch {
      await cleanCoreResources(plan.resourcesRoot);
      throw new Error("CORE_BUILD_CACHE_CLEANUP_FAILED");
    }
  }
  if (buildError) {
    throw buildError;
  }
  return result;
}

export function selectBuildTarget(environment, platform, arch) {
  if (environment.TAMMY_CORE_TARGET !== undefined) {
    if (environment.CI !== "true") {
      throw new Error("CI_TARGET_REQUIRES_CI");
    }
    const selected = parseCiTarget(environment.TAMMY_CORE_TARGET);
    const native = selectTarget(platform, arch);
    if (selected.platform !== native.platform || selected.arch !== native.arch) {
      throw new Error("CORE_CGO_CROSS_BUILD_UNSUPPORTED");
    }
    return selected;
  }
  return selectTarget(platform, arch);
}

async function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const desktopPackage = JSON.parse(
    await readFile(path.join(root, "apps/desktop/package.json"), "utf8"),
  );
  const target = selectBuildTarget(process.env, process.platform, process.arch);
  const result = await buildCore({
    root,
    sqlcipher: await loadSqlcipherBuildInput(root, target),
    target,
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
