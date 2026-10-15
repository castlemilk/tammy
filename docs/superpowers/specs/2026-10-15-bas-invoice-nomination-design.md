# BAS invoice structural candidate nomination

Status: invoice structural nomination accepted after independent specification then quality reviews, focused/complete affected native race, existing-route TLS, default Core and pinned SQLCipher build verification. The [implementation plan](../plans/2026-10-15-bas-invoice-nomination.md) records exact evidence and review model fallback. This chooses invoice coverage units only. Purchase nomination and the combined producer remain open.

## Decision and alternatives

Nominate every retained invoice income line and every receipt allocation, with one invoice-root fallback when there are no income lines and one receipt fallback when that receipt has no allocations. These are coverage candidates, not amounts or eligibility decisions. Keep both families regardless of basis, date, value, draft/issued status or journal link. Full verified bodies later determine the additive role and disposition from retained basis/history, rather than the current organisation basis alone. A partly paid source must remain represented alongside its settlements.

Selecting just the current-basis family would reduce coverage rows but needs unavailable scalar selectors and can hide retained basis history or unpaid obligations. Counting parents alongside their children would duplicate coverage units and spend the cap on redundant rows. The chosen structural mapping is exact from the accepted five-family keys; its additional coverage rows are an explicit cost against the 1,000-candidate limit.

## API and ownership

Add `invoices.NominateBASCandidates(ctx, expected BASScalarSummary, keys []BASScalarKey, maximumCandidates int64) ([]BASCandidateKey, error)` as a pure module-owned mapping. It performs no SQL, body hydration, transaction lifecycle or writes. Existing summaries, key reads and full captures remain unchanged. The caller keeps the complete physical key set for later exact body reconciliation.

`BASCandidateKey` contains `Kind`, `OrganisationID`, `InvoiceID`, `ReceiptID` and `Sequence`. Four typed kinds distinguish invoice fallback, income line, receipt fallback and receipt allocation. Sequence is zero only for fallback; receipt ID is empty only for invoice-side families. These structured fields retain opaque SQL identities without inventing canonical references or concatenating component IDs. They carry no borrowed original provenance, computed body hash, amount, tax date, journal-line mapping or disposition.

Validate context, nonnegative reservation/budget, physical count/byte ceilings, every family shape, same organisation, positive signed-SQL versions/sequences, exact family counts and the independent existing 512-byte-plus-selected-cell recipe. Reject duplicate physical keys, reused receipt IDs and missing/cross-invoice root/posting/receipt parents. The summary remains a size reservation, not a content fingerprint. Arbitrary same-size body/reference changes still require the later owning full reader.

Map only after all input checks. Sort by candidate kind, invoice ID, receipt ID and sequence; preserve the input slice and its nullable values. Enforce the smaller of the caller's candidate allowance and 1,000; zero permits an empty set and negative budgets are invalid. Bound failure, malformed input or cancellation returns no partial nominations. This local limit neither reserves global body bytes nor proves the final cross-module fence or body-query order.

## Verification and remaining boundaries

Use a native API-absence RED before production code, then exact structural outputs from real draft/issue/post/payment writers and their complete invoice capture. Include both GST bases, old/future/unposted sources, an undated draft and zero-valued child allocations. Issuing requires a valid date; posted/receipt rows without children require synthetic or count-only structural fixtures and must not be claimed as financially valid full-capture journeys. Count-only immutable rows may establish exact 1,000/1,001 nomination limits without claiming financial-body validity. Check zero/exact/short budgets, physical reservation drift, malformed families/graphs, deterministic ordering, unchanged inputs, cancellation and read-only caller transaction behavior. Obtain independent specification review before quality review, then focused/affected native race, default Core, existing-route TLS and pinned production build evidence.

Purchase units, legacy evidence-only suppression, attribution/deferral decision attachment, transitive coverage proof representation within 20 identities, raw component persistence encoding, complete duplicate-fact comparison, global reservations and the combined transactional producer remain separate dependencies. A conservative legacy upper bound cannot establish the required exact candidate union. No Task 5 or packaged phase-three gate is complete from this helper.
