/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { findBlogPost } from "../../../lib/blog/posts";

export const metadata: Metadata = {
  title: "A practical 12-week cash-flow forecast for small business",
  description: "Build a rolling 12-week cash-flow view with opening balance, expected receipts, payments and closing balances. Includes an Australian small-business example.",
  alternates: { canonical: "/blog/the-12-week-cash-flow-view" },
  keywords: ["12 week cash flow forecast", "small business cash flow Australia", "weekly cash flow forecast template", "cash runway vs cash flow"],
  openGraph: { type: "article", title: "See the next twelve weeks | Tammy Journal", url: "/blog/the-12-week-cash-flow-view" },
};

export default function CashFlowArticlePage() {
  return <BlogArticle post={findBlogPost("the-12-week-cash-flow-view")}>
    <section id="start-with-balance">
      <h2>Start with money already available</h2>
      <p>A business can be profitable on paper and still struggle to pay a bill on Tuesday. A weekly cash-flow view focuses on actual expected movements through the bank, not merely invoices issued or revenue recorded.</p>
      <p>Start the forecast with the cleared cash balance you can use. Keep money that is restricted or otherwise unavailable out of the opening figure. Then list the next twelve weeks across the top of a worksheet and carry each closing balance into the following week.</p>
      <p>The <a href="https://business.gov.au/finance/cash-flow/set-up-a-cash-flow-statement" target="_blank" rel="noopener noreferrer">Australian Government cash-flow statement guide</a> uses this same opening-balance, receipts, payments and closing-balance structure.</p>
    </section>
    <section id="when-not-just-how-much">
      <h2>Put each payment in the week it may land</h2>
      <p>A monthly average smooths over timing. That can hide a week where payroll, rent and a tax payment fall before an important customer invoice is likely to be paid. Forecast from expected settlement dates, not just the date of the sale.</p>
      <ul>
        <li>Cash in: likely customer receipts, deposits, grants or other amounts you can reasonably expect.</li>
        <li>Cash out: supplier payments, wages, rent, tax, loan payments and planned purchases.</li>
        <li>Uncertainty: distinguish confirmed obligations from estimates, and be cautious with invoices that are already overdue.</li>
      </ul>
      <p>Make your assumptions visible. A large invoice due in week two but historically paid two weeks late should not automatically be forecast as cash in week two.</p>
    </section>
    <section id="a-simple-weekly-table">
      <h2>A simple weekly worksheet</h2>
      <p>Here is a four-week extract from a fictional twelve-week forecast. It starts with $10,000 and records expected inflows and outflows by week. The closing balance becomes the following week’s opening balance.</p>
      <section className="blog-table-scroll" aria-label="Illustrative weekly cash-flow worksheet">
        <table className="blog-data-table">
          <caption>Worked example, AUD. Not real customer data.</caption>
          <thead><tr><th scope="col">Week</th><th scope="col">Opening</th><th scope="col">Receipts</th><th scope="col">Payments</th><th scope="col">Closing</th></tr></thead>
          <tbody>
            <tr><th scope="row">1</th><td>$10,000</td><td>$3,000</td><td>$7,000</td><td>$6,000</td></tr>
            <tr><th scope="row">2</th><td>$6,000</td><td>$5,000</td><td>$4,000</td><td>$7,000</td></tr>
            <tr><th scope="row">3</th><td>$7,000</td><td>$1,000</td><td>$6,000</td><td>$2,000</td></tr>
            <tr><th scope="row">4</th><td>$2,000</td><td>$7,000</td><td>$3,000</td><td>$6,000</td></tr>
          </tbody>
        </table>
      </section>
      <p>The business ends week four with $6,000, but week three bottoms out at $2,000. A monthly sum alone can obscure that tighter week. Extend the columns through week twelve and review any projected negative closing balance before the payments fall due.</p>
      <div className="blog-example"><strong>Simple rolling formula</strong><p>Closing cash = opening cash + receipts − payments. The following week’s opening cash equals the previous week’s closing cash.</p></div>
    </section>
    <section id="check-the-forecast">
      <h2>Make it a routine, not a one-off spreadsheet</h2>
      <p>Each week, replace estimates with actual bank movements, investigate differences and add one more week to the end. The forecast stays a twelve-week view instead of becoming a stale document from last quarter.</p>
      <p>Review upcoming BAS payments, seasonal variations and unusual bills. A single fixed monthly burn rate can be helpful as an overview, but it does not account for these fluctuations. Tammy’s <a href="/tools/cash-runway-calculator">cash-runway calculator</a> provides that simpler estimate as a starting point, while the <a href="/tools/invoice-due-date-calculator">due-date calculator</a> helps with basic calendar-day invoice terms.</p>
      <div className="blog-source-list"><p>Sources: <a href="https://business.gov.au/finance/cash-flow/set-up-a-cash-flow-statement" target="_blank" rel="noopener noreferrer">business.gov.au, Set up a cash flow statement</a> and <a href="https://business.vic.gov.au/business-information/finance/cash-flow/cash-flow-forecasting" target="_blank" rel="noopener noreferrer">Business Victoria, Cash flow forecasting</a>. Examples are illustrative, with dates and amounts chosen solely to explain the worksheet.</p></div>
    </section>
  </BlogArticle>;
}
