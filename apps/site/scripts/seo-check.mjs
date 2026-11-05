import { readFileSync, readdirSync } from "node:fs";
import { resolve, relative, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const app = join(root, "app");
const configuration = readFileSync(join(root, "lib/site-config.ts"), "utf8");
const origin = configuration.match(/SITE_ORIGIN\s*=\s*"([^"]+)"/)?.[1];
if (!origin || !/^https:\/\/[^/]+$/.test(origin))
  throw new Error("SEO: SITE_ORIGIN must be one canonical HTTPS origin");

const routes = [];
function walk(dir) {
  for (const item of readdirSync(dir, { withFileTypes: true })) {
    const location = join(dir, item.name);
    if (item.isDirectory()) walk(location);
    else if (item.name === "page.tsx") {
      const folder = relative(app, dirname(location)).replaceAll("\\", "/");
      const route = folder === "" ? "/" : "/" + folder;
      const source = readFileSync(location, "utf8");
      if (!/export const metadata\s*:\s*Metadata\s*=/.test(source))
        throw new Error(`SEO: ${route} lacks explicit route metadata`);
      if (!/\btitle\s*:/.test(source) || !/\bdescription\s*:/.test(source))
        throw new Error(`SEO: ${route} lacks title/description metadata`);
      const canonical = source.match(/alternates\s*:\s*\{\s*canonical\s*:\s*["']([^"']+)["']/)?.[1];
      if (canonical !== route)
        throw new Error(`SEO: ${route} has canonical ${canonical ?? "MISSING"}`);
      routes.push(route);
    }
  }
}
walk(app);
const sitemap = readFileSync(join(root, "public/sitemap.xml"), "utf8");
const locs = [...sitemap.matchAll(/<loc>([^<]+)<\/loc>/g)].map(match=>match[1]);
const expected = routes.map(route => origin + (route === "/" ? "/" : route));
if (new Set(locs).size !== locs.length) throw new Error("SEO: sitemap contains duplicate URLs");
const missing = expected.filter(url => !locs.includes(url));
const extra = locs.filter(url => !expected.includes(url));
if (missing.length || extra.length)
  throw new Error(`SEO: sitemap mismatch. Missing: ${missing.join(", ") || "none"}; extra: ${extra.join(", ") || "none"}`);
const robots = readFileSync(join(root, "public/robots.txt"), "utf8");
if (!robots.includes("Sitemap: " + origin + "/sitemap.xml"))
  throw new Error("SEO: robots.txt sitemap location does not match canonical origin");
const llms = readFileSync(join(root, "public/llms.txt"), "utf8");
for (const route of routes.filter(route => route.startsWith("/tools") || route.startsWith("/guides"))) {
  if (!llms.includes(origin + route))
    throw new Error(`SEO: llms.txt does not link to ${route}`);
}
console.log(`SEO checks passed for ${routes.length} canonical routes on ${origin}`);
