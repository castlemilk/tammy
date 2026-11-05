# Tammy site: visual asset library

Artwork added in `feat/site-visual-tool-library-2026-11-05`.

| Asset | Where used | Shape | Notes |
| --- | --- | --- | --- |
| `public/assets/tammy-ledger-helix.svg` | Tools index hero | 880 × 500 | Ornamental ledger papers, calculation rings and paper geometry. No synthetic UI or encoded copy. |
| `public/assets/tammy-invoice-fold.svg` | Featured invoice generator tool | 380 × 290 | Folded invoice sheets and mathematical seal. Decorative; never presented as an actual issued invoice. |
| `public/assets/tammy-margin-orbit.svg` | Tammy Journal margin feature | 540 × 500 | Concentric accounting proportion diagram. Decorative behind the real HTML calculation. |

All three are text-free, self-contained SVGs. They contain no remote image requests, fonts, scripts or logos from third parties. Their palette matches Soft Precision ivory, sage and forest. Their geometric, paper-based shapes continue the approved editorial concept rather than introducing another visual identity.

## Compositional rules

- Keep all headings, invoice numbers, numeric results, filters and actions in semantic HTML.
- Do not embed screenshots containing fictional customer data as authoritative product UI.
- Keep the ledger hero as a single authored visual moment, not an animated screensaver.
- Make the index searchable and filterable without a network call; all seven destination routes remain rendered in the default state.
- Focus and reset are visible and operable using keyboard alone.
- Respect reduced-motion settings. Do not animate prices or financial calculations.
- Keep the original `DESIGN.md` palette and reading typography; do not add external fonts to the public site.

## Design review

Hallmark (manual): subject-led illustration, one visually dominant tool, no invented social proof or decorative hero metrics. The existing tool layout predates the strict Hallmark stamp protocol, so this change is an incremental review, **not** a claim of successful Hallmark sloplint.

Design Taste (manual): tangible accounting artefacts replace generic orbs and identical icon cards. Search and category facets add genuine value without anonymous hero screenshots.

Impeccable (manual): filtered count, empty state and reset; accessible filter buttons and search label; focused navigation; readable keyboard states; narrow-page fallbacks; reduced-motion handling.

Automatic checks: existing marketing-site pipeline runs TypeScript, oxlint, Vitest, SEO route inventory and Vinext build. Pixel-based desktop/mobile screenshot review and actual browser invoice-PDF download remain independent release checks.

## Branding and publication

`tammy.tax` is the intended customer-facing domain but the canonical stays on the existing verified ChatGPT Sites host until the custom-domain route, TLS certificate and production publishing are confirmed. Updating artwork in GitHub alone does not deploy to Sites.
