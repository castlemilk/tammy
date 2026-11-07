/* oxlint-disable next/no-html-link-for-pages -- Vinext handles these static routes. */
import type { Metadata } from "next";
import { ToolsShell } from "../../../components/tools-shell";
import { GuideStructuredData } from "../../../components/guide-structured-data";

export const metadata: Metadata = {
  title: "GST inclusive vs exclusive: how to calculate 10% GST",
  description: "Learn how to add GST to an Australian price, extract GST from an inclusive total using 1/11, handle rounding and understand which sales are not taxable.",
  alternates: { canonical: "/guides/gst-inclusive-vs-exclusive" },
  keywords: ["GST inclusive vs exclusive", "GST inclusive formula Australia", "GST exclusive formula", "remove GST from price", "1/11 GST"],
  openGraph: { type: "article", title: "GST inclusive vs exclusive | Tammy", url: "/guides/gst-inclusive-vs-exclusive" },
};

export default function GstGuidePage() {
  return <ToolsShell section="guides">
    <nav className="tools-breadcrumb" aria-label="Breadcrumb"><a href="/">Home</a><span aria-hidden="true">/</span><a href="/guides">Guides</a><span aria-hidden="true">/</span><span aria-current="page">GST inclusive vs exclusive</span></nav>
    <article className="tools-editorial">
      <header className="tools-editorial-heading">
        <p className="tools-overline">Australian GST explained</p>
        <h1>Inclusive, exclusive. One clear difference.</h1>
        <p className="tools-editorial-lede">A price that includes GST is not the same as one before GST. This is how the numbers work for an ordinary fully taxable sale in Australia.</p>
        <div className="tools-editorial-meta"><span>Standard GST: 10%</span><span>Cent rounding examples</span></div>
      </header>
      <div className="tools-editorial-grid">
        <nav className="tools-article-toc" aria-label="On this page">
          <strong>On this page</strong>
          <a href="#gst-add">Add GST</a>
          <a href="#gst-extract">Extract GST</a>
          <a href="#gst-why">Why not subtract 10%?</a>
          <a href="#gst-rounding">Rounding</a>
          <a href="#gst-exceptions">Not every sale has GST</a>
        </nav>
        <div className="tools-article-body">
          <section id="gst-add">
            <h2>Starting with a price excluding GST</h2>
            <p>For a standard taxable supply, multiply the GST-exclusive price by <strong>10%</strong> to calculate GST. Add that GST amount to the original price.</p>
            <div className="tools-formula"><span>GST-exclusive price</span><strong>$100.00</strong><span>+ GST (10%)</span><strong>$10.00</strong><span>= Total including GST</span><strong>$110.00</strong></div>
            <p>Formula: <code>including GST = excluding GST × 1.10</code>. Use money in cents when implementing this in software and round the GST component appropriately.</p>
          </section>
          <section id="gst-extract">
            <h2>Starting with a price including GST</h2>
            <p>If a $110.00 price already contains 10% GST, the GST component is <strong>one-eleventh</strong> of that total, or $10.00. The ex-GST amount is $100.00.</p>
            <div className="tools-formula"><span>GST-inclusive price</span><strong>$110.00</strong><span>GST (1/11 of total)</span><strong>$10.00</strong><span>Price excluding GST</span><strong>$100.00</strong></div>
            <p>Formula: <code>GST = inclusive total ÷ 11</code>. The ex-GST amount is the total minus the GST component.</p>
          </section>
          <section id="gst-why">
            <h2>Why not just subtract 10%?</h2>
            <p>Subtracting 10% of $110 gives $99, which is not the pre-GST price. The 10% rate applies to the original <em>exclusive</em> amount, whereas GST represents 1/11 of the <em>inclusive</em> amount.</p>
            <p>The distinction matters when calculating quotes, reconciling receipts and comparing margins.</p>
          </section>
          <section id="gst-rounding">
            <h2>Rounding to cents</h2>
            <p>GST calculations can produce fractions of a cent, especially across multiple invoice lines. The ATO permits rounding approaches in the tax-invoice context. Tammy’s calculator displays amounts rounded to the nearest cent, with half-cent results rounded up.</p>
            <p>Where many lines are involved, the sum of line-rounded GST can differ from calculating GST on an aggregated amount. Keep your rounding method consistent with the accounting records you actually use.</p>
          </section>
          <section id="gst-exceptions">
            <h2>Not every Australian sale is taxable at 10%</h2>
            <p>Some supplies are GST-free or input-taxed, while others are outside the scope of GST. A business that is not registered for GST does not generally charge GST on its sales. Always establish the correct tax treatment rather than automatically applying 10% to every item.</p>
            <p>The <a href="/tools/gst-calculator">Tammy GST calculator</a> is for fully taxable supplies at the standard rate only. For mixed supplies, use separate line treatments or a more complete accounting workpaper.</p>
          </section>
          <section className="tools-article-sources" aria-label="Primary sources">
            <h2>Primary sources</h2>
            <p><a href="https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/gst" target="_blank" rel="noopener noreferrer">ATO GST guidance</a> covers taxable, GST-free and input-taxed supplies. The ATO’s <a href="https://www.ato.gov.au/api/public/content/0-1e92db95-a75c-4f4e-a3d4-39f43b1a3b25" target="_blank" rel="noopener noreferrer">tax-invoice GST rounding guidance</a> explains permitted rounding methods.</p>
          </section>
        </div>
      </div>
    </article>
    <section className="tools-article-action">
      <div><p className="tools-overline">Calculate with real numbers</p><h2>See the GST breakdown.</h2><p>Enter an inclusive or exclusive AUD price and get the GST component immediately.</p></div>
      <a className="tools-primary-link" href="/tools/gst-calculator">Open GST calculator <span aria-hidden="true">↗</span></a>
    </section>
    <GuideStructuredData name="GST inclusive vs exclusive: how to calculate Australian GST" description="Learn the Australian standard 10% GST add/extract formulas, one-eleventh calculation, rounding and GST treatment boundaries." path="/guides/gst-inclusive-vs-exclusive" />
  </ToolsShell>;
}
