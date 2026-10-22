import { execFile } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

export async function checkCleanTree(run = execFileAsync) {
  const { stdout } = await run("git", ["status", "--porcelain=v1", "--untracked-files=all"], {
    cwd: process.cwd(),
    shell: false,
  });
  if (String(stdout).length === 0) {
    return;
  }

  const paths = String(stdout)
    .trimEnd()
    .split("\n")
    .map((line) => line.slice(3));
  const error = new Error(`DIRTY_WORKTREE\n${paths.join("\n")}`);
  error.code = "DIRTY_WORKTREE";
  error.paths = paths;
  throw error;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    await checkCleanTree();
  } catch (error) {
    if (error.code === "DIRTY_WORKTREE") {
      process.stderr.write(`${error.message}\n`);
      process.exitCode = 1;
    } else {
      throw error;
    }
  }
}
