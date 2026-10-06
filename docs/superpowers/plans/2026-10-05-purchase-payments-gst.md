# Purchase payments and GST credits implementation plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents are available) or superpowers:executing-plans to implement this plan. Steps use checkbox syntax for tracking. Follow @karpathy-guidelines, @superpowers:test-driven-development and @superpowers:verification-before-completion; assess actual persisted accounting and packaged product behavior.

**Goal:** Explicitly post a reviewed AUD purchase once, record actual dated payments, retain exact eligible GST and review decisions, and reconcile the ledger, saved GST workpapers and their PDF/CSV exports.

**Architecture:** Extend the existing small `internal/purchases` package beside its opening-payable ports, with immutable purchase sources and transaction-owned commands using the invoice accounting pattern. Purchase allocations, payments, evidence, attribution, deferral and legacy decisions provide one typed, complete reporting snapshot; `internal/reporting` captures that snapshot without UI page limits. Documents owns intake/review, purchases owns accounting and tax decisions, and the phase-one export implementation renders the exact retained workpaper.

**Tech stack:** Go, Connect/protobuf with Protovalidate, SQLCipher, TypeScript/React, Electron Conveyor, existing reporting PDF/CSV renderers, Vitest and packaged Playwright.

**Authority and dependency:** The user's 2026-10-05 instruction to continue [the reporting programme](../specs/2026-10-04-reporting-programme-design.md) authorises this plan and its execution. Implement [the purchase design](../specs/2026-10-04-purchase-payments-gst-design.md) in the current `issue/sbr-registration-ready` worktree. The older design's pending-approval wording does not require a new human approval stop. Planning may run alongside phase one; changes to shared contracts, BAS sources, storage or export renderers start only after [phase-one exports](2026-10-05-income-bas-exports.md) pass newly built packaged acceptance and are committed. Reserve migration **0012** for this phase and **0013** for the later BAS lifecycle. No BAS freeze/preparation authority or transport to the ATO is introduced in this phase.

All file paths and commands below are relative to the repository root. Run shell commands through `rtk`; examples use `rtk proxy mise exec -- ...` to retain the pinned toolchain and unfiltered failing-test output. Execute native/tagged/package checks serially; do not start duplicate builds. Record a failing assertion before implementation, then the passing result, for each behavior change. A compile error is acceptable only for the first contract/API existence check; financial RED evidence should fail on the expected accounting assertion.

## Chunk 1: Contracts, retained storage and exact rules

### Task 1: Define the focused purchase and reporting contracts

**Files:**

- Create `proto/tammy/v1/purchases.proto` and `services/core/internal/contracts/purchase_accounting_contract_test.go`.
- Modify `proto/tammy/v1/accounting.proto`, `proto/tammy/v1/documents.proto`, `proto/tammy/v1/tax.proto`, `test/e2e/coverage.yaml`.
- Generate `services/core/internal/gen/tammy/v1/purchases.pb.go`, `services/core/internal/gen/tammy/v1/tammyv1connect/purchases.connect.go`, `packages/connect-client/src/gen/tammy/v1/purchases_pb.ts` and the changed accounting/documents/tax generated files through `buf.gen.yaml`.
- Create `packages/connect-client/src/purchase-accounting-contract.test.ts` using the existing generated-contract test owner.

- [x] Write descriptor/round-trip validation tests for all eight RPCs: `GetPurchaseAccounting`, `PostReviewedPurchase`, `RecordPurchasePayment`, `ListPurchasePayments`, `RecordPurchaseTaxEvidence`, `AttributePurchaseGst`, `DeferPurchaseGstClaim`, `ResolveLegacyPurchaseReview`. Include unknown enums, missing reviewed decisions, malformed UUIDs/hashes/dates, explicit zero GST, positive AUD payment and bounded source lists.
- [x] Run `rtk proxy mise exec -- go test ./services/core/internal/contracts -run Purchase -count=1`; capture RED for missing purchase contracts.
- [x] Define the complete messages below. Preserve existing field numbers and optional semantics. Append public journal enum values `PURCHASE`, `PURCHASE_PAYMENT`, `PURCHASE_GST_ATTRIBUTION`; do not reuse `INVOICE_PAYMENT`. Add a dedicated typed `BasPurchaseSourceLine` and `BasPurchaseReview` collection to `BasWorkpaper`, plus a separately labelled legacy-candidate GST amount, retaining existing `sources` for legacy snapshots. New 1B contains only reconciled attributed credit; unresolved legacy amounts stay explicit provisional candidates, not silently omitted or represented as zero entitlement. The post request contains document identity/version and allocations, never an authoritative renderer-supplied document snapshot.

| Contract | Required retained facts |
| --- | --- |
| Purchase posting | ID, organisation/document IDs, reviewed version, original hash, deterministic reviewed snapshot/hash, supplier/invoice/date, GST basis/profile version/history watermark, gross/supplier GST/eligible GST, ordered allocations, duplicate/prior-accounting decision, journal/revision and actor/time. |
| Allocation | Stable sequence/account/source revision, gross, supplier GST, eligible GST, explicit business/private use, capital/non-capital/excluded BAS classification, operating/investing/financing cash-flow classification, eligibility/evidence decisions and noncreditable reason. |
| Accounting projection | Posted flag, accounting version, all-row paid/outstanding and GST eligible/deferred/suspense/attributed totals, payment count and typed review outcomes. Avoid embedding an unbounded payment history; list it separately. |
| Payment | ID, actual date/amount/bank/reference, journal, deterministic snapshot/hash and exact ordered gross/supplier-GST/eligible-GST allocation deltas. |
| Evidence | Retained document/source ID/version/hash, reviewed classification (including unknown and supported exception), held date, registration/business/evidence confirmation, reason, actor and recorded time. |
| Attribution | Selected immutable source/allocation references, explicit claim period, actual normal event dates, release posting date, evidence, reviewed as-of date, timing/deadline facts including earliest required-return due date and its retained evidence references for delayed claims, amount computed by core, linked deferral when present, source/hash and journal. |
| Deferral | Identified eligible unclaimed suspense amounts, affected period, complete evidence/timing decision, reason, earliest eligible period/actual required-return due date plus retained due-date evidence/ordinary last claim date, source/hash. Outstanding status is derived from later links. |
| Legacy review | Typed document **or opening-payable** source identity/version/hash, period/balance-date scope, explicit disposition, supporting payment/claim/evidence links and reason. An opening source need not have a document. |
| BAS purchase source/review | Posting/document/payment/attribution identity and hashes, allocation sequence/use/BAS classification, invoice date, optional actual payment date, normal tax event, selected claim period, gross/supplier/eligible/claimed GST, evidence, source revision and unresolved/excluded/unsupported/deferral decisions. |

