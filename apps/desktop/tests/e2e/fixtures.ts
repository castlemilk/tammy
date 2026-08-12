import type { ChildProcess } from "node:child_process";
import { execFile } from "node:child_process";
import { mkdir, rm, writeFile } from "node:fs/promises";
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
  assertOwnedStagedArtifact,
  closeAndReapElectron,
  type ElectronLifecycleOperations,
  pollForNoCoreProcesses,
  runElectronLifecycle,
  STAGED_ARTIFACT_FILENAMES,
  type StagedArtifact,
} from "./electron-lifecycle";
import { findExactCoreProcesses } from "./process-check";

const execFileAsync = promisify(execFile);
const CLOSE_TIMEOUT_MS = 5_000;
const ORPHAN_POLL_INTERVAL_MS = 100;
const ORPHAN_POLL_TIMEOUT_MS = 5_000;
const STARTUP_DIAGNOSTIC_DELAY_MS = 2_000;
const STARTUP_DIAGNOSTIC_TIMEOUT_MS = 5_000;
const STARTUP_DIAGNOSTIC_MAX_BYTES = 1024 * 1024;

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
  readonly restart: () => Promise<Page>;
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
  readonly stagedArtifactsRoot: string;
  traceStarted?: boolean;
}

export function createElectronLaunchArguments(
  userDataPath: string,
  target: PackagedLayout["target"],
  continuousIntegration: boolean,
): string[] {
  return [
    `--user-data-dir=${userDataPath}`,
    ...(continuousIntegration && target === "darwin-arm64" ? ["--disable-gpu"] : []),
  ];
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

async function captureMacOSStartupDiagnostic(state: FixtureLifecycleState): Promise<void> {
  if (state.packagedLayout.target !== "darwin-arm64") return;
  await mkdir(state.rawArtifacts, { recursive: true });
  const diagnosticPath = path.join(path.dirname(state.rawArtifacts), "core-startup.sample.txt");
  await writeFile(diagnosticPath, "CORE_STARTUP_SAMPLE_PENDING\n", {
    encoding: "utf8",
    mode: 0o600,
  });
  let diagnostic = "CORE_STARTUP_PROCESS_NOT_FOUND\n";
  try {
    const processes = await findExactCoreProcesses(state.packagedLayout.coreExecutable);
    const process = processes.at(-1);
    if (process) {
      const { stderr, stdout } = await execFileAsync(
        "/usr/bin/sample",
        [String(process.processId), "1", "1"],
        {
          encoding: "utf8",
          killSignal: "SIGKILL",
          maxBuffer: STARTUP_DIAGNOSTIC_MAX_BYTES,
          timeout: STARTUP_DIAGNOSTIC_TIMEOUT_MS,
        },
      );
      diagnostic = `${stdout}${stderr}`;
    }
  } catch {
    diagnostic = "CORE_STARTUP_SAMPLE_FAILED\n";
  }
  await writeFile(diagnosticPath, diagnostic, {
    encoding: "utf8",
    mode: 0o600,
  });
}

async function firstWindowWithStartupDiagnostic(
  application: ElectronApplication,
  state: FixtureLifecycleState,
): Promise<Page> {
  const firstWindow = application.firstWindow();
  let timer: ReturnType<typeof setTimeout> | undefined;
  const diagnosticDue = new Promise<"DIAGNOSTIC">((resolve) => {
    timer = setTimeout(() => resolve("DIAGNOSTIC"), STARTUP_DIAGNOSTIC_DELAY_MS);
  });
  try {
    const outcome = await Promise.race([firstWindow.then(() => "WINDOW" as const), diagnosticDue]);
    if (outcome === "DIAGNOSTIC") await captureMacOSStartupDiagnostic(state);
    return await firstWindow;
  } finally {
    if (timer) clearTimeout(timer);
  }
}

function stagedArtifact(
  state: FixtureLifecycleState,
  kind: StagedArtifact["kind"],
): StagedArtifact {
  return {
    kind,
    path: path.join(state.stagedArtifactsRoot, STAGED_ARTIFACT_FILENAMES[kind]),
  };
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
    attachArtifact: async (state, artifact) => {
      assertOwnedStagedArtifact(state.stagedArtifactsRoot, artifact);
      const attachment = {
        screenshot: { contentType: "image/png", name: "electron-screenshot" },
        trace: { contentType: "application/zip", name: "electron-trace" },
        video: { contentType: "video/webm", name: "electron-video" },
      } as const;
      await testInfo.attach(attachment[artifact.kind].name, {
        contentType: attachment[artifact.kind].contentType,
        path: artifact.path,
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
    deleteStagedArtifacts: async (state, artifacts) => {
      for (const artifact of artifacts) {
        assertOwnedStagedArtifact(state.stagedArtifactsRoot, artifact);
      }
      await rm(state.stagedArtifactsRoot, { force: true, recursive: true });
    },
    didTestFail: () => testInfo.status !== testInfo.expectedStatus,
    removeRawArtifacts: async (state) => {
      await rm(state.rawArtifacts, { force: true, recursive: true });
    },
    setup: async (state) => {
      const launch = async () => {
        const application = await _electron.launch({
          args: createElectronLaunchArguments(
            path.join(state.rawArtifacts, "user-data"),
            state.packagedLayout.target,
            process.env.CI !== undefined,
          ),
          artifactsDir: path.join(state.rawArtifacts, "playwright"),
          chromiumSandbox: true,
          executablePath: state.packagedLayout.appExecutable,
          offline: true,
          recordVideo: { dir: path.join(state.rawArtifacts, "video") },
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
        const page = await firstWindowWithStartupDiagnostic(application, state);
        observe(page);
        state.page = page;
        return { application, page };
      };
      const launched = await launch();
      return {
        application: launched.application,
        consoleErrors: state.consoleErrors,
        page: launched.page,
        pageErrors: state.pageErrors,
        packagedLayout: state.packagedLayout,
        restart: async () => {
          await closeAndReapElectron({
            forceKillMain: () => forceKillMain(state.mainProcess),
            gracefulClose: () => state.application?.close() ?? Promise.resolve(),
            mainClosed: state.mainClosed ?? new Promise<void>(() => {}),
            timeoutMs: CLOSE_TIMEOUT_MS,
          });
          await pollForNoCoreProcesses({
            intervalMs: ORPHAN_POLL_INTERVAL_MS,
            query: () => findExactCoreProcesses(state.packagedLayout.coreExecutable),
            timeoutMs: ORPHAN_POLL_TIMEOUT_MS,
          });
          return (await launch()).page;
        },
      };
    },
    stageScreenshot: async (state) => {
      if (!state.page || state.page.isClosed()) return undefined;
      const artifact = stagedArtifact(state, "screenshot");
      await mkdir(state.stagedArtifactsRoot, { recursive: true });
      await state.page.screenshot({ path: artifact.path });
      return artifact;
    },
    stageVideo: async (state) => {
      const video = state.page?.video();
      if (!video) return undefined;
      const artifact = stagedArtifact(state, "video");
      await mkdir(state.stagedArtifactsRoot, { recursive: true });
      await video.saveAs(artifact.path);
      return artifact;
    },
    stopAndStageTrace: async (state) => {
      if (!state.application) throw new Error("ELECTRON_APPLICATION_MISSING");
      const artifact = stagedArtifact(state, "trace");
      await mkdir(state.stagedArtifactsRoot, { recursive: true });
      await state.application.context().tracing.stop({ path: artifact.path });
      return artifact;
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
      stagedArtifactsRoot: testInfo.outputPath("evidence-staging"),
    };
    await runElectronLifecycle(state, fixtureOperations(use, testInfo));
  },
});

export { expect };
