import type { CoreReadiness } from "../shared/readiness";
import type { CoreClient } from "./core-client";
import type { CoreProcess } from "./core-process";
import { createProductionDependencies } from "./index-production";

export type DesktopApplicationErrorCode =
  | "APPLICATION_CLOSING"
  | "LOCAL_ENGINE_SHUTDOWN_FAILED"
  | "LOCAL_ENGINE_UNAVAILABLE";

const ERROR_MESSAGES: Readonly<Record<DesktopApplicationErrorCode, string>> = {
  APPLICATION_CLOSING: "The application is closing.",
  LOCAL_ENGINE_SHUTDOWN_FAILED: "The local engine could not be stopped.",
  LOCAL_ENGINE_UNAVAILABLE: "The local engine is unavailable.",
};

export class DesktopApplicationError extends Error {
  public constructor(public readonly code: DesktopApplicationErrorCode) {
    super(ERROR_MESSAGES[code]);
    this.name = "DesktopApplicationError";
  }
}

export interface DesktopWindow {
  close(): void;
  isDestroyed(): boolean;
  loadURL(url: string): Promise<void>;
  once(event: "ready-to-show", listener: () => void): this;
  show(): void;
}

export interface DesktopDependencies {
  readonly applicationUrl: string;
  readonly core: Pick<CoreProcess, "start" | "stop">;
  readonly createClient: (readiness: Readonly<CoreReadiness>) => Readonly<CoreClient>;
  readonly createWindow: () => DesktopWindow;
  readonly exit: (code: number) => void;
  readonly installRuntimeSecurity: () => Promise<() => void>;
  readonly installWindowSecurity: (window: DesktopWindow) => () => void;
  readonly listenForQuit: (listener: () => void) => () => void;
  readonly logger: { error(message: string): void };
  readonly ready: () => Promise<void>;
  readonly registerIpc: (
    getWindow: () => DesktopWindow | undefined,
    client: Readonly<CoreClient>,
  ) => () => void;
  readonly registerScheme: () => () => void;
}

export interface DesktopApplication {
  shutdown(): Promise<void>;
}

function releaseSafely(release: (() => void) | undefined): void {
  try {
    release?.();
  } catch {
    // Security releases are best effort; the process exits after the core is confirmed stopped.
  }
}

export async function startDesktopApplication(
  dependencies: DesktopDependencies,
): Promise<Readonly<DesktopApplication>> {
  let window: DesktopWindow | undefined;
  let releaseRuntime: (() => void) | undefined;
  let releaseWindow: (() => void) | undefined;
  let unregisterIpc: (() => void) | undefined;
  let unlistenQuit: (() => void) | undefined;
  let shutdownPromise: Promise<void> | undefined;
  const releaseScheme = dependencies.registerScheme();

  const shutdown = (exitCode = 0): Promise<void> => {
    if (shutdownPromise) return shutdownPromise;
    shutdownPromise = (async () => {
      releaseSafely(unregisterIpc);
      releaseSafely(releaseWindow);
      if (window && !window.isDestroyed()) window.close();
      try {
        await dependencies.core.stop();
      } catch {
        dependencies.logger.error("LOCAL_ENGINE_SHUTDOWN_FAILED");
        releaseSafely(releaseRuntime);
        releaseSafely(releaseScheme);
        releaseSafely(unlistenQuit);
        dependencies.exit(1);
        throw new DesktopApplicationError("LOCAL_ENGINE_SHUTDOWN_FAILED");
      }
      releaseSafely(releaseRuntime);
      releaseSafely(releaseScheme);
      releaseSafely(unlistenQuit);
      dependencies.exit(exitCode);
    })();
    void shutdownPromise.catch(() => undefined);
    return shutdownPromise;
  };
  unlistenQuit = dependencies.listenForQuit(() => void shutdown());

  try {
    await dependencies.ready();
    if (shutdownPromise) throw new DesktopApplicationError("APPLICATION_CLOSING");
    releaseRuntime = await dependencies.installRuntimeSecurity();
    const readiness = await dependencies.core.start();
    const client = dependencies.createClient(readiness);
    await client.getDiagnostics();
    if (shutdownPromise) throw new DesktopApplicationError("APPLICATION_CLOSING");
    unregisterIpc = dependencies.registerIpc(() => window, client);
    window = dependencies.createWindow();
    releaseWindow = dependencies.installWindowSecurity(window);
    window.once("ready-to-show", () => {
      if (!shutdownPromise && window && !window.isDestroyed()) window.show();
    });
    await window.loadURL(dependencies.applicationUrl);
  } catch (error) {
    if (shutdownPromise) {
      await shutdownPromise;
      throw error;
    }
    dependencies.logger.error("LOCAL_ENGINE_UNAVAILABLE");
    await shutdown(1).catch(() => undefined);
    throw new DesktopApplicationError("LOCAL_ENGINE_UNAVAILABLE");
  }

  return Object.freeze({ shutdown: () => shutdown() });
}

if (process.versions.electron) {
  void startDesktopApplication(createProductionDependencies()).catch(() => undefined);
}
