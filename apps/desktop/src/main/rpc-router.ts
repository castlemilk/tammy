import { create } from "@bufbuild/protobuf";
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
import type {
  AssertTOTPRequest,
  AssertTOTPResponse,
  ConfirmTOTPRequest,
  ConfirmTOTPResponse,
  EnrolTOTPRequest,
  EnrolTOTPResponse,
  GetCurrentUserRequest,
  GetCurrentUserResponse,
  SignInRequest,
  SignInResponse,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  AssertTOTPRequestSchema,
  AssertTOTPResponseSchema,
  ConfirmTOTPRequestSchema,
  ConfirmTOTPResponseSchema,
  EnrolTOTPRequestSchema,
  EnrolTOTPResponseSchema,
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
  SignInRequestSchema,
  SignInResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import type {
  CreateOrganisationRequest,
  CreateOrganisationResponse,
  GetOrganisationRequest,
  GetOrganisationResponse,
  RecordEntityVerificationRequest,
  RecordEntityVerificationResponse,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  CreateOrganisationRequestSchema,
  CreateOrganisationResponseSchema,
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
  RecordEntityVerificationRequestSchema,
  RecordEntityVerificationResponseSchema,
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
  GetMachineCredentialStatusRequest,
  GetMachineCredentialStatusResponse,
  GetSbrReadinessRequest,
  GetSbrReadinessResponse,
  ImportMachineCredentialRequest,
  ImportMachineCredentialResponse,
  ImportSbrProductIdRequest,
  ImportSbrProductIdResponse,
  RemoveMachineCredentialRequest,
  RemoveMachineCredentialResponse,
  RemoveSbrProductIdRequest,
  RemoveSbrProductIdResponse,
  ReplaceMachineCredentialRequest,
  ReplaceMachineCredentialResponse,
  RunSbrReadinessFixtureRequest,
  RunSbrReadinessFixtureResponse,
  UnlockMachineCredentialRequest,
  UnlockMachineCredentialResponse,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import {
  GetMachineCredentialStatusRequestSchema,
  GetMachineCredentialStatusResponseSchema,
  GetSbrReadinessRequestSchema,
  GetSbrReadinessResponseSchema,
  ImportMachineCredentialRequestSchema,
  ImportMachineCredentialResponseSchema,
  ImportSbrProductIdRequestSchema,
  ImportSbrProductIdResponseSchema,
  RemoveMachineCredentialRequestSchema,
  RemoveMachineCredentialResponseSchema,
  RemoveSbrProductIdRequestSchema,
  RemoveSbrProductIdResponseSchema,
  ReplaceMachineCredentialRequestSchema,
  ReplaceMachineCredentialResponseSchema,
  RunSbrReadinessFixtureRequestSchema,
  RunSbrReadinessFixtureResponseSchema,
  UnlockMachineCredentialRequestSchema,
  UnlockMachineCredentialResponseSchema,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
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
import type {
  MachineCredentialFileSelection,
  MachineCredentialMutationInput,
  MachineCredentialUnlockInput,
  SbrProductIdImportInput,
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
  REPORTING_CAPABILITY_CHANNEL,
  RUN_SBR_READINESS_FIXTURE_CHANNEL,
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
const getCurrentUserCodec = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetCurrentUserResponseSchema,
});
const enrolTotpCodec = createProtoMethodCodec({
  input: EnrolTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: EnrolTOTPResponseSchema,
});
const confirmTotpCodec = createProtoMethodCodec({
  input: ConfirmTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: ConfirmTOTPResponseSchema,
});
const assertTotpCodec = createProtoMethodCodec({
  input: AssertTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: AssertTOTPResponseSchema,
});
const getOrganisationCodec = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetOrganisationResponseSchema,
});
const recordEntityVerificationCodec = createProtoMethodCodec({
  input: RecordEntityVerificationRequestSchema,
  maximumRequestBytes: Math.floor(1.1 * 1024 * 1024),
  maximumResponseBytes: 32_768,
  output: RecordEntityVerificationResponseSchema,
});
const getSbrReadinessCodec = createProtoMethodCodec({
  input: GetSbrReadinessRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetSbrReadinessResponseSchema,
});
const getMachineCredentialStatusCodec = createProtoMethodCodec({
  input: GetMachineCredentialStatusRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetMachineCredentialStatusResponseSchema,
});
const removeMachineCredentialCodec = createProtoMethodCodec({
  input: RemoveMachineCredentialRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: RemoveMachineCredentialResponseSchema,
});
const removeSbrProductIdCodec = createProtoMethodCodec({
  input: RemoveSbrProductIdRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: RemoveSbrProductIdResponseSchema,
});
const runSbrReadinessFixtureCodec = createProtoMethodCodec({
  input: RunSbrReadinessFixtureRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: RunSbrReadinessFixtureResponseSchema,
});
const importMachineCredentialCodec = createProtoMethodCodec({
  input: ImportMachineCredentialRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: ImportMachineCredentialResponseSchema,
});
const replaceMachineCredentialCodec = createProtoMethodCodec({
  input: ReplaceMachineCredentialRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: ReplaceMachineCredentialResponseSchema,
});
const unlockMachineCredentialCodec = createProtoMethodCodec({
  input: UnlockMachineCredentialRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: UnlockMachineCredentialResponseSchema,
});
const importSbrProductIdCodec = createProtoMethodCodec({
  input: ImportSbrProductIdRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: ImportSbrProductIdResponseSchema,
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
  readonly getCurrentUser: (request: GetCurrentUserRequest) => Promise<GetCurrentUserResponse>;
  readonly enrolTotp: (request: EnrolTOTPRequest) => Promise<EnrolTOTPResponse>;
  readonly confirmTotp: (request: ConfirmTOTPRequest) => Promise<ConfirmTOTPResponse>;
  readonly assertTotp: (request: AssertTOTPRequest) => Promise<AssertTOTPResponse>;
  readonly getOrganisation: (request: GetOrganisationRequest) => Promise<GetOrganisationResponse>;
  readonly recordEntityVerification: (
    request: RecordEntityVerificationRequest,
  ) => Promise<RecordEntityVerificationResponse>;
  readonly getSbrReadiness: (request: GetSbrReadinessRequest) => Promise<GetSbrReadinessResponse>;
  readonly importMachineCredential: (
    request: ImportMachineCredentialRequest,
  ) => Promise<ImportMachineCredentialResponse>;
  readonly getMachineCredentialStatus: (
    request: GetMachineCredentialStatusRequest,
  ) => Promise<GetMachineCredentialStatusResponse>;
  readonly unlockMachineCredential: (
    request: UnlockMachineCredentialRequest,
  ) => Promise<UnlockMachineCredentialResponse>;
  readonly replaceMachineCredential: (
    request: ReplaceMachineCredentialRequest,
  ) => Promise<ReplaceMachineCredentialResponse>;
  readonly removeMachineCredential: (
    request: RemoveMachineCredentialRequest,
  ) => Promise<RemoveMachineCredentialResponse>;
  readonly importSbrProductId: (
    request: ImportSbrProductIdRequest,
  ) => Promise<ImportSbrProductIdResponse>;
  readonly removeSbrProductId: (
    request: RemoveSbrProductIdRequest,
  ) => Promise<RemoveSbrProductIdResponse>;
  readonly runSbrReadinessFixture: (
    request: RunSbrReadinessFixtureRequest,
  ) => Promise<RunSbrReadinessFixtureResponse>;
}

export interface DesktopRpcRouter {
  invoke(channel: string, request: Uint8Array): Promise<Uint8Array>;
  selectMachineCredentialFile(): Promise<MachineCredentialFileSelection>;
  importMachineCredential(input: MachineCredentialMutationInput): Promise<Uint8Array>;
  replaceMachineCredential(input: MachineCredentialMutationInput): Promise<Uint8Array>;
  unlockMachineCredential(input: MachineCredentialUnlockInput): Promise<Uint8Array>;
  importSbrProductId(input: SbrProductIdImportInput): Promise<Uint8Array>;
}

export interface TrustedSbrMainBoundary {
  selectMachineCredentialFile(): Promise<MachineCredentialFileSelection>;
  consumeMachineCredentialFile(handle: string): Promise<
    Readonly<{
      selectedLocalPath: string;
      securityScopedBookmark?: Uint8Array;
    }>
  >;
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

const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const textEncoder = new TextEncoder();
const unavailableTrustedSbrBoundary: TrustedSbrMainBoundary = Object.freeze({
  selectMachineCredentialFile: async () => Object.freeze({ selected: false as const }),
  consumeMachineCredentialFile: async () => {
    throw new Error("SBR_FILE_INTAKE_UNAVAILABLE");
  },
});

function exactKeys(value: unknown, keys: readonly string[]): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const actual = Object.keys(value);
  return actual.length === keys.length && actual.every((key) => keys.includes(key));
}

function validateCredentialMutationInput(
  input: MachineCredentialMutationInput,
): Readonly<MachineCredentialMutationInput> {
  if (
    !exactKeys(input, ["command", "handle", "password"]) ||
    !(input.command instanceof Uint8Array) ||
    input.command.byteLength === 0 ||
    input.command.byteLength > 8_192 ||
    typeof input.handle !== "string" ||
    !UUID_V7.test(input.handle) ||
    !isBoundedUtf8String(input.password, 1_024)
  ) {
    throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
  }
  return Object.freeze({
    command: new Uint8Array(input.command),
    handle: input.handle,
    password: input.password,
  });
}

function validateUnlockInput(
  input: MachineCredentialUnlockInput,
): Readonly<MachineCredentialUnlockInput> {
  if (
    !exactKeys(input, ["command", "password"]) ||
    !(input.command instanceof Uint8Array) ||
    input.command.byteLength === 0 ||
    input.command.byteLength > 8_192 ||
    !isBoundedUtf8String(input.password, 1_024)
  ) {
    throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
  }
  return Object.freeze({ command: new Uint8Array(input.command), password: input.password });
}

function validateProductInput(input: SbrProductIdImportInput): Readonly<SbrProductIdImportInput> {
  if (
    !exactKeys(input, ["command", "productId"]) ||
    !(input.command instanceof Uint8Array) ||
    input.command.byteLength === 0 ||
    input.command.byteLength > 8_192 ||
    !isBoundedUtf8String(input.productId, 1_024)
  ) {
    throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
  }
  return Object.freeze({ command: new Uint8Array(input.command), productId: input.productId });
}

function validateSelection(
  selection: MachineCredentialFileSelection,
): MachineCredentialFileSelection {
  if (exactKeys(selection, ["selected"]) && selection.selected === false) {
    return Object.freeze({ selected: false });
  }
  if (
    exactKeys(selection, ["selected", "handle"]) &&
    selection.selected === true &&
    typeof selection.handle === "string" &&
    UUID_V7.test(selection.handle)
  ) {
    return Object.freeze({ selected: true, handle: selection.handle });
  }
  throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
}

function validateTrustedFile(
  value: Readonly<{
    selectedLocalPath: string;
    securityScopedBookmark?: Uint8Array;
  }>,
): Readonly<{ selectedLocalPath: string; securityScopedBookmark?: Uint8Array }> {
  if (
    typeof value.selectedLocalPath !== "string" ||
    !value.selectedLocalPath.startsWith("/") ||
    textEncoder.encode(value.selectedLocalPath).byteLength > 4_096 ||
    (value.securityScopedBookmark !== undefined &&
      (!(value.securityScopedBookmark instanceof Uint8Array) ||
        value.securityScopedBookmark.byteLength === 0 ||
        value.securityScopedBookmark.byteLength > 65_536))
  ) {
    throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
  }
  return {
    selectedLocalPath: value.selectedLocalPath,
    ...(value.securityScopedBookmark === undefined
      ? {}
      : { securityScopedBookmark: new Uint8Array(value.securityScopedBookmark) }),
  };
}

export function createDesktopRpcRouter(
  client: DesktopRpcClient,
  trustedSbr: TrustedSbrMainBoundary = unavailableTrustedSbrBoundary,
): Readonly<DesktopRpcRouter> {
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
        case GET_CURRENT_USER_CHANNEL:
          return invokeDesktopRpc(getCurrentUserCodec, request, (decoded) =>
            client.getCurrentUser(decoded),
          );
        case ENROL_TOTP_CHANNEL:
          return invokeDesktopRpc(enrolTotpCodec, request, (decoded) => client.enrolTotp(decoded));
        case CONFIRM_TOTP_CHANNEL:
          return invokeDesktopRpc(confirmTotpCodec, request, (decoded) =>
            client.confirmTotp(decoded),
          );
        case ASSERT_TOTP_CHANNEL:
          return invokeDesktopRpc(assertTotpCodec, request, (decoded) =>
            client.assertTotp(decoded),
          );
        case GET_ORGANISATION_CHANNEL:
          return invokeDesktopRpc(getOrganisationCodec, request, (decoded) =>
            client.getOrganisation(decoded),
          );
        case RECORD_ENTITY_VERIFICATION_CHANNEL:
          return invokeDesktopRpc(recordEntityVerificationCodec, request, (decoded) =>
            client.recordEntityVerification(decoded),
          );
        case GET_SBR_READINESS_CHANNEL:
          return invokeDesktopRpc(getSbrReadinessCodec, request, (decoded) =>
            client.getSbrReadiness(decoded),
          );
        case GET_MACHINE_CREDENTIAL_STATUS_CHANNEL:
          return invokeDesktopRpc(getMachineCredentialStatusCodec, request, (decoded) =>
            client.getMachineCredentialStatus(decoded),
          );
        case REMOVE_MACHINE_CREDENTIAL_CHANNEL:
          return invokeDesktopRpc(removeMachineCredentialCodec, request, (decoded) =>
            client.removeMachineCredential(decoded),
          );
        case REMOVE_SBR_PRODUCT_ID_CHANNEL:
          return invokeDesktopRpc(removeSbrProductIdCodec, request, (decoded) =>
            client.removeSbrProductId(decoded),
          );
        case RUN_SBR_READINESS_FIXTURE_CHANNEL:
          return invokeDesktopRpc(runSbrReadinessFixtureCodec, request, (decoded) =>
            client.runSbrReadinessFixture(decoded),
          );
        default:
          throw new DesktopRpcRouterError("UNKNOWN_RPC_CHANNEL");
      }
    },
    selectMachineCredentialFile: async (): Promise<MachineCredentialFileSelection> => {
      try {
        const selection = validateSelection(await trustedSbr.selectMachineCredentialFile());
        if (textEncoder.encode(JSON.stringify(selection)).byteLength > 128) {
          throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
        }
        return selection;
      } catch {
        throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
      }
    },
    importMachineCredential: async (
      rawInput: MachineCredentialMutationInput,
    ): Promise<Uint8Array> => {
      const input = validateCredentialMutationInput(rawInput);
      let decoded: ImportMachineCredentialRequest;
      try {
        decoded = importMachineCredentialCodec.decodeRequest(input.command);
      } catch {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      if (
        decoded.selectedLocalPath !== "" ||
        decoded.securityScopedBookmark !== undefined ||
        decoded.password.byteLength !== 0
      ) {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      try {
        const file = validateTrustedFile(
          await trustedSbr.consumeMachineCredentialFile(input.handle),
        );
        return importMachineCredentialCodec.encodeResponse(
          await client.importMachineCredential(
            create(ImportMachineCredentialRequestSchema, {
              commandContext: decoded.commandContext,
              selectedLocalPath: file.selectedLocalPath,
              securityScopedBookmark: file.securityScopedBookmark,
              password: textEncoder.encode(input.password),
            }),
          ),
        );
      } catch {
        throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
      }
    },
    replaceMachineCredential: async (
      rawInput: MachineCredentialMutationInput,
    ): Promise<Uint8Array> => {
      const input = validateCredentialMutationInput(rawInput);
      let decoded: ReplaceMachineCredentialRequest;
      try {
        decoded = replaceMachineCredentialCodec.decodeRequest(input.command);
      } catch {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      if (
        decoded.selectedLocalPath !== "" ||
        decoded.securityScopedBookmark !== undefined ||
        decoded.password.byteLength !== 0
      ) {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      try {
        const file = validateTrustedFile(
          await trustedSbr.consumeMachineCredentialFile(input.handle),
        );
        return replaceMachineCredentialCodec.encodeResponse(
          await client.replaceMachineCredential(
            create(ReplaceMachineCredentialRequestSchema, {
              commandContext: decoded.commandContext,
              selectedLocalPath: file.selectedLocalPath,
              securityScopedBookmark: file.securityScopedBookmark,
              password: textEncoder.encode(input.password),
            }),
          ),
        );
      } catch {
        throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
      }
    },
    unlockMachineCredential: async (rawInput: MachineCredentialUnlockInput) => {
      const input = validateUnlockInput(rawInput);
      let decoded: UnlockMachineCredentialRequest;
      try {
        decoded = unlockMachineCredentialCodec.decodeRequest(input.command);
      } catch {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      if (decoded.password.byteLength !== 0) {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      try {
        return unlockMachineCredentialCodec.encodeResponse(
          await client.unlockMachineCredential(
            create(UnlockMachineCredentialRequestSchema, {
              commandContext: decoded.commandContext,
              password: textEncoder.encode(input.password),
            }),
          ),
        );
      } catch {
        throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
      }
    },
    importSbrProductId: async (rawInput: SbrProductIdImportInput) => {
      const input = validateProductInput(rawInput);
      let decoded: ImportSbrProductIdRequest;
      try {
        decoded = importSbrProductIdCodec.decodeRequest(input.command);
      } catch {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      if (decoded.productIdValue !== "") {
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }
      try {
        return importSbrProductIdCodec.encodeResponse(
          await client.importSbrProductId(
            create(ImportSbrProductIdRequestSchema, {
              commandContext: decoded.commandContext,
              productIdValue: input.productId,
              evteProductIdentifier: decoded.evteProductIdentifier,
              evteServiceIdentifier: decoded.evteServiceIdentifier,
            }),
          ),
        );
      } catch {
        throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
      }
    },
  });
}
