# Tammy visual migration behaviour contract

This records the UI consequences of the existing contracts; README.md,
docs/development/tech-state.md, protobuf contracts and core authorization remain the
sources of financial and security policy. Scope: Soft Precision styling, not a workflow rewrite.

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
| --- | --- | --- | --- | --- |
| Overview projections | features/overview/OverviewScreen, BusinessIncomePanel; reporting/ReportExportActions | Existing typed attention/income responses and report identity/version contract | Posted income beside attention; explicit civil dates; completion focus only if focus was lost | Overview/income/export tests and financial-workspace.spec.ts native journey |
| Navigation | AppShell, Navigation, app-shell/router.ts | Existing product/API contract | Existing protected local routes | app.test.tsx, packaged current-workflows.spec.ts |
| Controls | components/ui/Button, Card, Badge; styles.css | Existing product/API contract | Solid, outline, ghost; native labelled fields | Renderer tests and real screenshots |
| Select/Listbox | Existing native select fields | Existing product/API contract | Operating-system popup and keyboard model | Renderer/journal tests, packaged journey |
| Date | Existing input type=date | Existing product/API contract | Native locale/calendar; contract civil dates | Renderer and packaged workflow assertions |
| Form | Existing feature form and domain codec | Existing product/API contract | Native constraints retained; server errors inline | Unit tests, document invalid-total retry journey |
| Scrollbar | styles.css global baseline; components/HorizontalScrollArea | Existing product/API contract | Labelled keyboard-accessible horizontal table scroll, natural page/form scroll | Narrow viewport and keyboard assertions/screenshots |
| CRUD | Existing named protobuf methods and feature handlers | Existing product/API contract | Explicit review/post/match/reconcile; disabled pending actions | Packaged tests and core invariants |
| Table Selection | Existing document selection and journal detail navigation | Existing product/API contract | Local selection; no bulk scope | Keyboard, detail/back journeys |
| Toast | No global toast system in current app | Existing product/API contract | Inline persistent status/error only | Renderer tests |

The data projections remain bounded by their existing API page sizes. This migration
adds neither unbounded fetching nor new sort/filter persistence claims. A separate
pagination design is needed before extending these projections to larger datasets.

| Operation | Pending | Existing product/API contract | Confirmed outcome | Failure/recovery |
| --- | --- | --- | --- |
| Create workspace | Submit disabled | Existing product/API contract | Recovery code shown once | Keep form, inline error |
| Recovery/sign-in/organisation | Continue disabled | Existing product/API contract | Verified workspace/session/organisation, overview | Existing core contracts and inline error |
| Reopen | Unlock disabled | Existing product/API contract | New signed-in session, overview | Masked credentials, inline error |
| Document import | Retaining disabled | Existing product/API contract | Source retained in local document list | File validation or retain error |
| Document review | Saving disabled | Existing product/API contract | Reviewed status and disabled Review saved | Preserve edits, correct supplier length and retry |
| Add account | Saving disabled | Existing product/API contract | Existing chart reload | Cancel without write; existing error path |
| Journal | Posting disabled | Existing product/API contract | Posted read-only detail | Cancel without write; detail Back returns to list |
| Statement | Import/match/reconcile disabled pending | Existing product/API contract | Explicit matched/reconciled state | Existing row/domain validation; no auto-match |
| GST workpaper | Existing pending state | Existing product/API contract | Draft — not lodged, retained sources | Existing version/tax-period capability gates |

Keep all existing security boundaries, sensitive-value handling and native validation.
Do not add unsupported Undo, automated posting, external sends or production ATO submission.
Evidence is in output/playwright/soft-precision; never commit runtime traces, userData,
private logs, recovery codes or credentials. Screenshots of recovery must mask the code.

## Review workspace interaction contract (2026-10-22)

Sources: `proto/tammy/v1/documents.proto`, `proto/tammy/v1/banking.proto`, the
existing named desktop IPC methods, and the core command/idempotency contracts.
No server/domain capability changes are included.

- Document search/status controls filter only loaded records. Closing review
  returns focus to the source row. Cancel changes restores the retained candidate
  without a write; a failed review preserves edits. The confirmed reviewed record
  is read-only and never presented as a posted accounting entry.
- A statement starts with empty user inputs. Close preserves an in-screen draft;
  Cancel import discards that draft without writing. Route navigation still uses
  the existing ephemeral screen state and does not promise saved drafts.
- Matching requires a reviewed source reference of 1–128 trimmed characters,
  expected line version and command key. The new match form owns this small inline
  validation explicitly; existing native financial/date validation remains.
- Identical failed import/match/review commands retain their idempotency key for
  an unchanged in-screen retry. A synchronous guard blocks repeated submit events.
- Import/match/reconciliation success is announced only after decoding its named
  confirmation. If the subsequent refresh fails, the UI says the action was saved
  and blocks further writes until a successful refresh; it does not invite a
  duplicate import. Cancelling a match writes nothing and restores row focus.
- Reconciliation still requires every imported line to be matched. Global summary
  counts and latest-import balance are distinct from the loaded table projection.

The seven inherited supplementary audit exceptions remain intentional: six
existing forms retain browser validation, and CSV text remains vertically
resizable. No validation or editable-data affordance is weakened to satisfy the
supplementary defaults. The new reference form has explicit inline validation.

## Invoice workspace interaction contract (2026-10-23)

Sources: implemented `invoices.proto`, native invoice accounting commands, and
the existing connected invoice adapter. This pass changes presentation and
recovery affordances; it does not change command semantics or authorization.

