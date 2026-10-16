# BAS legacy decision records implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. ROOT owns active Go files and all Go/gofmt/tests/builds/commits. Reuse issue/sbr-registration-ready; subagents draft only under `/tmp`.

**Goal:** Read complete canonical legacy decision history within an explicit count/decision-byte reservation, without financial/source/event bodies.

**Architecture:** Add aggregate-only summary and guarded decision projection beside the accepted purchase scalar APIs. Keep existing full readers authoritative for current source/support/event proofs. [Design](../specs/2026-10-16-bas-legacy-decision-records-design.md).

**Tech Stack:** Go, SQLCipher, canonical protobuf, native race and real TLS regression tests.

## Chunk 1: Bound and validate the decision history

Files: create `services/core/internal/purchases/reporting_legacy_decisions.go`, `reporting_legacy_decision_sql.go`, `reporting_legacy_decisions_test.go` and `reporting_legacy_decisions_sqlcipher_test.go`; existing `ReadLegacy`, contracts and migrations stay unchanged.

- [x] Independently review the design/API and exact named charge recipe before implementation.
- [x] Activate API-absence tests and record RED in both default and native-tag build contexts with the focused command below from `services/core`. Native behavioral fixtures were activated after implementation; this is an API-absence RED, not a behavioral native RED.
- [x] Implement complete owned aggregate summary, per-source/total/byte ceilings, storage/owner guards and negative/short caller reservation checks.
- [x] Implement SQL cumulative/count/sentinel/per-source guards before driver copying; canonical record/hash/history validation, COUNT-only outer congruence, independent loaded charges, cancellation and no partial results.
- [x] Test real writers against full `ReadLegacy`/complete retained history, exact/short budgets, empty and multi-source histories, 1,000/1,001 per-source history, malformed/canonical/scalar/owner drift, oversized rejected-cell allocation bounds, encrypted reopen and caller rollback/no writes. Label decision-only count fixtures without events as such.
- [x] Obtain independent specification then quality review; resolve verified findings and pass final focused native race.

## Chunk 2: Verify and record

- [x] Run complete affected purchase/reporting native race, with only the previously accepted `TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration` stress exclusion; no decision/scalar/nomination exclusion.
- [x] Run final-source existing real TLS invoice/BAS and purchase commit/audit-rollback journeys.
- [x] Run all default Core tests.
- [x] Run pinned production build; independently verify binary SHA-256.
- [x] Check formatting, staged scope/whitespace, and update programme/lifecycle/complete-source records and bounded-effect constraints with precise limits.

Finish by committing the validated source and documentation, then verifying the worktree is clean.

## Evidence and boundaries

Default/native-tag API-absence builds both exited 1 for the missing methods/types (`/tmp/tammy-bas-legacy-decisions-api-default-red-2026-10-16.log`, `/tmp/tammy-bas-legacy-decisions-api-native-red-2026-10-16.log`). The first default API test passed in 0.612s. The first activated native behavior suite passed in 19.363s; no behavioral RED or production correction is claimed. ROOT added a fresh-reservation malformed-proto decoder case and real claim/link owner-edge regressions following review. The final focused command `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/purchases -run '^TestPurchaseBASLegacyDecisions' -count=1 -timeout=15m` exited 0 in 23.361s (`/tmp/tammy-bas-legacy-decisions-focused-native-owner-proof-final-2026-10-16.log`).

Real review/posting writers preserve promoted document history, opening decisions, distinct periods, Unicode reasons and complete support lists against full `ReadLegacy`. Opening setup uses the existing opening fixture and owned reader; scale rows explicitly omit result events and prove only decision history/scalar integrity. Independent byte-length and canonical hash oracles verify the exact six retained cells. Malformed/noncanonical protos reach the decoder under fresh reservations. Claim/link fixtures prove valid reads first, then reject foreign reference owners and foreign reviews/sources whose only requested-owner touch is the claim/link. Oversized 9 MiB drift cells stay under a 2 MiB Go allocation allowance after mutation, and a controlled last-row oversized count sentinel preserves count priority. Summary/body probes, current amount/event corruption, encrypted reopen, caller rollback and retained state checks pin the owned boundary.

Independent design review cleared using inherited settings during the earlier quota limitation. GPT-6 Luna Max specification then quality reviews cleared the final implementation; their suggested claim/link owner regressions are now included and quality re-review cleared them. ROOT alone ran Go and read complete output. Complete affected native race exited 0: purchases 591.392s and reporting 21.271s (`/tmp/tammy-bas-legacy-decisions-complete-native-race-final-2026-10-16.log`). Its sole exclusion was the previously accepted expensive public-posting stress fixture named above; no decision, scalar or nomination test was excluded. Final-source existing-route TLS exited 0 in 57.464s (`/tmp/tammy-bas-legacy-decisions-real-tls-final-2026-10-16.log`); these preserve existing invoice/BAS and purchase commit/audit-rollback routes, not a new endpoint or combined producer. All default Core packages exited 0 (`/tmp/tammy-bas-legacy-decisions-default-core-final-2026-10-16.log`). The pinned SQLCipher 4.15.0 production build exited 0 (`/tmp/tammy-bas-legacy-decisions-production-build-final-2026-10-16.log`); ROOT independently verified binary SHA-256 `0e6fdf8332584d62ebbfe1f127c83a60bb29c76ea27738c3c686243f995ec7b8`. Formatting and staged whitespace checks passed. This dependency checkpoint is accepted (2026-10-16).

This decision-body API is not a physical-key extension, currentness proof, provisional nomination or a completed global pre-body fence. Support/current-source/cash selectors, exact full-reader set reconciliation, combined byte/candidate arithmetic and the producer remain open. No overall Task 5 checkbox is complete.
