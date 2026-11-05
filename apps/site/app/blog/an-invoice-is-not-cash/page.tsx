/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { findBlogPost } from "../../../lib/blog/posts";

export const metadata: Metadata = {
  title: "Invoice payment terms: why an invoice is not cash",
  description: "Understand the difference between issuing an invoice, its payment due date and receiving cash. Practical Australian small-business payment-term examples.",
  alternates: { canonical: "/blog/an-invoice-is-not-cash" },
  keywords: ["invoice payment terms Australia", "30 day invoice terms", "invoice due date", "receivables cash flow"],
  openGraph: { type: "article", title: "An invoice is not cash | Tammy Journal", url: "/blog/an-invoice-is-not-cash" },
};

export default function InvoiceCashArticlePage() {
  return <BlogArticle post={findBlogPost("an-invoice-is-not-cash")}>
    <section id="three-dates">
      <h2>The three dates worth separating</h2>
      <p>An invoice records a request for payment. It helps explain what was supplied and how much the customer owes, but it is not a bank transaction. Three dates often sit behind one invoice, and each describes something different.</p>
      <ul>
        <li><strong>Issue date:</strong> the date the invoice is produced and sent.</li>
        <li><strong>Due date:</strong> the date payment is expected under the agreed terms.</li>
        <li><strong>Payment date:</strong> when the money actually arrives in your account.</li>
      </ul>
      <p>Imagine issuing an invoice on 1 November with 30 calendar-day terms. Counting from the next day, the due date is 1 December. If the customer pays on 9 December, that is the cash receipt date. A cash forecast that assumes the money arrived on the invoice date would be wrong by more than a month.</p>
    </section>
    <section id="payment-terms">
      <h2>Make the terms visible before work begins</h2>
      <p>A date calculation cannot settle a disputed contract. Payment terms belong in the agreement with your customer and should be easy to find on the invoice. Say whether the period counts from the issue date, receipt of the invoice, completion of work or the end of the month.</p>
      <p>Businesses commonly use 7, 14 or 30 days, but no single set of terms applies to every invoice. Calendar days also include weekends and public holidays; a contract that refers to business days needs a different calculation.</p>
      <div className="blog-example"><strong>What “Net 30” should make clear</strong><p>For a calendar-day agreement, write the exact due date on the document. An invoice issued on 1 November with payment due in 30 calendar days has a due date of 1 December. The customer can read the date without interpreting shorthand.</p></div>
      <p>The <a href="https://business.gov.au/finance/payments-and-invoicing/payment-terms" target="_blank" rel="noopener noreferrer">Australian Government payment terms guide</a> explains why agreed terms, payment methods and collection processes matter to cash flow.</p>
    </section>
    <section id="keep-the-evidence">
      <h2>Keep the evidence of what really happened</h2>
      <p>Once a bill is sent, preserve the final invoice and the customer’s relevant correspondence. When cash arrives, reconcile the receipt to the invoice using the actual amount and bank date. A payment reference can help, but it does not replace matching the real transaction.</p>
      <p>That distinction is especially important when several invoices share the same customer or when only part of an invoice has been paid. A due date is a useful reminder; it is not evidence of settlement.</p>
      <p>For GST-registered businesses, invoice records also need the appropriate tax information. The <a href="/guides/australian-tax-invoice-checklist">Australian tax invoice checklist</a> describes the core details, including supplier ABN and GST. Payment is a separate event from preparing that document.</p>
    </section>
    <section id="next-invoice">
      <h2>A short process for the next invoice</h2>
      <p>Agree the terms, make a clear invoice, include the exact due date and your payment instructions, send it promptly, then review unpaid amounts at regular intervals. Use a cash-flow forecast based on likely receipt dates instead of treating every open invoice as money available to spend.</p>
      <p>Try the <a href="/tools/invoice-due-date-calculator">invoice due-date calculator</a> for calendar-day terms, or create a local PDF with Tammy’s <a href="/tools/invoice-generator">invoice generator</a>. The tools do not send invoices, collect payments or modify your desktop accounting records.</p>
      <div className="blog-source-list"><p>Further reading: <a href="https://business.gov.au/finance/payments-and-invoicing/how-to-invoice" target="_blank" rel="noopener noreferrer">business.gov.au, How to invoice</a> and <a href="https://business.gov.au/finance/payments-and-invoicing/payment-terms" target="_blank" rel="noopener noreferrer">Payment terms</a>. Dates in this article are illustrative calendar-day examples.</p></div>
    </section>
  </BlogArticle>;
}
