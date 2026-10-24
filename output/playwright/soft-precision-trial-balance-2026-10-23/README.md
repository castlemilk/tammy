# Trial balance — board 09 comparison

The approved reference is unchanged board 09 at design commit
`19533020422b6ea2f0c77dc3117a051483f9f9ce`. It is a generated concept, kept
separate from actual ordinary-package captures. The relevant pane is lower left.

## Before and implemented scope

The baseline has a duplicate Account balances header above 50px report rows and
an untinted totals row. It shows the date in body copy; the concept places it
beside the display heading and makes the ledger/totals the dominant surface.
Its loading/error copy incorrectly recommends posting a journal for every failure.

The existing screen supports one authenticated, read-only as-of-today query.
It has no connected date picker or report export. This block adds neither.
`proto/tammy/v1/accounting.proto` owns the inclusive civil date, server-ordered
lines, complete response and authoritative debit/credit totals. Query/state logic
and the bigint money formatter are byte-identical to review head `3defba91`.
Financial/source exports elsewhere remain untouched.

The new report places its read-only date and explicit read refresh beside the
heading, removes the duplicate header, aligns compact 40px ledger rows and shades
the totals band. Account cells are semantic row headings. Equality compares only
the two provided totals; it does not recompute or certify the books. Unequal or
missing totals receive distinct labels. Existing fallback amount rendering is
unchanged. Shared WorkState, Button and HorizontalScrollArea own state, controls
and keyboard scrolling. The inset focus treatment is scoped to this report.

## Synthetic native baseline

The existing isolated workspace workflow is reused. Two fixture-only ordinary
accounts and three actual journals cover the day before, as-of day and following
day. The four-row primary report has exact AUD 3,319.00 debit and credit totals;
AUD 777.77 posted the following day is excluded. No values are mocked into the
rendered report. The current Core response is verified before/after read navigation.
Fixture dates follow the existing frozen renderer date, 12 May 2024, rather than
copying the concept's generated dates. Account IDs, names and protection never
come from the image.

The baseline passed 10.1s /10.9s including runner, zero retries/page/console errors.
Seven real frames cover 1440px, 900/390/320px keyboard focus, credit/totals scrolling
and back/forward. Runtime is clean `9bf430a7`; its Trial balance code is identical
to `3defba91`. The newly added test helper was uncommitted during this capture;
no clean harness binding is fabricated. [Exact frame/runtime hashes](before-captures.json).

## Focused validation before package

Eight report tests plus three shared scroller tests pass. The loading regression
fails on the original component (one failure, seven intentionally unselected tests),
then the changed source is restored byte-identically. Its original request/state
block and exact bigint formatter are verified byte-identical. Coverage includes
authentication/local civil-date boundaries, authoritative totals that differ from
visible rows, unequal totals, signed 64-bit formatting and negative cents, pending
repeat refresh, failed-read recovery, empty success and malformed response. These
are component/contract fixtures, not claims that Core returns invalid accounting.

The supplementary strict audit still has the same 19 rule/file findings: 14 existing
native-validation forms, four literal test mock buttons and one CSV resize rule.
No new exclusion or validation weakening is included. The first local typecheck
caught optional fixture fields and the first focused run caught an expectation of
property presence for an absent protobuf optional; those test-only issues were
corrected before native after qualification.

This initial commit precedes the clean package and after comparison. Passing tests
alone do not establish visual fidelity. No MAS, Windows 11 or official ATO evidence
is claimed. The previous ASAR failure recovered after one retry at `3defba91`;
its delayed phase remains unresolved, and this screen does not fix it.

Remaining after this one block: workspace/recovery, GST/BAS, Audit,
Settings/Organisation. Draft, merge and publication holds remain.
