import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  closeSync,
  constants as fsConstants,
  fstatSync,
  lstatSync,
  openSync,
  readSync,
} from "node:fs";
import { lstat, open } from "node:fs/promises";
import path from "node:path";

const MAX_CORE_BYTES = 512 * 1024 * 1024;
const READ_BUFFER_BYTES = 1024 * 1024;
const READ_ONLY_NO_FOLLOW = fsConstants.O_RDONLY | (fsConstants.O_NOFOLLOW ?? 0);
const LIVE_IDENTITY_OUTPUT_BYTES = 65_536;
const LIVE_IDENTITY_TIMEOUT_MS = 2_000;

export interface CoreExecutableIdentity {
  readonly ctimeNs: bigint;
  readonly dev: bigint;
  readonly ino: bigint;
  readonly mode: bigint;
  readonly mtimeNs: bigint;
  readonly nlink: bigint;
  readonly size: bigint;
}

export interface AuthenticatedCoreExecutable {
  readonly executablePath: string;
  readonly identity: CoreExecutableIdentity;
  readonly sha256: string;
}

function identity(stats: {
  readonly ctimeNs: bigint;
  readonly dev: bigint;
  readonly ino: bigint;
  readonly mode: bigint;
  readonly mtimeNs: bigint;
  readonly nlink: bigint;
  readonly size: bigint;
}): CoreExecutableIdentity {
  return Object.freeze({
    ctimeNs: stats.ctimeNs,
    dev: stats.dev,
    ino: stats.ino,
    mode: stats.mode,
    mtimeNs: stats.mtimeNs,
    nlink: stats.nlink,
    size: stats.size,
  });
}

function sameIdentity(left: CoreExecutableIdentity, right: CoreExecutableIdentity): boolean {
  return (
    left.ctimeNs === right.ctimeNs &&
    left.dev === right.dev &&
    left.ino === right.ino &&
    left.mode === right.mode &&
    left.mtimeNs === right.mtimeNs &&
    left.nlink === right.nlink &&
    left.size === right.size
  );
}

function requireRegularIdentity(stats: {
  isFile(): boolean;
  isSymbolicLink(): boolean;
  readonly nlink: bigint;
  readonly size: bigint;
}): void {
  if (
    !stats.isFile() ||
    stats.isSymbolicLink() ||
    stats.nlink !== 1n ||
    stats.size <= 0n ||
    stats.size > BigInt(MAX_CORE_BYTES)
  ) {
    throw new Error("CORE_EXECUTABLE_AUTHENTICATION_FAILED");
  }
}

function requireAuthority(authority: AuthenticatedCoreExecutable): void {
  if (
    authority === null ||
    typeof authority !== "object" ||
    !path.isAbsolute(authority.executablePath) ||
    path.normalize(authority.executablePath) !== authority.executablePath ||
    !/^[0-9a-f]{64}$/.test(authority.sha256) ||
    authority.identity === null ||
    typeof authority.identity !== "object"
  ) {
    throw new Error("CORE_EXECUTABLE_AUTHENTICATION_FAILED");
  }
  for (const value of Object.values(authority.identity)) {
    if (typeof value !== "bigint") throw new Error("CORE_EXECUTABLE_AUTHENTICATION_FAILED");
  }
}

export async function authenticateCoreExecutable(
  executablePath: string,
): Promise<AuthenticatedCoreExecutable> {
  if (!path.isAbsolute(executablePath) || path.normalize(executablePath) !== executablePath) {
    throw new Error("CORE_EXECUTABLE_AUTHENTICATION_FAILED");
  }
  let handle: Awaited<ReturnType<typeof open>> | undefined;
  try {
    const lexicalBefore = await lstat(executablePath, { bigint: true });
    requireRegularIdentity(lexicalBefore);
    handle = await open(executablePath, READ_ONLY_NO_FOLLOW);
    const openedBefore = await handle.stat({ bigint: true });
    requireRegularIdentity(openedBefore);
    const expectedIdentity = identity(lexicalBefore);
    if (!sameIdentity(expectedIdentity, identity(openedBefore))) throw new Error();

    const digest = createHash("sha256");
    const buffer = Buffer.alloc(Math.min(READ_BUFFER_BYTES, Number(openedBefore.size)));
    let position = 0;
    while (position < Number(openedBefore.size)) {
      const { bytesRead } = await handle.read(
        buffer,
        0,
        Math.min(buffer.length, Number(openedBefore.size) - position),
        position,
      );
      if (bytesRead === 0) throw new Error();
      digest.update(buffer.subarray(0, bytesRead));
      position += bytesRead;
    }
    const [openedAfter, lexicalAfter] = await Promise.all([
      handle.stat({ bigint: true }),
      lstat(executablePath, { bigint: true }),
    ]);
    requireRegularIdentity(openedAfter);
    requireRegularIdentity(lexicalAfter);
    if (
      !sameIdentity(expectedIdentity, identity(openedAfter)) ||
      !sameIdentity(expectedIdentity, identity(lexicalAfter))
    ) {
      throw new Error();
    }
    return Object.freeze({
      executablePath,
      identity: expectedIdentity,
      sha256: digest.digest("hex"),
    });
  } catch {
    throw new Error("CORE_EXECUTABLE_AUTHENTICATION_FAILED");
  } finally {
    await handle?.close().catch(() => undefined);
  }
}

