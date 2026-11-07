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
});
