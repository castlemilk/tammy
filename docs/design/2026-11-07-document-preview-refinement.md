# Readable document previews and grouped website releases

This bounded pass follows the [Soft Precision system review](2026-11-07-soft-precision-system.md) at `f3c426ab5702ac0e928d2f4ed51c28f791ec079e`. The subsequent user instruction requests integration and release. This report records local implementation and review; the final candidate, GitHub integration and deployed Worker version must be read from the separate release receipt. It does not turn earlier queued or cancelled CI into a pass.

## Two useful document fixes

The independent Astra Max Impeccable review found that narrow browser tables shrank to 9px, and their `Ex GST` column showed the extended line subtotal while the actual PDFs showed a unit price. Both amounts were arithmetically valid, but the preview and exported column had different meanings.

Both previews now keep 12px tabular figures in a shared, named, keyboard-focusable horizontal scroll region with a 440px minimum table width. A nearby cue follows the actual paper container width. Document totals remain outside the region. Both tables show `Unit ex GST` from the validated draft, agreeing with the PDF: quantity 2 × $125.00, GST $25.00, total $275.00; subtotal remains $250.00. Fractional quantity 1.5 is also covered. No tax calculation or PDF template changed.

| Actual browser evidence | Before | After |
| --- | --- | --- |
| Invoice, 390px | [Prior full journey](assets/evidence/invoice-filled-390.webp) | [Preview detail](assets/evidence/document-preview/invoice-390.webp) |
| Quote, 390px | [Prior full journey](assets/evidence/quote-filled-390.webp) | [Preview detail](assets/evidence/document-preview/quote-390.webp) |
| Quote, 320px | [Prior full journey](assets/evidence/quote-filled-320.webp) | [Preview detail](assets/evidence/document-preview/quote-320.webp) |
| Desktop | Prior review packet | [Invoice](assets/evidence/document-preview/invoice-1440.webp) · [Quote](assets/evidence/document-preview/quote-1440.webp) |

These are rendered implementation captures, not image-generated comps. The existing art direction and generated assets remain unchanged. The [evidence JSON](2026-11-07-document-preview-evidence.json) retains capture hashes, six viewport measurements, keyboard scrolling, pending geometry and actual PDF hashes. Scrollbars reserve 15px on this Chrome setup; viewport and content widths are recorded separately.

The reviewer independently inspected the new panel/full-page captures and actual PDF renders, checked all four new PDF binaries against the previous exports, and returned **ship** for both scored findings, with no material regression observed. This is a bounded confirm pass, not a new whole-site or native-app certification. A separate emulated-print check caught a stylesheet-order collision; more specific quote print selectors restore the existing 10px print type, zero table minimum width, visible overflow and hidden scroll cue. No OS print dialog or physical printer was exercised.

## CI groups preparation, publication and acceptance

The single **Tammy website** workflow identifies the PR or master run and its verify/deploy intent. Pull requests validate; only master pushes trigger the push lane. A master run serializes Worker publication, consumes that run's exact verified direct artifact and retains publication and HTTP acceptance as separate JSON evidence. Its summary does not report a failed or missing health check as success. Deployment credentials remain confined to the deployment step; permission scope, immutable action pins, Cuttlefish identity/fork guards and Sites boundaries remain intact.

The new HTTP acceptance script is bound to the exact receipt's revision, repository, verified account, Worker, URL and hashes. It imports the existing canonical origin from `lib/site-config.ts` and checks twelve public routes without credentials. The general preflight remains strict by default; the root URL with or without its trailing slash is treated as the same resource, with other route mismatches still rejected. This explicit transport/canonical distinction preserves the existing Sites canonical host; no canonical or DNS cutover is performed.

At the read-only setup inspection, GitHub returned zero repository runners, no repository Actions secrets and no account variable. The site lane at `f3c426ab` was queued; foundation Ubuntu and Windows passed while macOS was cancelled. CI code can be integrated, but unattended deployment cannot work until the separately authorized runner and credential setup is completed. PR #46 remains independently gated. The approved local Wrangler session is a separate release path, not Cuttlefish qualification.

## Executed verification and limits

- 312 distinct local tests passed: 93 site, 34 invoice/PDF and 185 release, descriptor, preflight and CI contract tests. Tests include stale/foreign receipt rejection, published-but-unhealthy behavior, and execution of the actual CI summary and artifact-verification scripts.
- Site/invoice typechecks, site/root lint, actionlint, nine-post editorial, 36-route SEO and fifteen-token checks passed. Root lint retains 136 pre-existing warnings and 54 informational diagnostics, with no errors.
- The local production build and actual Chromium invoice/quote journeys passed at 1440, 390 and 320px. Both narrow tables scroll using keyboard input without page overflow; pending quote dimensions and success/error semantics remain covered.
- Four new PDF downloads changed references 001→002 and amounts AUD 220→275. All four are byte-identical to the previous correctly generated exports; Poppler text/page checks and visual inspection confirm one page each.
- The read-only Cloudflare dashboard showed the expected existing account and Worker, workers.dev address, no custom domains or routes, and the previous source/version. No dashboard setting was changed.

Earlier Premium/Impeccable checks and manual Hallmark/Appllama guidance remain scoped to the preceding review. No new detector run, certification, native Mac test, physical touch, screen reader or other browser-engine test is claimed here. Release preparation must still record the final attested commit and fresh target-specific artifacts; live acceptance must use the actual publication receipt.
