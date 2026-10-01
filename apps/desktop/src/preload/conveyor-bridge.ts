import type { ConveyorBridge } from "electron-conveyor/renderer";

import {
  CONVEYOR_MANIFEST,
  CONVEYOR_PROCEDURE_CHANNELS,
  type ConveyorManifest,
  type ConveyorProcedureKind,
} from "../shared/conveyor-contract";

interface RestrictedIpcRenderer {
  invoke(channel: string, ...args: unknown[]): Promise<unknown>;
  sendSync(channel: string, ...args: unknown[]): unknown;
  on?(channel: string, listener: (...args: unknown[]) => void): unknown;
}

interface RestrictedContextBridge {
  exposeInMainWorld(key: string, api: ConveyorBridge): void;
}

const INVALID_REQUEST = "INVALID_CONVEYOR_REQUEST";
const INVALID_MANIFEST = "INVALID_CONVEYOR_MANIFEST";
const TRANSPORT_FAILURE = "IPC_TRANSPORT_FAILED";
const SUBSCRIPTIONS_DISABLED = "IPC_SUBSCRIPTIONS_DISABLED";

const channelModules = Object.freeze(
  Object.fromEntries(
    CONVEYOR_PROCEDURE_CHANNELS.map((channel) => [channel, channel.slice("conveyor:".length)]),
  ),
);

function hasOwn(value: object, key: string): boolean {
  return Object.hasOwn(value, key);
}

function invalidRequest(): never {
  throw new Error(INVALID_REQUEST);
}

function invalidManifest(): never {
  throw new Error(INVALID_MANIFEST);
}

function exactDataObject(value: unknown, expectedKeys: readonly string[]): Record<string, unknown> {
  if (
    typeof value !== "object" ||
    value === null ||
    Array.isArray(value) ||
    Object.getPrototypeOf(value) !== Object.prototype
  ) {
    return invalidManifest();
  }
  const ownKeys = Reflect.ownKeys(value);
  if (ownKeys.some((key) => typeof key !== "string")) return invalidManifest();
  const actualKeys = (ownKeys as string[]).sort();
  const sortedExpected = [...expectedKeys].sort();
  if (
    actualKeys.length !== sortedExpected.length ||
    actualKeys.some((key, index) => key !== sortedExpected[index])
  ) {
    return invalidManifest();
  }
  const copy: Record<string, unknown> = {};
  for (const key of expectedKeys) {
    const descriptor = Object.getOwnPropertyDescriptor(value, key);
    if (descriptor === undefined || !("value" in descriptor)) return invalidManifest();
    copy[key] = descriptor.value;
  }
  return copy;
}

function freezeManifest(value: unknown): ConveyorManifest {
  const source = exactDataObject(value, Object.keys(CONVEYOR_MANIFEST));
  const copy: Record<string, Record<string, ConveyorProcedureKind>> = {};
  for (const module of Object.keys(CONVEYOR_MANIFEST)) {
    const expectedProcedures = CONVEYOR_MANIFEST[module]!;
    const procedures = exactDataObject(source[module], Object.keys(expectedProcedures));
    const procedureCopy: Record<string, ConveyorProcedureKind> = {};
    for (const method of Object.keys(expectedProcedures)) {
      const kind = procedures[method];
      if ((kind !== "query" && kind !== "command") || kind !== expectedProcedures[method])
        return invalidManifest();
      procedureCopy[method] = kind;
    }
    copy[module] = Object.freeze(procedureCopy);
  }
  return Object.freeze(copy);
}

function expectedArity(channel: string, method: string): 0 | 1 {
  if (channel === "conveyor:system" && method === "getSystemDiagnostics") return 0;
  if (channel === "conveyor:sbr" && method === "selectMachineCredentialFile") return 0;
  return 1;
}

function validateProcedure(channel: unknown, method: unknown, args: readonly unknown[]): void {
  if (typeof channel !== "string" || typeof method !== "string" || !hasOwn(channelModules, channel))
    invalidRequest();
  const module = channelModules[channel];
  if (module === undefined || !hasOwn(CONVEYOR_MANIFEST[module]!, method)) invalidRequest();
  if (args.length !== expectedArity(channel, method)) invalidRequest();
}

export function createRestrictedConveyorBridge(ipcRenderer: RestrictedIpcRenderer): ConveyorBridge {
  const bridge: ConveyorBridge = {
    invoke: async (channel, method, ...args) => {
      validateProcedure(channel, method, args);
      try {
        return await ipcRenderer.invoke(channel, method, ...args);
      } catch {
        throw new Error(TRANSPORT_FAILURE);
      }
    },
    manifest: () => {
      let received: unknown;
      try {
        received = ipcRenderer.sendSync("conveyor:manifest");
      } catch {
        throw new Error(TRANSPORT_FAILURE);
      }
      return freezeManifest(received);
    },
    subscribe: () => {
      throw new Error(SUBSCRIPTIONS_DISABLED);
    },
  };
  return Object.freeze(bridge);
}

export function exposeRestrictedConveyorBridge(
  contextBridge: RestrictedContextBridge,
  bridge: ConveyorBridge,
): void {
  contextBridge.exposeInMainWorld("conveyor", bridge);
}
