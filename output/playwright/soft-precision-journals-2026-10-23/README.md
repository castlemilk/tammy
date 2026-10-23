# Journals · Soft Precision board 09

One implemented screen family: journal history → two-sided manual entry → posted
read-only detail. Review base: `22a97dc6a0c3790a0b404775196ae342f8e3d311`.
The actual unchanged board 09 comes from approved design commit
`19533020422b6ea2f0c77dc3117a051483f9f9ce`; it is a generated concept, never app
evidence. Board 08's list/detail pattern is the companion history reference.

## Current verified comparison

The final `after/` set is **14 real frames** from clean ordinary runtime
`ffc2fdaa7e8a74092d78025107df4a620516c369`. [Open the matched comparison](comparison.html)
for the identical 1440 × 1100 editor pair, unchanged reference, posted detail and
narrow keyboard states. All earlier attempts/intermediate sets remain unchanged.

## Actual comparison and contract

The inspected baseline stacks a loose form above a separate history panel; it
has no explicit two-sided preview, table description actions are non-semantic rows,
and an unavailable selected journal falls back to the history. History rows are
already fairly compact; no claim that they were 77px or universally oversized is
made. The structural form/history competition is the main mismatch.

The change makes entry the active surface while open, aligns the two real ordinary
accounts beside debit/credit readouts, and adds a quiet forest totals strip. Its
numbers use the existing exact bigint parser/formatter. The visible Post journal
action retains its existing direct-post meaning. There is no invented JN reference,
Add line, approval stage, reversal button, draft storage or source association.

History keeps one attached compact search/refresh rail and explicitly labels its
up-to-200 loaded projection. Search is transient/local and retained through detail.
Native buttons replace clickable table rows; read-only detail has aligned amounts,
a forest totals row, and explicit loading/unavailable recovery. Shared Button,
Badge, WorkSearch, WorkState, WorkNotice, ScreenHeading and HorizontalScrollArea
remain the owners. Runtime palette/type/radii are unchanged; compact-ledger-table
is shared geometry for these two accounting tables, not a new token set.

Cancel retains the existing in-screen draft; Escape follows that behavior with
composition/select/pending guards. Focus returns after cancellation/detail and
moves to persistent posting errors. The new synchronous in-flight guard blocks
concurrent submit events. Apart from that guard, the original post-handler source
is byte-identical: request fields, exact amounts, command key generation, native
validation, response checks, refresh and destination remain. This does not claim
new interrupted-response mutation replay semantics.

## Synthetic native baseline

The baseline reuses verified clean ordinary runtime
`4ce093a93422121cf7e0492456a20a7b0dcf2344`; its entire application source is
byte-identical to review base `22a97dc6`. The new test helper was uncommitted during
baseline, with its exact hash retained in `before-captures.json`. It uses the
existing synthetic workspace setup and real Core; no native service mocks.

The complete baseline passed **10.9s / 11.7s including runner**, zero retries,
console errors or page errors. Its **11 real frames** include a four-record history,
matching AUD 200 draft, invalid same-account rejection without a write, retained
cancelled values, 900/390/320px focus/containment, a double-click producing exactly
one additional two-line journal, exact equal debit/credit totals, read-only detail,
actual keyboard table scrolling and browser back/forward.

The first attempt took **39.1s** and stopped before posting at an exact label
locator for the native Debit account select. The accessibility snapshot exposes
its correct combobox name and expected options. Switching to the role/name locator
completed the baseline without an app change. The initial frame, inspected safe
failure PNG and attempt metadata are preserved in `before-attempt-1/`; private
runtime logs/authentication material are not copied into this repository.

## Focused implementation checks

**12 focused Journals/scroller tests pass in 2.64s**, including cancellation/focus,
composition guard, one exact balanced request under repeated submit, retained
posting failure, search/detail restoration, explicit unavailable detail, initial
loading/empty, read retry and exact large posted amounts. The existing financial
formatter correctly renders `9_007_199_254_740_991` minor units here; this is
component display coverage, not a full platform/financial-range qualification.

