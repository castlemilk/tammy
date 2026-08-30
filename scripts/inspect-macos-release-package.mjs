import { execFile as nodeExecFile } from "node:child_process";
import { createHash } from "node:crypto";
import { constants as fsConstants } from "node:fs";
import { lstat, mkdtemp, open, readdir, realpath, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import plist from "plist";

import { hashAppBundle } from "./capture-app-store-screenshots.mjs";
import { inspectReleaseRecordDurability } from "./macos-release-state.mjs";

const execFile = promisify(nodeExecFile);
const SHA256 = /^[0-9a-f]{64}$/u;
const SHA40 = /^[0-9a-f]{40}$/u;
const VERSION = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u;
const PRIVACY_URL = "https://tammy-accounting.castlemilk.chatgpt.site/privacy";
const SUPPORT_URL = "https://tammy-accounting.castlemilk.chatgpt.site/support";
const HELPERS = [
  "com.tammy.desktop.helper",
  "com.tammy.desktop.helper.GPU",
  "com.tammy.desktop.helper.Plugin",
  "com.tammy.desktop.helper.Renderer",
];
const FACT_KEYS = [
  "appBundleIdentifier",
  "appSha256",
  "architectures",
  "buildNumber",
  "exportCompliance",
  "gatekeeper",
  "helperIdentifiers",
  "installerSignature",
  "marketingVersion",
  "minimumMacOSVersion",
  "privacyPolicy",
  "support",
];

function fail(code = "MACOS_RELEASE_PACKAGE_MISMATCH") {
  throw new Error(code);
}

function record(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function exactKeys(value, keys) {
  return (
    record(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}

async function stableFileBytes(file, maximumBytes = 2 * 1024 * 1024 * 1024) {
  let handle;
  try {
    handle = await open(file, fsConstants.O_RDONLY | fsConstants.O_NOFOLLOW);
    const before = await handle.stat({ bigint: true });
    if (
      !before.isFile() ||
      before.isSymbolicLink() ||
      before.nlink !== 1n ||
      before.size <= 0n ||
      before.size > BigInt(maximumBytes)
    ) {
      fail();
    }
    const bytes = await handle.readFile();
    const after = await handle.stat({ bigint: true });
    if (
      before.dev !== after.dev ||
      before.ino !== after.ino ||
      before.mode !== after.mode ||
      before.nlink !== after.nlink ||
      before.size !== after.size ||
      before.mtimeNs !== after.mtimeNs ||
      before.ctimeNs !== after.ctimeNs ||
      bytes.byteLength !== Number(before.size)
    ) {
      fail();
    }
    return bytes;
  } catch (error) {
    if (error instanceof Error && error.message === "MACOS_RELEASE_PACKAGE_MISMATCH") throw error;
    fail();
  } finally {
    await handle?.close().catch(() => undefined);
  }
}

async function readJson(file) {
  try {
    return JSON.parse((await stableFileBytes(file, 16 * 1024 * 1024)).toString("utf8"));
  } catch {
    fail();
  }
}

function validateCandidate(value, version, buildNumber) {
  if (
    !record(value) ||
    value.releaseVersion !== version ||
    value.buildNumber !== buildNumber ||
    !SHA40.test(value.sourceCommit) ||
    !SHA40.test(value.sourceTree) ||
    !SHA256.test(value.appSha256) ||
    !SHA256.test(value.packageSha256)
  ) {
    fail();
  }
  return value;
}

async function readDurableCandidate(recordDirectory) {
  const buildNumber = path.basename(recordDirectory).match(/^build-([1-9]\d*)$/u)?.[1];
  const version = path.basename(path.dirname(recordDirectory));
  if (!buildNumber || !VERSION.test(version)) fail();
  const status = await lstat(recordDirectory).catch(() => fail());
  if (
    !status.isDirectory() ||
    status.isSymbolicLink() ||
    (await realpath(recordDirectory).catch(() => fail())) !== recordDirectory
  ) {
    fail();
  }
  const candidateParent = path.join(recordDirectory, "evidence/candidate");
  const directories = await readdir(candidateParent, { withFileTypes: true }).catch(() => fail());
  const candidates = directories.filter((entry) => entry.isDirectory() && !entry.isSymbolicLink());
  if (candidates.length !== 1) fail();
  const candidate = validateCandidate(
    await readJson(path.join(candidateParent, candidates[0].name, "candidate.json")),
    version,
    buildNumber,
  );
  const eventsDirectory = path.join(recordDirectory, "events");
  const eventEntries = await readdir(eventsDirectory, { withFileTypes: true }).catch(() => fail());
  const matches = [];
  for (const entry of eventEntries) {
    if (
      !entry.isFile() ||
      entry.isSymbolicLink() ||
      !/^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3}Z-candidate-built\.json$/u.test(entry.name)
    ) {
      continue;
    }
    const event = await readJson(path.join(eventsDirectory, entry.name));
    if (
      exactKeys(event, [
        "appSha256",
        "buildNumber",
        "kind",
        "marketingVersion",
        "packageSha256",
        "productSourceCommit",
        "productSourceTree",
        "unsignedContentManifestSha256",
      ]) &&
      event.kind === "candidate-built" &&
      event.marketingVersion === version &&
      event.buildNumber === buildNumber &&
      event.appSha256 === candidate.appSha256 &&
      event.packageSha256 === candidate.packageSha256 &&
      event.productSourceCommit === candidate.sourceCommit &&
      event.productSourceTree === candidate.sourceTree &&
      SHA256.test(event.unsignedContentManifestSha256)
    ) {
      matches.push({ event, relativePath: `events/${entry.name}` });
    }
  }
  if (matches.length !== 1) fail();
  return { buildNumber, candidate, candidateEvent: matches[0].relativePath, version };
}

async function verifyCandidateDurability(recordDirectory) {
  const repositoryRoot = path.resolve(recordDirectory, "../../../../../..");
  const relative = path.relative(repositoryRoot, recordDirectory).split(path.sep).join("/");
  if (!/^docs\/release\/records\/macos\/[^/]+\/build-[^/]+$/u.test(relative)) fail();
  return (
    await inspectReleaseRecordDurability({
      buildRoot: recordDirectory,
      repositoryRoot,
    })
  ).candidate;
}

async function run(command, arguments_) {
  return execFile(command, arguments_, {
    encoding: "utf8",
    env: { HOME: process.env.HOME ?? "/var/empty", PATH: "/usr/bin:/bin" },
    killSignal: "SIGKILL",
    maxBuffer: 1024 * 1024,
    timeout: 30_000,
  }).catch(() => fail());
}

async function findExpandedApp(directory) {
  const matches = [];
  let count = 0;
  const visit = async (current, depth) => {
    if (depth > 12) fail();
    for (const entry of await readdir(current, { withFileTypes: true })) {
      count += 1;
      if (count > 10_000 || entry.isSymbolicLink()) fail();
      const target = path.join(current, entry.name);
      if (!entry.isDirectory()) continue;
      if (entry.name === "Tammy.app") matches.push(target);
      else await visit(target, depth + 1);
    }
  };
  await visit(directory, 0);
  if (matches.length !== 1) fail();
  return matches[0];
}

async function defaultInspectArchive(packagePath) {
  const temporary = await mkdtemp(path.join(tmpdir(), "tammy-package-inspection-"));
  try {
    const expanded = path.join(temporary, "expanded");
    const [signature, gatekeeper] = await Promise.all([
      run("/usr/sbin/pkgutil", ["--check-signature", packagePath]),
      run("/usr/sbin/spctl", ["--assess", "--type", "install", "-vv", packagePath]),
    ]);
    const signatureText = `${signature.stdout}${signature.stderr}`;
    const gatekeeperText = `${gatekeeper.stdout}${gatekeeper.stderr}`;
    if (
      !/signed by a certificate trusted by Mac OS X/iu.test(signatureText) ||
      !/(?:Mac Installer Distribution|3rd Party Mac Developer Installer)/iu.test(signatureText) ||
      !/accepted/iu.test(gatekeeperText)
    ) {
      fail();
    }
    await run("/usr/sbin/pkgutil", ["--expand-full", packagePath, expanded]);
    const app = await findExpandedApp(expanded);
    const info = plist.parse(
      (await stableFileBytes(path.join(app, "Contents/Info.plist"), 1024 * 1024)).toString("utf8"),
    );
    if (!record(info)) fail();
    const executable = path.join(app, "Contents/MacOS/Tammy");
    const architecture = await run("/usr/bin/lipo", ["-archs", executable]);
    const helperRoot = path.join(app, "Contents/Frameworks");
    const helperIdentifiers = [];
    for (const suffix of ["", " (GPU)", " (Plugin)", " (Renderer)"]) {
      const helperInfo = plist.parse(
        (
          await stableFileBytes(
            path.join(helperRoot, `Tammy Helper${suffix}.app/Contents/Info.plist`),
            1024 * 1024,
          )
        ).toString("utf8"),
      );
      helperIdentifiers.push(helperInfo.CFBundleIdentifier);
    }
    return {
      appBundleIdentifier: info.CFBundleIdentifier,
      appSha256: await hashAppBundle(app),
      architectures: architecture.stdout.trim().split(/\s+/u).sort(),
      buildNumber: info.CFBundleVersion,
      exportCompliance: info.ITSAppUsesNonExemptEncryption === false ? "exempt" : "non-exempt",
      gatekeeper: "accepted",
      helperIdentifiers,
      installerSignature: "valid",
      marketingVersion: info.CFBundleShortVersionString,
      minimumMacOSVersion: info.LSMinimumSystemVersion,
      privacyPolicy: info.TammyPrivacyPolicyURL,
      support: info.TammySupportURL,
    };
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
}

function validateInspection(facts, durable) {
  if (
    !exactKeys(facts, FACT_KEYS) ||
    facts.appBundleIdentifier !== "com.tammy.desktop" ||
    facts.appSha256 !== durable.candidate.appSha256 ||
    facts.buildNumber !== durable.buildNumber ||
    facts.marketingVersion !== durable.version ||
    !Array.isArray(facts.architectures) ||
    facts.architectures.length !== 1 ||
    facts.architectures[0] !== "arm64" ||
    facts.minimumMacOSVersion !== "14.0" ||
    facts.exportCompliance !== "exempt" ||
    facts.gatekeeper !== "accepted" ||
    facts.installerSignature !== "valid" ||
    facts.privacyPolicy !== PRIVACY_URL ||
    facts.support !== SUPPORT_URL ||
    !Array.isArray(facts.helperIdentifiers) ||
    facts.helperIdentifiers.some((value, index) => value !== HELPERS[index]) ||
    facts.helperIdentifiers.length !== HELPERS.length
  ) {
    fail();
  }
  return facts;
}

export async function inspectMacOSReleasePackage({
  packagePath,
  packageSha256,
  recordDirectory,
  inspectArchive = defaultInspectArchive,
  verifyDurability = verifyCandidateDurability,
}) {
  if (
    typeof packagePath !== "string" ||
    !path.isAbsolute(packagePath) ||
    path.normalize(packagePath) !== packagePath ||
    typeof recordDirectory !== "string" ||
    !path.isAbsolute(recordDirectory) ||
    path.normalize(recordDirectory) !== recordDirectory ||
    typeof inspectArchive !== "function" ||
    typeof verifyDurability !== "function"
  ) {
    fail();
  }
  const durable = await readDurableCandidate(recordDirectory);
  if (!(await verifyDurability(recordDirectory))) fail();
  if (path.basename(packagePath) !== `Tammy-${durable.version}-build.${durable.buildNumber}.pkg`) {
    fail();
  }
  const digest = createHash("sha256")
    .update(await stableFileBytes(packagePath))
    .digest("hex");
  if (
    digest !== durable.candidate.packageSha256 ||
    (packageSha256 !== undefined && packageSha256 !== digest)
  ) {
    fail();
  }
  const facts = validateInspection(await inspectArchive(packagePath, durable), durable);
  const digestAfter = createHash("sha256")
    .update(await stableFileBytes(packagePath))
    .digest("hex");
  if (digestAfter !== digest) fail();
  return {
    ...facts,
    candidateEvent: durable.candidateEvent,
    packageFilename: path.basename(packagePath),
    packageSha256: digest,
    productSourceCommit: durable.candidate.sourceCommit,
    productSourceTree: durable.candidate.sourceTree,
  };
}

async function main() {
  const arguments_ = process.argv.slice(2);
  if (arguments_.length !== 4 || arguments_[0] !== "--package" || arguments_[2] !== "--record") {
    fail("MACOS_RELEASE_PACKAGE_ARGUMENT_INVALID");
  }
  const result = await inspectMacOSReleasePackage({
    packagePath: arguments_[1],
    recordDirectory: arguments_[3],
  });
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
