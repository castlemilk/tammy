import assert from "node:assert/strict";
import { it } from "node:test";
import { verifyDirectDeployment } from "./verify-direct-deployment.mjs";
import { REQUIRED_ROUTES } from "./live-site-preflight.mjs";

const revision = "a".repeat(40);
const receipt = {
  schema_version: 1, target: "cloudflare", worker_name: "tammy-site-direct",
  account_id: "2132ccf47ceb5fff234c34d85490470a",
  github: { repository: "castlemilk/tammy", commit_sha: revision },
  status: "published", version_id: "df005821-f3ae-451d-b728-610623a393ac",
  url: "https://tammy-site-direct.ben-ebsworth.workers.dev/",
  source_sha256: "b".repeat(64), build_sha256: "c".repeat(64),
};
function fixture({ missing = false } = {}) {
  return (url) => {
    const route = new URL(url).pathname;
    const origin = "https://tammy-accounting.castlemilk.chatgpt.site";
    const body = route === "/robots.txt" ? "Sitemap: " + origin + "/sitemap.xml" :
      route === "/sitemap.xml" ? "<urlset>" + REQUIRED_ROUTES.filter((x) => !x.endsWith(".txt") && !x.endsWith(".xml")).map((x) => "<url><loc>" + origin + x + "</loc></url>").join("") + "</urlset>" :
      '<link rel="canonical" href="' + origin + route + '">';
    return Promise.resolve({ status: missing && route === "/tools" ? 503 : 200, url, text: () => Promise.resolve(body), headers: { get: () => "text/html" } });
  };
}
void it("accepts the exact published receipt and keeps HTTP acceptance separate", async () => {
  const original = JSON.stringify(receipt);
  const result = await verifyDirectDeployment({ deployment: receipt, expectedRevision: revision, fetcher: fixture() });
  assert.equal(result.ok, true, result.failures.join("; "));
  assert.equal(result.checked.length, REQUIRED_ROUTES.length);
  assert.equal(result.canonicalOrigin, "https://tammy-accounting.castlemilk.chatgpt.site");
  assert.equal(result.version_id, receipt.version_id);
  assert.equal(JSON.stringify(receipt), original);
});
void it("rejects stale, foreign or uncertain publication records before any request", async () => {
  for (const patch of [
    { github: { repository: "castlemilk/tammy", commit_sha: "d".repeat(40) } },
    { github: { repository: "someone/tammy", commit_sha: revision } },
    { status: "outcome-uncertain" }, { target: "sites" }, { worker_name: "another-worker" },
    { account_id: "d".repeat(32) }, { url: "https://tammy.tax/" }, { version_id: null }, { build_sha256: "bad" },
  ]) {
    let requests = 0;
    await assert.rejects(verifyDirectDeployment({ deployment: { ...receipt, ...patch }, expectedRevision: revision, fetcher: () => { requests++; } }));
    assert.equal(requests, 0);
  }
});
void it("reports a published but unhealthy Worker as failed acceptance", async () => {
  const result = await verifyDirectDeployment({ deployment: receipt, expectedRevision: revision, fetcher: fixture({ missing: true }) });
  assert.equal(result.ok, false);
  assert(result.failures.some((x) => x.includes("503")));
  assert.equal(receipt.status, "published");
});
