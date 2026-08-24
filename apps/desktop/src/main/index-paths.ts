import { lstat, realpath } from "node:fs/promises";
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
  return authenticateCoreExecutable(physicalCandidate);
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
