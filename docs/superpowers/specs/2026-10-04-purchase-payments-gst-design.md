# Purchase payments and GST credits

Date: 2026-10-04. Status: proposed design; detailed approval and implementation plan are pending. This document authorizes no implementation by itself.

## Outcome and scope

A reviewed supplier document can be explicitly added to accounts, paid in one or more dated payments, and traced from its retained original through the ledger to BAS purchase credits. Paying a purchase settles its payable; it does not recognize the expense again. Cash GST is attributed only to the paid extent and supported evidence. Non-cash GST is attributed once from the posted purchase, subject to evidence review.

This is the purchase part of the requested local exports, purchase payments and full BAS preparation workflow. It supports invoice-first purchases in AUD, expense or asset allocations, partial payments, reviewed GST eligibility, retained payment history and local BAS provenance. Supplier registers, advances, credits, refunds, general settlement allocation, bank feeds and automatic bank matching are outside this slice. Existing supplier opening balances are not silently converted into new expense postings.

The recommended approach is a small typed purchase workflow using the existing command coordinator, transaction-scoped repositories, journal posting, period guards and revision tracking. A reporting-only paid flag would leave the ledger and BAS unreconciled. Implementing the older complete supplier/settlement plan would add substantially more work than these requested capabilities require.

This phase exposes complete purchase review states and correct saved GST workpapers and exports. The following BAS lifecycle phase consumes those states to enforce preparation/freeze blockers and adds its preparation checklist. Purchase acceptance can pass before those lifecycle controls exist; the combined prepared-report journey is validated in that later phase.

## Existing behavior that the change must respect

- `documents` retains immutable original bytes and their SHA-256. Review changes a document from `NEEDS_REVIEW` to `REVIEWED`, increments its version once and then freezes the reviewed fields. There is no corrected-review or supplier credit workflow today.
- The review currently accepts nonnegative subtotal, GST and total independently. Purchase posting must add arithmetic, date, evidence, eligibility and account checks; a reviewed document is not itself permission to claim all its GST.
- Identical file bytes are rejected per organisation. Two scans or PDFs of the same supplier invoice can still differ in hash, so source-file uniqueness alone is insufficient duplicate protection.
- Current non-cash BAS reads reviewed-document GST directly without an expense or payable journal. Current cash BAS excludes these document credits because purchase payments are absent.
- The chart already protects bank `1000`, payable `2000`, current input GST `1150`, deferred input GST `1160` and evidence suspense `1170`. Ordinary manual posting cannot write these controls.
- Invoice income already uses immutable source snapshots, exact payment allocations, transaction-owned commands and GST-basis history guards. New purchase commands must preserve those invoice behaviors.
- Documents currently expose only the first 200 rows and reject a cursor. Purchases must remain reachable after this limit; aggregate accounting and BAS queries must never use UI page limits.
- `purchase_opening_payables` stores retained conversion inputs. This slice does not provide a settlement path for those opening items. A source already included in an opening conversion or another journal must not be posted again as a new expense.

## Review and posting

The Documents detail view gains an Accounting section below the reviewed details. Saving the extraction review remains separate from **Add purchase to accounts**.

Posting shows the reviewed supplier, invoice number, invoice date, subtotal, supplier GST and gross total. A valid invoice date and consistent `subtotal + supplier GST = gross` are required. The server loads the document within the requested organisation, checks its reviewed version and snapshots its deterministic reviewed content, original hash and version. It does not trust a renderer-supplied document copy.

The user supplies ordered account allocations covering the complete gross amount. Each allocation records:

- active ordinary account, gross amount and the portion of supplier GST represented by that allocation;
- business or private use and a capital, non-capital or excluded BAS purchase classification;
- explicitly reviewed claimable GST, bounded by that allocation's supplier GST;
- evidence and eligibility confirmation, with a reason when supplier GST is partly or wholly noncreditable.

Retain each allocation's reviewed cash-flow classification as well as its BAS classification. Operating expenses, investing assets and financing/private allocations use an explicit supported classification consistent with the selected account. Reject unspecified or inappropriate classifications rather than inheriting the bank account's default for every payment.

