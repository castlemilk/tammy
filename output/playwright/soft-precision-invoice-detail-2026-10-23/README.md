# Issued invoice detail and receipt composition

This is the second small visual-fidelity block against approved board 07,
`07-sales-item-tracking.png`, at design commit
`19533020422b6ea2f0c77dc3117a051483f9f9ce`. Its middle and right windows are
concept references, not application evidence. The unchanged full reference is
retained in `../soft-precision-fidelity-2026-10-23/reference/`.

## Before capture

Five real, ordinary-packaged application captures were produced by the existing
`financial-workspace.spec.ts` journey, with `TAMMY_INVOICE_DETAIL_CAPTURE=before`.
The journey passed once in 13.2 seconds, with zero retries, page errors or console
errors. Runtime source is the verified clean ordinary package at
`08802fcaf66ed2a21955bf2148d0e5dff26d0d1f`; subsequent navigation qualification
changes at `07599f6415f10b325f2b1577930fff060e5817a8` affect tests and evidence,
not the application bundle. Capture instrumentation is a subsequent local change.

All five images use a 1440 × 1100 viewport, the same fixed renderer clock and one
isolated synthetic invoice, INV-UI-001, for Merri Studio — synthetic. Its native
values are $1,200 subtotal, $120 GST and $1,320 total. A single actual local receipt
of $500 leaves $820 outstanding. The additional $200 payment review is cancelled;
native assertions confirm it writes nothing. No real financial records or runtime
authentication material appear in these screenshots.

| Frame | Actual state |
| --- | --- |
| `06-issued-read-only` | Issued, before receipts |
| `07-payment-review` | Proposed $500 receipt, before cancellation |
| `08-recorded-receipt` | One confirmed $500 receipt |
| `09-review-existing-receipt` | One confirmed receipt; another $200 only proposed |
| `10-issued-items-with-receipt` | Review cancelled; still $500 received/$820 outstanding |

## Pixel review before implementation

The app's current invoice header omits the customer/date hierarchy from the
reference. A large boxed Income & payments section, repeated explanatory copy and
four oversized metrics precede a long disabled editor. The actual item table and
invoice totals are below the first viewport. The payment review appears above
receipt history and is visually joined to the entire accounting card.

The reference instead separates Items and Payments workspaces, uses a compact
customer/date header, presents issued items as plain read-only table cells, places
invoice totals beside received/outstanding amounts, and puts receipt history
before a quiet tinted review form. Filled forest actions, restrained borders and
spacing provide hierarchy without another large enclosing card.

Only implemented capabilities may be shown. The app has Draft/Issued lifecycle
states and real received/outstanding values; the concept's Part-paid lifecycle,
credit notes, receipt evidence attachments, Documents/History tabs and activity
trail do not establish supported features. Those controls will not be copied.
Existing PDF export, explicit legacy accounting review, GST-basis explanation,
receipt validation/version/idempotency rules and internal cost privacy must remain.

After evidence and validation are pending. These before captures do not establish
that the next implementation matches the reference or is qualified for release.

## Source checkpoint before packaging

The compact issued Items/Payments composition is implemented, with the existing
accounting controller, review validation and receipt command handlers retained.
Company-default editing stays mounted independently of view changes. Private cost
and source information is available under disclosure, outside the customer table.

- Focused invoice/navigation checks: 71 passed across five files.
- Full desktop regression after updating the remaining template/history view
  assertions: 1,297 passed across 110 files, zero skips, in 135.75 seconds. The
  earlier run passed 1,295 and identified those two stale assertions; no timeout,
  receipt-history bound, immutable-value or financial check was removed.
- Desktop TypeScript passes. Lint passes with the same 135 warnings/two infos.
- The configured strict source audit retains the same 19 findings, compared by
  file/rule/message: 14 native-validation forms, four literal test mock buttons,
  one CSV textarea resize rule. No exclusions or validation policy change.
- The preceding pushed head 07599f64 has all six hosted jobs passing, including
  both 20-case macOS runs. That is the completed gate before this new UI patch,
  not hosted qualification of this subsequent source.

The source is committed before one ordinary package build and real after capture.
Visual acceptance and native journey results will be recorded after inspecting
those application pixels.
