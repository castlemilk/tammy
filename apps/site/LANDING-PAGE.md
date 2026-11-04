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
