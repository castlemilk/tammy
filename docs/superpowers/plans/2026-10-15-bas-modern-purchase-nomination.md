# BAS modern purchase nomination implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. ROOT alone edits active Go, runs Go/gofmt/tests/builds and commits; agents draft `/tmp/*.pending` or review read-only. Reuse `issue/sbr-registration-ready` and never edit active Go while a Go/test/build process runs.

**Goal:** Read exact modern purchase structural candidates beside the complete accepted physical key set before financial bodies hydrate.

**Architecture:** One additive purchase-owned wrapper calls the accepted bounded key reader once and privately maps posting/payment children or disjoint zero-child fallbacks. It retains every physical family for later exact body/legacy reconciliation and chooses no eligibility or complete-purchase policy.

**Tech stack:** Go 1.26.4, native SQLCipher 4.15.0, schema 14.

Design: [modern purchase nomination](../specs/2026-10-15-bas-modern-purchase-nomination-design.md).

## Chunk 1: Native test-first implementation

Files to create: `services/core/internal/purchases/reporting_modern_nomination.go`, `reporting_modern_nomination_test.go`, `reporting_modern_nomination_sqlcipher_test.go`.

- [x] Independently review spec/plan; settle API/result and exact modern-only boundary before activation. GPT-6 Luna Max cleared the narrowed wrapper; keep a post-mapping context check and note evidence-only suppression applies to document roots, not opening payables.
- [x] Draft native exact writer/scalar/full-coverage identities and claim/conservation history checks. ROOT reads and activates tests before production code.
- [x] ROOT runs missing-API RED from `services/core`: `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -tags tammy_sqlcipher ./internal/purchases -run '^TestPurchaseBASModernNomination' -count=1 -timeout=15m`.
- [x] Add exact types, one guarded `ReadBASKeys` call and private structural mapping after the API-absence RED. Preserve all physical keys and distinguish payment own sequence from posting-allocation sequence. The first complete native run passed without a behavioral discrepancy.
- [x] Verify synthetic default fallback/order/linkage cases, native count-only boundaries, caller budgets, no body queries, complete physical history, cancellation and unchanged state/transaction ownership. ROOT formats and runs focused native race. Zero-child fallback is synthetic structural evidence, not a financially valid native source.

## Chunk 2: Review and acceptance

- [x] Independent GPT-6 Luna Max specification review clears final implementation before independent GPT-6 Luna Max quality review; both cleared with no remaining material finding.
- [x] ROOT reads complete affected purchase/reporting native race output, excluding only `TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration`; no scalar/nomination exclusions. Run existing real TLS invoice/BAS and purchase commit/audit rollback journeys, all default Core and pinned production build; verify binary SHA-256 independently.
- [x] Record precise evidence and open legacy/global boundaries in complete-source/lifecycle/programme docs. Check formatting/staged scope/whitespace and commit.

No existing reader, route, contract or migration changes. A modern-only local candidate fence is not the purchase slice of the exact global union. Every later global producer gate remains required.

## Verification checkpoint (accepted, 2026-10-15)

ROOT read the native missing-API RED output before production activation (`/tmp/tammy-bas-modern-purchase-nomination-api-native-red-final-2026-10-15.log`, exit 1). Independent GPT-6 Luna Max design and specification reviews cleared the modern-only boundary and final sources. Review corrected an undefined plain-struct accessor and a fixture assumption that the second payment would retain only one allocation; current writers retain both allocation rows and full readers require own/reference sequences to match. These were caught before activation, not production regression REDs.

The first focused native race exited 0 in 6.133s (`/tmp/tammy-bas-modern-purchase-nomination-focused-native-first-2026-10-15.log`), selecting all new default and native tests with no exclusions. Real cash/non-cash mixed-allocation sources match writer and complete coverage bodies. Repeated claims preserve the exact three modern source candidates while retaining two deferrals, three decision portions and one attribution; the accounting oracle preserves 250 cents unpaid, 250 cents suspense and 500 cents attributed. Legacy-only discovery retains document/review, manual journal and opening payable/conversion/item identities with a zero modern-candidate allowance. Count-only 1,000/1,001 rows prove the local hard cap; synthetic fallback/nonidentity-sequence examples claim structural mapping only. Short key/byte/candidate allowances and cancellation before/after key query return no partial result, perform no body reads and leave retained state and the caller's transaction unchanged.

Independent GPT-6 Luna Max quality review cleared the final implementation and tests with no material finding. Existing real TLS invoice/BAS and purchase commit/audit rollback journeys exited 0 in 55.788s (`/tmp/tammy-bas-modern-purchase-nomination-real-tls-final-2026-10-15.log`); these preserve existing routes, not a combined producer or new endpoint. Complete affected purchase/reporting native race exited 0 in 493.521s/20.994s (`/tmp/tammy-bas-modern-purchase-nomination-complete-native-race-final-2026-10-15.log`); ROOT read its full output. Its only exclusion was the previously accepted expensive `TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration` public-posting stress fixture. No scalar or nomination test was excluded.

All default Core packages exited 0 (`/tmp/tammy-bas-modern-purchase-nomination-default-core-final-2026-10-15.log`). The pinned SQLCipher 4.15.0 production build exited 0 (`/tmp/tammy-bas-modern-purchase-nomination-production-build-final-2026-10-15.log`); ROOT independently verified binary SHA-256 `f867094d90c6bfb07afefccb76c2892d4236669253f4b68d41aae0a0e7712c44`. Formatting and staged whitespace/scope checks passed. No existing reader, route, contract or migration changed. Exact legacy nomination, shared global fences and the combined producer remain open; no overall Task 5 checkbox is complete.
