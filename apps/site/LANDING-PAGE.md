# Tammy landing-page implementation

This standalone marketing surface lives in apps/site, distinct from the Electron desktop app.

## Visual direction
- Soft Precision: oat canvas, ivory paper, forest/sage, with deep-forest, ink and draft-amber bands. Product scenes are illustrative app windows drawn in HTML from one synthetic set of books (components/landing/scenario.ts).
- First viewport presents an illustrative, synthetic workspace and the real product differentiator: local encrypted accounting on macOS.
- Semantic HTML, keyboard-focused navigation, reduced-motion fallbacks, mobile layout from 320px.
- Motion: the server HTML is the finished page. Demonstrations play once or wait for a press, nothing loops, and an amount is never tweened or counted up.
- The landing page uses no illustration assets apart from the four Journal covers; product scenes are synthetic HTML and must never pass for screenshots.

## Copy and publication truth
- **Do not** describe bank feeds, bank sync, AI auto-categorization, live ATO/SBR lodgement, email sending, payment collection or reminders, remote MCP access, free trials, published price plans, or public binaries as available. Recording dated payments against an issued invoice or a posted purchase is implemented: say payments are recorded and the balance owing falls; do not call it payment tracking.
- The illustrative workspace is explicitly labelled synthetic.
- Public CTA is an internal walkthrough or a mailto enquiry. The site is not a purchase, signup or download flow.
- Keep apps/site/content/public-content.generated.ts as the identity/privacy/legal source. This change does not touch it.
- The 12 November 2026 landing page describes purchases and recorded payments, invoice receipts, the Overview and its exports, the cash and non-cash GST workpaper views and the local API counts, each checked against PRODUCT.md and docs/development/tech-state.md. Owner sign-off: on 14 November 2026 the owner asked for this page to be merged and released, after these claims were listed for review. Do not add further release claims until qualified by the desktop app owner.

## SEO
- Canonical and OG origin: tammy-accounting.castlemilk.chatgpt.site.
- Homepage metadata, JSON-LD SoftwareApplication and FAQPage (the six visible questions, word for word), with no invented review/rating/offer, static sitemap.xml, robots.txt, llms.txt.
- Change the origin consistently before connecting a custom production domain.
- The site uses local SVGs, system fonts and no external tracking tags.

## Verification
Run from apps/site with the repo's pinned dependencies:
- pnpm typecheck
- pnpm lint
- pnpm test
- pnpm build

Review desktop and mobile at 320/390/768/1280/1440. Validate canonical, Open Graph and sitemap on the published deployment. No automated claims about these checks are made by this document.

## Public business tools (Nov 2026)

Tools are at `/tools`, with individual canonical routes for Australian invoices, GST and due dates.

- The invoice generator imports the same validated `@tammy/invoices/model` and pdfcn-adapted `@tammy/invoices/pdf` used by desktop. It loads a browser-only Forme worker build and WASM URL on demand, then uses the original desktop PDF formatter. It does not call any server route or save data.
- The invoice PDF requires business name, valid ABN checksum, addresses, dates, customer and valid line amounts. Its PDF engine uses bundled fonts and declines unsupported characters. The browser may need time to load the WASM chunk on first export.
- The calculator uses integer AUD cents and half-up rounding for standard GST. The date calculator uses UTC date arithmetic to avoid DST transitions and intentionally uses calendar days, not business days.
- Tool pages have canonical metadata and WebApplication JSON-LD; the index has CollectionPage markup, and each URL appears in sitemap.xml and llms.txt.
- No payment collection, email sending, public tax lodgement, bank feeds, account signup, or unverified business claims.
- The rendered tool art is semantic CSS decoration. Generated raster concept art is retained as optional design reference and is not an authenticated screenshot.

Validation: `pnpm --filter sites-project typecheck`, `pnpm --filter sites-project lint`, `pnpm --filter sites-project test`, `pnpm --filter sites-project build`, with mobile/desktop preview before any publication.

## November follow-up: discovery, content and visual system

- `/tools/abn-checker`: offline modulus-89 ABN format check through the existing invoice model. It does not verify business registration or GST status; official ABN Lookup remains the source of truth.
- `/tools/profit-margin-calculator`: integer AUD cents; gross profit, margin-on-sale and markup-on-cost. GST is deliberately excluded from input prices.
- `/guides`, `/guides/australian-tax-invoice-checklist` and `/guides/gst-inclusive-vs-exclusive`: original, sourced editorial content, not mass-produced search pages.
- The Tools hub uses an original layered-accounting SVG illustration with better responsive layouts, visible focus, five functional utilities, and a guide-reading section.
- Site-level `SITE_ORIGIN` now owns canonical URL configuration in shared layout, tools and homepage JSON-LD; the sitemap and llms.txt remain materialized public files.
- `pnpm --dir apps/site seo:check` scans routes for metadata/canonicals and compares their URLs against the sitemap and llms.txt. CI runs this audit before the production build.

