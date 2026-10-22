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
] as const);

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
