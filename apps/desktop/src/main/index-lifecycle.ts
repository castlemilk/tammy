import type { CoreReadiness } from "../shared/readiness";
import type { CoreClient } from "./core-client";
import type { CoreProcess } from "./core-process";
import type { SbrFileReleaseKind } from "./sbr-file-intake";

export type DesktopApplicationErrorCode =
  | "APPLICATION_CLOSING"
  | "LOCAL_ENGINE_SHUTDOWN_FAILED"
  | "LOCAL_ENGINE_UNAVAILABLE";

const ERROR_MESSAGES: Readonly<Record<DesktopApplicationErrorCode, string>> = {
  APPLICATION_CLOSING: "The application is closing.",
  LOCAL_ENGINE_SHUTDOWN_FAILED: "The local engine could not be stopped.",
  LOCAL_ENGINE_UNAVAILABLE: "The local engine is unavailable.",
};

type Release = () => void;

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
  readonly cleanupSensitiveState: () => void;
  readonly core: Pick<CoreProcess, "start" | "stop">;
  readonly createClient: (readiness: Readonly<CoreReadiness>) => Readonly<CoreClient>;
  readonly createWindow: () => DesktopWindow;
  readonly exit: (code: number) => void;
  readonly installRuntimeSecurity: () => Promise<Release>;
  readonly installWindowSecurity: (window: DesktopWindow) => Release;
  readonly listenForQuit: (listener: () => void) => Release;
  readonly logger: { error(message: string): void };
  readonly ready: () => Promise<void>;
  readonly releaseKind: SbrFileReleaseKind;
  readonly registerIpc: (
    getWindow: () => DesktopWindow | undefined,
    client: Readonly<CoreClient>,
  ) => Release;
  readonly registerScheme: () => Release;
}

export interface DesktopApplication {
  shutdown(): Promise<void>;
}

function onceRelease(release: Release): Release {
  let owned = true;
  return () => {
    if (!owned) return;
    owned = false;
    release();
  };
}

function releaseSafely(release: Release | undefined): void {
  try {
    release?.();
  } catch {
    // Shutdown continues to the local-core confirmation boundary.
  }
}

export async function startDesktopApplication(
  dependencies: DesktopDependencies,
): Promise<Readonly<DesktopApplication>> {
  let closing = false;
  let window: DesktopWindow | undefined;
  let releaseScheme: Release | undefined;
  let unlistenQuit: Release | undefined;
  let releaseRuntime: Release | undefined;
  let releaseWindow: Release | undefined;
  let unregisterIpc: Release | undefined;
  let pendingRuntime: Promise<Release> | undefined;
  let pendingCore: Promise<Readonly<CoreReadiness>> | undefined;
  let coreReady = false;
  let shutdownPromise: Promise<void> | undefined;
  const cleanupSensitiveState = onceRelease(dependencies.cleanupSensitiveState);

  const ensureOpen = (): void => {
    if (closing) throw new DesktopApplicationError("APPLICATION_CLOSING");
  };
  const shutdown = (exitCode = 0): Promise<void> => {
    closing = true;
    if (shutdownPromise) return shutdownPromise;
    const coreWasPending = pendingCore !== undefined && !coreReady;
    shutdownPromise = (async () => {
      releaseSafely(unregisterIpc);
      releaseSafely(cleanupSensitiveState);
      releaseSafely(releaseWindow);
      if (window && !window.isDestroyed()) window.close();
      let stopFailed = false;
      if (pendingCore) {
        try {
          await dependencies.core.stop();
        } catch {
          stopFailed = true;
        }
      }
      const [runtimeResult, coreResult] = await Promise.all([
        pendingRuntime?.then(
          (release) => release,
          () => undefined,
        ),
        pendingCore?.then(
          () => true,
          () => false,
        ),
      ]);
      releaseSafely(runtimeResult);
      if (coreWasPending && coreResult) {
        try {
          await dependencies.core.stop();
          stopFailed = false;
        } catch {
          stopFailed = true;
        }
      }
      if (stopFailed) {
        dependencies.logger.error("LOCAL_ENGINE_SHUTDOWN_FAILED");
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

  try {
    releaseScheme = onceRelease(dependencies.registerScheme());
    unlistenQuit = onceRelease(dependencies.listenForQuit(() => void shutdown()));
    if (closing) unlistenQuit();
  } catch {
    releaseSafely(releaseScheme);
    releaseSafely(cleanupSensitiveState);
    dependencies.logger.error("LOCAL_ENGINE_UNAVAILABLE");
    dependencies.exit(1);
    throw new DesktopApplicationError("LOCAL_ENGINE_UNAVAILABLE");
  }

  try {
    await dependencies.ready();
    ensureOpen();
    pendingRuntime = dependencies.installRuntimeSecurity().then(onceRelease);
    releaseRuntime = await pendingRuntime;
    ensureOpen();
    pendingCore = dependencies.core.start();
    const readiness = await pendingCore;
    coreReady = true;
    ensureOpen();
    const client = dependencies.createClient(readiness);
    await client.getDiagnostics();
    ensureOpen();
    unregisterIpc = onceRelease(dependencies.registerIpc(() => window, client));
    window = dependencies.createWindow();
    releaseWindow = onceRelease(dependencies.installWindowSecurity(window));
    window.once("ready-to-show", () => {
      if (!closing && window && !window.isDestroyed()) window.show();
    });
    await window.loadURL(dependencies.applicationUrl);
    ensureOpen();
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
