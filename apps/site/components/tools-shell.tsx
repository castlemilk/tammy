/* oxlint-disable next/no-html-link-for-pages -- Vinext handles these static same-origin routes. */
import type { ReactNode } from "react";
import { SITE_ORIGIN } from "../lib/site-config";

export const origin = SITE_ORIGIN;

export function ToolsShell({ children }: { readonly children: ReactNode }) {
  return (
    <div className="tools-shell">
      <a href="#tools-main" className="skip-link">Skip to content</a>
      <header className="tools-header">
        <a href="/" className="tools-logo" aria-label="Tammy home"><span className="tools-leaf" aria-hidden="true" />Tammy</a>
        <nav aria-label="Tools navigation">
          <a href="/tools">All tools</a>
          <a href="/tools/invoice-generator">Invoices</a>
          <a href="/tools/gst-calculator">GST</a>
          <a href="/guides">Guides</a>
          <a href="/blog">Journal</a>
        </nav>
        <a href="/" className="tools-back">About Tammy</a>
      </header>
      <main id="tools-main">{children}</main>
      <footer className="tools-footer">
        <div><a className="tools-logo" href="/">Tammy</a><p>Thoughtful tools for Australian businesses.</p></div>
        <nav aria-label="Tools footer">
          <a href="/tools">All tools</a><a href="/guides">Guides</a><a href="/blog">Journal</a><a href="/privacy">Privacy</a><a href="/support">Support</a>
        </nav>
        <small>Tools run locally in your browser. Nothing here files a BAS, issues an invoice in Tammy, or sends business records to a server.</small>
      </footer>
    </div>
  );
}

export function ToolsBreadcrumb({ current }: { readonly current: string }) {
  return <nav aria-label="Breadcrumb" className="tools-breadcrumb"><a href="/">Home</a><span aria-hidden="true">/</span><a href="/tools">Tools</a><span aria-hidden="true">/</span><span aria-current="page">{current}</span></nav>;
}

export function ToolStructuredData({ name, description, path }: { readonly name: string; readonly description: string; readonly path: string }) {
  const data = {
    "@context": "https://schema.org",
    "@type": "WebApplication",
    name, description, url: origin + path,
    applicationCategory: "BusinessApplication",
    operatingSystem: "Web browser",
    isAccessibleForFree: true,
    offers: { "@type": "Offer", price: 0, priceCurrency: "AUD" },
    inLanguage: "en-AU",
    publisher: { "@type": "Organization", name: "Gamma Systems Pty Ltd" },
  };
  return <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(data) }} />;
}
