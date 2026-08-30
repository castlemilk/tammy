import { constants as fsConstants } from "node:fs";
import { lstat, mkdir, open, readdir, realpath } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  inspectReleaseRecordDurability,
  validateReleaseAttestation,
  validateReleaseLifecycleEvent,
} from "./macos-release-state.mjs";
import { validateBuildLedger } from "./reserve-macos-build.mjs";

const ATTESTATION_KINDS = new Set([
  "age-rating",
  "app-store-warning-review",
  "company-controller",
  "content-rights",
  "export-compliance",
  "metadata-assets-entered",
  "pricing-availability",
  "privacy-answer",
  "processed-build",
  "seller-eligibility",
]);
const PRE_UPLOAD_KINDS = [
  "company-controller",
  "seller-eligibility",
  "content-rights",
  "export-compliance",
  "pricing-availability",
  "privacy-answer",
];
const PRE_SUBMIT_KINDS = [
  "processed-build",
  "metadata-assets-entered",
  "age-rating",
  "app-store-warning-review",
];

function fail(code = "MACOS_APP_STORE_FACT_INVALID") {
  throw new Error(code);
}

async function stableFileBytes(file, maximumBytes = 1024 * 1024) {
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
      fail("MACOS_APP_STORE_FACT_INPUT_INVALID");
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
      fail("MACOS_APP_STORE_FACT_INPUT_INVALID");
    }
    return bytes;
  } catch (error) {
    if (error instanceof Error && /^MACOS_APP_STORE_FACT_/u.test(error.message)) throw error;
    fail("MACOS_APP_STORE_FACT_INPUT_INVALID");
  } finally {
    await handle?.close().catch(() => undefined);
  }
}

async function readJson(file, code = "MACOS_APP_STORE_FACT_INVALID") {
  try {
    return JSON.parse((await stableFileBytes(file)).toString("utf8"));
  } catch (error) {
    if (error instanceof Error && /^MACOS_APP_STORE_FACT_/u.test(error.message)) throw error;
    fail(code);
  }
}

async function optionalJsonRecords(directory) {
  let entries;
  try {
    const status = await lstat(directory);
    if (!status.isDirectory() || status.isSymbolicLink()) fail();
    entries = await readdir(directory, { withFileTypes: true });
  } catch (error) {
    if (error?.code === "ENOENT") return [];
    throw error;
  }
  const result = [];
  for (const entry of entries.sort((left, right) => left.name.localeCompare(right.name))) {
    if (!entry.isFile() || entry.isSymbolicLink() || !entry.name.endsWith(".json")) fail();
    result.push(await readJson(path.join(directory, entry.name)));
  }
  return result;
}

function relativeDestination(record) {
  const root = path.posix.join(
    "docs/release/records/macos",
    record.releaseVersion,
    `build-${record.buildNumber}`,
  );
  if (ATTESTATION_KINDS.has(record.kind)) {
    return path.posix.join(root, "attestations", `${record.kind}.json`);
  }
  return path.posix.join(
    root,
    "events",
    `${record.occurredAt.replaceAll(":", "-")}-${record.kind}.json`,
  );
}

async function requireReservation(repositoryRoot, record) {
  const ledger = validateBuildLedger(
    await readJson(path.join(repositoryRoot, "apps/desktop/release/macos/build-numbers.json")),
  );
  if (
    ledger.entries.filter(
      (entry) =>
        entry.marketingVersion === record.releaseVersion &&
        entry.buildNumber === record.buildNumber,
    ).length !== 1
  ) {
    fail("MACOS_APP_STORE_FACT_PREREQUISITE_MISSING");
  }
}

function candidateMatches(record, candidate) {
  return (
    candidate?.kind === "candidate-built" &&
    candidate.marketingVersion === record.releaseVersion &&
    candidate.buildNumber === record.buildNumber
  );
}

async function validatePrerequisites(repositoryRoot, record) {
  await requireReservation(repositoryRoot, record);
  const buildRoot = path.join(
    repositoryRoot,
    "docs/release/records/macos",
    record.releaseVersion,
    `build-${record.buildNumber}`,
  );
  const durability = await inspectReleaseRecordDurability({ buildRoot, repositoryRoot });
  const [attestations, eventRecords] = await Promise.all([
    optionalJsonRecords(path.join(buildRoot, "attestations")),
    optionalJsonRecords(path.join(buildRoot, "events")),
  ]);
  const candidate = eventRecords.find((event) => candidateMatches(record, event));
  if (record.kind === "company-controller" || record.kind === "seller-eligibility") return;
  if (ATTESTATION_KINDS.has(record.kind)) {
    if (!candidate) fail("MACOS_APP_STORE_FACT_PREREQUISITE_MISSING");
    if (!durability.candidate) {
      fail("MACOS_APP_STORE_FACT_PREREQUISITE_NOT_DURABLE");
    }
    if (record.kind === "export-compliance" && record.outcome === "non-exempt") {
      fail("MACOS_APP_STORE_FACT_SUPERSESSION_REQUIRED");
    }
    if (record.kind === "privacy-answer") {
      const candidateRoot = path.join(buildRoot, "evidence/candidate");
      const directories = await readdir(candidateRoot, { withFileTypes: true }).catch(() => []);
      const passing = directories.some((entry) => entry.isDirectory() && !entry.isSymbolicLink());
      if (!passing) fail("MACOS_APP_STORE_FACT_PREREQUISITE_MISSING");
    }
    return;
  }
  const priorEvents = eventRecords.filter((event) => event.kind !== "candidate-built");
  if (record.kind === "uploaded") {
    if (
      !candidate ||
      !PRE_UPLOAD_KINDS.every((kind) => attestations.some((item) => item.kind === kind)) ||
      record.packageSha256 !== candidate.packageSha256 ||
      record.productSourceCommit !== candidate.productSourceCommit ||
      record.productSourceTree !== candidate.productSourceTree
    ) {
      fail("MACOS_APP_STORE_FACT_PREREQUISITE_MISSING");
    }
    if (
      !durability.candidate ||
      !PRE_UPLOAD_KINDS.every((kind) => durability.attestationKinds.includes(kind))
    ) {
      fail("MACOS_APP_STORE_FACT_PREREQUISITE_NOT_DURABLE");
    }
  } else if (record.kind === "submitted") {
    if (
      !priorEvents.some((event) => event.kind === "uploaded") ||
      !PRE_SUBMIT_KINDS.every((kind) => attestations.some((item) => item.kind === kind))
    ) {
      fail("MACOS_APP_STORE_FACT_PREREQUISITE_MISSING");
    }
    if (
      !durability.eventKinds.includes("uploaded") ||
      !PRE_SUBMIT_KINDS.every((kind) => durability.attestationKinds.includes(kind))
    ) {
      fail("MACOS_APP_STORE_FACT_PREREQUISITE_NOT_DURABLE");
    }
  } else if (record.kind === "approved" || record.kind === "rejected") {
    if (!priorEvents.some((event) => event.kind === "submitted")) {
      fail("MACOS_APP_STORE_FACT_PREREQUISITE_MISSING");
    }
    if (!durability.eventKinds.includes("submitted")) {
      fail("MACOS_APP_STORE_FACT_PREREQUISITE_NOT_DURABLE");
    }
  } else {
    if (!candidate) fail("MACOS_APP_STORE_FACT_PREREQUISITE_MISSING");
    if (!durability.candidate) fail("MACOS_APP_STORE_FACT_PREREQUISITE_NOT_DURABLE");
  }
}

