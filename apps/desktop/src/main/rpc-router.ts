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
  GetAttentionSummaryRequest,
  GetAttentionSummaryResponse,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import type { SignInRequest, SignInResponse } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { SignInRequestSchema, SignInResponseSchema } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import type {
  CreateOrganisationRequest,
  CreateOrganisationResponse,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  CreateOrganisationRequestSchema,
  CreateOrganisationResponseSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
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
  CONFIRM_RECOVERY_CHANNEL,
  CREATE_ACCOUNT_CHANNEL,
  CREATE_ORGANISATION_CHANNEL,
  CREATE_WORKSPACE_CHANNEL,
  GET_JOURNAL_CHANNEL,
  GET_TRIAL_BALANCE_CHANNEL,
  GET_DOCUMENT_CHANNEL,
  INGEST_DOCUMENT_CHANNEL,
  LIST_ACCOUNTS_CHANNEL,
  LIST_DOCUMENTS_CHANNEL,
  LIST_JOURNALS_CHANNEL,
  POST_MANUAL_JOURNAL_CHANNEL,
  SIGN_IN_CHANNEL,
  SAVE_DOCUMENT_REVIEW_CHANNEL,
  UNLOCK_WORKSPACE_CHANNEL,
} from "../shared/desktop-api";
import { createProtoMethodCodec, ProtoIpcError } from "../shared/proto-ipc";

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

const attentionCodec = createProtoMethodCodec({
  input: GetAttentionSummaryRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 65_536,
  output: GetAttentionSummaryResponseSchema,
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
  readonly getTrialBalance: (
    request: GetTrialBalanceRequest,
  ) => Promise<GetTrialBalanceResponse>;
  readonly ingestDocument: (request: IngestDocumentRequest) => Promise<IngestDocumentResponse>;
  readonly listDocuments: (request: ListDocumentsRequest) => Promise<ListDocumentsResponse>;
  readonly getDocument: (request: GetDocumentRequest) => Promise<GetDocumentResponse>;
  readonly saveDocumentReview: (
    request: SaveDocumentReviewRequest,
  ) => Promise<SaveDocumentReviewResponse>;
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
}

export interface DesktopRpcRouter {
  invoke(channel: string, request: Uint8Array): Promise<Uint8Array>;
}

export function createDesktopRpcRouter(client: DesktopRpcClient): Readonly<DesktopRpcRouter> {
  return Object.freeze({
    invoke: async (channel: string, request: Uint8Array): Promise<Uint8Array> => {
      try {
        switch (channel) {
          case CREATE_WORKSPACE_CHANNEL:
            return createWorkspaceCodec.encodeResponse(
              await client.createWorkspace(createWorkspaceCodec.decodeRequest(request)),
            );
          case CONFIRM_RECOVERY_CHANNEL:
            return confirmRecoveryCodec.encodeResponse(
              await client.confirmRecovery(confirmRecoveryCodec.decodeRequest(request)),
            );
          case UNLOCK_WORKSPACE_CHANNEL:
            return unlockWorkspaceCodec.encodeResponse(
              await client.unlockWorkspace(unlockWorkspaceCodec.decodeRequest(request)),
            );
          case SIGN_IN_CHANNEL:
            return signInCodec.encodeResponse(await client.signIn(signInCodec.decodeRequest(request)));
          case CREATE_ORGANISATION_CHANNEL:
            return createOrganisationCodec.encodeResponse(
              await client.createOrganisation(createOrganisationCodec.decodeRequest(request)),
            );
          case LIST_ACCOUNTS_CHANNEL:
            return listAccountsCodec.encodeResponse(
              await client.listAccounts(listAccountsCodec.decodeRequest(request)),
            );
          case CREATE_ACCOUNT_CHANNEL:
            return createAccountCodec.encodeResponse(
              await client.createAccount(createAccountCodec.decodeRequest(request)),
            );
          case POST_MANUAL_JOURNAL_CHANNEL:
            return postManualJournalCodec.encodeResponse(
              await client.postManualJournal(postManualJournalCodec.decodeRequest(request)),
            );
          case LIST_JOURNALS_CHANNEL:
            return listJournalsCodec.encodeResponse(
              await client.listJournals(listJournalsCodec.decodeRequest(request)),
            );
          case GET_JOURNAL_CHANNEL:
            return getJournalCodec.encodeResponse(
              await client.getJournal(getJournalCodec.decodeRequest(request)),
            );
          case GET_TRIAL_BALANCE_CHANNEL:
            return getTrialBalanceCodec.encodeResponse(
              await client.getTrialBalance(getTrialBalanceCodec.decodeRequest(request)),
            );
          case INGEST_DOCUMENT_CHANNEL:
            return ingestDocumentCodec.encodeResponse(
              await client.ingestDocument(ingestDocumentCodec.decodeRequest(request)),
            );
          case LIST_DOCUMENTS_CHANNEL:
            return listDocumentsCodec.encodeResponse(
              await client.listDocuments(listDocumentsCodec.decodeRequest(request)),
            );
          case GET_DOCUMENT_CHANNEL:
            return getDocumentCodec.encodeResponse(
              await client.getDocument(getDocumentCodec.decodeRequest(request)),
            );
          case SAVE_DOCUMENT_REVIEW_CHANNEL:
            return saveDocumentReviewCodec.encodeResponse(
              await client.saveDocumentReview(saveDocumentReviewCodec.decodeRequest(request)),
            );
          case ATTENTION_SUMMARY_CHANNEL:
            return attentionCodec.encodeResponse(
              await client.getAttentionSummary(attentionCodec.decodeRequest(request)),
            );
          default:
            throw new DesktopRpcRouterError("UNKNOWN_RPC_CHANNEL");
        }
      } catch (error) {
        if (error instanceof DesktopRpcRouterError) throw error;
        if (error instanceof ProtoIpcError) throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
        throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
      }
    },
  });
}
