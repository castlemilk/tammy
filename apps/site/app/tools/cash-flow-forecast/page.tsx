/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { CashFlowForecast } from "../../../components/cash-flow-forecast";
import { ToolStructuredData, ToolsBreadcrumb, ToolsShell } from "../../../components/tools-shell";

export const metadata: Metadata = {
  title: "Free 12-week cash flow forecast and CSV export Australia",
  description: "Build a dated 12-week Australian business cash forecast from opening cash and expected payments. Explore shortfalls and download the weekly CSV in your browser.",
  alternates: { canonical: "/tools/cash-flow-forecast" },
  keywords: ["12 week cash flow forecast", "free cash flow forecast Australia", "weekly business cash flow", "cash forecast CSV"],
  openGraph: { type:"website",title:"12-week cash flow forecast | Tammy",url:"/tools/cash-flow-forecast" },
};

export default function CashFlowForecastPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="Cash flow forecast" />
    <header className="tools-page-heading">
      <p className="tools-overline">Free Australian business planning</p>
      <h1>Put expected cash on the calendar.</h1>
      <p>Build a dated twelve-week forecast from an opening balance, incoming receipts and outgoing payments. See the lowest projected balance and export your own CSV without signing in.</p>
    </header>
    <CashFlowForecast />
    <section className="tools-explain" aria-labelledby="forecast-explain">
      <h2 id="forecast-explain">Why a weekly view?</h2>
      <p>Monthly averages hide the week when a supplier invoice or GST payment is due before customers pay. A weekly forecast starts with available cash, adds expected receipts and subtracts payments at their likely dates. Each closing balance becomes the next week's opening balance.</p>
      <p>This is a scenario based only on dates and amounts you enter. It does not connect to bank feeds, predict payment behaviour, calculate GST or store your records. Save the generated CSV in a suitable private location if you want to retain your work.</p>
      <p>See <a href="https://business.gov.au/finance/cash-flow/set-up-a-cash-flow-statement" rel="noopener noreferrer" target="_blank">business.gov.au guidance on cash flow statements</a> and the <a href="/blog/the-12-week-cash-flow-view">Tammy twelve-week cash flow guide</a>.</p>
    </section>
    <aside className="tools-related" aria-label="Related resources">
      <a href="/tools/cash-runway-calculator">Estimate monthly cash runway <span aria-hidden="true">↗</span></a>
      <a href="/blog/when-profit-and-cash-part-ways">Understand profit versus cash <span aria-hidden="true">↗</span></a>
    </aside>
    <ToolStructuredData name="Twelve-week cash flow forecast" description="Prepare a dated twelve-week cash scenario with weekly cash balance, expected receipts and payments, and downloadable CSV." path="/tools/cash-flow-forecast" />
  </ToolsShell>;
}
