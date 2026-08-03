import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { parseCoverageManifest } from "./check-e2e-coverage.mjs";
import { SLICE_ONE_RPC_POLICY } from "./slice-one-coverage-policy.mjs";

const SYSTEM_RPC = "tammy.v1.SystemService.GetDiagnostics";

test("Slice 1 coverage declares the exact normative policy for all 65 RPCs", async () => {
  const coverage = parseCoverageManifest(await readFile("test/e2e/coverage.yaml", "utf8"));
  const actualRpcNames = Object.keys(coverage.rpcs).filter((rpcName) => rpcName !== SYSTEM_RPC);

  assert.equal(Object.keys(SLICE_ONE_RPC_POLICY).length, 65);
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
