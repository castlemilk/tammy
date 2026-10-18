# BAS invoice component orphan implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. ROOT owns active repository files, Go/gofmt/tests/builds and commits; subagents draft/review under `/tmp`. Reuse `issue/sbr-registration-ready`.

**Goal:** Reject invoice income-line and payment-allocation orphans in full reporting reads, matching existing scalar discovery, and withhold malformed component cells before driver copying.

**Architecture:** These tables have no organisation column. Reuse their existing scalar LEFT JOIN graph, candidate scope and link-invalid predicates for full aggregates and body queries. Fully unattributable rows retain the existing global rejection sentinel. Full body projections guard every selected component cell against link or numeric-storage violations in the same statement. Valid charges, ordering, financial semantics and journal enumeration remain unchanged.

**Tech Stack:** Go, native SQLCipher, real invoice/accounting/payment writers, race tests and existing real TLS journeys.

Status: accepted, 2026-10-18. The accepted direct posting/receipt owner correction is `34bb892d`. This checkpoint owns only the two reference-only component families; broader malformed-reference fanout, purchase physical scopes and overall Task 5 remain open. No schema, public API, financial policy or budget category changes.

## Chunk 1: Reproduce component omission and late copying

**Files:** Create `services/core/internal/invoices/reporting_component_orphan_sqlcipher_test.go` and `reporting_component_atomic_sqlcipher_test.go`, using existing writer, budget, mutation and query probes.

- [x] Independently review the design against scalar/full contracts and actual parent relationships.
- [x] Add exact real-writer canonical/scalar/protobuf/provenance/charge controls and foreign TEXT-parent scope isolation. Preserve tiny-budget rejection before hydration.
- [x] Add full preflight/capture and scalar parity regressions for an income line with a target invoice but no posting, fully orphaned income and allocation rows, missing receipt allocations, and BLOB component references to canonical TEXT parents. Verify stored type/key/parent absence, no body or journal reads for static rejection, unchanged revisions/`total_changes` and caller authentication. Fully unattributable orphans must also reject for an otherwise empty requested organisation ID; prove its empty baseline first and do not infer ownership.
- [x] Add late hooks immediately before the income-line or allocation body query, after capture's own fresh preflight. Each component family gets both cases: a newly orphaned row with a 9 MiB numeric BLOB after an empty reservation, and a still-linked row with the same BLOB after a canonical reservation. Verify type/size/parent links without fetching the large cell, restore CHECK settings/triggers, then sample Go allocation. Require nil/`ErrRepository`, a bounded error, less than 2 MiB allocation, exact body prefix through the failing component and no later body/journal proof query. Preserve post-mutation state and authenticated caller transaction.
- [x] ROOT activates and inspects drafts, then records actual behavioral native RED before production edits:
  `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/invoices -run InvoiceComponentOrphan -count=1 -timeout=10m`

## Chunk 2: Share discovery and guard full projections

**Files:** Create private `services/core/internal/invoices/reporting_component_scope.go`; modify `reporting_scalar_keys.go` and `reporting.go`.

- [x] Extract the existing component FROM/scope/link-invalid SQL into private constants without changing scalar semantics. Income scope is posting owner target OR invoice owner target OR both parents missing. Allocation scope is receipt/posting/invoice owner target OR receipt missing. Keep existing owner existence, missing link, empty reference, owner mismatch and positive INTEGER sequence invalidation. Do not add direct-owner BLOB matching to tables without owners.
- [x] Use those shared LEFT JOIN relations/scopes in the two full aggregates. Combine link-invalid with the existing full numeric storage checks (all five income numeric columns, all three allocation numeric columns). Keep resource names, `rowLimit=0`, 512-plus-reference retained byte recipes, family order, bound/byte/integrality priority and journal preflight unchanged.
- [x] Use the existing private guarded-row projection helper for all six income-line and all four allocation cells, with the combined link/numeric-storage-invalid expression. Retain actual outer SELECT markers, original valid values and ordering. Keep scanner value/sequence validation and nil-on-error behavior.
- [x] Run focused native GREEN. Obtain independent specification then quality review and resolve verified findings before final-source gates.

## Chunk 3: Final validation and acceptance

- [x] Run complete affected invoice/accounting/reporting native race with no exclusions on frozen source:
  `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/invoices ./internal/accounting ./internal/reporting -count=1 -timeout=20m`
- [x] Run existing real TLS invoice/BAS save/replay and purchase commit/audit-rollback journeys:
  `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/localproduct -run '^(TestLedgerModuleCreatesOrganisationAndInstallsAustralianChartThroughRealServer|TestPurchaseCommandThroughRealTLSCommitsOnceAndRollsBackAuditFailure)$' -count=1 -timeout=20m`