- Saved invoices use ten-row client pages over the adapter's existing complete
  cursor-loaded snapshot. Search and Draft/Issued filters are ephemeral local
  view state. Search/filter changes reset paging; refresh clamps it. Clear restores
  search focus. A failed list load is distinct from an empty workspace, and a
  refresh error keeps previously loaded records visible.
- Saving leaves the confirmed draft open. Validation preserves edits and focuses
  the inline error. Opening another record or creating a new draft preserves the
  existing explicit unsaved-replacement confirmation and Keep editing action.
- Issuing requires the existing confirmation, locks the invoice and records its
  native income/GST/receivable entry. It sends nothing to the customer. The issue
  confirmation's cancel action writes nothing.
- Recording money received is a separate explicit command. Cancel writes nothing;
  successful feedback follows the confirmed response. Receipt totals do not add
  income again. Export cancellation saves no file.
- Invoice amount tables have labelled, focusable horizontal scroll containers.
  Notes and addresses use bounded content growth rather than horizontal resizing.
  Narrow layouts preserve natural page scrolling, readable numbers and visible
  keyboard focus, including with reduced motion.

The original seven supplementary audit exceptions remain. The reporting branch
also contains eight native-validation forms and four literal test mock buttons
flagged by that scanner. This pass resolves seven invoice textarea geometry
findings; it preserves financial/security validation and records the remaining
findings in the new evidence report without exclusions.


### Invoice list/detail navigation (2026-10-23)

The first invoice-route visit opens on the saved-record list. Returning from a
related screen restores the cached active editor, including unsaved work. Back to
invoices explicitly returns to the list; route activation does not discard edits.
A replaced authenticated session removes the old cached editor entirely.
New invoice and Open explicitly
enter the editor/detail view. Saving stays with the confirmed invoice. Back returns
to the preserved search, status and page and restores the originating row control
(or New invoice). Unsaved invoice edits use the existing Keep editing / Discard
changes and continue confirmation before returning; cancellation writes nothing.
The editor remains mounted while the list is shown. Payment mutation pending state
disables the new back/new actions. No lifecycle statuses, financial calculations,
receipt contracts or export/issue confirmations change.

### Issued Items/Payments views (2026-10-23)

- An issued record opens on Items, using immutable draft values and the existing
  invoice calculation model. This is a display table rather than disabled editor
  fields. Addresses, notes, payment instructions, private costs and source IDs
  remain available under disclosure. Customer-facing amounts exclude private costs.
- A named native fieldset groups Items/Payments buttons with explicit pressed
  state. Standard Tab, Enter and Space operate them; these are view controls rather
  than an incomplete ARIA tab widget. View changes perform no accounting command.
- The existing accounting controller and payment form stay mounted across view
  changes. An unfinished receipt review is retained. The existing version, validation,
  unchanged retry identity and begin-new-receipt behavior remain authoritative.
- Confirmed receipt history precedes payment review. Cancel writes nothing and
  restores Record payment focus. Opening review focuses Payment date; errors focus
  their alert. Pending mutation disables view changes, back/new and cancellation.
  Successful receipt feedback follows its confirmed native response.
- Legacy issued invoices still require the explicit duplicate/manual-entry review
  before Add to accounts. Loading/error does not manufacture zero received or an
  outstanding balance. Native GST-basis explanation and partial-history counts remain.
- Company-default editing stays mounted independently of Items/Payments. PDF export
  remains explicit and cancellation saves nothing. Draft editing/issue confirmation,
  customer privacy and original PDF output contracts remain unchanged.

### Draft editor and on-screen preview (2026-10-23)

- `InvoiceLineTable` owns its draft density variant. Native date/select ownership,
  labelled decimal input bindings, all private estimates, line limits and canonical
  calculations remain. Private controls stay outside the primary amount columns.
- `InvoiceDraftPreview` receives an explicit public projection from the same
  calculation result. No private costs, margins, source IDs or editor JSON enter
  that projection. It is a local view, with no customer-view route or sending API.
- Editing updates this preview synchronously without saving or issuing. Invalid
  entries clear stale monetary values. Save failure retains both input and preview;
  saved state is displayed only for the current confirmed unchanged record.
- Preview navigation changes only focus/scroll, preserving the route and draft.
  Both editable and preview tables own labelled keyboard-operable horizontal scroll.
  The editor and page retain natural height at 320/390px and with long content.
- Save, explicit issue confirmation and PDF export retain the existing handlers,
  validation, busy gates, optimistic version and privacy contracts. Opening issue
  review focuses Keep as draft; explicit cancellation restores Issue focus. Editing
  dismisses that review while retaining typing focus. Successful issue focuses the
  immutable invoice heading. No automatic send, reminder or customer portal exists.

### Banking review focus and composition (2026-10-23)

- List search/status remains ephemeral and filters only the existing loaded
  projection. Matching does not reset the query. Summary counts retain their
  existing organisation-wide meaning; the latest import balance remains separate.
- Selecting a line focuses its reviewed reference. Selecting that same line again
  keeps the typed reference. Close, Cancel match and unconsumed Escape write
  nothing, discard that match draft and restore its row action, or search when
  the row has been filtered away. Escape during IME composition or a pending
  mutation does not close a form.
- Escape in import follows Close: keep the in-screen draft and return focus to
  Import statement. Explicit Cancel import still clears it. Browser/native
  constraints and the vertically resizable CSV field remain intentional.
- Persistent success/error feedback reuses WorkNotice and receives visible focus
  after an action. Confirmation still requires the existing decoded response;
  post-save refresh failure still blocks another write until refresh succeeds.
- The financial handlers, command keys, expected versions, loaded page size,
  reconciliation gate and financial/security boundaries are unchanged. Matching
  records a reviewed string only; no document link, journal or payment is created.
