import { create } from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createContextValues,
  createRouterTransport,
  type Transport,
} from "@connectrpc/connect";
import type { ConnectTransportOptions } from "@connectrpc/connect-node";
import { ListAccountsRequestSchema } from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  BasPreparationService,
  GetBasPreparationRequestSchema,
  ListBasObligationsProfilesRequestSchema,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import {
  IngestDocumentRequestSchema,
  ListDocumentsRequestSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  AssertTOTPRequestSchema,
  AssignRolesRequestSchema,
  ConfirmTOTPRequestSchema,
  EnrolTOTPRequestSchema,
  GetCurrentUserRequestSchema,
  IdentityService,
  SignInRequestSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  GetOrganisationRequestSchema,
  OrganisationService,
  RecordEntityVerificationRequestSchema,
  UpdateOrganisationRequestSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  GetAttentionSummaryRequestSchema,
  OverviewService,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  GetReportingCapabilityRequestSchema,
  ReportingCapabilityService,
  ReportingCapabilityStatus,
  ReportingEntityType,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import {
  GetMachineCredentialStatusRequestSchema,
  GetSbrReadinessRequestSchema,
  ImportMachineCredentialRequestSchema,
  ImportSbrProductIdRequestSchema,
  RemoveMachineCredentialRequestSchema,
  RemoveSbrProductIdRequestSchema,
  ReplaceMachineCredentialRequestSchema,
  RunSbrReadinessFixtureRequestSchema,
  SbrService,
  UnlockMachineCredentialRequestSchema,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import {
  GetDiagnosticsRequestSchema,
  GetDiagnosticsResponseSchema,
  RuntimeMode,
  SystemService,
} from "@tammy/connect-client/tammy/v1/system_pb.js";
import { CreateBasDraftRequestSchema } from "@tammy/connect-client/tammy/v1/tax_pb.js";
import {
  CreateWorkspaceRequestSchema,
  UnlockWorkspaceRequestSchema,
  WorkspaceService,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { describe, expect, it, vi } from "vitest";

import type { CoreReadiness } from "../shared/readiness";
import {
  type CoreClient,
  CoreClientError,
  type CoreTransportFactory,
  capabilityInterceptor,
  createCoreClient,
  type SystemDiagnostics,
} from "./core-client";

const connectNodeMocks = vi.hoisted(() => ({
  createConnectTransport: vi.fn(),
}));

vi.mock("@connectrpc/connect-node", () => connectNodeMocks);

const CAPABILITY = Buffer.alloc(32, 0x31).toString("base64url");
const READINESS = Object.freeze({
  protocol: "tammy-core-ready-v1",
  port: 43_219,
  caPem: "SECRET TEST CA",
  capability: CAPABILITY,
}) satisfies Readonly<CoreReadiness>;

function fakeTransport(
  response: {
    readonly apiVersion: string;
    readonly coreVersion: string;
    readonly runtimeMode: RuntimeMode;
    readonly networkRequired: boolean;
  } = {
    apiVersion: "tammy.v1",
    coreVersion: "test-core",
    runtimeMode: RuntimeMode.OFFLINE,
    networkRequired: false,
  },
  failure?: ConnectError,
  rpcDelayMs = 0,
): {
  readonly factory: CoreTransportFactory;
  readonly methods: unknown[];
  readonly receivedHeaders: Headers[];
  readonly receivedTimeouts: Array<number | undefined>;
  readonly receivedBasPreparationCalls: Array<{
    readonly timeoutMs: number | undefined;
    readonly headers: Headers;
  }>;
  readonly receivedBasProfileListCalls: Array<{
    readonly timeoutMs: number | undefined;
    readonly headers: Headers;
  }>;
  readonly receivedWorkspaceCreateCalls: Array<{
    readonly timeoutMs: number | undefined;
    readonly headers: Headers;
  }>;
} {
  const methods: unknown[] = [];
  const receivedHeaders: Headers[] = [];
  const receivedTimeouts: Array<number | undefined> = [];
  const receivedBasPreparationCalls: Array<{
    readonly timeoutMs: number | undefined;
    readonly headers: Headers;
  }> = [];
  const receivedBasProfileListCalls: Array<{
    readonly timeoutMs: number | undefined;
    readonly headers: Headers;
  }> = [];
  const receivedWorkspaceCreateCalls: Array<{
    readonly timeoutMs: number | undefined;
    readonly headers: Headers;
  }> = [];
  const factory = vi.fn(
    (options: ConnectTransportOptions): Transport =>
      createRouterTransport(
        (router) => {
          router.service(SystemService, {
            getDiagnostics: async (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              receivedTimeouts.push(context.timeoutMs());
              if (rpcDelayMs > 0) {
                await new Promise<void>((resolve) => setTimeout(resolve, rpcDelayMs));
              }
              if (failure) {
                throw failure;
              }
              return response;
            },
          });
          router.service(WorkspaceService, {
            createWorkspace: async (_request, context) => {
              methods.push(context.method);
              const headers = new Headers(context.requestHeader);
              receivedHeaders.push(headers);
              receivedWorkspaceCreateCalls.push({
                timeoutMs: context.timeoutMs(),
                headers,
              });
              if (rpcDelayMs > 0) {
                await new Promise<void>((resolve) => setTimeout(resolve, rpcDelayMs));
              }
              return {};
            },
          });
          router.service(OverviewService, {
            getAttentionSummary: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              return {
                documentsNeedingReview: 3,
                documentsReviewedInPeriod: 12,
              };
            },
          });
          router.service(ReportingCapabilityService, {
            getReportingCapability: (request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              return {
                capability: {
                  report: request.report,
                  taxYear: request.taxYear,
                  entityType: request.entityType,
                  status: ReportingCapabilityStatus.AVAILABLE,
                  appVersion: "test-core",
                  summary: "Tammy supports a local reviewed-document GST workpaper only.",
                },
              };
            },
          });
          router.service(IdentityService, {
            getCurrentUser: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              return {};
            },
            assignRoles: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              return {};
            },
            enrolTOTP: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              return {};
            },
            confirmTOTP: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              return {};
            },
            assertTOTP: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              return {};
            },
          });
          router.service(OrganisationService, {
            getOrganisation: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              return {};
            },
            updateOrganisation: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              return {};
            },
            recordEntityVerification: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              return {};
            },
          });
          router.service(SbrService, {
            getSbrReadiness: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              receivedTimeouts.push(context.timeoutMs());
              return {};
            },
            importMachineCredential: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              receivedTimeouts.push(context.timeoutMs());
              return {};
            },
            getMachineCredentialStatus: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              receivedTimeouts.push(context.timeoutMs());
              return {};
            },
            unlockMachineCredential: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              receivedTimeouts.push(context.timeoutMs());
              return {};
            },
            replaceMachineCredential: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              receivedTimeouts.push(context.timeoutMs());
              return {};
            },
            removeMachineCredential: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              receivedTimeouts.push(context.timeoutMs());
              return {};
            },
            importSbrProductId: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              receivedTimeouts.push(context.timeoutMs());
              return {};
            },
            removeSbrProductId: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              receivedTimeouts.push(context.timeoutMs());
              return {};
            },
            runSbrReadinessFixture: (_request, context) => {
              methods.push(context.method);
              receivedHeaders.push(new Headers(context.requestHeader));
              receivedTimeouts.push(context.timeoutMs());
              return {};
            },
          });
          router.service(BasPreparationService, {
            listBasObligationsProfiles: async (_request, context) => {
              methods.push(context.method);
              const headers = new Headers(context.requestHeader);
              receivedHeaders.push(headers);
              receivedBasProfileListCalls.push({
                timeoutMs: context.timeoutMs(),
                headers,
              });
              if (rpcDelayMs > 0) {
                await new Promise<void>((resolve) => setTimeout(resolve, rpcDelayMs));
              }
              return {};
            },
            getBasPreparation: async (_request, context) => {
              methods.push(context.method);
              const headers = new Headers(context.requestHeader);
              receivedHeaders.push(headers);
              receivedBasPreparationCalls.push({
                timeoutMs: context.timeoutMs(),
                headers,
              });
              if (rpcDelayMs > 0) {
                await new Promise<void>((resolve) => setTimeout(resolve, rpcDelayMs));
              }
              return {};
            },
          });
        },
        {
          transport: {
            ...(options.defaultTimeoutMs === undefined
              ? {}
              : { defaultTimeoutMs: options.defaultTimeoutMs }),
            interceptors: options.interceptors ?? [],
          },
        },
      ),
  );

  return {
    factory,
    methods,
    receivedHeaders,
    receivedTimeouts,
    receivedBasPreparationCalls,
    receivedBasProfileListCalls,
    receivedWorkspaceCreateCalls,
  };
}

