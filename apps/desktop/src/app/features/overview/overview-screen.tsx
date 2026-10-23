import { create } from "@bufbuild/protobuf";
import { CivilDateSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  BasAttentionStatus,
  GetAttentionSummaryRequestSchema,
  type GetAttentionSummaryResponse,
  GetAttentionSummaryResponseSchema,
  ReportingPeriodSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import { Building2, Calculator, FileText, LoaderCircle } from "lucide-react";
import { useMemo } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import paperLoop from "../../assets/paper-loop.png";
import { ScreenHeading, SurfaceHeading } from "../../components/workspace-primitives";
import type { ReportOrganisationApi } from "../reporting/report-export-actions";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { BusinessIncomePanel } from "./business-income-panel";

const attentionCodec = createProtoMethodCodec({
  input: GetAttentionSummaryRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 65_536,
  output: GetAttentionSummaryResponseSchema,
});

interface OverviewScreenProps {
  readonly api: Pick<TammyConveyorModule<"reporting">, "getAttentionSummary"> &
    Partial<Pick<TammyConveyorModule<"reporting">, "exportIncome">>;
  readonly organisationApi?: ReportOrganisationApi;
  readonly accountingApi?: Pick<TammyConveyorModule<"accounting">, "getBusinessIncomeSummary">;
  readonly onOpenBas?: (path: string) => void;
  readonly onReviewInvoices?: () => void;
  readonly now?: Date;
  readonly workspace: AuthenticatedWorkspace | undefined;
}

type SummaryState =
  | { readonly status: "loading" }
  | { readonly status: "unavailable" }
  | { readonly status: "ready"; readonly value: GetAttentionSummaryResponse };

export function OverviewScreen({
  api,
  accountingApi,
  organisationApi,
  now,
  onOpenBas,
  onReviewInvoices,
  workspace,
}: OverviewScreenProps) {
  const instant = now ?? new Date();
  const year = instant.getFullYear();
  const month = instant.getMonth();
  const day = instant.getDate();
  const workspaceId = workspace?.workspaceId ?? "";
  const organisationId = workspace?.organisationId ?? workspaceId;
  const userId = workspace?.userId ?? "";
  const sessionId = workspace?.sessionId ?? "";
  const input = useMemo(
    () =>
      attentionCodec.encodeRequest(
        attentionRequest({ organisationId, sessionId, userId }, new Date(year, month, day)),
      ),
    [day, month, organisationId, sessionId, userId, year],
  );
  const attention = api.getAttentionSummary.useQuery({
    enabled: workspaceId.length > 0,
    input,
    retry: false,
  });
  const summary = useMemo<SummaryState>(() => {
    if (workspaceId.length === 0 || attention.isError) return { status: "unavailable" };
    if (!attention.data) return { status: "loading" };
    try {
      return { status: "ready", value: attentionCodec.decodeResponse(attention.data) };
    } catch {
      return { status: "unavailable" };
    }
  }, [attention.data, attention.isError, workspaceId]);

  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-6">
      <div className="flex min-w-0 items-center justify-between gap-6">
        <ScreenHeading
          area={new Intl.DateTimeFormat("en-AU", {
            weekday: "long",
            day: "numeric",
            month: "long",
            year: "numeric",
          }).format(new Date(year, month, day))}
          title="Overview"
          description="A little clarity for your day. Your income and the next things to review."
        />
        <img
          alt=""
          aria-hidden="true"
          className="h-24 w-32 shrink-0 object-contain max-[700px]:hidden"
          src={paperLoop}
        />
      </div>
      <div
        className="grid min-w-0 items-start gap-6 min-[1100px]:grid-cols-[minmax(0,1fr)_340px]"
        data-testid="overview-workspace"
      >
        {accountingApi ? (
          <BusinessIncomePanel
            api={accountingApi}
            {...(api.exportIncome ? { exportApi: { exportIncome: api.exportIncome } } : {})}
            {...(organisationApi ? { organisationApi } : {})}
            {...(now ? { now } : {})}
            {...(onReviewInvoices ? { onReviewInvoices } : {})}
            workspace={workspace}
          />
        ) : null}
        <OverviewAttention summary={summary} onOpenBas={onOpenBas} />
      </div>
    </div>
  );
}

