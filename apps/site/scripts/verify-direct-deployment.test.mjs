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
const unconfirmed = { ...receipt, status: "published-unconfirmed", url: null, readback: "matched" };
void it("accepts a readback-matched publication with url null and reports its publication status", async () => {
  const requested = [];
  const recordingFixture = fixture();
  const result = await verifyDirectDeployment({
    deployment: unconfirmed, expectedRevision: revision,
    fetcher: (url, init) => { requested.push(url); return recordingFixture(url, init); },
  });
  assert.equal(result.ok, true, result.failures.join("; "));
  assert.equal(result.publication_status, "published-unconfirmed");
  assert.equal(result.version_id, receipt.version_id);
  assert(requested.length > 0);
  for (const url of requested) assert.equal(new URL(url).origin, "https://tammy-site-direct.ben-ebsworth.workers.dev");
});
void it("accepts a published-unconfirmed record that carries the exact direct Worker URL", async () => {
  const result = await verifyDirectDeployment({
    deployment: { ...unconfirmed, url: "https://tammy-site-direct.ben-ebsworth.workers.dev/" },
    expectedRevision: revision, fetcher: fixture(),
  });
  assert.equal(result.ok, true, result.failures.join("; "));
  assert.equal(result.publication_status, "published-unconfirmed");
});
void it("reports publication_status published for confirmed receipts", async () => {
  const result = await verifyDirectDeployment({ deployment: receipt, expectedRevision: revision, fetcher: fixture() });
  assert.equal(result.publication_status, "published");
});
void it("rejects every other status and any foreign URL on an unconfirmed record before any request", async () => {
  for (const patch of [
    { status: "outcome-uncertain" }, { status: "preflight-failed" }, { status: "started" },
    { status: "published-unconfirmed", url: "https://tammy.tax/" },
    { status: "published-unconfirmed", url: "https://tammy-site-direct.ben-ebsworth.workers.dev/other" },
    { status: "published-unconfirmed", version_id: null },
    { status: "published", url: null },
  ]) {
    let requests = 0;
    await assert.rejects(verifyDirectDeployment({
      deployment: { ...unconfirmed, ...patch }, expectedRevision: revision,
      fetcher: () => { requests++; },
    }));
    assert.equal(requests, 0, JSON.stringify(patch));
  }
});
