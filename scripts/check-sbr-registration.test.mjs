import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createSbrRegistrationHandoffReport } from "./check-sbr-registration.mjs";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function run(arguments_ = []) {
  return new Promise((resolve, reject) => {
    const child = spawn(
      "mise",
      ["exec", "--", "node", "scripts/check-sbr-registration.mjs", ...arguments_],
      {
        cwd: repositoryRoot,
        shell: false,
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => (stdout += chunk));
    child.stderr.on("data", (chunk) => (stderr += chunk));
    child.once("error", reject);
    child.once("close", (code, signal) => resolve({ code, signal, stderr, stdout }));
  });
}

test("reports the complete external handoff without claiming EVTE readiness", () => {
  assert.deepEqual(createSbrRegistrationHandoffReport(), {
    schema: "tammy-sbr-registration-check-v1",
    environment: "EVTE",
    status: "BLOCKED",
    code: "EVTE_SIGNED_INPUTS_REQUIRED",
    required_external_items: [
      "DSP_REGISTRATION",
      "PRODUCT_REGISTRATION",
      "OSF_ASSESSMENT",
      "ATO_COMPONENT_LICENCE_AND_TARGET",
      "EVTE_ACCESS",
      "SIGNED_ENDPOINT_PROFILE",
      "SERVICE_ENROLMENT",
      "SERVICE_CONFORMANCE",
      "INDEPENDENT_REVIEW",
      "EXPIRY_AND_REVALIDATION",
      "REDACTED_EVIDENCE_EXPORT",
    ],
    production_sbr: "UNAVAILABLE",
    bas_submission: "UNAVAILABLE",
  });
});

test("command emits one bounded non-secret report and accepts no inputs", async () => {
  const result = await run();
  assert.equal(result.code, 1);
  assert.equal(result.signal, null);
  assert.equal(result.stderr, "");
  const report = JSON.parse(result.stdout);
  assert.deepEqual(report, createSbrRegistrationHandoffReport());
  assert.equal(report.status, "BLOCKED");
  assert.equal(report.code, "EVTE_SIGNED_INPUTS_REQUIRED");
  assert.ok(Buffer.byteLength(result.stdout) < 4096);
  assert.doesNotMatch(
    result.stdout,
    /(?:password|credential|product.?id|endpoint.?url|\/Users\/)/i,
  );

  const rejected = await run(["--credential=/private/tmp/example.p12"]);
  assert.notEqual(rejected.code, 0);
  assert.equal(rejected.stdout, "");
  assert.equal(rejected.stderr, "SBR_REGISTRATION_CHECK_ARGUMENTS_INVALID\n");
});
