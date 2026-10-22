import { Code, ConnectError } from "@connectrpc/connect";
import {
  BasCoreCorrection,
  type BasPreparationCorrectionCode,
} from "../shared/bas-preparation-errors";
import { invokeCore, type ProtoCodec } from "./proto";

const statusToCorrection: Readonly<Partial<Record<Code, BasPreparationCorrectionCode>>> = {
  [Code.ResourceExhausted]: "BAS_SOURCE_CAPACITY",
  [Code.FailedPrecondition]: "BAS_CONFLICT",
  [Code.AlreadyExists]: "BAS_IDEMPOTENCY_CONFLICT",
  [Code.PermissionDenied]: "BAS_PERMISSION_DENIED",
  [Code.Unauthenticated]: "BAS_AUTHENTICATION_REQUIRED",
  [Code.DeadlineExceeded]: "BAS_REQUEST_TIMEOUT",
};

function ownDataValue(value: object, name: string): unknown {
  const descriptor = Object.getOwnPropertyDescriptor(value, name);
  return descriptor && "value" in descriptor ? descriptor.value : undefined;
}

function actualConnectCode(error: unknown): Code | undefined {
  try {
    if (
      (typeof error !== "object" && typeof error !== "function") ||
      error === null ||
      Object.getPrototypeOf(error) !== ConnectError.prototype
    )
      return undefined;

    // ConnectError has a custom Symbol.hasInstance that accepts structurally
    // compatible objects. Require its actual base prototype and own fields so
    // a plain object that spoofs `name` and `code` cannot cross this boundary.
    const name = ownDataValue(error, "name");
    const code = ownDataValue(error, "code");
    const rawMessage = ownDataValue(error, "rawMessage");
    const metadata = ownDataValue(error, "metadata");
    const details = ownDataValue(error, "details");
    if (
      name !== "ConnectError" ||
      typeof code !== "number" ||
      !Number.isInteger(code) ||
      typeof rawMessage !== "string" ||
      !(metadata instanceof Headers) ||
      !Array.isArray(details) ||
      !Object.hasOwn(error, "cause")
    )
      return undefined;

    return code as Code;
  } catch {
    return undefined;
  }
}

export function basPreparationCoreCorrection(error: unknown): BasCoreCorrection | undefined {
  const status = actualConnectCode(error);
  const code = status === undefined ? undefined : statusToCorrection[status];
  return code === undefined ? undefined : new BasCoreCorrection(code);
}

export function invokeBasPreparation<Request, Response>(
  codec: ProtoCodec<Request, Response>,
  frame: Uint8Array,
  call: (request: Request) => Promise<Response>,
): Promise<Uint8Array> {
  return invokeCore(codec, frame, async (request) => {
    try {
      return await call(request);
    } catch (error) {
      throw basPreparationCoreCorrection(error) ?? error;
    }
  });
}
