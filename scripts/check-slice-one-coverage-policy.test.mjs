import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { parseCoverageManifest } from "./check-e2e-coverage.mjs";
import { SLICE_ONE_RPC_POLICY } from "./slice-one-coverage-policy.mjs";

const SYSTEM_RPC = "tammy.v1.SystemService.GetDiagnostics";
const ALL_ROLES_ALLOWED = ["workspace_admin", "business_preparer", "business_lodger", "auditor"];

test("coverage declares the exact normative policy for all 68 non-system RPCs", async () => {
  const coverage = parseCoverageManifest(await readFile("test/e2e/coverage.yaml", "utf8"));
  const actualRpcNames = Object.keys(coverage.rpcs).filter((rpcName) => rpcName !== SYSTEM_RPC);

  assert.equal(Object.keys(SLICE_ONE_RPC_POLICY).length, 68);
  assert.deepEqual(actualRpcNames.sort(), Object.keys(SLICE_ONE_RPC_POLICY).sort());

  for (const [rpcName, expected] of Object.entries(SLICE_ONE_RPC_POLICY)) {
    const actual = coverage.rpcs[rpcName];
    assert.deepEqual(
      {
        preload: actual.preload,
        projections: actual.projections,
        routes: actual.routes,
        roles: actual.roles,
        principalFailures: actual.principalFailures,
        list: actual.list,
        idempotency: actual.idempotency,
      },
      expected,
      rpcName,
    );
  }
});

test("Slice 1 policy does not export a duplicate idempotency-mode vocabulary", async () => {
  const policyModule = await import("./slice-one-coverage-policy.mjs");

  assert.equal(Object.hasOwn(policyModule, "SLICE_ONE_IDEMPOTENCY_MODES"), false);
});

test("coverage conflict failures match every public request concurrency field", async () => {
  const protoPaths = [
    "proto/tammy/v1/workspace.proto",
    "proto/tammy/v1/identity.proto",
    "proto/tammy/v1/organisation.proto",
    "proto/tammy/v1/accounting.proto",
    "proto/tammy/v1/audit.proto",
  ];

  for (const protoPath of protoPaths) {
    const source = await readFile(protoPath, "utf8");
    const packageName = source.match(/^package ([a-z0-9.]+);$/m)?.[1];
    const requestBodies = new Map(
      [...source.matchAll(/^message (\w+Request) \{([\s\S]*?)^\}$/gm)].map((match) => [
        match[1],
        match[2],
      ]),
    );

    for (const serviceMatch of source.matchAll(/^service (\w+) \{([\s\S]*?)^\}$/gm)) {
      for (const rpcMatch of serviceMatch[2].matchAll(/^\s+rpc (\w+)\((\w+Request)\)/gm)) {
        const rpcName = `${packageName}.${serviceMatch[1]}.${rpcMatch[1]}`;
        const request = requestBodies.get(rpcMatch[2]);
        const failures = SLICE_ONE_RPC_POLICY[rpcName].principalFailures;

        if (/\bexpected_version\s*=/.test(request)) {
          assert.ok(failures.includes("STALE_VERSION"), rpcName);
        } else {
          assert.ok(!failures.includes("STALE_VERSION"), rpcName);
        }
        if (/\bexpected_financial_revision\s*=/.test(request)) {
          assert.ok(failures.includes("SOURCE_CONFLICT"), rpcName);
          assert.ok(!failures.includes("STALE_VERSION"), rpcName);
        }
      }
    }
  }
});

test("session-action authentication failures match request presence", async () => {
  const sources = {
    workspace: await readFile("proto/tammy/v1/workspace.proto", "utf8"),
    identity: await readFile("proto/tammy/v1/identity.proto", "utf8"),
  };
  const expectations = [
    ["tammy.v1.WorkspaceService.LockWorkspace", "workspace", "LockWorkspaceRequest"],
    [
      "tammy.v1.WorkspaceService.ForgetRememberedWorkspace",
      "workspace",
      "ForgetRememberedWorkspaceRequest",
    ],
    ["tammy.v1.IdentityService.SignOut", "identity", "SignOutRequest"],
  ];

  assert.deepEqual(
    Object.entries(SLICE_ONE_RPC_POLICY)
      .filter(([, policy]) => policy.idempotency.mode === "session_action")
      .map(([rpcName]) => rpcName)
      .sort(),
    expectations.map(([rpcName]) => rpcName).sort(),
  );

  for (const [rpcName, sourceName, requestName] of expectations) {
    const request = sources[sourceName].match(
      new RegExp(`^message ${requestName} \\{([\\s\\S]*?)^\\}`, "m"),
    )?.[1];
    const authenticationRequired =
      /AuthenticationContext authentication = \d+ \[\(buf\.validate\.field\)\.required = true\]/.test(
        request,
      );
    assert.equal(
      SLICE_ONE_RPC_POLICY[rpcName].principalFailures.includes("AUTHENTICATION_REQUIRED"),
      authenticationRequired,
      rpcName,
    );
  }
});

test("RPCs allowed to all four defined roles never claim permission denial", () => {
  for (const [rpcName, policy] of Object.entries(SLICE_ONE_RPC_POLICY)) {
    const allRolesAllowed = ALL_ROLES_ALLOWED.every(
      (role) => policy.roles[role] === "planned_allowed",
    );
    if (allRolesAllowed) {
      assert.ok(!policy.principalFailures.includes("PERMISSION_DENIED"), rpcName);
    }
  }
});

test("SetAccountStatus distinguishes system and control account failures", () => {
  const failures =
    SLICE_ONE_RPC_POLICY["tammy.v1.AccountingService.SetAccountStatus"].principalFailures;

  assert.ok(failures.includes("SYSTEM_ACCOUNT"));
  assert.ok(failures.includes("CONTROL_ACCOUNT"));
});

test("projection helper keeps TOTP initialisms in one snake-case token", async () => {
  const { coverageProjectionName } = await import("./slice-one-coverage-policy.mjs");

  assert.equal(typeof coverageProjectionName, "function");
  assert.deepEqual(
    ["EnrolTOTP", "ConfirmTOTP", "AssertTOTP", "DisableTOTP"].map(coverageProjectionName),
    ["enrol_totp_result", "confirm_totp_result", "assert_totp_result", "disable_totp_result"],
  );
});
