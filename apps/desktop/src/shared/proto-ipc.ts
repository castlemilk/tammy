import {
  create,
  type DescMessage,
  fromBinary,
  type MessageShape,
  toBinary,
} from "@bufbuild/protobuf";
import { AnySchema } from "@bufbuild/protobuf/wkt";

export type ProtoIpcErrorCode =
  | "PROTO_FRAME_INVALID"
  | "PROTO_FRAME_TOO_LARGE"
  | "PROTO_FRAME_TYPE_MISMATCH";

export class ProtoIpcError extends Error {
  public constructor(public readonly code: ProtoIpcErrorCode) {
    super(code);
    this.name = "ProtoIpcError";
  }
}

export interface ProtoMethodCodec<Input extends DescMessage, Output extends DescMessage> {
  decodeRequest(frame: Uint8Array): MessageShape<Input>;
  decodeResponse(frame: Uint8Array): MessageShape<Output>;
  encodeRequest(message: MessageShape<Input>): Uint8Array;
  encodeResponse(message: MessageShape<Output>): Uint8Array;
}

interface ProtoMethodCodecConfig<Input extends DescMessage, Output extends DescMessage> {
  readonly input: Input;
  readonly maximumRequestBytes: number;
  readonly maximumResponseBytes: number;
  readonly output: Output;
}

function exactBytes(left: Uint8Array, right: Uint8Array): boolean {
  if (left.byteLength !== right.byteLength) return false;
  let difference = 0;
  for (let index = 0; index < left.byteLength; index += 1) {
    difference |= (left[index] ?? 0) ^ (right[index] ?? 0);
  }
  return difference === 0;
}

function checkedLimit(limit: number): number {
  if (!Number.isSafeInteger(limit) || limit < 1) throw new ProtoIpcError("PROTO_FRAME_INVALID");
  return limit;
}

function encodeFrame<Schema extends DescMessage>(
  schema: Schema,
  message: MessageShape<Schema>,
  maximumBytes: number,
): Uint8Array {
  try {
    const frame = toBinary(
      AnySchema,
      create(AnySchema, {
        typeUrl: `type.googleapis.com/${schema.typeName}`,
        value: toBinary(schema, message, { writeUnknownFields: false }),
      }),
      { writeUnknownFields: false },
    );
    if (frame.byteLength > maximumBytes) throw new ProtoIpcError("PROTO_FRAME_TOO_LARGE");
    return new Uint8Array(frame);
  } catch (error) {
    if (error instanceof ProtoIpcError) throw error;
    throw new ProtoIpcError("PROTO_FRAME_INVALID");
  }
}

function decodeFrame<Schema extends DescMessage>(
  schema: Schema,
  input: Uint8Array,
  maximumBytes: number,
): MessageShape<Schema> {
  if (!(input instanceof Uint8Array) || input.byteLength === 0) {
    throw new ProtoIpcError("PROTO_FRAME_INVALID");
  }
  if (input.byteLength > maximumBytes) throw new ProtoIpcError("PROTO_FRAME_TOO_LARGE");
  const frame = new Uint8Array(input);

  try {
    const envelope = fromBinary(AnySchema, frame, { readUnknownFields: true });
    const canonicalEnvelope = toBinary(AnySchema, envelope, { writeUnknownFields: false });
    if (!exactBytes(frame, canonicalEnvelope)) throw new ProtoIpcError("PROTO_FRAME_INVALID");
    if (envelope.typeUrl !== `type.googleapis.com/${schema.typeName}`) {
      throw new ProtoIpcError("PROTO_FRAME_TYPE_MISMATCH");
    }
    const message = fromBinary(schema, new Uint8Array(envelope.value), { readUnknownFields: true });
    const canonicalMessage = toBinary(schema, message, { writeUnknownFields: false });
    if (!exactBytes(envelope.value, canonicalMessage))
      throw new ProtoIpcError("PROTO_FRAME_INVALID");
    return message;
  } catch (error) {
    if (error instanceof ProtoIpcError) throw error;
    throw new ProtoIpcError("PROTO_FRAME_INVALID");
  }
}

export function createProtoMethodCodec<Input extends DescMessage, Output extends DescMessage>(
  config: ProtoMethodCodecConfig<Input, Output>,
): Readonly<ProtoMethodCodec<Input, Output>> {
  const maximumRequestBytes = checkedLimit(config.maximumRequestBytes);
  const maximumResponseBytes = checkedLimit(config.maximumResponseBytes);
  return Object.freeze({
    decodeRequest: (frame: Uint8Array) => decodeFrame(config.input, frame, maximumRequestBytes),
    decodeResponse: (frame: Uint8Array) => decodeFrame(config.output, frame, maximumResponseBytes),
    encodeRequest: (message: MessageShape<Input>) =>
      encodeFrame(config.input, message, maximumRequestBytes),
    encodeResponse: (message: MessageShape<Output>) =>
      encodeFrame(config.output, message, maximumResponseBytes),
  });
}