### Domain transition hold
`tammy.tax` is the desired branded hostname, but **do not** change `SITE_ORIGIN`, sitemap, robots.txt or llms.txt until the ChatGPT Sites custom-domain validation, apex routing and HTTPS work on that hostname. Once verified, change them together, build, deploy and validate live canonical tags and sitemap. No domain or DNS change is part of this code-only PR. The preceding `tammy-accounting.castlemilk.chatgpt.site` canonical remains correct until migration.

### Release boundaries
Site changes do not change Tammy's desktop application, ATO/EVTE/SBR readiness, public download availability, tax eligibility or business registration. Generated illustration is decorative; no synthetic financial evidence is claimed as customer data or performance proof.

## Tammy Journal and extended calculator library (4 November 2026)

- `/blog` and three original articles explain margin versus markup, invoice payment timing, and rolling 12-week cash-flow forecasting. Each has a worked example, source references, in-page navigation, and BlogPosting JSON-LD with a real publication date.
- `/tools/break-even-calculator` estimates contribution, minimum whole units and revenue with integer AUD cents and BigInt for potentially large whole-unit results. If contribution is non-positive and fixed costs are positive, it explains that break-even is not reachable.
- `/tools/cash-runway-calculator` estimates a constant-rate cash runway using integer cents, with explicit no-net-burn and input-error states. It is not a dated cash forecast.
- `/blog` maintains a curated post inventory in `lib/blog/posts.ts` used by the index and BlogPosting structured data. The visual system has editorial paper-like layouts, responsive examples, an accessible reading index, and no invented metrics, quotes, or product guarantees.
- Sitemap, `llms.txt`, and an XML RSS feed index all public journal pages. The existing `seo:check` audit also checks those links.
- Existing canonical origin remains the ChatGPT Sites host pending verified production routing and HTTPS for `tammy.tax`.

These changes do not alter desktop software, issue real invoices, contact any remote business service, add monitoring, or claim ATO qualification. All example figures are synthetic and disclosed as such.

## Four new Journal articles and image kit (5 November 2026)

- `/blog/when-to-register-for-gst-australia` covers $75,000 GST turnover threshold, rolling current/projected 12-month tests, 21-day registration deadline and applicable exceptions, referencing ATO guidance.
- `/blog/business-receipts-five-year-record-keeping` covers receipt/source evidence, commonly required records, five-year baseline and longer exceptions, digital integrity and backups.
- `/blog/simpler-bas-g1-1a-1b-explained` covers Simpler BAS fields G1, 1A and 1B using a consistent fictional $22,000 taxable-sales / $7,150 purchase example. Clearly distinguishes the GST amount from other BAS obligations and from actual lodgement.
- `/blog/bank-statement-reconciliation-steps` covers matching source statement rows and ledger entries, verifying an illustrative $40 bank fee difference, then retaining the review trail.
- Each route has distinct canonical metadata, a shared BlogPosting JSON-LD article template, four semantic section anchors and sourced references.
- New indexed illustrations are `public/assets/journal-gst-threshold.svg`, `journal-records-archive.svg`, `journal-bas-workpaper.svg`, `journal-bank-reconciliation.svg`; all vector, local, text-free, decorative, without external embeds.
- `lib/blog/posts.ts` is the curated article inventory. Blog index, RSS feed, sitemap, `llms.txt`, homepage journal links and site tests are updated.
- A separate set of 4 original editorial raster illustrations (PNG and WebP plus thumbnails) is available as an asset package for future visual exploration; blog source remains crisp self-contained SVG until an intentional raster integration.

### Review boundaries
The tax content was checked against relevant ATO and Australian Government sources on 5 November 2026. Examples are hypothetical, not client data. Neither the blog nor Tammy's development app submits a BAS, syncs with bank accounts, automatically backs up an encrypted workspace or verifies GST registration.

## Tammy Blog MDX system (5 November 2026)

