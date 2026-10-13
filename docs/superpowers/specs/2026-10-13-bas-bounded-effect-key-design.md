# BAS bounded effect-key discovery constraints (2026-10-13)

Independent GPT-6 Luna Max review cleared these design constraints. Invoice/purchase API sketches and candidate mappings still require native TDD. The accounting candidate-key dependency below is implemented and verified; Task 5 acceptance remains open.

Existing invoice/accounting payload-reader storage-class guards are accepted in `d1c19f5d`. Accounting candidate-key query guards are now accepted; the remaining scalar discovery and combined producer still require implementation.

## Proposed boundary

Add package-owned scalar summaries and bounded typed-key reads for invoices, purchases, and accounting. Keep source-root count, receipt count, physical scalar-key count, and semantic coverage-candidate count distinct; invoice `CandidateCount` currently denotes invoice roots only.

```go
type BASScalarSummary struct {
    RootCount, ReceiptCount int64
    ScalarKeyCount, ScalarBytes int64
}

SummarizeBASScalarKeys(ctx, organisationID) (BASScalarSummary, error) // COUNT/SUM only
ReadBASScalarKeys(ctx, organisationID, expected BASScalarSummary,
    maxKeyCount, maxBytes int64) (BASScalarKeys, error) // bounded scalar columns only
```

Use this exact order on the caller-owned serializable SQLCipher transaction:

1. Run every module's scalar aggregate summaries (COUNT/SUM only), before any typed-key or parent-ID read.
2. Sum and enforce shared source-root count ≤1,000 and receipt count ≤1,000 separately; reserve combined `ScalarBytes`. Query-side `typeof(column)='integer'` guards must reject non-INTEGER storage in every scanned integer key/version/sequence/count/revision/line/amount cell before accepting a charge. Numeric-affinity TEXT/BLOB can escape existing byte sums, especially in invoice/accounting projections. Keep accepted migrations unchanged: no schema/STRICT migration is part of this dependency. Preserve NULL presence; never coerce NULL to zero/empty or a zero hash. Test oversized TEXT/BLOB numeric-affinity values and NULL-vs-zero/empty distinctions.
3. Run existing invoice, purchase, accounting, and rule-bundle SQL payload preflights. Their bounded parent-ID discovery is part of preflight and occurs only after the shared count/scalar fences.
4. Read bounded typed root/component/provenance keys under the reserved scalar charge. Set the physical-key scan ceiling from the 8 MiB limit divided by the 512-byte minimum row charge, plus a sentinel; proof/history keys do not consume the semantic candidate cap. Verify `ScalarKeyCount` and scalar charges against summaries.
5. Apply the explicit semantic mapping, form the exact candidate-key union, and enforce coverage candidates ≤1,000. Then compute final global byte arithmetic; only after both fences pass may any body hydrate.

`ScalarKeyCount` is physical and is not final `CoverageCount`. The final 8 MiB reservation includes the sum of all module `ScalarBytes`, invoice/purchase/accounting retained base bytes, `RuleBundleReportingPreflight.PayloadBytes`, and every other separately retained body category exactly once. Purchase low-value exception bytes are already included in purchase `BasePayloadBytes`; do not charge them again. The fact/journal portion is:

```text
accounting canonical fact rows + purchase BasePayloadBytes
    - purchase TaxFactPayloadBytes (only after exact duplicate-fact validation)
    + unique complete-journal proof charges
```

For fact deduplication, compare every purchase-retained row with its exact all-organisation accounting fact in the same verified transaction: fact ID, organisation ID, journal-line ID, every retained raw fact/source value, source type/ID/revision, nullable content-hash presence/value, and expected matched row set/count. Do not require purchase and accounting per-row byte charges to equal: accounting charges `512 + its selected TEXT/BLOB fields`, including joined parent IDs/organisation/date; purchase charges `512 + all selected retained-row columns`, including numeric values cast to text, and excludes joined-parent bytes. Validate each module's reserved-vs-loaded per-row charge independently. Subtract the purchase-declared `TaxFactPayloadBytes` only because the canonical accounting rows replace those duplicate purchase proof rows; on any identity/content mismatch, do not subtract or drop facts. Deduplicate shared complete-journal proofs only by exact organisation/journal ID and equal charge under the canonical accounting journal recipe; unequal charges fail closed. After hydration, compare each module's exact key set/count and independently computed byte charge to its reservation. `baspreparation` coordinates; it owns no table SQL or transaction lifecycle.

Use namespaced physical keys such as `ScalarKey{module, family, ownerID, kind, effectID, sequence, parentID}`. Root/receipt decisions are fallback candidates only when no child effect exists; otherwise child rows carry the root/receipt decision and lineage, with no duplicate parent money row. Preserve zero-value allocations. Never collapse by `SourceRef`, shared journal, amount, account, or date.

## Accepted accounting candidate-key dependency

`JournalRepository.SummarizeTaxFactBASKeys` exposes aggregate-only organisation-bound `TaxFactBASScalarSummary` values. `RootCount` and `ReceiptCount` are zero; `ScalarKeyCount` counts physical facts, without applying the final semantic 1,000-candidate fence. Selected TEXT/BLOB cells plus 512 bytes per row fit one 8 MiB reservation, giving a physical ceiling of 16,384 keys. Missing/foreign owner, journal-line and journal parents, malformed nullable source hashes and non-INTEGER scanned source-revision/line-number cells fail before typed values are returned.

`ReadTaxFactBASKeys` verifies that reservation and returns exact fact/organisation/line/journal identities, line number and source type/ID/revision/hash presence/value. SQL guards ordered cumulative bytes and row numbers before projecting cells to the driver, including the count sentinel; loaded row charges must equal SQL charges. Cancellation or drift returns no partial result. The caller retains transaction ownership. Native writer, corruption, allocation/error bounds, rollback/reopen, race/TLS, default Core and production-build evidence is recorded in the [complete source plan](../plans/2026-10-12-bas-complete-source-capture.md).

