/* oxlint-disable next/no-html-link-for-pages -- Vinext serves these static same-origin routes. */
import type { Metadata } from "next";
import { ToolStructuredData, ToolsBreadcrumb, ToolsShell } from "../../../components/tools-shell";
import { InvoiceDueDateCalculator } from "../../../components/invoice-due-date-calculator";

export const metadata: Metadata = {
  title: "Invoice due-date calculator Australia",
  description: "Calculate an invoice payment due date using 7, 14, 30, 45 or 60 calendar-day terms. Accurate across month ends and leap years.",
  alternates: { canonical: "/tools/invoice-due-date-calculator" },
  keywords: ["invoice due date calculator", "net 30 calculator", "invoice payment terms Australia", "14 day invoice calculator"],
  openGraph: { title: "Invoice due-date calculator | Tammy", description: "Calculate the due date from an invoice date and agreed calendar-day terms.", url: "/tools/invoice-due-date-calculator", type: "website" },
};
export default function InvoiceDueDateCalculatorPage() {
  return <ToolsShell>
    <ToolsBreadcrumb current="Invoice due-date calculator" />
    <header className="tools-page-heading"><p className="tools-overline">Free business tool</p><h1>Know when payment is due.</h1><p>Choose an invoice date and the payment period you agreed with your customer. Get the exact date, even across month ends and leap years.</p></header>
    <InvoiceDueDateCalculator />
    <section className="tools-explain"><h2>Calendar days or business days?</h2><p>This calculator adds calendar days, starting after the invoice date. An invoice dated 1 November with 14 calendar-day terms is due on 15 November. It does not adjust for weekends or public holidays, and it does not imply that all invoices have the same legal payment terms.</p><p>Check the contract or payment terms agreed with your customer, especially where payment is due at month end or after receiving an invoice rather than on its issue date.</p></section>
    <aside className="tools-related" aria-label="Related tools"><a href="/tools/invoice-generator">Make an invoice <span aria-hidden="true">↗</span></a><a href="/tools/gst-calculator">Calculate GST <span aria-hidden="true">↗</span></a></aside>
    <ToolStructuredData name="Invoice due-date calculator" description="Calculate a due date by adding calendar days to an invoice issue date." path="/tools/invoice-due-date-calculator" />
  </ToolsShell>;
}
