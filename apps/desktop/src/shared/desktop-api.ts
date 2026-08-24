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
export const CREATE_ACCOUNT_CHANNEL = "tammy:accounting-create-account";
export const LIST_ACCOUNTS_CHANNEL = "tammy:accounting-list-accounts";
export const POST_MANUAL_JOURNAL_CHANNEL = "tammy:accounting-post-manual-journal";
export const LIST_JOURNALS_CHANNEL = "tammy:accounting-list-journals";
export const GET_JOURNAL_CHANNEL = "tammy:accounting-get-journal";
export const GET_TRIAL_BALANCE_CHANNEL = "tammy:accounting-get-trial-balance";
export const IMPORT_BANK_STATEMENT_CHANNEL = "tammy:banking-import-statement";
export const LIST_BANK_STATEMENT_LINES_CHANNEL = "tammy:banking-list-lines";
export const MATCH_BANK_STATEMENT_LINE_CHANNEL = "tammy:banking-match-line";
export const COMPLETE_BANK_RECONCILIATION_CHANNEL = "tammy:banking-complete-reconciliation";
export const GET_BANKING_SUMMARY_CHANNEL = "tammy:banking-summary";
export const INGEST_DOCUMENT_CHANNEL = "tammy:documents-ingest";
export const LIST_DOCUMENTS_CHANNEL = "tammy:documents-list";
export const GET_DOCUMENT_CHANNEL = "tammy:documents-get";
export const SAVE_DOCUMENT_REVIEW_CHANNEL = "tammy:documents-save-review";
export const CREATE_BAS_DRAFT_CHANNEL = "tammy:tax-create-bas-draft";
export const GET_CURRENT_BAS_DRAFT_CHANNEL = "tammy:tax-current-bas-draft";
export const ATTENTION_SUMMARY_CHANNEL = "tammy:overview-attention-summary";
export const REPORTING_CAPABILITY_CHANNEL = "tammy:reporting-get-capability";
export const GET_CURRENT_USER_CHANNEL = "tammy:identity-current-user";
export const ENROL_TOTP_CHANNEL = "tammy:identity-enrol-totp";
export const CONFIRM_TOTP_CHANNEL = "tammy:identity-confirm-totp";
export const ASSERT_TOTP_CHANNEL = "tammy:identity-assert-totp";
export const GET_ORGANISATION_CHANNEL = "tammy:organisation-get";
export const RECORD_ENTITY_VERIFICATION_CHANNEL = "tammy:organisation-record-verification";
export const GET_SBR_READINESS_CHANNEL = "tammy:sbr-readiness";
export const GET_MACHINE_CREDENTIAL_STATUS_CHANNEL = "tammy:sbr-credential-status";
export const REMOVE_MACHINE_CREDENTIAL_CHANNEL = "tammy:sbr-credential-remove";
export const REMOVE_SBR_PRODUCT_ID_CHANNEL = "tammy:sbr-product-id-remove";
export const RUN_SBR_READINESS_FIXTURE_CHANNEL = "tammy:sbr-run-fixture";
export const SELECT_MACHINE_CREDENTIAL_FILE_CHANNEL = "tammy:sbr-credential-select";
export const IMPORT_MACHINE_CREDENTIAL_CHANNEL = "tammy:sbr-credential-import";
export const REPLACE_MACHINE_CREDENTIAL_CHANNEL = "tammy:sbr-credential-replace";
export const UNLOCK_MACHINE_CREDENTIAL_CHANNEL = "tammy:sbr-credential-unlock";
export const IMPORT_SBR_PRODUCT_ID_CHANNEL = "tammy:sbr-product-id-import";

