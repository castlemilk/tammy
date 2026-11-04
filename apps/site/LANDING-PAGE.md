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
