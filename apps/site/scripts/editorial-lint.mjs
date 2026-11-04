import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const site = join(dirname(fileURLToPath(import.meta.url)), "..");
const contentRoot = join(site, "content", "blog");
const entries = readdirSync(contentRoot).filter((file) => file.endsWith(".mdx")).sort();
const source = readFileSync(join(site, "lib/blog/posts.ts"), "utf8");
const sitemap = readFileSync(join(site, "public/sitemap.xml"), "utf8");
const feed = readFileSync(join(site, "public/rss.xml"), "utf8");
const llms = readFileSync(join(site, "public/llms.txt"), "utf8");
const origin = readFileSync(join(site, "lib/site-config.ts"), "utf8")
  .match(/SITE_ORIGIN\s*=\s*"([^"]+)"/)?.[1];
const failures = [];
const banned = [
  /in today's (?:fast-paced|ever-changing) world/i,
  /in an ever-evolving landscape/i,
  /unlock (?:the power|your potential)/i,
  /(?:a )?game[- ]changer/i,
  /seamlessly (?:integrate|manage|navigate)/i,
  /it's not just .{1,120}, it's /i,
  /delve into the (?:world|complexities)/i,
  /empower (?:your|businesses|users)/i,
  /in conclusion[,.:]/i,
  /whether you're .{10,120} or .{5,120}/i,
];
for (const filename of entries) {
  const slug = filename.slice(0, -4);
  const mdx = readFileSync(join(contentRoot, filename), "utf8");
  const route = "/blog/" + slug;
  const failuresForFile = [];
  if (!source.includes('slug: "' + slug + '"')) failuresForFile.push("missing blog inventory entry");
  if (!sitemap.includes((origin ?? "") + route + "</loc>")) failuresForFile.push("missing canonical sitemap entry");
  if (!feed.includes((origin ?? "") + route + "</link>")) failuresForFile.push("missing RSS item");
  if (!llms.includes((origin ?? "") + route)) failuresForFile.push("missing llms.txt link");
  if (!existsSync(join(site, "app/blog", slug, "page.tsx")))
    failuresForFile.push("missing explicit article route");
  const body = mdx
    .replace(/<[^>]*>/g, " ")
    .replace(/https?:\/\/[^\s]+/g, " ")
    .replace(/[\{\}\[\]"']/g, " ");
  const words = body.match(/[\w'-]+/g)?.length ?? 0;
  if (words < 350) failuresForFile.push("too thin: " + words + " words (minimum 350)");
  const sections = [...mdx.matchAll(/<h2 id="([a-z0-9-]+)">/g)].map((m) => m[1]);
  if (sections.length < 3 || new Set(sections).size !== sections.length)
    failuresForFile.push("needs 3+ unique explicit h2 section IDs");
  if (!/<SourceNote\s/.test(mdx)) failuresForFile.push("needs an attributed SourceNote");
  if (!/<(?:MoneyTable|BarChart|Formula)\s/.test(mdx))
    failuresForFile.push("needs one auditable numeric block");
  if (!/<ToolLink\s/.test(mdx)) failuresForFile.push("needs a real linked Tammy tool");
  if (!/illustrative|fictional|assumptions|hypothetical/i.test(mdx))
    failuresForFile.push("example assumptions must be labelled");
  if (!/https:\/\/(?:www\.)?(?:ato\.gov\.au|business\.gov\.au|business\.vic\.gov\.au)/i.test(mdx))
    failuresForFile.push("requires a relevant official primary source");
  for (const [index, pattern] of banned.entries()) {
    if (pattern.test(mdx)) failuresForFile.push("editorial cliché pattern " + (index + 1));
  }
  for (const match of mdx.matchAll(/<Figure[^>]+src="(\/assets\/[a-z0-9-]+\.svg)"/g)) {
    if (!existsSync(join(site, "public", match[1])))
      failuresForFile.push("missing local illustration " + match[1]);
  }
  if (failuresForFile.length) failures.push({ filename, issues: failuresForFile });
  else console.log("Editorial OK:", filename, "(" + words + " words, " + sections.length + " sections)");
}

if (entries.length < 1) failures.push({ filename: "<directory>", issues: ["no MDX posts discovered"] });
for (const item of failures) {
  console.error("Editorial FAIL:", item.filename);
  for (const error of item.issues) console.error("  - " + error);
}
if (failures.length) process.exitCode = 1;
else console.log("Editorial quality checks passed for " + entries.length + " MDX posts.");
