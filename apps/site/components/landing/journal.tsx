/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
/* oxlint-disable next/no-img-element -- Local SVGs use native images; Vinext does not require Next image optimization. */
/*
 * The Tammy Journal and the guides: a reading room on the sage ground /blog
 * uses. Four stories, each led by the number its article is argued from, set
 * at three scales like the front page of a periodical: one lead, one second
 * story, two briefs. Titles, categories, figures, covers and alt text all come
 * from lib/blog/posts.ts, so this band cannot disagree with the Journal.
 *
 * A server component with no script: the only things that move are a title
 * underline and a cover easing a few pixels under the pointer, and the one
 * guess-first disclosure is a native <details>.
 */
import { Fragment } from "react";
import { blogPath, blogPosts, findBlogPost, type BlogPost } from "../../lib/blog/posts";
import { Arrow } from "./kit";

type Tier = "lead" | "second" | "brief";

/* Reading order is also the order of scale. */
const stories: readonly { slug: string; tier: Tier }[] = [
  { slug: "not-every-bank-deposit-is-sales", tier: "lead" },
  { slug: "the-price-rise-that-didnt-fix-margin", tier: "second" },
  { slug: "the-month-end-review-that-found-220", tier: "brief" },
  { slug: "which-quarter-gets-the-gst", tier: "brief" },
];

/* The guide index keeps its list local to app/guides/page.tsx; these two
 * titles and lines are copied from it word for word. */
const guides = [
  {
    href: "/guides/australian-tax-invoice-checklist",
    title: "Australian tax invoice checklist",
    line: "What a tax invoice needs to contain, when to show GST and how the $1,000 recipient threshold works.",
  },
  {
    href: "/guides/gst-inclusive-vs-exclusive",
    title: "GST inclusive and exclusive: explained",
    line: "Add 10% GST, extract the 1/11 component and understand why some supplies are treated differently.",
  },
] as const;

/* Articles that carry a scenario explorer, a balanced journal entry or a
 * reader checklist (components/mdx): which-quarter-gets-the-gst,
 * not-every-bank-deposit-is-sales, the-month-end-review-that-found-220. */
const INTERACTIVE_ARTICLES = 3;

/* The guess-first prompt under the deposit story. The parts are the article's
 * own table (content/blog/not-every-bank-deposit-is-sales.mdx): a $2,200 cash
 * sale that is $2,000 of taxable sales plus $200 GST, a $5,000 loan and $1,500
 * from the owner. They are drawn to scale under the answer, and the prompt is
 * printed only while they still add up to the figure the post is argued from. */
const GUESS = {
  slug: "not-every-bank-deposit-is-sales",
  /* Whole cents, in the order the answer names them: taxable sales first. */
  parts: [200_000, 20_000, 500_000, 150_000],
  rest: "The rest was GST collected, a loan and owner capital.",
} as const;

const wholeDollars = (cents: number) => "$" + (cents / 100).toLocaleString("en-AU");

const NUMBER_WORDS = [
  "zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
  "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen", "twenty",
] as const;

/** Counts are read from the data and spelled out, so the copy cannot drift from the library. */
function spell(count: number): string {
  return NUMBER_WORDS[count] ?? String(count);
}

