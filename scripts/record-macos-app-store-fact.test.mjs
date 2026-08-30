import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { recordMacOSAppStoreFact } from "./record-macos-app-store-fact.mjs";

const version = "0.1.0";
const build = "42";
const sha = (digit) => digit.repeat(64);

function attestation(kind, outcome) {
  return {
    accountablePerson: "Ben Ebsworth",
    buildNumber: build,
    confirmedAt: "2026-08-31T05:00:00.000Z",
    evidenceReference: `apple/${kind}.png`,
    kind,
    outcome,
    releaseVersion: version,
    schemaVersion: 1,
  };
}

async function fixture(context) {
  const repositoryRoot = await mkdtemp(path.join(tmpdir(), "tammy-fact-repository-"));
  const inputRoot = await mkdtemp(path.join(tmpdir(), "tammy-fact-input-"));
  context.after(() =>
    Promise.all([
      rm(repositoryRoot, { force: true, recursive: true }),
      rm(inputRoot, { force: true, recursive: true }),
    ]),
  );
  await mkdir(path.join(repositoryRoot, "apps/desktop/release/macos"), { recursive: true });
  await writeFile(
    path.join(repositoryRoot, "apps/desktop/release/macos/build-numbers.json"),
    `${JSON.stringify({ schemaVersion: 1, entries: [{ buildNumber: build, marketingVersion: version, reservedAt: "2026-08-31T00:00:00.000Z", reservedBy: "Ben Ebsworth", state: "reserved" }] })}\n`,
  );
  return { inputRoot, repositoryRoot };
}

test("checks outside-repository operator input without creating a record", async (context) => {
  const { inputRoot, repositoryRoot } = await fixture(context);
  const input = path.join(inputRoot, "content-rights.json");
  await writeFile(input, `${JSON.stringify(attestation("company-controller", "confirmed"))}\n`);
  const result = await recordMacOSAppStoreFact({ check: true, input, repositoryRoot });
  assert.equal(result.outcome, "validated");
  await assert.rejects(readFile(path.join(repositoryRoot, result.destination)), /ENOENT/);

  const mutable = path.join(repositoryRoot, "fact.json");
  await writeFile(mutable, await readFile(input));
  await assert.rejects(
    recordMacOSAppStoreFact({ check: true, input: mutable, repositoryRoot }),
    /MACOS_APP_STORE_FACT_INPUT_INVALID/,
  );
});

test("records one fact exclusively and enforces lifecycle prerequisites", async (context) => {
  const { inputRoot, repositoryRoot } = await fixture(context);
  const input = path.join(inputRoot, "company-controller.json");
  await writeFile(input, `${JSON.stringify(attestation("company-controller", "confirmed"))}\n`);
  const recorded = await recordMacOSAppStoreFact({ check: false, input, repositoryRoot });
  assert.equal(recorded.outcome, "recorded");
  assert.deepEqual(
    JSON.parse(await readFile(path.join(repositoryRoot, recorded.destination), "utf8")),
    attestation("company-controller", "confirmed"),
  );
  await assert.rejects(
    recordMacOSAppStoreFact({ check: false, input, repositoryRoot }),
    /MACOS_APP_STORE_FACT_EXISTS/,
  );

  const uploaded = path.join(inputRoot, "uploaded.json");
  await writeFile(
    uploaded,
    `${JSON.stringify({ appStoreConnectBuildId: "1234567890", buildNumber: build, kind: "uploaded", occurredAt: "2026-08-31T06:00:00.000Z", operator: "Ben Ebsworth", packageSha256: sha("2"), productSourceCommit: "a".repeat(40), productSourceTree: "b".repeat(40), releaseVersion: version, schemaVersion: 1 })}\n`,
  );
  await assert.rejects(
    recordMacOSAppStoreFact({ check: true, input: uploaded, repositoryRoot }),
    /MACOS_APP_STORE_FACT_PREREQUISITE_MISSING/,
  );
});

test("rejects a fact whose local candidate prerequisite is not durable on origin", async (context) => {
  const { inputRoot, repositoryRoot } = await fixture(context);
  const events = path.join(repositoryRoot, "docs/release/records/macos/0.1.0/build-42/events");
  await mkdir(events, { recursive: true });
  await writeFile(
    path.join(events, "2026-08-31T04-00-00.000Z-candidate-built.json"),
    `${JSON.stringify({
      appSha256: sha("1"),
      buildNumber: build,
      kind: "candidate-built",
      marketingVersion: version,
      packageSha256: sha("2"),
      productSourceCommit: "a".repeat(40),
      productSourceTree: "b".repeat(40),
    })}\n`,
  );
  const input = path.join(inputRoot, "content-rights.json");
  await writeFile(input, `${JSON.stringify(attestation("content-rights", "owned"))}\n`);
  await assert.rejects(
    recordMacOSAppStoreFact({ check: true, input, repositoryRoot }),
    /MACOS_APP_STORE_FACT_PREREQUISITE_NOT_DURABLE/,
  );
});
