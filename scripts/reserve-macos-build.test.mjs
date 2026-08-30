import assert from "node:assert/strict";
import { mkdir, mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  parseReservationArguments,
  readMacOSLifecycleEvents,
  reserveMacOSBuild,
  validateBuildLedger,
  validateConsumedBuildNumbers,
} from "./reserve-macos-build.mjs";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function ledger(entries = []) {
  return { schemaVersion: 1, entries };
}

function entry(buildNumber, overrides = {}) {
  return {
    buildNumber,
    marketingVersion: "0.1.0",
    reservedAt: "2026-08-30T00:00:00.000Z",
    reservedBy: "Ben Ebsworth",
    state: "reserved",
    ...overrides,
  };
}

test("validates the strict monotonic build-number ledger", () => {
  const valid = ledger([
    entry("1"),
    entry("2", { marketingVersion: "0.1.1", reservedAt: "2026-08-30T00:01:00.000Z" }),
  ]);
  assert.deepEqual(validateBuildLedger(valid), valid);

  for (const invalid of [
    { ...valid, extra: true },
    ledger([{ ...entry("1"), extra: true }]),
    ledger([entry("0")]),
    ledger([entry("01")]),
    ledger([entry("1", { marketingVersion: "01.2.3" })]),
    ledger([entry("2"), entry("1", { reservedAt: "2026-08-30T00:01:00.000Z" })]),
    ledger([entry("1"), entry("1", { marketingVersion: "0.2.0" })]),
    ledger([entry("1", { reservedAt: "not-a-time" })]),
    ledger([entry("1", { reservedBy: "" })]),
    ledger([entry("1", { reservedBy: `ghp_${"a".repeat(20)}` })]),
    ledger([entry("1", { reservedBy: "AKIAABCDEFGHIJKLMNOP" })]),
    ledger([entry("1", { reservedBy: "-----BEGIN PRIVATE KEY-----" })]),
    ledger([entry("1", { reservedBy: "a".repeat(40) })]),
    ledger([entry("1", { reservedBy: "b".repeat(64) })]),
    ledger([entry("1", { state: "uploaded" })]),
    ledger([{ ...entry("1"), apiToken: "redacted" }]),
  ]) {
    assert.throws(() => validateBuildLedger(invalid), /MACOS_BUILD_LEDGER_INVALID/);
  }
  assert.deepEqual(
    validateBuildLedger(ledger([entry("1", { marketingVersion: "1.2.3-rc.1+build.5" })])),
    ledger([entry("1", { marketingVersion: "1.2.3-rc.1+build.5" })]),
  );
});

test("parses only the explicit reservation or read-only check CLI", () => {
  assert.deepEqual(
    parseReservationArguments([
      "--version",
      "0.1.0",
      "--operator",
      "Ben Ebsworth",
      "--number",
      "1",
    ]),
    { mode: "reserve", version: "0.1.0", operator: "Ben Ebsworth", number: "1" },
  );
  assert.deepEqual(parseReservationArguments(["--check"]), { mode: "check" });
  for (const argv of [
    [],
    ["--version", "0.1.0", "--operator", "Ben Ebsworth"],
    ["--number", "1", "--version", "0.1.0", "--operator", "Ben Ebsworth"],
    ["--version", "0.1.0", "--operator", "Ben Ebsworth", "--number", "1", "--latest-from-apple"],
    ["--check", "--number", "1"],
    ["--version", "01.2.3", "--operator", "Ben Ebsworth", "--number", "1"],
    ["--version", "0.1.0", "--operator", `ghp_${"a".repeat(20)}`, "--number", "1"],
    ["--version", "0.1.0", "--operator", "a".repeat(40), "--number", "1"],
  ]) {
    assert.throws(() => parseReservationArguments(argv), /MACOS_BUILD_RESERVATION_INPUT_INVALID/);
  }
});