function capitalise(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * A post's description as its premise and what follows. The premise is the first
 * sentence; the lead and the briefs print only that (the rest of the lead's would
 * give away its answer), and the second story, which has the room, reads on.
 */
function hook(post: BlogPost): { premise: string; rest: string } {
  const end = post.description.search(/[.?!](?=\s+[A-Z])/);
  if (end === -1) return { premise: post.description, rest: "" };
  return { premise: post.description.slice(0, end + 1), rest: post.description.slice(end + 1).trim() };
}

/** A pause for screen readers between the parts of a story; the layout does the same for the eye. */
function Stop() {
  return <span className="lp-visually-hidden">. </span>;
}

/** The post's figure as display type. "75 → 100" gets a drawn arrow and is read as "75 to 100". */
function Figure({ value }: { value: string }) {
  const [from, to] = value.split(" → ");
  if (from === undefined || to === undefined) return <>{value}</>;
  return (
    <>
      {from}
      <span className="lp-visually-hidden"> to </span>
      <svg className="lp-journal-arrow" viewBox="0 0 28 24" aria-hidden="true" focusable="false">
        <path d="M2 12h23M16.5 3.5 25 12l-8.5 8.5" />
      </svg>
      {to}
    </>
  );
}

/** A title with its hyphenated words kept whole, so "month-end" never breaks across lines. */
function Title({ text }: { text: string }) {
  return (
    <>
      {text.split(" ").map((word, index) => (
        <Fragment key={word + String(index)}>
          {index > 0 ? " " : null}
          {word.includes("-") ? <span className="lp-journal-nowrap">{word}</span> : word}
        </Fragment>
      ))}
    </>
  );
}

function Story({ slug, tier }: { slug: string; tier: Tier }) {
  const post = findBlogPost(slug);
  const { premise, rest } = hook(post);
  return (
    <li className="lp-journal-story" data-tier={tier}>
      <a className="lp-journal-link" href={blogPath(post)}>
        <p className="lp-journal-figure">
          <Figure value={post.figure} />
        </p>
        <p className="lp-journal-figure-label">
          {post.figureLabel}
          <Stop />
        </p>
        <div className="lp-journal-text">
          <p className="lp-label lp-journal-category">
            {post.category}
            <Stop />
          </p>
          <h3 className="lp-h3 lp-journal-title">
            <Title text={post.title} />
          </h3>
          <p className="lp-journal-hook">
            {premise}
            {tier === "second" && rest ? <span className="lp-journal-hook-rest"> {rest}</span> : null}
          </p>
        </div>
        {/* Last in the link so its description closes the link's name; the grid places it. */}
        {post.cover ? (
          <img
            className="lp-journal-cover"
            src={post.cover}
            alt={post.coverAlt ?? ""}
            width={1200}
            height={720}
            loading="lazy"
            decoding="async"
          />
        ) : null}
      </a>
      {slug === GUESS.slug && wholeDollars(GUESS.parts.reduce((sum, part) => sum + part, 0)) === post.figure ? (
        /* Interactive content cannot sit inside the story's link, so the prompt follows it. */
        <details className="lp-journal-guess">
          <summary>
            <svg className="lp-journal-guess-mark" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
              <path d="M12 5v14M5 12h14" />
            </svg>
            How much of {post.figure} was taxable sales?
          </summary>
          <div className="lp-journal-guess-answer">
            <p>
              <strong className="lp-amount">{wholeDollars(GUESS.parts[0])}.</strong> {GUESS.rest}
            </p>
            {/* The same split drawn to scale. Decorative: the sentence says it. */}
            <span className="lp-journal-guess-split" aria-hidden="true">
              {GUESS.parts.map((part) => (
                <i key={part} style={{ flexGrow: part }} />
              ))}
            </span>
          </div>
        </details>
      ) : null}
    </li>
  );
}

export function Journal() {
  const articleCount = spell(blogPosts.length);
  return (
    <section className="lp-section lp-journal" id="journal" data-tone="sage" data-room="reading" aria-labelledby="landing-journal-heading">
      <div className="lp-frame">
        <header className="lp-journal-head">
          <h2 className="lp-h2" id="landing-journal-heading">The Tammy Journal</h2>
          <div className="lp-journal-intro">
            <p className="lp-deck">{capitalise(articleCount)} articles, each argued from a number.</p>
            <p className="lp-small">Worked examples with sources. The figures are hypothetical, not customer records.</p>
            <a className="lp-link" href="/blog">
              Explore the Journal
              <Arrow />
            </a>
          </div>
        </header>

        <ul className="lp-journal-stories">
          {stories.map((story) => (
            <Story key={story.slug} {...story} />
          ))}
        </ul>

        <div className="lp-journal-shelf">
          <div className="lp-journal-shelf-lead">
            <h3 className="lp-h3 lp-journal-shelf-title">Guides</h3>
            <p className="lp-small lp-journal-counts">
              {capitalise(articleCount)} articles, {spell(guides.length)} guides. {capitalise(spell(INTERACTIVE_ARTICLES))} articles have interactive pieces.
            </p>
            <p className="lp-journal-more">
              <a className="lp-link" href="/guides">
                All guides
                <Arrow />
              </a>
              <a className="lp-link" href="/rss.xml">Journal RSS</a>
            </p>
          </div>
          <ul className="lp-journal-guides">
            {guides.map((guide) => (
              <li key={guide.href}>
                <a className="lp-journal-guide" href={guide.href}>
                  <span className="lp-journal-guide-title">{guide.title}</span>
                  <Stop />
                  <span className="lp-journal-guide-line">{guide.line}</span>
                </a>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  );
}
