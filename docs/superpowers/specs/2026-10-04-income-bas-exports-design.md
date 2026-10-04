# Income and BAS exports design

Status: implementation authorised by the user's 2026-10-05 programme continuation instruction; see [execution plan](../plans/2026-10-05-income-bas-exports.md).

## Outcome and sequence

Add local PDF and CSV export actions to the existing Overview business-income panel and GST & BAS workpaper. This is the first of three separately planned changes: exports, typed purchase payments/GST credits, and the BAS preparation lifecycle. Later work can extend the same report renderers to additional retained source metadata and frozen BAS preparation records.

## User experience

Overview offers **Export PDF** and **Export CSV** for the selected income period and balance date. The PDF summarises business revenue excluding GST, invoice income, dated receipts, invoices owing, attributed invoice GST, GST basis and review warnings. The CSV is explicitly an **income summary**, with metric/value/currency/context rows; it is not a transaction listing.

GST & BAS offers the same format actions for the workpaper currently displayed. Export its saved figures and all retained sales and purchase source rows, with period, basis, workpaper identifier/time, stale-source status and warnings. A stale workpaper remains exportable with its warning; export never silently regenerates it. Both formats identify it as a local workpaper, **Draft — not lodged**.

The native save dialog suggests a period-based filename and the selected extension. Cancellation creates no file. The UI preserves the user's reporting dates, displays completion or a specific refresh/render/save error, and never supplies amounts or paths to the writer. Format actions are unavailable while the displayed report is loading or invalid.

## Authoritative read and consistency boundary

Income export requests carry the authenticated organisation, expected displayed organisation version, period, balance date, format and the displayed financial revision. The frontend retains the version from the authoritative `GetOrganisation` response used to display the report context. Main captures an immutable request and the current authenticated session, then re-reads `GetBusinessIncomeSummary`. That RPC already projects all metrics and the financial revision in one SQLCipher read transaction. A revision mismatch requires refresh before export. Do not aggregate income from paginated invoice screens or the latest 200 displayed receipts.

BAS export requests carry organisation, expected displayed organisation version, format and expected workpaper ID/version/period. Main re-reads `GetCurrentBasDraft`, compares that identity, and refuses an unexpected replacement. Its saved figures, complete bounded source rows and live warnings are copied before asynchronous rendering.

For both formats, main reads `GetOrganisation` immediately before and after the transactional report read. Both organisation responses must match the requested ID and expected version. A mismatch during acquisition requires refresh; only a matching immutable organisation/report pair is retained for rendering. The session capture's organisation ID alone is not a profile-version guard. Tests cover a version change before acquisition, between the two organisation reads, and during rendering. After successful acquisition, a later profile update does not rewrite the retained report identity, just as a later ledger write does not rewrite its figures.

Exports use the acquired report snapshot even if later ledger activity occurs. They state the retained revision/acquisition time so their provenance is clear. Session and organisation identity must remain authorised after the dialog and rendering and immediately before final file activation; locking, sign-out or switching workspace cancels the write. No new accounting command, migration or reporting snapshot RPC is required for these standalone exports.

## Rendering and file boundaries

Add pure report renderers in a dedicated reporting package: income PDF/CSV and BAS PDF/CSV. Reuse the existing Forme/pdfcn typography, page header/footer and PDF text checks. Report tables have their own columns and repeat headers across pages. Totals use exact integer AUD cents; render all retained rows, including zero totals and empty-source cases. The supported BAS bound is 1,000 sales plus 1,000 purchase sources. Reject output above 16 MiB instead of truncating it.

BAS CSV uses a fixed rectangular schema with row kinds for metadata, totals, sales, purchases and warnings. Retain available document/invoice/payment IDs, supplier/customer, source dates, exact gross/GST decimals, sales revision/hash, basis, snapshot ID/time and stale status. Do not invent payment references, bank accounts or purchase hashes absent from the current response. The purchase-payment phase will add authoritative purchase metadata to the same schema.

CSV serialization follows RFC 4180 quoting and line endings, preserving Unicode, commas, embedded quotes and newlines. Neutralise text cells that could execute spreadsheet formulas, including leading whitespace/control characters before `=`, `+`, `-` or `@`. Numeric amounts use a validated exact-decimal formatter and are not treated as uncontrolled text. Tests parse output with an independent CSV parser. PDF font preflight rejects unsupported glyphs clearly; CSV remains available without dropping characters.

Reuse the tested invoice export boundary for save-dialog authority, symlink rejection, mode-0600 temporary files, write/fsync and atomic activation. Share file-writing logic only through a narrow validated writer; retain all existing invoice export behavior. Renderer IPC accepts a bounded format and source selection, never PDF bytes, raw file paths, URLs or arbitrary templates. All errors are bounded and contain no credentials or private source-document bodies.

## Integration and tests

Wire dedicated typed export methods through the reporting Conveyor module, production main registration and frontend reporting APIs. Add session-bound actions to the Overview income panel and BAS screen. Existing read RPCs and report calculations remain unchanged.

Meaningful unit and integration coverage verifies exact cents/negative net GST, zero and maximum source counts, repeated PDF headers, unsupported PDF glyphs with Unicode CSV success, malformed dates/currencies/identities, CSV formula strings, size limits, stale revision/workpaper mismatches, cancellation, session changes at each asynchronous boundary, symlinks and temporary-file cleanup.

Packaged acceptance issues an actual invoice, records a partial receipt, selects reporting dates and exports all four real files through controlled native dialogs. Parse the PDF and CSV independently and compare totals, dates, basis, source rows and warnings with the authoritative core records. Include both cash and non-cash BAS attribution, stale saved workpaper export followed by explicit regeneration, cancellation and restart. Preserve the existing invoice/template/PDF and SBR regression journeys. Record clean source revision, artifact hashes, inspected PDF pages/screenshots and final test results.

## Alternatives and boundary

A combined ZIP accountant pack would require unrelated manifest/archive machinery and decisions about mixing periods. Transaction-level income exports would require new complete detail RPCs; the existing accounting panel deliberately bounds receipt display. Start with the requested standalone summary and workpaper exports, which have complete authoritative source data today. This phase does not lodge a BAS, calculate a company tax return or mutate books.
