/** Public, browser-only Tammy utilities. Curated rather than scraped SEO inventory. */
export type ToolCategory = "invoicing" | "gst" | "pricing" | "cashflow";

export interface PublicTool {
  readonly href: string;
  readonly name: string;
  readonly summary: string;
  readonly category: ToolCategory;
  readonly categoryLabel: string;
  readonly icon: string;
  readonly note: string;
  readonly keywords: string;
  readonly featured?: boolean;
}

export const toolCategories: ReadonlyArray<{ id: ToolCategory | "all"; label: string }> = [
  { id: "all", label: "All tools" },
  { id: "invoicing", label: "Invoicing" },
  { id: "gst", label: "Tax & GST" },
  { id: "pricing", label: "Pricing" },
  { id: "cashflow", label: "Cash flow" },
];

export const publicTools: readonly PublicTool[] = [
  {
    href: "/tools/invoice-generator",
    name: "Invoice generator",
    summary: "Build an Australian invoice with itemised GST and export a PDF to keep.",
    category: "invoicing", categoryLabel: "Invoicing", icon: "invoices",
    note: "PDF export", keywords: "invoice tax ABN document pdf", featured: true,
  },
  {
    href: "/tools/gst-calculator",
    name: "GST calculator",
    summary: "Add GST or extract the 10% component from an inclusive amount.",
    category: "gst", categoryLabel: "Tax & GST", icon: "gst-bas",
    note: "Inclusive and exclusive", keywords: "tax gst inclusive exclusive 10 percent",
  },
  {
    href: "/tools/abn-checker",
    name: "ABN checker",
    summary: "Validate an 11-digit ABN checksum before consulting the official register.",
    category: "gst", categoryLabel: "Tax & GST", icon: "abn-form",
    note: "Format check only", keywords: "ABR australian business number validate",
  },
  {
    href: "/tools/invoice-due-date-calculator",
    name: "Invoice due-date calculator",
    summary: "Turn agreed calendar-day payment terms into an exact invoice due date.",
    category: "invoicing", categoryLabel: "Invoicing", icon: "review",
    note: "Payment terms", keywords: "net 30 14 days payment calendar",
  },
  {
    href: "/tools/profit-margin-calculator",
    name: "Profit margin calculator",
    summary: "Compare gross profit, markup on cost and margin on sales.",
    category: "pricing", categoryLabel: "Pricing", icon: "margin-scale",
    note: "Markup and margin", keywords: "profit gross percentage markup price",
  },
  {
    href: "/tools/break-even-calculator",
    name: "Break-even calculator",
    summary: "Find the number of units needed to cover fixed costs at your price.",
    category: "pricing", categoryLabel: "Pricing", icon: "ledger",
    note: "Units and revenue", keywords: "fixed variable costs units contribution",
  },
  {
    href: "/tools/cash-runway-calculator",
    name: "Cash runway calculator",
    summary: "Estimate months of cash cover at your current monthly net burn.",
    category: "cashflow", categoryLabel: "Cash flow", icon: "reconciliation",
    note: "Cash coverage", keywords: "forecast cash burn liquidity months receipts",
  },
];

export function matchTools(query: string, category: ToolCategory | "all"): PublicTool[] {
  const needle = query.trim().toLocaleLowerCase("en-AU");
  return publicTools.filter((tool) => {
    if (category !== "all" && tool.category !== category) return false;
    if (!needle) return true;
    return [tool.name, tool.summary, tool.categoryLabel, tool.note, tool.keywords]
      .some((text) => text.toLocaleLowerCase("en-AU").includes(needle));
  });
}
