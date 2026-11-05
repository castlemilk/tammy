/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { tammyMdxComponents } from "../../../components/mdx/tammy-components";
import { findBlogPost } from "../../../lib/blog/posts";
import MdxBody from "../../../content/blog/not-every-bank-deposit-is-sales.mdx";

export const metadata: Metadata = {
  title: "Not every bank deposit is sales revenue",
  description: "A worked $8,700 bank deposit example distinguishes taxable sales, a business loan and owner capital, with a balanced journal and source-document checklist.",
  alternates: { canonical: "/blog/not-every-bank-deposit-is-sales" },
  keywords: ["bank deposits not sales revenue","classify business bank deposits","cash sales business loans owner capital","business bookkeeping deposits"],
  openGraph: {
    type: "article",
    title: "Not every bank deposit is sales revenue",
    description: "A worked $8,700 bank deposit example distinguishes taxable sales, a business loan and owner capital, with a balanced journal and source-document checklist.",
    url: "/blog/not-every-bank-deposit-is-sales",
  },
};

export default function BankDepositArticle() {
  return <BlogArticle post={findBlogPost("not-every-bank-deposit-is-sales")}>
    <MdxBody components={tammyMdxComponents} />
  </BlogArticle>;
}
