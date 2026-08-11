# Business and individual self-reporting backlog

**Baseline date:** 11 August 2026  
**Normative design:** [Business and individual self-reporting design](../superpowers/specs/2026-08-11-business-individual-self-reporting-design.md)  
**Current capability source:** [Current technical state](tech-state.md)

## 1. Outcome and prioritisation

This backlog covers the product work required for Tammy to support mainstream Australian self-reporting for small businesses, sole traders, and individuals.

It uses four priorities:

- **P0 — reporting foundation:** required before any report can be called complete.
- **P1 — mainstream complete:** required for the first dependable business and individual self-reporting releases.
- **P2 — expanded obligations:** important for employers and common entity structures after the core reporting lifecycle is proven.
- **P3 — specialist coverage:** valuable but complex situations that should not delay the mainstream product.

“Done” always means connected desktop workflow plus real encrypted-core behaviour, not a schema, service, or placeholder screen alone.

## 2. Current capability review

| Capability | Connected now | Main gap to self-reporting |
| --- | --- | --- |
| Encrypted workspace and sign-in | Yes | Production credential lifecycle, connected backup/restore, complete administration. |
| Australian organisation setup | Partial | Obligation profile, entity types, reporting registrations, period calendar, individual profile. |
| Chart, journals, trial balance | Partial | Opening balances, reversals, periods, complete ledger, financial statements, source subledgers. |
| Document evidence | Partial | Broader evidence types, extraction/review provenance, deductions/assets/income binding, retention UX. |
| Banking | Partial | Standard import formats, transfers, automated suggestions with human approval, full reconciliation reports. |
| GST/BAS | Draft subset | Complete labels, adjustments, IAS, validation, declaration, amendment, delivery, receipt. |
| Audit/backup/restore core | Backend components | Complete user journeys, evidence exports, release validation. |
| Sales and receivables | No | Full module required. |
| Purchases and payables | Narrow reviewed documents only | Full bills, credits, payments, allocations, ageing required. |
| Fixed assets and depreciation | No | Required for business accounts and annual tax. |
| Payroll/STP/super | No | Full employer-reporting programme required. |
| Annual business tax | No | Entity profiles, tax reconciliation, schedules, calculation, delivery required. |
| Individual tax | No | Entire taxpayer, income, deduction, investment, calculation, and myTax handoff domain required. |
| ATO/SBR transport | No | DSP registration, machine credential, conformance, transport, operations required. |

## 3. Programme backlog

### SR-00 — Product truth and unsupported-situation guardrails

**Priority:** P0  
**Users:** Everyone

- [x] Add a capability registry that distinguishes `AVAILABLE`, `PREPARATION_ONLY`, `HANDOFF_ONLY`, `DIRECT_LODGEMENT`, and `UNSUPPORTED` by report, year, entity type, and app version.
- [ ] Show the boundary before setup and before every declaration; never use “lodged” without an official successful outcome.
- [ ] Detect unsupported profiles and transactions early, explain why Tammy cannot complete them, and generate an accountant handoff pack.
- [ ] Version product claims and help content with the relevant rule/taxonomy bundle.
- [x] Remove or relabel placeholder routes that imply unimplemented reporting.

**Exit:** A packaged E2E matrix proves every navigation path and report card tells the truth for supported, preparation-only, and unsupported cases.

### SR-01 — Taxpayer, entity, registration, and obligation profiles

**Priority:** P0  
**Users:** Individuals, sole traders, companies, partnerships, trusts, employers

- [ ] Add individual and business workspace setup modes.
- [ ] Store protected taxpayer/entity identifiers: ABN, masked TFN reference, legal name, structure, residency, contacts, addresses, branches, and authorised declarants.
- [ ] Add GST basis/frequency, financial year, PAYG instalment and withholding roles, FBT, TPAR, WET, LCT, fuel-tax-credit, payroll, and super registrations.
- [ ] Add individual Medicare, private-health, spouse/dependant, HELP/TSL, residency, and sole-trader context.
- [ ] Build an effective-dated obligation calendar with due dates, user-recorded deferrals, status, reminders, and prior-period history.
- [ ] Add safe profile changes that create future-dated obligation transitions rather than rewriting history.

