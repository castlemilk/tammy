import { lstat, realpath } from "node:fs/promises";
import path from "node:path";

interface BundledCorePathOptions {
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

async function requirePhysicalDirectory(candidate: string): Promise<string> {
  const before = await lstat(candidate).catch(() => null);
  if (!before?.isDirectory() || before.isSymbolicLink()) {
    throw new Error("INVALID_CORE_BINARY");
  }
  const physical = await realpath(candidate).catch(() => {
    throw new Error("INVALID_CORE_BINARY");
  });
  const after = await lstat(candidate).catch(() => null);
  if (
    !after?.isDirectory() ||
    after.isSymbolicLink() ||
    before.dev !== after.dev ||
    before.ino !== after.ino
  ) {
    throw new Error("INVALID_CORE_BINARY");
  }
  return physical;
}

export async function resolveBundledCorePath(options: BundledCorePathOptions): Promise<string> {
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
    requirePhysicalDirectory(resourcesRoot),
    requirePhysicalDirectory(coreRoot),
    requirePhysicalDirectory(targetRoot),
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
  return physicalCandidate;
}
