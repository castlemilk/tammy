/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { CashRunwayCalculator } from "../../../components/cash-runway-calculator";
import { ToolsShell, ToolsBreadcrumb, ToolStructuredData } from "../../../components/tools-shell";

export const metadata: Metadata = {
  title: "Cash runway calculator for Australian small business",
  description: "Estimate small-business cash runway using available cash, average monthly receipts and monthly payments. Free private browser calculator.",
  alternates: { canonical: "/tools/cash-runway-calculator" },
  keywords: ["cash runway calculator Australia", "business cash burn calculator", "monthly cash flow calculator", "small business cash runway"],
  openGraph: { title: "Cash runway calculator | Tammy", description: "Estimate how long your available cash lasts at a constant monthly net burn.", type: "website", url: "/tools/cash-runway-calculator" },
};

export default function CashRunwayCalculatorPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="Cash runway calculator" />
    <header className="tools-page-heading">
      <p className="tools-overline">Cash flow planning</p>
      <h1>How far will the cash carry you?</h1>
      <p>Estimate runway from a cash balance and expected monthly receipts and payments. See the gap before it becomes a surprise.</p>
    </header>
    <CashRunwayCalculator />
    <section className="tools-explain" aria-labelledby="cash-explanation">
      <h2 id="cash-explanation">What is cash runway?</h2>
      <p>Runway estimates how long available cash lasts when payments exceed receipts at a steady rate. First calculate <strong>monthly cash burn</strong> by subtracting receipts from payments. Then divide available cash by that monthly burn.</p>
      <p>For example, $24,000 in cash, $6,000 in monthly receipts and $10,000 in monthly payments gives a net burn of $4,000 a month and a simplified runway of <strong>6.0 months</strong>.</p>
      <p>Real businesses rarely have perfectly even months. Payroll cycles, BAS payments, annual bills, customer delays and seasonal sales can make cash run short earlier. Use a dated cash-flow forecast as the next step, not this single number as a promise.</p>
      <p>For a forecast template and explanation, see <a href="https://business.gov.au/finance/cash-flow/set-up-a-cash-flow-statement" target="_blank" rel="noopener noreferrer">business.gov.au: Set up a cash flow statement</a>.</p>
    </section>
    <aside className="tools-related" aria-label="Related resources">
      <a href="/blog/the-12-week-cash-flow-view">Read the 12-week cash-flow guide <span aria-hidden="true">↗</span></a>
      <a href="/tools/invoice-due-date-calculator">Work out an invoice due date <span aria-hidden="true">↗</span></a>
    </aside>
    <ToolStructuredData name="Cash runway calculator" description="Estimate cash runway from opening cash, expected monthly receipts and monthly payments." path="/tools/cash-runway-calculator" />
  </ToolsShell>;
}
