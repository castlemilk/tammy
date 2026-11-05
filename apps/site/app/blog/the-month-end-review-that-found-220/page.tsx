/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { tammyMdxComponents } from "../../../components/mdx/tammy-components";
import { findBlogPost } from "../../../lib/blog/posts";
import MdxBody from "../../../content/blog/the-month-end-review-that-found-220.mdx";

export const metadata: Metadata = {
  title: "A month-end bank reconciliation that found a $220 duplicate",
  description: "A practical month-end review: statement $15,000, ledger $14,780, a duplicated supplier payment and an interactive bookkeeping checklist.",
  alternates: { canonical: "/blog/the-month-end-review-that-found-220" },
  keywords: ["month end close checklist Australia","bank reconciliation discrepancy example","duplicate payment accounting","monthly bookkeeping checklist"],
  openGraph: {
    type: "article",
    title: "A month-end bank reconciliation that found a $220 duplicate",
    description: "A practical month-end review: statement $15,000, ledger $14,780, a duplicated supplier payment and an interactive bookkeeping checklist.",
    url: "/blog/the-month-end-review-that-found-220",
  },
};

export default function MonthEndArticle() {
  return <BlogArticle post={findBlogPost("the-month-end-review-that-found-220")}>
    <MdxBody components={tammyMdxComponents} />
  </BlogArticle>;
}
