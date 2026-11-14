/*
 * The nine screens of the hero's illustrative window. All nine are siblings in
 * the server HTML; hero.css shows the one whose rail radio is checked, so the
 * rail works before hydration and with JavaScript off.
 *
 * Every figure comes from scenario.ts. Labels, column heads and status words
 * are the desktop app's own (apps/desktop/src/app/features/*).
 *
 * <Enc> is the record "as stored": an aria-hidden cover drawn over a real cell
 * or tile. Its glyphs are authored strings held in hero.css, so nothing
 * unreadable reaches the text of the page, and the real value underneath is
 * always present, whole, at its final amount.
 */
import type { CSSProperties, ReactNode } from "react";
import { Icon, type IconName, Money, Pill, Tick } from "./kit";
import type { ScreenId } from "./hero-screens";
import {
  accounts,
  business,
  gstWorkpaper,
  invoice,
  manualJournal,
  overview,
  pendingDocument,
  proposedInvoice,
  purchase,
  statement,
  trialBalance,
} from "./scenario";

/** Stagger index for the resolve: rows clear top to bottom. */
const at = (index: number) => ({ "--r": index }) as CSSProperties;

function Enc({ g = 0, bar }: { g?: number; bar?: number }) {
  return bar === undefined ? (
    <span className="lp-hero-enc" data-g={g % 8} aria-hidden="true" />
  ) : (
    <span className="lp-hero-enc" data-bar={(bar % 4) + 1} aria-hidden="true" />
  );
}

function Panel({
  id,
  eyebrow,
  title,
  meta,
  children,
}: {
  id: ScreenId;
  eyebrow: string;
  title: string;
  meta?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="lp-hero-panel" data-panel={id}>
      <header className="lp-hero-panel-head">
        <div>
          <p className="lp-app-eyebrow">{eyebrow}</p>
          <p className="lp-app-title lp-hero-panel-title">{title}</p>
        </div>
        {meta}
      </header>
      {children}
    </div>
  );
}

/* ---------- Overview ---------- */

// What the hero's own Documents and Banking panels show: one document still to
// review and one reviewed; two imported statement lines, neither reconciled.
const documentsNeedingReview = 1;
const documentsReviewed = 1;
const bankLinesUnreconciled = statement.rows.length;

function OverviewPanel() {
  return (
    <Panel id="overview" eyebrow={"Thursday " + trialBalance.asAt} title="Overview">
      <div className="lp-hero-ov">
        <div className="lp-hero-card">
          <div className="lp-hero-card-head">
            <p className="lp-hero-card-title">Business income</p>
            <p className="lp-hero-card-meta">This quarter · {business.quarterShort}</p>
          </div>
          <dl className="lp-hero-metrics lp-hero-ov-metrics">
            <div className="lp-hero-metric lp-hero-stored" data-lead="" style={at(0)}>
              <dt>Business income (ex GST)</dt>
              <dd>
                <Money cents={overview.businessIncomeCents} />
                <Enc g={0} />
              </dd>
            </div>
            <div className="lp-hero-metric lp-hero-stored" style={at(1)}>
              <dt>Invoice income (ex GST)</dt>
              <dd>
                <Money cents={overview.invoiceIncomeCents} />
                <Enc g={3} />
              </dd>
            </div>
            <div className="lp-hero-metric lp-hero-stored" style={at(2)}>
              <dt>Payments received</dt>
              <dd>
                <Money cents={overview.paymentsReceivedCents} />
                <Enc g={5} />
              </dd>
            </div>
            <div className="lp-hero-metric lp-hero-stored" style={at(3)}>
              <dt>Invoices owing</dt>
              <dd>
                <Money cents={overview.invoicesOwingCents} />
                <Enc g={6} />
              </dd>
            </div>
          </dl>
          <p className="lp-hero-card-foot lp-hero-stored" style={at(4)}>
            Invoice GST attributed in this period: <Money cents={invoice.gstCents} /> ({business.basis.toLowerCase()}).
            <Enc g={2} />
          </p>
        </div>
        <div className="lp-hero-card">
          <div className="lp-hero-card-head">
            <p className="lp-hero-card-title">Needs review</p>
          </div>
          <ul className="lp-hero-review">
            <li className="lp-hero-stored" style={at(1)}>
              <Icon name="document" />
              <span className="lp-hero-review-text">
                <strong>Documents</strong>
                <span className="lp-hero-sub">{documentsReviewed} reviewed this quarter</span>
              </span>
              <span className="lp-hero-count">{documentsNeedingReview}</span>
              <Enc g={1} />
            </li>
            <li className="lp-hero-stored" style={at(2)}>
              <Icon name="bank" />
              <span className="lp-hero-review-text">
                <strong>Banking</strong>
                <span className="lp-hero-sub">{bankLinesUnreconciled} unreconciled this quarter</span>
              </span>
              <span className="lp-hero-count">{bankLinesUnreconciled}</span>
              <Enc g={4} />
            </li>
            <li className="lp-hero-stored" style={at(3)}>
              <Icon name="gst" />
              <span className="lp-hero-review-text">
                <strong>GST &amp; BAS</strong>
                <span className="lp-hero-sub">{gstWorkpaper.status}</span>
              </span>
              <span className="lp-hero-count">Draft</span>
              <Enc g={7} />
            </li>
          </ul>
        </div>
      </div>
    </Panel>
  );
}

