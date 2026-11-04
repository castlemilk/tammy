# Tammy Journal: four additional articles and illustrated asset kit

Created 5 November 2026. This change extends the existing Soft Precision editorial visual system and stacks on the prior site/resource PRs.

| Post | Canonical route (relative) | Source SVG included in website |
| --- | --- | --- |
| GST registration | `/blog/when-to-register-for-gst-australia` | `public/assets/journal-gst-threshold.svg` |
| Receipts and records | `/blog/business-receipts-five-year-record-keeping` | `public/assets/journal-records-archive.svg` |
| Simpler BAS workpaper | `/blog/simpler-bas-g1-1a-1b-explained` | `public/assets/journal-bas-workpaper.svg` |
| Bank statement reconciliation | `/blog/bank-statement-reconciliation-steps` | `public/assets/journal-bank-reconciliation.svg` |

## Art direction

- Visual language: ivory paper, forest and sage, old ledger instruments, brass accents, restrained engraved mountain linework. Subject-specific compositions, not templated stock icon cards.
- The published website uses four original, text-free, self-contained SVGs sized on a common 1200 × 720 viewBox. No external image URLs, font files, scripts, or unlicensed logos.
- Each new post is rendered by the existing `BlogArticle` component. The article hero includes an illustration and an HTML data/example panel; the archive includes a small editorial thumbnail. Headline, numeric examples, official links, table contents, accessibility semantics and interactions are real HTML rather than painted image text.
- An additional separately supplied illustration pack contains four original raster illustrations, each cropped to its native panel and provided as PNG, desktop WebP and small WebP. The raster images are visual assets for future use and **are not yet wired into the site**. Do not mistake the separate image ZIP for a deployed website update.
- Images are decorative concept art, not product screenshots or evidence of actual customer records or ATO certification.

## Subject / source boundary

- GST registration covers current/projected GST turnover, the standard $75,000 annual threshold, a 21-day deadline once required, and exceptions. See `https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/gst/registering-for-gst`.
- Recordkeeping describes the general five-year starting point, with longer retention/altered start dates as applicable. See `https://business.gov.au/finance/payments-and-invoicing/record-keeping` and `https://www.ato.gov.au/businesses-and-organisations/preparing-lodging-and-paying/record-keeping-for-business`.
- BAS illustrates G1, 1A, 1B with $22,000 taxable sales including $2,000 GST and $7,150 eligible purchases including $650 GST, for a $1,350 GST difference before other obligations. See `https://softwaredevelopers.ato.gov.au/SimplerBAS`.
- Bank reconciliation illustrates opening $8,000, deposits $4,400 and statement payments $1,750, versus the book's $1,710 payments. The difference is a fictional $40 fee that must be verified, not automatically posted. See `https://business.gov.au/finance/financial-tools-and-templates/key-financial-terms`.
- None of these pages implies that Tammy is a registered tax/BAS agent, performs real bank-feed synchronisation, files with ATO, or has hosted backups. Any application capability statement must remain consistent with `LANDING-PAGE.md`.

## Search and release

- Curated article metadata and image references live in `lib/blog/posts.ts`.
- New static canonical routes, structured `BlogPosting` data, links from the journal index and homepage, RSS entries, sitemap URLs, and `llms.txt` additions are updated in this PR.
- The existing SEO audit checks route coverage and public RSS consistency. Focused tests verify image assets, four article sections per post, source links, canonical metadata, publication date, BAS/workpaper arithmetic, and reconciliation arithmetic.
- Preserve current canonical `tammy-accounting.castlemilk.chatgpt.site` until `tammy.tax` custom-domain certificate, hostname verification and actual production deployment are confirmed.
- This source change does not itself publish to ChatGPT Sites. Browser viewport and actual production/page-load review remain independent gates.
