import type { ChildProcess } from "node:child_process";
import { chmod, lstat, mkdir, mkdtemp, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { createServer, type Server } from "node:http";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { _electron, expect, test } from "@playwright/test";
import { closeAndReapElectron, pollForNoCoreProcesses } from "./electron-lifecycle";
import {
  ElectronProcessDiagnostics,
  observeElectronProcessDiagnostics,
  safeElectronCloseFailureCode,
  safeFirstWindowFailureCode,
} from "./electron-process-diagnostics";
import { findExactCoreProcesses } from "./process-check";

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const electronExecutable = createRequire(import.meta.url)("electron") as string;
const rendererRoot = path.join(desktopRoot, "out/renderer");
const mainEntry = path.join(desktopRoot, "out/main/index.js");
const coreExecutable = path.join(
  desktopRoot,
  "resources/core",
  `${process.platform}-${process.arch}`,
  process.platform === "win32" ? "tammy-core.exe" : "tammy-core",
);
const SMOKE_STAGE_MARKERS = Object.freeze({
  TEMP_DIRECTORY: "ELECTRON_VITE_SMOKE_TEMP_DIRECTORY",
  RENDERER_SERVER: "ELECTRON_VITE_SMOKE_RENDERER_SERVER",
  ELECTRON_LAUNCH: "ELECTRON_VITE_SMOKE_ELECTRON_LAUNCH",
  FIRST_WINDOW: "ELECTRON_VITE_SMOKE_FIRST_WINDOW",
  BRIDGE_EVALUATION: "ELECTRON_VITE_SMOKE_BRIDGE_EVALUATION",
  SANDBOX_CHECK: "ELECTRON_VITE_SMOKE_SANDBOX_CHECK",
  CONTRACT_CHECKS: "ELECTRON_VITE_SMOKE_CONTRACT_CHECKS",
  CORE_PROCESS_CHECK: "ELECTRON_VITE_SMOKE_CORE_PROCESS_CHECK",
  APPLICATION_CLOSE: "ELECTRON_VITE_SMOKE_APPLICATION_CLOSE",
  CORE_REAP: "ELECTRON_VITE_SMOKE_CORE_REAP",
  RENDERER_SERVER_CLOSE: "ELECTRON_VITE_SMOKE_RENDERER_SERVER_CLOSE",
  TEMP_DIRECTORY_CLEANUP: "ELECTRON_VITE_SMOKE_TEMP_DIRECTORY_CLEANUP",
  COMPLETE: "ELECTRON_VITE_SMOKE_COMPLETE",
} as const);

type SmokeStage = keyof typeof SMOKE_STAGE_MARKERS;

function processClosePromise(mainProcess: ChildProcess): Promise<void> {
  if (mainProcess.exitCode !== null || mainProcess.signalCode !== null) return Promise.resolve();
  return new Promise((resolve) => {
    const onClose = () => {
      mainProcess.removeListener("close", onClose);
      resolve();
    };
    mainProcess.once("close", onClose);
    if (mainProcess.exitCode !== null || mainProcess.signalCode !== null) onClose();
  });
}

function contentType(filePath: string): string {
  const extension = path.extname(filePath);
  if (extension === ".css") return "text/css; charset=utf-8";
  if (extension === ".html") return "text/html; charset=utf-8";
  if (extension === ".js") return "text/javascript; charset=utf-8";
  if (extension === ".svg") return "image/svg+xml";
  return "application/octet-stream";
}

async function sendStaticAsset(
  request: import("node:http").IncomingMessage,
  response: import("node:http").ServerResponse,
  root: string,
): Promise<void> {
  if (request.method !== "GET") {
    response.writeHead(405, { Allow: "GET" }).end();
    return;
  }
  let pathname: string;
  try {
    pathname = decodeURIComponent(new URL(request.url ?? "/", "http://127.0.0.1").pathname);
  } catch {
    response.writeHead(400).end();
    return;
  }
  const candidate = path.resolve(root, `.${pathname === "/" ? "/index.html" : pathname}`);
  if (
    path.relative(root, candidate).startsWith("..") ||
    path.isAbsolute(path.relative(root, candidate))
  ) {
    response.writeHead(404).end();
    return;
  }
  try {
    const [rootPath, candidatePath, stats] = await Promise.all([
      realpath(root),
      realpath(candidate),
      lstat(candidate),
    ]);
    if (
      !stats.isFile() ||
      path.relative(rootPath, candidatePath).startsWith("..") ||
      path.isAbsolute(path.relative(rootPath, candidatePath))
    ) {
      response.writeHead(404).end();
      return;
    }
    response
      .writeHead(200, { "Content-Type": contentType(candidatePath) })
      .end(await readFile(candidatePath));
  } catch {
    response.writeHead(404).end();
  }
}

async function startRendererServer(
  root: string,
): Promise<{ readonly close: () => Promise<void>; readonly origin: string }> {
  const server: Server = createServer((request, response) => {
    void sendStaticAsset(request, response, root);
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") {
    server.close();
    throw new Error("ELECTRON_VITE_STATIC_SERVER_ADDRESS_INVALID");
  }
  return {
    close: () =>
      new Promise((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      ),
    origin: `http://127.0.0.1:${address.port}`,
  };
}

test("runs the real electron-vite development runtime offline and cleans up Core", async () => {
  test.skip(
    process.platform !== "darwin" && process.platform !== "win32",
    "unsupported Electron smoke target",
  );

  let userDataPath: string | undefined;
  let server: Awaited<ReturnType<typeof startRendererServer>> | undefined;
  let application: Awaited<ReturnType<typeof _electron.launch>> | undefined;
  let processDiagnostics: ElectronProcessDiagnostics | undefined;
  let bodyFailure: unknown;
  let currentStage: SmokeStage = "TEMP_DIRECTORY";
  const reachedStages: SmokeStage[] = [];
  const cleanupFailures: Error[] = [];

  const enterStage = (stage: SmokeStage) => {
    currentStage = stage;
    if (reachedStages.at(-1) !== stage) reachedStages.push(stage);
  };

  const recordCleanupFailure = (operation: string, failure: unknown) => {
    cleanupFailures.push(
      failure instanceof Error
        ? new Error(`${operation}: ${failure.message}`, { cause: failure })
        : new Error(`${operation}: ${String(failure)}`),
    );
  };

  const attachSafeDiagnostics = async (failure: unknown): Promise<void> => {
    const summary =
      processDiagnostics?.formatSummary(application?.process()) ??
      "electronProcess=UNKNOWN;stderrMarkers=none";
    const closeCode = safeElectronCloseFailureCode(failure);
    const failureCode =
      closeCode === "ELECTRON_CLOSE_FAILED" ? safeFirstWindowFailureCode(failure) : closeCode;
    const diagnosticPath = test
      .info()
      .outputPath(`electron-vite-safe-process-diagnostics-${currentStage.toLowerCase()}.txt`);
    await mkdir(path.dirname(diagnosticPath), { recursive: true, mode: 0o700 });
    await writeFile(
      diagnosticPath,
      `${[
        `failure=${failureCode}`,
        `currentStage=${SMOKE_STAGE_MARKERS[currentStage]}`,
        `reachedStages=${reachedStages.map((stage) => SMOKE_STAGE_MARKERS[stage]).join(",")}`,
        summary,
      ].join("\n")}\n`,
      { encoding: "utf8", mode: 0o600 },
    );
    await chmod(diagnosticPath, 0o600);
    await test
      .info()
      .attach(`electron-vite-safe-process-diagnostics-${currentStage.toLowerCase()}`, {
        path: diagnosticPath,
        contentType: "text/plain",
      });
  };

  try {
    enterStage("TEMP_DIRECTORY");
    const physicalTempRoot = await realpath(tmpdir());
    userDataPath = await mkdtemp(path.join(physicalTempRoot, "tammy-electron-vite-"));
    expect(await realpath(userDataPath)).toBe(userDataPath);
    enterStage("RENDERER_SERVER");
    server = await startRendererServer(rendererRoot);
    enterStage("ELECTRON_LAUNCH");
    application = await _electron.launch({
      args: [mainEntry, `--user-data-dir=${userDataPath}`],
      chromiumSandbox: true,
      env: { ...process.env, ELECTRON_RENDERER_URL: server.origin },
      executablePath: electronExecutable,
    });
    processDiagnostics = new ElectronProcessDiagnostics();
    observeElectronProcessDiagnostics(application.process(), processDiagnostics);
    enterStage("FIRST_WINDOW");
    const page = await application.firstWindow();
    enterStage("BRIDGE_EVALUATION");
    const bridgeEvidence = await page.evaluate(async () => {
      const bridgeKeys = Object.keys(window.conveyor).sort();
      const manifest = window.conveyor.manifest();
      const diagnosticsResult = (await window.conveyor.invoke(
        "conveyor:system",
        "getSystemDiagnostics",
      )) as
        | { readonly data: { readonly runtimeMode?: string }; readonly ok: true }
        | { readonly error: unknown; readonly ok: false };
      let unknownCallError = "";
      try {
        await window.conveyor.invoke("conveyor:unknown", "unknown");
      } catch (error) {
        unknownCallError = error instanceof Error ? error.message : String(error);
      }
      let subscriptionError = "";
      try {
        window.conveyor.subscribe("conveyor:system", () => undefined);
      } catch (error) {
        subscriptionError = error instanceof Error ? error.message : String(error);
      }
      return {
        bridgeKeys,
        diagnosticsResult,
        manifestModules: Object.keys(manifest).sort(),
        subscriptionError,
        legacyFacadeType: typeof (window as unknown as Record<string, unknown>)["tammy"],
        unknownCallError,
      };
    });
    enterStage("SANDBOX_CHECK");
    const sandboxEnabled = await application.evaluate(({ BrowserWindow }) => {
      const [window] = BrowserWindow.getAllWindows();
      const webContents = window?.webContents as unknown as
        | { getLastWebPreferences(): { sandbox?: boolean } }
        | undefined;
      return webContents?.getLastWebPreferences().sandbox;
    });

    enterStage("CONTRACT_CHECKS");
    expect(bridgeEvidence.bridgeKeys).toEqual(["invoke", "manifest", "subscribe"]);
    expect(bridgeEvidence.manifestModules).toEqual([
      "accounting",
      "banking",
      "basPreparation",
      "documents",
      "organisation",
      "purchases",
      "reporting",
      "sbr",
      "system",
      "workspace",
    ]);
    expect(bridgeEvidence.legacyFacadeType).toBe("undefined");
    expect(bridgeEvidence.unknownCallError).toBe("INVALID_CONVEYOR_REQUEST");
    expect(bridgeEvidence.subscriptionError).toBe("IPC_SUBSCRIPTIONS_DISABLED");
    expect(bridgeEvidence.diagnosticsResult).toMatchObject({
      data: { runtimeMode: "offline" },
      ok: true,
    });
    expect(sandboxEnabled).toBe(true);
    expect(await page.evaluate(() => typeof process)).toBe("undefined");
    expect(new URL(page.url()).origin).toBe(server.origin);
    enterStage("CORE_PROCESS_CHECK");
    expect((await findExactCoreProcesses(coreExecutable)).length).toBeGreaterThan(0);
  } catch (failure) {
    bodyFailure = failure;
    try {
      await attachSafeDiagnostics(failure);
    } catch (diagnosticFailure) {
      recordCleanupFailure("Safe process diagnostics capture failed", diagnosticFailure);
    }
  } finally {
    if (application) {
      enterStage("APPLICATION_CLOSE");
      const mainProcess = application.process();
      try {
        await closeAndReapElectron({
          forceKillMain: () => {
            if (mainProcess.exitCode !== null || mainProcess.signalCode !== null) return;
            if (!mainProcess.kill("SIGKILL")) throw new Error("ELECTRON_FORCE_KILL_FAILED");
          },
          gracefulClose: () => application?.close() ?? Promise.resolve(),
          mainClosed: processClosePromise(mainProcess),
          stage: "final-test-teardown",
          timeoutMs: 5_000,
        });
      } catch (failure) {
        recordCleanupFailure(
          "Electron application close failed",
          safeElectronCloseFailureCode(failure),
        );
        try {
          await attachSafeDiagnostics(failure);
        } catch (diagnosticFailure) {
          recordCleanupFailure("Safe process diagnostics capture failed", diagnosticFailure);
        }
      }
    }
    enterStage("CORE_REAP");
    try {
      await pollForNoCoreProcesses({
        intervalMs: 100,
        query: () => findExactCoreProcesses(coreExecutable),
        timeoutMs: 5_000,
      });
    } catch (failure) {
      recordCleanupFailure("Core process cleanup failed", failure);
      try {
        await attachSafeDiagnostics(failure);
      } catch (diagnosticFailure) {
        recordCleanupFailure("Safe process diagnostics capture failed", diagnosticFailure);
      }
    }
    if (server) {
      enterStage("RENDERER_SERVER_CLOSE");
      try {
        await server.close();
      } catch (failure) {
        recordCleanupFailure("Renderer server close failed", failure);
        try {
          await attachSafeDiagnostics(failure);
        } catch (diagnosticFailure) {
          recordCleanupFailure("Safe process diagnostics capture failed", diagnosticFailure);
        }
      }
    }
    if (userDataPath) {
      enterStage("TEMP_DIRECTORY_CLEANUP");
      try {
        await rm(userDataPath, { force: true, recursive: true });
      } catch (failure) {
        recordCleanupFailure("Electron user-data cleanup failed", failure);
        try {
          await attachSafeDiagnostics(failure);
        } catch (diagnosticFailure) {
          recordCleanupFailure("Safe process diagnostics capture failed", diagnosticFailure);
        }
      }
    }
    enterStage("COMPLETE");
  }
  if (bodyFailure !== undefined) {
    if (cleanupFailures.length > 0) {
      throw new AggregateError(
        [bodyFailure, ...cleanupFailures],
        "ELECTRON_VITE_SMOKE_AND_CLEANUP_FAILED",
      );
    }
    throw bodyFailure;
  }
  if (cleanupFailures.length > 0) {
    throw new AggregateError(cleanupFailures, "ELECTRON_VITE_SMOKE_CLEANUP_FAILED");
  }
});
