# BAS legacy-decision owner scope correction

Status: accepted, 2026-10-17, after independent review and final-source native race, existing-route TLS, default Core and pinned production-build validation. This corrects one direct review-owner scope omission in the complete census required by the [decision-history design](../specs/2026-10-16-bas-legacy-decision-records-design.md), following the owner-storage audit during [document-state acceptance](2026-10-17-bas-document-states.md).

ROOT owns active Go files, Go/gofmt/tests/builds and commits. Reuse `issue/sbr-registration-ready`; GPT-6 Luna Max agents review or draft only under `/tmp`.

## Boundary and success criteria

The shared decision CTE scopes rows through TEXT owner/reference equality before its invalid-storage checks. A review owner stored as an exact 36-byte BLOB containing the requested UUID can disappear if none of its references supplies a canonical target TEXT owner edge. Include that exact BLOB owner only to reach the existing invalid-owner/storage rejection. Preserve accepted identities, existing reference-owner scope, selected-cell charging, count/byte/history bounds, canonical decoding and caller transaction ownership. No API, schema, financial policy or nomination change belongs here.

This closes the direct review-owner omission only. Purchase physical families, invoice/accounting scopes and complete malformed-reference fanout still require their own native reproductions and module-owned hardening before combined capture acceptance. Task 5 remains open.

## Implementation

- [x] Activate real-writer native controls and corruption-only regressions: a same-byte BLOB review owner with no target TEXT reference must fail aggregate summary without decision/body reads; the guarded read must reject an owner introduced after a fresh empty reservation; a different UUID BLOB owner remains outside scope. Snapshot after deliberate fixture mutations to detect API writes and preserve the authenticated caller transaction.
- [x] Record actual native behavioral RED before production changes. Add one exact 36-byte BLOB-match owner scope arm, without casting accepted cells; existing missing-owner/`IS NOT target` checks reject it. Run the regression GREEN and full focused decision/document-state native race.
- [x] Obtain independent specification and quality review. Resolve verified findings before freezing final source.
- [x] Run complete affected purchase/reporting native race with only the accepted expensive public-posting stress exclusion, existing-route real TLS journeys, all default Core tests and pinned production build; independently verify the binary SHA-256.
- [x] Record exact evidence and remaining boundaries in programme progress; check formatting/scope/whitespace, commit and verify the worktree is clean.

## Evidence

The initial scope risk and proposed three native regressions were drafted read-only by GPT-6 Luna Max. ROOT activated them and recorded actual native behavioral RED: the same-byte BLOB review-owner summary returned an empty successful reservation, and the guarded read after a fresh empty summary returned an empty successful census (`/tmp/tammy-bas-legacy-decision-owner-scope-native-red-2026-10-17.log`, exit 1 in 2.314s). The unrelated BLOB UUID control did not fail.

The single exact-byte owner scope arm makes the existing invalid-owner/storage checks reject that row without normalizing accepted identities. The same native regressions passed in 3.145s (`/tmp/tammy-bas-legacy-decision-owner-scope-native-green-2026-10-17.log`). The drift case checks the exact zero-record sentinel (`legacy decision records`, limit 0) before body scanning. Combined focused native decision/document-state race passed in 41.651s (`/tmp/tammy-bas-legacy-decision-owner-scope-focused-native-final-2026-10-17.log`). Independent GPT-6 Luna Max specification and quality reviews cleared.

Final-source complete affected native race passed in 620.515s for purchases and 25.303s for reporting (`/tmp/tammy-bas-legacy-decision-owner-scope-complete-native-race-final-2026-10-17.log`), excluding only the previously accepted expensive public-posting stress fixture; no owner-scope/decision/document-state/scalar/nomination exclusion. Existing real TLS invoice/BAS and purchase commit/audit-rollback journeys passed in 65.330s (`/tmp/tammy-bas-legacy-decision-owner-scope-real-tls-final-2026-10-17.log`). All default Core packages passed (`/tmp/tammy-bas-legacy-decision-owner-scope-default-core-final-2026-10-17.log`). The pinned SQLCipher 4.15.0 production build passed (`/tmp/tammy-bas-legacy-decision-owner-scope-production-build-final-2026-10-17.log`), with independently verified binary SHA-256 `e5ce517a3b28d976f34136bbc5da83f31b909614fed606a8dcdaa23460dbcc40`. Formatting and scope/whitespace checks passed. These existing-route journeys do not accept a new endpoint or combined producer.
