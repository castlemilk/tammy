import { randomBytes, timingSafeEqual } from "node:crypto";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

import { LOCAL_API_READ_RESOURCES, LOCAL_API_TOOLS } from "./catalog.ts";
import type { LocalApiConnection } from "./connection.ts";
import { type ValidatedLocalApiRequest, validateLocalApiRequest } from "./request.ts";

export type { LocalApiConnection } from "./connection.ts";
export { readLocalApiConnectionFile, validateLocalApiConnection } from "./connection.ts";

export const LOCAL_API_MAX_REQUEST_BYTES = 128 * 1024;
export const LOCAL_API_MAX_RESPONSE_BYTES = 1024 * 1024;
export const LOCAL_API_DEADLINE_MS = 5_000;

export interface LocalApiBroker {
  readonly connection: LocalApiConnection;
  close(): Promise<void>;
}

const SAFE_EXECUTION_ERRORS = new Set([
  "LOCAL_SESSION_REQUIRED",
  "LOCAL_PAIRING_REVOKED",
  "LOCAL_READ_FAILED",
]);

class BrokerError extends Error {
  readonly code: string;
  readonly status: number;
  constructor(code: string, status: number) {
    super(code);
    this.code = code;
    this.status = status;
  }
}

function headerValues(request: IncomingMessage, name: string): string[] {
  const values: string[] = [];
  for (let index = 0; index < request.rawHeaders.length; index += 2) {
    if (request.rawHeaders[index]?.toLowerCase() === name)
      values.push(request.rawHeaders[index + 1] ?? "");
  }
  return values;
}

function send(response: ServerResponse, status: number, body: string): void {
  if (response.destroyed || response.writableEnded) return;
  response.writeHead(status, {
    "Cache-Control": "no-store",
    Connection: "close",
    "Content-Length": Buffer.byteLength(body),
    "Content-Type": "application/json; charset=utf-8",
    "X-Content-Type-Options": "nosniff",
  });
  response.end(body);
}

function readBody(request: IncomingMessage, signal: AbortSignal): Promise<unknown> {
  return new Promise((resolve, reject) => {
    let length = 0;
    const chunks: Buffer[] = [];
    const cleanup = () => {
      request.off("data", onData);
      request.off("end", onEnd);
      request.off("error", onError);
      request.off("aborted", onError);
      signal.removeEventListener("abort", onAbort);
    };
    const fail = (error: BrokerError) => {
      cleanup();
      request.resume();
      reject(error);
    };
    const onError = () => fail(new BrokerError("INVALID_REQUEST", 400));
    const onAbort = () => fail(new BrokerError("REQUEST_TIMEOUT", 504));
    const onData = (chunk: Buffer) => {
      length += chunk.length;
      if (length > LOCAL_API_MAX_REQUEST_BYTES) fail(new BrokerError("PAYLOAD_TOO_LARGE", 413));
      else chunks.push(chunk);
    };
    const onEnd = () => {
      cleanup();
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString("utf8")));
      } catch {
        reject(new BrokerError("INVALID_REQUEST", 400));
      }
    };
    request.on("data", onData);
    request.once("end", onEnd);
    request.once("error", onError);
    request.once("aborted", onError);
    signal.addEventListener("abort", onAbort, { once: true });
    if (signal.aborted) onAbort();
  });
}

