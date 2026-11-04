/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { findBlogPost } from "../../../lib/blog/posts";

export const metadata: Metadata = {
  title: "Margin vs markup: the pricing difference that matters",
  description: "A worked Australian small-business example showing why 50% markup is only 33.33% margin, and how contribution affects the break-even point.",
  alternates: { canonical: "/blog/margin-is-not-markup" },
  keywords: ["margin vs markup", "margin markup difference", "pricing calculation Australia", "gross profit margin"],
  openGraph: { type: "article", title: "Margin is not markup | Tammy Journal", url: "/blog/margin-is-not-markup" },
};

export default function MarginArticlePage() {
  return <BlogArticle post={findBlogPost("margin-is-not-markup")}>
    <section id="two-denominators">
      <h2>Two denominators, one profit</h2>
      <p>A product can earn exactly the same $40 of gross profit while being described as having 50% markup or 33.33% margin. Neither number is wrong. They answer different questions, and treating them as interchangeable makes pricing targets misleading.</p>
      <p><strong>Markup</strong> asks how much profit you add relative to the cost. <strong>Margin</strong> asks how much of the sale is left after that direct cost. The first denominator is the cost; the second is the selling price.</p>
      <blockquote>Markup starts with what you paid. Margin starts with what you sold.</blockquote>
      <p>For GST-registered businesses, compare prices on a consistent ex-GST basis. A customer-facing price including GST may be useful for quoting, but GST collected is not the same thing as gross profit.</p>
    </section>
    <section id="working-example">
      <h2>The $80 and $120 example</h2>
      <p>Imagine a single service or product whose direct cost is $80 and whose selling price is $120, both excluding GST. There is $40 left before fixed overheads, financing, tax and other costs.</p>
      <table className="blog-data-table">
        <caption>Illustrative per-unit pricing, excluding GST</caption>
        <thead><tr><th scope="col">Measure</th><th scope="col">Calculation</th><th scope="col">Result</th></tr></thead>
        <tbody>
          <tr><th scope="row">Gross profit</th><td>$120 − $80</td><td>$40.00</td></tr>
          <tr><th scope="row">Markup</th><td>$40 ÷ $80</td><td>50.00%</td></tr>
          <tr><th scope="row">Margin</th><td>$40 ÷ $120</td><td>33.33%</td></tr>
        </tbody>
      </table>
      <p>Suppose someone expects a 50% margin and prices the item by adding 50% to the $80 cost. They arrive at $120, but they have actually applied a 50% markup. To achieve a genuine 50% margin, the selling price in this simplified example would need to be $160.</p>
    </section>
    <section id="price-decisions">
      <h2>Where pricing goes wrong</h2>
      <p>Gross margin is not net profit. Rent, administration, subscriptions, delivery, payment fees and other overheads still have to be covered. That is why the next useful number is often <strong>contribution per unit</strong>, the selling price minus the variable cost of delivering one more unit.</p>
      <div className="blog-example"><strong>What if fixed costs are $3,000?</strong><p>At $40 of contribution per unit, $3,000 ÷ $40 = 75 units. You would need to sell 75 whole units in that period to cover those fixed costs, assuming the price and variable cost remain constant.</p></div>
      <p>More sales are not automatically better if the variable cost is as high as the selling price. If contribution is zero or negative, adding volume cannot cover positive fixed costs. Mixes of different products, seasonal demand and stock constraints also make real break-even analysis more involved.</p>
    </section>
    <section id="use-the-numbers">
      <h2>Use the numbers before the price tag</h2>
      <p>Start with direct costs, check whether the number you want is markup or margin, then model how many units or hours you can realistically sell. Keep the pricing assumptions and actual outcomes side by side so you can correct them as costs change.</p>
      <p>Use Tammy’s free <a href="/tools/profit-margin-calculator">margin calculator</a> to compare both percentages. For the next step, open the <a href="/tools/break-even-calculator">break-even calculator</a> to turn contribution into a target number of units.</p>
      <div className="blog-source-list"><p>Source: <a href="https://business.vic.gov.au/business-information/finance/pricing-for-profit/calculate-your-breakeven-point-margin-and-markup" target="_blank" rel="noopener noreferrer">Business Victoria, Calculate your break-even point, margin and markup</a>. Examples in this article are illustrative, not a claim about Tammy customer results.</p></div>
    </section>
  </BlogArticle>;
}