This is candidate identity discovery only. The source tuple does not become the fact row's own original provenance, and the projection is insufficient for duplicate fact proof removal. Future coordination must still apply every module's shared fences in the order above, verify complete raw fact proofs and retain each current fact as an independent candidate.

## Tables and typed shapes

- **Invoices:** `invoices_v1` includes all states; `invoice_income_postings_v1` is keyed by invoice, `invoice_income_lines_v1` by `(invoice_id, sequence)`, `invoice_payment_v1` is the receipt row, and `invoice_payment_lines_v1` by `(payment_id, sequence)`. Existing shapes are `BASReportingCapture → BASInvoiceCandidate{Posting, Receipts} → BASInvoicePosting{IncomeLines}, BASInvoiceReceipt{Allocations}`. Expose invoice/posting and receipt/allocation keys, nullable journal linkage, and parent IDs. Keep all states, unposted/no-line roots, and zero-allocation receipts.
- **Purchases:** source families include `purchase_postings_v1`, `purchase_posting_allocations_v1`, `purchase_payments_v1`, `purchase_payment_allocations_v1`, `purchase_tax_evidence_v1`, `purchase_accounting_events_v1`, `purchase_gst_deferrals_v1`, `purchase_gst_deferral_portions_v1`, `purchase_gst_attributions_v1`, `purchase_legacy_reviews_v1`, purchase-linked `documents`, `purchase_opening_payables`, `opening_conversions`, and `opening_items`. Preserve all posting/document/opening roots, including `POSTED`/`REPLACED` openings; expose component and nullable-provenance keys. Current shapes are `PurchaseCoverageReportingCapture{Postings []PostingReportingRecord, LegacySources []*LegacyPurchaseReportingRecord, Documents, Journals, Rows}`; posting records retain posting/accounting/payments/evidence/claims, and legacy records retain document/opening source, `CurrentRoot`, reviews, and journal edges. Evidence/reviews/events are proof keys unless they independently carry a tax decision. Keep attribution and deferral keys distinct until tests define their units and candidate mapping. Manual receipt discovery may retain the current cash/sign predicate and unique linked-journal candidate unit; it must not infer GST treatment from sign or account.
- **Accounting:** the all-organisation `tax_facts` census references `journal_lines` and journals without state/source/date/sign filters; `cash_flow_facts` can help identify linked manual receipts. Every fact is an independent candidate `(fact.id, fact.journal_line_id)`, even when purchase retains it as lineage. `TaxFactReportingRecord` retains values, `Rule`, source type/ID/revision, nullable `SourceContentHashPresent` plus bytes, and journal source metadata. Source type/ID/revision are required by schema; content hash is nullable.

## Parent integrity and fact ownership

For every scanned family, start from that family's retained rows and `LEFT JOIN` every expected owner/parent; do not let an inner join hide an orphan. Validate both existence and same-organisation ownership and retain explicit unresolved status (or abort the whole capture before returning partial results). This applies to every root→organisation edge, invoice→posting/income-line and invoice→receipt/allocation edges; purchase→allocation/payment/evidence/event/deferral/portion/attribution/review/opening/conversion/document edges; and fact/cash-flow→journal-line→journal→organisation edges. Include invalid rows in scalar key counts before deciding whether the capture can proceed.

Invoice posting/payment and purchase posting writers currently pass `nil` TaxFacts to `JournalRepository.Post`; no module-owned facts are emitted. Every current fact therefore remains an independent manual/unsupported blocker, even if SourceRef resembles an invoice or purchase. Purchase-retained fact rows are lineage only and must match accounting facts exactly before their byte charge is treated as duplicate.

Future coalescing needs a module-supplied exact source tuple (required source type/ID/revision plus nullable content-hash presence/value) and the exact `journal_line_id` mapped by that writer to the component. Purchase posting's emitted expense-line mapping can be pinned by tests using `BuildPurchaseJournal`'s sequence rule (`gross_minor - eligible_gst_minor > 0`) and exact line ID; this is not current tax-fact ownership. Invoice income/payment allocations and aggregated payment/attribution journal lines have no retained per-child mapping, so do not assume one. Unsupported/ambiguous facts stay separate blockers; never derive GST direction or timing from account/sign/date.

## TDD checkpoints and decisions to keep open

1. RED storage-class tests for every scanned integer key/version/count/sequence/revision/line/amount cell, TEXT/BLOB numeric-affinity violations, and NULL-vs-zero/empty/hash-absent distinctions; then tests for all-family orphan/foreign-parent detection.
2. RED module summary/key-read tests: root and receipt counts/caps are separately combined before typed-key reads; scalar count/charge agrees under one transaction; reads fail at `cap+1`.
3. RED candidate-union tests before body loaders: exact root/receipt fallback, no parent-money duplication, all current facts independent, and candidate cap 1,000. Pin legacy-review row selection and attribution/deferral candidate units plus amount-conservation rules before implementation.
4. Pin only supported exact journal-line mappings (purchase posting emitted lines); prove invoice allocation and aggregated payment lines do not invent mappings. Test payload arithmetic with the exact all-org fact set and each module's independent recipe charge; do not assert cross-module per-row charge equality. Prove a mismatch never drops candidates or subtracts bytes.
5. Only then wire body hydration; compare exact hydrated key sets/counts/charges and run broader integration/E2E gates.

`ScalarKeyCount` remains a physical read count, not semantic coverage count. Keep unresolved attribution/deferral/review row semantics explicit in producer tests rather than choosing their coverage behavior here.
