import { request } from "node:http";

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
  CallToolRequestSchema,
  ErrorCode,
  ListResourcesRequestSchema,
  ListToolsRequestSchema,
  McpError,
  ReadResourceRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { INVOICE_DRAFT_JSON_SCHEMA } from "@tammy/invoices/model";
import {
  LOCAL_API_DEADLINE_MS,
  LOCAL_API_MAX_REQUEST_BYTES,
  LOCAL_API_MAX_RESPONSE_BYTES,
} from "./broker.ts";
import {
  INVOICE_PROPOSAL_TOOL,
  LOCAL_API_READ_RESOURCES,
  LOCAL_API_READ_TOOLS,
} from "./catalog.ts";
import { type LocalApiConnection, validateLocalApiConnection } from "./connection.ts";
import { validateLocalApiRequest } from "./request.ts";

const SAFE_BROKER_ERRORS = new Set([
  "INVALID_ARGUMENTS",
  "INVALID_HOST",
  "INVALID_REQUEST",
  "LOCAL_PAIRING_REVOKED",
  "LOCAL_READ_FAILED",
  "LOCAL_SESSION_REQUIRED",
  "METHOD_NOT_ALLOWED",
  "ORIGIN_NOT_ALLOWED",
  "PAYLOAD_TOO_LARGE",
  "REQUEST_TIMEOUT",
  "RESPONSE_TOO_LARGE",
  "UNAUTHORIZED",
  "UNKNOWN_OPERATION",
  "UNKNOWN_RESOURCE",
  "UNKNOWN_ROUTE",
  "UNKNOWN_TOOL",
  "UNSUPPORTED_MEDIA_TYPE",
  "UNSUPPORTED_VERSION",
]);

class RelayError extends Error {
  readonly code: string;
  constructor(code: string) {
    super(code);
    this.code = code;
  }
}

function errorCode(error: unknown): string {
  return error instanceof RelayError ? error.code : "LOCAL_BROKER_UNAVAILABLE";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

async function brokerRequest(
  connection: LocalApiConnection,
  path: "/v1/capabilities" | "/v1/read",
  input?: unknown,
): Promise<unknown> {
  const url = new URL(connection.origin);
  const body = input === undefined ? "" : JSON.stringify(input);
  if (Buffer.byteLength(body) > LOCAL_API_MAX_REQUEST_BYTES)
    throw new RelayError("PAYLOAD_TOO_LARGE");
  return new Promise((resolve, reject) => {
    let settled = false;
    const fail = (code: string) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      reject(new RelayError(code));
      outgoing.destroy();
    };
    // No fetch redirects, proxy environment handling, URL overrides, or caller-supplied headers.
    const outgoing = request(
      {
        agent: false,
        headers: {
          Authorization: `Bearer ${connection.capability}`,
          "Content-Length": Buffer.byteLength(body),
          "Content-Type": "application/json",
          Host: url.host,
        },
        hostname: "127.0.0.1",
        maxHeaderSize: LOCAL_API_MAX_REQUEST_BYTES,
        method: path === "/v1/capabilities" ? "GET" : "POST",
        path,
        port: url.port,
      },
      (response) => {
        if (
          response.statusCode === undefined ||
          response.statusCode < 200 ||
          response.statusCode >= 600 ||
          (response.statusCode >= 300 && response.statusCode < 400) ||
          !/^application\/json(?:\s*;\s*charset=utf-8)?$/i.test(
            response.headers["content-type"] ?? "",
          ) ||
          Number(response.headers["content-length"] ?? "0") > LOCAL_API_MAX_RESPONSE_BYTES
        ) {
          response.resume();
          fail("LOCAL_BROKER_INVALID_RESPONSE");
          return;
        }
        let length = 0;
        const chunks: Buffer[] = [];
        response.on("data", (chunk: Buffer) => {
          length += chunk.length;
          if (length > LOCAL_API_MAX_RESPONSE_BYTES) fail("LOCAL_BROKER_INVALID_RESPONSE");
          else chunks.push(chunk);
        });
        response.on("error", () => fail("LOCAL_BROKER_UNAVAILABLE"));
        response.on("aborted", () => fail("LOCAL_BROKER_UNAVAILABLE"));
        response.on("end", () => {
          if (settled) return;
          let value: unknown;
          try {
            value = JSON.parse(Buffer.concat(chunks).toString("utf8"));
          } catch {
            fail("LOCAL_BROKER_INVALID_RESPONSE");
            return;
          }
          if (
            isRecord(value) &&
            value.ok === false &&
            isRecord(value.error) &&
            typeof value.error.code === "string" &&
            SAFE_BROKER_ERRORS.has(value.error.code)
          ) {
            fail(value.error.code);
            return;
          }
          if (response.statusCode !== 200 || !isRecord(value)) {
            fail("LOCAL_BROKER_INVALID_RESPONSE");
            return;
          }
          if (path === "/v1/read") {
            if (value.ok !== true || !Object.hasOwn(value, "data")) {
              fail("LOCAL_BROKER_INVALID_RESPONSE");
              return;
            }
            value = value.data;
          } else if (
            value.version !== "v1" ||
            !Array.isArray(value.tools) ||
            !Array.isArray(value.resources)
          ) {
            fail("LOCAL_BROKER_INVALID_RESPONSE");
            return;
          }
          settled = true;
          clearTimeout(timer);
          resolve(value);
        });
      },
    );
    const timer = setTimeout(() => fail("REQUEST_TIMEOUT"), LOCAL_API_DEADLINE_MS);
    outgoing.on("error", () => fail("LOCAL_BROKER_UNAVAILABLE"));
    outgoing.end(body);
  });
}

