import type { Metadata } from "next";
import { ToolsShell, ToolsBreadcrumb, ToolStructuredData } from "../../../components/tools-shell";
import { ProfitMarginCalculator } from "../../../components/profit-margin-calculator";

export const metadata: Metadata = {
  title: "Profit margin and markup calculator Australia",
  description: "Calculate gross profit, margin percentage and markup percentage from cost and selling price in AUD. Free online calculator with clear GST-exclusive inputs.",
  alternates: { canonical: "/tools/profit-margin-calculator" },
  keywords: ["profit margin calculator", "markup calculator", "gross profit calculator Australia", "margin versus markup"],
  openGraph: { type: "website", title: "Profit margin calculator | Tammy", description: "See gross profit, margin and markup from cost and selling price.", url: "/tools/profit-margin-calculator" },
};

export default function ProfitMarginCalculatorPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="Profit margin calculator" />
    <header className="tools-page-heading">
      <p className="tools-overline">Pricing and profitability</p>
      <h1>A better sense of the margin.</h1>
      <p>Enter a product or service cost and its selling price. See the gross profit, gross margin and markup side by side.</p>
    </header>
    <ProfitMarginCalculator />
    <section className="tools-explain" aria-labelledby="margin-explain">
      <h2 id="margin-explain">Margin and markup are different.</h2>
      <p><strong>Gross profit</strong> is selling price minus direct cost. <strong>Margin</strong> expresses profit as a share of the selling price, while <strong>markup</strong> expresses profit as a share of the cost. They share the same dollar profit, but use different denominators.</p>
      <div className="tools-example-comparison" aria-label="Margin and markup example">
        <div><span>Cost, ex GST</span><strong>$80.00</strong></div>
        <div><span>Selling price, ex GST</span><strong>$120.00</strong></div>
        <div><span>Margin</span><strong>33.33%</strong></div>
        <div><span>Markup</span><strong>50.00%</strong></div>
      </div>
      <p>Use ex-GST amounts for both inputs when comparing prices for a GST-registered business. The calculator uses per-unit direct costs and does not estimate overhead, shipping, merchant fees or tax unless you include them in the cost.</p>
    </section>
    <aside className="tools-related" aria-label="Related calculators">
      <a href="/tools/gst-calculator">Calculate GST first <span aria-hidden="true">↗</span></a>
      <a href="/tools/invoice-generator">Create an invoice <span aria-hidden="true">↗</span></a>
    </aside>
    <ToolStructuredData name="Profit margin and markup calculator" description="Calculate margin, markup and gross profit from AUD selling and cost prices." path="/tools/profit-margin-calculator" />
  </ToolsShell>;
}
