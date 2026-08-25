# Company EOFY Contracts and Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Define the complete, generated 2026 private-company EOFY contract and lifecycle authority, while exposing only an honest fail-closed capability until the later domain and SBR stages are implemented.

**Architecture:** Three bounded Protobuf services own financial close, company return preparation, and company-return submission. Pure Go transition policies and checked transition fixtures are the single lifecycle authority. New business RPCs enter the E2E catalogue as `declared_future` and remain absent from Electron preload until their owning implementation plans promote them atomically; the existing pre-workspace capability service is the only newly promoted production behavior.

**Tech Stack:** Go 1.26, ConnectRPC, Buf/Protobuf Edition 2023, Protovalidate, TypeScript 7, Electron preload IPC, Node test runner, Vitest, YAML E2E coverage manifests.

---

**Approved design:** `docs/superpowers/specs/2026-08-25-company-eofy-and-sbr-design.md`

**Scope:** Delivery stage 1 only: generated contracts, lifecycle authority, permission vocabulary, transition/coverage catalogues, generated exports, and the fail-closed 2026 company-return capability. Do not add persistence, accounting calculations, report-bundle execution, UI screens, native file intake, SBR network operations, simulator behavior, or Taskfile launch scenarios here; those belong to stages 2–10 of the approved design.

**Greenfield rule:** Change the contract directly. Do not add legacy aliases, compatibility adapters, dual schemas, migrations for unreleased contract shapes, or generic key/value escape hatches.

## Chunk 1: Lifecycle authority and generated service contracts

### Task 1: Freeze the close, return, and attempt transition authority

**Files:**

- Create: `docs/development/company-return-2026-lifecycle.md`
- Create: `proto/tammy/v1/financial_close.proto`
- Create: `proto/tammy/v1/company_tax.proto`
- Create: `proto/tammy/v1/company_return_submission.proto`
- Create: `services/core/internal/annualreporting/transitions.go`
- Create: `services/core/internal/annualreporting/transitions_test.go`
- Create: `services/core/internal/contracts/company_eofy_contract_test.go`
- Create: `test/fixtures/reporting/transitions.pb.json`
- Create: `test/fixtures/tax/transitions.pb.json`
- Modify: `scripts/build-transition-index.test.mjs`
- Regenerate: `services/core/internal/gen/tammy/v1/financial_close.pb.go`
- Regenerate: `services/core/internal/gen/tammy/v1/company_tax.pb.go`
- Regenerate: `services/core/internal/gen/tammy/v1/company_return_submission.pb.go`
- Regenerate: `packages/connect-client/src/gen/tammy/v1/financial_close_pb.ts`
- Regenerate: `packages/connect-client/src/gen/tammy/v1/company_tax_pb.ts`
- Regenerate: `packages/connect-client/src/gen/tammy/v1/company_return_submission_pb.ts`
- Regenerate: `test/e2e/transitions.yaml`

- [ ] Write `docs/development/company-return-2026-lifecycle.md` first. State that it is the implementation authority for the approved design and include the exact transition tables below. Explain each operation-triggered edge, identify terminal states, and state that any unlisted edge fails with `INVALID_STATE_TRANSITION` without mutation.

  | Financial close from | Allowed to | Owner/trigger |
  |---|---|---|
  | `COLLECTING` | `BLOCKED`, `REVIEW_READY` | deterministic close validation |
  | `BLOCKED` | `COLLECTING`, `REVIEW_READY` | source/resolution change followed by validation |
  | `REVIEW_READY` | `COLLECTING`, `BLOCKED`, `FROZEN` | edit, validation regression, or freeze |
  | `FROZEN` | `COLLECTING` | authenticated pre-declaration reopen of the mutable close aggregate; every prior frozen snapshot remains immutable and addressable |

  `ReopenFinancialClose` is valid only from `FROZEN`, only while no dependent declaration exists, and requires a reason plus purpose-bound fresh authentication. It changes the current `FinancialClose` aggregate back to `COLLECTING`, preserves the old `FinancialCloseSnapshot`, and marks dependent undeclared drafts stale. Once any dependent return is declared, `ReopenFinancialClose` fails closed and `StartFinancialCloseCorrection` creates a new linked `COLLECTING` close/working revision without mutating the original close, snapshot, books, or return.

  | Company return from | Allowed to | Owner/trigger |
  |---|---|---|
  | `COLLECTING` | `BLOCKED`, `REVIEW_READY` | deterministic validation |
  | `BLOCKED` | `COLLECTING`, `REVIEW_READY` | input/source change followed by validation |
  | `REVIEW_READY` | `COLLECTING`, `BLOCKED`, `DECLARED` | edit, validation regression, or fresh declaration |
  | `DECLARED` | `PRELODGE_PENDING`, `REPLACED` | pre-lodge intent, or declaration withdrawal followed by replacement creation |
  | `PRELODGE_PENDING` | `DECLARED`, `READY_TO_LODGE`, `PRELODGE_REVIEW`, `BLOCKED`, `PRELODGE_OUTCOME_UNKNOWN` | proven not dispatched/aborted, or committed pre-lodge result |
  | `PRELODGE_REVIEW` | `DECLARED`, `REPLACED` | all official warnings acknowledged plus fresh redeclaration, or replacement |
  | `READY_TO_LODGE` | `LODGE_PENDING`, `REPLACED` | lodge intent, or replacement before any accepted lodge |
  | `PRELODGE_OUTCOME_UNKNOWN` | `DECLARED`, `READY_TO_LODGE`, `PRELODGE_REVIEW`, `BLOCKED` | reconciliation of the same pre-lodge identity; `DECLARED` means definitive non-acceptance without new blockers/warnings |
  | `LODGE_PENDING` | `READY_TO_LODGE`, `DELIVERED`, `LODGE_REJECTED`, `LODGE_OUTCOME_UNKNOWN` | proven not dispatched/aborted, or committed lodge result |
  | `LODGE_OUTCOME_UNKNOWN` | `DELIVERED`, `LODGE_REJECTED` | reconciliation of the same lodge identity |
  | `LODGE_REJECTED` | `REPLACED` | replacement linked to the rejected attempt |
  | `DELIVERED` | `SUPERSEDED_BY_AMENDMENT` | a linked amendment is itself accepted and delivered |
  | `REPLACED` | none | terminal retained predecessor |
  | `SUPERSEDED_BY_AMENDMENT` | none | terminal retained predecessor |

  Record explicitly that creating an amendment does **not** immediately supersede a delivered predecessor. The predecessor moves to `SUPERSEDED_BY_AMENDMENT` only in the same committed unit of work that records the amendment's accepted lodge result.

  | Durable attempt from | Allowed to | Meaning |
  |---|---|---|
  | `PREPARED` | `DISPATCHING`, `ABORTED` | begin helper dispatch, or prove cancellation before dispatch |
  | `DISPATCHING` | `NOT_DISPATCHED`, `RESULT_RECORDED`, `OUTCOME_UNKNOWN` | proof no bytes could be accepted, definitive bounded result, or possible acceptance |
  | `NOT_DISPATCHED` | `PREPARED`, `ABORTED` | retry the same operation identity, or cancel it |
  | `OUTCOME_UNKNOWN` | `RESULT_RECORDED` | status/reconciliation resolves the same operation identity |
  | `RESULT_RECORDED` | `COMMITTED` | core result, receipt/status, and audit commit acknowledged |
  | `COMMITTED` | none | terminal retained attempt |
  | `ABORTED` | none | terminal retained attempt with proof no request was accepted |

- [ ] In the same document add the operation/outcome matrix below. This prevents the generic attempt state from accidentally granting report authority.

  | Operation | Definitive outcome | Report result |
  |---|---|---|
  | `PRELODGE` | success | `READY_TO_LODGE` |
  | `PRELODGE` | warnings requiring acknowledgement | `PRELODGE_REVIEW` |
  | `PRELODGE` | definitive validation failure | `BLOCKED` |
  | `PRELODGE` | possible acceptance / unresolved transport | `PRELODGE_OUTCOME_UNKNOWN` |
  | `LODGE` | accepted with official receipt | `DELIVERED` |
  | `LODGE` | definitively rejected | `LODGE_REJECTED` |
  | `LODGE` | possible acceptance / unresolved transport | `LODGE_OUTCOME_UNKNOWN` |
  | `STATUS` / `RECONCILE` for pre-lodge | resolved success, warnings, definitive non-acceptance, or validation failure | only `READY_TO_LODGE`, `PRELODGE_REVIEW`, `DECLARED`, or `BLOCKED`; never `DELIVERED` |
  | `STATUS` / `RECONCILE` for lodge | resolved accepted or rejected | only `DELIVERED` or `LODGE_REJECTED` |
  | aborted/proven not dispatched pre-lodge | no request bytes could be accepted | atomically restore `DECLARED`; retain the aborted/not-dispatched attempt and same operation identity |
  | aborted/proven not dispatched lodge | no request bytes could be accepted | atomically restore `READY_TO_LODGE`; retain the aborted/not-dispatched attempt and same operation identity |

