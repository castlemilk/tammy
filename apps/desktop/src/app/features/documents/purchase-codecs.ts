import {
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import * as p from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";

const query = { maximumRequestBytes: 16_384, maximumResponseBytes: 4_194_304 };
const command = { maximumRequestBytes: 131_072, maximumResponseBytes: 4_194_304 };
export const purchaseCodecs = {
  get: createProtoMethodCodec({
    ...query,
    input: p.GetPurchaseAccountingRequestSchema,
    output: p.GetPurchaseAccountingResponseSchema,
  }),
  list: createProtoMethodCodec({
    ...query,
    input: p.ListPurchasePaymentsRequestSchema,
    output: p.ListPurchasePaymentsResponseSchema,
  }),
  post: createProtoMethodCodec({
    ...command,
    input: p.PostReviewedPurchaseRequestSchema,
    output: p.PostReviewedPurchaseResponseSchema,
  }),
  payment: createProtoMethodCodec({
    ...command,
    input: p.RecordPurchasePaymentRequestSchema,
    output: p.RecordPurchasePaymentResponseSchema,
  }),
  evidence: createProtoMethodCodec({
    ...command,
    input: p.RecordPurchaseTaxEvidenceRequestSchema,
    output: p.RecordPurchaseTaxEvidenceResponseSchema,
  }),
  attribute: createProtoMethodCodec({
    ...command,
    input: p.AttributePurchaseGstRequestSchema,
    output: p.AttributePurchaseGstResponseSchema,
  }),
  defer: createProtoMethodCodec({
    ...command,
    input: p.DeferPurchaseGstClaimRequestSchema,
    output: p.DeferPurchaseGstClaimResponseSchema,
  }),
  legacy: createProtoMethodCodec({
    ...command,
    input: p.ResolveLegacyPurchaseReviewRequestSchema,
    output: p.ResolveLegacyPurchaseReviewResponseSchema,
  }),
};
export const accountCodec = createProtoMethodCodec({
  ...query,
  input: ListAccountsRequestSchema,
  output: ListAccountsResponseSchema,
});
export const retainedDocumentCodec = createProtoMethodCodec({
  ...query,
  input: ListDocumentsRequestSchema,
  output: ListDocumentsResponseSchema,
});
