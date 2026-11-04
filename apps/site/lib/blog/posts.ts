/** Curated editorial inventory. No generated author identities or fabricated metrics. */
export interface BlogPost {
  slug: string;
  title: string;
  description: string;
  category: string;
  publishedAt: string;
  publishedLabel: string;
  figure: string;
  figureLabel: string;
  figureFoot: string;
  toc: readonly { id: string; label: string }[];
  cover?: string;
  coverAlt?: string;
}

export const blogPosts: readonly BlogPost[] = [
  {
    slug: "the-price-rise-that-didnt-fix-margin",
    title: "The price rise that didn't fix the margin.",
    description: "One $10 change in unit costs can move your break-even target by a third. A worked contribution example, with the assumptions left visible.",
    category: "Pricing",
    publishedAt: "2026-11-05",
    publishedLabel: "5 November 2026",
    figure: "75 → 100",
    figureLabel: "break-even units when variable cost rises from $80 to $90",
    figureFoot: "Illustrative monthly fixed costs: $3,000; selling price: $120 excluding GST.",
    cover: "/assets/journal-price-sensitivity.svg",
    coverAlt: "Ascending ledger-height columns and a fine brass arc representing changing unit economics",
    toc: [
      { id: "contribution-is-what-counts", label: "One sale's contribution" },
      { id: "cost-change", label: "The ten-dollar difference" },
      { id: "price-and-demand", label: "Price and demand" },
      { id: "run-a-scenario", label: "Run the scenario" },
    ],
  },
  {
    slug: "the-invoice-ageing-trap",
    title: "The ageing report is using the wrong clock.",
    description: "An invoice's age is not the same as its days overdue. Four dates show how to interpret a debtors ageing report without confusing the two.",
    category: "Invoicing",
    publishedAt: "2026-11-05",
    publishedLabel: "5 November 2026",
    figure: "$5,900",
    figureLabel: "overdue balance in an illustrative $7,100 invoice ledger",
    figureFoot: "As at 5 November 2026, using the agreed due dates.",
    cover: "/assets/journal-invoice-ageing.svg",
    coverAlt: "Four layered invoice sheets arranged along a thin brass time axis",
    toc: [
      { id: "which-clock", label: "Which clock?" },
      { id: "four-invoices", label: "Four invoices" },
      { id: "follow-up-with-context", label: "Follow-up with context" },
      { id: "forecast-the-receipt", label: "Forecast the receipt" },
    ],
  },
  {
    slug: "when-profit-and-cash-part-ways",
    title: "When profit and cash part ways.",
    description: "An $8,000 profit can coexist with $5,000 of net cash movement. A precise bridge between invoices, unpaid costs and the bank.",
    category: "Cash flow",
    publishedAt: "2026-11-05",
    publishedLabel: "5 November 2026",
    figure: "$3,000",
    figureLabel: "difference between illustrative profit and net cash movement",
    figureFoot: "Receivables increased $4,000; unpaid expenses increased $1,000.",
    cover: "/assets/journal-profit-to-cash.svg",
    coverAlt: "Separate ledger and bank-record panels joined by three brass reconciliation arcs",
    toc: [
      { id: "two-measures", label: "Two different measures" },
      { id: "the-month-on-paper", label: "A worked month" },
      { id: "the-bridge", label: "The profit-to-cash bridge" },
      { id: "plan-from-both", label: "Plan from both reports" },
    ],
  },

  {
    slug: "when-to-register-for-gst-australia",
    title: "When GST registration becomes necessary.",
    description: "The $75,000 threshold is about turnover, not profit. Learn to compare your current and projected twelve-month GST turnover and what changes after registration.",
    category: "GST",
    publishedAt: "2026-11-05",
    publishedLabel: "5 November 2026",
    figure: "$75,000",
    figureLabel: "standard annual GST turnover threshold for Australian businesses",
    figureFoot: "Different thresholds and exceptions apply to some activities and organisations.",
    cover: "/assets/journal-gst-threshold.svg",
    coverAlt: "Abstract arc gauge indicating a turnover threshold, surrounded by concentric ledger rings",
    toc: [
      { id: "turnover-not-profit", label: "Turnover, not profit" },
      { id: "two-windows", label: "Two twelve-month windows" },
      { id: "deadline-and-exceptions", label: "Registration and exceptions" },
      { id: "after-registering", label: "What changes next" },
    ],
  },
  {
    slug: "business-receipts-five-year-record-keeping",
    title: "Keep the evidence behind the entry.",
    description: "A tidy ledger needs its source documents. What Australian businesses generally keep, why five years is a baseline, and how to protect electronic records.",
    category: "Records",
    publishedAt: "2026-11-05",
    publishedLabel: "5 November 2026",
    figure: "5 years",
    figureLabel: "general minimum retention period for many Australian business records",
    figureFoot: "Some retention periods start later or run longer.",
    cover: "/assets/journal-records-archive.svg",
    coverAlt: "Layered archival receipts and folders with an organised filing pattern",
    toc: [
      { id: "source-evidence", label: "What evidence explains" },
      { id: "keep-these-records", label: "What records to keep" },
      { id: "five-years-and-beyond", label: "Five years and exceptions" },
      { id: "protect-and-recover", label: "Digital integrity and backup" },
    ],
  },
  {
    slug: "simpler-bas-g1-1a-1b-explained",
    title: "Three BAS labels. One clear workpaper.",
    description: "How G1, 1A and 1B fit together under Simpler BAS, illustrated with taxable sales, eligible GST credits and a review checklist.",
    category: "BAS",
    publishedAt: "2026-11-05",
    publishedLabel: "5 November 2026",
    figure: "G1 / 1A / 1B",
    figureLabel: "three GST fields ordinarily reported under Simpler BAS",
    figureFoot: "Other BAS obligations may still require additional reporting fields.",
    cover: "/assets/journal-bas-workpaper.svg",
    coverAlt: "Three neat columns of an accounting workpaper connected by a balance line",
    toc: [
      { id: "the-three-labels", label: "What the labels mean" },
      { id: "worked-period", label: "A small worked period" },
      { id: "claim-only-eligible", label: "Check credit eligibility" },
      { id: "draft-before-lodgement", label: "Review before lodging" },
    ],
  },
  {
    slug: "bank-statement-reconciliation-steps",
    title: "Make the bank and ledger agree.",
    description: "A practical bank-statement reconciliation method: compare balances, trace transactions, explain timing differences and retain evidence.",
    category: "Bookkeeping",
    publishedAt: "2026-11-05",
    publishedLabel: "5 November 2026",
    figure: "$40",
    figureLabel: "illustrative unrecorded bank fee found during reconciliation",
    figureFoot: "A difference to investigate, not an amount to force into balance.",
    cover: "/assets/journal-bank-reconciliation.svg",
    coverAlt: "Two accounting registers connected by fine matching arcs and a reconciliation marker",
    toc: [
      { id: "same-period", label: "Compare the same period" },
      { id: "trace-the-rows", label: "Trace the transactions" },
      { id: "resolve-differences", label: "Explain differences" },
      { id: "save-the-trail", label: "Retain the evidence" },
    ],
  },  {
    slug: "margin-is-not-markup",
    title: "Margin is not markup.",
    description: "The two percentages describe the same profit from different sides of a sale. One quiet mix-up can make a pricing plan look healthier than it is.",
    category: "Pricing",
    publishedAt: "2026-11-04",
    publishedLabel: "4 November 2026",
    figure: "33.33%",
    figureLabel: "margin on a $120 sale with $80 direct cost",
    figureFoot: "The same sale has 50% markup on cost.",
    toc: [
      { id: "two-denominators", label: "Two denominators" },
      { id: "working-example", label: "A worked example" },
      { id: "price-decisions", label: "Where pricing goes wrong" },
      { id: "use-the-numbers", label: "Try the calculation" },
    ],
  },
  {
    slug: "an-invoice-is-not-cash",
    title: "An invoice is not cash.",
    description: "The due date is a promise about timing, not money already in the bank. A practical way to think about terms, follow-up and your records.",
    category: "Invoicing",
    publishedAt: "2026-11-04",
    publishedLabel: "4 November 2026",
    figure: "30 days",
    figureLabel: "an example payment term, not an expected payment date",
    figureFoot: "Cash arrives when the customer actually pays.",
    toc: [
      { id: "three-dates", label: "Three different dates" },
      { id: "payment-terms", label: "Terms should be explicit" },
      { id: "keep-the-evidence", label: "Record the real event" },
      { id: "next-invoice", label: "For the next invoice" },
    ],
  },
  {
    slug: "the-12-week-cash-flow-view",
    title: "See the next twelve weeks.",
    description: "A monthly runway figure is a useful signal. A dated weekly cash-flow view shows the pinch points hiding inside that average.",
    category: "Cash flow",
    publishedAt: "2026-11-04",
    publishedLabel: "4 November 2026",
    figure: "12 weeks",
    figureLabel: "a rolling window for expected cash in and out",
    figureFoot: "Timing matters more than a smooth monthly average.",
    toc: [
      { id: "start-with-balance", label: "Start with what is in the bank" },
      { id: "when-not-just-how-much", label: "When, not just how much" },
      { id: "a-simple-weekly-table", label: "Build a weekly view" },
      { id: "check-the-forecast", label: "Check estimates against actuals" },
    ],
  },


] as const;

export function findBlogPost(slug: string): BlogPost {
  const post = blogPosts.find((item) => item.slug === slug);
  if (!post) throw new Error("Unknown editorial slug: " + slug);
  return post;
}

export function blogPath(post: BlogPost): string {
  return "/blog/" + post.slug;
}