Allocation gross totals must equal the document gross, and allocation supplier GST totals must equal its reviewed supplier GST. Private allocations have zero claimable GST and use an explicitly selected appropriate ordinary equity, drawing or receivable account. They are not automatically booked as business expenses. Business allocations use an appropriate ordinary expense or asset account; their noncreditable GST remains in that expense or asset cost. Protected controls, cash accounts, archived accounts, cross-organisation accounts and inappropriate account types are rejected. Business-use allocations and the credit decision are retained; a percentage or OCR value alone cannot authorize a claim.

For the full payable, posting debits each selected account by its gross allocation less claimable GST, debits an input GST control for total claimable GST, and credits accounts payable by total gross. Eligible cash GST starts in `1160`. Eligible non-cash GST with ready evidence goes to `1150`; unresolved evidence goes to `1170`. Zero claimable GST is allowed. A zero-value document may retain an accounting snapshot without a zero journal and cannot receive a positive payment.

GST-bearing purchases require the organisation's reviewed GST basis and retained profile history. Reuse `CheckGSTBasisHistory` and the retained profile-version checks. This design does not enable retrospective basis changes or weaken existing invoice guards.

One posting is allowed per document. A semantic duplicate check uses supplier identity where present, otherwise normalized supplier name, together with invoice number, and presents matching retained purchases. A known duplicate cannot create a second payable or credit. A false match can proceed only through an explicit distinct-transaction confirmation with a retained reason; the check must not silently treat another file hash as another purchase. Posting also requires confirmation that the purchase has not already been accounted for or claimed elsewhere. Sources linked to existing accounting/opening provenance remain blocked from new posting until reviewed rather than guessed.

Posted snapshots and payment records remain immutable. This slice does not reopen document review or allow generic journal reversal of a purchase-owned posting. Corrections requiring supplier credits or replacement postings remain a visible unsupported operation; changing evidence is not a way to change the supplier amounts or business-use allocation.

## Payments and balanced example

**Record payment** appears only after successful posting. It takes actual payment date, positive AUD amount, an active cash or cash-equivalent asset account, reference and expected accounting version. Payment date must be on or after the supplier invoice date. Advances and deposits before an invoice are rejected with an explanatory message; moving the entered date to make the payment pass is not offered.

The server calculates outstanding balance from every retained payment, rejects overpayment, and posts debit payable / credit bank. For cash GST it also releases the newly paid portion of eligible GST from deferred to current when supported by ready evidence. When evidence is unresolved, it moves that paid eligible amount from deferred to evidence suspense. Non-cash payments never create another BAS credit or a second expense.

Use cumulative exact integer allocations over the immutable purchase allocations, following the existing invoice allocation model. For each payment, retain the difference between cumulative allocations before and after it. This conserves cents, is monotone, bounds credit by paid eligible GST and releases the final residual on full settlement. Large values must not use floating point or overflowing products. Capital and non-capital payment amounts must follow the same allocation history.

Persist the cash-flow facts required by `JournalRepository` in the same transaction. Purchase posting lines, payable lines and GST-control release lines are `NONCASH`. The payment's bank-credit line has signed negative operating/investing/financing components derived from those same retained gross allocation deltas; their sum equals the bank credit exactly. Mixed payments remain mixed in cash-flow projections instead of being wholly operating because the selected bank account has that default classification.

For a wholly business purchase of $110 including $10 eligible GST, under cash GST with ready evidence:

| Event | Balanced journal | Purchase BAS credit |
| --- | --- | --- |
| Add purchase to accounts | Dr expense $100; Dr deferred input GST $10; Cr payable $110 | $0 |
| Pay $55 | Dr payable $55; Cr bank $55; Dr current input GST $5; Cr deferred input GST $5 | $5 in the payment's supported claim period |
| Pay the remaining $55 | Dr payable $55; Cr bank $55; Dr current input GST $5; Cr deferred input GST $5 | $5 in that payment's supported claim period |

After settlement, payable and deferred GST are zero, expense remains $100, bank decreased $110 and current input GST is $10. Under non-cash GST with ready evidence, posting instead debits current input GST $10 and contributes one $10 purchase credit; both payments only settle the payable. A 50% private split uses explicit private and business accounts and cannot claim the private GST.

