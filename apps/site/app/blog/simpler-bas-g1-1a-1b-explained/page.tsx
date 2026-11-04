/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { findBlogPost } from "../../../lib/blog/posts";

export const metadata: Metadata = {
  title: "Simpler BAS explained: G1, 1A and 1B with examples",
  description: "Understand the three Simpler BAS GST labels in Australia: G1 total sales, 1A GST on sales, 1B eligible GST on purchases, with a reviewed worked example.",
  alternates: { canonical: "/blog/simpler-bas-g1-1a-1b-explained" },
  keywords: ["simpler BAS G1 1A 1B", "BAS GST on sales and purchases", "complete BAS for GST example", "Australian GST workpaper"],
  openGraph: { type: "article", title: "Three BAS labels, one clear workpaper | Tammy Journal", url: "/blog/simpler-bas-g1-1a-1b-explained" },
};

export default function SimplerBasArticlePage() {
  return <BlogArticle post={findBlogPost("simpler-bas-g1-1a-1b-explained")}>
    <section id="the-three-labels">
      <h2>Three labels, but not three guesses.</h2>
      <p>Under the ATO&apos;s <strong>Simpler BAS</strong> reporting method, the main GST fields for an eligible small business are G1, 1A and 1B. This method is generally the default for businesses with GST turnover below $10 million, although circumstances and reporting arrangements matter.</p>
      <ul>
        <li><strong>G1: Total sales.</strong> The total amount of reportable sales for the period, ordinarily including GST where applicable.</li>
        <li><strong>1A: GST on sales.</strong> The GST payable on taxable sales, taking relevant adjustments into account.</li>
        <li><strong>1B: GST on purchases.</strong> The GST credits the business is entitled to claim on eligible purchases, including applicable adjustments.</li>
      </ul>
      <p>G1 is not simply the same as 1A multiplied by eleven when sales include GST-free, input-taxed or other differently treated transactions. The nature and timing of supplies matter. The <a href="https://softwaredevelopers.ato.gov.au/SimplerBAS" target="_blank" rel="noopener noreferrer">ATO&apos;s Simpler BAS information</a> explains the core labels.</p>
    </section>

    <section id="worked-period">
      <h2>Walk through a simple reporting period.</h2>
      <p>Consider a fictional business registered for GST, with exclusively standard-taxable sales and fully creditable purchases for one reporting period. Its sales total $22,000 including $2,000 GST. Eligible purchases total $7,150 including $650 GST.</p>
      <div className="blog-table-scroll">
        <table className="blog-data-table">
          <caption>Illustrative Simpler BAS figures, AUD. Assumes all supplies and eligible acquisitions are fully taxable at 10%.</caption>
          <thead><tr><th scope="col">Label</th><th scope="col">Meaning</th><th scope="col">Illustrative amount</th></tr></thead>
          <tbody>
            <tr><th scope="row">G1</th><td>Total sales, including GST</td><td>$22,000</td></tr>
            <tr><th scope="row">1A</th><td>GST on sales</td><td>$2,000</td></tr>
            <tr><th scope="row">1B</th><td>Eligible GST credits on purchases</td><td>$650</td></tr>
            <tr><th scope="row">Difference</th><td>1A less 1B</td><td>$1,350</td></tr>
          </tbody>
        </table>
      </div>
      <p>The $1,350 difference is the GST position <em>before</em> other activity-statement obligations, credits or adjustments. It is not necessarily the final amount payable for the BAS as a whole. PAYG withholding, other tax fields and the business&apos;s accounting basis can change the overall outcome.</p>
      <p>The point of the worksheet is to trace the figures back to classified sales, purchases and their documents, rather than type three plausible-looking amounts into the form.</p>
    </section>

    <section id="claim-only-eligible">
      <h2>Not every purchase creates a GST credit.</h2>
      <p>For 1B, the relevant question is whether the business is entitled to an input tax credit. The answer depends on factors such as whether the purchase is for a creditable purpose, the supply&apos;s tax treatment, registration, documentary requirements and the accounting basis.</p>
      <p>A supplier invoice showing GST is useful evidence, but it does not automatically prove a credit is claimable. Equally, a bank transaction without an invoice may not give you enough information to classify the GST correctly.</p>
      <div className="blog-example">
        <strong>Review before summing</strong>
        <p>Gather the period&apos;s documents, reconcile imported bank transactions, inspect the GST treatment of each relevant line and separate amounts that still need investigation. Only then calculate the 1A and 1B totals from reviewed evidence.</p>
      </div>
    </section>

    <section id="draft-before-lodgement">
      <h2>A prepared workpaper is not a lodged BAS.</h2>
      <p>Before finalising a reporting period, check the dates, the chosen accounting method, any adjustments, tax invoices, classification exceptions and differences between ledger and bank balances. Retain the calculation and source documents with the reporting-period record.</p>
      <p>Tammy&apos;s current desktop application supports local GST/BAS workpaper drafts for review. It <strong>does not submit</strong> a BAS through the ATO&apos;s production lodgement systems. The article explains accounting concepts and is not a claim that Tammy has ATO approval.</p>
      <p>Use the <a href="/tools/gst-calculator">GST calculator</a> for a basic fully taxable sale, the <a href="/guides/gst-inclusive-vs-exclusive">GST inclusive/exclusive guide</a> for the one-eleventh formula, and the <a href="/blog/business-receipts-five-year-record-keeping">record-keeping article</a> for retaining the workpaper trail.</p>
      <div className="blog-source-list">
        <p>Primary sources: <a href="https://softwaredevelopers.ato.gov.au/SimplerBAS" target="_blank" rel="noopener noreferrer">ATO, Simpler BAS for small business</a>; <a href="https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/gst/lodging-your-bas-or-annual-gst-return" target="_blank" rel="noopener noreferrer">ATO, lodging your BAS or annual GST return</a>. The example excludes mixed tax treatments and is not a complete BAS computation.</p>
      </div>
    </section>
  </BlogArticle>;
}