- [x] Add status/accounting filters to `ListDocumentsRequest` without breaking old requests. Keep `PageRequest`/`PageInfo`; add a bounded `PurchaseService.ListPurchasePayments` page. Set bounded item/response sizes consistently with 1,000 BAS source capacity and 16 MiB export limits; never encode amounts as floating point.
- [x] Add declared-future coverage rows while contracts are not wired; specify planned read/write role admission, principal failures, persistent-command exact replay and changed-request conflict. Run `rtk proxy mise exec -- pnpm proto:format`, `pnpm proto:lint`, `pnpm proto:breaking`, `pnpm proto:generate`, the focused Go test and `pnpm --filter @tammy/connect-client typecheck`. Commit only the coherent contract files/generated outputs/coverage changes, then run `rtk proxy mise exec -- pnpm contracts`.

**Contract-gate constraint:** `scripts/check-generated-tree.mjs` deliberately rejects any dirty generated output, even an intended change. Do not disable it or call a dirty-tree failure a passed gate. Commit the reviewed generated contract delta and rerun the full gate; later generation must produce no new diff. `scripts/check-e2e-coverage.mjs` also hardcodes the module catalogue, handled in Task 13.

**Retained source shape:** Save each future claim's individual portion in `BasPurchaseFutureClaim`, with its deferral identity/hash and complete timing/evidence facts. `BasPurchaseLegacyReview` retains the decision's scalar and period provenance; its ordered payment/evidence references live in `BasWorkpaper.legacy_purchase_support`, globally bounded at 1,000. Do not embed a 200-portion command or 400 supporting references in every one of 1,000 review rows. Task-one tests reproduce the former 50,642,489-byte shape and verify the linear replacement. Item limits still require actual encoded-response and rendered-export byte guards.

**Task 1 evidence (2026-10-06):** Contract commit `62363327` passed independent specification and code-quality reviews, fresh full Go contract tests, all 43 Connect client tests, client typecheck, protobuf format/lint/breaking checks and documentation checks. The full `rtk proxy mise exec -- pnpm contracts` gate exited 0 after the reviewed commit, with a clean generated tree. RED→GREEN and size evidence is retained in `/tmp/tammy-purchase-contracts-task1-20261006`; `full-contracts.log` records the post-commit gate. These contracts remain declared future until the service, desktop bridge and packaged purchase journey are implemented.

### Task 2: Add migration 0012 without creating transactions

**Files:**

- Create `services/core/internal/storage/migrations/0012_purchase_payments_gst.sql` and `0012_purchase_payments_gst_test.go`.
- Modify `services/core/internal/storage/migrations/embed.go` and existing migration catalogue/count tests; append 0012 without changing prior migration bytes. Advance `services/core/internal/app/local_composition_sqlcipher.go`'s production migration target to 12 and retain its latest-embedded-target regression.
- Modify `services/core/internal/localproduct/storage_upgrade_sqlcipher_integration_test.go` and affected SQLCipher/workspace/reporting tests only where latest-version expectations require 12. Historical prefix/version fixtures remain historical.

- [x] Add migration-prefix tests with a version-11 workspace containing reviewed/undated/duplicate documents, supplier opening payables (one without document), an issued invoice/payment and saved non-cash BAS. Save byte/value hashes of existing source rows, journal totals and workpaper payloads. Assert successful migration adds no journal, payable, credit or business source and does not rewrite saved sources.
- [x] Run `rtk proxy mise exec -- go test ./services/core/internal/storage/migrations -run Purchase -count=1`; capture RED for absent migration/table/constraints.
- [x] Create immutable tables from the contract: `purchase_postings_v1`, `purchase_posting_allocations_v1`, `purchase_payments_v1`, `purchase_payment_allocations_v1`, `purchase_tax_evidence_v1`, `purchase_gst_attributions_v1`, `purchase_gst_deferrals_v1`, `purchase_legacy_reviews_v1`. Add queryable deferral portions, `purchase_accounting_events_v1` for contiguous immutable accounting versions (one event per accepted command), `purchase_command_audit_v1` for durable command evidence, and dedicated saved BAS purchase-source/review/support tables for complete frozen source bytes/hashes. An immutable `bas_workpaper_purchase_metadata_v1` record retains the new format and legacy candidate AUD total even when its source collections are empty; historical workpapers remain without that record. Derive totals and current states from retained rows; no mutable balance cache becomes authoritative.
- [x] Use composite organisation/purchase/document/source keys, amount/currency/version/date/hash checks, source-allocation identity and unique command keys. Posting has one unique document; payment/attribution journals are unique. Evidence/legacy ownership is verified through retained source tables, including `opening_conversions.organisation_id`. Enforce no-update/no-delete triggers on new immutable tables. Cumulative paid/claimed limits remain transaction checks, supplemented by SQL uniqueness for individual claims/events. Index organisation/event date, purchase/sequence, source identity, period and future-claim discovery.
- [x] Add an immutable `journals.source_kind` column defaulting to legacy `0`, storing new public enum identities separately from SQL `source_type`. This avoids decoding every `BANKING` journal as an invoice receipt. Add compatibility/check and no-change triggers; legacy journals keep their existing interpretation. Do not modify old journal data or alter immutable journal values as migration backfill.
- [x] Run default migration tests, then `rtk proxy mise exec -- pnpm sqlcipher:build` once and `rtk proxy mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/localproduct ./services/core/internal/storage/sqlcipher -run 'Purchase|Upgrade' -count=1 -timeout=5m`. Test automatic upgrade only after successful unlock; locked/incorrect-passphrase startup leaves database bytes untouched. Replace the existing fixture that forges v10 by dropping only 0011 artifacts with a genuine encrypted historical-prefix fixture, then upgrade to 12 while retaining its previous assertions. Commit the migration and tests.

**Task 2 evidence (2026-10-06):** Storage commit `06f51261` passed independent specification and code-quality reviews, all default Go tests, the complete affected native SQLCipher/workspace/reporting/app/localproduct suites, and the explicit real TLS historical-prefix upgrade test. Historical migrations 0001–0011 remain byte-identical to `62363327`; migration 0013 remains reserved. Native assertions cover immutable ownership and replacement guards, exact typed bounds, empty saved-workpaper metadata, failed-DDL rollback, no business backfill, and locked/wrong-passphrase byte preservation. Final commands, hashes and limits are recorded in `/tmp/task2-final-delivery.md`; `/tmp/task2-final-full-affected-native.log` and `/tmp/task2-final-ledger-tls.log` record the final native runs, and `/tmp/task2-root-all-go-reviewed.log` records the root default Go gate. The generic `Purchase|Upgrade` selection runs no localproduct tests, so the ledger TLS scenario was explicitly executed. Purchase commands, UI and packaged acceptance remain later tasks.

