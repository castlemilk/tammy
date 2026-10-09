# Purchase reporting source contract

This document is the phase-three handoff for the phase-two purchase reporting port. It describes the implementation in services/core/internal/purchases/reporting_port.go and reporting_sources.go, consumed by services/core/internal/reporting/bas_sources.go. The bridge is not a replacement for that Core port.

## Stable port and acquisition

purchases.ReportingPort has one operation:

- CaptureBAS(context.Context, organisationID string, period *tammyv1.PurchaseClaimPeriod, balanceDate *tammyv1.CivilDate, asOf time.Time) (*BASSnapshot, error)

The response shape is:

~~~go
type BASSnapshot struct {
    Sources                  []*tammyv1.BasPurchaseSourceLine
    Reviews                  []*tammyv1.BasPurchaseReview
    LegacySupport            []*tammyv1.BasPurchaseLegacySupport
    LegacyCandidateGST       *tammyv1.Money
    ExcludedCashPurchaseCount uint64
    Fingerprint              []byte // 32-byte SHA-256
}
~~~

CaptureBAS runs in the caller's transaction against the authenticated, canonical organisation. It validates the current organisation profile and GST-basis history through invoices.CheckGSTBasisHistory, applies the pinned low-value rule metadata, and reads the complete purchase-owned sources. It is independent of Documents page size and cursor state. A missing or corrupt source, a changed source hash, or any supported-bound overflow fails the acquisition; it never returns a truncated “complete” snapshot.

BASSnapshot contains:

| Field | Meaning |
| --- | --- |
| Sources | Typed purchase source allocations in the requested BAS view, including eligible credits and retained zero-eligible outcomes, cash-basis payment allocations, and the single eligible non-cash posting/attribution. |
| Reviews | Typed unresolved, excluded, unsupported, duplicate, evidence, timing, legacy and deferral outcomes. A review is retained even when it changes no eligible cents. |
| LegacySupport | Ordered provenance for prior claim/payment/evidence decisions and opening-payable support, linked to each candidate and disposition. |
| LegacyCandidateGST | A separately labelled provisional AUD amount; it is not reconciled 1B. |
| ExcludedCashPurchaseCount | Count of excluded legacy cash-source candidates, separately surfaced for review. |
| Fingerprint | 32-byte SHA-256 commitment to the captured purchase/profile/rule inputs and deterministic BAS purchase view. |

Each BasPurchaseSourceLine retains the source identity and relevant posting/document/hash/version, optional payment and payment hash, optional GST-attribution identity/hash, allocation sequence, use/BAS/cash-flow classifications, invoice date, optional actual payment date, normal tax-event date, claim period, exact AUD gross/supplier/eligible/claimed GST, evidence reference, source revision/hash, and optional account/version/supplier/invoice/release-posting metadata.

Each BasPurchaseReview retains review kind/status, affected period, relevant invoice/payment dates, eligible and unclaimed GST, optional legacy disposition or future-claim obligation, evidence reference, source revision/hash, reason and optional document identity. Reviews cover unresolved eligibility, missing evidence, claim timing, excluded unpaid/private/non-creditable items, duplicates, unsupported existing settlement, deferred future claim, old/undated legacy documents, opening payables and resolved legacy decisions.

BasPurchaseFutureClaim retains deferral ID/hash, organisation/posting/document/version, typed PurchaseGstSourcePortion, affected period, timing review, deferred GST, reason, actor and timestamp. PurchaseGstSourcePortion preserves normal tax-event date, earliest otherwise eligible period, required-return due date and its evidence reference, ordinary last claim date, evidence identity and held date. Deferral is immutable; outstanding status is derived from later attribution links.

## Completeness and accounting semantics

Owned postings are read in stable invoice-date/ID order with a 1,001-row lookahead. A result above 1,000 fails with purchases.ErrSourceBound. Legacy reviewed documents without an owned posting and opening payables share one 1,000-candidate bound. Documents and payments remain separately paginated to operators; those pages are not the aggregate-reporting source.

