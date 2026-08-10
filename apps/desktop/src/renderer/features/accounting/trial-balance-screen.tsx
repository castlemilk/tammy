import { create } from "@bufbuild/protobuf";
import {
  type GetTrialBalanceResponse,
  GetTrialBalanceRequestSchema,
  GetTrialBalanceResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { CivilDateSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { LoaderCircle } from "lucide-react";
import { useEffect, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const codec = createProtoMethodCodec({ input: GetTrialBalanceRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 524_288, output: GetTrialBalanceResponseSchema });

interface TrialBalanceScreenProps {
  readonly api: Pick<TammyDesktopAPI, "getTrialBalance">;
  readonly now?: Date;
  readonly workspace: AuthenticatedWorkspace | undefined;
}

type State = { readonly status: "loading" } | { readonly status: "unavailable" } | { readonly status: "ready"; readonly value: GetTrialBalanceResponse };

export function TrialBalanceScreen({ api, now, workspace }: TrialBalanceScreenProps) {
  const instant = now ?? new Date();
  const asOf = `${instant.getFullYear()}-${instant.getMonth() + 1}-${instant.getDate()}`;
  const [state, setState] = useState<State>(workspace?.organisationId ? { status: "loading" } : { status: "unavailable" });
  useEffect(() => {
    if (!workspace?.organisationId) { setState({ status: "unavailable" }); return; }
    let active = true;
    const request = create(GetTrialBalanceRequestSchema, {
      authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
      organisationId: workspace.organisationId,
      asOfDate: create(CivilDateSchema, { year: instant.getFullYear(), month: instant.getMonth() + 1, day: instant.getDate() }),
    });
    setState({ status: "loading" });
    void api.getTrialBalance(codec.encodeRequest(request)).then((frame) => codec.decodeResponse(frame)).then((value) => { if (active) setState({ status: "ready", value }); }).catch(() => { if (active) setState({ status: "unavailable" }); });
    return () => { active = false; };
  }, [api, asOf, workspace]);
  return <div className="mx-auto grid w-full max-w-[920px] gap-5"><div><h1 className="text-[18px] font-semibold tracking-[-0.02em] text-foreground">Trial balance</h1><p className="mt-1 text-[11px] leading-5 text-muted-foreground">Debit and credit balances as at {instant.toLocaleDateString("en-AU")}.</p></div><section className="overflow-hidden rounded-[6px] border border-border bg-surface">{state.status === "ready" ? <table className="w-full border-collapse text-left"><thead className="border-b border-border bg-background/60 text-[9px] uppercase text-muted-foreground"><tr><th className="px-3 py-2">Account</th><th className="px-3 py-2 text-right">Debit (AUD)</th><th className="px-3 py-2 text-right">Credit (AUD)</th></tr></thead><tbody className="divide-y divide-border text-[10px]">{state.value.lines.map((line) => <tr key={line.account?.id ?? line.code}><td className="px-3 py-2.5"><span className="mr-3 font-mono text-muted-foreground">{line.code}</span><span className="font-medium">{line.name}</span></td><td className="px-3 py-2.5 text-right font-mono">{moneyLabel(line.debits?.minorUnits ?? 0n)}</td><td className="px-3 py-2.5 text-right font-mono">{moneyLabel(line.credits?.minorUnits ?? 0n)}</td></tr>)}</tbody><tfoot className="border-t border-border text-[10px] font-semibold text-forest"><tr><td className="px-3 py-2.5">Total</td><td className="px-3 py-2.5 text-right font-mono">{moneyLabel(state.value.totalDebits?.minorUnits ?? 0n)}</td><td className="px-3 py-2.5 text-right font-mono">{moneyLabel(state.value.totalCredits?.minorUnits ?? 0n)}</td></tr></tfoot></table> : <div className="grid min-h-64 place-items-center text-center"><div>{state.status === "loading" ? <LoaderCircle className="mx-auto size-4 animate-spin text-forest" /> : null}<p className="mt-2 text-[12px] font-semibold">{state.status === "unavailable" ? "Trial balance unavailable" : "Loading trial balance"}</p><p className="mt-1 text-[10px] text-muted-foreground">Post a journal to establish a financial revision.</p></div></div>}</section></div>;
}

function moneyLabel(minor: bigint): string { const absolute = minor < 0n ? -minor : minor; return `${minor < 0n ? "−" : ""}$${(absolute / 100n).toLocaleString("en-AU")}.${(absolute % 100n).toString().padStart(2, "0")}`; }
