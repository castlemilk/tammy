# Banking · Soft Precision comparison

This block follows the Banking panel in approved board 08 at design commit
`19533020422b6ea2f0c77dc3117a051483f9f9ce`. The unchanged full concept is in
`reference/08-documents-banking-activity.png`; it is not application evidence.
All before/after captures use the real local Electron app and isolated synthetic
records. No credentials, recovery material, real records or runtime logs belong
in this folder.

## Comparison and supported behavior

The real baseline uses a large progress/balance band, approximately 77px rows and
an inline review form below the entire table and reconciliation footer. At
1440 × 1100, only the review heading fits beneath six statement lines. Board 08
uses a compact transaction list, nearby review controls and a restrained summary.

The change removes the oversized progress band, targets 56px rows, places the
selected non-modal review beside the list from 1380px, and stacks naturally below
that width. The existing shared scroller owns keyboard access to all columns.
Forest actions, oat/ivory surfaces, cocoa text, soft serif display and sans/tabular
data reuse existing tokens. No decoration enters financial data.

The summary uses only actual global unmatched/unreconciled counts and the latest
imported statement's closing balance. The table remains a loaded projection of
up to 200 lines. No opening/closing difference, book/cash balance, file metadata,
account selector, live feed, automatic match, document link or payment is invented
to imitate the concept. The existing three-column local CSV contract remains.

Import, match and reconcile handler source and the reconciliation gate are
byte-identical to review base `724ac6fe9e5b2cba0cee6870658196c575b58486`.
Expected versions, command keys, retry identity, confirmed-response checks and
refresh-failure write gates remain. Core and protobuf sources are unchanged.
Escape follows existing Close/Cancel semantics, ignores composition/pending work,
and restores focus. Re-selecting a line preserves its reference; confirmation or
error feedback receives visible focus after an action.

## Real journey

`apps/desktop/tests/e2e/support/banking-journey.ts` extends the existing financial
workspace packaged case. Capture mode runs Banking after the ordinary synthetic
workspace workflow; normal CI runs it after invoices, Overview and Documents.
It creates no new CI job, fixture authority or production capability.

1. Reject malformed CSV without importing; retain input and cancel without a
   write. Reopen and verify explicit cancellation cleared that draft.
2. Enter an opening balance of AUD 1,000 and five explicit signed transactions.
   Double-click import and verify exactly five new rows, six including the
   fixture's existing reconciled row, and latest import balance AUD 1,678.10.
3. Select a line by keyboard, cancel its reference without a version change,
   then reopen with search retained. Review fields and financial values stay
   visible together on wide screens.
4. Under reduced motion, inspect 390/320px containment and visible field focus.
   The after path adds 900px, explicit keyboard table scrolling, repeated-row
   selection, Escape return, confirmation focus and combined status/search checks.
5. Double-click Confirm match; verify exactly one version advance and the saved
   reviewed string. Recover from no results and explicitly review remaining rows.
6. Complete reconciliation explicitly; double-click protection retains one
   state transition, all five new rows reach version 3/Reconciled, global pending
   counts become zero, and the latest import balance remains AUD 1,678.10.
7. Read actual journal IDs before/after and verify no journal was posted. Exercise
   Overview → back → forward → back and verify the retained reconciled state.

Native service calls are not mocked in these captures. `inputs.json` contains
only synthetic financial inputs. Read calls verify outcomes; typed command writes
go through the visible app controls. Capture files use exclusive creation.

## Baseline and retained attempts

The successful baseline test took **10.7s** (**11.4s** including the runner), with
zero Playwright retries, page errors or console errors. Its **13 frames** are in
`before/`. Runtime source is clean ordinary package
`0f3ed9bd7151cda8757ab5e2874f95e343d78205`, whose renderer/Core source is identical
to the review base. `before-captures.json` records hashes and dimensions. The
capture helper was initially uncommitted and later extended; no initial helper
commit/hash is falsely claimed. File mtimes on copied attempt frames describe
their retained copies rather than an asserted original capture instant.

