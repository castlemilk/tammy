/* oxlint-disable next/no-html-link-for-pages -- Vinext serves same-origin static routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { findBlogPost } from "../../../lib/blog/posts";
import { tammyMdxComponents } from "../../../components/mdx/tammy-components";
import MdxBody from "../../../content/blog/quarterly-bas-due-dates-australia.mdx";
export const metadata: Metadata = {
  title: "Quarterly BAS due dates in Australia: reference calendar",
  description: "Understand standard BAS due dates for July–September, October–December, January–March and April–June, including conditional extensions and business-day rules.",
  alternates: { canonical: "/blog/quarterly-bas-due-dates-australia" },
  keywords: ["quarterly BAS due dates Australia","BAS due date 2026 2027","BAS lodgement dates self lodge","quarter 2 BAS February"],
  openGraph: { type: "article", title: "Quarterly BAS due dates in Australia: reference calendar", url: "/blog/quarterly-bas-due-dates-australia" },
};
export default function QuarterBasDatesArticle() {
  return <BlogArticle post={findBlogPost("quarterly-bas-due-dates-australia")}>
    <MdxBody components={tammyMdxComponents} />
  </BlogArticle>;
}
