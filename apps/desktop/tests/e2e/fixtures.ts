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
} from "@playwright/test";

import { findExactCoreProcesses } from "./process-check";

const execFileAsync = promisify(execFile);
const CLOSE_TIMEOUT_MS = 5_000;

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

async function closeWithin(application: ElectronApplication, timeoutMs: number): Promise<void> {
  let timer: NodeJS.Timeout | undefined;
  try {
    await Promise.race([
      application.close(),
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error("ELECTRON_CLOSE_TIMEOUT")), timeoutMs);
        timer.unref();
      }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

export const test = base.extend<ElectronFixtures>({
  // biome-ignore lint/correctness/noEmptyPattern: Playwright requires fixture dependencies to use object destructuring.
  electronHarness: async ({}, use, testInfo) => {
    const packagedLayout = await locatePackagedApplication();
    const rawArtifacts = testInfo.outputPath("electron-raw");
    const videoDirectory = path.join(rawArtifacts, "video");
    const application = await _electron.launch({
      artifactsDir: path.join(rawArtifacts, "playwright"),
      chromiumSandbox: true,
      executablePath: packagedLayout.appExecutable,
      offline: true,
      recordVideo: { dir: videoDirectory },
    });
    const consoleErrors: string[] = [];
    const pageErrors: string[] = [];
    const observed = new WeakSet<Page>();
    const observe = (page: Page) => {
      if (observed.has(page)) return;
      observed.add(page);
      observePage(page, consoleErrors, pageErrors);
    };
    application.windows().forEach(observe);
    application.on("window", observe);
    const page = await application.firstWindow();
    observe(page);
    const tracePath = testInfo.outputPath("electron-trace.zip");
    const traceStarted = testInfo.retry === 1;
    if (traceStarted) {
      await application.context().tracing.start({ screenshots: true, snapshots: true });
    }

    await use({ application, consoleErrors, page, pageErrors, packagedLayout });

    const failed = testInfo.status !== testInfo.expectedStatus;
    if (failed && !page.isClosed()) {
      await page.screenshot({ path: testInfo.outputPath("failure.png") }).catch(() => undefined);
    }
    if (traceStarted) {
      await application
        .context()
        .tracing.stop({ path: tracePath })
        .catch(() => undefined);
      if (failed) {
        await testInfo.attach("electron-trace", {
          contentType: "application/zip",
          path: tracePath,
        });
      }
    }
    const video = page.video();
    await closeWithin(application, CLOSE_TIMEOUT_MS);
    if (video) {
      if (failed) {
        const retainedVideo = testInfo.outputPath("failure.webm");
        await video.saveAs(retainedVideo);
        await testInfo.attach("electron-video", { contentType: "video/webm", path: retainedVideo });
      } else {
        await video.delete();
      }
    }
    await rm(rawArtifacts, { force: true, recursive: true });
    const remaining = await findExactCoreProcesses(packagedLayout.coreExecutable);
    expect(remaining, "the exact bundled core process must exit with Electron").toEqual([]);
  },
});

export { expect };
