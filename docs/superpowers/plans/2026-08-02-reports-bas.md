# Financial Reports and BAS Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Slice 5: complete cross-reconciled financial reports, cash/non-cash GST detail, revision-pinned BAS workpapers, local validation/declaration/simulator states, evidence exports, invalidation/correction, and packaged canonical accounting oracles.

**Architecture:** One monotonic financial revision and module-local revisions form a retained `RevisionVector`. Reporting reads only immutable projections through `FinancialReportReadPort` in one snapshot transaction; Tax consumes typed accounting/settlement facts and owns versioned BAS calculations/state. Generated report/BAS snapshots pin canonical queries, source hashes, vectors, rule bundles, provenance, and content hashes so later mutations invalidate rather than rewrite them.

**Tech Stack:** Buf/Protobuf, Go/Connect/SQLCipher, deterministic PDF/CSV/Protobuf evidence rendering, Electron/React/TypeScript, Vitest, Playwright.

---

**Normative design:** `docs/superpowers/specs/2026-08-02-core-business-accounting-suite-design.md` §§5.3–5.5, 8, 11–15 and foundation SBR state rules.

**Prerequisite:** Slice 4 is green and all accounting, subledger, tax-fact, and banking projections reconcile.

**Required skills while executing:** `@superpowers:test-driven-development`, `@security-best-practices`, `@frontend-design`, `@playwright`, and `@superpowers:verification-before-completion`.

**Micro-TDD rule:** Add each named formula, transition, mutation-class, and export case individually; run its narrow test and observe the named failure, implement only that behavior, rerun to `PASS`, then continue. Never postpone a red case to a later task.

## Slice 5 RPC and UoW map

| Service/RPCs | Owner, required ports, and class | Named preload/route | Scenario |
|---|---|---|---|
| Reporting `GenerateReport` | ordinary persisted snapshot command for every accounting-read role (admin, preparer, lodger, auditor), including `YEAR_END_CLOSE_PREVIEW`; FinancialReportReadPort, revision repository, AuditAppender, one UoW | `generateReport`; `/reports` | E2E-12/E2E-14 |
| Reporting `GetReport`, `ListReports` | read query for all authenticated roles including auditor; pinned Reporting projections in `UoW.Read` | matching lower-camel; `/reports` | E2E-12 |
| Reporting `ExportReport`, `CancelReportExport`, `RetryReportExport` | ordinary persisted render-job commands for every accounting-read role; retained snapshot, encrypted artefact store, approved destination, AuditAppender; cancel/retry requires original job owner or admin | matching lower-camel; `/reports` | E2E-12 |
| Reporting `GetReportExportJob`, `ListReportExportJobs` | read query for all authenticated roles including auditor | matching lower-camel; `/reports` | E2E-12 |
| Tax `CreateBASWorkpaper`, `ValidateBAS`, `RecordAdjustment` | ordinary persistent admin/preparer commands; AccountingTaxFactPort, SettlementTaxFactPort, OrganisationReadPort, ArtefactReadPort/TaxRules, AccountingPostingPort for adjustment reclassification, TaxReportImpactPort, AuditAppender in one UoW | matching lower-camel; `/tax/bas` | E2E-13/E2E-14 |
| Tax `AcceptDeclaration` | ordinary business-lodger command plus new fresh TOTP and matching content hash; Tax/Audit in one UoW | `acceptDeclaration`; `/tax/bas` | E2E-13 |
| Tax `GetBASWorkpaper`, `ListBASWorkpapers` | read query for all authenticated roles including auditor | matching lower-camel; `/tax/bas` | E2E-13/E2E-14 |
| Tax `PrepareBASSimulation`, `CancelPreparedBASSimulation` | ordinary persistent business-lodger commands with expected version/idempotency; prepare commits identifiers/payload hash in `PREPARED`, cancel commits `PREPARED → CANCELLED`; AuditAppender | matching lower-camel; `/tax/simulator` | E2E-13 |
| Tax `SubmitBASSimulation` | durable external-dispatch command for business-lodger plus new fresh TOTP and operation key/expected transmission version: commit `DISPATCHING`, invoke simulator gateway outside SQL, then atomically commit response/evidence/state/audit or typed send-phase outcome | `submitBASSimulation`; `/tax/simulator` | E2E-13 |
| Tax `ReconcileBASSimulation` | durable reconciliation command for business-lodger without a new TOTP, with operation key/expected UNKNOWN transmission version: load original identifiers/payload hash, invoke gateway outside SQL, atomically commit conclusive evidence/state/audit; inconclusive retains `UNKNOWN` | `reconcileBASSimulation`; `/tax/simulator` | E2E-13 |
| Tax `GetBASSimulation`, `ListBASSimulations` | read query for all authenticated roles including auditor | matching lower-camel; `/tax/simulator` | E2E-13 |
| Tax `ExportBAS`, `CancelBASExport`, `RetryBASExport` | ordinary persisted render-job commands for admin/preparer/lodger/auditor; retained BAS/evidence/audit exporter, approved destination, AuditAppender; cancel/retry requires original job owner or admin | matching lower-camel; `/tax/bas` | E2E-13 |
| Tax `GetBASExportJob`, `ListBASExportJobs` | read query for all authenticated roles including auditor | matching lower-camel; `/tax/bas` | E2E-13 |

Every descriptor row is added to `coverage.yaml` with all roles/transitions/failures/replay/list states. Production ATO/SBR RPCs, routes, endpoints, credentials, and adapters remain absent. Generation produces exactly `services/core/internal/gen/tammy/v1/{reporting,tax,accounting,settlements,banking,audit,events,fixtures}.pb.go`, `packages/connect-client/src/gen/tammy/v1/{reporting,tax,accounting,settlements,banking,audit,events,fixtures}_pb.ts`, and `services/core/internal/gen/tammy/v1/tammyv1connect/{reporting,tax,accounting,settlements,banking,audit}.connect.go`; lock/sum/package-export changes are committed atomically whenever changed.