**Exit:** Canonical scenarios produce the correct obligation set and reporting periods across entity/profile changes.

### SR-02 — Versioned rule, taxonomy, and form-bundle platform

**Priority:** P0  
**Users:** Every reporting module

- [ ] Define signed rule bundles containing tax year/period, effective dates, form labels, rates, thresholds, rounding, validations, and source links.
- [ ] Pin each report snapshot to exact bundle, schema, descriptor, and calculation-engine versions.
- [ ] Add install, verify, activate, roll back, and retire workflows with downgrade protection.
- [ ] Preserve old bundles for historical reproduction and amendments.
- [ ] Build an annual ATO/SBR change-intake checklist, impact report, legal review, and golden-fixture update workflow.
- [ ] Fail closed when the app lacks an approved bundle for a requested year or service.

**Exit:** The same historical inputs reproduce the same output after a later rules upgrade, while a new period uses the new approved bundle.

### SR-03 — Tax-fact ledger and source provenance

**Priority:** P0  
**Users:** Business and individual calculations

- [ ] Introduce typed, immutable tax facts separate from—but linked to—the accounting journal.
- [ ] Represent period/year, owner, category, amount, source, evidence, apportionment, certainty, and correction lineage.
- [ ] Project accounting, payroll, asset, contractor, investment, rental, CGT, prefill, and user-asserted facts through module-owned ports.
- [ ] Detect duplicates across imports, books, prefill, and manual entry.
- [ ] Support explicit exclusions and user explanations without deleting source facts.
- [ ] Add source-to-label and label-to-source queries used by every workpaper.

**Exit:** Golden datasets reconcile every report label to an immutable, non-duplicated fact set and evidence manifest.

### SR-04 — Shared report lifecycle, declarations, delivery, and amendments

**Priority:** P0  
**Users:** Every reporting module

- [ ] Implement collecting, blocked, review-ready, declared, lodging/handoff, delivered, rejected, and amendment states.
- [ ] Freeze facts/calculations at declaration and require fresh authentication for high-risk delivery actions.
- [ ] Classify validation outcomes as blocker, warning, information, or unsupported.
- [ ] Build deterministic report snapshots, PDFs, machine-readable handoff exports, and evidence manifests.
- [ ] Retain official receipts/status or user-supplied external confirmation.
- [ ] Add linked amendments with reason, changed facts, difference calculation, and original preservation.
- [ ] Add payment/refund tracking as a user reconciliation aid without pretending Tammy controls ATO accounts.

**Exit:** One shared lifecycle passes create-to-amend E2E for a preparation-only report and a simulated direct-lodgement report.

### BOOK-01 — Complete accounting kernel and financial close

**Priority:** P0  
**Users:** Businesses and sole traders

- [ ] Connect opening conversions, account update/archive, reversals, close/reopen period, and complete general-ledger screens.
- [ ] Add profit and loss, balance sheet, cash-flow statement, general ledger, journal, trial balance, and GST-detail reports.
- [ ] Add account-level drill-down, comparative periods, retained deterministic report snapshots, and export.
- [ ] Implement bank and credit-card transfers, standard CSV/OFX/QFX import, duplicate controls, match review, and reconciliation statements.
- [ ] Add end-of-period checklist: uncategorised facts, unreconciled accounts, suspense/control balances, missing evidence, and period lock.
- [ ] Connect backup, restore, and audit-evidence workflows before tax reports rely on retained data.

**Exit:** A packaged canonical quarter reconciles journals, subledgers, bank statements, financial statements, GST facts, backup/restore, and audit evidence.

### BOOK-02 — Sales, invoicing, and receivables

**Priority:** P1  
**Users:** Businesses and sole traders

- [ ] Customers, contacts, addresses, tax identity, and payment terms.
- [ ] Quotes, invoices, recurring drafts, credit notes, write-offs, and customer statements.
- [ ] GST-inclusive/exclusive lines, mixed tax treatment, discounts, rounding, and deposits.
- [ ] Receipts, allocations, overpayments, refunds, reversals, and bank matching.
- [ ] Aged receivables, overdue workflow, and source-to-journal/GST provenance.
- [ ] PDF generation and local email/share handoff without requiring vendor cloud storage.

