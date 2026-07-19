import { ConnectError, createClient, type Interceptor, type Transport } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-node";
import { RuntimeMode, SystemService } from "@tammy/connect-client/tammy/v1/system_pb.js";

import type { CoreReadiness } from "../shared/readiness";

const CAPABILITY_HEADER = "X-Tammy-Capability";
const EXPECTED_API_VERSION = "tammy.v1";

export interface SystemDiagnostics {
  readonly apiVersion: string;
  readonly coreVersion: string;
  readonly runtimeMode: "offline";
  readonly networkRequired: false;
}

export interface CoreClient {
  readonly getDiagnostics: () => Promise<SystemDiagnostics>;
}

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

function productionTransport(readiness: Readonly<CoreReadiness>): Transport {
  return createConnectTransport({
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
}

function capabilityHeaders(capability: string): Headers {
  const headers = new Headers();
  headers.set(CAPABILITY_HEADER, capability);
  return headers;
}

export function createCoreClient(
  readiness: Readonly<CoreReadiness>,
  transport: Transport = productionTransport(readiness),
): Readonly<CoreClient> {
  const client = createClient(SystemService, transport);

  return Object.freeze({
    getDiagnostics: async (): Promise<SystemDiagnostics> => {
      let response: Awaited<ReturnType<typeof client.getDiagnostics>>;
      try {
        response = await client.getDiagnostics(
          {},
          {
            headers: capabilityHeaders(readiness.capability),
          },
        );
      } catch (error) {
        throw new ConnectError("Core request failed.", ConnectError.from(error).code);
      }

      if (
        response.apiVersion !== EXPECTED_API_VERSION ||
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
