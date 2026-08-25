# Company Return 2026 Lifecycle Authority

This document is the implementation authority for the financial-close, Company return 2026, and durable submission-attempt lifecycles in the approved company EOFY design. The core enforces these transition tables. Any unlisted edge, including a self-transition or an edge involving an unspecified or unknown state, fails with `INVALID_STATE_TRANSITION` and performs no mutation.

## Financial close

| From | Allowed to | Owner or trigger |
|---|---|---|
| `COLLECTING` | `BLOCKED`, `REVIEW_READY` | Deterministic close validation. |
| `BLOCKED` | `COLLECTING`, `REVIEW_READY` | A source or resolution changes, followed by deterministic validation. |
| `REVIEW_READY` | `COLLECTING`, `BLOCKED`, `FROZEN` | An edit, a validation regression, or an authenticated freeze. |
| `FROZEN` | `COLLECTING` | Authenticated pre-declaration reopen of the mutable close aggregate. Every prior frozen snapshot remains immutable and addressable. |

`ReopenFinancialClose` is valid only from `FROZEN` while no dependent declaration exists. It requires a reason and purpose-bound fresh authentication. The operation moves the current `FinancialClose` aggregate to `COLLECTING`, preserves the old `FinancialCloseSnapshot`, and marks dependent undeclared drafts stale.

After any dependent return has been declared, `ReopenFinancialClose` fails closed. `StartFinancialCloseCorrection` instead creates a new, linked `COLLECTING` close and working revision while preserving the original close, snapshot, books, and return.

Financial close has no terminal state: the only edge out of `FROZEN` is the guarded reopen described above.

## Company return

| From | Allowed to | Owner or trigger |
|---|---|---|
| `COLLECTING` | `BLOCKED`, `REVIEW_READY` | Deterministic return validation. |
| `BLOCKED` | `COLLECTING`, `REVIEW_READY` | An input or source changes, followed by deterministic validation. |
| `REVIEW_READY` | `COLLECTING`, `BLOCKED`, `DECLARED` | An edit, a validation regression, or a fresh declaration. |
| `DECLARED` | `PRELODGE_PENDING`, `REPLACED` | Pre-lodge intent, or declaration withdrawal followed by replacement creation. |
| `PRELODGE_PENDING` | `DECLARED`, `READY_TO_LODGE`, `PRELODGE_REVIEW`, `BLOCKED`, `PRELODGE_OUTCOME_UNKNOWN` | Proven non-dispatch or abort restores the declaration; otherwise a committed pre-lodge result selects the outcome state. |
| `PRELODGE_REVIEW` | `DECLARED`, `REPLACED` | All official warnings are acknowledged and the immutable report is freshly redeclared, or a replacement is created. |
| `READY_TO_LODGE` | `LODGE_PENDING`, `REPLACED` | Lodge intent, or replacement before any accepted lodge. |
| `PRELODGE_OUTCOME_UNKNOWN` | `DECLARED`, `READY_TO_LODGE`, `PRELODGE_REVIEW`, `BLOCKED` | Status or reconciliation of the same pre-lodge identity. `DECLARED` means definitive non-acceptance with no new blockers or warnings. |
| `LODGE_PENDING` | `READY_TO_LODGE`, `DELIVERED`, `LODGE_REJECTED`, `LODGE_OUTCOME_UNKNOWN` | Proven non-dispatch or abort restores lodge readiness; otherwise a committed lodge result selects the outcome state. |
| `LODGE_OUTCOME_UNKNOWN` | `DELIVERED`, `LODGE_REJECTED` | Status or reconciliation of the same lodge identity. |
| `LODGE_REJECTED` | `REPLACED` | A replacement is linked to the definitively rejected attempt. |
| `DELIVERED` | `SUPERSEDED_BY_AMENDMENT` | A linked amendment is itself accepted and delivered. |
| `REPLACED` | none | Terminal retained predecessor. |
| `SUPERSEDED_BY_AMENDMENT` | none | Terminal retained predecessor. |

`REPLACED` and `SUPERSEDED_BY_AMENDMENT` are terminal. Creating an amendment does not immediately supersede a delivered predecessor. The predecessor moves to `SUPERSEDED_BY_AMENDMENT` only in the same committed unit of work that records the linked amendment's accepted lodge result.

