import { create } from "@bufbuild/protobuf";
import { AnySchema } from "@bufbuild/protobuf/wkt";
import {
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  GetDiagnosticsRequestSchema,
  GetDiagnosticsResponseSchema,
} from "@tammy/connect-client/tammy/v1/system_pb.js";
import { describe, expect, it } from "vitest";

import { createProtoMethodCodec, ProtoIpcError } from "./proto-ipc";

const codec = createProtoMethodCodec({
  input: GetDiagnosticsRequestSchema,
  maximumRequestBytes: 64,
  maximumResponseBytes: 256,
  output: GetDiagnosticsResponseSchema,
});

describe("binary protobuf IPC codec", () => {
  it("round trips only the generated request and response messages", () => {
    const request = create(GetDiagnosticsRequestSchema);
    const requestFrame = codec.encodeRequest(request);
    expect(codec.decodeRequest(requestFrame)).toEqual(request);

    const response = create(GetDiagnosticsResponseSchema, {
      apiVersion: "tammy.v1",
      coreVersion: "test",
      networkRequired: false,
    });
    const responseFrame = codec.encodeResponse(response);
    expect(codec.decodeResponse(responseFrame)).toEqual(response);
  });

  it("rejects another generated message type even when its payload is valid", () => {
    const overview = createProtoMethodCodec({
      input: GetAttentionSummaryRequestSchema,
      maximumRequestBytes: 512,
      maximumResponseBytes: 512,
      output: GetAttentionSummaryResponseSchema,
    });
    const wrongFrame = overview.encodeRequest(create(GetAttentionSummaryRequestSchema));

    expect(() => codec.decodeRequest(wrongFrame)).toThrowError(
      expect.objectContaining<Partial<ProtoIpcError>>({ code: "PROTO_FRAME_TYPE_MISMATCH" }),
    );
  });

  it("rejects oversized, malformed, non-canonical, and unknown-field frames", () => {
    const valid = codec.encodeRequest(create(GetDiagnosticsRequestSchema));
    const oversized = new Uint8Array(65);
    const malformed = Uint8Array.of(0xff);
    const unknownOuter = new Uint8Array([...valid, 0x18, 0x01]);
    const nonCanonical = new Uint8Array([
      ...codec.encodeRequest(create(GetDiagnosticsRequestSchema)),
    ]);
    // Duplicate type_url makes the standard message semantically decodable but non-canonical.
    const any = create(AnySchema, {
      typeUrl: "type.googleapis.com/tammy.v1.GetDiagnosticsRequest",
      value: new Uint8Array(),
    });
    const duplicateType = new Uint8Array([
      0x0a,
      any.typeUrl.length,
      ...new TextEncoder().encode(any.typeUrl),
      ...nonCanonical,
    ]);

    for (const frame of [oversized, malformed, unknownOuter, duplicateType]) {
      expect(() => codec.decodeRequest(frame)).toThrowError(ProtoIpcError);
    }
  });

  it("copies frame inputs and outputs across ownership boundaries", () => {
    const encoded = codec.encodeRequest(create(GetDiagnosticsRequestSchema));
    const callerCopy = new Uint8Array(encoded);
    const decoded = codec.decodeRequest(callerCopy);
    callerCopy.fill(0xff);

    expect(decoded).toEqual(create(GetDiagnosticsRequestSchema));
    expect(encoded).not.toEqual(callerCopy);
  });
});
