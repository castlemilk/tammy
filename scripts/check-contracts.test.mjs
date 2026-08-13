import assert from "node:assert/strict";
import { access, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

test("contract checker removes ignored descriptor temp residue on failure", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-contracts-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  const calls = [];
  const { checkContracts } = await import("./check-contracts.mjs");

  await assert.rejects(
    checkContracts({
      buildDescriptorsFn: async () => {
        calls.push("descriptors");
        await mkdir(path.join(root, ".tmp/contracts"), { recursive: true });
        await writeFile(path.join(root, ".tmp/contracts/descriptors.pb"), "temp");
      },
      checkGeneratedTreeFn: async () => {
        calls.push("generated");
        throw new Error("GENERATED_TREE_DRIFT");
      },
      checkTransitionIndexFn: async () => calls.push("transitions"),
      root,
      runE2ECoverageFn: async () => calls.push("coverage"),
    }),
    { message: "GENERATED_TREE_DRIFT" },
  );
  assert.deepEqual(calls, ["descriptors", "generated"]);
  await assert.rejects(access(path.join(root, ".tmp/contracts")));
});

test("contract checker passes production mode through and still cleans up", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-contracts-production-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  const { checkContracts } = await import("./check-contracts.mjs");

  await assert.rejects(
    checkContracts({
      buildDescriptorsFn: async () => {
        await mkdir(path.join(root, ".tmp/contracts"), { recursive: true });
        await writeFile(path.join(root, ".tmp/contracts/descriptors.pb"), "temp");
      },
      checkGeneratedTreeFn: async () => {},
      checkTransitionIndexFn: async () => {},
      requireProduction: true,
      root,
      runE2ECoverageFn: async ({ requireProduction }) => {
        assert.equal(requireProduction, true);
        throw new Error("E2E_COVERAGE_FUTURE_PROMOTION_REQUIRED");
      },
    }),
    { message: "E2E_COVERAGE_FUTURE_PROMOTION_REQUIRED" },
  );
  await assert.rejects(access(path.join(root, ".tmp/contracts")));
});

test("contract checker parses only the production mode flag", async () => {
  const { contractsCliOptions } = await import("./check-contracts.mjs");

  assert.deepEqual(contractsCliOptions([]), { requireProduction: false });
  assert.deepEqual(contractsCliOptions(["--require-production"]), { requireProduction: true });
  assert.throws(() => contractsCliOptions(["--unknown"]), {
    message: "CONTRACTS_MODE_INVALID",
  });
});