Payment entry does not execute a bank transfer. Existing statement matching only stores a textual reference and does not prove settlement, account ownership or a ledger match. This slice does not infer payments from those references or mark a bank statement reconciled automatically.

## Tax evidence and delayed claims

Record a reviewed evidence classification and its actual held date, linked to a retained document source, revision and hash. Positive GST eligibility needs confirmation of registration, business use and the relevant evidence requirements. A reviewed invoice or receipt is not automatically a valid tax invoice. Any permitted tax-invoice exception must be explicitly identified and supported; it must not be inferred from OCR or a small amount without the relevant eligibility check.

The ordinary ready-evidence path attributes cash credits from actual payments and non-cash credits from the invoice-first purchase. Evidence held on or before the normal attribution event supports this automatic path. If its held date is later than the payment or invoice event, require the explicit timing review below even when that evidence is available by the time the transaction is entered. The held date is an evidence guard, not an invented payment or attribution date. Future-held evidence relative to the server's reporting/review as-of date cannot support a prepared BAS.

Late evidence needs an explicit review. Keep the original payment date and every previously saved BAS snapshot. Paid eligible GST without resolved evidence remains in `1170` and is shown as **Needs GST evidence/claim-period review**. `RecordPurchaseTaxEvidence` appends the evidence event; it does not automatically backdate a BAS credit. `AttributePurchaseGst` requires an explicitly selected open claim period, evidence held by the preparation/review as-of date, eligible paid extent for cash purchases, the unclaimed retained amount, and a reviewed timing reason. The command creates immutable attribution sources and the corresponding `1170 → 1150` release journal. It cannot exceed eligible GST, claim the same source twice, change the original date, or bypass a closed period.

Mechanical timing limits cannot be waived by confirmation: each cash source's actual payment date must be on or before the selected claim-period end, and the invoice date must be on or before that end for this invoice-first non-cash slice. The command carries a suspense-release posting date within the selected open claim period, on or after every selected source's normal attribution event. The evidence-held date remains separate and must be available by the explicitly confirmed preparation/review as-of date. Reject an earlier period or release date even when evidence and timing confirmation are otherwise valid. Enforce a determinable ordinary claim time limit from retained dates; unsupported exceptions or an indeterminable limit require review rather than a bypass.

Claim timing may depend on when a tax invoice is held at lodgment, rather than simply its calendar date. The normal payment date, invoice date and evidence-held date must therefore remain separate fields. This slice must not implement a guessed `max(payment date, held date)` tax date. The selected claim period requires explicit confirmation that it is an allowable period and within the applicable claim time limit. Unsupported exceptions or a disputed earlier/closed claim period block preparation and remain reviewable; they do not silently become zero credits or a later claim.

An affected BAS cannot be marked prepared while evidence, timing, duplicate or legacy-source issues remain unresolved. Drafts may still display the known totals and unresolved sources. `DeferPurchaseGstClaim` records a typed immutable decision for identified eligible, unclaimed suspense sources, affected period, reason, evidence/timing review and expected accounting version. It cannot defer an already attributed amount or undo an automatic claim. A supported evidence-complete deferral resolves only that period's claim decision; it does not waive missing eligibility, evidence, duplication or timing facts. It leaves GST in suspense and retains an outstanding future-claim record and determinable deadline. Future review lists discover it until attribution; period-specific deferral is never a permanent exclusion. Attribution retains and closes the referenced deferral without changing its history. Expired or disputed entitlement remains visible as unsupported review; this slice does not rewrite an immutable posting to remove it. Restart, staleness and claim-limit checks include these decisions.

## Non-cash compatibility and BAS integration

Migration adds purchase structures but posts no business transactions and rewrites no saved workpaper. Existing saved reviewed-document credits remain historical snapshots.

For a newly generated BAS, a posted purchase replaces that document's legacy direct-credit source. Do not sum the old document GST and the new posting GST. Cash purchase sources come from immutable payment allocations or explicitly reviewed attribution events, selected by claim period. Non-cash purchase sources come from posted eligible purchases or their delayed evidence attribution, never from settlement a second time. GST-suspense amounts remain excluded from claimed 1B until attributed. Capital/non-capital purchase totals and business/private exclusions stay traceable to the same allocation sources.

