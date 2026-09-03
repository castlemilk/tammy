import { GetAttentionSummaryRequestSchema, GetAttentionSummaryResponseSchema } from "@tammy/connect-client/tammy/v1/overview_pb.js";
import type { StandardSchemaV1 } from "electron-conveyor/define";
import { GetReportingCapabilityRequestSchema, GetReportingCapabilityResponseSchema } from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import { CreateBasDraftRequestSchema, CreateBasDraftResponseSchema, GetCurrentBasDraftRequestSchema, GetCurrentBasDraftResponseSchema } from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule, query } from "../init";
import { boundedProtoFrame, invokeCore } from "../proto";

const conveyorProtoFrame = (maximumBytes: number): StandardSchemaV1<Uint8Array, Uint8Array> => boundedProtoFrame(maximumBytes) as unknown as StandardSchemaV1<Uint8Array, Uint8Array>;

const currentBas = createProtoMethodCodec({ input: GetCurrentBasDraftRequestSchema, maximumRequestBytes: 16_384, maximumResponseBytes: 262_144, output: GetCurrentBasDraftResponseSchema });
const capability = createProtoMethodCodec({ input: GetReportingCapabilityRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: GetReportingCapabilityResponseSchema });
const attention = createProtoMethodCodec({ input: GetAttentionSummaryRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 65_536, output: GetAttentionSummaryResponseSchema });
const createBas = createProtoMethodCodec({ input: CreateBasDraftRequestSchema, maximumRequestBytes: 16_384, maximumResponseBytes: 262_144, output: CreateBasDraftResponseSchema });

export const reporting = defineModule({
  getCurrentBasDraft: query(conveyorProtoFrame(16_384), ({ input, ctx }) => invokeCore(currentBas, input, ctx.client.getCurrentBasDraft)),
  getReportingCapability: query(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(capability, input, ctx.client.getReportingCapability)),
  getAttentionSummary: query(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(attention, input, ctx.client.getAttentionSummary)),
  createBasDraft: command(conveyorProtoFrame(16_384), ({ input, ctx }) => invokeCore(createBas, input, ctx.client.createBasDraft)),
});
