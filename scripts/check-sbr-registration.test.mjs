import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdir, mkdtemp, symlink, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  checkInstalledSbrRegistration,
  createSbrRegistrationHandoffReport,
} from "./check-sbr-registration.mjs";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const INSTALLED_FILES = [
  "sbr-profile-v1.json",
  "sbr-profile-v1.sig",
  "sbr-component-v1.json",
  "sbr-registration-v1.json",
  "sbr-registration-v1.sig",
  "sbr-endpoint-profile-v1.json",
];

function run(arguments_ = []) {
  return new Promise((resolve, reject) => {
    const child = spawn(
      "mise",
      ["exec", "--", "node", "scripts/check-sbr-registration.mjs", ...arguments_],
      { cwd: repositoryRoot, shell: false, stdio: ["ignore", "pipe", "pipe"] },
    );
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => (stdout += chunk));
    child.stderr.on("data", (chunk) => (stderr += chunk));
    child.once("error", reject);
    child.once("close", (code, signal) => resolve({ code, signal, stderr, stdout }));
  });
}

test("reports missing exact installed inputs without claiming EVTE readiness", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-registration-check-"));
  assert.deepEqual(await checkInstalledSbrRegistration({ repositoryRoot: root }), {
    ...createSbrRegistrationHandoffReport(),
    installed_input_state: "MISSING",
    missing_installed_inputs: [
      "PROFILE_MANIFEST",
      "PROFILE_SIGNATURE",
      "COMPONENT_MANIFEST",
      "REGISTRATION_MANIFEST",
      "REGISTRATION_SIGNATURE",
      "ENDPOINT_PROFILE",
    ],
  });
});

test("validates all exact installed inputs through the authenticated preflight", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-registration-check-"));
  const installed = path.join(root, "config/sbr/evte");
  await mkdir(installed, { recursive: true });
  for (const file of INSTALLED_FILES) await writeFile(path.join(installed, file), file, { mode: 0o600 });
  const authenticate = (input) => {
    assert.equal(input.phase, "PRE_CONFORMANCE");
    assert.deepEqual(
      Object.values(input)
        .filter((value) => value instanceof Uint8Array)
        .map((value) => Buffer.from(value).toString()),
      INSTALLED_FILES,
    );
    return {
      readiness: { ready: false, code: "EVTE_TRUST_ROOT_UNREGISTERED" },
      fingerprints: {
        component_sha256: "a".repeat(64),
        endpoint_sha256: "b".repeat(64),
        registration_sha256: "c".repeat(64),
      },
      metadata: { environment: "EVTE", target: "darwin/arm64", endpoint_id: "evte", endpoint_revision: 1 },
    };
  };

  const report = await checkInstalledSbrRegistration({ authenticate, repositoryRoot: root });
  assert.equal(report.status, "BLOCKED");
  assert.equal(report.code, "EVTE_TRUST_ROOT_UNREGISTERED");
  assert.equal(report.installed_input_state, "AUTHENTICATED_BLOCKED");
  assert.equal(report.production_sbr, "UNAVAILABLE");
  assert.equal(report.bas_submission, "UNAVAILABLE");
  assert.equal(JSON.stringify(report).includes(root), false);
});

test("rejects unsafe installed inputs without disclosing their path", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-registration-check-"));
  const installed = path.join(root, "config/sbr/evte");
  await mkdir(installed, { recursive: true });
  await symlink("/private/tmp/not-an-installed-input", path.join(installed, INSTALLED_FILES[0]));

  const report = await checkInstalledSbrRegistration({ repositoryRoot: root });
  assert.equal(report.status, "BLOCKED");
  assert.equal(report.code, "INSTALLED_INPUT_UNSAFE");
  assert.equal(report.installed_input_state, "INVALID");
  assert.doesNotMatch(JSON.stringify(report), /private\/tmp|tammy-registration-check/u);
});

test("CLI is bounded, registration blocks nonzero, and doctor preflight may continue", async () => {
  for (const [arguments_, expectedCode] of [[[], 1], [["--doctor-preflight"], 0]]) {
    const result = await run(arguments_);
    assert.equal(result.code, expectedCode);
    assert.equal(result.signal, null);
    assert.equal(result.stderr, "");
    const report = JSON.parse(result.stdout);
    assert.equal(report.status, "BLOCKED");
    assert.ok(Buffer.byteLength(result.stdout) < 4096);
    assert.doesNotMatch(result.stdout, /(?:password|credential|product.?id|endpoint.?url|\/Users\/)/iu);
  }

  const rejected = await run(["--credential=/private/tmp/example.p12"]);
  assert.notEqual(rejected.code, 0);
  assert.equal(rejected.stdout, "");
  assert.equal(rejected.stderr, "SBR_REGISTRATION_CHECK_ARGUMENTS_INVALID\n");
});
