# Invoice income and BAS integration

User request: link invoices to business income and automatically include that income at business level for tax reporting and BAS.

## Behavior

Issuing a saved invoice atomically records receivables, sales excluding GST, and GST. Drafts contribute no income. One immutable invoice-to-accounting link prevents duplicate posting without rewriting issued invoice snapshots. Older issued invoices expose a reviewed **Add to accounts** action; read APIs and migration activation do not create historical financial postings. Zero-dollar invoices receive a link without a fabricated zero journal. Closed periods reject posting and preserve the prior document state.

Create or select a valid ordinary revenue account for invoice sales; use the existing protected receivables and GST accounts. Private unit costs and reviewed-expense links remain pricing metadata and are not posted as new expenses. Generic journal reversal must not bypass typed invoice/payment state.

An issued, posted invoice accepts audited **Record payment** entries with actual payment date, AUD amount, bank account and optional reference. Payments cannot predate issue, overpay, alter the original invoice or recognise revenue twice. Accounting version checks and coordinator idempotency prevent stale edits and retry duplicates. Immutable payment allocations use cumulative integer proportions so partial payments sum exactly to the original line gross and GST on final settlement.

## Business reporting

Overview shows business income excluding GST from posted revenue accounts, issued invoice income, payments received and outstanding invoice balances. Quarter/financial-year periods are explicit. Outstanding uses a separate as-of date and includes invoices from older periods, less receipts through that date. Database aggregation covers all records, independently of the 200-invoice display limit. Unposted older invoices are visible as needing accounting review. The same ledger entries are available through Journals and Trial balance as sources for tax preparation; this feature does not claim an implemented company-return workflow.

BAS sales G1 include eligible GST-inclusive taxable, GST-free and input-taxed invoice sales; out-of-scope lines are excluded. Non-cash invoice sales are attributed on the issue date; cash sales use actual recorded payments and allocated GST. Capture GST basis with the posting, preserve sales provenance in workpapers, and offer regeneration when source facts change. Cash-basis reviewed purchases currently have no payment allocations; do not include unsupported document-date credits silently. Surface the missing-payment-evidence limitation and exclude those unsubstantiated credits from a draft. Existing workpaper snapshots retain their saved figures.

The ATO distinguishes cash payment attribution from non-cash invoice/payment attribution: [cash guidance](https://smallbusiness.taxsuperandyou.gov.au/goods-and-services-tax/accounting-for-gst-on-a-cash-basis), [non-cash guidance](https://smallbusiness.taxsuperandyou.gov.au/goods-and-services-tax/accounting-for-gst-on-a-non-cash-basis). Payment-before-invoice deposits, credit notes and refunds need explicit treatment rather than guessed adjustments. Existing profile-change audit records retain no previous basis or effective date. The local profile command auditor does not persist those events. Check retained successful create/update/verification responses for a complete version sequence with the same GST basis, and additionally check any retained basis-change audit events. Any missing historical version or basis change rejects posting and receipts for review. Name-only and verification changes with complete retained history remain allowed. Zero-GST invoices can post without a configured GST basis; positive GST requires a configured basis.

## Architecture

Use the existing invoice command coordinator and caller-owned SQLCipher transaction. Add an invoice accounting port implemented by transaction-scoped accounting repositories. Immutable tables retain posting links, dated payments and their line allocations. Post debit receivables, credit revenue and current GST for non-cash or deferred GST for cash; payments debit bank, credit receivables, and move the cash-attributed GST from deferred to current. Bump ledger/settlement/tax revisions once per command.

Additive invoice RPCs: `GetInvoiceAccounting`, `PostInvoiceIncome`, `RecordInvoicePayment`, `GetBusinessIncomeSummary`. Existing invoice draft/MCP schemas and issue request stay unchanged. Add typed receivable/payment journal source enums. Extend BAS workpapers with sales sources, GST basis, source fingerprint, live staleness and review warnings. Keep errors bounded, authenticated and organisation-scoped.

## Alternatives

A reporting-only sum of invoices could expose sales quickly, but would leave the trial balance and business ledger disconnected. Manual journals would preserve accounting mechanics but keep the duplication burden on users. Atomic typed postings give income, receipts, overview and BAS a shared accounting source and are the selected implementation.

## Verification

Test balanced postings, mixed GST, cash deferred/current accounts, zero amounts, closed-period rollback, legacy linking, organisation isolation, stale versions, replay/concurrency, partial-payment cents, per-receipt line allocation conservation, final settlement, zero-gross lines, overflow-safe proportions at 1e12 cents, and income without private costs. BAS tests cover date boundaries, cash/unpaid/paid cases, exclusions, old snapshots and regeneration. Test root totals beyond 200 invoices and explicit BAS source-limit errors beyond 1,000 rather than silent truncation. Packaged E2E must issue a real invoice, inspect income/journals/trial balance, record a payment, regenerate BAS and verify everything after restart. Review actual screenshots and retain acceptance evidence.