- [ ] Add failing table tests in `annualreporting/transitions_test.go` for every allowed edge above, every terminal state, representative illegal cross-operation edges (`PRELODGE_PENDING -> DELIVERED`, `PRELODGE_OUTCOME_UNKNOWN -> DELIVERED`, `LODGE_OUTCOME_UNKNOWN -> READY_TO_LODGE`), and all unspecified enum values. The transition functions must be pure and return `false` for unknown numeric values.

- [ ] Add failing descriptor/fixture tests in the focused `company_eofy_contract_test.go` requiring the three enum names and the exact transition sets. Extend `build-transition-index.test.mjs` so the already-reserved `reporting` and `tax` fixture paths participate in deterministic sorting. Run:

  ```bash
  rtk mise exec -- go test ./services/core/internal/annualreporting/... ./services/core/internal/contracts/...
  rtk mise exec -- node --test scripts/build-transition-index.test.mjs
  ```

  Expected: FAIL because the enums, policies, and fixtures do not yet exist.

- [ ] Define these Edition 2023 enums with a zero `UNSPECIFIED` sentinel and no legacy aliases:

  - In `financial_close.proto`, `FinancialCloseState`: `COLLECTING`, `BLOCKED`, `REVIEW_READY`, `FROZEN`.
  - In `company_tax.proto`, `CompanyReturnState`: all 14 public states from the approved design; `CompanyReturnRelationshipKind`: `ORIGINAL`, `REPLACEMENT`, `AMENDMENT`; `CompanyReturnOperationType`: `PRELODGE`, `LODGE`, `STATUS`, `RECONCILE`; and `CompanyReturnOperationOutcome`: `SUCCESS`, `WARNINGS`, `REJECTED`, `OUTCOME_UNKNOWN`.
  - In `company_return_submission.proto`, `CompanyReturnAttemptState`: `PREPARED`, `DISPATCHING`, `NOT_DISPATCHED`, `RESULT_RECORDED`, `OUTCOME_UNKNOWN`, `COMMITTED`, `ABORTED`.

  This placement is intentional: `company_return_submission.proto` imports `company_tax.proto` for return projections and shared operation/outcome enums. `company_tax.proto` must not import the submission file, so the Protobuf dependency graph remains acyclic.

- [ ] Run `rtk pnpm proto:format && rtk pnpm proto:generate` immediately after defining the enums. Confirm the three generated Go descriptor files and three generated TypeScript files exist before implementing the Go transition policies or rerunning reflection tests.

- [ ] Implement `CanTransitionFinancialClose`, `CanTransitionCompanyReturn`, and `CanTransitionCompanyReturnAttempt` as exhaustive `switch` policies in `annualreporting/transitions.go`. Do not derive edges from enum ordering and do not permit self-transitions; idempotent command replay is handled above the transition layer.

- [ ] Add `ResolveReportTransition(currentReportState, AttemptResolution)` tests and implementation for the exact operation/outcome matrix. `AttemptResolution` contains `original_attempt_id`, `requested_attempt_id`, `original_operation`, `resolving_operation`, `attempt_state`, and optional `outcome`. Rollback is permitted only when the same attempt ID is bound to `NOT_DISPATCHED` or `ABORTED` with no outcome; a recorded result requires `RESULT_RECORDED` or `COMMITTED` and a defined outcome; reconciliation requires the same attempt ID and the original operation class. Reject a pre-lodge result that requests `DELIVERED`, a lodge result that requests `READY_TO_LODGE` without proof of non-dispatch, and every attempt-ID/operation binding mismatch.

- [ ] Populate `test/fixtures/reporting/transitions.pb.json` with all financial-close edges and `test/fixtures/tax/transitions.pb.json` with every company-return and attempt edge. Run `rtk pnpm transitions:generate`; inspect the generated `test/e2e/transitions.yaml` and confirm it contains only descriptor-valid, sorted edges.

- [ ] Run:

  ```bash
  rtk mise exec -- go test ./services/core/internal/annualreporting/... ./services/core/internal/contracts/...
  rtk mise exec -- node --test scripts/build-transition-index.test.mjs
  rtk pnpm transitions:check
  ```

  Expected: PASS, including rejection of all illegal and operation-confused transitions.

- [ ] Commit:

  ```bash
  rtk git add docs/development/company-return-2026-lifecycle.md proto/tammy/v1/financial_close.proto proto/tammy/v1/company_tax.proto proto/tammy/v1/company_return_submission.proto services/core/internal/annualreporting services/core/internal/contracts/company_eofy_contract_test.go services/core/internal/gen/tammy/v1 packages/connect-client/src/gen/tammy/v1 scripts/build-transition-index.test.mjs test/fixtures/reporting/transitions.pb.json test/fixtures/tax/transitions.pb.json test/e2e/transitions.yaml
  rtk git commit -m "feat: define company EOFY lifecycle authority"
  ```

### Contract conventions for Tasks 2–4

Apply these exact conventions to all three new Edition 2023 files:

- IDs are strings with the repository UUIDv7 validator; hashes/fingerprints are `bytes` with `len: 32` unless a field explicitly names a human-readable version.
- Stable bundle/rule/fact/service/result codes are strings matching `^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`; safe descriptions/reasons are UTF-8 strings of 1–2,000 characters as specified below.
- Currency-bearing messages reuse `Money`; dates reuse `CivilDate`; timestamps use required `google.protobuf.Timestamp`; provenance references reuse `SourceRef` and are capped at 100 per item.
- Every closed enum uses `defined_only: true` and excludes its zero sentinel when required in a command or persisted projection.
- No new message may contain a map, `google.protobuf.Any`, `google.protobuf.Struct`, or `google.protobuf.Value`.
- Mutation responses contain the mutated aggregate plus no more than 200 bounded validation outcomes. Query/list responses expose no raw official payload, unrestricted receipt, secret, path, endpoint, or credential/Product ID bytes.
- In the RPC tables below, `authentication` is shorthand for `authentication AuthenticationContext required`, `command_context` for `command_context CommandContext required`, `organisation_id` and every `*_id` for UUIDv7 strings unless explicitly identified as a stable code, `expected_version` for `uint64 >= 1`, and `page` for required `PageRequest`/`PageInfo` as appropriate. High-risk commands require `command_context.fresh_factor`; never add a second top-level `FreshFactorContext`.

### Task 2: Define the bounded financial-close service

**Files:**

- Modify: `proto/tammy/v1/financial_close.proto`
- Modify: `services/core/internal/contracts/company_eofy_contract_test.go`
- Regenerate: `services/core/internal/gen/tammy/v1/financial_close.pb.go`
- Regenerate: `services/core/internal/gen/tammy/v1/tammyv1connect/financial_close.connect.go`
- Regenerate: `packages/connect-client/src/gen/tammy/v1/financial_close_pb.ts`

- [ ] Add failing reflection tests for the exact eight RPCs and the descriptor tables below. Run `rtk mise exec -- go test ./services/core/internal/contracts/...`; expect missing messages/RPCs.

- [ ] Define subordinate enums: `CloseCheckSeverity {BLOCKER, WARNING}`, `CloseCheckResult {FAILED, PASSED, RESOLVED}`, and `FinancialStatementKind {PROFIT_AND_LOSS, BALANCE_SHEET, CASH_FLOW, TRIAL_BALANCE, GENERAL_LEDGER, GST_DETAIL, FIXED_ASSET_SCHEDULE, FRANKING_RECONCILIATION}`.

- [ ] Define exact projections (the table order is the field number):

  | Message | Fields in numeric order |
  |---|---|
  | `CloseCheck` | `id string`, `close_id string`, `rule_id string`, `severity CloseCheckSeverity`, `result CloseCheckResult`, `source_revision uint64 >=1`, `affected_sources repeated SourceRef max 100`, `optional resolution string min 1 max 2000`, `optional resolved_by_user_id string UUIDv7`, `optional resolved_at Timestamp` |
  | `StatementHash` | `kind FinancialStatementKind`, `content_hash bytes len 32` |
  | `FinancialStatementApproval` | `id string`, `period_start CivilDate required`, `period_end CivilDate required`, `financial_revision uint64 >=1`, `approval_wording_version string max 128`, `approval_wording_hash bytes len 32`, `statement_hashes repeated StatementHash min 1 max 16`, `approved_by_user_id string`, `fresh_factor_assertion_id string`, `approved_at Timestamp` |
  | `FinancialCloseSnapshot` | `id string`, `close_id string`, `organisation_id string`, `verified_abn string pattern ^[0-9]{11}$`, `income_year int32 const 2026`, `period_start CivilDate required`, `period_end CivilDate required`, `currency string const AUD`, `snapshot_hash bytes len 32`, `financial_revision uint64 >=1`, `subledger_revisions repeated SourceRevision max 32`, `statement_hashes repeated StatementHash min 4 max 16`, `trial_balance_hash bytes len 32`, `checklist_hash bytes len 32`, `reconciliation_hash bytes len 32`, `accounting_rule_fingerprint bytes len 32`, `gst_rule_fingerprint bytes len 32`, `asset_rule_fingerprint bytes len 32`, `evidence_manifest_hash bytes len 32`, `audit_head_hash bytes len 32`, `approval FinancialStatementApproval required`, `corrects_close_id string optional`, `frozen_at Timestamp` |
  | `SourceRevision` | `owner string max 64`, `revision uint64 >=1`, `content_hash bytes len 32` |
  | `FinancialClose` | `id string`, `organisation_id string`, `income_year int32 const 2026`, `period_start CivilDate required`, `period_end CivilDate required`, `currency string const AUD`, `version uint64 >=1`, `state FinancialCloseState`, `financial_revision uint64 >=1`, `latest_frozen_snapshot FinancialCloseSnapshot optional`, `created_at Timestamp`, `updated_at Timestamp` |
  | `FinancialStatementLine` | `stable_code string max 128`, `label string max 256`, `amount Money required`, `sources repeated SourceRef max 100` |
  | `FinancialStatement` | `kind FinancialStatementKind`, `content_hash bytes len 32`, `lines repeated FinancialStatementLine max 2000` |
  | `FinancialStatements` | `close_id string`, `snapshot_id string`, `financial_revision uint64 >=1`, `statements repeated FinancialStatement min 4 max 8` |

