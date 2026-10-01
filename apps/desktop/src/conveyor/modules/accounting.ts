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
  GetBusinessIncomeSummaryRequestSchema,
  GetBusinessIncomeSummaryResponseSchema,
  GetInvoiceAccountingRequestSchema,
  GetInvoiceAccountingResponseSchema,
  GetInvoiceLogoRequestSchema,
  GetInvoiceLogoResponseSchema,
  GetInvoiceRequestSchema,
  GetInvoiceResponseSchema,
  GetInvoiceTemplateRequestSchema,
  GetInvoiceTemplateResponseSchema,
  IssueInvoiceRequestSchema,
  IssueInvoiceResponseSchema,
  ListInvoicesRequestSchema,
  ListInvoicesResponseSchema,
  PostInvoiceIncomeRequestSchema,
  PostInvoiceIncomeResponseSchema,
  RecordInvoicePaymentRequestSchema,
  RecordInvoicePaymentResponseSchema,
  SaveInvoiceDraftRequestSchema,
  SaveInvoiceDraftResponseSchema,
  SaveInvoiceTemplateRequestSchema,
  SaveInvoiceTemplateResponseSchema,
  UploadInvoiceLogoRequestSchema,
  UploadInvoiceLogoResponseSchema,
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
const getInvoiceTemplate = createProtoMethodCodec({
  input: GetInvoiceTemplateRequestSchema,
  output: GetInvoiceTemplateResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 32768,
});
const saveInvoiceTemplate = createProtoMethodCodec({
  input: SaveInvoiceTemplateRequestSchema,
  output: SaveInvoiceTemplateResponseSchema,
  maximumRequestBytes: 32768,
  maximumResponseBytes: 32768,
});
const uploadInvoiceLogo = createProtoMethodCodec({
  input: UploadInvoiceLogoRequestSchema,
  output: UploadInvoiceLogoResponseSchema,
  maximumRequestBytes: 1179648,
  maximumResponseBytes: 8192,
});
const getInvoiceLogo = createProtoMethodCodec({
  input: GetInvoiceLogoRequestSchema,
  output: GetInvoiceLogoResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 1179648,
});

const getInvoiceAccounting = createProtoMethodCodec({
  input: GetInvoiceAccountingRequestSchema,
  output: GetInvoiceAccountingResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
});
const postInvoiceIncome = createProtoMethodCodec({
  input: PostInvoiceIncomeRequestSchema,
  output: PostInvoiceIncomeResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
});
const recordInvoicePayment = createProtoMethodCodec({
  input: RecordInvoicePaymentRequestSchema,
  output: RecordInvoicePaymentResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
});
const getBusinessIncomeSummary = createProtoMethodCodec({
  input: GetBusinessIncomeSummaryRequestSchema,
  output: GetBusinessIncomeSummaryResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
});

export const accounting = defineModule({
  getInvoiceAccounting: query(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(getInvoiceAccounting, input, ctx.client.getInvoiceAccounting),
  ),
  postInvoiceIncome: command(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(postInvoiceIncome, input, ctx.client.postInvoiceIncome),
  ),
  recordInvoicePayment: command(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(recordInvoicePayment, input, ctx.client.recordInvoicePayment),
  ),
  getBusinessIncomeSummary: query(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(getBusinessIncomeSummary, input, ctx.client.getBusinessIncomeSummary),
  ),

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
  getInvoiceTemplate: query(conveyorProtoFrame(8192), ({ input, ctx }) =>
    invokeCore(getInvoiceTemplate, input, ctx.client.getInvoiceTemplate),
  ),
  saveInvoiceTemplate: command(conveyorProtoFrame(32768), ({ input, ctx }) =>
    invokeCore(saveInvoiceTemplate, input, ctx.client.saveInvoiceTemplate),
  ),
  uploadInvoiceLogo: command(conveyorProtoFrame(1179648), ({ input, ctx }) =>
    invokeCore(uploadInvoiceLogo, input, ctx.client.uploadInvoiceLogo),
  ),
  getInvoiceLogo: query(conveyorProtoFrame(8192), ({ input, ctx }) =>
    invokeCore(getInvoiceLogo, input, ctx.client.getInvoiceLogo),
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
