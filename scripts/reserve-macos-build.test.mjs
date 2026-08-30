import assert from "node:assert/strict";
import { mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  parseReservationArguments,
  reserveMacOSBuild,
  validateBuildLedger,
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
    ["--version", "0.1.0", "--operator", `ghp_${"a".repeat(20)}`, "--number", "1"],
    ["--version", "0.1.0", "--operator", "a".repeat(40), "--number", "1"],
  ]) {
    assert.throws(() => parseReservationArguments(argv), /MACOS_BUILD_RESERVATION_INPUT_INVALID/);
  }
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
