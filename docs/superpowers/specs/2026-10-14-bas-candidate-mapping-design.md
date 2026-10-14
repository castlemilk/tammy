# BAS candidate mapping: writer and reader oracles (2026-10-14)

Status: oracle checkpoint accepted after independent GPT-6 Luna Max specification and quality reviews and ROOT's focused/affected native race verification. The [implementation plan](../plans/2026-10-14-bas-candidate-mapping-oracles.md) records the evidence. Semantic candidate admission and the combined producer remain open.

## Boundary

Purchase physical discovery is accepted in `7ffc0c4c`. Invoice, purchase and accounting keys identify retained rows under finite reservations; they do not establish eligibility, applicable legacy decisions, monetary conservation or semantic coverage units. The [bounded effect-key constraints](2026-10-13-bas-bounded-effect-key-design.md) and [complete source plan](../plans/2026-10-12-bas-complete-source-capture.md) remain authoritative.

This checkpoint pins the owning writers' and complete readers' existing behavior with native SQLCipher tests that also inspect the accepted scalar keys. It introduces no production mapper, nomination API, eligibility rule, contract or migration. Passing these oracles is a prerequisite for choosing semantic units; it does not accept a candidate union or complete Task 5.

Use the existing issue/sbr-registration-ready worktree. ROOT alone activates Go files, formats, runs tests and commits; agents draft or review without editing active Go files. Preserve the existing caller-owned transaction and schema guards.

## Three identities

Keep a retained decision identity, an underlying monetary source identity and a complete proof identity separate. An attribution's own ID and a deferral's `(deferral_id, sequence)` identify immutable decisions. A verified posting/payment allocation identifies an underlying credit. A journal ID identifies a complete accounting proof; several allocations or attribution decisions can share that proof without becoming one decision or gaining individual line ownership.

Equal source links nominate a comparison target. They are not interchangeable source snapshots until the owning reader verifies complete bodies and hashes. Never merge candidates by SourceRef, journal, account, amount or date. Every current accounting tax fact remains independent because current invoice/purchase writers do not emit module-owned facts.

## Legacy oracle

Cross real retained review history and source-linked manual journals with scalar keys, `LegacyCandidates`, and complete purchase coverage. Assert exact review IDs and source/record hashes, root flags and unique receipt identities alongside the independently computed full-reader status.

Required cases cross every retained review in a real immutable history and establish that an unpaid review cannot waive a later cash payment. A combined period/balance selection change may change the reader's decision while physical keys remain unchanged; this fixture does not independently isolate both selectors. New payment facts may add keys and change classification while old review identities remain present. Pin the exact added manual journal key and receipt flag, not just the count. The oracle must state which input changed rather than equating a reservation with a content fingerprint. Existing conflicting-review tests remain responsible for rejecting latest-review-only classification; the new history comparison does not replace those tests.

Keep two distinct paths explicit: a valid evidence-only source is verified through `LegacyEvidenceForPosting` and the existing workpaper without another credit; a promoted document is omitted by `LegacyCandidates` but its immutable history remains in complete coverage with `CurrentRoot=false` beside the owning modern posting. Complete coverage retains bodies and lineage; it does not classify a promoted source's current-period status. The promoted oracle establishes retention and ownership, without claiming a new producer disposition.

Do not infer current applicability from scalar source version/hash alone. `LegacyReviewApplies` also requires the reviewed document snapshot and affected-period/balance scope. The classifier considers all consistent applicable history, checks evidence/payment facts, and treats contradictions explicitly. Current scalar keys omit some of these inputs. A future admission rule may need disposition, affected-period start/end, reporting balance, previously-claimed-period presence/values, the current reviewed-snapshot digest and exact source/support/payment associations. These are selector gaps, not an accepted new API. Any admission rule depending on them needs a bounded purchase-owned nomination contract and complete verification later.

## Attribution and deferral oracle

Cross real command-created headers, portions, attribution IDs and source associations with `VerifiedClaims`, complete purchase coverage and the existing BAS workpaper amounts. Keep all physical history keys even when a later claim changes their current remaining amount.

Pin repeated-period decisions over the same source: original deferred extents remain retained, while outstanding credit is counted once per verified physical source. Attribute one whole selected portion while sibling portions remain outstanding. An attribution consumes that selected portion's remaining extent; the API does not accept a partial claim amount. Every matching older deferral remainder must reflect the later attribution, even when it names a different deferral.

Assert the owning accounting invariant separately from history amounts: attributed GST plus cash-unpaid deferred GST plus suspense GST equals posting eligible GST. A deferral decision does not transfer money. Cash posting and payment allocations are not independent additions of the same eligible credit. Verify exact command/journal linkage without manufacturing per-child payment or attribution journal-line mappings.

The new claim oracles pin history identities and concrete monetary extents. Existing period-status tests remain authoritative; this checkpoint does not choose current/past/future candidate admission. A later nomination rule may require claim/affected periods, event/release timing, exact source portions and a verified remaining-state selector. Current physical keys do not expose all those inputs.

## Test discipline

Add focused native tests in `purchases_test`, reusing real document/account/posting/payment/evidence/legacy/claim commands and the complete readers. New tests must cross scalar discovery with verified bodies and concrete amounts; do not duplicate existing body-only tests. Use hard-coded expected identities/counts/amounts grounded in the fixture, preserve zero or outstanding siblings, and assert no reader writes or caller transaction completion.

These tests characterize established production behavior. Their first passing baseline is oracle evidence, not a production RED. If an actual discrepancy appears, reproduce it before any production fix. Future nomination or union code still requires genuine API/behavior REDs before implementation. Do not manufacture failures by changing correct expected results.

Specification review precedes quality review. ROOT runs the focused native suite with the race detector and the affected complete purchase/reporting native race suite, excluding only the already accepted expensive public-posting stress fixture. Scalar/oracle tests have no exclusions. Record full output and actual exit status before accepting this checkpoint. Existing-route TLS and production-build evidence belongs to the accepted reader checkpoint; these new tests alone do not exercise a combined producer or packaged phase-three workflow.

## Decisions that remain open

The oracle checkpoint leaves legacy candidate admission, promoted/evidence-only suppression, attribution/deferral semantic units and current/past/future visibility policy open until the fixtures and reviews settle them. Preserve full history as proof without automatically spending one coverage slot per proof row.

The recommended follow-on is the smallest purchase-owned bounded nomination contract justified by those results, then a pure typed union with exact fallback rules, deterministic ordering and the 1,000/1,001 candidate fence before body hydration. A conservative upper bound must be labelled as such and cannot establish exact-union acceptance. Missing nomination inputs fail closed rather than being guessed from sign, date or account.

Full duplicate-fact proof comparison and independent byte arithmetic, shared global fences, supported journal-line mapping, coverage's bounded lineage/evidence representation, source-currentness hashing and the combined transactional producer remain separate requirements. No overall Task 5 checkbox is complete.
