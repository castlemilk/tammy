import { randomUUID } from "node:crypto";
import { open, readFile, rename, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const LEDGER_KEYS = ["entries", "schemaVersion"];
const ENTRY_KEYS = ["buildNumber", "marketingVersion", "reservedAt", "reservedBy", "state"];
const VERSION = /^[0-9]+\.[0-9]+\.[0-9]+$/;
const BUILD = /^[1-9][0-9]*$/;
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
    if (current.entries.some((entry) => entry.buildNumber === number)) {
      fail("MACOS_BUILD_RESERVATION_CONFLICT");
    }
    const largest = current.entries.length === 0 ? 0n : BigInt(current.entries.at(-1).buildNumber);
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
  if (input.mode === "check") {
    const ledger = validateBuildLedger(JSON.parse(await readFile(defaultLedgerPath, "utf8")));
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
  });
  process.stdout.write(`${JSON.stringify(ledger.entries.at(-1))}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch((error) => {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  });
}
