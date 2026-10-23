import { create } from "@bufbuild/protobuf";
import {
  type Account,
  AccountDesignation,
  GetJournalRequestSchema,
  GetJournalResponseSchema,
  type Journal,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
  ManualJournalLineInputSchema,
  PostManualJournalRequestSchema,
  PostManualJournalResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  CivilDateSchema,
  CommandContextSchema,
  MoneySchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { ArrowLeft, Plus, RefreshCw } from "lucide-react";
import { type FormEvent, type Ref, useEffect, useMemo, useRef, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { HorizontalScrollArea } from "../../components/horizontal-scroll-area";
import { WorkNotice, WorkSearch, WorkState } from "../../components/review-workspace";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { ScreenHeading, SurfaceHeading } from "../../components/workspace-primitives";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const accountsCodec = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 131_072,
  output: ListAccountsResponseSchema,
});
const journalsCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListJournalsResponseSchema,
});
const journalCodec = createProtoMethodCodec({
  input: GetJournalRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 262_144,
  output: GetJournalResponseSchema,
});
const postCodec = createProtoMethodCodec({
  input: PostManualJournalRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 262_144,
  output: PostManualJournalResponseSchema,
});

interface JournalsScreenProps {
  readonly api: Pick<
    TammyConveyorModule<"accounting">,
    "getJournal" | "listAccounts" | "listJournals" | "postManualJournal"
  >;
  readonly onJournalPosted?: () => Promise<void>;
  readonly onNavigate: (path: string) => void;
  readonly path: string;
  readonly workspace: AuthenticatedWorkspace | undefined;
}

type JournalState =
  | { readonly status: "loading" }
  | { readonly status: "unavailable" }
  | {
      readonly accounts: readonly Account[];
      readonly journals: readonly Journal[];
      readonly status: "ready";
    };

