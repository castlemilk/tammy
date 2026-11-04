# Tammy Journal and tools library: visual design review

Review date: 4 November 2026. Scope: site-only PR stacked on the existing Tammy
Soft Precision landing-page and resource-library work. The approved design identity
in the repository-root `DESIGN.md` remains authoritative.

## Design read
- Audience: Australian sole traders and small-business operators who need to
  understand invoicing, GST, margin and cash flow without sales jargon.
- One job for the new Journal: help readers understand a business decision, then
  offer the related calculation or source.
- Design idea: a financial notebook. Articles are built from real transaction
  relationships (cost vs sale, invoice vs cash, opening vs closing cash), not
  generic charts or decorative product mockups.
- Palette and type: retain Tammy's existing ivory, forest, sage and humanist
  serif/sans pairing. No new theme or external font dependency.
- Direction dials (Taste): moderate composition variance, very low motion,
  medium reading density. The central signature is a worked example treated as
  a printed accounting artifact, not a metric card.

## Hallmark anti-template pass (manual)
- Blog has a strong single feature and a separate editorial archive, not a
  three-column icon-card row or invented logo/testimonial rail.
- Headline, description and first article link are present without script.
- Section labels are content categories or actual dates, not arbitrary
  numbered ornaments.
- Existing site palettes/tokens are reused; new blog CSS is scoped to the
  journal and its reading states.
- No fake results, rankings, followers, customer testimonials, usage counts,
  or capability claims.
- Focus-visible is inherited from the ToolsShell; narrow tables are labelled
  keyboard-scrollable regions; reduced motion does not remove content.

## Design Taste pass (manual)
- The subject-specific motif is the working ledger, not a gradient, orb, or
  hero card manufactured for decoration.
- Vary scale: major feature, low-key archive, then a long-form article with
  a visible content index and an actually useful data example.
- Avoid a uniform six-card grid in the journal. The tools hub retains its
  familiar cards, with the invoice generator structurally distinguished.
- Keep article typography and measure readable on mobile; avoid heavy
  client-side JavaScript on content-only routes.
- No novelty animation or rotating numbers that would obscure financial meaning.

## Impeccable quality pass (manual)
- Real tasks: view invoice/payment examples and use a linked calculator.
- States: calculator input validation, non-positive contribution, zero burn,
  and large integer-cent output are explicitly implemented and tested.
- Navigation: functional links to guides, blog posts, calculator routes and
  source references, with no inert controls.
- Screen-reader: semantic headings, labelled inputs, region names on
  scrollable tables, and live calculation updates.
- Performance: the Journal is server-rendered, with no new JavaScript
  dependencies or animation package.
- Layout: editorial reading column and TOC stack naturally below the desktop
  breakpoint; no horizontal scrolling except a named example table.

## Verification boundaries
This was a manual skill-informed review. The Hallmark sloplint, Taste browser
inspector, Impeccable context launcher and a pixel-based browser comparison
were not executed here. CI checks build, lint, typecheck, unit tests and
canonical/SEO inventory. Desktop/mobile visual inspection, actual PDF export
and live Sites publication remain separate release checks.
