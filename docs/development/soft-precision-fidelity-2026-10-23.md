# Invoice reference fidelity — first block

The user rejected token alignment as insufficient visual acceptance. This block
uses the actual board 07 invoice-list pixels from design commit
19533020422b6ea2f0c77dc3117a051483f9f9ce. Board 02 applies to creation, not the list.

| Dimension | Observed mismatch | First correction |
| --- | --- | --- |
| Layout | Saved-record card stacked above a long always-visible editor | List arrival, separate editor/detail with explicit back |
| Hierarchy | Display-sized serif heading, eyebrow, repeated card title | Compact working sans heading, one title, attached controls |
| Colour | Cocoa wordmark and outlined primary action | Forest wordmark and filled forest New invoice |
| Spacing | Two-line records and 78px rows, extra card/header padding | Separate customer column, 56px rows, quiet attached footer |
| Shape/component | Oversized bordered card and loose status rail | Flat ivory working surface, compact bordered/selected filters |
| Illustration | No art in the reference's actual list | No decorative illustration added to numerical data |

The written palette remains authoritative over generated image inconsistencies.
Only actual Draft and Issued lifecycle statuses are used. No sending, stock,
credits, payment gateway, unsupported outstanding projection or synthetic product
capability is added. Receipts remain in the real detail workspace.

Before: unchanged clean packaged source 7aa28d4e, eight real synthetic records
created through the supported UI, four Draft/four Issued, 1440 × 1100 viewport.
The baseline capture journey passes 1/1 in 12.0 seconds without page/console errors.
Existing evidence is never overwritten. After/reference comparison is the next
validation gate; passing tests alone does not establish visual match.

Focused tests pass 74/74 across eight files after updating editor tests to use the
explicit New invoice path. New regressions verify preserved filtered-list focus
and unsaved-back Keep editing/Discard behavior without writes. A regression also
protects late defaults for an untouched explicitly opened new invoice. TypeScript
and lint pass (135 existing warnings and two informational diagnostics).