export function JournalsScreen({
  api,
  onJournalPosted,
  onNavigate,
  path,
  workspace,
}: JournalsScreenProps) {
  const [adding, setAdding] = useState(false);
  const [posting, setPosting] = useState(false);
  const [error, setError] = useState<string>();
  const [date, setDate] = useState(() => new Date().toISOString().slice(0, 10));
  const [memo, setMemo] = useState("");
  const [amount, setAmount] = useState("");
  const [debitAccount, setDebitAccount] = useState("");
  const [creditAccount, setCreditAccount] = useState("");
  const [query, setQuery] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const postingGuard = useRef(false);
  const newJournalButton = useRef<HTMLButtonElement>(null);
  const dateInput = useRef<HTMLInputElement>(null);
  const errorRegion = useRef<HTMLElement>(null);
  const searchInput = useRef<HTMLInputElement>(null);
  const detailHeading = useRef<HTMLHeadingElement>(null);
  const journalButtons = useRef(new Map<string, HTMLButtonElement>());
  const wasAdding = useRef(false);
  const journalID = useMemo(
    () => new URL(path, "https://tammy.invalid").searchParams.get("journal"),
    [path],
  );
  const organisationId = workspace?.organisationId ?? "";
  const userId = workspace?.userId ?? "";
  const sessionId = workspace?.sessionId ?? "";
  const accountsInput = useMemo(
    () =>
      accountsCodec.encodeRequest(
        create(ListAccountsRequestSchema, {
          authentication: { actorUserId: userId, sessionId },
          organisationId,
          page: create(PageRequestSchema, { pageSize: 200 }),
        }),
      ),
    [organisationId, sessionId, userId],
  );
  const journalsInput = useMemo(
    () =>
      journalsCodec.encodeRequest(
        create(ListJournalsRequestSchema, {
          authentication: { actorUserId: userId, sessionId },
          organisationId,
          page: create(PageRequestSchema, { pageSize: 200 }),
        }),
      ),
    [organisationId, sessionId, userId],
  );
  const journalInput = useMemo(
    () =>
      journalCodec.encodeRequest(
        create(GetJournalRequestSchema, {
          authentication: { actorUserId: userId, sessionId },
          journalId: journalID ?? "",
        }),
      ),
    [journalID, sessionId, userId],
  );
  const accountsQuery = api.listAccounts.useQuery({
    enabled: organisationId.length > 0,
    input: accountsInput,
    retry: false,
  });
  const journalsQuery = api.listJournals.useQuery({
    enabled: organisationId.length > 0,
    input: journalsInput,
    retry: false,
  });
  const journalQuery = api.getJournal.useQuery({
    enabled: Boolean(journalID && workspace),
    input: journalInput,
    retry: false,
  });
  const state = useMemo<JournalState>(() => {
    if (!organisationId || accountsQuery.isError || journalsQuery.isError)
      return { status: "unavailable" };
    if (!accountsQuery.data || !journalsQuery.data) return { status: "loading" };
    try {
      return {
        accounts: accountsCodec.decodeResponse(accountsQuery.data).accounts,
        journals: journalsCodec.decodeResponse(journalsQuery.data).journals,
        status: "ready",
      };
    } catch {
      return { status: "unavailable" };
    }
  }, [
    accountsQuery.data,
    accountsQuery.isError,
    journalsQuery.data,
    journalsQuery.isError,
    organisationId,
  ]);
  const selected = useMemo(() => {
    if (!journalQuery.data) return undefined;
    try {
      return journalCodec.decodeResponse(journalQuery.data).journal;
    } catch {
      return undefined;
    }
  }, [journalQuery.data]);

  const previousJournalID = useRef(journalID);
  useEffect(() => {
    if (journalID && selected?.id === journalID) detailHeading.current?.focus();
    if (!journalID && previousJournalID.current) {
      (journalButtons.current.get(previousJournalID.current) ?? searchInput.current)?.focus();
    }
    previousJournalID.current = journalID;
  }, [journalID, selected?.id]);
  useEffect(() => {
    if (adding) dateInput.current?.focus();
    else if (wasAdding.current) newJournalButton.current?.focus();
    wasAdding.current = adding;
  }, [adding]);
  useEffect(() => {
    if (error) errorRegion.current?.focus();
  }, [error]);

  const ordinaryAccounts =
    state.status === "ready"
      ? state.accounts.filter((account) => account.designation === AccountDesignation.ORDINARY)
      : [];

  const postJournal = async (event: FormEvent) => {
    event.preventDefault();
    if (!workspace?.organisationId || postingGuard.current) return;
    const minorUnits = amountToMinor(amount);
    if (minorUnits <= 0 || debitAccount === creditAccount) {
      setError("Choose two different accounts and enter a positive amount.");
      return;
    }
    postingGuard.current = true;
    setPosting(true);
    setError(undefined);
    try {
      const [year = 0, month = 0, day = 0] = date.split("-").map(Number);
      if (year < 1900 || month < 1 || day < 1) throw new Error("invalid date");
      const request = create(PostManualJournalRequestSchema, {
        commandContext: create(CommandContextSchema, {
          idempotencyKey: uuidV7(),
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
        }),
        organisationId: workspace.organisationId,
        postingDate: create(CivilDateSchema, { year, month, day }),
        memo,
        lines: [
          create(ManualJournalLineInputSchema, {
            clientLineId: uuidV7(),
            accountId: debitAccount,
            description: memo,
            debit: create(MoneySchema, { currencyCode: "AUD", minorUnits }),
            credit: create(MoneySchema, { currencyCode: "AUD" }),
          }),
          create(ManualJournalLineInputSchema, {
            clientLineId: uuidV7(),
            accountId: creditAccount,
            description: memo,
            debit: create(MoneySchema, { currencyCode: "AUD" }),
            credit: create(MoneySchema, { currencyCode: "AUD", minorUnits }),
          }),
        ],
      });
      const response = postCodec.decodeResponse(
        await api.postManualJournal(postCodec.encodeRequest(request)),
      );
      if (!response.journal?.id) throw new Error("invalid journal");
      await Promise.all([api.listJournals.invalidate(journalsInput), onJournalPosted?.()]);
      onNavigate(`/accounting/journals?journal=${response.journal.id}`);
      setAdding(false);
      setMemo("");
      setAmount("");
    } catch {
      setError("The journal could not be posted. Check the accounts and balance.");
    } finally {
      postingGuard.current = false;
      setPosting(false);
    }
  };

  const closeEntry = () => {
    if (postingGuard.current) return;
    setAdding(false);
    setError(undefined);
  };
  const refresh = async () => {
    setRefreshing(true);
    try {
      await Promise.all([accountsQuery.refetch(), journalsQuery.refetch()]);
    } finally {
      setRefreshing(false);
    }
  };
  const filteredJournals =
    state.status === "ready"
      ? state.journals.filter((journal) =>
          journal.memo.toLocaleLowerCase("en-AU").includes(query.trim().toLocaleLowerCase("en-AU")),
        )
      : [];

  if (journalID) {
    if (selected)
      return (
        <JournalDetail
          accounts={state.status === "ready" ? state.accounts : []}
          headingRef={detailHeading}
          journal={selected}
          onBack={() => onNavigate("/accounting/journals")}
        />
      );
    const loading = journalQuery.isLoading || journalQuery.isFetching;
    return (
      <div className="mx-auto grid w-full max-w-[1120px] gap-4">
        <ScreenHeading
          area="Accounting"
          title="Journal"
          description="Retained accounting entry."
          action={
            <Button
              onClick={() => onNavigate("/accounting/journals")}
              type="button"
              variant="outline"
            >
              <ArrowLeft aria-hidden="true" className="size-4" /> Journals
            </Button>
          }
        />
        <section className="work-surface">
          <WorkState
            loading={loading}
            title={loading ? "Loading journal" : "Journal unavailable"}
            description={
              loading
                ? "Reading the retained entry."
                : "This journal could not be loaded. Return to the list or try again."
            }
            action={
              !loading ? (
                <Button onClick={() => void journalQuery.refetch()} type="button" variant="outline">
                  Retry journal
                </Button>
              ) : undefined
            }
          />
        </section>
      </div>
    );
  }

  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-4">
      <ScreenHeading
        area="Accounting"
        title="Journals"
        description={
          adding
            ? "Review both sides before posting to your books."
            : "Balanced accounting entries and their retained sources."
        }
        action={
          <Button
            ref={newJournalButton}
            disabled={posting || ordinaryAccounts.length < 2}
            aria-expanded={adding}
            aria-controls={adding ? "manual-journal-entry" : undefined}
            variant={adding ? "outline" : "default"}
            onClick={() => {
              if (adding) closeEntry();
              else {
                setError(undefined);
                setAdding(true);
              }
            }}
            type="button"
          >
            {adding ? null : <Plus aria-hidden="true" className="size-4" />}
            {adding ? "Cancel" : "New journal"}
          </Button>
        }
      />
      {ordinaryAccounts.length < 2 && state.status === "ready" ? (
        <WorkNotice>
          Create at least two ordinary accounts before posting a manual journal.
        </WorkNotice>
      ) : null}
      {adding ? (
        <JournalForm
          accounts={ordinaryAccounts}
          amount={amount}
          creditAccount={creditAccount}
          date={date}
          dateInput={dateInput}
          debitAccount={debitAccount}
          error={error}
          errorRegion={errorRegion}
          memo={memo}
          onAmount={(value) => {
            setAmount(value);
            setError(undefined);
          }}
          onCancel={closeEntry}
          onCredit={(value) => {
            setCreditAccount(value);
            setError(undefined);
          }}
          onDate={(value) => {
            setDate(value);
            setError(undefined);
          }}
          onDebit={(value) => {
            setDebitAccount(value);
            setError(undefined);
          }}
          onMemo={(value) => {
            setMemo(value);
            setError(undefined);
          }}
          onSubmit={postJournal}
          posting={posting}
        />
      ) : (
        <section aria-label="Journal history" className="work-surface min-w-0 overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
            <h2 className="font-serif text-[24px] leading-8">Journal history</h2>
            <div className="flex w-full max-w-[360px] min-w-0 items-center gap-2">
              <WorkSearch
                inputRef={searchInput}
                label="Search journal descriptions"
                value={query}
                onChange={setQuery}
              />
              <Button
                aria-label="Refresh journals"
                disabled={refreshing || journalsQuery.isFetching || accountsQuery.isFetching}
                onClick={() => void refresh()}
                type="button"
                variant="ghost"
                className="size-11 shrink-0 p-0"
              >
                <RefreshCw
                  aria-hidden="true"
                  className={refreshing ? "size-4 animate-spin" : "size-4"}
                />
              </Button>
            </div>
          </div>
          {state.status === "ready" && filteredJournals.length > 0 ? (
            <HorizontalScrollArea label="Journal history table">
              <table className="compact-ledger-table w-full min-w-[640px] border-collapse text-left">
                <caption className="sr-only">Journal history</caption>
                <thead className="border-b border-border text-[12px] text-muted-foreground">
                  <tr>
                    <th scope="col" className="w-[112px]">
                      Date
                    </th>
                    <th scope="col">Description</th>
                    <th scope="col" className="whitespace-nowrap text-right">
                      Amount · AUD
                    </th>
                    <th scope="col" className="w-[100px]">
                      State
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border text-[14px]">
                  {filteredJournals.map((journal) => (
                    <tr className="hover:bg-muted/50" key={journal.id}>
                      <td className="whitespace-nowrap tabular-nums">{civilDateLabel(journal)}</td>
                      <td>
                        <button
                          type="button"
                          aria-label={`Open journal: ${journal.memo || "Journal entry"}`}
                          ref={(node) => {
                            if (node) journalButtons.current.set(journal.id, node);
                            else journalButtons.current.delete(journal.id);
                          }}
                          onClick={() => onNavigate(`/accounting/journals?journal=${journal.id}`)}
                          className="focus-ring min-h-11 w-full cursor-pointer py-1 text-left font-medium hover:text-forest hover:underline active:text-foreground"
                        >
                          {journal.memo || "Journal entry"}
                        </button>
                      </td>
                      <td className="whitespace-nowrap text-right font-semibold tabular-nums">
                        {moneyLabel(journal.totalDebits?.minorUnits ?? 0n)}
                      </td>
                      <td>
                        <Badge
                          variant="outline"
                          className={
                            journal.state === 1
                              ? "border-transparent bg-forest-soft text-forest"
                              : "bg-muted"
                          }
                        >
                          {journal.state === 1 ? "Posted" : "Reversed"}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </HorizontalScrollArea>
          ) : (
            <WorkState
              loading={state.status === "loading"}
              title={
                state.status === "loading"
                  ? "Loading journals"
                  : state.status === "unavailable"
                    ? "Journals unavailable"
                    : query
                      ? "No matching journals"
                      : "No journals yet"
              }
              description={
                state.status === "unavailable"
                  ? "The journal history could not be loaded. Try refreshing the list."
                  : query
                    ? "Try another description or clear your search."
                    : state.status === "loading"
                      ? "Reading your retained accounting entries."
                      : "Create a balanced manual journal when you are ready."
              }
              action={
                state.status === "unavailable" ? (
                  <Button
                    onClick={() => void refresh()}
                    disabled={refreshing}
                    type="button"
                    variant="outline"
                  >
                    Retry journals
                  </Button>
                ) : undefined
              }
            />
          )}
          <p className="border-t border-border px-4 py-2 text-[12px] text-muted-foreground">
            {state.status === "ready"
              ? filteredJournals.length +
                " of " +
                state.journals.length +
                " loaded journals · up to 200."
              : "Showing the locally loaded journal history."}
          </p>
        </section>
      )}
    </div>
  );
}

function JournalForm(props: {
  readonly accounts: readonly Account[];
  readonly amount: string;
  readonly creditAccount: string;
  readonly date: string;
  readonly dateInput: Ref<HTMLInputElement>;
  readonly debitAccount: string;
  readonly error: string | undefined;
  readonly errorRegion: Ref<HTMLElement>;
  readonly memo: string;
  readonly onAmount: (value: string) => void;
  readonly onCancel: () => void;
  readonly onCredit: (value: string) => void;
  readonly onDate: (value: string) => void;
  readonly onDebit: (value: string) => void;
  readonly onMemo: (value: string) => void;
  readonly onSubmit: (event: FormEvent) => void;
  readonly posting: boolean;
}) {
  const minor = amountToMinor(props.amount);
  const value = minor > 0n ? moneyLabel(minor) : "—";
  return (
    <form
      id="manual-journal-entry"
      aria-label="Manual journal entry"
      aria-busy={props.posting}
      className="work-surface min-w-0 overflow-hidden"
      onSubmit={props.onSubmit}
      onKeyDown={(event) => {
        if (
          event.key === "Escape" &&
          !event.defaultPrevented &&
          !event.nativeEvent.isComposing &&
          !props.posting &&
          !(event.target instanceof HTMLSelectElement) &&
          !(event.target instanceof HTMLInputElement && event.target.type === "date")
        ) {
          event.preventDefault();
          props.onCancel();
        }
      }}
    >
      <div className="border-b border-border px-4 py-3">
        <h2 className="font-serif text-[24px] leading-8">Manual entry</h2>
      </div>
      <fieldset disabled={props.posting} className="min-w-0">
        <legend className="sr-only">Journal details</legend>
        <div className="grid min-w-0 grid-cols-[180px_minmax(0,1fr)_180px] gap-4 border-b border-border p-4 max-[900px]:grid-cols-2 max-[700px]:grid-cols-1">
          <label className="grid min-w-0 gap-1 text-[13px] font-medium">
            Date
            <input
              ref={props.dateInput}
              className={fieldClassName()}
              onChange={(event) => props.onDate(event.target.value)}
              required
              type="date"
              value={props.date}
            />
          </label>
          <label className="grid min-w-0 gap-1 text-[13px] font-medium">
            Description
            <input
              className={fieldClassName()}
              maxLength={512}
              onChange={(event) => props.onMemo(event.target.value)}
              required
              value={props.memo}
            />
          </label>
          <label className="grid min-w-0 gap-1 text-[13px] font-medium">
            Amount (AUD)
            <input
              className={fieldClassName()}
              min="0.01"
              onChange={(event) => props.onAmount(event.target.value)}
              required
              step="0.01"
              type="number"
              value={props.amount}
            />
          </label>
        </div>
        <div
          className="grid grid-cols-[minmax(0,1fr)_140px_140px] gap-4 border-b border-border px-4 py-2 text-[12px] text-muted-foreground max-[700px]:hidden"
          aria-hidden="true"
        >
          <span>Ordinary account</span>
          <span className="text-right">Debit · AUD</span>
          <span className="text-right">Credit · AUD</span>
        </div>
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_140px_140px] items-center gap-4 border-b border-border px-4 py-1 max-[700px]:grid-cols-2 max-[700px]:py-3">
          <AccountSelect
            accounts={props.accounts}
            label="Debit account"
            onChange={props.onDebit}
            value={props.debitAccount}
          />
          <DraftAmount label="Debit line · debit amount" value={value} />
          <DraftAmount label="Debit line · credit amount" value="—" />
        </div>
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_140px_140px] items-center gap-4 px-4 py-1 max-[700px]:grid-cols-2 max-[700px]:py-3">
          <AccountSelect
            accounts={props.accounts}
            label="Credit account"
            onChange={props.onCredit}
            value={props.creditAccount}
          />
          <DraftAmount label="Credit line · debit amount" value="—" />
          <DraftAmount label="Credit line · credit amount" value={value} />
        </div>
      </fieldset>
      <dl className="grid min-w-0 grid-cols-3 gap-4 border-y border-border bg-forest-soft p-4 max-[700px]:grid-cols-1">
        <AmountTotal label="Total debit · AUD" value={value} />
        <AmountTotal label="Total credit · AUD" value={value} />
        <AmountTotal label="Difference · AUD" value={minor > 0n ? moneyLabel(0n) : "—"} />
      </dl>
      {props.error ? (
        <section
          aria-label="Journal error"
          ref={props.errorRegion}
          tabIndex={-1}
          className="focus-ring m-4 rounded-xl"
        >
          <WorkNotice>{props.error}</WorkNotice>
        </section>
      ) : null}
      <div className="flex flex-wrap items-center justify-between gap-4 p-4">
        <p className="max-w-[500px] text-[13px] leading-5 text-muted-foreground">
          Two ordinary accounts. Posting records an equal debit and credit in your books.
        </p>
        <Button className="min-w-[128px]" disabled={props.posting} type="submit">
          {props.posting ? "Posting…" : "Post journal"}
        </Button>
      </div>
    </form>
  );
}
function DraftAmount({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="min-w-0 text-right text-[14px] tabular-nums">
      <span className="sr-only max-[700px]:not-sr-only max-[700px]:block max-[700px]:text-[12px] max-[700px]:text-muted-foreground">
        {label.includes("· debit") ? "Debit · AUD" : "Credit · AUD"}
      </span>
      <HorizontalScrollArea label={label} className="whitespace-nowrap">
        {value}
      </HorizontalScrollArea>
    </div>
  );
}
function AmountTotal({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-[12px] text-muted-foreground">{label}</dt>
      <dd className="mt-1 text-[20px] font-semibold leading-7 tabular-nums">
        <HorizontalScrollArea label={label} className="whitespace-nowrap">
          {value}
        </HorizontalScrollArea>
      </dd>
    </div>
  );
}

