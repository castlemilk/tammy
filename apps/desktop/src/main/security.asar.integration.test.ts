import { writeSync } from "node:fs";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { availableParallelism, release, tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

import { describe, expect, it } from "vitest";

import {
  HarnessPhaseTimings,
  HarnessProcessError,
  productionHarnessTimers,
  runHarnessProcess,
  spawnHarnessProcess,
  terminateHarnessProcess,
} from "../../tests/helpers/electron-harness-process";
import {
  readPreparedTestElectron,
  resolveTestElectronPackageRoot,
} from "../../tests/helpers/prepared-electron";

const DESKTOP_ROOT = process.cwd();
const WORKSPACE_ROOT = resolve(DESKTOP_ROOT, "../..");
const ASAR_PACKAGE_ROOT = join(
  WORKSPACE_ROOT,
  "node_modules",
  ".pnpm",
  "@electron+asar@3.4.1",
  "node_modules",
  "@electron",
  "asar",
);
const HARNESS_PATH = join(DESKTOP_ROOT, "tests", "fixtures", "electron-asar-harness.mjs");
const SECURITY_SOURCE = join(DESKTOP_ROOT, "src", "main", "security.ts");
const HARNESS_TIMEOUT_MS = 20_000;
const CLOSE_CONFIRMATION_TIMEOUT_MS = 5_000;

interface AsarModule {
  createPackage(source: string, destination: string): Promise<void>;
}

async function pinnedAsar(): Promise<AsarModule> {
  const packageJson = JSON.parse(
    await readFile(join(ASAR_PACKAGE_ROOT, "package.json"), "utf8"),
  ) as { readonly name?: unknown; readonly version?: unknown };
  expect(packageJson).toMatchObject({
    name: "@electron/asar",
    version: "3.4.1",
  });
  return import(
    pathToFileURL(join(ASAR_PACKAGE_ROOT, "lib", "asar.js")).href
  ) as Promise<AsarModule>;
}

async function pinnedElectron(): Promise<string> {
  return readPreparedTestElectron(await resolveTestElectronPackageRoot(DESKTOP_ROOT));
}

async function runElectron(
  executable: string,
  rendererRoot: string,
  userDataPath: string,
): Promise<{ readonly stderr: string; readonly stdout: string }> {
  const environment = { ...process.env };
  delete environment.ELECTRON_RUN_AS_NODE;
  environment.ELECTRON_DISABLE_SECURITY_WARNINGS = "true";
  environment.TAMMY_TEST_RENDERER_ROOT = rendererRoot;
  environment.TAMMY_TEST_SECURITY_SOURCE = SECURITY_SOURCE;
  environment.TAMMY_TEST_USER_DATA_ROOT = userDataPath;
  const platformArguments = process.platform === "linux" ? ["--no-sandbox"] : [];
  const phaseTimings = new HarnessPhaseTimings();
  try {
    return await runHarnessProcess({
      phaseTimings,
      closeConfirmationTimeoutMs: CLOSE_CONFIRMATION_TIMEOUT_MS,
      electronArguments: [
        ...platformArguments,
        `--user-data-dir=${userDataPath}`,
        "--enable-logging=stderr",
        resolve(HARNESS_PATH, ".."),
      ],
      environment,
      executable,
      platform: process.platform,
      spawnProcess: spawnHarnessProcess,
      terminateProcess: terminateHarnessProcess,
      timeoutMs: HARNESS_TIMEOUT_MS,
      timers: productionHarnessTimers,
    });
  } finally {
    // Bypass Vitest's successful-console suppression with one fixed, bounded summary.
    // Failure to deliver diagnostics must not replace the original process outcome.
    try {
      writeSync(
        1,
        `TAMMY_ASAR_TIMING ${JSON.stringify({
          platform: process.platform,
          arch: process.arch,
          node: process.version,
          kernel: release(),
          availableParallelism: availableParallelism(),
          event: ["push", "pull_request"].includes(process.env.GITHUB_EVENT_NAME ?? "")
            ? process.env.GITHUB_EVENT_NAME
            : "other",
          displayPresent: Boolean(environment.DISPLAY),
          dbusTransport:
            environment.DBUS_SESSION_BUS_ADDRESS === undefined
              ? "absent"
              : environment.DBUS_SESSION_BUS_ADDRESS.startsWith("unix:")
                ? "unix"
                : environment.DBUS_SESSION_BUS_ADDRESS.startsWith("tcp:")
                  ? "tcp"
                  : "other",
          nodeOptionsPresent: Boolean(environment.NODE_OPTIONS),
          nodeCompileCachePresent: Boolean(environment.NODE_COMPILE_CACHE),
          nodeCompileCacheDisabled: environment.NODE_DISABLE_COMPILE_CACHE === "1",
          timings: phaseTimings.snapshot(),
        })}\n`,
      );
    } catch {}
  }
}

describe("packaged ASAR renderer protocol", () => {
  it("boots a background harness and serves a real archive through pinned Electron net.fetch", {
    timeout: 30_000,
  }, async () => {
    const directory = await mkdtemp(join(tmpdir(), "tammy-asar-integration-"));
    let safeToRemove = true;
    try {
      const source = join(directory, "source");
      const archive = join(directory, "app.asar");
      await mkdir(join(source, "renderer"), { recursive: true });
      await writeFile(join(source, "renderer", "index.html"), "<main>ASAR ready</main>");
      await (await pinnedAsar()).createPackage(source, archive);

      const electronExecutable = await pinnedElectron();
      let result: { readonly stderr: string; readonly stdout: string };
      try {
        result = await runElectron(
          electronExecutable,
          join(archive, "renderer"),
          join(directory, "user-data"),
        );
      } catch (error) {
        safeToRemove = error instanceof HarnessProcessError && error.processClosed;
        throw error;
      }
      const resultLine = result.stdout
        .split(/\r?\n/)
        .find((line) => line.startsWith("TAMMY_ASAR_RESULT "));
      expect(resultLine).toBeDefined();
      expect(JSON.parse(resultLine?.slice("TAMMY_ASAR_RESULT ".length) ?? "")).toEqual({
        body: "<main>ASAR ready</main>",
        contentType: "text/html; charset=utf-8",
        dockVisible: process.platform === "darwin" ? false : null,
        isolatedUserData: true,
        nosniff: "nosniff",
        status: 200,
      });
      expect(result.stderr).not.toContain("TAMMY_ASAR_FAILURE");
    } finally {
      if (safeToRemove) {
        await rm(directory, { force: true, recursive: true });
      }
    }
  });
});
