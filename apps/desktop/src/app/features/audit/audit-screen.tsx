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
import { useEffect, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

type AuditAPI = Pick<
  TammyDesktopAPI,
  "getCurrentBasDraft" | "listBankStatementLines" | "listDocuments" | "listJournals"
>;
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
  api,
  workspace,
}: {
  readonly api: AuditAPI;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const [activities, setActivities] = useState<readonly Activity[]>();
  const [unavailable, setUnavailable] = useState(false);
  useEffect(() => {
    if (!workspace?.organisationId) {
      setUnavailable(true);
      return;
    }
    let active = true;
    const authentication = { actorUserId: workspace.userId, sessionId: workspace.sessionId };
    const page = create(PageRequestSchema, { pageSize: 200 });
    void Promise.allSettled([
      api.listJournals(
        journalsCodec.encodeRequest(
          create(ListJournalsRequestSchema, {
            authentication,
            organisationId: workspace.organisationId,
            page,
          }),
        ),
      ),
      api.listBankStatementLines(
        bankingCodec.encodeRequest(
          create(ListBankStatementLinesRequestSchema, {
            authentication,
            organisationId: workspace.organisationId,
            page,
          }),
        ),
      ),
      api.listDocuments(
        documentsCodec.encodeRequest(
          create(ListDocumentsRequestSchema, {
            authentication,
            organisationId: workspace.organisationId,
            page,
          }),
        ),
      ),
      api.getCurrentBasDraft(
        basCodec.encodeRequest(
          create(GetCurrentBasDraftRequestSchema, {
            authentication,
            organisationId: workspace.organisationId,
          }),
        ),
      ),
    ])
      .then((results) => {
        if (!active) return;
        const items: Activity[] = [];
        if (results[0]?.status === "fulfilled")
          for (const journal of journalsCodec.decodeResponse(results[0].value).journals)
            items.push({
              at: civilTime(journal.postingDate),
              detail: `${money(journal.totalDebits?.minorUnits)} debits and credits`,
              id: journal.id,
              kind: "Journal",
              label: journal.memo,
            });
        if (results[1]?.status === "fulfilled")
          for (const line of bankingCodec.decodeResponse(results[1].value).lines)
            items.push({
              at: civilTime(line.transactionDate),
              detail: `${money(line.amount?.minorUnits)} · ${statusLabel(line.status)}`,
              id: line.id,
              kind: "Banking",
              label: line.description,
            });
        if (results[2]?.status === "fulfilled")
          for (const document of documentsCodec.decodeResponse(results[2].value).documents)
            items.push({
              at: document.createdAt ? Number(document.createdAt.seconds) * 1000 : 0,
              detail: `${document.candidate?.invoiceNumber || "No invoice number"} · ${document.status === 2 ? "Reviewed" : "Needs review"}`,
              id: document.id,
              kind: "Document",
              label: document.candidate?.supplierName || document.sourceDisplayName,
            });
        if (results[3]?.status === "fulfilled") {
          const workpaper = basCodec.decodeResponse(results[3].value).workpaper;
          if (workpaper)
            items.push({
              at: workpaper.createdAt ? Number(workpaper.createdAt.seconds) * 1000 : 0,
              detail: `GST credits ${money(workpaper.gstCredits1b?.minorUnits)} · Draft — not lodged`,
              id: workpaper.id,
              kind: "GST & BAS",
              label: `${civilLabel(workpaper.periodStart)} – ${civilLabel(workpaper.periodEnd)}`,
            });
        }
        setActivities(
          items.sort((left, right) => right.at - left.at || left.id.localeCompare(right.id)),
        );
      })
      .catch(() => {
        if (active) setUnavailable(true);
      });
    return () => {
      active = false;
    };
  }, [api, workspace]);

  return (
    <div className="mx-auto grid w-full max-w-[980px] gap-4">
      <div>
        <h1 className="text-[18px] font-semibold tracking-[-0.02em]">Audit trail</h1>
        <p className="mt-1 text-[11px] text-muted-foreground">
          A local chronological view of retained business records.
        </p>
      </div>
      <section className="overflow-hidden rounded-[6px] border border-border bg-surface">
        <div className="border-b border-border px-3 py-2 text-[9px] font-semibold uppercase tracking-[0.05em] text-muted-foreground">
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
                <span className="rounded-full bg-muted px-2 py-1 text-center text-[8px] font-semibold text-muted-foreground">
                  {activity.kind}
                </span>
                <span>
                  <span className="block text-[10px] font-semibold">{activity.label}</span>
                  <span className="mt-1 block text-[9px] text-muted-foreground">
                    {activity.detail}
                  </span>
                </span>
                <time className="text-[9px] text-muted-foreground">
                  {activity.at > 0 ? new Date(activity.at).toLocaleDateString("en-AU") : "—"}
                </time>
              </li>
            ))}
          </ol>
        ) : (
          <div className="grid min-h-64 place-items-center p-8 text-center">
            <div>
              <FileClock className="mx-auto size-5 text-muted-foreground" />
              <p className="mt-2 text-[11px] font-semibold">
                {unavailable ? "Activity unavailable" : "No retained activity yet"}
              </p>
              <p className="mt-1 text-[9px] text-muted-foreground">
                Documents, journals, banking and BAS drafts appear here.
              </p>
            </div>
          </div>
        )}
      </section>
      <p className="text-[9px] text-muted-foreground">
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
