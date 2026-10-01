import { create } from "@bufbuild/protobuf";
import {
  AccountStatus,
  AccountType,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { CivilDateSchema, PageRequestSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentStatus,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
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
import { validateInvoiceDraft } from "@tammy/invoices/model";
import { useMemo } from "react";
import type { TammyConveyor } from "../../../conveyor/client";
import { invoiceAccountingToRecord } from "../../../shared/invoice-accounting";
import { invoiceLogoToPreview, invoiceTemplateFromProto } from "../../../shared/invoice-template";
import { invoiceDraftToProto, invoiceToRecord } from "../../../shared/invoices";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { type InvoicesApi, InvoicesScreen } from "./invoices-screen";

const get = createProtoMethodCodec({
  input: GetInvoiceRequestSchema,
  output: GetInvoiceResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 131072,
});
const list = createProtoMethodCodec({
  input: ListInvoicesRequestSchema,
  output: ListInvoicesResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 16777216,
});
const save = createProtoMethodCodec({
  input: SaveInvoiceDraftRequestSchema,
  output: SaveInvoiceDraftResponseSchema,
  maximumRequestBytes: 131072,
  maximumResponseBytes: 131072,
});
const issue = createProtoMethodCodec({
  input: IssueInvoiceRequestSchema,
  output: IssueInvoiceResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 131072,
});
const documents = createProtoMethodCodec({
  input: ListDocumentsRequestSchema,
  output: ListDocumentsResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 4194304,
});
const getTemplate = createProtoMethodCodec({
  input: GetInvoiceTemplateRequestSchema,
  output: GetInvoiceTemplateResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 32768,
});
const saveTemplate = createProtoMethodCodec({
  input: SaveInvoiceTemplateRequestSchema,
  output: SaveInvoiceTemplateResponseSchema,
  maximumRequestBytes: 32768,
  maximumResponseBytes: 32768,
});
const uploadLogo = createProtoMethodCodec({
  input: UploadInvoiceLogoRequestSchema,
  output: UploadInvoiceLogoResponseSchema,
  maximumRequestBytes: 1179648,
  maximumResponseBytes: 8192,
});
const getLogo = createProtoMethodCodec({
  input: GetInvoiceLogoRequestSchema,
  output: GetInvoiceLogoResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 1179648,
});
const getAccounting = createProtoMethodCodec({
  input: GetInvoiceAccountingRequestSchema,
  output: GetInvoiceAccountingResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 262144,
});
const postIncome = createProtoMethodCodec({
  input: PostInvoiceIncomeRequestSchema,
  output: PostInvoiceIncomeResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 262144,
});
const recordPayment = createProtoMethodCodec({
  input: RecordInvoicePaymentRequestSchema,
  output: RecordInvoicePaymentResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 262144,
});
const accounts = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  output: ListAccountsResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 131072,
});

