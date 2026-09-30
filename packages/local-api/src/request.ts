import {
  LOCAL_API_READ_RESOURCES,
  LOCAL_API_READ_TOOLS,
  type LocalApiReadResource,
  type LocalApiReadTool,
} from "./catalog.ts";

export const LOCAL_API_VERSION = "v1" as const;

export type LocalApiValidationErrorCode =
  | "INVALID_ARGUMENTS"
  | "INVALID_REQUEST"
  | "UNKNOWN_OPERATION"
  | "UNKNOWN_RESOURCE"
  | "UNKNOWN_TOOL"
  | "UNSUPPORTED_VERSION";

export interface ValidatedLocalApiToolCall {
  readonly arguments: Readonly<Record<string, never>>;
  readonly operation: "tool.call";
  readonly tool: LocalApiReadTool;
  readonly version: typeof LOCAL_API_VERSION;
}

export interface ValidatedLocalApiResourceRead {
  readonly operation: "resource.read";
  readonly resource: LocalApiReadResource;
  readonly version: typeof LOCAL_API_VERSION;
}

export type ValidatedLocalApiRequest = ValidatedLocalApiResourceRead | ValidatedLocalApiToolCall;

export type LocalApiValidationResult =
  | Readonly<{ ok: true; value: ValidatedLocalApiRequest }>
  | Readonly<{ error: Readonly<{ code: LocalApiValidationErrorCode }>; ok: false }>;

const EMPTY_ARGUMENTS = Object.freeze({}) as Readonly<Record<string, never>>;
const TOOL_KEYS = new Set<PropertyKey>(["arguments", "name", "operation", "version"]);
const RESOURCE_KEYS = new Set<PropertyKey>(["operation", "uri", "version"]);
const toolsByName = new Map<string, LocalApiReadTool>(
  LOCAL_API_READ_TOOLS.map((tool) => [tool.name, tool]),
);
const resourcesByUri = new Map<string, LocalApiReadResource>(
  LOCAL_API_READ_RESOURCES.map((resource) => [resource.uri, resource]),
);

function failure(code: LocalApiValidationErrorCode): LocalApiValidationResult {
  return Object.freeze({ error: Object.freeze({ code }), ok: false });
}

function isPlainRecord(value: unknown): value is Record<PropertyKey, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const prototype = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
}

function hasOnlyKeys(
  value: Record<PropertyKey, unknown>,
  allowed: ReadonlySet<PropertyKey>,
): boolean {
  return Reflect.ownKeys(value).every((key) => allowed.has(key));
}

function validEmptyArguments(value: unknown): boolean {
  return isPlainRecord(value) && Reflect.ownKeys(value).length === 0;
}

export function validateLocalApiRequest(input: unknown): LocalApiValidationResult {
  if (!isPlainRecord(input)) return failure("INVALID_REQUEST");
  if (typeof input.version !== "string") return failure("INVALID_REQUEST");
  if (input.version !== LOCAL_API_VERSION) return failure("UNSUPPORTED_VERSION");
  if (typeof input.operation !== "string") return failure("INVALID_REQUEST");

  if (input.operation === "tool.call") {
    if (!hasOnlyKeys(input, TOOL_KEYS) || typeof input.name !== "string") {
      return failure("INVALID_REQUEST");
    }
    const tool = toolsByName.get(input.name);
    if (tool === undefined) return failure("UNKNOWN_TOOL");
    if (Object.hasOwn(input, "arguments") && !validEmptyArguments(input.arguments)) {
      return failure("INVALID_ARGUMENTS");
    }
    return Object.freeze({
      ok: true,
      value: Object.freeze({
        arguments: EMPTY_ARGUMENTS,
        operation: "tool.call",
        tool,
        version: LOCAL_API_VERSION,
      }),
    });
  }

  if (input.operation === "resource.read") {
    if (!hasOnlyKeys(input, RESOURCE_KEYS) || typeof input.uri !== "string") {
      return failure("INVALID_REQUEST");
    }
    const resource = resourcesByUri.get(input.uri);
    if (resource === undefined) return failure("UNKNOWN_RESOURCE");
    return Object.freeze({
      ok: true,
      value: Object.freeze({
        operation: "resource.read",
        resource,
        version: LOCAL_API_VERSION,
      }),
    });
  }

  return failure("UNKNOWN_OPERATION");
}
