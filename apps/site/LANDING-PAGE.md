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
