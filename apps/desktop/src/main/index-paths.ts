import { constants as fsConstants } from "node:fs";
import { lstat, open, realpath } from "node:fs/promises";
import path from "node:path";

import { type AuthenticatedCoreExecutable, authenticateCoreExecutable } from "./core-executable";

interface BundledResourcePathOptions {
  readonly arch: string;
  readonly developmentResourcesPath: string;
  readonly isPackaged: boolean;
  readonly platform: string;
  readonly resourcesPath: string;
}

const CORE_TARGETS: Readonly<Record<string, string>> = Object.freeze({
  "darwin/arm64": "tammy-core",
  "win32/x64": "tammy-core.exe",
});
const MAX_BUILD_MANIFEST_BYTES = 65_536;

async function pinnedCoreDigest(
  resourcesRoot: string,
  physicalResources: string,
  target: string,
): Promise<string> {
  const buildRoot = path.join(resourcesRoot, "build");
  const physicalBuild = await requirePhysicalDirectory(buildRoot, "INVALID_CORE_BINARY");
  if (!isContained(physicalResources, physicalBuild)) throw new Error("INVALID_CORE_BINARY");
  const manifestPath = path.join(physicalBuild, "build-manifest.json");
  let handle: Awaited<ReturnType<typeof open>> | undefined;
  try {
    handle = await open(manifestPath, fsConstants.O_RDONLY | (fsConstants.O_NOFOLLOW ?? 0));
    const before = await handle.stat({ bigint: true });
    if (
      !before.isFile() ||
      before.isSymbolicLink() ||
      before.nlink !== 1n ||
      before.size <= 0n ||
      before.size > BigInt(MAX_BUILD_MANIFEST_BYTES)
    ) {
      throw new Error();
    }
    const bytes = await handle.readFile();
    const after = await handle.stat({ bigint: true });
    if (
      before.dev !== after.dev ||
      before.ino !== after.ino ||
      before.mode !== after.mode ||
      before.nlink !== after.nlink ||
      before.size !== after.size ||
      before.mtimeNs !== after.mtimeNs ||
      before.ctimeNs !== after.ctimeNs ||
      BigInt(bytes.byteLength) !== before.size
    ) {
      throw new Error();
    }
    const manifest: unknown = JSON.parse(bytes.toString("utf8"));
    if (
      manifest === null ||
      typeof manifest !== "object" ||
      (manifest as Record<string, unknown>).schema !== "tammy-build-manifest-v1" ||
      (manifest as Record<string, unknown>).target !== target ||
      !/^[0-9a-f]{64}$/.test(String((manifest as Record<string, unknown>).core_sha256))
    ) {
      throw new Error();
    }
    return String((manifest as Record<string, unknown>).core_sha256);
  } catch {
    throw new Error("INVALID_CORE_BINARY");
  } finally {
    await handle?.close().catch(() => undefined);
  }
}

function isContained(parent: string, candidate: string): boolean {
  const relative = path.relative(parent, candidate);
  return (
    relative !== "" &&
    relative !== ".." &&
    !relative.startsWith(`..${path.sep}`) &&
    !path.isAbsolute(relative)
  );
}

async function requirePhysicalDirectory(candidate: string, errorCode: string): Promise<string> {
  const before = await lstat(candidate).catch(() => null);
  if (!before?.isDirectory() || before.isSymbolicLink()) {
    throw new Error(errorCode);
  }
  const physical = await realpath(candidate).catch(() => {
    throw new Error(errorCode);
  });
  const after = await lstat(candidate).catch(() => null);
  if (
    !after?.isDirectory() ||
    after.isSymbolicLink() ||
    before.dev !== after.dev ||
    before.ino !== after.ino
  ) {
    throw new Error(errorCode);
  }
  return physical;
}

