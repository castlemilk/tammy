# d04 packaged report visual audit

Result: DONE / CLEAR for rendered report and retained screenshot review. One non-blocking PDF metadata observation is recorded below.

Scope: read-only inspection in the isolated sbr-registration-ready worktree. Both cash/evidence.json and non-cash/evidence.json were read before inspection; each identifies sourceRevision d04a532295e977c3a88b3550e7935cdbf52c6272, sourceDirty false, records.status passed. No source edits, builds, or commits. Scratch rendering only.

All eight PDFs were rendered with pdftoppm at 120 dpi and every page was viewed with view_image. Actual pdfinfo page counts total 14, matching the expected count:

- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/cash/bas.pdf: all pages 1-2 viewed (2 pages).
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/cash/income.pdf: all pages 1-1 viewed (1 pages).
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/cash/stale-bas.pdf: all pages 1-2 viewed (2 pages).
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/cash/regenerated-bas.pdf: all pages 1-2 viewed (2 pages).
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/non-cash/bas.pdf: all pages 1-2 viewed (2 pages).
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/non-cash/income.pdf: all pages 1-1 viewed (1 pages).
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/non-cash/stale-bas.pdf: all pages 1-2 viewed (2 pages).
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/non-cash/regenerated-bas.pdf: all pages 1-2 viewed (2 pages).

All eight retained E2E screenshot files were individually viewed:

- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/cash/initial-bas.png
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/cash/initial-income.png
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/cash/stale-bas.png
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/cash/regenerated-bas.png
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/non-cash/initial-bas.png
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/non-cash/initial-income.png
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/non-cash/stale-bas.png
- /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/reporting-exports-e2e/non-cash/regenerated-bas.png

Findings:

- No clipped or overlapping content, broken table cells, missing rendered glyphs, truncated metadata, source IDs, fingerprints, or hashes. Accented Élodie and literal quotation marks render correctly. Organisation names use full-width rows. Workpaper and source identifiers have ample horizontal space; page footers and numbering are readable.
- Report identity, period/basis, immutable acquisition, workpaper identity/version, source changes, financial revision, balance date and summary-only labels are present and readable. Exact extracted-text presence checks against retained comparison authority passed for 95 provenance values across the eight PDFs; layout was independently checked on the rendered pages.
- Cash BAS starts at G1 80 / GST 5 / credits 0 / payable 5; regeneration retains two dated receipt sources and shows 120 / 7.50 / 0 / 7.50. Excluded reviewed purchases and the need for payment evidence are clearly disclosed, with count 1 initially and 2 after regeneration. No retained purchase sources are claimed.
- Non-cash BAS starts at G1 160 / GST 10 / credits 10 / payable 0; regeneration retains two reviewed purchases, credits 20 and signed net payable -10. The UI describes that last state as GST refundable 10. These representations are coherent. Invoice issue-date basis is visible in the UI.
- Both stale PDFs retain original source IDs/fingerprints and totals, set sources changed true, and visibly explain that saved figures remain until explicit regeneration. Screenshots show the equivalent warning. Regenerated PDFs have new workpaper IDs/fingerprints and no stale warning.
- Income PDFs and UI show revenue excluding GST 170, dated receipts 90 and invoices owing 90 as of 2026-10-03; attributed invoice GST is 5 cash / 10 non-cash. The distinct revenue, payment, balance and tax labels are readable.
- UI screenshots show readable draft/not-lodged and unsupported full-BAS notices, complete visible warnings and totals. Normal viewport scrolling accounts for lower content beyond screenshot bounds; no overlap within the captured viewport.

Non-blocking polish observations:

- pdfinfo displays the BAS PDF Info Title as `GST workpaper â•ﬂ Draft â•ﬂ not lodged` in all six BAS PDFs. The actual page title renders correctly as `GST workpaper — Draft — not lodged`. This appears to be metadata encoding only and does not impair report readability.
- BAS page 1 ends with the Invoice sales sources heading while its table begins on page 2. Both are readable, but keeping the heading with the table would improve pagination.

Limits: this is the packaged-report visual/provenance-presence review only. It does not claim full programme completion, packaged full-E2E gate success, registration acceptance, or independent numeric/artifact-hash acceptance. The parent owns the running gate and numeric/hash audit.

Scratch: /tmp/tammy-report-d04-visual contains the 14 viewed PNG renders and extracted layout text.
