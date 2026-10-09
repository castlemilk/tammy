import { create, type DescMessage, type Message } from "@bufbuild/protobuf";
import * as accountingProto from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import * as bankingProto from "@tammy/connect-client/tammy/v1/banking_pb.js";
import * as documentsProto from "@tammy/connect-client/tammy/v1/documents_pb.js";
import * as identityProto from "@tammy/connect-client/tammy/v1/identity_pb.js";
import * as invoicesProto from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import * as organisationProto from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import * as overviewProto from "@tammy/connect-client/tammy/v1/overview_pb.js";
import * as purchasesProto from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import * as reportingCapabilityProto from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import * as taxProto from "@tammy/connect-client/tammy/v1/tax_pb.js";
import * as workspaceProto from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { createCaller } from "electron-conveyor/main";
import { expect, test, vi } from "vitest";

import type { ProtoMethodCodec } from "../../shared/proto-ipc";

interface CodecConfig {
  readonly input: DescMessage;
  readonly maximumRequestBytes: number;
  readonly maximumResponseBytes: number;
  readonly output: DescMessage;
}

interface CapturedCodec {
  readonly codec: ProtoMethodCodec<DescMessage, DescMessage>;
  readonly config: CodecConfig;
}

const codecCapture = vi.hoisted(() => ({ entries: [] as CapturedCodec[] }));

vi.mock("electron", () => ({ app: { isPackaged: true } }));
vi.mock("../../shared/proto-ipc", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../shared/proto-ipc")>();
  return {
    ...actual,
    createProtoMethodCodec: (config: CodecConfig) => {
      const codec = actual.createProtoMethodCodec(config);
      codecCapture.entries.push({
        codec: codec as unknown as ProtoMethodCodec<DescMessage, DescMessage>,
        config,
      });
      return codec;
    },
  };
});

import manifest from "../../shared/conveyor-manifest.json";
import { ProtoIpcError } from "../../shared/proto-ipc";
import { createTammyRouter } from "../router";

interface OrdinaryCase {
  readonly coreMethod: string;
  readonly input: DescMessage;
  readonly maximumRequestBytes: number;
  readonly maximumResponseBytes: number;
  readonly method: string;
  readonly module: string;
  readonly output: DescMessage;
}

