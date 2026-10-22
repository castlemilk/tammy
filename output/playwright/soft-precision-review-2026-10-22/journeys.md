# Soft Precision review workspace — real app evidence

This pass implements Documents → Banking and the shared navigation shell. The
approved references are boards 03, 08, 09 and 11 at design commit
`19533020422b6ea2f0c77dc3117a051483f9f9ce`; their actual repository PNG pixels were
inspected. Concepts are references, never runtime evidence.

## Baseline and isolation

Remote master was `14e502727155951f8468cc0d331cd2847aeaad56`; no open PRs were
reported at the new-pass checkpoint. Work is isolated on
`ui/soft-precision-review-workspace-2026-10-22`. The original checkout and the
concurrent `issue/sbr-registration-ready` worktree were read-only throughout.
That separate worktree advanced from `6a08ccce` to `7b47ac5a` during this pass;
its changes and unmerged capabilities were not imported here.

The 16 `before` PNGs render master 14e. The package source is test-only capture
commit `9c0312eb4f0ee58431419fbf1ed0c4c76243a29e`. `baseline.json` records exact
hashes/dimensions. Both packaged baseline journeys passed (33.0s), as did four
package signature/resource checks. The prior 41 screenshots and 55 archived files
remain byte-identical. No tracked .pnpm-store paths were changed or added.

## Implemented decisions

- A full-width document table owns search/status filters and precise AUD columns.
  A separate evidence/review surface stacks on narrow screens. Retained native
  text is labelled honestly; no PDF/image preview API was invented. Confirmed
  reviews become read-only; Cancel changes restores the retained values.
- Banking starts with empty user inputs. Close import preserves the current
  in-screen draft; Cancel discards it without a command. The table owns the main
  surface, with actual summary counts and a latest-import balance.
- A manual reference is explicitly entered before matching. Matching is distinct
  from reconciliation and does not create an accounting entry, payment or source
  link. Version and command-key contracts remain intact.
- Repeated submissions are synchronously guarded. Unchanged failed requests
  retain the same idempotency key. A confirmed command followed by refresh failure
  reports the saved action and blocks further writes until refresh succeeds.
- Existing routes are grouped in a calm navigation shell, with a forest selected
  state, route context and compact accessible rail. New forms use normal document
  scrolling; only dense tables scroll horizontally.

## Journey actions

| Milestone | Real action and assertion |
| --- | --- |
| 04 / 05 | Read the same retained synthetic document / reconciled statement at 1440 × 1000. |
| 11 | Reject a synthetic text file; supported formats and size guidance remain visible. |
| 12 / 13 | Upload a synthetic PNG, enter a review, submit an overlong supplier to the actual core, retain failed edits, correct it, double-click Save and confirm read-only reviewed state. |
| 20 / 21 | Reject invalid CSV; import a signed synthetic row with an explicitly entered opening balance; completion stays disabled while unmatched. |
| 24 | Close and reopen an import draft; verify the draft persists. Cancel it, reopen, and verify empty inputs. |
| 25 | Open explicit match review; enter a reference; cancel without matching; reopen empty; enter a new reference and double-click Confirm. A separate narrow capture exercises the form at 320px. |
| 22 / 23 | Confirm matched state, then double-click reconciliation and verify completed state plus exact closing balance. |
| 26 / 27 | Search to no results without losing retained records; clear search; close the review surface. |
| 28 / 29 | Inspect the genuine empty document / banking states before any accounting fixtures are created. |
| 18 | Tab from Overview to Documents; verify actual solid keyboard outline. |
| 19 | At 320 / 390 / 900px with reduced motion, verify no page horizontal overflow on Documents, Banking and Trial balance; verify navigation transitions are disabled. At 320px, open and cancel an import and verify the complete balance never wraps its cents. |

The connected accounting regression also opens account/journal forms, cancels
without writes, returns from journal detail, and uses browser Back/Forward through
local activity. API fixtures are synthetic and isolated in the test-owned user
profile. No credentials, recovery codes, userData, traces, videos or runtime logs
are included in this evidence directory.

## Verification checkpoints

- Full native desktop suite: **399/399 tests, 35 files**, 32.72s. A first restricted
  run could not read the Go build cache or launch the Electron harness; the native
  execution passed without changing tests or production safeguards.
- Focused document/banking regressions: **7/7**, including cancel, retry command
  identity, read-only confirmation and confirmed-import/failed-refresh handling.
- Typecheck and complete lint passed; lint verified all 55 archived hashes.
- Descriptor/build/archival regression selection: **52/52**.
- Initial new-UI package: all four signature/resource checks passed. The first
  current-workflows launch closed before any window/action; the separate foundation
  test passed (10.3s). A single targeted diagnostic rerun passed the entire current
  workflow (22.1s). No build or app-safety setting was relaxed for the rerun.
- Rendered review identified a toolbar wrap and narrow long-balance wrap; both
  were corrected before the final capture package. Final results follow below.

## Supplementary static audit

`static-ui-audit-config.json` limits this application audit to its maintained
renderer and records the existing native select/date ownership. A naive repo-root
scan additionally sees vendored SQLCipher examples and packaged Chromium licence
HTML; those are not application UI and were not changed.

The scoped strict result is **seven findings, zero unresolved owners**. Six
existing forms retain native validation; the CSV field remains vertically
resizable. These choices preserve the existing product contract. The new match
reference form has explicit inline validation. No exception was hidden or blanket
lint exclusion added; this is not a strict-audit compliance claim.

The local package is macOS arm64 development evidence. Hosted Windows Server smoke
is not Windows 11 release qualification. No application was released or deployed.