export const DESKTOP_PROTO_CHANNELS = Object.freeze([
  CREATE_WORKSPACE_CHANNEL,
  CONFIRM_RECOVERY_CHANNEL,
  UNLOCK_WORKSPACE_CHANNEL,
  SIGN_IN_CHANNEL,
  CREATE_ORGANISATION_CHANNEL,
  CREATE_ACCOUNT_CHANNEL,
  LIST_ACCOUNTS_CHANNEL,
  POST_MANUAL_JOURNAL_CHANNEL,
  LIST_JOURNALS_CHANNEL,
  GET_JOURNAL_CHANNEL,
  GET_TRIAL_BALANCE_CHANNEL,
  IMPORT_BANK_STATEMENT_CHANNEL,
  LIST_BANK_STATEMENT_LINES_CHANNEL,
  MATCH_BANK_STATEMENT_LINE_CHANNEL,
  COMPLETE_BANK_RECONCILIATION_CHANNEL,
  GET_BANKING_SUMMARY_CHANNEL,
  INGEST_DOCUMENT_CHANNEL,
  LIST_DOCUMENTS_CHANNEL,
  GET_DOCUMENT_CHANNEL,
  SAVE_DOCUMENT_REVIEW_CHANNEL,
  CREATE_BAS_DRAFT_CHANNEL,
  GET_CURRENT_BAS_DRAFT_CHANNEL,
  REPORTING_CAPABILITY_CHANNEL,
  ATTENTION_SUMMARY_CHANNEL,
  GET_CURRENT_USER_CHANNEL,
  ENROL_TOTP_CHANNEL,
  CONFIRM_TOTP_CHANNEL,
  ASSERT_TOTP_CHANNEL,
  GET_ORGANISATION_CHANNEL,
  RECORD_ENTITY_VERIFICATION_CHANNEL,
  GET_SBR_READINESS_CHANNEL,
  GET_MACHINE_CREDENTIAL_STATUS_CHANNEL,
  REMOVE_MACHINE_CREDENTIAL_CHANNEL,
  REMOVE_SBR_PRODUCT_ID_CHANNEL,
  RUN_SBR_READINESS_FIXTURE_CHANNEL,
] as const);

export const DESKTOP_PROTO_REQUEST_LIMITS = Object.freeze([
  32_768,
  16_384,
  16_384,
  16_384,
  32_768,
  32_768,
  16_384,
  131_072,
  16_384,
  8_192,
  8_192,
  262_144,
  16_384,
  16_384,
  16_384,
  16_384,
  11 * 1024 * 1024,
  16_384,
  8_192,
  32_768,
  16_384,
  16_384,
  8_192,
  8_192,
  8_192,
  8_192,
  8_192,
  8_192,
  8_192,
  Math.floor(1.1 * 1024 * 1024),
  8_192,
  8_192,
  8_192,
  8_192,
  8_192,
] as const);

if (DESKTOP_PROTO_REQUEST_LIMITS.length !== DESKTOP_PROTO_CHANNELS.length) {
  throw new Error("DESKTOP_PROTO_REQUEST_LIMITS_MISMATCH");
}

export const DESKTOP_MEDIATED_CHANNELS = Object.freeze([
  SELECT_MACHINE_CREDENTIAL_FILE_CHANNEL,
  IMPORT_MACHINE_CREDENTIAL_CHANNEL,
  REPLACE_MACHINE_CREDENTIAL_CHANNEL,
  UNLOCK_MACHINE_CREDENTIAL_CHANNEL,
  IMPORT_SBR_PRODUCT_ID_CHANNEL,
] as const);

export type MachineCredentialFileSelection =
  | Readonly<{ selected: false }>
  | Readonly<{ selected: true; handle: string }>;

export interface MachineCredentialMutationInput {
  readonly command: Uint8Array;
  readonly handle: string;
  readonly password: string;
}

export interface MachineCredentialUnlockInput {
  readonly command: Uint8Array;
  readonly password: string;
}

export interface SbrProductIdImportInput {
  readonly command: Uint8Array;
  readonly productId: string;
}

export function isBoundedUtf8String(value: unknown, maximumBytes: number): value is string {
  if (
    typeof value !== "string" ||
    !Number.isSafeInteger(maximumBytes) ||
    maximumBytes < 0 ||
    value.length > maximumBytes
  ) {
    return false;
  }

  for (let index = 0; index < value.length; index += 1) {
    const codeUnit = value.charCodeAt(index);
    if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
      if (index + 1 >= value.length) return false;
      const trailing = value.charCodeAt(index + 1);
      if (trailing < 0xdc00 || trailing > 0xdfff) return false;
      index += 1;
    } else if (codeUnit >= 0xdc00 && codeUnit <= 0xdfff) {
      return false;
    }
  }

  const scratch = new Uint8Array(maximumBytes + 1);
  try {
    const encoded = new TextEncoder().encodeInto(value, scratch);
    return encoded.read === value.length && encoded.written <= maximumBytes;
  } finally {
    scratch.fill(0);
  }
}

