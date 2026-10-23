# Finite implemented-screen comparison inventory

Chart block from review base `8cb09e34d16ebacbf610eead1cb8494546b2a13f`.
Before runtime: `ffc2fdaa7e8a74092d78025107df4a620516c369`, whose Chart source is identical to that base. Final rendered Chart verification is pending in this initial implementation commit. Approved boards remain pinned to design
commit `19533020422b6ea2f0c77dc3117a051483f9f9ce`. Passing tests are separate
from visual comparison, and representative comparisons do not imply every
concept detail or workflow state is complete.

| Family | Real implemented scope | Board | Dedicated comparison status |
| --- | --- | --- | --- |
| Workspace entry / organisation / recovery / unlock / sign-in | Existing local workspace/authentication contracts | 11, 13, shared 03 | Earlier shared uplift and captures; full board comparisons remain open. |
| Overview | Posted income, receipt/owing projections, civil dates, attention and BAS target | 01, 03 | Representative comparison retained in the Overview folder; exact concept fidelity is not claimed. |
| Documents / embedded purchase review | Source retention, extracted/manual review, separate existing purchase controls | 08, parts 10/12 | Representative list/source/review comparison and keyboard follow-up retained. No original-file viewer contract. |
| Invoices | Saved list, Draft/Issued lifecycle, editable/public preview, explicit issue, receipts, PDF/template | 02, 07 | Representative list, issued Items/Payments and draft/preview comparisons retained. |
| Banking | Explicit local CSV, reviewed reference strings and reconciliation completion | 08, format cautions 13 | Representative before/reference/after comparison retained in the Banking folder. Wide review adjacency, narrow focus/table scrolling and confirmed outcomes captured. Hosted results belong to their exact head in draft PR #11. |
| Chart of accounts | Ordinary account creation and protected/system account display | 09 | Current block: native baseline retained; grouped rows, local search and explicit protection implemented. Rendered after comparison and exact-head CI remain pending. |
| Journals | List/detail and existing balanced two-sided posting | 09, list/detail 08 | Representative comparison retained. Both original hosted macOS runs passed 20/20 at `8cb09e34`; see the original run links in README.md. |
| Trial balance | Actual as-of account balances | 09 | Dedicated reference comparison remains open. |
| GST & BAS | Reviewed draft workpaper and preparation/review/freeze/export lifecycle | 09, 14 | Connected reporting journeys exist; dedicated visual acceptance remains open. No production ATO submission. |
| Audit trail / local activity | Consolidated local activity projection | 08 | Dedicated reference comparison remains open; no audit-chain verification claim. |
| Settings / organisation | Existing workspace summary, diagnostics, business identity and verification | 11, 13 | Dedicated reference comparison remains open. |
| SBR readiness | Supported local registration checks and deterministic simulator | 14, 03/13 | Separate active functional owner; no official ATO or production qualification. |
| Privacy / support | Existing full in-app policy and navigation | Shared 03 | Readable captures exist; no dedicated board, and in-app content does not prove public-site policy publication. |

The 12 authenticated routes are grouped into 11 families; Settings and Organisation
share a family. Entry and privacy are separate from that count. Subpanels are not
silently counted as standalone routes.

Accepted fallback routes remain unconnected: `/workspace-trust`, `/restore`,
`/restore/evidence`, `/settings/security`, `/settings/backup`, `/settings/users`,
`/settings/ownership`, `/accounting/opening-balances` and `/accounting/periods`.
Route acceptance and existing Core/tests do not establish a completed screen.

Planned contacts/quotes/credit notes, inventory, forecasts, live bank feeds,
automatic posting, email/reminder delivery, customer portals, gateways, mobile
apps, migration UI and production ATO submission are not inferred from boards.
Unmerged owner work is not claimed as current runtime capability. The other
owner's checkout was clean at `2a75edc8` and preserved during this block.

Remaining evidence limits: 19 strict configured audit findings, the existing local
macOS 26.5.2 SBR startup and second-unlock discrepancies, and the newly observed
existing invalid calendar-date statement import and extreme-value display precision. These are not erased by hosted
CI or screenshots. Draft, merge and publication holds remain; ordinary macOS 14
and Windows Server smoke do not qualify MAS, Windows 11 or official ATO use.
