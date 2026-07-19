import type { SystemDiagnostics as CoreSystemDiagnostics } from "../main/core-client";

export type SystemDiagnostics = CoreSystemDiagnostics;

export const SYSTEM_DIAGNOSTICS_CHANNEL = "tammy:system-diagnostics";

export interface TammyDesktopAPI {
  readonly getSystemDiagnostics: () => Promise<SystemDiagnostics>;
}

declare global {
  interface Window {
    readonly tammy: TammyDesktopAPI;
  }
}
