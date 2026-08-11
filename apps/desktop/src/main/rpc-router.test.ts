import { create } from "@bufbuild/protobuf";
import {
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  CreateOrganisationRequestSchema,
  CreateOrganisationResponseSchema,
  OrganisationSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
  ReportingCapabilitySchema,
  ReportingCapabilityStatus,
  ReportingEntityType,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import { GetDiagnosticsRequestSchema } from "@tammy/connect-client/tammy/v1/system_pb.js";
import { describe, expect, it, vi } from "vitest";
import {
  CREATE_ORGANISATION_CHANNEL,
  LIST_ACCOUNTS_CHANNEL,
  REPORTING_CAPABILITY_CHANNEL,
} from "../shared/desktop-api";
import { createProtoMethodCodec } from "../shared/proto-ipc";
import {
  ATTENTION_SUMMARY_CHANNEL,
  createDesktopRpcRouter,
  type DesktopRpcClient,
  DesktopRpcRouterError,
} from "./rpc-router";

const attentionCodec = createProtoMethodCodec({
  input: GetAttentionSummaryRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 65_536,
  output: GetAttentionSummaryResponseSchema,
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
const reportingCapabilityCodec = createProtoMethodCodec({
  input: GetReportingCapabilityRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetReportingCapabilityResponseSchema,
});

function rpcClient(getAttentionSummary: DesktopRpcClient["getAttentionSummary"]): DesktopRpcClient {
  return {
    createWorkspace: vi.fn(),
    confirmRecovery: vi.fn(),
    unlockWorkspace: vi.fn(),
    signIn: vi.fn(),
    createOrganisation: vi.fn(),
    createAccount: vi.fn(),
    listAccounts: vi.fn(),
    postManualJournal: vi.fn(),
    listJournals: vi.fn(),
    getJournal: vi.fn(),
    getTrialBalance: vi.fn(),
    importBankStatement: vi.fn(),
    listBankStatementLines: vi.fn(),
    matchBankStatementLine: vi.fn(),
    completeBankReconciliation: vi.fn(),
    getBankingSummary: vi.fn(),
    ingestDocument: vi.fn(),
    listDocuments: vi.fn(),
    getDocument: vi.fn(),
    saveDocumentReview: vi.fn(),
    createBasDraft: vi.fn(),
    getCurrentBasDraft: vi.fn(),
    getReportingCapability: vi.fn(),
    getAttentionSummary,
  };
}

describe("named desktop protobuf RPC router", () => {
  it("decodes, invokes, and returns only the generated Overview messages", async () => {
    const response = create(GetAttentionSummaryResponseSchema, {
      documentsNeedingReview: 3,
      documentsReviewedInPeriod: 12,
    });
    const getAttentionSummary = vi.fn(async () => response);
    const router = createDesktopRpcRouter(rpcClient(getAttentionSummary));
    const request = create(GetAttentionSummaryRequestSchema, {
      organisationId: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb",
    });

    const encoded = await router.invoke(
      ATTENTION_SUMMARY_CHANNEL,
      attentionCodec.encodeRequest(request),
    );

    expect(getAttentionSummary).toHaveBeenCalledExactlyOnceWith(request);
    expect(attentionCodec.decodeResponse(encoded)).toEqual(response);
  });

  it("routes the exact bounded reporting capability request and response", async () => {
    const response = create(GetReportingCapabilityResponseSchema, {
      capability: create(ReportingCapabilitySchema, {
        report: ReportKind.GST_WORKPAPER,
        taxYear: 2024,
        entityType: ReportingEntityType.AU_BUSINESS,
        status: ReportingCapabilityStatus.AVAILABLE,
        appVersion: "test-core",
        summary: "Tammy supports a local reviewed-document GST workpaper only.",
      }),
    });
    const getReportingCapability = vi.fn(async () => response);
    const router = createDesktopRpcRouter({
      ...rpcClient(vi.fn()),
      getReportingCapability,
    });
    const request = create(GetReportingCapabilityRequestSchema, {
      report: ReportKind.GST_WORKPAPER,
      taxYear: 2024,
      entityType: ReportingEntityType.AU_BUSINESS,
    });

    const encoded = await router.invoke(
      REPORTING_CAPABILITY_CHANNEL,
      reportingCapabilityCodec.encodeRequest(request),
    );

    expect(getReportingCapability).toHaveBeenCalledExactlyOnceWith(request);
    expect(reportingCapabilityCodec.decodeResponse(encoded)).toEqual(response);

    await expect(
      router.invoke(REPORTING_CAPABILITY_CHANNEL, new Uint8Array(8_193)),
    ).rejects.toMatchObject({ code: "INVALID_RPC_REQUEST" });
    getReportingCapability.mockResolvedValueOnce(
      create(GetReportingCapabilityResponseSchema, {
        capability: create(ReportingCapabilitySchema, {
          report: ReportKind.GST_WORKPAPER,
          taxYear: 2024,
          entityType: ReportingEntityType.AU_BUSINESS,
          status: ReportingCapabilityStatus.AVAILABLE,
          appVersion: "test-core",
          summary: "Tammy supports a local reviewed-document GST workpaper only.",
          limitations: ["x".repeat(32_769)],
        }),
      }),
    );
    await expect(
      router.invoke(REPORTING_CAPABILITY_CHANNEL, reportingCapabilityCodec.encodeRequest(request)),
    ).rejects.toMatchObject({ code: "CORE_REQUEST_FAILED" });
    getReportingCapability.mockResolvedValueOnce(
      new Proxy(create(GetReportingCapabilityResponseSchema), {
        get: () => {
          throw new Error("invalid core response");
        },
      }),
    );
    await expect(
      router.invoke(REPORTING_CAPABILITY_CHANNEL, reportingCapabilityCodec.encodeRequest(request)),
    ).rejects.toMatchObject({ code: "CORE_REQUEST_FAILED" });
  });

  it("decodes and routes the generated organisation setup command", async () => {
    const response = create(CreateOrganisationResponseSchema, {
      organisation: create(OrganisationSchema, {
        id: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cc",
        displayName: "Tammy Business",
        legalName: "Tammy Business Pty Ltd",
        abn: "51824753556",
        version: 1n,
      }),
    });
    const createOrganisation = vi.fn(async () => response);
    const router = createDesktopRpcRouter({
      ...rpcClient(vi.fn()),
      createOrganisation,
    });
    const request = create(CreateOrganisationRequestSchema, {
      displayName: "Tammy Business",
      legalName: "Tammy Business Pty Ltd",
      abn: "51824753556",
    });

    const encoded = await router.invoke(
      CREATE_ORGANISATION_CHANNEL,
      createOrganisationCodec.encodeRequest(request),
    );

    expect(createOrganisation).toHaveBeenCalledExactlyOnceWith(request);
    expect(createOrganisationCodec.decodeResponse(encoded)).toEqual(response);
  });

  it("decodes and routes the generated chart read", async () => {
    const response = create(ListAccountsResponseSchema);
    const listAccounts = vi.fn(async () => response);
    const router = createDesktopRpcRouter({
      ...rpcClient(vi.fn()),
      listAccounts,
    });
    const request = create(ListAccountsRequestSchema, {
      organisationId: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb",
    });

    const encoded = await router.invoke(
      LIST_ACCOUNTS_CHANNEL,
      listAccountsCodec.encodeRequest(request),
    );

    expect(listAccounts).toHaveBeenCalledExactlyOnceWith(request);
    expect(listAccountsCodec.decodeResponse(encoded)).toEqual(response);
  });

  it("rejects unknown channels and invalid, oversized, or wrong-type request frames before core", async () => {
    const getAttentionSummary = vi.fn();
    const router = createDesktopRpcRouter(rpcClient(getAttentionSummary));
    const wrongCodec = createProtoMethodCodec({
      input: GetDiagnosticsRequestSchema,
      maximumRequestBytes: 8_192,
      maximumResponseBytes: 65_536,
      output: GetAttentionSummaryResponseSchema,
    });

    for (const [channel, frame] of [
      ["tammy:unknown", attentionCodec.encodeRequest(create(GetAttentionSummaryRequestSchema))],
      [ATTENTION_SUMMARY_CHANNEL, Uint8Array.of(0xff)],
      [ATTENTION_SUMMARY_CHANNEL, new Uint8Array(8_193)],
      [ATTENTION_SUMMARY_CHANNEL, wrongCodec.encodeRequest(create(GetDiagnosticsRequestSchema))],
    ] as const) {
      await expect(router.invoke(channel, frame)).rejects.toBeInstanceOf(DesktopRpcRouterError);
    }
    expect(getAttentionSummary).not.toHaveBeenCalled();
  });

  it("maps core failures to one stable fault without exposing details", async () => {
    const getAttentionSummary = vi.fn(async () => {
      throw new Error("capability=secret database=/private/workspace.db");
    });
    const router = createDesktopRpcRouter(rpcClient(getAttentionSummary));
    const request = attentionCodec.encodeRequest(create(GetAttentionSummaryRequestSchema));

    const error = await router
      .invoke(ATTENTION_SUMMARY_CHANNEL, request)
      .catch((caught: unknown) => caught);

    expect(error).toMatchObject({ code: "CORE_REQUEST_FAILED", message: "CORE_REQUEST_FAILED" });
    expect(String(error)).not.toContain("secret");
    expect(String(error)).not.toContain("workspace.db");
  });

  it("maps reporting capability core failures to the generic desktop fault", async () => {
    const getReportingCapability = vi.fn(async () => {
      throw new Error("capability=secret database=/private/workspace.db");
    });
    const router = createDesktopRpcRouter({
      ...rpcClient(vi.fn()),
      getReportingCapability,
    });
    const request = reportingCapabilityCodec.encodeRequest(
      create(GetReportingCapabilityRequestSchema, {
        report: ReportKind.BAS,
        taxYear: 2024,
        entityType: ReportingEntityType.AU_BUSINESS,
      }),
    );

    const error = await router
      .invoke(REPORTING_CAPABILITY_CHANNEL, request)
      .catch((caught: unknown) => caught);

    expect(error).toMatchObject({ code: "CORE_REQUEST_FAILED", message: "CORE_REQUEST_FAILED" });
    expect(String(error)).not.toContain("secret");
    expect(String(error)).not.toContain("workspace.db");
  });
});
