---
name: tammy-blog
description: Author, edit, fact-check and publish Tammy Journal articles using Soft Precision MDX components, original editorial visuals, human prose, source-backed Australian accounting guidance and automated de-slop checks. Trigger on /tammy-blog <topic>, "write a Tammy article", "add a Tammy Journal post", or work on the editorial library.
argument-hint: "<topic> [--angle <specific question>] [--type explainer|worked-example|field-guide]"
---

# Tammy Journal editorial skill

**Purpose.** Turn one genuine accounting question into a useful, well-researched article that can be read without a calculator and understood with one. A post should feel like a careful editor and an experienced Australian bookkeeper reviewed it, not like search-keyword scaffolding.

This skill is scoped to `castlemilk/tammy`, specifically `apps/site`. Consult repository-root `DESIGN.md`, `apps/site/LANDING-PAGE.md` and the current product boundary before changing content. The public website is not the desktop app.

## The user command

`/tammy-blog <topic>` means: select a narrow angle, inspect the existing post inventory, verify live primary sources, draft the MDX, build a true worked example, choose 1–2 blocks from the approved component catalogue, create an original image when it contributes, update discoverability files, run tests and open a draft PR. Respect extra constraints supplied with the topic. Do not imply publication or deployment without a confirmed Sites deployment.

## Research gate: the claim ledger

Before writing, create a private or committed claim ledger with rows: factual claim, source URL, official publication/update date when shown, exact supporting section, applicability or exceptions, review date and confidence. Australian law, ATO rulings, GST treatment and current thresholds need live verification against official ATO, ABR, business.gov.au, legislation or state authority pages. Where a claim lacks evidence, remove it or clearly mark it as hypothetical. Financial figures come from shown assumptions, not invention masquerading as measured market data. Do not guess an individual's actual receipts, tax status or business activity.

Verify relevant definitions and time windows. Distinguish **general rules** from context-dependent eligibility or exceptions. Do not confuse gross with net, profit with cash, ABN checksum with ABR registration, GST-free with input-taxed, BAS workpaper preparation with ATO filing, or a published public calculator with Tammy desktop capabilities.

## Human voice and anti-slop

Read `references/editorial-voice.md` before drafting. Open with the actual situation, not a grand statement about Australian business. Write one testable thesis, 3–5 purposeful sections, and an illustrative example with every number reconciling. Prefer short conversational sentences alternating with longer explanatory passages. Write in Australian English. Translate jargon once, then use the proper term naturally. Let specific evidence carry authority.

Ban opener clichés (`in today's fast-paced world`, `in an ever-evolving landscape`), vague beneficence (`unlock`, `empower`, `game-changing`, `seamlessly`), artificial contrast cadence (`not just X, but Y`), generic concluding recaps and unsupported promises. Do not generate FAQ spam, fake testimonials, invented customer names or invented regulatory assurances. Keep quotations original and cite actual sources for claims.

## MDX publishing contract

- New source: `apps/site/content/blog/<slug>.mdx`. It is a real MDX file compiled by the Vite MDX plugin at build, imported by an explicit Next/Vinext route. **Do not** put `import` or `export` in public-authored MDX bodies. Use registered components instead.
- Route: `apps/site/app/blog/<slug>/page.tsx` with `metadata: Metadata`, a relative canonical, `BlogArticle`, `findBlogPost`, and `<MdxBody components={tammyMdxComponents} />`.
- Index: add a matching entry to `apps/site/lib/blog/posts.ts`, with dated metadata, genuine article summary, figure/example and TOC section IDs. Headings in MDX must have stable explicit IDs or matching generated anchors.
- Assets: original, text-free local SVG under `apps/site/public/assets/`, with `cover` and useful `coverAlt`. Charts and numeric results are **real semantic HTML or SVG components**; never paint numbers into an image.
- Navigation: add meaningful contextual links to existing tools, guides and other articles. Register the article URL in `public/sitemap.xml`, `public/rss.xml` and `public/llms.txt`. Keep metadata and content aligned.
- Source snippets use official links; external citations use `<SourceNote url="..." label="..."/>` and do not rely only on an unspecific footer link.
- Use examples from `references/mdx-component-catalog.md`. Prefer `<Callout>`, `<WorkedExample>`, `<Formula>`, `<MoneyTable>`, `<BarChart>`, `<Figure>`, `<ToolLink>`, and `<SourceNote>` before introducing a new component. For a true accounting distinction that improves by comparison, reuse `<ScenarioExplorer>`. For multi-line debit/credit examples, use `<JournalEntry>` which rejects an unbalanced entry. For practical reviews, `<ReviewChecklist>` is a local-only reader aid, never a claim of automatic verification.