const ORDINARY_CASES = [
  {
    module: "purchases",
    method: "getPurchaseAccounting",
    coreMethod: "getPurchaseAccounting",
    input: purchasesProto.GetPurchaseAccountingRequestSchema,
    output: purchasesProto.GetPurchaseAccountingResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 4 * 1024 * 1024,
  },
  {
    module: "purchases",
    method: "postReviewedPurchase",
    coreMethod: "postReviewedPurchase",
    input: purchasesProto.PostReviewedPurchaseRequestSchema,
    output: purchasesProto.PostReviewedPurchaseResponseSchema,
    maximumRequestBytes: 131_072,
    maximumResponseBytes: 4 * 1024 * 1024,
  },
  {
    module: "purchases",
    method: "recordPurchasePayment",
    coreMethod: "recordPurchasePayment",
    input: purchasesProto.RecordPurchasePaymentRequestSchema,
    output: purchasesProto.RecordPurchasePaymentResponseSchema,
    maximumRequestBytes: 131_072,
    maximumResponseBytes: 4 * 1024 * 1024,
  },
  {
    module: "purchases",
    method: "listPurchasePayments",
    coreMethod: "listPurchasePayments",
    input: purchasesProto.ListPurchasePaymentsRequestSchema,
    output: purchasesProto.ListPurchasePaymentsResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 4 * 1024 * 1024,
  },
  {
    module: "purchases",
    method: "recordPurchaseTaxEvidence",
    coreMethod: "recordPurchaseTaxEvidence",
    input: purchasesProto.RecordPurchaseTaxEvidenceRequestSchema,
    output: purchasesProto.RecordPurchaseTaxEvidenceResponseSchema,
    maximumRequestBytes: 131_072,
    maximumResponseBytes: 4 * 1024 * 1024,
  },
  {
    module: "purchases",
    method: "attributePurchaseGst",
    coreMethod: "attributePurchaseGst",
    input: purchasesProto.AttributePurchaseGstRequestSchema,
    output: purchasesProto.AttributePurchaseGstResponseSchema,
    maximumRequestBytes: 131_072,
    maximumResponseBytes: 4 * 1024 * 1024,
  },
  {
    module: "purchases",
    method: "deferPurchaseGstClaim",
    coreMethod: "deferPurchaseGstClaim",
    input: purchasesProto.DeferPurchaseGstClaimRequestSchema,
    output: purchasesProto.DeferPurchaseGstClaimResponseSchema,
    maximumRequestBytes: 131_072,
    maximumResponseBytes: 4 * 1024 * 1024,
  },
  {
    module: "purchases",
    method: "resolveLegacyPurchaseReview",
    coreMethod: "resolveLegacyPurchaseReview",
    input: purchasesProto.ResolveLegacyPurchaseReviewRequestSchema,
    output: purchasesProto.ResolveLegacyPurchaseReviewResponseSchema,
    maximumRequestBytes: 131_072,
    maximumResponseBytes: 4 * 1024 * 1024,
  },
  {
    module: "accounting",
    method: "getInvoiceAccounting",
    coreMethod: "getInvoiceAccounting",
    input: invoicesProto.GetInvoiceAccountingRequestSchema,
    output: invoicesProto.GetInvoiceAccountingResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 262_144,
  },
  {
    module: "accounting",
    method: "postInvoiceIncome",
    coreMethod: "postInvoiceIncome",
    input: invoicesProto.PostInvoiceIncomeRequestSchema,
    output: invoicesProto.PostInvoiceIncomeResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 262_144,
  },
  {
    module: "accounting",
    method: "recordInvoicePayment",
    coreMethod: "recordInvoicePayment",
    input: invoicesProto.RecordInvoicePaymentRequestSchema,
    output: invoicesProto.RecordInvoicePaymentResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 262_144,
  },
  {
    module: "accounting",
    method: "getBusinessIncomeSummary",
    coreMethod: "getBusinessIncomeSummary",
    input: invoicesProto.GetBusinessIncomeSummaryRequestSchema,
    output: invoicesProto.GetBusinessIncomeSummaryResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 16_384,
  },

  {
    module: "workspace",
    method: "createWorkspace",
    coreMethod: "createWorkspace",
    input: workspaceProto.CreateWorkspaceRequestSchema,
    output: workspaceProto.CreateWorkspaceResponseSchema,
    maximumRequestBytes: 32_768,
    maximumResponseBytes: 65_536,
  },
  {
    module: "workspace",
    method: "confirmRecovery",
    coreMethod: "confirmRecovery",
    input: workspaceProto.ConfirmRecoveryRequestSchema,
    output: workspaceProto.ConfirmRecoveryResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 16_384,
  },
  {
    module: "workspace",
    method: "unlockWorkspace",
    coreMethod: "unlockWorkspace",
    input: workspaceProto.UnlockWorkspaceRequestSchema,
    output: workspaceProto.UnlockWorkspaceResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 16_384,
  },
  {
    module: "workspace",
    method: "signIn",
    coreMethod: "signIn",
    input: identityProto.SignInRequestSchema,
    output: identityProto.SignInResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 32_768,
  },
  {
    module: "organisation",
    method: "getCurrentUser",
    coreMethod: "getCurrentUser",
    input: identityProto.GetCurrentUserRequestSchema,
    output: identityProto.GetCurrentUserResponseSchema,
    maximumRequestBytes: 8_192,
    maximumResponseBytes: 32_768,
  },
  {
    module: "organisation",
    method: "getOrganisation",
    coreMethod: "getOrganisation",
    input: organisationProto.GetOrganisationRequestSchema,
    output: organisationProto.GetOrganisationResponseSchema,
    maximumRequestBytes: 8_192,
    maximumResponseBytes: 32_768,
  },
  {
    module: "organisation",
    method: "createOrganisation",
    coreMethod: "createOrganisation",
    input: organisationProto.CreateOrganisationRequestSchema,
    output: organisationProto.CreateOrganisationResponseSchema,
    maximumRequestBytes: 32_768,
    maximumResponseBytes: 32_768,
  },
  {
    module: "organisation",
    method: "enrolTotp",
    coreMethod: "enrolTotp",
    input: identityProto.EnrolTOTPRequestSchema,
    output: identityProto.EnrolTOTPResponseSchema,
    maximumRequestBytes: 8_192,
    maximumResponseBytes: 32_768,
  },
  {
    module: "organisation",
    method: "confirmTotp",
    coreMethod: "confirmTotp",
    input: identityProto.ConfirmTOTPRequestSchema,
    output: identityProto.ConfirmTOTPResponseSchema,
    maximumRequestBytes: 8_192,
    maximumResponseBytes: 32_768,
  },
  {
    module: "organisation",
    method: "assertTotp",
    coreMethod: "assertTotp",
    input: identityProto.AssertTOTPRequestSchema,
    output: identityProto.AssertTOTPResponseSchema,
    maximumRequestBytes: 8_192,
    maximumResponseBytes: 32_768,
  },
  {
    module: "organisation",
    method: "recordEntityVerification",
    coreMethod: "recordEntityVerification",
    input: organisationProto.RecordEntityVerificationRequestSchema,
    output: organisationProto.RecordEntityVerificationResponseSchema,
    maximumRequestBytes: Math.floor(1.1 * 1024 * 1024),
    maximumResponseBytes: 32_768,
  },
  {
    module: "accounting",
    method: "listAccounts",
    coreMethod: "listAccounts",
    input: accountingProto.ListAccountsRequestSchema,
    output: accountingProto.ListAccountsResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 131_072,
  },
  {
    module: "accounting",
    method: "listJournals",
    coreMethod: "listJournals",
    input: accountingProto.ListJournalsRequestSchema,
    output: accountingProto.ListJournalsResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 262_144,
  },
  {
    module: "accounting",
    method: "getJournal",
    coreMethod: "getJournal",
    input: accountingProto.GetJournalRequestSchema,
    output: accountingProto.GetJournalResponseSchema,
    maximumRequestBytes: 8_192,
    maximumResponseBytes: 262_144,
  },
  {
    module: "accounting",
    method: "getTrialBalance",
    coreMethod: "getTrialBalance",
    input: accountingProto.GetTrialBalanceRequestSchema,
    output: accountingProto.GetTrialBalanceResponseSchema,
    maximumRequestBytes: 8_192,
    maximumResponseBytes: 524_288,
  },
  {
    module: "accounting",
    method: "createAccount",
    coreMethod: "createAccount",
    input: accountingProto.CreateAccountRequestSchema,
    output: accountingProto.CreateAccountResponseSchema,
    maximumRequestBytes: 32_768,
    maximumResponseBytes: 32_768,
  },
  {
    module: "accounting",
    method: "postManualJournal",
    coreMethod: "postManualJournal",
    input: accountingProto.PostManualJournalRequestSchema,
    output: accountingProto.PostManualJournalResponseSchema,
    maximumRequestBytes: 131_072,
    maximumResponseBytes: 262_144,
  },
  {
    module: "banking",
    method: "listBankStatementLines",
    coreMethod: "listBankStatementLines",
    input: bankingProto.ListBankStatementLinesRequestSchema,
    output: bankingProto.ListBankStatementLinesResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 262_144,
  },
  {
    module: "banking",
    method: "getBankingSummary",
    coreMethod: "getBankingSummary",
    input: bankingProto.GetBankingSummaryRequestSchema,
    output: bankingProto.GetBankingSummaryResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 16_384,
  },
  {
    module: "banking",
    method: "importBankStatement",
    coreMethod: "importBankStatement",
    input: bankingProto.ImportBankStatementRequestSchema,
    output: bankingProto.ImportBankStatementResponseSchema,
    maximumRequestBytes: 262_144,
    maximumResponseBytes: 32_768,
  },
  {
    module: "banking",
    method: "matchBankStatementLine",
    coreMethod: "matchBankStatementLine",
    input: bankingProto.MatchBankStatementLineRequestSchema,
    output: bankingProto.MatchBankStatementLineResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 32_768,
  },
  {
    module: "banking",
    method: "completeBankReconciliation",
    coreMethod: "completeBankReconciliation",
    input: bankingProto.CompleteBankReconciliationRequestSchema,
    output: bankingProto.CompleteBankReconciliationResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 16_384,
  },
  {
    module: "documents",
    method: "listDocuments",
    coreMethod: "listDocuments",
    input: documentsProto.ListDocumentsRequestSchema,
    output: documentsProto.ListDocumentsResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 4 * 1024 * 1024,
  },
  {
    module: "documents",
    method: "getDocument",
    coreMethod: "getDocument",
    input: documentsProto.GetDocumentRequestSchema,
    output: documentsProto.GetDocumentResponseSchema,
    maximumRequestBytes: 8_192,
    maximumResponseBytes: 2 * 1024 * 1024,
  },
  {
    module: "documents",
    method: "ingestDocument",
    coreMethod: "ingestDocument",
    input: documentsProto.IngestDocumentRequestSchema,
    output: documentsProto.IngestDocumentResponseSchema,
    maximumRequestBytes: 11 * 1024 * 1024,
    maximumResponseBytes: 2 * 1024 * 1024,
  },
  {
    module: "documents",
    method: "saveDocumentReview",
    coreMethod: "saveDocumentReview",
    input: documentsProto.SaveDocumentReviewRequestSchema,
    output: documentsProto.SaveDocumentReviewResponseSchema,
    maximumRequestBytes: 32_768,
    maximumResponseBytes: 2 * 1024 * 1024,
  },
  {
    module: "reporting",
    method: "getCurrentBasDraft",
    coreMethod: "getCurrentBasDraft",
    input: taxProto.GetCurrentBasDraftRequestSchema,
    output: taxProto.GetCurrentBasDraftResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 4_194_304,
  },
  {
    module: "reporting",
    method: "getReportingCapability",
    coreMethod: "getReportingCapability",
    input: reportingCapabilityProto.GetReportingCapabilityRequestSchema,
    output: reportingCapabilityProto.GetReportingCapabilityResponseSchema,
    maximumRequestBytes: 8_192,
    maximumResponseBytes: 32_768,
  },
  {
    module: "reporting",
    method: "getAttentionSummary",
    coreMethod: "getAttentionSummary",
    input: overviewProto.GetAttentionSummaryRequestSchema,
    output: overviewProto.GetAttentionSummaryResponseSchema,
    maximumRequestBytes: 8_192,
    maximumResponseBytes: 65_536,
  },
  {
    module: "reporting",
    method: "createBasDraft",
    coreMethod: "createBasDraft",
    input: taxProto.CreateBasDraftRequestSchema,
    output: taxProto.CreateBasDraftResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 4_194_304,
  },
] as const satisfies readonly OrdinaryCase[];

