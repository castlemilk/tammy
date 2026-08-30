import assert from "node:assert/strict";
import test from "node:test";

import {
  createExternalHandoffEvent,
  detectMacOSEgressEnforcer,
  parseMacOSProcessTreeSnapshot,
  validateMacOSRuntimeEgressEvidence,
} from "./macos-runtime-egress.mjs";

const sha = (digit) => digit.repeat(64);
const privacyUrl = "https://tammy-accounting.castlemilk.chatgpt.site/privacy";
const supportUrl = "https://tammy-accounting.castlemilk.chatgpt.site/support";

test("requires a positive active-containment and audit preflight", async () => {
  const passing = {
    auditSamples: 3,
    childInheritanceDenied: true,
    dnsDenied: true,
    loopbackAllowed: true,
    nonLoopbackDenied: true,
  };
  assert.deepEqual(
    await detectMacOSEgressEnforcer({ platform: "darwin", probe: async () => passing }),
    { kind: "sandbox-exec", preflight: passing },
  );
  for (const changed of [
    { ...passing, auditSamples: 0 },
    { ...passing, childInheritanceDenied: false },
    { ...passing, dnsDenied: false },
    { ...passing, loopbackAllowed: false },
    { ...passing, nonLoopbackDenied: false },
  ]) {
    await assert.rejects(
      detectMacOSEgressEnforcer({ platform: "darwin", probe: async () => changed }),
      /MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE/,
    );
  }
  await assert.rejects(
    detectMacOSEgressEnforcer({ platform: "linux", probe: async () => passing }),
    /MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE/,
  );
});

test("parses a pinned process tree and rejects replacements", () => {
  const snapshot = [
    "100\t1\t/Applications/Tammy.app/Contents/MacOS/Tammy",
    "101\t100\t/Applications/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core",
    "102\t100\t/Applications/Tammy.app/Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
  ].join("\n");
  assert.deepEqual(parseMacOSProcessTreeSnapshot(snapshot, 100), [
    {
      executablePath: "/Applications/Tammy.app/Contents/MacOS/Tammy",
      parentProcessId: 1,
      processId: 100,
    },
    {
      executablePath: "/Applications/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core",
      parentProcessId: 100,
      processId: 101,
    },
    {
      executablePath:
        "/Applications/Tammy.app/Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
      parentProcessId: 100,
      processId: 102,
    },
  ]);
  assert.throws(
    () => parseMacOSProcessTreeSnapshot(`${snapshot}\n103\t100\t/tmp/replacement`, 100),
    /MACOS_RUNTIME_PROCESS_EVIDENCE_INVALID/,
  );
});

test("records only exact gesture-bound public handoffs and strict zero-egress evidence", () => {
  const privacy = createExternalHandoffEvent({
    allowedUrls: [privacyUrl, supportUrl],
    occurredAt: "2026-08-31T00:00:01.000Z",
    url: privacyUrl,
    userGesture: true,
  });
  const support = createExternalHandoffEvent({
    allowedUrls: [privacyUrl, supportUrl],
    occurredAt: "2026-08-31T00:00:02.000Z",
    url: supportUrl,
    userGesture: true,
  });
  const evidence = {
    appSha256: sha("a"),
    auditSamples: 4,
    buildNumber: "42",
    coreSha256: sha("b"),
    deniedDnsAttempts: 0,
    deniedNonLoopbackAttempts: 0,
    handoffs: [privacy, support],
    helperSha256: sha("c"),
    listeners: [{ address: "127.0.0.1", owner: "authenticated-core", port: 43123 }],
    marketingVersion: "0.1.0",
    observationSamples: 10,
    observedNonLoopbackConnections: 0,
    processPaths: [
      "/Applications/Tammy.app/Contents/MacOS/Tammy",
      "/Applications/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core",
      "/Applications/Tammy.app/Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
    ],
    productSourceCommit: "d".repeat(40),
    productSourceTree: "e".repeat(40),
    schemaVersion: 1,
  };
  assert.deepEqual(validateMacOSRuntimeEgressEvidence(evidence), evidence);
  for (const mutate of [
    (value) => ({ ...value, observationSamples: 0 }),
    (value) => ({ ...value, deniedDnsAttempts: 1 }),
    (value) => ({ ...value, observedNonLoopbackConnections: 1 }),
    (value) => ({ ...value, handoffs: [privacy] }),
    (value) => ({
      ...value,
      listeners: [...value.listeners, { address: "0.0.0.0", owner: "app", port: 80 }],
    }),
  ]) {
    assert.throws(
      () => validateMacOSRuntimeEgressEvidence(mutate(evidence)),
      /MACOS_RUNTIME_EGRESS_EVIDENCE_INVALID/,
    );
  }
});