- [ ] Define the exact request/response descriptors:

  | RPC | Request fields in numeric order | Response fields |
  |---|---|---|
  | `CreateFinancialClose` | `command_context CommandContext required`, `organisation_id string`, `income_year int32 == 2026`, `period_start CivilDate required`, `period_end CivilDate required` | `close FinancialClose required` |
  | `GetFinancialClose` | `authentication AuthenticationContext required`, `organisation_id string`, `close_id string` | `close FinancialClose required` |
  | `ListCloseChecks` | `authentication`, `organisation_id`, `close_id`, `page PageRequest required` | `checks repeated CloseCheck max 200`, `page PageInfo required` |
  | `ResolveCloseWarning` | `command_context`, `organisation_id`, `close_id`, `expected_version uint64 >=1`, `check_id string`, `resolution string min 1 max 2000` | `close`, `check` required |
  | `FreezeFinancialClose` | `command_context`, `organisation_id`, `close_id`, `expected_version` | `close`, `snapshot` required |
  | `ReopenFinancialClose` | `command_context`, `organisation_id`, `close_id`, `expected_version`, `reason string min 1 max 2000` | `close required`, `preserved_snapshot_id string` |
  | `StartFinancialCloseCorrection` | `command_context`, `organisation_id`, `close_id`, `expected_version`, `reason string min 1 max 2000` | `original_close FinancialClose required`, `correction_close FinancialClose required` |
  | `GetFinancialStatements` | `authentication`, `organisation_id`, `close_id`, `snapshot_id string` | `statements FinancialStatements required` |

- [ ] Run `rtk pnpm proto:format && rtk pnpm proto:generate`; get the structural reflection tests green before writing behavioral validation tests.

- [ ] Before adding conditional rules, add executable Protovalidate tests that reject wrong year/period, non-AUD close currency, a non-AUD `FinancialStatementLine.amount`, a `FROZEN` close without a snapshot, a resolved check missing any resolution field, a failed/passed check carrying resolution fields, and missing/wrong-purpose close fresh factors; include matching valid fixtures. Run the focused contract test and expect failure because these structurally valid messages are still accepted, not because generated types are missing.

- [ ] Add message-level Protovalidate CEL constraints requiring every close, snapshot, and approval period to be exactly `2025-07-01` through `2026-06-30`, and every `FinancialStatementLine.amount` currency to be `AUD`. `FROZEN` requires `latest_frozen_snapshot` and matching snapshot/approval revisions; a reopened `COLLECTING` close retains `latest_frozen_snapshot` but may have a newer working financial revision. Require the three optional close-check resolution fields together iff result is `RESOLVED`. Assert `command_context.fresh_factor` purposes `financial_close_freeze`, `financial_close_reopen`, and `financial_close_start_correction`. The service must derive approval wording/version/hash and statement hashes from the accepted build/rules and current close; the renderer supplies none of them.

- [ ] Regenerate after adding CEL, then run the same behavioral tests again. Implement only schema/validation fixes until `rtk mise exec -- go test ./services/core/internal/contracts/...` passes.

- [ ] Commit:

  ```bash
  rtk git add proto/tammy/v1/financial_close.proto services/core/internal/contracts/company_eofy_contract_test.go services/core/internal/gen/tammy/v1 packages/connect-client/src/gen/tammy/v1
  rtk git commit -m "feat: define financial close contracts"
  ```

### Task 3: Define the bounded 2026 company-return preparation service

**Files:**

- Modify: `proto/tammy/v1/company_tax.proto`
- Modify: `services/core/internal/contracts/company_eofy_contract_test.go`
- Regenerate: `services/core/internal/gen/tammy/v1/company_tax.pb.go`
- Regenerate: `services/core/internal/gen/tammy/v1/tammyv1connect/company_tax.connect.go`
- Regenerate: `packages/connect-client/src/gen/tammy/v1/company_tax_pb.ts`

- [ ] Add failing reflection tests for the exact 17 RPCs and all descriptor tables below. Also reject any dynamic/map field and require every repeated field to carry a maximum. Run `rtk mise exec -- go test ./services/core/internal/contracts/...`; expect failure.

- [ ] Define subordinate enums exactly: `RequiredAnswer {YES, NO}`, `ReturnFactProvenanceKind {FROZEN_BOOK, REVIEWED_TAX_ADJUSTMENT, VERIFIED_PROFILE, EXPLICIT_EVIDENCED_INPUT, BUNDLE_ELECTION, CALCULATION_RULE}`, `ReturnFactValidationStatus {VALID, BLOCKER, WARNING, INFORMATION, UNSUPPORTED}`, `ReturnValidationSeverity {BLOCKER, WARNING, INFORMATION, UNSUPPORTED}`, `TaxAdjustmentType {NON_DEDUCTIBLE_EXPENSE, EXEMPT_NON_ASSESSABLE_INCOME, ACCOUNTING_TAX_DEPRECIATION, PROVISION_ACCRUAL_REVERSAL, TAX_PAYMENT_CREDIT, CURRENT_YEAR_REVENUE_LOSS, CARRIED_FORWARD_REVENUE_LOSS}`, `TaxAdjustmentTiming {PERMANENT, TEMPORARY}`, `HoldingCompanyKind {NONE, AUSTRALIAN, FOREIGN}`, `BaseRatePassiveIncomeClassification {PASSIVE, NON_PASSIVE}`, `SmallBusinessEntityChoice {APPLY, DO_NOT_APPLY}`, `DepreciationChoice {STANDARD, SUPPORTED_SMALL_BUSINESS}`, and `CompanyReturnExportKind {REDACTED_REVIEW_PDF, ENCRYPTED_HANDOFF_ARCHIVE}`. Every enum also has its prefixed zero `UNSPECIFIED` sentinel; every mandatory answer/classification field rejects that sentinel.

- [ ] Define exact input messages. Secrets are transient `SecretInput` values and must never appear in response projections:

  | Message | Fields in numeric order |
  |---|---|
  | `AddressInput` | `line_1 string min 1 max 128`, `line_2 string max 128`, `locality string min 1 max 128`, `state string pattern ^(ACT|NSW|NT|QLD|SA|TAS|VIC|WA)$`, `postcode string pattern ^[0-9]{4}$`, `country_code string const AU` |
  | `RelatedEntityTurnoverContribution` | `entity_name string min 1 max 200`, `entity_abn string pattern ^[0-9]{11}$`, `amount Money required`, `evidence repeated SourceRef min 1 max 20`, `reviewed_control_or_affiliate_basis string min 1 max 2000` |
  | `PassiveIncomeClassificationInput` | `income_source SourceRef required`, `classification BaseRatePassiveIncomeClassification`, `bundle_rule_id string max 128`, `evidence repeated SourceRef min 1 max 20`, `reviewed_by_user_id string UUIDv7` |
  | `ApplicabilityAnswers` | `RequiredAnswer` fields `tofa_applies`, `psi_applies`, `interposed_entity_election_applies`, `consolidated_group_member`, `research_and_development_incentive`, `international_dealings`, `reportable_tax_position`, `life_insurance_business`, `cgt_schedule_required`, `losses_schedule_required`, `other_schedule_required`, `fb_or_unsupported_payroll_effect`, `division_7a_unresolved`, `unsupported_inventory`, `unsupported_multicurrency`, `unsupported_crypto`; every field rejects `UNSPECIFIED`, and each `YES` is a blocker for this bundle |
  | `PriorRevenueLossInput` | `opening_balance Money required`, `ownership_continuity_confirmed RequiredAnswer`, `same_or_similar_business_judgement_required RequiredAnswer`, `evidence repeated SourceRef min 1 max 20` |
  | `CompanyTaxProfileInput` | `legal_name string min 1 max 200`, `tfn SecretInput required`, `current_postal_address AddressInput required`, `prior_postal_address AddressInput required`, `main_business_address AddressInput required`, `australian_resident RequiredAnswer`, `private_company RequiredAnswer`, `main_business_activity_code string pattern ^[0-9]{6}$`, `main_business_activity_description string min 1 max 200`, `refund_bsb SecretInput`, `refund_account_number SecretInput`, `final_return RequiredAnswer`, `holding_company_kind HoldingCompanyKind`, `immediate_holding_name string max 200`, `ultimate_holding_name string max 200`, `related_turnover repeated RelatedEntityTurnoverContribution max 100`, `passive_income_classifications repeated PassiveIncomeClassificationInput max 500`, `small_business_entity_choice SmallBusinessEntityChoice`, `depreciation_choice DepreciationChoice`, `optional prior_revenue_loss PriorRevenueLossInput`, `applicability ApplicabilityAnswers required` |
  | `CompanyReturnInput` | `loss_amount_to_apply Money required`, `external_summary_evidence repeated SourceRef max 20`, `payroll_summary_evidence repeated SourceRef max 20`, `review_note string max 2000` |

