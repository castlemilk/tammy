import { Plus } from "lucide-react";
import { type FormEvent, useState } from "react";
import { ApiError, api, type JournalInput } from "./api";
import { amountInput, formatAmount, parseAmount, sumAmounts } from "./money";
import { AppLink } from "./shell";
import type { ListPage } from "./types";
import { useCommandKey } from "./use-command-key";
import { useRemote } from "./use-remote";
import { displayDate, errorMessage, today } from "./utils";

export function PageLimit({ page }: { page: ListPage | undefined }) {
  return page?.mayHaveMore ? (
    <p className="notice" role="status">
      Showing the first {page.returnedCount} records. Additional records may exist; continuation is
      not available yet.
    </p>
  ) : null;
}

export interface PendingJournal {
  payload: Omit<JournalInput, "idempotencyKey">;
  idempotencyKey: string;
}

export function ChartScreen() {
  const accounts = useRemote("accounts", api.accounts);
  const [adding, setAdding] = useState(false);
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [type, setType] = useState("expense");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const command = useCommandKey();
  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    const payload = { code: code.trim(), name: name.trim(), type };
    try {
      await api.createAccount({ ...payload, idempotencyKey: command.keyFor(payload) });
      command.clear();
      setAdding(false);
      setCode("");
      setName("");
      accounts.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="screen">
      <div className="page-heading">
        <div>
          <h1>Chart of accounts</h1>
          <p>Accounts used to organise your books</p>
        </div>
        <button
          className="button primary"
          onClick={() => {
            setAdding(!adding);
            setError(undefined);
          }}
          type="button"
        >
          <Plus aria-hidden="true" />
          Add account
        </button>
      </div>
      {adding ? (
        <section className="panel form-panel">
          <div className="panel-heading">
            <div>
              <h2>Add an ordinary account</h2>
              <p>Protected system and control accounts are created by Tammy</p>
            </div>
            <button className="text-button" onClick={() => setAdding(false)} type="button">
              Cancel
            </button>
          </div>
          <form className="form-grid" onSubmit={save}>
            <label>
              Account code
              <input onChange={(event) => setCode(event.target.value)} required value={code} />
            </label>
            <label>
              Account name
              <input onChange={(event) => setName(event.target.value)} required value={name} />
            </label>
            <label>
              Account type
              <select onChange={(event) => setType(event.target.value)} value={type}>
                <option value="asset">Asset</option>
                <option value="liability">Liability</option>
                <option value="equity">Equity</option>
                <option value="income">Income</option>
                <option value="expense">Expense</option>
              </select>
            </label>
            <div className="form-actions">
              <button className="button primary" disabled={busy} type="submit">
                {busy ? "Saving…" : "Save account"}
              </button>
            </div>
          </form>
          {error ? (
            <p className="notice error form-error" role="alert">
              {error}
            </p>
          ) : null}
        </section>
      ) : null}
      {accounts.state.status === "error" ? (
        <p className="notice error" role="alert">
          {accounts.state.message}{" "}
          <button className="text-button" onClick={accounts.reload} type="button">
            Retry
          </button>
        </p>
      ) : accounts.state.status === "loading" ? (
        <p className="loading" role="status">
          Loading accounts…
        </p>
      ) : (
        <>
          <PageLimit page={accounts.state.value.page} />
          <section className="panel table-panel">
            <div className="panel-heading">
              <div>
                <h2>Accounts</h2>
                <p>Ordinary accounts can be used for manual journals</p>
              </div>
            </div>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Code</th>
                    <th>Account</th>
                    <th>Type</th>
                    <th>Normal balance</th>
                    <th>Designation</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {accounts.state.value.accounts.map((account) => (
                    <tr key={account.id}>
                      <td>{account.code}</td>
                      <td className="strong">{account.name}</td>
                      <td className="capitalize">{account.type.replaceAll("_", " ")}</td>
                      <td className="capitalize">{account.normalBalance}</td>
                      <td className="capitalize">{account.designation}</td>
                      <td>{account.active ? "Active" : "Inactive"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {accounts.state.value.accounts.length === 0 ? (
              <p className="table-empty">No accounts yet</p>
            ) : null}
          </section>
        </>
      )}
    </div>
  );
}

export function JournalsScreen({
  navigate,
  pending,
  setPending,
}: {
  navigate: (path: string) => void;
  pending: PendingJournal | undefined;
  setPending: (value: PendingJournal | undefined) => void;
}) {
  const accounts = useRemote("accounts", api.accounts);
  const journals = useRemote("journals", api.journals);
  const [adding, setAdding] = useState(Boolean(pending));
  const [date, setDate] = useState(pending?.payload.date ?? today());
  const [description, setDescription] = useState(pending?.payload.description ?? "");
  const [debit, setDebit] = useState(pending?.payload.lines[0]?.accountId ?? "");
  const [credit, setCredit] = useState(pending?.payload.lines[1]?.accountId ?? "");
  const [amount, setAmount] = useState(
    pending ? amountInput(pending.payload.lines[0]?.debitMinor) : "",
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [selected, setSelected] = useState<string>();
  const command = useCommandKey();
  const ordinary =
    accounts.state.status === "ready"
      ? accounts.state.value.accounts.filter(
          (item) => item.active && item.designation === "ordinary",
        )
      : [];
  async function post(event: FormEvent) {
    event.preventDefault();
    let request = pending;
    if (!request) {
      const cents = parseAmount(amount);
      if (cents === undefined || cents <= 0 || !debit || !credit || debit === credit) {
        setError(
          "Choose two different ordinary accounts and a positive amount with no more than two decimal places.",
        );
        return;
      }
      const payload = {
        date,
        description: description.trim(),
        lines: [
          { accountId: debit, description: description.trim(), debitMinor: cents, creditMinor: 0 },
          { accountId: credit, description: description.trim(), debitMinor: 0, creditMinor: cents },
        ],
      };
      request = { payload, idempotencyKey: command.keyFor(payload) };
    }
    setBusy(true);
    setError(undefined);
    try {
      await api.postJournal({ ...request.payload, idempotencyKey: request.idempotencyKey });
      command.clear();
      setPending(undefined);
      setAdding(false);
      setDescription("");
      setAmount("");
      journals.reload();
    } catch (cause) {
      if (!(cause instanceof ApiError) || cause.status >= 500 || cause.status === 408)
        setPending(request);
      else setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }
  const entries = journals.state.status === "ready" ? journals.state.value.journals : [];
  const selectedJournal = entries.find((entry) => entry.id === selected);
  return (
    <div className="screen">
      <div className="page-heading">
        <div>
          <h1>Journals</h1>
          <p>Balanced entries and the accounts behind them</p>
        </div>
        <button
          className="button primary"
          onClick={() => {
            setAdding(!adding);
            setError(undefined);
          }}
          type="button"
        >
          <Plus aria-hidden="true" />
          {pending ? "Resume journal" : "New journal"}
        </button>
      </div>
      {pending ? (
        <p className="notice" role="status">
          The service could not confirm whether this journal posted. Retry the same journal using
          its original operation ID, or check the journal list before leaving.{" "}
          <button className="text-button" onClick={journals.reload} type="button">
            Refresh journal list
          </button>
        </p>
      ) : null}
      {adding ? (
        <section className="panel form-panel">
          <div className="panel-heading">
            <div>
              <h2>New journal</h2>
              <p>Post an equal debit and credit to two ordinary accounts</p>
            </div>
            <button className="text-button" onClick={() => setAdding(false)} type="button">
              Cancel
            </button>
          </div>
          {ordinary.length < 2 && !pending ? (
            <p className="notice">
              Create at least two active ordinary accounts before posting.{" "}
              <AppLink className="text-link" navigate={navigate} to="/accounts">
                View chart of accounts
              </AppLink>
            </p>
          ) : (
            <form className="form-grid" onSubmit={post}>
              <label>
                Date
                <input
                  disabled={Boolean(pending)}
                  onChange={(event) => setDate(event.target.value)}
                  required
                  type="date"
                  value={date}
                />
              </label>
              <label>
                Description
                <input
                  disabled={Boolean(pending)}
                  onChange={(event) => setDescription(event.target.value)}
                  required
                  value={description}
                />
              </label>
              <label>
                Debit account
                <select
                  disabled={Boolean(pending)}
                  onChange={(event) => setDebit(event.target.value)}
                  required
                  value={debit}
                >
                  <option value="">Select an account</option>
                  {ordinary.map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.code} — {account.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Credit account
                <select
                  disabled={Boolean(pending)}
                  onChange={(event) => setCredit(event.target.value)}
                  required
                  value={credit}
                >
                  <option value="">Select an account</option>
                  {ordinary.map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.code} — {account.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Amount (AUD)
                <input
                  disabled={Boolean(pending)}
                  inputMode="decimal"
                  onChange={(event) => setAmount(event.target.value)}
                  placeholder="0.00"
                  required
                  value={amount}
                />
              </label>
              <div className="form-actions">
                <button className="button primary" disabled={busy} type="submit">
                  {busy ? "Posting…" : pending ? "Retry same journal" : "Post journal"}
                </button>
              </div>
            </form>
          )}
          {error ? (
            <p className="notice error form-error" role="alert">
              {error}
            </p>
          ) : null}
        </section>
      ) : null}
      {accounts.state.status === "error" ? (
        <p className="notice error" role="alert">
          Accounts unavailable: {accounts.state.message}
        </p>
      ) : null}
      {journals.state.status === "error" ? (
        <p className="notice error" role="alert">
          {journals.state.message}{" "}
          <button className="text-button" onClick={journals.reload} type="button">
            Retry
          </button>
        </p>
      ) : journals.state.status === "loading" ? (
        <p className="loading" role="status">
          Loading journals…
        </p>
      ) : (
        <>
          <PageLimit page={journals.state.value.page} />
          <section className="panel table-panel">
            <div className="panel-heading">
              <h2>Journal entries</h2>
            </div>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Date</th>
                    <th>Description</th>
                    <th>Status</th>
                    <th className="numeric">Debit (AUD)</th>
                    <th className="numeric">Credit (AUD)</th>
                  </tr>
                </thead>
                <tbody>
                  {entries.map((entry) => (
                    <tr key={entry.id}>
                      <td>{displayDate(entry.date)}</td>
                      <td>
                        <button
                          className="text-button"
                          onClick={() => setSelected(selected === entry.id ? undefined : entry.id)}
                          type="button"
                        >
                          {entry.description || "Journal entry"}
                        </button>
                      </td>
                      <td className="capitalize">{entry.state}</td>
                      <td className="numeric">
                        {formatAmount(sumAmounts(entry.lines.map((line) => line.debitMinor)))}
                      </td>
                      <td className="numeric">
                        {formatAmount(sumAmounts(entry.lines.map((line) => line.creditMinor)))}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {entries.length === 0 ? (
              <p className="table-empty">No journals yet. Create an entry to get started.</p>
            ) : null}
          </section>
        </>
      )}
      {selectedJournal ? (
        <section className="panel table-panel">
          <div className="panel-heading">
            <div>
              <h2>Journal detail</h2>
              <p>
                {displayDate(selectedJournal.date)} · {selectedJournal.description}
              </p>
            </div>
            <button className="text-button" onClick={() => setSelected(undefined)} type="button">
              Close
            </button>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Account</th>
                  <th>Description</th>
                  <th className="numeric">Debit (AUD)</th>
                  <th className="numeric">Credit (AUD)</th>
                </tr>
              </thead>
              <tbody>
                {selectedJournal.lines.map((line) => (
                  <tr key={line.id}>
                    <td>
                      {line.accountCode} {line.accountName}
                    </td>
                    <td>{line.description || "—"}</td>
                    <td className="numeric">{formatAmount(line.debitMinor)}</td>
                    <td className="numeric">{formatAmount(line.creditMinor)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}
    </div>
  );
}

export function TrialBalanceScreen() {
  const [date, setDate] = useState(today());
  const [asOf, setAsOf] = useState(date);
  const balance = useRemote(`trial-${asOf}`, () => api.trialBalance(asOf));
  return (
    <div className="screen">
      <div className="page-heading">
        <div>
          <h1>Trial balance</h1>
          <p>Check the debits and credits recorded in your books</p>
        </div>
      </div>
      <section className="panel">
        <form
          className="inline-form"
          onSubmit={(event) => {
            event.preventDefault();
            setAsOf(date);
            if (date === asOf) balance.reload();
          }}
        >
          <label>
            As of date
            <input
              onChange={(event) => setDate(event.target.value)}
              required
              type="date"
              value={date}
            />
          </label>
          <button className="button secondary" type="submit">
            Show balance
          </button>
        </form>
      </section>
      {balance.state.status === "error" ? (
        <p className="notice error" role="alert">
          {balance.state.message}{" "}
          <button className="text-button" onClick={balance.reload} type="button">
            Retry
          </button>
        </p>
      ) : balance.state.status === "loading" ? (
        <p className="loading" role="status">
          Loading trial balance…
        </p>
      ) : (
        <section className="panel table-panel">
          <div className="panel-heading">
            <div>
              <h2>Trial balance</h2>
              <p>As at {displayDate(balance.state.value.asOfDate)}</p>
            </div>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Code</th>
                  <th>Account</th>
                  <th className="numeric">Debit (AUD)</th>
                  <th className="numeric">Credit (AUD)</th>
                </tr>
              </thead>
              <tbody>
                {balance.state.value.lines.map((line) => (
                  <tr key={`${line.accountId}-${line.accountCode}`}>
                    <td>{line.accountCode}</td>
                    <td className="strong">{line.accountName}</td>
                    <td className="numeric">{formatAmount(line.debitMinor)}</td>
                    <td className="numeric">{formatAmount(line.creditMinor)}</td>
                  </tr>
                ))}
                <tr className="total-row">
                  <td colSpan={2}>Total</td>
                  <td className="numeric">{formatAmount(balance.state.value.totalDebitsMinor)}</td>
                  <td className="numeric">{formatAmount(balance.state.value.totalCreditsMinor)}</td>
                </tr>
              </tbody>
            </table>
          </div>
          {balance.state.value.lines.length === 0 ? (
            <p className="table-empty">No balances for this date</p>
          ) : null}
        </section>
      )}
    </div>
  );
}
