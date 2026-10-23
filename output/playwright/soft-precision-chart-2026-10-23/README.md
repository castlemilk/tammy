# Chart of accounts · Soft Precision board 09

One finite screen block from review base
`8cb09e34d16ebacbf610eead1cb8494546b2a13f`. The unchanged generated
[board 09](reference/09-accounting-core.png) is a visual reference, not app evidence.
Its original design commit is `19533020422b6ea2f0c77dc3117a051483f9f9ce`.

## Baseline and actual mismatch

The real baseline is an ungrouped four-column table with a repeated green Active
badge, a separate Accounts subheading, no code/name search and no displayed
System/Control ownership. The approved concept uses compact grouped rows, quiet
hierarchy and conspicuous protection. The file name `02-grouped-chart.png`
identifies the comparison point; its baseline pixels are truthfully ungrouped.

Baseline uses the verified ordinary `ffc2fdaa7e8a74092d78025107df4a620516c369`
runtime. Its Chart application source is identical to review base. The new
uncommitted helper and parent test hashes are recorded in
[before-captures.json](before-captures.json), alongside all ten real frames.
The baseline passed **11.1s /12.0s including runner**, zero retries, page errors
and console errors, using only isolated synthetic records and real Core.

The journey starts with the actual installed Control/System accounts and the
ordinary expense/equity fixtures. It creates four additional ordinary accounts
through the UI, producing the same **18-record** list for before/after comparison
at **1440 × 1100**. It checks retained cancellation, rejected duplicate code 1000
without a write, corrected input, 900/390/320px containment and visible Tab focus,
one account after repeated Save, actual horizontal keyboard scrolling and browser
back/forward. Native reads verify all protected accounts remain byte-equivalent
and the original journal IDs do not change. No real financial data or credentials
are used.

## Implementation and contracts

The list groups the existing primary account classifications and sorts copied
rows naturally by code within each group. All eight known types and an explicit
Unclassified fallback remain visible. Native buttons disclose each group with
expanded state and keyboard activation. Code/name search is transient and checks
only the existing **up-to-200 loaded projection**; changing search reveals its
matching groups. Clear returns focus. The UI does not claim whole-chart search,
new server paging or persisted view preferences.

Protected Control and System ownership is explicitly labelled with restrained
warm badges; ordinary ownership and Active/Archived lifecycle stay separate.
Read-only rows do not imply an edit/archive capability. Creation still offers only
the original five types and creates an ordinary account. No control-account editing,
new account lifecycle command, balance, report or ledger posting is invented.

The financial request, decoded response validation and normal-balance function are
byte-identical to the base, verified in [contract-verification.json](contract-verification.json).
The same authenticated command fields, UUID generation, classifications and retry
identity semantics remain. The synchronous guard only blocks concurrent submits;
this does not claim new replay guarantees for an interrupted response.

Native required/length/select constraints remain. Cancel and Escape retain the
in-screen draft and restore Add account focus; composition and native Select
Escape handling are respected. Pending work disables fields and cancel. Rejected
input remains, persistent feedback receives focus, and correction clears old
feedback without a write. Confirmed creation is distinguished from refresh failure;
an unsuccessful refresh blocks further creation until an explicit successful read.
Loading, empty, no-match and unavailable states are distinct. Route navigation
does not promise stored drafts or persisted search.

Shared Button, Badge, WorkSearch, WorkNotice, WorkState and HorizontalScrollArea
remain the owners. Table geometry uses the existing palette and typography, with
44px group actions and compact read-only rows. No art is placed behind data.

## Focused validation and current boundary

**11 Chart/scroller tests pass in 2.30s**, covering all classifications, unknown
metadata, protection versus lifecycle, search/disclosure, constraints, cancellation,
composition/select guards, synchronous duplicate submit, exact command fields,
rejected-input recovery, confirmed-save refresh failure and explicit read recovery.
The original handler reproduces **two** create calls under the same batched submit
events; the guarded handler makes **one**. The protected experiment restored the
changed source byte-identically. Private diagnostic output is not committed.

The first typecheck caught an optional test locator name; it now uses the exact
synthetic record name, and stable corrected source passes typecheck. Strict
configured audit remains non-green with the **same 19 rule/file findings** as the
prior block: 14 native-validation forms, four test mock buttons and one CSV resize
rule. Native validation and audit policy are unchanged; no exclusions are added.

This initial implementation commit precedes the clean package and rendered after
journey. It does not claim visual acceptance or current hosted qualification.

## First rendered after and bounded density correction

Clean source `3e9278190f3666a2c13893bb18f94b68570e0ab3` passed seven package
checks/one expected skip and the actual native Chart journey **11.1s /11.9s**,
zero retries, page errors and console errors. All **14 initial after frames** and
DOM measurements remain unchanged in `after-initial/`, with exact provenance in
[initial-after-captures.json](initial-after-captures.json).

Original-size comparison exposed protected labels wrapping into two lines, making
those rows **47px** versus **36px** for ordinary records. The full-width saved
banner also adds a separate row above the list. The correction keeps each complete
protected label on one line and moves persistent saved confirmation beside the
primary action. The narrow table's outward outline is clipped by the containing
surface; a Chart-only inset offset keeps that existing forest focus outline inside
the clipped geometry. Shared keyboard behavior is unchanged.

The expanded native assertions require all 18 known desktop fixture rows to be
36px, keep 44px group actions, and verify full protected-label visibility through
real keyboard horizontal scrolling at 390/320px. A second clean package is required
for these source changes; initial images are not relabelled as corrected evidence.

## Retained focus failure and cascade diagnosis

Clean corrected source `6707b3b146d2a04e850b0b32e8f73958162047f0` passed seven
package checks/one skip. Its capture passed the desktop **36px** fixture-row and
**44px** control assertions, then failed in **21.3s** at the first 390px focus check:
expected inset offset -2px, computed +2px. The assertion, ten-second expectation,
zero retries and native test budget remain unchanged. The 13 partial/safe failure
frames, measurements and provenance are retained in `after-focus-attempt/` and
[focus-attempt-captures.json](focus-attempt-captures.json). The later narrow label
proof and back/forward did not run on this attempt. No private logs, traces or
accessibility setup records are committed.

Inspection of the compiled CSS proves the Chart override was in `components`,
while canonical focus is in the higher-priority `utilities` layer. Moving the
existing three-line override beside that utility fixes layer precedence without
altering other components. A bounded renderer build verifies both emitted rules
are now in utilities and the Chart override follows canonical focus. Application
code and financial handlers are unchanged from `6707b3b1`; a clean package/native
pass is still required for this CSS correction. This is a diagnosed source fix,
not a blind retry or assertion weakening.

The preceding Journals head has terminal original first-attempt success in both
[push run 40031390128](https://github.com/castlemilk/tammy/actions/runs/40031390128)
and [PR run 40031395953](https://github.com/castlemilk/tammy/actions/runs/40031395953),
20/20 packaged cases each. Those outcomes belong to that exact source, not this
new Chart implementation.

Remaining after this block: workspace/recovery, Trial balance, GST/BAS, Audit and
Settings/Organisation. SBR remains with its functional owner. Existing financial
date/Banking precision findings, 19 audit findings and local startup discrepancies
remain. No MAS, Windows 11 or official ATO qualification; draft, merge and
publication holds remain. All prior captures, archives and concurrent work are
preserved. No new job, monitor, agent, release or authority change is included.
