/* oxlint-disable next/no-html-link-for-pages -- Vinext serves same-origin static routes. */
import type { Metadata } from "next";
import { BlogArticle } from "../../../components/blog-article";
import { findBlogPost } from "../../../lib/blog/posts";
import { tammyMdxComponents } from "../../../components/mdx/tammy-components";
import MdxBody from "../../../content/blog/how-to-lodge-bas-yourself-australia.mdx";
export const metadata: Metadata = {
  title: "How to lodge your own BAS in Australia: a practical guide",
  description: "Step-by-step self-lodgement BAS preparation: find your ATO activity statement, review G1, 1A and 1B, lodge online and retain the acknowledgement.",
  alternates: { canonical: "/blog/how-to-lodge-bas-yourself-australia" },
  keywords: ["lodge BAS yourself Australia","how to lodge BAS online","simpler BAS self lodgement","G1 1A 1B how to prepare"],
  openGraph: { type: "article", title: "How to lodge your own BAS in Australia: a practical guide", url: "/blog/how-to-lodge-bas-yourself-australia" },
};
export default function SelfLodgeBasArticle() {
  return <BlogArticle post={findBlogPost("how-to-lodge-bas-yourself-australia")}>
    <MdxBody components={tammyMdxComponents} />
  </BlogArticle>;
}