**Per-task red/green index:** Task 1 starts with `rtk go test ./services/core/internal/contracts -run '^TestReportingTaxDescriptor/CreateBASWorkpaper$'` and `rtk pnpm --filter @tammy/desktop test -- -t 'tax preload registry'`; Task 2 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/reporting -run '^TestReportRepository/immutable_content_hash$'`; Task 3 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/revisions -run '^TestFinancialChangeSet/invoice_draft_update$'`; Task 4 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/reporting -run '^TestReportSnapshot/competing_revision$'`; Task 5 with `rtk go test ./services/core/internal/reporting -run '^TestBalanceSheet/current_year_earnings$'`; Task 6 with `rtk go test ./services/core/internal/reporting -run '^TestIndirectCashFlow/working_capital_sign$'`; Task 7 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/reporting -run '^TestGSTDetail/control_account_equality$'`; Task 8 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/tax -run '^TestBASCalculator/truncate_positive_99_cents$'`; Task 9 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/tax -run '^TestBASSimulator/unknown_not_resubmittable$'`; Task 10 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/exports -run '^TestEvidenceExport/payload_byte_hash$'`; Task 11 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/backup -run '^TestBackup/report_and_bas_artefacts$'`; Task 12 with `rtk pnpm --filter @tammy/desktop test -- -t 'local simulation not lodged'`; and Task 13 with `rtk pnpm exec node --test --test-name-pattern 'canonical month cross-projection oracle' scripts/check-core-accounting-evidence.test.mjs`, initially returning `SLICE_EVIDENCE_ORACLE_MISSING`. Each new case first exposes its named missing symbol/codec, state/formula mismatch, partial-state assertion, or missing evidence tuple; implement only that case and rerun the identical owning-package command to `PASS` before the broad task command.

## Financial revision/source-impact wiring table

| Mutation class | Existing service files marked/verified in this slice | Local revisions and tax impact |
|---|---|---|
| Account classification and manual/opening/period journal | `accounting/accounts.go`, `posting_service.go`, `opening_conversion_service.go`, `period_service.go` | financial + ledger + tax-source as applicable; `TaxReportImpactPort` |
| Organisation profile/GST/rule/ownership | `organisations/service.go`, `ownership_service.go`, `artefacts/rule_repository.go` | financial + organisation-profile + rule-bundle; `OrganisationImpactPort` then Tax impact in same TxScope |
| Contact snapshot used by an open quote/invoice/bill draft | Contact service plus the owning Sales/Purchases snapshot adapter from Slices 2–3 | financial exactly once only when the edit changes an open source snapshot |
| Quote/invoice draft create/update/cancel/conversion; invoice issue/credit/correction | Sales service files from Slice 2 | financial; ledger + settlement + tax-source only when posted/facts change; Tax impact |
| Bill draft create/update/cancel; approval/credit/correction/evidence reclassification | Purchases service files from Slice 3 | financial; ledger + settlement + tax-source only when posted/facts change; Tax impact |
| Payment/refund/allocation/reversal | Settlement services | financial + settlement + tax-source; Tax impact and AccountingPostingPort if reclassified |
| `RecordAdjustment` (introduced and proven in Task 9) | Tax service | financial + tax-source; Tax impact and AccountingPostingPort reclassification |
| Match/completed-or-undone reconciliation | Banking match/reconciliation services | financial + banking; Tax impact only when source tax facts change |
| Report/BAS generation/export | Reporting/Tax services | no source revision for pure snapshot/export; retained command/audit only |

Tests run every row under success, multiple-effects-one-increment, rollback, exact replay, changed conflict, and competing-write barriers. All ports receive the same active `TxScope`; no module reads another module's tables.

## Chunk 1: Snapshot contracts, revisions, and financial reports

### Task 1: Complete Reporting and Tax Protobuf contracts

**Files:**
- Modify: `proto/tammy/v1/reporting.proto`
- Create: `proto/tammy/v1/tax.proto`
- Modify: `proto/tammy/v1/accounting.proto`
- Modify: `proto/tammy/v1/settlements.proto`
- Modify: `proto/tammy/v1/banking.proto`
- Modify: `proto/tammy/v1/audit.proto`
- Modify: `proto/tammy/v1/events.proto`
- Modify: `proto/tammy/v1/fixtures.proto`
- Create: `services/core/internal/contracts/reporting_tax_proto_test.go`
- Create: `services/core/internal/reporting/service_handler.go`
- Create: `services/core/internal/reporting/service_handler_test.go`
- Create: `services/core/internal/tax/service_handler.go`
- Create: `services/core/internal/tax/service_handler_test.go`
- Modify: `services/core/internal/transport/registrar.go`
- Modify: `services/core/internal/transport/registrar_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/main/rpc-router.ts`
- Modify: `apps/desktop/src/main/rpc-router.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Modify: `packages/connect-client/package.json`
- Modify: `pnpm-lock.yaml`
- Create: `test/fixtures/reporting/report-formulas.pb.json`
- Create: `test/fixtures/reporting/transitions.pb.json`
- Create: `test/fixtures/tax/bas-facts.pb.json`
- Create: `test/fixtures/tax/transitions.pb.json`
- Create: `test/fixtures/tax/simulator-wattle-co.pb.json`
- Modify: `test/e2e/coverage.yaml`

- [ ] Write a failing descriptor test requiring `RevisionVector`, canonical report queries, typed report rows, signed/display amounts, validations/evidence manifests, and exactly the Reporting/Tax/simulator RPCs in the Slice 5 map with report/transmission lifecycles, provenance, and typed failures.
- [ ] Add messages/RPCs compatibly; no report uses maps/floats or untyped row bags. Every persistent Reporting/Tax/BAS/simulator command—including Prepare/Cancel/Submit/Reconcile and export Cancel/Retry—carries a UUID operation key and the expected aggregate/job/transmission version it mutates; exact replay and changed-request conflict follow foundation §6.3 across durable phase commits.
- [ ] Extend the existing `PostManualJournal` request compatibly with optional generated `YearEndClosePreviewRef` fields for report ID, retained preview version, source revision vector, retained preview content hash, and expected retained-earnings account/version. The command revalidates those exact fields under its normal UoW; descriptor/coverage tests retain the existing `E2E-14` command surface rather than introducing a second posting RPC.
- [ ] Create concrete generated Reporting and Tax handler adapters. Each implements its complete generated interface, delegates to narrow report/BAS/state/simulator/export interfaces, and temporarily returns typed `FEATURE_NOT_READY` only until the owning task binds that delegate. Register both once and test that the slice gate rejects any remaining temporary binding.
- [ ] Add every exact generated codec/method to the imported production preload/router registry in this same contract change, with temporary typed-unavailable delegates rather than a generic tunnel; extend package exports for all generated modules.
- [ ] Encode the authoritative BAS source-impact transitions and simulator transitions as generated fixtures consumed by Go, TypeScript, and the coverage checker.
- [ ] Put retained report/export-job lifecycles in `test/fixtures/reporting/transitions.pb.json` and BAS/declaration/simulator/export-job lifecycles in `test/fixtures/tax/transitions.pb.json`; run `rtk pnpm transitions:generate` before `contracts` and fail on index drift.
- [ ] Add golden formula/tax facts for contra accounts, current-year/retained earnings, ageing boundaries, cash-flow facts, cash/non-cash GST, evidence pending, adjustments, and the canonical month.
- [ ] Encode the deterministic simulator golden fixture in `simulator-wattle-co.pb.json`: `Wattle & Co Test Pty Ltd`, simulator-only ABN `11 000 000 560`, period 1 April–30 June 2026, the two foundation §7.3 transactions dated 30 June, declaration version `SIM-AS-DECL-2026-01` with the exact retained declaration text, `SIM.ACCEPTED`, receipt `SIM-2026-Q4-0001`, and `$80` payable. Mark every simulator-only identity/declaration/response value impossible to select for EVTE or production.
- [ ] Extend `coverage.yaml` for every RPC/preload/role/transition/failure/replay/list state under `E2E-12`, `E2E-13`, or `E2E-14`.
- [ ] Run `rtk pnpm proto:generate && rtk pnpm contracts && rtk pnpm --filter @tammy/connect-client test && rtk pnpm --filter @tammy/desktop test && rtk go test -tags tammy_sqlcipher ./services/core/internal/reporting/... ./services/core/internal/tax/... ./services/core/internal/app/... ./services/core/internal/transport/...`; verify earlier stored fixtures decode and generated/export/preload/handler trees are clean.
- [ ] Commit: `rtk git add proto services/core/internal/contracts services/core/internal/gen packages/connect-client pnpm-lock.yaml services/core/internal/reporting services/core/internal/tax services/core/internal/app services/core/internal/transport apps/desktop/src/shared apps/desktop/src/main apps/desktop/src/preload test && rtk git commit -m "feat: define reporting and bas contracts"`.

### Task 2: Add revision, report, BAS, and artefact storage

**Files:**
- Create: `services/core/internal/storage/migrations/0040_reporting_tax.sql`
- Create: `services/core/internal/storage/migrations/0040_reporting_tax_test.go`
- Modify: `services/core/internal/revisions/repository.go`
- Modify: `services/core/internal/revisions/repository_test.go`
- Create: `services/core/internal/reporting/repository.go`
- Create: `services/core/internal/reporting/repository_test.go`
- Create: `services/core/internal/tax/repository.go`
- Create: `services/core/internal/tax/repository_test.go`

- [ ] Write repository tests proving one financial-revision increment per committed UoW, independent module revision increments, no increment on rollback/read/idempotent replay, and exact retained vectors.
- [ ] Add normalized revision, report snapshot/row/validation/export-job/artefact, BAS report/calculation/field/provenance/validation/declaration/transmission-simulator/adjustment tables with immutable version rows and unique content/source hashes.
- [ ] Enforce one current calculation per draft version, immutable accepted/rejected snapshots, linked revision/correction reports, and no in-place update of superseded evidence.
- [ ] Encrypt rendered artefacts through the evidence blob store and retain exact payload bytes/hash/descriptor hash/manifest metadata.
- [ ] Add migration tests from every prior schema prefix, staged failure recovery, full invariant verification, and prior fixture decoding.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/storage/... ./services/core/internal/revisions/... ./services/core/internal/reporting/... ./services/core/internal/tax/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/storage services/core/internal/revisions services/core/internal/reporting services/core/internal/tax && rtk git commit -m "feat: persist revisioned reports and bas"`.

