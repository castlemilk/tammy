import type { ChildProcess } from "node:child_process";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { lstat, mkdir, readFile, realpath, rm, writeFile } from "node:fs/promises";
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
  hashAppBundle,
  type ScreenshotCaptureContract,
  validateScreenshotCaptureContract,
} from "../../../../scripts/capture-app-store-screenshots.mjs";
import { TAMMY_LAUNCH_SCENARIO_SWITCH } from "../../src/shared/launch-scenario";

import {
  assertOwnedStagedArtifact,
  closeAndReapElectron,
  type ElectronCloseStage,
  type ElectronLifecycleOperations,
  pollForNoCoreProcesses,
  runElectronLifecycle,
  STAGED_ARTIFACT_FILENAMES,
  type StagedArtifact,
} from "./electron-lifecycle";
import {
  type CoreProcessInstancePin,
  findAuthenticatedCoreProcesses,
  findAuthenticatedStagedHelperProcesses,
  type PackagedCoreAuthority,
  type StagedHelperAuthority,
  sampleAuthenticatedStagedHelperSockets,
} from "./process-check";
import { removeSbrE2eResult, type SbrE2ePassedResult, writePassedSbrE2eResult } from "./sbr-result";

const execFileAsync = promisify(execFile);
const CLOSE_TIMEOUT_MS = 20_000;
const ORPHAN_POLL_INTERVAL_MS = 100;
const ORPHAN_POLL_TIMEOUT_MS = 5_000;
const STARTUP_DIAGNOSTIC_DELAY_MS = 2_000;
const STARTUP_DIAGNOSTIC_TIMEOUT_MS = 5_000;
const STARTUP_DIAGNOSTIC_MAX_BYTES = 1024 * 1024;
const HELPER_SAMPLE_INTERVAL_MS = 10;

export interface PackagedLayout {
  readonly appExecutable: string;
  readonly appSha256: string;
  readonly coreExecutable: string;
  readonly coreSha256: string;
  readonly helperExecutable?: string;
  readonly helperSha256?: string;
  readonly profileFingerprint?: string;
  readonly profileSha256?: string;
  readonly releaseKind: "ordinary-package" | "mas";
  readonly sourceRevision: string;
  readonly target: "darwin-arm64" | "win32-x64";
}

export interface ElectronHarness {
  readonly application: ElectronApplication;
  readonly consoleErrors: string[];
  readonly page: Page;
  readonly pageErrors: string[];
  readonly packagedLayout: PackagedLayout;
  readonly currentPage: () => Page;
  readonly injectMachineCredentialSelection: (selectedPath: string) => Promise<void>;
  readonly markSbrPassed: (fixtureSha256: string) => void;
  readonly restart: (
    stage: "accounting-workflow-restart" | "sbr-helper-death-recovery",
  ) => Promise<Page>;
  readonly switchUserDataRoot: (name: "secondary") => Promise<Page>;
  readonly usePrimaryUserDataRoot: () => Promise<Page>;
}

interface ElectronFixtures {
  readonly electronHarness: ElectronHarness;
}

interface FixtureLifecycleState {
  application?: ElectronApplication;
  readonly consoleErrors: string[];
  readonly coreProcessPins: Map<number, CoreProcessInstancePin>;
  mainClosed?: Promise<void>;
  mainProcess?: ChildProcess;
  page?: Page;
  readonly pageErrors: string[];
  readonly packagedLayout: PackagedLayout;
  currentUserDataPath: string;
  corePathObserved?: boolean;
  readonly primaryUserDataPath: string;
  readonly rawArtifacts: string;
  readonly stagedArtifactsRoot: string;
  traceStarted?: boolean;
  forcedKillUsed?: boolean;
  helperObserver?: HelperObserver;
  readonly helperRuntimeBases: string[];
  helperSamples?: number;
  helperViolations?: number;
  helperOrphans?: number;
  sbrFixtureSha256?: string;
}

interface HelperObserver {
  survivors(): Promise<readonly { readonly executablePath: string; readonly processId: number }[]>;
  stop(): Promise<{ readonly samples: number; readonly violations: number }>;
}

export function createElectronLaunchArguments(
  userDataPath: string,
  target: PackagedLayout["target"],
  continuousIntegration: boolean,
  launchScenario?: "sbr-simulator",
  appStoreScreenshotCapture = false,
): string[] {
  return [
    `--user-data-dir=${userDataPath}`,
    ...((continuousIntegration || appStoreScreenshotCapture) && target === "darwin-arm64"
      ? ["--disable-gpu"]
      : []),
    ...(appStoreScreenshotCapture ? ["--force-device-scale-factor=1", "--lang=en-AU"] : []),
    ...(launchScenario ? [`${TAMMY_LAUNCH_SCENARIO_SWITCH}${launchScenario}`] : []),
  ];
}

