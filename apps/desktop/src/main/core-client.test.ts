import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, createContextValues, type Transport } from "@connectrpc/connect";
import {
  GetDiagnosticsRequestSchema,
  GetDiagnosticsResponseSchema,
  RuntimeMode,
  SystemService,
} from "@tammy/connect-client/tammy/v1/system_pb.js";
import { describe, expect, it, vi } from "vitest";

import type { CoreReadiness } from "../shared/readiness";
import {
  CoreClientError,
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
): {
  readonly transport: Transport;
  readonly unary: ReturnType<typeof vi.fn>;
} {
  const unary = vi.fn(
    async (
      method: (typeof SystemService.method)["getDiagnostics"],
      _signal: AbortSignal | undefined,
      _timeoutMs: number | undefined,
      header: HeadersInit | undefined,
    ) => ({
      stream: false as const,
      service: SystemService,
      method,
      header: new Headers(),
      trailer: new Headers(),
      message: create(GetDiagnosticsResponseSchema, response),
      requestHeader: new Headers(header),
    }),
  );

  return {
    unary,
    transport: {
      unary,
      stream: vi.fn(() => Promise.reject(new Error("unexpected streaming call"))),
    } as unknown as Transport,
  };
}

function serialized(value: unknown): string {
  if (value instanceof Error) {
    return `${value.name} ${value.message} ${JSON.stringify(value)}`;
  }
  return JSON.stringify(value);
}

describe("createCoreClient", () => {
  it("constructs the production transport with pinned loopback TLS 1.3 settings", async () => {
    const { transport } = fakeTransport();
    connectNodeMocks.createConnectTransport.mockReturnValue(transport);

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
    const { transport, unary } = fakeTransport();
    const client = createCoreClient(READINESS, transport);

    await client.getDiagnostics();

    expect(unary).toHaveBeenCalledTimes(1);
    expect(unary.mock.calls[0]?.[0]).toBe(SystemService.method.getDiagnostics);
    expect(unary.mock.calls[0]?.[4]).toEqual({});
    const header = new Headers(unary.mock.calls[0]?.[3]);
    expect(header.get("X-Tammy-Capability")).toBe(CAPABILITY);
    expect([...header.entries()].filter(([name]) => name === "x-tammy-capability")).toHaveLength(1);
  });

  it("returns only a frozen structured-clone-safe projection", async () => {
    const { transport } = fakeTransport();
    const diagnostics: SystemDiagnostics = await createCoreClient(
      READINESS,
      transport,
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
    const { transport } = fakeTransport({
      apiVersion: "tammy.v1",
      coreVersion: secret,
      runtimeMode: RuntimeMode.OFFLINE,
      networkRequired: false,
      ...override,
    });

    const error = await createCoreClient(READINESS, transport)
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

  it("sanitizes transport failures while preserving the Connect status code", async () => {
    const transport = {
      unary: vi.fn(async () => {
        throw new ConnectError(
          `${READINESS.capability}:${READINESS.port}:${READINESS.caPem}`,
          Code.Unauthenticated,
          new Headers({
            Authorization: READINESS.capability,
          }),
        );
      }),
      stream: vi.fn(() => Promise.reject(new Error("unexpected streaming call"))),
    } as unknown as Transport;

    const error = await createCoreClient(READINESS, transport)
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
