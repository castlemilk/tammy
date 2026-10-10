# BAS preparation, review and manual handoff implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. Each task needs failing behaviour tests, implementation, passing verification, spec review and code-quality review. Execute after packaged acceptance of the export and purchase phases.

**Goal:** Prepare a complete supported GST/PAYG statement, freeze the exact reviewed version using real approval authority, export it, and retain user-recorded external handling and linked revisions without changing history.

**Architecture:** A focused `internal/baspreparation` domain owns effective-dated profiles, stable statement families and mutable preparation aggregates referencing immutable calculations, validations, approvals and observations. Transaction-scoped commands reuse the ordinary coordinator, identity/fresh-factor authority, durable audit and existing immutable GST workpapers. Source capture includes the complete coverage manifest supplied by invoices, purchases and tax facts; report persistence/export does not alter source currentness.

**Tech stack:** Generated Go/TypeScript Connect contracts, SQLCipher migration 0013, Go integer/rational calculations, Electron/Conveyor, React and phase-one reporting renderers/native writer.

**Authority and boundary:** The user's 2026-10-05 continuation instruction approves the linked reporting programme. The initial profile is Simpler GST plus small/medium PAYGW and notified amount/rate PAYGI without variations, sharing monthly or quarterly windows. Unknown or unsupported obligations block freezing. No ATO transport/submission is part of this work.

## Chunk 1: Contracts, rules and lifecycle

### Task 1: Pin deterministic form rules and pure calculations

Files: create `services/core/internal/baspreparation/calculation.go`, `calculation_test.go`, `calendar.go`, `calendar_test.go`; document rules in `docs/development/bas-preparation.md`.

- [x] First test exact-cent G1/1A/1B, nil/unknown/not-applicable/known-zero values, nonnegative atomic labels and overflow rejection. Test whole-dollar atomic truncation and derived equations: W5 = reported W2 + W3 + W4; 4 = W5; 8A = 1A + 4 + 5A; 8B = 1B; 9 = absolute difference with PAYABLE/REFUNDABLE/NIL direction. Demonstrate cents across W2/W3/W4 cannot create an extra dollar in W5.
- [x] Test notified T7 → 5A and exact rational notified T2 × reportable T1 → T11 → 5A, bounded decimal parsing without floats, independent exact-cent traces and rounding differences. Business income is comparison only, never automatic T1. Inoperative/uncertain prior variation, new variation, credits and missing notice provenance block completion.
- [x] Implement pure calculations with explicit label state, exact integer/rational arithmetic and a pinned rule/calculator/form version. Retain inspectable operands/results. Verify official sources referenced by the design (ATO API URLs may require indexed search when direct fetch returns 403) and record retrieval date/reference in rules documentation.
- [x] Test calendar monthly and Australian GST quarters irrespective of financial-year end, civil date boundaries, leap days, normal suggested due dates versus confirmed external due dates, no inferred extensions, registration part-period/cadence/mixed/annual unsupported outcomes and income-year PAYGI method consistency.

Task 1 checkpoint (2026-10-10): calculator/calendar API absence supplied initial RED; malformed due-date evidence and six incompatible unused PAYGI payloads supplied behaviour RED before their guards. Root independently passed the complete focused package (0.382s), race detector (1.714s), and all default Core/SBR-helper Go packages with `TMPDIR=/private/tmp GOFLAGS=-p=1` (log `/tmp/tammy-bas-task1-default-go-final-2026-10-10.log`). Independent specification and code-quality reviews found no blocking defect; positive next-income-year and defensive-copy cases were added. [Rules documentation](../../development/bas-preparation.md) records the versions, sources, golden examples and remaining transactional profile/evidence responsibility. This is a pure foundation checkpoint; phase-three persistence, permissions, UI and packaged acceptance remain open.

### Task 2: Add bounded generated BAS preparation contracts

Files: create `proto/tammy/v1/bas_preparation.proto`; extend `overview.proto`; regenerate `services/core/internal/gen` and `packages/connect-client/src/gen`; update descriptor/transition/E2E metadata where required.