- [ ] Define exact reusable return projections:

  | Message | Fields in numeric order |
  |---|---|
  | `MaskedCompanyTaxProfile` | `organisation_id string`, `version uint64 >=1`, `legal_name string min 1 max 200`, `masked_tfn string min 1 max 16`, `verified_abn string pattern ^[0-9]{11}$`, `current_postal_address AddressInput required`, `prior_postal_address AddressInput required`, `main_business_address AddressInput required`, `australian_resident RequiredAnswer`, `private_company RequiredAnswer`, `main_business_activity_code string pattern ^[0-9]{6}$`, `main_business_activity_description string min 1 max 200`, `masked_refund_bsb string max 16`, `masked_refund_account string max 32`, `final_return RequiredAnswer`, `holding_company_kind HoldingCompanyKind`, `immediate_holding_name string max 200`, `ultimate_holding_name string max 200`, `related_turnover repeated RelatedEntityTurnoverContribution max 100`, `passive_income_classifications repeated PassiveIncomeClassificationInput max 500`, `small_business_entity_choice SmallBusinessEntityChoice`, `depreciation_choice DepreciationChoice`, `optional prior_revenue_loss PriorRevenueLossInput`, `applicability ApplicabilityAnswers required`, `updated_by_user_id string`, `updated_at Timestamp` |
  | `TaxAdjustment` | `id`, `return_id`, `version`, `type TaxAdjustmentType`, `bundle_rule_id`, `amount Money`, `timing TaxAdjustmentTiming`, `explanation max 2000`, `sources repeated SourceRef max 100`, `evidence repeated SourceRef max 100`, `created_by_user_id`, `reviewed_by_user_id`, `updated_at` |
  | `TaxElectionChoice` | `oneof choice {bool boolean_value; string string_value max 128; Decimal decimal_value}` |
  | `TaxElection` | `id`, `return_id`, `version`, `bundle_election_id`, `choice TaxElectionChoice`, `explanation max 2000`, `evidence repeated SourceRef min 1 max 100`, `created_by_user_id`, `reviewed_by_user_id`, `updated_at` |
  | `ReturnFactValue` | `oneof value {string string_value max 512; bool boolean_value; sint64 integer_value; Money money_value; Decimal decimal_value; CivilDate date_value}` |
  | `ReturnFact` | `fact_id string max 128`, `value ReturnFactValue required`, `submitted_value ReturnFactValue required`, `provenance ReturnFactProvenanceKind`, `mapping_id string max 128`, `rule_id string max 128`, `sources repeated SourceRef max 100`, `evidence repeated SourceRef max 100`, `validation_status ReturnFactValidationStatus` |
  | `TaxReconciliationTerm` | `stable_id`, `rule_id`, `amount Money`, `sources repeated SourceRef max 100`, `evidence repeated SourceRef max 100` |
  | `TaxReconciliation` | `content_hash bytes len 32`, `accounting_profit_before_tax Money required`, `additions repeated TaxReconciliationTerm max 200`, `subtractions repeated TaxReconciliationTerm max 200`, `eligible_applied_losses repeated TaxReconciliationTerm max 100`, `taxable_income_or_loss Money required`, `gross_tax Money required`, `payg_and_credits repeated TaxReconciliationTerm max 100`, `net_tax_payable_or_refund Money required` |
  | `ReturnValidationOutcome` | `id`, `validation_revision`, `severity`, `stable_code`, `fact_ids repeated string max 100`, `sources repeated SourceRef max 100`, `safe_message max 1000`, `acknowledged bool` |
  | `ValidationAcknowledgement` | `id`, `return_id`, `warning_id`, `validation_revision`, `actor_user_id`, `fresh_factor_assertion_id`, `acknowledged_at` |
  | `Declaration` | `id`, `return_id`, `report_hash`, `validation_revision`, `acknowledgement_ids repeated string max 200`, `declaration_wording_version`, `declaration_wording_hash`, `terms_version`, `privacy_reference_version`, `actor_user_id`, `fresh_factor_assertion_id`, `declared_at`, `supersedes_declaration_id optional` |
  | `CompanyReturnDeliverySummary` | `latest_attempt_id`, `operation_type`, `outcome`, `safe_status_code`, `delivered_at optional`, `receipt_id optional` |
  | `CompanyReturn` | `id`, `organisation_id`, `income_year == 2026`, `period_start`, `period_end`, `relationship_kind`, `root_return_id`, `predecessor_return_id optional`, `successor_return_id optional`, `related_attempt_id optional`, `preparation_bundle_id`, `preparation_bundle_fingerprint`, `source_close_id`, `source_close_hash`, `tax_reconciliation_hash`, `state`, `version`, `validation_revision`, `declared_snapshot_hash optional`, `current_declaration_id optional`, `delivery CompanyReturnDeliverySummary optional`, `created_at`, `updated_at` |

- [ ] Number every field in each projection in the exact table order starting at 1. Use required-message validation where the table does not say optional. Cap masked strings at 200, stable IDs/codes/versions at 128, acknowledgement IDs at UUIDv7, and monetary collections as shown.

- [ ] Define the exact RPC descriptors:

  | RPC | Request fields in numeric order | Response fields |
  |---|---|---|
  | `GetCompanyTaxProfile` | `authentication`, `organisation_id` | `profile MaskedCompanyTaxProfile required` |
  | `SetCompanyTaxProfile` | `command_context`, `organisation_id`, `expected_version uint64 (0 creates; >=1 updates)`, `input CompanyTaxProfileInput required` | `profile required` |
  | `CreateCompanyReturn` | `command_context`, `organisation_id`, `source_close_id`, `input CompanyReturnInput required` | `company_return`, `tax_reconciliation`, `validation repeated max 200` |
  | `GetCompanyReturn` | `authentication`, `organisation_id`, `return_id` | `company_return`, `tax_reconciliation`, `validation repeated max 200` |
  | `ListCompanyReturnFacts` | `authentication`, `organisation_id`, `return_id`, `page` | `facts repeated max 200`, `page` |
  | `SetCompanyReturnInput` | `command_context`, `organisation_id`, `return_id`, `expected_version`, `input CompanyReturnInput` | `company_return`, `tax_reconciliation`, `validation repeated max 200` |
  | `UpsertTaxAdjustment` | `command_context`, `organisation_id`, `return_id`, `expected_version`, `adjustment TaxAdjustmentInput required` | `company_return`, `adjustment`, `tax_reconciliation`, `validation repeated max 200` |
  | `RemoveTaxAdjustment` | `command_context`, `organisation_id`, `return_id`, `expected_version`, `adjustment_id` | `company_return`, `tax_reconciliation`, `validation repeated max 200` |
  | `UpsertTaxElection` | `command_context`, `organisation_id`, `return_id`, `expected_version`, `election TaxElectionInput required` | `company_return`, `election`, `tax_reconciliation`, `validation repeated max 200` |
  | `RemoveTaxElection` | `command_context`, `organisation_id`, `return_id`, `expected_version`, `election_id` | `company_return`, `tax_reconciliation`, `validation repeated max 200` |
  | `ValidateCompanyReturn` | `command_context`, `organisation_id`, `return_id`, `expected_version` | `company_return`, `validation repeated max 200` |
  | `AcknowledgeReturnWarning` | `command_context`, `organisation_id`, `return_id`, `expected_version`, `warning_id`, `validation_revision uint64 >=1` | `company_return`, `acknowledgement`, `validation repeated max 200` |
  | `DeclareCompanyReturn` | `command_context`, `organisation_id`, `return_id`, `expected_version`, `validation_revision uint64 >=1` | `company_return`, `declaration` |
  | `WithdrawCompanyReturnDeclaration` | `command_context`, `organisation_id`, `return_id`, `expected_version`, `reason string min 1 max 2000` | `company_return`, `retained_declaration` |
  | `ExportCompanyReturnPack` | `command_context`, `organisation_id`, `return_id`, `expected_version`, `kind CompanyReturnExportKind`, `export_passphrase SecretInput required only for encrypted handoff` | `export_id string UUIDv7`, `content_hash bytes len 32`, `safe_filename string min 1 max 255 with no slash/backslash/control`, `kind CompanyReturnExportKind`; no path or bytes |
  | `CreateCompanyReturnReplacement` | `command_context`, `organisation_id`, `predecessor_return_id`, `expected_predecessor_version`, `source_close_id`, `reason min 1 max 2000` | `predecessor`, `replacement` |
  | `CreateCompanyReturnAmendment` | `command_context`, `organisation_id`, `effective_original_return_id`, `latest_accepted_return_id`, `expected_latest_version`, `source_close_id`, `reason min 1 max 2000` | `effective_original`, `amendment` |

