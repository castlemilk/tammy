import { type DescMessage, type Message, type MessageShape, ScalarType } from "@bufbuild/protobuf";
import { FeatureSet_FieldPresence } from "@bufbuild/protobuf/wkt";
import {
  AttributePurchaseGstRequestSchema,
  AttributePurchaseGstResponseSchema,
  DeferPurchaseGstClaimRequestSchema,
  DeferPurchaseGstClaimResponseSchema,
  GetPurchaseAccountingRequestSchema,
  GetPurchaseAccountingResponseSchema,
  ListPurchasePaymentsRequestSchema,
  ListPurchasePaymentsResponseSchema,
  PostReviewedPurchaseRequestSchema,
  PostReviewedPurchaseResponseSchema,
  RecordPurchasePaymentRequestSchema,
  RecordPurchasePaymentResponseSchema,
  RecordPurchaseTaxEvidenceRequestSchema,
  RecordPurchaseTaxEvidenceResponseSchema,
  ResolveLegacyPurchaseReviewRequestSchema,
  ResolveLegacyPurchaseReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule, query } from "../init";
import { conveyorProtoFrame, invokeCore } from "../proto";

function assertPurchaseScalar(kind: ScalarType, value: unknown, longAsString: boolean): void {
  let valid = false;
  switch (kind) {
    case ScalarType.BOOL:
      valid = typeof value === "boolean";
      break;
    case ScalarType.STRING:
      valid = typeof value === "string" && value.isWellFormed();
      break;
    case ScalarType.BYTES:
      valid = value instanceof Uint8Array;
      break;
    case ScalarType.INT32:
    case ScalarType.SINT32:
    case ScalarType.SFIXED32:
      valid =
        typeof value === "number" &&
        Number.isInteger(value) &&
        value >= -(2 ** 31) &&
        value < 2 ** 31;
      break;
    case ScalarType.UINT32:
    case ScalarType.FIXED32:
      valid = typeof value === "number" && Number.isInteger(value) && value >= 0 && value < 2 ** 32;
      break;
    case ScalarType.INT64:
    case ScalarType.SINT64:
    case ScalarType.SFIXED64:
    case ScalarType.UINT64:
    case ScalarType.FIXED64: {
      const unsigned = kind === ScalarType.UINT64 || kind === ScalarType.FIXED64;
      let integer: bigint | undefined;
      if (longAsString) {
        if (typeof value === "string" && /^(?:0|[1-9][0-9]*|-[1-9][0-9]*)$/.test(value))
          integer = BigInt(value);
      } else if (typeof value === "bigint") integer = value;
      valid =
        integer !== undefined &&
        integer >= (unsigned ? 0n : -(1n << 63n)) &&
        integer < 1n << (unsigned ? 64n : 63n);
      break;
    }
    case ScalarType.FLOAT:
      valid =
        typeof value === "number" &&
        (!Number.isFinite(value) || Math.abs(value) <= 3.4028234663852886e38);
      break;
    case ScalarType.DOUBLE:
      valid = typeof value === "number";
      break;
  }
  if (!valid) throw new Error("INVALID_PURCHASE_MESSAGE");
}

// The binary codec rejects unknown wire fields. Check the generated message
// shape and defined enum values too, before Core input or renderer output can
// silently lose an unsupported fact during protobuf serialization. Core owns
// validation of required reviewed facts, amounts, dates and source ownership.
function assertPurchaseMessage(schema: DescMessage, value: unknown): asserts value is Message {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    throw new Error("INVALID_PURCHASE_MESSAGE");
  const message = value as Record<string, unknown>;
  const allowed = new Set([
    "$typeName",
    "$unknown",
    ...schema.fields.map((field) => field.oneof?.localName ?? field.localName),
  ]);
  if (
    message.$typeName !== schema.typeName ||
    Object.keys(message).some((name) => !allowed.has(name)) ||
    (message.$unknown !== undefined &&
      (!Array.isArray(message.$unknown) || message.$unknown.length !== 0))
  )
    throw new Error("INVALID_PURCHASE_MESSAGE");

  for (const oneof of schema.oneofs) {
    const selected = message[oneof.localName];
    if (typeof selected !== "object" || selected === null || Array.isArray(selected))
      throw new Error("INVALID_PURCHASE_MESSAGE");
    const selection = selected as Record<string, unknown>;
    if (
      Object.keys(selection).some((key) => key !== "case" && key !== "value") ||
      !Object.hasOwn(selection, "case") ||
      (selection.case === undefined
        ? selection.value !== undefined
        : !Object.hasOwn(selection, "value") || selection.value === undefined) ||
      (selection.case !== undefined &&
        !oneof.fields.some((field) => field.localName === selection.case))
    )
      throw new Error("INVALID_PURCHASE_MESSAGE");
  }

  for (const field of schema.fields) {
    let current = message[field.localName];
    if (field.oneof) {
      const selection = message[field.oneof.localName] as { case?: string; value?: unknown };
      if (selection.case !== field.localName) continue;
      current = selection.value;
    }
    if (current === undefined) {
      if (
        field.fieldKind === "message" ||
        (field.presence === FeatureSet_FieldPresence.EXPLICIT && !field.oneof)
      )
        continue;
      throw new Error("INVALID_PURCHASE_MESSAGE");
    }
    if (field.fieldKind === "message") assertPurchaseMessage(field.message, current);
    else if (field.fieldKind === "scalar")
      assertPurchaseScalar(field.scalar, current, field.longAsString);
    else if (field.fieldKind === "enum") {
      if (!field.enum.values.some(({ number }) => number === current))
        throw new Error("INVALID_PURCHASE_MESSAGE");
    } else if (field.fieldKind === "list") {
      if (!Array.isArray(current)) throw new Error("INVALID_PURCHASE_MESSAGE");
      for (const item of current) {
        if (field.listKind === "message") assertPurchaseMessage(field.message, item);
        else if (field.listKind === "scalar")
          assertPurchaseScalar(field.scalar, item, field.longAsString);
        else if (
          field.listKind === "enum" &&
          !field.enum.values.some(({ number }) => number === item)
        )
          throw new Error("INVALID_PURCHASE_MESSAGE");
      }
    }
  }
}

