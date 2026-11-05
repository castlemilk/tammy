/* oxlint-disable next/no-html-link-for-pages -- Vinext handles these static routes. */
import type { Metadata } from "next";
import { ToolsShell } from "../../../components/tools-shell";
import { GuideStructuredData } from "../../../components/guide-structured-data";

export const metadata: Metadata = {
  title: "Australian tax invoice requirements: a practical checklist",
  description: "What an Australian tax invoice needs: supplier ABN, dates, goods or services, GST, recipient details for invoices of $1,000 or more, and practical examples.",
  alternates: { canonical: "/guides/australian-tax-invoice-checklist" },
  keywords: ["Australian tax invoice requirements", "tax invoice checklist Australia", "ABN invoice requirements", "GST tax invoice $1000"],
  openGraph: { type: "article", title: "Australian tax invoice checklist | Tammy", url: "/guides/australian-tax-invoice-checklist" },
};

const essentials = [
  ["Supplier details", "A clear supplier identity and Australian Business Number (ABN)."],
  ["Issue date", "The date the tax invoice was issued."],
  ["What was supplied", "Description, quantity where relevant and price of goods or services."],
  ["GST treatment", "Which supplies are taxable and the GST amount payable in relation to each taxable supply."],
  ["Recipient at $1,000+", "For supplies totalling $1,000 or more, enough detail to identify the recipient, or their ABN."],
] as const;

export default function TaxInvoiceChecklistPage() {
  return <ToolsShell>
    <nav className="tools-breadcrumb" aria-label="Breadcrumb"><a href="/">Home</a><span aria-hidden="true">/</span><a href="/guides">Guides</a><span aria-hidden="true">/</span><span aria-current="page">Tax invoice checklist</span></nav>
    <article className="tools-editorial">
      <header className="tools-editorial-heading">
        <p className="tools-overline">Australian invoicing</p>
        <h1>What goes on a tax invoice?</h1>
        <p className="tools-editorial-lede">The details behind a document your customer can use to account for a GST-creditable purchase, without unnecessary paperwork theatre.</p>
        <div className="tools-editorial-meta"><span>For Australian businesses</span><span>Includes a practical checklist</span></div>
      </header>
      <div className="tools-editorial-grid">
        <nav className="tools-article-toc" aria-label="On this page">
          <strong>On this page</strong>
          <a href="#tax-invoice-or-invoice">Which invoice?</a>
          <a href="#required-details">Required details</a>
          <a href="#invoice-1000">The $1,000 threshold</a>
          <a href="#gst-example">Worked example</a>
          <a href="#check-before-sending">Before sending</a>
        </nav>
        <div className="tools-article-body">
          <section id="tax-invoice-or-invoice">
            <h2>Invoice or tax invoice?</h2>
            <p>A business registered for GST generally supplies tax invoices for its taxable sales. A non-GST-registered business must not charge GST and ordinarily sends an invoice rather than labelling it a tax invoice. An ABN by itself does not prove GST registration.</p>
            <p>A tax invoice needs to clearly identify the taxable supplies and their GST amounts. A single invoice may include GST-free items alongside taxable supplies; show the treatment of each clearly instead of applying 10% to everything.</p>
          </section>
          <section id="required-details">
            <h2>A five-part checklist</h2>
            <ol className="tools-checklist">
              {essentials.map(([title, detail])=><li key={title}><strong>{title}</strong><span>{detail}</span></li>)}
            </ol>
            <p>These are the central information requirements from the ATO’s ruling on tax invoices. Other rules apply to particular supplies and recipient-created tax invoices. The official guidance takes precedence over a general-purpose template.</p>
          </section>
          <section id="invoice-1000">
            <h2>When the invoice reaches $1,000</h2>
            <p>For a supply with a total price of at least <strong>$1,000, including GST</strong>, the tax invoice also needs enough information to identify the recipient, or the recipient’s ABN. For amounts below that threshold, recipient identity is not generally one of the core tax-invoice requirements, though business-to-business invoices benefit from including it.</p>
            <p>For example, if you invoice a client $1,100 including GST, identify the customer on the document as well as your own business.</p>
          </section>
          <section id="gst-example">
            <h2>A simple $1,100 example</h2>
            <div className="tools-article-example">
              <div><span>Services excluding GST</span><strong>$1,000.00</strong></div>
              <div><span>GST at 10%</span><strong>$100.00</strong></div>
              <div><span>Total including GST</span><strong>$1,100.00</strong></div>
            </div>
            <p>Make the invoice number and payment terms prominent for your own records, even though not all common business conveniences are requirements of an ATO tax invoice.</p>
          </section>
          <section id="check-before-sending">
            <h2>Before you send it</h2>
            <p>Confirm that your ABN is current, the GST treatment is correct, prices and totals reconcile, and the recipient details are accurate. Keep a copy of the final invoice in your records and send it using your normal business channels.</p>
            <p>Use the free <a href="/tools/abn-checker">ABN checksum checker</a> for a preliminary format check, or the <a href="/tools/invoice-generator">Tammy invoice generator</a> for an editable draft and downloadable PDF. Neither performs a live ABR registration check or sends an invoice for you.</p>
          </section>
          <section className="tools-article-sources" aria-label="Primary sources">
            <h2>Primary sources</h2>
            <p><a href="https://www.ato.gov.au/law/view/document?DocID=GST/GSTR20131/NAT/ATO/00001" target="_blank" rel="noopener noreferrer">ATO GSTR 2013/1: Tax invoices</a> details the required information, including the recipient-identification threshold. See also the <a href="https://business.gov.au/finance/payments-and-invoicing/how-to-invoice" target="_blank" rel="noopener noreferrer">Australian Government’s invoicing guide</a> for everyday business invoicing practices.</p>
          </section>
        </div>
      </div>
    </article>
    <section className="tools-article-action">
      <div><p className="tools-overline">Put the checklist to work</p><h2>Create an Australian invoice.</h2><p>Enter your business details, items, dates and GST treatment, then download a PDF without an account.</p></div>
      <a className="tools-primary-link" href="/tools/invoice-generator">Open invoice generator <span aria-hidden="true">↗</span></a>
    </section>
    <GuideStructuredData name="Australian tax invoice requirements: a practical checklist" description="A practical checklist for Australian tax invoices, including supplier ABNs, taxable supplies, GST, the $1,000 recipient threshold and worked examples." path="/guides/australian-tax-invoice-checklist" />
  </ToolsShell>;
}
