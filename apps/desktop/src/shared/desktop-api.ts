import preloadMethods from "./preload-methods.json";

export interface SystemDiagnostics {
  readonly apiVersion: string;
  readonly coreVersion: string;
  readonly runtimeMode: "offline";
  readonly networkRequired: false;
}

export const SYSTEM_DIAGNOSTICS_CHANNEL = "tammy:system-diagnostics";
export const ATTENTION_SUMMARY_CHANNEL = "tammy:overview-attention-summary";

export interface TammyDesktopAPI {
  readonly getSystemDiagnostics: () => Promise<SystemDiagnostics>;
  readonly getAttentionSummary: (request: Uint8Array) => Promise<Uint8Array>;
}

const EXPECTED_DESKTOP_PRELOAD_METHODS = [
  "getSystemDiagnostics",
  "getAttentionSummary",
] as const satisfies readonly (keyof TammyDesktopAPI)[];

if (
  preloadMethods.length !== EXPECTED_DESKTOP_PRELOAD_METHODS.length ||
  preloadMethods.some((method, index) => method !== EXPECTED_DESKTOP_PRELOAD_METHODS[index])
) {
  throw new Error("DESKTOP_PRELOAD_METHODS_MISMATCH");
}

export const DESKTOP_PRELOAD_METHODS = Object.freeze([
  ...preloadMethods,
] as (keyof TammyDesktopAPI)[]);

declare global {
  interface Window {
    readonly tammy: TammyDesktopAPI;
  }
}
