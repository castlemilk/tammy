# BAS purchase scalar root owner scope implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. ROOT owns active files, Go/gofmt/tests/builds and commits; subagents draft/review under `/tmp`. Reuse `issue/sbr-registration-ready`.

**Goal:** Include exact requested BLOB owner UUIDs in physical purchase posting/document scalar discovery so existing storage guards reject them instead of returning empty success.

**Architecture:** Extend only the direct owner arm of the posting predicate with `typeof(p.organisation_id)='blob' AND length(p.organisation_id)=36 AND p.organisation_id=CAST(target.organisation_id AS BLOB)`, and add the equivalent expression on `d.organisation_id` to the document predicate. `owner` remains the separate `organisations` join alias. Preserve existing posting document/journal TEXT reference scope, the document root anti-join, all other family predicates and the generic scalar storage/window guards. This is rejection-only discovery; no identity is normalized.

**Tech Stack:** Go, native SQLCipher, real `PostReviewed`/document writers, race tests and existing real TLS regression journeys.

Run every Go test command below from the worktree's `services/core` directory. Run gofmt and the pinned production build from the worktree root.

Status: accepted, 2026-10-19. This owns only `purchase_postings_v1.organisation_id` and `documents.organisation_id` in `SummarizeBASKeys`/`ReadBASKeys`. Full coverage root/fallback/identity/body discovery and later repository replay require separate same-statement protection and remain open. Other physical families, opening orphan/replacement closure, broader reference fanout, current-source/current-cash selectors, reconciliation, shared fences, combined producer and overall Task 5 remain open. No API, schema, financial policy or byte category changes.

## Chunk 1: Reproduce omitted physical roots

**Files:** Create `services/core/internal/purchases/reporting_scalar_root_owner_sqlcipher_test.go` and, if needed, a focused sibling atomic test file using existing `purchaseBASScalarProbe`, writer fixtures, state snapshots and trigger-restoring mutators.

- [x] Independently review this scope against the actual physical/full reader separation and existing generic owner/storage guards.
- [x] Add a canonical real-writer control for exact keys, all family/root/receipt counts, source provenance and source-derived scalar byte charges. Preserve valid root flags, ordering, nil-vs-present hashes/journal IDs and small-budget priority (also covered by existing scalar budget tests in the final suite).
- [x] Build isolated corruption fixtures from real writer rows. Prove an otherwise empty physical target census before mutation, with every other family outside target scope. Cover each direct owner as an exact requested 36-byte BLOB with foreign/missing target TEXT parent edges. Summaries and key reads must return nil/`ErrRepository` before typed key or body queries. A distinct BLOB UUID with foreign parents remains outside this direct scope. Also retain the canonical target-parent posting case so existing reference-arm rejection remains effective.
- [x] Hook immediately before existing guarded key SQL, after `ReadBASKeys`' own fresh empty summary, and mutate one family owner to requested BLOB storage. Require two summary reads including the baseline, one key query, no bodies and the exact zero-key `SourceBoundError` with no partial keys.
- [x] Independently exercise each family with the same late owner mutation plus a 9 MiB BLOB in its uncharged version cell (`posting.reviewed_version` / `document.version`). Verify type/UUID bytes/length and scope isolation without fetching the large cell, restore fixture triggers/check settings, then measure Go allocation. Require the same zero-key sentinel, short error, less than 2 MiB allocation and unchanged post-mutation revisions/`total_changes` plus authenticated caller transaction. The byte charge alone cannot withhold this version cell. An additional successful empty key read proves the baseline before the measured late operation; its summary/key counters are recorded separately.
- [x] ROOT inspects/activates drafts and runs actual native behavioral RED before production edits:
  `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/purchases -run PurchaseScalarRootOwner -count=1 -timeout=10m`

## Chunk 2: Reach existing rejection guards

**Files:** Modify only `services/core/internal/purchases/reporting_scalar_modern.go` and `reporting_scalar_legacy.go` for production behavior.

- [x] Add the exact requested-owner BLOB arm to `BASScalarPosting`'s predicate, preserving its existing TEXT owner/document/journal arms.
- [x] Add the same arm to `BASScalarDocument`'s predicate. Preserve its TEXT-only root/fallback anti-join and existing invalid-state checks. Generic mandatory organisation storage checks already reject a selected BLOB owner and mask all selected cells; no projector or ordinary/full reader change is needed for this checkpoint.
- [x] Run focused native GREEN. Obtain independent specification then quality review and resolve verified findings before final-source gates.

## Chunk 3: Final validation and acceptance

- [x] Run complete affected purchase/accounting/reporting native race on frozen Go source, retaining only the previously accepted expensive public-posting stress exclusion. No scalar or owner-scope test is excluded:
  `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/purchases ./internal/accounting ./internal/reporting -skip '^TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration$' -count=1 -timeout=30m`
