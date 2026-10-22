import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { stringify } from "yaml";

export const TRANSITION_FIXTURES = Object.freeze([
  "test/fixtures/proto/transitions.pb.json",
  "test/fixtures/sales/transitions.pb.json",
  "test/fixtures/payables/transitions.pb.json",
  "test/fixtures/documents/transitions.pb.json",
  "test/fixtures/banking/transitions.pb.json",
  "test/fixtures/reporting/transitions.pb.json",
  "test/fixtures/tax/transitions.pb.json",
]);

async function readFixture(file) {
  try {
    return JSON.parse(await readFile(file, "utf8"));
  } catch (error) {
    if (error?.code === "ENOENT") return undefined;
    throw new Error("TRANSITION_FIXTURE_INVALID");
  }
}

export async function buildTransitionIndex({ root = process.cwd() } = {}) {
  const entries = [];
  for (const fixturePath of TRANSITION_FIXTURES) {
    const fixture = await readFixture(path.join(root, fixturePath));
    if (fixture === undefined) continue;
    if (!Array.isArray(fixture.transitions)) {
      throw new Error("TRANSITION_FIXTURE_INVALID");
    }
    for (const transition of fixture.transitions) {
      if (typeof transition?.enum !== "string" || typeof transition?.transition !== "string") {
        throw new Error("TRANSITION_FIXTURE_INVALID");
      }
      entries.push([
        `${transition.enum}.${transition.transition}`,
        {
          enum: transition.enum,
          transition: transition.transition,
        },
      ]);
    }
  }
  entries.sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0));
  return { schemaVersion: 1, transitions: Object.fromEntries(entries) };
}

export async function writeTransitionIndex({ root = process.cwd() } = {}) {
  const output = path.join(root, "test/e2e/transitions.yaml");
  await mkdir(path.dirname(output), { recursive: true });
  await writeFile(output, stringify(await buildTransitionIndex({ root })), "utf8");
}

export async function checkTransitionIndex({ root = process.cwd() } = {}) {
  const output = path.join(root, "test/e2e/transitions.yaml");
  let retained;
  try {
    retained = await readFile(output, "utf8");
  } catch {
    throw new Error("TRANSITION_INDEX_DRIFT");
  }
  const generated = stringify(await buildTransitionIndex({ root }));
  if (retained !== generated) {
    throw new Error("TRANSITION_INDEX_DRIFT");
  }
}

export function transitionIndexMode(args) {
  if (args.length === 1 && args[0] === "--write") return "write";
  if (args.length === 1 && args[0] === "--check") return "check";
  throw new Error("TRANSITION_INDEX_MODE_REQUIRED");
}

async function main() {
  const mode = transitionIndexMode(process.argv.slice(2));
  if (mode === "write") {
    await writeTransitionIndex();
  } else {
    await checkTransitionIndex();
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
