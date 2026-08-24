import { contextBridge, ipcRenderer } from "electron";

import type {
  MachineCredentialFileSelection,
  MachineCredentialMutationInput,
  MachineCredentialUnlockInput,
  SbrProductIdImportInput,
  SystemDiagnostics,
  TammyDesktopAPI,
} from "../shared/desktop-api";
import {
  ASSERT_TOTP_CHANNEL,
  ATTENTION_SUMMARY_CHANNEL,
  COMPLETE_BANK_RECONCILIATION_CHANNEL,
  CONFIRM_RECOVERY_CHANNEL,
  CONFIRM_TOTP_CHANNEL,
  CREATE_ACCOUNT_CHANNEL,
  CREATE_BAS_DRAFT_CHANNEL,
  CREATE_ORGANISATION_CHANNEL,
  CREATE_WORKSPACE_CHANNEL,
  ENROL_TOTP_CHANNEL,
  GET_BANKING_SUMMARY_CHANNEL,
  GET_CURRENT_BAS_DRAFT_CHANNEL,
  GET_CURRENT_USER_CHANNEL,
  GET_DOCUMENT_CHANNEL,
  GET_JOURNAL_CHANNEL,
  GET_MACHINE_CREDENTIAL_STATUS_CHANNEL,
  GET_ORGANISATION_CHANNEL,
  GET_SBR_READINESS_CHANNEL,
  GET_TRIAL_BALANCE_CHANNEL,
  IMPORT_BANK_STATEMENT_CHANNEL,
  IMPORT_MACHINE_CREDENTIAL_CHANNEL,
  IMPORT_SBR_PRODUCT_ID_CHANNEL,
  INGEST_DOCUMENT_CHANNEL,
  isBoundedUtf8String,
  LIST_ACCOUNTS_CHANNEL,
  LIST_BANK_STATEMENT_LINES_CHANNEL,
  LIST_DOCUMENTS_CHANNEL,
  LIST_JOURNALS_CHANNEL,
  MATCH_BANK_STATEMENT_LINE_CHANNEL,
  POST_MANUAL_JOURNAL_CHANNEL,
  RECORD_ENTITY_VERIFICATION_CHANNEL,
  REMOVE_MACHINE_CREDENTIAL_CHANNEL,
  REMOVE_SBR_PRODUCT_ID_CHANNEL,
  REPLACE_MACHINE_CREDENTIAL_CHANNEL,
  REPORTING_CAPABILITY_CHANNEL,
  RUN_SBR_READINESS_FIXTURE_CHANNEL,
  SAVE_DOCUMENT_REVIEW_CHANNEL,
  SELECT_MACHINE_CREDENTIAL_FILE_CHANNEL,
  SIGN_IN_CHANNEL,
  SYSTEM_DIAGNOSTICS_CHANNEL,
  UNLOCK_MACHINE_CREDENTIAL_CHANNEL,
  UNLOCK_WORKSPACE_CHANNEL,
} from "../shared/desktop-api";
import preloadMethods from "../shared/preload-methods.json";
import { parseLaunchScenarioArgument } from "../shared/launch-scenario";

type Invoke = (channel: string, ...args: unknown[]) => Promise<unknown>;

const STANDARD_REQUEST_BYTES = 8_192;
const STANDARD_RESPONSE_BYTES = 32_768;
const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const textEncoder = new TextEncoder();

class PreloadRpcError extends Error {
  public constructor(public readonly code: "CORE_REQUEST_FAILED" | "INVALID_RPC_REQUEST") {
    super(code);
    this.name = "PreloadRpcError";
  }
}

function invalidRequest(): never {
  throw new PreloadRpcError("INVALID_RPC_REQUEST");
}

function coreFailure(): never {
  throw new PreloadRpcError("CORE_REQUEST_FAILED");
}

async function invokeSanitized(
  invoke: Invoke,
  channel: string,
  ...args: unknown[]
): Promise<unknown> {
  try {
    return await invoke(channel, ...args);
  } catch (error) {
    const code =
      typeof error === "object" && error !== null && "code" in error ? error.code : undefined;
    const message = error instanceof Error ? error.message : "";
    if (code === "INVALID_RPC_REQUEST" || message.includes("INVALID_RPC_REQUEST")) {
      invalidRequest();
    }
    coreFailure();
  }
}

