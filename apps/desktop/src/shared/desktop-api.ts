export interface SystemDiagnostics {
  readonly apiVersion: string;
  readonly coreVersion: string;
  readonly runtimeMode: "offline";
  readonly networkRequired: false;
}

export const SYSTEM_DIAGNOSTICS_CHANNEL = "tammy:system-diagnostics";

export interface TammyDesktopAPI {
  readonly getSystemDiagnostics: () => Promise<SystemDiagnostics>;
}

declare global {
  interface Window {
    readonly tammy: TammyDesktopAPI;
  }
}
