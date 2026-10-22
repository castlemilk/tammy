import { create } from "@bufbuild/protobuf";
import {
  BasCalculationSchema,
  BasCoverageCandidateKind,
  BasCoverageDisposition,
  BasFrozenBasSchema,
  BasLiveCurrentness,
  BasRawSourceRefSchema,
  BasSourceCoverageSchema,
  BasSourceIdentitySchema,
  BasSourceProofScope,
  GetBasCalculationRequestSchema,
  GetBasCalculationResponseSchema,
  GetFrozenBasRequestSchema,
  GetFrozenBasResponseSchema,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import { expect, it } from "vitest";
import { BAS_PREPARATION_RESPONSE_FRAME_LIMIT } from "./bas-preparation-ipc";
import { createProtoMethodCodec, type ProtoIpcError } from "./proto-ipc";

const calculationCodec = createProtoMethodCodec({
  input: GetBasCalculationRequestSchema,
  output: GetBasCalculationResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: BAS_PREPARATION_RESPONSE_FRAME_LIMIT,
});
const frozenCodec = createProtoMethodCodec({
  input: GetFrozenBasRequestSchema,
  output: GetFrozenBasResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: BAS_PREPARATION_RESPONSE_FRAME_LIMIT,
});

function calculationWithRetainedEvidenceIdentityBytes(idLength: number) {
  const evidenceIdentity = create(BasSourceIdentitySchema, {
    rawSource: create(BasRawSourceRefSchema, {
      type: "legacy_source",
      id: "s".repeat(idLength),
      computedProofHash: new Uint8Array(32).fill(7),
      proofScope: BasSourceProofScope.REFERENCE_ONLY_PROJECTION,
    }),
  });
  const sourceCoverage = create(BasSourceCoverageSchema, {
    rawSource: create(BasRawSourceRefSchema, {
      type: "legacy_source",
      id: "candidate",
      computedProofHash: new Uint8Array(32).fill(8),
      proofScope: BasSourceProofScope.REFERENCE_ONLY_PROJECTION,
    }),
    candidateKind: BasCoverageCandidateKind.MANUAL_TAX_FACT,
    disposition: BasCoverageDisposition.UNRESOLVED,
    classificationCode: "RETAINED_SOURCE_IDENTITY",
    reason: "Exact source identity retained for report review.",
    evidenceIdentities: Array.from({ length: 20 }, () => evidenceIdentity),
  });
  return create(BasCalculationSchema, {
    coverage: Array.from({ length: 1000 }, () => sourceCoverage),
  });
}

it("round trips large exact calculation and frozen responses while rejecting frames above 16 MiB", () => {
  const acceptedCalculation = calculationWithRetainedEvidenceIdentityBytes(300);
  const calculationFrame = calculationCodec.encodeResponse(
    create(GetBasCalculationResponseSchema, { calculation: acceptedCalculation }),
  );
  expect(calculationFrame.byteLength).toBeGreaterThan(4_194_304);
  expect(calculationCodec.decodeResponse(calculationFrame).calculation?.coverage).toHaveLength(
    1000,
  );

  const acceptedFrozenFrame = frozenCodec.encodeResponse(
    create(GetFrozenBasResponseSchema, {
      frozenBas: create(BasFrozenBasSchema, { calculationSnapshot: acceptedCalculation }),
      liveCurrentness: BasLiveCurrentness.CURRENT,
    }),
  );
  expect(acceptedFrozenFrame.byteLength).toBeGreaterThan(4_194_304);
  expect(
    frozenCodec.decodeResponse(acceptedFrozenFrame).frozenBas?.calculationSnapshot?.coverage,
  ).toHaveLength(1000);

  const oversizedCalculation = calculationWithRetainedEvidenceIdentityBytes(900);
  expect(() =>
    calculationCodec.encodeResponse(
      create(GetBasCalculationResponseSchema, { calculation: oversizedCalculation }),
    ),
  ).toThrowError(
    expect.objectContaining<Partial<ProtoIpcError>>({ code: "PROTO_FRAME_TOO_LARGE" }),
  );
  expect(() =>
    frozenCodec.encodeResponse(
      create(GetFrozenBasResponseSchema, {
        frozenBas: create(BasFrozenBasSchema, { calculationSnapshot: oversizedCalculation }),
        liveCurrentness: BasLiveCurrentness.CURRENT,
      }),
    ),
  ).toThrowError(
    expect.objectContaining<Partial<ProtoIpcError>>({ code: "PROTO_FRAME_TOO_LARGE" }),
  );
});
