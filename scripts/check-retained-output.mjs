import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { readFile } from "node:fs/promises";
import path from "node:path";

export async function verifyRetainedOutput({ root = process.cwd() } = {}) {
  const manifest = JSON.parse(
    await readFile(path.join(root, "compliance/retained-output-lint.json"), "utf8"),
  );
  if (
    manifest.version !== 1 ||
    !/^[0-9a-f]{40}$/.test(manifest.captureBaseRevision) ||
    !Array.isArray(manifest.entries) ||
    manifest.entries.length === 0
  ) {
    throw new Error("RETAINED_OUTPUT_MANIFEST_INVALID");
  }
  const paths = new Set();
  for (const entry of manifest.entries) {
    if (
      typeof entry.path !== "string" ||
      !entry.path.startsWith("output/") ||
      entry.path.split("/").some((segment) => !segment || segment === "." || segment === "..") ||
      /[\\*?{}[\]!]/.test(entry.path) ||
      !/^[0-9a-f]{64}$/.test(entry.sha256) ||
      paths.has(entry.path)
    ) {
      throw new Error("RETAINED_OUTPUT_MANIFEST_INVALID");
    }
    paths.add(entry.path);
  }
  const config = JSON.parse(await readFile(path.join(root, "biome.json"), "utf8"));
  const outputExclusions = config.files.includes.filter(
    (pattern) => pattern.startsWith("!") && /(^|\/)output(\/|$)/.test(pattern.slice(1)),
  );
  const expected = [...paths].map((file) => `!${file}`).sort();
  if (JSON.stringify(outputExclusions.sort()) !== JSON.stringify(expected)) {
    throw new Error("RETAINED_OUTPUT_LINT_SCOPE_MISMATCH");
  }
  const attributes = await readFile(path.join(root, ".gitattributes"), "utf8");
  const protectedPaths = attributes
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.startsWith("output/") && line.endsWith(" -text"))
    .sort();
  if (
    JSON.stringify(protectedPaths) !==
    JSON.stringify([...paths].map((file) => `${file} -text`).sort())
  ) {
    throw new Error("RETAINED_OUTPUT_CHECKOUT_SCOPE_MISMATCH");
  }
  for (const entry of manifest.entries) {
    const hash = createHash("sha256");
    for await (const chunk of createReadStream(path.join(root, entry.path))) hash.update(chunk);
    if (hash.digest("hex") !== entry.sha256) {
      throw new Error(`RETAINED_OUTPUT_BYTES_CHANGED:${entry.path}`);
    }
  }
  return manifest.entries.length;
}

if (import.meta.main) {
  console.log(`Verified ${await verifyRetainedOutput()} byte-preserved archived output files.`);
}
