import { contextBridge, ipcRenderer } from "electron";

import type { SystemDiagnostics, TammyDesktopAPI } from "../shared/desktop-api";
import { SYSTEM_DIAGNOSTICS_CHANNEL } from "../shared/desktop-api";
import preloadMethods from "../shared/preload-methods.json";

type Invoke = (channel: string) => Promise<SystemDiagnostics>;

export function createTammyDesktopAPI(invoke: Invoke): TammyDesktopAPI {
  const api = {
    getSystemDiagnostics: () => invoke(SYSTEM_DIAGNOSTICS_CHANNEL),
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
