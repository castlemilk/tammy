export type LocalApiReadModule =
  | "accounting"
  | "banking"
  | "documents"
  | "organisation"
  | "reporting"
  | "sbr"
  | "system";

export interface LocalApiReadTarget {
  readonly module: LocalApiReadModule;
  readonly procedure: string;
}

export interface LocalApiReadTool {
  readonly description: string;
  readonly input: Readonly<{ kind: "empty" }>;
  readonly name: string;
  readonly readOnly: true;
  readonly target: LocalApiReadTarget;
}

export interface LocalApiReadResource {
  readonly description: string;
  readonly mimeType: "application/json";
  readonly name: string;
  readonly target: LocalApiReadTarget;
  readonly uri: string;
}

function deepFreeze<T>(value: T): T {
  if (typeof value === "object" && value !== null) {
    for (const child of Object.values(value)) deepFreeze(child);
    Object.freeze(value);
  }
  return value;
}

const emptyInput = (): Readonly<{ kind: "empty" }> => ({ kind: "empty" });
const target = <M extends LocalApiReadModule>(
  module: M,
  procedure: string,
): Readonly<{ module: M; procedure: string }> => ({ module, procedure });

export const LOCAL_API_READ_TOOLS = deepFreeze([
  {
    description: "Read the current organisation trial balance.",
    input: emptyInput(),
    name: "tammy.accounting.get_trial_balance",
    readOnly: true,
    target: target("accounting", "getTrialBalance"),
  },
  {
    description: "List accounts for the current organisation.",
    input: emptyInput(),
    name: "tammy.accounting.list_accounts",
    readOnly: true,
    target: target("accounting", "listAccounts"),
  },
  {
    description: "List journals for the current organisation.",
    input: emptyInput(),
    name: "tammy.accounting.list_journals",
    readOnly: true,
    target: target("accounting", "listJournals"),
  },
  {
    description: "Read the banking summary for the current organisation.",
    input: emptyInput(),
    name: "tammy.banking.get_summary",
    readOnly: true,
    target: target("banking", "getBankingSummary"),
  },
  {
    description: "List document metadata without returning document bodies.",
    input: emptyInput(),
    name: "tammy.documents.list_metadata",
    readOnly: true,
    target: target("documents", "listDocuments"),
  },
  {
    description: "Read the signed-in user's public profile.",
    input: emptyInput(),
    name: "tammy.identity.get_current_user",
    readOnly: true,
    target: target("organisation", "getCurrentUser"),
  },
  {
    description: "Read the current organisation.",
    input: emptyInput(),
    name: "tammy.organisation.get_current",
    readOnly: true,
    target: target("organisation", "getOrganisation"),
  },
  {
    description: "Read items requiring attention in the current organisation.",
    input: emptyInput(),
    name: "tammy.reporting.get_attention_summary",
    readOnly: true,
    target: target("reporting", "getAttentionSummary"),
  },
  {
    description: "Read the current BAS draft.",
    input: emptyInput(),
    name: "tammy.reporting.get_current_bas_draft",
    readOnly: true,
    target: target("reporting", "getCurrentBasDraft"),
  },
  {
    description: "Read reporting capabilities available to the current organisation.",
    input: emptyInput(),
    name: "tammy.reporting.get_capability",
    readOnly: true,
    target: target("reporting", "getReportingCapability"),
  },
  {
    description: "Read SBR readiness without exposing credential material.",
    input: emptyInput(),
    name: "tammy.sbr.get_readiness",
    readOnly: true,
    target: target("sbr", "getSbrReadiness"),
  },
  {
    description: "Read local Tammy runtime diagnostics.",
    input: emptyInput(),
    name: "tammy.system.get_diagnostics",
    readOnly: true,
    target: target("system", "getSystemDiagnostics"),
  },
] as const satisfies readonly LocalApiReadTool[]);

export const LOCAL_API_READ_RESOURCES = deepFreeze([
  {
    description: "Accounts for the current organisation.",
    mimeType: "application/json",
    name: "Current accounts",
    target: target("accounting", "listAccounts"),
    uri: "tammy://current/accounts",
  },
  {
    description: "Journals for the current organisation.",
    mimeType: "application/json",
    name: "Current journals",
    target: target("accounting", "listJournals"),
    uri: "tammy://current/journals",
  },
  {
    description: "The current organisation.",
    mimeType: "application/json",
    name: "Current organisation",
    target: target("organisation", "getOrganisation"),
    uri: "tammy://current/organisation",
  },
  {
    description: "Items requiring attention in the current organisation.",
    mimeType: "application/json",
    name: "Reporting attention",
    target: target("reporting", "getAttentionSummary"),
    uri: "tammy://current/reporting/attention",
  },
  {
    description: "The current BAS draft.",
    mimeType: "application/json",
    name: "Current BAS draft",
    target: target("reporting", "getCurrentBasDraft"),
    uri: "tammy://current/reporting/bas-draft",
  },
  {
    description: "SBR readiness without credential material.",
    mimeType: "application/json",
    name: "SBR readiness",
    target: target("sbr", "getSbrReadiness"),
    uri: "tammy://current/sbr/readiness",
  },
  {
    description: "The current organisation trial balance.",
    mimeType: "application/json",
    name: "Current trial balance",
    target: target("accounting", "getTrialBalance"),
    uri: "tammy://current/trial-balance",
  },
  {
    description: "The signed-in user's public profile.",
    mimeType: "application/json",
    name: "Current user",
    target: target("organisation", "getCurrentUser"),
    uri: "tammy://current/user",
  },
  {
    description: "Local Tammy runtime diagnostics.",
    mimeType: "application/json",
    name: "System diagnostics",
    target: target("system", "getSystemDiagnostics"),
    uri: "tammy://system/diagnostics",
  },
] as const satisfies readonly LocalApiReadResource[]);

export type LocalApiReadToolName = (typeof LOCAL_API_READ_TOOLS)[number]["name"];
export type LocalApiReadResourceUri = (typeof LOCAL_API_READ_RESOURCES)[number]["uri"];