### Task 3: Implement the allocation and eligibility domain

**Files:**

- Create `services/core/internal/purchases/domain.go`, `domain_test.go`, `allocation.go`, `allocation_test.go`, `evidence.go`, `evidence_test.go`.
- Read/reuse `services/core/internal/invoices/payment_allocation.go`, `accounting/accounts.go`, `accounting/cashflow.go`, `accounting/templates/au_small_business_v1.json`.
- Keep `purchases/opening_repository.go` and `opening_port.go` intact as existing opening owners.

- [ ] Write table/property tests for exact document arithmetic, zero purchase, ordered allocations covering gross and supplier GST, claimable GST bounded by supplier GST, explicit private zero credit, noncreditable reason, missing eligibility, and inappropriate/missing classifications. Test ordinary expense/asset business accounts and appropriate equity/drawing/receivable private accounts; reject protected/cash/archived/foreign-organisation/wrong-type accounts.
- [ ] Add allocation tests covering one-cent payments, repeated tiny payments, mixed gross weights/GST rates/private/capital categories, alternative payment partitions, full settlement and maximum accepted money values. Assert every gross delta sums to payment; every line is monotone and bounded; full settlement releases exact eligible GST; partitioned and single cumulative end states match. Run `rtk proxy mise exec -- go test ./services/core/internal/purchases -run 'Allocation|Eligibility|ReviewedPurchase' -count=1` and capture RED.
- [ ] Implement checked integer validation and the existing exact divisor method with arbitrary-precision product comparison. Keep the purchase helper local unless extraction into a shared internal money package makes both domains smaller and preserves invoice regression tests. Do not implement independent per-payment rounding or per-cent loops proportional to large money values.

Supplier GST and eligible GST each use their own cumulative proportion of allocated gross, followed by after-minus-before deltas. Their cumulative values satisfy eligible ≤ supplier ≤ gross. Individual eligible and supplier deltas can cross different cent-rounding boundaries, so do not impose eligible-delta ≤ supplier-delta or nest the eligible floor through the already rounded supplier GST. Add explicit `gross/supplier/eligible = 5/3/2` crossover and `55/5/3`, cumulative gross paid 37 → eligible 2 tests. This retains the existing invoice paid-extent model and avoids an extra artificial rounding delay.

```text
beforeGross, beforeEligible = cumulative(immutableAllocations, paidBefore)
afterGross,  afterEligible  = cumulative(immutableAllocations, paidBefore + amount)
deltaGross[i]    = afterGross[i]    - beforeGross[i]
deltaEligible[i] = afterEligible[i] - beforeEligible[i]
sum(deltaGross) = amount; every delta >= 0
paidAfter = purchaseGross => afterEligible[i] = allocationEligibleGST[i]
```

- [ ] Treat missing/late/future evidence separately from eligibility. A supported positive entitlement can be retained in deferred/suspense; unknown registration/business eligibility cannot become a positive approved entitlement. Supported tax-invoice exceptions require typed reviewed facts and a retained rule reference; OCR or amount alone grants none.
- [ ] Run the focused tests and `rtk proxy mise exec -- go test ./services/core/internal/invoices -run Allocation -count=1`. Commit the domain helpers and meaningful conservation tests.

### Task 4: Make owned purchase journals unambiguous and protected

**Files:**

- Modify `services/core/internal/accounting/journal.go`, `journal_repository.go`, `posting_policy.go` only for required admission/mapping, and their existing tests.
- Create `services/core/internal/accounting/purchase_journal_sqlcipher_test.go`.
- Create `services/core/internal/purchases/journals.go`, `journals_test.go`, `revisions.go`, `revisions_test.go`.

- [ ] Add failing tests for `PAYABLE` purchase and `BANKING` purchase-payment admission, `TAX` suspense-release admission, correct distinct public source decoding/listing, and rejection of generic reversal for all purchase-owned journals. Retain old invoice `BANKING` decoding and invoice reversal guards. Cover rollback of journal/source-kind/cash-flow insertion.
- [ ] Run `rtk proxy mise exec -- go test ./services/core/internal/accounting ./services/core/internal/purchases -run 'PurchaseJournal|PurchaseRevision' -count=1`; capture RED. Tagged storage assertions run with the same focused regex after the single native build.
- [ ] Extend `JournalRepository.Post` admission for `PAYABLE` and owned `TAX`, retain `BANKING`, and validate SQL/public-kind pairs. Insert `source_kind` for new journals; retain fallback decoding of old rows. Replace upper-bound enum assumptions with explicit supported values. Include source kind when filtering/listing and block generic reversal before reserving revisions. Reuse typed protected accounts `2000`, `1150`, `1160`, `1170`; never select a control solely from a renderer account ID.
- [ ] Build purchase posting, payment and release journals with exact debits/credits. Every non-bank line gets `NONCASH`; the bank-credit line gets negative operating/investing/financing components from retained gross deltas, whose sum is exactly `-payment`. Store actual cash-flow facts, not just projection labels. Use `PeriodRepository.IsPostingDateOpen` for invoice/payment/release dates, including zero-journal posting checks.
- [ ] Reserve one financial revision per accepted purchase command, bump ledger only when a journal is posted, settlement for payment, tax-source for eligibility/evidence/claim/deferral/legacy decisions. Store the relevant revision vector on the immutable command event. Do not call the existing ledger-only `ReserveFinancialRevision` and then reserve again; source, journal and audit must share one reservation. Run focused tests plus existing accounting/invoice posting tests and commit.

## Chunk 2: Purchase posting, payment and tax decisions

### Task 5: Persist one explicit reviewed purchase

**Files:**

- Create `services/core/internal/purchases/repository.go`, `posting.go`, `posting_sqlcipher_test.go`, `duplicates.go`, `duplicates_test.go`.
- Modify `services/core/internal/purchases/opening_repository.go` only to add a read projection needed for source/provenance checks; retain the existing `RecordOpeningPayable` port.
- Read `services/core/internal/invoices/accounting_repository.go` for `CheckGSTBasisHistory`, retained basis/profile validation and source hash verification.

