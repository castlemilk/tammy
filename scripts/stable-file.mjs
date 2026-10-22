import { createHash } from "node:crypto";
import { constants, lstat, open } from "node:fs/promises";

const READ_ONLY_NO_FOLLOW = constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0);

function identity(stats) {
  return {
    ctimeNs: stats.ctimeNs,
    dev: stats.dev,
    ino: stats.ino,
    mode: stats.mode,
    mtimeNs: stats.mtimeNs,
    nlink: stats.nlink,
    size: stats.size,
  };
}

function sameIdentity(left, right) {
  return Object.keys(left).every((key) => left[key] === right[key]);
}

async function readExactly(handle, length) {
  const bytes = Buffer.alloc(length);
  let offset = 0;
  while (offset < length) {
    const { bytesRead } = await handle.read(bytes, offset, length - offset, offset);
    if (bytesRead === 0) throw new Error("STABLE_FILE_SHORT_READ");
    offset += bytesRead;
  }
  return bytes;
}

export async function withStableFileHandle(file, { afterRead, code, maxBytes }, reader) {
  let handle;
  try {
    if (typeof code !== "string" || !Number.isSafeInteger(maxBytes) || maxBytes < 0) {
      throw new Error(code);
    }
    const lexicalBefore = await lstat(file, { bigint: true });
    if (
      !lexicalBefore.isFile() ||
      lexicalBefore.isSymbolicLink() ||
      lexicalBefore.size > BigInt(maxBytes) ||
      lexicalBefore.size > BigInt(Number.MAX_SAFE_INTEGER)
    ) {
      throw new Error(code);
    }
    handle = await open(file, READ_ONLY_NO_FOLLOW);
    const openedBefore = await handle.stat({ bigint: true });
    const expected = identity(lexicalBefore);
    if (!openedBefore.isFile() || !sameIdentity(expected, identity(openedBefore))) {
      throw new Error(code);
    }
    const result = await reader(handle, Number(openedBefore.size));
    await afterRead?.();
    const [openedAfter, lexicalAfter] = await Promise.all([
      handle.stat({ bigint: true }),
      lstat(file, { bigint: true }),
    ]);
    if (
      !openedAfter.isFile() ||
      !lexicalAfter.isFile() ||
      lexicalAfter.isSymbolicLink() ||
      !sameIdentity(expected, identity(openedAfter)) ||
      !sameIdentity(expected, identity(lexicalAfter))
    ) {
      throw new Error(code);
    }
    return result;
  } catch {
    throw new Error(code);
  } finally {
    await handle?.close().catch(() => {});
  }
}

export function readStableFileBytes(file, options) {
  return withStableFileHandle(file, options, readExactly);
}

export function hashStableFile(file, options) {
  return withStableFileHandle(file, options, async (handle, size) => {
    const digest = createHash("sha256");
    const buffer = Buffer.alloc(Math.min(1024 * 1024, Math.max(size, 1)));
    let position = 0;
    while (position < size) {
      const length = Math.min(buffer.length, size - position);
      const { bytesRead } = await handle.read(buffer, 0, length, position);
      if (bytesRead === 0) throw new Error("STABLE_FILE_SHORT_READ");
      digest.update(buffer.subarray(0, bytesRead));
      position += bytesRead;
    }
    return digest.digest("hex");
  });
}
