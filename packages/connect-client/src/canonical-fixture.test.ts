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
});
