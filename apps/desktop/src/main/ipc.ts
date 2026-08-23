import type { SystemDiagnostics } from "../shared/desktop-api";
import {
  DESKTOP_MEDIATED_CHANNELS,
  DESKTOP_PROTO_CHANNELS,
  DESKTOP_PROTO_REQUEST_LIMITS,
  IMPORT_MACHINE_CREDENTIAL_CHANNEL,
  IMPORT_SBR_PRODUCT_ID_CHANNEL,
  isBoundedUtf8String,
  REPLACE_MACHINE_CREDENTIAL_CHANNEL,
  SELECT_MACHINE_CREDENTIAL_FILE_CHANNEL,
  SYSTEM_DIAGNOSTICS_CHANNEL,
  UNLOCK_MACHINE_CREDENTIAL_CHANNEL,
} from "../shared/desktop-api";
import preloadMethods from "../shared/preload-methods.json";
import { type DesktopRpcRouter, DesktopRpcRouterError } from "./rpc-router";
import { isAllowedApplicationDocumentURL, isTrustedApplicationURL } from "./security";

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

export interface DesktopIpcOptions extends DiagnosticsIpcOptions {
  readonly router: DesktopRpcRouter;
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
const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const typedArrayPrototype = Object.getPrototypeOf(Uint8Array.prototype) as object;

interface ExactByteView {
  readonly buffer: ArrayBuffer;
  readonly byteLength: number;
  readonly byteOffset: number;
}

function hasExactKeys(value: unknown, keys: readonly string[]): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const actual = Object.keys(value);
  return actual.length === keys.length && actual.every((key) => keys.includes(key));
}

function exactByteView(value: unknown, maximumBytes: number): ExactByteView | undefined {
  if (
    !Number.isSafeInteger(maximumBytes) ||
    maximumBytes < 1 ||
    !ArrayBuffer.isView(value) ||
    Object.getPrototypeOf(value) !== Uint8Array.prototype
  ) {
    return undefined;
  }

  try {
    const buffer = Reflect.get(typedArrayPrototype, "buffer", value) as unknown;
    const byteLength = Reflect.get(typedArrayPrototype, "byteLength", value) as unknown;
    const byteOffset = Reflect.get(typedArrayPrototype, "byteOffset", value) as unknown;
    const bufferByteLength = Reflect.get(ArrayBuffer.prototype, "byteLength", buffer) as unknown;
    if (
      typeof byteLength !== "number" ||
      !Number.isSafeInteger(byteLength) ||
      byteLength < 1 ||
      byteLength > maximumBytes ||
      typeof byteOffset !== "number" ||
      !Number.isSafeInteger(byteOffset) ||
      byteOffset < 0 ||
      typeof bufferByteLength !== "number" ||
      !Number.isSafeInteger(bufferByteLength) ||
      byteOffset > bufferByteLength - byteLength
    ) {
      return undefined;
    }
    return { buffer: buffer as ArrayBuffer, byteLength, byteOffset };
  } catch {
    return undefined;
  }
}

function copyExactBytes(view: ExactByteView): Uint8Array {
  const source = new Uint8Array(view.buffer, view.byteOffset, view.byteLength);
  const copy = new Uint8Array(view.byteLength);
  Uint8Array.prototype.set.call(copy, source);
  return copy;
}

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
      isAllowedApplicationDocumentURL(senderFrame.url, applicationUrl)
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