- [ ] Define `TaxAdjustmentInput` as `adjustment_id optional`, `type`, `bundle_rule_id`, `amount`, `timing TaxAdjustmentTiming`, `explanation`, `sources max 100`, `evidence max 100`; define `TaxElectionInput` as `election_id optional`, `bundle_election_id`, `choice`, `explanation`, `evidence min 1 max 100`. These inputs cannot name an unknown bundle ID at runtime, and omitted adjustment timing is descriptor-invalid.

- [ ] Run `rtk pnpm proto:format && rtk pnpm proto:generate`; get the structural reflection tests green before writing behavioral validation tests.

- [ ] Before adding conditional rules, add executable Protovalidate tests that reject omitted/`UNSPECIFIED` mandatory answers and adjustment timing, empty identity/address values, non-AUD amounts, wrong year/period/bundle, a prior-loss record with zero balance/missing evidence/unknown continuity, an encrypted handoff without passphrase, a redacted export with passphrase, and missing/wrong-purpose fresh factors; include a no-prior-loss valid profile and matching valid 2026 fixtures. Run the focused contract test and expect failure only for the not-yet-added conditional rules; zero-enum/structural cases should already be rejected.

- [ ] Add message-level Protovalidate CEL requiring every `CompanyReturn` to use income year `2026`, period `2025-07-01` through `2026-06-30`, preparation bundle ID `au-company-return-2026-preparation-v1`, and AUD for every profile, adjustment, reconciliation, and return-input `Money`. An absent `prior_revenue_loss` means no carried-forward loss; when present, it requires a positive opening balance, explicit continuity answers, and at least one evidence reference. Require the encrypted handoff export to have a passphrase and the redacted PDF export to omit it. Assert `command_context.fresh_factor` presence and exact purposes in comments/tests: `company_tax_edit_secrets`, `company_return_acknowledge_warning`, `company_return_declare`, `company_return_withdraw_declaration`, and `company_return_export`. Replacement/amendment commands do not mutate predecessor snapshots and do not themselves grant declaration authority.

- [ ] Regenerate after adding CEL, then run `rtk mise exec -- go test ./services/core/internal/contracts/...`. Fix only contract/validation defects until it passes.

- [ ] Commit:

  ```bash
  rtk git add proto/tammy/v1/company_tax.proto services/core/internal/contracts/company_eofy_contract_test.go services/core/internal/gen/tammy/v1 packages/connect-client/src/gen/tammy/v1
  rtk git commit -m "feat: define company return preparation contracts"
  ```

### Task 4: Define the bounded company-return submission service

**Files:**

- Modify: `proto/tammy/v1/company_return_submission.proto`
- Modify: `services/core/internal/contracts/company_eofy_contract_test.go`
- Regenerate: `services/core/internal/gen/tammy/v1/company_return_submission.pb.go`
- Regenerate: `services/core/internal/gen/tammy/v1/tammyv1connect/company_return_submission.connect.go`
- Regenerate: `packages/connect-client/src/gen/tammy/v1/company_return_submission_pb.ts`

- [ ] Add failing reflection tests for the exact five RPCs. Assert that no request contains environment, Product ID, service ID, ABN, endpoint, profile/bundle fingerprint, payload bytes, credential bytes/password, or local path fields. Run the contract tests and expect failure.

- [ ] Define `SubmissionEnvironment {SIMULATOR, EVTE, PRODUCTION}`, `SubmissionRetryClassification {NEVER, SAME_IDENTITY_AFTER_PROVEN_NOT_DISPATCHED, STATUS_OR_RECONCILE_ONLY}`, and the exact projections:

  | Message | Fields in numeric order |
  |---|---|
  | `CompanyReturnSubmissionAttempt` | `id string UUIDv7`, `return_id string UUIDv7`, `declaration_id string UUIDv7`, `report_snapshot_hash bytes len 32`, `official_payload_hash bytes len 32`, `environment SubmissionEnvironment`, `product_identifier_fingerprint bytes len 32`, `service_id string stable-code min 1 max 128 (explicitly not UUIDv7)`, `operation_type CompanyReturnOperationType`, `operation_id string UUIDv7`, `idempotency_identity string UUIDv7`, `state CompanyReturnAttemptState`, `optional outcome CompanyReturnOperationOutcome`, `retry_classification SubmissionRetryClassification`, `optional response_hash bytes len 32`, `created_at Timestamp required`, `updated_at Timestamp required` |
  | `CompanyReturnSubmissionReceipt` | `id string UUIDv7`, `attempt_id string UUIDv7`, `encrypted_receipt_ref string min 1 max 128 opaque storage reference`, `safe_display_summary string min 1 max 2000`, `optional conversation_id string min 1 max 128 stable identifier`, `optional submission_id string min 1 max 128 stable identifier (explicitly not UUIDv7)`, `received_at Timestamp required`, `response_schema_fingerprint bytes len 32`, `content_hash bytes len 32`; CEL requires at least one external identifier |
  | `CompanyReturnStatusObservation` | `id string UUIDv7`, `attempt_id string UUIDv7`, `operation_type CompanyReturnOperationType`, `stable_result_code string min 1 max 128`, `safe_status string min 1 max 512`, `observed_at Timestamp required`, `response_hash bytes len 32` |
  | `CompanyReturnSubmission` | `return_id string UUIDv7`, `latest_attempt CompanyReturnSubmissionAttempt required`, `receipt CompanyReturnSubmissionReceipt optional`, `status_history repeated CompanyReturnStatusObservation max 200` |

- [ ] Define the exact RPC descriptors:

  | RPC | Request fields in numeric order | Response fields |
  |---|---|---|
  | `PreLodgeCompanyReturn` | `command_context CommandContext required`, `organisation_id string UUIDv7`, `return_id string UUIDv7`, `declaration_id string UUIDv7`, `expected_return_version uint64 >=1` | `company_return CompanyReturn required`, `submission CompanyReturnSubmission required` |
  | `LodgeCompanyReturn` | `command_context CommandContext required`, `organisation_id string UUIDv7`, `return_id string UUIDv7`, `declaration_id string UUIDv7`, `expected_return_version uint64 >=1` | `company_return CompanyReturn required`, `submission CompanyReturnSubmission required` |
  | `GetCompanyReturnSubmission` | `authentication AuthenticationContext required`, `organisation_id string UUIDv7`, `return_id string UUIDv7` | `company_return CompanyReturn required`, `submission CompanyReturnSubmission required` |
  | `RefreshCompanyReturnStatus` | `command_context CommandContext required`, `organisation_id string UUIDv7`, `return_id string UUIDv7`, `attempt_id string UUIDv7`, `expected_return_version uint64 >=1` | `company_return CompanyReturn required`, `submission CompanyReturnSubmission required` |
  | `ReconcileUnknownCompanyReturnSubmission` | `command_context CommandContext required`, `organisation_id string UUIDv7`, `return_id string UUIDv7`, `attempt_id string UUIDv7`, `expected_return_version uint64 >=1` | `company_return CompanyReturn required`, `submission CompanyReturnSubmission required` |

- [ ] Run `rtk pnpm proto:format && rtk pnpm proto:generate`; get the structural reflection tests green before writing behavioral validation tests.

- [ ] Before adding conditional rules, add executable Protovalidate tests that reject a receipt with neither external identifier, outcome before dispatch, missing outcome after a recorded result, `OUTCOME_UNKNOWN` on `RESULT_RECORDED`/`COMMITTED`, a definitive outcome on `OUTCOME_UNKNOWN`, pre-lodge delivery/receipt, delivered lodge without receipt, receipt without delivered lodge, and missing/wrong-purpose fresh factors; include matching valid pending, unknown, rejected, and delivered fixtures. Run the focused contract test and expect failure because these structurally valid messages are still accepted.

- [ ] Add message-level CEL requiring at least one receipt external identifier; `outcome` is absent in `PREPARED`, `DISPATCHING`, `NOT_DISPATCHED`, and `ABORTED`; `OUTCOME_UNKNOWN` state has exactly the unknown outcome; `RESULT_RECORDED` and `COMMITTED` have only definitive `SUCCESS`, `WARNINGS`, or `REJECTED`; and a receipt exists iff the operation is `LODGE`, the definitive outcome is accepted `SUCCESS`, and the returned company return is `DELIVERED`. Assert `command_context.fresh_factor` purposes `company_return_prelodge`, `company_return_lodge`, and `company_return_reconcile_unknown`. Core/helper later select every authority field from signed installed state. Pre-lodge can never create a receipt or `DELIVERED`, and reconciliation must retain the original attempt's operation type and identity.

