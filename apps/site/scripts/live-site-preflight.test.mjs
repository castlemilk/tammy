import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { inspectLiveSite, assertTargetOrigin } from "./live-site-preflight.mjs";

const ORIGIN = "https://tammy.tax";
const ROUTES = ["/", "/tools", "/blog", "/robots.txt", "/sitemap.xml"];
function fixture({
  canonicalHost = ORIGIN, sitemapHost = ORIGIN, noindex = false,
  redirectHost = ORIGIN, missing = null,
} = {}) {
  return async (url) => {
    const path = new URL(url).pathname;
    if (path === missing) throw new Error("connection refused");
    const meta = '<meta name="robots" content="' + (noindex ? "noindex" : "index,follow") + '">';
    const body = path === "/sitemap.xml" ?
      '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">' +
      ROUTES.filter((x) => !x.endsWith(".xml") && !x.endsWith(".txt")).map((p) => "<url><loc>" + sitemapHost + p + "</loc></url>").join("") +
      "</urlset>" :
      path === "/robots.txt" ? "User-agent: *\nSitemap: " + sitemapHost + "/sitemap.xml" :
      '<html><head><link href="' + canonicalHost + path + '" rel="canonical">' + meta + "</head><body>Valid</body></html>";
    return {
      status: 200,
      url: redirectHost + path,
      headers: { get: (name) => name === "content-type" ? (path.endsWith(".xml") ? "application/xml" : "text/html") : null },
      text: async () => body,
    };
  };
}
void describe("Sites production preflight", () => {
  void it("accepts a valid fully canonicalised page set", async () => {
    const result = await inspectLiveSite({ origin: ORIGIN, routes: ROUTES, fetcher: fixture() });
    assert.equal(result.ok, true, result.failures.join("; "));
    assert.equal(result.checked.length, ROUTES.length);
  });
  void it("rejects non-HTTPS and pathful origins", () => {
    for (const value of ["http://tammy.tax", "https://tammy.tax/tools", "ftp://tammy.tax", "https://a:b@tammy.tax"]) {
      assert.throws(() => assertTargetOrigin(value));
    }
  });
  void it("accepts an equivalent root canonical without weakening other route matching", async () => {
    const inspect = async (route, canonicalValue) => inspectLiveSite({
      origin: ORIGIN, routes: [route], fetcher: async (url) => {
        const response = await fixture()(url);
        return { ...response, text: async () => '<link rel="canonical" href="' + canonicalValue + '">' };
      },
    });
    assert.equal((await inspect("/", ORIGIN)).ok, true);
    for (const canonicalValue of [ORIGIN + "/tools/", ORIGIN + "/tools?other=1", ORIGIN, "https://foreign.example/tools"]) {
      assert.equal((await inspect("/tools", canonicalValue)).ok, false);
    }
  });
  void it("fails canonical mismatches and noindex without redirecting", async () => {
    const result = await inspectLiveSite({ origin: ORIGIN, routes: ROUTES, fetcher: fixture({ canonicalHost:"https://old.chatgpt.site", noindex:true }) });
    assert.equal(result.ok, false);
    assert(result.failures.some((x) => x.includes("canonical")));
    assert(result.failures.some((x) => x.includes("noindex")));
  });
  void it("fails source-host redirects and invalid sitemap domains", async () => {
    const result = await inspectLiveSite({ origin: ORIGIN, routes: ROUTES, fetcher: fixture({ redirectHost: "https://other.example", sitemapHost:"https://other.example" }) });
    assert.equal(result.ok, false);
    assert(result.failures.some((x) => x.includes("redirected off")));
    assert(result.failures.some((x) => x.includes("foreign-origin")));
  });
  void it("distinguishes unavailable routes from valid pages", async () => {
    const result = await inspectLiveSite({ origin: ORIGIN, routes: ROUTES, fetcher: fixture({missing:"/blog"}) });
    assert.equal(result.ok, false);
    assert(result.failures.some((x) => x.startsWith("/blog: unavailable")));
  });
  void it("requires an explicit canonical origin for the independent direct Worker", async () => {
    const direct = "https://tammy-site-direct.ben-ebsworth.workers.dev";
    const fetcher = fixture({ redirectHost: direct });
    const implicit = await inspectLiveSite({ origin: direct, routes: ROUTES, fetcher });
    assert.equal(implicit.ok, false);
    const explicit = await inspectLiveSite({ origin: direct, canonicalOrigin: ORIGIN, routes: ROUTES, fetcher });
    assert.equal(explicit.ok, true, explicit.failures.join("; "));
    const redirected = await inspectLiveSite({ origin: direct, canonicalOrigin: ORIGIN, routes: ROUTES, fetcher: fixture() });
    assert.equal(redirected.ok, false);
    assert(redirected.failures.some((x) => x.includes("redirected off")));
  });
  void it("rejects an invalid canonical override before fetching", async () => {
    let requests = 0;
    await assert.rejects(inspectLiveSite({ origin: ORIGIN, canonicalOrigin: "http://tammy.tax", fetcher: () => { requests++; } }));
    assert.equal(requests, 0);
  });
});
