import { createHash } from "node:crypto";
import { lstat, readdir, readFile } from "node:fs/promises";
import path from "node:path";

const HASH_PATTERN = /^[0-9a-f]{64}$/;

const TARGETS = Object.freeze({
  "darwin/arm64": Object.freeze({
    app: "out/Tammy-darwin-arm64/Tammy.app/Contents/MacOS/Tammy",
    build: "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/build",
    core: "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/core",
    executable: "tammy-core",
    resourceBase: "out/Tammy-darwin-arm64/Tammy.app/Contents/Resources",
    target: "darwin-arm64",
  }),
  "win32/x64": Object.freeze({
    app: "out/Tammy-win32-x64/Tammy.exe",
    build: "out/Tammy-win32-x64/resources/build",
    core: "out/Tammy-win32-x64/resources/core",
    executable: "tammy-core.exe",
    resourceBase: "out/Tammy-win32-x64/resources",
    target: "win32-x64",
  }),
});

function selectTarget(platform, arch) {
  const selected = TARGETS[`${platform}/${arch}`];
  if (!selected) throw new Error("UNSUPPORTED_PACKAGE_TARGET");
  return selected;
}

function assertAbsoluteDirectoryPath(value) {
  if (
    typeof value !== "string" ||
    !path.isAbsolute(value) ||
    path.normalize(value) !== value ||
    value.split(path.sep).includes("..")
  ) {
    throw new Error("INVALID_DESKTOP_ROOT");
  }
}

function assertContained(parent, candidate, code) {
  const relative = path.relative(parent, candidate);
  if (
    relative === "" ||
    relative === ".." ||
    relative.startsWith(`..${path.sep}`) ||
    path.isAbsolute(relative)
  ) {
    throw new Error(code);
  }
}

export function resolvePackagedLayout({ desktopRoot, platform, arch }) {
  assertAbsoluteDirectoryPath(desktopRoot);
  const selected = selectTarget(platform, arch);
  const sourceCoreRoot = path.join(desktopRoot, "resources", "core");
  const sourceBuildRoot = path.join(desktopRoot, "resources", "build");
  const packagedCoreRoot = path.join(desktopRoot, selected.core);
  const packagedBuildRoot = path.join(desktopRoot, selected.build);
  const sourceCore = path.join(sourceCoreRoot, selected.target, selected.executable);
  const packagedCore = path.join(packagedCoreRoot, selected.target, selected.executable);
  const sourceManifest = path.join(sourceBuildRoot, "build-manifest.json");
  const packagedManifest = path.join(packagedBuildRoot, "build-manifest.json");
  const appExecutable = path.join(desktopRoot, selected.app);
  for (const candidate of [
    sourceCoreRoot,
    sourceBuildRoot,
    packagedCoreRoot,
    packagedBuildRoot,
    sourceCore,
    packagedCore,
    sourceManifest,
    packagedManifest,
    appExecutable,
  ]) {
    assertContained(desktopRoot, candidate, "PACKAGE_PATH_TRAVERSAL");
  }
  if (packagedCore.split(path.sep).includes("app.asar")) {
    throw new Error("PACKAGED_CORE_INSIDE_ASAR");
  }
  return {
    appExecutable,
    packagedBuildRoot,
    packagedCore,
    packagedCoreRoot,
    packagedManifest,
    sourceBuildRoot,
    sourceCore,
    sourceCoreRoot,
    sourceManifest,
    target: selected.target,
  };
}

async function enumerateTree(root, code) {
  const rootStats = await lstat(root).catch(() => null);
  if (!rootStats?.isDirectory() || rootStats.isSymbolicLink()) {
    throw new Error(code);
  }
  const entries = [];
  async function visit(directory, prefix) {
    const children = await readdir(directory, { withFileTypes: true });
    children.sort((left, right) => left.name.localeCompare(right.name));
    for (const child of children) {
      const relative = prefix ? `${prefix}/${child.name}` : child.name;
      const absolute = path.join(directory, child.name);
      const stats = await lstat(absolute);
      if (stats.isSymbolicLink()) throw new Error(code);
      if (stats.isDirectory()) {
        entries.push(`${relative}/`);
        await visit(absolute, relative);
      } else if (stats.isFile()) {
        entries.push(relative);
      } else {
        throw new Error(code);
      }
    }
  }
  await visit(root, "");
  return entries;
}

async function assertExactTree(root, expected, code) {
  const actual = await enumerateTree(root, code);
  if (
    actual.length !== expected.length ||
    actual.some((entry, index) => entry !== expected[index])
  ) {
    throw new Error(code);
  }
  const keep = await lstat(path.join(root, ".gitkeep")).catch(() => null);
  if (!keep?.isFile() || keep.isSymbolicLink() || keep.size !== 0) {
    throw new Error(code);
  }
}

async function assertRegularFile(file, code) {
  const stats = await lstat(file).catch(() => null);
  if (!stats?.isFile() || stats.isSymbolicLink()) throw new Error(code);
  return stats;
}

async function hashFile(file) {
  return createHash("sha256")
    .update(await readFile(file))
    .digest("hex");
}