### Task 3: Audit and prove the financial revision in every source-changing UoW

**Files:**
- Modify: `services/core/internal/app/command.go`
- Modify: `services/core/internal/accounting/posting_service.go`
- Modify: `services/core/internal/contacts/service.go`
- Modify: `services/core/internal/sales/quote_service.go`
- Modify: `services/core/internal/sales/invoice_service.go`
- Modify: `services/core/internal/purchases/service.go`
- Modify: `services/core/internal/settlements/service.go`
- Modify: `services/core/internal/banking/match_service.go`
- Modify: `services/core/internal/banking/import_service.go`
- Modify: `services/core/internal/banking/cash_service.go`
- Modify: `services/core/internal/banking/transfer_service.go`
- Modify: `services/core/internal/banking/reconciliation_service.go`
- Modify: `services/core/internal/accounting/accounts.go`
- Modify: `services/core/internal/accounting/opening_conversion_service.go`
- Modify: `services/core/internal/accounting/period_service.go`
- Modify: `services/core/internal/organisations/service.go`
- Modify: `services/core/internal/organisations/ownership_service.go`
- Modify: `services/core/internal/artefacts/rule_repository.go`
- Create: `services/core/internal/revisions/change_set.go`
- Create: `services/core/internal/revisions/change_set_test.go`
- Create: `services/core/internal/revisions/integration_test.go`

