# BAS Task 5 closeout implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. ROOT owns active repository edits, Go/gofmt/tests/builds and commits. GPT-6 Luna Max agents audit, draft under `/tmp` and independently review. Reuse `issue/sbr-registration-ready` from `40fe2ba3`.

**Goal:** Accept complete BAS source capture through a real preparation command using one caller-owned SQLCipher transaction.

**Architecture:** Keep module-owned queries and mature body validation. Coordinate aggregate reservations, typed identities, exact nomination, shared limits, body reconciliation, coverage and source-currentness hashes in `internal/baspreparation`. Keep the existing saved-workpaper route compatible. Preserve Tasks 1–4, migrations and existing financial policy.

**Tech Stack:** Go, native SQLCipher 4.15.0, generated BAS contracts, existing authenticated local command transactions and real TLS. Every shell command uses `rtk proxy`. Go commands run from `services/core` with `env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec --`.

Status: in progress, 2026-10-20. The user authorised this closeout after the accepted payment and posting-allocation owner checkpoints. This plan consolidates the remaining Task 5 work; Tasks 6–11 retain their existing lifecycle scope. No Task 5 or packaged phase-three acceptance follows from a dependency alone.

## Chunk 1: Freeze the finite dependency inventory

- [x] Audit every remaining purchase family across scalar discovery, preflight, empty selection, actual body SQL and mature replay. Record protected paths and confirmed source gaps separately from observed native failures. Map every gap to a repair group below; add no unrelated hardening.
- [ ] Pin the minimum current-source/current-cash and exact legacy nomination contracts against the accepted writer/reader oracles. Separate decision/body categories from physical keys; reserve every additional retained category explicitly.
- [ ] Pin coordinator interfaces, stage ordering, coverage identity representation and the production command acceptance path. Independently review this plan before implementation of the corresponding chunk.

The fixed repair groups are: remaining modern child/proof families; opening roots and replacement/reference closure; required parent/reference fanout and dependent-statement guards. Final inventory must name every affected table and statement before these groups can be accepted. Existing direct-owner checkpoints are retained; source inspection is not a runtime-defect claim.

Independent GPT-6 Luna Max source audit is recorded in `/tmp/tammy-task5-reader-inventory-2026-10-20.md`; ROOT read the complete report. The finite matrix is:

| Repair group | Tables / ownership | Statements to cover |
| --- | --- | --- |
| Modern payment allocation | `purchase_payment_allocations_v1.organisation_id` | scalar family; raw source query 3 in full/modern preflight/body; modern empty branch; `verifyPaymentAllocations` |
| Modern evidence/event history | `purchase_tax_evidence_v1`, `purchase_accounting_events_v1`, `purchase_command_audit_v1` direct owners | scalar evidence/event families; operation-key and journal-ID discovery; raw source/event/command companion queries; `retainedEvidenceHistory`, `verifyEvents`, `paymentEvents`; existing revision-claim/idempotency companions retain event-key scope without invented organisation columns |
| Modern claim history | `purchase_gst_attributions_v1`, `purchase_gst_deferrals_v1`, `purchase_gst_deferral_portions_v1` direct owners | scalar families; raw preflight/body and attribution journal-ID discovery; `loadAttributions`, `loadDeferrals`, `verifyDeferralPortions`; exact required payment/allocation/evidence/event links |
| Opening owner/closure | `opening_conversions` owner; `purchase_opening_payables` / `opening_items` inherited owner; full coverage `purchase_legacy_reviews_v1` | scalar root seed / invalid sentinel; root/closure census and identity discovery; opening journal/account discovery; raw conversion/payable/item/review preflight/body; opening/conversion hydration and existing replacement/orphan checks |
| Manual reference / receipt discovery | `journals`, `journal_lines`, `accounts`, `tax_facts`; source IDs from `documents` and `purchase_opening_payables` / `opening_conversions` | `purchaseCoverageManualJournalEdges` and `purchaseCoverageLegacyReceiptIDs` in `reporting_coverage.go`, including count/identity/preflight/body statements; fact-derived source discovery and body guards; reconciliation with existing scalar links and accounting proofs |
| Final purchase parity | all retained families above and accepted direct-owner families | exact scalar/raw/hydrated history/journal identity and hash parity under one transaction, empty target and late after-reservation controls |

The source audit confirms missing selection/guard paths, not successful runtime captures or measured allocation failures. Native reproductions precede each production correction. Existing scalar link checks, replay cardinality backstops, replacement/cycle policy and financial validation are retained.

