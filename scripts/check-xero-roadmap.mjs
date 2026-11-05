#!/usr/bin/env node
/* Minimal dependency-free validation for the competitive roadmap's CSV ledger.
 * Never fetch competitor pages or infer production status from this script.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const path = resolve(root, "docs/roadmap/xero-feature-inventory-2026-11-05.csv");
const source = readFileSync(path, "utf8");
const required = ["id", "domain", "capability", "xero_scope", "tammy_status",
  "priority", "gate", "dependency", "evidence_url", "notes"];
const status = new Set(["connected_dev", "partial_dev", "core_only", "not_started", "external_gate"]);
const gates = new Map([["G0", "P0"], ["G1", "P0"], ["G2", "P0"],
  ["G3", "P1"], ["G4", "P2"], ["G5", "P3"]]);
const scopes = new Set(["core", "premium", "platform", "practice", "tammy_enabler"]);

function parseCSV(input) {
  const records = [];
  let fields = [], current = "", quoted = false;
  for (let i = 0; i < input.length; i++) {
    const char = input[i];
    if (quoted) {
      if (char === '"' && input[i + 1] === '"') { current += '"'; i++; }
      else if (char === '"') quoted = false;
      else current += char;
      continue;
    }
    if (char === '"') { if (current !== "") throw new Error("Unexpected quote"); quoted = true; }
    else if (char === ",") { fields.push(current); current = ""; }
    else if (char === "\n") { fields.push(current.replace(/\r$/, ""));
      if (fields.some((field) => field !== "")) records.push(fields);
      fields = []; current = "";
    } else current += char;
  }
  if (quoted) throw new Error("Unclosed CSV quote");
  if (current || fields.length) {
    fields.push(current);
    records.push(fields);
  }
  return records;
}
const [header, ...records] = parseCSV(source);
if (JSON.stringify(header) !== JSON.stringify(required))
  throw new Error("Roadmap header/schema drift: " + JSON.stringify(header));
if (records.length < 200) throw new Error("Roadmap unexpectedly lost feature coverage: " + records.length);

const ids = new Set(), domains = new Set(), summary = new Map();
for (const [idx, row] of records.entries()) {
  if (row.length !== required.length) throw new Error("Row " + (idx + 2) + " has " + row.length + " columns");
  const record = Object.fromEntries(header.map((key, i) => [key, row[i]]));
  if (!/^[A-Z]+-\d{2,3}$/.test(record.id)) throw new Error("Invalid feature ID: " + record.id);
  if (ids.has(record.id)) throw new Error("Duplicate feature ID: " + record.id);
  ids.add(record.id);
  domains.add(record.domain);
  if (!record.capability.trim() || !record.notes.trim() || !record.dependency.trim())
    throw new Error("Unspecified item: " + record.id);
  if (!status.has(record.tammy_status) || !gates.has(record.gate) ||
      !scopes.has(record.xero_scope)) throw new Error("Unknown enum in " + record.id);
  if (gates.get(record.gate) !== record.priority)
    throw new Error("Priority and acceptance gate mismatch in " + record.id);
  if (!record.evidence_url.startsWith("https://"))
    throw new Error("Evidence link must be HTTPS: " + record.id);
  const key = record.tammy_status;
  summary.set(key, (summary.get(key) ?? 0) + 1);
}
if (domains.size < 18) throw new Error("Expected cross-product coverage, got " + domains.size + " domains");
const roadmap = readFileSync(resolve(root, "docs/roadmap/xero-replacement-roadmap.md"), "utf8");
for (const gate of gates.keys()) if (!roadmap.includes(gate)) throw new Error("Missing acceptance gate " + gate);
for (const heading of ["Xero", "migration", "backup", "payroll", "bank"]) {
  if (!roadmap.toLowerCase().includes(heading.toLowerCase()))
    throw new Error("Missing major roadmap narrative: " + heading);
}
console.log("Tammy Xero roadmap inventory validated:");
console.log("  Features: " + records.length + ", domains: " + domains.size);
console.log("  State: " + [...summary].map(([k,v]) => k + "=" + v).join(", "));
console.log("  All release gates, source fields, IDs and priority mappings consistent.");
