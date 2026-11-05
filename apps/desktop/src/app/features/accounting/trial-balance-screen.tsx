import { create } from "@bufbuild/protobuf";
import {
  GetTrialBalanceRequestSchema,
  type GetTrialBalanceResponse,
  GetTrialBalanceResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { CivilDateSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { CheckCircle2, LoaderCircle, RefreshCw } from "lucide-react";
import { useMemo } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { HorizontalScrollArea } from "../../components/horizontal-scroll-area";
import { WorkState } from "../../components/review-workspace";
import { Button } from "../../components/ui/button";
import { ScreenHeading } from "../../components/workspace-primitives";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const codec = createProtoMethodCodec({
  input: GetTrialBalanceRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 524_288,
  output: GetTrialBalanceResponseSchema,
});

interface TrialBalanceScreenProps {
  readonly api: Pick<TammyConveyorModule<"accounting">, "getTrialBalance">;
  readonly now?: Date;
  readonly workspace: AuthenticatedWorkspace | undefined;
}

type State =
  | { readonly status: "loading" }
  | { readonly status: "unavailable" }
  | { readonly status: "ready"; readonly value: GetTrialBalanceResponse };

export function TrialBalanceScreen({ api, now, workspace }: TrialBalanceScreenProps) {
  const instant = now ?? new Date();
  const year = instant.getFullYear();
  const month = instant.getMonth() + 1;
  const day = instant.getDate();
  const organisationId = workspace?.organisationId ?? "";
  const userId = workspace?.userId ?? "";
  const sessionId = workspace?.sessionId ?? "";
  const input = useMemo(
    () =>
      codec.encodeRequest(
        create(GetTrialBalanceRequestSchema, {
          authentication: { actorUserId: userId, sessionId },
          organisationId,
          asOfDate: create(CivilDateSchema, { year, month, day }),
        }),
      ),
    [day, month, organisationId, sessionId, userId, year],
  );
  const trialBalance = api.getTrialBalance.useQuery({
    enabled: organisationId.length > 0,
    input,
    retry: false,
  });
  const state = useMemo<State>(() => {
    if (!organisationId || trialBalance.isError) return { status: "unavailable" };
    if (!trialBalance.data) return { status: "loading" };
    try {
      return { status: "ready", value: codec.decodeResponse(trialBalance.data) };
    } catch {
      return { status: "unavailable" };
    }
  }, [organisationId, trialBalance.data, trialBalance.isError]);
  const dateLabel = instant.toLocaleDateString("en-AU", {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
  const civilDate = `${year}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
  const totalsAvailable =
    state.status === "ready" &&
    state.value.totalDebits !== undefined &&
    state.value.totalCredits !== undefined;
  const equalTotals =
    totalsAvailable &&
    state.status === "ready" &&
    state.value.totalDebits?.minorUnits === state.value.totalCredits?.minorUnits;
  const refresh = (
    <Button
      aria-label="Refresh trial balance"
      title="Refresh trial balance"
      className="size-11 p-0"
      disabled={!organisationId || trialBalance.isFetching}
      onClick={() => void trialBalance.refetch()}
      type="button"
      variant="ghost"
    >
      {trialBalance.isFetching ? (
        <LoaderCircle aria-hidden="true" className="size-4 animate-spin" />
      ) : (
        <RefreshCw aria-hidden="true" className="size-4" />
      )}
    </Button>
  );
  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-4">
      <ScreenHeading
        area="Accounting · Reports"
        title="Trial balance"
        description="Debit and credit balances in Australian dollars."
        action={
          <>
            <div className="mr-2 text-right">
              <p className="text-[12px] text-muted-foreground">As at</p>
              <time className="text-[14px] font-medium tabular-nums" dateTime={civilDate}>
                {dateLabel}
              </time>
            </div>
            {refresh}
          </>
        }
      />
      <section
        aria-label="Trial balance report"
        aria-busy={trialBalance.isFetching}
        className="work-surface overflow-hidden"
      >
        {state.status === "ready" ? (
          <>
            <HorizontalScrollArea label="Account balances table" className="trial-balance-scroll">
              <table className="work-table trial-balance-table min-w-[640px]">
                <caption className="sr-only">
                  Account balances as at {dateLabel}, in Australian dollars.
                </caption>
                <colgroup>
                  <col />
                  <col className="w-[22%]" />
                  <col className="w-[22%]" />
                </colgroup>
                <thead className="border-b border-border bg-background/60">
                  <tr>
                    <th scope="col">Account</th>
                    <th scope="col" className="text-right">
                      Debit (AUD)
                    </th>
                    <th scope="col" className="text-right">
                      Credit (AUD)
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {state.value.lines.map((line) => (
                    <tr key={line.account?.id ?? line.code}>
                      <th scope="row" className="text-left">
                        <span className="mr-3 inline-block min-w-[4ch] font-mono text-muted-foreground">
                          {line.code}
                        </span>
                        <span className="font-medium text-foreground">{line.name}</span>
                      </th>
                      <td className="whitespace-nowrap text-right tabular-nums">
                        {moneyLabel(line.debits?.minorUnits ?? 0n)}
                      </td>
                      <td className="whitespace-nowrap text-right tabular-nums">
                        {moneyLabel(line.credits?.minorUnits ?? 0n)}
                      </td>
                    </tr>
                  ))}
                </tbody>
                <tfoot
                  className={
                    equalTotals
                      ? "bg-forest-soft text-forest"
                      : totalsAvailable
                        ? "bg-warning-soft"
                        : "bg-muted"
                  }
                >
                  <tr>
                    <th scope="row" className="text-left">
                      <span>Total</span>
                      <span className="ml-3 inline-flex items-center gap-1.5 text-[12px] font-normal">
                        {equalTotals ? (
                          <CheckCircle2 aria-hidden="true" className="size-3.5 shrink-0" />
                        ) : null}
                        {!totalsAvailable
                          ? "Totals unavailable"
                          : equalTotals
                            ? "Debits equal credits"
                            : "Debits and credits differ"}
                      </span>
                    </th>
                    <td className="whitespace-nowrap text-right font-semibold tabular-nums">
                      {moneyLabel(state.value.totalDebits?.minorUnits ?? 0n)}
                    </td>
                    <td className="whitespace-nowrap text-right font-semibold tabular-nums">
                      {moneyLabel(state.value.totalCredits?.minorUnits ?? 0n)}
                    </td>
                  </tr>
                </tfoot>
              </table>
            </HorizontalScrollArea>
            {state.value.lines.length === 0 ? (
              <WorkState
                title="No account balances"
                description="There are no account balances in this report for the as-of date."
              />
            ) : null}
          </>
        ) : (
          <WorkState
            loading={state.status === "loading"}
            title={
              state.status === "unavailable" ? "Trial balance unavailable" : "Loading trial balance"
            }
            description={
              state.status === "unavailable"
                ? organisationId
                  ? "The report could not be loaded. Refresh to try again."
                  : "Open an organisation to view its trial balance."
                : "Reading the local account balances for the as-of date."
            }
          />
        )}
      </section>
    </div>
  );
}

function moneyLabel(minor: bigint): string {
  const absolute = minor < 0n ? -minor : minor;
  return `${minor < 0n ? "−" : ""}$${(absolute / 100n).toLocaleString("en-AU")}.${(absolute % 100n).toString().padStart(2, "0")}`;
}
