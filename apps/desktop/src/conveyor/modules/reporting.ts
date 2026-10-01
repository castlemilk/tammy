import {
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import {
  CreateBasDraftRequestSchema,
  CreateBasDraftResponseSchema,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule, query } from "../init";
import { conveyorProtoFrame, invokeCore } from "../proto";

const currentBas = createProtoMethodCodec({
  input: GetCurrentBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4_194_304,
  output: GetCurrentBasDraftResponseSchema,
});
const capability = createProtoMethodCodec({
  input: GetReportingCapabilityRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetReportingCapabilityResponseSchema,
});
const attention = createProtoMethodCodec({
  input: GetAttentionSummaryRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 65_536,
  output: GetAttentionSummaryResponseSchema,
});
const createBas = createProtoMethodCodec({
  input: CreateBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4_194_304,
  output: CreateBasDraftResponseSchema,
});

export const reporting = defineModule({
  getCurrentBasDraft: query(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(currentBas, input, ctx.client.getCurrentBasDraft),
  ),
  getReportingCapability: query(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(capability, input, ctx.client.getReportingCapability),
  ),
  getAttentionSummary: query(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(attention, input, ctx.client.getAttentionSummary),
  ),
  createBasDraft: command(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(createBas, input, ctx.client.createBasDraft),
  ),
});
