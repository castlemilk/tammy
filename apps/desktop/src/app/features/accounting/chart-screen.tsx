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
import { ChevronDown, LockKeyhole, Plus, RefreshCw } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { HorizontalScrollArea } from "../../components/horizontal-scroll-area";
import { WorkNotice, WorkSearch, WorkState } from "../../components/review-workspace";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { ScreenHeading } from "../../components/workspace-primitives";
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
  readonly api: Pick<TammyConveyorModule<"accounting">, "createAccount" | "listAccounts">;
  readonly workspace: AuthenticatedWorkspace | undefined;
}

type ChartState =
  | { readonly status: "loading" }
  | { readonly status: "unavailable" }
  | { readonly accounts: readonly Account[]; readonly status: "ready" };

export function ChartScreen({ api, workspace }: ChartScreenProps) {
  const organisationId = workspace?.organisationId ?? "";
  const userId = workspace?.userId ?? "";
  const sessionId = workspace?.sessionId ?? "";
  const listAccountsInput = useMemo(
    () =>
      codec.encodeRequest(
        create(ListAccountsRequestSchema, {
          authentication: { actorUserId: userId, sessionId },
          organisationId,
          page: create(PageRequestSchema, { pageSize: 200 }),
        }),
      ),
    [organisationId, sessionId, userId],
  );
  const accountsQuery = api.listAccounts.useQuery({
    enabled: organisationId.length > 0,
    input: listAccountsInput,
    retry: false,
  });
  const chart = useMemo<ChartState>(() => {
    if (!organisationId || accountsQuery.isError) return { status: "unavailable" };
    if (!accountsQuery.data) return { status: "loading" };
    try {
      return { accounts: codec.decodeResponse(accountsQuery.data).accounts, status: "ready" };
    } catch {
      return { status: "unavailable" };
    }
  }, [accountsQuery.data, accountsQuery.isError, organisationId]);
  const [adding, setAdding] = useState(false);
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [type, setType] = useState<AccountType>(AccountType.EXPENSE);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [refreshRequired, setRefreshRequired] = useState(false);
  const [search, setSearch] = useState("");
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const createGuard = useRef(false);
  const addButtonRef = useRef<HTMLButtonElement>(null);
  const codeRef = useRef<HTMLInputElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const errorRef = useRef<HTMLElement>(null);
  const returnFocus = useRef(false);
  useEffect(() => {
    if (adding) codeRef.current?.focus();
    else if (!saving && returnFocus.current) {
      addButtonRef.current?.focus();
      returnFocus.current = false;
    }
  }, [adding, saving]);
  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);
  const groups = useMemo(() => {
    if (chart.status !== "ready") return [];
    const query = search.trim().toLocaleLowerCase("en-AU");
    const accounts = chart.accounts
      .filter((account) =>
        `${account.code} ${account.name}`.toLocaleLowerCase("en-AU").includes(query),
      )
      .toSorted((a, b) => a.code.localeCompare(b.code, "en-AU", { numeric: true }));
    return accountGroups
      .map((group) => ({
        ...group,
        accounts: accounts.filter((account) =>
          group.type === undefined
            ? !knownAccountTypes.has(account.type)
            : account.type === group.type,
        ),
      }))
      .filter((group) => group.accounts.length > 0);
  }, [chart, search]);
  const matchingCount = groups.reduce((count, group) => count + group.accounts.length, 0);
  const searchAccounts = (value: string) => {
    setSearch(value);
    setCollapsed(new Set());
  };
  const closeEntry = () => {
    if (createGuard.current) return;
    returnFocus.current = true;
    setAdding(false);
    setError(undefined);
  };
  const refreshAccounts = async () => {
    if (!organisationId || createGuard.current) return;
    try {
      const result = await accountsQuery.refetch();
      if (result.isError || !result.data) throw new Error("chart unavailable");
      codec.decodeResponse(result.data);
      setRefreshRequired(false);
      setError(undefined);
    } catch {
      setError("The chart could not refresh. Your records remain on this device.");
    }
  };

  const addAccount = async (event: FormEvent) => {
    event.preventDefault();
    if (
      !workspace?.organisationId ||
      createGuard.current ||
      chart.status !== "ready" ||
      refreshRequired
    )
      return;
    createGuard.current = true;
    setSaving(true);
    setError(undefined);
    setNotice(undefined);
    let confirmed = false;
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
      confirmed = true;
      setNotice("Account saved.");
      setCode("");
      setName("");
      returnFocus.current = true;
      setAdding(false);
      await api.listAccounts.invalidate(listAccountsInput);
    } catch {
      if (confirmed) setRefreshRequired(true);
      setError(
        confirmed
          ? "Account saved, but the chart could not refresh. Refresh the chart before adding another account."
          : "The account could not be created. Check the code and try again.",
      );
    } finally {
      createGuard.current = false;
      setSaving(false);
    }
  };

  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-4">
      <ScreenHeading
        area="Accounting · Structure"
        title="Chart of accounts"
        description="A clear structure for your business ledger."
        action={
          <div className="flex flex-wrap items-center gap-3">
            {notice && !error ? (
              <p className="text-[13px] text-forest" role="status">
                {notice}
              </p>
            ) : null}
            <Button
              className="h-11 text-[14px]"
              aria-expanded={adding}
              disabled={saving || (!adding && (chart.status !== "ready" || refreshRequired))}
              onClick={() => {
                if (adding) closeEntry();
                else {
                  setError(undefined);
                  setNotice(undefined);
                  setAdding(true);
                }
              }}
              ref={addButtonRef}
              type="button"
              variant={adding ? "outline" : "default"}
            >
              {adding ? null : <Plus aria-hidden="true" className="size-4" />}{" "}
              {adding ? "Cancel" : "Add account"}
            </Button>
          </div>
        }
      />

      {error ? (
        <section
          aria-label="Account error"
          className="focus-ring rounded-xl"
          ref={errorRef}
          tabIndex={-1}
        >
          <WorkNotice>{error}</WorkNotice>
        </section>
      ) : null}
      {adding ? (
        <form
          aria-label="Create ordinary account"
          className="work-surface overflow-hidden"
          onKeyDown={(event) => {
            if (
              event.key === "Escape" &&
              !event.nativeEvent.isComposing &&
              !(event.target instanceof HTMLSelectElement)
            ) {
              event.preventDefault();
              closeEntry();
            }
          }}
          onSubmit={addAccount}
        >
          <fieldset
            className="min-w-0"
            disabled={saving || chart.status !== "ready" || refreshRequired}
          >
            <legend className="sr-only">New ordinary account</legend>
            <div className="border-b border-border px-4 py-3">
              <h2 className="font-serif text-[24px] leading-8">New ordinary account</h2>
              <p className="mt-1 text-[13px] leading-5 text-muted-foreground">
                Creates an ordinary account. Installed system and control accounts stay protected.
              </p>
            </div>
            <div className="grid grid-cols-[120px_minmax(0,1fr)_160px_auto] items-end gap-4 p-4 max-[760px]:grid-cols-1">
              <label className="grid gap-1.5 text-[13px] font-semibold text-foreground">
                Code
                <input
                  className={fieldClassName()}
                  maxLength={32}
                  onChange={(event) => {
                    setError(undefined);
                    setCode(event.target.value.toUpperCase());
                  }}
                  ref={codeRef}
                  required
                  value={code}
                />
              </label>
              <label className="grid gap-1.5 text-[13px] font-semibold text-foreground">
                Account name
                <input
                  className={fieldClassName()}
                  maxLength={160}
                  onChange={(event) => {
                    setError(undefined);
                    setName(event.target.value);
                  }}
                  required
                  value={name}
                />
              </label>
              <label className="grid gap-1.5 text-[13px] font-semibold text-foreground">
                Type
                <select
                  className={fieldClassName()}
                  onChange={(event) => {
                    setError(undefined);
                    setType(Number(event.target.value) as AccountType);
                  }}
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
            </div>
          </fieldset>
        </form>
      ) : null}

      <section className="work-surface overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-3 py-3">
          <div className="min-w-0 flex-1 basis-72">
            <WorkSearch
              label="Search accounts by code or name"
              inputRef={searchRef}
              value={search}
              onChange={searchAccounts}
            />
          </div>
          <div className="flex items-center gap-2">
            {chart.status === "ready" ? (
              <span className="text-[13px] tabular-nums text-muted-foreground">
                {chart.accounts.length} loaded accounts
              </span>
            ) : null}
            <Button
              aria-label="Refresh accounts"
              className="size-11 p-0"
              disabled={saving || accountsQuery.isFetching || !organisationId}
              onClick={() => {
                void refreshAccounts();
              }}
              type="button"
              variant="ghost"
            >
              <RefreshCw aria-hidden="true" className="size-4" />
            </Button>
          </div>
        </div>
        {chart.status === "ready" && matchingCount > 0 ? (
          <HorizontalScrollArea className="chart-accounts-scroll" label="Accounts table">
            <table className="chart-accounts-table w-full min-w-[700px] border-collapse text-left text-[13px]">
              <thead className="border-b border-border bg-background/60 text-[12px] text-muted-foreground">
                <tr>
                  <th className="w-24 font-semibold" scope="col">
                    Code
                  </th>
                  <th className="font-semibold" scope="col">
                    Account name
                  </th>
                  <th className="w-32 font-semibold" scope="col">
                    Type
                  </th>
                  <th className="w-40 font-semibold" scope="col">
                    Protection
                  </th>
                  <th className="w-24 font-semibold" scope="col">
                    State
                  </th>
                </tr>
              </thead>
              {groups.map((group) => (
                <tbody className="divide-y divide-border border-t border-border" key={group.label}>
                  <tr className="bg-background/70">
                    <th colSpan={5} scope="rowgroup">
                      <Button
                        aria-expanded={!collapsed.has(group.label)}
                        className="h-11 w-full justify-start rounded-none px-0 text-[13px] font-semibold"
                        onClick={() =>
                          setCollapsed((value) => {
                            const next = new Set(value);
                            if (next.has(group.label)) next.delete(group.label);
                            else next.add(group.label);
                            return next;
                          })
                        }
                        type="button"
                        variant="ghost"
                      >
                        <ChevronDown
                          aria-hidden="true"
                          className={`size-3.5 ${collapsed.has(group.label) ? "-rotate-90" : ""}`}
                        />
                        {group.label} ({group.accounts.length})
                      </Button>
                    </th>
                  </tr>
                  {collapsed.has(group.label)
                    ? null
                    : group.accounts.map((account) => (
                        <tr key={account.id}>
                          <td className="font-mono font-medium tabular-nums text-foreground">
                            {account.code}
                          </td>
                          <td className="font-medium text-foreground">{account.name}</td>
                          <td className="text-muted-foreground">
                            {accountTypeLabel(account.type)}
                          </td>
                          <td>
                            {account.designation === AccountDesignation.CONTROL ||
                            account.designation === AccountDesignation.SYSTEM ? (
                              <Badge
                                className="gap-1 whitespace-nowrap border-warning-border bg-warning-soft text-warning"
                                variant="outline"
                              >
                                <LockKeyhole aria-hidden="true" className="size-3 shrink-0" />
                                <span>
                                  Protected ·{" "}
                                  {account.designation === AccountDesignation.CONTROL
                                    ? "Control"
                                    : "System"}
                                </span>
                              </Badge>
                            ) : (
                              <span className="text-muted-foreground">
                                {account.designation === AccountDesignation.ORDINARY
                                  ? "Ordinary"
                                  : "Unknown"}
                              </span>
                            )}
                          </td>
                          <td className="text-muted-foreground">
                            {account.status === AccountStatus.ACTIVE ? (
                              "Active"
                            ) : account.status === AccountStatus.ARCHIVED ? (
                              <Badge variant="outline">Archived</Badge>
                            ) : (
                              "Unknown"
                            )}
                          </td>
                        </tr>
                      ))}
                </tbody>
              ))}
            </table>
          </HorizontalScrollArea>
        ) : (
          <WorkState
            title={
              chart.status === "loading"
                ? "Loading accounts"
                : chart.status === "unavailable"
                  ? "Chart unavailable"
                  : chart.accounts.length
                    ? "No matching accounts"
                    : "No accounts yet"
            }
            description={
              chart.status === "loading"
                ? "Reading the local chart."
                : chart.status === "unavailable"
                  ? "The local chart could not be read. Your records remain on this device."
                  : chart.accounts.length
                    ? "Search checks the codes and names of the loaded accounts."
                    : "Add an ordinary account to extend your local chart."
            }
            loading={chart.status === "loading"}
            action={
              chart.status === "ready" && chart.accounts.length ? (
                <Button
                  onClick={() => {
                    searchAccounts("");
                    searchRef.current?.focus();
                  }}
                  type="button"
                  variant="outline"
                >
                  Show all accounts
                </Button>
              ) : undefined
            }
          />
        )}
        {chart.status === "ready" ? (
          <footer className="border-t border-border px-3 py-2 text-[12px] leading-5 text-muted-foreground">
            {matchingCount} of {chart.accounts.length} loaded accounts · up to 200. Protected system
            and control accounts are read-only.
          </footer>
        ) : null}
      </section>
    </div>
  );
}

const accountGroups = [
  { label: "Assets", type: AccountType.ASSET },
  { label: "Liabilities", type: AccountType.LIABILITY },
  { label: "Equity", type: AccountType.EQUITY },
  { label: "Income", type: AccountType.REVENUE },
  { label: "Other income", type: AccountType.OTHER_REVENUE },
  { label: "Expenses", type: AccountType.EXPENSE },
  { label: "Other expenses", type: AccountType.OTHER_EXPENSE },
  { label: "Contra", type: AccountType.CONTRA },
  { label: "Unclassified", type: undefined },
] as const;
const knownAccountTypes = new Set<number>(
  accountGroups.flatMap((group) => (group.type === undefined ? [] : [group.type])),
);

function fieldClassName(): string {
  return "focus-ring h-11 w-full rounded-xl border border-border bg-surface px-3 text-[14px] text-foreground outline-none";
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
