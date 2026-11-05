/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { findBlogPost } from "../../../lib/blog/posts";

export const metadata: Metadata = {
  title: "Business receipts and the 5-year record-keeping rule",
  description: "Which business records Australians generally need to retain, the ATO five-year minimum and exceptions, and how to preserve readable electronic receipts and evidence.",
  alternates: { canonical: "/blog/business-receipts-five-year-record-keeping" },
  keywords: ["ATO business records five years", "how long keep receipts Australia", "electronic business records ATO", "tax invoice records small business"],
  openGraph: { type: "article", title: "Keep the evidence behind the entry | Tammy Journal", url: "/blog/business-receipts-five-year-record-keeping" },
};

export default function RecordsArticlePage() {
  return <BlogArticle post={findBlogPost("business-receipts-five-year-record-keeping")}>
    <section id="source-evidence">
      <h2>A ledger entry is an answer. The receipt is the evidence.</h2>
      <p>A transaction record might say that $330 was spent on office supplies. The supporting invoice explains the supplier, date, nature of the purchase and any GST shown. A bank entry proves that money moved, but may not explain what was acquired or whether a GST credit is available.</p>
      <p>Good books bring these pieces together: the original document, the accounting treatment, the related bank movement and any review decision. That makes the record understandable long after the person who entered it has forgotten the detail.</p>
      <div className="blog-example">
        <strong>One purchase, several useful records</strong>
        <p>A fictional $330 invoice includes $30 GST on an otherwise fully taxable purchase. Retain the supplier invoice, the corresponding bank transaction and the bookkeeping entry. Whether a GST credit is claimable depends on the actual purchase and the business circumstances, not merely the appearance of $30 on the document.</p>
      </div>
    </section>

    <section id="keep-these-records">
      <h2>What usually belongs in the archive?</h2>
      <p>Business records relevant to tax, superannuation and registrations generally include evidence of income and sales, purchases and expenses, assets, bank movements, GST, payroll and relevant business decisions. Depending on the enterprise, contracts, reconciliations, payment records and working papers may explain why a transaction was accounted for in a particular way.</p>
      <p>The <a href="https://business.gov.au/finance/payments-and-invoicing/record-keeping" target="_blank" rel="noopener noreferrer">Australian Government record-keeping guide</a> sets out common categories. The records should be in English or readily translatable and detailed enough to explain the transaction. A summary total alone is often less useful than the evidence that produced it.</p>
      <p>A practical filing pattern connects the receipt or invoice reference to its accounting entry and, where relevant, to the imported bank-statement row. It does not require inventing a complicated folder taxonomy for every single coffee receipt.</p>
    </section>

    <section id="five-years-and-beyond">
      <h2>Five years is the common starting point.</h2>
      <p>The ATO&apos;s general business record-keeping rule is that most relevant records are kept for <strong>five years</strong>. The period ordinarily begins after the record was prepared or obtained, or the transaction or act was completed, whichever is later. Different starting points and longer retention requirements can apply, including matters involving particular assessments, assets or employee obligations.</p>
      <p>Do not interpret “five years from the invoice date” as a universal deletion rule. A document linked to a longer-running tax matter may need to be retained beyond that simple date. A good records policy records the reason for retention as well as the approximate end point.</p>
      <p>See the <a href="https://www.ato.gov.au/businesses-and-organisations/preparing-lodging-and-paying/record-keeping-for-business" target="_blank" rel="noopener noreferrer">ATO record-keeping guidance</a> and <a href="https://www.ato.gov.au/law/view/document?DocID=TXR/TR967/NAT/ATO/00001" target="_blank" rel="noopener noreferrer">Taxation Ruling TR 96/7</a> for the underlying principles.</p>
    </section>

    <section id="protect-and-recover">
      <h2>Digital records still need a recovery plan.</h2>
      <p>Scanned receipts, exported invoices and electronic transaction records can be practical evidence if they remain legible, accessible and protected from alteration. Changing accounting software should not leave the business unable to reconstruct or explain its earlier records.</p>
      <p>For a local-first workflow, encryption protects data on the device, but it does not replace a backup strategy. Store recoverable backups separately, test that they open, and retain access to exported records when devices or applications are retired. Keep sensitive business documents in access-controlled storage rather than sending them to arbitrary online converters.</p>
      <p>Tammy stores its development-stage accounting workspace locally and supports explicit document workflows. It does not claim automatic cloud backup or server-based document recovery. The related <a href="/blog/bank-statement-reconciliation-steps">bank reconciliation guide</a> shows how retained statements fit into that evidence trail, while the <a href="/guides/australian-tax-invoice-checklist">tax invoice checklist</a> covers document content.</p>
      <div className="blog-source-list">
        <p>Sources: <a href="https://business.gov.au/finance/payments-and-invoicing/record-keeping" target="_blank" rel="noopener noreferrer">business.gov.au, Record keeping</a> and <a href="https://www.ato.gov.au/businesses-and-organisations/preparing-lodging-and-paying/record-keeping-for-business" target="_blank" rel="noopener noreferrer">ATO, Record keeping for business</a>. This article describes general rules rather than every industry-specific retention period.</p>
      </div>
    </section>
  </BlogArticle>;
}