export async function startLocalApiBroker(options: {
  execute: (request: ValidatedLocalApiRequest) => Promise<unknown>;
}): Promise<LocalApiBroker> {
  const capability = randomBytes(32).toString("base64url");
  const authorization = Buffer.from(`Bearer ${capability}`);
  let host = "";
  let closed = false;

  const processRequest = async (request: IncomingMessage, signal: AbortSignal): Promise<string> => {
    const credentials = headerValues(request, "authorization");
    const supplied = Buffer.from(credentials[0] ?? "");
    if (
      closed ||
      credentials.length !== 1 ||
      supplied.length !== authorization.length ||
      !timingSafeEqual(supplied, authorization)
    ) {
      throw new BrokerError("UNAUTHORIZED", 401);
    }
    if (headerValues(request, "origin").length !== 0)
      throw new BrokerError("ORIGIN_NOT_ALLOWED", 403);
    const hosts = headerValues(request, "host");
    if (hosts.length !== 1 || hosts[0] !== host) throw new BrokerError("INVALID_HOST", 400);
    if (request.url !== "/v1/capabilities" && request.url !== "/v1/read") {
      throw new BrokerError("UNKNOWN_ROUTE", 404);
    }
    if (request.url === "/v1/capabilities") {
      if (request.method !== "GET") throw new BrokerError("METHOD_NOT_ALLOWED", 405);
      if (
        request.headers["transfer-encoding"] !== undefined ||
        Number(request.headers["content-length"] ?? "0") !== 0
      ) {
        throw new BrokerError("INVALID_REQUEST", 400);
      }
      return JSON.stringify({
        version: "v1",
        tools: LOCAL_API_TOOLS,
        resources: LOCAL_API_READ_RESOURCES,
      });
    }
    if (request.method !== "POST") throw new BrokerError("METHOD_NOT_ALLOWED", 405);
    const contentTypes = headerValues(request, "content-type");
    if (
      contentTypes.length !== 1 ||
      !/^application\/json(?:\s*;\s*charset=utf-8)?$/i.test(contentTypes[0] ?? "")
    ) {
      throw new BrokerError("UNSUPPORTED_MEDIA_TYPE", 415);
    }
    if (Number(request.headers["content-length"] ?? "0") > LOCAL_API_MAX_REQUEST_BYTES) {
      throw new BrokerError("PAYLOAD_TOO_LARGE", 413);
    }
    const input = await readBody(request, signal);
    if (signal.aborted) throw new BrokerError("REQUEST_TIMEOUT", 504);
    const validated = validateLocalApiRequest(input);
    if (!validated.ok) throw new BrokerError(validated.error.code, 400);
    let data: unknown;
    try {
      data = await options.execute(validated.value);
    } catch (error) {
      const code =
        error instanceof Error && SAFE_EXECUTION_ERRORS.has(error.message)
          ? error.message
          : "LOCAL_READ_FAILED";
      throw new BrokerError(code, code === "LOCAL_READ_FAILED" ? 502 : 403);
    }
    let body: string;
    try {
      const serializedData = JSON.stringify(data);
      if (serializedData === undefined) throw new Error();
      body = `{"ok":true,"data":${serializedData}}`;
    } catch {
      throw new BrokerError("LOCAL_READ_FAILED", 502);
    }
    if (Buffer.byteLength(body) > LOCAL_API_MAX_RESPONSE_BYTES)
      throw new BrokerError("RESPONSE_TOO_LARGE", 502);
    return body;
  };

  const server = createServer(
    {
      connectionsCheckingInterval: 100,
      headersTimeout: LOCAL_API_DEADLINE_MS,
      maxHeaderSize: LOCAL_API_MAX_REQUEST_BYTES,
      requestTimeout: LOCAL_API_DEADLINE_MS,
    },
    (request, response) => {
      const controller = new AbortController();
      let timer: ReturnType<typeof setTimeout>;
      const deadline = new Promise<never>((_resolve, reject) => {
        timer = setTimeout(() => {
          reject(new BrokerError("REQUEST_TIMEOUT", 504));
          controller.abort();
        }, LOCAL_API_DEADLINE_MS);
      });
      void Promise.race([processRequest(request, controller.signal), deadline])
        .then((body) => send(response, 200, body))
        .catch((error: unknown) => {
          const failure =
            error instanceof BrokerError ? error : new BrokerError("LOCAL_READ_FAILED", 502);
          send(
            response,
            failure.status,
            JSON.stringify({ ok: false, error: { code: failure.code } }),
          );
          request.resume();
        })
        .finally(() => clearTimeout(timer));
    },
  );
  server.headersTimeout = LOCAL_API_DEADLINE_MS;
  server.requestTimeout = LOCAL_API_DEADLINE_MS;
  server.on("clientError", (_error, socket) => {
    socket.end("HTTP/1.1 400 Bad Request\r\nConnection: close\r\nContent-Length: 0\r\n\r\n");
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
  const address = server.address();
  if (address === null || typeof address === "string") {
    server.close();
    throw new Error("LOCAL_BROKER_UNAVAILABLE");
  }
  host = `127.0.0.1:${address.port}`;
  const connection: LocalApiConnection = Object.freeze({
    capability,
    origin: `http://${host}`,
    version: "v1",
  });
  let closePromise: Promise<void> | undefined;
  return {
    connection,
    close() {
      if (closePromise !== undefined) return closePromise;
      closed = true;
      closePromise = new Promise<void>((resolve, reject) => {
        server.close((error) => {
          if (error) reject(new Error("LOCAL_BROKER_UNAVAILABLE"));
          else resolve();
        });
        server.closeAllConnections();
      });
      return closePromise;
    },
  };
}
