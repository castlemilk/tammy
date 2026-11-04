/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { Metadata } from "next";
import { ToolsShell, origin } from "../../components/tools-shell";
import { blogPath, blogPosts } from "../../lib/blog/posts";

export const metadata: Metadata = {
  title: "Tammy Journal | Australian accounting, invoicing and cash flow",
  description: "Thoughtful Australian small-business articles on margins, invoice payment terms and cash-flow forecasting. Practical examples and free tools, without the jargon.",
  alternates: { canonical: "/blog" },
  openGraph: {
    type: "website", title: "The Tammy Journal",
    description: "Pricing, cash flow and invoicing in plain language.",
    url: "/blog",
  },
};
const lead = blogPosts[0];
const others = blogPosts.slice(1);

export default function BlogPage() {
  return <ToolsShell>
    <nav className="tools-breadcrumb" aria-label="Breadcrumb">
      <a href="/">Home</a><span aria-hidden="true">/</span><span aria-current="page">Journal</span>
    </nav>
    <header className="blog-index-intro">
      <div><p className="blog-volume">Tammy Journal</p><h1>Keeping the books, <span>thinking ahead.</span></h1></div>
      <p>Short, researched notes on the numbers Australian small businesses use every day.</p>
    </header>
    <section className="blog-feature" aria-labelledby="blog-feature-title">
      <div className="blog-feature-copy">
        <p className="blog-issue-meta"><span>{lead.category}</span><time dateTime={lead.publishedAt}>{lead.publishedLabel}</time></p>
        <h2 id="blog-feature-title"><a href={blogPath(lead)}>{lead.title}</a></h2>
        <p>{lead.description}</p>
        <a className="blog-story-link" href={blogPath(lead)}>Read the article <span aria-hidden="true">↗</span></a>
      </div>
      <div className="blog-feature-figure" aria-label="Illustrative pricing calculation">
        <div className="blog-feature-ledger">
          <span>In the example</span>
          <div><span>Cost</span><strong>$80</strong></div>
          <div><span>Sale</span><strong>$120</strong></div>
          <div><span>Gross profit</span><strong>$40</strong></div>
          <div className="blog-feature-summary"><span>Margin</span><strong>33.33%</strong></div>
        </div>
      </div>
    </section>
    <section className="blog-archive" aria-labelledby="blog-latest">
      <div className="blog-archive-heading"><h2 id="blog-latest">From the journal</h2><p>Invoicing and cash flow, with worked examples.</p></div>
      <div className="blog-archive-list">
        {others.map((post) => <a key={post.slug} href={blogPath(post)} className="blog-archive-item">
          <div><span className="blog-archive-category">{post.category}</span><time dateTime={post.publishedAt}>{post.publishedLabel}</time></div>
          <div><h3>{post.title}</h3><p>{post.description}</p></div>
          <span className="blog-archive-symbol" aria-hidden="true">↗</span>
        </a>)}
      </div>
    </section>
    <aside className="blog-index-footer" aria-label="Related resources">
      <div><h2>Numbers need context.</h2><p>Read the practical tax and GST guides, or work through a calculation yourself.</p></div>
      <div><a href="/guides">Australian business guides</a><a href="/tools">Free calculators and invoices</a></div>
    </aside>
    <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify({
      "@context": "https://schema.org", "@type": "Blog", name: "Tammy Journal",
      url: origin + "/blog", inLanguage: "en-AU",
      blogPost: blogPosts.map((post) => ({ "@type": "BlogPosting", headline: post.title, url: origin + blogPath(post), datePublished: post.publishedAt })),
    }) }} />
  </ToolsShell>;
}
