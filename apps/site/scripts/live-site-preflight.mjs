/**
 * Read-only production smoke audit. Does not deploy, publish, mutate DNS,
 * send customer data or submit any URL to Google. Run AFTER publication.
 * Usage: node scripts/live-site-preflight.mjs https://tammy.tax
 */
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

export const REQUIRED_ROUTES = Object.freeze([
  "/", "/tools", "/tools/invoice-generator", "/tools/quote-generator", "/tools/cash-flow-forecast", "/tools/gst-calculator",
  "/tools/abn-checker", "/tools/break-even-calculator", "/blog",
  "/privacy", "/robots.txt", "/sitemap.xml",
]);

function attr(tag, name) {
  const re = new RegExp("(?:^|\\s)" + name + "\\s*=\\s*[\"']([^\"']+)[\"']", "i");
  return tag.match(re)?.[1] ?? null;
}
function canonical(html) {
  for (const tag of html.match(/<link\b[^>]*>/gi) ?? []) {
    if ((attr(tag, "rel") ?? "").toLowerCase().split(/\s+/).includes("canonical")) {
      return attr(tag, "href");
    }
  }
  return null;
}
function hasNoindex(html) {
  return (html.match(/<meta\b[^>]*>/gi) ?? []).some((tag) =>
    ["robots", "googlebot"].includes((attr(tag, "name") ?? "").toLowerCase()) &&
    /(?:^|[,\s])noindex(?:[,\s]|$)/i.test(attr(tag, "content") ?? ""));
}
export function assertTargetOrigin(value) {
  let parsed;
  try { parsed = new URL(value); } catch { throw new Error("Enter a full HTTPS URL"); }
  if (parsed.protocol !== "https:" || parsed.username || parsed.password ||
    parsed.pathname !== "/" || parsed.search || parsed.hash) {
    throw new Error("Preflight requires a clean HTTPS origin, such as https://tammy.tax");
  }
  return parsed.origin;
}
export async function inspectLiveSite({
  origin, canonicalOrigin = origin, fetcher = fetch, routes = REQUIRED_ROUTES,
}) {
  const base = assertTargetOrigin(origin);
  // The direct Worker deliberately retains the configured source canonical origin.
  // Transport/redirect checks still apply to the independently verified Worker.
  const canonicalBase = assertTargetOrigin(canonicalOrigin);
  const failures = [];
  const checked = [];
  const bodies = new Map();

  for (const route of routes) {
    try {
      const response = await fetcher(base + route, {
        redirect: "follow",
        signal: AbortSignal.timeout(10000),
        headers: { Accept: route.endsWith(".xml") ? "application/xml" : "text/html,*/*" },
      });
      const url = new URL(response.url || base + route);
      const body = await response.text();
      const contentType = response.headers?.get("content-type") ?? "";
      bodies.set(route, body);
      if (response.status !== 200) failures.push(route + ": expected HTTP 200, got " + response.status);
      if (url.origin !== base) failures.push(route + ": redirected off the intended domain to " + url.origin);
      if (route.endsWith(".txt")) {
        if (!body.includes("Sitemap: " + canonicalBase + "/sitemap.xml")) {
          failures.push(route + ": sitemap directive does not use " + canonicalBase);
        }
      } else if (route.endsWith(".xml")) {
        if (!/<urlset\b/.test(body)) failures.push(route + ": expected XML sitemap urlset");
      } else {
        if (!/text\/html/i.test(contentType)) failures.push(route + ": expected HTML, got " + (contentType || "unknown"));
        const expected = canonicalBase + route;
        const actual = canonical(body);
        // An origin URL and its explicit root slash are the same resource.
        const matches = actual === expected || (route === "/" && actual === canonicalBase);
        if (!matches) failures.push(route + ": canonical is " + (actual ?? "missing") + ", expected " + expected);
        if (hasNoindex(body)) failures.push(route + ": page advertises noindex");
      }
      checked.push({ route, status: response.status, canonical: route.endsWith(".xml") || route.endsWith(".txt") ? null : canonical(body) });
    } catch (error) {
      failures.push(route + ": unavailable (" + (error instanceof Error ? error.message : "request failed") + ")");
    }
  }
  const sitemap = bodies.get("/sitemap.xml");
  if (sitemap) {
    const locations = [...sitemap.matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => m[1].trim());
    if (!locations.length) failures.push("sitemap.xml has no URL entries");
    if (locations.length !== new Set(locations).size) failures.push("sitemap.xml has duplicate URLs");
    for (const loc of locations) {
      try {
        if (new URL(loc).origin !== canonicalBase) failures.push("sitemap contains foreign-origin URL " + loc);
      } catch { failures.push("sitemap contains malformed URL " + loc); }
    }
    for (const route of routes.filter((x) => x !== "/robots.txt" && x !== "/sitemap.xml")) {
      if (!locations.includes(canonicalBase + route)) failures.push("sitemap missing " + route);
    }
  }
  return { origin: base, canonicalOrigin: canonicalBase, checked, failures, ok: failures.length === 0 };
}

async function main() {
  const origin = process.argv[2];
  if (!origin) {
    console.error("Usage: node scripts/live-site-preflight.mjs https://tammy.tax");
    process.exitCode = 2;
    return;
  }
  try {
    const result = await inspectLiveSite({ origin });
    for (const check of result.checked) console.log("HTTP " + check.status + " " + check.route + (check.canonical ? " | " + check.canonical : ""));
    if (!result.ok) {
      for (const failure of result.failures) console.error("FAIL " + failure);
      process.exitCode = 1;
    } else console.log("PASS live Tammy site routes, indexability and canonical host");
  } catch (error) {
    console.error("FAIL " + (error instanceof Error ? error.message : "Preflight failed"));
    process.exitCode = 2;
  }
}
if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  await main();
}
