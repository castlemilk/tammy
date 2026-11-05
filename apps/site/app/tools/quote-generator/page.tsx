/* oxlint-disable next/no-html-link-for-pages -- Vinext serves same-origin static routes. */
import type { Metadata } from "next";
import { QuoteGenerator } from "../../../components/quote-generator";
import { ToolStructuredData, ToolsBreadcrumb, ToolsShell } from "../../../components/tools-shell";

export const metadata: Metadata = {
  title: "Free Australian quote and estimate generator",
  description: "Create an itemised Australian business quote with GST treatment, reference and validity date. Download a PDF directly or save a local draft JSON. No signup or upload.",
  alternates: { canonical: "/tools/quote-generator" },
  keywords: ["free quote generator Australia", "small business estimate template", "GST quote template", "print quote as PDF"],
  openGraph: {
    type: "website",
    title: "Australian quote generator | Tammy",
    description: "Itemised quotes with GST, direct PDF export and user-controlled local drafts.",
    url: "/tools/quote-generator",
  },
};

export default function QuoteGeneratorPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="Quote generator" />
    <header className="tools-page-heading">
      <p className="tools-overline">Free business tool</p>
      <h1>A clear quote, before the work begins.</h1>
      <p>Set out your proposed work, GST treatment and validity date. Print it or save as a PDF in your browser. No signup and no data upload.</p>
    </header>
    <QuoteGenerator />
    <section className="tools-explain" aria-labelledby="quote-explain">
      <h2 id="quote-explain">Quote first, invoice later.</h2>
      <p>A quote communicates a proposed price, scope and validity period. It is not an issued tax invoice or evidence that a customer has accepted the work. Use explicit assumptions and confirm the terms directly with your customer.</p>
      <p>For a business registered for GST, this tool can include standard taxable and GST-free quoted lines. Non-GST-registered businesses should not charge GST. The totals use the same validated integer-cent arithmetic as Tammy&apos;s invoice model, but printing a quote does not post sales, GST or receivables.</p>
      <p>Download your quote as a PDF from the browser, use Print as a fallback, or explicitly export a draft JSON file that you can reopen later. Draft files contain the details you entered and are not encrypted. Quote-to-invoice conversion and accounting posting remain separate future work.</p>
      <p>See <a href="https://business.gov.au/finance/payments-and-invoicing/how-to-invoice" rel="noopener noreferrer" target="_blank">Australian Government invoicing guidance</a> before turning accepted work into an invoice.</p>
    </section>
    <aside className="tools-related" aria-label="Related resources">
      <a href="/tools/invoice-generator">Create an invoice <span aria-hidden="true">↗</span></a>
      <a href="/tools/gst-calculator">Calculate GST <span aria-hidden="true">↗</span></a>
    </aside>
    <ToolStructuredData name="Australian quote generator" description="Prepare an estimate with itemised GST and print or save it as a PDF without uploading data." path="/tools/quote-generator" />
  </ToolsShell>;
}
