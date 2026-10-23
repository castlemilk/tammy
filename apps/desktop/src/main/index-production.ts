import path from "node:path";
import { fileURLToPath } from "node:url";
import { create } from "@bufbuild/protobuf";
import { GetCurrentUserRequestSchema } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  type GetInvoiceRequest,
  GetInvoiceRequestSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { renderInvoicePdf } from "@tammy/invoices/pdf";
import {
  app,
  BrowserWindow,
  dialog,
  type Event,
  ipcMain,
  Menu,
  net,
  protocol,
  session,
  shell,
} from "electron";
import { registerTammyConveyor } from "../conveyor/registration";
import { invoiceToRecord } from "../shared/invoices";
import { APPROVED_PUBLIC_URLS } from "../shared/official-resources";
import { readPublicLinks } from "../shared/public-links";
import type { ReportExports } from "../shared/report-export";
import { createCoreClient } from "./core-client";
import { createCoreLaunchArguments } from "./core-launch";
import { CoreProcess } from "./core-process";
import type { DesktopDependencies, DesktopWindow } from "./index-lifecycle";
import { resolveBundledCorePath, resolveBundledSbrProfileLocation } from "./index-paths";
import { createInvoiceExport, type InvoiceExport } from "./invoice-export";
import { createInvoiceIssuer } from "./invoice-issue";
import { loadInvoiceLogo } from "./invoice-logo";
import {
  parseDesktopLaunchScenario,
  rendererLaunchScenarioArguments,
  requiresSimulatorProfile,
} from "./launch-scenario";
import { createDesktopLocalIntegration, type DesktopLocalIntegration } from "./local-integration";
import { createLocalIntegrationMenuItems } from "./local-integration-menu";
import {
  createLocalIntegrationSession,
  type LocalIntegrationSession,
} from "./local-integration-session";
import { createReportExports } from "./report-export";
import {
  createSbrFileIntake,
  type SbrFileIntake,
  type SbrFileReleaseKind,
} from "./sbr-file-intake";
import {
  createRendererSecurityPolicy,
  createSecureWebPreferences,
  installApplicationProtocol,
  installApplicationScheme,
  installContentSecurityPolicy,
  installSessionGuards,
  installWindowGuards,
} from "./security";
import { type DesktopShutdownStage, desktopShutdownMarker } from "./startup-diagnostics";

function releaseAll(releases: readonly (() => void)[]): void {
  for (const release of [...releases].reverse()) {
    release();
  }
}

export function resolveSbrFileReleaseKind(options: {
  readonly isMas: boolean;
  readonly isPackaged: boolean;
}): SbrFileReleaseKind {
  if (!options.isPackaged) return "development";
  return options.isMas ? "mas" : "ordinary-package";
}

interface ProductionDependencyOverrides {
  readonly createFileIntake?: (options: {
    readonly releaseKind: SbrFileReleaseKind;
    readonly showOpenDialog: Parameters<typeof createSbrFileIntake>[0]["showOpenDialog"];
  }) => Readonly<SbrFileIntake>;
  readonly isMas?: boolean;
}