- [ ] Write one table row per mutation already implemented in Slices 1–4, including ownership/opening/period; each row names the service method, expected local revisions, financial delta, and required `TaxReportImpactPort` invocation. Use a recording adapter here because the real retained-report adapter is installed in Task 9; Task 9 owns the separate `RecordAdjustment` row and reruns this whole matrix against real retained report outcomes.
- [ ] Prove each successful source-changing UoW—including every quote/invoice/bill draft mutation named in the table—increments `financial_revision` once even when it touches multiple modules; unrelated unused-contact edits, reads, rollback, and replay do not increment.
- [ ] Verify the Slice 1–4 UoW-scoped `FinancialChangeSet`: modules mark typed effects and the command coordinator persists one monotonic revision plus required local revisions immediately before audit/result commit. If a predecessor command is not wired, amend and reaccept that predecessor slice before proceeding; do not defer a missing first-write revision to Slice 5.
- [ ] Include revision-before/after in typed audit metadata without making the release descriptor fingerprint part of the semantic idempotency hash.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/revisions/... ./services/core/internal/app/... ./services/core/internal/accounting/... ./services/core/internal/contacts/... ./services/core/internal/sales/... ./services/core/internal/purchases/... ./services/core/internal/settlements/... ./services/core/internal/banking/... ./services/core/internal/organisations/... ./services/core/internal/artefacts/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/revisions services/core/internal/app services/core/internal/accounting services/core/internal/contacts services/core/internal/sales services/core/internal/purchases services/core/internal/settlements services/core/internal/banking services/core/internal/organisations services/core/internal/artefacts && rtk git commit -m "feat: verify atomic financial revisions"`.

### Task 4: Implement revision-pinned report snapshots and shared read ports

**Files:**
- Modify: `services/core/internal/reporting/financial_report_read_port.go`
- Modify: `services/core/internal/reporting/financial_report_read_port_test.go`
- Create: `services/core/internal/reporting/snapshot_adapter.go`
- Create: `services/core/internal/reporting/snapshot_adapter_test.go`
- Create: `services/core/internal/reporting/snapshot.go`
- Create: `services/core/internal/reporting/snapshot_test.go`
- Create: `services/core/internal/reporting/service.go`
- Create: `services/core/internal/reporting/service_integration_test.go`
- Modify: `services/core/internal/reporting/service_handler.go`
- Modify: `services/core/internal/reporting/service_handler_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Write snapshot tests that read the vector before/after within one SQLite read transaction, inject a competing mutation, and return `FINANCIAL_REVISION_CHANGED` instead of mixed data.
- [ ] Implement `FinancialReportReadPort.Snapshot` as a composition adapter over Accounting, Settlements, and Banking immutable projections; Reporting never reads their tables.
- [ ] Canonicalize and retain the generated query, complete source content hash, revision vector, result content hash, classifications/rule versions, organisation heading, basis, filters, and generation instant.
- [ ] Require expected financial revision when supplied and use the complete vector for aged/banking/GST/BAS/combined reports; ledger-only reports still retain the complete response vector.
- [ ] Implement `GenerateReport` and explicit Get/List queries via stable pages and typed validations.
- [ ] Bind Generate/Get/List report delegates and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/reporting/... ./services/core/internal/revisions/... ./services/core/internal/app/... ./services/core/internal/transport/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/reporting services/core/internal/app && rtk git commit -m "feat: generate revision pinned report snapshots"`.

### Task 5: Implement trial balance, P&L, balance sheet, and comparisons

**Files:**
- Create: `services/core/internal/reporting/financials.go`
- Create: `services/core/internal/reporting/financials_test.go`
- Create: `services/core/internal/reporting/financials_property_test.go`
- Create: `services/core/internal/reporting/general_ledger.go`
- Create: `services/core/internal/reporting/general_ledger_test.go`
- Create: `services/core/internal/reporting/journal_detail.go`
- Create: `services/core/internal/reporting/journal_detail_test.go`
- Create: `services/core/internal/reporting/year_end_close.go`
- Create: `services/core/internal/reporting/year_end_close_test.go`
- Create: `services/core/internal/accounting/year_end_close_preview_port.go`
- Create: `services/core/internal/accounting/year_end_close_preview_port_test.go`
- Modify: `services/core/internal/accounting/posting_service.go`
- Modify: `services/core/internal/accounting/posting_service_integration_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Turn every executable formula in Design §11.4 into table/property tests using debit-positive signed balances and separately asserted display signs.
- [ ] Cover contra parents, revenue/other revenue, expense/other expense, assets/liabilities/equity, explicit retained earnings, derived current-year earnings, financial-year boundary, year-end close journal, and no double counting.
- [ ] Implement deterministic stable section/display ordering and blocking validations for missing/contradictory classifications; never plug an unexplained amount.
- [ ] Run comparative columns through the same calculator with their own range/vector/classification version, including a retained prior snapshot whose older classification differs from the current version.
- [ ] Cross-check trial-balance account totals against journal detail and `assets = liabilities + contributed/retained equity + current-year earnings`.
- [ ] Allow every accounting-read role (admin, preparer, lodger, auditor) to `GenerateReport(YEAR_END_CLOSE_PREVIEW)` and return explicit P&L-closing lines to retained earnings, period/vector/content hash, permissions and blockers. Only admin/preparer may pass that preview ref/hash to existing `PostManualJournal`; lodger/auditor posting attempts are denied. Revalidate under the ordinary command UoW, post exactly once with replay/conflict behavior, and prove reports derive current-year earnings before close but never double count after the close journal.
- [ ] Bind `YearEndClosePreviewPort` in production composition and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/reporting/... ./services/core/internal/accounting/... ./services/core/internal/app/... -race -count=1`; include stale preview, changed vector/hash/account, closed period, exact replay, changed conflict, and rollback cases through the real `PostManualJournal` application path.
- [ ] Commit: `rtk git add services/core/internal/reporting services/core/internal/accounting services/core/internal/app && rtk git commit -m "feat: calculate financial statements"`.

### Task 6: Implement cash-flow facts and indirect cash flow

**Files:**
- Modify: `services/core/internal/accounting/cashflow.go`
- Modify: `services/core/internal/accounting/cashflow_test.go`
- Modify: `services/core/internal/accounting/posting_policy.go`
- Modify: `services/core/internal/accounting/journal.go`
- Create: `services/core/internal/reporting/cashflow.go`
- Create: `services/core/internal/reporting/cashflow_test.go`
- Create: `test/fixtures/reporting/cashflow-cases.pb.json`

- [ ] Re-run first-write posting tests requiring every cash journal line's immutable operating/investing/financing/transfer components to sum exactly to that line; journals without cash emit none.
- [ ] Verify the already-implemented source policies for sale/purchase/payment/refund/fee/interest/transfer/loan/owner contribution/asset disposal/depreciation and manual explicit classification. If any predecessor source lacks a fact, amend/reaccept that predecessor rather than backfilling unverifiable history here.
- [ ] Write indirect formula tests for net profit, non-cash P&L adjustments, `-Δ signed_balance` working capital, exclusions, operating fact equality, transfers net zero, and all three final checks in §11.4.
- [ ] Block missing/contradictory classifications; cover debt-funded asset acquisition without false cash flows.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/accounting/... ./services/core/internal/reporting/... -count=1`.
- [ ] Commit: `rtk git add services/core/internal/accounting services/core/internal/reporting test/fixtures/reporting && rtk git commit -m "feat: report reconciled indirect cash flow"`.

### Task 7: Complete subledger, GST, activity, and reconciliation reports

**Files:**
- Create: `services/core/internal/reporting/gst.go`
- Create: `services/core/internal/reporting/gst_test.go`
- Modify: `services/core/internal/reporting/receivables.go`
- Modify: `services/core/internal/reporting/payables.go`
- Create: `services/core/internal/reporting/activity.go`
- Create: `services/core/internal/reporting/activity_test.go`
- Create: `services/core/internal/reporting/bank_reconciliation.go`
- Create: `services/core/internal/reporting/bank_reconciliation_test.go`

