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
import { ArrowLeft, LoaderCircle, Plus } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
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
    TammyDesktopAPI,
    "getJournal" | "listAccounts" | "listJournals" | "postManualJournal"
  >;
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

export function JournalsScreen({ api, onNavigate, path, workspace }: JournalsScreenProps) {
  const [state, setState] = useState<JournalState>(
    workspace?.organisationId ? { status: "loading" } : { status: "unavailable" },
  );
  const [selected, setSelected] = useState<Journal>();
  const [adding, setAdding] = useState(false);
  const [posting, setPosting] = useState(false);
  const [error, setError] = useState<string>();
  const [date, setDate] = useState(() => new Date().toISOString().slice(0, 10));
  const [memo, setMemo] = useState("");
  const [amount, setAmount] = useState("");
  const [debitAccount, setDebitAccount] = useState("");
  const [creditAccount, setCreditAccount] = useState("");
  const journalID = useMemo(
    () => new URL(path, "https://tammy.invalid").searchParams.get("journal"),
    [path],
  );

  useEffect(() => {
    if (!workspace?.organisationId) {
      setState({ status: "unavailable" });
      return;
    }
    let active = true;
    const authentication = { actorUserId: workspace.userId, sessionId: workspace.sessionId };
    const accountsRequest = create(ListAccountsRequestSchema, {
      authentication,
      organisationId: workspace.organisationId,
      page: create(PageRequestSchema, { pageSize: 200 }),
    });
    const journalsRequest = create(ListJournalsRequestSchema, {
      authentication,
      organisationId: workspace.organisationId,
      page: create(PageRequestSchema, { pageSize: 200 }),
    });
    setState({ status: "loading" });
    void Promise.all([
      api
        .listAccounts(accountsCodec.encodeRequest(accountsRequest))
        .then((frame) => accountsCodec.decodeResponse(frame)),
      api
        .listJournals(journalsCodec.encodeRequest(journalsRequest))
        .then((frame) => journalsCodec.decodeResponse(frame)),
    ])
      .then(([accounts, journals]) => {
        if (active)
          setState({ accounts: accounts.accounts, journals: journals.journals, status: "ready" });
      })
      .catch(() => {
        if (active) setState({ status: "unavailable" });
      });
    return () => {
      active = false;
    };
  }, [api, workspace]);

  useEffect(() => {
    if (!workspace || !journalID) {
      setSelected(undefined);
      return;
    }
    let active = true;
    const request = create(GetJournalRequestSchema, {
      authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
      journalId: journalID,
    });
    void api
      .getJournal(journalCodec.encodeRequest(request))
      .then((frame) => journalCodec.decodeResponse(frame))
      .then((response) => {
        if (active) setSelected(response.journal);
      })
      .catch(() => {
        if (active) setError("The journal could not be read.");
      });
    return () => {
      active = false;
    };
  }, [api, journalID, workspace]);

  const ordinaryAccounts =
    state.status === "ready"
      ? state.accounts.filter((account) => account.designation === AccountDesignation.ORDINARY)
      : [];

  const postJournal = async (event: FormEvent) => {
    event.preventDefault();
    if (!workspace?.organisationId) return;
    const minorUnits = amountToMinor(amount);
    if (minorUnits <= 0 || debitAccount === creditAccount) {
      setError("Choose two different accounts and enter a positive amount.");
      return;
    }
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
      onNavigate(`/accounting/journals?journal=${response.journal.id}`);
      setSelected(response.journal);
      setAdding(false);
      setMemo("");
      setAmount("");
    } catch {
      setError("The journal could not be posted. Check the accounts and balance.");
    } finally {
      setPosting(false);
    }
  };

  if (journalID && selected) {
    return (
      <JournalDetail
        accounts={state.status === "ready" ? state.accounts : []}
        journal={selected}
        onBack={() => onNavigate("/accounting/journals")}
      />
    );
  }

  return (
    <div className="mx-auto grid w-full max-w-[920px] gap-5">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-[18px] font-semibold tracking-[-0.02em] text-foreground">Journals</h1>
          <p className="mt-1 text-[11px] leading-5 text-muted-foreground">
            Balanced accounting entries and their retained sources.
          </p>
        </div>
        <Button
          className="h-8 text-[10px]"
          disabled={ordinaryAccounts.length < 2}
          onClick={() => setAdding((value) => !value)}
          type="button"
        >
          <Plus aria-hidden="true" className="size-3" /> New journal
        </Button>
      </div>
      {ordinaryAccounts.length < 2 && state.status === "ready" ? (
        <p className="rounded-[6px] border border-warning-border bg-warning-soft px-3 py-2 text-[10px] text-foreground">
          Create at least two ordinary accounts before posting a manual journal.
        </p>
      ) : null}
      {adding ? (
        <JournalForm
          accounts={ordinaryAccounts}
          amount={amount}
          creditAccount={creditAccount}
          date={date}
          debitAccount={debitAccount}
          memo={memo}
          onAmount={setAmount}
          onCredit={setCreditAccount}
          onDate={setDate}
          onDebit={setDebitAccount}
          onMemo={setMemo}
          onSubmit={postJournal}
          posting={posting}
        />
      ) : null}
      {error ? (
        <p className="text-[10px] text-destructive" role="alert">
          {error}
        </p>
      ) : null}
      <section className="overflow-hidden rounded-[6px] border border-border bg-surface">
        {state.status === "ready" && state.journals.length > 0 ? (
          <table className="w-full border-collapse text-left">
            <thead className="border-b border-border bg-background/60 text-[9px] uppercase tracking-[0.05em] text-muted-foreground">
              <tr>
                <th className="px-3 py-2">Date</th>
                <th className="px-3 py-2">Description</th>
                <th className="px-3 py-2 text-right">Amount</th>
                <th className="px-3 py-2">State</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border text-[10px]">
              {state.journals.map((journal) => (
                <tr
                  className="focus-ring cursor-pointer hover:bg-muted/50"
                  key={journal.id}
                  onClick={() => onNavigate(`/accounting/journals?journal=${journal.id}`)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      onNavigate(`/accounting/journals?journal=${journal.id}`);
                    }
                  }}
                  tabIndex={0}
                >
                  <td className="px-3 py-2.5">{civilDateLabel(journal)}</td>
                  <td className="px-3 py-2.5 font-medium">{journal.memo || "Journal entry"}</td>
                  <td className="px-3 py-2.5 text-right font-mono">
                    {moneyLabel(journal.totalDebits?.minorUnits ?? 0n)}
                  </td>
                  <td className="px-3 py-2.5">{journal.state === 1 ? "Posted" : "Reversed"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <EmptyState status={state.status} />
        )}
      </section>
    </div>
  );
}

function JournalForm(props: {
  readonly accounts: readonly Account[];
  readonly amount: string;
  readonly creditAccount: string;
  readonly date: string;
  readonly debitAccount: string;
  readonly memo: string;
  readonly onAmount: (value: string) => void;
  readonly onCredit: (value: string) => void;
  readonly onDate: (value: string) => void;
  readonly onDebit: (value: string) => void;
  readonly onMemo: (value: string) => void;
  readonly onSubmit: (event: FormEvent) => void;
  readonly posting: boolean;
}) {
  return (
    <form
      className="grid gap-3 rounded-[6px] border border-border bg-surface p-3"
      onSubmit={props.onSubmit}
    >
      <div className="grid grid-cols-2 gap-3 max-[700px]:grid-cols-1">
        <label className="grid gap-1 text-[10px] font-medium">
          Date
          <input
            className={fieldClassName()}
            onChange={(event) => props.onDate(event.target.value)}
            required
            type="date"
            value={props.date}
          />
        </label>
        <label className="grid gap-1 text-[10px] font-medium">
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
      <label className="grid gap-1 text-[10px] font-medium">
        Description
        <input
          className={fieldClassName()}
          maxLength={512}
          onChange={(event) => props.onMemo(event.target.value)}
          required
          value={props.memo}
        />
      </label>
      <div className="grid grid-cols-2 gap-3 max-[700px]:grid-cols-1">
        <AccountSelect
          accounts={props.accounts}
          label="Debit account"
          onChange={props.onDebit}
          value={props.debitAccount}
        />
        <AccountSelect
          accounts={props.accounts}
          label="Credit account"
          onChange={props.onCredit}
          value={props.creditAccount}
        />
      </div>
      <Button className="justify-self-end" disabled={props.posting} type="submit">
        {props.posting ? "Posting…" : "Post journal"}
      </Button>
    </form>
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
    <label className="grid gap-1 text-[10px] font-medium">
      {label}
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
  onBack,
}: {
  readonly accounts: readonly Account[];
  readonly journal: Journal;
  readonly onBack: () => void;
}) {
  const names = new Map(
    accounts.map((account) => [account.id, `${account.code} — ${account.name}`]),
  );
  return (
    <div className="mx-auto grid w-full max-w-[920px] gap-5">
      <div>
        <Button onClick={onBack} type="button" variant="ghost">
          <ArrowLeft aria-hidden="true" className="size-3" /> Journals
        </Button>
        <h1 className="mt-3 text-[18px] font-semibold">{journal.memo || "Journal entry"}</h1>
        <p className="mt-1 text-[11px] text-muted-foreground">
          {civilDateLabel(journal)} · {journal.state === 1 ? "Posted" : "Reversed"}
        </p>
      </div>
      <section className="overflow-hidden rounded-[6px] border border-border bg-surface">
        <table className="w-full border-collapse text-left">
          <thead className="border-b border-border bg-background/60 text-[9px] uppercase text-muted-foreground">
            <tr>
              <th className="px-3 py-2">Account</th>
              <th className="px-3 py-2">Description</th>
              <th className="px-3 py-2 text-right">Debit</th>
              <th className="px-3 py-2 text-right">Credit</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border text-[10px]">
            {journal.lines.map((line) => (
              <tr key={line.id}>
                <td className="px-3 py-2.5 font-medium">
                  {names.get(line.accountId) ?? line.accountId}
                </td>
                <td className="px-3 py-2.5 text-muted-foreground">{line.description}</td>
                <td className="px-3 py-2.5 text-right font-mono">
                  {line.debit?.minorUnits ? moneyLabel(line.debit.minorUnits) : "—"}
                </td>
                <td className="px-3 py-2.5 text-right font-mono">
                  {line.credit?.minorUnits ? moneyLabel(line.credit.minorUnits) : "—"}
                </td>
              </tr>
            ))}
          </tbody>
          <tfoot className="border-t border-border text-[10px] font-semibold">
            <tr>
              <td className="px-3 py-2.5" colSpan={2}>
                Total
              </td>
              <td className="px-3 py-2.5 text-right font-mono">
                {moneyLabel(journal.totalDebits?.minorUnits ?? 0n)}
              </td>
              <td className="px-3 py-2.5 text-right font-mono">
                {moneyLabel(journal.totalCredits?.minorUnits ?? 0n)}
              </td>
            </tr>
          </tfoot>
        </table>
      </section>
    </div>
  );
}

function EmptyState({ status }: { readonly status: JournalState["status"] }) {
  return (
    <div className="grid min-h-64 place-items-center text-center">
      <div>
        {status === "loading" ? (
          <LoaderCircle className="mx-auto size-4 animate-spin text-forest" />
        ) : null}
        <p className="mt-2 text-[12px] font-semibold">
          {status === "unavailable" ? "Journals unavailable" : "No journals yet"}
        </p>
        <p className="mt-1 text-[10px] text-muted-foreground">
          Post a balanced manual journal to begin.
        </p>
      </div>
    </div>
  );
}
function fieldClassName(): string {
  return "focus-ring h-9 w-full rounded-[6px] border border-border bg-surface px-3 text-[11px] outline-none";
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
