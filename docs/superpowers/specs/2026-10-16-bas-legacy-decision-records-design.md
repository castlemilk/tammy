# Bounded BAS legacy decision records

Status: accepted dependency checkpoint, 2026-10-16. Independent reviews and focused/affected native race, existing-route TLS, default Core and pinned production-build verification passed. The [implementation plan](../plans/2026-10-16-bas-legacy-decision-records.md) records exact evidence. This follows legacy currentness and support ancestry in `1d497908`, within [complete source capture](../plans/2026-10-12-bas-complete-source-capture.md).

## Owned boundary

Add a separately budgeted purchase-owned read of complete canonical immutable legacy decision history. `ReadLegacy` cannot perform this job before financial-body hydration: it loads the primary document/opening, supporting document bytes and complete result events. The new projection returns only `LegacyPurchaseReview` decision records, not those financial/source/event bodies. The decision proto contains scopes, dispositions and bounded retained support refs, but no money. This is explicitly a decision-body read, not a scalar key or a currentness/eligibility proof.

```go
type BASLegacyDecisionSummary struct {
    OrganisationID string
    RecordCount, DecisionBytes int64
}
type BASLegacyDecisionRead struct {
    Summary BASLegacyDecisionSummary
    Reviews []*tammyv1.LegacyPurchaseReview
}
SummarizeBASLegacyDecisions(ctx context.Context, organisationID string) (*BASLegacyDecisionSummary, error)
ReadBASLegacyDecisions(ctx context.Context, organisationID string,
    expected BASLegacyDecisionSummary, maximumRecords, maximumDecisionBytes int64) (*BASLegacyDecisionRead, error)
```

The summary enumerates all organisation-owned histories, including promoted documents and opening history, without period, current-source, latest-review or disposition filters. It returns COUNT/SUM metadata only. Its reservation is not an identity/content fingerprint. Reject missing/foreign source owners, malformed outer storage and invalid selected-cell shapes. Cross-owner references touching the requested organisation must fail rather than disappear. Support owners inside the proto, current source/cash facts and result events are explicitly deferred.

## Exact charge and guarded read

Charge each record at `512 + UTF-8/blob bytes of id, organisation_id, source_kind, source_id, review_proto, record_hash`, where source ID is the exact document/opening ID and accounting version is an INTEGER under the fixed allowance. Select these fields and only finite numeric guard markers. Inspect all compared outer numeric cells with INTEGER/NULL guards, all required/optional outer text/hash storage shapes, per-record proto BLOB length 1..131072, and required source/claim/link ownership in SQL before copying a decision body.

The named decision pool is at most 8 MiB, with a separate physical record ceiling of 16,384 and per-source history ceiling of 1,000. Do not impose the semantic 1,000-candidate cap on combined history. Negative caller allowances fail before SQL; zero permits an empty history. Verify a fresh summary against the caller's comparable expected reservation, enforce caller/default caps, then use SQL ordered cumulative-byte, per-source-count and row/sentinel guards so rejected cells never reach the driver. Drift can return no partial read. Independently recompute loaded record charges and count; close rows before scalar congruence queries.

Order by exact structured `(source_kind, source_id, accounting_version, id)`. Require each source's versions to start at one and remain contiguous. Check canonical proto bytes, record/domain IDs, organisation, source scope, source revision, valid actor/timestamp/review, exact record hash and equality of every outer scalar through the existing COUNT-only `verifyLegacyScalars`. Preserve complete support lists and historical source tuples. No document/opening/evidence/event hydration, writes, revisions or transaction lifecycle belongs to either API. Cancellation at every stage returns no read.

## Integration limits and alternatives

Do not call this scalar discovery or weaken the existing producer fence silently. Its intended future pre-financial-body use requires explicit reservation of this additional decision category after shared source/root/receipt and existing module payload preflights. A future coordinator must account for decision bytes in shared arithmetic; it may not subtract existing purchase base/event charges without proving exact reuse and lifetime semantics. This checkpoint does not implement that coordinator or accept a whole-capture pre-body fence.

Calling `ReadLegacy` would hydrate the bodies needed only after the candidate fence. A normalized support-ref sidecar would require writer/migration/backfill lifecycle work. Returning a caller-selected subset of physical review IDs could hide history. The complete owned decision projection is the smallest useful dependency and leaves existing full readers unchanged.

Current-document/support/current-cash selectors and provisional legacy nominations remain open. A manual-journal version does not prove a body hash. Future provisional admission/suppression must be reconciled exactly against complete full-reader results, and any mismatch must abort the whole capture. No new nomination, suppression, GST rule, contract or migration is part of this dependency; overall Task 5 remains open.
