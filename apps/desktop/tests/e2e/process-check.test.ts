import { describe, expect, it, vi } from "vitest";

import {
  type MacOSProcessTreeAuthority,
  observeAuthenticatedMacOSProcessTree,
  type ProcessQueryExecFile,
} from "./process-check";

const sha = (digit: string): string => digit.repeat(64);
const app = "/Applications/Tammy.app/Contents/MacOS/Tammy";
const helper =
  "/Applications/Tammy.app/Contents/Frameworks/Tammy Helper.app/Contents/MacOS/Tammy Helper";
const core = "/Applications/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core";

const authority: MacOSProcessTreeAuthority = {
  executables: { [app]: sha("a"), [core]: sha("c"), [helper]: sha("b") },
  rootExecutable: app,
  rootProcessId: 100,
};

function query(stdout: string): ProcessQueryExecFile {
  return vi.fn((_command, _arguments, _options, callback) => callback(null, stdout, ""));
}

describe("macOS process-tree evidence", () => {
  it("authenticates only the pinned root and descendants", async () => {
    const execFile = query(
      `  100  50 ${app}\n  101 100 ${helper}\n  102 100 ${core}\n  900   1 /usr/bin/unrelated\n`,
    );
    const snapshot = await observeAuthenticatedMacOSProcessTree(authority, new Map(), {
      authenticateExecutable: async (executablePath) => authority.executables[executablePath] ?? "",
      execFile,
    });

    expect(execFile).toHaveBeenCalledWith(
      "/bin/ps",
      ["-axo", "pid=,ppid=,comm="],
      expect.objectContaining({ maxBuffer: 64 * 1024, shell: false }),
      expect.any(Function),
    );
    expect(snapshot.processes.map(({ processId }) => processId)).toEqual([100, 101, 102]);
    expect(snapshot.processPaths).toEqual([app, helper, core].sort());
  });

  it("rejects an unpinned child and a same-pid executable replacement", async () => {
    await expect(
      observeAuthenticatedMacOSProcessTree(authority, new Map(), {
        authenticateExecutable: async () => sha("a"),
        execFile: query(`100 50 ${app}\n101 100 /tmp/replacement\n`),
      }),
    ).rejects.toThrow("UNPINNED_PROCESS_EXECUTABLE");

    const pins = new Map([[100, { executablePath: helper, sha256: sha("b") }]]);
    await expect(
      observeAuthenticatedMacOSProcessTree(authority, pins, {
        authenticateExecutable: async () => sha("a"),
        execFile: query(`100 50 ${app}\n`),
      }),
    ).rejects.toThrow("PROCESS_TREE_INSTANCE_CHANGED");
  });
});
