import { type FormEvent, useState } from "react";
import { api } from "./api";
import { formatAmount } from "./money";
import { useCommandKey } from "./use-command-key";
import { useRemote } from "./use-remote";
import { displayDate, errorMessage } from "./utils";

export function BasScreen() {
  const bas = useRemote("bas", api.bas);
  const [start, setStart] = useState("2024-04-01");
  const [end, setEnd] = useState("2024-06-30");
  const [showForm, setShowForm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const command = useCommandKey();
  const workpaper = bas.state.status === "ready" ? bas.state.value.workpaper : null;
  async function createDraft(event: FormEvent) {
    event.preventDefault();
    if (!start.startsWith("2024-") || !end.startsWith("2024-") || start > end) {
      setError(
        "Choose a period that starts and ends within calendar 2024, with the start on or before the end.",
      );
      return;
    }
    const payload = { periodStart: start, periodEnd: end };
    setBusy(true);
    setError(undefined);
    try {
      await api.createBas({ ...payload, idempotencyKey: command.keyFor(payload) });
      command.clear();
      setShowForm(false);
      bas.reload();
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
          <h1>GST &amp; BAS</h1>
          <p>Review a local GST workpaper from reviewed documents</p>
        </div>
        {workpaper ? (
          <button className="button secondary" onClick={() => setShowForm(!showForm)} type="button">
            Create another draft
          </button>
        ) : null}
      </div>
      <p className="notice">
        Drafts cover periods within calendar 2024. Full BAS preparation, declaration and lodgement
        are not available in the web app.
      </p>
      {error ? (
        <p className="notice error" role="alert">
          {error}
        </p>
      ) : null}
      {bas.state.status === "error" ? (
        <p className="notice error" role="alert">
          {bas.state.message}{" "}
          <button className="text-button" onClick={bas.reload} type="button">
            Retry
          </button>
        </p>
      ) : bas.state.status === "loading" ? (
        <p className="loading" role="status">
          Loading GST workpaper…
        </p>
      ) : (
        <>
          {!workpaper || showForm ? (
            <section className="panel form-panel">
              <div className="panel-heading">
                <div>
                  <h2>Create a local draft</h2>
                  <p>
                    Reviewed documents not yet posted as purchases are listed as provisional GST
                    candidates, whatever their date
                  </p>
                </div>
              </div>
              <form className="inline-form" onSubmit={createDraft}>
                <label>
                  Period start
                  <input
                    onChange={(event) => setStart(event.target.value)}
                    required
                    type="date"
                    value={start}
                  />
                </label>
                <label>
                  Period end
                  <input
                    onChange={(event) => setEnd(event.target.value)}
                    required
                    type="date"
                    value={end}
                  />
                </label>
                <button className="button primary" disabled={busy} type="submit">
                  {busy ? "Creating…" : "Create local draft"}
                </button>
              </form>
            </section>
          ) : null}
          {workpaper ? (
            <>
              <div className="bas-heading">
                <div>
                  <h2>GST workpaper</h2>
                  <p>
                    {displayDate(workpaper.periodStart)} – {displayDate(workpaper.periodEnd)}
                  </p>
                </div>
                <span className="status pending">Draft — not lodged</span>
              </div>
              {workpaper.sourcesChanged ? (
                <p className="notice" role="status">
                  Records changed after this draft was created. Create another draft to include the
                  changes.
                </p>
              ) : null}
              {workpaper.reviewWarnings.length ? (
                <div className="notice">
                  <p>Review before relying on this draft</p>
                  <ul>
                    {workpaper.reviewWarnings.map((warning) => (
                      <li key={warning}>{warning}</li>
                    ))}
                  </ul>
                </div>
              ) : null}
              {workpaper.limitations.length ? (
                <div className="notice">
                  <p>About this draft</p>
                  <ul>
                    {workpaper.limitations.map((limitation) => (
                      <li key={limitation}>{limitation}</li>
                    ))}
                  </ul>
                </div>
              ) : null}
              <section className="panel">
                <h2>GST summary</h2>
                <dl className="summary-list">
                  <div>
                    <dt>Sales (G1)</dt>
                    <dd>{formatAmount(workpaper.salesG1Minor)}</dd>
                  </div>
                  <div>
                    <dt>GST on sales (1A)</dt>
                    <dd>{formatAmount(workpaper.gstOnSales1aMinor)}</dd>
                  </div>
                  <div>
                    <dt>GST credits (1B)</dt>
                    <dd>{formatAmount(workpaper.gstCredits1bMinor)}</dd>
                  </div>
                  <div className="total-row">
                    <dt>Net GST position</dt>
                    <dd>{formatAmount(workpaper.netGstPayableMinor)}</dd>
                  </div>
                </dl>
              </section>
              {workpaper.sources.length || workpaper.candidateGstMinor === null ? (
                <section className="panel table-panel">
                  <div className="panel-heading">
                    <div>
                      <h2>Reviewed source documents</h2>
                      <p>Purchase documents contributing to GST credits (1B)</p>
                    </div>
                  </div>
                  <div className="table-wrap">
                    <table>
                      <thead>
                        <tr>
                          <th>Date</th>
                          <th>Supplier</th>
                          <th>Invoice</th>
                          <th className="numeric">Gross (AUD)</th>
                          <th className="numeric">GST credit (AUD)</th>
                        </tr>
                      </thead>
                      <tbody>
                        {workpaper.sources.map((source) => (
                          <tr key={`${source.id}:${source.date}:${source.gstCreditMinor}`}>
                            <td>{displayDate(source.date)}</td>
                            <td>{source.supplierName || "—"}</td>
                            <td>{source.invoiceNumber || "—"}</td>
                            <td className="numeric">{formatAmount(source.grossMinor)}</td>
                            <td className="numeric">{formatAmount(source.gstCreditMinor)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  {workpaper.sources.length === 0 ? (
                    <p className="table-empty">No reviewed purchase documents in this period</p>
                  ) : null}
                </section>
              ) : null}
              {workpaper.candidateGstMinor === null ? null : (
                <section className="panel table-panel">
                  <div className="panel-heading">
                    <div>
                      <h2>Provisional GST candidates</h2>
                      <p>
                        Reviewed documents not yet posted as purchases, of any date, kept out of GST
                        credits (1B)
                      </p>
                    </div>
                  </div>
                  <div className="table-wrap">
                    <table>
                      <thead>
                        <tr>
                          <th>Date</th>
                          <th>Supplier</th>
                          <th>Invoice</th>
                          <th className="numeric">Candidate GST (AUD)</th>
                        </tr>
                      </thead>
                      <tbody>
                        {workpaper.candidates.map((candidate) => (
                          <tr key={`${candidate.id}:${candidate.candidateGstMinor}`}>
                            <td>{displayDate(candidate.date)}</td>
                            <td>{candidate.supplierName || "—"}</td>
                            <td>{candidate.invoiceNumber || "—"}</td>
                            <td className="numeric">{formatAmount(candidate.candidateGstMinor)}</td>
                          </tr>
                        ))}
                        {workpaper.candidates.length ? (
                          <tr className="total-row">
                            <td colSpan={3}>Total provisional candidates, all dates</td>
                            <td className="numeric">{formatAmount(workpaper.candidateGstMinor)}</td>
                          </tr>
                        ) : null}
                      </tbody>
                    </table>
                  </div>
                  {workpaper.candidates.length === 0 ? (
                    <p className="table-empty">No provisional GST candidates</p>
                  ) : null}
                </section>
              )}
            </>
          ) : null}
        </>
      )}
    </div>
  );
}
