import { randomBytes } from "node:crypto";
import { lstat, mkdir, open, readFile, unlink } from "node:fs/promises";
import path from "node:path";

export const SBR_BUILD_LOCK_ENV = "TAMMY_SBR_BUILD_LOCK_TOKEN";
const LOCK_DIRECTORY = ".tmp/sbr-build-owner";

function lockLocation(root) {
  if (typeof root !== "string" || !path.isAbsolute(root) || path.normalize(root) !== root) {
    throw new Error("SBR_BUILD_LOCK_INVALID");
  }
  return {
    directory: path.join(root, LOCK_DIRECTORY),
    file: path.join(root, LOCK_DIRECTORY, "owner.lock"),
  };
}

export async function acquireSbrBuildLock(root) {
  const location = lockLocation(root);
  await mkdir(location.directory, { recursive: true });
  const directoryStats = await lstat(location.directory).catch(() => null);
  if (!directoryStats?.isDirectory() || directoryStats.isSymbolicLink()) {
    throw new Error("SBR_BUILD_LOCK_INVALID");
  }
  let handle;
  try {
    handle = await open(location.file, "wx", 0o600);
  } catch (error) {
    if (error?.code === "EEXIST") throw new Error("SBR_BUILD_LOCKED");
    throw new Error("SBR_BUILD_LOCK_INVALID");
  }
  const token = randomBytes(32).toString("hex");
  try {
    await handle.writeFile(`${token}\n`);
    const owned = await handle.stat({ bigint: true });
    return Object.freeze({
      path: location.file,
      token,
      async release() {
        await handle.close().catch(() => {});
        const current = await lstat(location.file, { bigint: true }).catch(() => null);
        const contents = current?.isFile()
          ? await readFile(location.file, "utf8").catch(() => null)
          : null;
        if (current?.dev === owned.dev && current.ino === owned.ino && contents === `${token}\n`) {
          await unlink(location.file).catch(() => {});
        }
      },
    });
  } catch {
    await handle.close().catch(() => {});
    await unlink(location.file).catch(() => {});
    throw new Error("SBR_BUILD_LOCK_INVALID");
  }
}

export async function enterSbrBuildOwnership(root, environment = process.env) {
  const inherited = environment?.[SBR_BUILD_LOCK_ENV];
  if (inherited === undefined) return acquireSbrBuildLock(root);
  if (typeof inherited !== "string" || !/^[0-9a-f]{64}$/.test(inherited)) {
    throw new Error("SBR_BUILD_LOCK_INVALID");
  }
  const location = lockLocation(root);
  const stats = await lstat(location.file).catch(() => null);
  const contents =
    stats?.isFile() && !stats.isSymbolicLink()
      ? await readFile(location.file, "utf8").catch(() => null)
      : null;
  if (contents !== `${inherited}\n`) throw new Error("SBR_BUILD_LOCK_INVALID");
  return Object.freeze({
    inherited: true,
    path: location.file,
    token: inherited,
    async release() {},
  });
}