function createPurchaseCodec<Input extends DescMessage, Output extends DescMessage>(config: {
  readonly input: Input;
  readonly maximumRequestBytes: number;
  readonly maximumResponseBytes: number;
  readonly output: Output;
}) {
  const codec = createProtoMethodCodec(config);
  return Object.freeze({
    decodeRequest(frame: Uint8Array): MessageShape<Input> {
      const request = codec.decodeRequest(frame);
      assertPurchaseMessage(config.input, request);
      return request;
    },
    encodeResponse(response: MessageShape<Output>): Uint8Array {
      assertPurchaseMessage(config.output, response);
      return codec.encodeResponse(response);
    },
  });
}

const getPurchaseAccounting = createPurchaseCodec({
  input: GetPurchaseAccountingRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: GetPurchaseAccountingResponseSchema,
});
const postReviewedPurchase = createPurchaseCodec({
  input: PostReviewedPurchaseRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: PostReviewedPurchaseResponseSchema,
});
const recordPurchasePayment = createPurchaseCodec({
  input: RecordPurchasePaymentRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: RecordPurchasePaymentResponseSchema,
});
const listPurchasePayments = createPurchaseCodec({
  input: ListPurchasePaymentsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: ListPurchasePaymentsResponseSchema,
});
const recordPurchaseTaxEvidence = createPurchaseCodec({
  input: RecordPurchaseTaxEvidenceRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: RecordPurchaseTaxEvidenceResponseSchema,
});
const attributePurchaseGst = createPurchaseCodec({
  input: AttributePurchaseGstRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: AttributePurchaseGstResponseSchema,
});
const deferPurchaseGstClaim = createPurchaseCodec({
  input: DeferPurchaseGstClaimRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: DeferPurchaseGstClaimResponseSchema,
});
const resolveLegacyPurchaseReview = createPurchaseCodec({
  input: ResolveLegacyPurchaseReviewRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: ResolveLegacyPurchaseReviewResponseSchema,
});

export const purchases = defineModule({
  getPurchaseAccounting: query(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(getPurchaseAccounting, input, ctx.client.getPurchaseAccounting),
  ),
  postReviewedPurchase: command(conveyorProtoFrame(131_072), ({ input, ctx }) =>
    invokeCore(postReviewedPurchase, input, ctx.client.postReviewedPurchase),
  ),
  recordPurchasePayment: command(conveyorProtoFrame(131_072), ({ input, ctx }) =>
    invokeCore(recordPurchasePayment, input, ctx.client.recordPurchasePayment),
  ),
  listPurchasePayments: query(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(listPurchasePayments, input, ctx.client.listPurchasePayments),
  ),
  recordPurchaseTaxEvidence: command(conveyorProtoFrame(131_072), ({ input, ctx }) =>
    invokeCore(recordPurchaseTaxEvidence, input, ctx.client.recordPurchaseTaxEvidence),
  ),
  attributePurchaseGst: command(conveyorProtoFrame(131_072), ({ input, ctx }) =>
    invokeCore(attributePurchaseGst, input, ctx.client.attributePurchaseGst),
  ),
  deferPurchaseGstClaim: command(conveyorProtoFrame(131_072), ({ input, ctx }) =>
    invokeCore(deferPurchaseGstClaim, input, ctx.client.deferPurchaseGstClaim),
  ),
  resolveLegacyPurchaseReview: command(conveyorProtoFrame(131_072), ({ input, ctx }) =>
    invokeCore(resolveLegacyPurchaseReview, input, ctx.client.resolveLegacyPurchaseReview),
  ),
});