- Reused the registered MDX approach from `castlemilk/benebsworth.com`, adapted to Tammy's Vinext/Vite stack through the official `@mdx-js/rollup` plugin and `remark-gfm`.
- Authored MDX source in `apps/site/content/blog/*.mdx`; three new explicit routes import static MDX and register only curated local components.
- `components/mdx/tammy-components.tsx` provides Callout, WorkedExample, Formula, MoneyTable, BarChart, Figure, ToolLink and SourceNote. The accessible chart contains a machine-readable table of its underlying numeric values.
- Site-specific `app/blog/mdx.css` preserves Soft Precision colour/type tokens and the deliberate long-form article reading measure.
- `.claude/skills/tammy-blog/SKILL.md` contains authoring, fact-checking, citation, de-slop, design and quality gates; supporting references include voice, block grammar and a real MDX template.
- `scripts/editorial-lint.mjs` fails new articles that lack substantive text, official sources, numeric examples, true tool links or stable section anchors and catches repeated generative clichés. Marketing CI runs it after Vitest.
- New stories: contribution sensitivity, accounts receivable ageing and profit-to-cash reconciliation. Every numerical scenario is hypothetical and reconciled. The blog inventory, RSS, sitemap, llms.txt and homepage links are updated.
- No external chart service, fake company testimonials, automatic ATO lodgement, bank syncing, public Mac app downloads or third-party tracking has been introduced.

**Release:** These repository edits are not a ChatGPT Sites deployment. Keep the original Sites host canonical until `tammy.tax` is confirmed live with TLS and the intended content.

## Growth foundation: functional quote and release verification (5 November 2026)

- `/tools/quote-generator`: anonymous quote editor, item lines, quote validity date, optional ABN, GST/non-GST treatment, live totals and browser Print / Save as PDF. Reuses the validated invoice model's integer-cent math but does not issue a tax invoice or post accounting. Direct one-click PDF export and quote-to-invoice handoff are separate acceptance tasks.
- `scripts/live-site-preflight.mjs`: read-only production checker for HTTPS routes, canonical links, sitemap and robots. Can only confirm a domain after Sites has really published it; it does not alter DNS, host or deploy.
- `lib/growth/events.ts`: strict event data allowlist with opt-in injected publisher. No network sink, cookies, form capture or analytics transport exists in this branch. Consent-dependent tracking remains unlaunched pending privacy review.
- New quote route in the source catalogue, sitemap and llms.txt. Existing canonical stays on the Sites host until the custom domain is confirmed active; user-visible pages must not claim this is a paid product.

## Self-lodgement preparation extension (5 November 2026)

- `/tools/bas-workpaper`: strictly illustrative Simpler BAS GST-only G1, 1A, 1B exercise, user-classified inputs, whole-dollar label reference and a standard quarterly date. Does not determine GST eligibility, tax treatment, PAYG or lodgement deadline.
- `/tools/sole-trader-tax-checklist`: local-only reader checklist for an individual tax-return preparation, with print and reset, no collected personal amounts.
- Journal: `how-to-lodge-bas-yourself-australia`, `lodge-sole-trader-tax-return-mytax`, `quarterly-bas-due-dates-australia`; each includes ATO primary source, worked examples, article metadata and local SVG.
- Updated catalogue, home navigation, RSS, sitemap, llms.txt and tests. Existing Sites canonical remains unchanged until the publication and custom-domain move are verified.
- These free tools are **not** an authorised ATO submission channel and **not** a tax/BAS agent service.

## Shared shadcn + Gamma + Tailwind foundations (November continuation)

- `components/ui/`: reusable shadcn-style Button, Input, Textarea, Card and semantic Table primitives using Tailwind v4 token utilities and `cn`.
- `components/gamma/empty-state.tsx`: Gamma catalogue's validated Empty State composition, adapted to Tailwind and a scoped Gamma Atelier/Ledger-style token bridge in `app/tools/gamma-theme.css`. Tammy's existing Soft Precision palette and fonts remain authoritative.
- `/tools/cash-flow-forecast`: fully client-side, dated 12-week receipts/payments worksheet, no bank feeds or persistence, exact AUD cent arithmetic, lowest closing cash and CSV export with formula-injection guards.
- Quotes now use direct Forme/PDFcn PDF generation from the shared invoice engine but render a true QUOTE (never an issued invoice); an explicit local JSON draft download/import allows reuse without cloud accounts, backend calls or automatic storage. JSON draft files contain user-entered customer data and are not encrypted.
- Existing print and Save as PDF route remains as a fallback. Tests cover renderer identity, money, draft formats, forecast, CSV and the component system.
- These code changes are not a ChatGPT Sites deployment or a Vercel project configuration change.

## Shared UI component adoption (5 November 2026)

Site controls use Tailwind v4 semantic tokens and the existing shadcn/UI
configuration, with locally owned accessible Button, Input, Select, Checkbox,
Label and Card primitives. The BAS quarter switch is the owner's actual Gamma
SegmentedControl, vendored with its maintained CSS and mapped to Soft Precision
through `app/design-system.css`. Tax/checklist and quote forms reuse these
primitives. See `UI-COMPONENT-STANDARD.md` and
`components/gamma/README.md` for design-system authority, review gates and
the remaining gradual migration. This changes presentation only, not GST,
BAS, quote arithmetic, customer data handling, publishing or ATO integration.