function OverviewAttention({
  summary,
  onOpenBas,
}: {
  readonly summary: SummaryState;
  readonly onOpenBas: ((path: string) => void) | undefined;
}) {
  const cards = attentionCards(summary);
  const basCard = cards[2];
  const target =
    summary.status === "ready" ? basPreparationPath(summary.value.basPreparation) : undefined;
  return (
    <section
      className="work-surface min-w-0 overflow-hidden"
      aria-label="Local attention"
      aria-busy={summary.status === "loading"}
    >
      <SurfaceHeading
        title="Needs review"
        description="From your local documents, banking and GST workflows."
      />
      <dl className="m-0 divide-y divide-border px-5">
        {cards.slice(0, 2).map((card) => {
          const Icon = card.icon;
          return (
            <div className="flex min-w-0 items-center justify-between gap-4 py-4" key={card.label}>
              <dt className="min-w-0">
                <span className="flex items-center gap-2 text-[14px] font-semibold">
                  <Icon
                    aria-hidden="true"
                    className="size-4 shrink-0 text-forest"
                    strokeWidth={1.7}
                  />
                  {card.label}
                </span>
                <span className="mt-1 block text-[12px] leading-5 text-muted-foreground">
                  {card.detail}
                </span>
              </dt>
              <dd className="m-0 min-w-10 rounded-lg bg-muted px-2 py-1 text-center text-[22px] font-semibold leading-7 tabular-nums">
                {card.value}
              </dd>
            </div>
          );
        })}
      </dl>
      <div className="grid min-w-0 gap-2 border-t border-border bg-muted p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="flex items-center gap-2 text-[14px] font-semibold">
            <Calculator aria-hidden="true" className="size-4 text-forest" strokeWidth={1.7} />
            GST &amp; BAS
          </h3>
          <span className="text-[12px] font-semibold">{basCard?.value}</span>
        </div>
        <p className="text-[14px] leading-6 text-muted-foreground">
          {summary.status === "ready"
            ? basAttentionDetail(summary.value.basStatus)
            : "BAS status unavailable"}
        </p>
        {summary.status === "ready" && onOpenBas ? (
          target ? (
            <a
              aria-label={`Open exact BAS preparation ${summary.value.basPreparation?.id}`}
              className="focus-ring inline-flex min-h-11 min-w-0 w-fit items-center rounded-xl text-[14px] font-semibold text-forest underline underline-offset-4 [overflow-wrap:anywhere]"
              href={target}
              onClick={(event) => {
                event.preventDefault();
                onOpenBas(target);
              }}
            >
              Open exact preparation · {summary.value.basPreparation?.id}
            </a>
          ) : (
            <button
              className="focus-ring inline-flex min-h-11 w-fit items-center rounded-xl text-left text-[14px] font-semibold text-forest underline underline-offset-4"
              onClick={() => onOpenBas("/gst-bas")}
              type="button"
            >
              Open BAS preparation
            </button>
          )
        ) : null}
      </div>
      {summary.status === "ready" && summary.value.attentionItems.length > 0 ? (
        <ul
          className="divide-y divide-border border-t border-border"
          aria-label="Local attention items"
        >
          {summary.value.attentionItems.map((item) => (
            <li
              className="min-h-14 break-words px-5 py-3 text-[14px] leading-6 text-foreground"
              key={`${item.kind}-${item.resource?.id ?? item.label}`}
            >
              {item.label}
            </li>
          ))}
        </ul>
      ) : (
        <div className="border-t border-border px-5 py-5" role="status">
          {summary.status === "loading" ? (
            <LoaderCircle aria-hidden="true" className="mb-2 size-4 animate-spin text-forest" />
          ) : null}
          <p className="text-[14px] font-semibold text-foreground">
            {summary.status === "loading"
              ? "Reading your local summary…"
              : summary.status === "unavailable"
                ? "Attention summary unavailable"
                : "No additional review items"}
          </p>
          <p className="mt-1 text-[12px] leading-5 text-muted-foreground">
            {summary.status === "unavailable"
              ? "The local attention summary could not be read. Your data remains on this device."
              : summary.status === "loading"
                ? "Checking documents, banking and GST."
                : "Document and bank counts are shown above. Check the separate BAS status before continuing."}
          </p>
        </div>
      )}
    </section>
  );
}

