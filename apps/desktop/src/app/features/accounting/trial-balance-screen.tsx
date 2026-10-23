import { create } from "@bufbuild/protobuf";
import {
  GetTrialBalanceRequestSchema,
  type GetTrialBalanceResponse,
  GetTrialBalanceResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { CivilDateSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { LoaderCircle } from "lucide-react";
import { useMemo } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { HorizontalScrollArea } from "../../components/horizontal-scroll-area";
import { ScreenHeading, SurfaceHeading } from "../../components/workspace-primitives";
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
  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-6">
      <ScreenHeading
        area="Accounting · Reports"
        title="Trial balance"
        description={`Debit and credit balances as at ${instant.toLocaleDateString("en-AU")}.`}
      />
      <section className="work-surface overflow-hidden">
        <SurfaceHeading
          title="Account balances"
          description="Debits and credits in Australian dollars."
        />
        {state.status === "ready" ? (
          <HorizontalScrollArea label="Account balances table">
            <table className="w-full border-collapse text-left">
              <thead className="border-b border-border bg-background/60 text-[12px] uppercase tracking-[0.06em] text-muted-foreground">
                <tr>
                  <th className="px-5 py-3">Account</th>
                  <th className="px-5 py-3 text-right">Debit (AUD)</th>
                  <th className="px-5 py-3 text-right">Credit (AUD)</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border text-[14px]">
                {state.value.lines.map((line) => (
                  <tr key={line.account?.id ?? line.code}>
                    <td className="px-5 py-3.5">
                      <span className="mr-3 font-mono text-muted-foreground">{line.code}</span>
                      <span className="font-medium">{line.name}</span>
                    </td>
                    <td className="px-5 py-3.5 text-right font-mono tabular-nums">
                      {moneyLabel(line.debits?.minorUnits ?? 0n)}
                    </td>
                    <td className="px-5 py-3.5 text-right font-mono tabular-nums">
                      {moneyLabel(line.credits?.minorUnits ?? 0n)}
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot className="border-t border-border text-[14px] font-semibold text-forest">
                <tr>
                  <td className="px-5 py-3.5">Total</td>
                  <td className="px-5 py-3.5 text-right font-mono tabular-nums">
                    {moneyLabel(state.value.totalDebits?.minorUnits ?? 0n)}
                  </td>
                  <td className="px-5 py-3.5 text-right font-mono tabular-nums">
                    {moneyLabel(state.value.totalCredits?.minorUnits ?? 0n)}
                  </td>
                </tr>
              </tfoot>
            </table>
          </HorizontalScrollArea>
        ) : (
          <div className="grid min-h-64 place-items-center text-center">
            <div>
              {state.status === "loading" ? (
                <LoaderCircle className="mx-auto size-4 animate-spin text-forest" />
              ) : null}
              <p className="mt-2 text-[14px] font-semibold">
                {state.status === "unavailable"
                  ? "Trial balance unavailable"
                  : "Loading trial balance"}
              </p>
              <p className="mt-1 text-[14px] text-muted-foreground">
                Post a journal to establish a financial revision.
              </p>
            </div>
          </div>
        )}
      </section>
    </div>
  );
}

function moneyLabel(minor: bigint): string {
  const absolute = minor < 0n ? -minor : minor;
  return `${minor < 0n ? "−" : ""}$${(absolute / 100n).toLocaleString("en-AU")}.${(absolute % 100n).toString().padStart(2, "0")}`;
}
