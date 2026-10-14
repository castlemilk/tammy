# BAS candidate mapping oracle implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. ROOT alone edits active Go files, runs Go/gofmt/tests and commits. Reuse the existing issue/sbr-registration-ready worktree; agents draft and review without changing active Go files.

**Goal:** Pin purchase physical-history identities against complete legacy/claim classification and monetary conservation before implementing semantic candidate mapping.

**Architecture:** Add native `purchases_test` fixtures crossing existing scalar readers, real writers and complete body/workpaper readers. Preserve every runtime API and schema guard. These are existing-behavior oracles, not a candidate mapper or producer.

**Tech stack:** Go 1.26.4, native SQLCipher 4.15.0, existing schema 14 and accepted compatibility fixtures.

Design: [candidate mapping boundary](../specs/2026-10-14-bas-candidate-mapping-design.md). Existing fixture/read semantics remain with their owning modules.

## Chunk 1: Independent fixture drafts and design review

Files to create:

- `services/core/internal/purchases/candidate_mapping_legacy_sqlcipher_test.go`: legacy scalar-history/full-reader identity, applicability and contradiction oracles.
- `services/core/internal/purchases/candidate_mapping_claims_sqlcipher_test.go`: attribution/deferral scalar-history/full-reader conservation oracles.

Use existing fixture helpers in `legacy_sqlcipher_test.go`, `claim_sqlcipher_test.go`, `reporting_sources_sqlcipher_test.go`, and the accepted `reporting_scalar_*_sqlcipher_test.go` files. Do not add test-only production interfaces or weaken SQL triggers. Keep drafts under `/tmp/*.pending` until ROOT has read them and no Go process is running.

- [x] Obtain independent legacy and claim/conservation fixture drafts grounded in current writers/readers.
- [x] Review the written boundary and plan independently; resolve scope or financial-policy gaps before activation.
- [x] ROOT reads both drafts and activates only meaningful cross-reader tests, avoiding body-only duplication.

## Chunk 2: Native oracles

- [x] Pin exact legacy review/source hash identities and root/receipt flags against complete history and period/balance classification. Include stale payment facts and promoted/evidence-only history where existing fixtures support them.
- [x] Pin multi-portion/repeated-period deferral decisions against exact scalar identities, original/current remainders, later attribution and sibling balances. Assert complete owned accounting and BAS amount conservation separately from decision counts.
- [x] Assert reads preserve retained state, source revisions and caller transaction ownership. Use real immutable history rather than hand-inserted financial proof bodies.
- [x] ROOT formats and runs the focused native suite. Record the established-behavior baseline honestly; do not claim production RED if the existing implementation already satisfies the oracle.
- [x] Resolve actual fixture errors separately from behavioral discrepancies. Reproduce a behavioral failure before any production fix; obtain review if it changes the scope.

ROOT commands in the worktree's `services/core`:

```sh
rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/purchases -run '^TestBASCandidate.*Oracle' -count=1 -timeout=15m
rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/purchases ./internal/reporting -skip '^TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration$' -count=1 -timeout=30m
```

ROOT runs `rtk proxy mise exec -- gofmt -l services/core/internal/purchases/candidate_mapping_*_sqlcipher_test.go` at the worktree root after Go stops. No oracle/scalar test exclusion is accepted.

## Chunk 3: Acceptance and next dependency

- [x] Independent specification review clears the final tests and design before independent quality review.
- [x] ROOT reads successful focused and affected native race output; records actual logs/status and the accepted stress exclusion.
- [x] Record the exact verified invariants and remaining nomination/policy gaps in the complete-source plan and programme status.
- [x] Check formatting and staged diff, then commit the oracle checkpoint without claiming candidate-union or Task 5 acceptance.

This checkpoint has no runtime behavior, contract or migration change. Broader default/TLS/production-build checks are necessary if a verified discrepancy leads to production changes; otherwise the native real-writer/full-reader journeys directly validate the new tests. Overall producer and packaged phase-three acceptance remain open.

## Verification checkpoint (accepted, 2026-10-14)

Independent GPT-6 Luna Max specification then quality review cleared the final boundary and tests after narrowing promoted-history and combined-scope claims, adding exact receipt/history/coverage/hash comparisons, and anchoring claim headers to writer responses. The final focused native race passed in 5.813s (`/tmp/tammy-bas-candidate-mapping-oracles-native-reviewed-final-2026-10-14.log`), with all four oracle tests and their subtests selected and no exclusions. These characterize existing production behavior; they are not production REDs.

The initial native race exited 1 in 5.608s (`/tmp/tammy-bas-candidate-mapping-oracles-native-first-2026-10-14.log`) because the legacy fixture compared the original source's 1,000-cent GST with a zero-GST supporting document. `legacyRequest` creates that support document and leaves the fixture pointer on it. The corrected oracle anchors its expectation to the original reviewed source; its race baseline passed in 6.866s (`/tmp/tammy-bas-candidate-mapping-oracles-native-final-2026-10-14.log`). Quality review then strengthened writer-header equality and nil diagnostics before the final 5.813s run. No production discrepancy or runtime fix was made.

The final affected purchase/reporting native race exited 0 in 489.070s/20.136s (`/tmp/tammy-bas-candidate-mapping-oracles-complete-native-race-final-2026-10-14.log`); ROOT read its complete output. Only the previously accepted expensive `TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration` stress fixture was excluded; no oracle or scalar test was excluded. Formatting and staged whitespace checks passed. No production code, contract, migration or existing reader changed, so this checkpoint adds no new TLS/default/production-build claim. Candidate admission, nomination selectors, exact union/global fences, duplicate-proof verification and the combined producer remain open.