**Exit:** Sales documents, settlements, receivables control, bank matches, financial reports, and GST labels reconcile exactly.

### BOOK-03 — Purchases, payables, and expenses

**Priority:** P1  
**Users:** Businesses and sole traders

- [ ] Suppliers, bills, supplier credits, expense claims, recurring drafts, and approval states.
- [ ] Local evidence extraction creates candidates only; human review remains mandatory before posting.
- [ ] GST treatment, private/non-deductible apportionment, capital-versus-expense review, withholding indicators, and contractor identity.
- [ ] Payments, allocations, overpayments, refunds, reversals, and bank matching.
- [ ] Aged payables, supplier statements, due-date workflow, and duplicate-invoice detection.
- [ ] Feed contractor facts to TPAR eligibility and payments reporting.

**Exit:** Purchases, settlements, payables control, banking, GST credits, deductions, and retained evidence reconcile exactly.

### BOOK-04 — Fixed assets, depreciation, and disposals

**Priority:** P1  
**Users:** Businesses and sole traders

- [ ] Asset register with acquisition evidence, tax/accounting cost, business-use percentage, location, owner, and effective date.
- [ ] Accounting and tax depreciation methods, low-value pools, instant-write-off/general small-business-pool rules by year where applicable.
- [ ] Improvements, private-use changes, balancing adjustments, disposals, and linked CGT facts.
- [ ] Depreciation journals, schedules, reconciliations, and annual-return mappings.
- [ ] Preserve rule-year assumptions and explicit user elections.

**Exit:** Asset schedules reconcile opening cost/written-down value, journals, tax adjustments, disposals, and annual labels.

### BAS-01 — Complete GST and IAS preparation

**Priority:** P0  
**Users:** GST/PAYG-registered businesses

- [ ] Support cash and non-cash GST bases, monthly/quarterly/annual cycles, branches, and period transitions.
- [ ] Calculate and explain G1, G2, G3, G10, G11, G21-G24, 1A, 1B, and derived totals as applicable.
- [ ] Add PAYG instalment options and labels, PAYG withholding W labels, and supported FBT/WET/LCT/fuel-tax-credit labels behind obligation flags.
- [ ] Add GST adjustments, bad debts/recoveries, private use, change of use, rounding, prior-period corrections, and import/manual reconciliation.
- [ ] Add ATO-aligned validation, source-to-label drill-down, variance/threshold checks, and complete declaration wording.
- [ ] Support original, revision/amendment, deterministic manual lodgement pack, and external-confirmation recording.

**Exit:** Official and Tammy-owned cash/non-cash scenarios calculate every applicable label and reproduce after backup/restore and rule upgrades.

### BAS-02 — Business SBR connection and operations

**Priority:** P1  
**Users:** Businesses directly lodging supported obligations

- [ ] Complete DSP registration, operational framework, security review, service enrolment, test environment, conformance suite, self-certification, and production approval.
- [ ] Add a local machine-credential capability with secure import, access control, expiry/rotation, revocation, and redacted diagnostics.
- [ ] Implement SBR message construction, service/taxonomy discovery, validation, pre-lodge where available, lodge, status, receipt, and amendment protocols.
- [ ] Bind declaration, payload, response, and receipt hashes to the audit chain.
- [ ] Add idempotent retry, timeout, duplicate-submission protection, outage messaging, queued recovery, and operator status runbooks.
- [ ] Build a deterministic ATO simulator and external-test evidence suite; never enable production by build flag alone.
- [ ] Add service-level kill switches and preparation/manual fallback when the ATO or Tammy service profile is unavailable.

**Exit:** Conformance evidence and a production-authorised canary prove end-to-end BAS/IAS submission, receipt, retry, rejection, and amendment before general availability.

### IND-01 — Individual return profile and personalisation

**Priority:** P1  
**Users:** Individual self-lodgers