## Free receipt/expense organiser (5 November 2026)

- `/tools/expense-organiser`: manual expense register with date, supplier, category, gross cents, user-entered estimated business use and descriptive GST evidence; optional browser-memory PDF/JPG/PNG receipt file association (<=5 MB).
- Totals deliberately distinguish paid amounts, user-entered business-use allocations and an indicative standard GST component; they **do not** estimate legal deductions or claimable GST credits.
- CSV export quotes/formula-neutralises user-controlled text; JSON import/export validates version and fields strictly with a 2 MB file cap. Both are browser initiated and contain private supplier details; binary receipt attachments are **not embedded** and must be kept separately.
- No account, backend upload, automatic OCR, browser localStorage, ATO filing or ledger posting. No claims of persistent synced storage.
- Tool directory, homepage, canonical sitemap and llms.txt updated. Actual Sites publication/canonical migration remains a separate release gate.

## Landing rebuild (November 2026)

The landing page is built from one component per band in `components/landing/`. `app/page.tsx` imports each band's `.tsx` and `.css` by its fixed filename. Use this section as the rule set for any edit to a band.

### Page order

`app/page.tsx` renders the bands in this order. Each band is a `<section>` with its own component and an `aria-labelledby` heading.

| # | Band | Component | id | Ground (`data-tone`) | `data-room` |
| --- | --- | --- | --- | --- | --- |
| 1 | Hero: the encrypted workspace unlocks into a trial balance; a rail you can click | `hero.tsx` | (h1 `landing-heading`) | canvas (default) | mac |
| 2 | GST instrument: a tool that works right now | `gst-strip.tsx` | `try-gst` | oat | browser |
| 3 | How it works: pinned stage, five steps a person takes | `workflow.tsx` | `workflow` | canvas | mac |
| 4 | What the workspace does: invoicing signature, overview, the rest | `features.tsx` + `invoicing.tsx` | `features` | paper | mac |
| 5 | GST workpaper: prepared is not lodged | `bas.tsx` | `bas` | draft | mac |
| 6 | Privacy: encrypted on disk | `privacy.tsx` | `privacy-first` | deep | mac |
| 7 | Local API and MCP | `integrations.tsx` | `integrations` | ink | mac |
| 8 | Free tools: bench of three sums and tray of twelve | `tools.tsx` + `bench.tsx` | `free-tools` | canvas | browser |
| 9 | The Journal and guides | `journal.tsx` | `journal` | sage | reading |
| 10 | Plain answers: T-account of limits and FAQ | `answers.tsx` | `questions` | oat | mac |
| 11 | Availability and enquiry | `closing.tsx` | `availability` | forest | mac |

The header, footer, base stylesheet (`app/landing.css`), tokens (`app/tokens.css`), the kit (`components/landing/kit.tsx`), the motion primitives (`motion.tsx`) and the shared scenario (`scenario.ts`) are shared. Change them only in a page-level edit, never from inside a band.

### File ownership and class names

- A band owns `components/landing/<band>.tsx`, `<band>.css`, and any helper file it creates. Helper files must start with the band name (for example `hero-window.tsx`, `bench-abn.tsx`) so that two bands never write to the same file.
- Every class a band writes starts with `lp-<band>-` (for example `.lp-hero-rail`). The GST strip is the one exception: its files are named `gst-strip…` and its classes start with `lp-gst-`. Shared classes from `landing.css` (`.lp-frame`, `.lp-h2`, `.lp-btn`, `.lp-pill`) are used, not redefined. Scope styles under the section's class; do not style bare elements.
- Relative imports only, with no `@/` alias and no runtime `next/*` imports. Add the oxlint header comments the existing bands use for internal `<a href>` links and `<img>` elements.

### Motion contract

The server HTML is the finished page. Every scene is in its final state, and every panel's content is present. Motion is an upgrade applied on top of that.

