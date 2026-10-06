# Income and saved BAS exports acceptance

Validated on 6 October 2026 against the ordinary macOS arm64 package built from clean committed revision `d04a532295e977c3a88b3550e7935cdbf52c6272`. All six packaged end-to-end journeys passed in 5.7 minutes. This completes phase one of the [reporting programme](../superpowers/specs/2026-10-04-reporting-programme-design.md); purchase accounting and the BAS preparation lifecycle remain the next two phases.

## Verified behavior

- Overview exports the selected income summary as PDF or CSV, retaining the income period, independent balance date, financial revision, organisation identity and acquisition time. It is labelled as a summary, without implying an invoice transaction listing.
- GST & BAS exports the displayed saved workpaper, with its ID/version, period, source fingerprint, exact totals, every retained source and warnings. It remains **Draft — not lodged**. A later source change leaves the saved figures and exported sources intact until explicit regeneration; restart retains the regenerated workpaper.
- Native cancellation creates no file. Invalid save targets produce corrective guidance and allow a deliberate retry. Session changes, sign-out, workspace changes, profile drift and stale displayed identities reject exports at their relevant boundaries. Activation uses an authenticated, mode-0600 temporary file, fsync and atomic rename; failure cleans up the temporary file.
- CSV values retain exact integer cents, RFC 4180 quoting, Unicode and formula neutralisation. PDF source IDs, hashes, names and amounts survive independent text extraction. A pinned report font removes only the ligature substitution table to preserve text provenance; original licensed fonts and licence notices are retained.
- All 1,000 sales and 1,000 purchase rows render without truncation. The maximum renderer fixture produces a 203-page, 5,277,707-byte PDF. Output above 16 MiB fails explicitly. Unsupported PDF text receives specific guidance to use CSV, which preserves the original characters.

## Packaged reconciliation

Each cash/non-cash journey performs native PDF/CSV saves, cancellation, invalid-directory failure and retry, export of a stale original, explicit regeneration and reopen after restart. Independent `csv-parse` and `pdfjs-dist` assertions compare every source field against retained authoritative Core responses. A separate audit using `csv-parse` and Poppler checked the resulting files and copied evidence bytes.

| Case | Initial G1 / 1A / 1B / net GST | Regenerated G1 / 1A / 1B / net GST |
| --- | --- | --- |
| Cash | $80.00 / $5.00 / $0.00 / $5.00 | $120.00 / $7.50 / $0.00 / $7.50 |
| Non-cash | $160.00 / $10.00 / $10.00 / $0.00 | $160.00 / $10.00 / $20.00 / -$10.00 |

Both income summaries retain $170.00 revenue excluding GST. Receipts/invoices owing change from $90.00/$90.00 to $135.00/$45.00 after the additional receipt. Stale exports retain the original workpaper identity, rows and amounts; regeneration creates a new identity, which survives restart. Full nanosecond timestamps, organisation versions, source revisions, source hashes, invoice/payment references and formula-prefixed text reconcile.

The audit verified 40 artifact hashes across 12 CSVs, eight PDFs, eight screenshots and 12 Core comparison records. All 20 PDF/CSV files have mode 0600; no cancelled output or temporary export remains. All 14 PDF pages and eight screenshots were visually inspected without clipping, overlap or missing rendered glyphs. The visual review additionally checked exact presence of 95 provenance values.

## Validation

| Check | Observed result |
| --- | --- |
| `task verify:quick` | Passed: 747 repository script tests, 36 desktop script tests, default Go suite, 32 invoice tests, 19 report tests, 32 local API tests and 849 desktop tests across 78 files; one opt-in reproducibility check skipped |
| TypeScript, protobuf and contract gates | All five package typechecks, format/lint/breaking/generation/contracts passed |
| Full `go test -tags tammy_sqlcipher ./services/core/... -count=1` | All packages passed, including identity (86.848s), workspace (115.953s) and reporting (10.636s) |
| Focused identity/workspace SQLCipher race regression, three repetitions | Passed: identity (96.621s), workspace (3.401s) |
| Fresh `task package:e2e` | Six passed in 5.7 minutes: current workflows 16.8s, first run 1.5s, invoice income 11.2s, cash exports 12.6s, non-cash exports 13.0s, SBR simulator 4.7 minutes |
| Package signature, integrity, fuses and resources | Seven passed; Windows-specific check skipped on macOS |
| Independent spec-compliance then code-quality review | No outstanding findings in export changes or identity/workspace fix |
| Lint and whitespace | No errors; 23 pre-existing warnings and two infos; diff check passed |

The packaged SBR regression exposed a real concurrent identity read/write failure. `GetCurrentUser` previously read then upgraded a deferred SQLite transaction while another writer was active. Repository-owned identity operations now share an admission gate, session reads reserve the writer before taking the identity mutex, and workspace mutations reserve the writer before dependent validation. Caller-owned identity methods retain their transaction lock order. This preserves active-session checks, idle expiry, revocation, atomic session touch/audit, optimistic mutations and rollback without adding RPC retries or weakening the UI guard.

Native RED cases reproduced the original busy conflict, stale snapshot revocation races, a writer-admission/sign-out race and premature workspace validation. The final real SQLCipher tests passed repeatedly and under the race detector. Existing workspace lock/unlock/forget/recovery/replay semantics and the complete native suite passed. Security parameters were unchanged.

## Evidence and package identity

- App SHA-256: `2a0df1bf2c1575b644633115e297ca4cbaac5c429bbfc8c734702f1bd821b67f`
- Core SHA-256: `59f04657746d1b303fea7476921987b9581de80d42c7a88c40bbd4e86a0d2950`
- Manifest SHA-256: `f03b07383c60ff56d0d9fd8ab68250d20d245bc4f7880fef59e31408314cc406`

The tested package is `.tmp/electron-builder/ordinary/mac-arm64/Tammy.app`. Artifact copies, per-file hashes, comparison records, `independent-inspection.json` and `visual-review.md` are preserved at `/Users/benebsworth/projects/tammy/output/tammy-income-bas-exports-2026-10-06-d04a5322/`. The older export audit directory is not this build's acceptance evidence.

Logs: `/tmp/tammy-phase1-session-fix-package-e2e.log`, `/tmp/tammy-current-user-gate-verify-final.log`, `/tmp/tammy-current-user-gate-native-final.log`, `/tmp/tammy-session-writer-final-race.log`; RED/GREEN identity logs use `/tmp/tammy-current-user-{red,revocation-red,admission-red,gate-green}.log`, and workspace logs use `/tmp/tammy-workspace-writer-{red,green,semantics}.log`.

## Limits and next phase

PDF rejects combining marks and format-control characters rather than silently altering text; CSV remains available. Poppler displays incorrectly encoded em-dash separators in the BAS PDF document-info title, while rendered headings, status and provenance are correct. The invoice-source heading can end the first page before its table continues on page two. These are non-blocking metadata/pagination polish limitations. The packaged PDF parser emits font diagnostics; independent extracted-text, page-count, provenance and rendered-page checks pass.

This is local income-summary and saved GST-workpaper export support. Cash reviewed-purchase credits still require payment evidence and remain excluded with a warning. Purchase posting, actual payments and reconciled GST sources are phase two, using migration 0012. Complete supported BAS preparation/PAYG/freeze/manual external records are phase three, using migration 0013. This ad-hoc macOS development package establishes no Windows, App Store, registration or production ATO transport result.
