import { create } from "@bufbuild/protobuf";
import {
  JournalSource,
  JournalState,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  BankStatementLineStatus,
  ListBankStatementLinesRequestSchema,
  ListBankStatementLinesResponseSchema,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
import { PageRequestSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentStatus,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  BasWorkpaperStatus,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { BookOpenCheck, Calculator, ChevronRight, FileText, Info, Landmark } from "lucide-react";
import { useId, useMemo, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { WorkNotice, WorkSearch, WorkState } from "../../components/review-workspace";
import { ScreenHeading } from "../../components/workspace-primitives";
import { aud } from "../documents/purchase-command-state";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const recordKinds = ["Journal", "Banking", "Document", "GST & BAS"] as const;
type RecordKind = (typeof recordKinds)[number];
interface Activity {
  readonly at: number;
  readonly dateLabel: string;
  readonly detail: string;
  readonly id: string;
  readonly kind: RecordKind;
  readonly label: string;
  readonly status: string;
  readonly version: string;
  readonly sourceDetails: readonly (readonly [string, string])[];
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
  const [kind, setKind] = useState("All");
  const [search, setSearch] = useState("");
  const projection = useMemo(() => {
    if (!enabled || pending) return undefined;
    const items: Activity[] = [];
    const missing = new Set<string>();
    for (const [name, query] of [
      ["Journals", journals.isError],
      ["Banking", bankLines.isError],
      ["Documents", documentList.isError],
      ["GST workpaper", currentBas.isError],
    ] as const)
      if (query) missing.add(name);
    try {
      if (journals.data)
        for (const journal of journalsCodec.decodeResponse(journals.data).journals)
          items.push({
            at: civilTime(journal.postingDate),
            dateLabel: "Posting date",
            detail: `${money(journal.totalDebits?.minorUnits)} debits and credits`,
            id: journal.id,
            kind: "Journal",
            label: journal.memo || "Journal record",
            status:
              journal.state === JournalState.POSTED
                ? "Posted"
                : journal.state === JournalState.REVERSED
                  ? "Reversed"
                  : "Unknown status",
            version: journal.version.toString(),
            sourceDetails: [
              ["Journal source", enumLabel(JournalSource[journal.source], journal.source)],
            ],
          });
    } catch {
      missing.add("Journals");
    }
    try {
      if (bankLines.data)
        for (const line of bankingCodec.decodeResponse(bankLines.data).lines)
          items.push({
            at: civilTime(line.transactionDate),
            dateLabel: "Transaction date",
            detail: money(line.amount?.minorUnits),
            id: line.id,
            kind: "Banking",
            label: line.description || "Statement line",
            status: statusLabel(line.status),
            version: line.version.toString(),
            sourceDetails: [
              ["Statement import ID", line.statementImportId || "Not supplied"],
              ["Recorded match reference", line.matchReference || "No reference recorded"],
            ],
          });
    } catch {
      missing.add("Banking");
    }
    try {
      if (documentList.data)
        for (const document of documentsCodec.decodeResponse(documentList.data).documents)
          items.push({
            at: document.createdAt ? Number(document.createdAt.seconds) * 1000 : 0,
            dateLabel: "Added to workspace",
            detail: document.candidate?.invoiceNumber || "No invoice number",
            id: document.id,
            kind: "Document",
            label:
              document.candidate?.supplierName || document.sourceDisplayName || "Document record",
            status:
              document.status === DocumentStatus.REVIEWED
                ? "Reviewed"
                : document.status === DocumentStatus.NEEDS_REVIEW
                  ? "Needs review"
                  : "Unknown status",
            version: document.version.toString(),
            sourceDetails: [["Source file", document.sourceDisplayName || "Not supplied"]],
          });
    } catch {
      missing.add("Documents");
    }
    try {
      if (currentBas.data) {
        const workpaper = basCodec.decodeResponse(currentBas.data).workpaper;
        if (workpaper)
          items.push({
            at: workpaper.createdAt ? Number(workpaper.createdAt.seconds) * 1000 : 0,
            dateLabel: "Workpaper created",
            detail: `GST credits ${money(workpaper.gstCredits1b?.minorUnits)}`,
            id: workpaper.id,
            kind: "GST & BAS",
            label: `${civilLabel(workpaper.periodStart)} – ${civilLabel(workpaper.periodEnd)}`,
            status:
              workpaper.status === BasWorkpaperStatus.DRAFT_NOT_LODGED
                ? "Draft — not lodged"
                : "Unknown status",
            version: workpaper.version.toString(),
            sourceDetails: [
              ["Period start", civilLabel(workpaper.periodStart)],
              ["Period end", civilLabel(workpaper.periodEnd)],
            ],
          });
      }
    } catch {
      missing.add("GST workpaper");
    }
    return {
      items: items.sort((left, right) => right.at - left.at || left.id.localeCompare(right.id)),
      missing: [...missing],
    };
  }, [
    bankLines.data,
    bankLines.isError,
    currentBas.data,
    currentBas.isError,
    documentList.data,
    documentList.isError,
    enabled,
    journals.data,
    journals.isError,
    pending,
  ]);
  const term = search.trim().toLocaleLowerCase("en-AU");
  const visible = projection?.items.filter(
    (item) =>
      (kind === "All" || item.kind === kind) &&
      (!term ||
        `${item.label} ${item.kind} ${item.detail} ${item.status}`
          .toLocaleLowerCase("en-AU")
          .includes(term)),
  );
  const unavailable =
    !enabled || (projection?.items.length === 0 && projection.missing.length === 4);
  return (
    <div className="audit-workspace mx-auto grid w-full max-w-[1120px] gap-4">
      <ScreenHeading
        area="Business records · Local activity"
        title="Audit trail"
        description="A clear view of your retained business records."
      />
      <aside
        aria-label="About this activity view"
        className="flex items-start gap-3 rounded-xl border border-warning-border bg-warning-soft px-4 py-3 text-[13px] leading-5"
      >
        <Info aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-warning" />
        <div className="min-w-0">
          <p>
            <strong>Activity view.</strong> A summary of loaded records, not a full history of
            actions.
          </p>
          <details className="mt-1">
            <summary className="focus-ring w-fit cursor-pointer rounded text-[12px] font-medium">
              What this view includes
            </summary>
            <p className="mt-2 max-w-3xl text-[12px] leading-5">
              Up to 200 journals, bank statement lines and documents from each source, plus the
              current GST workpaper. Dates are each record’s posting, transaction or creation date;
              they are not action timestamps. Actors and earlier record versions are not supplied by
              this view. This product view does not replace exported audit-chain verification
              evidence.
            </p>
          </details>
        </div>
      </aside>
      {projection?.missing.length ? (
        <WorkNotice>
          <p className="font-semibold">Some records could not be loaded</p>
          <p className="text-[13px]">
            Unavailable sources: {projection.missing.join(", ")}. Available records remain visible;
            this view may be incomplete.
          </p>
        </WorkNotice>
      ) : null}
      <section className="work-surface overflow-hidden" aria-label="Retained activity">
        <div className="grid gap-3 border-b border-border px-4 py-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-[16px] font-semibold">Retained records</h2>
            <p className="text-[12px] tabular-nums text-muted-foreground" role="status">
              {projection
                ? `${visible?.length ?? 0} of ${projection.items.length} loaded records`
                : pending
                  ? "Loading retained records…"
                  : "Records unavailable"}
            </p>
          </div>
          <div className="flex items-center gap-3 max-[650px]:flex-wrap">
            <WorkSearch
              label="Search loaded activity"
              clearLabel="Clear activity search"
              value={search}
              onChange={setSearch}
            />
            <label className="grid min-w-0 gap-1 text-[12px] font-medium max-[650px]:w-full">
              <span className="sr-only">Filter loaded activity by record type</span>
              <select
                className="work-input h-11 w-[200px] max-w-full text-[14px] max-[650px]:w-full"
                value={kind}
                onChange={(event) => setKind(event.target.value)}
              >
                <option value="All">All record types</option>
                {recordKinds.map((value) => (
                  <option value={value} key={value}>
                    {value === "Banking"
                      ? "Bank statement lines"
                      : value === "GST & BAS"
                        ? "GST workpaper"
                        : value === "Journal"
                          ? "Journals"
                          : "Documents"}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </div>
        {pending ? (
          <WorkState
            loading
            title="Loading retained records"
            description="Reading the local records available for this workspace."
          />
        ) : unavailable ? (
          <WorkState
            title="Activity unavailable"
            description="These local records could not be read. Return to the workspace and try again."
          />
        ) : !projection?.items.length ? (
          <WorkState
            title={
              projection?.missing.length
                ? "No records available from loaded sources"
                : "No retained activity yet"
            }
            description={
              projection?.missing.length
                ? "Unavailable sources may contain records. This is not confirmation of an empty history."
                : "Documents, journals, bank statement lines and GST workpapers appear here."
            }
          />
        ) : !visible?.length ? (
          <WorkState
            title="No matching loaded records"
            description="Try another record type or clear your search. Your retained records are unchanged."
          />
        ) : (
          <ol aria-label="Retained activity records" className="divide-y divide-border">
            {visible.map((activity) => (
              <ActivityRecord key={`${activity.kind}-${activity.id}`} activity={activity} />
            ))}
          </ol>
        )}
      </section>
    </div>
  );
}

function ActivityRecord({ activity }: { readonly activity: Activity }) {
  const id = useId();
  const Icon =
    activity.kind === "Journal"
      ? BookOpenCheck
      : activity.kind === "Banking"
        ? Landmark
        : activity.kind === "Document"
          ? FileText
          : Calculator;
  const date =
    activity.at > 0 ? new Date(activity.at).toLocaleDateString("en-AU") : "Date unavailable";
  const quiet = ["Posted", "Reviewed", "Reconciled", "Matched"].includes(activity.status);
  return (
    <li className="audit-record">
      <details className="group">
        <summary
          aria-label={`Record details: ${activity.kind} — ${activity.label}`}
          aria-describedby={`${id}-date ${id}-detail ${id}-status`}
          className="audit-summary focus-ring grid min-h-[76px] cursor-pointer list-none grid-cols-[92px_32px_minmax(0,1fr)_auto_16px] items-center gap-3 px-4 py-3 [&::-webkit-details-marker]:hidden max-[700px]:grid-cols-[28px_minmax(0,1fr)_16px] max-[700px]:gap-y-1.5"
        >
          <span
            className="text-[12px] tabular-nums text-muted-foreground max-[700px]:col-start-2 max-[700px]:row-start-2"
            id={`${id}-date`}
          >
            {activity.at > 0 ? (
              <time dateTime={new Date(activity.at).toISOString()}>{date}</time>
            ) : (
              date
            )}
          </span>
          <span className="grid size-8 place-items-center rounded-lg bg-forest-soft text-forest max-[700px]:col-start-1 max-[700px]:row-start-1">
            <Icon aria-hidden="true" className="size-4" />
          </span>
          <span className="min-w-0 max-[700px]:col-start-2 max-[700px]:row-start-1">
            <span className="block text-[11px] font-semibold text-muted-foreground">
              {activity.kind === "Banking" ? "Bank statement line" : activity.kind}
            </span>
            <span className="block text-[14px] font-semibold leading-5">{activity.label}</span>
            <span
              className="mt-0.5 block text-[13px] tabular-nums leading-5 text-muted-foreground"
              id={`${id}-detail`}
            >
              {activity.detail}
            </span>
          </span>
          <span
            className={`text-[12px] font-medium max-[700px]:col-start-2 max-[700px]:row-start-3 ${quiet ? "text-forest" : "text-muted-foreground"}`}
            id={`${id}-status`}
          >
            {activity.status}
          </span>
          <ChevronRight
            aria-hidden="true"
            className="size-4 text-muted-foreground group-open:rotate-90 max-[700px]:col-start-3 max-[700px]:row-start-1"
          />
        </summary>
        <div className="border-t border-border bg-background/50 px-4 py-4">
          <h3 className="mb-3 text-[13px] font-semibold">Retained record details</h3>
          <dl className="grid grid-cols-[160px_minmax(0,1fr)] gap-x-4 gap-y-2 text-[12px] leading-5 max-[650px]:grid-cols-1">
            {[
              ["Record ID", activity.id],
              ["Record version", activity.version],
              [activity.dateLabel, date],
              ["Current record status", activity.status],
              ...activity.sourceDetails,
            ].map(([label, value]) => (
              <div className="contents" key={label}>
                <dt className="text-muted-foreground">{label}</dt>
                <dd className="min-w-0 break-all">{value}</dd>
              </div>
            ))}
          </dl>
        </div>
      </details>
    </li>
  );
}

function enumLabel(value: string | undefined, code: number) {
  return value
    ? value
        .toLowerCase()
        .replaceAll("_", " ")
        .replace(/^./, (letter) => letter.toUpperCase())
    : `Unknown source (${code})`;
}

function money(value: bigint | undefined): string {
  return aud(value);
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
  return status === BankStatementLineStatus.RECONCILED
    ? "Reconciled"
    : status === BankStatementLineStatus.MATCHED
      ? "Matched"
      : status === BankStatementLineStatus.UNMATCHED
        ? "Unmatched"
        : "Unknown status";
}
