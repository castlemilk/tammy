import assert from "node:assert/strict";
import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { parseArgs } from "node:util";
import { pathToFileURL } from "node:url";
import { inspectLiveSite } from "./live-site-preflight.mjs";
import { SITE_ORIGIN } from "../lib/site-config.ts";

const DIRECT_ORIGIN = "https://tammy-site-direct.ben-ebsworth.workers.dev";

// Publication and HTTP acceptance are separate records. This never republishes,
// logs into Cloudflare, changes routing or rewrites the original receipt.
export async function verifyDirectDeployment({ deployment, expectedRevision, fetcher = fetch }) {
  assert.match(expectedRevision ?? "", /^[a-f0-9]{40}$/, "Expected an exact Git revision");
  assert.equal(deployment.schema_version, 1);
  assert.equal(deployment.target, "cloudflare");
  assert.equal(deployment.worker_name, "tammy-site-direct");
  assert.equal(deployment.account_id, "2132ccf47ceb5fff234c34d85490470a");
  assert.equal(deployment.github?.repository, "castlemilk/tammy");
  assert.equal(deployment.github?.commit_sha, expectedRevision, "Receipt must match this workflow revision");
  assert.equal(deployment.status, "published", "HTTP acceptance requires confirmed publication");
  assert.match(deployment.version_id ?? "", /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/);
  assert.equal(deployment.url, DIRECT_ORIGIN + "/", "Only the verified direct Worker is accepted");
  for (const field of ["source_sha256", "build_sha256"]) assert.match(deployment[field] ?? "", /^[a-f0-9]{64}$/);

  const result = await inspectLiveSite({ origin: DIRECT_ORIGIN, canonicalOrigin: SITE_ORIGIN, fetcher });
  return {
    schema_version: 1,
    kind: "direct-worker-http-acceptance",
    checked_at: new Date().toISOString(),
    github: deployment.github,
    version_id: deployment.version_id,
    source_sha256: deployment.source_sha256,
    build_sha256: deployment.build_sha256,
    ...result,
    scope: "HTTP routes and canonical metadata; not browser/PDF behaviour or an independent Cloudflare version readback",
  };
}

async function main() {
  try {
    const { values } = parseArgs({ options: { record: { type: "string" }, revision: { type: "string" }, output: { type: "string" } } });
    assert.ok(values.record && values.output, "Usage: verify-direct-deployment.mjs --record RECEIPT --revision SHA --output NEW_JSON");
    const deployment = JSON.parse(readFileSync(values.record, "utf8"));
    const result = await verifyDirectDeployment({ deployment, expectedRevision: values.revision });
    writeFileSync(values.output, JSON.stringify(result, null, 2) + "\n", { flag: "wx" });
    console.log(`${result.ok ? "PASS" : "FAIL"}: ${result.checked.length} direct Worker routes; canonical origin ${result.canonicalOrigin}`);
    if (!result.ok) {
      for (const failure of result.failures) console.error(failure);
      process.exitCode = 1;
    }
  } catch (error) {
    console.error(error instanceof Error ? error.message : "Direct Worker acceptance failed");
    process.exitCode = 1;
  }
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) await main();