- [ ] Tax year, residency, date-of-birth band, spouse, dependants, deceased-estate indicator, address/contact, and bank-refund handoff details.
- [ ] Medicare exemption/reduction, private-health cover/statements, HELP/TSL and other supported loan context.
- [ ] Personalise the return from applicable sections while allowing the user to add or remove sections with explanation.
- [ ] Prior-year carry-forward facts, losses, asset registers, and comparison without copying stale income.
- [ ] Completeness interview and unsupported-complexity escalation.

**Exit:** Representative employee, investor, landlord, and sole-trader profiles activate the correct sections and checks.

### IND-02 — Prefill and external-data reconciliation

**Priority:** P1  
**Users:** Individual self-lodgers

- [ ] Import user-obtained ATO/myTax data where a stable permitted format exists; otherwise support guided manual entry.
- [ ] Capture payer/provider identity, period, source, import time, and immutable original values.
- [ ] Reconcile prefill against local payroll, bank, investment, and business records.
- [ ] Show missing, duplicate, changed, and user-overridden values with reasons.
- [ ] Never scrape myGov/myTax, ask for a myGov password, or automate protected consumer sessions.

**Exit:** Reimport is idempotent, conflicts are reviewable, and every accepted/overridden value retains provenance.

### IND-03 — Employment, government, super, interest, and dividend income

**Priority:** P1  
**Users:** Mainstream individuals

- [ ] Salary/wages, allowances, lump sums, termination payments, reportable fringe benefits, and withholding credits.
- [ ] Government payments and allowances.
- [ ] Australian interest across accounts and ownership shares.
- [ ] Dividends, franking credits, trust/managed-fund annual tax statement fields, and joint ownership.
- [ ] Super pensions, annuities, and common taxable/untaxed components.
- [ ] Foreign income indicators route to the expanded module when simple handling is insufficient.

**Exit:** Golden returns reconcile each income source and credit to myTax sections without duplication.

### IND-04 — Deductions and substantiation

**Priority:** P1  
**Users:** Employees, investors, and sole traders

- [ ] Work-related car, travel, clothing/laundry, self-education, working-from-home, tools/equipment, union/professional, and other expenses.
- [ ] Gifts/donations, cost of managing tax affairs, interest/dividend deductions, and personal super contributions.
- [ ] Method-specific calculators, logbooks/diaries, receipt evidence, business/private apportionment, and threshold rules by year.
- [ ] Depreciating work assets with links to the asset register.
- [ ] Duplicate detection against reimbursed/employer-paid and business expenses.
- [ ] Explain why an amount is included, limited, apportioned, or blocked.

**Exit:** Each supported method has boundary/golden tests and a complete evidence checklist suitable for myTax entry and later review.

### IND-05 — Rental property

**Priority:** P1  
**Users:** Individual landlords

- [ ] Property identity, ownership shares, availability-for-rent periods, co-owner split, and acquisition facts.
- [ ] Rent and other income; interest, rates, insurance, agent fees, repairs, capital works, decline in value, and other expenses.
- [ ] Private-use and non-arm's-length adjustments, borrowing expenses, vacant-property checks, and capital-versus-repair review.
- [ ] Property-level evidence, depreciation/capital-works schedules, net result, carried losses, and disposal handoff to CGT.

**Exit:** Single/joint property scenarios reconcile property schedules, deduction limits, CGT basis, and myTax labels.

### IND-06 — Investments and capital gains tax

**Priority:** P1 mainstream; P2 advanced  
**Users:** Investors and asset disposers

- [ ] Asset parcels and cost base, ownership, corporate actions, brokerage, distributions, and disposal proceeds.
- [ ] Shares, ETFs/managed funds, cryptocurrency, real property, collectables, and personal-use asset classification.
- [ ] CGT events, exemptions, losses, discount method, indexation/other method where applicable, and loss carry-forward.
- [ ] Main-residence and partial-use workflows with explicit unsupported-complexity checks.
- [ ] Import adapters only for documented user-export formats, with immutable raw import and duplicate controls.
- [ ] Capital-gain worksheet and evidence pack mapped to myTax.

