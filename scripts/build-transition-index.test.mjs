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

test("sorts reserved reporting and tax fixtures with other slices by fully-qualified enum and transition", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-transitions-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  for (const slice of ["proto", "reporting", "tax"]) {
    await mkdir(path.join(root, "test/fixtures", slice), { recursive: true });
  }
  await writeFile(
    path.join(root, "test/fixtures/proto/transitions.pb.json"),
    JSON.stringify({
      transitions: [{ enum: "tammy.v1.PeriodState", transition: "OPEN->CLOSED" }],
    }),
  );
  await writeFile(
    path.join(root, "test/fixtures/reporting/transitions.pb.json"),
    JSON.stringify({
      transitions: [{ enum: "tammy.v1.FinancialCloseState", transition: "REVIEW_READY->FROZEN" }],
    }),
  );
  await writeFile(
    path.join(root, "test/fixtures/tax/transitions.pb.json"),
    JSON.stringify({
      transitions: [{ enum: "tammy.v1.CompanyReturnState", transition: "DECLARED->PRELODGE_PENDING" }],
    }),
  );
  const { buildTransitionIndex } = await import("./build-transition-index.mjs");

  const index = await buildTransitionIndex({ root });

  assert.deepEqual(Object.keys(index.transitions), [
    "tammy.v1.CompanyReturnState.DECLARED->PRELODGE_PENDING",
    "tammy.v1.FinancialCloseState.REVIEW_READY->FROZEN",
    "tammy.v1.PeriodState.OPEN->CLOSED",
  ]);
});

test("repository index includes the reserved reporting and tax lifecycle fixtures", async () => {
  const { buildTransitionIndex } = await import("./build-transition-index.mjs");

  const index = await buildTransitionIndex();

  assert.ok(
    Object.hasOwn(
      index.transitions,
      "tammy.v1.FinancialCloseState.FINANCIAL_CLOSE_STATE_REVIEW_READY->FINANCIAL_CLOSE_STATE_FROZEN",
    ),
  );
  assert.ok(
    Object.hasOwn(
      index.transitions,
      "tammy.v1.CompanyReturnState.COMPANY_RETURN_STATE_DECLARED->COMPANY_RETURN_STATE_PRELODGE_PENDING",
    ),
  );
  assert.ok(
    Object.hasOwn(
      index.transitions,
      "tammy.v1.CompanyReturnAttemptState.COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED->COMPANY_RETURN_ATTEMPT_STATE_COMMITTED",
    ),
  );
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
