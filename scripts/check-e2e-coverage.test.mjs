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
          stage: "production",
          preload: "getWorkspaceState",
          cases: ["foundation/offline-ready"],
          projections: ["workspace_state"],
          routes: ["/unlock"],
          roles: Object.fromEntries(ROLES.map((role) => [role, "allowed"])),
          principalFailures: ["AUTHENTICATION_REQUIRED"],
          list: { states: ["found"] },
          idempotency: { mode: "query", outcomes: ["not_applicable"] },
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

function futureInput() {
  const input = validInput();
  input.coverage.scenarios["E2E-00"].futureCases = ["foundation/workspace-state"];
  input.coverage.rpcs[RPC] = {
    stage: "declared_future",
    preload: "getWorkspaceState",
    cases: [],
    futureCases: ["foundation/workspace-state"],
    projections: ["workspace_state"],
    routes: ["/unlock"],
    roles: Object.fromEntries(ROLES.map((role) => [role, "planned_allowed"])),
    principalFailures: ["PERMISSION_DENIED"],
    list: { states: ["not_applicable_single_query"] },
    idempotency: { mode: "query", outcomes: ["not_applicable"] },
  };
  input.preloadMethods = [];
  return input;
}

function futureTransitionInput() {
  const input = validInput();
  input.coverage.scenarios["E2E-00"].futureCases = ["foundation/workspace-state"];
  input.coverage.transitions["tammy.v1.WorkspaceState.LOCKED->READY"] = {
    stage: "declared_future",
    cases: [],
    futureCases: ["foundation/workspace-state"],
  };
  input.transitionIds = ["tammy.v1.WorkspaceState.LOCKED->READY"];
  return input;
}

test("checker module exists", async () => {
  const checker = await import("./check-e2e-coverage.mjs");

  assert.equal(typeof checker.checkE2ECoverage, "function");
});

test("parses the production-only coverage CLI option", async () => {
  const { coverageCliOptions } = await import("./check-e2e-coverage.mjs");
  const descriptorPath = path.join(process.cwd(), "descriptors.pb");

  assert.deepEqual(
    coverageCliOptions(["--require-production", "--descriptors", "descriptors.pb"]),
    {
      descriptorPath,
      requireProduction: true,
    },
  );
  assert.deepEqual(coverageCliOptions(["--descriptors", "descriptors.pb"]), {
    descriptorPath,
    requireProduction: false,
  });
  assert.throws(() => coverageCliOptions(["--require-production"]), {
    message: "E2E_COVERAGE_DESCRIPTORS_REQUIRED",
  });
});

test("declared future RPC permits an absent planned preload", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");

  assert.doesNotThrow(() => checkE2ECoverage(futureInput()));
});

test("declared future RPC requires promotion when its preload is exposed", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = futureInput();
  input.preloadMethods = ["getWorkspaceState"];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_FUTURE_PROMOTION_REQUIRED",
  });
});

test("declared future RPC rejects executed cases", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = futureInput();
  input.coverage.rpcs[RPC].cases = ["foundation/offline-ready"];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });
});

test("declared future RPC rejects an unknown future case", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = futureInput();
  input.coverage.rpcs[RPC].futureCases = ["foundation/unknown-future"];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_FUTURE_CASE_MISSING",
  });
});

test("declared future transition permits a planned case", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");

  assert.doesNotThrow(() => checkE2ECoverage(futureTransitionInput()));
});

test("declared future transition rejects an executed case", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = futureTransitionInput();
  input.coverage.transitions["tammy.v1.WorkspaceState.LOCKED->READY"].cases = [
    "foundation/offline-ready",
  ];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });
});

test("a future case cannot satisfy a production RPC", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.scenarios["E2E-00"].futureCases = ["foundation/workspace-state"];
  input.coverage.rpcs[RPC].cases = ["foundation/workspace-state"];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_CASE_MISSING",
  });
});

test("a future case cannot satisfy a production transition", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.scenarios["E2E-00"].futureCases = ["foundation/workspace-state"];
  input.coverage.transitions["tammy.v1.WorkspaceState.LOCKED->READY"] = {
    stage: "production",
    cases: ["foundation/workspace-state"],
  };
  input.transitionIds = ["tammy.v1.WorkspaceState.LOCKED->READY"];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_CASE_MISSING",
  });
});

test("rejects an unknown coverage stage", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.rpcs[RPC].stage = "planned";

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });
});

test("production mode rejects declared future RPC coverage", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = futureInput();
  input.requireProduction = true;

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_FUTURE_PROMOTION_REQUIRED",
  });
});

test("production mode rejects declared future transition coverage", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = futureTransitionInput();
  input.requireProduction = true;

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_FUTURE_PROMOTION_REQUIRED",
  });
});

test("production RPC coverage requires an executed case", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.rpcs[RPC].cases = [];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });
});

