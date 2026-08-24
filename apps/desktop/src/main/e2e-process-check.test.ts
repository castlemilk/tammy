// @vitest-environment node

import { createHash } from "node:crypto";
import { chmod, mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it, vi } from "vitest";

import {
  findAuthenticatedStagedHelperProcesses,
  type StagedHelperAuthority,
  sampleAuthenticatedStagedHelperSockets,
} from "../../tests/e2e/process-check";

type Callback = (
  error: (Error & { code?: number | string }) | null,
  stdout: string,
  stderr: string,
) => void;

function runner(results: readonly { error?: Error & { code?: number }; stdout?: string }[]) {
  let index = 0;
  return vi.fn((_: string, __: readonly string[], ___: unknown, callback: Callback) => {
    const result = results[index++] ?? {};
    callback(result.error ?? null, result.stdout ?? "", "");
    return {};
  });
}

function executableImage(processId: number, executablePath: string) {
  return { stdout: `p${processId}\nftxt\nn${executablePath}\n` };
}

function executableImageWithDyld(processId: number, executablePath: string) {
  return {
    stdout: `p${processId}\nftxt\nn${executablePath}\nftxt\nn/usr/lib/dyld\n`,
  };
}

const temporaryRoots: string[] = [];

async function fixture(): Promise<{
  readonly authority: StagedHelperAuthority;
  readonly bytes: Buffer;
  readonly stagedPath: string;
}> {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-helper-observer-"));
  temporaryRoots.push(root);
  const packagedPath = path.join(
    root,
    "Tammy.app/Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
  );
  const trustedRuntimeBase = path.join(root, "user-data/local-core/core/sbr-runtime");
  const stagedPath = path.join(
    trustedRuntimeBase,
    "tammy-sbr-runtime-0123456789abcdef01234567",
    "sbr-helper",
  );
  const bytes = Buffer.from("authenticated packaged helper");
  await mkdir(path.dirname(packagedPath), { recursive: true });
  await mkdir(path.dirname(stagedPath), { recursive: true });
  await writeFile(packagedPath, bytes, { mode: 0o500 });
  await writeFile(stagedPath, bytes, { mode: 0o500 });
  return {
    authority: {
      helperSha256: createHash("sha256").update(bytes).digest("hex"),
      packagedExecutable: packagedPath,
      trustedRuntimeBases: [trustedRuntimeBase],
    },
    bytes,
    stagedPath,
  };
}

afterEach(async () => {
  await Promise.all(
    temporaryRoots.splice(0).map((root) => rm(root, { force: true, recursive: true })),
  );
});

