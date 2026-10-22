import { create, toBinary } from "@bufbuild/protobuf";
import { AnySchema } from "@bufbuild/protobuf/wkt";
import {
  AuthenticationContextSchema,
  CommandContextSchema,
  SourceRefSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  CreateOrganisationRequestSchema,
  CreateOrganisationResponseSchema,
  GstBasis,
  GstReportingFrequency,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { describe, expect, it, vi } from "vitest";
import { installLegacyRuleCreateShimInMain } from "../../tests/e2e/support/legacy-rule-create-shim";
import { createProtoMethodCodec } from "../shared/proto-ipc";

const CHANNEL = "conveyor:organisation";
const STATE_KEY = "__legacyRuleCreateShimTestState";
const CURRENT_HASH = Uint8Array.from({ length: 32 }, (_, index) => index + 1);
const LEGACY_HASH = Uint8Array.from({ length: 32 }, (_, index) => 255 - index);
const EXPECTED_TYPE_URL = "type.googleapis.com/tammy.v1.CreateOrganisationRequest";

const codec = createProtoMethodCodec({
  input: CreateOrganisationRequestSchema,
  output: CreateOrganisationResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
});

function hashHex(value: Uint8Array): string {
  return [...value].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

function createOrganisationRequest() {
  return create(CreateOrganisationRequestSchema, {
    commandContext: create(CommandContextSchema, {
      idempotencyKey: "018f2f5e-7b5e-7a11-8f00-000000000611",
      authentication: create(AuthenticationContextSchema, {
        actorUserId: "018f2f5e-7b5e-7a11-8f00-000000000612",
        sessionId: "018f2f5e-7b5e-7a11-8f00-000000000613",
      }),
    }),
    abn: "51824753556",
    legalName: "Synthetic source review Pty Ltd",
    displayName: "Synthetic source review",
    entityType: "company",
    gstBasis: GstBasis.CASH,
    gstReportingFrequency: GstReportingFrequency.QUARTERLY,
    financialYearEndMonth: 6,
    activeTaxRuleBundle: create(SourceRefSchema, {
      type: "tax_rule_bundle",
      id: "018f2f5e-7b5e-7a11-8f00-000000000614",
      revision: 1n,
      contentHash: CURRENT_HASH,
    }),
  });
}

function installedHandler() {
  type Handler = (event: unknown, ...args: unknown[]) => unknown;
  const original = vi.fn(async (_event: unknown, ...args: unknown[]) => ({
    method: args[0],
    payload: args[1],
  }));
  const handlers = new Map<string, Handler>([[CHANNEL, original]]);
  const ipcMain = { _invokeHandlers: handlers };
  installLegacyRuleCreateShimInMain(
    { ipcMain },
    {
      currentHashHex: hashHex(CURRENT_HASH),
      expectedTypeUrl: EXPECTED_TYPE_URL,
      legacyHashHex: hashHex(LEGACY_HASH),
      stateKey: STATE_KEY,
    },
  );
  const handler = handlers.get(CHANNEL);
  if (!handler) throw new Error("LEGACY_RULE_SHIM_HANDLER_MISSING");
  return { handler, handlers, ipcMain, original };
}

describe("legacy rule create shim", () => {
  it("changes only the exact typed create request hash and restores the original handler", async () => {
    const { handler, handlers, ipcMain, original } = installedHandler();
    const event = { sender: "synthetic" };
    const untouchedFrame = Uint8Array.of(0xde, 0xad, 0xbe, 0xef);
    await expect(handler(event, "getOrganisation", untouchedFrame)).resolves.toEqual({
      method: "getOrganisation",
      payload: untouchedFrame,
    });
    expect(original).toHaveBeenCalledTimes(1);
    expect(original.mock.calls[0]?.[2]).toBe(untouchedFrame);

    const request = createOrganisationRequest();
    const requestFrame = codec.encodeRequest(request);
    await handler(event, "createOrganisation", requestFrame);

    const forwardedFrame = original.mock.calls[1]?.[2];
    expect(forwardedFrame).toBeInstanceOf(Uint8Array);
    if (!(forwardedFrame instanceof Uint8Array))
      throw new Error("LEGACY_RULE_SHIM_FORWARDED_FRAME_MISSING");
    const decoded = codec.decodeRequest(forwardedFrame);
    const bundle = request.activeTaxRuleBundle;
    if (!bundle) throw new Error("LEGACY_RULE_SHIM_TEST_BUNDLE_MISSING");
    const expectedRequest = create(CreateOrganisationRequestSchema, {
      ...request,
      activeTaxRuleBundle: create(SourceRefSchema, {
        ...bundle,
        contentHash: LEGACY_HASH,
      }),
    });
    expect(decoded).toEqual(expectedRequest);
    expect(forwardedFrame).toEqual(codec.encodeRequest(expectedRequest));
    expect(requestFrame).toEqual(codec.encodeRequest(request));
    expect(handlers.get(CHANNEL)).toBe(original);
    expect((ipcMain as unknown as Record<string, unknown>)[STATE_KEY]).toEqual({
      replacementCount: 1,
      restored: true,
    });

    await handlers.get(CHANNEL)?.(event, "createOrganisation", requestFrame);
    expect(original).toHaveBeenCalledTimes(3);
    expect(original.mock.calls[2]?.[2]).toBe(requestFrame);
  });

  it("rejects a create frame with a different Any type URL without forwarding it", async () => {
    const { handler, original } = installedHandler();
    const request = createOrganisationRequest();
    const mismatchedFrame = toBinary(
      AnySchema,
      create(AnySchema, {
        typeUrl: "type.googleapis.com/tammy.v1.UpdateOrganisationRequest",
        value: toBinary(CreateOrganisationRequestSchema, request),
      }),
    );

    await expect(handler({}, "createOrganisation", mismatchedFrame)).rejects.toThrow(
      "BAS_ACCEPTANCE_LEGACY_RULE_TYPE_MISMATCH",
    );
    expect(original).not.toHaveBeenCalled();
  });
});