function serialized(value: unknown): string {
  if (value instanceof Error) {
    return `${value.name} ${value.message} ${JSON.stringify(value)}`;
  }
  return JSON.stringify(value);
}

describe("createCoreClient", () => {
  it.each([
    [
      "INGEST_DOCUMENT",
      (client: CoreClient) => client.ingestDocument(create(IngestDocumentRequestSchema)),
    ],
    [
      "LIST_DOCUMENTS",
      (client: CoreClient) => client.listDocuments(create(ListDocumentsRequestSchema)),
    ],
    [
      "LIST_ACCOUNTS",
      (client: CoreClient) => client.listAccounts(create(ListAccountsRequestSchema)),
    ],
    [
      "CREATE_BAS_DRAFT",
      (client: CoreClient) => client.createBasDraft(create(CreateBasDraftRequestSchema)),
    ],
    [
      "UNLOCK_WORKSPACE",
      (client: CoreClient) => client.unlockWorkspace(create(UnlockWorkspaceRequestSchema)),
    ],
    ["SIGN_IN", (client: CoreClient) => client.signIn(create(SignInRequestSchema))],
  ] as const)(
    "retains only a fixed operation and status when %s fails",
    async (operation, invoke) => {
      const failure = new ConnectError(
        "private path /tmp/payroll, password and source bytes",
        Code.DeadlineExceeded,
        {
          Authorization: READINESS.capability,
        },
      );
      const factory: CoreTransportFactory = () => ({
        unary: async () => {
          throw failure;
        },
        stream: async () => {
          throw failure;
        },
      });
      const diagnostic = vi.spyOn(console, "error").mockImplementation(() => undefined);
      try {
        await expect(invoke(createCoreClient(READINESS, factory))).rejects.toMatchObject({
          code: Code.DeadlineExceeded,
          rawMessage: "Core request failed.",
        });
        expect(diagnostic).toHaveBeenCalledExactlyOnceWith(
          `LOCAL_ENGINE_CORE_RPC_${operation}_FAILED LOCAL_ENGINE_CONNECT_DEADLINE_EXCEEDED`,
        );
      } finally {
        diagnostic.mockRestore();
      }
    },
  );

  it("constructs the production transport with pinned loopback TLS 1.3 settings", async () => {
    const { factory } = fakeTransport();
    connectNodeMocks.createConnectTransport.mockImplementation(factory);

    await createCoreClient(READINESS).getDiagnostics();

    expect(connectNodeMocks.createConnectTransport).toHaveBeenCalledTimes(1);
    expect(connectNodeMocks.createConnectTransport).toHaveBeenCalledWith({
      baseUrl: `https://127.0.0.1:${READINESS.port}`,
      httpVersion: "1.1",
      defaultTimeoutMs: 5_000,
      nodeOptions: {
        ca: READINESS.caPem,
        rejectUnauthorized: true,
        minVersion: "TLSv1.3",
        maxVersion: "TLSv1.3",
      },
      interceptors: [expect.any(Function)],
    });
  });

  it("calls the generated diagnostics method with exactly one capability header", async () => {
    const { factory, methods, receivedHeaders } = fakeTransport();
    const client = createCoreClient(READINESS, factory);

    await client.getDiagnostics();

    expect(methods).toEqual([SystemService.method.getDiagnostics]);
    expect(receivedHeaders).toHaveLength(1);
    const header = receivedHeaders[0] as Headers;
    expect(header.get("X-Tammy-Capability")).toBe(CAPABILITY);
    expect([...header.entries()].filter(([name]) => name === "x-tammy-capability")).toHaveLength(1);
  });

  it("calls the generated Overview method and retains the generated response type", async () => {
    const { factory, methods, receivedHeaders } = fakeTransport();
    const client = createCoreClient(READINESS, factory);
    const request = create(GetAttentionSummaryRequestSchema, {
      organisationId: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb",
    });

    const response = await client.getAttentionSummary(request);

    expect(response.$typeName).toBe("tammy.v1.GetAttentionSummaryResponse");
    expect(response.documentsNeedingReview).toBe(3);
    expect(methods).toEqual([OverviewService.method.getAttentionSummary]);
    expect(receivedHeaders[0]?.get("X-Tammy-Capability")).toBe(CAPABILITY);
  });

  it("calls the generated reporting capability method before workspace setup", async () => {
    const { factory, methods, receivedHeaders } = fakeTransport();
    const client = createCoreClient(READINESS, factory);
    const request = create(GetReportingCapabilityRequestSchema, {
      report: ReportKind.GST_WORKPAPER,
      taxYear: 2024,
      entityType: ReportingEntityType.AU_BUSINESS,
    });

    const response = await client.getReportingCapability(request);

    expect(response.capability).toMatchObject({
      report: ReportKind.GST_WORKPAPER,
      taxYear: 2024,
      entityType: ReportingEntityType.AU_BUSINESS,
      status: ReportingCapabilityStatus.AVAILABLE,
      appVersion: "test-core",
    });
    expect(methods).toEqual([ReportingCapabilityService.method.getReportingCapability]);
    expect(receivedHeaders[0]?.get("X-Tammy-Capability")).toBe(CAPABILITY);
  });

  it("exposes the generated BAS preparation client with capability authentication", async () => {
    const { factory, methods, receivedHeaders } = fakeTransport();
    const client = createCoreClient(READINESS, factory);

    const response = await client.basPreparation?.listBasObligationsProfiles(
      create(ListBasObligationsProfilesRequestSchema),
    );

    expect(response?.$typeName).toBe("tammy.v1.ListBasObligationsProfilesResponse");
    expect(methods).toEqual([BasPreparationService.method.listBasObligationsProfiles]);
    expect(receivedHeaders).toHaveLength(1);
    expect(receivedHeaders[0]?.get("X-Tammy-Capability")).toBe(CAPABILITY);
  });

  it("bounds only GetBasPreparation at 30 seconds while ordinary RPCs keep the 5-second default", async () => {
    const { factory, receivedTimeouts, receivedBasPreparationCalls, receivedBasProfileListCalls } =
      fakeTransport(undefined, undefined, 5_300);
    const client = createCoreClient(READINESS, factory);

    const preparationClient = client.basPreparation;
    if (!preparationClient) throw new Error("BAS preparation client is unavailable.");
    const [ordinaryResult, profileListResult, preparationResult] = await Promise.allSettled([
      client.getDiagnostics(),
      preparationClient.listBasObligationsProfiles(create(ListBasObligationsProfilesRequestSchema)),
      preparationClient.getBasPreparation(create(GetBasPreparationRequestSchema)),
    ]);

    for (const result of [ordinaryResult, profileListResult]) {
      expect(result.status).toBe("rejected");
      if (result.status === "rejected") {
        expect(ConnectError.from(result.reason).code).toBe(Code.DeadlineExceeded);
      }
    }
    expect(preparationResult.status).toBe("fulfilled");
    expect(receivedTimeouts).toHaveLength(1);
    expect(receivedTimeouts[0]).toBeGreaterThanOrEqual(4_900);
    expect(receivedTimeouts[0]).toBeLessThanOrEqual(5_000);
    expect(receivedBasProfileListCalls).toHaveLength(1);
    expect(receivedBasProfileListCalls[0]?.timeoutMs).toBeGreaterThanOrEqual(4_900);
    expect(receivedBasProfileListCalls[0]?.timeoutMs).toBeLessThanOrEqual(5_000);
    expect(receivedBasProfileListCalls[0]?.headers.get("X-Tammy-Capability")).toBe(CAPABILITY);
    expect(receivedBasPreparationCalls).toHaveLength(1);
    expect(receivedBasPreparationCalls[0]?.timeoutMs).toBeGreaterThan(29_000);
    expect(receivedBasPreparationCalls[0]?.timeoutMs).toBeLessThanOrEqual(30_000);
    expect(receivedBasPreparationCalls[0]?.headers.get("X-Tammy-Capability")).toBe(CAPABILITY);
  }, 8_000);

  it("bounds workspace creation at 30 seconds while ordinary RPCs keep the 5-second default", async () => {
    const { factory, receivedTimeouts, receivedWorkspaceCreateCalls } = fakeTransport(
      undefined,
      undefined,
      5_300,
    );
    const client = createCoreClient(READINESS, factory);
    const [ordinaryResult, workspaceResult] = await Promise.allSettled([
      client.getDiagnostics(),
      client.createWorkspace(create(CreateWorkspaceRequestSchema)),
    ]);

    expect(ordinaryResult.status).toBe("rejected");
    if (ordinaryResult.status === "rejected")
      expect(ConnectError.from(ordinaryResult.reason).code).toBe(Code.DeadlineExceeded);
    expect(workspaceResult.status).toBe("fulfilled");
    expect(receivedTimeouts).toHaveLength(1);
    expect(receivedTimeouts[0]).toBeGreaterThanOrEqual(4_900);
    expect(receivedTimeouts[0]).toBeLessThanOrEqual(5_000);
    expect(receivedWorkspaceCreateCalls).toHaveLength(1);
    expect(receivedWorkspaceCreateCalls[0]?.timeoutMs).toBeGreaterThan(29_000);
    expect(receivedWorkspaceCreateCalls[0]?.timeoutMs).toBeLessThanOrEqual(30_000);
    expect(receivedWorkspaceCreateCalls[0]?.headers.get("X-Tammy-Capability")).toBe(CAPABILITY);
  }, 8_000);

  it("preserves explicit shorter BAS read deadlines, cancellation signals, and caller headers", async () => {
    const { factory, receivedBasPreparationCalls } = fakeTransport(undefined, undefined, 500);
    const client = createCoreClient(READINESS, factory);
    const preparation = client.basPreparation;
    if (!preparation) throw new Error("BAS preparation client is unavailable.");

    const controller = new AbortController();
    const cancelledRead = preparation
      .getBasPreparation(create(GetBasPreparationRequestSchema), {
        timeoutMs: 30_000,
        signal: controller.signal,
        headers: { "X-Caller-Test": "cancel-preserved" },
      })
      .catch((error: unknown) => error);
    const shortRead = preparation
      .getBasPreparation(create(GetBasPreparationRequestSchema), {
        timeoutMs: 100,
        headers: { "X-Caller-Test": "timeout-preserved" },
      })
      .catch((error: unknown) => error);
    setTimeout(() => controller.abort(), 25);

    const [cancelledError, shortError] = await Promise.all([cancelledRead, shortRead]);

    expect(ConnectError.from(cancelledError).code).toBe(Code.Canceled);
    expect(ConnectError.from(shortError).code).toBe(Code.DeadlineExceeded);
    const cancelledCall = receivedBasPreparationCalls.find(
      (call) => call.headers.get("X-Caller-Test") === "cancel-preserved",
    );
    expect(cancelledCall?.timeoutMs).toBeGreaterThan(29_000);
    expect(cancelledCall?.timeoutMs).toBeLessThanOrEqual(30_000);
    const shortCall = receivedBasPreparationCalls.find(
      (call) => call.headers.get("X-Caller-Test") === "timeout-preserved",
    );
    expect(shortCall?.timeoutMs).toBeGreaterThan(0);
    expect(shortCall?.timeoutMs).toBeLessThanOrEqual(100);
  });

  it("maps every required identity, organisation, and SBR call to its generated client", async () => {
    const { factory, methods, receivedHeaders } = fakeTransport();
    const client = createCoreClient(READINESS, factory);
    const calls = [
      ["getCurrentUser", GetCurrentUserRequestSchema],
      ["assignRoles", AssignRolesRequestSchema],
      ["enrolTotp", EnrolTOTPRequestSchema],
      ["confirmTotp", ConfirmTOTPRequestSchema],
      ["assertTotp", AssertTOTPRequestSchema],
      ["getOrganisation", GetOrganisationRequestSchema],
      ["updateOrganisation", UpdateOrganisationRequestSchema],
      ["recordEntityVerification", RecordEntityVerificationRequestSchema],
      ["getSbrReadiness", GetSbrReadinessRequestSchema],
      ["importMachineCredential", ImportMachineCredentialRequestSchema],
      ["getMachineCredentialStatus", GetMachineCredentialStatusRequestSchema],
      ["unlockMachineCredential", UnlockMachineCredentialRequestSchema],
      ["replaceMachineCredential", ReplaceMachineCredentialRequestSchema],
      ["removeMachineCredential", RemoveMachineCredentialRequestSchema],
      ["importSbrProductId", ImportSbrProductIdRequestSchema],
      ["removeSbrProductId", RemoveSbrProductIdRequestSchema],
      ["runSbrReadinessFixture", RunSbrReadinessFixtureRequestSchema],
    ] as const;

    for (const [method, requestSchema] of calls) {
      await (client[method] as (request: unknown) => Promise<unknown>)(create(requestSchema));
    }

    expect(methods).toEqual([
      IdentityService.method.getCurrentUser,
      IdentityService.method.assignRoles,
      IdentityService.method.enrolTOTP,
      IdentityService.method.confirmTOTP,
      IdentityService.method.assertTOTP,
      OrganisationService.method.getOrganisation,
      OrganisationService.method.updateOrganisation,
      OrganisationService.method.recordEntityVerification,
      SbrService.method.getSbrReadiness,
      SbrService.method.importMachineCredential,
      SbrService.method.getMachineCredentialStatus,
      SbrService.method.unlockMachineCredential,
      SbrService.method.replaceMachineCredential,
      SbrService.method.removeMachineCredential,
      SbrService.method.importSbrProductId,
      SbrService.method.removeSbrProductId,
      SbrService.method.runSbrReadinessFixture,
    ]);
    expect(receivedHeaders).toHaveLength(calls.length);
    expect(receivedHeaders.every((header) => header.get("X-Tammy-Capability") === CAPABILITY)).toBe(
      true,
    );
  });

  it("gives every local SBR helper RPC enough time to return its authoritative result", async () => {
    const { factory, receivedTimeouts } = fakeTransport();
    const client = createCoreClient(READINESS, factory);
    const calls = [
      ["getSbrReadiness", GetSbrReadinessRequestSchema],
      ["importMachineCredential", ImportMachineCredentialRequestSchema],
      ["getMachineCredentialStatus", GetMachineCredentialStatusRequestSchema],
      ["unlockMachineCredential", UnlockMachineCredentialRequestSchema],
      ["replaceMachineCredential", ReplaceMachineCredentialRequestSchema],
      ["removeMachineCredential", RemoveMachineCredentialRequestSchema],
      ["importSbrProductId", ImportSbrProductIdRequestSchema],
      ["removeSbrProductId", RemoveSbrProductIdRequestSchema],
      ["runSbrReadinessFixture", RunSbrReadinessFixtureRequestSchema],
    ] as const;

    for (const [method, requestSchema] of calls) {
      await (client[method] as (request: unknown) => Promise<unknown>)(create(requestSchema));
    }

    expect(receivedTimeouts).toHaveLength(calls.length);
    for (const timeout of receivedTimeouts) {
      expect(timeout).toBeGreaterThanOrEqual(64_000);
      expect(timeout).toBeLessThanOrEqual(65_000);
    }
  });

  it("returns only a frozen structured-clone-safe projection", async () => {
    const { factory } = fakeTransport();
    const diagnostics: SystemDiagnostics = await createCoreClient(
      READINESS,
      factory,
    ).getDiagnostics();

    expect(diagnostics).toEqual({
      apiVersion: "tammy.v1",
      coreVersion: "test-core",
      runtimeMode: "offline",
      networkRequired: false,
    });
    expect(Object.keys(diagnostics)).toEqual([
      "apiVersion",
      "coreVersion",
      "runtimeMode",
      "networkRequired",
    ]);
    expect(Object.isFrozen(diagnostics)).toBe(true);
    expect(serialized(diagnostics)).not.toContain(READINESS.port.toString());
    expect(serialized(diagnostics)).not.toContain(READINESS.caPem);
    expect(serialized(diagnostics)).not.toContain(READINESS.capability);
    expect(serialized(diagnostics)).not.toContain("$typeName");
  });

  it.each([
    ["unexpected API version", { apiVersion: "tammy.v2" }],
    ["non-offline runtime", { runtimeMode: RuntimeMode.UNSPECIFIED }],
    ["network-dependent runtime", { networkRequired: true }],
  ])("rejects %s with one stable sanitized error", async (_name, override) => {
    const secret = `${READINESS.capability}:${READINESS.port}:${READINESS.caPem}`;
    const { factory } = fakeTransport({
      apiVersion: "tammy.v1",
      coreVersion: secret,
      runtimeMode: RuntimeMode.OFFLINE,
      networkRequired: false,
      ...override,
    });

    const error = await createCoreClient(READINESS, factory)
      .getDiagnostics()
      .catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(CoreClientError);
    expect(error).toMatchObject({
      code: "INVALID_DIAGNOSTICS",
      message: "Core returned invalid diagnostics.",
    });
    expect(Object.keys(error as object)).toEqual(["code", "name"]);
    expect(serialized(error)).not.toContain(secret);
    expect(serialized(error)).not.toContain(READINESS.port.toString());
    expect(serialized(error)).not.toContain(READINESS.caPem);
    expect(serialized(error)).not.toContain(READINESS.capability);
  });

  it.each([
    ["empty", ""],
    ["longer than 128 characters", "v".repeat(129)],
    ["containing a newline", "dev\nsecret-version"],
    ["containing a tab", "dev\tsecret-version"],
    ["containing DEL", "dev\u007fsecret-version"],
    ["containing non-ASCII", "dév"],
  ])("rejects a %s core version without exposing its content", async (_name, coreVersion) => {
    const { factory } = fakeTransport({
      apiVersion: "tammy.v1",
      coreVersion,
      runtimeMode: RuntimeMode.OFFLINE,
      networkRequired: false,
    });

    const error = await createCoreClient(READINESS, factory)
      .getDiagnostics()
      .catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(CoreClientError);
    expect(error).toMatchObject({
      code: "INVALID_DIAGNOSTICS",
      message: "Core returned invalid diagnostics.",
    });
    if (coreVersion.length > 0) {
      expect(serialized(error)).not.toContain(coreVersion);
    }
  });

  it.each(["v", "dev", "V".repeat(128), "release 1.2.3+build-7"])(
    "accepts a printable ASCII core version: %j",
    async (coreVersion) => {
      const { factory } = fakeTransport({
        apiVersion: "tammy.v1",
        coreVersion,
        runtimeMode: RuntimeMode.OFFLINE,
        networkRequired: false,
      });

      await expect(createCoreClient(READINESS, factory).getDiagnostics()).resolves.toMatchObject({
        coreVersion,
      });
    },
  );

  it("sanitizes transport failures while preserving the Connect status code", async () => {
    const failure = new ConnectError(
      `${READINESS.capability}:${READINESS.port}:${READINESS.caPem}`,
      Code.Unauthenticated,
      new Headers({
        Authorization: READINESS.capability,
      }),
    );
    const { factory } = fakeTransport(undefined, failure);

    const error = await createCoreClient(READINESS, factory)
      .getDiagnostics()
      .catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(ConnectError);
    expect(error).toMatchObject({
      code: Code.Unauthenticated,
      message: "[unauthenticated] Core request failed.",
      rawMessage: "Core request failed.",
    });
    expect(serialized(error)).not.toContain(READINESS.port.toString());
    expect(serialized(error)).not.toContain(READINESS.caPem);
    expect(serialized(error)).not.toContain(READINESS.capability);
    expect((error as ConnectError).metadata.get("Authorization")).toBeNull();
  });
});