function exactKeys(value: unknown, keys: readonly string[]): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const actual = Object.keys(value);
  return actual.length === keys.length && actual.every((key) => keys.includes(key));
}

function boundedString(value: unknown): value is string {
  return isBoundedUtf8String(value, 1_024);
}

function copiedCommand(value: unknown): Uint8Array {
  if (!(value instanceof Uint8Array) || value.byteLength === 0 || value.byteLength > 8_192) {
    invalidRequest();
  }
  return new Uint8Array(value);
}

function checkedBinaryResponse(value: unknown): Uint8Array {
  if (!(value instanceof Uint8Array) || value.byteLength === 0 || value.byteLength > 32_768) {
    coreFailure();
  }
  return new Uint8Array(value);
}

export function createTammyDesktopAPI(invoke: Invoke): TammyDesktopAPI {
  const binaryMethod =
    (channel: string, maximumRequestBytes?: number, maximumResponseBytes?: number) =>
    async (...args: unknown[]): Promise<Uint8Array> => {
      if (args.length !== 1) invalidRequest();
      const request = args[0];
      if (
        !(request instanceof Uint8Array) ||
        request.byteLength === 0 ||
        (maximumRequestBytes !== undefined && request.byteLength > maximumRequestBytes)
      ) {
        invalidRequest();
      }
      const response = await invokeSanitized(invoke, channel, new Uint8Array(request));
      if (
        !(response instanceof Uint8Array) ||
        (maximumResponseBytes !== undefined && response.byteLength > maximumResponseBytes)
      ) {
        coreFailure();
      }
      return new Uint8Array(response);
    };
  const mediatedBinaryMethod =
    <Input>(channel: string, copyInput: (input: Input) => Input) =>
    async (...args: unknown[]): Promise<Uint8Array> => {
      if (args.length !== 1) invalidRequest();
      return checkedBinaryResponse(
        await invokeSanitized(invoke, channel, copyInput(args[0] as Input)),
      );
    };
  const copyCredentialMutation = (
    input: MachineCredentialMutationInput,
  ): MachineCredentialMutationInput => {
    if (
      !exactKeys(input, ["command", "handle", "password"]) ||
      typeof input.handle !== "string" ||
      !UUID_V7.test(input.handle) ||
      !boundedString(input.password)
    ) {
      invalidRequest();
    }
    return Object.freeze({
      command: copiedCommand(input.command),
      handle: input.handle,
      password: input.password,
    });
  };
  const copyUnlock = (input: MachineCredentialUnlockInput): MachineCredentialUnlockInput => {
    if (!exactKeys(input, ["command", "password"]) || !boundedString(input.password)) {
      invalidRequest();
    }
    return Object.freeze({ command: copiedCommand(input.command), password: input.password });
  };
  const copyProductIdImport = (input: SbrProductIdImportInput): SbrProductIdImportInput => {
    if (!exactKeys(input, ["command", "productId"]) || !boundedString(input.productId)) {
      invalidRequest();
    }
    return Object.freeze({ command: copiedCommand(input.command), productId: input.productId });
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
    importBankStatement: binaryMethod(IMPORT_BANK_STATEMENT_CHANNEL),
    listBankStatementLines: binaryMethod(LIST_BANK_STATEMENT_LINES_CHANNEL),
    matchBankStatementLine: binaryMethod(MATCH_BANK_STATEMENT_LINE_CHANNEL),
    completeBankReconciliation: binaryMethod(COMPLETE_BANK_RECONCILIATION_CHANNEL),
    getBankingSummary: binaryMethod(GET_BANKING_SUMMARY_CHANNEL),
    ingestDocument: binaryMethod(INGEST_DOCUMENT_CHANNEL),
    listDocuments: binaryMethod(LIST_DOCUMENTS_CHANNEL),
    getDocument: binaryMethod(GET_DOCUMENT_CHANNEL),
    saveDocumentReview: binaryMethod(SAVE_DOCUMENT_REVIEW_CHANNEL),
    createBasDraft: binaryMethod(CREATE_BAS_DRAFT_CHANNEL),
    getCurrentBasDraft: binaryMethod(GET_CURRENT_BAS_DRAFT_CHANNEL),
    getReportingCapability: binaryMethod(REPORTING_CAPABILITY_CHANNEL),
    getAttentionSummary: binaryMethod(ATTENTION_SUMMARY_CHANNEL),
    getCurrentUser: binaryMethod(
      GET_CURRENT_USER_CHANNEL,
      STANDARD_REQUEST_BYTES,
      STANDARD_RESPONSE_BYTES,
    ),
    enrolTotp: binaryMethod(ENROL_TOTP_CHANNEL, STANDARD_REQUEST_BYTES, STANDARD_RESPONSE_BYTES),
    confirmTotp: binaryMethod(
      CONFIRM_TOTP_CHANNEL,
      STANDARD_REQUEST_BYTES,
      STANDARD_RESPONSE_BYTES,
    ),
    assertTotp: binaryMethod(ASSERT_TOTP_CHANNEL, STANDARD_REQUEST_BYTES, STANDARD_RESPONSE_BYTES),
    getOrganisation: binaryMethod(
      GET_ORGANISATION_CHANNEL,
      STANDARD_REQUEST_BYTES,
      STANDARD_RESPONSE_BYTES,
    ),
    recordEntityVerification: binaryMethod(
      RECORD_ENTITY_VERIFICATION_CHANNEL,
      Math.floor(1.1 * 1024 * 1024),
      STANDARD_RESPONSE_BYTES,
    ),
    getSbrReadiness: binaryMethod(
      GET_SBR_READINESS_CHANNEL,
      STANDARD_REQUEST_BYTES,
      STANDARD_RESPONSE_BYTES,
    ),
    getMachineCredentialStatus: binaryMethod(
      GET_MACHINE_CREDENTIAL_STATUS_CHANNEL,
      STANDARD_REQUEST_BYTES,
      STANDARD_RESPONSE_BYTES,
    ),
    removeMachineCredential: binaryMethod(
      REMOVE_MACHINE_CREDENTIAL_CHANNEL,
      STANDARD_REQUEST_BYTES,
      STANDARD_RESPONSE_BYTES,
    ),
    removeSbrProductId: binaryMethod(
      REMOVE_SBR_PRODUCT_ID_CHANNEL,
      STANDARD_REQUEST_BYTES,
      STANDARD_RESPONSE_BYTES,
    ),
    runSbrReadinessFixture: binaryMethod(
      RUN_SBR_READINESS_FIXTURE_CHANNEL,
      STANDARD_REQUEST_BYTES,
      STANDARD_RESPONSE_BYTES,
    ),
    selectMachineCredentialFile: async (
      ...args: unknown[]
    ): Promise<MachineCredentialFileSelection> => {
      if (args.length !== 0) invalidRequest();
      const response = await invokeSanitized(invoke, SELECT_MACHINE_CREDENTIAL_FILE_CHANNEL);
      let responseBytes: number;
      try {
        responseBytes = textEncoder.encode(JSON.stringify(response)).byteLength;
      } catch {
        coreFailure();
      }
      if (responseBytes > 128) coreFailure();
      if (exactKeys(response, ["selected"]) && response.selected === false) {
        return Object.freeze({ selected: false });
      }
      if (
        exactKeys(response, ["selected", "handle"]) &&
        response.selected === true &&
        typeof response.handle === "string" &&
        UUID_V7.test(response.handle)
      ) {
        return Object.freeze({ selected: true, handle: response.handle });
      }
      coreFailure();
    },
    importMachineCredential: mediatedBinaryMethod(
      IMPORT_MACHINE_CREDENTIAL_CHANNEL,
      copyCredentialMutation,
    ),
    replaceMachineCredential: mediatedBinaryMethod(
      REPLACE_MACHINE_CREDENTIAL_CHANNEL,
      copyCredentialMutation,
    ),
    unlockMachineCredential: mediatedBinaryMethod(UNLOCK_MACHINE_CREDENTIAL_CHANNEL, copyUnlock),
    importSbrProductId: mediatedBinaryMethod(IMPORT_SBR_PRODUCT_ID_CHANNEL, copyProductIdImport),
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
contextBridge.exposeInMainWorld(
  "tammyLaunchScenario",
  parseLaunchScenarioArgument(process.argv),
);