- [x] Define typed enums/messages for obligation applicability (UNKNOWN/ACTIVE/NOT_APPLICABLE), GST method/registration/turnover, PAYGW/PAYGI category/method/cadence, effective profile evidence, confirmed reporting period/due date, label values/traces, source coverage dispositions, validation findings/severity, lifecycle states and USER_RECORDED_UNVERIFIED observations.
- [x] Define organisation-scoped queries for current/history obligations profiles, calendar/period history, exact preparation Get/List and immutable calculation/frozen history with bounded authenticated cursors.
- [x] Define commands SetBasObligationsProfile, ConfirmBasPeriod, SaveBasPaygInputs, RegenerateBasPreparation, ValidateBasPreparation, AcknowledgeBasWarnings, FreezeBasForManualHandoff, CreateBasReplacement, CreateBasRevision, RecordExternalBasObservation, CorrectExternalBasObservation, ResolveExternalBasDispute, ReplaceRejectedBasAttempt and RecordBasExportEvent. Every command carries CommandContext, expected version and exact source/calculation/validation identity where relevant; chain commands also pin family version/head ID/hash and branch/active-attempt versions. Approval commands carry a real FreshFactorContext.
- [x] Preserve existing TaxService/current draft compatibility. Extend overview with exact BAS identities and truthful local statuses. Do not reuse official SBR receipt/delivery authority or introduce a submit endpoint.
- [x] Add contract tests for field bounds/enums/required identity, transitions and permitted source data. Run `rtk proxy mise exec -- pnpm proto:format`, `proto:lint`, `proto:generate`, descriptor/transition checks; review generated diffs and commit intended outputs before the full generated-tree gate.

Task 2 checkpoint (2026-10-10): source and intended generated outputs are committed at `2b1a67e5530a3d0ae5488bec6a64f4c21f90c822`. The service declares 15 bounded queries and 14 commands, including exact full observation/dispute-resolution reads, typed correction history, saved PAYG input readback and warning acknowledgements. Root recorded failing tests for missing APIs, incorrect resource selectors, lost correction payloads, calendar overflow and exact-zero label-9 traces before their fixes. Final regenerated runtime contracts passed (1.162s); default Core/SBR-helper Go packages passed, all five TypeScript checks passed, and the script suite passed 757 tests with one optional skip. Descriptor, transition, coverage and compatibility checks passed; lint reported zero errors with 105 warnings and two infos. Independent specification and code-quality reviews found no blocker. After committing generated outputs, the full `pnpm contracts` gate passed (log `/tmp/tammy-bas-task2-full-contracts-gate-2026-10-10.log`). These APIs remain `declared_future`: no lifecycle service, desktop bridge or packaged phase-three journey is implemented by this checkpoint.

### Task 3: Implement the complete state/chain policy first

Files: create `internal/baspreparation/transitions.go`, `transitions_test.go`.

- [ ] Test collecting/blocked/review-ready changes; only current successful validation plus bound warning acknowledgements can freeze. Frozen labels never reopen or mutate; source changes only add a live indication.
- [ ] Test qualifying completed external lodgement/acceptance versus pending/unknown/rejection/handoff/payment/refund observations. Only qualifying confirmations can advance the effective chain. No event upgrades trust above USER_RECORDED_UNVERIFIED.
- [ ] Test one stable family per organisation/actual period/window scope independent of profile/rule changes, atomic single head/successor, latest predecessor checks and single active branch/attempt. Creating a revision alone cannot supersede a confirmed original.
- [ ] Test unused-frozen replacement blocked by pending/unknown/disputed/contradictory external use; rejected attempt recovery requires a resolved exact rejection and new fresh purpose. Terminal predecessors remain retained.
- [ ] Test append-only disputes/corrections and later rejection contradicting confirmation; dispute overlays block advancement while preserving retained head. UPHOLD_RECORDED_CHAIN clears overlay only. REQUIRE_CORRECTIVE_REVISION keeps head and reserves one branch; only its active qualifying confirmation advances head. Include changed head/dependencies/conflicting observations, both outcomes, multiple unused/rejected corrective attempts, stale branches and dependent-chain conflicts.

## Chunk 2: Encrypted persistence and complete source capture

### Task 4: Migration and transaction repositories

Files: create `services/core/internal/storage/migrations/0013_bas_preparation.sql`, focused `internal/baspreparation/*_repository.go` and SQLCipher tests.

