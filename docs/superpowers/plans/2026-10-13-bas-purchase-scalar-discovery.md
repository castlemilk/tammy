# BAS purchase scalar discovery implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. ROOT alone runs Go, gofmt, tests, builds and commits; drafts and independent reviews may run in parallel without editing active Go files.

**Goal:** Supply bounded purchase physical identities before the combined BAS producer loads source bodies.

**Architecture:** Add module-owned aggregate summaries and guarded typed-key reads on the existing caller-owned `Repository` executor. Reuse the accepted accounting/invoice reservation pattern and purchase census semantics. Existing body readers retain responsibility for full proofs; this dependency does not implement the combined producer.

**Tech stack:** Go 1.26.4, native SQLCipher 4.15.0, existing schema 14; no migration or contract changes.

## Chunk 1: Public reservation and physical shapes

Create `services/core/internal/purchases/reporting_scalar_keys.go` for public types, summary/read coordination and scanning. Keep family SQL in focused `reporting_scalar_modern.go` and `reporting_scalar_legacy.go` files. Add native tests under `reporting_scalar_*_sqlcipher_test.go`.

`Repository.SummarizeBASKeys(ctx, organisationID)` returns `*BASScalarSummary`; `ReadBASKeys(ctx, organisationID, expected BASScalarSummary, maximumKeyCount, maximumScalarBytes)` returns `[]BASScalarKey`. Summary contains organisation, separate root/receipt counts, physical key count, scalar byte charge and fixed per-family counts. The value reservation is comparable and is not a content fingerprint.

The 15 disjoint physical families are posting, posting allocation, payment, payment allocation, evidence, accounting event, deferral, deferral portion, attribution, document, legacy review, opening payable, opening conversion, opening item and explicitly source-linked manual journal. Physical identity is the family's own `id`, except accounting events use `operation_key`, posting allocations use `(posting_id, sequence)`, payment allocations use `(payment_id, sequence)` and deferral portions use `(deferral_id, sequence)`. For composite families the key's `ID` holds that exact first primary-key column; `Sequence` holds the second. IDs are opaque SQL identities rather than validated domain references. Keys retain only applicable typed parent/reference IDs, source kind/ID, object/accounting/account/source versions, allocation sequence and independently nullable source/record hashes. Every key returns `IsRoot` and `IsReceipt` flags: promoted documents remain keys without roots, opening context never creates roots, and linked manual journals may be nonreceipts. Optional fields remain SQL NULL; a record proof hash never substitutes for absent source provenance. No amount, date, status, proto, document bytes or journal line is returned. Original/source hashes do not become a child row's own original provenance.

Each row charges 512 bytes plus the selected TEXT/BLOB bytes, including every selected reference/hash. Charge is a retained-cell recipe, not a Go heap estimate. Cap roots at 1,000 and combined receipts at 1,000 independently; cap physical keys at 16,384 and bytes at 8 MiB. Bounds retain count/byte error priority over storage errors. Caller budgets may narrow these limits; negative budgets fail.

- [x] Activate minimal native API-absence tests and record their actual RED before production code. ROOT observed actual exit 1 with only missing `SummarizeBASKeys`/`ReadBASKeys` methods in `/tmp/tammy-bas-purchase-scalar-api-native-red-2026-10-13.log`.
- [x] Add aggregate-only summaries and exact-budget typed reads, using fixed family counts and caller transaction ownership.
- [x] Test exact selected charges, independent hash/NULL presence and real writer-created modern histories before accepting the API.

## Chunk 2: Ownership, census and guarded SQL

Start every family at its actual retained rows and LEFT JOIN required owners/ancestors. Include target ownership through a row or any resolved required parent; missing and cross-owner links abort. Unattributable opening payable/item orphans fail closed. Validate every selected integer's storage class before accepting its fixed allowance, positive versions/sequences, required BLOB32 hash shape and optional source tuple presence. Raw conversion hashes remain distinct 64-character lowercase hex TEXT bytes. Inspect posting gross storage/zero-total nullable journal linkage only for integrity; no amount is projected.

