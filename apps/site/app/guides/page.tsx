/* oxlint-disable next/no-html-link-for-pages -- Vinext handles these static routes. */
import type { Metadata } from "next";
import { ToolsShell, origin } from "../../components/tools-shell";

export const metadata: Metadata = {
  title: "Australian invoicing and GST guides",
  description: "Plain-language Australian small business guides: tax invoice requirements, GST inclusive and exclusive calculations, with practical examples and free tools.",
  alternates: { canonical: "/guides" },
  openGraph: { type: "website", title: "Australian accounting guides | Tammy", url: origin + "/guides", description: "Useful GST and invoicing explainers with examples and references." },
};

const guides = [
  { href: "/guides/australian-tax-invoice-checklist", type: "Invoicing", title: "Australian tax invoice checklist", description: "What a tax invoice needs to contain, when to show GST and how the $1,000 recipient threshold works." },
  { href: "/guides/gst-inclusive-vs-exclusive", type: "GST", title: "GST inclusive and exclusive: explained", description: "Add 10% GST, extract the 1/11 component and understand why some supplies are treated differently." },
] as const;

export default function GuidesIndexPage() {
  return <ToolsShell>
    <nav className="tools-breadcrumb" aria-label="Breadcrumb"><a href="/">Home</a><span aria-hidden="true">/</span><span aria-current="page">Guides</span></nav>
    <header className="tools-page-heading tools-guides-intro"><p className="tools-overline">A clearer way through the details</p><h1>Useful notes for the books.</h1><p>Practical Australian business explanations with worked examples, links to primary sources and free tools you can use directly.</p></header>
    <section className="tools-guide-index" aria-label="Available guides">
      {guides.map((guide, index) => <a href={guide.href} className="tools-guide-card" key={guide.href}>
        <span className="tools-guide-card-number">{String(index + 1).padStart(2, "0")}</span>
        <div><p className="tools-overline">{guide.type}</p><h2>{guide.title}</h2><p>{guide.description}</p></div>
        <span aria-hidden="true" className="tools-guide-card-arrow">↗</span>
      </a>)}
    </section>
    <section className="tools-guide-cta">
      <div><p className="tools-overline">Prefer to work with the numbers?</p><h2>Try the calculators.</h2><p>Invoice, GST, ABN and profitability tools are available without signing up.</p></div>
      <a href="/tools" className="tools-primary-link">Browse free tools <span aria-hidden="true">↗</span></a>
    </section>
    <aside className="tools-related" aria-label="More reading">
      <a href="/blog">Read the Tammy Journal <span aria-hidden="true">↗</span></a>
      <a href="/blog/the-12-week-cash-flow-view">See a rolling cash-flow example <span aria-hidden="true">↗</span></a>
    </aside>
    <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify({
      "@context": "https://schema.org", "@type": "CollectionPage",
      name: "Australian invoicing and GST guides", url: origin + "/guides",
      hasPart: guides.map(guide => ({ "@type": "WebPage", name: guide.title, url: origin + guide.href }))
    }) }} />
  </ToolsShell>;
}
