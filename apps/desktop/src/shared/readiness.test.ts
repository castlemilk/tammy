import { describe, expect, it } from "vitest";

import { parseReadiness, ReadinessError } from "./readiness";

const CERTIFICATE = `-----BEGIN CERTIFICATE-----
MIIDCTCCAfGgAwIBAgIUfyv/Dzl4yZ1jFPe6zuXvGRYDFlowDQYJKoZIhvcNAQEL
BQAwFDESMBAGA1UEAwwJbG9jYWxob3N0MB4XDTI2MDcxOTE0MzcxOFoXDTI2MDcy
MDE0MzcxOFowFDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEApx6O1eD2I7HXr/Qp6yO/90Zwk1OCGL3WaclJte+BZg+a
Ae4BLndHGc575pd96ntzWdk8rGpWjXQ/r/fYvLB9QAHW3xYP78QQcewGEXOaLIe7
sGDWATxxVaxO/3KRfjjuc6iAMt5erSENEafG8yNLuIweZuTa46VGQgWoI9C3PmG3
AB56sYA2ZPL2gW/QUcp6pcIi6TYFMqVffNprTaS8qhKUwiHeVrUR0gJYeaMv9dbZ
tTw7k3WUwK9+Xmyh6D1vN1YIpoaqcLge0/4tTXmoWcGPIRW+h6XwW84Qc0vELkTt
DEp4OVS46Wd24JnLR9/m/qEfMX08JpSknCHYKr582QIDAQABo1MwUTAdBgNVHQ4E
FgQUtQpIJKU696beoddFIu73TlTBC0UwHwYDVR0jBBgwFoAUtQpIJKU696beoddF
Iu73TlTBC0UwDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEABkh+
c1JTbRZzx+9vJZkLG3IqjE1na8+zgEcLt9AdVwPxfarpJAaiRruscZ3Sbyt8Yd57
cPE73Zf0fmDBg7gDzajkcfgLjwXNAeuZJs05Fdwl2WDSZIGwxIXCyjJ0w10Sz5jA
8IdN415Nvc0+WVNuEmS6VpeosQjq1JQlpq4h5BH37WgHeGdbip3m0hrP/+UVKW0s
+ZDK5DTeBRhMJ56u7r4JYy6abqAOlLQ6lry08pthjz20MhtC2oQU49Y5WL6j394S
LDDUIdLMdw4J/5IrjCErqL7ASHWXjWZwsS6JRHdCCI5yaFgDQidzEDdbnM7KvH3P
w33IoyLPX5HmoGJSBw==
-----END CERTIFICATE-----`;

const CAPABILITY = Buffer.alloc(32, 0xa5).toString("base64url");
const encoder = new TextEncoder();

function wire(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    protocol: "tammy-core-ready-v1",
    port: 54321,
    ca_pem: CERTIFICATE,
    capability: CAPABILITY,
    ...overrides,
  };
}

function encodeRecord(value: unknown): Uint8Array {
  return encoder.encode(`${JSON.stringify(value)}\n`);
}

function expectReadinessError(
  bytes: Uint8Array,
  code: ReadinessError["code"],
  message: string,
): void {
  try {
    parseReadiness(bytes);
    throw new Error("parseReadiness unexpectedly accepted invalid input");
  } catch (error) {
    expect(error).toBeInstanceOf(ReadinessError);
    expect(error).toMatchObject({ code, message });
    expect(Object.keys(error as object)).toEqual(["code", "name"]);
  }
}

describe("parseReadiness", () => {
  it("parses one exact record into a frozen, camel-case projection", () => {
    const readiness = parseReadiness(encodeRecord(wire()));

    expect(readiness).toEqual({
      protocol: "tammy-core-ready-v1",
      port: 54321,
      caPem: CERTIFICATE,
      capability: CAPABILITY,
    });
    expect(Object.keys(readiness)).toEqual(["protocol", "port", "caPem", "capability"]);
    expect(Object.isFrozen(readiness)).toBe(true);
  });

  it.each([
    ["invalid JSON", encoder.encode("{nope}\n"), "INVALID_JSON", "Invalid readiness JSON."],
    [
      "invalid UTF-8",
      Uint8Array.from([0xc3, 0x28, 0x0a]),
      "INVALID_ENCODING",
      "Invalid readiness encoding.",
    ],
    [
      "an unknown field",
      encodeRecord(wire({ unexpected: true })),
      "INVALID_SCHEMA",
      "Invalid readiness record.",
    ],
    [
      "a missing field",
      encodeRecord({
        protocol: "tammy-core-ready-v1",
        port: 54321,
        ca_pem: CERTIFICATE,
      }),
      "INVALID_SCHEMA",
      "Invalid readiness record.",
    ],
    [
      "an extra line",
      encoder.encode(`${JSON.stringify(wire())}\n{}\n`),
      "INVALID_FRAMING",
      "Invalid readiness framing.",
    ],
    [
      "bytes after the final newline",
      encoder.encode(`${JSON.stringify(wire())}\n `),
      "INVALID_FRAMING",
      "Invalid readiness framing.",
    ],
    [
      "no final newline",
      encoder.encode(JSON.stringify(wire())),
      "INVALID_FRAMING",
      "Invalid readiness framing.",
    ],
    [
      "more than 65,536 bytes",
      new Uint8Array(65_537),
      "RECORD_TOO_LARGE",
      "Readiness record is too large.",
    ],
  ] as const)("rejects %s", (_name, bytes, code, message) => {
    expectReadinessError(bytes, code, message);
  });

  it.each([
    ["a numeric string", "54321"],
    ["a host-and-port string", "127.0.0.1:54321"],
    ["a wildcard address", "0.0.0.0:54321"],
    ["a fractional number", 123.5],
    ["zero", 0],
    ["a negative number", -1],
    ["a number above 65535", 65_536],
  ])("rejects port input shaped as %s", (_name, port) => {
    expectReadinessError(
      encodeRecord(wire({ port })),
      "INVALID_SCHEMA",
      "Invalid readiness record.",
    );
  });

  it("rejects content that is not a PEM certificate", () => {
    expectReadinessError(
      encodeRecord(
        wire({ ca_pem: "-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----" }),
      ),
      "INVALID_CA",
      "Invalid readiness certificate.",
    );
  });

  it.each([
    ["padded", `${CAPABILITY}=`],
    ["malformed", `${CAPABILITY.slice(0, -1)}*`],
    ["non-canonical", `${CAPABILITY.slice(0, -1)}/`],
    ["wrong decoded length", Buffer.alloc(31, 0xa5).toString("base64url")],
  ])("rejects a %s capability", (_name, capability) => {
    expectReadinessError(
      encodeRecord(wire({ capability })),
      "INVALID_CAPABILITY",
      "Invalid readiness capability.",
    );
  });

  it("never exposes record content through its public errors", () => {
    const secrets = [CAPABILITY, "54321", "BEGIN CERTIFICATE", "PRIVATE-SECRET"];
    const cases = [
      encodeRecord(wire({ capability: "PRIVATE-SECRET" })),
      encodeRecord(wire({ ca_pem: "PRIVATE-SECRET" })),
      encoder.encode(`${JSON.stringify(wire())}\nPRIVATE-SECRET`),
    ];

    for (const bytes of cases) {
      try {
        parseReadiness(bytes);
        throw new Error("parseReadiness unexpectedly accepted invalid input");
      } catch (error) {
        const publicText = `${String(error)} ${JSON.stringify(error)}`;
        for (const secret of secrets) {
          expect(publicText).not.toContain(secret);
        }
      }
    }
  });
});
