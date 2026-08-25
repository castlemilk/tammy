import {
  create,
  type DescFile,
  type DescMessage,
  type DescService,
  fromBinary,
  toBinary,
} from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  AuthenticationContextSchema,
  CivilDateSchema,
  PageInfoSchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  CompanyReturnAttemptState,
  CompanyReturnSubmissionAttemptSchema,
  CompanyReturnSubmissionSchema,
  CompanyReturnSubmissionService,
  file_tammy_v1_company_return_submission,
  GetCompanyReturnSubmissionRequestSchema,
  GetCompanyReturnSubmissionResponseSchema,
  SubmissionEnvironment,
  SubmissionRetryClassification,
} from "@tammy/connect-client/tammy/v1/company_return_submission_pb.js";
import {
  CompanyReturnOperationType,
  CompanyReturnRelationshipKind,
  CompanyReturnSchema,
  CompanyReturnState,
  CompanyTaxService,
  file_tammy_v1_company_tax,
  ListCompanyReturnFactsRequestSchema,
  ListCompanyReturnFactsResponseSchema,
} from "@tammy/connect-client/tammy/v1/company_tax_pb.js";
import {
  FinancialCloseService,
  file_tammy_v1_financial_close,
  ListCloseChecksRequestSchema,
  ListCloseChecksResponseSchema,
} from "@tammy/connect-client/tammy/v1/financial_close_pb.js";
import { describe, expect, test } from "vitest";

const organisationId = "018f0c4a-7b9d-7abc-8def-0123456789ab";
const actorUserId = "018f0c4a-7b9d-7abc-8def-0123456789ac";
const sessionId = "018f0c4a-7b9d-7abc-8def-0123456789ad";
const closeId = "018f0c4a-7b9d-7abc-8def-0123456789ae";
const returnId = "018f0c4a-7b9d-7abc-8def-0123456789af";
const declarationId = "018f0c4a-7b9d-7abc-8def-0123456789b0";
const attemptId = "018f0c4a-7b9d-7abc-8def-0123456789b1";
const operationId = "018f0c4a-7b9d-7abc-8def-0123456789b2";
const idempotencyIdentity = "018f0c4a-7b9d-7abc-8def-0123456789b3";
const hash = new Uint8Array(32).fill(0x2a);
const instant = timestampFromDate(new Date("2026-07-01T00:00:00.000Z"));
const authentication = create(AuthenticationContextSchema, {
  actorUserId,
  sessionId,
});
const pageRequest = create(PageRequestSchema, { pageSize: 50 });
const emptyPage = create(PageInfoSchema, { returnedCount: 0 });

function ownMessages(file: DescFile): DescMessage[] {
  const messages: DescMessage[] = [];
  const visit = (message: DescMessage) => {
    messages.push(message);
    message.nestedMessages.forEach(visit);
  };
  file.messages.forEach(visit);
  return messages;
}

function expectExactMethods(service: DescService, expected: readonly string[]): string[] {
  const actual = service.methods.map((method) => method.name);
  expect(actual).toEqual(expected);
  expect(new Set(actual).size).toBe(expected.length);
  return actual.map((name) => `${service.typeName}.${name}`);
}