Replacing only the handler with the exact previous `22a97dc6` handler makes the
repeated-submit regression fail with two calls instead of one. The original handler
SHA256 is `5dbc78bfc720a67bf6115f7a8c0901d098dd388627310717af8e789199829ec4`.
The protected experiment restored the changed source byte-for-byte, SHA256
`ad44ca5b18ced4bdc4ad08adf230403ee736a3cc9a95564acbf5941213d6e0a6`.
No Core writes occur in that component experiment.

Initial new test assertions exposed two harness details, not application failures:
user-event normalizes a number input's 15.50 to 15.5, and inherited fieldset
`disabled` is tested through `:disabled`, rather than the input's own property.
The initial focused result was six passes/three failed assertions; corrected
expectations passed nine before the three additional state/range cases passed.
Typecheck caught an exact-optional prop, unavailable Badge variant names and a
Testing Library-only unsupported `exact` option; those are corrected. Final
Typecheck and lint pass, with the unchanged **135 warnings /12 infos**. All 55
archived files verify. The initial package/after verification and later visual corrections are retained
below; exact-head hosted evidence is separate from local checks and visual review.

## Scope and remaining work

Prior invoice, Overview, Documents and Banking comparisons remain unchanged.
Dedicated comparisons still remain for workspace entry/recovery, Chart of accounts,
Trial balance, GST & BAS, Audit trail and Settings/Organisation. SBR remains with
its active functional owner. Future/unconnected features are not inferred from
boards or source-only APIs.

The existing invalid-calendar-date statement acceptance, extreme-value Banking
formatter gap, 19 strict audit findings and local startup discrepancies remain
separate. No release, MAS/Windows 11/ATO qualification, ready transition, merge,
upload, signing/profile/security change, credentials or spending is included.

Final pre-package strict audit retains the identical **19 rule/file findings**, using
the unchanged canonical config. No exclusion, native validation change or cache
cleanup. `preservation.json` verifies every **731 prior tracked output PNGs**
byte-identical; all **55 archives** verify through the repository lint owner. The
Escape date/select guards have component-event coverage; native OS popup geometry
and real OS IME qualification are not claimed.

The app and app-shell regression suites also pass **26/26 in 3.84s**. The final
pre-package lint run covers 657 files with the same 135 warnings and 12 infos.

## First rendered after and bounded breadcrumb correction

Clean runtime `4abab3e0c3c1f18479b64eb5f1f312c0971e30f9` passed seven package
checks with one expected skip. The first after journey passed **10.7s /11.4s**;
the complete invoice → Overview → Documents → Banking → Journals case passed
**14.8s /15.5s**, zero retries/page/console errors. All **13 initial after frames**
and measured geometry are retained in `after-initial/`, bound by
`initial-after-captures.json`. Actual editor, detail and 320px pixels were inspected.
The editor is 1120 × 459.5 at viewport 1440 × 1100; minimum controls are **44px**
and totals use canonical `rgb(231, 237, 226)`.

The detail PNG exposed the pre-existing shell breadcrumb fallback to Workspace for
`/accounting/journals?journal=…`. Navigation already strips the query for its active
state; AppShell did not do so for its label. The correction aligns the shell's
label comparison with that existing rule. Route validation, queries, journal IDs,
financial/security handlers and navigation destinations are untouched.

The new standalone shell test initially omitted the canonical Conveyor test-fixture
setup, then corrected that harness requirement. In a protected experiment against
the exact original shell, the real regression fails because the Journals breadcrumb
is missing while its navigation link is active. The corrected shell was restored
byte-identically (SHA256
`54adb169f0bddad2def440bc12e48b99f8f688954e872122d881f62416714b02`).
The final app/app-shell/router block passes **36 tests**. A new native assertion
requires the posted detail's actual breadcrumb to read Journals. Corrected captures
will come from the next clean package; no initial image is silently relabeled.

## Narrow recovery feedback correction

