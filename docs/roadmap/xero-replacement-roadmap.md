# Tammy → Xero replacement roadmap (Australia)

**Snapshot:** 5 November 2026  
**Status:** Strategic roadmap proposal, not accepted delivery commitments  
**Market:** Australian microbusinesses first, followed by employers, inventory/project-based businesses and accountant practices  
**North star:** A business can safely stop using Xero for a supported profile, independently reconcile its accounts, produce trustworthy reports, preserve records and maintain a working fallback/exit path.

This document plans an *alternative to Xero*, not a replica of Xero's cloud architecture or brand. The companion [259-capability inventory](./xero-feature-inventory-2026-11-05.csv) is the operational feature-parity ledger. Each line has an ID, domain, Xero scope/source, Tammy status, priority, gate and dependency. It is intentionally a **decomposed feature inventory**, not an assertion that Xero lists 259 separate marketed products.

## 1. The strategic choice

**First customer:** Australian sole trader or owner-operated service business, one AUD entity, no employees, no stock, simple GST and basic invoices/bills. They may use an external accountant or ATO portal for actual BAS lodgement. They want reliable desktop records, full ownership of their data, a clean month-end and a practical escape from subscription lock-in.

**First commercial promise, only after the gates below pass:** *Replace the everyday Xero bookkeeping workflow for a supported small Australian business. Keep records locally and export accountant-ready evidence. BAS can be prepared locally, but not lodged from Tammy.*

Do **not** initially promise employee payroll, multi-currency, eCommerce stock, remote multi-user collaboration, live bank feeds, payment gateway settlement or direct ATO filing. Those are later, separately gated capabilities. The public marketing site and its calculators do not prove desktop readiness.

**Differentiators that Xero cannot simply copy into Tammy's roadmap:**
- Local-first, encrypted, offline-capable ledger with optional rather than mandatory network integration.
- Legible provenance for every posted transaction; no silent AI/OCR posting.
- User-owned, verifiable backup/export/restore and transparent rollback from a migration.
- Accountant handoff without requiring the accountant to adopt a new practice platform.
- One financial truth shared by invoice, bills, banks, GST and reports. No isolated invoice-maker detour.

The meaningful competitor is an *already-operating Xero ledger*. UI elegance alone will not persuade a business to risk losing invoices, payment allocations, tax history or reconciliations.

## 2. Research and parity boundary

