import { execFile } from "node:child_process";
import { lstat, mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { createBuildPlan, loadSqlcipherBuildInput, selectTarget } from "./build-core.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const target = selectTarget(process.platform, process.arch);
const cipher = await loadSqlcipherBuildInput(root, target);
const plan = createBuildPlan({ root, target, sqlcipher: cipher, version: "0.1.0" });
const directory = path.join(root, ".tmp", "purchase-e2e-inspector");
await mkdir(directory, { recursive: true, mode: 0o700 });
const info = await lstat(directory);
if (!info.isDirectory() || info.isSymbolicLink()) throw new Error("INVALID_INSPECTOR_DIRECTORY");
const output = path.join(directory, process.platform === "win32" ? "inspect.exe" : "inspect");
await promisify(execFile)(
  "go",
  [
    "build",
    "-p",
    "1",
    "-trimpath",
    "-tags=tammy_sqlcipher",
    "-o",
    output,
    "./services/core/testsupport/purchase-e2e-inspector",
  ],
  { ...plan.options, maxBuffer: 1024 * 1024 },
);
