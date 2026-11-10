import { ArrowRight, ChevronRight, FilePlus2, FileText } from "lucide-react";
import { api } from "./api";
import { formatAmount, sumAmounts } from "./money";
import { AppLink } from "./shell";
import { useRemote } from "./use-remote";
import { displayDate } from "./utils";

export function OverviewScreen({ navigate }: { navigate: (path: string) => void }) {
  const overview = useRemote("overview", api.overview);
  const journals = useRemote("journals", api.journals);
  const summary = overview.state.status === "ready" ? overview.state.value : undefined;
  const latest = journals.state.status === "ready" ? journals.state.value.journals.slice(0, 5) : [];
  const cards = [
    {
      title: "Documents",
      value: summary ? String(summary.documentsNeedingReview) : "—",
      detail: "need review",
      path: "/documents",
    },
    {
      title: "Banking",
      value: summary ? String(summary.bankingLinesNeedingReconciliation) : "—",
      detail: "need reconciliation",
      path: "/banking",
    },
    {
      title: "GST & BAS",
      value: summary ? (summary.currentDraftBasWorkpapers > 0 ? "Draft" : "No draft") : "—",
      detail: summary?.currentDraftBasWorkpapers
        ? "retained, not lodged"
        : "local workpaper not started",
      path: "/gst-bas",
    },
  ];
  return (
    <div className="screen">
      <div className="page-heading">
        <div>
          <h1>Overview</h1>
          <p>What needs attention across documents, banking and GST</p>
        </div>
        <AppLink className="button primary" navigate={navigate} to="/journals">
          <FilePlus2 aria-hidden="true" />
          New journal
        </AppLink>
      </div>
      {overview.state.status === "error" ? (
        <p className="notice error" role="alert">
          {overview.state.message}{" "}
          <button className="text-button" onClick={overview.reload} type="button">
            Retry
          </button>
        </p>
      ) : null}
      <div className="summary-grid">
        {cards.map((card) => (
          <AppLink className="summary-card" key={card.title} navigate={navigate} to={card.path}>
            <strong>{card.title}</strong>
            <b>{card.value}</b>
            <span>{card.detail}</span>
          </AppLink>
        ))}
      </div>
      <div className="overview-grid">
        <section className="panel attention-panel">
          <h2>Needs attention</h2>
          {overview.state.status === "loading" ? (
            <p className="loading">Loading your books…</p>
          ) : summary?.attentionItems.length ? (
            <ul className="attention-list">
              {summary.attentionItems.map((item, index) => (
                <li key={`${item.kind}-${item.resourceId ?? index}`}>
                  <AppLink
                    navigate={navigate}
                    to={
                      item.kind.toLowerCase().includes("bank")
                        ? "/banking"
                        : item.kind.toLowerCase().includes("bas")
                          ? "/gst-bas"
                          : "/documents"
                    }
                  >
                    <FileText aria-hidden="true" />
                    <span>{item.label}</span>
                    <ChevronRight aria-hidden="true" />
                  </AppLink>
                </li>
              ))}
            </ul>
          ) : summary ? (
            <div className="empty-content">
              {journals.state.status === "ready" && latest.length === 0 ? (
                <>
                  <h3>Your workspace is ready</h3>
                  <p>Create your accounts, then record your first transaction</p>
                  <AppLink className="button primary" navigate={navigate} to="/journals">
                    Create your first journal
                  </AppLink>
                  <AppLink className="text-link" navigate={navigate} to="/accounts">
                    View chart of accounts <ArrowRight aria-hidden="true" />
                  </AppLink>
                </>
              ) : (
                <>
                  <h3>No items need attention</h3>
                  <p>Review your records whenever you need to</p>
                </>
              )}
            </div>
          ) : null}
        </section>
        <section className="panel next-panel">
          <h2>Next steps</h2>
          <ol className="next-list">
            {[
              ["Set up your accounts", "Create ordinary accounts to get started", "/accounts"],
              ["Record a transaction", "Add a journal to record an entry", "/journals"],
              [
                "Review your balances",
                "Check your trial balance when you’re ready",
                "/trial-balance",
              ],
            ].map(([title, detail, path], index) => (
              <li key={title}>
                <AppLink navigate={navigate} to={path ?? "/overview"}>
                  <span className="step-number">{index + 1}</span>
                  <span>
                    <strong>{title}</strong>
                    <small>{detail}</small>
                  </span>
                  <ChevronRight aria-hidden="true" />
                </AppLink>
              </li>
            ))}
          </ol>
        </section>
      </div>
      <div className="lower-grid">
        <section className="panel recent-panel">
          <div className="panel-heading">
            <h2>Recent journals</h2>
            <AppLink className="text-link" navigate={navigate} to="/journals">
              View all <ArrowRight aria-hidden="true" />
            </AppLink>
          </div>
          {journals.state.status === "error" ? (
            <p className="notice error" role="alert">
              {journals.state.message}
            </p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Date</th>
                    <th className="optional-col">Reference</th>
                    <th>Description</th>
                    <th className="numeric">Debit (AUD)</th>
                    <th className="numeric">Credit (AUD)</th>
                  </tr>
                </thead>
                <tbody>
                  {latest.map((journal) => (
                    <tr key={journal.id}>
                      <td>{displayDate(journal.date)}</td>
                      <td className="optional-col">{journal.reference || "—"}</td>
                      <td>{journal.description || "—"}</td>
                      <td className="numeric">
                        {formatAmount(sumAmounts(journal.lines.map((line) => line.debitMinor)))}
                      </td>
                      <td className="numeric">
                        {formatAmount(sumAmounts(journal.lines.map((line) => line.creditMinor)))}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {journals.state.status === "loading" ? (
                <p className="loading">Loading journals…</p>
              ) : latest.length === 0 ? (
                <div className="empty-content compact">
                  <h3>No journals yet</h3>
                  <p>Create your first journal to record a transaction</p>
                  <AppLink className="button primary" navigate={navigate} to="/journals">
                    Create your first journal
                  </AppLink>
                </div>
              ) : null}
            </div>
          )}
        </section>
        <section className="panel insight-panel">
          <h2>Intelligence</h2>
          <div className="empty-content compact">
            <h3>Built from your books</h3>
            <p>Review suggestions with links to the records behind them</p>
            <AppLink className="text-link" navigate={navigate} to="/intelligence">
              View insights <ArrowRight aria-hidden="true" />
            </AppLink>
          </div>
        </section>
      </div>
    </div>
  );
}