## Durable company-return attempt

| From | Allowed to | Meaning |
|---|---|---|
| `PREPARED` | `DISPATCHING`, `ABORTED` | Begin helper dispatch, or prove cancellation before dispatch. |
| `DISPATCHING` | `NOT_DISPATCHED`, `RESULT_RECORDED`, `OUTCOME_UNKNOWN` | Prove no bytes could be accepted, retain a definitive bounded result, or record possible acceptance. |
| `NOT_DISPATCHED` | `PREPARED`, `ABORTED` | Retry the same operation identity, or cancel it. |
| `OUTCOME_UNKNOWN` | `RESULT_RECORDED` | Status or reconciliation resolves the same operation identity. |
| `RESULT_RECORDED` | `COMMITTED` | The core acknowledges the atomic result, receipt or status, and audit commit. |
| `COMMITTED` | none | Terminal retained attempt. |
| `ABORTED` | none | Terminal retained attempt with proof that no request was accepted. |

`COMMITTED` and `ABORTED` are terminal.

## Operation and outcome authority

The attempt lifecycle records delivery durability; it does not independently grant a company-return transition. The operation binding and definitive outcome determine the report transition.

| Operation | Definitive outcome | Report result |
|---|---|---|
| `PRELODGE` | Success | `READY_TO_LODGE` |
| `PRELODGE` | Warnings requiring acknowledgement | `PRELODGE_REVIEW` |
| `PRELODGE` | Definitive validation failure | `BLOCKED` |
| `PRELODGE` | Possible acceptance or unresolved transport | `PRELODGE_OUTCOME_UNKNOWN` |
| `LODGE` | Accepted with an official receipt | `DELIVERED` |
| `LODGE` | Definitively rejected | `LODGE_REJECTED` |
| `LODGE` | Possible acceptance or unresolved transport | `LODGE_OUTCOME_UNKNOWN` |
| `STATUS` or `RECONCILE` for pre-lodge | Resolved success, warnings, definitive non-acceptance, or validation failure | Only `READY_TO_LODGE`, `PRELODGE_REVIEW`, `DECLARED`, or `BLOCKED`; never `DELIVERED`. |
| `STATUS` or `RECONCILE` for lodge | Resolved accepted or rejected | Only `DELIVERED` or `LODGE_REJECTED`. |
| Aborted or proven-not-dispatched pre-lodge | No request bytes could be accepted | Atomically restore `DECLARED`; retain the aborted or not-dispatched attempt and the same attempt and operation identity. |
| Aborted or proven-not-dispatched lodge | No request bytes could be accepted | Atomically restore `READY_TO_LODGE`; retain the aborted or not-dispatched attempt and the same attempt and operation identity. |

Rollback requires the requested attempt ID to equal the original attempt ID, an attempt state of `NOT_DISPATCHED` or `ABORTED`, and no outcome. A recorded result requires `RESULT_RECORDED` or `COMMITTED` plus a defined outcome. `STATUS` and `RECONCILE` may resolve only the same attempt ID and original operation class. Operation-confused outcomes, identity mismatches, and all other unlisted resolutions fail without mutation.

## Permission authority

The authorization action in this matrix is passed to the core authorization boundary. Fresh-factor and aggregate-state checks are additional requirements, never substitutes for role authorization.

| Operation group | Authorization action | workspace_admin | business_preparer | business_lodger | auditor |
|---|---|---:|---:|---:|---:|
| Get/list close, statements, profile, return, facts, submission/status | `read_financial` | allow | allow | allow | allow |
| Create/resolve/reopen/correct close; edit profile/return/adjustment/election; validate/export; create replacement/amendment | `prepare_tax` | allow | allow | deny | deny |
| Freeze and approve exact financial statements | `approve_financial_close` | deny | deny | allow | deny |
| Acknowledge official warning; declare; withdraw declaration | `declare_company_return` | deny | deny | allow | deny |
| Pre-lodge, lodge, refresh delivery status, reconcile unknown outcome | `lodge` | deny | deny | allow | deny |

A sole operator may hold both administrator/preparer and lodger roles. Workspace administration alone does not silently grant legal approval/declaration/lodge authority.
