/* oxlint-disable next/no-html-link-for-pages -- Vinext handles static same-origin routes. */
/* oxlint-disable next/no-img-element -- Local decorative SVG icons are not raster content. */
import type { Metadata } from "next";
import { ToolsShell, origin } from "../../components/tools-shell";

export const metadata: Metadata = {
  title: "Free Australian business tools",
  description: "Free Australian invoice PDF generator, GST and ABN checker, margin, break-even, cash runway and payment-date calculators. No signup or upload.",
  alternates: { canonical: "/tools" },
  keywords: ["Australian business calculators", "free invoice generator Australia", "GST calculator Australia", "ABN checker", "profit margin calculator", "break even calculator", "cash runway calculator"],
  openGraph: { title: "Free business tools | Tammy", description: "Invoices, GST and payment terms, made clearer.", url: origin + "/tools", type: "website" },
};

const items = [
  { number: "01", href: "/tools/invoice-generator", icon: "invoices", title: "Invoice generator", detail: "Create an Australian invoice with line items, 10% GST and a PDF you can keep.", meta: "Tax invoices and regular invoices" },
  { number: "02", href: "/tools/gst-calculator", icon: "gst-bas", title: "GST calculator", detail: "Add or remove 10% GST with cent-accurate totals and a clear breakdown.", meta: "Inclusive and exclusive" },
  { number: "03", href: "/tools/invoice-due-date-calculator", icon: "review", title: "Invoice due-date calculator", detail: "Work out when payment is due from an invoice date and agreed terms.", meta: "7, 14, 30, 45 or 60 days" },
  { number: "04", href: "/tools/abn-checker", icon: "abn-form", title: "ABN checker", detail: "Validate an Australian Business Number checksum before checking official registration details.", meta: "11-digit, modulus-89 format check" },
  { number: "05", href: "/tools/profit-margin-calculator", icon: "margin-scale", title: "Profit margin calculator", detail: "Compare gross profit, markup and margin from your cost and selling price.", meta: "Ex-GST AUD pricing" },
  { number: "06", href: "/tools/break-even-calculator", icon: "ledger", title: "Break-even calculator", detail: "Calculate how many units you need to sell to cover fixed costs.", meta: "Contribution per unit and sales" },
  { number: "07", href: "/tools/cash-runway-calculator", icon: "reconciliation", title: "Cash runway calculator", detail: "Estimate how far your available cash will go at a steady monthly burn.", meta: "Simple monthly cash flow" },
] as const;

export default function ToolsPage() {
  const data = {
    "@context": "https://schema.org",
    "@type": "CollectionPage",
    name: "Free Australian business tools",
    url: origin + "/tools",
    hasPart: items.map(({ href, title }) => ({ "@type": "WebPage", name: title, url: origin + href })),
  };
  return (
    <ToolsShell>
      <section className="tools-hero tools-index-hero">
        <div>
          <p className="tools-overline">Tammy tools</p>
          <h1>Good tools. <em>Clearer books.</em></h1>
          <p>Make invoices, check GST and ABNs, calculate margins and break-even points, or estimate cash runway. Everyday business tools, without a signup.</p>
          <a className="tools-primary-link" href="/tools/invoice-generator">Create an invoice <span aria-hidden="true">↗</span></a>
        </div>
        <div className="tools-hero-art" aria-hidden="true"><img className="tools-hero-illustration" src="/assets/tools-workflow-engraving.svg" alt="" /></div>
      </section>
      <section className="tools-content-section" aria-labelledby="available-tools-heading">
        <div className="tools-section-heading"><p className="tools-overline">Business essentials</p><h2 id="available-tools-heading">Choose what you need.</h2></div>
        <div className="tools-card-grid">
          {items.map(item => <a className="tools-card" href={item.href} key={item.href}>
            <span className="tools-card-number">{item.number}</span>
            <span className="tools-card-icon" aria-hidden="true"><img src={"/assets/" + item.icon + ".svg"} alt="" aria-hidden="true" /></span>
            <h3>{item.title}</h3><p>{item.detail}</p>
            <span className="tools-card-foot">{item.meta}<span aria-hidden="true">↗</span></span>
          </a>)}
        </div>
      </section>
      <section className="tools-guides-callout" aria-labelledby="tools-guide-heading">
        <div className="tools-guides-callout-copy"><p className="tools-overline">Practical reading</p><h2 id="tools-guide-heading">Understand the numbers behind the tools.</h2><p>Original Australian invoicing and GST explainers, with source references and examples you can check yourself.</p><a href="/guides" className="tools-secondary-link">Browse all guides <span aria-hidden="true">↗</span></a><a href="/blog" className="tools-secondary-link tools-journal-link">Read the Tammy Journal <span aria-hidden="true">↗</span></a></div>
        <div className="tools-guides-callout-links">
          <a href="/guides/australian-tax-invoice-checklist"><span>Invoicing</span><strong>What an Australian tax invoice needs</strong><span aria-hidden="true">↗</span></a>
          <a href="/guides/gst-inclusive-vs-exclusive"><span>GST</span><strong>Inclusive and exclusive GST, explained</strong><span aria-hidden="true">↗</span></a>
        </div>
      </section>
      <section className="tools-trust-band"><strong>Your numbers stay your numbers.</strong><p>Calculations and invoice data remain in this browser tab. There is no account, backend submission or tracking added by these tools.</p></section>
      <section className="tools-content-section tools-index-bridge"><div><p className="tools-overline">A step further</p><h2>From quick calculations to a real ledger.</h2></div><p>Tammy is building a private Mac accounting workspace for journals, source documents and GST/BAS drafts. The public tools above work independently of that development software.</p><a href="/" className="tools-secondary-link">Explore Tammy</a></section>
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(data) }} />
    </ToolsShell>
  );
}