Legacy review reference bodies are assigned to the opening/legacy-history group, rather than an unnamed manual-reference extension: `purchaseCoverageLegacyReviewQuery` and `purchaseCoverageBuildLegacySources` in `reporting_coverage.go`, and reporting calls to `ReadLegacy`, `verifyLegacyScalars` and `verifyLegacyEvents` in `legacy_repository.go`. Their current-support/current-cash meaning belongs to Chunk 4; the repair inventory covers owner/reference discovery and safe body replay only.

## Chunk 2: Shared reservation coordination

**Files:** create `services/core/internal/baspreparation/sources_reservation.go`, `sources_reservation_test.go` and `sources_reservation_sqlcipher_test.go`; reuse `invoices.BASScalarSummary`, `purchases.BASScalarSummary` and `accounting.TaxFactBASScalarSummary`. Payload coordination follows this first scalar stage.

The first reviewed contract is `ReserveBASSourceScalars(ctx context.Context, organisationID string, ports BASSourceScalarPorts, maximumScalarBytes int64) (*BASSourceScalarReservation, error)`. The three typed ports expose only their existing aggregate summary methods. The result preserves all three comparable summaries and combined `RootCount`, `ReceiptCount`, `ScalarKeyCount` and `ScalarBytes`. Accounting contributes zero roots/receipts and its fact count remains explicit in its summary, without a semantic candidate count. Reject negative budgets before reads, clamp to 8 MiB, validate organisation/count/charge consistency, and checked-sum shared ceilings of 1,000 roots, 1,000 receipts, 16,384 physical keys and 8 MiB scalar bytes. No key, parent-ID or body reader is exposed by these ports. This API is a scalar reservation, not a content fingerprint, final payload reservation or complete producer.

- [x] Write failing coordinator tests proving that every aggregate summary runs before any typed-key/parent-ID preflight; combined root and receipt limits are distinct; shared scalar bytes and physical-key counts are bounded; cancellation/errors return no partial reservation and prevent subsequent reads.
- [x] Run the tests and record actual RED before activating production code.
- [x] Implement the typed scalar reservation stage. Accounting facts remain independent physical keys and add no roots/receipts; semantic candidate mapping happens later. Retain each module's original reservation and independent byte recipe. Do not subtract purchase tax-fact bytes on identity alone, equate the modules' per-row charges, or present an upper bound as the exact final payload.
- [ ] Validate unique complete-journal identity/organisation/charge agreement and preserve all additional decision/document/selector categories for final arithmetic. Shared journal proof identity never merges financial effects.
- [x] Run focused unit/native SQLCipher tests and independent specification then quality review for the scalar reservation stage. Payload coordination remains separate.

## Chunk 3: Close the recorded reader repair groups

**Files:** purchase `reporting_scalar_modern.go`, `reporting_scalar_legacy.go`, `reporting_coverage*.go`, `reporting_postings*.go` and the actual reporting replay helpers identified by the inventory; focused native regression files beside them.

- [ ] Reproduce each recorded omission or rejected-body copy with real writer-backed native SQLCipher tests before its production repair. Include canonical TEXT controls and unrelated UUID scope isolation.
- [ ] Complete remaining modern child/proof owner scope and required-parent validation in their actual scalar, raw and replay statements, including empty captures.
- [ ] Complete opening payable/conversion/item root seeding, reachable replacement closure, scalar references, raw body and reporting replay protection.
- [ ] Complete the recorded missing/foreign/BLOB reference fanout and dependent-family statement guards. Preserve selected cells, ordering, counts, charges, fingerprints and ordinary APIs for valid retained data.
- [ ] Verify static rejection before bodies, late same/cross-row mutation, masked empty sentinels, bounded Go allocation for rejected 9 MiB cells, short errors, unchanged retained state/revisions and caller transaction ownership.
- [ ] Independently review the complete finite inventory against implementation and native evidence; close the group only when every recorded row is covered.

## Chunk 4: Exact legacy nomination and complete proof reconciliation

**Files:** new focused purchase-owned selector/nomination files and tests, using `reporting_legacy_decisions.go`, `reporting_document_states.go`, `reporting_modern_nomination.go` and the accepted candidate oracles. Exact interfaces and SQL projections are recorded after Chunk 1 review.

The writer-path audit in `/tmp/tammy-task5-nomination-contract-2026-10-20.md` was revised after ROOT challenged an initial stored-hash proposal. The selected approach adds no hash-column migration: document review changes candidate/status/version atomically, legacy decisions pin the exact current source, and journal reversal changes the original's state/version. Use existing current document status/version/raw SHA/promotion plus complete bounded decisions for provisional source matching, then recompute reviewed Candidate hashes and support proofs through the full reader and abort on any disagreement. Never widen the candidate set after hydration. A separately charged exact Candidate scalar projection remains an implementation option only if required by a concrete failing fixture, not a prerequisite to invent.

