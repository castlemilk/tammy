/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { ToolsShell, ToolsBreadcrumb, ToolStructuredData } from "../../../components/tools-shell";
import { AbnChecker } from "../../../components/abn-checker";

export const metadata: Metadata = {
  title: "Free ABN checker Australia | Validate ABN format",
  description: "Check an Australian Business Number using the official 11-digit ABN checksum. Free, private browser tool with a link to ABN Lookup for status and GST registration.",
  alternates: { canonical: "/tools/abn-checker" },
  keywords: ["ABN checker", "ABN validation", "check ABN Australia", "ABN number format"],
  openGraph: { type: "website", title: "ABN checker | Tammy", description: "Validate an ABN checksum before checking official registration details.", url: "/tools/abn-checker" },
};

export default function AbnCheckerPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="ABN checker" />
    <header className="tools-page-heading">
      <p className="tools-overline">Australian business identifiers</p>
      <h1>Check the number. Then check the register.</h1>
      <p>Check whether an Australian Business Number has the correct 11-digit format and checksum, without submitting it anywhere.</p>
    </header>
    <AbnChecker />
    <section className="tools-explain" aria-labelledby="abn-check-explain">
      <h2 id="abn-check-explain">What does an ABN check prove?</h2>
      <p>An ABN contains eleven digits. The official format check subtracts one from the first digit, multiplies the result by positional weights, and tests the weighted sum with modulus 89. Tammy uses the same checksum validation as its local invoice model.</p>
      <p>A passing checksum <strong>does not</strong> establish that the ABN is active, belongs to a particular business or is registered for GST. For those facts, use the public <a href="https://abr.business.gov.au/" target="_blank" rel="noopener noreferrer">Australian Business Register’s ABN Lookup</a>. Check important supplier details independently.</p>
      <p>The method is documented by <a href="https://abr.business.gov.au/Help/AbnFormat" target="_blank" rel="noopener noreferrer">ABN Lookup’s format guide</a>. This tool does not query or scrape ABR records.</p>
    </section>
    <aside className="tools-related" aria-label="Related tools">
      <a href="/tools/invoice-generator">Prepare an invoice <span aria-hidden="true">↗</span></a>
      <a href="/guides/australian-tax-invoice-checklist">Read the tax invoice checklist <span aria-hidden="true">↗</span></a>
    </aside>
    <ToolStructuredData name="Australian ABN checksum checker" description="Validate 11-digit Australian Business Number format using the official modulus-89 checksum, locally in the browser." path="/tools/abn-checker" />
  </ToolsShell>;
}
