import { create } from "@bufbuild/protobuf";
import {
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  AssertTOTPRequestSchema,
  AssertTOTPResponseSchema,
  ConfirmTOTPRequestSchema,
  ConfirmTOTPResponseSchema,
  EnrolTOTPRequestSchema,
  EnrolTOTPResponseSchema,
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  CreateOrganisationRequestSchema,
  CreateOrganisationResponseSchema,
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
  OrganisationSchema,
  RecordEntityVerificationRequestSchema,
  RecordEntityVerificationResponseSchema,
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
import { GetDiagnosticsRequestSchema } from "@tammy/connect-client/tammy/v1/system_pb.js";
import { describe, expect, it, vi } from "vitest";
import {
  CREATE_ORGANISATION_CHANNEL,
  DESKTOP_PROTO_CHANNELS,
  DESKTOP_PROTO_REQUEST_LIMITS,
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
    getCurrentUser: vi.fn(),
    enrolTotp: vi.fn(),
    confirmTotp: vi.fn(),
    assertTotp: vi.fn(),
    getOrganisation: vi.fn(),
    recordEntityVerification: vi.fn(),
    getSbrReadiness: vi.fn(),
    importMachineCredential: vi.fn(),
    getMachineCredentialStatus: vi.fn(),
    unlockMachineCredential: vi.fn(),
    replaceMachineCredential: vi.fn(),
    removeMachineCredential: vi.fn(),
    importSbrProductId: vi.fn(),
    removeSbrProductId: vi.fn(),
    runSbrReadinessFixture: vi.fn(),
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

  it("freezes the exact added generic channel order without mediated mutation channels", () => {
    expect(DESKTOP_PROTO_CHANNELS.slice(-11)).toEqual([
      "tammy:identity-current-user",
      "tammy:identity-enrol-totp",
      "tammy:identity-confirm-totp",
      "tammy:identity-assert-totp",
      "tammy:organisation-get",
      "tammy:organisation-record-verification",
      "tammy:sbr-readiness",
      "tammy:sbr-credential-status",
      "tammy:sbr-credential-remove",
      "tammy:sbr-product-id-remove",
      "tammy:sbr-run-fixture",
    ]);
    expect(DESKTOP_PROTO_CHANNELS).not.toContain("tammy:sbr-credential-import");
    expect(DESKTOP_PROTO_CHANNELS).not.toContain("tammy:sbr-credential-replace");
    expect(DESKTOP_PROTO_CHANNELS).not.toContain("tammy:sbr-credential-unlock");
    expect(DESKTOP_PROTO_CHANNELS).not.toContain("tammy:sbr-product-id-import");
    expect(Object.isFrozen(DESKTOP_PROTO_CHANNELS)).toBe(true);
    expect(DESKTOP_PROTO_REQUEST_LIMITS).toHaveLength(DESKTOP_PROTO_CHANNELS.length);
    expect(DESKTOP_PROTO_REQUEST_LIMITS.slice(-11)).toEqual([
      8_192,
      8_192,
      8_192,
      8_192,
      8_192,
      Math.floor(1.1 * 1024 * 1024),
      8_192,
      8_192,
      8_192,
      8_192,
      8_192,
    ]);
    expect(Object.isFrozen(DESKTOP_PROTO_REQUEST_LIMITS)).toBe(true);
  });

  it("routes every added generated method with the exact standard caps", async () => {
    const cases = [
      [
        "tammy:identity-current-user",
        GetCurrentUserRequestSchema,
        GetCurrentUserResponseSchema,
        "getCurrentUser",
      ],
      ["tammy:identity-enrol-totp", EnrolTOTPRequestSchema, EnrolTOTPResponseSchema, "enrolTotp"],
      [
        "tammy:identity-confirm-totp",
        ConfirmTOTPRequestSchema,
        ConfirmTOTPResponseSchema,
        "confirmTotp",
      ],
      [
        "tammy:identity-assert-totp",
        AssertTOTPRequestSchema,
        AssertTOTPResponseSchema,
        "assertTotp",
      ],
      [
        "tammy:organisation-get",
        GetOrganisationRequestSchema,
        GetOrganisationResponseSchema,
        "getOrganisation",
      ],
      [
        "tammy:sbr-readiness",
        GetSbrReadinessRequestSchema,
        GetSbrReadinessResponseSchema,
        "getSbrReadiness",
      ],
      [
        "tammy:sbr-credential-status",
        GetMachineCredentialStatusRequestSchema,
        GetMachineCredentialStatusResponseSchema,
        "getMachineCredentialStatus",
      ],
      [
        "tammy:sbr-credential-remove",
        RemoveMachineCredentialRequestSchema,
        RemoveMachineCredentialResponseSchema,
        "removeMachineCredential",
      ],
      [
        "tammy:sbr-product-id-remove",
        RemoveSbrProductIdRequestSchema,
        RemoveSbrProductIdResponseSchema,
        "removeSbrProductId",
      ],
      [
        "tammy:sbr-run-fixture",
        RunSbrReadinessFixtureRequestSchema,
        RunSbrReadinessFixtureResponseSchema,
        "runSbrReadinessFixture",
      ],
    ] as const;

    for (const [channel, requestSchema, responseSchema, method] of cases) {
      const request = create(requestSchema);
      const response = create(responseSchema);
      const implementation = vi.fn(async () => response);
      const client = { ...rpcClient(vi.fn()), [method]: implementation };
      const router = createDesktopRpcRouter(client);
      const codec = createProtoMethodCodec({
        input: requestSchema,
        maximumRequestBytes: 8_192,
        maximumResponseBytes: 32_768,
        output: responseSchema,
      });

      const encoded = await router.invoke(channel, codec.encodeRequest(request));

      expect(implementation).toHaveBeenCalledExactlyOnceWith(request);
      expect(codec.decodeResponse(encoded)).toEqual(response);
      await expect(router.invoke(channel, new Uint8Array(8_193))).rejects.toMatchObject({
        code: "INVALID_RPC_REQUEST",
      });
    }
  });

  it("allows the verification request up to 1.1 MiB while keeping a 32 KiB response", async () => {
    const response = create(RecordEntityVerificationResponseSchema);
    const recordEntityVerification = vi.fn(async () => response);
    const router = createDesktopRpcRouter({
      ...rpcClient(vi.fn()),
      recordEntityVerification,
    });
    const codec = createProtoMethodCodec({
      input: RecordEntityVerificationRequestSchema,
      maximumRequestBytes: Math.floor(1.1 * 1024 * 1024),
      maximumResponseBytes: 32_768,
      output: RecordEntityVerificationResponseSchema,
    });
    const request = create(RecordEntityVerificationRequestSchema);

    await expect(
      router.invoke("tammy:organisation-record-verification", codec.encodeRequest(request)),
    ).resolves.toEqual(expect.any(Uint8Array));
    await expect(
      router.invoke(
        "tammy:organisation-record-verification",
        new Uint8Array(Math.floor(1.1 * 1024 * 1024) + 1),
      ),
    ).rejects.toMatchObject({ code: "INVALID_RPC_REQUEST" });
  });

  it("injects only trusted file and transient values into partial generated commands", async () => {
    const path = "/private/trusted/credential.p12";
    const bookmark = Uint8Array.of(9, 8, 7);
    const selection = {
      selectMachineCredentialFile: vi.fn(async () => ({
        selected: true as const,
        handle: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb",
      })),
      consumeMachineCredentialFile: vi.fn(async () => ({
        selectedLocalPath: path,
        securityScopedBookmark: bookmark,
      })),
    };
    const responses = {
      importMachineCredential: create(ImportMachineCredentialResponseSchema),
      replaceMachineCredential: create(ReplaceMachineCredentialResponseSchema),
      unlockMachineCredential: create(UnlockMachineCredentialResponseSchema),
      importSbrProductId: create(ImportSbrProductIdResponseSchema),
    };
    const client = {
      ...rpcClient(vi.fn()),
      importMachineCredential: vi.fn(async () => responses.importMachineCredential),
      replaceMachineCredential: vi.fn(async () => responses.replaceMachineCredential),
      unlockMachineCredential: vi.fn(async () => responses.unlockMachineCredential),
      importSbrProductId: vi.fn(async () => responses.importSbrProductId),
    };
    const router = createDesktopRpcRouter(client, selection);
    const handle = "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb";

    await expect(router.selectMachineCredentialFile()).resolves.toEqual({ selected: true, handle });
    const importCodec = createProtoMethodCodec({
      input: ImportMachineCredentialRequestSchema,
      maximumRequestBytes: 8_192,
      maximumResponseBytes: 32_768,
      output: ImportMachineCredentialResponseSchema,
    });
    const replaceCodec = createProtoMethodCodec({
      input: ReplaceMachineCredentialRequestSchema,
      maximumRequestBytes: 8_192,
      maximumResponseBytes: 32_768,
      output: ReplaceMachineCredentialResponseSchema,
    });
    const unlockCodec = createProtoMethodCodec({
      input: UnlockMachineCredentialRequestSchema,
      maximumRequestBytes: 8_192,
      maximumResponseBytes: 32_768,
      output: UnlockMachineCredentialResponseSchema,
    });
    const productCodec = createProtoMethodCodec({
      input: ImportSbrProductIdRequestSchema,
      maximumRequestBytes: 8_192,
      maximumResponseBytes: 32_768,
      output: ImportSbrProductIdResponseSchema,
    });

    await router.importMachineCredential({
      command: importCodec.encodeRequest(create(ImportMachineCredentialRequestSchema)),
      handle,
      password: "credential-password",
    });
    await router.replaceMachineCredential({
      command: replaceCodec.encodeRequest(create(ReplaceMachineCredentialRequestSchema)),
      handle,
      password: "replacement-password",
    });
    await router.unlockMachineCredential({
      command: unlockCodec.encodeRequest(create(UnlockMachineCredentialRequestSchema)),
      password: "unlock-password",
    });
    await router.importSbrProductId({
      command: productCodec.encodeRequest(
        create(ImportSbrProductIdRequestSchema, {
          evteProductIdentifier: "EVTE.PRODUCT",
          evteServiceIdentifier: "EVTE.SERVICE",
        }),
      ),
      productId: "product-id",
    });

    expect(client.importMachineCredential).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        selectedLocalPath: path,
        securityScopedBookmark: bookmark,
        password: new TextEncoder().encode("credential-password"),
      }),
    );
    expect(client.replaceMachineCredential).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ selectedLocalPath: path }),
    );
    expect(client.unlockMachineCredential).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ password: new TextEncoder().encode("unlock-password") }),
    );
    expect(client.importSbrProductId).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        productIdValue: "product-id",
        evteProductIdentifier: "EVTE.PRODUCT",
        evteServiceIdentifier: "EVTE.SERVICE",
      }),
    );
  });

  it("rejects renderer-supplied trusted fields before selection or core", async () => {
    const selection = {
      selectMachineCredentialFile: vi.fn(),
      consumeMachineCredentialFile: vi.fn(),
    };
    const client = rpcClient(vi.fn());
    const router = createDesktopRpcRouter(client, selection);
    const handle = "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb";
    const importCodec = createProtoMethodCodec({
      input: ImportMachineCredentialRequestSchema,
      maximumRequestBytes: 8_192,
      maximumResponseBytes: 32_768,
      output: ImportMachineCredentialResponseSchema,
    });
    const productCodec = createProtoMethodCodec({
      input: ImportSbrProductIdRequestSchema,
      maximumRequestBytes: 8_192,
      maximumResponseBytes: 32_768,
      output: ImportSbrProductIdResponseSchema,
    });

    for (const request of [
      create(ImportMachineCredentialRequestSchema, { selectedLocalPath: "/renderer/secret" }),
      create(ImportMachineCredentialRequestSchema, { securityScopedBookmark: Uint8Array.of(1) }),
      create(ImportMachineCredentialRequestSchema, { password: Uint8Array.of(1) }),
    ]) {
      await expect(
        router.importMachineCredential({
          command: importCodec.encodeRequest(request),
          handle,
          password: "trusted-separate-value",
        }),
      ).rejects.toMatchObject({ code: "INVALID_RPC_REQUEST", message: "INVALID_RPC_REQUEST" });
    }
    await expect(
      router.importSbrProductId({
        command: productCodec.encodeRequest(
          create(ImportSbrProductIdRequestSchema, { productIdValue: "renderer-secret" }),
        ),
        productId: "trusted-separate-value",
      }),
    ).rejects.toMatchObject({ code: "INVALID_RPC_REQUEST" });
    expect(selection.consumeMachineCredentialFile).not.toHaveBeenCalled();
    expect(client.importMachineCredential).not.toHaveBeenCalled();
    expect(client.importSbrProductId).not.toHaveBeenCalled();
  });

  it("sanitizes mediated resolution, core, and response failures", async () => {
    const secret = "/private/secret.p12 product-id-secret";
    const selection = {
      selectMachineCredentialFile: vi.fn(async () => {
        throw new Error(secret);
      }),
      consumeMachineCredentialFile: vi.fn(async () => {
        throw new Error(secret);
      }),
    };
    const router = createDesktopRpcRouter(rpcClient(vi.fn()), selection);
    const command = createProtoMethodCodec({
      input: ImportMachineCredentialRequestSchema,
      maximumRequestBytes: 8_192,
      maximumResponseBytes: 32_768,
      output: ImportMachineCredentialResponseSchema,
    }).encodeRequest(create(ImportMachineCredentialRequestSchema));

    for (const call of [
      router.selectMachineCredentialFile(),
      router.importMachineCredential({
        command,
        handle: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb",
        password: "secret-password",
      }),
    ]) {
      const error = await call.catch((caught: unknown) => caught);
      expect(error).toMatchObject({ code: "CORE_REQUEST_FAILED", message: "CORE_REQUEST_FAILED" });
      expect(String(error)).not.toContain(secret);
      expect(String(error)).not.toContain("secret-password");
    }
  });
});
