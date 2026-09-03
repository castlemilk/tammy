import { create } from "@bufbuild/protobuf";
import {
  GetDiagnosticsRequestSchema,
  GetDiagnosticsResponseSchema,
} from "@tammy/connect-client/tammy/v1/system_pb.js";
import { ConveyorError } from "electron-conveyor/define";
import { describe, expect, it } from "vitest";

import { createProtoMethodCodec } from "../shared/proto-ipc";
import { sanitizeHandlerError } from "./error";
import { boundedProtoFrame, invokeCore } from "./proto";

const codec = createProtoMethodCodec({
  input: GetDiagnosticsRequestSchema,
  maximumRequestBytes: 64,
  maximumResponseBytes: 512,
  output: GetDiagnosticsResponseSchema,
});

function frame(): Uint8Array {
  return codec.encodeRequest(create(GetDiagnosticsRequestSchema));
}

function hasCode(error: unknown, code: string): boolean {
  expect(error).toBeInstanceOf(ConveyorError);
  expect(error).toMatchObject({ code, message: code });
  return true;
}

describe("boundedProtoFrame", () => {
  it("accepts exactly one non-empty bounded Uint8Array and copies it", () => {
    const input = new Uint8Array([1, 2, 3]);
    const result = boundedProtoFrame(3).parse(input);

    input[0] = 9;
    expect(result).toEqual(new Uint8Array([1, 2, 3]));
    expect(result).not.toBe(input);
  });

  it("rejects non-canonical, empty, detached, oversized, and invalid binary values", () => {
    class Uint8ArraySubclass extends Uint8Array {}
    const detached = new Uint8Array([1]);
    structuredClone(detached, { transfer: [detached.buffer] });
    const schema = boundedProtoFrame(2);

    for (const value of [
      new Uint8ArraySubclass([1]),
      [1],
      new DataView(new ArrayBuffer(1)),
      detached,
      new Uint8Array(),
      new Uint8Array([1, 2, 3]),
    ]) {
      const result = schema.safeParse(value);
      expect(result.success).toBe(false);
      if (!result.success) expect(result.error.issues).toEqual([{ code: "custom", message: "Invalid binary frame", path: [] }]);
    }
  });
});

describe("invokeCore", () => {
  it("decodes a copied accepted frame and returns a copied encoded response", async () => {
    const request = frame();
    const response = codec.encodeResponse(
      create(GetDiagnosticsResponseSchema, {
        apiVersion: "tammy.v1",
        coreVersion: "0.1.0",
        networkRequired: false,
      }),
    );
    let received: Uint8Array | undefined;

    const result = await invokeCore(codec, request, async (decoded) => {
      received = codec.encodeRequest(decoded);
      return codec.decodeResponse(response);
    });
    request.fill(0);
    response.fill(0);

    expect(received).toEqual(frame());
    expect(result).not.toBe(response);
    expect(codec.decodeResponse(result).apiVersion).toBe("tammy.v1");
  });

  it("maps canonical request failures to INVALID_RPC_REQUEST", async () => {
    await expect(invokeCore(codec, new Uint8Array([1]), async () => create(GetDiagnosticsResponseSchema))).rejects.toSatisfy(
      (error: unknown) => hasCode(error, "INVALID_RPC_REQUEST"),
    );
  });

  it("maps Core and response encoding failures to CORE_REQUEST_FAILED without leaking details", async () => {
    const secret = "/private/credential.p12 password=secret subprocess output";
    await expect(
      invokeCore(codec, frame(), async () => {
        throw new Error(secret);
      }),
    ).rejects.toSatisfy((error: unknown) => hasCode(error, "CORE_REQUEST_FAILED"));
    await expect(
      invokeCore(
        { ...codec, encodeResponse: () => { throw new Error(secret); } },
        frame(),
        async () => create(GetDiagnosticsResponseSchema),
      ),
    ).rejects.toSatisfy((error: unknown) => hasCode(error, "CORE_REQUEST_FAILED"));
  });
});

it("sanitizes every handler failure without serializing sensitive details", () => {
  const dangerous = [
    new Error("/Users/alice/private.p12 password=hunter2 stack trace"),
    { path: "/tmp/private", output: "subprocess", bookmark: "bookmark", handle: "handle" },
    "unknown thrown value",
    null,
  ];

  for (const value of dangerous) {
    const error = sanitizeHandlerError(value);
    expect(error).toMatchObject({ code: "CORE_REQUEST_FAILED", message: "CORE_REQUEST_FAILED" });
    expect(JSON.stringify(error)).not.toMatch(/private|password|subprocess|bookmark|handle|unknown/i);
  }
});