describe("company EOFY generated contracts", () => {
  test("binary round-trips a financial-close request and response", () => {
    const request = create(ListCloseChecksRequestSchema, {
      authentication,
      organisationId,
      closeId,
      page: pageRequest,
    });
    const response = create(ListCloseChecksResponseSchema, {
      checks: [],
      page: emptyPage,
    });

    expect(
      fromBinary(ListCloseChecksRequestSchema, toBinary(ListCloseChecksRequestSchema, request)),
    ).toEqual(request);
    expect(
      fromBinary(ListCloseChecksResponseSchema, toBinary(ListCloseChecksResponseSchema, response)),
    ).toEqual(response);
  });

  test("binary round-trips a company-tax request and response", () => {
    const request = create(ListCompanyReturnFactsRequestSchema, {
      authentication,
      organisationId,
      returnId,
      page: pageRequest,
    });
    const response = create(ListCompanyReturnFactsResponseSchema, {
      facts: [],
      page: emptyPage,
    });

    expect(
      fromBinary(
        ListCompanyReturnFactsRequestSchema,
        toBinary(ListCompanyReturnFactsRequestSchema, request),
      ),
    ).toEqual(request);
    expect(
      fromBinary(
        ListCompanyReturnFactsResponseSchema,
        toBinary(ListCompanyReturnFactsResponseSchema, response),
      ),
    ).toEqual(response);
  });

  test("binary round-trips a company-return submission request and response", () => {
    const request = create(GetCompanyReturnSubmissionRequestSchema, {
      authentication,
      organisationId,
      returnId,
    });
    const companyReturn = create(CompanyReturnSchema, {
      id: returnId,
      organisationId,
      incomeYear: 2026,
      periodStart: create(CivilDateSchema, { year: 2025, month: 7, day: 1 }),
      periodEnd: create(CivilDateSchema, { year: 2026, month: 6, day: 30 }),
      relationshipKind: CompanyReturnRelationshipKind.ORIGINAL,
      rootReturnId: returnId,
      preparationBundleId: "au-company-return-2026-preparation-v1",
      preparationBundleFingerprint: hash,
      sourceCloseId: closeId,
      sourceCloseHash: hash,
      taxReconciliationHash: hash,
      state: CompanyReturnState.DECLARED,
      version: 1n,
      validationRevision: 1n,
      declaredSnapshotHash: hash,
      currentDeclarationId: declarationId,
      createdAt: instant,
      updatedAt: instant,
    });
    const latestAttempt = create(CompanyReturnSubmissionAttemptSchema, {
      id: attemptId,
      returnId,
      declarationId,
      reportSnapshotHash: hash,
      officialPayloadHash: hash,
      environment: SubmissionEnvironment.SIMULATOR,
      productIdentifierFingerprint: hash,
      serviceId: "company-return-2026",
      operationType: CompanyReturnOperationType.PRELODGE,
      operationId,
      idempotencyIdentity,
      state: CompanyReturnAttemptState.ABORTED,
      retryClassification: SubmissionRetryClassification.NEVER,
      createdAt: instant,
      updatedAt: instant,
    });
    const response = create(GetCompanyReturnSubmissionResponseSchema, {
      companyReturn,
      submission: create(CompanyReturnSubmissionSchema, {
        returnId,
        latestAttempt,
        statusHistory: [],
      }),
    });

    expect(
      fromBinary(
        GetCompanyReturnSubmissionRequestSchema,
        toBinary(GetCompanyReturnSubmissionRequestSchema, request),
      ),
    ).toEqual(request);
    expect(
      fromBinary(
        GetCompanyReturnSubmissionResponseSchema,
        toBinary(GetCompanyReturnSubmissionResponseSchema, response),
      ),
    ).toEqual(response);
  });

  test("contains no dynamic JSON or map fields", () => {
    const forbiddenMessages = new Set([
      "google.protobuf.Any",
      "google.protobuf.Struct",
      "google.protobuf.Value",
    ]);
    const violations = [
      file_tammy_v1_financial_close,
      file_tammy_v1_company_tax,
      file_tammy_v1_company_return_submission,
    ].flatMap((file) =>
      ownMessages(file).flatMap((message) =>
        message.fields
          .filter(
            (field) =>
              field.fieldKind === "map" ||
              (field.fieldKind === "message" && forbiddenMessages.has(field.message.typeName)) ||
              (field.fieldKind === "list" &&
                field.listKind === "message" &&
                forbiddenMessages.has(field.message.typeName)),
          )
          .map((field) => `${message.typeName}.${field.name}`),
      ),
    );

    expect(violations).toEqual([]);
  });

  test("exposes each of the 30 fixed RPCs exactly once", () => {
    const methods = [
      ...expectExactMethods(FinancialCloseService, [
        "CreateFinancialClose",
        "GetFinancialClose",
        "ListCloseChecks",
        "ResolveCloseWarning",
        "FreezeFinancialClose",
        "ReopenFinancialClose",
        "StartFinancialCloseCorrection",
        "GetFinancialStatements",
      ]),
      ...expectExactMethods(CompanyTaxService, [
        "GetCompanyTaxProfile",
        "SetCompanyTaxProfile",
        "CreateCompanyReturn",
        "GetCompanyReturn",
        "ListCompanyReturnFacts",
        "SetCompanyReturnInput",
        "UpsertTaxAdjustment",
        "RemoveTaxAdjustment",
        "UpsertTaxElection",
        "RemoveTaxElection",
        "ValidateCompanyReturn",
        "AcknowledgeReturnWarning",
        "DeclareCompanyReturn",
        "WithdrawCompanyReturnDeclaration",
        "ExportCompanyReturnPack",
        "CreateCompanyReturnReplacement",
        "CreateCompanyReturnAmendment",
      ]),
      ...expectExactMethods(CompanyReturnSubmissionService, [
        "PreLodgeCompanyReturn",
        "LodgeCompanyReturn",
        "GetCompanyReturnSubmission",
        "RefreshCompanyReturnStatus",
        "ReconcileUnknownCompanyReturnSubmission",
      ]),
    ];

    expect(methods).toHaveLength(30);
    expect(new Set(methods).size).toBe(30);
  });
});