export function createProductionDependencies(
  processArguments: readonly string[] = process.argv,
  overrides: ProductionDependencyOverrides = {},
): DesktopDependencies {
  const localLaunch = parseDesktopLaunchScenario(processArguments);
  if (localLaunch.userDataPath !== undefined) {
    app.setPath("userData", localLaunch.userDataPath);
  }
  const buildDirectory = path.dirname(fileURLToPath(import.meta.url));
  const developmentResourcesPath = path.resolve(buildDirectory, "../../resources");
  const developmentRendererUrl = process.env.ELECTRON_RENDERER_URL;
  if (!app.isPackaged && developmentRendererUrl === undefined) {
    throw new Error("INVALID_VITE_ORIGIN");
  }
  const policy = createRendererSecurityPolicy(app.isPackaged ? undefined : developmentRendererUrl);
  const publicLinks = readPublicLinks();
  const allowedExternalUrls = [
    ...APPROVED_PUBLIC_URLS,
    publicLinks.privacyPolicy,
    publicLinks.support,
  ].filter((value): value is string => value !== undefined);
  let core: CoreProcess | undefined;
  let nativeWindow: BrowserWindow | undefined;
  let requestQuit: (() => void) | undefined;
  let integration: DesktopLocalIntegration | undefined;
  let integrationSession: LocalIntegrationSession | undefined;
  let reportExports: ReportExports | undefined;
  let invoiceExport: InvoiceExport<GetInvoiceRequest> | undefined;
  let releaseIntegrationMenu: (() => void) | undefined;
  const logShutdownMarker = (stage: DesktopShutdownStage): void => {
    try {
      console.error(desktopShutdownMarker(stage));
    } catch {
      // Diagnostic sinks must not prevent application shutdown.
    }
  };
  const releaseKind = resolveSbrFileReleaseKind({
    isMas:
      overrides.isMas ?? (process as NodeJS.Process & Readonly<{ mas?: boolean }>).mas === true,
    isPackaged: app.isPackaged,
  });
  const fileIntake = (overrides.createFileIntake ?? createSbrFileIntake)({
    releaseKind,
    showOpenDialog: async (options) =>
      dialog.showOpenDialog({
        properties: [...options.properties],
        securityScopedBookmarks: options.securityScopedBookmarks,
      }),
  });
  const coreBoundary = {
    start: async () => {
      const binary = await resolveBundledCorePath({
        arch: process.arch,
        developmentResourcesPath,
        isPackaged: app.isPackaged,
        platform: process.platform,
        resourcesPath: process.resourcesPath,
      });
      const sbrProfile =
        requiresSimulatorProfile(localLaunch.kind) &&
        process.platform === "darwin" &&
        process.arch === "arm64" &&
        releaseKind !== "mas"
          ? await resolveBundledSbrProfileLocation({
              arch: process.arch,
              developmentResourcesPath,
              isPackaged: app.isPackaged,
              platform: process.platform,
              resourcesPath: process.resourcesPath,
            })
          : undefined;
      core = new CoreProcess({
        binary,
        args: createCoreLaunchArguments({
          isPackaged: app.isPackaged,
          ...(sbrProfile ? { sbrProfile } : {}),
          userDataPath: app.getPath("userData"),
        }),
      });
      return core.start();
    },
    stop: async () => {
      try {
        if (integration) {
          logShutdownMarker("INTEGRATION_REVOKE_STARTED");
          try {
            await integration.revoke();
          } finally {
            logShutdownMarker("INTEGRATION_REVOKE_SETTLED");
          }
        }
      } finally {
        if (core) {
          logShutdownMarker("CORE_PROCESS_STOP_STARTED");
          try {
            await core.stop();
          } finally {
            logShutdownMarker("CORE_PROCESS_STOP_SETTLED");
          }
        }
      }
    },
  };

  return {
    applicationUrl: policy.applicationUrl,
    cleanupSensitiveState: () => {
      fileIntake.clear();
      integrationSession?.clear();
      releaseIntegrationMenu?.();
      releaseIntegrationMenu = undefined;
    },
    core: coreBoundary,
    createClient: (readiness) => {
      integrationSession = createLocalIntegrationSession(createCoreClient(readiness), () => {
        void integration?.revoke().catch(() => undefined);
      });
      integration = createDesktopLocalIntegration(
        app.getPath("userData"),
        integrationSession.createReader,
        integrationSession.clearInvoiceProposal,
      );
      const current = integrationSession;
      reportExports = createReportExports({
        client: current.client,
        capture: current.captureCurrent,
        select: (defaultPath, format) =>
          dialog.showSaveDialog({
            defaultPath,
            title: `Export ${format.toUpperCase()} report`,
            filters: [{ name: format.toUpperCase(), extensions: [format] }],
          }),
      });
      invoiceExport = createInvoiceExport({
        prepare: async (request: GetInvoiceRequest) => {
          if (!request.authentication) throw new Error("LOCAL_SESSION_REQUIRED");
          const guard = current.captureCurrent(request.authentication, request.organisationId);
          const result = await current.client.getInvoice(create(GetInvoiceRequestSchema, request));
          guard();
          if (!result.invoice || result.invoice.organisationId !== request.organisationId)
            throw new Error("INVOICE_EXPORT_FAILED");
          const record = invoiceToRecord(result.invoice);
          const logo = await loadInvoiceLogo(
            current.client,
            request.authentication,
            request.organisationId,
            record.logoAssetId,
            guard,
          );
          return {
            invoice: {
              ...record.draft,
              id: record.id,
              state: record.state,
              sellerName: record.sellerName,
              sellerAbn: record.sellerAbn,
              ...(logo ? { logo } : {}),
            },
            guard,
          };
        },
        render: renderInvoicePdf,
        select: (defaultPath) =>
          dialog.showSaveDialog({
            defaultPath,
            title: "Export invoice PDF",
            filters: [{ name: "PDF", extensions: ["pdf"] }],
          }),
      });
      return Object.freeze({
        ...current.client,
        issueInvoice: createInvoiceIssuer({
          client: current.client,
          capture: current.captureCurrent,
          render: renderInvoicePdf,
        }),
      });
    },
    createWindow: () => {
      const browser = new BrowserWindow({
        backgroundColor: "#f6f7f8",
        height: 760,
        minHeight: 640,
        minWidth: 900,
        show: false,
        webPreferences: {
          ...createSecureWebPreferences(path.resolve(buildDirectory, "../preload/index.cjs")),
          additionalArguments: rendererLaunchScenarioArguments(localLaunch.kind),
        },
        width: 1180,
      });
      nativeWindow = browser;
      if (integration) {
        const previousMenu = Menu.getApplicationMenu();
        Menu.setApplicationMenu(
          Menu.buildFromTemplate([
            ...(process.platform === "darwin" ? [{ role: "appMenu" as const }] : []),
            { role: "fileMenu" },
            { role: "editMenu" },
            { role: "viewMenu" },
            {
              label: "Integrations",
              submenu: [
                ...createLocalIntegrationMenuItems(integration, {
                  showMessageBox: (options) => dialog.showMessageBox(browser, options),
                  showItemInFolder: (filePath) => shell.showItemInFolder(filePath),
                }),
              ],
            },
            { role: "windowMenu" },
          ]),
        );
        releaseIntegrationMenu = () => Menu.setApplicationMenu(previousMenu);
      }
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
            rendererRoot: path.join(app.getAppPath(), "out/renderer"),
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
        try {
          console.error(desktopShutdownMarker("BEFORE_QUIT"));
        } catch {
          // Diagnostic sinks must not prevent application shutdown.
        }
        event.preventDefault();
        listener();
      };
      const windowAllClosed = () => {
        try {
          console.error(desktopShutdownMarker("WINDOW_ALL_CLOSED"));
        } catch {
          // Diagnostic sinks must not prevent application shutdown.
        }
        listener();
      };
      app.on("before-quit", beforeQuit);
      app.on("window-all-closed", windowAllClosed);
      return () => {
        requestQuit = undefined;
        app.removeListener("before-quit", beforeQuit);
        app.removeListener("window-all-closed", windowAllClosed);
      };
    },
    logger: { error: (message) => console.error(message) },
    ready: () => app.whenReady(),
    releaseKind,
    registerConveyor: (getWindow, client) =>
      registerTammyConveyor({
        applicationUrl: policy.applicationUrl,
        client,
        fileIntake,
        invoiceExport,
        reportExports,
        invoiceProposal: async (request) => {
          if (!integrationSession || !request.authentication)
            throw new Error("LOCAL_SESSION_REQUIRED");
          const guard = integrationSession.captureCurrent(
            request.authentication,
            request.organisationId,
          );
          await integrationSession.client.getCurrentUser(
            create(GetCurrentUserRequestSchema, { authentication: request.authentication }),
          );
          guard();
          return integrationSession.getInvoiceProposal(
            request.authentication,
            request.organisationId,
          );
        },
        ipcMain,
        getWindow: () =>
          getWindow() !== undefined && nativeWindow && !nativeWindow.isDestroyed()
            ? nativeWindow
            : undefined,
      }),
    registerScheme: () => installApplicationScheme({ app, protocol }),
  };
}
