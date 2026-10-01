import { create } from "@bufbuild/protobuf";
import { PageRequestSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentStatus,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
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
import { validateInvoiceDraft } from "@tammy/invoices/model";
import { useMemo } from "react";
import type { TammyConveyor } from "../../../conveyor/client";
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

export function ConnectedInvoicesScreen({
  conveyor,
  workspace,
}: {
  readonly conveyor: TammyConveyor;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const api = useMemo<InvoicesApi>(
    () => ({
      list: async () => {
        const context = authority(workspace);
        const response = list.decodeResponse(
          await conveyor.accounting.listInvoices(
            list.encodeRequest(
              create(ListInvoicesRequestSchema, {
                ...context,
                page: create(PageRequestSchema, { pageSize: 200 }),
              }),
            ),
          ),
        );
        return response.invoices.map(invoiceToRecord);
      },
      save: async (draft, invoiceId = "", expectedVersion = 0) => {
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
    }),
    [conveyor, workspace],
  );
  return (
    <InvoicesScreen
      api={api}
      key={`${workspace?.userId}:${workspace?.sessionId}:${workspace?.organisationId}`}
    />
  );
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