- [ ] Add direct transaction tests for `$110/$10` cash posting (`expense=10000`, `1160=1000`, `2000=-11000`) and non-cash ready-evidence posting (`1150=1000`), plus unresolved-evidence non-cash (`1170=1000`). Mixed allocations retain noncreditable GST in cost and private account debits. Zero-value posting retains a snapshot without a zero journal.
- [ ] Add rejection tests for unreviewed/stale/foreign document, missing/invalid invoice date, inconsistent subtotal/GST/total, invalid allocation accounts, basis change/incomplete retained history, closed date, second posting, prior accounting/opening linkage, same source and semantic duplicate. Different-file duplicates must fail; explicit distinct-transaction confirmation retains reason and matched IDs/hashes. Run `rtk proxy mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/purchases -run 'PostReviewed|Duplicate' -count=1 -timeout=3m` and capture RED.
- [ ] Load `documents.Repository.Get` within the caller-owned transaction, verify organisation/status/version, marshal the reviewed source deterministically and snapshot original/reviewed hashes. Resolve profile and retained GST-basis history server-side. Compare explicit prior-accounting decision against discovered journal/opening provenance. Posting a reviewed extraction remains a separate action.
- [ ] Normalize the retained supplier identity when available; otherwise Unicode-aware normalized supplier name plus invoice number identifies duplicate candidates. Missing identity/number becomes explicit review rather than invented uniqueness. Query all retained purchase and opening references, excluding evidence-only links from new transaction candidates. A confirmed actual duplicate remains blocked; only a documented false match/distinct transaction can proceed.
- [ ] Insert posting/allocations/initial evidence and retain normal automatic-claim provenance, journal/cash-flow facts/accounting-version event under one transaction. The ordinary on-time supported posting/payment path does **not** require a separate user-entered return due date: preserve the simple `$110` posting/two-payment path. Actual earliest required-return due-date evidence is required when resolving delayed/suspense attribution or an evidence-complete deferral in Tasks 7–8; those paths cannot guess deadlines. Verify source hashes on every subsequent read. Run the focused suite and invoice basis-history regression; commit.

**Normal claim ownership:** Ordinary claims are derived from the immutable owned posting/payment allocation, evidence visible at its retained accounting version and recorded time, and its actual `1150` journal debit. They need no separate `TAX` release journal or delayed-attribution row; saved BAS sources may have no attribution ID. Preserve that historical evidence selection and hash. Later evidence append never converts an originally suspended source into an automatic claim. Explicit delayed releases in `purchase_gst_attributions_v1` consume only the remaining owned suspense extent; normal and explicit claims participate in the same no-double-claim calculation. This storage choice preserves the required accounting behavior without manufacturing due-date facts for the ordinary path.

### Task 6: Record actual payments with all-row balances

**Files:**

- Create `services/core/internal/purchases/payments.go`, `payments_sqlcipher_test.go`, `pagination.go`, `pagination_sqlcipher_test.go`.

- [ ] Write failing tests for two `$55` cash payments in different periods; retained delta GST is `500` each, expense stays `10000`, payable/deferred end at zero, bank is `-11000`, current input GST is `1000`. The same non-cash payments settle only the payable and create no additional credit. Include mixed-category negative cash-flow projection/fact totals.
- [ ] Add rejection/property tests: payment before invoice (unsupported advance), no posting, zero/negative/foreign amount, overpayment, archived/noncash/protected/foreign bank, stale accounting version, closed date, zero purchase, 201+ retained payments, large values and concurrent final payments. Run `rtk proxy mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/purchases -run 'PurchasePayment|PaymentPage' -count=1 -timeout=3m` and capture RED.
- [ ] Recompute paid amount/version and cumulative ordered deltas from **all** immutable rows in the transaction. Reject amount greater than authoritative outstanding before inserting anything. Retain actual payment date/hash and delta classifications. For cash, release newly paid eligible GST `1160→1150` only when evidence was held by the event and supported; otherwise `1160→1170`. For non-cash, no settlement-based GST attribution exists.
- [ ] Implement stable payment history keysets (`created_at,id`) with 1..200 page limit, retained anchors, a bounded page byte budget and no omission on timestamp ties. Aggregate accounting totals never reuse this page. Signed transport cursors bind organisation, document, actor, limit and filters through `internal/paging`, mirroring `localproduct/invoices_sqlcipher.go`; foreign/malformed/tampered/filter-changed cursors fail.
- [ ] Verify true SQL balances/cash-flow facts and journal ownership after each event, then repeat projection checks after close/reopen. Run invoice payment/allocation regression and commit.

### Task 7: Retain evidence and ordinary claim deadlines

**Files:**

- Create `services/core/internal/purchases/evidence_repository.go`, `claim_timing.go`, `claim_timing_test.go`, `evidence_sqlcipher_test.go`.
- Create `test/fixtures/purchases/claim-timing.json`; extend `docs/development/bas-reporting-rules.md` with purchase-specific rule pins/golden references only when needed.

