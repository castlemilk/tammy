import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("node:path", async (importOriginal) => {
  const actual = await importOriginal<typeof import("node:path")>();
  return {
    ...actual,
    default: actual.posix,
  };
});

import {
  type AuthenticatedCoreExecutable,
  authenticateCoreExecutable,
  LIVE_IDENTITY_TIMEOUT_MS,
  type LiveIdentityLsof,
  verifyMacOSPrimaryExecutableImage,
  verifySpawnedCoreExecutable,
} from "./core-executable";

const CORE_PATH = "/Applications/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core";
const AUTHORITY: AuthenticatedCoreExecutable = Object.freeze({
  executablePath: CORE_PATH,
  identity: Object.freeze({
    ctimeNs: 1n,
    dev: 2n,
    ino: 3n,
    mode: 0o100700n,
    mtimeNs: 4n,
    nlink: 1n,
    size: 5n,
  }),
  sha256: "a".repeat(64),
});

function image(device: string, inode: string, executablePath: string): string {
  return `ftxt\nD${device}\ni${inode}\nn${executablePath}\n`;
}

describe("macOS live core image verification", () => {
  it("accepts the authenticated primary image when later txt mappings are unrelated", () => {
    const output = `p12345\n${image("0x2", "3", CORE_PATH)}${image("0x9", "10", "/usr/lib/dyld")}`;

    expect(() => verifyMacOSPrimaryExecutableImage(output, 12_345, AUTHORITY)).not.toThrow();
  });

  it("rejects a foreign primary image even when the authenticated image is mapped later", () => {
    const output = `p12345\n${image("0x8", "9", "/tmp/foreign-core")}${image(
      "0x2",
      "3",
      CORE_PATH,
    )}`;

    expect(() => verifyMacOSPrimaryExecutableImage(output, 12_345, AUTHORITY)).toThrowError(
      "CORE_EXECUTABLE_AUTHENTICATION_FAILED",
    );
  });
});

const TIMEOUT = Object.assign(new Error("lsof timed out"), {
  code: null,
  killed: true,
  signal: "SIGKILL",
});
const SIGTERM_KILL = Object.assign(new Error("lsof terminated"), {
  code: null,
  killed: true,
  signal: "SIGTERM",
});
const SPAWN_ENOENT = Object.assign(new Error("spawn lsof ENOENT"), { code: "ENOENT" });
const NON_ZERO_EXIT = Object.assign(new Error("lsof failed"), { code: 1, killed: false });

// These tests authenticate a real temporary file, and this module mocks node:path as POSIX,
// so they run only on POSIX hosts. The live lsof identity check they cover is macOS-only.
describe.skipIf(process.platform === "win32")("asynchronous live identity verification", () => {
  let root: string | undefined;

  afterEach(async () => {
    if (root) await rm(root, { recursive: true, force: true });
    root = undefined;
  });

  async function authenticatedFixture(): Promise<{
    readonly authority: AuthenticatedCoreExecutable;
    readonly output: (pid: number) => string;
  }> {
    root = await mkdtemp(path.join(tmpdir(), "tammy-core-live-"));
    const executablePath = path.join(root, "tammy-core");
    await writeFile(executablePath, "authenticated-core", { mode: 0o700 });
    const authority = await authenticateCoreExecutable(executablePath);
    return {
      authority,
      output: (pid) =>
        `p${pid}\n${image(
          authority.identity.dev.toString(),
          authority.identity.ino.toString(),
          executablePath,
        )}`,
    };
  }

  it("uses a 3 second lsof timeout and accepts the matching image", async () => {
    const { authority, output } = await authenticatedFixture();
    const lsof = vi.fn<LiveIdentityLsof>().mockResolvedValueOnce(output(4242));

    await expect(
      verifySpawnedCoreExecutable(4242, authority, "darwin", lsof),
    ).resolves.toBeUndefined();
    expect(lsof).toHaveBeenCalledTimes(1);
    expect(LIVE_IDENTITY_TIMEOUT_MS).toBe(3_000);
  });

  it("retries exactly once after a timeout", async () => {
    const { authority, output } = await authenticatedFixture();
    const lsof = vi
      .fn<LiveIdentityLsof>()
      .mockRejectedValueOnce(TIMEOUT)
      .mockResolvedValueOnce(output(4242));

    await expect(
      verifySpawnedCoreExecutable(4242, authority, "darwin", lsof),
    ).resolves.toBeUndefined();
    expect(lsof).toHaveBeenCalledTimes(2);
  });

  it("retries exactly once after a spawn error", async () => {
    const { authority, output } = await authenticatedFixture();
    const lsof = vi
      .fn<LiveIdentityLsof>()
      .mockRejectedValueOnce(SPAWN_ENOENT)
      .mockResolvedValueOnce(output(4242));

    await expect(
      verifySpawnedCoreExecutable(4242, authority, "darwin", lsof),
    ).resolves.toBeUndefined();
    expect(lsof).toHaveBeenCalledTimes(2);
  });

  it("fails closed after the single retry also times out", async () => {
    const { authority } = await authenticatedFixture();
    const lsof = vi
      .fn<LiveIdentityLsof>()
      .mockRejectedValueOnce(TIMEOUT)
      .mockRejectedValueOnce(TIMEOUT)
      .mockResolvedValueOnce("unreachable");

    await expect(verifySpawnedCoreExecutable(4242, authority, "darwin", lsof)).rejects.toThrowError(
      "CORE_EXECUTABLE_AUTHENTICATION_FAILED",
    );
    expect(lsof).toHaveBeenCalledTimes(2);
  });

  it("never retries a SIGTERM termination that is not our timeout kill", async () => {
    const { authority } = await authenticatedFixture();
    const lsof = vi.fn<LiveIdentityLsof>().mockRejectedValueOnce(SIGTERM_KILL);

    await expect(verifySpawnedCoreExecutable(4242, authority, "darwin", lsof)).rejects.toThrowError(
      "CORE_EXECUTABLE_AUTHENTICATION_FAILED",
    );
    expect(lsof).toHaveBeenCalledTimes(1);
  });

  it("never retries a non-zero lsof exit", async () => {
    const { authority, output } = await authenticatedFixture();
    const lsof = vi
      .fn<LiveIdentityLsof>()
      .mockRejectedValueOnce(NON_ZERO_EXIT)
      .mockResolvedValueOnce(output(4242));

    await expect(verifySpawnedCoreExecutable(4242, authority, "darwin", lsof)).rejects.toThrowError(
      "CORE_EXECUTABLE_AUTHENTICATION_FAILED",
    );
    expect(lsof).toHaveBeenCalledTimes(1);
  });

  it("never retries an identity mismatch", async () => {
    const { authority } = await authenticatedFixture();
    const lsof = vi
      .fn<LiveIdentityLsof>()
      .mockResolvedValueOnce(`p4242\n${image("9", "9", "/tmp/replacement-core")}`)
      .mockResolvedValueOnce("unreachable");

    await expect(verifySpawnedCoreExecutable(4242, authority, "darwin", lsof)).rejects.toThrowError(
      "CORE_EXECUTABLE_AUTHENTICATION_FAILED",
    );
    expect(lsof).toHaveBeenCalledTimes(1);
  });

  it("does not invoke lsof on non-macOS platforms", async () => {
    const { authority } = await authenticatedFixture();
    const lsof = vi.fn<LiveIdentityLsof>();

    await expect(
      verifySpawnedCoreExecutable(4242, authority, "linux", lsof),
    ).resolves.toBeUndefined();
    expect(lsof).not.toHaveBeenCalled();
  });
});
