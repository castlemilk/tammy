import { create } from "@bufbuild/protobuf";
import {
  type Account,
  AccountStatus,
  AccountType,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { PageRequestSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { LoaderCircle } from "lucide-react";
import { useEffect, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const codec = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 131_072,
  output: ListAccountsResponseSchema,
});

interface ChartScreenProps {
  readonly api: Pick<TammyDesktopAPI, "listAccounts">;
  readonly workspace: AuthenticatedWorkspace | undefined;
}

type ChartState =
  | { readonly status: "loading" }
  | { readonly status: "unavailable" }
  | { readonly accounts: readonly Account[]; readonly status: "ready" };

export function ChartScreen({ api, workspace }: ChartScreenProps) {
  const [chart, setChart] = useState<ChartState>(
    workspace?.organisationId ? { status: "loading" } : { status: "unavailable" },
  );

  useEffect(() => {
    if (!workspace?.organisationId) {
      setChart({ status: "unavailable" });
      return;
    }
    let active = true;
    const request = create(ListAccountsRequestSchema, {
      authentication: {
        actorUserId: workspace.userId,
        sessionId: workspace.sessionId,
      },
      organisationId: workspace.organisationId,
      page: create(PageRequestSchema, { pageSize: 200 }),
    });
    setChart({ status: "loading" });
    void api
      .listAccounts(codec.encodeRequest(request))
      .then((frame) => codec.decodeResponse(frame))
      .then((response) => {
        if (active) setChart({ accounts: response.accounts, status: "ready" });
      })
      .catch(() => {
        if (active) setChart({ status: "unavailable" });
      });
    return () => {
      active = false;
    };
  }, [api, workspace]);

  return (
    <div className="mx-auto grid w-full max-w-[920px] gap-5">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-[18px] font-semibold tracking-[-0.02em] text-foreground">
            Chart of accounts
          </h1>
          <p className="mt-1 text-[11px] leading-5 text-muted-foreground">
            Accounts installed for your Australian business ledger.
          </p>
        </div>
        {chart.status === "ready" ? (
          <p className="text-[10px] font-medium text-muted-foreground">
            {chart.accounts.length} accounts
          </p>
        ) : null}
      </div>

      <section className="overflow-hidden rounded-[6px] border border-border bg-surface">
        {chart.status === "ready" && chart.accounts.length > 0 ? (
          <table className="w-full border-collapse text-left">
            <thead className="border-b border-border bg-background/60 text-[9px] uppercase tracking-[0.05em] text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-semibold">Code</th>
                <th className="px-3 py-2 font-semibold">Account</th>
                <th className="px-3 py-2 font-semibold">Type</th>
                <th className="px-3 py-2 font-semibold">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border text-[10px]">
              {chart.accounts.map((account) => (
                <tr key={account.id}>
                  <td className="w-24 px-3 py-2.5 font-mono font-medium text-foreground">{account.code}</td>
                  <td className="px-3 py-2.5 font-medium text-foreground">{account.name}</td>
                  <td className="px-3 py-2.5 text-muted-foreground">{accountTypeLabel(account.type)}</td>
                  <td className="px-3 py-2.5">
                    <span className="rounded-full bg-success-soft px-2 py-1 text-[9px] font-semibold text-forest">
                      {account.status === AccountStatus.ACTIVE ? "Active" : "Archived"}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <div className="grid min-h-64 place-items-center px-5 py-10 text-center">
            <div>
              {chart.status === "loading" ? (
                <LoaderCircle aria-hidden="true" className="mx-auto size-4 animate-spin text-forest" />
              ) : null}
              <p className="mt-2 text-[12px] font-semibold text-foreground">
                {chart.status === "unavailable" ? "Chart unavailable" : "No accounts yet"}
              </p>
              <p className="mt-1 max-w-sm text-[10px] leading-4 text-muted-foreground">
                {chart.status === "unavailable"
                  ? "The local chart could not be read. Your data remains on this device."
                  : "Accounts will appear here once they are installed."}
              </p>
            </div>
          </div>
        )}
      </section>
    </div>
  );
}

function accountTypeLabel(type: AccountType): string {
  switch (type) {
    case AccountType.ASSET:
      return "Asset";
    case AccountType.LIABILITY:
      return "Liability";
    case AccountType.EQUITY:
      return "Equity";
    case AccountType.REVENUE:
      return "Revenue";
    case AccountType.OTHER_REVENUE:
      return "Other revenue";
    case AccountType.EXPENSE:
      return "Expense";
    case AccountType.OTHER_EXPENSE:
      return "Other expense";
    case AccountType.CONTRA:
      return "Contra";
    default:
      return "Unclassified";
  }
}
