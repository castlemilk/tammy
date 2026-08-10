import { contextBridge, ipcRenderer } from "electron";

import type { SystemDiagnostics, TammyDesktopAPI } from "../shared/desktop-api";
import {
  ATTENTION_SUMMARY_CHANNEL,
  CONFIRM_RECOVERY_CHANNEL,
  CREATE_ACCOUNT_CHANNEL,
  CREATE_ORGANISATION_CHANNEL,
  CREATE_WORKSPACE_CHANNEL,
  GET_JOURNAL_CHANNEL,
  GET_TRIAL_BALANCE_CHANNEL,
  LIST_ACCOUNTS_CHANNEL,
  LIST_JOURNALS_CHANNEL,
  POST_MANUAL_JOURNAL_CHANNEL,
  SIGN_IN_CHANNEL,
  SYSTEM_DIAGNOSTICS_CHANNEL,
  UNLOCK_WORKSPACE_CHANNEL,
} from "../shared/desktop-api";
import preloadMethods from "../shared/preload-methods.json";

type Invoke = (channel: string, ...args: unknown[]) => Promise<unknown>;

export function createTammyDesktopAPI(invoke: Invoke): TammyDesktopAPI {
  const binaryMethod = (channel: string) => async (request: Uint8Array): Promise<Uint8Array> => {
    if (!(request instanceof Uint8Array)) throw new Error("INVALID_PROTO_FRAME");
    const response = await invoke(channel, new Uint8Array(request));
    if (!(response instanceof Uint8Array)) throw new Error("INVALID_PROTO_FRAME");
    return new Uint8Array(response);
  };
  const api = {
    getSystemDiagnostics: () => invoke(SYSTEM_DIAGNOSTICS_CHANNEL) as Promise<SystemDiagnostics>,
    createWorkspace: binaryMethod(CREATE_WORKSPACE_CHANNEL),
    confirmRecovery: binaryMethod(CONFIRM_RECOVERY_CHANNEL),
    unlockWorkspace: binaryMethod(UNLOCK_WORKSPACE_CHANNEL),
    signIn: binaryMethod(SIGN_IN_CHANNEL),
    createOrganisation: binaryMethod(CREATE_ORGANISATION_CHANNEL),
    createAccount: binaryMethod(CREATE_ACCOUNT_CHANNEL),
    listAccounts: binaryMethod(LIST_ACCOUNTS_CHANNEL),
    postManualJournal: binaryMethod(POST_MANUAL_JOURNAL_CHANNEL),
    listJournals: binaryMethod(LIST_JOURNALS_CHANNEL),
    getJournal: binaryMethod(GET_JOURNAL_CHANNEL),
    getTrialBalance: binaryMethod(GET_TRIAL_BALANCE_CHANNEL),
    getAttentionSummary: binaryMethod(ATTENTION_SUMMARY_CHANNEL),
  } satisfies TammyDesktopAPI;
  if (
    Object.keys(api).length !== preloadMethods.length ||
    Object.keys(api).some((method, index) => method !== preloadMethods[index])
  ) {
    throw new Error("PRELOAD_METHODS_MISMATCH");
  }
  return Object.freeze(api);
}

const tammy = createTammyDesktopAPI((channel, ...args) => ipcRenderer.invoke(channel, ...args));

contextBridge.exposeInMainWorld("tammy", tammy);
