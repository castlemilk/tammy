import { randomUUID } from "node:crypto";
import { open, readdir, readFile, rename, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { validateReleaseLifecycleEvent } from "./macos-release-state.mjs";

const LEDGER_KEYS = ["entries", "schemaVersion"];
const ENTRY_KEYS = ["buildNumber", "marketingVersion", "reservedAt", "reservedBy", "state"];
const VERSION =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?:(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;
const BUILD = /^[1-9][0-9]*$/;
const SHA40 = /^[0-9a-f]{40}$/;
const SHA256 = /^[0-9a-f]{64}$/;
const UTC_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;
const SECRET_VALUE_PATTERNS = [
  /\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}\b/i,
  /\b(?:ghp|github_pat)_[A-Za-z0-9_]{16,}\b/i,
  /\bAKIA[A-Z0-9]{16}\b/i,
  /-----BEGIN [A-Z ]*PRIVATE KEY-----/i,
  /\b(?:password|token|secret)=[^&\s]{8,}/i,
];
const SHA_FACT = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/i;
const defaultLedgerPath = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../apps/desktop/release/macos/build-numbers.json",
);
const defaultRecordsRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../docs/release/records/macos",
);
const LIFECYCLE_KINDS = new Set([
  "uploaded",
  "expired",
  "superseded",
  "submitted",
  "approved",
  "rejected",
]);
const CONSUMING_KINDS = new Set(["uploaded", "expired", "rejected", "superseded"]);
const CANDIDATE_BUILT_KEYS = [
  "appSha256",
  "buildNumber",
  "kind",
  "marketingVersion",
  "packageSha256",
  "productSourceCommit",
  "productSourceTree",
  "unsignedContentManifestSha256",
];

function fail(code) {
  throw new Error(code);
}

function assertExactKeys(value, expected) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    fail("MACOS_BUILD_LEDGER_INVALID");
  }
  const actual = Object.keys(value).sort();
  const keys = [...expected].sort();
  if (actual.length !== keys.length || actual.some((key, index) => key !== keys[index])) {
    fail("MACOS_BUILD_LEDGER_INVALID");
  }
}

function exactKeys(value, expected) {
  return (
    value !== null &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    Object.keys(value).length === expected.length &&
    expected.every((key) => Object.hasOwn(value, key))
  );
}

function isCandidateBuiltMarker(record, relativePath) {
  const parts = relativePath.split("/");
  const filename = parts.at(-1) ?? "";
  const timestampParts = filename.match(
    /^(\d{4}-\d{2}-\d{2}T)(\d{2})-(\d{2})-(\d{2}\.\d{3}Z)-candidate-built\.json$/,
  );
  const timestamp = timestampParts
    ? `${timestampParts[1]}${timestampParts[2]}:${timestampParts[3]}:${timestampParts[4]}`
    : "";
  return (
    exactKeys(record, CANDIDATE_BUILT_KEYS) &&
    record.kind === "candidate-built" &&
    VERSION.test(record.marketingVersion) &&
    BUILD.test(record.buildNumber) &&
    SHA40.test(record.productSourceCommit) &&
    SHA40.test(record.productSourceTree) &&
    [record.unsignedContentManifestSha256, record.appSha256, record.packageSha256].every((hash) =>
      SHA256.test(hash),
    ) &&
    timestampParts !== null &&
    isUtcTime(timestamp) &&
    relativePath === `${record.marketingVersion}/build-${record.buildNumber}/events/${filename}`
  );
}

function isUtcTime(value) {
  if (typeof value !== "string" || !UTC_TIME.test(value)) return false;
  try {
    return new Date(value).toISOString() === value;
  } catch {
    return false;
  }
}

function hasControlCharacters(value) {
  return [...value].some((character) => {
    const codePoint = character.codePointAt(0);
    return codePoint < 32 || codePoint === 127;
  });
}

function isOperator(value) {
  return (
    typeof value === "string" &&
    value.trim() === value &&
    value.length >= 2 &&
    value.length <= 100 &&
    !hasControlCharacters(value) &&
    !SECRET_VALUE_PATTERNS.some((pattern) => pattern.test(value)) &&
    !SHA_FACT.test(value)
  );
}

export function validateBuildLedger(ledger) {
  assertExactKeys(ledger, LEDGER_KEYS);
  if (ledger.schemaVersion !== 1 || !Array.isArray(ledger.entries)) {
    fail("MACOS_BUILD_LEDGER_INVALID");
  }
  let priorNumber = 0n;
  for (const entry of ledger.entries) {
    assertExactKeys(entry, ENTRY_KEYS);
    if (
      !BUILD.test(entry.buildNumber) ||
      !VERSION.test(entry.marketingVersion) ||
      !isUtcTime(entry.reservedAt) ||
      !isOperator(entry.reservedBy) ||
      entry.state !== "reserved"
    ) {
      fail("MACOS_BUILD_LEDGER_INVALID");
    }
    const number = BigInt(entry.buildNumber);
    if (number <= priorNumber) fail("MACOS_BUILD_LEDGER_INVALID");
    priorNumber = number;
  }
  return ledger;
}