- [ ] Write migration tests preserving existing organisations, reviewed legacy documents/opening payables, purchases/invoice payments and saved workpapers with no business journals or label rewrites.
- [ ] Add immutable profile revisions, confirmed periods, family identity/head/version, preparations/branches/active-attempt versions, calculations/coverage manifests, typed PAYG evidence, validations/acks, frozen approvals, external observations/corrections/dispute resolutions and export history. Enforce scoped keys/unique lineage/foreign keys/amount checks and no-update/no-delete triggers for retained facts; mutable aggregate updates use expected version.
- [ ] Retain deterministic protobuf snapshot bytes and SHA256 with ID/version verification on reads; cap source/evidence data and reject corruption rather than dropping rows. Evidence remains encrypted and references retained document hashes; never put payroll bytes or destination paths in audit.
- [ ] Test exact-ID reads, stable authenticated pagination, restart, immutable triggers, rollback and backup/restore preserving all facts/family links. Restored assertions/sessions follow existing identity policy.

### Task 5: Transactional complete coverage manifest

Files: focused capture ports in `internal/baspreparation/sources.go`; extend `internal/reporting` module-owned read ports and purchase source contracts only after phase two is accepted.

- [ ] Capture profile, organisation version, effective rule metadata, complete invoice postings/receipts, purchase settlement/attribution/evidence/deferral/legacy/opening dispositions, typed manual journal tax facts and PAYG inputs in one encrypted transaction. Include exact source IDs/versions/hashes/date/lineage and revision vector.
- [ ] Fingerprint eligible, excluded, unresolved and unsupported candidates, known unpaid/zero/not-applicable decisions and evidence links, not only eligible totals. Report-only storage/export events stay outside fingerprint. Enforce full bounded capture; change `ErrSourceBound` and warning guidance so users are never told to shorten their actual activity-statement period.
- [ ] Test an attributable issued but unposted invoice blocks complete freeze (draft invoices excluded with explanation); a newly unposted/unresolved/excluded candidate invalidates review even with unchanged totals. Historical cash payments, undated/older legacy candidates and opening provenance must remain in coverage.
- [ ] Add an accounting-owned read projection for every retained manual `TaxFact` and its journal/line/source lineage, rule/hash and posting date. The existing `TaxFact` has no reviewed sales/purchase/adjustment classification or tax-event date: every such unclassified fact is a typed source-linked blocker in this release. Do not derive GST direction/timing from amount sign, account code/name or journal date. Any future supported classification requires its own immutable reviewed fact and timing rule; unmatched/generic reversal/refund/adjustment/special-scheme facts also block. Verify no invoice/document/payment/journal lineage duplicates. Aggregate manual numbers never override computed GST.
- [ ] Reconcile exact G1/1A/1B and payable/current/deferred/suspense conservation through module-owned facts. Deferral only resolves the supported period decision while keeping discoverable future obligations/deadlines.

### Task 6: Profile/period/input/capture/validation services

Files: `internal/baspreparation/service.go`, `profile_service.go`, `preparation_service.go`, `validation.go` and tests; extend `internal/app/command.go` registry.

- [ ] Register prepare_tax commands with canonical typed request/result names; source facts, versions, retained audit/result and idempotency commit together. Keep SQL out of coordinator callbacks beyond typed repositories.
- [ ] Set immutable effective obligation profiles with explicit answers/evidence/reason, complete supported scope and GST basis history rejection preserved. Test non-overlapping effective intervals, unknown historical gaps and no retroactive history invented by a later profile entry. Confirm period/window/due-date source without inventing extensions; corrected profiles stay in the same statement family. Pin PAYGI method to the confirmed income year and reject conflicting same-year concurrent method choices.
- [ ] Retain reviewed PAYGW W1/W2/W3/W4 and PAYGI notice/income/rate inputs, explicit zero/state/evidence and review as-of. Unknown inputs can be saved but block readiness. Never infer withholding from salary or T1 from business income.
- [ ] Regenerate new immutable calculation/workpaper under collecting family; clear acknowledgements. Validate to stable source-linked BLOCKER/WARNING/INFORMATION/UNSUPPORTED findings. Warning acknowledgements bind exact validation hash/revision and cannot waive blockers.
- [ ] Test expected-version/concurrent commands, exact lost-response replay, changed payload under same key, failed transaction rollback, audit single effects, unsupported profile and source-bound visible errors.

