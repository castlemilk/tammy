import { create } from "@bufbuild/protobuf";
import {
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  ListBankStatementLinesRequestSchema,
  ListBankStatementLinesResponseSchema,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
import { PageRequestSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { FileClock, LoaderCircle } from "lucide-react";
import { useMemo } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

interface Activity {
  readonly at: number;
  readonly detail: string;
  readonly id: string;
  readonly kind: string;
  readonly label: string;
}

const journalsCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListJournalsResponseSchema,
});
const bankingCodec = createProtoMethodCodec({
  input: ListBankStatementLinesRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListBankStatementLinesResponseSchema,
});
const documentsCodec = createProtoMethodCodec({
  input: ListDocumentsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: ListDocumentsResponseSchema,
});
const basCodec = createProtoMethodCodec({
  input: GetCurrentBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: GetCurrentBasDraftResponseSchema,
});

export function AuditScreen({
  accounting,
  banking,
  documents,
  reporting,
  workspace,
}: {
  readonly accounting: Pick<TammyConveyorModule<"accounting">, "listJournals">;
  readonly banking: Pick<TammyConveyorModule<"banking">, "listBankStatementLines">;
  readonly documents: Pick<TammyConveyorModule<"documents">, "listDocuments">;
  readonly reporting: Pick<TammyConveyorModule<"reporting">, "getCurrentBasDraft">;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const organisationId = workspace?.organisationId ?? "";
  const userId = workspace?.userId ?? "";
  const sessionId = workspace?.sessionId ?? "";
  const inputs = useMemo(() => {
    const authentication = { actorUserId: userId, sessionId };
    const page = create(PageRequestSchema, { pageSize: 200 });
    return {
      banking: bankingCodec.encodeRequest(
        create(ListBankStatementLinesRequestSchema, { authentication, organisationId, page }),
      ),
      bas: basCodec.encodeRequest(
        create(GetCurrentBasDraftRequestSchema, { authentication, organisationId }),
      ),
      documents: documentsCodec.encodeRequest(
        create(ListDocumentsRequestSchema, { authentication, organisationId, page }),
      ),
      journals: journalsCodec.encodeRequest(
        create(ListJournalsRequestSchema, { authentication, organisationId, page }),
      ),
    };
  }, [organisationId, sessionId, userId]);
  const enabled = organisationId.length > 0;
  const journals = accounting.listJournals.useQuery({
    enabled,
    input: inputs.journals,
    retry: false,
  });
  const bankLines = banking.listBankStatementLines.useQuery({
    enabled,
    input: inputs.banking,
    retry: false,
  });
  const documentList = documents.listDocuments.useQuery({
    enabled,
    input: inputs.documents,
    retry: false,
  });
  const currentBas = reporting.getCurrentBasDraft.useQuery({
    enabled,
    input: inputs.bas,
    retry: false,
  });
  const pending =
    enabled && [journals, bankLines, documentList, currentBas].some((query) => query.isPending);
  const activities = useMemo<readonly Activity[] | undefined>(() => {
    if (!enabled || pending) return undefined;
    const items: Activity[] = [];
    try {
      if (journals.data)
        for (const journal of journalsCodec.decodeResponse(journals.data).journals)
          items.push({
            at: civilTime(journal.postingDate),
            detail: `${money(journal.totalDebits?.minorUnits)} debits and credits`,
            id: journal.id,
            kind: "Journal",
            label: journal.memo,
          });
    } catch {
      /* one unavailable projection must not hide the others */
    }
    try {
      if (bankLines.data)
        for (const line of bankingCodec.decodeResponse(bankLines.data).lines)
          items.push({
            at: civilTime(line.transactionDate),
            detail: `${money(line.amount?.minorUnits)} · ${statusLabel(line.status)}`,
            id: line.id,
            kind: "Banking",
            label: line.description,
          });
    } catch {
      /* one unavailable projection must not hide the others */
    }
    try {
      if (documentList.data)
        for (const document of documentsCodec.decodeResponse(documentList.data).documents)
          items.push({
            at: document.createdAt ? Number(document.createdAt.seconds) * 1000 : 0,
            detail: `${document.candidate?.invoiceNumber || "No invoice number"} · ${document.status === 2 ? "Reviewed" : "Needs review"}`,
            id: document.id,
            kind: "Document",
            label: document.candidate?.supplierName || document.sourceDisplayName,
          });
    } catch {
      /* one unavailable projection must not hide the others */
    }
    try {
      if (currentBas.data) {
        const workpaper = basCodec.decodeResponse(currentBas.data).workpaper;
        if (workpaper)
          items.push({
            at: workpaper.createdAt ? Number(workpaper.createdAt.seconds) * 1000 : 0,
            detail: `GST credits ${money(workpaper.gstCredits1b?.minorUnits)} · Draft — not lodged`,
            id: workpaper.id,
            kind: "GST & BAS",
            label: `${civilLabel(workpaper.periodStart)} – ${civilLabel(workpaper.periodEnd)}`,
          });
      }
    } catch {
      /* one unavailable projection must not hide the others */
    }
    return items.sort((left, right) => right.at - left.at || left.id.localeCompare(right.id));
  }, [bankLines.data, currentBas.data, documentList.data, enabled, journals.data, pending]);
  const unavailable =
    !enabled ||
    (!pending &&
      activities?.length === 0 &&
      [journals, bankLines, documentList, currentBas].every((query) => query.isError));

  return (
    <div className="mx-auto grid w-full max-w-[980px] gap-4">
      <div>
        <h1 className=" page-title">Audit trail</h1>
        <p className="mt-1 text-[14px] text-muted-foreground">
          A local chronological view of retained business records.
        </p>
      </div>
      <section className="overflow-x-auto rounded-[20px] border border-border bg-surface">
        <div className="border-b border-border px-3 py-2 text-[12px] font-semibold uppercase tracking-[0.05em] text-muted-foreground">
          Recent activity
        </div>
        {activities === undefined && !unavailable ? (
          <div className="grid min-h-64 place-items-center">
            <LoaderCircle className="size-4 animate-spin text-forest" />
          </div>
        ) : activities && activities.length > 0 ? (
          <ol className="divide-y divide-border">
            {activities.map((activity) => (
              <li
                className="grid grid-cols-[90px_1fr_auto] items-center gap-3 px-3 py-3"
                key={`${activity.kind}-${activity.id}`}
              >
                <span className="rounded-full bg-muted px-2 py-1 text-center text-[12px] font-semibold text-muted-foreground">
                  {activity.kind}
                </span>
                <span>
                  <span className="block text-[14px] font-semibold">{activity.label}</span>
                  <span className="mt-1 block text-[12px] text-muted-foreground">
                    {activity.detail}
                  </span>
                </span>
                <time className="text-[12px] text-muted-foreground">
                  {activity.at > 0 ? new Date(activity.at).toLocaleDateString("en-AU") : "—"}
                </time>
              </li>
            ))}
          </ol>
        ) : (
          <div className="grid min-h-64 place-items-center p-8 text-center">
            <div>
              <FileClock className="mx-auto size-5 text-muted-foreground" />
              <p className="mt-2 text-[14px] font-semibold">
                {unavailable ? "Activity unavailable" : "No retained activity yet"}
              </p>
              <p className="mt-1 text-[12px] text-muted-foreground">
                Documents, journals, banking and BAS drafts appear here.
              </p>
            </div>
          </div>
        )}
      </section>
      <p className="text-[12px] text-muted-foreground">
        This product view does not replace exported audit-chain verification evidence.
      </p>
    </div>
  );
}

function money(value: bigint | undefined): string {
  return new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD" }).format(
    Number(value ?? 0n) / 100,
  );
}
function civilTime(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
): number {
  return value ? Date.UTC(value.year, value.month - 1, value.day) : 0;
}
function civilLabel(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
): string {
  return value
    ? `${value.day.toString().padStart(2, "0")}/${value.month.toString().padStart(2, "0")}/${value.year}`
    : "—";
}
function statusLabel(status: number): string {
  return status === 3 ? "Reconciled" : status === 2 ? "Matched" : "Unmatched";
}