function AccountSelect({
  accounts,
  label,
  onChange,
  value,
}: {
  readonly accounts: readonly Account[];
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly value: string;
}) {
  return (
    <label className="grid min-w-0 gap-1 text-[13px] font-medium max-[700px]:col-span-2">
      <span className="min-[701px]:sr-only">{label}</span>
      <select
        className={fieldClassName()}
        onChange={(event) => onChange(event.target.value)}
        required
        value={value}
      >
        <option value="">Choose account</option>
        {accounts.map((account) => (
          <option key={account.id} value={account.id}>
            {account.code} — {account.name}
          </option>
        ))}
      </select>
    </label>
  );
}

function JournalDetail({
  accounts,
  journal,
  headingRef,
  onBack,
}: {
  readonly accounts: readonly Account[];
  readonly journal: Journal;
  readonly headingRef: Ref<HTMLHeadingElement>;
  readonly onBack: () => void;
}) {
  const names = new Map(
    accounts.map((account) => [account.id, `${account.code} — ${account.name}`]),
  );
  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-4">
      <ScreenHeading
        area="Accounting · Journal detail"
        title={journal.memo || "Journal entry"}
        headingRef={headingRef}
        description={`${civilDateLabel(journal)} · ${journal.state === 1 ? "Posted" : "Reversed"}`}
        action={
          <Button onClick={onBack} type="button" variant="outline">
            <ArrowLeft aria-hidden="true" className="size-4" /> Journals
          </Button>
        }
      />
      <section className="work-surface overflow-hidden">
        <SurfaceHeading title="Entry lines" description="Read-only · amounts in AUD" />
        <HorizontalScrollArea label="Journal entry lines table">
          <table className="compact-ledger-table w-full min-w-[680px] border-collapse text-left">
            <thead className="border-b border-border bg-background/60 text-[12px] text-muted-foreground">
              <tr>
                <th className="px-5 py-3">Account</th>
                <th className="px-5 py-3">Description</th>
                <th className="px-5 py-3 text-right">Debit</th>
                <th className="px-5 py-3 text-right">Credit</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border text-[14px]">
              {journal.lines.map((line) => (
                <tr key={line.id}>
                  <td className="px-5 py-3.5 font-medium">
                    {names.get(line.accountId) ?? line.accountId}
                  </td>
                  <td className="px-5 py-3.5 text-muted-foreground">{line.description}</td>
                  <td className="px-5 py-3.5 text-right whitespace-nowrap tabular-nums">
                    {line.debit?.minorUnits ? moneyLabel(line.debit.minorUnits) : "—"}
                  </td>
                  <td className="px-5 py-3.5 text-right whitespace-nowrap tabular-nums">
                    {line.credit?.minorUnits ? moneyLabel(line.credit.minorUnits) : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
            <tfoot className="border-t border-border bg-forest-soft text-[14px] font-semibold">
              <tr>
                <td className="px-5 py-3.5" colSpan={2}>
                  Total
                </td>
                <td className="px-5 py-3.5 text-right whitespace-nowrap tabular-nums">
                  {moneyLabel(journal.totalDebits?.minorUnits ?? 0n)}
                </td>
                <td className="px-5 py-3.5 text-right whitespace-nowrap tabular-nums">
                  {moneyLabel(journal.totalCredits?.minorUnits ?? 0n)}
                </td>
              </tr>
            </tfoot>
          </table>
        </HorizontalScrollArea>
      </section>
    </div>
  );
}

function fieldClassName(): string {
  return "focus-ring h-11 w-full min-w-0 rounded-xl border border-border bg-surface px-3 text-[14px] outline-none";
}
function amountToMinor(value: string): bigint {
  if (!/^\d+(?:\.\d{1,2})?$/.test(value)) return 0n;
  const [whole, fraction = ""] = value.split(".");
  return BigInt(whole ?? "0") * 100n + BigInt(fraction.padEnd(2, "0"));
}
function moneyLabel(minor: bigint): string {
  const absolute = minor < 0n ? -minor : minor;
  return `${minor < 0n ? "−" : ""}$${(absolute / 100n).toLocaleString("en-AU")}.${(absolute % 100n).toString().padStart(2, "0")}`;
}
function civilDateLabel(journal: Journal): string {
  const date = journal.postingDate;
  return date
    ? `${date.day.toString().padStart(2, "0")}/${date.month.toString().padStart(2, "0")}/${date.year}`
    : "—";
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
