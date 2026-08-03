import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";

test("generated tree checker rejects tracked or untracked drift", async () => {
  const calls = [];
  const run = async (command, args, options) => {
    calls.push({ command, args, options });
    return { stdout: " M packages/connect-client/src/gen/tammy/v1/system_pb.ts\n" };
  };
  const { checkGeneratedTree } = await import("./check-generated-tree.mjs");
  const root = path.resolve("/workspace/tammy");

  await assert.rejects(checkGeneratedTree({ root, run }), {
    message: "GENERATED_TREE_DRIFT",
  });
  assert.deepEqual(calls[0], {
    command: "git",
    args: [
      "status",
      "--porcelain=v1",
      "--untracked-files=all",
      "--",
      "services/core/internal/gen",
      "packages/connect-client/src/gen",
    ],
    options: { cwd: root, shell: false },
  });
});
