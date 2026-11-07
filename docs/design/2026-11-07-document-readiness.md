# Retain the first document entry

Live acceptance after PR #48 found a narrow readiness defect: when the first business-name entry arrived before React hydrated the form, the name could disappear. On deployed candidate `f580456f25321c6ed0a73c078fb8ce771cb53078`, the captured invoice preview still said “Your business” and export validation requested “Enter your business name.” No malformed invoice was produced. All four PDFs passed once the page was ready, isolating early editing from PDF generation.

Invoice and quote editors now render as named native fieldsets, disabled and `aria-busy` until their client code can retain input. The shared `useBrowserReady` hook returns the same disabled state on the server and first hydration render. The existing small locality label reads “Preparing form…” during that interval. A no-JavaScript paragraph explains why the editor cannot run. The normal form, preview, calculations, document data and PDF renderer remain owned by the existing components.

Native fieldset margin and quote padding/border are explicitly reset to preserve the prior layout. This is a functional follow-up to the established Soft Precision system, with no new art direction or asset generation.

## Actual regression evidence

The production-browser test held the invoice or quote client module before it loaded, verified that native controls and the PDF action were disabled, then released the module. The first entered name appeared in each live preview. A separate JavaScript-disabled context showed the explanation with controls still disabled. The initial fallback locator matched the non-boxed `noscript` container; inspection confirmed the paragraph was rendered, and the final test checks that visible paragraph directly.

| State | Invoice | Quote |
| --- | --- | --- |
| Client module held | [Loading](assets/evidence/document-readiness/invoice-loading-390.webp) | [Loading](assets/evidence/document-readiness/quote-loading-390.webp) |
| First entry retained | [Ready](assets/evidence/document-readiness/invoice-ready-390.webp) | [Ready](assets/evidence/document-readiness/quote-ready-390.webp) |
| JavaScript disabled | Server-render unit contract | [Visible explanation](assets/evidence/document-readiness/quote-no-javascript-390.webp) |

[Evidence JSON](2026-11-07-document-readiness-evidence.json) retains all three readiness cases, six document viewport checks at 1440/390/320px, keyboard scrolling, pending dimensions, emulated print checks and four actual PDF hashes. All four PDFs remain byte-identical to the prior correct exports; one-page text/totals checks passed. The browser test uses real production modules and native disabled behavior, without injecting application state.

The independent Astra Max reviewer read the exact source diff and browser records, inspected seven actual loading/ready/document captures, and independently compared all four PDFs with the preceding exports. It marked the readiness issue resolved with disposition **ship**, finding no material layout or product regression in this bounded local batch. It did not execute browser/build commands or claim live deployment verification.

The site suite passes 95 tests, including server-rendered disabled controls and enabled client controls that retain the first name. Site typecheck/lint and root lint pass; root lint retains the existing 136 warnings and 54 informational diagnostics. Release preparation will retain the final exact commit and target-specific hashes separately. No physical device, screen reader, alternate engine or OS print dialog is claimed.

PR #48's integration and direct Worker deployment remain historical evidence for `f580456f`; this follow-up does not relabel that artifact. Cuttlefish registration, CI credentials and DNS migration remain separate pending work. The existing account, Worker, canonical origin and Sites fallback remain the intended release boundary.
