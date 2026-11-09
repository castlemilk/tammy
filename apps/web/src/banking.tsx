import { type FormEvent, useState } from "react";
import { PageLimit } from "./accounting";
import { api } from "./api";
import { formatAmount, parseAmount } from "./money";
import { parseStatementRows } from "./statement";
import { useCommandKey } from "./use-command-key";
import { useRemote } from "./use-remote";
import { displayDate, errorMessage } from "./utils";

export function BankingScreen() {
  const banking = useRemote("banking", api.banking);
  const [opening, setOpening] = useState("0.00");
  const [rows, setRows] = useState("");
  const [matching, setMatching] = useState<string>();
  const [reference, setReference] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const importCommand = useCommandKey();
  const matchCommand = useCommandKey();
  const reconcileCommand = useCommandKey();
  const data = banking.state.status === "ready" ? banking.state.value : undefined;
  const selected = data?.lines.find((line) => line.id === matching);
  const canReconcile =
    !!data &&
    !data.page?.mayHaveMore &&
    data.lines.some((line) => line.status === "matched") &&
    !data.lines.some((line) => line.status === "unmatched");

  async function importRows(event: FormEvent) {
    event.preventDefault();
    setError(undefined);
    try {
      const openingBalanceMinor = parseAmount(opening);
      if (openingBalanceMinor === undefined)
        throw new Error("Enter a valid opening balance with no more than two decimal places.");
      const payload = { openingBalanceMinor, lines: parseStatementRows(rows) };
      setBusy(true);
      await api.importBank({ ...payload, idempotencyKey: importCommand.keyFor(payload) });
      importCommand.clear();
      setRows("");
      banking.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }
  async function match(event: FormEvent) {
    event.preventDefault();
    if (!selected) return;
    const payload = { version: selected.version, reference: reference.trim() };
    if (!payload.reference) {
      setError("Enter a match reference.");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await api.matchBank(selected.id, {
        ...payload,
        idempotencyKey: matchCommand.keyFor({ id: selected.id, ...payload }),
      });
      matchCommand.clear();
      setMatching(undefined);
      setReference("");
      banking.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }
  async function reconcile() {
    if (!canReconcile) return;
    setBusy(true);
    setError(undefined);
    try {
      await api.reconcileBank({
        idempotencyKey: reconcileCommand.keyFor(
          data?.lines.map((line) => [line.id, line.version, line.status]),
        ),
      });
      reconcileCommand.clear();
      banking.reload();
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
          <h1>Banking</h1>
          <p>Import statement rows, match them, then complete reconciliation</p>
        </div>
      </div>
      <section className="panel form-panel">
        <div className="panel-heading">
          <div>
            <h2>Import a statement</h2>
            <p>
              Paste one row per line: YYYY-MM-DD, description, signed amount. This does not connect
              a bank feed.
            </p>
          </div>
        </div>
        <form className="form-stack" onSubmit={importRows}>
          <label>
            Opening balance (AUD)
            <input
              inputMode="decimal"
              onChange={(event) => setOpening(event.target.value)}
              required
              value={opening}
            />
          </label>
          <label>
            Statement rows
            <textarea
              onChange={(event) => setRows(event.target.value)}
              placeholder="2024-05-12, Office supplies, -319.00"
              required
              value={rows}
            />
          </label>
          <div>
            <button className="button primary" disabled={busy} type="submit">
              {busy ? "Working…" : "Import statement"}
            </button>
          </div>
        </form>
      </section>
      {error ? (
        <p className="notice error" role="alert">
          {error}
        </p>
      ) : null}
      {data?.summary && data.summary.importedLineCount > 0 ? (
        <section className="panel">
          <h2>Statement summary</h2>
          <dl className="summary-list">
            <div>
              <dt>Latest closing balance</dt>
              <dd>{formatAmount(data.summary.latestClosingBalanceMinor)}</dd>
            </div>
            <div>
              <dt>Unmatched lines</dt>
              <dd>{data.summary.unmatchedLineCount}</dd>
            </div>
            <div>
              <dt>Unreconciled lines</dt>
              <dd>{data.summary.unreconciledLineCount}</dd>
            </div>
          </dl>
        </section>
      ) : null}
      {banking.state.status === "error" ? (
        <p className="notice error" role="alert">
          {banking.state.message}{" "}
          <button className="text-button" onClick={banking.reload} type="button">
            Retry
          </button>
        </p>
      ) : banking.state.status === "loading" ? (
        <p className="loading" role="status">
          Loading banking…
        </p>
      ) : (
        <>
          <PageLimit page={banking.state.value.page} />
          <section className="panel table-panel">
            <div className="panel-heading">
              <div>
                <h2>Statement lines</h2>
                <p>Matched lines are reconciled only after you complete the reconciliation</p>
              </div>
              <button
                className="button secondary"
                disabled={!canReconcile || busy}
                onClick={() => void reconcile()}
                type="button"
              >
                Complete reconciliation
              </button>
            </div>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Date</th>
                    <th>Description</th>
                    <th className="numeric">Amount (AUD)</th>
                    <th>Status</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  {banking.state.value.lines.map((line) => (
                    <tr key={line.id}>
                      <td>{displayDate(line.date)}</td>
                      <td className="strong">{line.description}</td>
                      <td className="numeric">{formatAmount(line.amountMinor)}</td>
                      <td>
                        <span
                          className={`status ${line.status === "reconciled" ? "ready" : "pending"}`}
                        >
                          {line.status === "reconciled"
                            ? "Reconciled"
                            : line.status === "matched"
                              ? "Matched"
                              : "Unmatched"}
                        </span>
                      </td>
                      <td>
                        {line.status === "unmatched" ? (
                          <button
                            className="text-button"
                            onClick={() => {
                              setMatching(line.id);
                              setReference("");
                              setError(undefined);
                            }}
                            type="button"
                          >
                            Match
                          </button>
                        ) : (
                          "—"
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {banking.state.value.lines.length === 0 ? (
              <p className="table-empty">No statement lines yet</p>
            ) : null}
          </section>
        </>
      )}
      {selected ? (
        <section className="panel form-panel">
          <div className="panel-heading">
            <div>
              <h2>Match statement line</h2>
              <p>
                {selected.description} · {formatAmount(selected.amountMinor)}
              </p>
            </div>
            <button className="text-button" onClick={() => setMatching(undefined)} type="button">
              Cancel
            </button>
          </div>
          <form className="inline-form" onSubmit={match}>
            <label>
              Match reference
              <input
                onChange={(event) => setReference(event.target.value)}
                required
                value={reference}
              />
            </label>
            <button className="button primary" disabled={busy} type="submit">
              Mark matched
            </button>
          </form>
        </section>
      ) : null}
    </div>
  );
}
