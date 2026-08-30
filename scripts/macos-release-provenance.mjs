import { execFile } from "node:child_process";
import { realpath } from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

import { validateBuildLedger } from "./reserve-macos-build.mjs";

const execFileAsync = promisify(execFile);
const SHA40 = /^[0-9a-f]{40}$/;
const SHA256 = /^[0-9a-f]{64}$/;
const VERSION =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?:(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;
const BUILD = /^[1-9][0-9]*$/;
const SOURCE_KEYS = ["ledger", "marketingVersion", "productSourceCommit", "productSourceTree"];
const EVENT_INPUT_KEYS = [
  "appSha256",
  "buildNumber",
  "marketingVersion",
  "packageSha256",
  "productSource",
  "unsignedContentManifestSha256",
];

function fail(code) {
  throw new Error(code);
}

function assertExactKeys(value, expected, code) {
  if (!value || typeof value !== "object" || Array.isArray(value)) fail(code);
  const actual = Object.keys(value).sort();
  const keys = [...expected].sort();
  if (actual.length !== keys.length || actual.some((key, index) => key !== keys[index])) fail(code);
}

async function git(root, args) {
  try {
    const environment = Object.fromEntries(
      Object.entries(process.env).filter(([key]) => !key.startsWith("GIT_")),
    );
    const { stdout } = await execFileAsync("git", args, {
      cwd: root,
      encoding: "utf8",
      env: environment,
      maxBuffer: 1_000_000,
    });
    return stdout;
  } catch {
    fail("MACOS_PRODUCT_SOURCE_INVALID");
  }
}

export async function readProductSource(root) {
  if (typeof root !== "string" || !path.isAbsolute(root)) fail("MACOS_PRODUCT_SOURCE_INVALID");
  const [resolvedRoot, topLevel] = await Promise.all([
    realpath(root).catch(() => fail("MACOS_PRODUCT_SOURCE_INVALID")),
    git(root, ["rev-parse", "--show-toplevel"]),
  ]);
  const resolvedTopLevel = await realpath(topLevel.trim()).catch(() =>
    fail("MACOS_PRODUCT_SOURCE_INVALID"),
  );
  if (resolvedTopLevel !== resolvedRoot) fail("MACOS_PRODUCT_SOURCE_INVALID");
  const statusBefore = await git(root, ["status", "--porcelain=v1", "--untracked-files=all"]);
  if (statusBefore !== "") fail("MACOS_PRODUCT_SOURCE_DIRTY");
  const productSourceCommit = (await git(root, ["rev-parse", "--verify", "HEAD"])).trim();
  const productSourceTree = (await git(root, ["rev-parse", "--verify", "HEAD^{tree}"])).trim();
  if (!SHA40.test(productSourceCommit) || !SHA40.test(productSourceTree)) {
    fail("MACOS_PRODUCT_SOURCE_INVALID");
  }

  let desktopPackage;
  let ledger;
  try {
    desktopPackage = JSON.parse(await git(root, ["show", "HEAD:apps/desktop/package.json"]));
    ledger = validateBuildLedger(
      JSON.parse(await git(root, ["show", "HEAD:apps/desktop/release/macos/build-numbers.json"])),
    );
  } catch {
    fail("MACOS_PRODUCT_SOURCE_INVALID");
  }
  if (!VERSION.test(desktopPackage?.version ?? "")) fail("MACOS_PRODUCT_SOURCE_INVALID");

  const statusAfter = await git(root, ["status", "--porcelain=v1", "--untracked-files=all"]);
  const commitAfter = (await git(root, ["rev-parse", "--verify", "HEAD"])).trim();
  if (statusAfter !== "" || commitAfter !== productSourceCommit) fail("MACOS_PRODUCT_SOURCE_DIRTY");
  return {
    productSourceCommit,
    productSourceTree,
    marketingVersion: desktopPackage.version,
    ledger,
  };
}

export function createCandidateBuiltEvent(input) {
  const code = "MACOS_CANDIDATE_PROVENANCE_INVALID";
  assertExactKeys(input, EVENT_INPUT_KEYS, code);
  assertExactKeys(input.productSource, SOURCE_KEYS, code);
  const {
    productSource,
    buildNumber,
    marketingVersion,
    unsignedContentManifestSha256,
    appSha256,
    packageSha256,
  } = input;
  validateBuildLedger(productSource.ledger);
  const reservation = productSource.ledger.entries.filter(
    (entry) => entry.buildNumber === buildNumber && entry.marketingVersion === marketingVersion,
  );
  if (
    reservation.length !== 1 ||
    productSource.marketingVersion !== marketingVersion ||
    !VERSION.test(marketingVersion) ||
    !BUILD.test(buildNumber) ||
    !SHA40.test(productSource.productSourceCommit) ||
    !SHA40.test(productSource.productSourceTree) ||
    ![unsignedContentManifestSha256, appSha256, packageSha256].every((hash) => SHA256.test(hash))
  ) {
    fail(code);
  }
  return {
    kind: "candidate-built",
    buildNumber,
    marketingVersion,
    productSourceCommit: productSource.productSourceCommit,
    productSourceTree: productSource.productSourceTree,
    unsignedContentManifestSha256,
    appSha256,
    packageSha256,
  };
}
