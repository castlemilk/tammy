// @vitest-environment node
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  GetDiagnosticsRequestSchema,
  GetDiagnosticsResponseSchema,
} from "@tammy/connect-client/tammy/v1/system_pb.js";
import { ConveyorError } from "electron-conveyor/define";
import { describe, expect, it } from "vitest";
import { createProtoMethodCodec } from "../shared/proto-ipc";
import { basPreparationCoreCorrection, invokeBasPreparation } from "./bas-preparation-proto";
import { sanitizeHandlerError } from "./error";

const codec = createProtoMethodCodec({
  input: GetDiagnosticsRequestSchema,
  maximumRequestBytes: 128,
  maximumResponseBytes: 512,
  output: GetDiagnosticsResponseSchema,
});
const frame = codec.encodeRequest(create(GetDiagnosticsRequestSchema));

const mappedStatuses = [
  [Code.ResourceExhausted, "BAS_SOURCE_CAPACITY"],
  [Code.FailedPrecondition, "BAS_CONFLICT"],
  [Code.AlreadyExists, "BAS_IDEMPOTENCY_CONFLICT"],
  [Code.PermissionDenied, "BAS_PERMISSION_DENIED"],
  [Code.Unauthenticated, "BAS_AUTHENTICATION_REQUIRED"],
  [Code.DeadlineExceeded, "BAS_REQUEST_TIMEOUT"],
] as const;

describe("BAS preparation Core error boundary", () => {
  it.each(mappedStatuses)("maps Connect status %s to a static code", (status, code) => {
    const error = new ConnectError(
      "/private/workspace password=secret",
      status,
      { authorization: "Bearer private-token" },
      [],
    );

    expect(basPreparationCoreCorrection(error)).toMatchObject({ code, message: code });
  });

  it("does not trust spoofed, malformed, or unrelated errors", () => {
    const spoofed = Object.assign(new Error("private"), {
      name: "ConnectError",
      code: Code.ResourceExhausted,
      metadata: new Headers(),
      details: [],
      rawMessage: "private",
      cause: undefined,
    });
    const malformed = Object.create(ConnectError.prototype) as ConnectError;
    Object.assign(malformed, { code: Code.AlreadyExists });

    expect(spoofed instanceof ConnectError).toBe(true);
    for (const error of [
      spoofed,
      malformed,
      { code: Code.ResourceExhausted, message: "BAS_SOURCE_CAPACITY" },
      new Error("BAS_IDEMPOTENCY_CONFLICT"),
      new ConnectError("private", Code.InvalidArgument),
      new ConnectError("private", Code.Internal),
    ])
      expect(basPreparationCoreCorrection(error)).toBeUndefined();
  });

  it("propagates only the safe code and keeps Connect details private", async () => {
    const sourceError = new ConnectError(
      "/private/db/path password=hunter2",
      Code.AlreadyExists,
      { authorization: "Bearer secret-token" },
      [],
    );
    let rejected: unknown;
    try {
      await invokeBasPreparation(codec, frame, async () => {
        throw sourceError;
      });
    } catch (error) {
      rejected = error;
    }

    expect(rejected).toBeInstanceOf(ConveyorError);
    expect(rejected).toMatchObject({
      code: "BAS_IDEMPOTENCY_CONFLICT",
      message: "BAS_IDEMPOTENCY_CONFLICT",
    });
    const sanitized = sanitizeHandlerError(rejected);
    expect(sanitized).toMatchObject({
      code: "BAS_IDEMPOTENCY_CONFLICT",
      message: "BAS_IDEMPOTENCY_CONFLICT",
    });
    expect(JSON.stringify(sanitized)).not.toMatch(/private|hunter2|secret-token|metadata/i);
  });

  it("keeps malformed and unmapped failures generic without leaking their messages", async () => {
    const spoofed = Object.assign(new Error("/private password=secret"), {
      name: "ConnectError",
      code: Code.ResourceExhausted,
      metadata: new Headers(),
      details: [],
      rawMessage: "/private password=secret",
      cause: undefined,
    });
    for (const source of [spoofed, new ConnectError("/private password=secret", Code.Internal)]) {
      let rejected: unknown;
      try {
        await invokeBasPreparation(codec, frame, async () => {
          throw source;
        });
      } catch (error) {
        rejected = error;
      }
      expect(rejected).toMatchObject({
        code: "CORE_REQUEST_FAILED",
        message: "CORE_REQUEST_FAILED",
      });
      expect(JSON.stringify(sanitizeHandlerError(rejected))).not.toMatch(
        /private|password|secret/i,
      );
    }
  });
});
