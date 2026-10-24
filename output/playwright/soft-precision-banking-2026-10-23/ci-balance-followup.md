# Banking balance · original CI failure and narrow correction

The original head `0fa0ab38bd5638fefc9fe2f337629cec9bb4de4f` failed both
macOS jobs on attempt 1. This is a real presentation regression introduced by
the Banking summary, not a provisioning or SBR timeout.

| Original execution | Outcome |
| --- | --- |
| [Push 40014591250 / 120623273926](https://github.com/castlemilk/tammy/actions/runs/40014591250/job/120623273926) | Four passed in 5.1m; case five failed; 15 not run |
| [PR 40014599613 / 120623303099](https://github.com/castlemilk/tammy/actions/runs/40014599613/job/120623303099) | Four passed in 5.0m; case five failed; 15 not run |

Both fail the existing `current-workflows.spec.ts:1211` assertion: at 320px,
`$123,456,679.01` must have `white-space: nowrap`; the new summary instead
inherits `normal` and explicitly permits word breaking. The prior validated
`724ac6fe` passed that unchanged assertion in both original 20-case runs. Task
acquisition took one second in each failed Banking run. Ubuntu passed 1,311
cases/typecheck/lint and all 55 archive hashes in both; Windows Server passed
1,310 cases plus one expected skip/typecheck in both. Windows Server is not
Windows 11 qualification.

[Original retained artifact](https://github.com/castlemilk/tammy/actions/runs/40014599613/artifacts/12528482057):
1,598,495 bytes; ZIP SHA256
`c8c7771ae14f1aae6e422a36f52f091cd23529baab4c5a4a855a947f3426341b`.
Its hash was verified before extracting only the failure PNG into a private
temporary directory. The inspected synthetic 320 × 1000 image shows the import
form, scrolled above the off-screen summary; the precise computed-style failure
comes from the original assertion/log, not a claim that the amount is visible in
that image. No trace, runtime/private log or authentication material is copied
into this repository.

## Correction and finite validation

The balance reuses the existing `HorizontalScrollArea`, with a descriptive region
name, full-value no-wrap text and the established narrow 18px font. Its containing
summary item is bounded to available width. Oversized values can scroll within
the labelled region; the page retains its own width. Shared left/right keyboard
handling and visible focus remain the canonical owner. The financial formatter,
import/match/reconcile handlers and reconciliation gate are unchanged.

The original native no-wrap assertion is retained. Additional assertions check
18px narrow text, the full named balance region, actual focus, visible outline
and page containment. The existing Banking capture journey checks balance
no-wrap at 900/390/320px; corrected captures use a new `after-money-fix/` directory,
so all previous captures and their delivered links remain unchanged.

A new component regression supplies a large exactly representable balance,
verifies its complete text and keyboard region, and confirms no financial write.
It fails against the exact original `0fa0ab38` component because the named
keyboard region is absent; the changed source was restored byte-for-byte after
that protected experiment (SHA256
`9731df4cbb69be391d81bf88db08d8150c5eaa301508e3960ea5c666027358dd`).
The corrected component/scroller run passes **10/10 in 2.70s**. Typecheck and lint
pass; lint remains **135 warnings and 12 infos**, without exclusions or broad
formatting. Native package, original connected case and new captures still need
verification at the corrected clean source before hosted claims are made.

No timeout, worker, retry, CI budget or assertion policy changes. No manual retry
or full-suite dispatch was requested for the failed head. The next normal push
must be verified independently at its exact remote commit.

## Separate pre-existing precision finding

The first new test used `9_007_199_254_740_991` minor units and expected
`$90,071,992,547,409.91`; the unchanged formatter displayed
`$90,071,992,547,409.90`. Its `Number(value) / 100` conversion loses a cent at
that magnitude. This was an actual failed focused test (nine passed, one failed),
not a passed financial-range qualification. The layout regression now uses
`9_000_000_000_000_000` minor units / `$90,000,000,000,000.00`, which is exactly
representable and still stresses value width. This records, rather than repairs
or conceals, the existing financial display limitation. Core values and handlers
are unchanged. The separately recorded invalid-calendar-date acceptance also
remains unresolved; neither finding is release-qualified by this visual block.

## Corrected native verification

The clean ordinary package at **`4ce093a93422121cf7e0492456a20a7b0dcf2344`**
passes seven checks and one expected Windows-only skip; Core/helper hashes remain
unchanged. The first focused native attempt passed the original no-wrap and new
18px assertions, then failed the added outline assertion after programmatic
focus (**22.5s**). This was a test-input mistake: the canonical focus ring follows
`:focus-visible`. The test now enters the region with actual Shift+Tab/Tab before
asserting visible focus; no application change or rebuild followed that correction.

The same original packaged case then passed **13.8s / 14.6s including runner**.
The Banking capture passed **12.8s / 13.8s**, and the connected invoice → Overview
→ Documents → Banking case passed **15.8s / 16.8s**, with zero retries, page errors
or console errors. The capture helper is unchanged from clean source `4ce093a9`;
the exact current-workflow harness hash binds the subsequently committed two-key
assertion correction in `after-money-fix-captures.json`.

All **715 previously tracked output PNGs** verify byte-identical against the
failed head; all 55 archived files verify unchanged. The strict configured audit
still has the same **19 rule/file findings** (not a green audit). New evidence is
15 Banking frames plus one 320px full-page original-regression capture. The latter
visibly shows the complete `$123,456,679.01` amount on one line with keyboard focus;
its 320 × 1000 viewport produces a 320 × 2162 full-page PNG. Earlier after sets,
metadata and Library items are preserved. New original hosted outcomes remain a
separate exact-head verification step after the normal push.
