/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BreakEvenCalculator } from "../../../components/break-even-calculator";
import { ToolsShell, ToolsBreadcrumb, ToolStructuredData } from "../../../components/tools-shell";

export const metadata: Metadata = {
  title: "Break-even calculator Australia | Units and sales",
  description: "Free business break-even calculator. Enter fixed costs, selling price and variable cost to estimate the units and revenue needed to cover your costs.",
  alternates: { canonical: "/tools/break-even-calculator" },
  keywords: ["break even calculator Australia", "break even units formula", "fixed costs variable cost calculator", "contribution margin"],
  openGraph: { title: "Break-even calculator | Tammy", description: "Calculate break-even units and revenue from your costs and price.", type: "website", url: "/tools/break-even-calculator" },
};

export default function BreakEvenCalculatorPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="Break-even calculator" />
    <header className="tools-page-heading">
      <p className="tools-overline">Pricing and profitability</p>
      <h1>Know the point where costs are covered.</h1>
      <p>Calculate how many units a small business needs to sell before a product or service covers its fixed costs.</p>
    </header>
    <BreakEvenCalculator />
    <section className="tools-explain" aria-labelledby="break-even-explanation">
      <h2 id="break-even-explanation">How does break-even work?</h2>
      <p>Subtract the variable cost per unit from the selling price to get the <strong>contribution per unit</strong>. Divide fixed costs for the period by that contribution. Round up to a whole unit, because you generally cannot sell a fraction of a unit.</p>
      <p>For example, if monthly fixed costs are $3,000, the selling price is $120 and the variable cost is $80, each unit contributes $40. You need to sell <strong>75 units</strong> to cover those fixed costs. Revenue at that point is $9,000.</p>
      <p>If selling price is equal to or less than variable cost, increasing volume cannot cover positive fixed costs. This simple model excludes changes in demand, taxes, finance costs and different product mixes.</p>
      <p>Read the <a href="https://business.vic.gov.au/business-information/finance/pricing-for-profit/calculate-your-breakeven-point-margin-and-markup" target="_blank" rel="noopener noreferrer">Business Victoria guide to break-even, margin and markup</a> for the formulas and context.</p>
    </section>
    <aside className="tools-related" aria-label="Related resources">
      <a href="/tools/profit-margin-calculator">Compare margin and markup <span aria-hidden="true">↗</span></a>
      <a href="/blog/margin-is-not-markup">Read the pricing notebook <span aria-hidden="true">↗</span></a>
    </aside>
    <ToolStructuredData name="Australian break-even calculator" description="Estimate the units and revenue needed to cover fixed costs from unit selling price and variable costs." path="/tools/break-even-calculator" />
  </ToolsShell>;
}
