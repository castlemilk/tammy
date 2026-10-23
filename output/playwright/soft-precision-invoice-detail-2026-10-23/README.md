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

These before captures do not establish that the implementation matches the
reference or is qualified for release.

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

## First actual after attempt

The first ordinary package at clean source `5e8b5bd0` passed seven applicable
package checks, with one Windows-only skip. Its full financial journey passed
once in 13.4 seconds, including keyboard view selection, retained unsubmitted
review, cancel/repeated actions, native balances, export and narrow scrolling.
All seven actual frames are retained unchanged in `attempt-1-after/` with hashes.

Pixel inspection confirms that the items table and all totals now fit in the
first desktop viewport, and recorded receipts precede review. It also identifies
two real colour defects: the selected underline used an unregistered colour
utility, and the review form's opacity utility produced no tint. The correction
uses the existing forest variable and canonical oat background. Native computed
colour assertions are added; no reference image is substituted for the real app.
Narrow overview framing also resets table scroll explicitly before capture.

After these CSS/capture corrections, all 71 focused cases pass again. TypeScript
and lint pass with the same warnings.

## Corrected actual after evidence

Eight final frames in `after/` come from clean ordinary package source
`9b559fea66a4e8821f3719975d0dbfabce0501e4`, with seven applicable package checks
passing and one Windows-only skip. `after-captures.json` records application,
manifest, native Core and image hashes. The capture instrumentation adds a narrow
cancelled review after packaging; no application source changes after that build.
The Core binary hash is unchanged from the before package.

The complete synthetic capture journey passes once in **15.0 seconds**, zero
retries/page/console errors. It additionally checks actual computed forest
underline/oat review colours, Space selection with visible focus, date-entry focus,
retained unsubmitted review, cancel focus restoration, one receipt after repeated
save, PDF cancel/private export, back/forward, reduced motion, document containment
and real keyboard table scrolling at 390/320 px. The extra 320 px receipt review
is cancelled; native balances still show one $500 receipt and $820 outstanding.

Actual pixels were inspected for Items, payment review and the narrow form. The
compact customer/date header, plain item cells, adjacent totals/receipt summary,
forest view underline, three payment metrics, confirmed receipt history and oat
review surface now follow the corresponding board 07 composition. The 320 px form
stacks all fields and actions without page overflow and retains visible focus.
The synthetic invoice has one real $1,200 line rather than inventing the concept's
two lines. Only actual Issued state and received/outstanding values are presented.

[Open the before/reference/after comparison](comparison.html). The two desktop
pairs have matching 1440 × 1100 viewports; the unchanged composite reference is
shown separately and is not a runtime baseline. Five selected app frames were
successfully delivered to Library; confirmed IDs and byte hashes are in
`library-delivery.json`. All 543 previously tracked output PNGs, including the
earlier UI evidence, remain byte-identical. The five new before and seven first
attempt frames are also unchanged, as are all 55 archived files.

## Connected native qualification

Four distinct relevant packaged cases pass against the corrected application:

- Visual/interaction financial journey: 15.0 seconds, as above.
- Complete BAS lifecycle: 14.5 seconds, with receipt, review/freeze/revision and
  immutable export checks retained.
- Invoice/purchase/BAS workflow: 20.9 seconds, including PDF privacy/branding,
  template snapshots, received/outstanding balances and application restart.
- Cash-basis mixed-GST income/receipt/BAS restart: 15.3 seconds total, retaining
  both receipt records, allocated GST, bank/ledger totals and final BAS values.

The first connected run passed BAS but found that the later restart template
helper still expected disabled editor inputs. The corrected helper explicitly
checks issued display values, due date, logo and absence of editing fields. The
next run passed that case and exposed the cash journey's initial-Items assumption
when reading receipt history after restart. It now explicitly opens Payments,
retaining the complete history and native assertions. Only the affected cases
were rerun after each correction; original failure artifacts remain private.
No timeout, retry, financial assertion or runtime behavior is relaxed.

## Limits and remaining visual work

This establishes a representative issued invoice/detail/payment block, not full
application visual acceptance. Draft creation still needs its own board 02 pass;
other connected screens need separate matched comparisons. No unsupported credit
notes, inventory, sending, gateways, evidence attachments or history controls are
added. The strict audit remains non-green with the same 19 findings. The earlier
local macOS 26.5.2 SBR startup failure remains unexplained and paused. Ordinary
local evidence does not qualify MAS, Windows 11 or official ATO operation.
The PR stays draft; no ready, merge, upload or release action is taken.
