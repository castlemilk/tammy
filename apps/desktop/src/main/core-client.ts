import { ConnectError, createClient, type Interceptor, type Transport } from "@connectrpc/connect";
import { type ConnectTransportOptions, createConnectTransport } from "@connectrpc/connect-node";
import { RuntimeMode, SystemService } from "@tammy/connect-client/tammy/v1/system_pb.js";

import type { SystemDiagnostics } from "../shared/desktop-api";
import type { CoreReadiness } from "../shared/readiness";

export type { SystemDiagnostics } from "../shared/desktop-api";

const CAPABILITY_HEADER = "X-Tammy-Capability";
const EXPECTED_API_VERSION = "tammy.v1";
const CORE_VERSION_PATTERN = /^[\x20-\x7e]{1,128}$/;

export interface CoreClient {
  readonly getDiagnostics: () => Promise<SystemDiagnostics>;
}

export type CoreTransportFactory = (options: ConnectTransportOptions) => Transport;

export type CoreClientErrorCode = "INVALID_DIAGNOSTICS";

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
  const client = createClient(SystemService, transport);

  return Object.freeze({
    getDiagnostics: async (): Promise<SystemDiagnostics> => {
      let response: Awaited<ReturnType<typeof client.getDiagnostics>>;
      try {
        response = await client.getDiagnostics({});
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
