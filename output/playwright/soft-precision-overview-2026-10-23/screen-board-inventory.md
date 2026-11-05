# Implemented screen families versus approved boards

Finite review snapshot of **c1bf895d57d5f59ad7f979864f77f02659761624**, based on
the actual route renderer, components and connected journeys. The design pack's
older feature inventory is historical; its planned labels are not current
implementation evidence. Board references are pinned to
**19533020422b6ea2f0c77dc3117a051483f9f9ce**.

| Connected screen family | Actual supported surface at this head | Matching board | Visual comparison status |
| --- | --- | --- | --- |
| Workspace entry / organisation / recovery / unlock / sign-in | Local encrypted workspace and existing authentication/recovery semantics | 11, 13; shared states 03 | Earlier entry captures and shared uplift exist; full board-by-board fidelity remains open. |
| Overview | Local attention projections, exact BAS target, posted income, receipts, outstanding balances, civil date controls and income export | 01 Today composition; 03 states | **Selected next bounded block.** Before/reference/after required. No cash-balance graph or last-sync contract. |
| Documents and embedded purchase review | Local source import, extracted/manual review, retained evidence and connected purchase review/payment/export controls | 08; purchase portions 10, 12 | Earlier real connected captures exist. Dedicated reference comparison remains open; not the complete planned purchase suite. |
| Invoices | Saved list/search/filter, draft editor and public on-screen preview, explicit issue, read-only items, actual receipts, saved PDF and templates | 02, 07 | Representative list/detail/payment/draft comparisons completed at this head. This does not cover every possible dialog/state. |
| Banking | Existing opening balance, explicit CSV statement import, manual matching and reconciliation completion | 08; format-specific cautions 13 | Earlier connected captures exist; closer board comparison remains open. No live feed or automatic matching. |
| Chart of accounts | Ordinary-account creation and protected/system account display | 09 | Shared styling/captures exist; dedicated fidelity acceptance remains open. |
| Journals | List/detail and existing balanced posting workflow | 09; list/detail pattern 08 | Shared styling/captures exist; dedicated fidelity acceptance remains open. |
| Trial balance | As-of balances with actual accounting rows | 09 | Shared styling/captures exist; dedicated fidelity acceptance remains open. |
| GST & BAS | Retained reviewed-document draft workpaper and connected preparation/review/freeze/export lifecycle, with user-recorded external status kept distinct | 09, 14 | Integrated reporting journeys exist; dedicated visual comparison remains open. No production ATO lodgement. |
| Audit trail / local activity | Consolidated local activity projection | 08 | Earlier capture/shared styling exists; dedicated comparison remains open. This does not establish cryptographic audit-chain verification. |
| Settings / organisation | Local workspace summary, diagnostics, canonical business identity and existing verification form | 11, 13 | Earlier connected captures exist; dedicated fidelity acceptance remains open. |
| SBR readiness | Local registration checks and supported deterministic simulator/readiness controls | 14, shared states 03/13 | Functional work and qualification have a separate active owner. Not an official ATO approval or production submission surface. |
| Privacy and support | Complete in-app policy content and navigation | No matching dedicated board; shared typography 03 | Earlier readable captures exist. In-app source is not proof of publication of expanded public-site policies. |

The 12 authenticated accounting/settings routes above are grouped into 11 families
and exclude entry/privacy; Settings and Organisation share one family here.
Subpanels such as company templates and purchase payments belong to their parent
screen family; they are not silently counted as standalone navigation screens.

## Accepted route strings that are not connected screens at this head

`COMPLETE_ROUTES` accepts `/workspace-trust`, `/restore`, `/restore/evidence`,
`/settings/security`, `/settings/backup`, `/settings/users`, `/settings/ownership`,
`/accounting/opening-balances` and `/accounting/periods`, but this head's active
route renderer falls back to **Workspace action unavailable / not yet connected**.
Route acceptance and Core/test existence alone do not make these complete UI.
Boards 11, 12 and 14 can inform future work; this block does not implement them.

## Other proposed board content outside this review head

Contacts, quotes, credit notes, complete supplier settlement/correction workflows,
a reporting catalogue, stock/inventory, forecasts, live feeds, heuristic
auto-posting, email/reminder delivery, a customer portal, mobile apps, gateways,
production ATO submission and migration UI are not inferred from the concepts.
Board 05's cash/forecast/month-end view and board 06's mobile views are exploratory.
Board 04 supplies existing arrival/empty-state art, not a workflow. Board 03 supplies
component guidance, not proof that every state is tested.

An active separate owner is implementing offline AI and shared component changes
on `issue/ato-approval-2026-10-23`; those unmerged changes are not included in this
head's inventory. Their dirty BAS file and checkout are preserved. The selected
Overview source files do not overlap that owner's current changes. This task will
not edit shared tokens/primitives, the app router or the other owner's checkout.
The existing report-export action owner needs a narrow completion-focus fix:
the real baseline loses keyboard focus when its button is disabled during a
cancelled export. Its financial/authority/export contract is preserved.

## Verification limits retained

The earlier local macOS 26.5.2 SBR startup discrepancy and the later second-unlock
failure remain recorded in their original evidence. Both hosted macOS 14 runs at
c1bf895d passed all 20 packaged cases without retries; that does not explain or
erase either local discrepancy. The same 19 strict static-audit findings remain
open. MAS, Windows 11 and official ATO qualification are separate boundaries.