describe("packaged SBR helper process observation", () => {
  it("pins an exact authenticated staged helper path and never matches by basename", async () => {
    const { authority, stagedPath } = await fixture();
    const execute = runner([
      { stdout: "51\n" },
      { stdout: `${stagedPath}\n` },
      executableImage(51, stagedPath),
    ]);
    const pinned = new Map<number, string>();

    await expect(
      findAuthenticatedStagedHelperProcesses(authority, pinned, { execFile: execute as never }),
    ).resolves.toEqual([{ executablePath: stagedPath, processId: 51 }]);
    expect(pinned).toEqual(new Map([[51, stagedPath]]));
    expect(execute).toHaveBeenNthCalledWith(
      1,
      "/usr/bin/pgrep",
      ["-f", "-x", expect.any(String)],
      expect.objectContaining({ shell: false }),
      expect.any(Function),
    );
    const query = execute.mock.calls[0]?.[1]?.[2];
    expect(typeof query).toBe("string");
    const processPattern = new RegExp(query ?? "", "u");
    expect(processPattern.test(stagedPath)).toBe(true);
    expect(processPattern.test("tammy-sbr-helper")).toBe(false);
    expect(processPattern.test(`/tmp/foreign/${path.basename(stagedPath)}`)).toBe(false);
  });

  it("pins the exact staged helper when lsof also reports the macOS dynamic loader", async () => {
    const { authority, stagedPath } = await fixture();
    const execute = runner([
      { stdout: "55\n" },
      { stdout: `${stagedPath}\n` },
      executableImageWithDyld(55, stagedPath),
    ]);
    const pinned = new Map<number, string>();

    await expect(
      findAuthenticatedStagedHelperProcesses(authority, pinned, { execFile: execute as never }),
    ).resolves.toEqual([{ executablePath: stagedPath, processId: 55 }]);
    expect(pinned).toEqual(new Map([[55, stagedPath]]));
  });

  it.each(["foreign root", "digest mismatch", "symlink"] as const)(
    "rejects a staged helper with %s",
    async (failureKind) => {
      const { authority, bytes, stagedPath } = await fixture();
      let observedPath = stagedPath;
      if (failureKind === "foreign root") {
        observedPath = path.join(
          path.dirname(path.dirname(authority.trustedRuntimeBases[0] ?? "")),
          "foreign/tammy-sbr-runtime-0123456789abcdef01234567/sbr-helper",
        );
        await mkdir(path.dirname(observedPath), { recursive: true });
        await writeFile(observedPath, bytes, { mode: 0o500 });
      } else if (failureKind === "digest mismatch") {
        await chmod(stagedPath, 0o700);
        await writeFile(stagedPath, "foreign helper", { mode: 0o500 });
        await chmod(stagedPath, 0o500);
      } else {
        const target = `${stagedPath}.real`;
        await writeFile(target, bytes, { mode: 0o500 });
        await rm(stagedPath);
        await symlink(target, stagedPath);
      }
      const execute = runner([
        { stdout: "52\n" },
        { stdout: `${observedPath}\n` },
        executableImage(52, observedPath),
      ]);

      await expect(
        findAuthenticatedStagedHelperProcesses(authority, new Map(), {
          execFile: execute as never,
        }),
      ).rejects.toThrow("UNAUTHENTICATED_STAGED_HELPER");
    },
  );

  it("rejects a PID whose pinned absolute staged path changes", async () => {
    const { authority, bytes, stagedPath } = await fixture();
    const secondPath = path.join(
      authority.trustedRuntimeBases[0] ?? "",
      "tammy-sbr-runtime-89abcdef0123456701234567/sbr-helper",
    );
    await mkdir(path.dirname(secondPath), { recursive: true });
    await writeFile(secondPath, bytes, { mode: 0o500 });
    const execute = runner([
      { stdout: "53\n" },
      { stdout: `${stagedPath}\n` },
      executableImage(53, stagedPath),
      { stdout: "53\n" },
      { stdout: `${secondPath}\n` },
      executableImage(53, secondPath),
    ]);
    const pinned = new Map<number, string>();

    await findAuthenticatedStagedHelperProcesses(authority, pinned, { execFile: execute as never });
    await expect(
      findAuthenticatedStagedHelperProcesses(authority, pinned, { execFile: execute as never }),
    ).rejects.toThrow("STAGED_HELPER_PATH_CHANGED");
  });

  it("rejects packaged source bytes outside the signed locator digest authority", async () => {
    const { authority } = await fixture();
    const execute = runner([]);

    await expect(
      findAuthenticatedStagedHelperProcesses(
        { ...authority, helperSha256: "0".repeat(64) },
        new Map(),
        { execFile: execute as never },
      ),
    ).rejects.toThrow("UNAUTHENTICATED_PACKAGED_HELPER");
    expect(execute).not.toHaveBeenCalled();
  });

  it("rejects forged argv when the macOS executable image is foreign", async () => {
    const { authority, stagedPath } = await fixture();
    const execute = runner([
      { stdout: "54\n" },
      { stdout: `${stagedPath}\n` },
      executableImage(54, "/tmp/foreign/sbr-helper"),
    ]);

    await expect(
      findAuthenticatedStagedHelperProcesses(authority, new Map(), {
        execFile: execute as never,
      }),
    ).rejects.toThrow("UNAUTHENTICATED_STAGED_HELPER");
  });

  it("treats empty lsof image output as an enumerated helper that exited", async () => {
    const { authority, stagedPath } = await fixture();
    const execute = runner([
      { stdout: "56\n" },
      { stdout: `${stagedPath}\n` },
      { stdout: "" },
    ]);
    const pinned = new Map<number, string>();

    await expect(
      findAuthenticatedStagedHelperProcesses(authority, pinned, {
        execFile: execute as never,
      }),
    ).resolves.toEqual([]);
    expect(pinned).toEqual(new Map());
  });

  it("still rejects non-empty malformed lsof image evidence", async () => {
    const { authority, stagedPath } = await fixture();
    const execute = runner([
      { stdout: "57\n" },
      { stdout: `${stagedPath}\n` },
      { stdout: "unexpected\n" },
    ]);

    await expect(
      findAuthenticatedStagedHelperProcesses(authority, new Map(), {
        execFile: execute as never,
      }),
    ).rejects.toThrow("INVALID_PROCESS_EVIDENCE");
  });

  it("counts one exact-path sample when lsof reports no TCP or UDP sockets", async () => {
    const { authority, stagedPath } = await fixture();
    const execute = runner([
      { stdout: "71\n" },
      { stdout: `${stagedPath}\n` },
      executableImage(71, stagedPath),
      { error: Object.assign(new Error("no sockets"), { code: 1 }) },
    ]);

    await expect(
      sampleAuthenticatedStagedHelperSockets(authority, new Map(), {
        execFile: execute as never,
      }),
    ).resolves.toEqual({ processIds: [71], samples: 1, violations: 0 });
    expect(execute.mock.calls[3]?.slice(0, 2)).toEqual([
      "/usr/sbin/lsof",
      ["-nP", "-a", "-p", "71", "-iTCP", "-iUDP"],
    ]);
  });

  it("rejects any listening or connected helper socket as a violation", async () => {
    const { authority, stagedPath } = await fixture();
    const execute = runner([
      { stdout: "72\n" },
      { stdout: `${stagedPath}\n` },
      executableImage(72, stagedPath),
      { stdout: "sbr-helper 72 user 3u IPv4 TCP 127.0.0.1:4444 (LISTEN)\n" },
    ]);

    await expect(
      sampleAuthenticatedStagedHelperSockets(authority, new Map(), {
        execFile: execute as never,
      }),
    ).resolves.toEqual({ processIds: [72], samples: 1, violations: 1 });
  });
});
