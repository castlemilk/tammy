# Tammy landing-page implementation

This standalone marketing surface lives in apps/site, distinct from the Electron desktop app.

## Visual direction
- Soft Precision: ivory editorial canvas, warm paper, forest/sage, ledger-inspired grids and topographic contour SVGs.
- First viewport presents an illustrative, synthetic workspace and the real product differentiator: local encrypted accounting on macOS.
- Semantic HTML, keyboard-focused navigation, reduced-motion fallbacks, mobile layout from 320px.
- Local illustration SVGs in public/assets are decorative and can be swapped for authentic product screenshots after release verification.

## Copy and publication truth
- **Do not** describe bank feeds, bank sync, AI auto-categorization, live ATO/SBR lodgement, email sending, payment tracking, remote MCP access, free trials, published price plans, or public binaries as available.
- The illustrative workspace is explicitly labelled synthetic.
- Public CTA is an internal walkthrough or a mailto enquiry. The site is not a purchase, signup or download flow.
- Keep apps/site/content/public-content.generated.ts as the identity/privacy/legal source. This change does not touch it.
- Do not update release claims until qualified by the desktop app owner.

## SEO
- Canonical and OG origin: tammy-accounting.castlemilk.chatgpt.site.
- Homepage metadata, JSON-LD SoftwareApplication (no invented review/rating/offer), static sitemap.xml, robots.txt, llms.txt.
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
