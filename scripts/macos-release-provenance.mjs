import { execFile } from "node:child_process";
import { lstat, readdir, readFile, realpath } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
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
const SCREENSHOT_RECORD =
  /^apps\/desktop\/release\/macos\/screenshots\/en-AU\/(?:manifest\.json|0[1-5]\.png|0[1-5]\.accessibility\.txt)$/u;
const ATTESTATION_RECORD =
  /^attestations\/(?:company-controller|seller-eligibility|content-rights|export-compliance|pricing-availability|privacy-answer|age-rating|processed-build|metadata-assets-entered|app-store-warning-review)\.json$/u;
const EVENT_RECORD =
  /^events\/\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3}Z-(?:candidate-built|uploaded|expired|superseded|submitted|approved|rejected)\.json$/u;
const CANDIDATE_RECORD =
  /^evidence\/candidate\/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\/(?:candidate\.json|metadata-snapshot\.json|privacy-evidence\.json|runtime-egress\.json|screenshots\.json|summary\.md)$/u;

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

function allowedFrozenChange(relativePath, marketingVersion, buildNumber) {
  if (SCREENSHOT_RECORD.test(relativePath)) return true;
  const prefix = `docs/release/records/macos/${marketingVersion}/build-${buildNumber}/`;
  if (!relativePath.startsWith(prefix)) return false;
  const recordPath = relativePath.slice(prefix.length);
  return (
    ATTESTATION_RECORD.test(recordPath) ||
    EVENT_RECORD.test(recordPath) ||
    CANDIDATE_RECORD.test(recordPath)
  );
}

async function readFrozenCandidate(root, marketingVersion, buildNumber) {
  const candidateRoot = path.join(
    root,
    "docs/release/records/macos",
    marketingVersion,
    `build-${buildNumber}`,
    "evidence/candidate",
  );
  const entries = await readdir(candidateRoot, { withFileTypes: true }).catch(() =>
    fail("MACOS_FROZEN_SOURCE_INVALID"),
  );
  if (entries.length !== 1 || !entries[0].isDirectory() || entries[0].isSymbolicLink()) {
    fail("MACOS_FROZEN_SOURCE_INVALID");
  }
  const candidatePath = path.join(candidateRoot, entries[0].name, "candidate.json");
  const status = await lstat(candidatePath).catch(() => fail("MACOS_FROZEN_SOURCE_INVALID"));
  if (!status.isFile() || status.isSymbolicLink()) fail("MACOS_FROZEN_SOURCE_INVALID");
  let candidate;
  try {
    candidate = JSON.parse(await readFile(candidatePath, "utf8"));
  } catch {
    fail("MACOS_FROZEN_SOURCE_INVALID");
  }
  if (
    candidate.releaseVersion !== marketingVersion ||
    candidate.buildNumber !== buildNumber ||
    !SHA40.test(candidate.sourceCommit) ||
    !SHA40.test(candidate.sourceTree)
  ) {
    fail("MACOS_FROZEN_SOURCE_INVALID");
  }
  return candidate;
}

export async function verifyFrozenProductSource(root, { marketingVersion, buildNumber }) {
  if (
    typeof root !== "string" ||
    !path.isAbsolute(root) ||
    !VERSION.test(marketingVersion ?? "") ||
    !BUILD.test(buildNumber ?? "")
  ) {
    fail("MACOS_FROZEN_SOURCE_INVALID");
  }
  const [resolvedRoot, topLevel] = await Promise.all([
    realpath(root).catch(() => fail("MACOS_FROZEN_SOURCE_INVALID")),
    git(root, ["rev-parse", "--show-toplevel"]),
  ]);
  const resolvedTopLevel = await realpath(topLevel.trim()).catch(() =>
    fail("MACOS_FROZEN_SOURCE_INVALID"),
  );
  if (resolvedRoot !== resolvedTopLevel) fail("MACOS_FROZEN_SOURCE_INVALID");
  if ((await git(root, ["status", "--porcelain=v1", "--untracked-files=all"])) !== "") {
    fail("MACOS_FROZEN_SOURCE_DIRTY");
  }
  const candidate = await readFrozenCandidate(root, marketingVersion, buildNumber);
  const recordedTree = (
    await git(root, ["rev-parse", "--verify", `${candidate.sourceCommit}^{tree}`])
  ).trim();
  if (recordedTree !== candidate.sourceTree) fail("MACOS_FROZEN_SOURCE_INVALID");
  const changed = (
    await git(root, [
      "diff",
      "--name-only",
      "--no-renames",
      "-z",
      `${candidate.sourceCommit}..HEAD`,
    ])
  )
    .split("\0")
    .filter(Boolean);
  if (
    changed.length === 0 ||
    changed.some((file) => !allowedFrozenChange(file, marketingVersion, buildNumber))
  ) {
    fail("MACOS_FROZEN_SOURCE_CHANGED");
  }
  const statusAfter = await git(root, ["status", "--porcelain=v1", "--untracked-files=all"]);
  if (statusAfter !== "") fail("MACOS_FROZEN_SOURCE_DIRTY");
  return {
    buildNumber,
    changedFiles: changed.sort(),
    marketingVersion,
    productSourceCommit: candidate.sourceCommit,
    productSourceTree: candidate.sourceTree,
    status: "frozen",
  };
}

async function main() {
  const arguments_ = process.argv.slice(2);
  const versionIndex = arguments_.indexOf("--version");
  const buildIndex = arguments_.indexOf("--build");
  if (
    arguments_[0] !== "--verify-frozen" ||
    arguments_.length !== 5 ||
    versionIndex < 0 ||
    buildIndex < 0 ||
    versionIndex === arguments_.length - 1 ||
    buildIndex === arguments_.length - 1
  ) {
    fail("MACOS_FROZEN_SOURCE_ARGUMENT_INVALID");
  }
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const result = await verifyFrozenProductSource(root, {
    buildNumber: arguments_[buildIndex + 1],
    marketingVersion: arguments_[versionIndex + 1],
  });
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