type Procedure = (...input: unknown[]) => Promise<unknown>;

function procedure(caller: unknown, module: string, method: string): Procedure {
  const result = (caller as Record<string, Record<string, Procedure>>)[module]?.[method];
  if (result === undefined) throw new Error(`Missing procedure: ${module}.${method}`);
  return result;
}

function capturedCodec(testCase: OrdinaryCase): CapturedCodec {
  const matches = codecCapture.entries.filter(({ config }) => config.input === testCase.input);
  expect(matches, `${testCase.module}.${testCase.method} codec capture`).toHaveLength(1);
  const match = matches[0];
  if (match === undefined) throw new Error(`Missing codec: ${testCase.module}.${testCase.method}`);
  return match;
}

function expectProtoError(action: () => unknown, code: string): void {
  let caught: unknown;
  try {
    action();
  } catch (error) {
    caught = error;
  }
  expect(caught).toBeInstanceOf(ProtoIpcError);
  expect(caught).toMatchObject({ code, message: code });
}

test("ordinary production codecs exactly match the complete module-to-Core contract", () => {
  for (const testCase of ORDINARY_CASES) {
    const captured = capturedCodec(testCase);
    expect(captured.config.input).toBe(testCase.input);
    expect(captured.config.output).toBe(testCase.output);
    expect(captured.config.maximumRequestBytes).toBe(testCase.maximumRequestBytes);
    expect(captured.config.maximumResponseBytes).toBe(testCase.maximumResponseBytes);
  }

  const ordinaryInputNames: ReadonlySet<string> = new Set(
    ORDINARY_CASES.map(({ input }) => input.typeName),
  );
  expect(
    codecCapture.entries.filter(({ config }) => ordinaryInputNames.has(config.input.typeName)),
  ).toHaveLength(ORDINARY_CASES.length);
});

