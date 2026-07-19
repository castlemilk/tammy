import type { ChildProcess } from "node:child_process";
import { execFile } from "node:child_process";
import { rm } from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

import {
  _electron,
  test as base,
  type ConsoleMessage,
  type ElectronApplication,
  expect,
  type Page,
  type TestInfo,
} from "@playwright/test";

import {
  closeAndReapElectron,
  type ElectronLifecycleOperations,
  pollForNoCoreProcesses,
  runElectronLifecycle,
} from "./electron-lifecycle";
import { findExactCoreProcesses } from "./process-check";

const execFileAsync = promisify(execFile);
const CLOSE_TIMEOUT_MS = 5_000;
const ORPHAN_POLL_INTERVAL_MS = 100;
const ORPHAN_POLL_TIMEOUT_MS = 5_000;

interface PackagedLayout {
  readonly appExecutable: string;
  readonly coreExecutable: string;
  readonly target: "darwin-arm64" | "win32-x64";
}

interface ElectronHarness {
  readonly application: ElectronApplication;
  readonly consoleErrors: string[];
  readonly page: Page;
  readonly pageErrors: string[];
  readonly packagedLayout: PackagedLayout;
}

interface ElectronFixtures {
  readonly electronHarness: ElectronHarness;
}

interface FixtureLifecycleState {
  application?: ElectronApplication;
  readonly consoleErrors: string[];
  mainClosed?: Promise<void>;
  mainProcess?: ChildProcess;
  page?: Page;
  readonly pageErrors: string[];
  readonly packagedLayout: PackagedLayout;
  readonly rawArtifacts: string;
  readonly tracePath: string;
  traceStarted?: boolean;
}

function isPackagedLayout(value: unknown): value is PackagedLayout {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record.appExecutable === "string" &&
    path.isAbsolute(record.appExecutable) &&
    typeof record.coreExecutable === "string" &&
    path.isAbsolute(record.coreExecutable) &&
    (record.target === "darwin-arm64" || record.target === "win32-x64")
  );
}

async function locatePackagedApplication(): Promise<PackagedLayout> {
  const desktopRoot = path.resolve(import.meta.dirname, "../..");
  const verifier = path.join(desktopRoot, "scripts", "find-packaged-app.mjs");
  const sourceManifest = path.join(desktopRoot, "resources", "build", "build-manifest.json");
  const { stdout } = await execFileAsync(
    process.execPath,
    [verifier, "--verify", "--source-manifest", sourceManifest],
    { cwd: desktopRoot, encoding: "utf8", maxBuffer: 1024 * 1024 },
  );
  let value: unknown;
  try {
    value = JSON.parse(stdout);
  } catch {
    throw new Error("PACKAGED_LAYOUT_EVIDENCE_INVALID");
  }
  if (!isPackagedLayout(value)) throw new Error("PACKAGED_LAYOUT_EVIDENCE_INVALID");
  return value;
}

function observePage(page: Page, consoleErrors: string[], pageErrors: string[]): void {
  page.on("console", (message: ConsoleMessage) => {
    if (message.type() === "error") consoleErrors.push(message.text());
  });
  page.on("pageerror", (error) => pageErrors.push(error.message));
}

function observeMainClose(mainProcess: ChildProcess): Promise<void> {
  if (mainProcess.exitCode !== null || mainProcess.signalCode !== null) {
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const closed = () => resolve();
    mainProcess.once("close", closed);
    if (mainProcess.exitCode !== null || mainProcess.signalCode !== null) {
      mainProcess.removeListener("close", closed);
      resolve();
    }
  });
}

function forceKillMain(mainProcess: ChildProcess | undefined): void {
  if (!mainProcess) throw new Error("ELECTRON_MAIN_PROCESS_MISSING");
  if (mainProcess.exitCode !== null || mainProcess.signalCode !== null) return;
  if (!mainProcess.kill("SIGKILL")) throw new Error("ELECTRON_FORCE_KILL_FAILED");
}