Cash-basis credit follows actual eligible payment allocation or explicit attribution by claim period, including an invoice dated before the quarter. Non-cash credit follows the eligible posted source/explicit attribution once; settlement is not a second credit. Standard source rows include automatic-credit and zero-eligible outcomes. Only claimed_gst contributes to reconciled 1B. Legacy candidate GST remains separate and cannot be confused with claimed entitlement. A missing payment row is not proof that a legacy payable remains unpaid. Prior claim, duplicate, non-creditable, evidence-only, confirmed-unpaid-through-date, and unsupported-settlement decisions keep their source provenance.

Every purchase, evidence, payment, attribution, deferral, legacy decision and newly unresolved review that can affect the result or its completeness participates in staleness. Therefore an evidence-only change or a new unresolved source can change the fingerprint while eligible cents remain constant. Saving a workpaper does not change its source fingerprint. The asOf value is not a direct fingerprint input; it can change deadline classifications and derived review outcomes. Phase three must capture again at preparation/freeze rather than assume an earlier draft remains current.

## Fingerprint construction

The purchase snapshot fingerprint is SHA-256 over deterministic protobuf data. The encoder length-prefixes each constituent byte sequence with an unsigned 64-bit big-endian byte length. In order, the commitment includes:

1. Current organisation profile and pinned low-value rule bytes and metadata (marker, source and retained-at time), or an explicit no-rule marker.
2. GST-basis history watermark as unsigned 64-bit big-endian data.
3. Each owned posting's state and its retained evidence, payment, attribution and deferral histories.
4. Each legacy document/opening-payable identity and provenance, prior claim/payment facts, current review, and support flags.
5. A deterministic BasWorkpaper view of organisation, period, basis, ordered sources, reviews, ordered legacy support and provisional legacy candidate GST.

The reporting collector builds an outer SHA-256 over the deterministic combined workpaper view, including organisation/period/balance date, sales and purchase rows, warnings, excluded-cash count, candidate total, the inner purchase fingerprint, and prior legacy purchase versions. This is distinct from financial_revision; saving a BAS cannot make itself stale by incrementing an unrelated global revision. The exact implementation and deterministic proto schema are the source of truth if field order or encoding changes.

## Bounds and overflow behavior

| Boundary | Limit and behavior |
| --- | --- |
| Owned purchase source scan | At most 1,000; 1,001 is detected and returns ErrSourceBound. |
| Legacy document plus opening candidates | 1,000 combined; overflow errors rather than dropping candidates. |
| BASSnapshot source, review and legacy-support collections | 1,000 per bounded collection. |
| Saved BAS source rows | 1,000 per supported collection; no page-based partial snapshot. |
| Workpaper warnings | At most 10. |
| Encoded purchase response | Each deterministic purchase message is bounded to 1 MiB. |
| Saved purchase source/review/support protobuf rows | At most 32 KiB / 64 KiB / 2 KiB respectively; oversize or corrupt rows fail. |
| Encoded full workpaper protobuf | At most 4 MiB including response envelope; oversize fails before partial save or response. |

Documents and payment histories have their own page contracts. Current regression coverage includes 201+ documents and payments, a source-bound overflow and the exact maximum renderer fixture; the renderer's maximal all-source export test covers 1,000 purchase rows. Phase three must preserve complete-source admission when composing the larger prepared report.

## Explicit limits for phase three

This port is a complete purchase view, not a complete-tax-source manifest. It does not yet prove all cross-domain sources were considered and classified. Phase three needs a versioned coverage object for every source domain and obligation, with explicit eligible, excluded, unresolved, unsupported or authoritative-not-applicable outcomes linked to source identity/hash/date/period. It must include purchases alongside invoices, manual journals and typed TaxFacts, organisation/profile/history, rule bundles and PAYG evidence. Empty purchase/source arrays do not prove “nothing to review.”

Do not infer tax direction or period from sign, account name, journal date or browser-page absence. Unclassified manual journal/tax facts are blockers. Do not infer an opening payable was settled from missing payment rows. Preserve separate purchased-source provenance, immutable history, old saved workpapers and migration 0012; migration 0013 remains available for the lifecycle. Phase three owns fresh capture, preparation/freeze blockers, PAYG/profile lifecycle and explicit external handoff records. A frozen report must bind exact prepared source coverage and retain unsupported decisions; this phase has no ATO submission, external lodgement history, or production transport.