Clean breadcrumb runtime `4b2f65c58cd99fe468dd4f7377b458063dbbf3ce`, with
clean test-only harness `010a61d68da340d71425a0e9a4bae297647723d5`, passed
seven package checks/one expected skip and the expanded native journey in
**10.7s /11.4s**, zero retries/page/console errors. Its **14 real frames** are
retained in `after-breadcrumb/`, with exact provenance in
`breadcrumb-after-captures.json`. The native case verifies the actual Journals
breadcrumb, manual/posted/version-1 response and reachable Tab-focused posting
action at 320px.

The inspected narrow footer image revealed a stale same-account validation alert
after correcting that account selection. Field edits now clear previous form
feedback while retaining all other draft values. No command is sent by editing;
the post handler, financial calculations and retry identity remain unchanged.
The focused regression fails on the prior source because the alert remains;
the corrected case requires no alert after correction and zero posting calls.
The native journey now asserts that same recovery before its narrow captures.
Earlier images are retained; a final clean package/capture will verify the actual
corrected state rather than hiding that observed defect.

## Final local verification and delivery

Final clean ordinary source **`ffc2fdaa7e8a74092d78025107df4a620516c369`**
passes seven package checks, one expected Windows-only skip; Core/helper hashes
remain unchanged. Final capture journey **11.3s /12.1s including runner**, full
connected invoice → Overview → Documents → Banking → Journals **15.1s /15.8s**,
and the original workspace/recovery/accounting/interaction case **13.3s /14.1s**.
All use zero retries, page errors and console errors. Actual original-size editor,
posted-detail and 320px footer pixels were inspected, including the correct
breadcrumb, clear feedback after correction and fully visible Tab-focused posting
action. Passing checks are separate from representative visual comparison.

The final focused suite passes **13/13 in 2.35s**; the app/app-shell/router block
passes **36/36 in 4.16s**. Typecheck and lint pass, with the existing 135 warnings
and 12 infos. The final strict audit retains the identical **19 rule/file findings**
without new exclusions. Old/new guard, breadcrumb and stale-feedback regressions
are documented above; the financial post handler is unchanged except its in-flight
guard. Descriptor bytes and coverage will remain unchanged when source provenance
is rebound before the scoped push.

Confirmed Library deliveries:

- [Before editor · real 1440 × 1100](https://chatgpt.com/api/library/files/libfile_fac0d627bfc88191a6d43cb1b2273145/download)
- [Approved board 09 · unchanged generated concept](https://chatgpt.com/api/library/files/libfile_d5b9bb9901ec8191867a34509b32c2bf/download)
- [After editor · real 1440 × 1100](https://chatgpt.com/api/library/files/libfile_92db7db190e48191a5464031ec7ead51/download)
- [Posted detail · real 1440 × 1100](https://chatgpt.com/api/library/files/libfile_14774322cd508191a4fdb61a98f3054e/download)
- [320px keyboard posting · real](https://chatgpt.com/api/library/files/libfile_daccc6f6bfe48191843bb9418f6cfb38/download)

`library-delivery.json` records confirmed IDs, bytes and SHA256; returned version
attributes were applied and verified only on transport copies outside the repository.
Capture bytes remain unchanged. `after-captures.json` binds the final runtime and
clean harness; `final-preservation.json` verifies all **772 PNGs tracked before
this final set**, including every original **731** predecessor. All **55 archives**
also verify. Final total: **786 tracked output PNGs** once committed; this folder
contains **54 real frames plus one unchanged concept**.

The old first/intermediate sources and failed assertions are not relabeled as final
passes. The narrow footer is fully visible in the inspected original 320 × 1000 PNG;
compressed previews are not used to infer clipping. This block adds no new CI job,
worker, retry, timeout or authority. Existing exact-head push/PR CI must be verified
independently after the normal push; terminal outcomes belong to the draft PR record.
Draft/merge/publication holds, concurrent owner work and all previously stated
financial/audit/platform limits remain.

The final evidence lint initially found two semantic-markup issues in the comparison
report's scroll wrapper. Using a named section with its existing keyboard-accessible
links resolves both; no app source, screenshots, validation rule or lint exclusion
changed. The corrected evidence-only check and repository lint pass.