function fixtureOperations(
  use: (harness: ElectronHarness) => Promise<void>,
  testInfo: TestInfo,
): ElectronLifecycleOperations<FixtureLifecycleState, ElectronHarness> {
  return {
    assertNoOrphan: async (state) => {
      await pollForNoCoreProcesses({
        intervalMs: ORPHAN_POLL_INTERVAL_MS,
        query: () => findExactCoreProcesses(state.packagedLayout.coreExecutable),
        timeoutMs: ORPHAN_POLL_TIMEOUT_MS,
      });
    },
    attachTrace: async (state) => {
      await testInfo.attach("electron-trace", {
        contentType: "application/zip",
        path: state.tracePath,
      });
    },
    closeAndReap: async (state) => {
      if (!state.application) return;
      await closeAndReapElectron({
        forceKillMain: () => forceKillMain(state.mainProcess),
        gracefulClose: () => state.application?.close() ?? Promise.resolve(),
        mainClosed: state.mainClosed ?? new Promise<void>(() => {}),
        timeoutMs: CLOSE_TIMEOUT_MS,
      });
    },
    didTestFail: () => testInfo.status !== testInfo.expectedStatus,
    handleVideo: async (state, retained) => {
      const video = state.page?.video();
      if (!video) return;
      if (retained) {
        const retainedVideo = testInfo.outputPath("failure.webm");
        await video.saveAs(retainedVideo);
        await testInfo.attach("electron-video", {
          contentType: "video/webm",
          path: retainedVideo,
        });
      } else {
        await video.delete();
      }
    },
    removeRawArtifacts: async (state) => {
      await rm(state.rawArtifacts, { force: true, recursive: true });
    },
    screenshot: async (state) => {
      if (state.page && !state.page.isClosed()) {
        await state.page.screenshot({ path: testInfo.outputPath("failure.png") });
      }
    },
    setup: async (state) => {
      const videoDirectory = path.join(state.rawArtifacts, "video");
      const application = await _electron.launch({
        artifactsDir: path.join(state.rawArtifacts, "playwright"),
        chromiumSandbox: true,
        executablePath: state.packagedLayout.appExecutable,
        offline: true,
        recordVideo: { dir: videoDirectory },
      });
      state.application = application;
      state.mainProcess = application.process();
      state.mainClosed = observeMainClose(state.mainProcess);

      const context = application.context();
      const observed = new WeakSet<Page>();
      const observe = (page: Page) => {
        if (observed.has(page)) return;
        observed.add(page);
        observePage(page, state.consoleErrors, state.pageErrors);
      };
      context.on("page", observe);
      context.pages().forEach(observe);

      if (testInfo.retry === 1) {
        await context.tracing.start({ screenshots: true, snapshots: true });
        state.traceStarted = true;
      }
      const page = await application.firstWindow();
      observe(page);
      state.page = page;
      return {
        application,
        consoleErrors: state.consoleErrors,
        page,
        pageErrors: state.pageErrors,
        packagedLayout: state.packagedLayout,
      };
    },
    stopTrace: async (state, retained) => {
      if (!state.application) throw new Error("ELECTRON_APPLICATION_MISSING");
      if (retained) {
        await state.application.context().tracing.stop({ path: state.tracePath });
      } else {
        await state.application.context().tracing.stop();
      }
    },
    use,
  };
}

export const test = base.extend<ElectronFixtures>({
  // biome-ignore lint/correctness/noEmptyPattern: Playwright requires fixture dependencies to use object destructuring.
  electronHarness: async ({}, use, testInfo) => {
    const packagedLayout = await locatePackagedApplication();
    const state: FixtureLifecycleState = {
      consoleErrors: [],
      packagedLayout,
      pageErrors: [],
      rawArtifacts: testInfo.outputPath("electron-raw"),
      tracePath: testInfo.outputPath("electron-trace.zip"),
    };
    await runElectronLifecycle(state, fixtureOperations(use, testInfo));
  },
});

export { expect };
