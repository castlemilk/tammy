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
- Don't turn planned bank feeds or stock tracking into visible capabilities.
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