export async function resolveBundledCorePath(
  options: BundledResourcePathOptions,
): Promise<AuthenticatedCoreExecutable> {
  const executable = CORE_TARGETS[`${options.platform}/${options.arch}`];
  if (!executable) {
    throw new Error("UNSUPPORTED_CORE_TARGET");
  }
  const resourcesRoot = path.resolve(
    options.isPackaged ? options.resourcesPath : options.developmentResourcesPath,
  );
  const coreRoot = path.join(resourcesRoot, "core");
  const targetRoot = path.join(coreRoot, `${options.platform}-${options.arch}`);
  const candidate = path.join(targetRoot, executable);
  if (!path.isAbsolute(candidate) || !isContained(coreRoot, candidate)) {
    throw new Error("INVALID_CORE_BINARY");
  }
  const [physicalResources, physicalCore, physicalTarget] = await Promise.all([
    requirePhysicalDirectory(resourcesRoot, "INVALID_CORE_BINARY"),
    requirePhysicalDirectory(coreRoot, "INVALID_CORE_BINARY"),
    requirePhysicalDirectory(targetRoot, "INVALID_CORE_BINARY"),
  ]);
  if (!isContained(physicalResources, physicalCore) || !isContained(physicalCore, physicalTarget)) {
    throw new Error("INVALID_CORE_BINARY");
  }
  const stats = await lstat(candidate).catch(() => null);
  if (!stats?.isFile() || stats.isSymbolicLink()) {
    throw new Error("INVALID_CORE_BINARY");
  }
  const physicalCandidate = await realpath(candidate).catch(() => null);
  const confirmedStats =
    physicalCandidate === null ? null : await lstat(physicalCandidate).catch(() => null);
  if (
    physicalCandidate === null ||
    !confirmedStats?.isFile() ||
    confirmedStats.isSymbolicLink() ||
    confirmedStats.dev !== stats.dev ||
    confirmedStats.ino !== stats.ino ||
    !isContained(physicalTarget, physicalCandidate)
  ) {
    throw new Error("INVALID_CORE_BINARY");
  }
  const expectedDigest = await pinnedCoreDigest(
    resourcesRoot,
    physicalResources,
    `${options.platform}-${options.arch}`,
  );
  const authenticated = await authenticateCoreExecutable(physicalCandidate);
  if (authenticated.sha256 !== expectedDigest) throw new Error("INVALID_CORE_BINARY");
  return authenticated;
}

export interface BundledSbrProfileLocation {
  readonly profilePath: string;
  readonly resourcesRoot: string;
}

export async function resolveBundledSbrProfileLocation(
  options: BundledResourcePathOptions,
): Promise<BundledSbrProfileLocation> {
  if (options.platform !== "darwin" || options.arch !== "arm64") {
    throw new Error("UNSUPPORTED_SBR_TARGET");
  }
  const resourcesRoot = path.resolve(
    options.isPackaged ? options.resourcesPath : options.developmentResourcesPath,
  );
  const sbrRoot = path.join(resourcesRoot, "sbr");
  const simulatorRoot = path.join(sbrRoot, "simulator");
  const profilePath = path.join(simulatorRoot, "sbr-profile-v1.json");
  const signaturePath = path.join(simulatorRoot, "sbr-profile-v1.sig");
  const [physicalResources, physicalSbr, physicalSimulator] = await Promise.all([
    requirePhysicalDirectory(resourcesRoot, "INVALID_SBR_PROFILE"),
    requirePhysicalDirectory(sbrRoot, "INVALID_SBR_PROFILE"),
    requirePhysicalDirectory(simulatorRoot, "INVALID_SBR_PROFILE"),
  ]);
  if (
    !isContained(physicalResources, physicalSbr) ||
    !isContained(physicalSbr, physicalSimulator)
  ) {
    throw new Error("INVALID_SBR_PROFILE");
  }
  for (const candidate of [profilePath, signaturePath]) {
    const before = await lstat(candidate).catch(() => null);
    const physical = await realpath(candidate).catch(() => null);
    const after = physical === null ? null : await lstat(physical).catch(() => null);
    if (
      !before?.isFile() ||
      before.isSymbolicLink() ||
      physical === null ||
      !after?.isFile() ||
      after.isSymbolicLink() ||
      before.dev !== after.dev ||
      before.ino !== after.ino ||
      !isContained(physicalSimulator, physical)
    ) {
      throw new Error("INVALID_SBR_PROFILE");
    }
  }
  return Object.freeze({
    profilePath: await realpath(profilePath),
    resourcesRoot: physicalResources,
  });
}
