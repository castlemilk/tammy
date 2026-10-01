# Company invoice template acceptance

Validated on 1 October 2026 using the ordinary macOS arm64 desktop package built from clean source revision `4c29e40340d18dc662268f07dd771e1661ba72f4`.

## Verified behavior

- Invalid logo upload preserves template entries. A real transparent PNG uploads, previews, saves and survives restart.
- Saving company defaults leaves the current invoice unchanged. Explicit application updates its seller address, notes, payment instructions, due date and logo while preserving customer details and lines.
- New invoices inherit updated defaults and terms. Previously saved drafts and issued invoices retain their own text and logo after defaults change or the template logo is removed.
- Draft and issued exports embed the saved image and payment text, retain their accounting totals and exclude private costs, profit and source/asset identifiers.
- Issued invoice fields and invoice logo removal remain unavailable for editing.
- Independent review of the actual desktop and 1024×700 template screenshots and the sample PDF found no material clipping, overlap or alignment problems.

## Validation results

| Check | Result |
| --- | --- |
| Desktop Vitest | 783 tests, 69 files passed |
| Invoice calculations/PDF Vitest | 32 passed |
| Local API Vitest | 32 passed |
| Generated client contracts Vitest | 24 passed |
| Desktop, invoice, local API and generated client TypeScript | Passed |
| Protobuf formatting, lint, breaking compatibility, generation and contract gate | Passed |
| Go default suite | Passed |
| SQLCipher race suite | All packages passed across full run and focused overview rerun |
| Packaged first-run journey | Passed, 1.5 seconds |
| Packaged SBR simulator regression | Passed, 4.9 minutes |
| Packaged accounting/invoice workflow | Passed, 14.2 seconds after selector correction |
| Package signature, fuses, ASAR/core/resource checks | 7 passed; Windows check skipped on macOS |
| Repository lint and whitespace | No errors; existing repository lint warnings remain |

The initial full race run exposed an existing overview fixture that migrated only to version 5 while reading banking tables. The fixture now migrates to version 9; its focused race rerun passed in 2.866 seconds. All other packages passed the full race run, including invoices, localproduct, organisations, identity and workspace.

The initial packaged invoice test used exact label-text selectors for controlled textareas. A headless Chromium reproduction showed that textarea content becomes part of `label.textContent`, while the accessible textbox name stays stable. The E2E selectors now use textbox role/name matching. The complete invoice workflow then passed against the same production package; no implementation change was needed.

## Packaged artifact

- App SHA-256: `f0ba37f53229937aa67b2d7c214964d03eef88723621abea8d85e1c13b8c2c58`
- Core SHA-256: `c91a1e65b8631f4b1fa2d2024ff8bfcc09eaf48ea2f57dfb4dc105934d54ecf2`
- Manifest SHA-256: `0d4e161509fa8a6990a06d0ff55716e9be442941bc5525bc376f4491874abbc4`

The local package is at `.tmp/electron-builder/ordinary/mac-arm64/Tammy.app`. The screenshot evidence is under `.tmp/invoice-cost-table-e2e/`. These generated artifacts are intentionally ignored by Git.

## Dedicated fictional demo

The sample at `/Users/benebsworth/projects/tammy/output/tammy-invoice-demo/` contains three reviewed synthetic expenses, saved company defaults and a logo, and editable draft `TEST-2026-001`. Its subtotal is $150.00, GST $10.50 and total $160.50. The exported PDF is one A4 page with embedded image content and the expected payment instructions. Restart retained all reviewed expenses, source links, totals, the saved draft and logo; no renderer console errors occurred. Desktop sizes 1440×900 and 1024×700 had no horizontal page overflow.

The active sample profile is `user-data-company-template`; `open-demo.sh`, `README.md` and `verification.json` identify it. The earlier `user-data` profile is preserved. That older profile stalled before readiness after the ad hoc package rebuild. Its catalogue and attempt-journal HMAC chains validated; blocking platform anchor access remains an unconfirmed candidate. A fresh profile was prepared rather than resetting or modifying the earlier authentication journals.

Raw local validation logs use the `/tmp/tammy-invoice-branding-…-20261001.log` prefix, including `desktop-tests-final`, `contracts`, `package`, `packaged-e2e`, `packaged-workflow-rerun`, `overview-race` and `demo`.
