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

export async function resolveBundledCorePath(options: BundledCorePathOptions): Promise<string> {
  const executable = CORE_TARGETS[`${options.platform}/${options.arch}`];
  if (!executable) {
    throw new Error("UNSUPPORTED_CORE_TARGET");
  }
  const resourcesRoot = path.resolve(
    options.isPackaged ? options.resourcesPath : options.developmentResourcesPath,
  );
  const coreRoot = path.join(resourcesRoot, "core");
  const candidate = path.join(coreRoot, `${options.platform}-${options.arch}`, executable);
  if (!path.isAbsolute(candidate) || !isContained(coreRoot, candidate)) {
    throw new Error("INVALID_CORE_BINARY");
  }
  const stats = await lstat(candidate).catch(() => null);
  if (!stats?.isFile() || stats.isSymbolicLink()) {
    throw new Error("INVALID_CORE_BINARY");
  }
  const [physicalRoot, physicalCandidate] = await Promise.all([
    realpath(coreRoot).catch(() => null),
    realpath(candidate).catch(() => null),
  ]);
  if (
    physicalRoot === null ||
    physicalCandidate === null ||
    !isContained(physicalRoot, physicalCandidate)
  ) {
    throw new Error("INVALID_CORE_BINARY");
  }
  return candidate;
}
