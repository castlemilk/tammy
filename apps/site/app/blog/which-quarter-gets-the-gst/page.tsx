/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { tammyMdxComponents } from "../../../components/mdx/tammy-components";
import { findBlogPost } from "../../../lib/blog/posts";
import MdxBody from "../../../content/blog/which-quarter-gets-the-gst.mdx";

export const metadata: Metadata = {
  title: "Which BAS quarter gets the GST? Cash vs non-cash",
  description: "An Australian GST timing example: invoice issued in June, paid in July. See which BAS quarter receives the $100 GST under cash and non-cash accounting methods.",
  alternates: { canonical: "/blog/which-quarter-gets-the-gst" },
  keywords: ["cash vs accrual GST Australia","which BAS quarter GST invoice","GST timing cash accounting","GST attribution invoice vs payment"],
  openGraph: {
    type: "article",
    title: "Which BAS quarter gets the GST? Cash vs non-cash",
    description: "An Australian GST timing example: invoice issued in June, paid in July. See which BAS quarter receives the $100 GST under cash and non-cash accounting methods.",
    url: "/blog/which-quarter-gets-the-gst",
  },
};

export default function GstTimingArticle() {
  return <BlogArticle post={findBlogPost("which-quarter-gets-the-gst")}>
    <MdxBody components={tammyMdxComponents} />
  </BlogArticle>;
}