Previously reviewed unlinked documents remain visible as legacy credit candidates. A non-cash draft must distinguish them from reconciled purchase credits, and **Mark prepared** is blocked until they are reviewed. Resolution is either an explicit new posting where it is valid to recognize the transaction, or an immutable reviewed decision that no new credit belongs in this workpaper because it was previously claimed, duplicated, noncreditable or outside this workpaper, with reason and retained source version/hash. An already accounted expense does not prove that its GST was claimed: an unresolved credit on such an expense still blocks preparation, and it cannot be settled by posting the expense again. Existing ledger data must be inspected before choosing new posting. This is not an automatic expense backfill or permission to silently discard previously claimed credits. A document needing settlement through an existing opening payable is shown as unsupported by this slice rather than posted again.

Cash-basis completeness also scans legacy reviewed documents and opening purchase provenance, including earlier-period invoices that could have been paid in the selected period. Missing new payment rows do not prove an old purchase unpaid. Retain a typed review outcome with supporting payment/claim-period facts: confirmed unpaid through the reporting balance date, already claimed with a retained period/evidence link, duplicate, noncreditable, or evidence-only document linked to an existing owned purchase. A known current-period payment lacking a supported owned settlement/attribution remains an explicit unsupported source. Unknown invoice/payment/claim history blocks complete preparation. Scan all relevant retained rows independently of UI pagination; include undated candidates in review instead of filtering them away. Additional evidence linked to a purchase enriches that source and cannot create another credit candidate. The lifecycle phase consumes these outcomes for cash and non-cash freeze guards.

Purchase BAS sources need source revision and hash, purchase/document identity, attribution or payment identity, invoice date, actual payment date where relevant, tax/claim period, allocated gross, capital/non-capital classification, eligible GST and evidence reference. Extend the purchase source contract additively or add a dedicated typed purchase-source message; do not overload `document_date` with a payment date. Include all these immutable records and unresolved review states in the source fingerprint. New payments, evidence, attribution, deferral and legacy resolution must make a saved draft stale. UI pagination and the current 200-document bound must never truncate credit collection; source bounds must fail visibly rather than return partial totals.

## Proposed contracts and storage

Add a focused `PurchaseService` in `proto/tammy/v1/purchases.proto` and retain the existing `DocumentService` for intake and extraction review. Proposed RPCs are:

| RPC | Required input and result |
| --- | --- |
| `GetPurchaseAccounting` | Authentication, organisation ID and document ID; returns posted source, accounting version, paid/outstanding totals, eligible/attributed/suspense GST and review status. |
| `PostReviewedPurchase` | Command context, organisation ID, document ID, expected reviewed version, complete ordered allocations, reviewed eligibility/evidence and prior-accounting/duplicate decision; returns the immutable accounting snapshot and journal. |
| `RecordPurchasePayment` | Command context, organisation ID, document ID, expected accounting version, actual date, amount, bank account and reference; returns payment, balanced journal and refreshed accounting. |
| `ListPurchasePayments` | Authentication, organisation/document IDs and bounded cursor page; returns stable payment history. Aggregated totals cover all rows. |
| `RecordPurchaseTaxEvidence` | Command context, organisation/document IDs, expected accounting version, retained evidence source, classification, held date and confirmation; returns the immutable evidence event and current review state. |
| `AttributePurchaseGst` | Command context, organisation/document IDs, expected accounting version, explicit claim period, selected unclaimed source references, evidence reference and reviewed timing reason; returns immutable attributions, release journal and refreshed accounting. Amounts are calculated from retained eligible balances. |
| `DeferPurchaseGstClaim` | Command context, organisation/document IDs, expected accounting version, selected eligible unclaimed suspense sources, affected period, evidence/timing decision and reason; returns immutable period-specific deferral and retained future-claim obligation. |
| `ResolveLegacyPurchaseReview` | Command context, organisation ID, typed reviewed-document or retained opening-payable source reference, expected source version/hash, explicit non-posting resolution, supporting payment/claim-period/evidence links and reason; returns the retained decision consumed by BAS preparation. Source ownership is verified even when an opening source has no document. New postings use `PostReviewedPurchase`. |