Evidence was reviewed **5 November 2026**. Major Xero categories, their plan packaging and partner/practice functionality are supported by:
- [Xero AU all features](https://www.xero.com/au/accounting-software/all-features/) and [AU plan comparison](https://www.xero.com/au/pricing-plans/).
- [Xero invoice/bill and bank workflows](https://www.xero.com/au/accounting-software/pay-bills/), [financial reporting](https://www.xero.com/au/accounting-software/run-financial-reports/), [fixed assets](https://www.xero.com/au/accounting-software/manage-fixed-assets/), [multi-currency](https://www.xero.com/au/accounting-software/use-multiple-currencies/) and [tracked inventory](https://central.xero.com/0/article/Track-your-inventory).
- [Xero Partner Hub](https://central.xero.com/0/article/About-Xero-Partner-Hub), [Workpapers](https://blog.xero.com/au/product-updates/xero-workpapers-updates-2026/) and the [AU app ecosystem](https://apps.xero.com/au/).
- [Xero's Australian export guidance](https://central.xero.com/0/article/Export-data-out-of-Xero-AU), [Accounting API](https://developer.xero.com/documentation/api/accounting/overview/) and [import guide](https://central.xero.com/0/article/Import-data-in-to-Xero).

The inventory covers **native Xero** features, **plan-dependent/premium** functions, **partner-practice** tools, **ecosystem** dependencies and **Tammy-only enablers** (for example, encrypted restore and migration acceptance). A third-party app's capability is *not* silently represented as a native Xero capability. Product features and plan bundles evolve; validate against fresh sources before implementation.

## 3. Tammy implementation baseline (not marketing claims)

Source of truth: [Current technical state](../development/tech-state.md), [Invoice builder](../development/invoice-builder.md), [Self-reporting backlog](../development/self-reporting-backlog.md) and the [Core accounting suite design](../superpowers/specs/2026-08-02-core-business-accounting-suite-design.md). Snapshot is **the documented development build on 5 November 2026**, not a newly executed end-to-end test.

| Domain | What is connected in development | Important gap |
| --- | --- | --- |
| Workspace | SQLCipher-backed local workspace, sign-in, recovery-code confirmation, AU organisation and GST basis | Complete restore UX, production credential lifecycle, multi-user permissions and signed release |
| General ledger | AU system chart, add ordinary accounts, balanced manual journals and trial balance | Opening balances, general-ledger drill-down, account update/archive, period and reversal workflows |
| Receivables | Versioned invoice drafts, issued PDFs, issue posts sales/receivables/GST, dated partial/full receipts | Customer register, customer ageing, credit notes, refunds, recurring invoicing, delivery |
| Payables | Reviewed-source purchases, immutable posting and partial payments; basis-aware GST | Supplier/bill register, bill approvals, credits, refunds, matched payments and ageing |
| Banking | Local signed CSV-style import with opening balance and explicit manual reconciliation | OFX/QFX, multi-account handling, bank-to-AR/AP matching, transfers, duplicate detection and complete reconciliation reports |
| Evidence | Local PDF/image intake, native PDF text extraction, document review and retention | Local image OCR, broad attachment linking, backup/restore in the desktop |
| GST/BAS | Local GST workpapers, provenance, PDF/CSV exports; some BAS preparation functions | Full cross-domain coverage, official lodgement, amendments and declared compliance outcome |
| Reporting | Trial balance, income/balance overview, selected PDF/CSV income exports and activity timeline | Full P&L, Balance Sheet, cash-flow statement, aged subledgers, GL drill-down and report packs |
| Integration | Opt-in authenticated local API, read MCP resources and staged invoice proposals | Bank feeds, payment gateways, public permissioned SDK, outbound connectors |
| Release | Development macOS package and Windows unpacked artifact; Mac App Store project inputs | Signed release, accepted installer/update, complete restore and external approval |

**Important freshness correction:** The latest technical state documents that issuing an invoice **does post** receivables/GST and actual dated receipts **can settle** balances. Do not revert to the earlier public-site description saying those functions are absent. The public site's earlier text and draft branches may lag the current app. This mismatch itself warrants a product-truth update.

**Inventory classification:** `connected_dev` (31), `partial_dev` (34), `core_only` (5), `not_started` (176), `external_gate` (13). Connected means a documented *development* workflow, not audited production availability. Current claims come from the repo, not a feature comparison guess.

## 4. Six release gates

The letters are **acceptance gates, not calendar commitments**. Do not start a dependent capability before its underlying accounting or identity invariants are proven.

| Gate | Outcome | Work | Exit evidence |
| --- | --- | --- | --- |
| **G0 — Trust and durability** | Safe to hold real books | Connected encrypted backup/restore, rollback, audit evidence, credential hardening, release signing, recovery drills | Cold restore and upgrade from a backed-up sample; audit-chain verification; signed clean-build offline packaged E2E |
| **G1 — Complete daily books** | Non-employer service business can operate without Xero for new periods | COA and opening balances, contacts, invoices/receipts/credits, supplier bills/payments/credits, multiple bank accounts/imports/matching, corrected journal/period workflows, full financial reports and GST source consistency | The 10-step owner-operator journey below; balances and aged subledgers reconcile after restart |
| **G2 — Verified Xero exit** | Existing Xero customer can migrate safely | CSV/API import, origin IDs, chart mapping, invoice/bill subledgers, bank history, BAS/attachment snapshots, period cutover, accountant pack, parallel-run comparison, reversal/rollback | Zero unexplained variance across opening TB, AR, AP and bank; reproducible migration; cancelled and failed imports leave state unchanged |
| **G3 — Optional connectivity** | Xero-like convenience without mandatory hosted bookkeeping | Reviewable bank rules, CDR/provider bank feeds, payment integrations, bill inbox, OCR, reminders, multi-user read access, BAS direct lodgement *if approved* | Every adapter opt-in, revocable and consented; offline mode still operates; external acceptance evidence separately retained |
| **G4 — Growing businesses** | Employer and more complex AU businesses can switch | Payroll/STP/Payday Super, expense claims, mileage, inventory, fixed assets, quotes/projects/time, multicurrency, advanced budgets and reports, mobile companion | Domain-specific tax/payroll conformance; asset/stock and FX tie-outs; migration from real but synthetic fixtures |
| **G5 — Practice and ecosystem** | Accounting firms and complex businesses can consolidate | Accountant portfolios, client workpapers, jobs, time billing, Xero Tax-type workflows if approved, curated app platform, multi-device optional sync, deep analytics | Signed role boundaries, practice workflow tests, regulatory gates and support/operational readiness |

**Strategic sequencing:** G0 is mandatory for a public product. G1 + G2 define a real *Xero replacement for the first target segment*. G3 makes it competitive for connected users. G4 expands the eligible business types. G5 is full suite/partner ecosystem ambition. Do not market G3/G4/G5 as present just because an interface can be mocked.

## 5. The first 10 epics, in dependency order

| Order | Epic | Why before the next | Acceptance gate |
| --- | --- | --- | --- |
| **01** | Workspace backup, restore, upgrade safety and audit evidence | Migration or real customer data without a tested restore path is unacceptable | G0 |
| **02** | General ledger drill-down, opening balances, period close/reopen and audited corrections | The subledgers need a complete period/GL contract | G1 |
| **03** | Contacts with customer/supplier roles, merge and import IDs | Required for clean invoices, supplier bills, statements and migration | G1 |
| **04** | Sales lifecycle: credit notes, overpayments, refunds, void/reversal, aged debtors | Issued invoices and receipts alone do not settle real-life exceptions | G1 |
| **05** | Bills lifecycle: supplier register, approvals, credits, reversals, due-date scheduling, aged payables | Turns reviewed-source posting into everyday AP | G1 |
| **06** | Bank import v2: multi-account CSV/OFX/QFX, duplicates, transfers, AR/AP matching and reconciliation report | Eliminates double posting and anchors daily accuracy | G1 |
| **07** | Financial statements: P&L, Balance Sheet, cash flow, GL detail, ageing and GST reconciliation | Accountants need independently checkable outputs | G1 |
| **08** | Xero migration and trial-run verifier | The existing Xero ledger must move across without silent loss | G2 |
| **09** | Accountant handoff and unsupported-situation guardrails | Full self-reporting/ATO approval is not yet available; a clean handoff closes the gap | G2 |
| **10** | Signed public release and end-to-end reliability audit | Converts development features into an operable product, with gated updates/support | G0 + G2 |

This sequence reflects dependencies, not an estimate of duration. Parts of epics 01, 02, 04, 05, 06, 07, 09 and 10 already have backend or UI foundations. Preserve and extend them, do not duplicate completed work.

### Recommended first engineering slice

**Ship a trustworthy cutover foundation, not another calculator or landing-page feature.**

- Connect **encrypted backup and restore** in the Electron UI using the existing Go core, including a manual restore drill into a separate location, audit verification, failed-restore rollback and an explicit proof of the backup's recovery key.
- In parallel, introduce **opening-balance and period-close domain contracts** plus user-facing general ledger detail. Current chart and trial balance features are usable foundations but cannot safely absorb an existing Xero organisation without opening balances.
- Build an executable **fixture oracle**: given a known bank/account/customer/supplier dataset, assert that the trial balance, bank balance, GST workpaper and subledgers agree after application restart.
- Only after those outcomes pass, connect the customer/supplier master and the first full sales/bills exception loops.

**Proposed first PR set:** `tammy-backup-restore-ui`; `tammy-opening-balances-ledger`; `tammy-contacts-register`; `tammy-ar-credits-ageing`; `tammy-ap-bills-credits`; `tammy-bank-match-v2`; `tammy-financial-statements`; `tammy-xero-importer`. PR names are planning identifiers, not branches that already exist.

## 6. The minimum Xero-switch journey (G1 + G2)

Run this on a real packaged Tammy build using **synthetic** AU business records:

1. Create a local encrypted organisation and verify an independently restorable backup.
2. Import a Xero-like chart, customers, suppliers and **as-of opening balances**; obtain a balanced opening TB.
3. Import outstanding customer invoices and bills; reconcile open AR/AP balances against the opening GL.
4. Create a new invoice, issue it, post its receivable and GST, export the customer PDF, and verify immutable history.
5. Post a supplier bill from a reviewed source, record a partial payment and verify cash/non-cash GST.
6. Import bank CSV/OFX/QFX rows, match an invoice receipt, supplier payment and internal transfer; duplicate import changes nothing.
7. Resolve a credit note/overpayment, review ageing, and reconcile the bank's ending balance.
8. Export P&L, Balance Sheet, cash flow, GST details and trial balance, each with drill-through to immutable source entries.
9. Produce a draft BAS workpaper and an accountant handoff pack. Do **not** label it lodged or accepted by ATO.
10. Restart, restore on a clean device, re-run every reconciliation and verify exactly the same values and source IDs.

**Acceptance conditions:** no unexplained monetary variance, no double-posting after replay/import, idempotent import, all documents accessible, no orphaned payment/credit allocations, correct cash vs non-cash GST, protected adjustment path, and recoverable backup with evidence. A complete release also needs installation/update support and a clear unsupported-entity boundary.

## 7. Migration architecture: Xero → Tammy

Xero's [Australian export instructions](https://central.xero.com/0/article/Export-data-out-of-Xero-AU) list chart of accounts, contacts, invoices/bills, fixed assets, bank reconciliation and financial statement exports. This is not one complete whole-company backup; supported file formats and data detail vary by domain.

**Import phases:**

1. **Discover:** accept a local export bundle, inspect filenames, schema versions, row counts, business identity and statement dates without changing the current workspace.
2. **Map:** resolve account codes, GST tax codes, contacts, invoice states, bank accounts and opening balances. Never map unsupported amounts silently to a suspense account.
3. **Preview:** create an immutable migration plan with source row identity, proposed Tammy entity, duplicates, unknown tax codes and excluded fields; estimate financial impact.
4. **Dry-run:** import into a separate encrypted test workspace. Compare TB, AR/AP, bank closing balances, year-to-date P&L and selected GST/BAS periods. Record each difference.
5. **Cutover:** require a signed-off as-of date, verify immutable historical extracts and import outstanding documents/payment allocations exactly once. Require a verified backup and a rollback option.
6. **Post-cutover:** run the next period in parallel, export a comparison evidence pack, verify no source transaction was replayed as a new posting and keep the original Xero exports.

**Data model principles:** stable `source_system`, `source_organisation`, `source_document_id`, `source_revision`, `source_row_hash` and `import_batch_id`; source-native amount and currency; validated mapped AUD amount and tax classification; effective dates; retained source bytes; explicit review decisions. A failed import must be reversible *without mutating previous valid books*.

Xero has an [Accounting API](https://developer.xero.com/documentation/api/accounting/overview/) and exportable CSVs. **Start with user-supplied CSV/PDF bundles** rather than requiring OAuth, Xero API scopes, rate-limit handling, cloud infrastructure or user credentials just to try Tammy. Add a read-only API-assisted import later when the local importer is sound.

## 8. Optional online services without abandoning local-first

Architecture: `Electron UI → authenticated local Go core → explicit adapter boundary`. The Go core owns the ledger, transactions, audit records and local encrypted source documents. A provider adapter receives only the minimal authorised data, cannot write ledger balances directly, and stages external results for user review.

**Bank feeds:** Australia's Consumer Data Right generally requires an accredited recipient or an eligible authorised arrangement. Read the [CDR provider accreditation path](https://www.cdr.gov.au/for-providers/become-accredited-data-recipient) and [ACCC CDR overview](https://www.accc.gov.au/by-industry/banking-and-finance/the-consumer-data-right). Work with a provider/sponsor before considering direct accreditation. Show consent, scope, expiry, revocation and last successful import. No silent feed activation.

**BAS direct filing:** ATO DSP registration, EVTE testing, security evidence and production whitelisting are independent external gates. Follow the [ATO DSP onboarding procedure](https://softwaredevelopers.ato.gov.au/getting_started). The existing SBR simulator is not a lodgement credential or assurance.

**Payroll and STP:** PAYG, STP Phase 2, leave, PAYDAY super contributions and SuperStream correctness form a *separate* regulated expansion. The [ATO STP service](https://softwaredevelopers.ato.gov.au/STP) and [Payday Super project](https://softwaredevelopers.ato.gov.au/PaydaySuper) document production obligations beginning July 2026. Prefer a regulated payroll integration before implementing payroll calculations from scratch.

**Online invoice payments:** integrate a payment service behind explicit consent; treat gateway fees, settlements, refunds, disputes, webhook replay and partial allocations as separate ledger events. An HTML invoice link cannot stand in for a reconciled payment.

**Ecosystem parity:** Xero's 1,000+ partner app ecosystem does not require writing 1,000 native integrations. Deliver a stable, permissioned local API, export formats, adapters and a few high-value verified providers. Separate third-party availability from first-party Tammy capability.

## 9. Score and sequencing method

For *candidate epics*, score from 0–5 on:
- **Switch blocker (30%)** — prevents a target business leaving Xero.
- **Accounting correctness / data safety (25%)** — protects money, history and recovery.
- **Customer frequency and time saved (20%)** — how often the target persona needs it.
- **Dependency leverage (15%)** — unblocks other high-value features.
- **Differentiation (10%)** — local-first advantage or observable UX win.

A high score cannot bypass required dependencies or regulatory approval. Complexity informs slicing and delivery order but is not a reason to put a safety gate below an attractive feature. Record assumptions, scope and measurable exit criteria for each epic.

User validation: collect anonymised descriptions of real workloads from 5–10 Australian non-employer businesses and 2–3 accountants/bookkeepers, without requiring access to their private records. Ask what makes them unwilling to cancel Xero, then test a synthetic version of that workflow. These are research targets, not completed interviews.

## 10. Operational roadmap maintenance

- Inventory path: `docs/roadmap/xero-feature-inventory-2026-11-05.csv`.
- Status vocabulary: `connected_dev`, `partial_dev`, `core_only`, `not_started`, `external_gate`. No “done” status until publicly usable, tested, safe and released.
- Gate vocabulary: `G0`–`G5`. Priority `P0` = G0–G2; `P1` = G3; `P2` = G4; `P3` = G5.
- At each review: pull current Xero feature/release notes, re-read `tech-state.md`, change statuses only with exact connected test evidence, and retain the last-reviewed date. Never infer UI availability from a Go package or test alone.
- Suggested release criterion by capability: versioned domain contract; authenticated API + encrypted persistence; actionable desktop UI; audit and reversal path; tested CSV/API migrations where relevant; packaged E2E; source/amount invariant; accessibility; truthfully disclosed unsupported scenarios; release documentation.
- Keep public site and marketing copy in sync with capabilities **only after** their accepted production release. A draft GitHub PR or a passing site build does not constitute desktop availability.

### Existing plans to reuse

1. [Core business accounting suite specification](../superpowers/specs/2026-08-02-core-business-accounting-suite-design.md) already describes many G1 domain contracts and explicitly excludes G4/G5 scope.
2. [Core accounting implementation programme](../superpowers/plans/2026-08-02-core-accounting-programme.md) contains detailed test-first vertical slices and exit gates. Reuse rather than rewriting accepted work.
3. [Self-reporting backlog](../development/self-reporting-backlog.md) owns complex ATO, year-end and income-tax work. Keep tax filing separate from accounting-ledger completion.
4. [Current technical state](../development/tech-state.md) governs what is actually connected.

## 11. Definition of success

The long-term goal is not “259 green checkmarks” by itself. It is a succession of defensible customer claims:

- **First:** a small AU service business can keep accurate books locally for a new period.
- **Second:** that same business can migrate *from Xero* and exit without missing records.
- **Third:** a typical small business can optionally connect bank/payment/BAS services, with each external outcome verified.
- **Fourth:** employers, stock-carrying and multi-currency businesses can migrate with fully supported domain workflows.
- **Finally:** accounting firms can serve many clients without reverting to Xero for practice, tax or collaboration.

Tammy should earn those claims through reconciliation, recovery and verified data migration, not design mockups or feature-count marketing.

## 12. Issue-level implementation tracker

[Roadmap parent issue #30](https://github.com/castlemilk/tammy/issues/30) owns the acceptance gates and initial dependency-ordered work:

| Issue | First work package |
| --- | --- |
| [#22](https://github.com/castlemilk/tammy/issues/22) | Encrypted backup, restore and audit verification (G0) |
| [#23](https://github.com/castlemilk/tammy/issues/23) | Opening balances, GL drill-down, periods and audited corrections (G1) |
| [#24](https://github.com/castlemilk/tammy/issues/24) | Shared customer/supplier register and deterministic import (G1) |
| [#25](https://github.com/castlemilk/tammy/issues/25) | Credit notes, receipts, refunds and aged receivables (G1) |
| [#26](https://github.com/castlemilk/tammy/issues/26) | Complete AP bill and supplier-credit workflow (G1) |
| [#27](https://github.com/castlemilk/tammy/issues/27) | Bank import formats, duplicates, transfer and AR/AP matching (G1) |
| [#28](https://github.com/castlemilk/tammy/issues/28) | Complete financial statements and source-backed reports (G1) |
| [#29](https://github.com/castlemilk/tammy/issues/29) | Dry-run Xero importer, cutover verification and rollback (G2) |

These are *proposed work packages*, not evidence that those capabilities have been completed. Start with #22, then #23 and #24 where dependencies permit. Keep the parent issue's checkboxes current with real acceptance artifacts, not GitHub PR count.
