/* oxlint-disable next/no-html-link-for-pages -- Vinext renders these same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { tammyMdxComponents } from "../../../components/mdx/tammy-components";
import { findBlogPost } from "../../../lib/blog/posts";
import StoryBody from "../../../content/blog/the-invoice-ageing-trap.mdx";

export const metadata: Metadata = {
  title: "Invoice ageing by due date: an Australian business example",
  description: "Age an invoice by its actual due date, not simply its issue date. A worked $7,100 accounts-receivable example with overdue buckets and practical payment steps.",
  alternates: { canonical: "/blog/the-invoice-ageing-trap" },
  keywords: ["invoice ageing report","accounts receivable ageing Australia","days overdue invoices","debtor ageing buckets"],
  openGraph: {
    type: "article",
    title: "Invoice ageing by due date: an Australian business example",
    description: "Age an invoice by its actual due date, not simply its issue date. A worked $7,100 accounts-receivable example with overdue buckets and practical payment steps.",
    url: "/blog/the-invoice-ageing-trap",
  },
};

export default function InvoiceAgeingArticle() {
  return <BlogArticle post={findBlogPost("the-invoice-ageing-trap")}>
    <StoryBody components={tammyMdxComponents} />
  </BlogArticle>;
}
