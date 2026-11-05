/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { findBlogPost } from "../../../lib/blog/posts";

export const metadata: Metadata = {
  title: "When to register for GST in Australia | $75,000 threshold",
  description: "Understand the $75,000 GST registration threshold for Australian businesses, current and projected 12-month turnover, the 21-day registration rule and exceptions.",
  alternates: { canonical: "/blog/when-to-register-for-gst-australia" },
  keywords: ["when to register for GST Australia", "GST threshold 75000", "GST registration sole trader", "projected GST turnover", "21 days GST registration"],
  openGraph: { type: "article", title: "When GST registration becomes necessary | Tammy Journal", url: "/blog/when-to-register-for-gst-australia" },
};

export default function GstRegistrationArticlePage() {
  return <BlogArticle post={findBlogPost("when-to-register-for-gst-australia")}>
    <section id="turnover-not-profit">
      <h2>The threshold follows turnover, not profit.</h2>
      <p>For most Australian businesses, the standard GST registration threshold is <strong>$75,000 in annual GST turnover</strong>. This is not a test of profit or how much money remains after paying expenses. A service business with $82,000 of relevant turnover and $60,000 of costs cannot simply treat its $22,000 profit as the number for the threshold.</p>
      <p>GST turnover is a defined measure. It generally starts with business income and excludes amounts such as GST included in sales, input-taxed sales and certain supplies not connected with Australia. Specific exclusions also apply to projected turnover. The label on your bank account or a year-to-date revenue chart is not automatically the official turnover calculation.</p>
      <p>The <a href="https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/gst/registering-for-gst" target="_blank" rel="noopener noreferrer">ATO guidance on registering for GST</a> explains both the threshold and what counts toward it.</p>
    </section>

    <section id="two-windows">
      <h2>Look backwards and forwards.</h2>
      <p>The ATO considers two rolling measures. <strong>Current GST turnover</strong> covers the present month and the preceding eleven months. <strong>Projected GST turnover</strong> considers the present month and the following eleven months. These are rolling twelve-month windows, not simply the Australian financial year ending on 30 June.</p>
      <div className="blog-example">
        <strong>A growing sole-trader example</strong>
        <p>Imagine eligible turnover over the current rolling year is $68,000, but signed work and realistic demand now suggest $92,000 over the next rolling year. The business should assess its projected GST turnover now, rather than waiting for completed invoices to reach $75,000.</p>
      </div>
      <p>The reverse can matter too. The ATO explains that a business whose current turnover has reached the threshold may not need to register where its projected turnover is below the threshold and the relevant conditions apply. This is why checking both windows is more useful than a single progress bar.</p>
    </section>

    <section id="deadline-and-exceptions">
      <h2>There is a deadline, and there are exceptions.</h2>
      <p>Once a business is <strong>required</strong> to register, the general deadline is <strong>21 days</strong>. A business expecting to reach the threshold in its first year should assess registration at the outset. Waiting can lead to GST liabilities on sales made after the date registration should have taken effect.</p>
      <p>The threshold is different for some activities. Non-profit organisations generally have a $150,000 threshold. Taxi, limousine and ride-sourcing passenger transport can trigger compulsory registration regardless of turnover; fuel tax credit claims can also require registration. Businesses below the relevant threshold can choose voluntary registration, subject to its obligations.</p>
      <p>An ABN is required before registering for GST, but <strong>holding an ABN alone does not mean the business is GST-registered</strong>. Use <a href="https://abr.business.gov.au/" target="_blank" rel="noopener noreferrer">official ABN Lookup</a> for registration status. Tammy&apos;s <a href="/tools/abn-checker">ABN checker</a> validates number format only.</p>
    </section>

    <section id="after-registering">
      <h2>Registration changes the everyday paperwork.</h2>
      <p>Once registered, a business generally accounts for GST on its taxable sales, may claim credits for eligible purchases and has BAS reporting obligations. The tax treatment of individual supplies still matters: GST-free and input-taxed transactions are not interchangeable with fully taxable sales.</p>
      <p>A useful starting routine is to keep a current turnover forecast, check supplier and customer tax details, make correctly labelled tax invoices, separate GST amounts from sales values and retain the underlying records. Use Tammy&apos;s <a href="/tools/gst-calculator">GST calculator</a> to inspect a standard 10% taxable price and the <a href="/guides/australian-tax-invoice-checklist">tax invoice checklist</a> before issuing documents.</p>
      <p>For reporting, the related <a href="/blog/simpler-bas-g1-1a-1b-explained">Simpler BAS workpaper guide</a> explains G1, 1A and 1B. Tammy&apos;s desktop application prepares local drafts; it does not lodge a BAS with the ATO.</p>
      <div className="blog-source-list">
        <p>Primary source: <a href="https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/gst/registering-for-gst" target="_blank" rel="noopener noreferrer">Australian Taxation Office, Registering for GST</a>. Thresholds and examples in this article were checked on 5 November 2026. Illustrative business figures are not a real taxpayer record.</p>
      </div>
    </section>
  </BlogArticle>;
}
