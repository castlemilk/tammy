import { rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { buildDescriptors } from "./build-descriptors.mjs";
import { checkTransitionIndex } from "./build-transition-index.mjs";
import { runE2ECoverage } from "./check-e2e-coverage.mjs";
import { checkGeneratedTree } from "./check-generated-tree.mjs";

export async function checkContracts({
  buildDescriptorsFn = buildDescriptors,
  checkGeneratedTreeFn = checkGeneratedTree,
  checkTransitionIndexFn = checkTransitionIndex,
  root = process.cwd(),
  runE2ECoverageFn = runE2ECoverage,
} = {}) {
  const temporaryDirectory = path.join(root, ".tmp/contracts");
  try {
    await rm(temporaryDirectory, { force: true, recursive: true });
    await buildDescriptorsFn({ mode: "validation", root });
    await checkGeneratedTreeFn({ root });
    await checkTransitionIndexFn({ root });
    await runE2ECoverageFn({
      descriptorPath: path.join(temporaryDirectory, "descriptors.pb"),
      root,
    });
  } finally {
    await rm(temporaryDirectory, { force: true, recursive: true });
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await checkContracts();
}
