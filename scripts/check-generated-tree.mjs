import { execFile } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

export async function checkGeneratedTree({ root = process.cwd(), run = execFileAsync } = {}) {
  const { stdout } = await run(
    "git",
    [
      "status",
      "--porcelain=v1",
      "--untracked-files=all",
      "--",
      "services/core/internal/gen",
      "packages/connect-client/src/gen",
    ],
    { cwd: root, shell: false },
  );
  if (String(stdout).trim().length > 0) {
    throw new Error("GENERATED_TREE_DRIFT");
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await checkGeneratedTree();
}