**Exit:** Golden parcel, crypto, managed-fund, property, loss, and discount scenarios reproduce label totals and cost-base evidence.

### IND-07 — Sole trader and personal-services income

**Priority:** P1  
**Users:** Sole traders filing through myTax

- [ ] Feed reconciled business P&L, GST-exclusive income/expenses, assets, depreciation, and tax adjustments into business/professional items.
- [ ] Industry/activity, ABN, main business location, accounting method, stock, motor vehicle, contractor, and reconciliation questions.
- [ ] PSI/PSB decision support that records user answers and escalates ambiguous cases rather than advising.
- [ ] Non-commercial loss tests, deferred loss tracking, and supported small-business concessions/elections.
- [ ] Reconcile TPAR-reported receipts and PAYG instalments/credits.

**Exit:** A canonical sole-trader year moves from business books to individual myTax handoff with no manual re-totaling.

### IND-08 — Individual tax calculation, offsets, and myTax handoff

**Priority:** P1  
**Users:** Individual self-lodgers

- [ ] Taxable income, rates, levies, Medicare levy/surcharge, private-health adjustment, offsets, HELP/TSL, PAYG credits, and refund/payable estimate by year.
- [ ] Carry-forward losses and credit/offset eligibility supported by collected facts.
- [ ] Section-by-section completeness and cross-checks against prefill and prior-year patterns.
- [ ] Generate a label-by-label myTax checklist, calculation report, evidence index, unresolved-warning list, and machine-readable archive.
- [ ] Guide users through manual myTax entry without scraping, browser automation, or credential capture.
- [ ] Record user-confirmed lodgement date, ATO receipt/reference, notice-of-assessment values, and variance from Tammy's estimate.
- [ ] Support linked amendments and preserve the original handoff pack.

**Exit:** Packaged E2E completes representative individual and sole-trader returns from setup through retained myTax handoff and amendment.

### PAY-01 — Payroll and PAYG withholding

**Priority:** P2  
**Users:** Employers and employees administering payroll

- [ ] Employee identity, employment basis, tax declaration, withholding settings, bank/super details, protected access, and termination.
- [ ] Pay calendars, ordinary earnings, allowances, overtime, bonuses, deductions, reimbursements, leave, lump sums, and termination payments.
- [ ] Effective-dated tax tables and deterministic gross-to-net calculations.
- [ ] Pay-run draft, review, approval, payslips, payment file handoff, reversal/correction, and payroll journals.
- [ ] PAYG withholding liabilities and BAS W-label reconciliation.
- [ ] Payroll audit, year-to-date reconciliation, and privacy-aware employee exports.

**Exit:** Golden pay runs reconcile employee YTD, bank payments, ledger liabilities, BAS labels, and corrected events.

### PAY-02 — STP, super, and payday-super readiness

**Priority:** P2  
**Users:** Employers

- [ ] STP Phase 2 categorisation, employee commencement/declaration support, pay events, update events, finalisation, amendments, and receipts.
- [ ] Service-specific SBR/DSP conformance and production enablement independent of BAS approval.
- [ ] Super guarantee eligibility, ordinary/qualifying earnings, effective-dated rates, salary sacrifice, choice/default fund, and liabilities.
- [ ] SuperStream contribution messages, clearing-house/payment handoff, outcomes, exceptions, refunds, and reconciliation.
- [ ] Missed/late super and super-guarantee-charge evidence workflow without giving legal advice.
- [ ] Implement current payday-super requirements from approved effective-dated rules before claiming support.

**Exit:** End-to-end employer scenarios prove pay, STP acceptance, super delivery/reconciliation, BAS withholding, finalisation, and amendments.

### BUS-01 — TPAR and contractor reporting

**Priority:** P2  
**Users:** Businesses in covered industries or government reporting contexts

- [ ] Determine TPAR applicability from services/industry and user confirmation.
- [ ] Maintain contractor ABN/name/address and payment/withholding facts from payables and banking.
- [ ] Produce original/amended reports, payee statements, validation, declaration, delivery, receipt, and evidence.
- [ ] Handle excluded payments, mixed invoices, GST, no-ABN withholding, and reconciliation to expense/payable totals.

