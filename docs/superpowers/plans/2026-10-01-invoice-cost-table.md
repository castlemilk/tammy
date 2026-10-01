# Invoice cost table implementation plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the approved compact expense-to-invoice editor and leave a usable fictional sample workspace with three reviewed expenses and a $160.50 saved draft.

**Architecture:** Keep draft state, persistence, dirty guards, issuance and PDF export in `InvoicesScreen`. Extract controlled line-table and reviewed-expense presentation components, retaining integer money calculations in `@tammy/invoices/model`. Use existing production IPC commands and the packaged Electron harness to verify persistence and customer PDF output.

**Tech Stack:** React, TypeScript, Tailwind, Vitest/Testing Library, Playwright Electron, Go/SQLCipher and offline Forme PDF rendering.

**Approved spec:** `docs/superpowers/specs/2026-10-01-invoice-cost-table-design.md`.

## Chunk 1: Editor and focused tests

**Files:**
- Modify: `apps/desktop/src/app/features/invoices/invoices-screen.tsx`.
- Create: focused table components and any shared editor types in the same feature directory.
- Test: `apps/desktop/src/app/features/invoices/invoices-screen.test.tsx`.

- [x] Add failing renderer tests for named semantic tables, batch selection, search, exact markup, linked-source display and live totals.
- [x] Add failing tests for duplicate prevention, re-selection after removal, atomic 50-line limit, preserved selection on invalid markup, empty/loading/error states, refresh preserving edits, unknown costs and issued read-only behavior.
- [x] Run `rtk proxy mise exec -- pnpm --filter @tammy/desktop exec vitest run src/app/features/invoices` and confirm failures correspond to absent behavior.
- [x] Implement the controlled line table and reviewed-expense table in Tammy's existing warm, forest-green accounting style. Preserve accessible field names, raw partial input and existing actions.
- [x] Import selected expenses atomically: validate all prices and line count before changing the draft; use existing default selling GST treatment; preserve source IDs and private costs.
- [x] Reconcile selections on refresh, exclude linked expenses and preserve invoice edits.
- [x] Repeat renderer tests; run desktop typecheck and changed-file Biome checks.

## Chunk 2: Production acceptance and demo

**Files:**
- Modify: `apps/desktop/tests/e2e/current-workflows.spec.ts`.
- Update: `docs/development/invoice-builder.md`.
- Retain demo artifacts: `/Users/benebsworth/projects/tammy/output/tammy-invoice-demo/`.

- [x] Extend real packaged acceptance to import a reviewed expense through the table, set markup and edit its selling price.
- [ ] Assert recalculated invoice totals, saved edits, draft/issued PDF decoded amounts, excluded private costs and source IDs, and persistence across restart.
- [ ] Capture the filled screen at normal desktop size and 1024×700, checking that horizontal table overflow stays inside the table.
- [ ] Build once with `rtk proxy mise exec -- pnpm desktop:package`.
- [ ] Run `rtk proxy mise exec -- pnpm --dir apps/desktop e2e -- current-workflows.spec.ts --project=darwin-arm64`; then run the existing packaged gate for regressions.
- [x] Create or reuse the dedicated fictional profile through production workspace/document/invoice commands. Confirm three reviewed expenses and `TEST-2026-001` with $150.00 subtotal, $10.50 GST and $160.50 total survive restart.
- [ ] Document test credentials and opening instructions beside the sample profile; open the completed sample with the rebuilt app.
- [x] Update the invoice builder instructions for selection, markup, live private profit and saved PDF export.

## Chunk 3: Final review

- [x] Review implementation against the approved spec; resolve functional and usability gaps.
- [x] Review code quality and privacy boundaries; verify no database/contract/PDF layout changes were introduced.
- [x] Run invoice engine tests, full desktop renderer/main tests, typecheck and lint. Record actual outcomes, including any unrelated limitations.
- [ ] Inspect final diff and screenshots, preserve user work and report the concrete result with sample access instructions.

## Validation before packaging

The final stable source passed 35 focused invoice renderer tests, all 758 desktop tests across 65 files, 24 invoice model/PDF tests and 32 local API/MCP tests. Desktop, invoice and local API typechecks passed. Changed-file Biome checks passed; repository lint exited successfully with 23 existing warnings and two informational diagnostics outside the feature. Independent spec and quality review found no remaining actionable issues. The packaged verifier requires committed, clean source; commit these validated changes before the final packaged runtime checks.
