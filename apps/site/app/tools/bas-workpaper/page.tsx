/* oxlint-disable next/no-html-link-for-pages -- Vinext serves same-origin static routes. */
import type { Metadata } from "next";
import { BasWorkpaper } from "../../../components/bas-workpaper";
import { ToolsBreadcrumb, ToolsShell, ToolStructuredData } from "../../../components/tools-shell";
export const metadata: Metadata = {
  title: "Free Simpler BAS GST workpaper | G1, 1A and 1B",
  description: "Illustrative Australian Simpler BAS workpaper: review G1 total sales, 1A GST on sales, 1B eligible GST credits and standard quarterly dates. No account or lodgement.",
  alternates: { canonical: "/tools/bas-workpaper" },
  keywords: ["BAS calculator Australia", "simpler BAS G1 1A 1B", "BAS workpaper", "GST on sales GST on purchases"],
  openGraph: { type: "website", title: "Simpler BAS GST workpaper | Tammy", url: "/tools/bas-workpaper" },
};
export default function BasWorkpaperPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="Simpler BAS workpaper" />
    <header className="tools-page-heading">
      <p className="tools-overline">Free Australian GST preparation</p>
      <h1>Prepare the figures. Keep the evidence.</h1>
      <p>Explore how G1, 1A and 1B relate for ordinary fully taxable and fully creditable transactions, with whole-dollar example amounts.</p>
    </header>
    <BasWorkpaper />
    <section className="tools-explain">
      <h2>What this calculation covers</h2>
      <p>For eligible businesses using Simpler BAS, G1 is total reportable sales, 1A is GST on sales and 1B is eligible GST credits on purchases. The calculator assumes taxable sale and creditable purchase totals already belong in the selected period under your accounting basis, then extracts standard GST as one-eleventh of their inclusive amounts.</p>
      <p>The ATO tells businesses to enter whole dollar BAS labels without cents and not to round up. Actual GST reporting may require line-by-line rounding, adjustments, different tax treatments or other reporting labels. This workpaper cannot decide which transactions qualify or which attribution period applies.</p>
      <p>Use your original invoices, receipts and ledger. Verify the generated BAS inside the <a href="https://www.ato.gov.au/" target="_blank" rel="noopener noreferrer">ATO&apos;s official online services</a> before lodging. Tammy does not submit it.</p>
    </section>
    <aside className="tools-related" aria-label="Related resources">
      <a href="/blog/how-to-lodge-bas-yourself-australia">How to lodge your own BAS <span aria-hidden="true">↗</span></a>
      <a href="/blog/which-quarter-gets-the-gst">Understand GST attribution <span aria-hidden="true">↗</span></a>
    </aside>
    <ToolStructuredData name="Illustrative Simpler BAS GST workpaper" description="Preview G1, 1A and 1B amounts from already classified GST-inclusive inputs; no ATO lodgement." path="/tools/bas-workpaper" />
  </ToolsShell>;
}