test("production transition coverage requires an executed case", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.transitions["tammy.v1.WorkspaceState.LOCKED->READY"] = {
    stage: "production",
    cases: [],
  };
  input.transitionIds = ["tammy.v1.WorkspaceState.LOCKED->READY"];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });
});

test("business RPC coverage requires complete deterministic metadata", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const mutations = [
    (rpc) => {
      rpc.projections = [];
    },
    (rpc) => {
      rpc.preload = "";
    },
    (rpc) => {
      rpc.routes = [];
    },
    (rpc) => {
      rpc.routes = [""];
    },
    (rpc) => {
      rpc.routes = ["unlock"];
    },
    (rpc) => {
      rpc.routes = ["/unlock", "/unlock"];
    },
    (rpc) => {
      rpc.principalFailures = [""];
    },
    (rpc) => {
      rpc.principalFailures = [];
    },
    (rpc) => {
      rpc.principalFailures = ["permission_denied"];
    },
    (rpc) => {
      rpc.principalFailures = ["PERMISSION_DENIED", "PERMISSION_DENIED"];
    },
    (rpc) => {
      rpc.list = {};
    },
    (rpc) => {
      rpc.list = { states: [] };
    },
    (rpc) => {
      rpc.list = { states: [null] };
    },
    (rpc) => {
      rpc.idempotency = {};
    },
    (rpc) => {
      rpc.idempotency = { mode: "query", outcomes: [] };
    },
    (rpc) => {
      rpc.idempotency = { mode: "query", outcomes: [null] };
    },
    (rpc) => {
      rpc.roles.auditor = null;
    },
  ];

  for (const mutate of mutations) {
    const input = futureInput();
    mutate(input.coverage.rpcs[RPC]);
    assert.throws(() => checkE2ECoverage(input), {
      message: "E2E_COVERAGE_MANIFEST_INVALID",
    });
  }
});

test("scenario cases are nonempty, unique, disjoint, and globally unambiguous", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const mutations = [
    (coverage) => {
      coverage.scenarios["E2E-00"].cases = [""];
    },
    (coverage) => {
      coverage.scenarios["E2E-00"].cases = ["foundation/offline-ready", "foundation/offline-ready"];
    },
    (coverage) => {
      coverage.scenarios["E2E-00"].futureCases = ["foundation/offline-ready"];
    },
    (coverage) => {
      coverage.scenarios["E2E-01"] = { cases: ["foundation/offline-ready"] };
    },
  ];

  for (const mutate of mutations) {
    const input = validInput();
    mutate(input.coverage);
    assert.throws(() => checkE2ECoverage(input), {
      message: "E2E_COVERAGE_MANIFEST_INVALID",
    });
  }
});

test("RPC and transition case arrays reject empty and duplicate IDs", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const rpcInput = futureInput();
  rpcInput.coverage.scenarios["E2E-00"].futureCases.push("foundation/second-future");
  rpcInput.coverage.rpcs[RPC].futureCases = [
    "foundation/workspace-state",
    "foundation/workspace-state",
  ];
  assert.throws(() => checkE2ECoverage(rpcInput), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });

  const transitionInput = futureTransitionInput();
  transitionInput.coverage.transitions["tammy.v1.WorkspaceState.LOCKED->READY"].futureCases = [""];
  assert.throws(() => checkE2ECoverage(transitionInput), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });
});

test("the legacy System stage omission never applies to transitions", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = validInput();
  input.coverage.transitions[SYSTEM_RPC] = {
    cases: ["foundation/offline-ready"],
  };
  input.transitionIds = [SYSTEM_RPC];

  assert.throws(() => checkE2ECoverage(input), {
    message: "E2E_COVERAGE_MANIFEST_INVALID",
  });
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
    stage: "production",
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

test("reports a deterministic error for invalid descriptor wire data", async () => {
  const { descriptorRpcNames } = await import("./check-e2e-coverage.mjs");

  for (const bytes of [Buffer.from([0x0a, 0x05, 0x01]), Buffer.from([0x0f])]) {
    assert.throws(
      () => descriptorRpcNames(bytes),
      (error) => {
        assert.equal(error.message, "E2E_COVERAGE_DESCRIPTOR_INVALID");
        assert.ok(error.cause instanceof Error);
        assert.notEqual(error.message, error.cause.message);
        return true;
      },
    );
  }
});

test("permits only the reviewed pre-workspace system-query exception", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const input = systemQueryInput();

  assert.doesNotThrow(() => checkE2ECoverage(input));
});

test("rejects the reviewed system-query exception on business RPC coverage", async () => {
  const { checkE2ECoverage } = await import("./check-e2e-coverage.mjs");
  const structuredInput = validInput();

  assert.doesNotThrow(() => checkE2ECoverage(structuredInput));

  for (const field of ["roles", "list", "idempotency"]) {
    const input = validInput();
    input.coverage.rpcs[RPC][field] = REVIEWED_EXCEPTION;

    assert.throws(() => checkE2ECoverage(input), {
      message: "E2E_COVERAGE_MANIFEST_INVALID",
    });
  }
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
