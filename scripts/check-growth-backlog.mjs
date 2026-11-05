#!/usr/bin/env node
/**
 * Dependency-free validation for Tammy's proposed growth backlog and SEO map.
 * This checks the planning data and dependency cycles, not the live site,
 * product release readiness, search rankings, or user traffic.
 */
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const read = (name) => readFileSync(resolve(root, "docs/roadmap", name), "utf8");

function parseCSV(source) {
  const rows = [];
  let row = [], field = "", quoted = false;
  for (let i = 0; i < source.length; i++) {
    const ch = source[i];
    if (quoted) {
      if (ch === '"' && source[i + 1] === '"') { field += '"'; i++; }
      else if (ch === '"') quoted = false;
      else field += ch;
    } else if (ch === '"') {
      if (field !== "") throw new Error("Unexpected quote in CSV field");
      quoted = true;
    } else if (ch === ",") {
      row.push(field); field = "";
    } else if (ch === "\n") {
      row.push(field.replace(/\r$/, ""));
      if (row.some((value) => value !== "")) rows.push(row);
      row = []; field = "";
    } else {
      field += ch;
    }
  }
  if (quoted) throw new Error("Unclosed CSV quote");
  if (row.length || field) {
    row.push(field);
    rows.push(row);
  }
  return rows;
}
function table(name, columns) {
  const [header, ...data] = parseCSV(read(name));
  if (JSON.stringify(header) !== JSON.stringify(columns))
    throw new Error(name + ": column drift " + JSON.stringify(header));
  return data.map((fields, index) => {
    if (fields.length !== columns.length)
      throw new Error(name + ": row " + (index + 2) + " has " + fields.length + " fields");
    return Object.fromEntries(header.map((field, i) => [field, fields[i]]));
  });
}
const backlog = table("growth-backlog-2026-11-05.csv", [
  "id", "epic_issue", "priority", "milestone", "workstream",
  "deliverable", "dependency", "acceptance", "primary_metric", "status",
]);
if (backlog.length < 40) throw new Error("Growth backlog has unexpectedly lost scope");
const issues = new Set(["#32", "#33", "#34", "#35", "#36", "#37", "#38"]);
const ids = new Set(backlog.map((item) => item.id));
if (ids.size !== backlog.length) throw new Error("Duplicate growth IDs");

const dependencies = new Map();
for (const item of backlog) {
  if (!/^GT-\d{3}$/.test(item.id)) throw new Error("Invalid growth ID " + item.id);
  if (!issues.has(item.epic_issue)) throw new Error("Unknown epic " + item.id);
  if (!new Set(["P0", "P1", "P2"]).has(item.priority)) throw new Error("Invalid priority " + item.id);
  if (!/^M[0-4]$/.test(item.milestone)) throw new Error("Invalid milestone " + item.id);
  if (item.status !== "proposed") throw new Error("Growth task incorrectly claims release: " + item.id);
  for (const field of ["workstream", "deliverable", "dependency", "acceptance", "primary_metric"]) {
    if (item[field].trim().length < 4)
      throw new Error("Missing or vague " + field + " in " + item.id);
  }
  const refs = [...item.dependency.matchAll(/GT-\d{3}/g)].map((x) => x[0]);
  for (const ref of refs) {
    if (!ids.has(ref)) throw new Error(item.id + ": unknown dependency " + ref);
    if (ref === item.id) throw new Error(item.id + ": circular self dependency");
  }
  dependencies.set(item.id, refs);
}
const visited = new Set(), walking = new Set();
function visit(id) {
  if (walking.has(id)) throw new Error("Growth dependency cycle involving " + id);
  if (visited.has(id)) return;
  walking.add(id);
  for (const dep of dependencies.get(id) ?? []) visit(dep);
  walking.delete(id);
  visited.add(id);
}
for (const id of ids) visit(id);

const seo = table("seo-content-map-2026-11-05.csv", [
  "id", "cluster", "intent", "query_theme", "primary_route", "page_type",
  "source_status", "related_route", "conversion_stage", "priority", "notes",
]);
if (seo.length < 25) throw new Error("SEO intent map unexpectedly lost coverage");
const seoIds = new Set(), status = new Set(["source_draft", "proposed"]);
const routeOwners = new Map();
for (const page of seo) {
  if (!/^SEO-\d{2}$/.test(page.id) || seoIds.has(page.id))
    throw new Error("Invalid or duplicated SEO ID " + page.id);
  seoIds.add(page.id);
  if (!status.has(page.source_status) ||
      !new Set(["P0", "P1", "P2"]).has(page.priority)) throw new Error("Invalid SEO status " + page.id);
  if (!/^\/[a-z0-9/-]*$/.test(page.primary_route) ||
      !/^\/[a-z0-9/-]*$/.test(page.related_route))
    throw new Error("SEO plan uses non-path or unescaped URL " + page.id);
  for (const field of ["cluster", "intent", "query_theme", "page_type", "conversion_stage", "notes"]) {
    if (page[field].trim().length < 4) throw new Error("Missing " + field + " in " + page.id);
  }
  const oldOwner = routeOwners.get(page.primary_route);
  // Multiple query phrases may intentionally share one URL, but cannot assign
  // the same URL to conflicting business domains.
  if (oldOwner && oldOwner !== page.cluster) throw new Error("SEO route conflict " + page.primary_route);
  routeOwners.set(page.primary_route, page.cluster);
}
const plan = read("free-tools-funnel-roadmap-2026-11-05.md");
const seoPlan = read("seo-strategy-2026-11-05.md");
const metrics = read("growth-measurement-2026-11-05.md");
for (const file of [plan, seoPlan, metrics]) {
  if (!file.includes("tammy.tax") || !file.includes("Search Console"))
    throw new Error("Planning documents omit canonical/measurement boundary");
}
for (const id of issues) if (!plan.includes("/issues/" + id.slice(1)))
  throw new Error("Growth strategy missing epic link " + id);
if (!seoPlan.includes("./seo-content-map-2026-11-05.csv"))
  throw new Error("SEO plan missing query intent map link");
console.log("Growth plan validated:", backlog.length, "tasks,", seo.length,
  "SEO intent rows,", issues.size, "epics, no circular GT dependencies");
