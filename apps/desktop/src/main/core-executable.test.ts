import { describe, expect, it } from "vitest";
import {
  type AuthenticatedCoreExecutable,
  verifyMacOSPrimaryExecutableImage,
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
