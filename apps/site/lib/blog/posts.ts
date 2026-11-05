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
}

export const blogPosts: readonly BlogPost[] = [
  {
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
