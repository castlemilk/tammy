import path from "node:path";
import { fileURLToPath } from "node:url";

import { app, BrowserWindow, type Event, ipcMain, net, protocol, session } from "electron";

import { createCoreClient } from "./core-client";
import { CoreProcess } from "./core-process";
import type { DesktopDependencies, DesktopWindow } from "./index";
import { resolveBundledCorePath } from "./index-paths";
import { registerDiagnosticsIpc } from "./ipc";
import {
  createRendererSecurityPolicy,
  createSecureWebPreferences,
  installApplicationProtocol,
  installApplicationScheme,
  installContentSecurityPolicy,
  installSessionGuards,
  installWindowGuards,
} from "./security";

function releaseAll(releases: readonly (() => void)[]): void {
  for (const release of [...releases].reverse()) {
    release();
  }
}

export function createProductionDependencies(): DesktopDependencies {
  const buildDirectory = path.dirname(fileURLToPath(import.meta.url));
  const developmentResourcesPath = path.resolve(buildDirectory, "../../resources");
  const policy = createRendererSecurityPolicy(
    app.isPackaged ? undefined : MAIN_WINDOW_VITE_DEV_SERVER_URL,
  );
  let core: CoreProcess | undefined;
  let nativeWindow: BrowserWindow | undefined;
  let requestQuit: (() => void) | undefined;
  const coreBoundary = {
    start: async () => {
      const binaryPath = await resolveBundledCorePath({
        arch: process.arch,
        developmentResourcesPath,
        isPackaged: app.isPackaged,
        platform: process.platform,
        resourcesPath: process.resourcesPath,
      });
      core = new CoreProcess({ binaryPath });
      return core.start();
    },
    stop: async () => {
      if (core) await core.stop();
    },
  };

  return {
    applicationUrl: policy.applicationUrl,
    core: coreBoundary,
    createClient: createCoreClient,
    createWindow: () => {
      const browser = new BrowserWindow({
        backgroundColor: "#f6f7f8",
        height: 760,
        minHeight: 640,
        minWidth: 900,
        show: false,
        webPreferences: createSecureWebPreferences(path.join(buildDirectory, "preload.cjs")),
        width: 1180,
      });
      nativeWindow = browser;
      browser.on("close", (event: Event) => {
        event.preventDefault();
        requestQuit?.();
      });
      const desktopWindow: DesktopWindow = {
        close: () => browser.destroy(),
        isDestroyed: () => browser.isDestroyed(),
        loadURL: (url) => browser.loadURL(url),
        once: (_event, listener) => {
          browser.once("ready-to-show", listener);
          return desktopWindow;
        },
        show: () => browser.show(),
      };
      return desktopWindow;
    },
    exit: (code) => app.exit(code),
    installRuntimeSecurity: async () => {
      const releases = [
        installContentSecurityPolicy(session.defaultSession, policy),
        installSessionGuards(session.defaultSession),
      ];
      if (app.isPackaged) {
        releases.push(
          await installApplicationProtocol({
            app,
            fetchFile: (url) => net.fetch(url),
            protocol,
            rendererRoot: path.join(app.getAppPath(), ".vite/renderer/main_window"),
          }),
        );
      }
      return () => releaseAll(releases);
    },
    installWindowSecurity: () => {
      if (!nativeWindow) throw new Error("WINDOW_NOT_CREATED");
      return installWindowGuards(nativeWindow.webContents, policy.applicationUrl);
    },
    listenForQuit: (listener) => {
      requestQuit = listener;
      const beforeQuit = (event: Event) => {
        event.preventDefault();
        listener();
      };
      app.on("before-quit", beforeQuit);
      app.on("window-all-closed", listener);
      return () => {
        requestQuit = undefined;
        app.removeListener("before-quit", beforeQuit);
        app.removeListener("window-all-closed", listener);
      };
    },
    logger: { error: (message) => console.error(message) },
    ready: () => app.whenReady(),
    registerIpc: (getWindow, client) =>
      registerDiagnosticsIpc({
        applicationUrl: policy.applicationUrl,
        getSystemDiagnostics: client.getDiagnostics,
        ipcMain,
        mainWindow: {
          get webContents() {
            if (!nativeWindow || getWindow() === undefined) throw new Error("WINDOW_NOT_CREATED");
            return nativeWindow.webContents;
          },
          isDestroyed: () => !nativeWindow || nativeWindow.isDestroyed(),
        },
      }),
    registerScheme: () => installApplicationScheme({ app, protocol }),
  };
}
