# Tammy site: visual asset library

Original SVG artwork was added in `feat/site-visual-tool-library-2026-11-05`. The November 7 feature pass adds the receipt-and-ledger illustration below.

| Asset | Where used | Shape | Notes |
| --- | --- | --- | --- |
| `public/assets/tammy-ledger-helix.svg` | Tools index hero | 880 × 500 | Ornamental ledger papers, calculation rings and paper geometry. No synthetic UI or encoded copy. |
| `public/assets/tammy-invoice-fold.svg` | Featured invoice generator tool | 380 × 290 | Folded invoice sheets and mathematical seal. Decorative; never presented as an actual issued invoice. |
| `public/assets/tammy-margin-orbit.svg` | Tammy Journal margin feature | 540 × 500 | Concentric accounting proportion diagram. Decorative behind the real HTML calculation. |
| `public/assets/source-ledger-1200.webp`, `source-ledger-640.webp` | Not used on any page since the November 2026 landing rebuild (it was the homepage source-review section) | 1200 × 800 / 640 × 427 | Original generated receipt, pencil and ledger illustration. Responsive, lazy-loaded, 38 KB / 11 KB. No embedded words, amounts or real records. |

The three SVGs are text-free and self-contained. All assets are served locally, with no remote image requests, fonts, scripts or third-party logos. Their palette matches Soft Precision ivory, sage and forest.

The generated illustration follows the explicitly requested local Imagegen skill. Its CLI was unavailable because the API key and SDK were absent; the available image-generation tool produced the asset instead. No packages or credentials were added. One edit removed unwanted numerals from the first result. [Prompt, generation path and asset hashes](../../docs/design/source-ledger-illustration.json) record the actual provenance; the tool did not expose a model version. Only deterministic resizing and WebP compression followed generation. The source PNG remains in the local task's `output/imagegen/` directory. This is artwork, never an actual application screenshot. Readable feature examples stay in semantic HTML and disclose their synthetic status.

## Compositional rules

- Keep all headings, invoice numbers, numeric results, filters and actions in semantic HTML.
- Do not embed screenshots containing fictional customer data as authoritative product UI.
- Keep the ledger hero as a single authored visual moment, not an animated screensaver.
- Make the index searchable and filterable without a network call; all twelve destination routes remain rendered in the default state.
- Focus and reset are visible and operable using keyboard alone.
- Respect reduced-motion settings. Do not tween or count up prices or results; a result is shown or replaced whole. A landing instrument may move the bar, line or highlight that explains a result, once.
- Keep the original `DESIGN.md` palette and reading typography; do not add external fonts to the public site.

## Design review

Hallmark (manual): subject-led illustration, one visually dominant tool, no invented social proof or decorative hero metrics. The existing tool layout predates the strict Hallmark stamp protocol, so this change is an incremental review, **not** a claim of successful Hallmark sloplint.

Design Taste (manual): tangible accounting artefacts replace generic orbs and identical icon cards. Search and category facets add genuine value without anonymous hero screenshots.

Impeccable (manual): filtered count, empty state and reset; accessible filter buttons and search label; focused navigation; readable keyboard states; narrow-page fallbacks; reduced-motion handling.

Automatic checks: existing marketing-site pipeline runs TypeScript, oxlint, Vitest, SEO route inventory and Vinext build. Pixel-based desktop/mobile screenshot review and actual browser invoice-PDF download remain independent release checks.

## Branding and publication

`tammy.tax` is the intended customer-facing domain but the canonical stays on the existing verified ChatGPT Sites host until the custom-domain route, TLS certificate and production publishing are confirmed. Updating artwork in GitHub alone does not deploy to Sites.
