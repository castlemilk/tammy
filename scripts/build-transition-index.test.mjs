import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

test("valid empty index", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-transitions-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  const { buildTransitionIndex } = await import("./build-transition-index.mjs");

  assert.deepEqual(await buildTransitionIndex({ root }), {
    schemaVersion: 1,
    transitions: {},
  });
});

test("sorts exact slice fixtures by fully-qualified enum and transition", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-transitions-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  for (const slice of ["proto", "sales"]) {
    await mkdir(path.join(root, "test/fixtures", slice), { recursive: true });
  }
  await writeFile(
    path.join(root, "test/fixtures/proto/transitions.pb.json"),
    JSON.stringify({
      transitions: [{ enum: "tammy.v1.PeriodState", transition: "OPEN->CLOSED" }],
    }),
  );
  await writeFile(
    path.join(root, "test/fixtures/sales/transitions.pb.json"),
    JSON.stringify({
      transitions: [{ enum: "tammy.v1.JobState", transition: "QUEUED->RUNNING" }],
    }),
  );
  const { buildTransitionIndex } = await import("./build-transition-index.mjs");

  const index = await buildTransitionIndex({ root });

  assert.deepEqual(Object.keys(index.transitions), [
    "tammy.v1.JobState.QUEUED->RUNNING",
    "tammy.v1.PeriodState.OPEN->CLOSED",
  ]);
});

test("write mode writes the canonical empty transition index", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-transitions-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  const { writeTransitionIndex } = await import("./build-transition-index.mjs");

  await writeTransitionIndex({ root });

  assert.equal(
    await readFile(path.join(root, "test/e2e/transitions.yaml"), "utf8"),
    "schemaVersion: 1\ntransitions: {}\n",
  );
});

test("check mode fails when the generated transition index drifts", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-transitions-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await mkdir(path.join(root, "test/e2e"), { recursive: true });
  await writeFile(
    path.join(root, "test/e2e/transitions.yaml"),
    "schemaVersion: 1\ntransitions:\n  stale: {}\n",
  );
  const { checkTransitionIndex } = await import("./build-transition-index.mjs");

  await assert.rejects(checkTransitionIndex({ root }), {
    message: "TRANSITION_INDEX_DRIFT",
  });
});

test("command dispatch accepts only write and check modes", async () => {
  const { transitionIndexMode } = await import("./build-transition-index.mjs");

  assert.equal(transitionIndexMode(["--write"]), "write");
  assert.equal(transitionIndexMode(["--check"]), "check");
  assert.throws(() => transitionIndexMode([]), {
    message: "TRANSITION_INDEX_MODE_REQUIRED",
  });
});