## Chunk 3: Approval, external observations and exact exports

### Task 7: Real role/factor authority and production composition

Files: extend `internal/authorisation/policy.go`, `internal/identity/service.go` only where supported purpose allowlists require; create `internal/localproduct/bas_preparation_sqlcipher.go`; wire `ledger_sqlcipher.go`/routes/core client.

- [ ] Add explicit BAS approval actions granted only to business_lodger; workspace_admin alone can prepare but cannot approve. Read queries require read_financial. Test forbidden roles and wrong organisation/session/workspace. Initial workspace owners currently have only workspace_admin, and AssignRoles has no desktop bridge: expose the existing bounded authenticated IdentityService AssignRoles command through core-client/organisation Conveyor and a restrained administrator-only explicit role control for the current operator, preserving all existing roles and expected user version. Do not automatically grant approval when preparing or freeze via administrator authority. Test admin-only freeze rejection, explicit retained dual-role assignment, nonadmin assignment denial and last-administrator preservation.
- [ ] Bind explicit business-lodger-only actions and stable fresh purposes: approve handoff → `bas_freeze_manual_handoff`; record external confirmation → `bas_record_external_confirmation`; assert frozen attempt unused/create replacement → `bas_replace_unused_attempt`; resolve dispute → `bas_resolve_external_dispute`; replace resolved rejected attempt → `bas_replace_rejected_attempt`. Issue/validate/atomically consume existing AssertTOTP factors. Preserve strict five-minute/session/actor/purpose/replay/rollback semantics and test every mapping, wrong purpose, replay and rollback; no fake challenge/passphrase substitute.
- [ ] Freeze checks expected aggregate/profile/calculation/validation/source currentness and acknowledgements in the commit transaction, pins exact manifest/cent figures/whole labels/rule/calculator/form/schema/descriptor/review approval hashes. Revalidate timing with the server civil-date clock at freeze: future-held evidence and ordinary deadline expiry can change eligibility without a new source row. Require retained actual required-due-date provenance for delayed claims/deferrals; nominal guidance cannot establish a deadline. Hash meaningful timing outcomes rather than the moving wall clock. Test source/profile change between review and freeze, unchanged-total coverage changes and later expiry after restart/backup restore.
- [ ] Bind an actual durable command auditor (the existing localCommandAuditor is a no-op). Keep canonical bounded event identifiers/hashes/status/reasons with no evidence bodies/secrets/paths. Test one audit on replay and rollback after audit injection.
- [ ] Wire routes and query transactions with active local identity; keep TaxService legacy handler thin and BAS lifecycle logic in its domain.

### Task 8: External records, replacement, revision and dispute commands

Files: focused `internal/baspreparation/external_service.go`, `revision_service.go`, repositories/tests.

- [ ] Append observations with frozen ID/hash, typed kind/channel, claimed time, optional bounded reference/evidence hash, actor/core time and immutable trust. Payment/handoff/rejection/pending observations never imply completed lodgement.
- [ ] Implement qualifying confirmation and atomic effective successor/head checks across original/replacement/revision attempts. Preserve originals and deltas. Only current active frozen attempt can qualify; concurrent original/replacement confirmation must yield one head.
- [ ] Implement reasoned append-only corrections/disputes, both purpose-bound resolutions, one corrective branch and recovery via unused assertion or resolved rejected attempt. Test these recoveries for ordinary replacement/revision branches as well as corrective branches, including pinned root/latest predecessor and active-attempt identities. Reject pending/contradictory/disputed recovery and stale expected branches/heads/dependent IDs. Never rewind/delete chain or change frozen labels. A later-period adjustment fact must be flagged for external review, not automatically carried or converted into a same-period revision.
- [ ] Verify exact replay/conflict/concurrency/history through real TLS+SQLCipher integration, including correction-required branch completion and all transition tests from Task 3.

### Task 9: Exact frozen/draft export adapters and retained export events

Files: extend phase-one `packages/reporting` snapshot/renderers, `apps/desktop/src/main/report-export.ts` and reporting Conveyor; core export-event command.

