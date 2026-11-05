# Tammy SEO strategy: Australian business tasks, not a keyword farm

**Snapshot:** 5 November 2026  
**Status:** Proposed. No verified Search Console baseline or proven tammy.tax publication in this roadmap.  
**Related:** [Growth roadmap](./free-tools-funnel-roadmap-2026-11-05.md), [intent/content map](./seo-content-map-2026-11-05.csv), [SEO epic #36](https://github.com/castlemilk/tammy/issues/36)

## 1. Positioning and search thesis

Position Tammy as the **Australian small-business toolbox you can use immediately**, with an optional path into **local-first, reliable accounting** when standalone work becomes a recurring ledger. Our first audience is sole traders and small non-employer service firms operating in AUD. Do not target all payroll, tax-agent and multinational needs before Tammy supports them.

Organic search is an acquisition channel, **not** the product. Every indexable tool page must have functional software and an explanation that remains useful to a person who arrived directly. Every Journal page needs one specific factual question, original worked examples, source-backed Australian rules and a matching tool or next action.

Pages containing financial guidance qualify for special care: primary sources, accurate GST definitions, dated review, visible source/author transparency and no pretence that the publisher is a registered adviser or ATO-approved lodgement provider.

### What is already built in source

- Public tools: invoice PDF, GST calculator, ABN checksum, invoice due dates, margin, break-even and cash runway.
- Journal: thirteen curated source-based articles across draft PRs #16–#20, including three MDX scenario stories, source-linked blocks and editorial lint.
- A semantic Tools index, titles/canonical metadata, sitemap, RSS, llms.txt, Article/Software-type structured data, and a repository SEO consistency check.
- **Not verified in production:** these PRs are stacked drafts, the live Sites project may be older, tammy.tax has not been verified serving the intended pages, and Search Console metrics have not been retrieved. All proposed URLs are **content targets**, not a claim of deployed pages.

## 2. Demand clusters: one primary URL per intent

The [content map CSV](./seo-content-map-2026-11-05.csv) is the maintained keyword-to-page plan. Keyword examples are *search-intent hypotheses*, not current ranking, traffic or volume measurements.

| Pillar | Tool / pillar page | Supporting article theme | Useful conversion handoff |
| --- | --- | --- | --- |
| Australian invoices & quotes | /tools/invoice-generator; proposed /tools/quote-generator | ABN tax invoice contents; quote versus invoice; due dates; overdue invoice ageing | Export PDF → optionally reuse customer/items → later AR/reconciliation |
| GST & BAS literacy | /tools/gst-calculator, /tools/abn-checker | Inclusive/exclusive GST; GST registration; G1/1A/1B; cash versus non-cash timing | Correct calculation → eventually reviewed GST/BAS workpapers |
| Pricing & viability | /tools/profit-margin-calculator, /tools/break-even-calculator | Markup versus margin; cost sensitivity; discounts and fees | Unit economics → eventually budget/P&L ledger |
| Cash & bookkeeping | /tools/cash-runway-calculator; proposed /tools/cash-flow-forecast | 12-week cash timing; profit versus cash; bank deposits vs sales; reconciliation | Scenario export → later bank/AP/AR-backed reporting |
| Xero switching (future paid intent) | Proposed /accounting/migrate-from-xero | Export bundle; opening TB; AR/AP migration; independent restore; data ownership | Waitlist/eligibility while G0–G2 incomplete; only actual safe import later |

**Avoid cannibalisation:** “GST inclusive calculator” and “remove GST” belong to one calculator page with helpful subsections, not two duplicated calculators. Quote vs invoice is a genuine distinct task; discount vs margin may warrant its own page only if it has distinct interactive functionality. Use Search Console queries and real completion data to decide splits or merges.

## 3. First-page template and UX contract

Every tool detail page should provide the working calculator/form in the first viewport or nearby, an explicit input definition, AU units, examples, assumptions, edge cases, and a source where regulated meaning is involved. No sign-in wall before a result, no intrusive modal, a usable mobile form, a keyboard-accessible submit/export and a PDF outcome where relevant.

Below the tool: one short worked example, one relevant Journal article and one meaningful next tool, then a restrained accounting benefit statement whose functionality exists in a released product. Do not use “lodge BAS now,” “sync bank automatically,” or invented subscription prices.

The Journal template should show a clear editorial title, published/reviewed dates with *actual* review work, evidence for material claims, a concrete example with reconciled numbers, accessible MDX charts and optional source-linked interactive blocks. Never change the page's publication date to imply a fresh legal review that never occurred.

A company/author About page, clear Australian publisher identity, true product availability statement, privacy policy, support links and clear ownership help users evaluate finance-related content. Do not invent accountant credentials, author profiles or fake “reviewed by CPA” badges.

## 4. Technical SEO launch sequence (critical path)

### Phase A — before switching the domain

1. Reconcile **GitHub source PRs #13–#20** with the actual hosted ChatGPT Sites project, including route availability and the invoice export flow. A successful Vite build is not a published site.
2. Add tammy.tax to Sites, verify DNS, TLS/SSL and hostname configuration; run direct HTTPS requests for at least /, /tools, /tools/invoice-generator, /blog, /privacy, /robots.txt and /sitemap.xml.
3. Inspect the HTML-rendered canonical links, meta titles/descriptions, JSON-LD, indexability, internal absolute URLs, RSS and outgoing links on both the new domain and old Sites host.
4. Only after the new site serves correct content, update SITE_ORIGIN, absolute links, sitemap, robots, RSS, OpenGraph, structured data and intended redirects together. Maintain a one-to-one URL mapping, not all old paths to the new homepage.
5. Check the new site's HTTP 200 routes and old-host permanent redirects once configured, and confirm no indexable duplicate. If the Sites platform does not support the necessary redirects, document the actual supported alternative before changing canonical.
6. Record every result as evidence. Do not say the move is done simply because Vercel DNS accepted A/TXT records.

### Phase B — Search Console and crawling

- Verify tammy.tax **Domain property** through supported DNS verification, and appropriate URL-prefix properties only where needed.
- Submit the correct new /sitemap.xml, inspect representative URLs for indexability and check robots/noindex/rendered canonical issues.
- Track discovered versus indexed pages, submitted sitemap state, page indexing errors, crawl errors, query/page impressions and clicks. Avoid asserting Google's indexing schedule.
- Resubmit sitemap after an accepted domain migration or substantial route addition; use URL Inspection to investigate outliers, not mass “request indexing” for every calculator.
- Add a controlled source-host redirect matrix and monitor duplicate/canonical conflicts across both hosts.

### Phase C — page quality and structured data

- Distinct H1, title, helpful descriptions and meaningful internal link text for each canonical page; no duplicated thin intent pages.
- Prefer WebApplication/SoftwareApplication properties only where they accurately reflect free software; set offers.price = 0 for real free utilities where applicable. **Do not fabricate ratings or reviews** merely to qualify for rich snippets. Eligibility is not a guarantee of rich display.
- Use Article/BlogPosting only for actual articles with truthful dates, author/publisher and relevant primary sources; avoid unsupported HowTo/FAQ rich-results gambits.
- Verify structured data with Google's Rich Results Test, rendered page content with URL Inspection and sitemap URLs with automated tests.
- Set meaningful image alt text or empty alt for decoration; avoid text in images that screen readers cannot access.
- Ship accessible responsive forms, local optimised SVGs and no unnecessary charting JS on content pages.
- Performance measures (LCP, INP, CLS) on actual hosted URL and representative mobile hardware, with regressions investigated instead of guessed.

### Phase D — ongoing editorial operations

- Weekly Search Console query-to-landing mismatch review: are users searching for BAS lodgement on a draft BAS page? Correct or clarify wording rather than overclaiming.
- Monthly primary-source checks for GST thresholds, BAS reporting rules, invoice requirements, financial statements and employment/tax topics. Capture the verified date and supporting official URL in the editorial claim ledger.
- Maintain an internal-link graph: pillar tool ↔ a specific explanatory guide ↔ next related tool or truthful accounting page. Avoid sitewide lists of every tool in every article.
- Archive or merge low-value pages only with measured evidence, correct redirects and preserved useful URLs.
- Revisit launch and content priorities based on completion, actual user feedback and search intent; no invented keyword difficulty or volume figures.

## 5. Editorial and content production strategy

Use the [Tammy Blog authoring skill](../../.claude/skills/tammy-blog/SKILL.md) in the stacked site branch. It specifies factual research, Australian English, MDX components, source attribution, calculation checks and anti-template editing.

**Initial editorial releases to support real tools:**

1. Invoice/quote cluster: tax invoice checklist, credit-note mechanics (after source check), invoice due-date ambiguity, quote-to-invoice handling. An accompanying functional quote generator must come before a marketing page pretending that conversion works.
2. Pricing cluster: discount percentage versus margin impact, billable-hour economics, gross margin versus net cash. Add an interactive sensitivity tool only if it computes a distinct user task.
3. Cash-flow cluster: opening balance, dated receipt/payment forecasting, unpaid invoices, BAS timing. A 12-week workbook is more useful than more static runway paragraphs.
4. Xero switch cluster: export formats, accounts and contacts mapping, trial balance reconciliation, migration preview safety. **Funnel to a clearly unavailable/waitlist state** until desktop cutover is tested.

One content brief per topic: intent; specific reader question; verified claim/source ledger; real task and formula; target primary URL; unique differentiation; one worked example; internal links and free tool CTA; responsible release owner; maintenance/review trigger. Fact-check laws, dates and amounts as a separate human review gate.

**Do not** mass-create local-suburb variants, FAQ stuffing, generic “top 10 accounting tips,” thin calculator clones or AI-written “fresh” posts without new value. Google's people-first guidance explicitly warns against content primarily generated to capture search traffic.

## 6. Distribution beyond organic search

- Offer clean, usable PDF and CSV exports that people share because the output is useful, not because they must post a link.
- Research relevant accountant/bookkeeper newsletters, contractor associations, legitimate AU small-business resource pages and professional communities, with genuine editorial outreach.
- Place an unobtrusive “Created with Tammy” reference only where appropriate and not misrepresenting an invoice as Tammy-issued accounting documentation.
- Publish transparent changelogs and original example breakdowns that demonstrate product correctness; do not assert nonexistent customers, product certifications or numbers.
- Avoid link-buying, forum spam, artificial review/reputation generation or programmatic page farms.

## 7. Measurement and experiment contract

The [measurement specification](./growth-measurement-2026-11-05.md) defines payloads and denominators. Search Console is the source of aggregate search impressions/clicks; consent-safe first-party events measure successful tool actions. Neither system alone identifies a person who paid on a separate device.

**Primary experiment order after M0:** reduce calculator input confusion → improve PDF completion/mobile export → compare inline explanation versus no explanation → test optional-save messaging → test one relevant accounting transition CTA. Do not start paid conversion experiments before a paid flow exists.

Each experiment needs a hypothesis, target segment, primary metric, error/accessibility guardrails, rollout/rollback and a declared stop rule. For low sample counts, report directional feedback without claiming statistical significance. Hold your ability to cancel experiments or disable instrumentation without breaking the free tools.

## 8. SEO source references

- [Google Search Central: Creating helpful, reliable, people-first content](https://developers.google.com/search/docs/fundamentals/creating-helpful-content)
- [Google Search Central: SEO starter guide](https://developers.google.com/search/docs/fundamentals/seo-starter-guide)
- [Google Search Central: Site moves with URL changes](https://developers.google.com/search/docs/crawling-indexing/site-move-with-url-changes)
- [Google Search Central: SoftwareApplication structured data](https://developers.google.com/search/docs/appearance/structured-data/software-app)
- [Google Search Central: Title links](https://developers.google.com/search/docs/appearance/title-link)
- [Google Search Console documentation](https://support.google.com/webmasters/answer/9128668)

This document is a quality and execution plan, not a prediction of Google rankings, verified search volumes, rich results, organic revenue or paid conversions.