- [x] Run all default Core packages (`rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test ./... -count=1 -timeout=20m`), then pinned `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- pnpm core:build` from the worktree root and independently verify the produced binary SHA-256.
- [x] Check formatting/staged whitespace and exact scope; update programme/lifecycle/complete-source/bounded-effect evidence. Existing-route TLS does not accept the combined producer or a new endpoint.
- [x] Commit the validated checkpoint and verify a clean worktree. Keep overall Task 5 and packaged phase-three acceptance open.

## Evidence

The read-only audit in `/tmp/tammy-bas-invoice-component-orphan-next-design-2026-10-18.md` found scalar LEFT JOIN discovery but full aggregate/body INNER JOIN omission. It ran no runtime tests; ROOT's runtime evidence follows.

Independent GPT-6 Luna Max plan review approved the candidate/link predicates, parent graph, charge recipes and query-stage design. Its nonblocking clarification is incorporated above: both late corruption variants apply independently to both component families.

ROOT activated and inspected the atomic draft, then observed native RED in 8.587s (`/tmp/tammy-bas-invoice-component-orphans-atomic-red-2026-10-18.log`). The late income orphan reached all five bodies instead of stopping at the third. Still-linked income/allocation 9 MiB numeric BLOBs produced 37,748,888-byte errors and allocated 366,909,136/366,899,160 Go bytes after mutation. Failure previews were bounded before this run. The initial late allocation orphan was rejected by later existing validation, so that draft alone did not prove orphan visibility. Both orphan variants were strengthened to start from a fresh empty reservation with foreign TEXT parent edges, making omission itself observable. No production edits preceded this RED.

ROOT inspected and activated the strengthened drafts, fixing an empty-scalar baseline's expected summary counter to include `ReadBASKeys`' own fresh summary. Full native behavioral RED then failed in 13.943s (`/tmp/tammy-bas-invoice-component-orphans-native-red-2026-10-18.log`). Both late orphans returned an empty successful capture after an empty reservation. Still-linked income/allocation BLOBs again produced 37,748,888-byte errors and allocated 366,908,304/366,900,432 Go bytes. All six static cases passed preflight; capture either returned success or reached five/seven body reads and journal discovery. Both component orphans also returned empty successful preflight/capture for a distinct requested ID whose empty scalar/full baseline had succeeded before mutation. The canonical writer, scalar rejection, foreign TEXT scope and tiny-budget controls did not fail. Production changes followed this behavioral RED.

The shared component scopes and full link/storage guards are implemented. Focused native GREEN passed in 12.516s (`/tmp/tammy-bas-invoice-component-orphans-native-green-2026-10-18.log`), including all static/empty-target cases and all four late 9 MiB probes. Both late orphan cases now fail at their own component query after capture's fresh empty preflight. Still-linked corruption is masked before numeric scanning. All four satisfy the less-than-2-MiB post-mutation allocation ceiling, bounded error, exact three/five body prefixes, no later journal reads, restored checks/triggers and unchanged state/authentication. Independent GPT-6 Luna Max specification then quality reviews cleared with no material findings.

Complete affected invoice/accounting/reporting native race passed in 176.399s/106.416s/42.530s with no exclusions (`/tmp/tammy-bas-invoice-component-orphans-complete-native-race-final-2026-10-18.log`) against frozen Go source. Existing real TLS invoice/BAS save/replay and purchase commit/audit-rollback journeys passed in 96.290s (`/tmp/tammy-bas-invoice-component-orphans-real-tls-final-2026-10-18.log`). These existing routes do not accept a combined producer or a new endpoint. All default Core packages passed (`/tmp/tammy-bas-invoice-component-orphans-default-core-final-2026-10-18.log`).

The pinned `pnpm core:build` passed after those tests, using SQLCipher 4.15.0 for darwin-arm64 (`/tmp/tammy-bas-invoice-component-orphans-production-build-final-2026-10-18.log`). Its final result and independent `shasum -a 256` agree on `87b8fb806d7f5d90173bbd0698ac7f16ea4fde1da938120c21a20c5c150aaf59` for `apps/desktop/resources/core/darwin-arm64/tammy-core`.

Final `gofmt -l` was empty for all five Go files, and staged whitespace checks passed. The checkpoint contains exactly five Go files and these five documentation records, with no unstaged changes at the scope check. Independent GPT-6 Luna Max acceptance-document review found only the lifecycle next-slice wording to update at acceptance; that now names purchase posting/document owner discovery. A separate read-only purchase scope audit is `/tmp/tammy-bas-purchase-physical-owner-next-audit-2026-10-18.md`; it makes no runtime claim and changes no purchase code. Commit identity and clean-tree verification are recorded in Git and the completion report. Overall Task 5 remains open.