- [ ] Regenerate after adding CEL, then run `rtk mise exec -- go test ./services/core/internal/contracts/... ./services/core/internal/annualreporting/...`; expect PASS.

- [ ] Commit:

  ```bash
  rtk git add proto/tammy/v1/company_return_submission.proto services/core/internal/contracts/company_eofy_contract_test.go services/core/internal/gen/tammy/v1 packages/connect-client/src/gen/tammy/v1
  rtk git commit -m "feat: define company return submission contracts"
  ```

### Task 5: Export and round-trip the generated TypeScript contracts

**Files:**

- Modify: `packages/connect-client/package.json`
- Create: `packages/connect-client/src/company-eofy-contract.test.ts`

- [ ] First add a Vitest that imports the three future public paths from `@tammy/connect-client`, constructs and binary-round-trips one valid request and response for each service, and asserts a dynamic JSON/map field cannot be found through the generated descriptors. Run `rtk pnpm --filter @tammy/connect-client test`; expect module-resolution failure because the exports are absent.

- [ ] Add exactly these exports, pointing to the generated TypeScript modules: `./tammy/v1/financial_close_pb.js`, `./tammy/v1/company_tax_pb.js`, and `./tammy/v1/company_return_submission_pb.js`.

- [ ] Run:

  ```bash
  rtk pnpm contracts
  rtk mise exec -- go test ./services/core/internal/contracts/... ./services/core/internal/annualreporting/...
  rtk pnpm --filter @tammy/connect-client test
  rtk pnpm --filter @tammy/connect-client typecheck
  ```

  Expected: PASS. Inspect generated Connect interfaces and confirm all 30 fixed RPCs are present once. Do not hand-edit generated files.

- [ ] Commit:

  ```bash
  rtk git add packages/connect-client/package.json packages/connect-client/src/company-eofy-contract.test.ts
  rtk git commit -m "test: expose company EOFY generated contracts"
  ```

## Chunk 2: Permission policy, fail-closed capability, and future coverage

### Task 6: Add explicit close, preparation, declaration, and lodge permissions

**Files:**

- Modify: `services/core/internal/authorisation/policy.go`
- Modify: `services/core/internal/authorisation/policy_test.go`
- Modify: `docs/development/company-return-2026-lifecycle.md`

- [ ] Append this exact permission matrix to the lifecycle authority document. It describes the action passed to the core authorization boundary; fresh-factor and aggregate-state checks are additional requirements, never substitutes for role authorization.

  | Operation group | Authorization action | workspace_admin | business_preparer | business_lodger | auditor |
  |---|---|---:|---:|---:|---:|
  | Get/list close, statements, profile, return, facts, submission/status | `read_financial` | allow | allow | allow | allow |
  | Create/resolve/reopen/correct close; edit profile/return/adjustment/election; validate/export; create replacement/amendment | `prepare_tax` | allow | allow | deny | deny |
  | Freeze and approve exact financial statements | `approve_financial_close` | deny | deny | allow | deny |
  | Acknowledge official warning; declare; withdraw declaration | `declare_company_return` | deny | deny | allow | deny |
  | Pre-lodge, lodge, refresh delivery status, reconcile unknown outcome | `lodge` | deny | deny | allow | deny |

  A sole operator may hold both administrator/preparer and lodger roles. Workspace administration alone does not silently grant legal approval/declaration/lodge authority.

- [ ] Add failing policy tests for two new actions, `ActionApproveFinancialClose` (`approve_financial_close`) and `ActionDeclareCompanyReturn` (`declare_company_return`), over every public role. Also add a table mapping all 30 RPC names to the expected existing/new action and assert the five groups above. Run `rtk mise exec -- go test ./services/core/internal/authorisation/...`; expect the two new actions to deny everyone because they are unknown.

- [ ] Add the two action constants and role sets to the deny-by-default policy. Do not create per-RPC role checks, duplicate role enums, or special administrator bypasses.

- [ ] Extend fresh-factor tests with the exact purpose vocabulary reserved in Chunk 1:

  ```text
  financial_close_freeze
  financial_close_reopen
  financial_close_start_correction
  company_tax_edit_secrets
  company_return_acknowledge_warning
  company_return_declare
  company_return_withdraw_declaration
  company_return_export
  company_return_prelodge
  company_return_lodge
  company_return_reconcile_unknown
  ```

  Reuse `ValidateFreshFactor`; do not add a second freshness clock or a looser duration. Purpose mismatch, future timestamp, exactly-five-minutes-old assertion, malformed ID, and missing marker all fail.

- [ ] Run `rtk mise exec -- go test ./services/core/internal/authorisation/... -race -count=1`; expect PASS.

- [ ] Commit:

  ```bash
  rtk git add services/core/internal/authorisation docs/development/company-return-2026-lifecycle.md
  rtk git commit -m "feat: authorize company EOFY responsibilities"
  ```

### Task 7: Expose an honest four-mode Company Return 2026 capability

**Files:**

- Modify: `proto/tammy/v1/reporting_capability.proto`
- Create: `services/core/internal/contracts/reporting_capability_contract_test.go`
- Modify: `services/core/internal/reportingcapability/registry.go`
- Modify: `services/core/internal/reportingcapability/registry_test.go`
- Modify: `services/core/internal/reportingcapability/service_test.go`
- Modify: `apps/desktop/tests/e2e/foundation.spec.ts`
- Regenerate: `services/core/internal/gen/tammy/v1/reporting_capability.pb.go`
- Regenerate: `services/core/internal/gen/tammy/v1/tammyv1connect/reporting_capability.connect.go`
- Regenerate: `packages/connect-client/src/gen/tammy/v1/reporting_capability_pb.ts`

- [ ] Add failing descriptor tests requiring `REPORT_KIND_COMPANY_TAX_RETURN`, `REPORTING_ENTITY_TYPE_AU_PRIVATE_COMPANY`, and a four-mode capability projection. Add registry tests for the exact key `COMPANY_TAX_RETURN / AU_PRIVATE_COMPANY / 2026`, every unlisted year/entity/report combination, and independent returned values. Run:

  ```bash
  rtk mise exec -- go test ./services/core/internal/contracts/... ./services/core/internal/reportingcapability/...
  ```

  Expected: FAIL because the new key and mode projection do not exist.

- [ ] Add `ReportingCapabilityMode {PREPARATION, SIMULATOR, EVTE, PRODUCTION}` and `ReportingModeAvailability {NOT_IMPLEMENTED, AVAILABLE, EXTERNAL_GATED}` with zero sentinels. Add this exact message and append it to `ReportingCapability` as field 8:

  | Message | Fields in numeric order |
  |---|---|
  | `ReportingModeCapability` | `mode ReportingCapabilityMode`, `availability ReportingModeAvailability`, `required_bundle_id optional string max 128`, `activated_bundle_fingerprint optional bytes len 32`, `required_service_name optional string max 128`, `summary string min 1 max 512`, `blockers repeated string max 16, each stable code max 128` |

  `modes` is `repeated ReportingModeCapability` with exactly four unique modes. Keep the existing aggregate `ReportingCapability.status` as a concise current-build summary; mode entries are the authoritative per-mode explanation, not a compatibility alias.

- [ ] Add `REPORTING_ENTITY_TYPE_AU_PRIVATE_COMPANY` and `REPORT_KIND_COMPANY_TAX_RETURN` directly with new unique values. Do not reuse `AU_BUSINESS`, infer a company from organisation metadata, or add an alias for another entity/report kind.

- [ ] Regenerate after the structural changes, then add executable Protovalidate tests that reject duplicate/missing modes, zero mode/availability, a fingerprint other than 32 bytes, and missing summaries. Add the message-level CEL needed for exactly one of each mode; regenerate and make the tests pass.

- [ ] Add the 2026 registry entry with overall `UNSUPPORTED` and the exact four rows below. This plan defines contracts and capability honesty only; it must not claim preparation or simulator behavior exists.

  | Mode | Availability | Required bundle/service | Stable blockers |
  |---|---|---|---|
  | `PREPARATION` | `NOT_IMPLEMENTED` | bundle `au-company-return-2026-preparation-v1` | `COMPANY_RETURN_PREPARATION_NOT_IMPLEMENTED` |
  | `SIMULATOR` | `NOT_IMPLEMENTED` | bundle `au-company-return-2026-preparation-v1` | `COMPANY_RETURN_SIMULATOR_NOT_IMPLEMENTED` |
  | `EVTE` | `NOT_IMPLEMENTED` | official service name `Company return 2026`; no invented service ID | `COMPANY_RETURN_DELIVERY_NOT_IMPLEMENTED`, `DSP_REGISTRATION_REQUIRED`, `OFFICIAL_SERVICE_ARTEFACTS_REQUIRED`, `EVTE_ACCESS_REQUIRED`, `CONFORMANCE_REQUIRED` |
  | `PRODUCTION` | `NOT_IMPLEMENTED` | official service name `Company return 2026`; no invented service ID | all EVTE blockers plus `PRODUCT_ID_REQUIRED`, `PRODUCTION_ACCESS_REQUIRED`, `RAM_MACHINE_CREDENTIAL_REQUIRED`, `RELEASE_APPROVAL_REQUIRED` |

  Summaries must say that contracts alone do not prepare, validate, simulate, or lodge a return. Leave `activated_bundle_fingerprint` absent in every row. `EXTERNAL_GATED` is not used while an internal `*_NOT_IMPLEMENTED` blocker remains; later owning stages may promote only their implemented mode and add the exact installed fingerprint.

