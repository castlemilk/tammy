import {
  CreateAccountRequestSchema,
  CreateAccountResponseSchema,
  GetJournalRequestSchema,
  GetJournalResponseSchema,
  GetTrialBalanceRequestSchema,
  GetTrialBalanceResponseSchema,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
  PostManualJournalRequestSchema,
  PostManualJournalResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  GetInvoiceRequestSchema,
  GetInvoiceResponseSchema,
  IssueInvoiceRequestSchema,
  IssueInvoiceResponseSchema,
  ListInvoicesRequestSchema,
  ListInvoicesResponseSchema,
  SaveInvoiceDraftRequestSchema,
  SaveInvoiceDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { ConveyorError } from "electron-conveyor/define";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule, query } from "../init";
import { conveyorProtoFrame, invokeCore } from "../proto";

const listAccounts = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 131_072,
  output: ListAccountsResponseSchema,
});
const listJournals = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListJournalsResponseSchema,
});
const getJournal = createProtoMethodCodec({
  input: GetJournalRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 262_144,
  output: GetJournalResponseSchema,
});
const getTrialBalance = createProtoMethodCodec({
  input: GetTrialBalanceRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 524_288,
  output: GetTrialBalanceResponseSchema,
});
const createAccount = createProtoMethodCodec({
  input: CreateAccountRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: CreateAccountResponseSchema,
});
const postManualJournal = createProtoMethodCodec({
  input: PostManualJournalRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 262_144,
  output: PostManualJournalResponseSchema,
});
const listInvoices = createProtoMethodCodec({
  input: ListInvoicesRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 16_777_216,
  output: ListInvoicesResponseSchema,
});
const getInvoice = createProtoMethodCodec({
  input: GetInvoiceRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 131_072,
  output: GetInvoiceResponseSchema,
});
const saveInvoice = createProtoMethodCodec({
  input: SaveInvoiceDraftRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 131_072,
  output: SaveInvoiceDraftResponseSchema,
});
const issueInvoice = createProtoMethodCodec({
  input: IssueInvoiceRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 131_072,
  output: IssueInvoiceResponseSchema,
});

export const accounting = defineModule({
  listAccounts: query(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(listAccounts, input, ctx.client.listAccounts),
  ),
  listJournals: query(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(listJournals, input, ctx.client.listJournals),
  ),
  getJournal: query(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(getJournal, input, ctx.client.getJournal),
  ),
  getTrialBalance: query(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(getTrialBalance, input, ctx.client.getTrialBalance),
  ),
  createAccount: command(conveyorProtoFrame(32_768), ({ input, ctx }) =>
    invokeCore(createAccount, input, ctx.client.createAccount),
  ),
  postManualJournal: command(conveyorProtoFrame(131_072), ({ input, ctx }) =>
    invokeCore(postManualJournal, input, ctx.client.postManualJournal),
  ),
  listInvoices: query(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(listInvoices, input, ctx.client.listInvoices),
  ),
  getInvoice: query(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(getInvoice, input, ctx.client.getInvoice),
  ),
  saveInvoiceDraft: command(conveyorProtoFrame(131_072), ({ input, ctx }) =>
    invokeCore(saveInvoice, input, ctx.client.saveInvoiceDraft),
  ),
  issueInvoice: command(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(issueInvoice, input, ctx.client.issueInvoice),
  ),
  exportInvoicePdf: command(conveyorProtoFrame(8_192), async ({ input, ctx }) => {
    try {
      if (!ctx.invoiceExport) throw new Error();
      return await ctx.invoiceExport.export(getInvoice.decodeRequest(input));
    } catch {
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    }
  }),
  getInvoiceProposal: query(conveyorProtoFrame(8_192), async ({ input, ctx }) => {
    try {
      if (!ctx.invoiceProposal) throw new Error();
      return { draft: await ctx.invoiceProposal(getInvoice.decodeRequest(input)) };
    } catch {
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    }
  }),
});
