/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
/* oxlint-disable next/no-img-element -- Original local editorial vector illustrations. */
import type { ReactNode } from "react";
import { ToolsShell, origin } from "./tools-shell";
import type { BlogPost } from "../lib/blog/posts";
import { blogPath } from "../lib/blog/posts";

export function BlogArticle({ post, children }: {
  readonly post: BlogPost;
  readonly children: ReactNode;
}) {
  const data = {
    "@context": "https://schema.org",
    "@type": "BlogPosting",
    headline: post.title,
    description: post.description,
    datePublished: post.publishedAt,
    dateModified: post.publishedAt,
    mainEntityOfPage: origin + blogPath(post),
    inLanguage: "en-AU",
    articleSection: post.category,
    author: { "@type": "Organization", name: "Gamma Systems Pty Ltd" },
    publisher: { "@type": "Organization", name: "Gamma Systems Pty Ltd" },
  };

  return <ToolsShell section="journal">
    <nav className="tools-breadcrumb" aria-label="Breadcrumb">
      <a href="/">Home</a><span aria-hidden="true">/</span>
      <a href="/blog">Journal</a><span aria-hidden="true">/</span>
      <span aria-current="page">{post.category}</span>
    </nav>
    <article className="blog-story-article">
      <header className="blog-story-heading">
        <div className="blog-story-headline">
          <p className="blog-issue-meta"><span>{post.category}</span><time dateTime={post.publishedAt}>{post.publishedLabel}</time></p>
          <h1>{post.title}</h1>
          <p className="blog-story-description">{post.description}</p>
        </div>
        <aside className={post.cover ? "blog-story-figure blog-story-figure-illustrated" : "blog-story-figure"} aria-label="Article at a glance">
          {post.cover && <img className="blog-story-illustration" src={post.cover} alt={post.coverAlt ?? ""} width="1200" height="720" loading="eager" />}
          <div className="blog-story-figure-content">
            <span>At a glance</span><strong>{post.figure}</strong>
            <p>{post.figureLabel}</p><small>{post.figureFoot}</small>
          </div>
        </aside>
      </header>
      <div className="blog-story-body">
        <nav className="blog-story-toc" aria-label="In this article">
          <strong>In this article</strong>
          {post.toc.map((entry) => <a key={entry.id} href={"#" + entry.id}>{entry.label}</a>)}
        </nav>
        <div className="blog-story-copy">{children}</div>
      </div>
    </article>
    <section className="blog-end-note" aria-label="More Tammy resources">
      <div><h2>Guides and tools</h2><p>Browse the calculators, business guides and other Journal articles.</p></div>
      <nav aria-label="More resources">
        <a href="/tools">Explore tools</a><a href="/blog">More journal entries</a><a href="/guides">Practical guides</a>
      </nav>
    </section>
    <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(data) }} />
  </ToolsShell>;
}