test("every ordinary framed procedure round-trips its intended request and response exactly once", async () => {
  const calls: Array<{ readonly method: string; readonly request?: Message }> = [];
  const responses: ReadonlyMap<string, Message> = new Map(
    ORDINARY_CASES.map(({ coreMethod, output }) => [coreMethod, create(output)]),
  );
  const diagnostics = Object.freeze({
    apiVersion: "tammy.v1",
    coreVersion: "test-core",
    runtimeMode: "offline" as const,
    networkRequired: false as const,
  });
  const client = new Proxy(
    {},
    {
      get: (_target, property) => {
        const method = String(property);
        if (method === "getDiagnostics")
          return async () => {
            calls.push({ method });
            return diagnostics;
          };
        return async (request: Message) => {
          calls.push({ method, request });
          const response = responses.get(method);
          if (response === undefined) throw new Error("unexpected Core method");
          return response;
        };
      },
    },
  );
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: client as never, fileIntake: {} as never },
  });

  await expect(caller.system.getSystemDiagnostics()).resolves.toEqual(diagnostics);
  const expectedRequests: Message[] = [];
  for (const testCase of ORDINARY_CASES) {
    const { codec } = capturedCodec(testCase);
    const request = create(testCase.input);
    expectedRequests.push(request);
    const responseFrame = await procedure(
      caller,
      testCase.module,
      testCase.method,
    )(codec.encodeRequest(request));

    expect(responseFrame).toBeInstanceOf(Uint8Array);
    expect(codec.decodeResponse(responseFrame as Uint8Array)).toEqual(create(testCase.output));
  }

  expect(calls.map(({ method }) => method)).toEqual([
    "getDiagnostics",
    ...ORDINARY_CASES.map(({ coreMethod }) => coreMethod),
  ]);
  expect(calls).toHaveLength(ORDINARY_CASES.length + 1);
  for (const [index, request] of expectedRequests.entries()) {
    const call = calls[index + 1];
    const testCase = ORDINARY_CASES[index];
    if (call === undefined || testCase === undefined) throw new Error("Missing ordinary call");
    expect(call.request).toEqual(request);
    expect(call.request?.$typeName).toBe(testCase.input.typeName);
  }
  expect(createTammyRouter().manifest).toEqual(manifest);
});

