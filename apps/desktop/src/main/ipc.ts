import type { SystemDiagnostics } from "../shared/desktop-api";
import { SYSTEM_DIAGNOSTICS_CHANNEL } from "../shared/desktop-api";
import preloadMethods from "../shared/preload-methods.json";
import { isTrustedApplicationURL } from "./security";

export const DIAGNOSTICS_PRELOAD_METHOD = "getSystemDiagnostics";

if (!preloadMethods.includes(DIAGNOSTICS_PRELOAD_METHOD)) {
  throw new Error("DIAGNOSTICS_PRELOAD_METHOD_MISSING");
}

interface IpcFrame {
  readonly url: string;
  isDestroyed(): boolean;
}

interface IpcWebContents {
  readonly mainFrame: IpcFrame;
  isDestroyed(): boolean;
}

interface IpcWindow {
  readonly webContents: IpcWebContents;
  isDestroyed(): boolean;
}

interface IpcInvokeEvent {
  readonly sender: unknown;
  readonly senderFrame: IpcFrame | null;
}

interface IpcMainBoundary {
  handle(
    channel: string,
    listener: (event: IpcInvokeEvent, ...args: unknown[]) => Promise<unknown> | unknown,
  ): void;
  removeHandler(channel: string): void;
}

export interface DiagnosticsIpcOptions {
  readonly applicationUrl: string;
  readonly getSystemDiagnostics: () => Promise<SystemDiagnostics>;
  readonly ipcMain: IpcMainBoundary;
  readonly mainWindow: IpcWindow;
}

export type IpcBoundaryErrorCode = "IPC_SENDER_REJECTED";

export class IpcBoundaryError extends Error {
  public readonly code: IpcBoundaryErrorCode;

  public constructor() {
    super("IPC_SENDER_REJECTED");
    this.name = "IpcBoundaryError";
    this.code = "IPC_SENDER_REJECTED";
  }
}

const registrations = new WeakMap<object, symbol>();

function isAcceptedSender(
  event: IpcInvokeEvent,
  mainWindow: IpcWindow,
  applicationUrl: string,
): boolean {
  try {
    if (mainWindow.isDestroyed()) {
      return false;
    }

    const webContents = mainWindow.webContents;
    if (webContents.isDestroyed() || event.sender !== webContents) {
      return false;
    }

    const expectedFrame = webContents.mainFrame;
    const senderFrame = event.senderFrame;
    return (
      senderFrame !== null &&
      senderFrame === expectedFrame &&
      !senderFrame.isDestroyed() &&
      senderFrame.url === applicationUrl
    );
  } catch {
    return false;
  }
}

export function registerDiagnosticsIpc(options: DiagnosticsIpcOptions): () => void {
  if (!isTrustedApplicationURL(options.applicationUrl)) {
    throw new Error("INVALID_APPLICATION_URL");
  }

  const registration = Symbol("diagnostics-ipc-registration");
  options.ipcMain.removeHandler(SYSTEM_DIAGNOSTICS_CHANNEL);
  options.ipcMain.handle(SYSTEM_DIAGNOSTICS_CHANNEL, async (event) => {
    if (!isAcceptedSender(event, options.mainWindow, options.applicationUrl)) {
      throw new IpcBoundaryError();
    }

    return options.getSystemDiagnostics();
  });
  registrations.set(options.ipcMain, registration);

  return () => {
    if (registrations.get(options.ipcMain) !== registration) {
      return;
    }
    options.ipcMain.removeHandler(SYSTEM_DIAGNOSTICS_CHANNEL);
    registrations.delete(options.ipcMain);
  };
}