function appStoreScreenshotElectronEnvironment(source: NodeJS.ProcessEnv): Record<string, string> {
  const home = source.HOME;
  if (!home || !path.isAbsolute(home) || home.includes("\0")) {
    throw new Error("APP_STORE_SCREENSHOT_ENVIRONMENT_INVALID");
  }
  const environment: Record<string, string> = {
    HOME: home,
    LANG: "en_AU.UTF-8",
    LC_ALL: "en_AU.UTF-8",
    TZ: "Australia/Melbourne",
  };
  for (const name of ["TMPDIR", "USER", "LOGNAME"] as const) {
    const value = source[name];
    if (!value) continue;
    if (value.includes("\0") || (name === "TMPDIR" && !path.isAbsolute(value))) {
      throw new Error("APP_STORE_SCREENSHOT_ENVIRONMENT_INVALID");
    }
    environment[name] = value;
  }
  return environment;
}

export function shouldRecordElectronVideo(
  target: PackagedLayout["target"],
  continuousIntegration: boolean,
): boolean {
  return !(continuousIntegration && target === "darwin-arm64");
}

function isPackagedLayout(value: unknown): value is PackagedLayout {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record.appExecutable === "string" &&
    path.isAbsolute(record.appExecutable) &&
    typeof record.appSha256 === "string" &&
    /^[0-9a-f]{64}$/.test(record.appSha256) &&
    typeof record.coreExecutable === "string" &&
    path.isAbsolute(record.coreExecutable) &&
    typeof record.coreSha256 === "string" &&
    /^[0-9a-f]{64}$/.test(record.coreSha256) &&
    typeof record.sourceRevision === "string" &&
    /^[0-9a-f]{40}$/.test(record.sourceRevision) &&
    (record.releaseKind === "ordinary-package" || record.releaseKind === "mas") &&
    (record.target === "darwin-arm64" || record.target === "win32-x64") &&
    (record.target !== "darwin-arm64" ||
      (typeof record.helperExecutable === "string" &&
        path.isAbsolute(record.helperExecutable) &&
        typeof record.helperSha256 === "string" &&
        /^[0-9a-f]{64}$/.test(record.helperSha256) &&
        typeof record.profileFingerprint === "string" &&
        /^[0-9a-f]{64}$/.test(record.profileFingerprint) &&
        typeof record.profileSha256 === "string" &&
        /^[0-9a-f]{64}$/.test(record.profileSha256)))
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

async function stableFileBytes(file: string, maximumBytes: number): Promise<Buffer> {
  const before = await lstat(file, { bigint: true });
  if (!before.isFile() || before.isSymbolicLink() || before.size > BigInt(maximumBytes)) {
    throw new Error("APP_STORE_SCREENSHOT_CAPTURE_INPUT_CHANGED");
  }
  const bytes = await readFile(file);
  const after = await lstat(file, { bigint: true });
  if (
    before.dev !== after.dev ||
    before.ino !== after.ino ||
    before.size !== after.size ||
    before.mtimeNs !== after.mtimeNs ||
    bytes.length !== Number(before.size)
  ) {
    throw new Error("APP_STORE_SCREENSHOT_CAPTURE_INPUT_CHANGED");
  }
  return bytes;
}

async function sha256File(file: string): Promise<string> {
  return createHash("sha256")
    .update(await stableFileBytes(file, 1024 * 1024 * 1024))
    .digest("hex");
}

async function locateScreenshotCaptureApplication(): Promise<PackagedLayout> {
  const contractPath = process.env.TAMMY_APP_STORE_SCREENSHOT_CONTRACT;
  if (
    !contractPath ||
    !path.isAbsolute(contractPath) ||
    path.normalize(contractPath) !== contractPath
  ) {
    throw new Error("APP_STORE_SCREENSHOT_CAPTURE_CONTRACT_MISSING");
  }
  let contract: ScreenshotCaptureContract;
  try {
    contract = validateScreenshotCaptureContract(
      JSON.parse((await stableFileBytes(contractPath, 128 * 1024)).toString("utf8")),
    );
  } catch {
    throw new Error("APP_STORE_SCREENSHOT_CAPTURE_CONTRACT_INVALID");
  }
  const appStatus = await lstat(contract.developmentApp);
  if (
    !appStatus.isDirectory() ||
    appStatus.isSymbolicLink() ||
    (await realpath(contract.developmentApp)) !== contract.developmentApp
  ) {
    throw new Error("APP_STORE_SCREENSHOT_DEVELOPMENT_APP_INVALID");
  }
  const appExecutable = path.join(contract.developmentApp, "Contents/MacOS/Tammy");
  const coreExecutable = path.join(
    contract.developmentApp,
    "Contents/Resources/core/darwin-arm64/tammy-core",
  );
  const helperExecutable = path.join(
    contract.developmentApp,
    "Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
  );
  const profile = path.join(
    contract.developmentApp,
    "Contents/Resources/sbr/simulator/sbr-profile-v1.json",
  );
  const [appSha256, coreSha256, helperSha256, profileSha256] = await Promise.all([
    hashAppBundle(contract.developmentApp),
    sha256File(coreExecutable),
    sha256File(helperExecutable),
    sha256File(profile),
  ]);
  if (appSha256 !== contract.developmentSignedAppSha256) {
    throw new Error("APP_STORE_SCREENSHOT_DEVELOPMENT_APP_CHANGED");
  }
  return {
    appExecutable,
    appSha256,
    coreExecutable,
    coreSha256,
    helperExecutable,
    helperSha256,
    profileFingerprint: profileSha256,
    profileSha256,
    releaseKind: "mas",
    sourceRevision: contract.productSourceCommit,
    target: "darwin-arm64",
  };
}

function sbrResultPath(): string {
  return path.resolve(import.meta.dirname, "../../../..", ".tmp/sbr-e2e/latest/result.json");
}

export async function locatePackagedApplicationForProject(
  projectName: string,
  options: {
    readonly locate?: () => Promise<PackagedLayout>;
    readonly resultPath?: string;
  } = {},
): Promise<PackagedLayout> {
  if (projectName === "darwin-arm64-app-store-screenshots") {
    return locateScreenshotCaptureApplication();
  }
  if (projectName === "darwin-arm64-sbr") {
    await removeSbrE2eResult(options.resultPath ?? sbrResultPath());
  }
  return (options.locate ?? locatePackagedApplication)();
}

function startHelperObserver(authority: StagedHelperAuthority): HelperObserver {
  let stopping = false;
  let samples = 0;
  let violations = 0;
  let failure: unknown;
  const pinned = new Map<number, string>();
  const loop = (async () => {
    while (!stopping) {
      try {
        const observed = await sampleAuthenticatedStagedHelperSockets(authority, pinned);
        samples += observed.samples;
        violations += observed.violations;
      } catch (error) {
        failure = error;
        stopping = true;
        break;
      }
      await new Promise<void>((resolve) => setTimeout(resolve, HELPER_SAMPLE_INTERVAL_MS));
    }
  })();
  return {
    survivors: () => findAuthenticatedStagedHelperProcesses(authority, pinned),
    stop: async () => {
      stopping = true;
      await loop;
      if (failure) throw failure;
      return { samples, violations };
    },
  };
}

function observePage(page: Page, consoleErrors: string[], pageErrors: string[]): void {
  page.on("console", (message: ConsoleMessage) => {
    if (message.type() === "error") consoleErrors.push(message.text());
  });
  page.on("pageerror", (error) => pageErrors.push(error.message));
}

export function observeMainExit(mainProcess: ChildProcess): Promise<void> {
  if (mainProcess.exitCode !== null || mainProcess.signalCode !== null) {
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const exited = () => resolve();
    mainProcess.once("exit", exited);
    if (mainProcess.exitCode !== null || mainProcess.signalCode !== null) {
      mainProcess.removeListener("exit", exited);
      resolve();
    }
  });
}

