import { constants } from "node:fs";
import { access, readFile, stat } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";

export const TEST_ELECTRON_VERSION = "43.1.1";

export async function resolveTestElectronPackageRoot(desktopRoot: string): Promise<string> {
  // Resolve metadata only: Electron's entry point can synchronously download a binary.
  const require = createRequire(join(desktopRoot, "package.json"));
  const packageFile = require.resolve("electron/package.json");
  const metadata = JSON.parse(await readFile(packageFile, "utf8")) as {
    readonly name?: unknown;
    readonly version?: unknown;
  };
  if (metadata.name !== "electron" || metadata.version !== TEST_ELECTRON_VERSION) {
    throw new Error("ELECTRON_TEST_PACKAGE_PIN_MISMATCH");
  }
  return dirname(packageFile);
}

export async function readPreparedTestElectron(
  packageRoot: string,
  platform: NodeJS.Platform = process.platform,
): Promise<string> {
  const relativeExecutable =
    platform === "darwin"
      ? "Electron.app/Contents/MacOS/Electron"
      : platform === "win32"
        ? "electron.exe"
        : "electron";
  try {
    const [installedPath, installedVersion] = await Promise.all([
      readFile(join(packageRoot, "path.txt"), "utf8"),
      readFile(join(packageRoot, "dist", "version"), "utf8"),
    ]);
    if (
      installedPath !== relativeExecutable ||
      installedVersion.trim().replace(/^v/, "") !== TEST_ELECTRON_VERSION
    ) {
      throw new Error("Installed Electron path or version differs from the test pin.");
    }
    const executable = join(packageRoot, "dist", relativeExecutable);
    if (!(await stat(executable)).isFile()) throw new Error("Electron executable is not a file.");
    await access(executable, constants.X_OK);
    return executable;
  } catch (cause) {
    throw new Error(
      "ELECTRON_TEST_DEPENDENCY_NOT_READY: run pnpm desktop:test to prepare the pinned binary before Vitest.",
      { cause },
    );
  }
}