export interface TammyDesktopAPI {
  readonly getSystemDiagnostics: () => Promise<SystemDiagnostics>;
  readonly createWorkspace: (request: Uint8Array) => Promise<Uint8Array>;
  readonly confirmRecovery: (request: Uint8Array) => Promise<Uint8Array>;
  readonly unlockWorkspace: (request: Uint8Array) => Promise<Uint8Array>;
  readonly signIn: (request: Uint8Array) => Promise<Uint8Array>;
  readonly createOrganisation: (request: Uint8Array) => Promise<Uint8Array>;
  readonly createAccount: (request: Uint8Array) => Promise<Uint8Array>;
  readonly listAccounts: (request: Uint8Array) => Promise<Uint8Array>;
  readonly postManualJournal: (request: Uint8Array) => Promise<Uint8Array>;
  readonly listJournals: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getJournal: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getTrialBalance: (request: Uint8Array) => Promise<Uint8Array>;
  readonly importBankStatement: (request: Uint8Array) => Promise<Uint8Array>;
  readonly listBankStatementLines: (request: Uint8Array) => Promise<Uint8Array>;
  readonly matchBankStatementLine: (request: Uint8Array) => Promise<Uint8Array>;
  readonly completeBankReconciliation: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getBankingSummary: (request: Uint8Array) => Promise<Uint8Array>;
  readonly ingestDocument: (request: Uint8Array) => Promise<Uint8Array>;
  readonly listDocuments: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getDocument: (request: Uint8Array) => Promise<Uint8Array>;
  readonly saveDocumentReview: (request: Uint8Array) => Promise<Uint8Array>;
  readonly createBasDraft: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getCurrentBasDraft: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getReportingCapability: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getAttentionSummary: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getCurrentUser: (request: Uint8Array) => Promise<Uint8Array>;
  readonly enrolTotp: (request: Uint8Array) => Promise<Uint8Array>;
  readonly confirmTotp: (request: Uint8Array) => Promise<Uint8Array>;
  readonly assertTotp: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getOrganisation: (request: Uint8Array) => Promise<Uint8Array>;
  readonly recordEntityVerification: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getSbrReadiness: (request: Uint8Array) => Promise<Uint8Array>;
  readonly getMachineCredentialStatus: (request: Uint8Array) => Promise<Uint8Array>;
  readonly removeMachineCredential: (request: Uint8Array) => Promise<Uint8Array>;
  readonly removeSbrProductId: (request: Uint8Array) => Promise<Uint8Array>;
  readonly runSbrReadinessFixture: (request: Uint8Array) => Promise<Uint8Array>;
  readonly selectMachineCredentialFile: () => Promise<MachineCredentialFileSelection>;
  readonly importMachineCredential: (input: MachineCredentialMutationInput) => Promise<Uint8Array>;
  readonly replaceMachineCredential: (input: MachineCredentialMutationInput) => Promise<Uint8Array>;
  readonly unlockMachineCredential: (input: MachineCredentialUnlockInput) => Promise<Uint8Array>;
  readonly importSbrProductId: (input: SbrProductIdImportInput) => Promise<Uint8Array>;
}

const EXPECTED_DESKTOP_PRELOAD_METHODS = [
  "getSystemDiagnostics",
  "createWorkspace",
  "confirmRecovery",
  "unlockWorkspace",
  "signIn",
  "createOrganisation",
  "createAccount",
  "listAccounts",
  "postManualJournal",
  "listJournals",
  "getJournal",
  "getTrialBalance",
  "importBankStatement",
  "listBankStatementLines",
  "matchBankStatementLine",
  "completeBankReconciliation",
  "getBankingSummary",
  "ingestDocument",
  "listDocuments",
  "getDocument",
  "saveDocumentReview",
  "createBasDraft",
  "getCurrentBasDraft",
  "getReportingCapability",
  "getAttentionSummary",
  "getCurrentUser",
  "enrolTotp",
  "confirmTotp",
  "assertTotp",
  "getOrganisation",
  "recordEntityVerification",
  "getSbrReadiness",
  "getMachineCredentialStatus",
  "removeMachineCredential",
  "removeSbrProductId",
  "runSbrReadinessFixture",
  "selectMachineCredentialFile",
  "importMachineCredential",
  "replaceMachineCredential",
  "unlockMachineCredential",
  "importSbrProductId",
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
