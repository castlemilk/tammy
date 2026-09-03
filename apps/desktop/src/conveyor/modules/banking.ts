import { CompleteBankReconciliationRequestSchema, CompleteBankReconciliationResponseSchema, GetBankingSummaryRequestSchema, GetBankingSummaryResponseSchema, ImportBankStatementRequestSchema, ImportBankStatementResponseSchema, ListBankStatementLinesRequestSchema, ListBankStatementLinesResponseSchema, MatchBankStatementLineRequestSchema, MatchBankStatementLineResponseSchema } from "@tammy/connect-client/tammy/v1/banking_pb.js";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule, query } from "../init";
import { conveyorProtoFrame, invokeCore } from "../proto";

const listLines = createProtoMethodCodec({ input: ListBankStatementLinesRequestSchema, maximumRequestBytes: 16_384, maximumResponseBytes: 262_144, output: ListBankStatementLinesResponseSchema });
const summary = createProtoMethodCodec({ input: GetBankingSummaryRequestSchema, maximumRequestBytes: 16_384, maximumResponseBytes: 16_384, output: GetBankingSummaryResponseSchema });
const importStatement = createProtoMethodCodec({ input: ImportBankStatementRequestSchema, maximumRequestBytes: 262_144, maximumResponseBytes: 32_768, output: ImportBankStatementResponseSchema });
const matchLine = createProtoMethodCodec({ input: MatchBankStatementLineRequestSchema, maximumRequestBytes: 16_384, maximumResponseBytes: 32_768, output: MatchBankStatementLineResponseSchema });
const completeReconciliation = createProtoMethodCodec({ input: CompleteBankReconciliationRequestSchema, maximumRequestBytes: 16_384, maximumResponseBytes: 16_384, output: CompleteBankReconciliationResponseSchema });

export const banking = defineModule({
  listBankStatementLines: query(conveyorProtoFrame(16_384), ({ input, ctx }) => invokeCore(listLines, input, ctx.client.listBankStatementLines)),
  getBankingSummary: query(conveyorProtoFrame(16_384), ({ input, ctx }) => invokeCore(summary, input, ctx.client.getBankingSummary)),
  importBankStatement: command(conveyorProtoFrame(262_144), ({ input, ctx }) => invokeCore(importStatement, input, ctx.client.importBankStatement)),
  matchBankStatementLine: command(conveyorProtoFrame(16_384), ({ input, ctx }) => invokeCore(matchLine, input, ctx.client.matchBankStatementLine)),
  completeBankReconciliation: command(conveyorProtoFrame(16_384), ({ input, ctx }) => invokeCore(completeReconciliation, input, ctx.client.completeBankReconciliation)),
});