The missing current-cash projection must include every existing predicate input: exact owner/journal/source identities and revisions, direct and tax-fact source associations, posting date/state, reciprocal reversal closure, line IDs/sequences/debit/credit, and current account ID/version/report classification. Current account classification can change without journal version, so journal identity alone is insufficient. Modern supporting payments additionally need actual payment dates alongside retained accounting version/source hash. Preserve every unpromoted document in complete coverage, including an unresolved NEEDS_REVIEW source; the existing reviewed-only `LegacyCandidates` credit helper is not the complete coverage census. Only verified consistent evidence-only links suppress a separate legacy coverage unit; promoted documents remain history, and POSTED/REPLACED opening payables remain units.

Final candidate identity includes module/kind and the exact component/sequence tuple; a shared parent source or journal never merges distinct financial units. Proof-body deduplication and any byte discount are separate from candidate count. These choices still require focused contract tests and independent review before implementation/acceptance of Chunk 4.

- [ ] Write API/behaviour REDs for separately bounded current owner/support/opening/cash facts required by legacy decisions. Include all immutable applicable history, promoted documents, valid evidence-only sources, conflicting/stale reviews, later payments and reversal ancestry.
- [ ] Implement only the reviewed purchase-owned selectors, with aggregate reservation, same-statement masks and independently recomputed charges. Do not load unreserved source/event/journal bodies before the candidate fence.
- [ ] Implement exact legacy admission/suppression and opening candidate rules; keep history as proof without allocating one financial effect per decision. Preserve distinct posting/payment allocations and fallback semantics.
- [ ] Reconcile the provisional candidate set and every selector/decision hash against complete module bodies. Any missing, additional or mismatched proof fails the whole capture. Keep every current accounting tax fact an independent typed blocker.
- [ ] Pin only writer-supported journal-line mappings and verify exact duplicate accounting facts using every retained raw cell and nullable provenance field before removing duplicate purchase proof charges.
- [ ] Run writer-backed nomination/conservation, corruption/drift/allocation and cancellation tests; obtain independent specification then quality review.

## Chunk 5: Complete transaction-scoped producer

**Files:** create `services/core/internal/baspreparation/sources.go` with focused reservation/mapping/hash/reconciliation companions and tests as necessary. Reuse existing repositories for profile, period, PAYG, organisation, rules and revisions; coordinator owns no module-table SQL.

- [ ] Write failing tests for deterministic typed union, exact disjoint parent fallbacks, zero/unpaid/old/future/undated sources, eligible/excluded/unresolved/unsupported decisions, cancellation and no partial capture.
- [ ] Enforce shared root/receipt/scalar/effect/byte limits before source-body hydration. Final arithmetic includes every retained category exactly once and only proven duplicate reuse. Test exact limit and limit+1/one-byte-short with read-order probes.
- [ ] Hydrate accepted module readers through the same executor and reconcile exact physical/candidate/proof sets, nullable provenance, counts and independent byte charges against reservations.
- [ ] Build complete bounded coverage and supported integer/rational calculations. Preserve unresolved manual tax facts as source-linked blockers; never infer GST direction/timing from sign, account or date. Prove G1/1A/1B and payable/current/deferred/suspense conservation.
- [ ] Hash complete ordered coverage and meaningful source decisions/evidence. Retain the complete revision vector for integrity; exclude aggregate `financial_revision` and report-only events from source-currentness comparison. Same-total source/evidence changes must invalidate review.
- [ ] Use real module rows for mixed invoice, modern/legacy/opening purchase, manual-fact and shared-journal tests. Prove no writes or transaction completion by the producer; native encrypted reopen and caller rollback retain exact results.
- [ ] Obtain independent specification then quality review before command integration.

## Chunk 6: Task 5 command acceptance and final evidence

**Files:** preparation command/domain composition and authenticated `services/core/internal/localproduct/` integration tests. Use existing canonical preparation contracts and expected-version/idempotency infrastructure; preserve `CreateBasDraft` semantics.

- [ ] Write a real TLS/native SQLCipher command RED that captures and appends the exact complete calculation under confirmed organisation/profile/period/rule/PAYG references.
- [ ] Mount the required preparation command through existing authorization and command transaction ownership. Capture, calculation append, retained result and audit commit atomically; rollback and identical lost-response replay have single effects.
- [ ] Test complete coverage identity/hash/amounts through the command, unchanged-total source changes, issued-unposted blockers, unsupported manual facts, cancellation/bounds and rollback. Existing saved-workpaper TLS tests are compatibility evidence only.
- [ ] Run focused producer/command tests, then complete affected native race suites across baspreparation/reporting/invoices/purchases/accounting/artefacts/localproduct. Record the previously accepted purchase stress exclusion explicitly if still used; do not exclude new closeout regressions.
- [ ] Run default Core, any changed-contract generation/checks, the pinned production build and independent binary hash verification. Inspect full outputs and exit results; final Go source is frozen throughout final gates.
- [ ] Independently review full Task 5 requirements and final documentation, commit validated work, verify clean worktree/hash, and only then mark Task 5 accepted. Tasks 6–11 and packaged phase three remain open until their own acceptance.

