/* oxlint-disable next/no-html-link-for-pages -- Vinext handles static same-origin routes. */
/* oxlint-disable next/no-img-element -- Local decorative SVG icons are not raster content. */
import type { Metadata } from "next";
import { ToolsShell, origin } from "../../components/tools-shell";
import { ToolLibrary } from "../../components/tool-library";
import { publicTools } from "../../lib/tool-library";

export const metadata: Metadata = {
  title: "Free Australian business tools",
  description: "Free invoice and quote generators, BAS workpaper, sole-trader tax checklist, GST, pricing and cash flow tools. No signup or upload.",
  alternates: { canonical: "/tools" },
  keywords: ["Australian business calculators", "free invoice generator Australia", "GST calculator Australia", "ABN checker", "profit margin calculator", "break even calculator", "cash runway calculator"],
  openGraph: { title: "Free business tools | Tammy", description: "Invoices, GST and payment terms, made clearer.", url: origin + "/tools", type: "website" },
};

export default function ToolsPage() {
  const data = {
    "@context": "https://schema.org",
    "@type": "CollectionPage",
    name: "Free Australian business tools",
    url: origin + "/tools",
    hasPart: publicTools.map(({ href, name }) => ({ "@type": "WebPage", name, url: origin + href })),
  };
  return (
    <ToolsShell>
      <section className="tools-hero tools-index-hero">
        <div>
          <h1>Free business tools</h1>
          <p>Create an invoice or quote, calculate GST, or plan cash flow. Use all twelve tools in your browser, without an account.</p>
        </div>
        <div className="tools-hero-art" aria-hidden="true"><img className="tools-hero-illustration" src="/assets/tammy-ledger-helix.svg" alt="" /></div>
      </section>
      <section className="tools-content-section tools-directory" aria-labelledby="available-tools-heading">
        <h2 className="sr-only" id="available-tools-heading">Choose a business tool</h2>
        <ToolLibrary />
      </section>
      <section className="tools-guides-callout" aria-labelledby="tools-guide-heading">
        <div className="tools-guides-callout-copy"><h2 id="tools-guide-heading">Guides and worked examples</h2><p>Australian invoicing and GST explainers with source references and calculations you can check.</p><a href="/guides" className="tools-secondary-link">Browse all guides <span aria-hidden="true">↗</span></a><a href="/blog" className="tools-secondary-link tools-journal-link">Read the Tammy Journal <span aria-hidden="true">↗</span></a></div>
        <div className="tools-guides-callout-links">
          <a href="/guides/australian-tax-invoice-checklist"><span>Invoicing</span><strong>What an Australian tax invoice needs</strong><span aria-hidden="true">↗</span></a>
          <a href="/guides/gst-inclusive-vs-exclusive"><span>GST</span><strong>Inclusive and exclusive GST, explained</strong><span aria-hidden="true">↗</span></a>
        </div>
      </section>
      <section className="tools-trust-band"><strong>Calculations stay in your browser.</strong><p>Calculations and invoice data remain in this browser tab. There is no account, backend submission or tracking added by these tools.</p></section>
      <section className="tools-content-section tools-index-bridge"><div><h2>Tammy for Mac</h2></div><p>Tammy is building a private Mac accounting workspace for journals, source documents and GST/BAS drafts. These public tools work independently of that development software.</p><a href="/" className="tools-secondary-link">Explore Tammy</a></section>
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(data) }} />
    </ToolsShell>
  );
}