export function registerDesktopIpc(options: DesktopIpcOptions): () => void {
  if (!isTrustedApplicationURL(options.applicationUrl)) {
    throw new Error("INVALID_APPLICATION_URL");
  }

  const registration = Symbol("desktop-ipc-registration");
  const channels = [
    SYSTEM_DIAGNOSTICS_CHANNEL,
    ...DESKTOP_PROTO_CHANNELS,
    ...DESKTOP_MEDIATED_CHANNELS,
  ] as const;
  for (const channel of channels) {
    options.ipcMain.removeHandler(channel);
  }
  options.ipcMain.handle(SYSTEM_DIAGNOSTICS_CHANNEL, async (event) => {
    if (!isAcceptedSender(event, options.mainWindow, options.applicationUrl)) {
      throw new IpcBoundaryError();
    }
    return options.getSystemDiagnostics();
  });
  for (let index = 0; index < DESKTOP_PROTO_CHANNELS.length; index += 1) {
    const channel = DESKTOP_PROTO_CHANNELS[index];
    const maximumRequestBytes = DESKTOP_PROTO_REQUEST_LIMITS[index];
    if (channel === undefined || maximumRequestBytes === undefined) {
      throw new Error("DESKTOP_PROTO_REQUEST_LIMITS_MISMATCH");
    }
    options.ipcMain.handle(channel, async (event, ...args) => {
      if (!isAcceptedSender(event, options.mainWindow, options.applicationUrl)) {
        throw new IpcBoundaryError();
      }
      if (args.length !== 1) {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      const request = exactByteView(args[0], maximumRequestBytes);
      if (request === undefined) throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      return new Uint8Array(await options.router.invoke(channel, copyExactBytes(request)));
    });
  }
  options.ipcMain.handle(SELECT_MACHINE_CREDENTIAL_FILE_CHANNEL, async (event, ...args) => {
    if (!isAcceptedSender(event, options.mainWindow, options.applicationUrl)) {
      throw new IpcBoundaryError();
    }
    if (args.length !== 0) throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
    return options.router.selectMachineCredentialFile();
  });
  for (const channel of [IMPORT_MACHINE_CREDENTIAL_CHANNEL, REPLACE_MACHINE_CREDENTIAL_CHANNEL]) {
    options.ipcMain.handle(channel, async (event, ...args) => {
      if (!isAcceptedSender(event, options.mainWindow, options.applicationUrl)) {
        throw new IpcBoundaryError();
      }
      if (args.length !== 1) throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      const input = args[0];
      if (!hasExactKeys(input, ["command", "handle", "password"])) {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      const command = exactByteView(input.command, 8_192);
      if (
        command === undefined ||
        typeof input.handle !== "string" ||
        !UUID_V7.test(input.handle) ||
        !isBoundedUtf8String(input.password, 1_024)
      ) {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      const copied = {
        command: copyExactBytes(command),
        handle: input.handle,
        password: input.password,
      };
      const response =
        channel === IMPORT_MACHINE_CREDENTIAL_CHANNEL
          ? await options.router.importMachineCredential(copied)
          : await options.router.replaceMachineCredential(copied);
      return new Uint8Array(response);
    });
  }
  options.ipcMain.handle(UNLOCK_MACHINE_CREDENTIAL_CHANNEL, async (event, ...args) => {
    if (!isAcceptedSender(event, options.mainWindow, options.applicationUrl)) {
      throw new IpcBoundaryError();
    }
    if (args.length !== 1) throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
    const input = args[0];
    if (!hasExactKeys(input, ["command", "password"])) {
      throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
    }
    const command = exactByteView(input.command, 8_192);
    if (command === undefined || !isBoundedUtf8String(input.password, 1_024)) {
      throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
    }
    return new Uint8Array(
      await options.router.unlockMachineCredential({
        command: copyExactBytes(command),
        password: input.password,
      }),
    );
  });
  options.ipcMain.handle(IMPORT_SBR_PRODUCT_ID_CHANNEL, async (event, ...args) => {
    if (!isAcceptedSender(event, options.mainWindow, options.applicationUrl)) {
      throw new IpcBoundaryError();
    }
    if (args.length !== 1) throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
    const input = args[0];
    if (!hasExactKeys(input, ["command", "productId"])) {
      throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
    }
    const command = exactByteView(input.command, 8_192);
    if (command === undefined || !isBoundedUtf8String(input.productId, 1_024)) {
      throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
    }
    return new Uint8Array(
      await options.router.importSbrProductId({
        command: copyExactBytes(command),
        productId: input.productId,
      }),
    );
  });
  registrations.set(options.ipcMain, registration);

  return () => {
    if (registrations.get(options.ipcMain) !== registration) return;
    for (const channel of channels) {
      options.ipcMain.removeHandler(channel);
    }
    registrations.delete(options.ipcMain);
  };
}