test("derives consumed build numbers from immutable lifecycle events and binds them to reservations", async (context) => {
  const directory = await mkdtemp(
    path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-build-events-"),
  );
  context.after(() => rm(directory, { recursive: true, force: true }));
  const eventsDirectory = path.join(directory, "0.1.0", "build-1", "events");
  await mkdir(eventsDirectory, { recursive: true });
  const uploaded = {
    schemaVersion: 1,
    kind: "uploaded",
    releaseVersion: "0.1.0",
    buildNumber: "1",
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T01:00:00.000Z",
    productSourceCommit: "a".repeat(40),
    productSourceTree: "b".repeat(40),
    packageSha256: "c".repeat(64),
    appStoreConnectBuildId: "1234567890",
  };
  await writeFile(
    path.join(eventsDirectory, "2026-08-30T01-00-00.000Z-uploaded.json"),
    `${JSON.stringify(uploaded, null, 2)}\n`,
  );
  const events = await readMacOSLifecycleEvents(directory);
  assert.deepEqual(events, [
    {
      relativePath: "0.1.0/build-1/events/2026-08-30T01-00-00.000Z-uploaded.json",
      event: uploaded,
    },
  ]);
  assert.deepEqual(validateConsumedBuildNumbers(ledger([entry("1")]), events), ["1"]);
  assert.throws(
    () => validateConsumedBuildNumbers(ledger(), events),
    /MACOS_BUILD_EVENT_LEDGER_MISMATCH/,
  );

  const expired = {
    schemaVersion: 1,
    kind: "expired",
    releaseVersion: "0.1.0",
    buildNumber: "1",
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T00:30:00.000Z",
    reason: "candidate-timeout",
  };
  assert.deepEqual(
    validateConsumedBuildNumbers(ledger([entry("1")]), [
      {
        relativePath: "0.1.0/build-1/events/2026-08-30T00-30-00.000Z-expired.json",
        event: expired,
      },
    ]),
    ["1"],
  );

  const submitted = {
    schemaVersion: 1,
    kind: "submitted",
    releaseVersion: "0.1.0",
    buildNumber: "1",
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T02:00:00.000Z",
    appStoreSubmissionReference: "apple/submission.json",
  };
  const rejected = {
    schemaVersion: 1,
    kind: "rejected",
    releaseVersion: "0.1.0",
    buildNumber: "1",
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T03:00:00.000Z",
    reviewReference: "apple/review.json",
    submittedEventPath: "events/2026-08-30T02-00-00.000Z-submitted.json",
  };
  assert.throws(
    () =>
      validateConsumedBuildNumbers(ledger([entry("1")]), [
        {
          relativePath: "0.1.0/build-1/events/2026-08-30T02-00-00.000Z-submitted.json",
          event: submitted,
        },
        {
          relativePath: "0.1.0/build-1/events/2026-08-30T03-00-00.000Z-rejected.json",
          event: rejected,
        },
      ]),
    /MACOS_BUILD_EVENT_LEDGER_MISMATCH/,
  );
});

test("rejects misplaced, misnamed, and unknown lifecycle event files", async (context) => {
  const uploaded = {
    schemaVersion: 1,
    kind: "uploaded",
    releaseVersion: "0.1.0",
    buildNumber: "1",
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T01:00:00.000Z",
    productSourceCommit: "a".repeat(40),
    productSourceTree: "b".repeat(40),
    packageSha256: "c".repeat(64),
    appStoreConnectBuildId: "1234567890",
  };
  for (const [relativePath, record] of [
    ["0.1.0/build-1/uploaded.json", uploaded],
    ["0.1.0/build-1/events/wrong-uploaded.json", uploaded],
    [
      "0.1.0/build-1/events/2026-08-30T01-00-00.000Z-uploded.json",
      { ...uploaded, kind: "uploded" },
    ],
  ]) {
    const directory = await mkdtemp(
      path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-invalid-build-events-"),
    );
    context.after(() => rm(directory, { recursive: true, force: true }));
    const file = path.join(directory, relativePath);
    await mkdir(path.dirname(file), { recursive: true });
    await writeFile(file, `${JSON.stringify(record, null, 2)}\n`);
    await assert.rejects(readMacOSLifecycleEvents(directory), /MACOS_BUILD_EVENT_LEDGER_MISMATCH/);
  }
});

test("recognizes an exact candidate-built marker without treating it as consumption", async (context) => {
  const directory = await mkdtemp(
    path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-candidate-build-event-"),
  );
  context.after(() => rm(directory, { recursive: true, force: true }));
  const candidateBuilt = {
    kind: "candidate-built",
    buildNumber: "1",
    marketingVersion: "0.1.0",
    productSourceCommit: "a".repeat(40),
    productSourceTree: "b".repeat(40),
    unsignedContentManifestSha256: "c".repeat(64),
    appSha256: "d".repeat(64),
    packageSha256: "e".repeat(64),
  };
  const file = path.join(
    directory,
    "0.1.0/build-1/events/2026-08-30T04-00-00.000Z-candidate-built.json",
  );
  await mkdir(path.dirname(file), { recursive: true });
  await writeFile(file, `${JSON.stringify(candidateBuilt, null, 2)}\n`);
  const records = await readMacOSLifecycleEvents(directory);
  assert.deepEqual(records, [
    {
      relativePath: "0.1.0/build-1/events/2026-08-30T04-00-00.000Z-candidate-built.json",
      event: candidateBuilt,
    },
  ]);
  assert.deepEqual(validateConsumedBuildNumbers(ledger([entry("1")]), records), []);
  assert.throws(
    () => validateConsumedBuildNumbers(ledger(), records),
    /MACOS_BUILD_EVENT_LEDGER_MISMATCH/,
  );
  await writeFile(file, `${JSON.stringify({ ...candidateBuilt, extra: true }, null, 2)}\n`);
  await assert.rejects(readMacOSLifecycleEvents(directory), /MACOS_BUILD_EVENT_LEDGER_MISMATCH/);

  const misplacedDirectory = await mkdtemp(
    path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-misplaced-candidate-event-"),
  );
  context.after(() => rm(misplacedDirectory, { recursive: true, force: true }));
  const misplacedFile = path.join(misplacedDirectory, "0.1.0/build-1/candidate-built.json");
  await mkdir(path.dirname(misplacedFile), { recursive: true });
  await writeFile(misplacedFile, `${JSON.stringify(candidateBuilt, null, 2)}\n`);
  await assert.rejects(
    readMacOSLifecycleEvents(misplacedDirectory),
    /MACOS_BUILD_EVENT_LEDGER_MISMATCH/,
  );
});