## Evidence and remaining work

Starting source `40fe2ba3e2eedd1fa86597f45db1b6b64c024a2f` is clean. It accepts direct posting-allocation owner rejection and the preceding direct payment-owner checkpoint; native race, existing-route TLS, default Core, pinned build and GPT-6 Luna Max reviews passed as recorded in their plans. Starting binary SHA-256 is `42e3691a6f12de757d4d36b86fe87b57dbb56b43afe9a234b40ed09ebec6366f`.

This closeout is authorised implementation work, not evidence of Task 5 completion. The [complete source design/plan](2026-10-12-bas-complete-source-capture.md), [bounded effect-key constraints](../specs/2026-10-13-bas-bounded-effect-key-design.md) and [lifecycle plan](2026-10-05-bas-preparation-lifecycle.md) remain authoritative. Runtime RED/GREEN, review findings, exact commands, final results and commits are appended as work proceeds.

Independent GPT-6 Luna Max architecture review approved the scalar-only first slice: retain the exact three module summaries, count only invoice/purchase roots and receipts, combine all physical keys/bytes, and expose no key/parent/body reader or payload/candidate acceptance. ROOT wrote unit tests first. Actual API-absence RED exited 1 before production activation (`/tmp/tammy-task5-source-scalars-api-red-2026-10-20.log`) for the missing `BASSourceScalarPorts`, `ReserveBASSourceScalars` and result/error types. After implementation, the focused unit race run passed in 1.823s (`/tmp/tammy-task5-source-scalars-unit-green-2026-10-20.log`).

The focused command is `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race ./internal/baspreparation -run '^TestReserveBASSourceScalars' -count=1` from `services/core` (API RED omitted `-race`). The tests exercise exact mixed-summary preservation, zero/short/exact and clamped budgets, separate shared root/receipt fences, independent fact keys, physical/byte capacity, invalid/overflowing summaries and cancellation/module failure without partial results. Synthetic count-only summaries test arithmetic and make no body-validity claim.

Independent specification review cleared the scalar contract and identified an ambiguous manual-reference inventory label, now resolved to exact tables/helpers above. Subsequent quality review found family-derived count bounds were missing: a purchase summary could underreport roots/receipts despite its mandatory posting/payment families. ROOT added five native-independent behavioral regressions before fixing production code; the actual race RED exited 1 in 0.580s (`/tmp/tammy-task5-source-scalars-count-consistency-red-2026-10-20.log`). The fix requires invoice postings not exceed roots, purchase postings <= roots <= postings+documents+opening payables, and purchase payments <= receipts <= payments+manual journals. Final focused unit race passed in 1.569s (`/tmp/tammy-task5-source-scalars-unit-final-2026-10-20.log`); independent quality re-review cleared the correction.

ROOT rejected the initial native sketch's placeholder fixture, requested a concrete writer-backed draft, read the complete replacement, and activated only that executable replacement. Initial native integration passed in 2.193s. ROOT then added hard-coded two-root/zero-receipt/three-key expectations, exact/one-byte-short native budgets and caller rollback with persisted-zero checks. Final focused native race passed in 2.280s (`/tmp/tammy-task5-source-scalars-native-final-2026-10-20.log`): `go test -race -tags tammy_sqlcipher ./internal/baspreparation -run '^TestReserveBASSourceScalars' -count=1 -timeout=10m` under the same RTK/mise environment. Real invoice Save, document Create and journal/Post TaxFact writers share one schema-14 SQLCipher transaction; the coordinator issues only the three real aggregate summary queries and preserves total_changes, all seven source revisions and caller authentication. Independent final GPT-6 Luna Max review cleared implementation, unit and native tests. The complete default baspreparation package also passed in 1.087s (`/tmp/tammy-task5-source-scalars-complete-default-2026-10-20.log`). Complete native baspreparation race passed in 335.583s with no exclusions (`/tmp/tammy-task5-source-scalars-complete-native-2026-10-20.log`), using `go test -race -tags tammy_sqlcipher ./internal/baspreparation -count=1 -timeout=20m` under the same RTK/mise environment. No Go source changed after final focused native GREEN. Formatting and whitespace checks passed; this accepts the scalar reservation stage only, not final payload/candidate fences or Task 5.
