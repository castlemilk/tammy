import { X509Certificate } from "node:crypto";

import { z } from "zod";

const MAX_READINESS_BYTES = 65_536;

export type ReadinessErrorCode =
  | "RECORD_TOO_LARGE"
  | "INVALID_FRAMING"
  | "INVALID_ENCODING"
  | "INVALID_JSON"
  | "INVALID_SCHEMA"
  | "INVALID_CA"
  | "INVALID_CAPABILITY";

const ERROR_MESSAGES: Readonly<Record<ReadinessErrorCode, string>> = {
  RECORD_TOO_LARGE: "Readiness record is too large.",
  INVALID_FRAMING: "Invalid readiness framing.",
  INVALID_ENCODING: "Invalid readiness encoding.",
  INVALID_JSON: "Invalid readiness JSON.",
  INVALID_SCHEMA: "Invalid readiness record.",
  INVALID_CA: "Invalid readiness certificate.",
  INVALID_CAPABILITY: "Invalid readiness capability.",
};

export class ReadinessError extends Error {
  public readonly code: ReadinessErrorCode;

  public constructor(code: ReadinessErrorCode) {
    super(ERROR_MESSAGES[code]);
    this.name = "ReadinessError";
    this.code = code;
  }
}

export interface CoreReadiness {
  readonly protocol: "tammy-core-ready-v1";
  readonly port: number;
  readonly caPem: string;
  readonly capability: string;
}

const wireSchema = z
  .object({
    protocol: z.literal("tammy-core-ready-v1"),
    port: z.number().int().min(1).max(65_535),
    ca_pem: z.string(),
    capability: z.string(),
  })
  .strict();

const certificatePemPattern =
  /^-----BEGIN CERTIFICATE-----\n[A-Za-z0-9+/=\n]+\n-----END CERTIFICATE-----$/;

function isCertificatePem(value: string): boolean {
  if (!certificatePemPattern.test(value)) {
    return false;
  }

  try {
    new X509Certificate(value);
    return true;
  } catch {
    return false;
  }
}

function isCanonicalCapability(value: string): boolean {
  try {
    const decoded = Buffer.from(value, "base64url");
    return decoded.byteLength === 32 && decoded.toString("base64url") === value;
  } catch {
    return false;
  }
}

export function parseReadiness(bytes: Uint8Array): Readonly<CoreReadiness> {
  if (bytes.byteLength > MAX_READINESS_BYTES) {
    throw new ReadinessError("RECORD_TOO_LARGE");
  }

  if (
    bytes.byteLength < 2 ||
    bytes[bytes.byteLength - 1] !== 0x0a ||
    bytes.subarray(0, bytes.byteLength - 1).includes(0x0a)
  ) {
    throw new ReadinessError("INVALID_FRAMING");
  }

  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(
      bytes.subarray(0, bytes.byteLength - 1),
    );
  } catch {
    throw new ReadinessError("INVALID_ENCODING");
  }

  let decoded: unknown;
  try {
    decoded = JSON.parse(text);
  } catch {
    throw new ReadinessError("INVALID_JSON");
  }

  const parsed = wireSchema.safeParse(decoded);
  if (!parsed.success) {
    throw new ReadinessError("INVALID_SCHEMA");
  }

  if (!isCertificatePem(parsed.data.ca_pem)) {
    throw new ReadinessError("INVALID_CA");
  }

  if (!isCanonicalCapability(parsed.data.capability)) {
    throw new ReadinessError("INVALID_CAPABILITY");
  }

  return Object.freeze({
    protocol: parsed.data.protocol,
    port: parsed.data.port,
    caPem: parsed.data.ca_pem,
    capability: parsed.data.capability,
  });
}