function attentionRequest(
  authority: {
    readonly organisationId: string;
    readonly sessionId: string;
    readonly userId: string;
  },
  instant: Date,
) {
  const year = instant.getFullYear();
  const month = instant.getMonth() + 1;
  const day = instant.getDate();
  const quarterStartMonth = Math.floor((month - 1) / 3) * 3 + 1;
  const quarterEndMonth = quarterStartMonth + 2;
  const quarterEndDay = new Date(year, quarterEndMonth, 0).getDate();
  return create(GetAttentionSummaryRequestSchema, {
    authentication: {
      actorUserId: authority.userId,
      sessionId: authority.sessionId,
    },
    organisationId: authority.organisationId,
    asOfDate: create(CivilDateSchema, { year, month, day }),
    reportingPeriod: create(ReportingPeriodSchema, {
      startDate: create(CivilDateSchema, { year, month: quarterStartMonth, day: 1 }),
      endDate: create(CivilDateSchema, { year, month: quarterEndMonth, day: quarterEndDay }),
    }),
  });
}

function attentionCards(summary: SummaryState) {
  if (summary.status !== "ready") {
    const value = summary.status === "loading" ? "…" : "—";
    return [
      { detail: "Local summary pending", icon: FileText, label: "Documents", value },
      { detail: "Local summary pending", icon: Building2, label: "Banking", value },
      { detail: "Local summary pending", icon: Calculator, label: "GST & BAS", value },
    ] as const;
  }
  const value = summary.value;
  return [
    {
      detail: `${value.documentsReviewedInPeriod} reviewed this quarter`,
      icon: FileText,
      label: "Documents",
      value: String(value.documentsNeedingReview),
    },
    {
      detail: `${value.bankingLinesUnreconciledInPeriod} unreconciled this quarter`,
      icon: Building2,
      label: "Banking",
      value: String(value.bankingLinesNeedingReconciliation),
    },
    {
      detail: basStatusLabel(value.basStatus),
      icon: Calculator,
      label: "GST & BAS",
      value: value.currentDraftBasWorkpapers > 0 ? "Draft" : "—",
    },
  ] as const;
}

function basStatusLabel(status: BasAttentionStatus): string {
  switch (status) {
    case BasAttentionStatus.DRAFT_NOT_LODGED:
      return "Draft — not lodged";
    case BasAttentionStatus.OUTDATED:
      return "Draft — source changes detected";
    case BasAttentionStatus.ACTION_NEEDED:
      return "Action needed · review this exact BAS preparation";
    case BasAttentionStatus.REVIEW_READY:
      return "Review ready · awaiting Business Lodger approval";
    case BasAttentionStatus.FROZEN_FOR_MANUAL_HANDOFF:
      return "Frozen for manual handoff · not lodged";
    case BasAttentionStatus.EXTERNAL_CONFIRMATION_RECORDED:
      return "User-recorded external status · unverified";
    case BasAttentionStatus.DISPUTED:
      return "User-recorded status disputed · review required";
    case BasAttentionStatus.NOT_CREATED:
      return "No BAS preparation yet";
    default:
      return "Status unavailable";
  }
}

function basAttentionDetail(status: BasAttentionStatus) {
  return basStatusLabel(status);
}

function basPreparationPath(
  ref: GetAttentionSummaryResponse["basPreparation"],
): string | undefined {
  if (
    !ref ||
    ref.type !== "bas_preparation" ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(ref.id) ||
    ref.revision < 1n ||
    ref.contentHash.byteLength !== 32
  )
    return undefined;
  const hash = [...ref.contentHash].map((byte) => byte.toString(16).padStart(2, "0")).join("");
  return `/gst-bas?preparation=${ref.id}&revision=${ref.revision}&hash=${hash}`;
}
