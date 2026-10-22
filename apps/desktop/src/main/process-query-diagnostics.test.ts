// @vitest-environment node

import type { ExecFileException } from "node:child_process";
import { describe, expect, it, vi } from "vitest";

import {
  findAuthenticatedCoreProcesses,
  findExactCoreProcesses,
  type ProcessQueryExecFile,
} from "../../tests/e2e/process-check";

const corePath = "/Applications/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core";
const authority = { coreSha256: "a".repeat(64), packagedExecutable: corePath };
const privateDiagnostic = "/private/user/core-db credential=must-not-escape";

type QueryResult =
  | { readonly kind: "success"; readonly stdout: string }
  | { readonly kind: "error"; readonly error: ExecFileException };

interface ProcessQueryFailureCase {
  readonly check: (execFile: ProcessQueryExecFile) => Promise<unknown>;
  readonly expected: string;
  readonly label: string;
  readonly results: readonly QueryResult[];
}

function queryError(
  properties: Partial<Pick<ExecFileException, "code" | "killed" | "signal">>,
): ExecFileException {
  return Object.assign(new Error(privateDiagnostic), properties) as ExecFileException;
}

function scriptedRunner(...results: readonly QueryResult[]): ProcessQueryExecFile {
  let index = 0;
  return vi.fn<ProcessQueryExecFile>((_command, _arguments, _options, callback) => {
    const result = results[index++];
    if (result === undefined) throw new Error("UNEXPECTED_PROCESS_QUERY");
    if (result.kind === "success") callback(null, result.stdout, privateDiagnostic);
    else callback(result.error, "", privateDiagnostic);
    return {};
  });
}

const processQueryFailureCases: readonly ProcessQueryFailureCase[] = [
  {
    label: "pgrep nonzero exit",
    expected: "PROCESS_QUERY_FAILED: PGREP_EXIT_NONZERO",
    check: (execFile) => findExactCoreProcesses(corePath, "darwin", { execFile }),
    results: [{ kind: "error", error: queryError({ code: 7 }) }],
  },
  {
    label: "ps nonzero exit",
    expected: "PROCESS_QUERY_FAILED: PS_EXIT_NONZERO",
    check: (execFile) =>
      findAuthenticatedCoreProcesses(authority, new Map(), "darwin", { execFile }),
    results: [
      { kind: "success", stdout: "41\n" },
      { kind: "error", error: queryError({ code: 7 }) },
    ],
  },
  {
    label: "lsof nonzero exit",
    expected: "PROCESS_QUERY_FAILED: LSOF_EXIT_NONZERO",
    check: (execFile) =>
      findAuthenticatedCoreProcesses(authority, new Map(), "darwin", { execFile }),
    results: [
      { kind: "success", stdout: "41\n" },
      { kind: "success", stdout: "instance-a\n" },
      { kind: "error", error: queryError({ code: 7 }) },
    ],
  },
  {
    label: "lsof signal termination",
    expected: "PROCESS_QUERY_FAILED: LSOF_SIGNAL",
    check: (execFile) =>
      findAuthenticatedCoreProcesses(authority, new Map(), "darwin", { execFile }),
    results: [
      { kind: "success", stdout: "41\n" },
      { kind: "success", stdout: "instance-a\n" },
      { kind: "error", error: queryError({ killed: true, signal: "SIGTERM" }) },
    ],
  },
  {
    label: "lsof timeout",
    expected: "PROCESS_QUERY_FAILED: LSOF_TIMEOUT",
    check: (execFile) =>
      findAuthenticatedCoreProcesses(authority, new Map(), "darwin", { execFile }),
    results: [
      { kind: "success", stdout: "41\n" },
      { kind: "success", stdout: "instance-a\n" },
      {
        kind: "error",
        error: queryError({ killed: true, signal: "SIGKILL" }),
      },
    ],
  },
];

describe("macOS process-query failure diagnostics", () => {
  it.each(processQueryFailureCases)(
    "reports only safe operation and failure class for $label",
    async ({ check, expected, results }) => {
      const execFile = scriptedRunner(...results);
      const rejection = await check(execFile).then(
        () => undefined,
        (error: unknown) => error,
      );

      expect(rejection).toBeInstanceOf(Error);
      expect((rejection as Error).message).toBe(expected);
      expect((rejection as Error).message).not.toContain(privateDiagnostic);
      expect((rejection as Error).message).not.toContain("41");
      expect((rejection as Error).message).not.toContain("SIGTERM");
      expect(execFile).toHaveBeenCalledTimes(results.length);
    },
  );

  it("keeps the existing pgrep timeout category while adding a fixed safe operation marker", async () => {
    const execFile = scriptedRunner({
      kind: "error",
      error: queryError({ killed: true, signal: "SIGKILL" }),
    });

    const rejection = await findExactCoreProcesses(corePath, "darwin", { execFile }).then(
      () => undefined,
      (error: unknown) => error,
    );

    expect(rejection).toBeInstanceOf(Error);
    expect((rejection as Error).message).toBe("PROCESS_QUERY_TIMEOUT: PGREP_TIMEOUT");
    expect((rejection as Error).message).not.toContain(privateDiagnostic);
  });
});
