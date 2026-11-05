/* oxlint-disable next/no-html-link-for-pages -- Vinext serves same-origin static routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { findBlogPost } from "../../../lib/blog/posts";
import { tammyMdxComponents } from "../../../components/mdx/tammy-components";
import MdxBody from "../../../content/blog/lodge-sole-trader-tax-return-mytax.mdx";
export const metadata: Metadata = {
  title: "Lodge your sole trader tax return yourself using myTax",
  description: "Prepare Australian sole trader business income and expense records, find the myTax business items, review the individual tax return and lodge through the ATO.",
  alternates: { canonical: "/blog/lodge-sole-trader-tax-return-mytax" },
  keywords: ["sole trader lodge own tax return","myTax sole trader income loss","self lodge Australian business tax return"],
  openGraph: { type: "article", title: "Lodge your sole trader tax return yourself using myTax", url: "/blog/lodge-sole-trader-tax-return-mytax" },
};
export default function MyTaxSoleTraderArticle() {
  return <BlogArticle post={findBlogPost("lodge-sole-trader-tax-return-mytax")}>
    <MdxBody components={tammyMdxComponents} />
  </BlogArticle>;
}