- [ ] Extend tests for all requested report kinds: trial balance, general ledger/journal detail, GST detail, aged receivables/payables, customer/supplier activity, and bank reconciliation summary/detail.
- [ ] Prove ageing choice/boundaries, credits/overpayments separate display, and control-account equality as of the same instant.
- [ ] Prove GST gross/net/tax/rule/source/revision/recognition/evidence/adjustment provenance and equality to current/deferred/evidence/adjustment control accounts.
- [ ] Prove reconciliation summaries use retained completed ranges/components and equal bank ledger public projections.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/reporting/... -count=1`.
- [ ] Commit: `rtk git add services/core/internal/reporting && rtk git commit -m "feat: complete accounting report projections"`.

## Chunk 2: BAS, exports, UI, and canonical acceptance

### Task 8: Aggregate GST facts into immutable BAS calculations

**Files:**
- Create: `services/core/internal/tax/facts.go`
- Create: `services/core/internal/tax/facts_test.go`
- Create: `services/core/internal/tax/calculator.go`
- Create: `services/core/internal/tax/calculator_test.go`
- Create: `services/core/internal/tax/calculator_property_test.go`
- Create: `services/core/internal/tax/service.go`
- Create: `services/core/internal/tax/service_integration_test.go`
- Modify: `services/core/internal/tax/service_handler.go`
- Modify: `services/core/internal/tax/service_handler_test.go`
- Create: `services/core/internal/accounting/tax_fact_port.go`
- Create: `services/core/internal/accounting/tax_fact_port_test.go`
- Modify: `services/core/internal/organisations/read_port.go`
- Modify: `services/core/internal/artefacts/rule_repository.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Write non-cash/cash BAS tests from source facts, part-payments, mixed lines, credits, refunds, evidence pending/released, adjustment pending/released, prior-attributed openings, late current-period corrections, and finalized historical reports.
- [ ] Add BAS presentation tests proving source facts remain cent-accurate while whole-dollar BAS fields discard cents toward zero (never round up), including positive/negative `.01`, `.50`, `.99`, aggregate-before-truncate, and exact G1/1A/1B/net mapping-bundle fixtures.
- [ ] Enforce one active rule bundle per reporting period; configuration changes only on day one of a later empty period; return `MIXED_TAX_RULE_BUNDLES` for violation.
- [ ] Implement workpaper calculation under one pinned read transaction, retaining profile/basis/vector/source hash/rules/mappings/fields/fact provenance/warnings/blockers/content hash/creator/time.
- [ ] Define `CreateBASWorkpaper` modes `INITIAL`, `RECALCULATION`, `REVISION`, and `CORRECTION`. `ACCEPTED` permits only a linked `REVISION`; `REJECTED` permits only a corrected linked `CORRECTION`; each requires predecessor ID/reason/period linkage and creates a new immutable report. Reject ACCEPTED→CORRECTION, REJECTED→REVISION, and either terminal→RECALCULATION; draft recalculation versions remain under the same workpaper.
- [ ] Implement local validation without editing calculated values; missing tax invoice and adjustment note create the exact warning/block behavior declared by retained rule bundles.
- [ ] Consume `AccountingTaxFactPort`, `SettlementTaxFactPort`, `OrganisationReadPort`, and `ArtefactReadPort` under the same read TxScope. Bind Create/Validate/Get/List BAS delegates here; leave `RecordAdjustment` on the typed temporary delegate until Task 9 installs authoritative source-impact handling first.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/tax/... ./services/core/internal/accounting/... ./services/core/internal/settlements/... ./services/core/internal/organisations/... ./services/core/internal/artefacts/... ./services/core/internal/app/... ./services/core/internal/transport/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/tax services/core/internal/accounting services/core/internal/organisations services/core/internal/artefacts services/core/internal/app && rtk git commit -m "feat: calculate cash and noncash bas"`.

### Task 9: Implement authoritative BAS invalidation, declaration, and simulator states

**Files:**
- Create: `services/core/internal/tax/impact_port.go`
- Create: `services/core/internal/tax/impact_port_test.go`
- Create: `services/core/internal/tax/state.go`
- Create: `services/core/internal/tax/state_test.go`
- Create: `services/core/internal/tax/simulator.go`
- Create: `services/core/internal/tax/simulator_test.go`
- Create: `services/core/internal/tax/activity_statement_gateway.go`
- Create: `services/core/internal/tax/simulator_gateway.go`
- Create: `services/core/internal/tax/gateway_contract_test.go`
- Create: `services/core/internal/tax/simulator_recovery.go`
- Create: `services/core/internal/tax/simulator_recovery_test.go`
- Create: `services/core/internal/tax/simulator_failure_test.go`
- Create: `services/core/internal/app/tax_impact_matrix_integration_test.go`
- Create: `apps/desktop/src/shared/build-profile.ts`
- Create: `apps/desktop/src/main/build-profile.ts`
- Create: `apps/desktop/src/main/build-profile.test.ts`
- Modify: `apps/desktop/src/main/core-process.ts`
- Modify: `apps/desktop/src/main/core-process.test.ts`
- Modify: `services/core/internal/buildinfo/info.go`
- Modify: `services/core/internal/buildinfo/info_test.go`
- Modify: `services/core/cmd/tammy-core/main.go`
- Modify: `services/core/cmd/tammy-core/main_test.go`
- Modify: `services/core/internal/organisations/service.go`
- Modify: `services/core/internal/organisations/service_test.go`
- Modify: `apps/desktop/forge.config.ts`
- Modify: `scripts/build-manifest-schema.mjs`
- Modify: `scripts/build-manifest-schema.test.mjs`
- Modify: `scripts/write-build-manifest.mjs`
- Modify: `scripts/write-build-manifest.test.mjs`
- Modify: `apps/desktop/scripts/find-packaged-app.mjs`
- Modify: `apps/desktop/scripts/find-packaged-app.test.mjs`
- Modify: `apps/desktop/tests/e2e/package-signature.test.mjs`
- Modify: `services/core/internal/tax/service_handler.go`
- Modify: `services/core/internal/tax/service_handler_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Modify: `services/core/internal/transport/registrar.go`
- Modify: `services/core/internal/transport/registrar_test.go`

