# Bounded BAS document states

Status: accepted, 2026-10-17, after independent GPT-6 Luna Max design/specification/quality reviews and final-source focused/affected native race, existing-route TLS, default Core and pinned production-build gates. The [implementation plan](../plans/2026-10-17-bas-document-states.md) records exact evidence. This is a source-owner dependency after bounded legacy decisions in `e2bd38e1`, within [complete source capture](../plans/2026-10-12-bas-complete-source-capture.md); overall Task 5 remains open.

## Owned boundary

Add an aggregate reservation and guarded purchase-owned read of every organisation document's current scalar state. The accepted physical document keys expose version and raw SHA, but omit review status. A complete document-state projection supplies that missing input without selecting document candidates, amounts, extracted/original bytes, postings, payments or event bodies. It includes both `NEEDS_REVIEW` and `REVIEWED`, promoted documents and retained evidence documents; it applies no period, review, root or decision filter and accepts no caller-selected IDs or decision subset.

```go
type BASDocumentStateSummary struct {
    OrganisationID string
    DocumentCount, StateBytes int64
}
type BASDocumentState struct {
    OrganisationID, ID string
    Version uint64
    Status tammyv1.DocumentStatus
    ContentHash []byte
    PostingID *string
}
type BASDocumentStateRead struct {
    Summary BASDocumentStateSummary
    Documents []BASDocumentState
}
SummarizeBASDocumentStates(ctx context.Context, organisationID string) (*BASDocumentStateSummary, error)
ReadBASDocumentStates(ctx context.Context, organisationID string,
    expected BASDocumentStateSummary, maximumDocuments, maximumStateBytes int64) (*BASDocumentStateRead, error)
```

`ContentHash` is the exact document raw `sha256`, not a reviewed-candidate hash. `PostingID` is the exact nullable owning purchase posting identity, not proof of its body/events. Require the document and any posting referencing it to exist under the same organisation. A finite global invalid-link probe must detect owned postings with missing/foreign document owners, foreign postings touching an owned document, and multiple postings for one document, including when no owned document row remains to project. Exact 36-byte BLOB owner IDs with the requested UUID's bytes must reach invalid-storage checks rather than disappear through TEXT equality; they are included only for rejection, never normalized into accepted owners. The same rejection boundary covers a posting's BLOB document reference whose exact bytes identify an owned TEXT document, including a foreign posting that would otherwise evade the strict join. A separate bounded guard-only lookup detects that malformed reference; ordinary data joins retain strict identity comparison. Unrelated foreign documents stay outside this organisation census. Later decision lookup must reject missing source/evidence owners rather than infer valid suppression from absence.

## Reservation and guarded read

Charge each projected row at `512 + UTF-8/blob bytes of id, organisation_id, status, sha256 and nullable posting_id`; `version` is an INTEGER under the fixed allowance. Status charges the exact selected SQL TEXT even though the public value is an enum. The named pool has separate ceilings of 16,384 document rows and 8 MiB. It is a selected-cell charge, not Go heap size. Do not add a 1,000 candidate/root cap to this metadata census. Its allowance is additional to existing physical-key, decision and full-payload reservations until shared reuse/lifetime accounting is proved.

Summary returns COUNT/SUM and finite invalid markers only, with a count sentinel. SQL guards required owner links, TEXT/INTEGER/BLOB storage and positive version. Document/organisation/present-posting identities must be TEXT of exactly 36 UTF-8 bytes; raw hash must be BLOB of exactly 32 bytes; status must be exactly `NEEDS_REVIEW` or `REVIEWED` (at most 12 bytes). A successful read additionally validates canonical domain IDs. Reject negative caller inputs before SQL; zero permits an empty census. Enforce caller/local caps, compare a fresh summary with the caller's comparable expected reservation, and guard every selected cell using SQL cumulative bytes, row number, invalid-state/link markers and a count sentinel before driver copying. A private leading metadata row carries the global invalid-link count even for an empty document set; it has no retained owner cells and does not count as a document. Finite row ordinals preserve document-ID order after rejected identities are masked to NULL. A summary is not an identity/content/currentness fingerprint; equal-count/equal-charge replacement needs the caller's serializable transaction and later exact proof reconciliation.

Order by exact document ID, reject duplicate/non-increasing identities, independently recompute loaded charges/count, and return no partial result on cancellation, drift, corruption or overflow. No writes, financial/source revisions or transaction lifecycle belong to either API. Existing scalar keys and full readers stay unchanged.

## Why this slice

Normal document writers permit only `NEEDS_REVIEW -> REVIEWED`, increment version, and retain immutable raw source bytes/hash. These current owner cells can support a later provisional reviewed-source/evidence comparison; full readers must still verify original bytes, candidate hash and complete admission/suppression equality. This API makes no currentness or eligibility decision itself.

Extending `BASScalarKey` with status would mix owner state into the accepted physical-only contract. Calling `documents.Get` would select financial/candidate fields before their intended fence. A decision-reference-only reader would need a new complete-reference fanout/input contract before this simple missing status could be used. The complete owned document census is the smallest independent dependency.

Modern payment dates and support ownership, linked posting full proofs, opening state/input hashes, current cash/reversal closure, complete legacy nomination and full-reader exact-set reconciliation remain open. An opening conversion hash is not the payable's retained input hash; a journal version is not a body hash. This adds no contract, migration, route, global union/fence or combined producer and completes no overall Task 5 checkbox.