Root membership preserves the existing disjoint census: owned postings, every owned document without its modern posting, and actual payables with owned POSTED/REPLACED conversions. Promoted documents and all immutable review versions remain physical history keys. Conversion/item/replacement context does not create a purchase root. Discover conversion replacement context in SQL without copying parent IDs into Go; validate missing/foreign edges and keep full replacement/reversal proof verification with the existing readers. Payable/item matching uses exact conversion and item IDs, never an incidental source mention.

The manual-journal family retains unique explicitly source-linked kind-0/non-opening journal identities, including nonreceipt history. Link through an exact owned document/payable ID directly or through an owned tax fact and its exact journal line. `IsReceipt` uses the existing POSTED/non-OPENING/non-REVERSAL predicate and an owned cash/cash-equivalent line whose credit exceeds debit. This is receipt discovery only; it supplies no GST direction. Separately reject missing/foreign line/account parents and non-INTEGER debit/credit/source-kind cells so the predicate cannot silently hide damage. Unresolved source-linked fact parents abort; accounting retains independent fact keys and full proofs.

Use one private scalar-row projection shared by summary and key read. Summary returns only aggregate family/root/receipt counts, charge and invalid count; no typed key or body read. Read rechecks its reservation, then uses stable family/ID/sequence order, reserved count plus one sentinel and cumulative-byte/row-number CASE guards before the driver sees selected cells. Invalid, overflow and sentinel rows expose finite markers only. Verify per-row bytes, complete family/root/receipt counts, ordering and final charge; cancellation/drift returns no partial output. Never write, change revisions or finish the caller transaction.

- [x] Record ownership/storage/hash/linkage behavioral REDs using actual encrypted rows; restore exact fixture guards before reads.
- [x] Implement modern and legacy/context projections without eligibility or latest-review selection.
- [x] Prove SQL count/cumulative-byte/invalid-cell guards prevent rejected cell allocation and expansive conversion errors after summary drift.

## Chunk 3: Native acceptance and review

Reuse actual modern writers via `claimSetup`, deferral and attribution commands; use `legacyRequest`/`ResolveLegacy`, real linked manual journals, `legacyOpeningFixture` and `coverageOpeningWithDocument` for legacy/opening history. Test zero allocations, source-only history, promoted documents, POSTED/REPLACED roots and context-only replacement conversions. Count-only cloned rows establish census limits rather than complete financial body validity; journal fixtures must follow the guarded DRAFT-to-POSTED transition.

- [x] Test independent combined root/receipt 1,000/1,001 boundaries, more than 1,000 child keys, key/byte exact and one-short budgets, wrong owners/orphans and nullable source provenance.
- [x] Assert root/receipt flags for promoted versus unposted documents, actual opening payables versus context, linked manual nonreceipts and multiple links to one receipt journal. OPENING, REVERSAL, non-POSTED and non-cash-credit journals must not gain a receipt flag.
- [x] Test cancellation before/after query, post-summary cell/count/byte drift, no body queries/writes, caller work/rollback and encrypted close/reopen.
- [x] Finish independent specification review before independent quality review; fix verified gaps and re-review.
- [x] Run focused native race, affected complete purchase/accounting/reporting native race, real TLS invoice/BAS/purchase regression, all default Core packages and pinned production build. Record actual exit statuses, complete outputs and independently checked binary hash.
- [x] Update the complete-source plan and programme status; commit only after those gates pass.

## Verification checkpoint (accepted, 2026-10-14)

The final focused scalar native race passed in 74.052s with no exclusions (`/tmp/tammy-bas-purchase-scalar-native-focused-final-race-2026-10-14.log`). Independent GPT-6 Luna Max specification review cleared the ownership/context corrections. The requested Luna Max quality agents then hit an account usage limit; a fresh independent reviewer using inherited model settings completed quality review and cleared the final source after the integer-kind fix below.

