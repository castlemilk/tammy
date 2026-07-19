import { type ChildProcessWithoutNullStreams, execFile, spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { Code, ConnectError } from "@connectrpc/connect";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

import { createCoreClient } from "./core-client";
import { type CoreChildProcess, CoreProcess, type SpawnCoreProcess } from "./core-process";

const execFileAsync = promisify(execFile);
const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../..");

function processIsAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    if (
      error instanceof Error &&
      "code" in error &&
      (error as NodeJS.ErrnoException).code === "ESRCH"
    ) {
      return false;
    }
    throw error;
  }
}

describe("CoreProcess and Connect-ES interoperability", () => {
  let temporaryDirectory: string;
  let binaryPath: string;
  let supervisor: CoreProcess | undefined;
  let child: ChildProcessWithoutNullStreams | undefined;

  beforeAll(async () => {
    temporaryDirectory = await mkdtemp(path.join(tmpdir(), "tammy-core-test-"));
    binaryPath = path.join(
      temporaryDirectory,
      process.platform === "win32" ? "tammy-core.exe" : "tammy-core",
    );
    await execFileAsync(
      "go",
      ["build", "-trimpath", "-o", binaryPath, "./services/core/cmd/tammy-core"],
      {
        cwd: repositoryRoot,
        windowsHide: true,
      },
    );
  }, 60_000);

  afterAll(async () => {
    await supervisor?.stop().catch(() => undefined);
    if (temporaryDirectory !== undefined) {
      await rm(temporaryDirectory, { recursive: true, force: true });
    }
  });

  it("calls the real offline core, rejects a changed capability, and leaves no child", async () => {
    const spawnCore: SpawnCoreProcess = (absoluteBinaryPath, args, options) => {
      child = spawn(absoluteBinaryPath, args, {
        env: options.env,
        shell: options.shell,
        stdio: ["pipe", "pipe", "pipe"],
        windowsHide: options.windowsHide,
      });
      return child as CoreChildProcess;
    };
    supervisor = new CoreProcess({
      binaryPath,
      spawn: spawnCore,
      readinessTimeoutMs: 10_000,
    });
    const readiness = await supervisor.start();
    const pid = child?.pid;
    expect(pid).toBeTypeOf("number");

    await expect(createCoreClient(readiness).getDiagnostics()).resolves.toEqual({
      apiVersion: "tammy.v1",
      coreVersion: "dev",
      runtimeMode: "offline",
      networkRequired: false,
    });

    const changedCapability = Buffer.alloc(32, 0x52).toString("base64url");
    expect(changedCapability).not.toBe(readiness.capability);
    const authenticationError = await createCoreClient({
      ...readiness,
      capability: changedCapability,
    })
      .getDiagnostics()
      .catch((caught: unknown) => caught);
    expect(authenticationError).toBeInstanceOf(ConnectError);
    expect(authenticationError).toMatchObject({
      code: Code.Unauthenticated,
      rawMessage: "Core request failed.",
    });
    expect(`${String(authenticationError)} ${JSON.stringify(authenticationError)}`).not.toContain(
      changedCapability,
    );

    await supervisor.stop();

    expect(child?.exitCode).toBe(0);
    expect(child?.signalCode).toBeNull();
    expect(processIsAlive(pid as number)).toBe(false);
    expect(supervisor.getDiagnostic()).toEqual({ state: "STOPPED" });
  }, 30_000);
});