## Design gate (Hallmark, Design Taste, Impeccable)

The approved Soft Precision world wins: warm ivory, forest and sage, ledger-like accounting artefacts, editorial serif headings, humanist sans body, calm motion. Do not copy benebsworth.com's dark technical colours or its science-specific effects. Adapt its MDX registration pattern, figure and component ergonomics, article hierarchy and prose QA.

- Use meaningful callouts only, no badge/eyebrow decoration, fake hero metrics, equal icon cards or gradients on headline text.
- Each new visual answers a concept: an invoice ageing sequence, a profit-to-cash bridge, a contribution margin. Avoid decorative diagrams that can be mistaken for measured data.
- Semantic tables have `<caption>`, scoped header cells and mobile handling. Charts show values in text and make underlying data accessible without reliance on colour.
- All links and inputs have clear keyboard focus; reduced motion never hides information.
- Use the same tokens and font roles as `DESIGN.md`. Do not quietly restyle the desktop app.

## Quality and release checks

1. Read the article aloud and cut filler. Check every table, subtotal, rate, unit and rounding method. Compare claims with the claim ledger.
2. Run `pnpm --dir apps/site editorial:lint`, `typecheck`, `lint`, `test`, `seo:check` and `build`. Do not weaken checks to make a bad article pass.
3. Audit `/blog` on desktop and mobile when browser access is available; inspect MDX component rendering, tables, anchor links and overflow. Inspect source and CI if browser access is unavailable and state that limitation.
4. Open a draft PR with source links, evidence, new assets and exact testing results. No unsupported SEO ranking claims. Never call a build a Sites deployment. Preserve the old canonical until `tammy.tax` is serving the verified version.

A completed article has a unique insight, specific examples, connected components, accurate references and a working reading experience. Quantity of pages is not a quality metric.

## Continued editorial work: a richer block grammar

Do not insert blocks just because they exist. For each article, choose one primary reading device:
- Timing or accounting-method uncertainty: **ScenarioExplorer** with concrete alternative periods, results and explanations.
- Double-entry bookkeeping: **JournalEntry** with cents and an internally balanced debit/credit total.
- A review task readers can actually perform: **ReviewChecklist** whose state is explicitly local and never treated as saved evidence.

Pair with a MoneyTable, Formula or an accessible BarChart when the narrative genuinely benefits. Keep one descriptive chart title and label units and hypothetical assumptions. Never turn a single figure into four duplicate display styles.

For each new article, also test the selected block's semantic labels and every numeric relationship. Prefer reusable domain components over bespoke decorations. Visual art must be concept-specific, text-free, local and subordinate to the accessible figures.

When editing older posts, preserve their source anchors and continue to validate canonical URLs and the RSS inventory. Avoid manual hardcoded post totals in tests; derive expected counts from the curated blog inventory when possible.

## UI composition when adding a blog-related tool

For new Tammy Journal interactive embeds or companion tools, consult
`apps/site/UI-COMPONENT-STANDARD.md`. Start with the site's shadcn-compatible
Tailwind primitives in `components/ui`; use maintained Gamma components via
`components/gamma` and the Soft Precision token bridge for specialised
interactions. Do not invent another font/color system or add one-off button,
checkbox and input CSS in each article. Prefer the existing audited MDX blocks
for numerical and explanatory content. Preserve source/financial verification
and accessibility on every new control.