- [ ] Generate transition-table tests for every Design §11.3 source-change/report/transmission pair and every invalid transition: `PREPARED → CANCELLED`, `SUBMISSION_PREPARED → DECLARED` on cancellation, `TECHNICAL_FAILURE_SAFE` only when dispatch is provably `NOT_STARTED`, orphaned `DISPATCHING → UNKNOWN`, no UNKNOWN resubmit, `UNKNOWN → RECONCILED_NOT_RECEIVED → DECLARED`, `REJECTED → CORRECTION` only, `ACCEPTED → REVISION` only, and immutable terminal snapshots.
- [ ] Replace the Slice 1 no-report adapter with `TaxReportImpactPort.ApplySourceImpact` in the same source-changing UoW; preserve prior calculations/validations/declarations/payloads as superseded evidence. Rerun the complete Task 3 producer matrix through public application interfaces and assert Sales, Purchases, Settlements, Banking, Accounting, Organisations, and Artefacts each reach the real retained report outcome atomically.
- [ ] Only after that production impact adapter is bound, implement `RecordAdjustment` with awareness/date/note/consideration provenance, `AccountingPostingPort` control reclassification, financial/tax revisions, `ApplySourceImpact`, retained invalidation/supersession, audit, and deterministic result in the same UoW.
- [ ] Model “outdated” only as a projection reason; regeneration creates a retained calculation version under the same draft or a linked correction/revision as specified.
- [ ] Require lodger plus fresh TOTP and matching content hash for `AcceptDeclaration`; store acknowledgement/declaration evidence.
- [ ] Implement `PrepareBASSimulation` to deterministically allocate original transaction/message/conversation IDs and payload hash and commit `PREPARED`; cancellation is an ordinary expected-version `PREPARED → CANCELLED` UoW. `SubmitBASSimulation` requires lodger plus fresh TOTP, validates current content/entity versions, commits `DISPATCHING`, closes the SQL transaction, invokes the simulator gateway exactly once with persisted identifiers/payload, then opens a new UoW to atomically persist encrypted raw response, parsed result, state, and audit.
- [ ] Persist the submit/reconcile operation election across phase commits. Exact replay reconstructs PREPARED/DISPATCHING/terminal state without a second gateway call, a changed semantic request conflicts, and only an explicit new transmission may follow `TECHNICAL_FAILURE_SAFE` or `RECONCILED_NOT_RECEIVED`.
- [ ] Add one failure-injection test per boundary and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/tax -run '^TestSimulationFailureInjection/(before_prepared_commit|after_prepared_commit|before_dispatching_commit|after_dispatching_commit|before_gateway|after_gateway_before_response_commit|after_response_state_audit)$' -count=1`; add each named subtest red→green in order. Assert `NOT_STARTED → TECHNICAL_FAILURE_SAFE`, `MAYBE_SENT`/EOF/timeout → UNKNOWN, and `RESPONSE_RECEIVED → ACCEPTED|REJECTED` only with complete response evidence; no SQL transaction spans gateway invocation and no uncertain outcome is retried.
- [ ] On startup, leave `PREPARED` awaiting explicit user submit/cancel and convert every orphaned `DISPATCHING` to `UNKNOWN` without sending. A `NOT_STARTED` attestation commits terminal `TECHNICAL_FAILURE_SAFE` evidence and returns the report to `DECLARED`; retry creates a new transmission. `ReconcileBASSimulation` requires lodger but no fresh TOTP, invokes the gateway outside SQL with the persisted original identifiers/payload hash, and atomically records `ACCEPTED`, `REJECTED`, or `RECONCILED_NOT_RECEIVED → DECLARED`; inconclusive response remains `UNKNOWN` and can be reconciled again but never resubmitted.
- [ ] Run one shared `ActivityStatementGateway` contract suite for `Get`, `Validate`, declaration preview, prepare/submit send phases, safe failure, rejection, acceptance, ambiguous recovery, reconciliation, and cancel, with the simulator as the only implementation. Assert the complete `simulator-wattle-co.pb.json` request/response/declaration/receipt/$80 byte-level golden and label every response/UI element `LOCAL SIMULATION — NOT LODGED`.
- [ ] Keep all production SBR adapters/routes/credentials absent or hard-disabled; add a test that no production endpoint string or network attempt exists.
- [ ] Extend the authenticated packaged build-manifest schema/writer/verifier with a non-user-editable SBR build profile. Only a test-signed manifest fixed to `TEST_SIGNED_SIMULATOR`/environment `SIMULATOR` enables the simulator and `SIMULATOR_FIXTURE`; normal offline release manifests keep SBR disabled and require `ABR_ONLINE`/`ABR_EXTRACT_MANUAL` verification. Startup rejects missing/tampered/unknown/profile-signature fields before BAS controls are enabled, and no runtime preference or CLI flag can override them.
- [ ] Carry the profile into Go without trusting an Electron argument or environment variable. Electron `core-process.ts` passes no profile override; both processes resolve the manifest/detached signature from the fixed packaged-resource location. Go `buildinfo` independently verifies the retained signing key/profile signature, its own executable hash and manifest schema/profile before composition, and `tammy-core` refuses startup on any mismatch. Tests reject relocated manifests, wrong key/signature, core hash, profile, argv/env override attempts, and validly signed but non-simulator profiles containing simulator markers.
- [ ] Gate simulator identity/declaration/response parsing on the verified profile. Any EVTE/production-shaped profile fails closed on `SIMULATOR_FIXTURE`, `SIM-*` declaration/receipt markers, or `SIM.*` response codes; add build-manifest, core bootstrap, and package-signature tests for each marker and prove the test-signed simulator profile cannot enable EVTE/production endpoints or controls.
- [ ] Inject the immutable verified build profile into OrganisationService. `RecordEntityVerification(source=SIMULATOR_FIXTURE)` succeeds only in a verified `TEST_SIGNED_SIMULATOR` profile and only for the exact Wattle & Co fixture identity/evidence hash; retain the profile/manifest hash with the verification. When core startup is otherwise permitted, exact tests deny disabled, EVTE/production-shaped, wrong-identity, and ordinary release profiles without verification/audit rows but with the ordinary deterministic idempotency error result/replay/conflict; a missing/tampered manifest refuses core startup before RPC availability. `ABR_ONLINE`/`ABR_EXTRACT_MANUAL` behavior remains unchanged.
- [ ] Bind adjustment/declaration/simulator delegates and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/tax/... ./services/core/internal/accounting/... ./services/core/internal/sales/... ./services/core/internal/purchases/... ./services/core/internal/settlements/... ./services/core/internal/banking/... ./services/core/internal/organisations/... ./services/core/internal/artefacts/... ./services/core/internal/buildinfo/... ./services/core/internal/app/... ./services/core/internal/transport/... ./services/core/cmd/tammy-core/... -race -count=1`, `rtk pnpm exec node --test scripts/build-manifest-schema.test.mjs scripts/write-build-manifest.test.mjs apps/desktop/scripts/find-packaged-app.test.mjs`, `rtk pnpm --filter @tammy/desktop test`, and `rtk pnpm package`; require manifest/profile/signature verification before commit.
- [ ] Commit: `rtk git add services/core/internal/tax services/core/internal/organisations services/core/internal/buildinfo services/core/internal/app services/core/internal/transport services/core/cmd/tammy-core apps/desktop/src/shared apps/desktop/src/main apps/desktop/forge.config.ts apps/desktop/scripts apps/desktop/tests/e2e/package-signature.test.mjs scripts && rtk git commit -m "feat: version bas state and simulator"`.

### Task 10: Render deterministic report and BAS exports

