# Tammy Journal: MDX editorial system

## Where it came from

This system adapts the MDX registry and editorial figure patterns in
[benebsworth.com](https://github.com/castlemilk/benebsworth.com), specifically
`.claude/skills/writing-blog-posts/SKILL.md`,
`components/mdx/editorial-components.tsx`,
`components/mdx/mdx-components.tsx` and the blog post render path. It is
reimplemented for Tammy's Vinext/Vite deployment rather than copying that
site's dark palette, science demos, content loader or static-export assumptions.

## Real MDX integration

- Compiler: `@mdx-js/rollup@3.1.1` with GFM, registered as a pre Vite plugin.
- Source files: `content/blog/<slug>.mdx` contain Markdown prose and registered JSX
  blocks; no imports, eval, dynamic module loading or raw HTML from users.
- Route: `app/blog/<slug>/page.tsx` is explicit and supplies the page metadata.
- Rendering: a statically imported MDX component is passed the
  `tammyMdxComponents` registry inside `BlogArticle`; no remote content
  execution or runtime compilation.
- Components: `components/mdx/tammy-components.tsx`. CSS:
  `app/blog/mdx.css`. Vite plugin also runs in Vitest so tests validate the
  actual compiled MDX, not a JSX mock.
- Metadata: `lib/blog/posts.ts`. Stable TOC anchors in MDX use semantic
  `<h2 id="...">` elements.
- Discoverability: a new post is indexed by `blogPosts`, `rss.xml`,
  `sitemap.xml`, `llms.txt`, plus links from the homepage where relevant.

## Components and semantics

- `Callout`: note, caution or methodology, with a real title and prose.
- `WorkedExample`: a hypothetical example with an explicit notice.
- `Formula`: labelled equation and result.
- `MoneyTable`: validated column counts, `caption`, column headers, row headers,
  accessible horizontal table scrolling on small screens.
- `BarChart`: accessible figure with numeric labels in HTML. The visual bars
  are only decoration; the underlying data is separately present in a semantic
  table. Rejects NaN, negative values and excessive series. No animation
  of financial numbers and no chart library dependency.
- `Figure`: committed local SVG, alt text and a caption. No arbitrary remote
  image embedding.
- `SourceNote`: attributed HTTPS primary source.
- `ToolLink`: real same-origin Tammy calculators only.

No automatic ATO lodgement, bank feed, remote backup, AI bookkeeping or public
Mac download is claimed. All scenarios describe their assumptions and GST basis.

## Editor skill

`.claude/skills/tammy-blog/SKILL.md` exposes `/tammy-blog <topic>` with
research, accuracy, plain Australian English and anti-template design gates.
Its reference files include the editorial voice and component catalogue.

## Quality checks

```sh
pnpm --dir apps/site editorial:lint
pnpm --dir apps/site typecheck
pnpm --dir apps/site lint
pnpm --dir apps/site test
pnpm --dir apps/site seo:check
pnpm --dir apps/site build
```

Marketing CI runs these after `pnpm install --frozen-lockfile`. The Editorial
MDX linter verifies source and URL inventory, 350+ words, unique TOC anchors,
primary sources, an auditable numerical block, a working tool link, hypothetical
example labelling, local assets and selected de-slop patterns. It deliberately
does not claim to replace human factual research or browser visual inspection.

## Publisher and canonical URLs

`tammy.tax` is the intended production domain but the existing Sites hostname
remains canonical until custom-domain routing, HTTPS and publication are
confirmed. Passing CI is not proof that the current live website changed.

## Three domain-focused editorial instruments (November continuation)

- `ScenarioExplorer`: lightweight, client-side switch between 2–4 fully described accounting cases. Built with semantic fieldsets, pressed buttons, visible totals and an accessible live detail region. State stays in the browser tab.
- `JournalEntry`: SSR double-entry example that validates non-negative integer cents, balanced credits and debits, and displays an accessible caption, row headers and tabular AUD figures. Never silently accepts an imbalanced teaching entry.
- `ReviewChecklist`: ephemeral reader-controlled checks, a live completed count and a working reset button. It cannot save, verify, reconcile or submit actual books.

New sources: `which-quarter-gets-the-gst.mdx` (cash/non-cash GST timing); `not-every-bank-deposit-is-sales.mdx` (bank receipt classification); `the-month-end-review-that-found-220.mdx` (ledger duplicate and monthly review). Matching article routes, original SVG illustrations, source citations, canonical URLs, sitemap/RSS/llms entries and tests accompany them.

An optional raster reference pack contains three generated text-free editorial illustrations with PNG/WebP/mobile variants. The website continues to use locally committed responsive SVG artwork; do not infer that the raster images were deployed.

The approved site claim and publication boundary remain unchanged.