**Exit:** Official-format scenarios reconcile contractor payments and pass the applicable electronic-reporting tests.

### BUS-02 — FBT workpapers and return

**Priority:** P2  
**Users:** Employers providing fringe benefits

- [ ] Benefit categories, employee/associate attribution, taxable values, elections, exemptions, employee contributions, and GST interactions.
- [ ] FBT year, gross-up rates, instalments, reportable fringe-benefit amounts, return, amendments, and evidence.
- [ ] Reconcile payroll/STP reportable amounts, BAS instalments, ledger expenses, and annual FBT liability.
- [ ] Escalate unsupported benefit valuations rather than estimate silently.

**Exit:** Supported benefit scenarios reconcile workpapers, employee reporting, instalments, and return totals.

### BUS-03 — Annual business tax reconciliation

**Priority:** P2  
**Users:** Companies, partnerships, trusts, and sole traders

- [ ] Close and sign off accounting periods; reconcile financial statements to the tax return.
- [ ] Permanent/temporary differences, non-deductible expenses, exempt income, depreciation differences, provisions, losses, and tax payments/credits.
- [ ] Reconcile GST, PAYG, payroll, TPAR, FBT, assets, loans, and owner/equity accounts before annual preparation.
- [ ] Generate a tax-reconciliation report with evidence and user elections.

**Exit:** Every annual-return label derives from books or an explicit adjustment and reconciles back to signed financial statements.

### BUS-04 — Company tax return and schedules

**Priority:** P2  
**Users:** Self-lodging small companies

- [ ] Company profile, status, residency, ownership/control indicators, financial and tax labels, losses, CGT, small-business questions, and declarations.
- [ ] Franking account, dividends/distributions, loans to shareholders indicators, and supported schedules.
- [ ] Tax calculation, instalment/credit reconciliation, original/amended return, delivery where an approved self-lodgement service exists, or deterministic handoff otherwise.
- [ ] Unsupported complexity guardrails for consolidated/international/special-purpose entities.

**Exit:** Mainstream private-company golden returns reconcile accounts, tax adjustments, calculation, schedules, and delivered artefact.

### BUS-05 — Partnership and trust returns

**Priority:** P2 mainstream; P3 complex trusts  
**Users:** Self-lodging partnerships and simple trusts

- [ ] Entity profile, partners/beneficiaries, ownership/distribution shares, statements, tax reconciliation, losses, CGT, and declarations.
- [ ] Distribution statements and machine-readable exports feeding related individual workspaces without hidden merging.
- [ ] Original/amended return and approved delivery/handoff.
- [ ] Explicitly block complex trust elections, streaming, international, deceased-estate, or professional-advice situations until separately supported.

**Exit:** Simple partnership and trust scenarios reconcile entity totals to participant distributions and downstream handoff exports.

### BUS-06 — Specialist indirect and industry obligations

**Priority:** P3  
**Users:** Applicable businesses only

- [ ] Wine equalisation tax, luxury car tax, and fuel-tax-credit calculation modules.
- [ ] Primary-producer income/expense and averaging support where approved.
- [ ] Inventory, trading stock, cost of goods, stocktakes, and valuation elections.
- [ ] Foreign-currency transaction support only after a separately approved multi-currency accounting design.
- [ ] Sector-specific schedules selected from real customer demand and available lodgement channels.

**Exit:** Each specialist module has its own product boundary, official scenarios, rules bundle, and release approval; none is implied by generic “business tax” wording.

### UX-01 — Guided reporting home and task system

**Priority:** P0  
**Users:** Everyone

- [ ] Replace the bookkeeping-only overview with obligations, due dates, progress, blockers, expected payments/refunds, and next actions.
- [ ] Add guided setup for individual, sole trader, and business paths.
- [ ] Use plain-language interviews with optional technical detail and official source links.
- [ ] Add save/resume, review queue, missing-evidence queue, prior-year comparison, and notification controls.
- [ ] Ensure keyboard, screen-reader, contrast, zoom, reduced-motion, and error-recovery accessibility.
- [ ] Distinguish estimates, drafts, declarations, handoffs, submissions, acceptances, rejections, and amendments consistently.

