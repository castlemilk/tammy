# Audit /local activity — Soft Precision, 24 October 2026

Approved reference: board 08’s right-hand activity pane at design commit `19533020422b6ea2f0c77dc3117a051483f9f9ce`. The reference is generated concept art, never application evidence. Baseline packaged journey passed one case /zero skipped /zero retried in 13.7s (15.0s suite). Five immutable baseline frames are retained at 1440×1100 and responsive 900/390/320×1000. All records are isolated synthetic fixtures.

## Actual capability and implementation boundary

This screen reads up to 200 journals, bank statement lines and documents, plus the current GST workpaper. It is a projection of current retained business records, not the immutable audit-event service or a complete action history. The UI does not invent actors, action timestamps, verified chain badges, official tax receipts, old versions, export controls or supported future events.

The implementation moves scope information beside the task, replaces oversized type pills with compact record hierarchy, displays current status from the actual returned enum, and provides keyboard-accessible source/version/reference disclosure. Local search and record-type filtering operate only on loaded records and add no server query or backend mutation. Partial failure retains available families with an explicit incomplete-view notice; empty, unavailable, loading and no-match views remain distinct. Unknown states do not become reviewed/reconciled/draft statuses.

All four authenticated requests, page sizes, retry policy and codecs are byte-identical; hashes are in contract-verification.json. Date conversion/order remain unchanged. The existing exact bigint AUD formatter is reused to fix the pre-existing extreme-value cent loss in this screen only, with signed int64 boundary regressions. No financial calculation, stored amount, audit integrity, retention, permission or security contract changes.

## Focused checks before packaging

Eight tests passed across Audit and shared review primitives, including four-family projection, local filtering without new requests, disclosed references/version, reversed/unknown status, malformed partial source, healthy-empty versus unavailable, disabled signed-out reads and signed int64 exact cents. Typecheck/lint and final native captures are recorded as they complete. Strict configured audit retains exactly the same 19 rule/file findings (14 native-validation policy differences, four test-mock buttons, one CSV resize rule); no exclusions or validation weakening.

## Remaining fidelity debt and ownership

GST/BAS is incrementally closer to board 14, not exact parity. Its final Review still exposes technical “exact snapshot” and rule terminology, and its hierarchy/copy remain more technical than the approved concept. This block records that debt without altering BAS semantics or prior evidence.

The separate `issue/ato-approval-2026-10-23` worktree at `2a75edc8` has active security/toolchain and identity-event changes. It has no edits to this Audit component/styles at the initial check. Those contracts, generated events and backend logging are not integrated here or claimed as qualified by this UI work. Tax mock research is a separate authorized scope and will not be mixed into this visual block.

All 901 prior PNGs and 55 archives remain unchanged. The unresolved ASAR reliability diagnosis remains open; ordinary CI only, with existing bounded diagnostics and no speculative repeat builds. The local macOS startup/unlock/teardown discrepancies are not erased by this baseline pass. Draft, merge and publication holds remain. No MAS, Windows 11 or official ATO qualification is claimed.

Remaining dedicated visual blocks after Audit: workspace/recovery and Settings/Organisation.
