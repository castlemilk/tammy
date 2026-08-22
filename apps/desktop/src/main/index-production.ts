import path from "node:path";
import { fileURLToPath } from "node:url";

import { app, BrowserWindow, type Event, ipcMain, net, protocol, session, shell } from "electron";

import { readPublicLinks } from "../shared/public-links";

import { createCoreClient } from "./core-client";
import { createCoreLaunchArguments, parseLocalLaunchArguments } from "./core-launch";
import { CoreProcess } from "./core-process";
import type { DesktopDependencies, DesktopWindow } from "./index-lifecycle";
import { resolveBundledCorePath } from "./index-paths";
import { registerDesktopIpc } from "./ipc";
import { createDesktopRpcRouter } from "./rpc-router";
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

export function createProductionDependencies(
  processArguments: readonly string[] = process.argv,
): DesktopDependencies {
  const localLaunch = parseLocalLaunchArguments(processArguments);
  if (localLaunch.userDataPath !== undefined) {
    app.setPath("userData", localLaunch.userDataPath);
  }
  const buildDirectory = path.dirname(fileURLToPath(import.meta.url));
  const developmentResourcesPath = path.resolve(buildDirectory, "../../resources");
  const policy = createRendererSecurityPolicy(
    app.isPackaged ? undefined : MAIN_WINDOW_VITE_DEV_SERVER_URL,
  );
  const publicLinks = readPublicLinks();
  const allowedExternalUrls = [publicLinks.privacyPolicy, publicLinks.support].filter(
    (value): value is string => value !== undefined,
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
      core = new CoreProcess({
        binaryPath,
        args: createCoreLaunchArguments({
          isPackaged: app.isPackaged,
          userDataPath: app.getPath("userData"),
        }),
      });
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
      return installWindowGuards(nativeWindow.webContents, policy.applicationUrl, {
        allowedExternalUrls,
        openExternal: (url) => shell.openExternal(url),
      });
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
      registerDesktopIpc({
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
        router: createDesktopRpcRouter(client),
      }),
    registerScheme: () => installApplicationScheme({ app, protocol }),
  };
}