1. Motion sits inside `@media screen and (prefers-reduced-motion: no-preference)`; every landing stylesheet uses that exact form. Under reduced motion every state change is an instant swap or a cross-fade of 200ms or less, with no pinning transforms, drawing or staggered arrival.
2. Never server-render `hidden`, `aria-hidden="true"`, inline `display:none` or `visibility:hidden` on real content. Hide things only with classes or attributes, and only for hidden-before-reveal states that script sets.
3. Amounts never tween or count up. Rows, figures and marks arrive whole at their final value. A figure may be replaced whole when an input changes.
4. Animate `opacity`, `transform` (`translate`, `scale`, `rotate`) and, for small areas only, `background-size`, `clip-path` and `stroke-dashoffset`. Do not animate layout on anything larger than a disclosure panel. No `filter`, `backdrop-filter` or `box-shadow` animation, and no `will-change` left on.
5. Overshoot (`--l-bounce`, `--l-snap`) applies to `scale`, `translate` and `rotate` only. `--l-bounce` is for pill landings only. Everything else uses `--l-settle` or `--l-ease`. A single move runs 140ms to 760ms; a highlight that holds before it fades (the tray's answer sweep, the GST strip and bench flashes, the Overview trace) may run longer, and a panel that is cut away may leave faster (the match panel in How it works leaves in 80ms). A whole demonstration takes under five seconds.
6. Nothing loops. Anything that plays without being pressed plays once and offers a `<button class="lp-replay">` with the replay icon and the word "Replay".
7. The hero's load moment is CSS-only keyframes that start at first paint, with no script and no `data-scene`. The h1 (the LCP element), lede, buttons and availability line are never animated.
8. Do not fade and rise whole sections. Motion goes where something arrives, lands, is drawn or is traced.

Demonstrations use CSS choreography with a single attribute flip. The finished state is rendered; a `data-play` or `data-state` attribute drives before, press and after sequences in CSS; the island only sets the attribute, and sets it again for Replay. Avoid `setTimeout` state machines where a CSS delay will do.

### Pinned strings

The test suite renders the whole page in jsdom, with no CSS, and everything rendered exists at once (all tabs, panels and steps). Each string below must appear exactly once on the page, in the band that owns it.

| String or rule | Owner |
| --- | --- |
| The only `<h1>`, text `Your books. On your Mac.`; the hero `<section>` is `aria-labelledby` it | hero |
| Text `In development. Public Mac download not yet available.` as the direct text of one visible element, not inside `<details>` | hero |
| Link `Release status` to `#availability` (one in the hero; the footer has its own) | hero |
| Link `See how it works` to `#workflow` (the only one on the page) | hero |
| Link `Try free tools` to `#free-tools` (one, inside the hero) | hero |
| Text matching `macOS 14 or later, Apple silicon`, derived from `publicContent` | hero, closing |
| Heading `From source records to a balanced ledger.` | workflow |
| Heading `Customer invoicing` in an `<article>` that also holds the link `Browser invoice generator` to `/tools/invoice-generator`, the text `Creates a PDF independently of your Mac workspace.` as the direct text of one visible element (not inside a closed `<details>`), and the text `Issue to the local ledger; export a customer PDF` as the direct text of one element | invoicing |
| Heading `Prepared is not lodged.` and the text `BAS drafts, not lodgement` as the direct text of one element | bas |
| Region `Free business tools` with exactly twelve links to `/tools/` routes, each once; the bench has no `/tools/` links | tools |
| Region `The Tammy Journal` with links to `/blog/which-quarter-gets-the-gst`, `/blog/not-every-bank-deposit-is-sales`, `/blog/the-month-end-review-that-found-220` and `/blog/the-price-rise-that-didnt-fix-margin`, each once | journal |
| Text `Can Tammy lodge my BAS with the ATO?` as the direct text of one element | answers |
| One element each whose direct text contains `Automatic bank feeds and bank-account syncing are not part`, `explicit issue action posts receivables, income and GST`, `does not email the invoice or collect payment` and `There is no Tammy cloud workspace` (the FAQ answers, kept word for word) | answers |
| Link `Enquire about Tammy`, a `mailto:` link (the only one on the page) | closing |
| At least one `Gamma Systems Pty Ltd` | footer |

The page must not contain a link named `Download for Mac` or `Get started for free`, nor the text `company EOFY`, `company tax return`, or any sentence saying Tammy does not post a receivable. Every `<a href="#x">` must point at exactly one element with `id="x"`, and no `href="#"` may appear.

### Scenario and figures

Every Mac figure comes from `components/landing/scenario.ts`, so one quarter of books is the single source. The $120.00 GST on the invoice is the same $120.00 in GST payable and at label 1A. Business names are "Example …". Browser instruments compute with the pure functions in `lib/tools/*` and `@tammy/invoices/model`, and never import the PDF engine or a full tool component.

### Checks

From `apps/site`, run `pnpm typecheck`, `pnpm lint`, `pnpm test` and `pnpm seo:check`. Check each band at 320, 390, 768, 1280 and 1440 wide, with reduced motion and with JavaScript off, before release.
