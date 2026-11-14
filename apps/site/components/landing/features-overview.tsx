/*
 * The Overview scene of the features band: the app's "Business income" panel,
 * read from the journal lines the workflow band posted. Server component. The
 * butter trace between a figure and its entries is CSS only (:has() in
 * features.css), so it works before hydration and without script.
 */
import type { CSSProperties, ReactNode } from "react";
import { aud } from "../../lib/tools/calculations";
import { AppWindow, Icon, Money, Note, Pill } from "./kit";
import { FeaturesTrace } from "./features-trace";
import { accounts, business, invoice, overview, trialBalance } from "./scenario";

type Trace = "income" | "invoice" | "received" | "owing";

/** Stagger index for the shared data-enter entrances. */
const at = (index: number) => ({ "--i": index }) as CSSProperties;

/** The quarter's two ends, taken from the scenario's own period line. */
const [quarterStart = "", quarterEnd = ""] = business.quarter.split(" to ");
const quarterYear = quarterEnd.slice(-4);

/** The exporter writes plain decimals: no symbol, no thousands separator. */
const plain = (cents: number) => `${Math.floor(cents / 100)}.${String(cents % 100).padStart(2, "0")}`;
/** An amount inside a line of working, where the dollar sign would be noise. */
const bare = (cents: number) => aud(cents).slice(1);

const fields: readonly { label: string; value: string; icon: "calendar" | null; fold?: boolean }[] = [
  { label: "Income period", value: "This quarter", icon: null },
  { label: "Income period start", value: `${quarterStart} ${quarterYear}`, icon: "calendar", fold: true },
  { label: "Income period end", value: quarterEnd, icon: "calendar", fold: true },
  { label: "Balances as of", value: trialBalance.asAt, icon: "calendar" },
];

const metrics: readonly { trace: Trace; label: string; cents: number; prominent?: boolean }[] = [
  { trace: "income", label: "Business income (ex GST)", cents: overview.businessIncomeCents, prominent: true },
  { trace: "invoice", label: "Invoice income (ex GST)", cents: overview.invoiceIncomeCents },
  { trace: "received", label: "Payments received", cents: overview.paymentsReceivedCents },
  { trace: "owing", label: "Invoices owing", cents: overview.invoicesOwingCents },
];

/**
 * The two journals behind the panel, with the figure each line feeds.
 * `trace` names the figures a line feeds and `reads` the figures a journal
 * feeds (both matched whole in features.css); `start` is the entry's first
 * row in the arrival order.
 */
/** The invoice number never breaks at its hyphen. */
const number = <span className="lp-num">{invoice.number}</span>;

const entries = [
  {
    start: 0,
    date: invoice.issueShort,
    memo: <>Invoice {number}</>,
    memoTrace: "invoice",
    reads: "income invoice owing",
    lines: [
      { ...invoice.issueLines[0], trace: "owing" },
      { ...invoice.issueLines[1], trace: "income invoice" },
      { ...invoice.issueLines[2], trace: "" },
    ],
  },
  {
    start: 1 + invoice.issueLines.length,
    date: invoice.payment.dateShort,
    memo: <>Payment for invoice {number}</>,
    memoTrace: "received",
    reads: "received owing",
    lines: [
      { ...invoice.receiptLines[0], trace: "received" },
      { ...invoice.receiptLines[1], trace: "owing" },
    ],
  },
] as const;

const code = (account: { code: string; name: string }) => (
  <>
    <span className="lp-mono">{account.code}</span> {account.name}
  </>
);

/** What each figure is read from, in words: also the buttons' descriptions. */
const working: readonly { trace: Trace; text: ReactNode }[] = [
  {
    trace: "income",
    text: (
      <>
        Business income is posted revenue before GST: the <Money cents={invoice.subtotalCents} /> credit to{" "}
        {code(accounts.sales)}.
      </>
    ),
  },
  {
    trace: "invoice",
    text: (
      <>
        Invoice income is the part that came from invoices: {number}, the same{" "}
        <Money cents={invoice.subtotalCents} />. Its <Money cents={invoice.gstCents} /> of GST is not income.
      </>
    ),
  },
  {
    trace: "received",
    text: (
      <>
        Payments received is the receipt recorded on {invoice.payment.date}:{" "}
        <Money cents={invoice.receivedCents} /> into {code(accounts.bank)}. It does not add income again.
      </>
    ),
  },
  {
    trace: "owing",
    text: (
      <>
        Invoices owing is what is left on {code(accounts.receivable)} on the day you picked:{" "}
        <span className="lp-mono lp-num">
          {bare(invoice.totalCents)} − {bare(invoice.receivedCents)} = {bare(invoice.outstandingCents)}
        </span>
        .
      </>
    ),
  },
];

