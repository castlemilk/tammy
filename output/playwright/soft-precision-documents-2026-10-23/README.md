# Documents · Soft Precision comparison

This block uses the Documents panel in approved board 08 from design commit
`19533020422b6ea2f0c77dc3117a051483f9f9ce`. The full, unchanged concept board is
in `reference/08-documents-banking-activity.png`; it is a visual reference, not
application evidence. All before/after PNGs are real local Electron renders
using isolated synthetic records. No real financial records or credentials are
included. The renderer clock remains the existing fixture's 12 May 2024.

## Structural comparison

The baseline has a two-level toolbar, approximately 77px rows, a large review
title, equal-width source/form columns and an unbounded source-text sheet. Only
the review header is visible below the five-row list at 1440 × 1100. Board 08
uses compact file rows and a smaller file header above a wider source area and
narrower extracted-details form.

The change keeps the real list/review hierarchy, reduces local row padding,
uses restrained PDF/image type badges, consolidates filters/search, shortens
pagination while retaining its accessible names, and gives retained source text
a bounded, keyboard-scrollable region. Inputs and actions retain 44px targets.
The review form is narrower on desktop and stacks on smaller windows. Forest
green remains the supported upload/save action colour; warm surfaces, cocoa
type and status tints use existing shared tokens. No decorative art enters the
data table.

The app retains extracted text, not an original-file viewer. It therefore keeps
that distinction explicit and does not imitate the board's page/zoom controls,
document-type selector, original PDF preview or unsupported statuses. Purchase
posting remains a separate, existing panel and action beneath document review.
This is one screen's comparison, not completion of app-wide visual fidelity.

## Journey

`apps/desktop/tests/e2e/support/documents-journey.ts` extends the existing
financial-workspace packaged test. Capture mode runs the Documents slice alone;
ordinary CI runs it after the existing invoice and Overview assertions.

1. Reject an unsupported text upload, then retain three valid native-text PDFs
   and one PNG through the real import control. Together with the workflow's
   existing reviewed source, this produces five real synthetic documents.
2. Select a PDF by keyboard, cancel an edit and restore its retained values.
3. Submit a 257-character supplier. The existing Core limit rejects the save;
   edits remain visible and document version/status remain unchanged.
4. Correct the supplier and explicitly confirm the total, then double-click
   Save review. Verify exactly one version advance and reviewed/read-only state.
   Read actual journal IDs before and after: document review posts no journal.
5. Search, close/reopen the selected review by keyboard, verify focus returns,
   recover from no matches, and open the image's manual-entry state.
6. At 390px and 320px widths under reduced motion, verify page containment,
   visible field focus and keyboard table scrolling. The corrected screen also
   verifies keyboard scrolling of long retained source text.
7. Navigate Overview → back → forward → back and confirm retained document
   count and journals are unchanged.

There is no mocked native service in these journeys. Typed Core calls only read
retained documents and journal IDs to verify outcomes. Component tests use the
existing conveyor fixture separately.

## Observed suggestion bug

The real baseline suggested Total 100.00 from the `Subtotal $100.00` line,
despite the explicit `Total $110.00` line. A focused ingestion regression fails
on the original regex and passes with whole-word label matching, also rejecting
the partial label `Totalled`. This corrects a suggestion; it does not confirm,
post, rebalance or change stored records automatically.

## Evidence and limits

`before-captures.json` records baseline provenance, 12 successful frames and
three retained frames from the initial incorrect test assumption. That attempt
expected unbalanced review amounts to fail; Core accepts those amounts during
review and owns posting checks separately. It is a test correction, not an
application defect. Raw test/process logs are outside the repository.

`preservation.json` verifies all 625 prior tracked output PNGs and 55 archived
output files against the preceding review head. Their bytes remain unchanged.
The static audit retains exactly the same 19 findings (14 existing native
validation forms, four test mock buttons, one CSV resize input); the policy and
exceptions were not weakened. Ordinary packaging and hosted macOS/Windows
Server checks do not establish MAS or Windows 11 release qualification.

See the prior Overview folder's `screen-board-inventory.md` for the finite list
of implemented screens and remaining dedicated comparisons. Draft, merge and
publication holds remain in force.