- [x] Run existing real TLS invoice/BAS save/replay and purchase commit/audit-rollback journeys:
  `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/localproduct -run '^(TestLedgerModuleCreatesOrganisationAndInstallsAustralianChartThroughRealServer|TestPurchaseCommandThroughRealTLSCommitsOnceAndRollsBackAuditFailure)$' -count=1 -timeout=20m`
- [x] Run all default Core packages (`rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test ./... -count=1 -timeout=20m`), then pinned `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- pnpm core:build` from the worktree root and independently verify binary SHA-256.
- [x] Check formatting, exact staged scope and whitespace; update programme/lifecycle/complete-source/bounded-effect records with exact evidence and the separate full-coverage follow-up.
- [x] Commit this validated checkpoint and verify a clean worktree. Leave Task 5 and packaged phase-three acceptance open.

## Evidence

The preceding invoice component checkpoint is accepted in `23939f1c`. The read-only purchase audit `/tmp/tammy-bas-purchase-physical-owner-next-audit-2026-10-18.md` identified TEXT-only direct physical scopes and separate full coverage selectors. Further integration inspection found that full coverage later calls `Get`, whose document read precedes posting ownership validation. A preflight-only guard cannot establish atomic late rejection there. This checkpoint therefore isolates the physical scalar correction and leaves full coverage explicitly open.

Independent GPT-6 Luna Max design review approved the scalar-only boundary and two predicate changes. The plan was clarified to name the physical owner columns, distinguish the `organisations` join alias, and specify command working directories. Re-review approved the corrected plan, including the uncharged version-cell allocation probes and previously accepted stress exclusion.

ROOT inspected and activated the atomic draft, then observed actual native RED in 2.222s (`/tmp/tammy-bas-purchase-scalar-root-owner-atomic-red-2026-10-19.log`). Both document and posting exact requested BLOB owners, introduced immediately before key SQL after its own fresh empty summary, returned a successful empty key slice with no error. Both 9 MiB uncharged version BLOBs were verified using SQLite type/length metadata before allocation measurement. Empty summary/key baselines succeeded with all 15 family counts zero, so another family's reference edge did not cause the result. Production predicates remained unchanged during this run.

ROOT inspected the static draft and corrected an assertion that confused two root families with the complete physical-key census; the canonical control instead locates posting/document keys within the whole ordered set. Static failures are nonfatal assertions so both summary and key APIs are observed in RED. Combined native behavioral RED failed in 7.015s (`/tmp/tammy-bas-purchase-scalar-root-owner-native-red-2026-10-19.log`): both pre-summary BLOB owners returned successful empty summaries and key reads, and both ordinary and 9 MiB late drift cases returned empty success. Canonical zero/nonzero writer version/hash/journal/root/order/charge controls, the canonical document/journal reference-arm control and unrelated BLOB graph control did not fail. Only after this run were the two production predicates extended.

Focused native GREEN passed in 7.924s (`/tmp/tammy-bas-purchase-scalar-root-owner-native-green-2026-10-19.log`). Both static owners now reject with no typed key/body query; ordinary late reads hit the exact zero-key sentinel after two summaries and one key query. Both 9 MiB uncharged version probes return no partial keys, remain below 2 MiB Go allocation after mutation/metadata and have errors under 512 bytes. Trigger definitions/check settings, retained state/`total_changes` and caller authentication are preserved. Canonical zero/nonzero, reference-arm and unrelated-owner controls pass. Independent GPT-6 Luna Max specification then quality reviews cleared with no material findings.

Complete affected purchase/accounting/reporting native race passed in 567.803s/56.257s/20.713s (`/tmp/tammy-bas-purchase-scalar-root-owner-complete-native-race-final-2026-10-19.log`) on frozen Go source. Its only exclusion was the previously accepted `TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration` stress fixture; no scalar or owner test was excluded. Existing real TLS invoice/BAS save/replay and purchase commit/audit-rollback journeys passed in 57.295s (`/tmp/tammy-bas-purchase-scalar-root-owner-real-tls-final-2026-10-19.log`); these exercise existing routes, not a combined source-capture producer.

All default Core packages passed (`/tmp/tammy-bas-purchase-scalar-root-owner-default-core-final-2026-10-19.log`). The pinned production SQLCipher build passed (`/tmp/tammy-bas-purchase-scalar-root-owner-production-build-final-2026-10-19.log`); ROOT independently verified binary SHA-256 `cc09c838b779dd92496f3fd942e0fa536b5fcabff66e7871f2a77a762a19bb2f`. Full purchase coverage root/fallback/identity/body discovery and repository replay remain the next separate correction; this scalar checkpoint does not close Task 5 or packaged phase-three acceptance.

Independent GPT-6 Luna Max final documentation review found no material mismatch in the five acceptance records. ROOT verified gofmt, the exact nine-file staged scope, no unstaged edits and clean whitespace. No Go source changed after the final native gates.
