# Soft Precision financial workspace — 23 October 2026

This iteration styles the implemented invoice workflow on the reporting programme branch. It does not establish any new financial or release policy.

## Review the rendered app

The `before/` and `after/` folders contain real macOS arm64 ordinary Electron screenshots captured at 1440 × 1100 and 390/320 × 900. Both phases use the same isolated synthetic workspace and journey. Generated design boards are visual references only; they are not evidence of implementation.

The connected invoice workspace now has a full-width saved-invoice table, customer/number search, Draft/Issued filters and ten-row client pages over the existing complete cursor-loaded snapshot. The editor uses shared ivory surfaces, forest actions, soft serif display type, 44px controls and legible tabular data. Invoice totals are separate from private cost estimates; actual outstanding and received amounts remain in Income & payments. Textareas grow with content within a bounded height and retain their own vertical scroll.

There are no Paid/Overdue lifecycle statuses, inventory, bank feeds, payment sending, automatic customer delivery or ATO lodgement claims. Issuance and receipt posting follow the existing native contracts; all monetary calculations and authorization remain unchanged.

## Reproducible journey

`apps/desktop/tests/e2e/financial-workspace.spec.ts` runs with zero retries. `TAMMY_FINANCIAL_UI_CAPTURE=before|after` enables these review screenshots; ordinary CI runs the same assertions without saving historical output. No setup/authentication frames are captured.

1. Start the isolated accounting fixture and open an empty invoice workspace.
2. Enter a taxable $1,200 service with a private $500 cost. Reject a three-decimal selling price while preserving the edit, then save by double-clicking. Confirm the native record is version 1.
3. Start replacement with unsaved notes, keep editing, then reopen the saved record and explicitly discard the note. No saved version changes.
4. Open/close company template settings. Review issue confirmation, cancel with no version change, then confirm. The issued invoice becomes read-only and native outstanding is $1,320.
5. Review a $500 receipt, cancel with zero persisted payments, then save by double-clicking. Native storage must contain exactly one receipt, $500 received and $820 outstanding.
6. Cancel PDF export with no output, then export a real PDF into the isolated profile; verify its signature and 0600 permissions on macOS.
7. Navigate away, back and forward. Reopen the issued record and verify the persisted balance and disabled customer field.
8. Search to no results, clear explicitly and verify returned keyboard focus with a visible outline. Check 390px and 320px layouts with reduced motion and tables confined to their own scroll surfaces.

The initial capture harness attempts exposed incomplete synthetic addresses, a bank-select label lookup and React-updated textarea label matching. They were corrected against the actual contracts; financial validation and receipt behavior were not changed. The first `/tmp` output alias was rejected by secure local startup; the successful run used the repository-owned output directory.

## Supplementary static audit

The previous audit configuration referenced `src/renderer`, which the reporting branch moved to `src/app`. This iteration audits the actual current app source, including tests; it does not edit the old report or add exclusions.

At the reporting base the strict scanner reports 26 findings. This invoice pass resolves seven textarea geometry findings using bounded content growth and `resize-none`. The remaining 19 are classified rather than hidden:

- The original seven project exceptions remain: six native-validation forms (chart, journals, banking, documents, setup and unlock), plus the vertically resizable CSV input. The established UX contract takes precedence over supplementary defaults.
- Eight additional native-validation forms belong to the reporting/SBR branch: invoice editing, legacy rule reference repair, machine credential, organisation verification, Product ID, simulator and two TOTP forms. This visual change preserves their financial/security validation.
- Four actionless-button findings are literal mock controls in `bas-lifecycle-screen.test.tsx`, not enabled product buttons.

Strict audit is therefore **not green**. No native validation, security form or release gate was weakened to make it pass.

## Release boundary

The reporting branch now implements local invoice/purchase accounting, BAS review/freeze/export and simulator SBR readiness. It still has 77 future RPC and 111 future transition entries. Its shipping-surface checker change is a separate product boundary decision; this UI pass does not infer approval from passing source or hosted tests.

Ordinary macOS packaging and Windows Server smoke are not MAS, Windows 11 or ATO production qualification. App Store Connect authentication was verified; the last observed agreement banner requires action by the Account Holder. No legal agreement was accepted and no build was uploaded by this task.

## Verification detail

- The narrow regression first failed with a 783px root at a 390px viewport. The saved-table hidden header escaped its unpositioned scroll container. A positioned scroll owner corrected that cause without hiding page overflow.
- A stricter follow-up failed with `viewport=320, available=305, document=320`: the inherited root minimum width ignored the native vertical scrollbar. The root now respects available width; the test compares against `documentElement.clientWidth` after keyboard scrolling.
- The shared semantic `HorizontalScrollArea` is the focus/scroll owner for the four invoice tables. Its single, documented Biome exception permits the required keyboard focus on a labelled scroll region; no global lint rule was relaxed.
- One intermediate package was rejected as `SOURCE_MANIFEST_INVALID` because an untracked audit report existed during the build. That report was moved outside the checkout. Final evidence must use a clean-source manifest and the normal package verifier; the rejected package is not delivery evidence.

## Visual review entry points

| Milestone | Before | After |
| --- | --- | --- |
| Saved draft and customer details | [Before](before/03-saved-draft.png) | [After](after/03-saved-draft.png) |
| Validation with edits retained | [Before](before/02-validation-preserves-edit.png) | [After](after/02-validation-preserves-edit.png) |
| Issue review and separate private totals | [Before](before/05-issue-confirmation.png) | [After](after/05-issue-confirmation.png) |
| Receipt review and cancellation | [Before](before/07-payment-review.png) | [After](after/07-payment-review.png) |
| Confirmed received/outstanding amounts | [Before](before/08-recorded-receipt.png) | [After](after/08-recorded-receipt.png) |
| 320px navigation and saved records | [Before](before/11-invoices-320.png) | [After](after/11-invoices-320.png) |
| Keyboard access to narrow amount columns | — | [After](after/12b-line-amounts-320.png) |
| Search no-results and visible focus | — | [No results](after/09-invoice-no-results.png), [focus](after/10-keyboard-focus.png) |

The final search review also removed duplicate native/application clear icons. The shared review search now owns one named clear action and restores input focus across Documents, Banking and Invoices.

## Local results

All 133 affected-screen tests (17 files), desktop TypeScript, and lint passed. Lint retains 135 existing warnings and two informational diagnostics. The final ordinary package passed seven integrity checks with the one Windows-only check skipped on macOS. Four packaged journeys passed in 1.2 minutes with zero retries/skips. There are 13 before and 17 after screenshots; all 81 historical screenshots and 55 archived files remain byte-identical. Hosted CI is reported separately for the pushed exact head.

[Library delivery](library-delivery.json), [capture hashes](screenshots.json), [package provenance](after-package.json), [journey results](journeys.json), and [preservation check](preservation.json) are retained alongside the images.