export function createLocalApiMcpServer(value: LocalApiConnection): Server {
  const connection = validateLocalApiConnection(value);
  const server = new Server(
    { name: "tammy-local", version: "0.0.0" },
    {
      capabilities: { resources: {}, tools: {} },
      instructions:
        "Read the paired, unlocked Tammy desktop's current organisation and propose editable invoice drafts. The user must review and save proposals in Tammy. No invoice save, issue, ledger posting, sending or credential access is available.",
    },
  );
  server.setRequestHandler(ListToolsRequestSchema, async () => {
    try {
      await brokerRequest(connection, "/v1/capabilities");
    } catch (error) {
      throw new McpError(ErrorCode.InternalError, errorCode(error));
    }
    return {
      tools: [
        ...LOCAL_API_READ_TOOLS.map((tool) => ({
          annotations: {
            destructiveHint: false,
            idempotentHint: true,
            openWorldHint: false,
            readOnlyHint: true,
          },
          description: tool.description,
          inputSchema: { additionalProperties: false, properties: {}, type: "object" as const },
          name: tool.name,
        })),
        {
          name: INVOICE_PROPOSAL_TOOL.name,
          description: INVOICE_PROPOSAL_TOOL.description,
          inputSchema: INVOICE_DRAFT_JSON_SCHEMA,
          annotations: {
            destructiveHint: false,
            idempotentHint: true,
            openWorldHint: false,
            readOnlyHint: false,
          },
        },
      ],
    };
  });
  server.setRequestHandler(ListResourcesRequestSchema, async () => {
    try {
      await brokerRequest(connection, "/v1/capabilities");
    } catch (error) {
      throw new McpError(ErrorCode.InternalError, errorCode(error));
    }
    return {
      resources: LOCAL_API_READ_RESOURCES.map(({ description, mimeType, name, uri }) => ({
        description,
        mimeType,
        name,
        uri,
      })),
    };
  });
  server.setRequestHandler(CallToolRequestSchema, async (request) => {
    const envelope = {
      arguments: request.params.arguments ?? {},
      name: request.params.name,
      operation: "tool.call",
      version: "v1",
    };
    const validated = validateLocalApiRequest(envelope);
    try {
      if (!validated.ok) throw new RelayError(validated.error.code);
      const data = await brokerRequest(connection, "/v1/read", envelope);
      return { content: [{ type: "text", text: JSON.stringify(data) }], isError: false };
    } catch (error) {
      return {
        content: [
          { type: "text", text: JSON.stringify({ ok: false, error: { code: errorCode(error) } }) },
        ],
        isError: true,
      };
    }
  });
  server.setRequestHandler(ReadResourceRequestSchema, async (request) => {
    const envelope = { operation: "resource.read", uri: request.params.uri, version: "v1" };
    const validated = validateLocalApiRequest(envelope);
    if (!validated.ok) throw new McpError(ErrorCode.InvalidParams, validated.error.code);
    try {
      const data = await brokerRequest(connection, "/v1/read", envelope);
      return {
        contents: [
          { mimeType: "application/json", text: JSON.stringify(data), uri: request.params.uri },
        ],
      };
    } catch (error) {
      throw new McpError(ErrorCode.InternalError, errorCode(error));
    }
  });
  return server;
}
