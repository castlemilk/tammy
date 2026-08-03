import { createHash } from "node:crypto";
import { fromJson, type JsonObject, toJson } from "@bufbuild/protobuf";
import { describe, expect, test } from "vitest";

import fixture from "../../../test/fixtures/proto/canonical-requests.json" with { type: "json" };
import { CanonicalRequestSchema } from "./gen/tammy/v1/fixtures_pb.js";

function normalizeCanonicalRequest(input: JsonObject): JsonObject {
  const request = fromJson(CanonicalRequestSchema, input, { ignoreUnknownFields: false });
  if (request.updateMask !== undefined) {
    request.updateMask.paths = [...new Set(request.updateMask.paths)].sort();
  }
  return toJson(CanonicalRequestSchema, request, { useProtoFieldName: true }) as JsonObject;
}

function canonicalJSON(value: unknown): string {
  if (value === null || typeof value === "boolean" || typeof value === "number") {
    return JSON.stringify(value);
  }
  if (typeof value === "string") {
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map(canonicalJSON).join(",")}]`;
  }
  const object = value as Record<string, unknown>;
  return `{${Object.keys(object)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${canonicalJSON(object[key])}`)
    .join(",")}}`;
}

function semanticHashV1(normalized: JsonObject): string {
  const semantic = structuredClone(normalized) as Record<string, unknown>;
  const commandContext = semantic.command_context as Record<string, unknown> | undefined;
  if (commandContext !== undefined) {
    delete commandContext.authentication;
    delete commandContext.idempotency_key;
  }
  const preimage = Buffer.concat([
    Buffer.from("tammy.semantic-request-hash\0v1\0", "utf8"),
    Buffer.from(fixture.messageType, "utf8"),
    Buffer.from([0]),
    Buffer.from(canonicalJSON(semantic), "utf8"),
  ]);
  return createHash("sha256").update(preimage).digest("hex");
}

describe("canonical protobuf JSON fixtures", () => {
  test("target the generated canonical request message", () => {
    expect(fixture.schemaVersion).toBe(1);
    expect(fixture.messageType).toBe("tammy.v1.CanonicalRequest");
  });

  for (const fixtureCase of fixture.cases) {
    test(fixtureCase.name, () => {
      expect(normalizeCanonicalRequest(fixtureCase.input)).toEqual(
        fixtureCase.expectedNormalizedJson,
      );
    });
  }

  for (const fixtureCase of fixture.unknownFieldCases) {
    test(fixtureCase.name, () => {
      expect(() => normalizeCanonicalRequest(fixtureCase.input)).toThrow();
    });
  }

  for (const fixtureCase of fixture.semanticHashCases) {
    test(fixtureCase.name, () => {
      const normalized = normalizeCanonicalRequest(fixtureCase.input);
      expect(canonicalJSON(normalized)).toBe(fixtureCase.expectedCanonicalJson);
      expect(fixtureCase.expectedMessageType).toBe(fixture.messageType);
      expect(fixtureCase.expectedSemanticHashVersion).toBe("v1");
      expect(semanticHashV1(normalized)).toBe(fixtureCase.expectedSemanticHashHex);
    });
  }

  test("semantic vector relationships", () => {
    const remainingNames = new Set([
      "semantic-v1",
      "non-bmp-control-string",
      "presence-absent",
      "presence-explicit-empty",
      "metadata-only-a",
      "metadata-only-b",
      "semantic-change",
    ]);
    for (const fixtureCase of fixture.semanticHashCases) {
      expect(remainingNames.delete(fixtureCase.name)).toBe(true);
    }
    expect(remainingNames.size).toBe(0);
    const hashes = new Map(
      fixture.semanticHashCases.map((fixtureCase) => [
        fixtureCase.name,
        fixtureCase.expectedSemanticHashHex,
      ]),
    );
    expect(hashes.size).toBe(7);
    expect(hashes.get("metadata-only-a")).toBe(hashes.get("metadata-only-b"));
    expect(hashes.get("presence-absent")).not.toBe(hashes.get("presence-explicit-empty"));
    expect(hashes.get("metadata-only-a")).not.toBe(hashes.get("semantic-change"));
  });
});
