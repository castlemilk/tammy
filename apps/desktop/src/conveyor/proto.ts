import { ConveyorError, type StandardSchemaV1 } from "electron-conveyor/define";
import { z } from "zod";

interface ProtoCodec<Request, Response> {
  decodeRequest(frame: Uint8Array): Request;
  encodeResponse(response: Response): Uint8Array;
}

function copyFrame(value: unknown, maximumBytes: number): Uint8Array | undefined {
  if (
    !Number.isSafeInteger(maximumBytes) ||
    maximumBytes < 1 ||
    !(value instanceof Uint8Array) ||
    Object.getPrototypeOf(value) !== Uint8Array.prototype
  )
    return undefined;
  try {
    if (value.byteLength === 0 || value.byteLength > maximumBytes || value.byteOffset < 0)
      return undefined;
    return new Uint8Array(value);
  } catch {
    return undefined;
  }
}

export function boundedProtoFrame(maximumBytes: number) {
  return z
    .custom<Uint8Array>((value) => copyFrame(value, maximumBytes) !== undefined, {
      message: "Invalid binary frame",
    })
    .transform((value) => copyFrame(value, maximumBytes)!);
}

export function conveyorProtoFrame(maximumBytes: number): StandardSchemaV1<Uint8Array, Uint8Array> {
  return boundedProtoFrame(maximumBytes) as unknown as StandardSchemaV1<Uint8Array, Uint8Array>;
}

export async function invokeCore<Request, Response>(
  codec: ProtoCodec<Request, Response>,
  frame: Uint8Array,
  call: (request: Request) => Promise<Response>,
): Promise<Uint8Array> {
  let request: Request;
  try {
    const copiedFrame = copyFrame(frame, Number.MAX_SAFE_INTEGER);
    if (copiedFrame === undefined) throw new Error("INVALID_FRAME");
    request = codec.decodeRequest(copiedFrame);
  } catch {
    throw new ConveyorError("INVALID_RPC_REQUEST", "INVALID_RPC_REQUEST");
  }
  try {
    const encoded = codec.encodeResponse(await call(request));
    const copiedResponse = copyFrame(encoded, Number.MAX_SAFE_INTEGER);
    if (copiedResponse === undefined) throw new Error("INVALID_RESPONSE");
    return copiedResponse;
  } catch {
    throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
  }
}
