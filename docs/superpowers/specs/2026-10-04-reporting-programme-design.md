# Income, purchase payments and BAS preparation programme

Date: 2026-10-04. Status: implementation authorised by the user's 2026-10-05 instruction to continue this programme. Delivery remains phased with independent validation before dependent changes.

Execution plans: [income/BAS exports](../plans/2026-10-05-income-bas-exports.md), [purchase payments/GST](../plans/2026-10-05-purchase-payments-gst.md), and [BAS preparation/manual handoff](../plans/2026-10-05-bas-preparation-lifecycle.md). Completion is recorded only after the corresponding tests and packaged acceptance.

Implementation status, 2026-10-06: phase one is validated at clean source `d04a532295e977c3a88b3550e7935cdbf52c6272`, with all six freshly built packaged journeys passing and exact PDF/CSV artifacts independently reconciled and visually inspected. See [export acceptance](../../development/income-bas-exports-acceptance-2026-10-06.md). Phase two purchase contracts are next; purchase storage uses migration 0012, and phase three retains 0013. The programme is not yet complete.

## Delivery order and review boundaries

Deliver three focused changes in the existing accounting style. Each receives its own implementation plan, focused tests and packaged desktop acceptance before the next depends on it.

1. [Income and BAS PDF/CSV exports](2026-10-04-income-bas-exports-design.md): Overview exports the selected income summary; GST & BAS exports the displayed saved workpaper and all its retained source rows. Exports preserve exact amounts, report identity and warnings, use the native save dialog, and do not change the ledger or silently regenerate a report.
2. [Purchase payments and GST credits](2026-10-04-purchase-payments-gst-design.md): a reviewed supplier document gains explicit account allocations and posting, followed by actual dated payments. Payments settle its payable without recognizing an expense again. Eligible cash GST follows the paid extent; non-cash GST is claimed once. Private use, missing evidence, semantic duplicates and existing legacy credits remain explicit review decisions.
3. [BAS preparation, review and manual handoff](2026-10-04-bas-preparation-lifecycle-design.md): confirm the statement period and obligations, collect evidence, review exact workpapers and whole-dollar labels, freeze the reviewed version, export it, record user-asserted external handling, and retain linked revisions and the original.

The first two produce independently useful product behavior. The third integrates their complete retained sources and extends the export renderers to exact frozen BAS versions. Shared source contracts and storage migration numbers are coordinated across the purchase and BAS plans rather than implemented concurrently by separate owners.

Phase two validates purchase accounting, GST attribution, unresolved review states and saved-workpaper exports. Preparation-state controls, freeze blockers and the combined prepared-report journey belong to phase three; phase-two acceptance does not require an unfinished lifecycle.

## Initial BAS scope

The proposed supported statement includes Simpler BAS GST, small or medium PAYG withholding, and PAYG instalments using a notified amount or notified rate without initiating a variation. PAYG inputs use reviewed payroll or ATO-notice evidence. Business income is visible for comparison; it is not silently substituted for PAYG instalment income.

Monthly and quarterly periods are supported where the applicable obligations share the statement period. Applicability and eligibility must be confirmed. Unknown answers, unsupported reporting methods or obligations, unresolved source evidence and unclassified journal tax facts block finalisation while remaining visible in draft review.

This scope extends the app's existing GST calculation. A complete local preparation workflow does not imply every BAS tax is implemented. Annual/mixed-cadence statements, large withholding, variations, FBT, WET, LCT and fuel tax credits require separate rules and remain explicitly unsupported in this release.

## UI and accounting acceptance

Overview receives two export actions. Documents receives an Accounting section with **Add purchase to accounts**, **Record payment**, retained allocations and payment history. GST & BAS becomes a period-led workbench with **Prepare**, **Review**, **Freeze**, **Export** and **External record** stages. Keep the restrained table-led design, source drill-down and specific corrective errors.

Validate actual persisted ledger and BAS amounts, not only displayed totals. The combined packaged journey uses an issued invoice and partial receipt, a $110 purchase with $10 eligible GST paid as two $55 payments across periods, reviewed PAYG evidence, all real PDF/CSV exports, restart, stale-source rejection, a frozen report and its linked revision. Parse exported artifacts independently and reconcile them to core records. Existing invoice/template and SBR journeys remain part of final regression.

The feature does not send a BAS to the ATO. Its external records are visibly user-recorded and unverified. Saving, exporting, freezing, recording external lodgement and recording payment are separate events.

## Alternatives considered

A single broad implementation would couple reports, purchase journals and tax lifecycle changes before any could be validated. A GST-only preparation flow would be smaller but leave common PAYG obligations outside the requested workflow. The proposed phased delivery makes each feature usable and testable, while the supported GST/PAYG profile gives the preparation flow a concrete and truthful boundary.