- [ ] Reuse [the verified programme rule pins](../../development/bas-reporting-rules.md), then pin the exact supported tax-invoice/exception and ordinary claim-limit golden cases, recording URL/version/access date and narrowly supported cases. Search verified primary sources if a page returns 403; do not substitute blogs or guess a threshold. The [ATO GST credit time-limit guidance](https://www.ato.gov.au/api/public/content/0-a7296cdd-cec8-49dc-b597-1d94ac991a01) ties the ordinary limit to the earliest otherwise eligible statement's required due date, setting aside holding a tax invoice; the [ATO period-of-review example](https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/guide-to-self-assessment-for-indirect-taxes/period-of-review) distinguishes that limit from the separate amendment-review period. For delayed/suspense attribution and deferral, retain evidence of the **actual required due date** for each otherwise eligible payment portion; nominal monthly/quarterly guidance and financial-year settings do not establish that date or a determinable deadline. Do not add due-date entry to ordinary on-time posting/payment. Treat unsupported/indeterminable delayed histories as typed review, preserving facts.
- [ ] Write RED golden tests for on-time, late, future-held and missing evidence; same invoice/payment date with different held dates; earliest required-return due date distinct from event/evidence date; deadline final day/next day; leap-day behavior per pinned rule; unknown due-date provenance, disputed exception and expired claims. Add a retained-evidence ownership/version/hash mismatch test. Run `rtk proxy mise exec -- go test ./services/core/internal/purchases -run 'ClaimTiming|Evidence' -count=1`.
- [ ] Implement a pure timing result carrying `NORMAL_SUPPORTED`, `TIMING_REVIEW_REQUIRED`, `MISSING_EVIDENCE`, `FUTURE_EVIDENCE`, `UNDETERMINABLE_LIMIT`, `EXPIRED`, or `UNSUPPORTED_EXCEPTION`, plus linked facts. Preserve invoice date, actual payment date, held date, claim period, release date and reviewed as-of date separately. Do not implement `max(payment,held)` as tax date. Determine the ordinary last claim date only from retained earliest eligible period/required-return due date and a pinned rule; no date can be guessed from receipt upload time.
- [ ] `RecordPurchaseTaxEvidence` appends an immutable source-linked event and advances accounting/tax-source versions even without a journal. It never changes amounts/use allocations, claims GST automatically, moves a historical payment, or edits a saved workpaper. A future-held date remains visible but cannot support readiness or claim attribution.
- [ ] Run pure tests, `rtk proxy mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/purchases -run Evidence -count=1 -timeout=3m`, and verify existing saved drafts become stale on an evidence-only change once Task 10 is integrated. Commit facts, rule pins and tests.

### Task 8: Attribute suspense once and retain deferrals

**Files:**

- Create `services/core/internal/purchases/attributions.go`, `deferrals.go`, `claim_sqlcipher_test.go`, `deferral_sqlcipher_test.go`.

- [ ] Add RED scenarios for missing/late cash evidence: payment preserves its date, eligible paid GST moves to `1170`, evidence append does not increase 1B, explicit allowed open-period attribution posts `Dr1150/Cr1170` once and retains the selected source/payment/evidence. Test late non-cash evidence analogously with the invoice source.
- [ ] Reject selected periods ending before any actual payment/invoice event; release dates outside selected period or before a selected event; future-held evidence beyond reviewed as-of; stale/mismatched source/version/hash; already claimed sources; above paid/eligible/unclaimed extent; closed period; expired/unknown deadline; unsupported/disputed timing exceptions. Run `rtk proxy mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/purchases -run 'AttributePurchase|Deferral' -count=1 -timeout=3m` and capture RED.
- [ ] Compute attributable balances from immutable eligible source portions minus every retained attribution. Add automatic normal claims and explicit delayed claims to the same uniquely owned source ledger, avoiding alternative paths that claim twice. Validate reviewed claim period and evidence/deadline facts, reserve one event/revision, persist selected exact source portions/attribution/hash/release journal. The caller selects sources and allowable period; core calculates amounts.
- [ ] Implement period-specific `DeferPurchaseGstClaim`: only evidence-complete/timing-supported, eligible **unclaimed suspense** portions can be deferred. Require reason and determinable deadline. Reject already attributed/automatically claimed amounts and unresolved eligibility/evidence/duplicate/timing facts. Leave `1170` untouched; retain a future-claim obligation. Derive outstanding/attributed state from links; append attribution-to-deferral links without updating or deleting deferrals.
- [ ] Test deferral resolves only the named period's claim decision, remains in later review lists, survives restart, expires visibly, and closes through one attribution. Concurrent attribution/deferral and retry cannot double claim or erase decisions. A new evidence/deferral decision advances version/fingerprint even if cents are unchanged. Run focused tests and commit.

### Task 9: Resolve legacy sources without reposting existing accounting

**Files:**

- Create `services/core/internal/purchases/legacy.go`, `legacy_repository.go`, `legacy_sqlcipher_test.go`, `opening_read.go`, `opening_read_test.go`.
- Use existing `services/core/internal/purchases/opening_repository.go`/`opening_port.go` and `accounting/opening_conversion_service.go` provenance.

- [ ] Add RED fixtures for unlinked reviewed non-cash document, previously accounted expense with unknown credit history, previously claimed GST, semantic duplicate, noncreditable source, evidence-only document link, undated source, prior-quarter invoice paid now, and supplier opening source **without a document**. Distinguish known current-period payment without supported settlement as `UNSUPPORTED_OWNED_SETTLEMENT`; no new payment rows never proves unpaid.
- [ ] Reject opening/document wrong organisation, stale version/hash/provenance, invalid owned evidence link, missing claimed period/evidence, blanket unpaid without a balance date, a zero/unsupported decision that pretends to waive missing history, and any attempt to post an already accounted/opening source again. Run `rtk proxy mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/purchases -run Legacy -count=1 -timeout=3m` and capture RED.
- [ ] Add module-owned read projections for retained opening inputs joined to conversion/accounting/source provenance. Compute version/hash from deterministic retained source bytes and ownership, retaining conversion/source IDs even without a document. This slice does not implement opening settlement, supplier register or generic settlement allocation.
- [ ] Persist typed reviewed outcomes: `CONFIRMED_UNPAID_THROUGH_DATE`, `ALREADY_CLAIMED`, `DUPLICATE`, `NONCREDITABLE`, `EVIDENCE_ONLY_LINKED`, or `UNSUPPORTED_EXISTING_SETTLEMENT`; require applicable supporting payment/claim-period/evidence links and reason. Exclude a source outside the selected period only when its retained facts and decision scope support that conclusion, rather than adding a blanket outside-period waiver. Unknown or unsupported facts remain unresolved; merely having an expense journal does not mean its GST was claimed. Posting is permitted only when independent provenance review proves a new accounting transaction is valid.
- [ ] Test the complete source list (not current page) discovers old and undated candidates. Evidence-only linked documents enrich their owned purchase and never create another credit candidate. Retain decisions through restart; stale/new contradictory facts become visible rather than overwritten. Commit.

## Chunk 3: Full BAS capture, secure commands and reachable UI

### Task 10: Capture and save complete purchase provenance in BAS

**Files:**

- Create `services/core/internal/purchases/reporting_port.go`, `reporting_sources.go`, `reporting_sources_sqlcipher_test.go`.
- Modify `services/core/internal/reporting/bas_sources.go`, `bas_repository.go`; create `purchase_bas_sqlcipher_test.go` and `purchase_fingerprint_sqlcipher_test.go`.
- Modify `services/core/internal/reportingcapability/registry.go`, `registry_test.go`, `invoice_income_test.go` only for truthful GST-workpaper limitations.

- [ ] Write RED snapshot tests for cash `$5/$5` across periods (older invoice), non-cash `$10` once despite two payments, mixed/private/capital allocation trace, suspense excluded from 1B, delayed selected-period credit, legacy candidates/decisions, opening provenance and outstanding deferral discovery. Assert legacy direct credit is never also counted for a posted purchase. Compare every retained source row and the control-account effect, not only summary text.
- [ ] Add stale detection tests for a new payment, evidence-only event, attribution, deferral, legacy decision and newly unresolved/excluded candidate with **unchanged eligible totals**. Saving/exporting alone does not change source fingerprint. Old saved workpaper figures/source bytes remain identical after later mutations and migration; reads may add source-change flags without changing retained values. Run `rtk proxy mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/reporting ./services/core/internal/purchases -run 'PurchaseBAS|PurchaseFingerprint' -count=1 -timeout=3m` and capture RED.
- [ ] Implement one transaction-owned module reporting port returning immutable eligible source lines **and** complete candidate/review coverage. Cash selects actual payment allocations or explicit attribution by claim period, including invoice dates before the period. Non-cash selects eligible posted source/attribution once; settlement contributes no second credit. Keep legacy unposted document candidate amounts explicitly labelled provisional/unreconciled in a separate candidate total and review table, outside new reconciled 1B, with typed unresolved review; legacy resolutions exclude only their supported scope. This does not discard entitlement: the source/amount/reason remains visible and unresolved for phase-three completeness. Saved snapshots made before migration retain their original direct-credit values.
- [ ] Deterministically fingerprint every relevant source identity/version/hash/classification/use/evidence/exclusion/unresolved/unsupported decision, deferral, legacy/opening link and retained profile/basis/rule metadata. Do not rely on global financial revision or only eligible cents. Validate source hashes while loading; failed hash/coverage collection yields a visible error, never a partial draft.
- [ ] Save typed source/review/support snapshots in the dedicated immutable BAS tables alongside legacy/sales rows. Reload exact complete snapshot data for exports. Keep source bounds explicit: 1,001 sources/reviews/support links beyond supported capacity returns an unsupported-volume/error result with no partial workpaper; do not suggest changing the actual statement period to evade completeness. Before saving, enforce the actual 4 MiB encoded response budget, including response-envelope overhead; retain the same bounded admission when reading saved snapshots. Reuse the Task-one maximal Unicode fixture (13,060,275 protobuf bytes) to prove an item-valid oversized snapshot fails visibly without a partial save or truncated response. Do not truncate warning/review collections to fit a small legacy string limit; typed detail carries full decisions, with a bounded summary.
- [ ] Test 201+ documents, 201+ payments, undated/older legacy rows, source hash corruption and both exact-limit/overflow boundaries. Update GST-workpaper capability text to purchase-payment/reconciled/legacy limitations while complete BAS remains unavailable until phase three. Run existing invoice/reporting/save-order regressions and commit.

### Task 11: Add document keyset pagination and review filters

**Files:**

- Create `services/core/internal/documents/pagination.go`, `pagination_sqlcipher_test.go`.
- Modify `services/core/internal/documents/repository.go`, `services/core/internal/localproduct/ledger_sqlcipher.go` document handler/composition.
- Add focused `services/core/internal/localproduct/document_pagination_sqlcipher_integration_test.go`.

- [ ] Write RED tests for 201+ documents, timestamp ties, first/middle/final empty page, a newly ingested row between page reads, byte-budget boundaries with large extracted text, reviewed/unreviewed and accounting/review filters. No existing anchor is skipped/duplicated; new rows remain discoverable on refresh.
- [ ] Reject forged/tampered/foreign-organisation/foreign-actor/cursor-filter-changed/invalid-limit cursors and mismatched retained anchors. Run `rtk proxy mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/documents ./services/core/internal/localproduct -run DocumentPage -count=1 -timeout=3m` and capture RED.
- [ ] Implement stable `(created_at DESC,id DESC)` keysets using limit+1 and byte budget within the actual 4 MiB IPC response limit, with retained anchor verification. Keep old `List` callers working or replace them deliberately after locating all call sites. The cursor codec binds organisation/session principal/filter/limit and a versioned snapshot name through existing `internal/paging`; do not expose the unsigned repository position as transport cursor.
- [ ] Populate real `PageInfo.next_cursor` in `documentHandler.ListDocuments` instead of rejecting cursors. Scope accounting filters using purchase-owned indexes, never a client supplied status hint. Keep aggregate/reporting queries independent of pages.
- [ ] Run native repository and real TLS page tests plus default document/ledger tests; commit.

### Task 12: Wire transactional PurchaseService, durable audit and retries

**Files:**

- Create `services/core/internal/purchases/service.go`, `service_test.go`, `command_port.go`.
- Modify `services/core/internal/app/command.go`, `command_test.go` and relevant `command_sqlcipher_integration_test.go` coverage.
- Create `services/core/internal/localproduct/purchases_sqlcipher.go`, `purchase_commands_sqlcipher_integration_test.go`, `purchase_queries_sqlcipher_integration_test.go`.
- Modify `services/core/internal/localproduct/ledger_sqlcipher.go` route registration/activation only; use `invoices_sqlcipher.go` as adapter pattern.

- [ ] Write RED real TLS/SQLCipher tests for all eight RPCs: active-session reads; administrator/preparer commands; lodger/auditor read-only admission; unauthenticated/forbidden/foreign-source rejection; malformed request error mapping; source/version/account/evidence ownership checked inside the transaction.
- [ ] Add exact replay/lost-response retry/changed payload under same key/new-key duplicate/concurrent expected-version tests. Inject failure after source/journal/revision/audit stages and assert rollback leaves no source, journal, cash-flow, revision claim, command result or retained audit effect. Count one effect for exact replay, including after restart. Run `rtk proxy mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/localproduct ./services/core/internal/app -run 'PurchaseCommand|PurchaseQuery' -count=1 -timeout=5m` and capture RED.
- [ ] Extend the closed `OrdinaryOperation` registry for the six mutation RPCs; use administrator/preparer accounting/tax action admission consistent with invoice operations. Create transaction repositories for purchases/documents/profile/accounts/journals/periods/evidence/opening provenance using the same encrypted executor. Do not let a repository begin/commit its own separate transaction. Query reads likewise use one encrypted transaction with active local authentication and organisation scope.
- [ ] Coordinator flow: authorize actor → elect durable exact request key/hash → reload expected source/accounting version → validate → append owned facts/journal/revision event → build result → append bounded audit/evidence → commit once. Record `purchase_command_audit_v1` through the existing transaction auditor; ensure ordinary audit-chain events/result commitments cover every command without dumping original evidence bytes or private paths. Metadata/content hashes link retained evidence. Use bounded specific typed fault details for corrective UI guidance instead of revealing SQL/internal errors.
- [ ] Register the generated PurchaseService route in `LedgerModule` and activate/deactivate with workspace lifecycle. Persist/replay canonical response bytes and fingerprints exactly; never automatically issue a new idempotency key after transport uncertainty. Run real TLS tagged tests, existing invoice-accounting/basis-history and app-command tests; commit.

### Task 13: Extend the schema-bound desktop bridge and manifests

**Files:**

- Create `apps/desktop/src/conveyor/modules/purchases.ts`, `purchases.test.ts`, `apps/desktop/src/main/core-client-purchases.test.ts`.
- Modify `apps/desktop/src/main/core-client.ts`, `conveyor/router.ts`, `shared/conveyor-manifest.json`, `shared/conveyor-contract.ts`, `shared/conveyor-contract.test.ts`, `shared/conveyor-contract.typecheck.ts`, `conveyor/router.test.ts`, `conveyor/registration.test.ts`, `conveyor/modules/core-domains.test.ts` and affected CoreClient mocks.
- Modify `scripts/check-e2e-coverage.mjs`, `scripts/check-e2e-coverage.test.mjs` for the explicit ninth module; update packaged fixed-surface assertions in `apps/desktop/tests/e2e/foundation.spec.ts` and associated fixtures.
- Update `test/e2e/coverage.yaml`; create only relevant purchase transition fixture in `test/fixtures/purchases/transitions.pb.json` and add its path to `scripts/build-transition-index.mjs` if a transition enum is introduced. Generate `test/e2e/transitions.yaml` through the owner.

- [ ] Write RED codec/client/router tests for each query/command, byte limits, unknown fields/invalid enums, malformed core output, request cancellation/errors and named module exposure. Assert raw IPC channels/file/network capabilities remain absent. Run `rtk proxy mise exec -- pnpm --filter @tammy/desktop exec vitest run src/conveyor/modules/purchases.test.ts src/main/core-client-purchases.test.ts src/shared/conveyor-contract.test.ts`.
- [ ] Add `createClient(PurchaseService, transport)` and typed CoreClient methods; each uses existing `coreRequest`. Add one named `purchases` Conveyor module with schema-bound frames/codecs and explicit request/response limits. Commands only accept generated facts/IDs, never journal bytes, trusted snapshots or paths.
- [ ] Extend the exact module catalogue from eight to nine consistently in router, shared contract, coverage parser and all fixed-surface tests; add every method once with correct query/command kind. Do not hide purchases in banking or relax the module allowlist to arbitrary names. Preserve phase-one export context/writer wiring.
- [ ] Bind production coverage rows to the real case `purchases/payments-gst` after the packaged journey exists. Include role failures, invalid cursor, stale source/accounting version, basis history, closed period, duplicate, evidence/timing/deadline/unsupported settlement, replay and changed-request outcomes. Only describe implemented behavior as production coverage; separate future BAS freeze cases.
- [ ] Run focused tests, `rtk proxy mise exec -- pnpm desktop:typecheck`, `rtk proxy mise exec -- node --test scripts/check-e2e-coverage.test.mjs`, generation/transition checks and `pnpm contracts` against clean generated outputs. Commit bridge/manifests/client tests.

### Task 14: Build the Documents accounting and evidence controls

**Files:**

- Modify `apps/desktop/src/app/features/documents/documents-screen.tsx`, `documents-screen.test.tsx` and `apps/desktop/src/app/app.tsx` where API/state injection is required.
- Create `apps/desktop/src/app/features/documents/purchase-accounting-panel.tsx`, `purchase-accounting-panel.test.tsx`, `purchase-command-state.ts`, `purchase-command-state.test.ts`.
- Modify `apps/desktop/src/app/features/bas/bas-screen.tsx`, `bas-screen.test.tsx` for typed reconciled/legacy/suspense/deferral review rows.

- [ ] Write RED user-flow tests for extraction review then separate **Add purchase to accounts**, ordered account/use/classification/evidence allocation entry, authoritative confirmation, **Record payment** only after posting, retained payment history, **Review GST evidence**, supported explicit claim/defer/legacy decisions, and opening-source unsupported guidance.
- [ ] Include stale/basis-history/closed-period/advance/overpayment/duplicate/evidence failures with entered values retained, pending double-click prevention, exact-key lost-response retry, no new-key automatic retry, changed form request requiring clear retry resolution, lock/sign-out/context changes and keyboard/screen-reader controls. Run `rtk proxy mise exec -- pnpm --filter @tammy/desktop exec vitest run src/app/features/documents src/app/features/bas` and capture RED.
- [ ] Use restrained table-led detail below reviewed document facts. Separate amounts from reviewed eligibility. Display authoritative allocation, gross/supplier/eligible GST, paid/outstanding, deferred/suspense/attributed totals, evidence dates, future-claim deadlines and immutable history. Confirmation explains expense once/payable settlement/GST effect. Display **Needs GST evidence/claim-period review** and truthful unsupported states. No action implies bank transfer, reconciliation, BAS preparation or ATO lodgement.
- [ ] Add reachable page controls/filter reset and selected-document retention across refresh; a purchase after the first 200 documents and the 201st payment must remain accessible. Show BAS purchase source drill-down with separate invoice/payment/claim dates and legacy/current distinction; link unresolved rows to the purchase review panel. Preserve saved workpaper ID/totals while showing stale status until explicit regeneration.
- [ ] Run renderer tests/typecheck plus CoreClient/Conveyor tests and inspect real desktop detail tables for width, focus, error guidance and exact-cent formatting. Commit.

## Chunk 4: Exact exports, real acceptance and phase-three handoff

### Task 15: Extend phase-one saved-workpaper exports with typed purchase sources

**Files:**

- Modify `packages/reporting/src/reports.ts`, `packages/reporting/src/reports.test.ts`, `apps/desktop/src/main/report-export.ts`, `report-export.test.ts` and `apps/desktop/src/shared/report-export.ts` from phase one. Reuse `apps/desktop/src/main/validated-export.ts` without changing its security/file contract for this additive source extension; `invoice-export.ts` wraps that shared writer.
- Add `apps/desktop/tests/e2e/support/purchase-export-assertions.ts` for independent artifact checks.

- [ ] Add RED renderer/acquisition tests covering payment/source/attribution IDs/hashes/revisions, separate dates/claim periods, ordered capital/non-capital/private allocations, evidence/timing/legacy/suspense/deferral states, reconciliation totals and maximum supported rows. Independently parse CSV and PDF text/page counts. Legacy saved workpapers still export their original shape/amounts truthfully.
- [ ] Run the phase-one renderer/main focused tests and capture RED for absent new source/review fields. Retain formula-cell neutralisation, Unicode CSV, PDF glyph checks, byte limits and source-completeness validation.
- [ ] Extend exact retained snapshot conversion/rendering rather than querying live purchases after export acquisition. Source mutation after a saved draft makes it stale but cannot silently regenerate exports; all exported figures and retained facts bind to that displayed workpaper ID/hash. Preserve native cancellation/failure/authentication/session/path/atomic-writer checks.
- [ ] Reconcile typed claimed sources to 1B and total source gross/classifications; list legacy/provisional and unresolved amounts separately so excluded suspense is never presented as claimed. Include full retained review decisions and deadlines without leaking plaintext original evidence/private paths.
- [ ] Run the phase-one focused renderer/main tests, invoice export regression, desktop typecheck and visual PDF page inspection. Commit the additive extension after phase-two source capture is complete.

### Task 16: Prove the journey through the newly built packaged application

**Files:**

- Create `apps/desktop/tests/e2e/purchase-payments.spec.ts`, `support/purchase-payments-workflow.ts`.
- Modify `apps/desktop/playwright.config.ts` to explicitly include `purchase-payments.spec.ts`; maintain current invoice/template/SBR projects.
- Modify `apps/desktop/tests/e2e/fixtures.ts` only for reusable real test boundaries; add synthetic evidence under `apps/desktop/tests/e2e/assets/`.
- Create `docs/development/purchase-payments-gst-acceptance-2026-10-05.md`; update `docs/development/user-journey-test-matrix.md` and programme implementation status after evidence passes.

- [ ] Write the real packaged RED case `purchases/payments-gst`: create/unlock/authenticate supported cash workspace, ingest synthetic `$110/$10` invoice dated before the first payment, review it, explicitly post, pay `$55` on `2026-09-30` and `$55` on `2026-10-02`, regenerate respective quarter drafts, export actual PDF/CSV through native save boundary, restart/reopen and verify retained identities/amounts/history. Use a test-controlled supported clock/as-of where needed; never change entered dates to bypass a guard.
- [ ] Add non-cash and mixed/private/capital variants, actual cash-flow fact/projection verification, an earlier-period legacy payment/review outcome, missing/late evidence to suspense then explicit attribution, evidence-complete deferral with future discovery and attribution, and one opening source without document. Include document/payment page 201, source-limit failure, stale draft after unchanged-total evidence mutation, cancellation/failure and retained original snapshot export.
- [ ] Through the real typed allowlisted bridge/core, assert persisted journal lines and trial-balance controls after each event: cash final expense `10000`, bank `-11000`, payable/deferred `0`, current input GST `1000`; workpaper credits `500` then `500`. Non-cash yields one `1000` credit and no second credit/direct-document duplicate. Query authoritative records for verification; perform primary posting/payment/evidence actions through the UI. Parse artifacts independently and compare every identity/date/source row/hash/amount/review state, not only displayed totals.
- [ ] Exercise authentication/forbidden roles/closed-period/advance/stale version/changed-key replay/lost response/duplicate prevention through real core integration; use UI packaged checks for truthful errors and retry state. Add no-ATO transport/assertion and unchanged bank-reconciliation checks. Keep complete BAS prepare/freeze controls and integrated prepared-report acceptance for phase three.
- [ ] Before a passing claim, run bounded focused default/pure/tagged/desktop/type/contract/lint checks from Tasks 1–15. Run `rtk proxy mise exec -- task verify:quick` after coherent commits and `rtk proxy mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/purchases ./services/core/internal/reporting ./services/core/internal/localproduct -count=1 -timeout=10m` serially using the already built native dependency. Resolve failures; do not add blanket retries or mark a timeout green.
- [ ] Run `rtk proxy mise exec -- task package:e2e` against the newly built Builder application, including existing foundation/current-workflows/invoice-income and macOS SBR regressions. A development Playwright test or old artifact is insufficient. Inspect screenshots/PDF pages, record artifact hashes/revision/commands/results and attach the persisted/parsed reconciliation evidence. Capture expected RED before implementation when executable; rerun after fixes to GREEN.
- [ ] Request independent spec-compliance then code-quality review, resolve findings and rerun only affected checks. Commit validated phase two; leave all required checks and limitations visible in the acceptance record. Do not proceed with phase-three shared source changes before this packaged acceptance completes.

### Task 17: Verify every requirement and hand off stable source ports

- [ ] Use the matrix below to verify the implementation and executable evidence cover every purchase-spec test group. A checked box without a command/result/artifact is not evidence.
- [ ] Document the stable `purchases.ReportingPort` response/fingerprint schema for phase three: complete eligible sources, candidate coverage, unsupported/unresolved outcomes, deferral deadlines and source links. Phase three adds preparation/freeze blockers and profile/PAYG lifecycle against this contract; it must not rediscover purchase credits from document browser pages.
- [ ] Preserve original saved workpapers and immutable purchase/journal history, ensure migration 0013 remains available, and leave unsupported opening settlements/credits/refunds/corrections explicit. Update programme status with phase-two committed revision and packaged evidence.

| Purchase design requirement | RED→GREEN evidence owner |
| --- | --- |
| 1. Cash `$110/$10`, split quarters, residual/restart | Tasks 3, 5, 6, 10, 16 allocation/repository/packaged tests. |
| 2. Non-cash credit once; no direct-source/payment duplicate | Tasks 5, 6, 10, 16 BAS and ledger assertions. |
| 3. Mixed/private/noncreditable/capital plus actual signed cash flow | Tasks 3–6, 10, 16 facts/projections/artifacts. |
| 4. Cents/large/partitioned payments and 201+ history | Tasks 3, 6, 13, 14, 16 exact property/pages. |
| 5. Organisation/account/arithmetic/date/amount/source/replay rejection | Tasks 1, 3, 5, 6, 12, 14. |
| 6. Exact replay/lost response/concurrency/rollback atomic effects | Tasks 6, 8, 12, 14, 16. |
| 7. Basis history/closed dates/generic reversal/invoice preservation | Tasks 4–8, 12 and retained invoice regression. |
| 8. Missing/future/late evidence, timing limits, deadline/deferral/stale | Tasks 7, 8, 10, 14–16. |
| 9. Same bytes/semantic duplicates/distinct confirmation/prior accounting | Tasks 5, 9, 12, 14, 16. |
| 10. 201+ documents, older/undated/opening candidates, source bounds | Tasks 9–11, 14, 16. |
| 11. Upgrade without backfill/rewrites; opening source without document | Tasks 2, 9, 10, 12, 16. |
| 12. Real TLS/SQLCipher plus packaged posting/payment/BAS/native exports | Tasks 12, 15, 16, acceptance record. |

**Completion boundary:** Phase two is complete only when all accepted command effects reconcile persisted ledger/control/GST facts, complete saved snapshots and actual exported artifacts after restart, all unresolved review states remain truthful/discoverable, and newly built packaged regression passes. Lifecycle readiness/freeze enforcement remains the separately authorised next phase.