Three original attempts remain visible rather than overwritten:

- `baseline-attempt-1/`: the existing app accepted `2024-02-30`, then displayed
  no transaction date. Core's `banking.validLineInput` checks day 1–31 without
  checking the month's length; its date reader subsequently returns no date.
  This contradicts the documented Gregorian date contract. It is a separate
  financial-validation gap, not a successful error-path test or fixed behavior.
  This visual block preserves financial handlers/Core and does not repair it.
- `baseline-attempt-2/`: malformed CSV was correctly rejected with retained text;
  a label locator could no longer resolve the controlled textarea. Its accessible
  textbox role/name remained correct and is now used by the journey.
- `baseline-attempt-3/`: two partial frames and the import-success failure image.
  A generic status locator collided with the existing Local engine live region;
  confirmations now use their specific text. This was a test correction.

## Local checks and audit boundary

Nine focused Banking/scroller tests and typecheck pass. The keyboard regression
fails against the exact original Banking component (missing expanded review
state) and the changed source was restored byte-for-byte after that experiment.

The broad desktop run completed **1,307 passes and four failures / 111 files**
in **127.51s** inside the execution sandbox. Failures were in three unchanged
native integration files: two loopback `EPERM` failures, Core readiness exit and
Electron ASAR `SIGABRT`. Re-running **only those three files** outside that
sandbox passed **all 21 tests in 4.72s**, without a source fix or another full
suite. The interrupted-environment result is not relabeled as an uninterrupted
all-green local suite. Hosted results must be tied to the later exact head.

The strict configured audit retains the same **19 rule/file findings**: 14
native-validation forms, four literal test mock buttons and one vertical CSV
resize rule. The current product/UX contract intentionally preserves validation
and the CSV affordance. No exclusions or policy weakening were added.
`static-ui-audit-config.json` is a byte-identical copy of the previously tracked
integrated-app configuration, SHA256
`cefae3a8a46a80fa356b029dd866ac2b12a38c08af3eee81434a8be6161af9ea`.

Two diagnostic scans are retained too: the unconfigured monorepo scan reports
61 findings including downloaded SQLCipher demo HTML and undecided native owners;
an obsolete temporary config points to the former `src/renderer` directory,
falls back to the root and reports 42. Neither is represented as the configured
product audit. The obsolete config copy is formatter-normalized, with unchanged
JSON meaning. The tracked configuration covers the current `src/app` UI root and
explicit native date/select ownership from UX-CONTRACT.md.

`preservation.json` verifies every **666 prior tracked output PNGs** against the
review base. All **55 archived files** also verify unchanged. Original invoice,
Overview, Documents, failure and reference evidence is retained.

## Retained CI timing contract

[Measured outer budget](../../../docs/development/ci-evidence/macos-budget-2026-10-23.md)
and [separate Task provisioning diagnosis](../../../docs/development/ci-evidence/task-provisioning-2026-10-23.md)
retain the original jobs and timestamps. The slow comparison reached SBR at
25m54s; its unchanged 8m test deadline plus a 2m artifact reserve needs 35m54s,
rounded to the finite **40m macOS job envelope**. Task has its separate **5m
acquisition limit**, with 10s connection / 120s transfer bounds, pinned checksum
and exact-version verification. Application readiness and tool provisioning are
distinct. No budgets, assertions, worker counts or retry policies change here.

The preceding review base passed all six jobs on their first attempts in
[push 40002560325](https://github.com/castlemilk/tammy/actions/runs/40002560325)
and [PR comparison 40002566685](https://github.com/castlemilk/tammy/actions/runs/40002566685),
20 packaged cases per macOS job. That is evidence for that base only, not this
new visual code. Current after captures, package and exact-head hosted outcomes
will be recorded after their actual validation.

Draft, merge and publication holds remain. Representative Banking fidelity does
not complete the remaining screen inventory. The original local macOS 26.5.2
SBR startup and second-unlock discrepancies remain; ordinary macOS 14/Windows
Server smoke do not establish MAS, Windows 11 or official ATO qualification.