`ListDocuments` gains stable keyset pagination and useful review/accounting filters; its organisation-bound cursor is validated. Queries require an active local session and scope every document, account, payment and evidence read to the requested organisation. Commands use the same administrator/preparer admission and durable exact replay rules as invoice accounting. A transport uncertainty retries the identical key and payload; changing payload under that key fails, and a new key cannot produce another posting or overpayment.

The next versioned storage migration adds these immutable tables (its numeric position is coordinated with the BAS workflow migration):

- `purchase_postings_v1`: document unique, organisation, reviewed version, original and reviewed snapshot hashes, deterministic snapshot bytes, invoice date, captured GST basis/profile version/history watermark, gross and eligible GST, journal and financial revision.
- `purchase_posting_allocations_v1`: purchase/document identity and immutable sequence, account, use/classification, gross, supplier GST, eligible GST and retained eligibility decision.
- `purchase_payments_v1` and `purchase_payment_allocations_v1`: payment identity, organisation/purchase, date, amount, bank, reference, journal, source snapshot/hash and exact ordered allocation deltas.
- `purchase_tax_evidence_v1`: immutable reviewed evidence events and their retained source/hash, held date, classification and confirmation.
- `purchase_gst_attributions_v1`: uniquely claimed purchase/payment allocation portions, eligible amount, explicit period, evidence/reason, deterministic source/hash and any suspense-release journal.
- `purchase_gst_deferrals_v1`: immutable source/amount, affected period, evidence/timing decision, reason, determinable deadline and source hash. Current outstanding status is derived from linked attribution/resolution events, not a rewritten deferral.
- `purchase_legacy_reviews_v1`: immutable typed reviewed-document/opening-payable source reference, retained version/hash, resolution and reason, scoped to the stated workpaper/claim context when period-specific. An opening source need not have a document; its retained opening identity and provenance remain mandatory.

Use foreign keys, organisation-ownership checks, amount checks, uniqueness and no-update/no-delete triggers. Dynamic cumulative paid/claimed limits and snapshot-hash verification remain enforced within the transaction; no mutable summary is the authoritative balance. Accounting version advances on payments and evidence/claim events, including zero-ledger-impact review changes, so stale commands fail.

Extend the closed `OrdinaryOperation` registry and typed command repository set; source records, journal, financial/tax/settlement revisions, audit and command result commit atomically. Reuse the existing `JournalRepository` and typed protected-account selection. SQL already accepts `PAYABLE` and `BANKING` sources; add distinct public purchase/payment journal-source enum values and update journal decoding, validation and source filters so a banking purchase journal is not presented as an invoice receipt. Generic journal reversal must reject purchase-owned journals; otherwise it could leave retained purchase/BAS sources inconsistent.

Relevant integration points are `internal/purchases`, `internal/documents`, `internal/accounting`, `internal/reporting/bas_sources.go`, `internal/app/command.go`, the localproduct routes/composition, generated Go/TypeScript contracts, main core client, a named schema-bound Conveyor purchase module, Documents accounting UI and the BAS preparation checklist. Existing SQLCipher open/migrate/key/lock behavior is preserved, including automatic upgrades of existing workspaces; no plaintext store or second ledger is introduced.

## UI and validation acceptance

The Documents accounting panel shows unposted/posted state, selected account allocations, business/private split, reviewed GST, evidence, paid/outstanding totals and payment history. Its primary actions are **Add purchase to accounts**, **Record payment** and explicit **Review GST evidence** when needed. Posting and payment confirmation show their accounting/BAS effect and authoritative results after commit. Closed-period, unsupported advance, duplicate, insufficient evidence, stale version and basis-history failures explain the corrective action while preserving entered values. Pending retries must not create another command key automatically.

The following BAS lifecycle phase links unresolved purchase sources to this panel through its checklist and enforces preparation/freeze eligibility. This phase makes those review states available and reconciles draft totals, saved-workpaper exports and ledger projections. No label describes a recorded payment as a completed bank transfer or a preparation status as ATO lodgment.