export function validateConsumedBuildNumbers(ledger, events) {
  validateBuildLedger(ledger);
  if (!Array.isArray(events)) fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
  const reservations = new Map(
    ledger.entries.map((entry) => [`${entry.marketingVersion}\0${entry.buildNumber}`, entry]),
  );
  const lifecycleByBuild = new Map();
  const consumed = new Set();
  for (const record of events) {
    if (!exactKeys(record, ["event", "relativePath"]) || typeof record.relativePath !== "string") {
      fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
    }
    const event = record.event;
    if (event?.kind === "candidate-built") {
      const key = `${event.marketingVersion}\0${event.buildNumber}`;
      if (!isCandidateBuiltMarker(event, record.relativePath) || !reservations.has(key)) {
        fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
      }
      continue;
    }
    const key = `${event?.releaseVersion}\0${event?.buildNumber}`;
    const lifecycle = lifecycleByBuild.get(key) ?? {
      prior: [],
      uploaded: false,
      submitted: false,
      reviewed: false,
      terminal: false,
    };
    const expectedPath = `${event?.releaseVersion}/build-${event?.buildNumber}/events/${event?.occurredAt?.replaceAll(":", "-")}-${event?.kind}.json`;
    try {
      validateReleaseLifecycleEvent(event, { priorEvents: lifecycle.prior });
    } catch {
      fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
    }
    if (
      record.relativePath !== expectedPath ||
      !reservations.has(key) ||
      (lifecycle.prior.at(-1)?.occurredAt ?? "") >= event.occurredAt
    ) {
      fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
    }
    if (event.kind === "uploaded") {
      if (lifecycle.uploaded || lifecycle.submitted || lifecycle.reviewed || lifecycle.terminal) {
        fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
      }
      lifecycle.uploaded = true;
    } else if (event.kind === "submitted") {
      if (!lifecycle.uploaded || lifecycle.submitted || lifecycle.reviewed || lifecycle.terminal) {
        fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
      }
      lifecycle.submitted = true;
    } else if (event.kind === "approved" || event.kind === "rejected") {
      if (!lifecycle.submitted || lifecycle.reviewed || lifecycle.terminal) {
        fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
      }
      lifecycle.reviewed = true;
      lifecycle.terminal = true;
    } else if (event.kind === "expired" || event.kind === "superseded") {
      if (lifecycle.submitted || lifecycle.reviewed || lifecycle.terminal) {
        fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
      }
      lifecycle.terminal = true;
    }
    lifecycle.prior.push(event);
    lifecycleByBuild.set(key, lifecycle);
    if (CONSUMING_KINDS.has(event.kind)) consumed.add(event.buildNumber);
  }
  return [...consumed].sort((left, right) => (BigInt(left) < BigInt(right) ? -1 : 1));
}

export async function readMacOSLifecycleEvents(recordsRoot = defaultRecordsRoot) {
  if (typeof recordsRoot !== "string" || !path.isAbsolute(recordsRoot)) {
    fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
  }
  const events = [];
  async function visit(directory) {
    let entries;
    try {
      entries = await readdir(directory, { withFileTypes: true });
    } catch (error) {
      if (error?.code === "ENOENT") return;
      fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
    }
    for (const entry of entries.sort((left, right) => left.name.localeCompare(right.name))) {
      const entryPath = path.join(directory, entry.name);
      if (entry.isSymbolicLink()) fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
      if (entry.isDirectory()) await visit(entryPath);
      else if (
        entry.isFile() &&
        entry.name.endsWith(".json") &&
        !entry.name.endsWith(".example.json")
      ) {
        let record;
        try {
          record = JSON.parse(await readFile(entryPath, "utf8"));
        } catch {
          fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
        }
        const relativePath = path.relative(recordsRoot, entryPath).split(path.sep).join("/");
        const isEventPath = relativePath.split("/").includes("events");
        const isLifecycle = LIFECYCLE_KINDS.has(record?.kind);
        const isCandidateBuilt = record?.kind === "candidate-built";
        if (isEventPath || isLifecycle || isCandidateBuilt) {
          if (isCandidateBuilt) {
            if (!isCandidateBuiltMarker(record, relativePath)) {
              fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
            }
            events.push({ event: record, relativePath });
            continue;
          }
          if (!isLifecycle) fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
          const expectedPath = `${record.releaseVersion}/build-${record.buildNumber}/events/${record.occurredAt?.replaceAll(":", "-")}-${record.kind}.json`;
          if (relativePath !== expectedPath) fail("MACOS_BUILD_EVENT_LEDGER_MISMATCH");
          events.push({ event: record, relativePath });
        }
      }
    }
  }
  await visit(recordsRoot);
  return events;
}

