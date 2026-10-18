# BAS accounting fact owner scope implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. ROOT owns active Go files, Go/gofmt/tests/builds and commits; subagents draft or review only under `/tmp`. Reuse `issue/sbr-registration-ready`.

**Goal:** Reject an accounting tax fact whose organisation UUID is stored as an exact-byte BLOB instead of silently omitting it from scalar or full reporting reads.

**Architecture:** Share canonical TEXT equality plus an exact 36-byte BLOB requested-owner arm across scalar scope, full preflight and candidate selection. BLOB matches exist solely for rejection. Explicit owner-type guards reject static corruption; candidate SQL masks fact cells before parent joins so late owner drift yields one invalid, fully masked row even when its parent is missing. Preserve valid values, order, charge arithmetic, limits and the caller's transaction.

**Tech Stack:** Go, native SQLCipher, real journal/fact writers, race tests and existing real TLS regression journeys.

Status: accepted, 2026-10-18. This corrects direct `tax_facts.organisation_id` scope only. Parent/reference fanout, other module scopes and overall Task 5 remain open. The approved bounded accounting discovery and full-reader contracts remain authoritative; this adds no API, policy, migration or reservation category.

## Chunk 1: Reproduce and correct the owned boundary

**Files:** Modify `services/core/internal/accounting/tax_fact_bas_keys.go` and `tax_fact_reporting.go`; create `tax_fact_owner_scope_sqlcipher_test.go` using existing native fixtures/probes.

- [x] Independently review this plan against the accepted accounting scalar/full contracts, actual LEFT JOIN semantics and the remaining scope boundary.
- [x] Activate real-writer canonical scalar/full controls with exact identity, provenance and payload checks. Run static BLOB-owner summary/read/full-preflight/full-capture assertions independently so scalar failures cannot hide the full API regression. Require rejection before key/body queries, exact stored BLOB type/36-byte UUID bytes, unchanged revisions/`total_changes` and an authenticated caller transaction. A distinct canonical UUID stored as BLOB remains outside direct scope.
- [x] Add scalar drift immediately before key SQL, after `ReadTaxFactBASKeys`' own fresh empty summary. Require two summaries including the baseline, one guarded key query, the exact zero-key bound sentinel, no bodies and no reader writes.
- [x] Add full capture drift immediately before candidate SQL, after capture's own empty preflight. Test both present and missing parent links; inject the target BLOB owner and a 9 MiB `source_id` via SQL. Verify stored type/bytes/body length without fetching the body. Sample allocation after fixture mutation; require nil/error, less than 2 MiB Go allocation, one candidate query and no later journal/line body queries. The foreign-owner mutations are corruption-only fixtures, not valid financial histories.
- [x] ROOT records actual behavioral native RED before the corresponding production edits:
  `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/accounting -run 'TaxFactOwnerScope|TaxFactBASKeysRejectSameByteBlobOwner|TaxFactReportingCaptureAtomically' -count=1 -timeout=10m`
- [x] Add a shared rejection-only owner predicate using requested TEXT equality or BLOB storage, exactly 36 bytes and equality to `CAST(target.organisation_id AS BLOB)`. Use it in scalar scope, full base aggregate, parent validation and candidate selection. Explicitly invalidate non-TEXT owner storage in scalar and full base/parent checks; preserve bound/byte error priority.
- [x] In candidate SQL, mask all 23 fact cells on non-TEXT owner storage inside a guarded relation before the existing LEFT JOINs. Masking the line reference forces all four parent cells NULL too. Keep the actual outer `SELECT fact.id, fact.organisation_id, fact.journal_line_id, fact.tax_code` marker so all existing probes/hooks still identify the candidate statement. Retain original valid 27-cell projection, joins, order and `maxTaxFactReportingFacts+1` limit. Existing NULL-identity scanning rejects the corrupt row without returning a partial capture or copying its oversized cell.
- [x] Run native regression GREEN. Obtain independent specification then quality review; resolve verified findings before final-source gates.

## Chunk 2: Final validation and acceptance record

- [x] Run complete affected invoice/accounting/reporting native race against frozen final source, with no exclusions.
- [x] Run existing real TLS invoice/BAS and purchase commit/audit-rollback journeys, and all default Core tests. These verify existing routes, not a new endpoint or the combined producer.
- [x] Run pinned `pnpm core:build` after Go tests finish and independently verify binary SHA-256.
- [x] Check formatting and staged scope/whitespace. Update programme, lifecycle, complete-source and bounded-effect records with exact evidence and remaining owner/reference limits.
- [x] Commit the validated checkpoint and verify a clean worktree. Do not mark overall Task 5 complete.

## Evidence

Static drafts identified TEXT-only `tax_facts.organisation_id` predicates in scalar and full readers. The preceding invoice-root checkpoint is accepted in `2a56a684`. Independent GPT-6 Luna Max plan review cleared the guarded-facts design and confirmed the existing LEFT JOIN semantics; the plan includes its independent static API checks and missing-parent drift case.

ROOT activated the initial writer-based scope tests before production edits. Native behavioral RED reproduced an empty successful scalar summary and a successful empty guarded read after its own fresh empty summary (`/tmp/tammy-bas-accounting-owner-scope-initial-native-red-2026-10-18.log`, exit 1 in 3.768s). After adding only the shared owner predicate and scalar owner-type guard, scalar checks passed while full preflight reproduced an empty successful reservation (`/tmp/tammy-bas-accounting-owner-scope-scalar-green-full-red-2026-10-18.log`, exit 1 in 3.263s). ROOT then inspected and activated the consolidated GPT-6 Luna Max test draft with independent full API checks and both late-parent variants before editing full-reader selection.

The consolidated full API RED independently reproduced empty successful preflight and capture, plus empty successful capture after its own fresh preflight with both present and missing journal-line parents (`/tmp/tammy-bas-accounting-owner-scope-full-native-red-2026-10-18.log`, exit 1 in 7.917s). After broadening only direct owner discovery and masking the guarded fact relation, all focused native cases passed in 6.486s (`/tmp/tammy-bas-accounting-owner-scope-native-green-2026-10-18.log`). Canonical payload/protobuf/provenance equality, unrelated BLOB isolation, exact stored type/bytes, two-summary/one-key drift and both 9 MiB allocation probes pass. Independent GPT-6 Luna Max specification then quality reviews cleared.

Final-source complete affected invoice/accounting/reporting native race passed in 186.870s/85.015s/35.284s, with no exclusions (`/tmp/tammy-bas-accounting-owner-scope-complete-native-race-final-2026-10-18.log`). Existing real TLS invoice/BAS and purchase commit/audit-rollback journeys passed in 111.985s (`/tmp/tammy-bas-accounting-owner-scope-real-tls-final-2026-10-18.log`). All default Core packages passed (`/tmp/tammy-bas-accounting-owner-scope-default-core-final-2026-10-18.log`). The pinned SQLCipher 4.15.0 production build passed (`/tmp/tammy-bas-accounting-owner-scope-production-build-final-2026-10-18.log`), and independent `shasum -a 256` confirmed binary SHA-256 `6b39a2beb7d115271c53a0011cee253d4227929d06ff113e22b225593f7f437d`. Formatting and staged scope/whitespace checks passed. These existing-route journeys do not accept a new endpoint or combined producer.
