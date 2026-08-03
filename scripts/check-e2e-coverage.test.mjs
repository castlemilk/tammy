import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { create, toBinary } from "@bufbuild/protobuf";
import { FileDescriptorSetSchema } from "@bufbuild/protobuf/wkt";
import { stringify } from "yaml";

const RPC = "tammy.v1.WorkspaceService.GetWorkspaceState";
const SYSTEM_RPC = "tammy.v1.SystemService.GetDiagnostics";
const REVIEWED_EXCEPTION = "not_applicable_pre_workspace_system_query";
const ROLES = ["workspace_admin", "business_preparer", "business_lodger", "auditor"];

function validInput() {
  return {
    coverage: {
      schemaVersion: 1,
      scenarios: {
        "E2E-00": { cases: ["foundation/offline-ready"] },
      },
      rpcs: {
        [RPC]: {
          preload: "getWorkspaceState",
          cases: ["foundation/offline-ready"],
          projections: ["workspace_state"],
          roles: Object.fromEntries(ROLES.map((role) => [role, "allowed"])),
        },
      },
      transitions: {},
    },
    descriptorRpcs: [RPC],
    preloadMethods: ["getWorkspaceState"],
    transitionIds: [],
  };
}

function systemQueryInput() {
  const input = validInput();
  delete input.coverage.rpcs[RPC];
  input.coverage.rpcs[SYSTEM_RPC] = {
    preload: "getSystemDiagnostics",
    cases: ["foundation/offline-ready"],
    projections: ["system_diagnostics"],
    roles: REVIEWED_EXCEPTION,
    list: REVIEWED_EXCEPTION,
    idempotency: REVIEWED_EXCEPTION,
  };
  input.descriptorRpcs = [SYSTEM_RPC];
  input.preloadMethods = ["getSystemDiagnostics"];
  return input;
}

test("checker module exists", async () => {
  const checker = await import("./check-e2e-coverage.mjs");

  assert.equal(typeof checker.checkE2ECoverage, "function");
});

test("missing descriptor RPC", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.rpcs = {};

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_RPC_MISSING",
  });
});

test("unknown coverage RPC", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.rpcs["tammy.v1.UnknownService.Unknown"] = input.coverage.rpcs[RPC];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_RPC_UNKNOWN",
  });
});

test("missing transition", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.transitionIds = ["tammy.v1.JobState.QUEUED->RUNNING"];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_TRANSITION_MISSING",
  });
});

test("unknown transition", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.transitions["tammy.v1.JobState.QUEUED->RUNNING"] = {
    cases: ["foundation/offline-ready"],
  };

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_TRANSITION_UNKNOWN",
  });
});

test("missing role outcome", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  delete input.coverage.rpcs[RPC].roles.auditor;

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_ROLE_MISSING",
  });
});

test("extra role outcome", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.rpcs[RPC].roles.superuser = "allowed";

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_ROLE_EXTRA",
  });
});

test("missing production preload", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.preloadMethods = [];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_PRELOAD_MISSING",
  });
});

test("missing scenario case", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.scenarios["E2E-00"].cases = [];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_CASE_MISSING",
  });
});

test("malformed coverage manifest", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage = { schemaVersion: 1 };

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });
});

test("malformed nested coverage manifest", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.rpcs[RPC].roles = null;

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });
});

test("duplicate YAML key", async () => {
  const { parseCoverageManifest } = await import("./check-e2e-coverage.mjs");

  assert.throws(
    () =>
      parseCoverageManifest(`
schemaVersion: 1
scenarios: {}
rpcs: {}
rpcs: {}
transitions: {}
`),
    { message: "E2E_COVERAGE_YAML_DUPLICATE_KEY" },
  );
});

test("decodes descriptor RPCs with the generated well-known schema", async () => {
  const { descriptorRpcNames } = await import("./check-e2e-coverage.mjs");
  const descriptorSet = create(FileDescriptorSetSchema, {
    file: [
      {
        name: "tammy/v1/system.proto",
        package: "tammy.v1",
        service: [
          {
            name: "SystemService",
            method: [{ name: "GetDiagnostics" }],
          },
        ],
      },
    ],
  });

  assert.deepEqual(descriptorRpcNames(toBinary(FileDescriptorSetSchema, descriptorSet)), [
    SYSTEM_RPC,
  ]);
});

test("permits only the reviewed pre-workspace system-query exception", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = systemQueryInput();

  assert.doesNotThrow(() => checkE2ECoverage(input));
});

test("system query requires the exact reviewed roles list and idempotency exceptions", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const invalidValues = [undefined, null, false, {}, []];

  for (const field of ["list", "idempotency", "roles"]) {
    for (const invalidValue of invalidValues) {
      const input = systemQueryInput();
      if (invalidValue === undefined) {
        delete input.coverage.rpcs[SYSTEM_RPC][field];
      } else {
        input.coverage.rpcs[SYSTEM_RPC][field] = invalidValue;
      }

      assert.throws(() => checkE2ECoverage(input), {
        message: "E2E_COVERAGE_MANIFEST_INVALID",
      });
    }
  }
});

test("rejects an unreviewed not-applicable exception", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = systemQueryInput();
  input.coverage.rpcs[SYSTEM_RPC].idempotency = "not_applicable_query";

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });
});

test("loads coverage from the descriptor and production manifests", async (context) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-coverage-"));
  context.after(() => rm(root, { force: true, recursive: true }));
  await mkdir(path.join(root, "test/e2e"), { recursive: true });
  await mkdir(path.join(root, "apps/desktop/src/shared"), { recursive: true });
  const input = systemQueryInput();
  await writeFile(path.join(root, "test/e2e/coverage.yaml"), stringify(input.coverage));
  await writeFile(
    path.join(root, "test/e2e/transitions.yaml"),
    stringify({ schemaVersion: 1, transitions: {} }),
  );
  await writeFile(
    path.join(root, "apps/desktop/src/shared/preload-methods.json"),
    JSON.stringify(["getSystemDiagnostics"]),
  );
  const descriptorSet = create(FileDescriptorSetSchema, {
    file: [
      {
        package: "tammy.v1",
        service: [{ name: "SystemService", method: [{ name: "GetDiagnostics" }] }],
      },
    ],
  });
  const descriptors = path.join(root, "descriptors.pb");
  await writeFile(descriptors, toBinary(FileDescriptorSetSchema, descriptorSet));
  const { runE2ECoverage } = await import("./check-e2e-coverage.mjs");

  await assert.doesNotReject(runE2ECoverage({ descriptorPath: descriptors, root }));
});
