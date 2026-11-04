/* oxlint-disable next/no-html-link-for-pages -- Vinext renders these same-origin routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { tammyMdxComponents } from "../../../components/mdx/tammy-components";
import { findBlogPost } from "../../../lib/blog/posts";
import StoryBody from "../../../content/blog/the-price-rise-that-didnt-fix-margin.mdx";

export const metadata: Metadata = {
  title: "Pricing sensitivity: how a $10 unit cost increase moves break-even",
  description: "A real calculation behind cost-plus pricing. See why $10 more variable cost moves break-even from 75 to 100 sales, and when a price increase helps.",
  alternates: { canonical: "/blog/the-price-rise-that-didnt-fix-margin" },
  keywords: ["break even price increase","contribution margin calculator","variable costs small business","cost change pricing"],
  openGraph: {
    type: "article",
    title: "Pricing sensitivity: how a $10 unit cost increase moves break-even",
    description: "A real calculation behind cost-plus pricing. See why $10 more variable cost moves break-even from 75 to 100 sales, and when a price increase helps.",
    url: "/blog/the-price-rise-that-didnt-fix-margin",
  },
};

export default function PriceSensitivityArticle() {
  return <BlogArticle post={findBlogPost("the-price-rise-that-didnt-fix-margin")}>
    <StoryBody components={tammyMdxComponents} />
  </BlogArticle>;
}