export function revalidateCoreExecutableForSpawn(authority: AuthenticatedCoreExecutable): void {
  requireAuthority(authority);
  let descriptor: number | undefined;
  try {
    const lexicalBefore = lstatSync(authority.executablePath, { bigint: true });
    requireRegularIdentity(lexicalBefore);
    if (!sameIdentity(authority.identity, identity(lexicalBefore))) throw new Error();
    descriptor = openSync(authority.executablePath, READ_ONLY_NO_FOLLOW);
    const openedBefore = fstatSync(descriptor, { bigint: true });
    requireRegularIdentity(openedBefore);
    if (!sameIdentity(authority.identity, identity(openedBefore))) throw new Error();

    const digest = createHash("sha256");
    const buffer = Buffer.alloc(Math.min(READ_BUFFER_BYTES, Number(openedBefore.size)));
    let position = 0;
    while (position < Number(openedBefore.size)) {
      const bytesRead = readSync(
        descriptor,
        buffer,
        0,
        Math.min(buffer.length, Number(openedBefore.size) - position),
        position,
      );
      if (bytesRead === 0) throw new Error();
      digest.update(buffer.subarray(0, bytesRead));
      position += bytesRead;
    }
    const openedAfter = fstatSync(descriptor, { bigint: true });
    const lexicalAfter = lstatSync(authority.executablePath, { bigint: true });
    requireRegularIdentity(openedAfter);
    requireRegularIdentity(lexicalAfter);
    if (
      !sameIdentity(authority.identity, identity(openedAfter)) ||
      !sameIdentity(authority.identity, identity(lexicalAfter)) ||
      digest.digest("hex") !== authority.sha256
    ) {
      throw new Error();
    }
  } catch {
    throw new Error("CORE_EXECUTABLE_AUTHENTICATION_FAILED");
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
  }
}

export function verifySpawnedCoreExecutable(
  processId: number | undefined,
  authority: AuthenticatedCoreExecutable,
  platform = process.platform,
): void {
  requireAuthority(authority);
  if (!Number.isSafeInteger(processId) || (processId ?? 0) <= 0) {
    throw new Error("CORE_EXECUTABLE_AUTHENTICATION_FAILED");
  }
  try {
    if (platform === "darwin") {
      const output = execFileSync(
        "/usr/sbin/lsof",
        ["-nP", "-a", "-p", String(processId), "-d", "txt", "-FDin"],
        {
          encoding: "utf8",
          env: { LANG: "C" },
          maxBuffer: LIVE_IDENTITY_OUTPUT_BYTES,
          shell: false,
          timeout: LIVE_IDENTITY_TIMEOUT_MS,
          windowsHide: true,
        },
      );
      const lines = output.split(/\r?\n/u).filter((line) => line.length > 0);
      if (lines[0] !== `p${processId}`) throw new Error();
      let matches = 0;
      for (let index = 1; index < lines.length; index += 4) {
        if (lines[index] !== "ftxt") throw new Error();
        const fields = lines.slice(index + 1, index + 4);
        const device = fields.find((field) => field.startsWith("D"));
        const inode = fields.find((field) => field.startsWith("i"));
        const name = fields.find((field) => field.startsWith("n"));
        if (
          fields.length !== 3 ||
          device === undefined ||
          inode === undefined ||
          name === undefined ||
          !/^D(?:0x[0-9a-f]+|[0-9]+)$/iu.test(device) ||
          !/^i[1-9][0-9]*$/u.test(inode) ||
          !path.posix.isAbsolute(name.slice(1))
        ) {
          throw new Error();
        }
        if (
          name.slice(1) === authority.executablePath &&
          BigInt(device.slice(1)) === authority.identity.dev &&
          BigInt(inode.slice(1)) === authority.identity.ino
        ) {
          matches += 1;
        }
      }
      if (matches !== 1) throw new Error();
    }
    revalidateCoreExecutableForSpawn(authority);
  } catch {
    throw new Error("CORE_EXECUTABLE_AUTHENTICATION_FAILED");
  }
}
