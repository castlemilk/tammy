import {
  type CallOptions,
  type Client,
  ConnectError,
  createClient,
  type Interceptor,
  type Transport,
} from "@connectrpc/connect";
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
import { BasPreparationService } from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
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
import type {
  AssertTOTPRequest,
  AssertTOTPResponse,
  AssignRolesRequest,
  AssignRolesResponse,
  ConfirmTOTPRequest,
  ConfirmTOTPResponse,
  EnrolTOTPRequest,
  EnrolTOTPResponse,
  GetCurrentUserRequest,
  GetCurrentUserResponse,
  SignInRequest,
  SignInResponse,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { IdentityService } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  type GetBusinessIncomeSummaryRequest,
  type GetBusinessIncomeSummaryResponse,
  type GetInvoiceAccountingRequest,
  type GetInvoiceAccountingResponse,
  type GetInvoiceLogoRequest,
  type GetInvoiceLogoResponse,
  type GetInvoiceRequest,
  type GetInvoiceResponse,
  type GetInvoiceTemplateRequest,
  type GetInvoiceTemplateResponse,
  InvoiceService,
  type IssueInvoiceRequest,
  type IssueInvoiceResponse,
  type ListInvoicesRequest,
  type ListInvoicesResponse,
  type PostInvoiceIncomeRequest,
  type PostInvoiceIncomeResponse,
  type RecordInvoicePaymentRequest,
  type RecordInvoicePaymentResponse,
  type SaveInvoiceDraftRequest,
  type SaveInvoiceDraftResponse,
  type SaveInvoiceTemplateRequest,
  type SaveInvoiceTemplateResponse,
  type UploadInvoiceLogoRequest,
  type UploadInvoiceLogoResponse,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import type {
  CreateOrganisationRequest,
  CreateOrganisationResponse,
  GetOrganisationRequest,
  GetOrganisationResponse,
  RecordEntityVerificationRequest,
  RecordEntityVerificationResponse,
  UpdateOrganisationRequest,
  UpdateOrganisationResponse,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { OrganisationService } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import type {
  GetAttentionSummaryRequest,
  GetAttentionSummaryResponse,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import { OverviewService } from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  type AttributePurchaseGstRequest,
  type AttributePurchaseGstResponse,
  type DeferPurchaseGstClaimRequest,
  type DeferPurchaseGstClaimResponse,
  type GetPurchaseAccountingRequest,
  type GetPurchaseAccountingResponse,
  type ListPurchasePaymentsRequest,
  type ListPurchasePaymentsResponse,
  type PostReviewedPurchaseRequest,
  type PostReviewedPurchaseResponse,
  PurchaseService,
  type RecordPurchasePaymentRequest,
  type RecordPurchasePaymentResponse,
  type RecordPurchaseTaxEvidenceRequest,
  type RecordPurchaseTaxEvidenceResponse,
  type ResolveLegacyPurchaseReviewRequest,
  type ResolveLegacyPurchaseReviewResponse,
} from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import type {
  GetReportingCapabilityRequest,
  GetReportingCapabilityResponse,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import { ReportingCapabilityService } from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
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
import { SbrService } from "@tammy/connect-client/tammy/v1/sbr_pb.js";
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
import type { SystemDiagnostics } from "../shared/conveyor-contract";
import type { CoreReadiness } from "../shared/readiness";
import { purchaseCoreCorrection } from "./purchase-errors";
import { safeCoreRpcFailure } from "./startup-diagnostics";

export type { SystemDiagnostics } from "../shared/conveyor-contract";

const CAPABILITY_HEADER = "X-Tammy-Capability";
const EXPECTED_API_VERSION = "tammy.v1";
const CORE_VERSION_PATTERN = /^[\x20-\x7e]{1,128}$/;
const BAS_PROOF_READ_TIMEOUT_MS = 30_000;
const BAS_LONG_RUNNING_COMMAND_TIMEOUT_MS = 30_000;
const WORKSPACE_CREATE_TIMEOUT_MS = 30_000;

function withDefaultCallTimeout<Request, Response>(
  call: (request: Request, options?: CallOptions) => Promise<Response>,
  defaultTimeoutMs: number,
): (request: Request, options?: CallOptions) => Promise<Response> {
  return (request, options) =>
    call(request, {
      ...options,
      timeoutMs: options?.timeoutMs ?? defaultTimeoutMs,
    });
}

function withBasProofReadTimeout<Request, Response>(
  read: (request: Request, options?: CallOptions) => Promise<Response>,
): (request: Request, options?: CallOptions) => Promise<Response> {
  return withDefaultCallTimeout(read, BAS_PROOF_READ_TIMEOUT_MS);
}

function withBasLongRunningCommandTimeout<Request, Response>(
  command: (request: Request, options?: CallOptions) => Promise<Response>,
): (request: Request, options?: CallOptions) => Promise<Response> {
  return withDefaultCallTimeout(command, BAS_LONG_RUNNING_COMMAND_TIMEOUT_MS);
}

export interface CoreClient {
  /** The generated local BAS preparation service. Optional only for legacy test doubles. */
  readonly basPreparation?: Client<typeof BasPreparationService>;
  readonly getPurchaseAccounting: (
    request: GetPurchaseAccountingRequest,
  ) => Promise<GetPurchaseAccountingResponse>;
  readonly postReviewedPurchase: (
    request: PostReviewedPurchaseRequest,
  ) => Promise<PostReviewedPurchaseResponse>;
  readonly recordPurchasePayment: (
    request: RecordPurchasePaymentRequest,
  ) => Promise<RecordPurchasePaymentResponse>;
  readonly listPurchasePayments: (
    request: ListPurchasePaymentsRequest,
  ) => Promise<ListPurchasePaymentsResponse>;
  readonly recordPurchaseTaxEvidence: (
    request: RecordPurchaseTaxEvidenceRequest,
  ) => Promise<RecordPurchaseTaxEvidenceResponse>;
  readonly attributePurchaseGst: (
    request: AttributePurchaseGstRequest,
  ) => Promise<AttributePurchaseGstResponse>;
  readonly deferPurchaseGstClaim: (
    request: DeferPurchaseGstClaimRequest,
  ) => Promise<DeferPurchaseGstClaimResponse>;
  readonly resolveLegacyPurchaseReview: (
    request: ResolveLegacyPurchaseReviewRequest,
  ) => Promise<ResolveLegacyPurchaseReviewResponse>;

  readonly getInvoiceAccounting: (
    request: GetInvoiceAccountingRequest,
  ) => Promise<GetInvoiceAccountingResponse>;
  readonly postInvoiceIncome: (
    request: PostInvoiceIncomeRequest,
  ) => Promise<PostInvoiceIncomeResponse>;
  readonly recordInvoicePayment: (
    request: RecordInvoicePaymentRequest,
  ) => Promise<RecordInvoicePaymentResponse>;
  readonly getBusinessIncomeSummary: (
    request: GetBusinessIncomeSummaryRequest,
  ) => Promise<GetBusinessIncomeSummaryResponse>;

  readonly saveInvoiceDraft: (
    request: SaveInvoiceDraftRequest,
  ) => Promise<SaveInvoiceDraftResponse>;
  readonly getInvoice: (request: GetInvoiceRequest) => Promise<GetInvoiceResponse>;
  readonly listInvoices: (request: ListInvoicesRequest) => Promise<ListInvoicesResponse>;
  readonly issueInvoice: (request: IssueInvoiceRequest) => Promise<IssueInvoiceResponse>;
  readonly getInvoiceTemplate: (
    request: GetInvoiceTemplateRequest,
  ) => Promise<GetInvoiceTemplateResponse>;
  readonly saveInvoiceTemplate: (
    request: SaveInvoiceTemplateRequest,
  ) => Promise<SaveInvoiceTemplateResponse>;
  readonly uploadInvoiceLogo: (
    request: UploadInvoiceLogoRequest,
  ) => Promise<UploadInvoiceLogoResponse>;
  readonly getInvoiceLogo: (request: GetInvoiceLogoRequest) => Promise<GetInvoiceLogoResponse>;
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
  readonly getCurrentUser: (request: GetCurrentUserRequest) => Promise<GetCurrentUserResponse>;
  readonly assignRoles: (request: AssignRolesRequest) => Promise<AssignRolesResponse>;
  readonly enrolTotp: (request: EnrolTOTPRequest) => Promise<EnrolTOTPResponse>;
  readonly confirmTotp: (request: ConfirmTOTPRequest) => Promise<ConfirmTOTPResponse>;
  readonly assertTotp: (request: AssertTOTPRequest) => Promise<AssertTOTPResponse>;
  readonly getOrganisation: (request: GetOrganisationRequest) => Promise<GetOrganisationResponse>;
  readonly updateOrganisation: (
    request: UpdateOrganisationRequest,
  ) => Promise<UpdateOrganisationResponse>;
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

// Credential mutations can require both a prepare and commit helper launch.
// Each authenticated local launch owns a 30-second deadline, so the renderer
// boundary must allow both phases plus bounded core bookkeeping.
const SBR_LOCAL_OPERATION_TIMEOUT_MS = 65_000;

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
  const invoiceClient = createClient(InvoiceService, transport);
  const bankingClient = createClient(BankingService, transport);
  const documentClient = createClient(DocumentService, transport);
  const purchaseClient = createClient(PurchaseService, transport);
  const overviewClient = createClient(OverviewService, transport);
  const reportingCapabilityClient = createClient(ReportingCapabilityService, transport);
  const workspaceClient = createClient(WorkspaceService, transport);
  const identityClient = createClient(IdentityService, transport);
  const organisationClient = createClient(OrganisationService, transport);
  const taxClient = createClient(TaxService, transport);
  const sbrClient = createClient(SbrService, transport);
  const rawBasPreparationClient = createClient(BasPreparationService, transport);
  const basPreparationClient = Object.freeze({
    ...rawBasPreparationClient,
    getBasPreparation: withBasProofReadTimeout(rawBasPreparationClient.getBasPreparation),
    listBasPreparations: withBasProofReadTimeout(rawBasPreparationClient.listBasPreparations),
    getBasCalculation: withBasProofReadTimeout(rawBasPreparationClient.getBasCalculation),
    listBasCalculations: withBasProofReadTimeout(rawBasPreparationClient.listBasCalculations),
    getFrozenBas: withBasProofReadTimeout(rawBasPreparationClient.getFrozenBas),
    listFrozenBas: withBasProofReadTimeout(rawBasPreparationClient.listFrozenBas),
    getBasExternalObservation: withBasProofReadTimeout(
      rawBasPreparationClient.getBasExternalObservation,
    ),
    listBasExternalObservations: withBasProofReadTimeout(
      rawBasPreparationClient.listBasExternalObservations,
    ),
    getBasExternalDisputeResolution: withBasProofReadTimeout(
      rawBasPreparationClient.getBasExternalDisputeResolution,
    ),
    listBasExternalDisputeResolutions: withBasProofReadTimeout(
      rawBasPreparationClient.listBasExternalDisputeResolutions,
    ),
    listBasExportEvents: withBasProofReadTimeout(rawBasPreparationClient.listBasExportEvents),
    regenerateBasPreparation: withBasLongRunningCommandTimeout(
      rawBasPreparationClient.regenerateBasPreparation,
    ),
    createBasRevision: withBasLongRunningCommandTimeout(rawBasPreparationClient.createBasRevision),
  });

  const coreRequest = async <Response>(
    request: () => Promise<Response>,
    operation?: Parameters<typeof safeCoreRpcFailure>[0],
  ): Promise<Response> => {
    try {
      return await request();
    } catch (error) {
      console.error(safeCoreRpcFailure(operation, error));
      throw new ConnectError("Core request failed.", ConnectError.from(error).code);
    }
  };

  const purchaseRequest = async <Response>(request: () => Promise<Response>): Promise<Response> => {
    try {
      return await request();
    } catch (error) {
      throw (
        purchaseCoreCorrection(error) ??
        new ConnectError("Core request failed.", ConnectError.from(error).code)
      );
    }
  };

  return Object.freeze({
    basPreparation: basPreparationClient,
    getPurchaseAccounting: (request: GetPurchaseAccountingRequest) =>
      purchaseRequest(() => purchaseClient.getPurchaseAccounting(request)),
    postReviewedPurchase: (request: PostReviewedPurchaseRequest) =>
      purchaseRequest(() => purchaseClient.postReviewedPurchase(request)),
    recordPurchasePayment: (request: RecordPurchasePaymentRequest) =>
      purchaseRequest(() => purchaseClient.recordPurchasePayment(request)),
    listPurchasePayments: (request: ListPurchasePaymentsRequest) =>
      purchaseRequest(() => purchaseClient.listPurchasePayments(request)),
    recordPurchaseTaxEvidence: (request: RecordPurchaseTaxEvidenceRequest) =>
      purchaseRequest(() => purchaseClient.recordPurchaseTaxEvidence(request)),
    attributePurchaseGst: (request: AttributePurchaseGstRequest) =>
      purchaseRequest(() => purchaseClient.attributePurchaseGst(request)),
    deferPurchaseGstClaim: (request: DeferPurchaseGstClaimRequest) =>
      purchaseRequest(() => purchaseClient.deferPurchaseGstClaim(request)),
    resolveLegacyPurchaseReview: (request: ResolveLegacyPurchaseReviewRequest) =>
      purchaseRequest(() => purchaseClient.resolveLegacyPurchaseReview(request)),
    getInvoiceAccounting: (request: GetInvoiceAccountingRequest) =>
      coreRequest(() => invoiceClient.getInvoiceAccounting(request)),
    postInvoiceIncome: (request: PostInvoiceIncomeRequest) =>
      coreRequest(() => invoiceClient.postInvoiceIncome(request)),
    recordInvoicePayment: (request: RecordInvoicePaymentRequest) =>
      coreRequest(() => invoiceClient.recordInvoicePayment(request)),
    getBusinessIncomeSummary: (request: GetBusinessIncomeSummaryRequest) =>
      coreRequest(() => invoiceClient.getBusinessIncomeSummary(request)),
    saveInvoiceDraft: (request: SaveInvoiceDraftRequest) =>
      coreRequest(() => invoiceClient.saveInvoiceDraft(request)),
    getInvoice: (request: GetInvoiceRequest) =>
      coreRequest(() => invoiceClient.getInvoice(request)),
    listInvoices: (request: ListInvoicesRequest) =>
      coreRequest(() => invoiceClient.listInvoices(request)),
    issueInvoice: (request: IssueInvoiceRequest) =>
      coreRequest(() => invoiceClient.issueInvoice(request)),
    getInvoiceTemplate: (request: GetInvoiceTemplateRequest) =>
      coreRequest(() => invoiceClient.getInvoiceTemplate(request)),
    saveInvoiceTemplate: (request: SaveInvoiceTemplateRequest) =>
      coreRequest(() => invoiceClient.saveInvoiceTemplate(request)),
    uploadInvoiceLogo: (request: UploadInvoiceLogoRequest) =>
      coreRequest(() => invoiceClient.uploadInvoiceLogo(request)),
    getInvoiceLogo: (request: GetInvoiceLogoRequest) =>
      coreRequest(() => invoiceClient.getInvoiceLogo(request)),
    createAccount: (request: CreateAccountRequest) =>
      coreRequest(() => accountingClient.createAccount(request)),
    listAccounts: (request: ListAccountsRequest) =>
      coreRequest(() => accountingClient.listAccounts(request), "LIST_ACCOUNTS"),
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
      coreRequest(() => documentClient.ingestDocument(request), "INGEST_DOCUMENT"),
    listDocuments: (request: ListDocumentsRequest) =>
      coreRequest(() => documentClient.listDocuments(request), "LIST_DOCUMENTS"),
    getDocument: (request: GetDocumentRequest) =>
      coreRequest(() => documentClient.getDocument(request)),
    saveDocumentReview: (request: SaveDocumentReviewRequest) =>
      coreRequest(() => documentClient.saveDocumentReview(request)),
    createBasDraft: (request: CreateBasDraftRequest) =>
      coreRequest(() => taxClient.createBasDraft(request), "CREATE_BAS_DRAFT"),
    getCurrentBasDraft: (request: GetCurrentBasDraftRequest) =>
      coreRequest(() => taxClient.getCurrentBasDraft(request), "GET_CURRENT_BAS_DRAFT"),
    createWorkspace: (request: CreateWorkspaceRequest) =>
      coreRequest(() =>
        workspaceClient.createWorkspace(request, { timeoutMs: WORKSPACE_CREATE_TIMEOUT_MS }),
      ),
    confirmRecovery: (request: ConfirmRecoveryRequest) =>
      coreRequest(() => workspaceClient.confirmRecovery(request)),
    unlockWorkspace: (request: UnlockWorkspaceRequest) =>
      coreRequest(() => workspaceClient.unlockWorkspace(request), "UNLOCK_WORKSPACE"),
    signIn: (request: SignInRequest) =>
      coreRequest(() => identityClient.signIn(request), "SIGN_IN"),
    createOrganisation: (request: CreateOrganisationRequest) =>
      coreRequest(() => organisationClient.createOrganisation(request)),
    getCurrentUser: (request: GetCurrentUserRequest) =>
      coreRequest(() => identityClient.getCurrentUser(request)),
    assignRoles: (request: AssignRolesRequest) =>
      coreRequest(() => identityClient.assignRoles(request)),
    enrolTotp: (request: EnrolTOTPRequest) => coreRequest(() => identityClient.enrolTOTP(request)),
    confirmTotp: (request: ConfirmTOTPRequest) =>
      coreRequest(() => identityClient.confirmTOTP(request)),
    assertTotp: (request: AssertTOTPRequest) =>
      coreRequest(() => identityClient.assertTOTP(request), "ASSERT_TOTP"),
    getOrganisation: (request: GetOrganisationRequest) =>
      coreRequest(() => organisationClient.getOrganisation(request)),
    updateOrganisation: (request: UpdateOrganisationRequest) =>
      coreRequest(() => organisationClient.updateOrganisation(request)),
    recordEntityVerification: (request: RecordEntityVerificationRequest) =>
      coreRequest(() => organisationClient.recordEntityVerification(request)),
    getSbrReadiness: (request: GetSbrReadinessRequest) =>
      coreRequest(() =>
        sbrClient.getSbrReadiness(request, { timeoutMs: SBR_LOCAL_OPERATION_TIMEOUT_MS }),
      ),
    importMachineCredential: (request: ImportMachineCredentialRequest) =>
      coreRequest(() =>
        sbrClient.importMachineCredential(request, {
          timeoutMs: SBR_LOCAL_OPERATION_TIMEOUT_MS,
        }),
      ),
    getMachineCredentialStatus: (request: GetMachineCredentialStatusRequest) =>
      coreRequest(() =>
        sbrClient.getMachineCredentialStatus(request, {
          timeoutMs: SBR_LOCAL_OPERATION_TIMEOUT_MS,
        }),
      ),
    unlockMachineCredential: (request: UnlockMachineCredentialRequest) =>
      coreRequest(() =>
        sbrClient.unlockMachineCredential(request, {
          timeoutMs: SBR_LOCAL_OPERATION_TIMEOUT_MS,
        }),
      ),
    replaceMachineCredential: (request: ReplaceMachineCredentialRequest) =>
      coreRequest(() =>
        sbrClient.replaceMachineCredential(request, {
          timeoutMs: SBR_LOCAL_OPERATION_TIMEOUT_MS,
        }),
      ),
    removeMachineCredential: (request: RemoveMachineCredentialRequest) =>
      coreRequest(() =>
        sbrClient.removeMachineCredential(request, {
          timeoutMs: SBR_LOCAL_OPERATION_TIMEOUT_MS,
        }),
      ),
    importSbrProductId: (request: ImportSbrProductIdRequest) =>
      coreRequest(() =>
        sbrClient.importSbrProductId(request, { timeoutMs: SBR_LOCAL_OPERATION_TIMEOUT_MS }),
      ),
    removeSbrProductId: (request: RemoveSbrProductIdRequest) =>
      coreRequest(() =>
        sbrClient.removeSbrProductId(request, { timeoutMs: SBR_LOCAL_OPERATION_TIMEOUT_MS }),
      ),
    runSbrReadinessFixture: (request: RunSbrReadinessFixtureRequest) =>
      coreRequest(() =>
        sbrClient.runSbrReadinessFixture(request, {
          timeoutMs: SBR_LOCAL_OPERATION_TIMEOUT_MS,
        }),
      ),
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