**Exit:** Usability and accessibility tests show a first-time user can identify and complete the next valid task without interpreting accounting jargon.

### UX-02 — Evidence, explainability, and collaboration handoff

**Priority:** P0  
**Users:** Everyone and their chosen adviser

- [ ] Evidence inbox for receipts, statements, contracts, logbooks, notices, tax statements, and user assertions.
- [ ] Source-to-label drill-down and “why this amount” explanations for calculations and exclusions.
- [ ] Missing-evidence and retention checklist by claim/report/year.
- [ ] Redacted accountant/reviewer export with reports, facts, evidence index, warnings, questions, and audit verification instructions.
- [ ] Import reviewed adjustment packs only through typed, user-approved workflows.
- [ ] Never create a vendor-access backdoor to the encrypted workspace.

**Exit:** A reviewer can independently reproduce a report from the exported pack without receiving Tammy credentials or unrelated private data.

### SEC-01 — Tax identity, credential, privacy, and retention controls

**Priority:** P0  
**Users:** Everyone

- [ ] Protect TFN, bank, health, employee, machine-credential, and submission data behind least-privilege projections and redaction.
- [ ] Connect user/role, TOTP, ownership transfer, passphrase change, backup, restore, and audit-evidence workflows.
- [ ] Add consent and disclosure records for imports, handoffs, and external submissions.
- [ ] Add configurable retention/disposal subject to legal minimums, holds, pending amendments, and backup semantics.
- [ ] Extend privacy manifest, support/privacy documentation, threat model, incident response, and sensitive-log tests.
- [ ] Complete App Store privacy answers and marketing review for every reporting release.

**Exit:** Security review and packaged adversarial tests prove renderer isolation, secret handling, redaction, crash recovery, backup/restore, and safe disposal.

### OPS-01 — Regulatory release and service operations

**Priority:** P0 before claims; P1 before direct lodgement  
**Users:** Product operators and support

- [ ] Maintain a regulatory inventory of reports, years, taxonomies, service versions, due dates, source links, owners, and expiry dates.
- [ ] Monitor ATO/SBR release notes, service status, certificates, credentials, and deprecations.
- [ ] Add signed release evidence and independent calculation/compliance review per rule bundle.
- [ ] Define support escalation, incident response, submission replay rules, customer communications, and kill-switch procedures.
- [ ] Retain conformance packs, test responses, production approvals, and App Store artefacts by release.
- [ ] Publish a precise support matrix and known limitations rather than an unqualified “complete tax” claim.

**Exit:** A release cannot enable or market a report unless the registry proves current rules, tests, approval, support, and rollback readiness.

### QA-01 — End-to-end reporting assurance

**Priority:** P0  
**Users:** Every release

- [ ] Extend descriptor-driven coverage to every report RPC, state transition, role, idempotency case, stale version, error class, and preload method.
- [ ] Add official and independently reviewed golden calculations per report/year.
- [ ] Add property tests for money, rates, rounding, apportionment, aggregation, and amendment differences.
- [ ] Add full real-SQLCipher journeys, migrations from every supported version, encrypted backup/restore, and tamper tests.
- [ ] Add deterministic SBR simulator responses, external-test-environment cases, network loss, timeout, duplicate, rejection, outage, and restart recovery.
- [ ] Add packaged macOS E2E for every marketed journey and retain evidence for the submitted App Store artefact.

**Exit:** The coverage manifest contains no declared-future entry for a marketed capability and every release gate is reproducible from a clean checkout.

## 4. Recommended delivery roadmap

### Wave 0 — Truthful foundation and current-app completion

**Backlog:** SR-00, SR-01 baseline, SR-02 platform, SR-03 platform, SR-04 platform, BOOK-01, UX-01 baseline, UX-02 baseline, SEC-01 connected controls, QA-01 baseline.

