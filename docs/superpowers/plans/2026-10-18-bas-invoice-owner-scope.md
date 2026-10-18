# BAS invoice root owner scope implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. ROOT owns active Go files, Go/gofmt/tests/builds and commits; subagents draft or review only under `/tmp`. Reuse `issue/sbr-registration-ready`.

**Goal:** Reject an invoice root whose organisation UUID is stored as an exact-byte BLOB instead of silently omitting it from scalar or full reporting reads.

**Architecture:** Share the exact TEXT-or-36-byte-BLOB requested-owner scope between scalar roots, the full root aggregate and the root body query. BLOB matches enter rejection paths only. Scalar guards and a full preflight invalid-owner marker reject static corruption; SQL CASE masks every full-root cell on a non-TEXT owner before driver copying, including corruption introduced after an empty preflight. Preserve existing valid rows, charge arithmetic, limits and transaction ownership.

**Tech Stack:** Go, native SQLCipher, real invoice writers, race tests and existing real TLS regression journeys.

Status: accepted, 2026-10-18. This corrects direct invoice-root ownership only. Child-owner and malformed-reference fanout, other module scopes and overall Task 5 remain open.

## Chunk 1: Reproduce and correct the owned boundary

**Files:** Modify `services/core/internal/invoices/reporting_scalar_keys.go` and `reporting.go`; create `reporting_owner_scope_sqlcipher_test.go` using existing native fixtures/probes.

- [x] Independently review this plan against the accepted invoice scalar/full reporting contracts and the remaining scope boundary.
- [x] Activate a canonical TEXT-owned root-only invoice writer control and require its exact counted/readable key. Add corruption-only regressions for hidden same-byte BLOB draft ownership and a distinct canonical V7 UUID stored as BLOB remaining outside scope. For drift, mutate through `beforeKeyRead` after `ReadBASKeys`' internal fresh empty summary and immediately before guarded key SQL; require the nil/error zero-key sentinel and no body read. Verify stored type/bytes, unchanged revisions/total_changes and authenticated caller transaction.
- [x] Add full preflight/capture canonical and unrelated-owner controls plus same-byte owner rejection before body queries. Use `beforeFirstInvoiceHydration` to corrupt the owner after an empty preflight and replace its proto with 9 MiB; require nil/error, less than 2 MiB Go allocation measured after fixture mutation and no later body queries. Preserve the outer `SELECT id,organisation_id,version,state,number` marker over the masked projection so existing body-query detection/hooks stay effective.
- [x] ROOT runs actual behavioral RED before production changes:
  `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/invoices -run 'OwnerScope|SameByteBlob|UnrelatedBlob' -count=1 -timeout=10m`
- [x] Add a shared `basInvoiceOwnerScopeSQL` predicate requiring BLOB storage, exactly 36 bytes and equality to `CAST(target.organisation_id AS BLOB)`. Add explicit non-TEXT owner invalidation to scalar/root preflight. Keep full valid root ordering and columns, masking all eight cells for a non-TEXT owner in SQL. No stored owner normalization or widened child/reference ownership.
- [x] Run regression GREEN. Obtain independent specification then quality review and resolve verified findings before final-source gates; the complete invoice suite is included in Chunk 2's affected native race gate.

## Chunk 2: Final validation and acceptance record

- [x] Run affected invoice/accounting/reporting native race, existing real TLS invoice/BAS and purchase commit/audit-rollback journeys, and all default Core tests against frozen final source.
- [x] Run pinned `pnpm core:build` after Go tests finish and independently verify binary SHA-256.
- [x] Check formatting and staged scope/whitespace. Update programme, lifecycle, complete-source and bounded-effect records with exact evidence and remaining owner/reference limits.
- [x] Commit the validated checkpoint and verify a clean worktree. Do not mark overall Task 5 complete.

## Evidence

The earlier read-only invoice draft identified TEXT-only root scope in both scalar and full reporting. GPT-6 Luna Max plan review cleared after clarifying the exact hook boundaries, canonical/foreign controls and 9 MiB allocation probe. ROOT activated the scalar tests and recorded actual native behavioral RED: summary silently returned an empty census and the post-internal-summary key read also returned empty without error (`/tmp/tammy-bas-invoice-owner-scope-scalar-native-red-2026-10-18.log`, exit 1 in 2.724s). Canonical root and unrelated BLOB UUID controls did not fail. The shared rejection-only scope and explicit scalar owner-type invalidation passed those native regressions in 3.614s (`/tmp/tammy-bas-invoice-owner-scope-scalar-native-green-2026-10-18.log`). Full API tests were activated next, before changing the full selectors.

The full API then reproduced the same omission: root preflight returned an empty reservation, and a BLOB owner introduced after capture's fresh empty preflight returned a successful empty capture after five body queries (`/tmp/tammy-bas-invoice-owner-scope-full-native-red-2026-10-18.log`, exit 1 in 2.102s). The root aggregate now uses the shared scope with explicit owner-type invalidation. The body query applies that scope and CASE-masks every root cell on invalid owner storage in the same statement. Combined scalar/full native regressions passed in 5.537s (`/tmp/tammy-bas-invoice-owner-scope-native-green-2026-10-18.log`), including exact canonical protobuf/payload equality, static rejection before body queries and the late 9 MiB body remaining under 2 MiB Go allocation after mutation. Independent GPT-6 Luna Max specification and quality reviews cleared.

Final-source complete affected invoice/accounting/reporting native race passed in 162.756s/102.415s/32.189s (`/tmp/tammy-bas-invoice-owner-scope-complete-native-race-final-2026-10-18.log`), with no exclusions. Existing real TLS invoice/BAS and purchase commit/audit-rollback journeys passed in 83.360s (`/tmp/tammy-bas-invoice-owner-scope-real-tls-final-2026-10-18.log`). All default Core packages passed (`/tmp/tammy-bas-invoice-owner-scope-default-core-final-2026-10-18.log`). The pinned SQLCipher 4.15.0 production build passed (`/tmp/tammy-bas-invoice-owner-scope-production-build-final-2026-10-18.log`), and independent `shasum -a 256` confirmed binary SHA-256 `b79ba0c2cdb3839f757bac98acee6e1661d412802adc37239e38eeafbea4fbc4`. Formatting and staged scope/whitespace checks passed. These existing-route journeys do not accept a new endpoint or combined producer.