/* ---------- Documents ---------- */

const documents = [
  {
    file: pendingDocument.file,
    kind: pendingDocument.fileKind,
    supplier: pendingDocument.supplier,
    reference: pendingDocument.reference,
    date: pendingDocument.dateShort,
    cents: pendingDocument.totalCents,
    reviewed: false,
  },
  {
    file: purchase.file,
    kind: purchase.fileKind,
    supplier: purchase.supplier,
    reference: purchase.reference,
    date: purchase.dateShort,
    cents: purchase.totalCents,
    reviewed: true,
  },
] as const;

function DocumentsPanel() {
  return (
    <Panel id="documents" eyebrow="Documents" title="Documents">
      <div className="lp-hero-card">
        <table className="lp-table lp-hero-table lp-hero-docs">
          <caption className="lp-visually-hidden">Source documents kept in the workspace</caption>
          <thead>
            <tr>
              <th scope="col">Document &amp; supplier</th>
              <th scope="col" data-col="ref">
                Invoice
              </th>
              <th scope="col" data-col="date">
                Date
              </th>
              <th scope="col" data-num="">
                Total · AUD
              </th>
              <th scope="col">Status</th>
            </tr>
          </thead>
          <tbody>
            {documents.map((doc, index) => (
              <tr key={doc.file} style={at(index)}>
                <th scope="row" data-col="doc">
                  <span className="lp-hero-doc">
                    <span className="lp-hero-filekind" data-kind={doc.kind}>
                      {doc.kind}
                    </span>
                    <span className="lp-hero-doc-text">
                      <span className="lp-hero-doc-file">{doc.file}</span>
                      <span className="lp-hero-sub">{doc.supplier}</span>
                    </span>
                  </span>
                  <Enc g={index} />
                </th>
                <td data-col="ref">
                  {doc.reference}
                  <Enc g={index + 3} />
                </td>
                <td data-col="date">
                  {doc.date}
                  <Enc g={index + 5} />
                </td>
                <td data-col="total" data-num="">
                  <Money className="lp-hero-strong" cents={doc.cents} />
                  <Enc bar={index} />
                </td>
                <td data-col="status">
                  {doc.reviewed ? (
                    <Pill tone="ok" tick>
                      Reviewed
                    </Pill>
                  ) : (
                    <Pill tone="warn" dot>
                      Needs review
                    </Pill>
                  )}
                  <Enc g={index + 6} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="lp-hero-card-foot">
          {documents.length} shown · {documents.length} loaded (up to 200) · Files stay in this workspace
        </p>
      </div>
      <div className="lp-hero-card">
        <div className="lp-hero-card-head">
          <p className="lp-hero-card-title">Purchase accounting</p>
          <p className="lp-hero-card-meta lp-hero-stored" style={at(2)}>
            {purchase.reference} · Added to accounts · {business.basis}
            <Enc g={5} />
          </p>
        </div>
        <dl className="lp-hero-metrics lp-hero-metrics--row">
          <div className="lp-hero-metric lp-hero-stored" style={at(2)}>
            <dt>Gross</dt>
            <dd>
              <Money cents={purchase.totalCents} />
              <Enc g={2} />
            </dd>
          </div>
          <div className="lp-hero-metric lp-hero-stored" style={at(2)}>
            <dt>Eligible GST</dt>
            <dd>
              <Money cents={purchase.gstCents} />
              <Enc g={4} />
            </dd>
          </div>
          <div className="lp-hero-metric lp-hero-stored" style={at(3)}>
            <dt>Paid</dt>
            <dd>
              <Money cents={purchase.payment.cents} />
              <Enc g={7} />
            </dd>
          </div>
          <div className="lp-hero-metric lp-hero-stored" style={at(3)}>
            <dt>Outstanding</dt>
            <dd>
              <Money cents={purchase.totalCents - purchase.payment.cents} />
              <Enc g={1} />
            </dd>
          </div>
        </dl>
      </div>
    </Panel>
  );
}

/* ---------- Invoices ---------- */

const invoiceList = [
  {
    number: proposedInvoice.number,
    customer: proposedInvoice.customer,
    cents: proposedInvoice.totalCents,
    issued: false,
  },
  { number: invoice.number, customer: invoice.customer, cents: invoice.totalCents, issued: true },
] as const;

function InvoicesPanel() {
  return (
    <Panel id="invoices" eyebrow="Sales · Invoices" title="Invoices">
      <div className="lp-hero-card">
        <table className="lp-table lp-hero-table lp-hero-rows lp-hero-invoices">
          <caption className="lp-visually-hidden">Saved invoices</caption>
          <thead>
            <tr>
              <th scope="col">Number</th>
              <th scope="col">Customer</th>
              <th scope="col" data-num="">
                Invoice total · AUD
              </th>
              <th scope="col">Status</th>
            </tr>
          </thead>
          <tbody>
            {invoiceList.map((item, index) => (
              <tr key={item.number} style={at(index)}>
                <th scope="row" data-col="number">
                  {item.number}
                  <Enc g={index + 2} />
                </th>
                <td data-col="customer">
                  {item.customer}
                  <Enc g={index + 4} />
                </td>
                <td data-col="total" data-num="">
                  <Money className="lp-hero-strong" cents={item.cents} />
                  <Enc bar={index + 1} />
                </td>
                <td data-col="status">
                  {item.issued ? <Pill tone="ok">Issued</Pill> : <Pill tone="warn">Draft</Pill>}
                  <Enc g={index + 6} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="lp-hero-card">
        <div className="lp-hero-card-head">
          <p className="lp-hero-card-title">Payments</p>
          <p className="lp-hero-card-meta lp-hero-stored" style={at(2)}>
            {invoice.number} · {business.basis} · GST recorded on issue
            <Enc g={1} />
          </p>
        </div>
        <dl className="lp-hero-metrics lp-hero-metrics--row">
          <div className="lp-hero-metric lp-hero-stored" style={at(2)}>
            <dt>Invoice total (AUD)</dt>
            <dd>
              <Money cents={invoice.totalCents} />
              <Enc g={0} />
            </dd>
          </div>
          <div className="lp-hero-metric lp-hero-stored" style={at(3)}>
            <dt>Received</dt>
            <dd>
              <Money cents={invoice.receivedCents} />
              <Enc g={3} />
            </dd>
          </div>
          <div className="lp-hero-metric lp-hero-stored" data-owing="" style={at(4)}>
            <dt>Outstanding</dt>
            <dd>
              <Money cents={invoice.outstandingCents} />
              <Enc g={5} />
            </dd>
          </div>
        </dl>
        <p className="lp-hero-card-foot lp-hero-stored" style={at(5)}>
          Due {invoice.dueShort}. Payment recorded {invoice.payment.dateShort} · {invoice.payment.reference} ·{" "}
          <Money cents={invoice.payment.cents} />
          <Enc g={7} />
        </p>
      </div>
    </Panel>
  );
}

/* ---------- Banking ---------- */

// The supplier payment has been matched by a person; the receipt has not.
const bankLines = [
  { ...statement.rows[0], matched: true },
  { ...statement.rows[1], matched: false },
] as const;
const bankLinesUnmatched = bankLines.filter((line) => !line.matched).length;

function BankingPanel() {
  return (
    <Panel id="banking" eyebrow="Banking" title="Banking">
      <div className="lp-hero-card">
        <div className="lp-hero-card-head">
          <p className="lp-hero-card-title lp-hero-card-title--serif">Statement lines</p>
          <p className="lp-hero-card-meta">Imported CSV · no live bank feed</p>
        </div>
        <table className="lp-table lp-hero-table lp-hero-rows lp-hero-bank">
          <caption className="lp-visually-hidden">Imported bank-statement lines</caption>
          <thead>
            <tr>
              <th scope="col">Date</th>
              <th scope="col">Description &amp; reference</th>
              <th scope="col" data-num="">
                Amount · AUD
              </th>
              <th scope="col">State</th>
            </tr>
          </thead>
          <tbody>
            {bankLines.map((line, index) => (
              <tr key={line.reference} style={at(index)}>
                <td data-col="date">
                  {line.date}
                  <Enc g={index + 5} />
                </td>
                <th scope="row" data-col="desc">
                  <span className="lp-hero-strong">{line.description}</span>
                  <span className="lp-hero-sub">{line.matched ? line.reference : "No reviewed reference yet"}</span>
                  <Enc g={index + 1} />
                </th>
                <td data-col="amount" data-num="">
                  <Money className="lp-hero-strong" cents={line.cents} />
                  <Enc bar={index + 2} />
                </td>
                <td data-col="state">
                  {line.matched ? (
                    <Pill tone="ok" tick>
                      Matched
                    </Pill>
                  ) : (
                    <Pill tone="warn">Unmatched</Pill>
                  )}
                  <Enc g={index + 3} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="lp-hero-bank-summary">
          <p className="lp-hero-card-title">Reconciliation summary</p>
          <dl className="lp-hero-metrics lp-hero-metrics--row">
            <div className="lp-hero-metric lp-hero-stored" style={at(2)}>
              <dt>Latest import balance · AUD</dt>
              <dd>
                <Money cents={statement.closingCents} />
                <Enc g={6} />
              </dd>
            </div>
            <div className="lp-hero-metric lp-hero-stored" style={at(3)}>
              <dt>Unmatched</dt>
              <dd>
                <span className="lp-num">{bankLinesUnmatched}</span>
                <Enc g={0} />
              </dd>
            </div>
            <div className="lp-hero-metric lp-hero-stored" style={at(3)}>
              <dt>Unreconciled</dt>
              <dd>
                <span className="lp-num">{bankLinesUnreconciled}</span>
                <Enc g={2} />
              </dd>
            </div>
          </dl>
          <p className="lp-hero-bank-next">Review every unmatched line before completing reconciliation.</p>
        </div>
      </div>
    </Panel>
  );
}

/* ---------- Chart of accounts ---------- */

// Protection follows the installed chart (services/core/internal/accounting/
// templates/au_small_business_v1.json): 1000, 1100, 1150 and 2100 are Control.
// 3100 and 6100 were added by a person, and 4000 Invoice sales is created with
// the Ordinary designation on first issue (invoices/accounting_repository.go).
const chart = [
  { account: accounts.bank, type: "Asset", control: true },
  { account: accounts.receivable, type: "Asset", control: true },
  { account: accounts.gstReceivable, type: "Asset", control: true },
  { account: accounts.gstPayable, type: "Liability", control: true },
  { account: accounts.ownerContributions, type: "Equity", control: false },
  { account: accounts.sales, type: "Revenue", control: false },
  { account: accounts.officeExpenses, type: "Expense", control: false },
] as const;

function ChartPanel() {
  return (
    <Panel id="chart" eyebrow="Accounting · Structure" title="Chart of accounts">
      <div className="lp-hero-card">
        <table className="lp-table lp-hero-table lp-hero-chart">
          <caption className="lp-visually-hidden">Chart of accounts</caption>
          <thead>
            <tr>
              <th scope="col">Code</th>
              <th scope="col">Account name</th>
              <th scope="col" data-col="type">
                Type
              </th>
              <th scope="col">Protection</th>
            </tr>
          </thead>
          <tbody>
            {chart.map((entry, index) => (
              <tr key={entry.account.code} style={at(index)}>
                <td data-col="code">
                  <span className="lp-code">{entry.account.code}</span>
                  <Enc g={index + 4} />
                </td>
                <th scope="row">
                  {entry.account.name}
                  <Enc g={index} />
                </th>
                <td data-col="type">
                  {entry.type}
                  <Enc g={index + 2} />
                </td>
                <td data-col="protection">
                  {entry.control ? (
                    <span className="lp-hero-protected">
                      <Icon name="lock" />
                      <span className="lp-hero-protected-word">Protected · </span>Control
                    </span>
                  ) : (
                    "Ordinary"
                  )}
                  <Enc g={index + 6} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Panel>
  );
}

/* ---------- Journals ---------- */

// Newest first. Memos are the ones the core writes: "Invoice {number}",
// "Payment for invoice {number}" (invoices/accounting_repository.go) and
// "Purchase accounting" (purchases/journals.go). The amount is total debits.
const journals = [
  {
    key: "receipt",
    date: invoice.payment.dateShort,
    memo: "Payment for invoice " + invoice.number,
    cents: invoice.payment.cents,
  },
  { key: "issue", date: invoice.issueShort, memo: "Invoice " + invoice.number, cents: invoice.totalCents },
  { key: "paid", date: purchase.payment.dateShort, memo: "Purchase accounting", cents: purchase.payment.cents },
  { key: "purchase", date: purchase.dateShort, memo: "Purchase accounting", cents: purchase.totalCents },
  { key: "manual", date: manualJournal.dateShort, memo: manualJournal.description, cents: manualJournal.cents },
] as const;

function JournalsPanel() {
  return (
    <Panel id="journals" eyebrow="Accounting" title="Journals">
      <div className="lp-hero-card">
        <div className="lp-hero-card-head">
          <p className="lp-hero-card-title lp-hero-card-title--serif">Journal history</p>
          <p className="lp-hero-card-meta">
            {journals.length} of {journals.length} loaded journals
          </p>
        </div>
        <table className="lp-table lp-hero-table lp-hero-rows lp-hero-journals">
          <caption className="lp-visually-hidden">Journal history, newest first</caption>
          <thead>
            <tr>
              <th scope="col">Date</th>
              <th scope="col">Description</th>
              <th scope="col" data-num="">
                Amount · AUD
              </th>
              <th scope="col">State</th>
            </tr>
          </thead>
          <tbody>
            {journals.map((journal, index) => (
              <tr key={journal.key} style={at(index)}>
                <td data-col="date">
                  {journal.date}
                  <Enc g={index + 5} />
                </td>
                <th scope="row" data-col="memo">
                  {journal.memo}
                  <Enc g={index + 2} />
                </th>
                <td data-col="amount" data-num="">
                  <Money className="lp-hero-strong" cents={journal.cents} />
                  <Enc bar={index + 3} />
                </td>
                <td data-col="state">
                  <Pill tone="ok">Posted</Pill>
                  <Enc g={index} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Panel>
  );
}

/* ---------- Trial balance (the default screen) ---------- */

function TrialBalancePanel() {
  return (
    <Panel
      id="trial-balance"
      eyebrow="Accounting · Reports"
      title="Trial balance"
      meta={
        <p className="lp-hero-asat">
          <span>As at</span> {trialBalance.asAt}
        </p>
      }
    >
      <div className="lp-hero-card">
        <table className="lp-table lp-hero-table lp-hero-tb">
          <caption className="lp-visually-hidden">
            Trial balance as at {trialBalance.asAt}: debit and credit balances in Australian dollars
          </caption>
          <thead>
            <tr>
              <th scope="col">Account</th>
              <th scope="col" data-num="">
                Debit <span className="lp-hero-unit">(AUD)</span>
              </th>
              <th scope="col" data-num="">
                Credit <span className="lp-hero-unit">(AUD)</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {trialBalance.rows.map((line, index) => (
              <tr key={line.account.code} style={at(index)}>
                <th scope="row">
                  <span className="lp-hero-acct">
                    <span className="lp-code">{line.account.code}</span>
                    <span>{line.account.name}</span>
                  </span>
                  <Enc g={index} />
                </th>
                <td data-num="">
                  <Money className={line.debitCents === 0 ? "lp-hero-zero" : undefined} cents={line.debitCents} />
                  <Enc bar={index} />
                </td>
                <td data-num="">
                  <Money className={line.creditCents === 0 ? "lp-hero-zero" : undefined} cents={line.creditCents} />
                  <Enc bar={index + 2} />
                </td>
              </tr>
            ))}
          </tbody>
          <tfoot>
            <tr className="lp-hero-total">
              <th scope="row">
                <span className="lp-hero-total-label">
                  Total
                  <span className="lp-hero-equal">
                    <Tick />
                    Debits equal credits
                  </span>
                </span>
              </th>
              <td data-num="">
                <Money cents={trialBalance.totalCents} />
              </td>
              <td data-num="">
                <Money cents={trialBalance.totalCents} />
              </td>
            </tr>
          </tfoot>
        </table>
      </div>
    </Panel>
  );
}

/* ---------- GST & BAS ---------- */

function GstPanel() {
  const paper = gstWorkpaper.nonCash;
  return (
    <Panel id="gst" eyebrow="Reporting · Australian tax" title="GST & BAS">
      <div className="lp-hero-card">
        <div className="lp-hero-gst-status lp-hero-stored" style={at(0)}>
          <Pill tone="warn">{gstWorkpaper.status}</Pill>
          <span>Review locally when ready.</span>
          <Enc g={2} />
        </div>
        <p className="lp-hero-gst-basis">
          {business.quarter} · {business.basis} · sales follow invoice issue dates
        </p>
        <dl className="lp-hero-metrics lp-hero-gst-metrics">
          <div className="lp-hero-metric lp-hero-stored" style={at(1)}>
            <dt>Sales (G1)</dt>
            <dd>
              <Money cents={paper.g1Cents} />
              <Enc g={1} />
            </dd>
          </div>
          <div className="lp-hero-metric lp-hero-stored" style={at(2)}>
            <dt>GST on sales (1A)</dt>
            <dd>
              <Money cents={paper.a1Cents} />
              <Enc g={4} />
            </dd>
          </div>
          <div className="lp-hero-metric lp-hero-stored" style={at(3)}>
            <dt>GST credits (1B)</dt>
            <dd>
              <Money cents={paper.b1Cents} />
              <Enc g={6} />
            </dd>
          </div>
          <div className="lp-hero-metric lp-hero-stored" data-net="" style={at(4)}>
            <dt>Net GST payable</dt>
            <dd>
              <Money cents={paper.netCents} />
              <Enc g={3} />
            </dd>
          </div>
        </dl>
      </div>
      <div className="lp-hero-card">
        <div className="lp-hero-card-head">
          <p className="lp-hero-card-title">Invoice sales sources</p>
          <p className="lp-hero-card-meta">Issued invoice</p>
        </div>
        <table className="lp-table lp-hero-table lp-hero-rows lp-hero-sources">
          <caption className="lp-visually-hidden">Invoice sales behind G1 and 1A</caption>
          <thead>
            <tr>
              <th scope="col">Tax date</th>
              <th scope="col">Customer</th>
              <th scope="col">Invoice</th>
              <th scope="col" data-num="">
                Sales (G1)
              </th>
              <th scope="col" data-num="">
                GST (1A)
              </th>
            </tr>
          </thead>
          <tbody>
            <tr style={at(5)}>
              <td data-col="date">
                {invoice.issueShort}
                <Enc g={5} />
              </td>
              <th scope="row" data-col="party">
                {invoice.customer}
                <Enc g={0} />
              </th>
              <td data-col="ref">
                {invoice.number}
                <Enc g={7} />
              </td>
              <td data-col="gross" data-num="" data-label="G1">
                <Money cents={paper.g1Cents} />
                <Enc bar={1} />
              </td>
              <td data-col="gst" data-num="" data-label="1A">
                <Money className="lp-hero-strong" cents={paper.a1Cents} />
                <Enc bar={3} />
              </td>
            </tr>
          </tbody>
        </table>
        <div className="lp-hero-card-head lp-hero-card-head--ruled">
          <p className="lp-hero-card-title">Reconciled purchase sources</p>
        </div>
        <table className="lp-table lp-hero-table lp-hero-rows lp-hero-sources">
          <caption className="lp-visually-hidden">Posted purchases behind 1B</caption>
          <thead>
            <tr>
              <th scope="col">Invoice date</th>
              <th scope="col">Supplier</th>
              <th scope="col">Invoice</th>
              <th scope="col" data-num="">
                Gross
              </th>
              <th scope="col" data-num="">
                Claimed GST
              </th>
            </tr>
          </thead>
          <tbody>
            <tr style={at(6)}>
              <td data-col="date">
                {purchase.dateShort}
                <Enc g={2} />
              </td>
              <th scope="row" data-col="party">
                {purchase.supplier}
                <Enc g={6} />
              </th>
              <td data-col="ref">
                {purchase.reference}
                <Enc g={4} />
              </td>
              <td data-col="gross" data-num="" data-label="Gross">
                <Money cents={purchase.totalCents} />
                <Enc bar={2} />
              </td>
              <td data-col="gst" data-num="" data-label="1B">
                <Money className="lp-hero-strong" cents={paper.b1Cents} />
                <Enc bar={0} />
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </Panel>
  );
}

/* ---------- Audit trail ---------- */

const auditRows: readonly {
  key: string;
  date: string;
  icon: IconName;
  kind: string;
  label: string;
  detail: ReactNode;
  status: string;
  settled: boolean;
}[] = [
  {
    key: "bank",
    date: statement.rows[1].date,
    icon: "bank",
    kind: "Bank statement line",
    label: statement.rows[1].description,
    detail: <Money cents={statement.rows[1].cents} />,
    status: "Unmatched",
    settled: false,
  },
  {
    key: "receipt",
    date: invoice.payment.dateShort,
    icon: "journal",
    kind: "Journal",
    label: "Payment for invoice " + invoice.number,
    detail: (
      <>
        <Money cents={invoice.payment.cents} /> debits and credits
      </>
    ),
    status: "Posted",
    settled: true,
  },
  {
    key: "document",
    date: pendingDocument.dateShort,
    icon: "document",
    kind: "Document",
    label: pendingDocument.supplier,
    detail: pendingDocument.reference,
    status: "Needs review",
    settled: false,
  },
  {
    key: "issue",
    date: invoice.issueShort,
    icon: "journal",
    kind: "Journal",
    label: "Invoice " + invoice.number,
    detail: (
      <>
        <Money cents={invoice.totalCents} /> debits and credits
      </>
    ),
    status: "Posted",
    settled: true,
  },
];

function AuditPanel() {
  return (
    <Panel id="audit" eyebrow="Business records · Local activity" title="Audit trail">
      <p className="lp-hero-notice">
        <Icon name="eye" />
        <span>
          <strong>Activity view.</strong> A summary of loaded records, not a full history of actions.
        </span>
      </p>
      <div className="lp-hero-card">
        <div className="lp-hero-card-head">
          <p className="lp-hero-card-title">Retained records</p>
        </div>
        <ul className="lp-hero-audit">
          {auditRows.map((record, index) => (
            <li className="lp-hero-stored" key={record.key} style={at(index)}>
              <span className="lp-hero-audit-date">{record.date}</span>
              <span className="lp-hero-audit-icon">
                <Icon name={record.icon} />
              </span>
              <span className="lp-hero-audit-text">
                <strong>{record.label}</strong>
                <span className="lp-hero-sub">
                  {record.kind} · {record.detail}
                </span>
              </span>
              <span className="lp-hero-audit-status" data-settled={record.settled ? "" : undefined}>
                {record.status}
              </span>
              <Enc g={index + 3} />
            </li>
          ))}
        </ul>
      </div>
    </Panel>
  );
}

export function HeroPanels() {
  return (
    <div className="lp-hero-panels">
      <OverviewPanel />
      <DocumentsPanel />
      <InvoicesPanel />
      <BankingPanel />
      <ChartPanel />
      <JournalsPanel />
      <TrialBalancePanel />
      <GstPanel />
      <AuditPanel />
    </div>
  );
}
