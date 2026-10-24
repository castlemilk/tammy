# Draft invoice creation — board 02

This small block uses the invoice editor and paper-preview composition on the
right of approved `02-automation-invoices.png`, pinned to design commit
`19533020422b6ea2f0c77dc3117a051483f9f9ce`. The unchanged full reference is in
`reference/`; it is a generated concept, never application evidence.

The reminder, email sending, Customer view and automation controls in that
concept are outside this implemented app workflow. The real app supports saved
draft PDF export, explicit issue confirmation and separately recorded receipts.
This change adds a local on-screen display of existing draft fields; it does not
add a PDF-preview API, a customer portal, delivery or automatic sending.

## Baseline and actual visual gap

Seven real before frames use the same isolated synthetic INV-UI-001 fixture and
fixed renderer clock as the preceding invoice journeys. Desktop viewport is
1440 × 1100; narrow windows are 390/320 × 900. The native journey passes once in
18.0 seconds, zero retries/page/console errors. Instrumentation was committed
before UI edits at `5d87d274d249259b06a6783aa673a1c97a38cc55`.

Runtime is the existing verified clean ordinary package at `9b559fea`, whose
application source is identical to the preceding review head `262a6e77`.
`before-captures.json` binds source/test identities and image hashes. The same
before package is recorded in the previous detail evidence directory. No real
financial records, credentials or recovery material appear in these captures.

The before image stacks a large details fieldset, editable table, expense import,
company defaults, notes, private estimates, totals and actions. The line table
only begins near the bottom of the first desktop viewport. No document preview
is beside the fields. Board 02 instead places a compact editor beside a readable
paper preview with a clear next action. The before issue-confirmation overview
is anchored at the page heading; the opened confirmation lies below the fold.
It is not offered as a focused image of that panel.

## Implemented composition

- A compact editor sits beside a local draft preview at wide desktop sizes; the
  two areas stack in small windows. The page/form keeps natural vertical scrolling.
- The existing line-table owner has a draft variant. Its primary amount columns
  exclude private cost, while an initially open private-estimate disclosure retains
  every cost field, linked-source hint, known-cost/profit total and existing edit
  binding. Supporting expense import and company defaults remain below the main
  working area, with their existing controls and state.
- The preview is built from the same canonical calculation result. Its explicit
  public projection omits private cost, margin, source IDs and embedded editor JSON.
  Invalid edits clear displayed amounts rather than retaining stale totals.
- Saved versus unsaved state is separate from Draft versus Issued. Unknown seller
  name/ABN are identified as pending save, with no invented organisation or contact.
  Save, issue and actual PDF export retain their existing handlers and gates.
- View preview and Back to invoice fields move keyboard focus locally without
  changing the route, saving or issuing. Issue review focuses Keep as draft;
  explicit cancellation returns to Issue, while editing keeps typing focus.

## Verification checkpoint

The full desktop suite passes **1,301/1,301 across 111 files**, zero skips, in
134.02 seconds. The prior focused invoice/template/navigation run passed 101
cases; the subsequent full run also includes the new confirmation-edit focus
regression. TypeScript passes. The single lint failure was formatting of newly
generated evidence metadata; scoped formatting fixes it without value changes.

Strict source audit retains the same 19 findings by file/rule/message: 14 native
validation forms, four literal test mock buttons and the CSV textarea resize rule.
No exclusion or native-validation policy changes. The four save/issue/PDF handlers
are byte-identical to `262a6e77`. No Core, protobuf, credential, signing, permission,
release or tracked-cache change is part of this block. All 563 prior tracked
output PNGs and 55 archived files remain byte-identical.

## Rendered comparison and connected journeys

[Open the before/reference/after comparison](comparison.html). Eleven new real
after frames are bound by `after-captures.json` to the clean ordinary Electron
package at **1ec93d6d372cd704fefc74f1f22d06b48d8fc560**. Package verification
passes seven checks with one expected skip. No runtime source changed after that
build. The capture journey passes once in 23.4 seconds, with zero retries,
page errors or console errors.

Actual wide pixels show invoice fields and editable amounts beside the paper,
where the previous screen only reached line items at the bottom of the first
viewport. Forest actions, cocoa type, an ivory paper, restrained warm total tint
and a soft serif seller identity follow the useful invoice composition in board
02. Manual customer/address/GST fields and private estimates require more room
than the concept's invented contact picker. Those supported controls remain
present. This is a substantial structural correction, not a claim of identical
pixels or app-wide design completion.

At 390 and 320 pixels the preview stacks below the editor. The named, focusable
line scrollers retain tabular values without making the entire document overflow.
Captures deliberately show a horizontal scroll position reached by ArrowRight;
clipped table columns are reachable using that local scroller. Totals remain
outside it. The 320-pixel stress frame shows a long unbroken customer name and
$1,100,000 total without document overflow. These temporary edits are restored
without persistence; the native saved version remains one. View preview and Back
to invoice fields retain the route and move visible focus. The focused issue
frame shows Keep as draft focused; cancellation restores the Issue trigger.

The native BAS lifecycle passes (15.8-second body), and cash/mixed-GST receipts
with restart pass separately (19.5 seconds). The invoice/purchase/BAS case fails
at its **second workspace unlock after restart**, at
`current-workflows.spec.ts:1814`: it remains on Unlock your workspace with the
generic unlock failure instead of reaching Overview. Earlier edit, template,
issue, receipt and BAS assertions completed. This is an unresolved local result;
neither an environmental cause nor a UI regression has been established. Its
original artifacts are retained privately outside Git. No authentication code,
timeouts, financial assertions or retry policy were changed to bypass it.

`validation.json` records both successes and this failure. New hosted checks must
be assessed at the new pushed head; the preceding head's six green checks do not
validate this block. No MAS, Windows 11, production ATO or release qualification
is claimed. The paused local SBR startup diagnostic remains separate.

All 563 earlier tracked output PNGs, the seven before frames and unchanged board
02 reference were rechecked byte-for-byte. All 55 archived files remain protected
by the existing repository checker. `library-delivery.json` records five confirmed
Library uploads made from separate byte-identical delivery copies. No private
runtime logs, workspace files, authentication material or recovery secrets are
included in this evidence directory.
