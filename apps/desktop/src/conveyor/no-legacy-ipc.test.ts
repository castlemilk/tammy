import { readdir, readFile } from "node:fs/promises";
import path from "node:path";

import { expect, it } from "vitest";

const FORBIDDEN = [
  /window\.tammy\b/u,
  /\bTammyDesktopAPI\b/u,
  /\bDesktopRpcRouter\b/u,
  /tammy:system-diagnostics/u,
  /tammy:workspace-create/u,
  /preload-methods\.json/u,
  /registerDesktopIpc/u,
] as const;
const EXECUTABLE_EXTENSIONS = new Set([
  ".cjs",
  ".js",
  ".json",
  ".mjs",
  ".ts",
  ".tsx",
  ".yaml",
  ".yml",
]);
const SKIPPED_DIRECTORIES = new Set([
  ".git",
  ".tmp",
  ".vite",
  "node_modules",
  "out",
  "test-results",
]);

async function executableFiles(root: string): Promise<string[]> {
  const entries = await readdir(root, { withFileTypes: true });
  const files: string[] = [];
  for (const entry of entries) {
    if (entry.isDirectory()) {
      if (SKIPPED_DIRECTORIES.has(entry.name)) continue;
      files.push(...(await executableFiles(path.join(root, entry.name))));
      continue;
    }
    if (EXECUTABLE_EXTENSIONS.has(path.extname(entry.name))) {
      files.push(path.join(root, entry.name));
    }
  }
  return files;
}

it("has no legacy desktop IPC source or configuration references", async () => {
  const desktopRoot = path.resolve(import.meta.dirname, "../..");
  const testFile = path.resolve(import.meta.dirname, "no-legacy-ipc.test.ts");
  const hits: string[] = [];
  for (const file of await executableFiles(desktopRoot)) {
    if (path.resolve(file) === testFile) continue;
    const source = await readFile(file, "utf8");
    for (const pattern of FORBIDDEN) {
      if (pattern.test(source)) hits.push(`${path.relative(desktopRoot, file)}:${pattern}`);
    }
  }
  expect(hits).toEqual([]);
});
