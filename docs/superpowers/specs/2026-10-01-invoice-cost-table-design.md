# Invoice expense and cost table design

**Date:** 2026-10-01
**Scope:** Tammy's existing invoice editor and the reviewed expenses used to build it.
**Direction:** The user approved a restrained accounting style with an editable line table, selectable reviewed expenses, live cost/profit/GST totals, and PDF export.

## Outcome

A business owner can see costs and selling prices together, edit an invoice in a compact table, select reviewed expenses to bill, save the draft, and export that saved version as a PDF. The sample workspace contains three expenses and invoice `TEST-2026-001`: $150.00 before GST, $10.50 GST, and $160.50 due.

## Approach

Use the existing Invoices screen as the workspace. This keeps the editable invoice, costs, and export action together and reuses the validated persistence and PDF engine.

Alternatives considered were a separate cost screen (adds navigation and duplicate invoice state) and expanding each line into a card (retains the current difficulty comparing amounts). A table provides the clearest comparison for this workflow.

## Invoice line table

Replace the separate line cards with one semantic table named **Invoice line items**. Columns are description, quantity, unit cost, selling price, GST treatment, line total, and a remove action. Unit cost is labelled private. Selling prices exclude GST; line totals include it. The table uses existing warm surfaces, forest green actions, subtle borders, clear focus outlines, and aligned tabular numbers.

- Description, quantity, cost, price, and GST treatment remain editable using the existing exact decimal rules and bounds.
- Keep existing accessible field names (`Description 1`, `Quantity 1`, `Unit cost 1`, `Unit price 1`, `GST treatment 1`) and remove actions.
- Show a source indication beneath the description for a linked expense; show **Manual line** for a manual item.
- Allow adding and removing rows; keep at least one row and the existing maximum of 50.
- Partial invalid input remains visible. Display an em dash for unavailable calculated amounts and a useful validation error on save.
- Issued invoices display the same table with disabled editing and no adding or removing expenses.
- At narrow widths, contain horizontal scrolling within the table. The rest of the screen and actions must fit the window. Inputs remain usable by keyboard.

## Reviewed expense table

Replace the single expense dropdown with a semantic table named **Reviewed expenses**. Each row shows a selection checkbox, description, net cost, source GST, and gross cost. A simple labelled search filters by description. Display a truthful empty state when no reviewed expenses exist and a different message when a search has no matches.

The user can select several rows, enter one markup percentage, and choose **Add selected expenses**. Show the selected count and net cost beside the action. Each selected expense becomes a quantity-one invoice line with its source reference, net cost, and selling price including the explicit markup. Replace the untouched first blank line when appropriate. Clear selection after adding. An expense already linked to this invoice is marked **Added** and cannot be selected again; it becomes selectable again if its line is removed. Applying a selection must be atomic: invalid markup or exceeding the line limit adds no rows and preserves selection.

Adding an expense does not establish its sale's GST treatment. Use the existing default for the invoice's registration status, then prompt the user to review selling price and GST. Source GST is visible for context. Editing unit cost changes the private invoice estimate; the original reviewed document and ledger remain unchanged. This distinction is explained once beneath the tables.

Refresh reloads reviewed expenses without replacing invoice edits. Reconcile selection to still available, unlinked IDs. Loading or failure must not appear as a successful empty result; show progress or a retryable error while retaining previously loaded rows and manual editing.

## Summary and actions

Keep a readable live summary beside the editor at wide widths and after it at narrow widths. Show subtotal, GST, and a prominent total due. Below, show private cost and profit before GST. Profit remains unknown when any line has an unknown cost; a known zero must be entered as zero.

The existing **Save draft**, **Export PDF**, and **Issue invoice** actions retain their semantics. Export uses the saved invoice and is disabled until changes are saved. Issuing requires explicit confirmation and then locks the invoice. Draft PDFs retain their watermark. Private costs, profit, and source IDs never appear in customer PDFs.

## Implementation boundaries

- `InvoicesScreen` owns the existing editor state, persistence, dirty guards, and actions.
- A focused invoice line table component renders controlled line inputs and receives line edits, add/remove callbacks, computed amounts, and read-only state.
- A focused reviewed expense table component owns search and selection presentation, receiving expenses, selected IDs, loading/busy state, and callbacks. Markup and atomic import remain in the screen or a small pure helper.
- Reuse `@tammy/invoices/model` for all invoice arithmetic and the existing connected API for saving, listing expenses, and PDF export.
- No contract, database, ledger, or PDF layout changes are required.

## Verification

Renderer tests cover visible tables, multi-selection, search and empty states, exact markup, duplicate prevention, line limits with no partial insertion, live totals after editing, unknown cost, refresh preserving edits, and issued read-only behavior. Existing invoice tests continue to cover persistence errors, unsaved replacement, precision validation, and issue/export guards.

The packaged Electron workflow imports reviewed expenses through the table, edits a selling price, saves, exports, and asserts the edited price and total in decoded PDF text. It confirms no private cost or source IDs leaked and retains saved edits after restarting. Inspect the filled table at the normal desktop size and at 1024×700. Run focused tests, type checks, lint, and the existing packaged E2E gate.

The separate demo profile is created through production commands, preserves the three reviewed expense documents and saved draft across restart, and is opened for the user to test. Its credentials are documented with the sample profile.
