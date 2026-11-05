/* oxlint-disable next/no-html-link-for-pages -- Vinext renders these same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { tammyMdxComponents } from "../../../components/mdx/tammy-components";
import { findBlogPost } from "../../../lib/blog/posts";
import StoryBody from "../../../content/blog/when-profit-and-cash-part-ways.mdx";

export const metadata: Metadata = {
  title: "Why profit and cash differ: a worked small business example",
  description: "See why $8,000 operating profit and $5,000 net cash movement can both be correct. A clear bridge between receivables, payables, expenses and the bank.",
  alternates: { canonical: "/blog/when-profit-and-cash-part-ways" },
  keywords: ["profit versus cash flow","accrual accounting cash flow","accounts receivable cash difference","profit to cash reconciliation"],
  openGraph: {
    type: "article",
    title: "Why profit and cash differ: a worked small business example",
    description: "See why $8,000 operating profit and $5,000 net cash movement can both be correct. A clear bridge between receivables, payables, expenses and the bank.",
    url: "/blog/when-profit-and-cash-part-ways",
  },
};

export default function ProfitCashArticle() {
  return <BlogArticle post={findBlogPost("when-profit-and-cash-part-ways")}>
    <StoryBody components={tammyMdxComponents} />
  </BlogArticle>;
}
