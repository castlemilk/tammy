/* oxlint-disable next/no-html-link-for-pages -- Vinext serves same-origin static routes. */
import type { Metadata } from "next";
import { TaxReturnChecklist } from "../../../components/tax-return-checklist";
import { ToolsBreadcrumb, ToolsShell, ToolStructuredData } from "../../../components/tools-shell";
export const metadata: Metadata = {
  title: "Free sole trader tax return preparation checklist Australia",
  description: "Prepare your sole trader income tax records before lodging with ATO myTax. Printable self-lodgement checklist for income, expenses, GST, PAYG and supporting evidence.",
  alternates: { canonical: "/tools/sole-trader-tax-checklist" },
  keywords: ["sole trader tax return checklist", "myTax sole trader prepare", "Australian sole trader income tax", "self lodge tax return"],
  openGraph: { type: "website", title: "Sole trader tax preparation | Tammy", url: "/tools/sole-trader-tax-checklist" },
};
export default function TaxReturnChecklistPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="Sole trader tax checklist" />
    <header className="tools-page-heading"><p className="tools-overline">Free tax preparation resource</p>
      <h1>Organise the tax return before you open myTax.</h1>
      <p>Work through the business records and review steps for an Australian sole trader&apos;s individual income-tax return.</p>
    </header>
    <TaxReturnChecklist />
    <section className="tools-explain">
      <h2>Sole traders use an individual tax return</h2>
      <p>An Australian sole trader generally declares business income and expenses in their individual return rather than a separate business tax return. A company, partnership or trust may have different return requirements.</p>
      <p>In myTax, select the business or sole-trader income or loss option where applicable, review income and expenses, and confirm the complete return. Tammy&apos;s checklist does not estimate personal tax, assess deductions, transmit records or lodge a return.</p>
      <p>See <a href="https://www.ato.gov.au/individuals-and-families" target="_blank" rel="noopener noreferrer">ATO individual tax guidance</a> and the <a href="/blog/lodge-sole-trader-tax-return-mytax">step-by-step sole trader guide</a>.</p>
    </section>
    <aside className="tools-related" aria-label="Related resources">
      <a href="/blog/lodge-sole-trader-tax-return-mytax">Self-lodgement walkthrough <span aria-hidden="true">↗</span></a>
      <a href="/tools/bas-workpaper">Prepare a BAS workpaper <span aria-hidden="true">↗</span></a>
    </aside>
    <ToolStructuredData name="Sole trader tax return preparation checklist" description="Private browser checklist to organise business income, receipts, deductions, GST/PAYG checks and review before ATO myTax self-lodgement." path="/tools/sole-trader-tax-checklist" />
  </ToolsShell>;
}