- [ ] Update the existing GST/BAS/individual entries to return four unique mode rows with honest current statuses, so every successful capability response satisfies the new invariant. GST workpaper may mark only `PREPARATION` available; BAS and individual remain unavailable. Unknown combinations return four `NOT_IMPLEMENTED` rows and the existing unsupported summary.

- [ ] Extend the existing packaged `reporting/capability-registry` journey in `foundation.spec.ts`. Let its helper accept report, entity, and year; query `COMPANY_TAX_RETURN / AU_PRIVATE_COMPANY / 2026`; assert overall `UNSUPPORTED`, exactly ordered `PREPARATION`, `SIMULATOR`, `EVTE`, `PRODUCTION` rows, all four `NOT_IMPLEMENTED`, exact blocker arrays from the table, required preparation bundle/service names, and absent activated fingerprints. Assert the app version equals the GST/BAS answers.

- [ ] Run:

  ```bash
  rtk pnpm proto:format
  rtk pnpm proto:generate
  rtk mise exec -- go test ./services/core/internal/contracts/... ./services/core/internal/reportingcapability/... -race -count=1
  rtk pnpm --filter @tammy/connect-client typecheck
  rtk pnpm desktop:package
  rtk pnpm --dir apps/desktop e2e --grep "runs the packaged first-run journey offline and exits cleanly"
  ```

  Expected: PASS. No registry row claims direct lodgment, EVTE readiness, production readiness, or an activated 2026 preparation bundle.

- [ ] Commit:

  ```bash
  rtk git add proto/tammy/v1/reporting_capability.proto services/core/internal/contracts services/core/internal/reportingcapability services/core/internal/gen/tammy/v1 packages/connect-client/src/gen/tammy/v1 apps/desktop/tests/e2e/foundation.spec.ts
  rtk git commit -m "feat: declare fail-closed company return capability"
  ```

### Task 8: Catalogue all future RPCs and lifecycle edges without exposing preload methods

**Files:**

- Modify: `test/e2e/coverage.yaml`
- Regenerate: `test/e2e/transitions.yaml`
- Modify: `scripts/check-e2e-coverage.test.mjs`
- Modify: `scripts/slice-one-coverage-policy.mjs`
- Modify: `scripts/check-slice-one-coverage-policy.test.mjs`

- [ ] Add a failing E2E coverage test that loads the real descriptor set/manifest and requires all 30 new RPCs plus every financial-close, company-return, and attempt transition to be catalogued. It must also assert that none of the planned lower-camel preload names appears in `apps/desktop/src/shared/preload-methods.json`. Run:

  ```bash
  rtk pnpm proto:descriptors:check
  rtk mise exec -- node --test scripts/check-e2e-coverage.test.mjs
  ```

  Expected: FAIL with missing company-EOFY RPC/transition coverage.

- [ ] Add `E2E-18` with these unique future cases and no executed cases:

  ```yaml
  E2E-18:
    cases: []
    futureCases:
      - company-eofy/contracts
      - company-eofy/lifecycle
      - company-eofy/permissions
      - company-eofy/financial-close
      - company-eofy/company-return
      - company-eofy/submission
  ```

- [ ] Add every new RPC with `stage: declared_future`, `cases: []`, its exact future-case set below, and the lower-camel RPC name as planned `preload`. Do not edit `preload-methods.json`, `desktop-api.ts`, main IPC/router, or preload implementation in this plan; the coverage checker must continue proving future methods are absent.

- [ ] In the table below use these exact shorthands when authoring both `coverage.yaml` and `SLICE_ONE_RPC_POLICY`:

  - Future cases: `FC = [company-eofy/contracts, company-eofy/permissions, company-eofy/financial-close]`; `CR = [company-eofy/contracts, company-eofy/permissions, company-eofy/company-return]`; `SUB = [company-eofy/contracts, company-eofy/permissions, company-eofy/submission]`.
  - Roles: `R = all four planned_allowed`; `P = workspace_admin/business_preparer planned_allowed, business_lodger/auditor denied`; `L = business_lodger planned_allowed, all others denied`.
  - Idempotency: `Q = query / [not_applicable]`; `C = persistent_command / [exact_replay, changed_request_conflict]`.
  - Failure prefixes: `RQ = [AUTHENTICATION_REQUIRED, NOT_FOUND]`; `PC = [AUTHENTICATION_REQUIRED, PERMISSION_DENIED, IDEMPOTENCY_CONFLICT]`; `PM = [AUTHENTICATION_REQUIRED, PERMISSION_DENIED, STALE_VERSION, IDEMPOTENCY_CONFLICT]`; `PF = PM + [FACTOR_ASSERTION_REQUIRED, FACTOR_ASSERTION_STALE]`.
  - List states are exact arrays; `NA` means `[not_applicable]`.

  | RPC | Cases | Projection | Route | Roles | Failures (exact order) | List | Idem |
  |---|---|---|---|---|---|---|---|
  | `FinancialCloseService.CreateFinancialClose` | FC | `create_financial_close_result` | `/eofy-company-tax/close` | P | PC + `INVALID_PERIOD`, `REPORT_BUNDLE_UNAVAILABLE` | NA | C |
  | `FinancialCloseService.GetFinancialClose` | FC | `get_financial_close_result` | close | R | RQ | `found,not_found` | Q |
  | `FinancialCloseService.ListCloseChecks` | FC | `list_close_checks_result` | close | R | RQ + `INVALID_CURSOR` | `empty,populated,filtered,paginated` | Q |
  | `FinancialCloseService.ResolveCloseWarning` | FC | `resolve_close_warning_result` | close | P | PM + `INVALID_STATE_TRANSITION` | NA | C |
  | `FinancialCloseService.FreezeFinancialClose` | FC | `freeze_financial_close_result` | close | L | PF + `FINANCIAL_CLOSE_BLOCKED` | NA | C |
  | `FinancialCloseService.ReopenFinancialClose` | FC | `reopen_financial_close_result` | close | P | PF + `INVALID_STATE_TRANSITION`, `REPORT_REPLACEMENT_REQUIRED`, `AMENDMENT_REQUIRED` | NA | C |
  | `FinancialCloseService.StartFinancialCloseCorrection` | FC | `start_financial_close_correction_result` | close | P | PF + `INVALID_STATE_TRANSITION`, `DECLARATION_REQUIRED` | NA | C |
  | `FinancialCloseService.GetFinancialStatements` | FC | `get_financial_statements_result` | close | R | RQ | `found,not_found` | Q |
  | `CompanyTaxService.GetCompanyTaxProfile` | CR | `get_company_tax_profile_result` | `/eofy-company-tax/return` | R | RQ | `found,not_found` | Q |
  | `CompanyTaxService.SetCompanyTaxProfile` | CR | `set_company_tax_profile_result` | return | P | PF + `UNSUPPORTED_COMPANY_SCENARIO` | NA | C |
  | `CompanyTaxService.CreateCompanyReturn` | CR | `create_company_return_result` | return | P | PC + `SOURCE_CLOSE_STALE`, `UNSUPPORTED_COMPANY_SCENARIO`, `REPORT_BUNDLE_UNAVAILABLE` | NA | C |
  | `CompanyTaxService.GetCompanyReturn` | CR | `get_company_return_result` | return | R | RQ | `found,not_found` | Q |
  | `CompanyTaxService.ListCompanyReturnFacts` | CR | `list_company_return_facts_result` | return | R | RQ + `INVALID_CURSOR` | `empty,populated,paginated` | Q |
  | `CompanyTaxService.SetCompanyReturnInput` | CR | `set_company_return_input_result` | return | P | PM + `SOURCE_CLOSE_STALE`, `UNSUPPORTED_COMPANY_SCENARIO`, `REPORT_BUNDLE_UNAVAILABLE` | NA | C |
  | `CompanyTaxService.UpsertTaxAdjustment` | CR | `upsert_tax_adjustment_result` | return | P | PM + `UNSUPPORTED_COMPANY_SCENARIO`, `REPORT_BUNDLE_UNAVAILABLE`, `INVALID_STATE_TRANSITION` | NA | C |
  | `CompanyTaxService.RemoveTaxAdjustment` | CR | `remove_tax_adjustment_result` | return | P | PM + `NOT_FOUND`, `INVALID_STATE_TRANSITION` | NA | C |
  | `CompanyTaxService.UpsertTaxElection` | CR | `upsert_tax_election_result` | return | P | PM + `UNSUPPORTED_COMPANY_SCENARIO`, `REPORT_BUNDLE_UNAVAILABLE`, `INVALID_STATE_TRANSITION` | NA | C |
  | `CompanyTaxService.RemoveTaxElection` | CR | `remove_tax_election_result` | return | P | PM + `NOT_FOUND`, `INVALID_STATE_TRANSITION` | NA | C |
  | `CompanyTaxService.ValidateCompanyReturn` | CR | `validate_company_return_result` | return | P | PM + `SOURCE_CLOSE_STALE`, `UNSUPPORTED_COMPANY_SCENARIO`, `REPORT_BUNDLE_UNAVAILABLE`, `REPORT_VALIDATION_FAILED` | NA | C |
  | `CompanyTaxService.AcknowledgeReturnWarning` | CR | `acknowledge_return_warning_result` | return | L | PF + `NOT_FOUND`, `REPORT_VALIDATION_FAILED`, `INVALID_STATE_TRANSITION` | NA | C |
  | `CompanyTaxService.DeclareCompanyReturn` | CR | `declare_company_return_result` | return | L | PF + `SOURCE_CLOSE_STALE`, `UNSUPPORTED_COMPANY_SCENARIO`, `REPORT_BUNDLE_UNAVAILABLE`, `REPORT_VALIDATION_FAILED` | NA | C |
  | `CompanyTaxService.WithdrawCompanyReturnDeclaration` | CR | `withdraw_company_return_declaration_result` | return | L | PF + `DECLARATION_REQUIRED`, `INVALID_STATE_TRANSITION`, `REPORT_REPLACEMENT_REQUIRED` | NA | C |
  | `CompanyTaxService.ExportCompanyReturnPack` | CR | `export_company_return_pack_result` | return | P | PF + `NOT_FOUND`, `REPORT_BUNDLE_UNAVAILABLE` | NA | C |
  | `CompanyTaxService.CreateCompanyReturnReplacement` | CR | `create_company_return_replacement_result` | return | P | PM + `REPORT_REPLACEMENT_REQUIRED`, `PRELODGE_OUTCOME_UNKNOWN`, `LODGE_OUTCOME_UNKNOWN`, `SOURCE_CLOSE_STALE`, `INVALID_STATE_TRANSITION` | NA | C |
  | `CompanyTaxService.CreateCompanyReturnAmendment` | CR | `create_company_return_amendment_result` | return | P | PM + `AMENDMENT_REQUIRED`, `SUBSEQUENT_AMENDMENT_UNSUPPORTED`, `LODGE_OUTCOME_UNKNOWN`, `SOURCE_CLOSE_STALE`, `INVALID_STATE_TRANSITION` | NA | C |
  | `CompanyReturnSubmissionService.PreLodgeCompanyReturn` | SUB | `pre_lodge_company_return_result` | `/eofy-company-tax/lodge` | L | PF + `DECLARATION_REQUIRED`, `SOURCE_CLOSE_STALE`, `REPORT_BUNDLE_UNAVAILABLE`, `SBR_CREDENTIAL_NOT_READY`, `SBR_PRODUCT_SERVICE_NOT_READY`, `PRELODGE_OUTCOME_UNKNOWN` | NA | C |
  | `CompanyReturnSubmissionService.LodgeCompanyReturn` | SUB | `lodge_company_return_result` | lodge | L | PF + `DECLARATION_REQUIRED`, `PRELODGE_REVIEW_REQUIRED`, `PRELODGE_OUTCOME_UNKNOWN`, `REPORT_BUNDLE_UNAVAILABLE`, `SBR_CREDENTIAL_NOT_READY`, `SBR_PRODUCT_SERVICE_NOT_READY`, `ATO_VALIDATION_FAILED`, `LODGE_OUTCOME_UNKNOWN` | NA | C |
  | `CompanyReturnSubmissionService.GetCompanyReturnSubmission` | SUB | `get_company_return_submission_result` | lodge | R | RQ | `found,not_found` | Q |
  | `CompanyReturnSubmissionService.RefreshCompanyReturnStatus` | SUB | `refresh_company_return_status_result` | lodge | L | PM + `SBR_CREDENTIAL_NOT_READY`, `SBR_PRODUCT_SERVICE_NOT_READY`, `PRELODGE_OUTCOME_UNKNOWN`, `LODGE_OUTCOME_UNKNOWN` | NA | C |
  | `CompanyReturnSubmissionService.ReconcileUnknownCompanyReturnSubmission` | SUB | `reconcile_unknown_company_return_submission_result` | lodge | L | PF + `SBR_CREDENTIAL_NOT_READY`, `SBR_PRODUCT_SERVICE_NOT_READY`, `PRELODGE_OUTCOME_UNKNOWN`, `LODGE_OUTCOME_UNKNOWN`, `INVALID_STATE_TRANSITION` | NA | C |

  Prefix every service name with `tammy.v1.` in the actual files. Replace route shorthand `close`, `return`, and `lodge` with the full route from the first row of that group. Expand every shorthand into literal YAML arrays; the policy module may reuse named JavaScript constants but must produce byte-for-byte-equivalent objects.

