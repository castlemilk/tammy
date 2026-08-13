import { create } from "@bufbuild/protobuf";
import {
  type Account,
  AccountDesignation,
  AccountStatus,
  AccountType,
  CreateAccountRequestSchema,
  CreateAccountResponseSchema,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  NormalBalance,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  CommandContextSchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { LoaderCircle, Plus } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const codec = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 131_072,
  output: ListAccountsResponseSchema,
});
const createAccountCodec = createProtoMethodCodec({
  input: CreateAccountRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: CreateAccountResponseSchema,
});

interface ChartScreenProps {
  readonly api: Pick<TammyDesktopAPI, "createAccount" | "listAccounts">;
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
  const [adding, setAdding] = useState(false);
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [type, setType] = useState<AccountType>(AccountType.EXPENSE);
  const [saving, setSaving] = useState(false);
  const [reload, setReload] = useState(0);
  const [error, setError] = useState<string>();

  useEffect(() => {
    void reload;
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
  }, [api, reload, workspace]);

  const addAccount = async (event: FormEvent) => {
    event.preventDefault();
    if (!workspace?.organisationId) return;
    setSaving(true);
    setError(undefined);
    try {
      const request = create(CreateAccountRequestSchema, {
        commandContext: create(CommandContextSchema, {
          idempotencyKey: uuidV7(),
          authentication: {
            actorUserId: workspace.userId,
            sessionId: workspace.sessionId,
          },
        }),
        organisationId: workspace.organisationId,
        code,
        name,
        type,
        normalBalance: normalBalanceFor(type),
        reportClassification: "custom.account",
        cashFlowClassification: "noncash",
      });
      const response = createAccountCodec.decodeResponse(
        await api.createAccount(createAccountCodec.encodeRequest(request)),
      );
      if (!response.account || response.account.designation !== AccountDesignation.ORDINARY) {
        throw new Error("invalid account");
      }
      setCode("");
      setName("");
      setAdding(false);
      setReload((value) => value + 1);
    } catch {
      setError("The account could not be created. Check the code and try again.");
    } finally {
      setSaving(false);
    }
  };

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
        <div className="flex items-center gap-3">
          {chart.status === "ready" ? (
            <p className="text-[10px] font-medium text-muted-foreground">
              {chart.accounts.length} accounts
            </p>
          ) : null}
          <Button
            className="h-8 text-[10px]"
            onClick={() => setAdding((value) => !value)}
            type="button"
          >
            <Plus aria-hidden="true" className="size-3" /> Add account
          </Button>
        </div>
      </div>

      {adding ? (
        <form
          className="grid grid-cols-[120px_minmax(0,1fr)_160px_auto] gap-3 rounded-[6px] border border-border bg-surface p-3 max-[760px]:grid-cols-1"
          onSubmit={addAccount}
        >
          <label className="grid gap-1 text-[10px] font-medium text-foreground">
            Code
            <input
              className={fieldClassName()}
              maxLength={32}
              onChange={(event) => setCode(event.target.value.toUpperCase())}
              required
              value={code}
            />
          </label>
          <label className="grid gap-1 text-[10px] font-medium text-foreground">
            Account name
            <input
              className={fieldClassName()}
              maxLength={160}
              onChange={(event) => setName(event.target.value)}
              required
              value={name}
            />
          </label>
          <label className="grid gap-1 text-[10px] font-medium text-foreground">
            Type
            <select
              className={fieldClassName()}
              onChange={(event) => setType(Number(event.target.value) as AccountType)}
              value={type}
            >
              <option value={AccountType.ASSET}>Asset</option>
              <option value={AccountType.LIABILITY}>Liability</option>
              <option value={AccountType.EQUITY}>Equity</option>
              <option value={AccountType.REVENUE}>Revenue</option>
              <option value={AccountType.EXPENSE}>Expense</option>
            </select>
          </label>
          <Button className="self-end" disabled={saving} type="submit">
            {saving ? "Saving…" : "Save account"}
          </Button>
        </form>
      ) : null}
      {error ? (
        <p className="text-[10px] text-destructive" role="alert">
          {error}
        </p>
      ) : null}

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
                  <td className="w-24 px-3 py-2.5 font-mono font-medium text-foreground">
                    {account.code}
                  </td>
                  <td className="px-3 py-2.5 font-medium text-foreground">{account.name}</td>
                  <td className="px-3 py-2.5 text-muted-foreground">
                    {accountTypeLabel(account.type)}
                  </td>
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
                <LoaderCircle
                  aria-hidden="true"
                  className="mx-auto size-4 animate-spin text-forest"
                />
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

function fieldClassName(): string {
  return "focus-ring h-9 w-full rounded-[6px] border border-border bg-surface px-3 text-[11px] text-foreground outline-none";
}

function normalBalanceFor(type: AccountType): NormalBalance {
  switch (type) {
    case AccountType.LIABILITY:
    case AccountType.EQUITY:
    case AccountType.REVENUE:
    case AccountType.OTHER_REVENUE:
      return NormalBalance.CREDIT;
    default:
      return NormalBalance.DEBIT;
  }
}

function uuidV7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  let milliseconds = Date.now();
  for (let index = 5; index >= 0; index -= 1) {
    bytes[index] = milliseconds & 0xff;
    milliseconds = Math.floor(milliseconds / 256);
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
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