export async function requestGracefulElectronQuit(
  application: Pick<ElectronApplication, "close" | "evaluate">,
): Promise<void> {
  await application.evaluate(({ app }) => {
    // Give Playwright's close request time to detach its Node inspector before
    // Electron begins native shutdown. Otherwise Electron can wait forever for
    // the still-attached debugger after app.quit().
    setTimeout(() => app.quit(), 250);
  });
  void application.close().catch(() => undefined);
}

function forceKillMain(mainProcess: ChildProcess | undefined): void {
  if (!mainProcess) throw new Error("ELECTRON_MAIN_PROCESS_MISSING");
  if (mainProcess.exitCode !== null || mainProcess.signalCode !== null) return;
  if (!mainProcess.kill("SIGKILL")) throw new Error("ELECTRON_FORCE_KILL_FAILED");
}

function packagedCoreAuthority(layout: PackagedLayout): PackagedCoreAuthority {
  return {
    coreSha256: layout.coreSha256,
    packagedExecutable: layout.coreExecutable,
  };
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
    const processes = await findAuthenticatedCoreProcesses(
      packagedCoreAuthority(state.packagedLayout),
      state.coreProcessPins,
    );
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

async function observeAuthenticatedCorePath(state: FixtureLifecycleState): Promise<void> {
  const deadline = Date.now() + ORPHAN_POLL_TIMEOUT_MS;
  while (true) {
    if (
      (
        await findAuthenticatedCoreProcesses(
          packagedCoreAuthority(state.packagedLayout),
          state.coreProcessPins,
        )
      ).length > 0
    ) {
      state.corePathObserved = true;
      return;
    }
    if (Date.now() >= deadline) throw new Error("PACKAGED_CORE_PATH_NOT_OBSERVED");
    await new Promise<void>((resolve) => setTimeout(resolve, ORPHAN_POLL_INTERVAL_MS));
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
        query: () =>
          findAuthenticatedCoreProcesses(
            packagedCoreAuthority(state.packagedLayout),
            state.coreProcessPins,
          ),
        timeoutMs: ORPHAN_POLL_TIMEOUT_MS,
      });
      if (state.helperObserver) {
        const observer = state.helperObserver;
        const observed = await observer.stop();
        delete state.helperObserver;
        state.helperSamples = observed.samples;
        state.helperViolations = observed.violations;
        const survivors = await observer.survivors();
        state.helperOrphans = survivors.length;
        if (survivors.length > 0) throw new Error("SBR_HELPER_PROCESS_ORPHAN");
        if (observed.violations > 0) throw new Error("SBR_HELPER_SOCKET_VIOLATION");
      }
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
        forceKillMain: () => {
          state.forcedKillUsed = true;
          forceKillMain(state.mainProcess);
        },
        gracefulClose: () =>
          state.application ? requestGracefulElectronQuit(state.application) : Promise.resolve(),
        mainClosed: state.mainClosed ?? new Promise<void>(() => {}),
        stage: "final-test-teardown",
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
    finalize: async (state, clean) => {
      const resultPath = sbrResultPath();
      if (!clean || !state.sbrFixtureSha256 || state.forcedKillUsed) {
        await removeSbrE2eResult(resultPath);
        if (clean && state.sbrFixtureSha256 && state.forcedKillUsed) {
          throw new Error("SBR_E2E_FORCED_KILL");
        }
        return;
      }
      const { helperSha256, profileSha256, sourceRevision } = state.packagedLayout;
      if (!helperSha256 || !profileSha256) throw new Error("SBR_E2E_LAYOUT_INCOMPLETE");
      const helperSamples = state.helperSamples ?? 0;
      const corePathVerified = state.corePathObserved === true;
      const helperPathVerified = helperSamples > 0;
      if (!corePathVerified || !helperPathVerified) {
        throw new Error("SBR_E2E_PROCESS_PATH_UNVERIFIED");
      }
      const value: SbrE2ePassedResult = {
        schema: "tammy-sbr-e2e-result-v1",
        source_revision: sourceRevision,
        profile_sha256: profileSha256,
        helper_sha256: helperSha256,
        fixture_sha256: state.sbrFixtureSha256,
        socket_samples: helperSamples,
        socket_violations: 0,
        core_path_verified: corePathVerified,
        helper_path_verified: helperPathVerified,
        core_orphans: 0,
        helper_orphans: 0,
        playwright_status: "PASSED",
        recorded_at: new Date().toISOString(),
      };
      await writePassedSbrE2eResult(resultPath, { ...value }, { expectedRevision: sourceRevision });
    },
    setup: async (state) => {
      const launch = async () => {
        const continuousIntegration = process.env.CI !== undefined;
        const appStoreScreenshotCapture =
          testInfo.project.name === "darwin-arm64-app-store-screenshots";
        const application = await _electron.launch({
          args: createElectronLaunchArguments(
            state.currentUserDataPath,
            state.packagedLayout.target,
            continuousIntegration,
            testInfo.project.name === "darwin-arm64-sbr" ? "sbr-simulator" : undefined,
            appStoreScreenshotCapture,
          ),
          artifactsDir: path.join(state.rawArtifacts, "playwright"),
          chromiumSandbox: true,
          ...(appStoreScreenshotCapture
            ? {
                env: appStoreScreenshotElectronEnvironment(process.env),
              }
            : {}),
          executablePath: state.packagedLayout.appExecutable,
          offline: true,
          ...(shouldRecordElectronVideo(state.packagedLayout.target, continuousIntegration)
            ? { recordVideo: { dir: path.join(state.rawArtifacts, "video") } }
            : {}),
        });
        state.application = application;
        state.mainProcess = application.process();
        state.mainClosed = observeMainExit(state.mainProcess);

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
        await observeAuthenticatedCorePath(state);
        observe(page);
        state.page = page;
        return { application, page };
      };
      if (testInfo.project.name === "darwin-arm64-sbr") {
        const helperPath = state.packagedLayout.helperExecutable;
        const helperSha256 = state.packagedLayout.helperSha256;
        if (!helperPath || !helperSha256) throw new Error("PACKAGED_HELPER_PATH_MISSING");
        state.helperObserver = startHelperObserver({
          helperSha256,
          packagedExecutable: helperPath,
          trustedRuntimeBases: state.helperRuntimeBases,
        });
      }
      const launched = await launch();
      const closeCurrent = async (stage: ElectronCloseStage) => {
        await closeAndReapElectron({
          forceKillMain: () => {
            state.forcedKillUsed = true;
            forceKillMain(state.mainProcess);
          },
          gracefulClose: () =>
            state.application ? requestGracefulElectronQuit(state.application) : Promise.resolve(),
          mainClosed: state.mainClosed ?? new Promise<void>(() => {}),
          stage,
          timeoutMs: CLOSE_TIMEOUT_MS,
        });
        await pollForNoCoreProcesses({
          intervalMs: ORPHAN_POLL_INTERVAL_MS,
          query: () =>
            findAuthenticatedCoreProcesses(
              packagedCoreAuthority(state.packagedLayout),
              state.coreProcessPins,
            ),
          timeoutMs: ORPHAN_POLL_TIMEOUT_MS,
        });
      };
      const harness: ElectronHarness = {
        application: launched.application,
        consoleErrors: state.consoleErrors,
        page: launched.page,
        pageErrors: state.pageErrors,
        packagedLayout: state.packagedLayout,
        currentPage: () => {
          if (!state.page) throw new Error("ELECTRON_PAGE_MISSING");
          return state.page;
        },
        injectMachineCredentialSelection: async (selectedPath) => {
          if (!path.isAbsolute(selectedPath) || path.normalize(selectedPath) !== selectedPath) {
            throw new Error("INVALID_E2E_CREDENTIAL_PATH");
          }
          if (!state.application) throw new Error("ELECTRON_APPLICATION_MISSING");
          await state.application.evaluate(async ({ dialog }, credentialPath) => {
            dialog.showOpenDialog = async () => ({
              canceled: false,
              filePaths: [credentialPath],
            });
          }, selectedPath);
        },
        markSbrPassed: (fixtureSha256) => {
          if (!/^[0-9a-f]{64}$/.test(fixtureSha256) || state.sbrFixtureSha256) {
            throw new Error("INVALID_SBR_FIXTURE_HASH");
          }
          state.sbrFixtureSha256 = fixtureSha256;
        },
        restart: async (stage) => {
          await closeCurrent(stage);
          return (await launch()).page;
        },
        switchUserDataRoot: async (name) => {
          if (name !== "secondary") throw new Error("INVALID_E2E_USER_DATA_ROOT");
          await closeCurrent("sbr-secondary-isolation");
          state.currentUserDataPath = path.join(state.rawArtifacts, "user-data-secondary");
          state.helperRuntimeBases.push(
            path.join(state.currentUserDataPath, "local-core", "core", "sbr-runtime"),
          );
          return (await launch()).page;
        },
        usePrimaryUserDataRoot: async () => {
          await closeCurrent("sbr-primary-return");
          state.currentUserDataPath = state.primaryUserDataPath;
          return (await launch()).page;
        },
      };
      return harness;
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
    const packagedLayout = await locatePackagedApplicationForProject(testInfo.project.name);
    const rawArtifacts = testInfo.outputPath("electron-raw");
    const primaryUserDataPath = path.join(rawArtifacts, "user-data-primary");
    const state: FixtureLifecycleState = {
      consoleErrors: [],
      coreProcessPins: new Map(),
      currentUserDataPath: primaryUserDataPath,
      helperRuntimeBases: [path.join(primaryUserDataPath, "local-core", "core", "sbr-runtime")],
      packagedLayout,
      pageErrors: [],
      primaryUserDataPath,
      rawArtifacts,
      stagedArtifactsRoot: testInfo.outputPath("evidence-staging"),
    };
    await runElectronLifecycle(state, fixtureOperations(use, testInfo));
  },
});

export { expect };
