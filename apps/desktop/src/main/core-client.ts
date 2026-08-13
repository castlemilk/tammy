import { ConnectError, createClient, type Interceptor, type Transport } from "@connectrpc/connect";
import { type ConnectTransportOptions, createConnectTransport } from "@connectrpc/connect-node";
import type {
  CreateAccountRequest,
  CreateAccountResponse,
  GetJournalRequest,
  GetJournalResponse,
  GetTrialBalanceRequest,
  GetTrialBalanceResponse,
  ListAccountsRequest,
  ListAccountsResponse,
  ListJournalsRequest,
  ListJournalsResponse,
  PostManualJournalRequest,
  PostManualJournalResponse,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { AccountingService } from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import type {
  CompleteBankReconciliationRequest,
  CompleteBankReconciliationResponse,
  GetBankingSummaryRequest,
  GetBankingSummaryResponse,
  ImportBankStatementRequest,
  ImportBankStatementResponse,
  ListBankStatementLinesRequest,
  ListBankStatementLinesResponse,
  MatchBankStatementLineRequest,
  MatchBankStatementLineResponse,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
import { BankingService } from "@tammy/connect-client/tammy/v1/banking_pb.js";
import type {
  GetDocumentRequest,
  GetDocumentResponse,
  IngestDocumentRequest,
  IngestDocumentResponse,
  ListDocumentsRequest,
  ListDocumentsResponse,
  SaveDocumentReviewRequest,
  SaveDocumentReviewResponse,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { DocumentService } from "@tammy/connect-client/tammy/v1/documents_pb.js";
import type { SignInRequest, SignInResponse } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { IdentityService } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import type {
  CreateOrganisationRequest,
  CreateOrganisationResponse,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { OrganisationService } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import type {
  GetAttentionSummaryRequest,
  GetAttentionSummaryResponse,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import { OverviewService } from "@tammy/connect-client/tammy/v1/overview_pb.js";
import type {
  GetReportingCapabilityRequest,
  GetReportingCapabilityResponse,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import { ReportingCapabilityService } from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import { RuntimeMode, SystemService } from "@tammy/connect-client/tammy/v1/system_pb.js";
import type {
  CreateBasDraftRequest,
  CreateBasDraftResponse,
  GetCurrentBasDraftRequest,
  GetCurrentBasDraftResponse,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { TaxService } from "@tammy/connect-client/tammy/v1/tax_pb.js";
import type {
  ConfirmRecoveryRequest,
  ConfirmRecoveryResponse,
  CreateWorkspaceRequest,
  CreateWorkspaceResponse,
  UnlockWorkspaceRequest,
  UnlockWorkspaceResponse,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { WorkspaceService } from "@tammy/connect-client/tammy/v1/workspace_pb.js";

import type { SystemDiagnostics } from "../shared/desktop-api";
import type { CoreReadiness } from "../shared/readiness";

export type { SystemDiagnostics } from "../shared/desktop-api";

const CAPABILITY_HEADER = "X-Tammy-Capability";
const EXPECTED_API_VERSION = "tammy.v1";
const CORE_VERSION_PATTERN = /^[\x20-\x7e]{1,128}$/;

export interface CoreClient {
  readonly createAccount: (request: CreateAccountRequest) => Promise<CreateAccountResponse>;
  readonly listAccounts: (request: ListAccountsRequest) => Promise<ListAccountsResponse>;
  readonly postManualJournal: (
    request: PostManualJournalRequest,
  ) => Promise<PostManualJournalResponse>;
  readonly listJournals: (request: ListJournalsRequest) => Promise<ListJournalsResponse>;
  readonly getJournal: (request: GetJournalRequest) => Promise<GetJournalResponse>;
  readonly getTrialBalance: (request: GetTrialBalanceRequest) => Promise<GetTrialBalanceResponse>;
  readonly importBankStatement: (
    request: ImportBankStatementRequest,
  ) => Promise<ImportBankStatementResponse>;
  readonly listBankStatementLines: (
    request: ListBankStatementLinesRequest,
  ) => Promise<ListBankStatementLinesResponse>;
  readonly matchBankStatementLine: (
    request: MatchBankStatementLineRequest,
  ) => Promise<MatchBankStatementLineResponse>;
  readonly completeBankReconciliation: (
    request: CompleteBankReconciliationRequest,
  ) => Promise<CompleteBankReconciliationResponse>;
  readonly getBankingSummary: (
    request: GetBankingSummaryRequest,
  ) => Promise<GetBankingSummaryResponse>;
  readonly ingestDocument: (request: IngestDocumentRequest) => Promise<IngestDocumentResponse>;
  readonly listDocuments: (request: ListDocumentsRequest) => Promise<ListDocumentsResponse>;
  readonly getDocument: (request: GetDocumentRequest) => Promise<GetDocumentResponse>;
  readonly saveDocumentReview: (
    request: SaveDocumentReviewRequest,
  ) => Promise<SaveDocumentReviewResponse>;
  readonly createBasDraft: (request: CreateBasDraftRequest) => Promise<CreateBasDraftResponse>;
  readonly getCurrentBasDraft: (
    request: GetCurrentBasDraftRequest,
  ) => Promise<GetCurrentBasDraftResponse>;
  readonly createWorkspace: (request: CreateWorkspaceRequest) => Promise<CreateWorkspaceResponse>;
  readonly confirmRecovery: (request: ConfirmRecoveryRequest) => Promise<ConfirmRecoveryResponse>;
  readonly unlockWorkspace: (request: UnlockWorkspaceRequest) => Promise<UnlockWorkspaceResponse>;
  readonly signIn: (request: SignInRequest) => Promise<SignInResponse>;
  readonly createOrganisation: (
    request: CreateOrganisationRequest,
  ) => Promise<CreateOrganisationResponse>;
  readonly getAttentionSummary: (
    request: GetAttentionSummaryRequest,
  ) => Promise<GetAttentionSummaryResponse>;
  readonly getReportingCapability: (
    request: GetReportingCapabilityRequest,
  ) => Promise<GetReportingCapabilityResponse>;
  readonly getDiagnostics: () => Promise<SystemDiagnostics>;
}

export type CoreTransportFactory = (options: ConnectTransportOptions) => Transport;

export type CoreClientErrorCode = "INVALID_DIAGNOSTICS";

const ERROR_MESSAGES: Readonly<Record<CoreClientErrorCode, string>> = {
  INVALID_DIAGNOSTICS: "Core returned invalid diagnostics.",
};

export class CoreClientError extends Error {
  public readonly code: CoreClientErrorCode;

  public constructor(code: CoreClientErrorCode) {
    super(ERROR_MESSAGES[code]);
    this.name = "CoreClientError";
    this.code = code;
  }
}

export function capabilityInterceptor(capability: string): Interceptor {
  return (next) => async (request) => {
    request.header.set(CAPABILITY_HEADER, capability);
    return next(request);
  };
}

export function createCoreClient(
  readiness: Readonly<CoreReadiness>,
  transportFactory: CoreTransportFactory = createConnectTransport,
): Readonly<CoreClient> {
  const transport = transportFactory({
    baseUrl: `https://127.0.0.1:${readiness.port}`,
    httpVersion: "1.1",
    defaultTimeoutMs: 5_000,
    nodeOptions: {
      ca: readiness.caPem,
      rejectUnauthorized: true,
      minVersion: "TLSv1.3",
      maxVersion: "TLSv1.3",
    },
    interceptors: [capabilityInterceptor(readiness.capability)],
  });
  const systemClient = createClient(SystemService, transport);
  const accountingClient = createClient(AccountingService, transport);
  const bankingClient = createClient(BankingService, transport);
  const documentClient = createClient(DocumentService, transport);
  const overviewClient = createClient(OverviewService, transport);
  const reportingCapabilityClient = createClient(ReportingCapabilityService, transport);
  const workspaceClient = createClient(WorkspaceService, transport);
  const identityClient = createClient(IdentityService, transport);
  const organisationClient = createClient(OrganisationService, transport);
  const taxClient = createClient(TaxService, transport);

  const coreRequest = async <Response>(request: () => Promise<Response>): Promise<Response> => {
    try {
      return await request();
    } catch (error) {
      throw new ConnectError("Core request failed.", ConnectError.from(error).code);
    }
  };

  return Object.freeze({
    createAccount: (request: CreateAccountRequest) =>
      coreRequest(() => accountingClient.createAccount(request)),
    listAccounts: (request: ListAccountsRequest) =>
      coreRequest(() => accountingClient.listAccounts(request)),
    postManualJournal: (request: PostManualJournalRequest) =>
      coreRequest(() => accountingClient.postManualJournal(request)),
    listJournals: (request: ListJournalsRequest) =>
      coreRequest(() => accountingClient.listJournals(request)),
    getJournal: (request: GetJournalRequest) =>
      coreRequest(() => accountingClient.getJournal(request)),
    getTrialBalance: (request: GetTrialBalanceRequest) =>
      coreRequest(() => accountingClient.getTrialBalance(request)),
    importBankStatement: (request: ImportBankStatementRequest) =>
      coreRequest(() => bankingClient.importBankStatement(request)),
    listBankStatementLines: (request: ListBankStatementLinesRequest) =>
      coreRequest(() => bankingClient.listBankStatementLines(request)),
    matchBankStatementLine: (request: MatchBankStatementLineRequest) =>
      coreRequest(() => bankingClient.matchBankStatementLine(request)),
    completeBankReconciliation: (request: CompleteBankReconciliationRequest) =>
      coreRequest(() => bankingClient.completeBankReconciliation(request)),
    getBankingSummary: (request: GetBankingSummaryRequest) =>
      coreRequest(() => bankingClient.getBankingSummary(request)),
    ingestDocument: (request: IngestDocumentRequest) =>
      coreRequest(() => documentClient.ingestDocument(request)),
    listDocuments: (request: ListDocumentsRequest) =>
      coreRequest(() => documentClient.listDocuments(request)),
    getDocument: (request: GetDocumentRequest) =>
      coreRequest(() => documentClient.getDocument(request)),
    saveDocumentReview: (request: SaveDocumentReviewRequest) =>
      coreRequest(() => documentClient.saveDocumentReview(request)),
    createBasDraft: (request: CreateBasDraftRequest) =>
      coreRequest(() => taxClient.createBasDraft(request)),
    getCurrentBasDraft: (request: GetCurrentBasDraftRequest) =>
      coreRequest(() => taxClient.getCurrentBasDraft(request)),
    createWorkspace: (request: CreateWorkspaceRequest) =>
      coreRequest(() => workspaceClient.createWorkspace(request)),
    confirmRecovery: (request: ConfirmRecoveryRequest) =>
      coreRequest(() => workspaceClient.confirmRecovery(request)),
    unlockWorkspace: (request: UnlockWorkspaceRequest) =>
      coreRequest(() => workspaceClient.unlockWorkspace(request)),
    signIn: (request: SignInRequest) => coreRequest(() => identityClient.signIn(request)),
    createOrganisation: (request: CreateOrganisationRequest) =>
      coreRequest(() => organisationClient.createOrganisation(request)),
    getAttentionSummary: async (
      request: GetAttentionSummaryRequest,
    ): Promise<GetAttentionSummaryResponse> => {
      return coreRequest(() => overviewClient.getAttentionSummary(request));
    },
    getReportingCapability: (request: GetReportingCapabilityRequest) =>
      coreRequest(() => reportingCapabilityClient.getReportingCapability(request)),
    getDiagnostics: async (): Promise<SystemDiagnostics> => {
      let response: Awaited<ReturnType<typeof systemClient.getDiagnostics>>;
      try {
        response = await systemClient.getDiagnostics({});
      } catch (error) {
        throw new ConnectError("Core request failed.", ConnectError.from(error).code);
      }

      if (
        response.apiVersion !== EXPECTED_API_VERSION ||
        !CORE_VERSION_PATTERN.test(response.coreVersion) ||
        response.runtimeMode !== RuntimeMode.OFFLINE ||
        response.networkRequired !== false
      ) {
        throw new CoreClientError("INVALID_DIAGNOSTICS");
      }

      return Object.freeze({
        apiVersion: response.apiVersion,
        coreVersion: response.coreVersion,
        runtimeMode: "offline",
        networkRequired: false,
      });
    },
  });
}
