import { constants } from "node:fs";
import { lstat, open } from "node:fs/promises";
import { isAbsolute } from "node:path";

export interface LocalApiConnection {
  readonly capability: string;
  readonly origin: string;
  readonly version: "v1";
}

const CONNECTION_KEYS = new Set(["capability", "origin", "version"]);
const CONNECTION_FILE_MAX_BYTES = 4 * 1024;

export function validateLocalApiConnection(value: unknown): LocalApiConnection {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("INVALID_CONNECTION");
  }
  const prototype = Object.getPrototypeOf(value);
  const record = value as Record<string, unknown>;
  const keys = Reflect.ownKeys(record);
  if (
    (prototype !== Object.prototype && prototype !== null) ||
    keys.length !== 3 ||
    keys.some((key) => typeof key !== "string" || !CONNECTION_KEYS.has(key)) ||
    record.version !== "v1" ||
    typeof record.origin !== "string" ||
    typeof record.capability !== "string"
  )
    throw new Error("INVALID_CONNECTION");

  const authority = /^http:\/\/127\.0\.0\.1:([1-9]\d{3,4})$/.exec(record.origin);
  const port = authority === null ? 0 : Number(authority[1]);
  if (port < 1024 || port > 65535 || !/^[A-Za-z0-9_-]{43}$/.test(record.capability)) {
    throw new Error("INVALID_CONNECTION");
  }
  const capabilityBytes = Buffer.from(record.capability, "base64url");
  if (
    capabilityBytes.length !== 32 ||
    capabilityBytes.toString("base64url") !== record.capability
  ) {
    throw new Error("INVALID_CONNECTION");
  }
  return Object.freeze({ capability: record.capability, origin: record.origin, version: "v1" });
}

export async function readLocalApiConnectionFile(path: string): Promise<LocalApiConnection> {
  try {
    if (!isAbsolute(path)) throw new Error();
    const before = await lstat(path);
    if (
      !before.isFile() ||
      (before.mode & 0o7777) !== 0o600 ||
      before.size > CONNECTION_FILE_MAX_BYTES
    ) {
      throw new Error();
    }
    const file = await open(
      path,
      constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0) | (constants.O_NONBLOCK ?? 0),
    );
    try {
      const opened = await file.stat();
      if (
        !opened.isFile() ||
        (opened.mode & 0o7777) !== 0o600 ||
        opened.size > CONNECTION_FILE_MAX_BYTES ||
        opened.dev !== before.dev ||
        opened.ino !== before.ino
      )
        throw new Error();
      const bytes = Buffer.alloc(CONNECTION_FILE_MAX_BYTES + 1);
      let length = 0;
      while (length < bytes.length) {
        const { bytesRead } = await file.read(bytes, length, bytes.length - length, null);
        if (bytesRead === 0) break;
        length += bytesRead;
      }
      const after = await file.stat();
      if (
        length > CONNECTION_FILE_MAX_BYTES ||
        after.size !== length ||
        (after.mode & 0o7777) !== 0o600
      ) {
        throw new Error();
      }
      return validateLocalApiConnection(JSON.parse(bytes.subarray(0, length).toString("utf8")));
    } finally {
      await file.close();
    }
  } catch {
    // File paths, capabilities, parser text and OS errors must not reach MCP logs.
    throw new Error("INVALID_CONNECTION_FILE");
  }
}
