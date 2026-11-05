/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { findBlogPost } from "../../../lib/blog/posts";

export const metadata: Metadata = {
  title: "How to reconcile a bank statement with your ledger",
  description: "A practical small-business bank reconciliation with a worked opening-balance example, unmatched transactions, a $40 fee difference and retained accounting evidence.",
  alternates: { canonical: "/blog/bank-statement-reconciliation-steps" },
  keywords: ["how to reconcile bank statements", "bank reconciliation example Australia", "bank statement ledger difference", "manual bookkeeping reconciliation"],
  openGraph: { type: "article", title: "Make the bank and ledger agree | Tammy Journal", url: "/blog/bank-statement-reconciliation-steps" },
};

export default function BankReconciliationArticlePage() {
  return <BlogArticle post={findBlogPost("bank-statement-reconciliation-steps")}>
    <section id="same-period">
      <h2>Start with the same account and the same dates.</h2>
      <p>Bank reconciliation is a check that your accounting cash records can be explained against the bank statement for the relevant account. It is not just a button that changes a transaction&apos;s status from pending to green.</p>
      <p>Choose the correct bank account, statement period, opening balance and closing balance. Make sure the accounting ledger covers the same period and account. If the opening figures do not agree, first understand whether a previous period has an unresolved difference or a legitimate outstanding item.</p>
      <p>As <a href="https://business.gov.au/finance/financial-tools-and-templates/key-financial-terms" target="_blank" rel="noopener noreferrer">business.gov.au&apos;s financial glossary</a> explains, reconciliation cross-checks the cash book against the relevant bank statements.</p>
    </section>

    <section id="trace-the-rows">
      <h2>Match the movements, not just the totals.</h2>
      <p>Work through each statement transaction, identifying the related entry in the books. A clear match normally needs more than an identical amount: check the counterparty, date, reference, bank account and the transaction&apos;s nature. Record the reason for any manual judgement.</p>
      <p>Deposits, supplier payments, internal transfers and refunds may look similar in a statement export. An internal transfer should not be misclassified as new sales revenue simply because cash arrived in the account.</p>
      <div className="blog-table-scroll">
        <table className="blog-data-table">
          <caption>Fictional bank movement example, AUD. Statement and ledger figures are deliberately compared.</caption>
          <thead><tr><th scope="col">Movement</th><th scope="col">Bank statement</th><th scope="col">Book balance</th></tr></thead>
          <tbody>
            <tr><th scope="row">Opening balance</th><td>$8,000.00</td><td>$8,000.00</td></tr>
            <tr><th scope="row">Deposits</th><td>+$4,400.00</td><td>+$4,400.00</td></tr>
            <tr><th scope="row">Payments and expenses</th><td>−$1,750.00</td><td>−$1,710.00</td></tr>
            <tr><th scope="row">Closing balance</th><td>$10,650.00</td><td>$10,690.00</td></tr>
          </tbody>
        </table>
      </div>
      <p>The bank closing balance is $10,650 but the ledger shows $10,690, a $40 difference. The book has not recorded one charge that appears in the statement. That is a hypothesis to verify, not permission to manufacture a balancing transaction.</p>
    </section>

    <section id="resolve-differences">
      <h2>Investigate the difference before adjusting the books.</h2>
      <p>Suppose the statement includes a genuine $40 bank fee that is absent from the ledger. Confirm the bank description and transaction date, then record the fee with the correct accounting treatment. The ledger cash balance would reduce from $10,690 to $10,650 and agree with this simplified statement.</p>
      <div className="blog-example">
        <strong>A resolved $40 difference</strong>
        <p>Ledger before fee: $10,690. Verified unrecorded fee: $40. Ledger after recording the fee: $10,650. Bank statement closing balance: $10,650. The records now agree for this illustration.</p>
      </div>
      <p>Other differences may arise from uncleared cheques, deposits in transit, duplicated imports, wrong periods, bank errors, fees, missing transfers or entries recorded against another account. Some can be explained by timing without changing the ledger immediately. Always identify which situation actually occurred.</p>
    </section>

    <section id="save-the-trail">
      <h2>Keep the reconciliation explainable next month.</h2>
      <p>Save the source statement, the related entries, the list of unresolved differences and the final reconciliation result. Where a journal adjustment was needed, retain why it was made and which source document supports it.</p>
      <p>Tammy supports importing bank-statement rows from files and manually reviewing matches. It does not currently provide automatic bank feeds, unattended bank syncing or a promise of automated ledger posting. A deliberate review is part of the workflow.</p>
      <p>For related tasks, read the <a href="/blog/business-receipts-five-year-record-keeping">business records guide</a> and the <a href="/blog/the-12-week-cash-flow-view">weekly cash-flow article</a>. Reconciliation tells you what actually happened; forecasting estimates what may happen next.</p>
      <div className="blog-source-list">
        <p>Sources: <a href="https://business.gov.au/finance/financial-tools-and-templates/key-financial-terms" target="_blank" rel="noopener noreferrer">business.gov.au, Key financial terms</a>; <a href="https://business.gov.au/finance/payments-and-invoicing/record-keeping" target="_blank" rel="noopener noreferrer">business.gov.au, Record keeping</a>. All amounts are illustrative and are not a Tammy customer&apos;s data.</p>
      </div>
    </section>
  </BlogArticle>;
}
