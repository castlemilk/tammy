import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

test("the macOS application bundle has a valid signature", {
  skip: process.platform !== "darwin",
}, async () => {
  const desktopRoot = path.resolve(import.meta.dirname, "../..");
  const appBundle = path.join(
    desktopRoot,
    "out",
    `Tammy-${process.platform}-${process.arch}`,
    "Tammy.app",
  );

  await execFileAsync(
    "/usr/bin/codesign",
    ["--verify", "--deep", "--strict", "--verbose=2", appBundle],
    { encoding: "utf8", maxBuffer: 1024 * 1024, timeout: 10_000 },
  );
});

test("the ad-hoc macOS executable does not enable hardened runtime", {
  skip: process.platform !== "darwin",
}, async () => {
  const desktopRoot = path.resolve(import.meta.dirname, "../..");
  const appExecutable = path.join(
    desktopRoot,
    "out",
    `Tammy-${process.platform}-${process.arch}`,
    "Tammy.app",
    "Contents",
    "MacOS",
    "Tammy",
  );

  const { stderr } = await execFileAsync("/usr/bin/codesign", ["-dvvv", appExecutable], {
    encoding: "utf8",
    maxBuffer: 1024 * 1024,
    timeout: 10_000,
  });
  assert.match(stderr, /Signature=adhoc/);
  assert.doesNotMatch(stderr, /flags=.*\bruntime\b/);
});