Affected purchase/accounting/reporting native race passed in 480.647s/49.816s/19.851s (`/tmp/tammy-bas-purchase-scalar-complete-native-race-final-2026-10-14.log`), excluding only the previously accepted expensive public-posting stress fixture named in the command below. No scalar test was excluded. Existing real TLS invoice/BAS save/replay and purchase commit/audit rollback journeys passed in 54.697s (`/tmp/tammy-bas-purchase-scalar-real-tls-final-2026-10-14.log`); they are existing-route regression evidence, not combined producer acceptance. All default Core packages passed (`/tmp/tammy-bas-purchase-scalar-default-core-final-2026-10-14.log`). The pinned production SQLCipher build passed (`/tmp/tammy-bas-purchase-scalar-production-build-final-2026-10-14.log`); ROOT independently verified binary SHA-256 `09a5cc58d249c5e384a545ff42c40bf7abb3e03e83e7a6bbe4459230f08c4618`. Formatting and staged whitespace checks passed. Every final command exited 0 and its full output was read. No contracts, accepted migrations or existing body readers changed.

Native behavioral REDs established rejected-cell ordering and an omitted corrupt deferral portion (5.216s, `/tmp/tammy-bas-purchase-scalar-native-behavior-first-2026-10-13.log`), then foreign manual/opening/fact links and missing DRAFT payable replacement context (2.647s, `/tmp/tammy-bas-purchase-scalar-legacy-scope-native-red-final-2026-10-13.log`). The four legacy scope regressions passed in 3.017s (`/tmp/tammy-bas-purchase-scalar-legacy-scope-native-green-2026-10-13.log`). Review also exposed unsupported integer manual journal kinds `1`/`-1` disappearing before validation. Both native cases failed in 1.692s (`/tmp/tammy-bas-purchase-scalar-invalid-integer-kind-native-red-2026-10-14.log`) and passed with the race detector in 2.905s after admitting those malformed rows into validation (`/tmp/tammy-bas-purchase-scalar-invalid-integer-kind-native-green-2026-10-14.log`). Valid modern kinds `6`/`7`/`8` remain outside the manual family.

Initial fixture failures were not behavioral evidence: event-version corruption targeted multiple rows and collided with the existing unique index; the mutation now targets version 1. The tax-fact-only fixture also created cash account code `1011` twice; its manual journal now uses the existing writer fixture with code `1012`, and the repaired focused native race passed in 2.475s (`/tmp/tammy-bas-purchase-scalar-legacy-fixture-native-green-2026-10-13.log`). The first broad race was interrupted when quality review found the integer-kind omission; it is not acceptance evidence. No production schema or writer guards changed.

ROOT commands run in the worktree's `services/core` unless stated otherwise:

```sh
rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -tags tammy_sqlcipher ./internal/purchases -run '^TestPurchaseBASScalar' -count=1 -timeout=20m
rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/purchases -run '^TestPurchaseBASScalar' -count=1 -timeout=20m
rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/purchases ./internal/accounting ./internal/reporting -skip '^TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration$' -count=1 -timeout=30m
rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./internal/localproduct -run '^(TestLedgerModuleCreatesOrganisationAndInstallsAustralianChartThroughRealServer|TestPurchaseCommandThroughRealTLSCommitsOnceAndRollsBackAuditFailure)$' -count=1 -timeout=15m
rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test ./... -count=1 -timeout=30m
```

Run `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- pnpm core:build` at the worktree root after tests stop. The existing expensive public-posting stress fixture may use its previously accepted exclusion only if documented explicitly; scalar tests have no exclusions.

This dependency leaves eligibility, root/receipt fallback, legacy-review applicability, attribution/deferral conservation and candidate units, exact supported journal-line mapping, complete duplicate-fact proof replacement, shared global fences, the combined producer and Task 5 acceptance open.
