# Integrated Soft Precision journeys — 23 October 2026

These are 17 real screenshots from the ordinary packaged Electron app at clean source `7aa28d4e243a1bde1006234f013e3c468469ae2c`. They use isolated synthetic data, not a user's workspace. No generated concept image is used as runtime evidence.

The integration combines merged master `8fde3884` with the invoice and qualification work from draft PRs #9/#10. Shared headings and surfaces now agree, while saved-invoice search/filters, bounded growing fields, visible error focus, receipt review and keyboard table scrolling remain. The updated onboarding, ABN handling, credential feedback and published-policy content are preserved.

## Validation

- Desktop TypeScript and lint pass. Lint retains 135 existing warnings and two informational diagnostics.
- All **1,290 desktop tests in 109 files pass**, zero skips, in the verified native context.
- The script tier passes **789 tests**, zero failures and one expected archive-reproducibility skip. The unchanged process fixture's startup race was reproduced and corrected with a bounded readiness handshake; production timeouts are unchanged.
- Ordinary package verification passes seven checks; the Windows-only check is skipped on macOS. Package/source hashes are in [package.json](package.json).
- The complete local packaged run passes **19 of 20** cases in **11.2 minutes**, with zero retries. The dedicated visual journey passes in 18.3 seconds. The final SBR-readiness case fails during initial Core startup with `LOCAL_ENGINE_CORE_SPAWN_FAILED`; it is not counted as passed or skipped.
- Independent bounded Core probes pass for ordinary and simulator startup, including exact live image path/device/inode checks. A 202-byte synthetic data-root path also passes. These probes do not establish the cause of the original Electron failure or qualify the complete SBR journey. Runtime startup, authentication and profile code are unchanged from master; no identity check or deadline is relaxed.

The initial sandboxed desktop attempt failed its native Go-cache, loopback and Electron operations; the same three files subsequently passed all 21 tests outside the tool sandbox, followed by the complete 1,290-test passing run. The initial script run's missing-PID failure and the original SBR failure remain recorded. Private runtime artifacts are retained outside Git and are not delivered with this evidence.

## Journey observed in the app

The invoice sequence rejects a three-decimal price while retaining edits; saves one version under repeated clicks; keeps or discards unsaved replacements; opens/closes the template; cancels then confirms issuance; preserves issued fields as read-only; cancels a receipt without writing; and records one $500 receipt under repeated clicks. Native records reconcile invoice total **$1,320**, received **$500** and outstanding **$820**. PDF cancellation writes nothing; the confirmed export has a PDF signature and private file mode. Back/forward navigation retains the saved balance.

Search no-results and clearing restore input focus. Reduced motion disables transitions. At 390 px and 320 px, the page fits the available width and read-only amount columns remain reachable with keyboard horizontal scrolling. The passing financial journey reports no page or console errors.

## Review entry points

| Milestone | Previous uplift | Integrated app |
| --- | --- | --- |
| Saved draft | [Previous](../soft-precision-financial-2026-10-23/after/03-saved-draft.png) | [Integrated](after/03-saved-draft.png) |
| Recorded receipt | [Previous](../soft-precision-financial-2026-10-23/after/08-recorded-receipt.png) | [Integrated](after/08-recorded-receipt.png) |
| 320 px saved records | [Previous](../soft-precision-financial-2026-10-23/after/11-invoices-320.png) | [Integrated](after/11-invoices-320.png) |
| Keyboard amount columns | [Previous](../soft-precision-financial-2026-10-23/after/12b-line-amounts-320.png) | [Integrated](after/12b-line-amounts-320.png) |

All **111 prior screenshots and 55 archived files remain byte-identical**. The capture helper exclusively creates each new file. [Screenshot hashes](screenshots.json) and [Library delivery](library-delivery.json) bind the images to their source. The approved boards were inspected as design references; their mock financial values and planned workflows were not treated as app contracts.

This is ordinary local-package evidence. It does not establish MAS, Windows 11 or official ATO qualification. No PR was merged or marked ready, no App Store profile was changed, and no application release or upload occurred. Hosted checks must be assessed at the eventual exact pushed integration head.