export function parseReservationArguments(argv) {
  if (argv.length === 1 && argv[0] === "--check") return { mode: "check" };
  if (
    argv.length !== 6 ||
    argv[0] !== "--version" ||
    argv[2] !== "--operator" ||
    argv[4] !== "--number"
  ) {
    fail("MACOS_BUILD_RESERVATION_INPUT_INVALID");
  }
  const [, version, , operator, , number] = argv;
  if (!VERSION.test(version) || !isOperator(operator) || !BUILD.test(number)) {
    fail("MACOS_BUILD_RESERVATION_INPUT_INVALID");
  }
  return { mode: "reserve", version, operator, number };
}

async function syncDirectory(directory) {
  const handle = await open(directory, "r");
  try {
    await handle.sync();
  } finally {
    await handle.close();
  }
}

export async function reserveMacOSBuild({
  ledgerPath = defaultLedgerPath,
  version,
  operator,
  number,
  reservedAt = new Date().toISOString(),
  consumedEvents = [],
}) {
  if (
    typeof ledgerPath !== "string" ||
    !path.isAbsolute(ledgerPath) ||
    !VERSION.test(version ?? "") ||
    !isOperator(operator) ||
    !BUILD.test(number ?? "") ||
    !isUtcTime(reservedAt)
  ) {
    fail("MACOS_BUILD_RESERVATION_INPUT_INVALID");
  }

  const lockPath = `${ledgerPath}.lock`;
  let lock;
  try {
    lock = await open(lockPath, "wx", 0o600);
  } catch (error) {
    if (error?.code === "EEXIST") fail("MACOS_BUILD_RESERVATION_CONFLICT");
    throw error;
  }

  const temporaryPath = `${ledgerPath}.tmp-${randomUUID()}`;
  try {
    const current = validateBuildLedger(JSON.parse(await readFile(ledgerPath, "utf8")));
    const consumedBuildNumbers = validateConsumedBuildNumbers(current, consumedEvents);
    if (current.entries.some((entry) => entry.buildNumber === number)) {
      fail("MACOS_BUILD_RESERVATION_CONFLICT");
    }
    const largestReserved =
      current.entries.length === 0 ? 0n : BigInt(current.entries.at(-1).buildNumber);
    const largestConsumed =
      consumedBuildNumbers.length === 0 ? 0n : BigInt(consumedBuildNumbers.at(-1));
    const largest = largestReserved > largestConsumed ? largestReserved : largestConsumed;
    if (BigInt(number) <= largest) fail("MACOS_BUILD_NUMBER_NOT_MONOTONIC");

    const next = validateBuildLedger({
      schemaVersion: 1,
      entries: [
        ...current.entries,
        {
          buildNumber: number,
          marketingVersion: version,
          reservedAt,
          reservedBy: operator,
          state: "reserved",
        },
      ],
    });
    const temporary = await open(temporaryPath, "wx", 0o644);
    try {
      await temporary.writeFile(`${JSON.stringify(next, null, 2)}\n`, "utf8");
      await temporary.sync();
    } finally {
      await temporary.close();
    }
    await rename(temporaryPath, ledgerPath);
    await syncDirectory(path.dirname(ledgerPath));
    return next;
  } finally {
    await rm(temporaryPath, { force: true }).catch(() => {});
    await lock.close().catch(() => {});
    await rm(lockPath, { force: true });
  }
}

async function main(argv) {
  const input = parseReservationArguments(argv);
  const consumedEvents = await readMacOSLifecycleEvents();
  if (input.mode === "check") {
    const ledger = validateBuildLedger(JSON.parse(await readFile(defaultLedgerPath, "utf8")));
    validateConsumedBuildNumbers(ledger, consumedEvents);
    process.stdout.write(
      `${JSON.stringify({ status: "valid", entries: ledger.entries.length })}\n`,
    );
    return;
  }
  const ledger = await reserveMacOSBuild({
    ledgerPath: defaultLedgerPath,
    version: input.version,
    operator: input.operator,
    number: input.number,
    consumedEvents,
  });
  process.stdout.write(`${JSON.stringify(ledger.entries.at(-1))}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch((error) => {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  });
}
