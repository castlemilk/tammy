import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { verifyRetainedOutput } from "./check-retained-output.mjs";

const ROOT = fileURLToPath(new URL("../", import.meta.url));

async function fixture(context) {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-retained-output-"));
  context.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(path.join(root, "output"));
  await mkdir(path.join(root, "compliance"));
  const bytes = '{"historical":true}';
  const entry = {
    path: "output/archived.json",
    sha256: createHash("sha256").update(bytes).digest("hex"),
  };
  const manifest = { version: 1, captureBaseRevision: "a".repeat(40), entries: [entry] };
  const config = { files: { includes: ["**", "!output/archived.json"] } };
  await writeFile(path.join(root, entry.path), bytes);
  await writeFile(
    path.join(root, "compliance/retained-output-lint.json"),
    JSON.stringify(manifest),
  );
  await writeFile(path.join(root, "biome.json"), JSON.stringify(config));
  return { root, manifest, config };
}

test("verifies retained bytes without reformatting historical evidence", async (context) => {
  const { root } = await fixture(context);
  assert.equal(await verifyRetainedOutput({ root }), 1);
  assert.equal(
    await readFile(path.join(root, "output/archived.json"), "utf8"),
    '{"historical":true}',
  );
});

test("even a semantically identical formatting change fails the byte guard", async (context) => {
  const { root } = await fixture(context);
  await writeFile(path.join(root, "output/archived.json"), '{\n  "historical": true\n}\n');
  await assert.rejects(verifyRetainedOutput({ root }), /RETAINED_OUTPUT_BYTES_CHANGED/);
});

test("missing retained evidence fails rather than silently removing its coverage", async (context) => {
  const { root } = await fixture(context);
  await rm(path.join(root, "output/archived.json"));
  await assert.rejects(verifyRetainedOutput({ root }), { code: "ENOENT" });
});

test("rejects broad output exclusions and missing exact exclusions", async (context) => {
  const { root, config } = await fixture(context);
  for (const exclusions of [["!output/**"], ["!**/output/**"], []]) {
    config.files.includes = ["**", ...exclusions];
    await writeFile(path.join(root, "biome.json"), JSON.stringify(config));
    await assert.rejects(verifyRetainedOutput({ root }), /RETAINED_OUTPUT_LINT_SCOPE_MISMATCH/);
  }
});

test("retained entries must be unique literal output paths with hashes", async (context) => {
  const { root, manifest } = await fixture(context);
  const original = structuredClone(manifest.entries[0]);
  for (const entries of [
    [{ ...original, path: "output/**" }],
    [{ ...original, path: "output/../source.ts" }],
    [{ ...original, path: "scripts/source.mjs" }],
    [{ ...original, sha256: "not-a-hash" }],
    [original, original],
  ]) {
    manifest.entries = entries;
    await writeFile(
      path.join(root, "compliance/retained-output-lint.json"),
      JSON.stringify(manifest),
    );
    await assert.rejects(verifyRetainedOutput({ root }), /RETAINED_OUTPUT_MANIFEST_INVALID/);
  }
});

test("the real retained inventory verifies and the lint front door always checks it", async () => {
  assert.equal(await verifyRetainedOutput({ root: ROOT }), 55);
  const manifest = JSON.parse(await readFile(path.join(ROOT, "package.json"), "utf8"));
  assert.equal(manifest.scripts.lint, "node scripts/check-retained-output.mjs && biome check .");
});

test("new maintained code and JSON under output remain subject to Biome", async (context) => {
  const biomeEntry = path.join(ROOT, "node_modules/@biomejs/biome/bin/biome");
  const directory = await mkdtemp(path.join(ROOT, "output/lint-policy-fixture-"));
  context.after(() => rm(directory, { recursive: true, force: true }));
  for (const [name, invalid, valid, options] of [
    [
      "new-maintained-example.ts",
      'const value = 1;\nif (value == "1") {\n  console.log(value);\n}\n',
      'const value = 1;\nif (value === "1") {\n  console.log(value);\n}\n',
      ["--formatter-enabled=false", "--assist-enabled=false"],
    ],
    ["new-capture.json", '{"value":true}', '{\n  "value": true\n}\n', []],
  ]) {
    const file = path.join(directory, name);
    for (const [input, expectedStatus] of [
      [invalid, 1],
      [valid, 0],
    ]) {
      await writeFile(file, input);
      const result = spawnSync(process.execPath, [biomeEntry, "check", file, ...options], {
        cwd: ROOT,
        encoding: "utf8",
        timeout: 10_000,
        maxBuffer: 256 * 1024,
        windowsHide: true,
      });
      assert.ifError(result.error);
      assert.equal(result.status, expectedStatus, result.stdout + result.stderr);
    }
  }
});
