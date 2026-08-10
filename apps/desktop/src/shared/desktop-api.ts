import preloadMethods from "./preload-methods.json";

export interface SystemDiagnostics {
  readonly apiVersion: string;
  readonly coreVersion: string;
  readonly runtimeMode: "offline";
  readonly networkRequired: false;
}

export const SYSTEM_DIAGNOSTICS_CHANNEL = "tammy:system-diagnostics";
export const CREATE_WORKSPACE_CHANNEL = "tammy:workspace-create";
export const CONFIRM_RECOVERY_CHANNEL = "tammy:workspace-confirm-recovery";
export const UNLOCK_WORKSPACE_CHANNEL = "tammy:workspace-unlock";
export const SIGN_IN_CHANNEL = "tammy:identity-sign-in";
export const CREATE_ORGANISATION_CHANNEL = "tammy:organisation-create";
export const LIST_ACCOUNTS_CHANNEL = "tammy:accounting-list-accounts";
export const ATTENTION_SUMMARY_CHANNEL = "tammy:overview-attention-summary";

export const DESKTOP_PROTO_CHANNELS = Object.freeze([
  CREATE_WORKSPACE_CHANNEL,
  CONFIRM_RECOVERY_CHANNEL,
  UNLOCK_WORKSPACE_CHANNEL,
  SIGN_IN_CHANNEL,
  CREATE_ORGANISATION_CHANNEL,
  LIST_ACCOUNTS_CHANNEL,
  ATTENTION_SUMMARY_CHANNEL,
] as const);

export interface TammyDesktopAPI {
  readonly getSystemDiagnostics: () => Promise<SystemDiagnostics>;
  readonly createWorkspace: (request: Uint8Array) => Promise<Uint8Array>;
  readonly confirmRecovery: (request: Uint8Array) => Promise<Uint8Array>;
  readonly unlockWorkspace: (request: Uint8Array) => Promise<Uint8Array>;
  readonly signIn: (request: Uint8Array) => Promise<Uint8Array>;
  readonly createOrganisation: (request: Uint8Array) => Promise<Uint8Array>;
  readonly listAccounts: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getAttentionSummary: (request: Uint8Array) => Promise<Uint8Array>;
}

const EXPECTED_DESKTOP_PRELOAD_METHODS = [
  "getSystemDiagnostics",
  "createWorkspace",
  "confirmRecovery",
  "unlockWorkspace",
  "signIn",
  "createOrganisation",
  "listAccounts",
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
