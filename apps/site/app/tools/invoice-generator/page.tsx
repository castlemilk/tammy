/* oxlint-disable next/no-html-link-for-pages -- Vinext serves these static same-origin routes. */
import type { Metadata } from "next";
import { ToolStructuredData, ToolsBreadcrumb, ToolsShell } from "../../../components/tools-shell";
import { InvoiceGenerator } from "../../../components/invoice-generator";

export const metadata: Metadata = {
  title: "Free Australian invoice generator | PDF",
  description: "Create a professional Australian invoice online. Add your ABN, client details, line items, GST and payment terms; download an A4 PDF. No account or upload.",
  alternates: { canonical: "/tools/invoice-generator" },
  keywords: ["free invoice generator Australia", "Australian tax invoice PDF", "ABN invoice template", "invoice maker GST Australia"],
  openGraph: { title: "Australian invoice generator | Tammy", description: "Make a tax invoice or regular invoice and save a PDF privately.", url: "/tools/invoice-generator", type: "website" },
};
export default function InvoiceGeneratorPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="Invoice generator" />
    <header className="tools-page-heading">
      <p className="tools-overline">Free business tool</p>
      <h1>Make an invoice worth keeping.</h1>
      <p>Prepare a regular invoice or an Australian tax invoice, check the totals and download a PDF. Your details stay in your browser, with no account or data upload.</p>
    </header>
    <InvoiceGenerator />
    <section className="tools-explain">
      <h2>What makes an Australian tax invoice?</h2>
      <p>GST-registered suppliers generally label the document “Tax invoice”, show their business name and ABN, issue date, a description and quantity/price of what was supplied, and the GST payable. For invoices of $1,000 or more, the recipient must also be identifiable. Non-GST-registered businesses should issue an ordinary invoice without claiming GST.</p>
      <p>This tool supports simple taxable and GST-free line items. It does not handle adjustments, discounts, mixed supply allocation, reverse charges or specialised tax treatments. Check your invoice against the <a href="https://business.gov.au/finance/payments-and-invoicing/how-to-invoice" target="_blank" rel="noreferrer">Australian Government invoicing guidance</a> before sending it.</p>
      <p>The PDF uses Tammy’s existing pdfcn-adapted Forme renderer and is created within your browser. Downloading it does not issue an invoice in the Tammy desktop app, post accounting entries or send anything to your customer.</p>
    </section>
    <aside className="tools-related" aria-label="Related calculators">
      <a href="/tools/gst-calculator">Calculate GST <span aria-hidden="true">↗</span></a>
      <a href="/tools/invoice-due-date-calculator">Work out payment terms <span aria-hidden="true">↗</span></a>
    </aside>
    <ToolStructuredData name="Australian invoice generator" description="Create an A4 invoice PDF locally in your browser with ABN and GST fields." path="/tools/invoice-generator" />
  </ToolsShell>;
}