test("every ordinary request and response enforces its exact production byte boundary", async () => {
  const calls: string[] = [];
  const client = new Proxy(
    {},
    {
      get: (_target, property) => async () => {
        calls.push(String(property));
        throw new Error("invalid frames must not reach Core");
      },
    },
  );
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: client as never, fileIntake: {} as never },
  });

  for (const testCase of ORDINARY_CASES) {
    const { codec } = capturedCodec(testCase);
    const invoke = procedure(caller, testCase.module, testCase.method);

    await expect(invoke(new Uint8Array(testCase.maximumRequestBytes))).rejects.toMatchObject({
      code: "INVALID_RPC_REQUEST",
      message: "INVALID_RPC_REQUEST",
    });
    await expect(invoke(new Uint8Array(testCase.maximumRequestBytes + 1))).rejects.toMatchObject({
      code: "INVALID_INPUT",
    });
    expectProtoError(
      () => codec.decodeResponse(new Uint8Array(testCase.maximumResponseBytes)),
      "PROTO_FRAME_INVALID",
    );
    expectProtoError(
      () => codec.decodeResponse(new Uint8Array(testCase.maximumResponseBytes + 1)),
      "PROTO_FRAME_TOO_LARGE",
    );
  }

  expect(calls).toEqual([]);
});

test("system diagnostics is the sole zero-input Core call and sanitizes every failure", async () => {
  const diagnostics = Object.freeze({
    apiVersion: "tammy.v1",
    coreVersion: "test-core",
    runtimeMode: "offline" as const,
    networkRequired: false as const,
  });
  const getDiagnostics = vi.fn(async () => diagnostics);
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: { getDiagnostics } as never, fileIntake: {} as never },
  });

  await expect(caller.system.getSystemDiagnostics()).resolves.toEqual(diagnostics);
  await expect(
    procedure(caller, "system", "getSystemDiagnostics")("unexpected"),
  ).rejects.toMatchObject({ code: "INVALID_INPUT" });
  expect(getDiagnostics).toHaveBeenCalledExactlyOnceWith();

  for (const failingCall of [
    () => {
      throw new Error("/private/diagnostics password=secret");
    },
    async () => {
      throw new Error("/private/diagnostics password=secret");
    },
  ]) {
    const failingCaller = createCaller(createTammyRouter(), {
      ctx: { client: { getDiagnostics: failingCall } as never, fileIntake: {} as never },
    });
    const error = await failingCaller.system
      .getSystemDiagnostics()
      .catch((caught: unknown) => caught);
    expect(error).toMatchObject({ code: "CORE_REQUEST_FAILED", message: "CORE_REQUEST_FAILED" });
    expect(JSON.stringify(error)).not.toMatch(/private|password|secret/i);
  }
});