async function validateInputPath(repositoryRoot, input) {
  if (
    typeof input !== "string" ||
    !path.isAbsolute(input) ||
    path.normalize(input) !== input ||
    typeof repositoryRoot !== "string" ||
    !path.isAbsolute(repositoryRoot) ||
    path.normalize(repositoryRoot) !== repositoryRoot
  ) {
    fail("MACOS_APP_STORE_FACT_INPUT_INVALID");
  }
  const [resolvedRoot, resolvedInput] = await Promise.all([
    realpath(repositoryRoot).catch(() => fail("MACOS_APP_STORE_FACT_INPUT_INVALID")),
    realpath(input).catch(() => fail("MACOS_APP_STORE_FACT_INPUT_INVALID")),
  ]);
  if (resolvedInput === resolvedRoot || resolvedInput.startsWith(`${resolvedRoot}${path.sep}`)) {
    fail("MACOS_APP_STORE_FACT_INPUT_INVALID");
  }
}

async function writeExclusive(file, bytes) {
  const handle = await open(file, "wx", 0o600).catch((error) => {
    if (error?.code === "EEXIST") fail("MACOS_APP_STORE_FACT_EXISTS");
    throw error;
  });
  try {
    await handle.writeFile(bytes);
    await handle.sync();
  } finally {
    await handle.close();
  }
  const directory = await open(path.dirname(file), fsConstants.O_RDONLY);
  try {
    await directory.sync();
  } finally {
    await directory.close();
  }
}

export async function recordMacOSAppStoreFact({ check, input, repositoryRoot }) {
  if (typeof check !== "boolean") fail("MACOS_APP_STORE_FACT_INPUT_INVALID");
  await validateInputPath(repositoryRoot, input);
  const bytes = await stableFileBytes(input);
  let record;
  try {
    record = JSON.parse(bytes.toString("utf8"));
    if (ATTESTATION_KINDS.has(record?.kind)) validateReleaseAttestation(record);
    else {
      const buildRoot = path.join(
        repositoryRoot,
        "docs/release/records/macos",
        record.releaseVersion,
        `build-${record.buildNumber}`,
      );
      const priorEvents = (await optionalJsonRecords(path.join(buildRoot, "events"))).filter(
        (event) => event.kind !== "candidate-built",
      );
      validateReleaseLifecycleEvent(record, { priorEvents });
    }
  } catch (error) {
    if (error instanceof Error && /^MACOS_APP_STORE_FACT_/u.test(error.message)) throw error;
    fail("MACOS_APP_STORE_FACT_INVALID");
  }
  await validatePrerequisites(repositoryRoot, record);
  const destination = relativeDestination(record);
  const absoluteDestination = path.join(repositoryRoot, ...destination.split("/"));
  try {
    await lstat(absoluteDestination);
    fail("MACOS_APP_STORE_FACT_EXISTS");
  } catch (error) {
    if (error instanceof Error && error.message === "MACOS_APP_STORE_FACT_EXISTS") throw error;
    if (error?.code !== "ENOENT") fail();
  }
  if (check) return { destination, kind: record.kind, outcome: "validated" };
  await mkdir(path.dirname(absoluteDestination), { recursive: true, mode: 0o700 });
  await writeExclusive(absoluteDestination, bytes);
  return { destination, kind: record.kind, outcome: "recorded" };
}

async function main() {
  const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const arguments_ = process.argv.slice(2);
  const check = arguments_[0] === "--check";
  const inputIndex = arguments_.indexOf("--input");
  if (
    inputIndex < 0 ||
    inputIndex !== arguments_.length - 2 ||
    (check ? arguments_.length !== 3 : arguments_.length !== 2)
  ) {
    fail("MACOS_APP_STORE_FACT_ARGUMENT_INVALID");
  }
  const result = await recordMacOSAppStoreFact({
    check,
    input: arguments_[inputIndex + 1],
    repositoryRoot,
  });
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
