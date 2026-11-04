/* oxlint-disable next/no-html-link-for-pages -- Vinext handles static same-origin routes. */
import type { Metadata } from "next";
import { ToolsShell, origin } from "../../components/tools-shell";

export const metadata: Metadata = {
  title: "Free Australian business tools",
  description: "Free, privacy-conscious Australian invoice generator, 10% GST calculator and invoice due-date calculator. No account or data upload needed.",
  alternates: { canonical: "/tools" },
  keywords: ["Australian business calculators", "free invoice generator Australia", "GST calculator Australia", "invoice due date calculator"],
  openGraph: { title: "Free business tools | Tammy", description: "Invoices, GST and payment terms, made clearer.", url: origin + "/tools", type: "website" },
};

const items = [
  { number: "01", href: "/tools/invoice-generator", icon: "▤", title: "Invoice generator", detail: "Create an Australian invoice with line items, 10% GST and a PDF you can keep.", meta: "Tax invoices and regular invoices" },
  { number: "02", href: "/tools/gst-calculator", icon: "%", title: "GST calculator", detail: "Add or remove 10% GST with cent-accurate totals and a clear breakdown.", meta: "Inclusive and exclusive" },
  { number: "03", href: "/tools/invoice-due-date-calculator", icon: "↗", title: "Invoice due-date calculator", detail: "Work out when payment is due from an invoice date and agreed terms.", meta: "7, 14, 30, 45 or 60 days" },
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
          <p>Useful little instruments for Australian business. Make an invoice, calculate GST or work out a due date. No account required.</p>
          <a className="tools-primary-link" href="/tools/invoice-generator">Create an invoice <span aria-hidden="true">↗</span></a>
        </div>
        <div className="tools-hero-art" aria-hidden="true"><span className="tools-art-orbit"/><span className="tools-art-paper"/><span className="tools-art-stamp">10% GST</span><span className="tools-art-line"/></div>
      </section>
      <section className="tools-content-section" aria-labelledby="available-tools-heading">
        <div className="tools-section-heading"><p className="tools-overline">Business essentials</p><h2 id="available-tools-heading">Choose what you need.</h2></div>
        <div className="tools-card-grid">
          {items.map(item => <a className="tools-card" href={item.href} key={item.href}>
            <span className="tools-card-number">{item.number}</span>
            <span className="tools-card-icon" aria-hidden="true">{item.icon}</span>
            <h3>{item.title}</h3><p>{item.detail}</p>
            <span className="tools-card-foot">{item.meta}<span aria-hidden="true">↗</span></span>
          </a>)}
        </div>
      </section>
      <section className="tools-trust-band"><strong>Your numbers stay your numbers.</strong><p>Calculations and invoice data remain in this browser tab. There is no account, backend submission or tracking added by these tools.</p></section>
      <section className="tools-content-section tools-index-bridge"><div><p className="tools-overline">A step further</p><h2>From quick calculations to a real ledger.</h2></div><p>Tammy is building a private Mac accounting workspace for journals, source documents and GST/BAS drafts. The public tools above work independently of that development software.</p><a href="/" className="tools-secondary-link">Explore Tammy</a></section>
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(data) }} />
    </ToolsShell>
  );
}