describe("capabilityInterceptor", () => {
  it("uses Headers.set to replace existing values instead of appending", async () => {
    const header = new Headers();
    header.append("X-Tammy-Capability", "stale-one");
    header.append("x-tammy-capability", "stale-two");
    const set = vi.spyOn(header, "set");
    const append = vi.spyOn(header, "append");
    const request = {
      stream: false as const,
      service: SystemService,
      method: SystemService.method.getDiagnostics,
      requestMethod: "POST",
      url: "https://127.0.0.1/tammy.v1.SystemService/GetDiagnostics",
      signal: new AbortController().signal,
      header,
      contextValues: createContextValues(),
      message: create(GetDiagnosticsRequestSchema),
    };
    const response = {
      stream: false as const,
      service: SystemService,
      method: SystemService.method.getDiagnostics,
      header: new Headers(),
      trailer: new Headers(),
      message: create(GetDiagnosticsResponseSchema),
    };
    const next = vi.fn(async () => response);

    await capabilityInterceptor(CAPABILITY)(next)(request);

    expect(set).toHaveBeenCalledTimes(1);
    expect(set).toHaveBeenCalledWith("X-Tammy-Capability", CAPABILITY);
    expect(append).not.toHaveBeenCalled();
    expect(header.get("X-Tammy-Capability")).toBe(CAPABILITY);
    expect([...header.entries()].filter(([name]) => name === "x-tammy-capability")).toHaveLength(1);
    expect(next).toHaveBeenCalledWith(request);
  });
});