- [ ] Export exact persisted preparation/calculation ID/hash. Main acquires that immutable identity and session; frozen figures survive later source/profile changes and include pinned company/profile/rules/manifests. Incomplete draft exports clearly identify unresolved outcomes and are never labelled complete/frozen.
- [ ] Include reviewed exact and reported whole-dollar GST/PAYG labels/calculation traces/direction, warnings/evidence/source provenance and revision original/revised/difference. Extend both PDF and fixed rectangular CSV without dropping purchase or PAYG rows.
- [ ] Retain independent started/cancelled/failed/completed export history and completion auditing tied to exact frozen identity, with no local path/bytes stored. Saving/exporting does not alter currentness or external status. Test cancellation/failure leaves frozen report retrievable for retry and no partial file.

## Chunk 4: Desktop workbench and packaged acceptance

### Task 10: Restrained period-led BAS UI

Files: extend `apps/desktop/src/app/features/bas` with focused profile/input/review/approval/history/observation components; add API/Conveyor/core client methods and Overview attention links.

- [ ] Show Prepare, Review, Freeze, Export and External record stages, period/history selection, confirmed profile/obligations/basis/due-date source and exact retained IDs. Preserve selected period through asynchronous reads.
- [ ] Add profile applicability/unsupported controls, PAYGW/PAYGI known/zero/unknown evidence inputs, source-linked corrective blockers and exact/whole-dollar/rounding review tables. Distinguish net GST from complete supported-statement payable/refundable total.
- [ ] Freeze exact review using actual existing TOTP flow and server approval role. Reuse `features/sbr/totp-setup.tsx` without requiring any SBR credential/transport; approval setup remains local identity capability. Show current operator approval role and route administrators to the explicit role control above; users lacking authority receive corrective guidance without a fake elevation prompt. Frozen views show ID/hash/versions/currentness and exports; external observation form makes user-recorded/unverified trust explicit. Linked revisions show original/revised/deltas. Retain cancelled export/predecessor/observation/dispute histories and purpose-bound corrective actions.
- [ ] Overview links exact BAS IDs with action-needed/review-ready/frozen/manual/user-recorded states. No Submit to ATO button or official acceptance wording.
- [ ] First write component/API tests for payload identities, dates, zero versus unknown, blockers, role/factor errors, stale freeze, lost-response identical retry and truthful external statuses; run desktop tests/typecheck/build.

### Task 11: Integrated real packaged journey and regression

Files: create `apps/desktop/tests/e2e/bas-preparation.spec.ts` plus focused support; update explicit Playwright testMatch and user journey matrix/evidence record.

- [ ] Start supported encrypted workspace without unclassified fixture journals, register real TOTP/approver role, configure confirmed profile/period, issue taxable/non-taxable invoices, post and partially receive them. Review/post a $110/$10 purchase and record two $55 actual payments across periods. Assert ledger, deferred/current/suspense and BAS amounts through authoritative core.
- [ ] Enter retained synthetic payroll/ATO evidence, resolve a real blocker, validate exact/whole labels, freeze with purpose-bound fresh auth, export actual PDF/CSV through native boundary and independently parse/reconcile every value/source/manifest. Inspect pages/screenshots and record hashes.
- [ ] Test cancellation/failure, stale source changes including unchanged totals, forbidden role, unsupported profile and changed idempotency/conflicting revision. Restart/reacquire auth, retrieve same frozen ID/hash/labels, mutate later sources and prove original remains unchanged.
- [ ] Record user-asserted completed external confirmation, create/review/freeze/export linked same-period revision and its separate confirmation, verify retained effective chain plus no ATO transport. Add disputed/corrective branch and rejected/unused recovery real-core cases as integration tests where long TOTP timing would make packaged UI repetition impractical.
- [ ] Run full unit/contracts/SQLCipher integrations/desktop typecheck/build and packaged foundation/invoice/template/SBR regression. Run appropriate `rtk proxy mise exec -- task verify:quick`, `task package:e2e` and contract/coverage tasks; inspect all exit statuses.
- [ ] Review against every design requirement, fix/reverify, then code quality review. Commit validated phase and update programme/development docs with actual results/clean revision/artifact hashes and remaining explicitly unsupported obligations. Preserve the worktree for final maintainer review.