export async function verifyPackagedLayout({ desktopRoot, platform, arch, sourceManifestPath }) {
  const layout = resolvePackagedLayout({ desktopRoot, platform, arch });
  if (
    typeof sourceManifestPath !== "string" ||
    path.resolve(sourceManifestPath) !== layout.sourceManifest
  ) {
    throw new Error("SOURCE_MANIFEST_PATH_INVALID");
  }
  const executable = path.basename(layout.sourceCore);
  const targetDirectory = path.basename(path.dirname(layout.sourceCore));
  const coreAllowlist = [".gitkeep", `${targetDirectory}/`, `${targetDirectory}/${executable}`];
  await assertExactTree(layout.sourceCoreRoot, coreAllowlist, "SOURCE_CORE_LAYOUT_INVALID");
  await assertExactTree(
    layout.sourceBuildRoot,
    [".gitkeep", "build-manifest.json"],
    "SOURCE_BUILD_LAYOUT_INVALID",
  );
  await assertExactTree(layout.packagedCoreRoot, coreAllowlist, "PACKAGED_CORE_LAYOUT_INVALID");
  await assertExactTree(
    layout.packagedBuildRoot,
    [".gitkeep", "build-manifest.json"],
    "PACKAGED_BUILD_LAYOUT_INVALID",
  );

  const appStats = await assertRegularFile(layout.appExecutable, "PACKAGE_APP_INVALID");
  const sourceCoreStats = await assertRegularFile(layout.sourceCore, "SOURCE_CORE_LAYOUT_INVALID");
  const packagedCoreStats = await assertRegularFile(
    layout.packagedCore,
    "PACKAGED_CORE_LAYOUT_INVALID",
  );
  await assertRegularFile(layout.sourceManifest, "SOURCE_BUILD_LAYOUT_INVALID");
  await assertRegularFile(layout.packagedManifest, "PACKAGED_BUILD_LAYOUT_INVALID");
  if (platform === "darwin") {
    if ((appStats.mode & 0o111) === 0) throw new Error("PACKAGE_APP_NOT_EXECUTABLE");
    if ((sourceCoreStats.mode & 0o111) === 0) {
      throw new Error("SOURCE_CORE_NOT_EXECUTABLE");
    }
    if ((packagedCoreStats.mode & 0o111) === 0) {
      throw new Error("PACKAGED_CORE_NOT_EXECUTABLE");
    }
  }

  const sourceManifest = await readFile(layout.sourceManifest);
  const packagedManifest = await readFile(layout.packagedManifest);
  if (!sourceManifest.equals(packagedManifest)) {
    throw new Error("PACKAGED_MANIFEST_MISMATCH");
  }
  let manifest;
  try {
    manifest = JSON.parse(sourceManifest.toString("utf8"));
  } catch {
    throw new Error("SOURCE_MANIFEST_INVALID");
  }
  if (
    manifest?.schema !== "tammy-build-manifest-v1" ||
    typeof manifest.core_sha256 !== "string" ||
    !HASH_PATTERN.test(manifest.core_sha256)
  ) {
    throw new Error("SOURCE_MANIFEST_INVALID");
  }
  const sourceCoreHash = await hashFile(layout.sourceCore);
  if (sourceCoreHash !== manifest.core_sha256) {
    throw new Error("SOURCE_CORE_HASH_MISMATCH");
  }
  const packagedCoreHash = await hashFile(layout.packagedCore);
  if (packagedCoreHash !== manifest.core_sha256) {
    throw new Error("PACKAGED_CORE_HASH_MISMATCH");
  }
  return {
    appExecutable: layout.appExecutable,
    appSha256: await hashFile(layout.appExecutable),
    coreExecutable: layout.packagedCore,
    coreSha256: packagedCoreHash,
    manifest: layout.packagedManifest,
    manifestSha256: createHash("sha256").update(sourceManifest).digest("hex"),
    target: layout.target,
  };
}

function parseArguments(argv) {
  let sourceManifestPath;
  let verify = false;
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--verify") {
      verify = true;
    } else if (argument === "--source-manifest" && index + 1 < argv.length) {
      sourceManifestPath = path.resolve(argv[index + 1]);
      index += 1;
    } else {
      throw new Error("INVALID_PACKAGE_ARGUMENTS");
    }
  }
  if (!verify || !sourceManifestPath) throw new Error("INVALID_PACKAGE_ARGUMENTS");
  return sourceManifestPath;
}

async function main() {
  const desktopRoot = path.resolve(import.meta.dirname, "..");
  const sourceManifestPath = parseArguments(process.argv.slice(2));
  const result = await verifyPackagedLayout({
    desktopRoot,
    platform: process.platform,
    arch: process.arch,
    sourceManifestPath,
  });
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === import.meta.filename) {
  main().catch((error) => {
    const code = error instanceof Error ? error.message : "PACKAGE_VERIFICATION_FAILED";
    process.stderr.write(`${JSON.stringify({ error: code })}\n`);
    process.exitCode = 1;
  });
}
