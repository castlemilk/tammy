import { contextBridge, ipcRenderer } from "electron";

import type { TammyDesktopAPI } from "../shared/desktop-api";
import { SYSTEM_DIAGNOSTICS_CHANNEL } from "../shared/desktop-api";

const tammy = Object.freeze({
  getSystemDiagnostics: () => ipcRenderer.invoke(SYSTEM_DIAGNOSTICS_CHANNEL),
}) satisfies TammyDesktopAPI;

contextBridge.exposeInMainWorld("tammy", tammy);