**Release outcome:** Tammy can truthfully say it keeps reporting-ready books. No direct tax-lodgement claim.

### Wave 1 — Full BAS preparation

**Backlog:** BOOK-02, BOOK-03, BOOK-04 essentials, BAS-01, obligation calendar, report lifecycle, evidence drill-down.

**Release outcome:** Complete reviewable BAS/IAS workpapers, declaration, amendment, and manual handoff for mainstream small businesses.

### Wave 2 — Individual and sole-trader self-preparation

**Backlog:** IND-01 through IND-08, with mainstream shares/managed funds/crypto/rental scope explicitly fixed for the release.

**Release outcome:** A user can prepare and substantiate a mainstream individual or sole-trader return and complete a guided myTax handoff.

### Wave 3 — Direct business BAS/IAS

**Backlog:** BAS-02 plus OPS-01 direct-service controls.

**Release outcome:** Approved businesses can lodge supported BAS/IAS reports through SBR and retain official outcomes. Manual fallback remains available.

### Wave 4 — Employer self-reporting

**Backlog:** PAY-01, PAY-02, PAYG/BAS integration, SEC-01 employee privacy expansion.

**Release outcome:** Payroll, STP, super, PAYG withholding, correction, and year-end finalisation work as one reconciled journey.

### Wave 5 — Annual business and contractor reporting

**Backlog:** BUS-01 through BUS-05, sequenced TPAR → tax reconciliation → company → partnership/simple trust → FBT according to demand and channel readiness.

**Release outcome:** Mainstream small entities can prepare and, where approved, deliver their annual reports without re-entering reconciled books.

### Wave 6 — Specialist expansion

**Backlog:** advanced IND-06, complex BUS-05, BUS-06, and separately approved sector modules.

**Release outcome:** Explicitly selected specialist cases become supported one module at a time; unsupported guardrails remain the default.

## 5. Dependency and sequencing rules

1. Do not start direct SBR before complete local preparation, declaration, amendment, and manual fallback exist.
2. Do not start annual tax calculation before financial close, source subledgers, assets, and tax-fact provenance reconcile.
3. Do not start sole-trader return completion before business books feed the individual schedule without re-entry.
4. Do not start STP before payroll journals, YTD reconciliation, correction, and employee privacy controls are complete.
5. Do not market a report before its connected packaged E2E, current rule bundle, evidence, support matrix, and external approval are retained.
6. Keep preparation usable offline. Network dependence begins only at an explicit import or submission action.
7. Treat each tax year and SBR service version as a maintained product, not a one-off launch.

## 6. First implementation tranche

The next detailed implementation plans should be written separately, in this order:

1. `reporting-obligation-foundation` — SR-00 through SR-04 core models and lifecycle.
2. `reporting-ready-books` — BOOK-01 and connected security/evidence workflows.
3. `complete-bas-preparation` — BOOK-02/03 essentials plus BAS-01.
4. `individual-tax-foundation` — IND-01/02 and the common personal fact model.
5. `individual-mainstream-return` — IND-03/04/08.
6. `individual-property-investment` — IND-05/06.
7. `sole-trader-tax-return` — IND-07 and business-to-person handoff.
8. `business-sbr-bas` — BAS-02 only after external DSP prerequisites are underway.

Each plan must name exact Protobuf contracts, migrations, domain packages, renderer routes, preload methods, fixtures, RED/GREEN commands, packaged E2E cases, and release-evidence gates. Do not turn this programme backlog into one oversized implementation change.

## 7. Backlog completion test

The programme is complete for a supported profile only when a clean packaged installation can:

1. create or reopen the correct workspace type;
2. determine the user's real obligations;
3. capture and reconcile all required facts;
4. calculate each applicable report using the correct historical rules;
5. explain every material value and show its evidence;
6. detect unsupported complexity before declaration;
7. complete the declaration;
8. directly lodge an approved business report or generate the individual myTax handoff;
9. retain and verify the outcome; and
10. reproduce, amend, back up, restore, and audit the report without data loss or claim drift.

Anything less remains an incomplete or preparation-only capability and must be labelled accordingly.
