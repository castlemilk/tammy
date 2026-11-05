/* oxlint-disable next/no-html-link-for-pages -- Vinext serves same-origin static routes. */
import type { Metadata } from "next";
import { ExpenseOrganiser } from "../../../components/expense-organiser";
import { ToolsBreadcrumb, ToolsShell, ToolStructuredData } from "../../../components/tools-shell";

export const metadata: Metadata = {
  title: "Free business receipt and expense organiser Australia",
  description: "Organise Australian small-business expenses and source receipts privately in your browser. Review categories, estimated business-use allocations and export CSV or JSON without an account.",
  alternates: { canonical: "/tools/expense-organiser" },
  keywords: [
    "business expense tracker Australia", "receipt organiser free", "small business expenses spreadsheet",
    "business expense record template", "receipt log CSV",
  ],
  openGraph: {
    type: "website",
    title: "Free expense and receipt organiser | Tammy",
    url: "/tools/expense-organiser",
  },
};

export default function ExpenseOrganiserPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="Expense organiser" />
    <header className="tools-page-heading">
      <p className="tools-overline">Free local-only business utility</p>
      <h1>Keep a clear trail of what you spent.</h1>
      <p>Build a manual expense register, compare estimated business-use amounts and export the records without signing in or uploading your receipts.</p>
    </header>
    <ExpenseOrganiser />
    <section className="tools-explain" aria-labelledby="expenses-explained">
      <h2 id="expenses-explained">A receipt is evidence, not a tax decision.</h2>
      <p>Record the amount actually paid, the supplier, date and what was purchased. Categorise it for your own review, then keep the original receipt with the exported register. A GST amount shown on a receipt does not automatically establish eligibility for a GST credit, and a business-use percentage alone does not determine a deduction.</p>
      <p>This tool does not connect to your bank, OCR documents, post accounting entries or send information to Tammy. Files are held only in the current browser session. The JSON worksheet is portable but not encrypted; it does not embed receipt PDFs or images.</p>
      <p>For Australian source-document requirements, consult the <a target="_blank" rel="noopener noreferrer" href="https://www.ato.gov.au/businesses-and-organisations/preparing-lodging-and-paying/record-keeping-for-business">ATO business record-keeping guidance</a> and <a href="/blog/business-receipts-five-year-record-keeping">Tammy's record-keeping guide</a>.</p>
    </section>
    <aside className="tools-related" aria-label="Related resources">
      <a href="/blog/business-receipts-five-year-record-keeping">Why keeping source records matters <span aria-hidden="true">↗</span></a>
      <a href="/tools/bas-workpaper">Explore a BAS GST workpaper <span aria-hidden="true">↗</span></a>
      <a href="/tools/cash-flow-forecast">Build a cash flow forecast <span aria-hidden="true">↗</span></a>
    </aside>
    <ToolStructuredData
      name="Expense and receipt organiser"
      description="A private, manual expense-record worksheet with CSV/JSON export and no sign-in or data upload."
      path="/tools/expense-organiser"
    />
  </ToolsShell>;
}