- [ ] Extend `SLICE_ONE_RPC_POLICY` with the same 30 entries and add a `COMPANY_EOFY_DECLARED_FUTURE_RPCS` export containing their exact fully-qualified names. Update `check-slice-one-coverage-policy.test.mjs` from 88 to 118 non-system RPCs; assert all 30 are `declared_future`, have the exact future cases above, and are absent from `preload-methods.json`. Add the three new proto paths to its request concurrency/failure audit. Extend that audit's stale-version predicate from only `expected_version` to the exact set `expected_version`, `expected_return_version`, `expected_predecessor_version`, and `expected_latest_version`; add one positive assertion for each spelling and keep a negative assertion for requests with none.

- [ ] Add every generated transition ID with `stage: declared_future`, `cases: []`, and `futureCases: [company-eofy/lifecycle]`. Run `rtk pnpm transitions:generate` before coverage validation.

- [ ] Run:

  ```bash
  rtk pnpm proto:descriptors:check
  rtk pnpm transitions:check
  rtk pnpm e2e:coverage
  rtk mise exec -- node --test scripts/build-transition-index.test.mjs scripts/check-e2e-coverage.test.mjs scripts/check-slice-one-coverage-policy.test.mjs
  ```

  Expected: PASS. Then run `rtk pnpm contracts:production` and expect it to fail specifically with `E2E_COVERAGE_FUTURE_PROMOTION_REQUIRED`; that failure is evidence that future contracts cannot enter a production release accidentally.

- [ ] Commit:

  ```bash
  rtk git add test/e2e/coverage.yaml test/e2e/transitions.yaml scripts/check-e2e-coverage.test.mjs scripts/slice-one-coverage-policy.mjs scripts/check-slice-one-coverage-policy.test.mjs
  rtk git commit -m "test: catalogue future company EOFY surface"
  ```

### Task 9: Verify and hand off the contract foundation

**Files:**

- Modify only if a verification defect is found in files already owned by Tasks 1–8.

- [ ] From a clean generated state, run the focused gates:

  ```bash
  rtk pnpm contracts
  rtk pnpm transitions:check
  rtk mise exec -- go test ./services/core/internal/contracts/... ./services/core/internal/annualreporting/... ./services/core/internal/authorisation/... ./services/core/internal/reportingcapability/... -race -count=1
  rtk mise exec -- node --test scripts/build-transition-index.test.mjs scripts/check-e2e-coverage.test.mjs scripts/check-slice-one-coverage-policy.test.mjs
  rtk pnpm --filter @tammy/connect-client test
  rtk pnpm --filter @tammy/connect-client typecheck
  ```

- [ ] Run the repository-wide non-packaged regression gates:

  ```bash
  rtk pnpm test
  rtk pnpm typecheck
  rtk pnpm lint
  rtk pnpm desktop:package
  rtk pnpm --dir apps/desktop e2e --grep "runs the packaged first-run journey offline and exits cleanly"
  rtk git diff --check
  ```

  Expected: PASS. The 30 business RPCs remain future and receive no renderer journey yet, but the already-production capability RPC must prove the new 2026 company key and four fail-closed mode rows through the packaged pre-setup journey.

- [ ] Inspect `rtk git status --short`, confirm generated files and manifests are committed, and confirm `apps/desktop/src/shared/preload-methods.json` is unchanged.

- [ ] If verification required a fix, commit only the focused correction with a descriptive message. Otherwise do not create an empty commit.

### Stage 1 acceptance gate

- The exact 2026 close/return/submission contract and lifecycle authority compile and pass structural plus behavioral validation tests.
- Every lifecycle edge and operation-specific outcome is explicit; pre-lodge cannot deliver, unknown outcomes cannot be retried blindly, and replacements/amendments preserve predecessors.
- The role policy separates preparation, close approval, declaration, and lodgment.
- The capability service recognizes the company-return capability only for `COMPANY_TAX_RETURN / AU_PRIVATE_COMPANY / 2026` and reports all four modes honestly `NOT_IMPLEMENTED` at Stage 1 while retaining the exact future external blockers; existing GST/BAS/individual entries remain independently honest.
- All 30 RPCs and all transitions are visible to descriptor/coverage review but absent from Electron preload and production-required coverage.
- The next implementation plan may consume these contracts without changing their semantics; a semantic change requires amending the approved design first.
