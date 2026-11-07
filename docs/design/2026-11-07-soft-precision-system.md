# Soft Precision: public system and working tools

This is the next unpublished design candidate on [PR #48](https://github.com/castlemilk/tammy/pull/48), extending baseline `c63c67d3c8eef558b6250fb1a2b78b974889acaa`. It preserves the existing Soft Precision identity and the preceding landing refinement. The direct Worker still serves source `56bd4ef4091a7df58e004b7ddb643e6f4c58cd7b`; this review is not publication, a Sites version, domain migration or CI qualification.

## What changed and why

Three separate public palettes and two wordmarks had drifted. `apps/site/app/tokens.css` now owns the public runtime palette, type families, spacing, radii and motion constants. Tailwind, Gamma, landing and tools consume it through explicit adapters. The common SiteBrand and AccountingStatus components retain the established two-leaf identity and distinct Draft, Reviewed, Posted, Not matched and Issued meanings. Desktop styles remain independently owned.

The tools directory had a compact-layout rule that lost to the generic hero through stylesheet order. Visitors reached decorative content before the actual choices. The corrected directory brings search, task filters and real tools together, with three/two/one-column reflow and all twelve descriptions and limitations retained. Search appears before filters in reading and keyboard order. Clear preserves the selected category and restores focus; Show all resets both.

GST now uses the existing Gamma native radio group and shared labelled field, with associated errors, no stale result during invalid input and unchanged exact-cent arithmetic. Shared and legacy tool fields use the intended paper, control radius and discoverable boundary. Focus is immediate and forest-coloured; the composite search has one outline. Pending buttons retain their label space and prevent repeat activation. Quote feedback distinguishes success from error. The mobile quote preview provides an explicitly labelled, keyboard-focusable table-scroll region and a nearby cue; its document totals remain outside that region. PDF templates and calculations were not changed.

A local component reference bundles the real React components and production CSS. It demonstrates native fields, Gamma selection, record states, disabled, error, empty and pending examples without adding a public route or performing accounting actions. See [the public system](../../apps/site/DESIGN-SYSTEM.md) and [current product truth](../../PRODUCT.md).

## Art direction and evidence

The generated [desktop/mobile tools study](assets/soft-precision-tools-study.webp) is a critique reference, not an approved pixel comp or a shipping screenshot. The new calculator, paper and pencil illustration supports the desktop heading and is omitted at narrow widths. It carries no invented financial data, product screen or capability claim. The responsive production images are approximately 16 KB and 6.5 KB.

The [asset record](2026-11-07-soft-precision-assets.json) retains exact prompts, source hashes, transformations and output hashes. Impeccable's WebP support writes prompt sidecars; no embedded binary metadata is claimed. This is an established-world, code-led extension: no concept seed, replacement identity or retrospective comp approval is claimed.

The following are actual Chromium captures, compressed as WebP for repository review; the original PNGs and browser scripts remain in the local review evidence directory.

| View | Before | Reviewed candidate |
| --- | --- | --- |
| Tools, 1440px | [Before](assets/evidence/tools-before-1440.webp) | [After](assets/evidence/tools-after-1440.webp) |
| Tools, 390px | [Before](assets/evidence/tools-before-390.webp) | [After](assets/evidence/tools-after-390.webp) |
| GST, 390px | [Before](assets/evidence/gst-before-390.webp) | [After](assets/evidence/gst-after-390.webp) |
| Actual component reference | — | [Desktop](assets/evidence/components-1440.webp) · [Mobile](assets/evidence/components-390.webp) |

Full-page captures cover home, tools, GST and Journal at 1440/390px, plus tools at 320px. Journal captures explicitly wait for lazy image decoding. Filled invoice and quote forms, search-empty and GST-error states are retained separately. Width measurements cover 320/390/768/1280/1440px and distinguish the reserved desktop scrollbar from content width. The tools first row begins around 545px on desktop and 672px at 390px.

## Review and verification boundaries

The first self-review caught a global border rule overriding component field/error colours and a generic hero paragraph rule adding unwanted spacing. Both were corrected before the independent review. A fresh Astra Max Impeccable reviewer opened eleven full-page/reference captures and four supplementary state/form captures, then returned `fix` with seven bounded findings. Those findings concern product-truth persistence, quote table scrolling, focus/error spacing, field parity, quote success feedback, production pending geometry and redundant labels/arrow glyphs. The review also checked the core issue implementation and withdrew an initial objection to native ledger posting: the README was stale, the current capability was real.

The confirm pass inspected twenty valid recaptures and scored all seven findings **resolved**, with disposition **ship** and no material regression observed from the batch. Its scope is the scored fixes, not a whole-application certification. The [browser evidence record](2026-11-07-soft-precision-evidence.json) contains the actual 27 journey checks, viewport measurements, production pending geometry and four PDF hashes. Native implementation/tests were inspected for product truth, not executed in this public-site pass.

Review-state captures: [single search focus](assets/evidence/tools-empty-state.webp), [GST validation](assets/evidence/gst-error-state.webp), [quote table keyboard scroll](assets/evidence/quote-table-scroll-390.webp), [production pending action](assets/evidence/quote-pending-390.webp), [invoice at 390px](assets/evidence/invoice-filled-390.webp), [quote at 390px](assets/evidence/quote-filled-390.webp) and [quote at 320px](assets/evidence/quote-filled-320.webp). The quote action remains 172.109375 × 44px while pending; a click scrolls it into view, which changes its viewport y-position without changing its size.

Executed checks are recorded separately from manual guidance:

- Site tests, invoice/PDF tests, release/descriptor/preflight tests and CI-contract tests; site/invoice typechecks; root/site lint; production builds; nine-post editorial and 36-route SEO checks.
- Browser keyboard skip link, search/filter/clear/empty recovery, Gamma arrows, invalid/corrected GST, exact AUD outputs, loaded-tool offline operation, BAS quarter/select, FAQ, Journal navigation, pending state, forced colours and reduced motion.
- Actual invoice and quote downloads, changed and repeated; PDF text/page checks and Poppler-rendered visual inspection. Initial examples change references 001→002 and AUD 220→275. Export tests are browser operations, not native issuance.
- Chromium touch emulation exercises filtering and Gamma selection. This is not physical-device coverage.
- The contract/token check verifies all fifteen palette values and both public route adapters. Sampled contrast ratios: ink/canvas 12.78, muted/canvas 5.38, paper/forest 7.58, danger/paper 6.70, field-border/paper 3.67, warning/soft 5.74. These are specific token pairs, not a site-wide accessibility certificate.
- Premium 1.4.0 strict static audit ran on the site with zero findings. Impeccable detection ran once on selected changed targets and reported two warnings plus 66 advisories. The generic warning stripe was removed; the invoice paper's bottom rule is intentional document geometry; remaining advisories include retained local colours/radii and leaf geometry. No zero-findings detector claim. The context command initially ran before checkout materialisation and found no context; the actual DESIGN.md and implementation were read directly afterwards.
- [Hallmark 2.0 guidance](https://github.com/nutlope/hallmark/blob/main/skills/hallmark/SKILL.md) and [public Appllama 1.3 guidance](https://github.com/Appllama/appllama-skills/blob/main/skills/appllama-app-design-skill/SKILL.md) were used as manual review lenses. No Hallmark CLI/stamp, Appllama MCP/simulator or automated certification ran. The Gamma catalogue's segmented-control reference was consulted; vendored Gamma behavior was preserved.

No physical touch device, screen reader, other browser engine or OS print dialog was tested. Legacy document/Journal geometry remains locally styled. The native Mac application is not re-approved by these website captures. Existing tax content and domain helpers are retained; this pass makes no new tax-rule claim.

## Release boundary

Final source is committed before normal descriptor attestation; the immediate evidence child becomes the candidate. Release artifacts must name that exact candidate, keep Cloudflare and Sites targets distinct, and remain outside source. The PR carries the actual source/build/archive hashes and check results once preparation completes. Old artifacts and deployed hashes are not relabelled.

Publication of this new design remains held pending its existing specific approval. DNS migration still needs the approved account-owned zone and complete DNS inventory. CI credentials and Cuttlefish registration/qualification remain separate unapproved work; a local pass or queued job does not satisfy those gates. No source bundle was transferred to Sites, no version was saved, and no credential, security, DNS or production setting was changed.
