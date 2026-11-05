/* oxlint-disable next/no-html-link-for-pages -- Vinext serves these static same-origin routes. */
import type { Metadata } from "next";
import { ToolStructuredData, ToolsBreadcrumb, ToolsShell } from "../../../components/tools-shell";
import { GstCalculator } from "../../../components/gst-calculator";

export const metadata: Metadata = {
  title: "Australian GST calculator | Add or remove 10%",
  description: "Calculate 10% Australian GST inclusive and exclusive amounts, see the GST component and total in AUD. Free browser-based calculator.",
  alternates: { canonical: "/tools/gst-calculator" },
  keywords: ["GST calculator Australia", "GST inclusive calculator", "GST exclusive calculator", "add 10% GST", "remove GST"],
  openGraph: { title: "GST calculator Australia | Tammy", description: "Add or remove 10% GST with an exact cent breakdown.", url: "/tools/gst-calculator", type: "website" },
};

export default function GstCalculatorPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="GST calculator" />
    <header className="tools-page-heading"><p className="tools-overline">Free business tool</p><h1>GST, without the guesswork.</h1><p>Add 10% GST to a price or extract the GST included in a total. See all three amounts in Australian dollars.</p></header>
    <GstCalculator />
    <section className="tools-explain"><h2>How is Australian GST calculated?</h2><p>For a standard taxable supply, GST is 10% of the price excluding GST. Where the price already includes 10% GST, the GST component is one-eleventh of the inclusive total, with rounding to the nearest cent.</p><p>This calculator assumes a fully taxable supply at the standard 10% rate. GST-free, input-taxed, out-of-scope and partly taxable supplies require separate treatment. Read the <a href="https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/gst" target="_blank" rel="noreferrer">ATO GST guidance</a> when classifying a transaction.</p></section>
    <aside className="tools-related" aria-label="Related tools"><a href="/tools/invoice-generator">Create an invoice <span aria-hidden="true">↗</span></a><a href="/tools/invoice-due-date-calculator">Calculate a due date <span aria-hidden="true">↗</span></a></aside>
    <ToolStructuredData name="Australian GST calculator" description="Add or extract standard Australian 10% GST using integer-cent arithmetic." path="/tools/gst-calculator" />
  </ToolsShell>;
}
