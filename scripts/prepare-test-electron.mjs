import { execFile } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import {
  readPreparedTestElectron,
  resolveTestElectronPackageRoot,
} from "../apps/desktop/tests/helpers/prepared-electron.ts";

const execFileAsync = promisify(execFile);
export const ELECTRON_TEST_SETUP_TIMEOUT_MS = 120_000;

export async function prepareTestElectron({
  desktopRoot = fileURLToPath(new URL("../apps/desktop/", import.meta.url)),
  runInstaller = execFileAsync,
  timeoutMs = ELECTRON_TEST_SETUP_TIMEOUT_MS,
} = {}) {
  if (
    !Number.isSafeInteger(timeoutMs) ||
    timeoutMs <= 0 ||
    timeoutMs > ELECTRON_TEST_SETUP_TIMEOUT_MS
  ) {
    throw new Error("ELECTRON_TEST_SETUP_TIMEOUT_INVALID");
  }
  const packageRoot = await resolveTestElectronPackageRoot(desktopRoot);
  try {
    return await readPreparedTestElectron(packageRoot);
  } catch {
    // Acquisition has its own deadline, outside the runtime test's readiness budget.
  }
  try {
    await runInstaller(process.execPath, [path.join(packageRoot, "install.js")], {
      cwd: packageRoot,
      env: {
        ...process.env,
        ELECTRON_INSTALL_ARCH: process.arch,
        ELECTRON_INSTALL_PLATFORM: process.platform,
      },
      killSignal: "SIGKILL",
      maxBuffer: 256 * 1024,
      shell: false,
      timeout: timeoutMs,
      windowsHide: true,
    });
    return await readPreparedTestElectron(packageRoot);
  } catch (cause) {
    throw new Error("ELECTRON_TEST_DEPENDENCY_SETUP_FAILED", { cause });
  }
}

if (import.meta.main) {
  try {
    await prepareTestElectron();
    console.log("Pinned Electron test dependency ready (runtime test not started).");
  } catch (error) {
    console.error(error);
    process.exitCode = 1;
  }
}
