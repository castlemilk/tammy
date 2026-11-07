# Tammy landing journey refinement: 7 November 2026

This unpublished pass compares with `f1b417ecaac17d20fabce89f843afd3ab8bddf8e`, the preceding [feature review](2026-11-07-site-feature-review.md). The live direct Worker still runs `56bd4ef4091a7df58e004b7ddb643e6f4c58cd7b`. No publication, domain migration, CI credential or runner change is part of this review.

## Critique and changes

The complete desktop and 320px journey repeated source review in separate workflow, feature, artwork and BAS sections. The twelve-row tools directory and decorative lock panel made the lower page long, while development availability was easy to miss.

- Keep the hero, Soft Precision typography/palette, four readable financial specimens and original generated artwork. Combine the compact process overview with the artwork, removing the separate section and circular navigation back to the overview. No additional image generation was needed; the [existing asset provenance](source-ledger-illustration.json) remains unchanged.
- Show development status and the absence of a public Mac download next to the hero actions. The header's Development access link leads to the existing enquiry section; Try free tools provides an immediately usable alternative.
- Keep native invoice issuance distinct from the browser PDF generator. Preserve all current accounting states, manual review/matching, bank-feed limits, GST/BAS boundaries and FAQ answers.
- Fold Australian accounting context into GST/BAS preparation. Replace the decorative privacy scene with the actual local-storage facts. Keep privacy policy and technical support reachable.
- Present all twelve tool destinations and descriptions in three named groups, led by invoicing; retain the four Journal stories and native FAQ disclosures. Tighten section spacing without shrinking financial text or tap targets. At 320px, Development access wraps within its header column and retains a 44px target.

## Rendered acceptance

[Evidence](2026-11-07-landing-journey-evidence.json) records baseline and final screenshot hashes, the exact rendered source-file hashes, viewport measurements and downloaded PDF hashes. The final GitHub candidate is recorded in [PR48](https://github.com/castlemilk/tammy/pull/48); documentation/evidence changes do not relabel older artifacts.

An independent reviewer inspected complete before/after desktop and mobile pixels, including native-width strips. The sole material first-round issue was the narrow header fit; the corrected header received a fresh **pass**, with no material visual issues remaining. This is a composition verdict, separate from interaction checks and publication authorization.

- Desktop capture height decreased from 9,143 to 6,887px; mobile from 12,887 to 11,503px. All twelve tools, four Journal links and four financial specimens remain visible.
- Checked 320, 390, 768, 1280 and 1440px layouts. All captions remain 12px and financial example text 13px or larger. At 320px the inherited body minimum width equals the viewport; desktop scrollbar measurement reports a 305px client width and 320px scroll width, while the rightmost content/header edge is 304px. No content clipping was observed. This is not a claim that the inherited global minimum-width rule changed.
- The earlier floating Skip to content control was a focused capture state. It is hidden in unfocused final captures, becomes visible with keyboard focus, and Enter reaches the main content anchor. Native FAQ Enter toggling and landing-to-tools navigation worked.
- Four real browser PDF downloads used nonpersonal examples: invoices `REFINE-QA-001/002` (AUD100/200) and quotes `REFINE-QUOTE-001/002` (AUD150/300). All were one-page PDFs with the expected references and changed totals. The second quote was also rendered and inspected. The first download-event wait timed out despite successful generation; the exact file was found and verified before proceeding, with no duplicate retry.
- Physical touch, assistive technology and the OS print dialog were not exercised. The quote flow was exercised at 320px; the invoice flow at desktop width. Existing form and accounting implementation is unchanged.

## Checks and method disclosure

Local checks passed with Node 24.18.0 / pnpm 11.15.0: 90 site tests, 34 invoice tests, 169 release/descriptor/preflight tests and seven CI-contract tests; site/invoice typechecks; site/root lint; nine-post editorial checks; 36-route SEO inventory; and direct production preview build. The new route tests protect all sixteen tool/Journal destinations, valid fragment targets and visible availability/PDF separation. Root lint retains 136 existing warnings and 54 information diagnostics, with no errors. An initial test-only sort-comparator lint error was fixed and the checks passed afterward.

Frontend Design and Frontend Design Premium guidance were applied to the existing marketing page. Premium 1.4.0 strict source audit returned zero findings. Impeccable skill 4.3.1's current local launcher reported CLI 4.0.0: context loading succeeded, and one detector run reported 27 advisory colour/radius observations, not zero findings. Two concerned a newly introduced privacy divider colour, which was replaced by a derivation of the existing sage token; the remaining observations concern retained styles. No detector rerun or certification is claimed. The established taste review and relevant public Appllama principles informed hierarchy, legibility and state checks; no Appllama MCP or simulator was executed. Hallmark remains a manual guidance lens because no approved installed CLI was available in the established review environments.

Final source must receive normal immediate-child descriptor attestation and independent workspace export/build verification before release consideration. Exact candidate build/provenance and current CI results belong in PR48. The existing preview image identities are replaced only after validation, retaining their f1b417ec version history. PDFs and source bundles remain local. New-design publication, domain/DNS access and migration, persistent CI credentials and Cuttlefish registration/qualification remain separate held gates.