**Files:**
- Create: `services/core/internal/exports/renderer.go`
- Create: `services/core/internal/exports/renderer_test.go`
- Create: `services/core/internal/exports/pdf.go`
- Create: `services/core/internal/exports/csv.go`
- Create: `services/core/internal/exports/evidence.go`
- Create: `services/core/internal/exports/evidence_test.go`
- Create: `services/core/internal/reporting/export_service.go`
- Create: `services/core/internal/reporting/export_service_test.go`
- Create: `services/core/internal/tax/export_service.go`
- Create: `services/core/internal/tax/export_service_test.go`
- Create: `services/core/internal/exports/job.go`
- Create: `services/core/internal/exports/job_test.go`
- Modify: `services/core/internal/audit/export.go`
- Modify: `services/core/internal/audit/export_test.go`
- Modify: `services/core/internal/reporting/service_handler.go`
- Modify: `services/core/internal/reporting/service_handler_test.go`
- Modify: `services/core/internal/tax/service_handler.go`
- Modify: `services/core/internal/tax/service_handler_test.go`
- Create: `scripts/verify-evidence-bundle.mjs`
- Create: `scripts/verify-evidence-bundle.test.mjs`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Write golden tests for PDF text/totals/page headers, formula-safe UTF-8 CSV, canonical Protobuf JSON, exact `payload.pb`, retained `descriptors.pb`, manifest hashes, schema fingerprint metadata, audit head, and deterministic output content hash.
- [ ] Implement persisted jobs with operation/input hashes, queued/running/completed/retryable/terminal/cancelled states, bounded progress, checkpoint/result ref, startup running→queued reconstruction, cancellation-versus-commit election, three process failures then explicit retry, destination-hash recovery, and no replacement of prior artefacts.
- [ ] Bind the named report/BAS Export/Cancel/Retry/Get/List delegates. Cancel and Retry are ordinary idempotent commands with expected job version; Retry is allowed only from retryable/terminal policy states and preserves prior artefact attempts.
- [ ] Enforce the RPC map's permissions exactly: report jobs may be created by any accounting-read role and BAS jobs by admin/preparer/lodger/auditor; cancel/retry is limited to the initiating actor or an admin, while every authenticated accounting-read role may Get/List retained jobs.
- [ ] Assert the concrete generated Reporting and Tax handlers have no remaining `FEATURE_NOT_READY` delegate and every RPC reaches its intended application interface through the production registrar.
- [ ] Render with the repository-owned deterministic PDF writer (no external binary), formula-safe CSV, canonical evidence, and Audit-owned signed evidence package; run signature/tamper tests through Audit.
- [ ] Require an approved destination handle for user exports and atomically rename only a fully verified archive/file. Never accept an arbitrary renderer path.
- [ ] Implement standalone evidence verifier using retained bytes/descriptors, preserving/re-emitting unknown binary fields and never reserializing to establish stored-byte hash validity.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/exports/... ./services/core/internal/reporting/... ./services/core/internal/tax/... ./services/core/internal/audit/... ./services/core/internal/app/... ./services/core/internal/transport/... -count=1` and `rtk pnpm exec node --test scripts/verify-evidence-bundle.test.mjs`.
- [ ] Commit: `rtk git add services/core/internal/exports services/core/internal/reporting services/core/internal/tax services/core/internal/audit services/core/internal/app scripts && rtk git commit -m "feat: export deterministic accounting evidence"`.

### Task 11: Extend encrypted backup/restore for reports, BAS, and exports

**Files:**
- Create: `services/core/internal/reporting/backup_projection.go`
- Create: `services/core/internal/reporting/backup_projection_test.go`
- Create: `services/core/internal/tax/backup_projection.go`
- Create: `services/core/internal/tax/backup_projection_test.go`
- Modify: `services/core/internal/backup/provider_registry.go`
- Modify: `services/core/internal/backup/provider_registry_test.go`
- Modify: `services/core/internal/backup/service.go`
- Modify: `services/core/internal/backup/service_integration_test.go`
- Modify: `services/core/internal/restore/provider_registry.go`
- Modify: `services/core/internal/restore/provider_registry_test.go`
- Modify: `services/core/internal/restore/service.go`
- Modify: `services/core/internal/restore/service_integration_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Add a red backup test containing retained report vectors/queries/rows/validations, BAS calculation/declaration/simulator versions, PDF/CSV/evidence artefacts, descriptor bytes, and audit links.
- [ ] Implement module-owned immutable backup projections and include their encrypted artefact blobs in the signed archive without cross-module SQL.
- [ ] Register Reporting and Tax providers in the production Backup/Restore provider registries and composition root; restore applies them through module-owned validators before the global atomic swap.
- [ ] Restore into a fresh installation and compare every report/BAS/simulator/export content hash, source vector, provenance, exact payload bytes, audit head/generation, and all cross-projection invariants.
- [ ] Reject missing/tampered snapshot, provenance, descriptor, payload, artefact, or signature before swap and leave active bytes unchanged.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/backup/... ./services/core/internal/restore/... ./services/core/internal/reporting/... ./services/core/internal/tax/... ./services/core/internal/app/... -race -count=1`; expect production registry plus Slice 5 backup/restore cases pass.
- [ ] Commit: `rtk git add services/core/internal/reporting services/core/internal/tax services/core/internal/backup services/core/internal/restore services/core/internal/app && rtk git commit -m "feat: preserve reports and bas in backup restore"`.

### Task 12: Build report/BAS IPC and production workflows

**Files:**
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/main/rpc-router.ts`
- Modify: `apps/desktop/src/main/rpc-router.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Create: `apps/desktop/src/renderer/features/reports/report-screen.tsx`
- Create: `apps/desktop/src/renderer/features/reports/report-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/tax/bas-screen.tsx`
- Create: `apps/desktop/src/renderer/features/tax/bas-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/tax/simulator-screen.tsx`
- Create: `apps/desktop/src/renderer/features/tax/simulator-screen.test.tsx`
- Create: `apps/desktop/src/renderer/app-shell/simulator-banner.tsx`
- Create: `apps/desktop/src/renderer/app-shell/simulator-banner.test.tsx`
- Create: `apps/desktop/tests/e2e/financial-reports.spec.ts`
- Create: `apps/desktop/tests/e2e/bas.spec.ts`
- Create: `apps/desktop/tests/e2e/period-corrections.spec.ts`
- Modify: `apps/desktop/src/renderer/app-shell/navigation.tsx`
- Modify: `apps/desktop/src/renderer/app-shell/app-shell.tsx`
- Create: `apps/desktop/src/renderer/app-shell/app-shell.test.tsx`
- Modify: `apps/desktop/src/renderer/app.tsx`
- Modify: `apps/desktop/src/renderer/app.test.tsx`

- [ ] Add named binary IPC tests for all reporting/tax/export methods and approved destination selection.
- [ ] Write React tests for report filters/comparisons, loading/stale/blocking validation, signed/display values, drilldown provenance, ageing, reconciliation detail, year-end-close preview/post/replay/permission flow, workpaper versions, validation, declaration/TOTP, simulator outcomes, source invalidation, correction, and exports.
- [ ] Before each corresponding UI path, add/run thin packaged `E2E-12 financial report`, `E2E-13 BAS workpaper`, and `E2E-14 year end close` cases, observe the missing production control, implement only that vertical codec/UI workflow, and rerun its exact grep to pass. Commit these specs here; Task 13 expands exhaustive matrices/oracles.
- [ ] Implement Reports and BAS navigation with accessible tables, print preview, provenance links, visible revision/content hash, and no hidden plugs.
- [ ] Mount the simulator banner above routing in root `app.tsx`, not only the authenticated shell. In every verified test-signed simulator build, render a permanent non-dismissible application-frame banner with exact text `SIMULATOR — NOT FOR ATO LODGMENT`, including locked/setup/report screens, and repeat that exact warning in the submission confirmation. Root/shell/screen-reader/keyboard tests require it; a missing/disabled-profile banner/profile mismatch blocks simulator controls.
- [ ] Visibly distinguish draft/validated/declared/prepared/simulator/accepted/rejected/superseded states; every simulator response remains additionally labelled `LOCAL SIMULATION — NOT LODGED` and never implies ATO lodgement.
- [ ] Run `rtk pnpm --filter @tammy/desktop test && rtk pnpm typecheck && rtk pnpm lint`.
- [ ] Commit: `rtk git add apps/desktop && rtk git commit -m "feat: add financial report and bas workflows"`.

### Task 13: Run canonical packaged accounting/BAS oracles

**Files:**
- Modify: `apps/desktop/tests/e2e/financial-reports.spec.ts`
- Modify: `apps/desktop/tests/e2e/bas.spec.ts`
- Modify: `apps/desktop/tests/e2e/period-corrections.spec.ts`
- Create: `apps/desktop/tests/e2e/canonical-month.spec.ts`
- Create: `apps/desktop/tests/e2e/cash-basis-month.spec.ts`
- Modify: `apps/desktop/tests/e2e/support/runtime-coverage.ts`
- Modify: `apps/desktop/tests/e2e/support/runtime-coverage.test.ts`
- Create: `compliance/evidence/core-accounting/slice-5-runtime-coverage.json`
- Modify: `compliance/traceability/core-accounting.csv`
- Modify: `compliance/evidence/core-accounting/manifest.json`
- Modify: `scripts/check-core-accounting-evidence.mjs`
- Modify: `scripts/check-core-accounting-evidence.test.mjs`
- Modify: `.github/workflows/foundation-windows11-e2e.yml`

- [ ] Implement packaged `E2E-12` across every report/formula/comparison/ageing/cash-flow/export path and `E2E-13` across cash/non-cash BAS, missing-evidence blocking validation, declaration, prepared cancellation, every simulator outcome, invalidation, supersession, correction/revision, and export.
- [ ] In packaged `E2E-13`, first verify the test-signed manifest/profile and permanent `SIMULATOR — NOT FOR ATO LODGMENT` app-frame banner, then create the workspace and record `SIMULATOR_FIXTURE` before BAS preparation. Run the exact Wattle & Co fixture through Get, Validate, declaration preview/acceptance, Prepare, confirmation warning, Submit, accepted response and retained receipt; assert simulator ABN/markers never enter EVTE/production selection or any network request, and the visible result is `SIM.ACCEPTED`, `SIM-2026-Q4-0001`, `$80`, and `LOCAL SIMULATION — NOT LODGED`.
- [ ] Complete `E2E-14` for manual journals, year-end close preview/post/permission/replay/no-double-counting, close/reopen, every source/payment/allocation closed-period failure, linked correction/reversal, and tax impact.
- [ ] Implement the exact canonical month from Design §15.3, including `$1,100` sale, `$220` extracted bill, imported/matched payments, `$880` reconciliation, every public report, `G1 $1,100`, `1A $100`, `1B $20`, net `$80`, evidence export, restart, and audit checks.
- [ ] Implement the second cash-basis scenario with part payments, mixed lines, credits, supplier credits, reversal, duplicate imports, transfer, fee, reconciliation undo/recomplete, scanned OCR, and stale BAS regeneration.
- [ ] Create an encrypted backup, restore into a fresh installation, and only then assert all six Design §15.4 cross-projection oracles through public Protobuf projections, including restored projections versus the pre-backup manifest.
- [ ] Drive every Slice 5 coverage row through all roles, transitions/invalid transitions, stale versions, exact replay/changed conflict, principal failures, and empty/populated/filter/page states with zero skips.
- [ ] Extend the generated-client-boundary tracer and write canonical `{caseId, fullyQualifiedRpc, actorRole, outcomeCode}` tuples to `slice-5-runtime-coverage.json`; compare them with tuples expanded from `coverage.yaml` and reject missing, extra, duplicate, skipped, or scenario-only claims.
- [ ] Write preliminary runtime tuples/results only to the run's untracked temporary evidence directory. Do not create/modify tracked `compliance/evidence` before the reviewed source commit; materialize `slice-5-runtime-coverage.json` and the retained manifest only after clean-source descriptor generation.
- [ ] Run `rtk pnpm contracts && rtk pnpm lint && rtk pnpm typecheck && rtk pnpm test`.
- [ ] Run `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep "E2E-12|E2E-13|E2E-14|canonical month|cash basis month"` with network denied and retain export/descriptor/fixture/artefact/result hashes.
- [ ] Run `rtk pnpm exec node --test scripts/check-core-accounting-evidence.test.mjs`; retain preliminary artefacts outside tracked evidence until the clean source commit exists.
- [ ] Request independent review, resolve critical/important findings, rerun affected gates, and commit reviewed source without retained descriptor/result evidence: `rtk git add apps/desktop test compliance/traceability scripts .github/workflows/foundation-windows11-e2e.yml && rtk git commit -m "test: accept reports and bas slice"`.
- [ ] From that clean source commit run `TAMMY_SOURCE_REVISION=$(rtk git rev-parse HEAD) rtk pnpm proto:descriptors:evidence`; verify the manifest subject, rerun the exact macOS packaged gate, then run `.github/workflows/foundation-windows11-e2e.yml` job `windows11-23h2-x64-packaged-e2e` at that same committed revision and require the identical matrix, 23H2 attestation, signatures, offline guard, encrypted storage, and zero skips. Write the Slice 5 evidence manifest and run `rtk pnpm core-accounting:evidence -- --slice 5` expecting `slice 5 evidence verified`.
- [ ] Commit only retained evidence: `rtk git add compliance/contracts compliance/evidence/core-accounting && rtk git commit -m "build: retain slice 5 accounting evidence"`.

## Slice 5 exit gate

- [ ] Every report and BAS value retains source provenance, vector, rule/classification version, canonical query, and content hash.
- [ ] Journal/trial balance/financial statements/subledgers/banking/GST/BAS/cash-flow projections cross-reconcile.
- [ ] BAS invalidation and simulator states follow the authoritative transition fixtures; production ATO/SBR remains unavailable.
- [ ] E2E-12, E2E-13, complete E2E-14, canonical month, and cash-basis month pass in the signed packaged app.