test("atomically reserves a greater build number without phase-two facts", async (context) => {
  const directory = await mkdtemp(
    path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-build-ledger-"),
  );
  context.after(() => rm(directory, { recursive: true, force: true }));
  const ledgerPath = path.join(directory, "build-numbers.json");
  await writeFile(ledgerPath, `${JSON.stringify(ledger([entry("1")]), null, 2)}\n`);

  const result = await reserveMacOSBuild({
    ledgerPath,
    version: "0.1.0",
    operator: "Ben Ebsworth",
    number: "3",
    reservedAt: "2026-08-30T00:02:00.000Z",
  });
  assert.deepEqual(result.entries.at(-1), entry("3", { reservedAt: "2026-08-30T00:02:00.000Z" }));
  assert.deepEqual(JSON.parse(await readFile(ledgerPath, "utf8")), result);
  assert.deepEqual(await readdir(directory), ["build-numbers.json"]);
  assert.equal(JSON.stringify(result).includes("Commit"), false);
  assert.equal(JSON.stringify(result).includes("Sha256"), false);
  assert.equal(JSON.stringify(result).includes("a".repeat(40)), false);
  assert.equal(JSON.stringify(result).includes("b".repeat(64)), false);

  const before = await readFile(ledgerPath, "utf8");
  await assert.rejects(
    reserveMacOSBuild({
      ledgerPath,
      version: "0.2.0",
      operator: "Ben Ebsworth",
      number: "2",
      reservedAt: "2026-08-30T00:03:00.000Z",
    }),
    /MACOS_BUILD_NUMBER_NOT_MONOTONIC/,
  );
  assert.equal(await readFile(ledgerPath, "utf8"), before);
});

test("parallel reservation attempts never duplicate or corrupt the ledger", async (context) => {
  const directory = await mkdtemp(
    path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-build-ledger-race-"),
  );
  context.after(() => rm(directory, { recursive: true, force: true }));
  const ledgerPath = path.join(directory, "build-numbers.json");
  await writeFile(ledgerPath, `${JSON.stringify(ledger(), null, 2)}\n`);
  const request = {
    ledgerPath,
    version: "0.1.0",
    operator: "Ben Ebsworth",
    number: "1",
    reservedAt: "2026-08-30T00:00:00.000Z",
  };

  const results = await Promise.allSettled([
    reserveMacOSBuild(request),
    reserveMacOSBuild(request),
  ]);
  assert.equal(results.filter(({ status }) => status === "fulfilled").length, 1);
  assert.equal(results.filter(({ status }) => status === "rejected").length, 1);
  assert.match(results.find(({ status }) => status === "rejected").reason.message, /CONFLICT/);
  assert.deepEqual(validateBuildLedger(JSON.parse(await readFile(ledgerPath, "utf8"))).entries, [
    entry("1"),
  ]);
  assert.deepEqual(await readdir(directory), ["build-numbers.json"]);
});

test("the committed empty ledger and package scripts expose no implicit reservation", async () => {
  const committedLedger = JSON.parse(
    await readFile(
      path.join(repositoryRoot, "apps/desktop/release/macos/build-numbers.json"),
      "utf8",
    ),
  );
  assert.deepEqual(validateBuildLedger(committedLedger), ledger());
  const rootPackage = JSON.parse(await readFile(path.join(repositoryRoot, "package.json"), "utf8"));
  assert.equal(rootPackage.scripts["macos:build:reserve"], "node scripts/reserve-macos-build.mjs");
  assert.equal(
    rootPackage.scripts["macos:build:check"],
    "node scripts/reserve-macos-build.mjs --check",
  );
});
