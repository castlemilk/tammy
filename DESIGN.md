---
version: alpha
name: Tammy Soft Precision
description: Warm local accounting with the precision of a financial instrument.
colors:
  primary: '#235C4B'
  background: '#F7F3EA'
  surface: '#FFFCF6'
  foreground: '#302A25'
  muted: '#EEE8DC'
  muted-foreground: '#6C6258'
  border: '#DCD3C5'
  success-soft: '#E7EDE2'
  warning: '#845020'
  warning-soft: '#FBECD6'
  danger: '#A1352B'
  apricot: '#F2BFA5'
  butter: '#F4DC91'
typography:
  sans:
    fontFamily: 'Avenir Next, Segoe UI Variable, Helvetica Neue, sans-serif'
  serif:
    fontFamily: 'Iowan Old Style, Palatino Linotype, Georgia, serif'
  mono:
    fontFamily: 'SFMono-Regular, Cascadia Mono, Roboto Mono, monospace'
rounded:
  control: '12px'
  card: '20px'
  arrival: '28px'
spacing:
  page-max: '1120px'
  page-padding: '32px'
components:
  button: {}
  card: {}
  navigation: {}
  field: {}
  table: {}
---

# Tammy Design System

## Overview

### Creative North Star

A warm studio desk with the precision of a financial instrument. The approved
[Soft Precision pack](https://github.com/castlemilk/tammy/tree/19533020422b6ea2f0c77dc3117a051483f9f9ce/docs/design/soft-precision-2026-10-22)
is the visual reference. Boards 3, 8, 9 and 11 informed this implementation.

### Product context and register

Tammy serves Australian small businesses using a local desktop accounting workspace.
This is a product interface: readable records, explicit actions and trustworthy state
come first. English Australian dates and AUD financial values follow existing contracts.
README.md and docs/development/tech-state.md distinguish connected features from planned
suite work. Generated screens cannot establish new financial or security behaviour.

Runtime token ownership: apps/desktop/src/app/styles.css is canonical. This document
mirrors its accepted palette; shared Button, Badge, Card and AppShell consume the tokens.
There is no generated token export or external font dependency.

## Colors

Oat canvas, ivory paper, cocoa text and forest actions. Pale forest is selection and
success support. Apricot and butter belong to arrival art or supportive surfaces,
never low-contrast body text. Warnings and errors have explicit text as well as colour.
The app uses one light theme; forced-colour mode retains system-operable scrollbars.

## Typography

Use soft system serif display type only for wordmark and major headings, 28–36px,
1.2 line-height. Controls/body use a humanist system sans at 14px; secondary metadata
is at least 12px. Financial tables use lining tabular numerals. Account codes and
technical references may use mono. Dense data never uses display serif type.

## Layout

Spacing follows the approved 4/8/12/16/24/32/48/64px rhythm. Content has a 1120px
maximum width and 32px desktop padding, reduced to 16px on narrow windows. The 216px
navigation rail becomes a labelled-accessible 64px icon rail below 900px. Complex
work panels stack below 1100px. Tables scroll within their own surface; forms and the
page retain natural vertical scrolling. Test 320, 390, 900 and 1440px widths.

## Elevation & Depth

Use warm surface differences and crisp borders. Avoid gradients, texture and art behind
financial data. Overlays may use a subtle shadow; regular work surfaces do not need one.

## Shapes

Controls use 12px corners, main surfaces 20px, arrival surfaces 28px. Compact statuses
may use pills. Keep hit targets at least 44px for primary controls and navigation.

## Components

### Foundational visual states

Default, hover, pressed, visible focus, selected, disabled and pending states use shared
tokens. Focus is a 2px forest outline with a 2px offset. Disabled actions retain a clear
label and native disabled behaviour. App-owned spinners show pending work; never animate
financial values. Do not claim successful persistence before a confirmed response.

### Buttons and actions

Reuse the shared shadcn-derived Button: forest solid for primary actions, bordered ivory
for secondary actions, ghost for supporting navigation. Preserve explicit action labels
and pending disable gates. Cancel closes local account/journal forms without writes.

### Navigation and data display

Keep the existing routes and primary navigation labels. Active navigation uses pale
forest and forest text. The shell describes a local workspace rather than inventing an
organisation name or offering an inert business switcher. A keyboard skip link targets
the main content. Tables remain semantic tables with aligned, unanimated financial data.

### Forms and overlays

Preserve current labelled native controls, native constraint validation, and server/domain
validation for this visual migration. Native select/date popups are intentionally owned by
the operating system. Replacing validation, secret-reveal semantics or financial mutation
contracts requires separate behaviour work. Errors preserve entered values. This pass adds
no destructive dialogs, toasts, credentials, permissions or new external actions.

### Iconography

Reuse Lucide at 1.8px stroke. Labels remain present or accessible when the rail collapses.

### Motion

Use short colour transitions and a restrained button press. Reduced motion disables
transitions and removes spinner animation; it never changes financial values.

### Content and data visualization

Use calm, specific language. Keep Suggested, Reviewed, Matched and Reconciled distinct.
GST remains Draft — not lodged. Local activity is not audit-chain verification. The
approved transparent paper-loop art appears only at arrival/completion, outside tables.

## Do's and Don'ts

- Do reuse semantic tokens and shared controls across connected screens.
- Do verify real packaged screenshots with isolated synthetic data.
- Do preserve core authorization, amounts, tax rules and idempotency.
- Use planned reference screens only for capabilities now implemented and tested.
  Invoice editing and reporting are implemented; bank feeds and stock tracking remain planned.
- Don't add art to financial tables or copy generated financial examples as contracts.

## Review workspace uplift (2026-10-22)

Documents and Banking use a full-width working surface with an attached toolbar.
`components/review-workspace.tsx` owns the shared heading, local search, counted
status filters, loading/empty state and inline feedback. `styles.css` owns
`work-surface`, `work-table`, `work-input` and `work-eyebrow`; these reuse the
established palette and geometry. Filters operate on the explicitly labelled
loaded projection (up to 200), not the whole organisation.

## Invoice workspace uplift (2026-10-23)

Invoices now use the reporting branch's implemented invoice, template, issue,
receipt and PDF contracts. Saved records reuse the review workspace search,
filters and states, with ten-row client pages over the existing complete
cursor-loaded snapshot. Only Draft and Issued are lifecycle statuses. Invoice
totals, private cost estimates and actual receipt/outstanding balances are distinct.

The full-width editor uses shared `work-input` controls. Address and note fields
grow with content within a bounded height; invoice tables own their horizontal
scroll and explicit keyboard focus through `components/HorizontalScrollArea`.
Hidden table headings stay within their
positioned scroll container. Small-window receipt metrics stack before amounts
need to wrap. Root sizing accounts for the vertical scrollbar at a 320px window,
and shared buttons respect their available container width.

Source text and review details sit below the document table in a two-column
workspace, stacking below 1050px. There is no original-file preview API. Reviewed
values are read-only and the confirmed protobuf response updates the record.
Banking keeps import and manual match review as non-modal inline forms with
normal document scrolling. The explicit reference records the user's review;
it does not imply a ledger/document link or automated matching.

The navigation groups existing routes into Workspace, Accounting and Activity.
The forest selected item and route context use the same shell at every route.
The compact rail retains accessible link names. Tables scroll horizontally inside
their surface at narrow widths; forms stack and retain full values.

## Shared styling across connected screens (2026-10-23)

The October 22 pack remains the visual reference for connected features. Shared
`ScreenHeading` and `SurfaceHeading` components give working screens a consistent
hierarchy. Main content has a 1120px working width; tables own horizontal overflow,
while long forms and policy documents retain natural document scrolling. Navigation
uses pale forest selection with forest text, and shows the current business in the
route context. Financial data stays in readable sans and tabular numerals.

The same hierarchy and surfaces extend through purchase review, retained evidence,
invoice templates, reporting exports, RAM credentials and account security forms.
Metadata is at least 12px, narrative copy and inputs use 14px, and standard controls
retain a 44px target. `HorizontalScrollArea` owns named, focusable table regions so
read-only accounting and invoice tables can scroll with the keyboard at narrow widths.

`WorkspaceEntryLayout` applies boards 11 and 13 to workspace setup, recovery and
sign-in: a quiet introduction beside a clear form surface, with business and security
fields grouped by purpose. The two-column layout stacks on narrow windows. Secrets
are masked by default; new secrets get live format and confirmation feedback that
matches Core's Unicode policy. Current credentials keep their own sign-in semantics.

Privacy and support is a complete, readable policy with section navigation, sourced
from the same generated policy content as the public site. Settings uses a concise
summary and an in-app route to the full document. The ABN browser lookup is explicit
and uses the official register; checksum feedback is separate from registry evidence.
These interactions do not change financial, approval or SBR production capabilities.

## Issued invoice composition — board 07 (2026-10-23)

Issued invoices use the board's middle/right composition: a compact invoice ID,
actual Issued badge, customer and civil dates, followed by Items/Payments view
controls. Customer-facing line values are plain read-only cells with tabular
amounts. Invoice totals and confirmed received/outstanding values share a compact
summary; private estimates and source references remain under explicit disclosure.
Company defaults retain their own mounted editor, separate from immutable invoices.

Payments use three compact amount surfaces, a filled forest Record payment action,
confirmed receipt history and a quiet tinted review form. Outstanding uses a soft
butter surface. Only actual Draft/Issued lifecycle states are shown. Credit notes,
evidence attachments and History/Documents controls in the generated concept are
not supported by this screen and are not copied. No illustration belongs behind
these financial tables. Visual acceptance requires the real matched captures;
shared token adoption and passing tests alone do not establish reference fidelity.


## Invoice reference fidelity — board 07 (2026-10-23)

Board 07 is the canonical invoice-list composition. The list is the arrival view;
new/edit/issued work occupies a separate view with an explicit Back to invoices
control. Saved-record filters, search and paging remain mounted and recover focus
on return. The editor is not stacked below the list.

Working invoice headings use the shared ScreenHeading workspace variant: 26px
semibold humanist sans with a short description and no visible eyebrow. Serif
remains the wordmark/arrival/display face. The shared wordmark uses forest; the
invoice workspace uses ivory paper. New invoice is forest solid. The status rail
uses a solid forest selected control, quiet borders and actual Draft/Issued counts.
The table separates number, customer, due date, total and status into single-line
columns with 56px rows. It has no redundant Saved invoices card header. Selected
records use a restrained apricot tint. The written colour tokens remain canonical;
generated shadows/gradients and unsupported statuses are not copied.

Board 02 is a creation reference, not the list target. There is no decorative art
inside board 07's working list; none is added. Receipt balances come from their
verified accounting projection rather than being guessed from invoice totals.

## Draft editor and local preview — board 02 (2026-10-23)

The creation workspace uses a compact editor beside an ivory paper preview on wide
desktop windows, stacking below 1280px. Both retain natural document height. The
existing line-table owner provides a draft density variant: editable selling data
is primary, and private estimates/source hints are grouped in its disclosure.
Expense import and company defaults remain available beneath the main working area.

The paper preview uses public draft fields, canonical rounded amounts, the actual
saved seller identity/logo when known, soft serif identity and readable sans/tabular
data. It is labelled On-screen preview; actual PDF export uses the saved draft.
Saved/unsaved state is explicit and separate from lifecycle. Invalid amounts clear
the preview totals. The concept's reminder, sending and Customer view controls are
not supported and are omitted. No new art is added to the financial data.

## Overview composition — board 01 (2026-10-23)

Overview uses the Today board's compact financial summary beside an attention
queue. Its actual contract is posted business income, invoice income, receipts
and owing balances with explicit reporting/as-of dates. It is not a cash-position
chart, bank-feed status, forecast or automatic matching surface. Document/bank
counts remain separate from BAS lifecycle status and the returned attention list;
an empty additional-item list never declares BAS complete.

The income section and 340px attention column align at wide desktop sizes and
stack below 1100px. The primary income figure uses sans/tabular numerals, with
smaller receipt and balance values below. Existing date inputs, refresh, identity
and export actions remain visible. The arrival header uses the current civil date,
serif display and existing paper-loop art outside the financial work area. No new
shared tokens, primitive variants, dependencies or routes are introduced.

The existing report-export owner restores focus to the initiating action after a
completed/cancelled export only when focus fell back to the page. It preserves
another focused control and clears the target on authority changes. Native BAS
navigation uses the shared forest focus ring. These changes do not alter amounts,
authority checks, expected financial revisions, export arguments or file behavior.

## Banking composition — board 08 (2026-10-23)

The compact statement list is the main work surface. A selected line's non-modal
review form sits beside it from 1380px, with natural page scrolling and stacked
panels on smaller windows. The shared horizontal table scroller retains keyboard
access to every column. Rows target 56px while allowing long names/references to
wrap; controls remain at least 44px. Existing tokens and shared buttons own colour,
focus and geometry. No art, gradients or invented matching suggestions enter data.

The quiet forest reconciliation summary shows only returned global unmatched and
unreconciled counts plus the latest imported statement's closing balance. This is
not a book/cash balance, a computed difference, or totals over the bounded table.
CSV import remains explicit and does not imply a live feed. The concept's filename,
account/period controls and automatic document links have no supported UI contract
here and are omitted. Real matched before/reference/after captures are required.

## Trial balance composition — board 09 (2026-10-23)

The report keeps its authoritative server order and exact bigint figures in one
compact ledger surface. A read-only as-of civil date sits beside the serif heading;
there is no editable-date or export capability on this screen. Debit/credit columns
align in tabular sans, and a restrained forest totals band labels equality only
when both supplied totals exist and agree. Shared WorkState distinguishes loading,
empty and unavailable reads; Refresh repeats the same read without a financial
write. The report uses the existing keyboard scroller with a scoped inset outline.
