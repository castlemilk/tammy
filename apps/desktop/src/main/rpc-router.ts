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
import {
  CreateAccountRequestSchema,
  CreateAccountResponseSchema,
  GetJournalRequestSchema,
  GetJournalResponseSchema,
  GetTrialBalanceRequestSchema,
  GetTrialBalanceResponseSchema,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
  PostManualJournalRequestSchema,
  PostManualJournalResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
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
import {
  CompleteBankReconciliationRequestSchema,
  CompleteBankReconciliationResponseSchema,
  GetBankingSummaryRequestSchema,
  GetBankingSummaryResponseSchema,
  ImportBankStatementRequestSchema,
  ImportBankStatementResponseSchema,
  ListBankStatementLinesRequestSchema,
  ListBankStatementLinesResponseSchema,
  MatchBankStatementLineRequestSchema,
  MatchBankStatementLineResponseSchema,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
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
import {
  GetDocumentRequestSchema,
  GetDocumentResponseSchema,
  IngestDocumentRequestSchema,
  IngestDocumentResponseSchema,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
  SaveDocumentReviewRequestSchema,
  SaveDocumentReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import type { SignInRequest, SignInResponse } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  SignInRequestSchema,
  SignInResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import type {
  CreateOrganisationRequest,
  CreateOrganisationResponse,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  CreateOrganisationRequestSchema,
  CreateOrganisationResponseSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import type {
  GetAttentionSummaryRequest,
  GetAttentionSummaryResponse,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import type {
  GetReportingCapabilityRequest,
  GetReportingCapabilityResponse,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import type {
  CreateBasDraftRequest,
  CreateBasDraftResponse,
  GetCurrentBasDraftRequest,
  GetCurrentBasDraftResponse,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import {
  CreateBasDraftRequestSchema,
  CreateBasDraftResponseSchema,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import type {
  ConfirmRecoveryRequest,
  ConfirmRecoveryResponse,
  CreateWorkspaceRequest,
  CreateWorkspaceResponse,
  UnlockWorkspaceRequest,
  UnlockWorkspaceResponse,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import {
  ConfirmRecoveryRequestSchema,
  ConfirmRecoveryResponseSchema,
  CreateWorkspaceRequestSchema,
  CreateWorkspaceResponseSchema,
  UnlockWorkspaceRequestSchema,
  UnlockWorkspaceResponseSchema,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";

import {
  ATTENTION_SUMMARY_CHANNEL,
  COMPLETE_BANK_RECONCILIATION_CHANNEL,
  CONFIRM_RECOVERY_CHANNEL,
  CREATE_ACCOUNT_CHANNEL,
  CREATE_BAS_DRAFT_CHANNEL,
  CREATE_ORGANISATION_CHANNEL,
  CREATE_WORKSPACE_CHANNEL,
  GET_BANKING_SUMMARY_CHANNEL,
  GET_CURRENT_BAS_DRAFT_CHANNEL,
  GET_DOCUMENT_CHANNEL,
  GET_JOURNAL_CHANNEL,
  GET_TRIAL_BALANCE_CHANNEL,
  IMPORT_BANK_STATEMENT_CHANNEL,
  INGEST_DOCUMENT_CHANNEL,
  LIST_ACCOUNTS_CHANNEL,
  LIST_BANK_STATEMENT_LINES_CHANNEL,
  LIST_DOCUMENTS_CHANNEL,
  LIST_JOURNALS_CHANNEL,
  MATCH_BANK_STATEMENT_LINE_CHANNEL,
  POST_MANUAL_JOURNAL_CHANNEL,
  REPORTING_CAPABILITY_CHANNEL,
  SAVE_DOCUMENT_REVIEW_CHANNEL,
  SIGN_IN_CHANNEL,
  UNLOCK_WORKSPACE_CHANNEL,
} from "../shared/desktop-api";
import { createProtoMethodCodec } from "../shared/proto-ipc";

export { ATTENTION_SUMMARY_CHANNEL } from "../shared/desktop-api";

const createWorkspaceCodec = createProtoMethodCodec({
  input: CreateWorkspaceRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 65_536,
  output: CreateWorkspaceResponseSchema,
});
const confirmRecoveryCodec = createProtoMethodCodec({
  input: ConfirmRecoveryRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: ConfirmRecoveryResponseSchema,
});
const signInCodec = createProtoMethodCodec({
  input: SignInRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: SignInResponseSchema,
});
const unlockWorkspaceCodec = createProtoMethodCodec({
  input: UnlockWorkspaceRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: UnlockWorkspaceResponseSchema,
});
const createOrganisationCodec = createProtoMethodCodec({
  input: CreateOrganisationRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: CreateOrganisationResponseSchema,
});
const listAccountsCodec = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 131_072,
  output: ListAccountsResponseSchema,
});
const createAccountCodec = createProtoMethodCodec({
  input: CreateAccountRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: CreateAccountResponseSchema,
});
const postManualJournalCodec = createProtoMethodCodec({
  input: PostManualJournalRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 262_144,
  output: PostManualJournalResponseSchema,
});
const listJournalsCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListJournalsResponseSchema,
});
const getJournalCodec = createProtoMethodCodec({
  input: GetJournalRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 262_144,
  output: GetJournalResponseSchema,
});
const getTrialBalanceCodec = createProtoMethodCodec({
  input: GetTrialBalanceRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 524_288,
  output: GetTrialBalanceResponseSchema,
});
const importBankStatementCodec = createProtoMethodCodec({
  input: ImportBankStatementRequestSchema,
  maximumRequestBytes: 262_144,
  maximumResponseBytes: 32_768,
  output: ImportBankStatementResponseSchema,
});
const listBankStatementLinesCodec = createProtoMethodCodec({
  input: ListBankStatementLinesRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListBankStatementLinesResponseSchema,
});
const matchBankStatementLineCodec = createProtoMethodCodec({
  input: MatchBankStatementLineRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: MatchBankStatementLineResponseSchema,
});
const completeBankReconciliationCodec = createProtoMethodCodec({
  input: CompleteBankReconciliationRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: CompleteBankReconciliationResponseSchema,
});
const getBankingSummaryCodec = createProtoMethodCodec({
  input: GetBankingSummaryRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: GetBankingSummaryResponseSchema,
});
const ingestDocumentCodec = createProtoMethodCodec({
  input: IngestDocumentRequestSchema,
  maximumRequestBytes: 11 * 1024 * 1024,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: IngestDocumentResponseSchema,
});
const listDocumentsCodec = createProtoMethodCodec({
  input: ListDocumentsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: ListDocumentsResponseSchema,
});
const getDocumentCodec = createProtoMethodCodec({
  input: GetDocumentRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: GetDocumentResponseSchema,
});
const saveDocumentReviewCodec = createProtoMethodCodec({
  input: SaveDocumentReviewRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: SaveDocumentReviewResponseSchema,
});
const createBasDraftCodec = createProtoMethodCodec({
  input: CreateBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: CreateBasDraftResponseSchema,
});
const getCurrentBasDraftCodec = createProtoMethodCodec({
  input: GetCurrentBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: GetCurrentBasDraftResponseSchema,
});

const attentionCodec = createProtoMethodCodec({
  input: GetAttentionSummaryRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 65_536,
  output: GetAttentionSummaryResponseSchema,
});
const reportingCapabilityCodec = createProtoMethodCodec({
  input: GetReportingCapabilityRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetReportingCapabilityResponseSchema,
});

export type DesktopRpcRouterErrorCode =
  | "CORE_REQUEST_FAILED"
  | "INVALID_RPC_REQUEST"
  | "UNKNOWN_RPC_CHANNEL";

export class DesktopRpcRouterError extends Error {
  public constructor(public readonly code: DesktopRpcRouterErrorCode) {
    super(code);
    this.name = "DesktopRpcRouterError";
  }
}

export interface DesktopRpcClient {
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
}

export interface DesktopRpcRouter {
  invoke(channel: string, request: Uint8Array): Promise<Uint8Array>;
}

interface DesktopRpcCodec<Request, Response> {
  decodeRequest(frame: Uint8Array): Request;
  encodeResponse(response: Response): Uint8Array;
}

async function invokeDesktopRpc<Request, Response>(
  codec: DesktopRpcCodec<Request, Response>,
  frame: Uint8Array,
  invokeCore: (request: Request) => Promise<Response>,
): Promise<Uint8Array> {
  let request: Request;
  try {
    request = codec.decodeRequest(frame);
  } catch {
    throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
  }

  try {
    return codec.encodeResponse(await invokeCore(request));
  } catch {
    throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
  }
}

export function createDesktopRpcRouter(client: DesktopRpcClient): Readonly<DesktopRpcRouter> {
  return Object.freeze({
    invoke: async (channel: string, request: Uint8Array): Promise<Uint8Array> => {
      switch (channel) {
        case CREATE_WORKSPACE_CHANNEL:
          return invokeDesktopRpc(createWorkspaceCodec, request, (decoded) =>
            client.createWorkspace(decoded),
          );
        case CONFIRM_RECOVERY_CHANNEL:
          return invokeDesktopRpc(confirmRecoveryCodec, request, (decoded) =>
            client.confirmRecovery(decoded),
          );
        case UNLOCK_WORKSPACE_CHANNEL:
          return invokeDesktopRpc(unlockWorkspaceCodec, request, (decoded) =>
            client.unlockWorkspace(decoded),
          );
        case SIGN_IN_CHANNEL:
          return invokeDesktopRpc(signInCodec, request, (decoded) => client.signIn(decoded));
        case CREATE_ORGANISATION_CHANNEL:
          return invokeDesktopRpc(createOrganisationCodec, request, (decoded) =>
            client.createOrganisation(decoded),
          );
        case LIST_ACCOUNTS_CHANNEL:
          return invokeDesktopRpc(listAccountsCodec, request, (decoded) =>
            client.listAccounts(decoded),
          );
        case CREATE_ACCOUNT_CHANNEL:
          return invokeDesktopRpc(createAccountCodec, request, (decoded) =>
            client.createAccount(decoded),
          );
        case POST_MANUAL_JOURNAL_CHANNEL:
          return invokeDesktopRpc(postManualJournalCodec, request, (decoded) =>
            client.postManualJournal(decoded),
          );
        case LIST_JOURNALS_CHANNEL:
          return invokeDesktopRpc(listJournalsCodec, request, (decoded) =>
            client.listJournals(decoded),
          );
        case GET_JOURNAL_CHANNEL:
          return invokeDesktopRpc(getJournalCodec, request, (decoded) =>
            client.getJournal(decoded),
          );
        case GET_TRIAL_BALANCE_CHANNEL:
          return invokeDesktopRpc(getTrialBalanceCodec, request, (decoded) =>
            client.getTrialBalance(decoded),
          );
        case IMPORT_BANK_STATEMENT_CHANNEL:
          return invokeDesktopRpc(importBankStatementCodec, request, (decoded) =>
            client.importBankStatement(decoded),
          );
        case LIST_BANK_STATEMENT_LINES_CHANNEL:
          return invokeDesktopRpc(listBankStatementLinesCodec, request, (decoded) =>
            client.listBankStatementLines(decoded),
          );
        case MATCH_BANK_STATEMENT_LINE_CHANNEL:
          return invokeDesktopRpc(matchBankStatementLineCodec, request, (decoded) =>
            client.matchBankStatementLine(decoded),
          );
        case COMPLETE_BANK_RECONCILIATION_CHANNEL:
          return invokeDesktopRpc(completeBankReconciliationCodec, request, (decoded) =>
            client.completeBankReconciliation(decoded),
          );
        case GET_BANKING_SUMMARY_CHANNEL:
          return invokeDesktopRpc(getBankingSummaryCodec, request, (decoded) =>
            client.getBankingSummary(decoded),
          );
        case INGEST_DOCUMENT_CHANNEL:
          return invokeDesktopRpc(ingestDocumentCodec, request, (decoded) =>
            client.ingestDocument(decoded),
          );
        case LIST_DOCUMENTS_CHANNEL:
          return invokeDesktopRpc(listDocumentsCodec, request, (decoded) =>
            client.listDocuments(decoded),
          );
        case GET_DOCUMENT_CHANNEL:
          return invokeDesktopRpc(getDocumentCodec, request, (decoded) =>
            client.getDocument(decoded),
          );
        case SAVE_DOCUMENT_REVIEW_CHANNEL:
          return invokeDesktopRpc(saveDocumentReviewCodec, request, (decoded) =>
            client.saveDocumentReview(decoded),
          );
        case CREATE_BAS_DRAFT_CHANNEL:
          return invokeDesktopRpc(createBasDraftCodec, request, (decoded) =>
            client.createBasDraft(decoded),
          );
        case GET_CURRENT_BAS_DRAFT_CHANNEL:
          return invokeDesktopRpc(getCurrentBasDraftCodec, request, (decoded) =>
            client.getCurrentBasDraft(decoded),
          );
        case ATTENTION_SUMMARY_CHANNEL:
          return invokeDesktopRpc(attentionCodec, request, (decoded) =>
            client.getAttentionSummary(decoded),
          );
        case REPORTING_CAPABILITY_CHANNEL:
          return invokeDesktopRpc(reportingCapabilityCodec, request, (decoded) =>
            client.getReportingCapability(decoded),
          );
        default:
          throw new DesktopRpcRouterError("UNKNOWN_RPC_CHANNEL");
      }
    },
  });
}
