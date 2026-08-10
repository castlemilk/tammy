import { create } from "@bufbuild/protobuf";
import { CivilDateSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  BasAttentionStatus,
  type GetAttentionSummaryResponse,
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
  ReportingPeriodSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import { Building2, Calculator, FileText, LoaderCircle } from "lucide-react";
import { useEffect, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const attentionCodec = createProtoMethodCodec({
  input: GetAttentionSummaryRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 65_536,
  output: GetAttentionSummaryResponseSchema,
});

interface OverviewScreenProps {
  readonly api: Pick<TammyDesktopAPI, "getAttentionSummary">;
  readonly now?: Date;
  readonly workspace: AuthenticatedWorkspace | undefined;
}

type SummaryState =
  | { readonly status: "loading" }
  | { readonly status: "unavailable" }
  | { readonly status: "ready"; readonly value: GetAttentionSummaryResponse };

export function OverviewScreen({ api, now, workspace }: OverviewScreenProps) {
  const instant = now ?? new Date();
  const asOfKey = `${instant.getFullYear()}-${instant.getMonth() + 1}-${instant.getDate()}`;
  const [summary, setSummary] = useState<SummaryState>(
    api && workspace ? { status: "loading" } : { status: "unavailable" },
  );

  useEffect(() => {
    if (!api || !workspace) {
      setSummary({ status: "unavailable" });
      return;
    }
    let active = true;
    const request = attentionRequest(workspace, instant);
    setSummary({ status: "loading" });
    void api
      .getAttentionSummary(attentionCodec.encodeRequest(request))
      .then((frame) => attentionCodec.decodeResponse(frame))
      .then((value) => {
        if (active) setSummary({ status: "ready", value });
      })
      .catch(() => {
        if (active) setSummary({ status: "unavailable" });
      });
    return () => {
      active = false;
    };
  }, [api, asOfKey, workspace]);

  const cards = attentionCards(summary);
  return (
    <div className="mx-auto grid w-full max-w-[920px] gap-5">
      <div>
        <h1 className="text-[18px] font-semibold tracking-[-0.02em] text-foreground">Overview</h1>
        <p className="mt-1 text-[11px] leading-5 text-muted-foreground">
          Your local accounting workspace at a glance.
        </p>
      </div>

      <div className="grid grid-cols-3 gap-3 max-[760px]:grid-cols-1">
        {cards.map((card) => {
          const Icon = card.icon;
          return (
            <section className="rounded-[6px] border border-border bg-surface p-3" key={card.label}>
              <div className="flex items-start gap-2.5">
                <span className="grid size-9 shrink-0 place-items-center rounded-full bg-success-soft text-forest">
                  <Icon aria-hidden="true" className="size-4" strokeWidth={1.7} />
                </span>
                <div className="min-w-0">
                  <h2 className="text-[10px] font-semibold text-foreground">{card.label}</h2>
                  <p className="mt-0.5 text-[17px] font-semibold leading-5 text-foreground">{card.value}</p>
                  <p className="mt-1 text-[9px] leading-4 text-muted-foreground">{card.detail}</p>
                </div>
              </div>
            </section>
          );
        })}
      </div>

      <section className="overflow-hidden rounded-[6px] border border-border bg-surface">
        <div className="border-b border-border px-3 py-2.5">
          <h2 className="text-[11px] font-semibold text-foreground">Needs review</h2>
        </div>
        {summary.status === "ready" && summary.value.attentionItems.length > 0 ? (
          <ul className="divide-y divide-border">
            {summary.value.attentionItems.map((item) => (
              <li className="px-3 py-2.5 text-[10px] text-foreground" key={`${item.kind}-${item.resource?.id ?? item.label}`}>
                {item.label}
              </li>
            ))}
          </ul>
        ) : (
          <div className="grid min-h-36 place-items-center px-5 py-8 text-center">
            <div>
              {summary.status === "loading" ? (
                <LoaderCircle aria-hidden="true" className="mx-auto size-4 animate-spin text-forest" />
              ) : null}
              <p className="mt-2 text-[11px] font-semibold text-foreground">
                {summary.status === "unavailable" ? "Overview unavailable" : "Nothing needs review"}
              </p>
              <p className="mt-1 text-[10px] leading-4 text-muted-foreground">
                {summary.status === "unavailable"
                  ? "The local summary could not be read. Your data remains on this device."
                  : "Your retained local workflows are up to date."}
              </p>
            </div>
          </div>
        )}
      </section>
    </div>
  );
}

function attentionRequest(workspace: AuthenticatedWorkspace, instant: Date) {
  const year = instant.getFullYear();
  const month = instant.getMonth() + 1;
  const day = instant.getDate();
  const quarterStartMonth = Math.floor((month - 1) / 3) * 3 + 1;
  const quarterEndMonth = quarterStartMonth + 2;
  const quarterEndDay = new Date(year, quarterEndMonth, 0).getDate();
  return create(GetAttentionSummaryRequestSchema, {
    authentication: {
      actorUserId: workspace.userId,
      sessionId: workspace.sessionId,
    },
    organisationId: workspace.workspaceId,
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
    case BasAttentionStatus.NOT_CREATED:
      return "No workpaper created";
    default:
      return "Status unavailable";
  }
}