Required tests, implemented as meaningful RED → GREEN cycles after design approval:

1. The $110/$10 cash example above, split across two BAS periods: expense once, bank/payable/deferred/current reconciliation, $5 then $5 credits, full-payment residual conservation and restart persistence.
2. The same non-cash purchase: one $10 posting credit, neither $55 payment claims another credit, posted document is not also counted through the legacy branch.
3. Mixed taxable/noncreditable/private/capital allocations; private GST excluded, business noncreditable GST remains in cost, account totals and BAS purchase classifications conserve the reviewed gross. Actual cash-flow facts and projections conserve each signed payment across retained operating/investing/financing classifications.
4. Cent-sized and large partial payments, different payment partitions and more than 200 retained payments; exact monotone allocations, no overflow, correct all-row totals and paginated history.
5. Wrong organisation, protected/wrong-type/archived account, inconsistent reviewed amounts, unreviewed/stale source, invalid date, advance, zero/negative/foreign-currency/overpayment and changed-key replay are rejected without partial writes.
6. Exact command replay and lost-response retry; one posting/payment/journal/revision/audit effect, expected-version conflict across concurrent attempts, and transaction rollback after an injected failure.
7. GST-basis changed or incomplete retained profile history, closed invoice/payment/claim period and generic reversal of purchase journals fail without weakening invoice accounting tests.
8. Missing, future or late evidence: real payment date preserved, paid GST moves to suspense, no guessed tax date/1B, explicit supported claim-period review releases once, earlier claim periods/release dates and expired or unknown limits are rejected, and the old saved draft becomes stale. Supported deferral survives restart, resolves only the affected period's decision, remains discoverable, and can be attributed once without duplicate claiming; unresolved states remain available to later preparation guards.
9. Same-file duplicate and different-file semantic duplicate; explicit distinct-transaction confirmation is retained, and already accounted/opening sources cannot silently generate another expense or payable.
10. More than 200 reviewed documents and older outstanding purchases remain reachable; BAS selection includes payment sources for invoices from previous periods, respects organisation scoping and fails clearly at a source bound. A prior-quarter legacy invoice paid this quarter produces a supported source or typed unsupported/review outcome; missing new payment rows never mean unpaid. Evidence-only linked documents do not create duplicate candidates.
11. Existing-schema migration with reviewed legacy documents, opening payables, invoice postings/payments and saved BAS data: no automatic ledger changes, historical snapshots retained, legacy issues visible, unresolved/resolved typed review states emitted without double credits. Record unpaid/previously-claimed review for an opening source with no document and reject wrong-organisation or changed-provenance inputs. The lifecycle phase verifies preparation/freeze enforcement from those states.
12. Real local TLS + SQLCipher command integration, unlock/restart, and a packaged desktop E2E for review → explicit posting → two actual payments → cash/non-cash BAS regeneration → saved-workpaper PDF/CSV exports and unresolved review states. Ledger and GST values must be asserted through the real product, not just mocked screen text. The following lifecycle phase separately verifies preparation/freeze blockers and the integrated prepared-report exports.

## Official tax references

The design's paid-extent guard follows [ATO GSTR 2000/13](https://www.ato.gov.au/law/view/document?LocID=%22GST%2FGSTR200013%2FNAT%2FATO%2Ffp24%22&PiT=20241204000001). Eligibility, business-use and tax-invoice review follow the [ATO GST credit guidance](https://www.ato.gov.au/api/public/content/0-1e92db95-a75c-4f4e-a3d4-39f43b1a3b25). Claim deadlines follow the [ATO time-limit guidance](https://www.ato.gov.au/api/public/content/0-a7296cdd-cec8-49dc-b597-1d94ac991a01), with the earliest otherwise eligible period retained independently of the date evidence was obtained. These sources support the claim guards; the software does not treat user-confirmed eligibility or timing as an automatic tax ruling.

Detailed approval must confirm the explicit posting/payment flow, private account allocations, evidence/claim-period review and legacy non-cash preparation block before an implementation plan is executed.
