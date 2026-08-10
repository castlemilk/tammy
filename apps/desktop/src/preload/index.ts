import { contextBridge, ipcRenderer } from "electron";

import type { SystemDiagnostics, TammyDesktopAPI } from "../shared/desktop-api";
import { ATTENTION_SUMMARY_CHANNEL, SYSTEM_DIAGNOSTICS_CHANNEL } from "../shared/desktop-api";
import preloadMethods from "../shared/preload-methods.json";

type Invoke = (channel: string, ...args: unknown[]) => Promise<unknown>;

export function createTammyDesktopAPI(invoke: Invoke): TammyDesktopAPI {
  const api = {
    getSystemDiagnostics: () => invoke(SYSTEM_DIAGNOSTICS_CHANNEL) as Promise<SystemDiagnostics>,
    getAttentionSummary: async (request: Uint8Array): Promise<Uint8Array> => {
      if (!(request instanceof Uint8Array)) throw new Error("INVALID_PROTO_FRAME");
      const response = await invoke(ATTENTION_SUMMARY_CHANNEL, new Uint8Array(request));
      if (!(response instanceof Uint8Array)) throw new Error("INVALID_PROTO_FRAME");
      return new Uint8Array(response);
    },
  } satisfies TammyDesktopAPI;
  if (
    Object.keys(api).length !== preloadMethods.length ||
    Object.keys(api).some((method, index) => method !== preloadMethods[index])
  ) {
    throw new Error("PRELOAD_METHODS_MISMATCH");
  }
  return Object.freeze(api);
}

const tammy = createTammyDesktopAPI((channel) => ipcRenderer.invoke(channel));

contextBridge.exposeInMainWorld("tammy", tammy);