export function ConnectedInvoicesScreen({
  conveyor,
  workspace,
}: {
  readonly conveyor: TammyConveyor;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const api = useMemo<InvoicesApi>(() => {
    const paymentAttempts = new Map<
      string,
      { readonly identity: string; readonly frame: Uint8Array }
    >();
    return {
      accounting: {
        beginPayment: (invoiceId) => {
          paymentAttempts.delete(invoiceId);
        },
        get: async (invoiceId) => {
          const response = getAccounting.decodeResponse(
            await conveyor.accounting.getInvoiceAccounting(
              getAccounting.encodeRequest(
                create(GetInvoiceAccountingRequestSchema, { ...authority(workspace), invoiceId }),
              ),
            ),
          );
          if (!response.accounting) throw new Error("INVOICE_ACCOUNTING_UNAVAILABLE");
          return invoiceAccountingToRecord(response.accounting);
        },
        post: async (invoiceId, expectedVersion) => {
          const context = authority(workspace);
          const response = postIncome.decodeResponse(
            await conveyor.accounting.postInvoiceIncome(
              postIncome.encodeRequest(
                create(PostInvoiceIncomeRequestSchema, {
                  organisationId: context.organisationId,
                  commandContext: {
                    authentication: context.authentication,
                    idempotencyKey: uuidV7(),
                  },
                  invoiceId,
                  expectedVersion: BigInt(expectedVersion),
                }),
              ),
            ),
          );
          if (!response.accounting) throw new Error("INVOICE_ACCOUNTING_UNAVAILABLE");
          await invalidateIncome(conveyor);
          return invoiceAccountingToRecord(response.accounting);
        },
        recordPayment: async (invoiceId, expectedAccountingVersion, payment) => {
          const context = authority(workspace);
          const [year, month, day] = payment.date.split("-").map(Number);
          const identity = JSON.stringify(payment);
          let attempt = paymentAttempts.get(invoiceId);
          if (attempt?.identity !== identity) {
            attempt = {
              identity,
              frame: recordPayment.encodeRequest(
                create(RecordInvoicePaymentRequestSchema, {
                  organisationId: context.organisationId,
                  commandContext: {
                    authentication: context.authentication,
                    idempotencyKey: uuidV7(),
                  },
                  invoiceId,
                  expectedAccountingVersion: BigInt(expectedAccountingVersion),
                  paymentDate: create(CivilDateSchema, {
                    year: year ?? 0,
                    month: month ?? 0,
                    day: day ?? 0,
                  }),
                  amount: { currencyCode: "AUD", minorUnits: BigInt(payment.amountMinor) },
                  bankAccountId: payment.bankAccountId,
                  reference: payment.reference,
                }),
              ),
            };
            paymentAttempts.set(invoiceId, attempt);
          }
          // Keep the original version and operation key together when a success
          // response is lost. A refreshed version must not create another receipt.
          const response = recordPayment.decodeResponse(
            await conveyor.accounting.recordInvoicePayment(attempt.frame),
          );
          if (!response.accounting) throw new Error("INVOICE_ACCOUNTING_UNAVAILABLE");
          const value = invoiceAccountingToRecord(response.accounting);
          paymentAttempts.delete(invoiceId);
          // The committed receipt remains successful if a derived cache cannot refresh.
          await invalidateIncome(conveyor);
          return value;
        },
        listBankAccounts: async () => {
          const result: { id: string; code: string; name: string }[] = [];
          const seen = new Set<string>();
          let cursor: string | undefined;
          do {
            const response = accounts.decodeResponse(
              await conveyor.accounting.listAccounts(
                accounts.encodeRequest(
                  create(ListAccountsRequestSchema, {
                    ...authority(workspace),
                    status: AccountStatus.ACTIVE,
                    type: AccountType.ASSET,
                    page: { pageSize: 200, ...(cursor ? { cursor } : {}) },
                  }),
                ),
              ),
            );
            result.push(
              ...response.accounts
                .filter(
                  (account) =>
                    account.status === AccountStatus.ACTIVE &&
                    ["balance_sheet.cash", "balance_sheet.cash_equivalent"].includes(
                      account.reportClassification,
                    ),
                )
                .map(({ id, code, name }) => ({ id, code, name })),
            );
            cursor = response.page?.nextCursor;
            if (cursor && seen.has(cursor)) throw new Error("BANK_ACCOUNTS_UNAVAILABLE");
            if (cursor) seen.add(cursor);
          } while (cursor);
          return result;
        },
      },
      list: async () => {
        const context = authority(workspace);
        const result: ReturnType<typeof invoiceToRecord>[] = [];
        const seen = new Set<string>();
        let cursor: string | undefined;
        do {
          const response = list.decodeResponse(
            await conveyor.accounting.listInvoices(
              list.encodeRequest(
                create(ListInvoicesRequestSchema, {
                  ...context,
                  page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
                }),
              ),
            ),
          );
          result.push(...response.invoices.map(invoiceToRecord));
          cursor = response.page?.nextCursor;
          if (cursor && seen.has(cursor)) throw new Error("INVOICES_UNAVAILABLE");
          if (cursor) seen.add(cursor);
        } while (cursor);
        return result;
      },
      save: async (draft, invoiceId = "", expectedVersion = 0, logoAssetId) => {
        const context = authority(workspace);
        const response = save.decodeResponse(
          await conveyor.accounting.saveInvoiceDraft(
            save.encodeRequest(
              create(SaveInvoiceDraftRequestSchema, {
                organisationId: context.organisationId,
                commandContext: {
                  authentication: context.authentication,
                  idempotencyKey: uuidV7(),
                },
                invoiceId,
                expectedVersion: BigInt(expectedVersion),
                draft: invoiceDraftToProto(draft),
                ...(logoAssetId === undefined ? {} : { logoAssetId }),
              }),
            ),
          ),
        );
        if (!response.invoice) throw new Error("INVOICE_UNAVAILABLE");
        return invoiceToRecord(response.invoice);
      },
      issue: async (invoiceId, expectedVersion) => {
        const context = authority(workspace);
        const response = issue.decodeResponse(
          await conveyor.accounting.issueInvoice(
            issue.encodeRequest(
              create(IssueInvoiceRequestSchema, {
                organisationId: context.organisationId,
                commandContext: {
                  authentication: context.authentication,
                  idempotencyKey: uuidV7(),
                },
                invoiceId,
                expectedVersion: BigInt(expectedVersion),
              }),
            ),
          ),
        );
        if (!response.invoice) throw new Error("INVOICE_UNAVAILABLE");
        await invalidateIncome(conveyor);
        return invoiceToRecord(response.invoice);
      },
      exportPdf: async (invoiceId) => {
        return await conveyor.accounting.exportInvoicePdf(
          get.encodeRequest(
            create(GetInvoiceRequestSchema, { ...authority(workspace), invoiceId }),
          ),
        );
      },
      getProposal: async () => {
        const response = await conveyor.accounting.getInvoiceProposal(
          get.encodeRequest(create(GetInvoiceRequestSchema, authority(workspace))),
        );
        return response.draft ? validateInvoiceDraft(response.draft) : undefined;
      },
      getTemplate: async () => {
        const context = authority(workspace);
        const response = getTemplate.decodeResponse(
          await conveyor.accounting.getInvoiceTemplate(
            getTemplate.encodeRequest(create(GetInvoiceTemplateRequestSchema, context)),
          ),
        );
        return invoiceTemplateFromProto(response.template, context.organisationId);
      },
      saveTemplate: async (defaults, expectedVersion) => {
        const context = authority(workspace);
        const response = saveTemplate.decodeResponse(
          await conveyor.accounting.saveInvoiceTemplate(
            saveTemplate.encodeRequest(
              create(SaveInvoiceTemplateRequestSchema, {
                organisationId: context.organisationId,
                commandContext: {
                  authentication: context.authentication,
                  idempotencyKey: uuidV7(),
                },
                expectedVersion: BigInt(expectedVersion),
                defaults,
              }),
            ),
          ),
        );
        return invoiceTemplateFromProto(response.template, context.organisationId);
      },
      uploadLogo: async (file) => {
        const context = authority(workspace);
        if (
          !["image/png", "image/jpeg"].includes(file.type) ||
          file.size < 1 ||
          file.size > 1048576
        )
          throw new Error("INVALID_INVOICE_LOGO");
        const content = await readFile(file);
        const response = uploadLogo.decodeResponse(
          await conveyor.accounting.uploadInvoiceLogo(
            uploadLogo.encodeRequest(
              create(UploadInvoiceLogoRequestSchema, {
                organisationId: context.organisationId,
                commandContext: {
                  authentication: context.authentication,
                  idempotencyKey: uuidV7(),
                },
                mimeType: file.type,
                content,
              }),
            ),
          ),
        );
        if (!response.logo) throw new Error("INVOICE_LOGO_UNAVAILABLE");
        return invoiceLogoToPreview(
          response.logo,
          content,
          context.organisationId,
          response.logo.id,
        ).id;
      },
      getLogo: async (logoAssetId) => {
        const context = authority(workspace);
        const response = getLogo.decodeResponse(
          await conveyor.accounting.getInvoiceLogo(
            getLogo.encodeRequest(create(GetInvoiceLogoRequestSchema, { ...context, logoAssetId })),
          ),
        );
        return invoiceLogoToPreview(
          response.logo,
          response.content,
          context.organisationId,
          logoAssetId,
        );
      },
      listExpenses: async () => {
        const response = documents.decodeResponse(
          await conveyor.documents.listDocuments(
            documents.encodeRequest(
              create(ListDocumentsRequestSchema, {
                ...authority(workspace),
                page: create(PageRequestSchema, { pageSize: 200 }),
              }),
            ),
          ),
        );
        return response.documents
          .filter((document) => document.status === DocumentStatus.REVIEWED && document.candidate)
          .map((document) => ({
            id: document.id,
            description: `${document.candidate?.supplierName || document.sourceDisplayName}${document.candidate?.invoiceNumber ? ` · ${document.candidate.invoiceNumber}` : ""}`,
            subtotalMinor: Number(document.candidate?.subtotal?.minorUnits ?? 0n),
            gstMinor: Number(document.candidate?.gst?.minorUnits ?? 0n),
          }));
      },
    };
  }, [conveyor, workspace]);
  return (
    <InvoicesScreen
      api={api}
      key={`${workspace?.userId}:${workspace?.sessionId}:${workspace?.organisationId}`}
    />
  );
}

function readFile(file: File): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      if (reader.result instanceof ArrayBuffer) resolve(new Uint8Array(reader.result));
      else reject(new Error("INVOICE_LOGO_UNAVAILABLE"));
    };
    reader.onerror = () => reject(new Error("INVOICE_LOGO_UNAVAILABLE"));
    reader.onabort = () => reject(new Error("INVOICE_LOGO_UNAVAILABLE"));
    reader.readAsArrayBuffer(file);
  });
}

async function invalidateIncome(conveyor: TammyConveyor) {
  const refreshes = [
    () => conveyor.accounting.getBusinessIncomeSummary.invalidate(),
    () => conveyor.accounting.listAccounts.invalidate(),
    () => conveyor.accounting.listJournals.invalidate(),
    () => conveyor.accounting.getTrialBalance.invalidate(),
    () => conveyor.reporting.getAttentionSummary.invalidate(),
    () => conveyor.reporting.getCurrentBasDraft.invalidate(),
  ];
  await Promise.allSettled(refreshes.map((refresh) => Promise.resolve().then(refresh)));
}

function authority(workspace: AuthenticatedWorkspace | undefined) {
  if (!workspace?.organisationId) throw new Error("AUTHENTICATION_REQUIRED");
  return {
    organisationId: workspace.organisationId,
    authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
  };
}
function uuidV7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  let milliseconds = Date.now();
  for (let index = 5; index >= 0; index--) {
    bytes[index] = milliseconds & 255;
    milliseconds = Math.floor(milliseconds / 256);
  }
  bytes[6] = ((bytes[6] ?? 0) & 15) | 112;
  bytes[8] = ((bytes[8] ?? 0) & 63) | 128;
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