export function FeaturesOverview() {
  return (
    <div className="lp-features-ov" data-scene="">
      <div className="lp-features-ov-head">
        <h3 className="lp-h3">The same entries, read as income.</h3>
        <p className="lp-body">What came in and what is owed, on the day you pick.</p>
      </div>
      <Note className="lp-features-ov-note">
        A summary of posted income. It is not a profit and loss, balance sheet or cash-flow statement.
      </Note>

      <AppWindow className="lp-features-ov-window" title={`${business.name} / Overview`}>
        <div className="lp-features-ov-body">
          <div className="lp-features-ov-panel">
            <div className="lp-features-ov-panel-head">
              <p className="lp-features-ov-panel-title">Business income</p>
              <p className="lp-features-ov-panel-caption">
                Posted revenue excluding GST, with invoice receipts and balances.
              </p>
            </div>

            <dl className="lp-features-ov-fields">
              {fields.map((field) => (
                <div key={field.label} data-fold={field.fold ? "" : undefined}>
                  <dt>{field.label}</dt>
                  <dd>
                    <span className="lp-num">{field.value}</span>
                    {field.icon ? <Icon name={field.icon} /> : <Chevron />}
                  </dd>
                </div>
              ))}
            </dl>

            <dl className="lp-features-ov-metrics">
              {metrics.map((metric, index) => (
                <div
                  key={metric.trace}
                  className="lp-features-ov-metric"
                  data-prominent={metric.prominent ? "" : undefined}
                  data-enter="fade"
                  style={at(9 + index)}
                >
                  <dt>{metric.label}</dt>
                  <dd>
                    <button
                      type="button"
                      className="lp-trace lp-amount"
                      data-trace={metric.trace}
                      aria-label={`${metric.label} ${aud(metric.cents)}`}
                      aria-describedby={`features-ov-working-${metric.trace}`}
                    >
                      <Money cents={metric.cents} />
                    </button>
                  </dd>
                </div>
              ))}
            </dl>

            {/* Pressing these here would do nothing, so they are drawn, not built. */}
            <div className="lp-features-ov-export" aria-hidden="true">
              <span>Export PDF</span>
              <span>Export CSV</span>
            </div>
          </div>

          <div className="lp-features-ov-source">
            <table className="lp-table lp-features-ov-entries">
              <caption>From Journals: the entries behind these figures</caption>
              <thead>
                <tr>
                  <th scope="col">Account</th>
                  <th scope="col" data-num="">Debit</th>
                  <th scope="col" data-num="">Credit</th>
                </tr>
              </thead>
              {entries.map((entry) => (
                <tbody key={entry.date}>
                  <tr className="lp-features-ov-entry" data-reads={entry.reads} data-enter="slide" style={at(entry.start)}>
                    <th scope="rowgroup" colSpan={3}>
                      <span className="lp-features-ov-entry-date lp-num">{entry.date}</span>
                      <span className="lp-trace-target lp-features-ov-mark" data-trace={entry.memoTrace}>
                        {entry.memo}
                      </span>
                      <Pill tone="ok">Posted</Pill>
                    </th>
                  </tr>
                  {entry.lines.map((line, index) => (
                    <tr
                      key={line.account.code}
                      data-reads={line.trace || undefined}
                      data-enter="slide"
                      style={at(entry.start + 1 + index)}
                    >
                      <th scope="row">
                        <span className="lp-code">{line.account.code}</span>
                        <span className="lp-trace-target lp-features-ov-mark" data-trace={line.trace || undefined}>
                          {line.account.name}
                        </span>
                      </th>
                      {(["debit", "credit"] as const).map((side) => (
                        <td key={side} data-num="">
                          {line.side === side ? (
                            <span
                              className="lp-trace-target lp-features-ov-mark lp-amount"
                              data-trace={line.trace || undefined}
                            >
                              {aud(line.cents)}
                            </span>
                          ) : null}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              ))}
            </table>

            <div className="lp-features-ov-working">
              <p data-for="rest">Each figure is read from these posted entries. Press one and this page traces it; in the app you open the entries in Journals.</p>
              {working.map((item) => (
                <p key={item.trace} data-for={item.trace} id={`features-ov-working-${item.trace}`}>
                  {item.text}
                </p>
              ))}
            </div>
          </div>
        </div>
      </AppWindow>

      <div className="lp-features-ov-out">
        <ul className="lp-features-ov-files">
          <li>
            <div className="lp-sheet lp-features-ov-sheet" data-kind="pdf" aria-hidden="true">
              <span className="lp-features-ov-sheet-kind">PDF</span>
              <span className="lp-features-ov-sheet-title">Income summary</span>
              {metrics.map((metric) => (
                <span key={metric.trace} className="lp-features-ov-sheet-row">
                  <span className="lp-features-ov-sheet-bar" />
                  <span className="lp-num">{aud(metric.cents)}</span>
                </span>
              ))}
            </div>
            <span className="lp-features-ov-file-name">income-summary.pdf</span>
          </li>
          <li>
            <div className="lp-sheet lp-features-ov-sheet" data-kind="csv" aria-hidden="true">
              <span className="lp-features-ov-sheet-kind">CSV</span>
              <span className="lp-features-ov-sheet-row" data-head="">
                <span className="lp-features-ov-sheet-bar" />
                <span className="lp-features-ov-sheet-bar" />
              </span>
              {metrics.map((metric) => (
                <span key={metric.trace} className="lp-features-ov-sheet-row">
                  <span className="lp-features-ov-sheet-bar" />
                  <span className="lp-num">{plain(metric.cents)}</span>
                </span>
              ))}
            </div>
            <span className="lp-features-ov-file-name">income-summary.csv</span>
          </li>
        </ul>
        <p className="lp-micro lp-features-ov-out-note">Exported copies sit outside the encrypted workspace.</p>
      </div>
      <FeaturesTrace />
    </div>
  );
}

/** The select's chevron, as the app draws it. Decorative. */
function Chevron() {
  return (
    <svg className="lp-icon lp-features-ov-chevron" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="m7 10 5 5 5-5" />
    </svg>
  );
}
