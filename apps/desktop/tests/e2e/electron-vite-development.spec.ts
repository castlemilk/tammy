import { createServer, type Server } from "node:http";
import { lstat, mkdtemp, readFile, realpath, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { _electron, expect, test } from "@playwright/test";

import { pollForNoCoreProcesses } from "./electron-lifecycle";
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
  if (path.relative(root, candidate).startsWith("..") || path.isAbsolute(path.relative(root, candidate))) {
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
    response.writeHead(200, { "Content-Type": contentType(candidatePath) }).end(await readFile(candidatePath));
  } catch {
    response.writeHead(404).end();
  }
}

async function startRendererServer(root: string): Promise<{ readonly close: () => Promise<void>; readonly origin: string }> {
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
    close: () => new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve()))),
    origin: `http://127.0.0.1:${address.port}`,
  };
}

test("runs the real electron-vite development runtime offline and cleans up Core", async () => {
  test.skip(process.platform !== "darwin" && process.platform !== "win32", "unsupported Electron smoke target");

  const userDataPath = await mkdtemp(path.join(process.env.TMPDIR ?? "/private/tmp", "tammy-electron-vite-"));
  const server = await startRendererServer(rendererRoot);
  let application: Awaited<ReturnType<typeof _electron.launch>> | undefined;
  try {
    application = await _electron.launch({
      args: [mainEntry, `--user-data-dir=${userDataPath}`],
      chromiumSandbox: true,
      env: { ...process.env, ELECTRON_RENDERER_URL: server.origin },
      executablePath: electronExecutable,
    });
    const page = await application.firstWindow();
    const diagnostics = await page.evaluate(() => window.tammy.getSystemDiagnostics());
    const sandboxEnabled = await application.evaluate(({ BrowserWindow }) => {
      const [window] = BrowserWindow.getAllWindows();
      const webContents = window?.webContents as unknown as
        | { getLastWebPreferences(): { sandbox?: boolean } }
        | undefined;
      return webContents?.getLastWebPreferences().sandbox;
    });

    expect(diagnostics.runtimeMode).toBe("offline");
    expect(sandboxEnabled).toBe(true);
    expect(await page.evaluate(() => typeof process)).toBe("undefined");
    expect(new URL(page.url()).origin).toBe(server.origin);
    expect((await findExactCoreProcesses(coreExecutable)).length).toBeGreaterThan(0);
  } finally {
    if (application) await application.close();
    await pollForNoCoreProcesses({
      intervalMs: 100,
      query: () => findExactCoreProcesses(coreExecutable),
      timeoutMs: 5_000,
    });
    await server.close();
    await rm(userDataPath, { force: true, recursive: true });
  }
});
