# Company EOFY and Direct SBR Lodgment Design

**Date:** 2026-08-25  
**Status:** User-approved design  
**Target:** Australian private-company EOFY preparation and company tax return  
**Delivery strategy:** One constrained end-to-end vertical slice  
**Extends:** `2026-07-19-tammy-local-first-accounting-sbr-design.md`, `2026-08-02-core-business-accounting-suite-design.md`, and `2026-08-21-local-accounting-sbr-registration-readiness-design.md`

## 1. Outcome

Tammy will let an authorised user of a mainstream Australian private company:

1. collect accounting records through bounded batch document and transaction-file intake;
2. review extracted facts before they affect the books;
3. complete the financial close and retain approved, hash-bound financial statements;
4. reconcile accounting profit to taxable income through explicit, evidenced adjustments;
5. prepare and validate a versioned company tax return;
6. review the exact return and accept the required declarations using fresh authentication;
7. submit the frozen return through the ATO PLS/SBR company-return service using the company's RAM-issued machine credential when report-specific production access exists; and
8. retain submission status, official validation outcomes, and the receipt without changing the declared return.

Before external ATO access exists, the same product must provide the complete preparation workflow, a deterministic handoff pack, and a network-disabled SBR simulator. The application must describe these modes accurately and must never imply that a simulated, EVTE, exported, or manually confirmed report was lodged in production.

This is a greenfield product boundary. The implementation may replace incomplete foundation contracts and screens when that reduces complexity. It does not need compatibility adapters for the current demonstration-only document, banking, BAS, or reporting records. Existing security, audit, encrypted-storage, backup, process, and native-boundary controls remain normative.

## 2. Current baseline and gap

The repository already contains:

- a local Electron renderer, trusted main process, Go core, SQLCipher workspace, and isolated SBR helper;
- identity, organisation verification, audit, backup, accounting journals, trial balance, basic document retention/review, basic statement-row import/reconciliation, and a small BAS workpaper;
- RAM machine-credential import, replacement, removal, unlock, ABN binding, Product ID storage, signed runtime-profile checks, deterministic SBR readiness simulation, and packaged security evidence; and
- task scenarios for accounting development, fresh workspaces, SBR simulation, EVTE readiness, packaging, release checks, and evidence export.

The current document screen reads a single bounded file in the renderer and performs limited PDF-text inference. The current banking screen accepts manually pasted CSV rows. The current tax service exposes only a development BAS workpaper. The reporting capability registry has no company-return capability, and the EVTE transport adapter is deliberately unavailable. There is no company tax domain, financial-close workbench, annual tax reconciliation, company-return schema, report lifecycle, or live ATO submission operation.

The new slice closes these gaps without claiming support for unrelated accounting or tax products.

## 3. Supported company boundary

### 3.1 Initial report identity and included company

The first implementation target is the income year ending **30 June 2026** (`2025-07-01` through `2026-06-30`). Its repository preparation bundle ID is `au-company-return-2026-preparation-v1`; its public capability key is `COMPANY_TAX_RETURN / AU_PRIVATE_COMPANY / 2026`. The official ATO Product Register names the delivery service **Company return 2026**. The machine service identifier, schema entry points, and external bundle version must be copied from the issued 2026 service artefacts and are external release inputs, never invented Tammy constants.

The preparation bundle supports the core Company tax return 2026 form only. It supports no attached schedule in its first release. A condition requiring a dividend and interest, CGT, losses, international dealings, R&D incentive, reportable tax position, consolidated-groups, PAYG payment-summary, or other separately lodged schedule blocks declaration. The capability registry exposes preparation, simulator, EVTE, and production as distinct modes; `2026` is the only available company-return year until another independently reviewed bundle is installed.

Within that core form, the supported coverage is company identity/EFT; item 1 only for an Australian or absent holding company with fully reviewed identity; item 2 main business activity; item 3 Australian-resident private-company status; item 4 only when no interposed-entity election applies; item 5 only when TOFA does not apply; item 6 supported ordinary income/expense calculation; item 7 taxable-income reconciliation; item 8 supported financial/other information derived from the frozen books; item 9 supported capital allowances; item 10 only for a supported small-business depreciation choice; item 11 not applicable because consolidation is excluded; item 12 only when no unsupported offset applies; item 13 limited revenue-loss information that does not trigger a losses schedule; items 14 onward only where the 2026 core form accepts an explicit `not applicable` answer under section 3.2; and the supported calculation statement/declaration. Bundle acceptance compares this list with every issued 2026 form field and service fact. A new or changed mandatory field stops the build and requires a reviewed spec/bundle change rather than an implicit blank or zero.

The first production scope supports one reporting party that is:

- an Australian-resident private company;
- represented by one verified organisation and ABN in one local workspace;
- keeping AUD books;
- using a standard 1 July to 30 June income year;
- carrying on an ordinary service or trading business;
- using the supported chart, journals, sales, purchases, settlements, banking, fixed assets, GST, PAYG, and evidence features;
- reporting ordinary Australian trading income and business-bank interest, but not dividend or trust/partnership distribution income;
- claiming ordinary operating deductions and supported tax depreciation;
- carrying forward a reviewed revenue-loss balance only where the user confirms that no ownership or business-continuity complexity requires professional assessment;
- recording ordinary dividends and simple franking-account debits and credits; and
- lodging an original return or a linked amendment.

The product supports straightforward fixed-asset disposals and capital gains only while the 2026 bundle determines that no separately required CGT schedule, rollover, small-business concession, foreign asset, or specialist calculation is triggered.

The company tax profile requires the legal company name, encrypted TFN, ABN, current and prior postal address, main business address, Australian resident/private status, main business activity code and description, final-return answer, Australian refund-account details where applicable, connected entities/affiliates, annual and aggregated turnover, assessable income, base-rate-entity passive income, small-business-entity choice, ownership continuity, and all explicit applicability questions below. TFN and refund-account values are masked after entry, encrypted at rest, excluded from ordinary logs/support output, and revealed only through a fresh-authenticated edit flow.

### 3.2 Supported 2026 rule and input matrix

| Rule area | Required reviewed inputs | Supported result | Fail-closed condition |
|---|---|---|---|
| Company identity and status | Legal name, TFN, ABN, addresses, activity code, resident/private answers, final-return answer | Core identity/status facts for one Australian private company | Missing identity, non-resident/public/special company status, substituted period, or profile/ABN mismatch |
| Holding and related entities | Immediate/ultimate holding answers, connected entities and affiliates, each entity's turnover contribution | Aggregated turnover with an evidence-backed entity list | Foreign holding detail, uncertain control/affiliate assessment, non-market associate dealing, or incomplete turnover evidence |
| Company tax rate | Aggregated turnover, assessable income, and classified base-rate-entity passive income | 25% when turnover is below $50 million and passive income is no more than 80%; otherwise 30% | User cannot classify connected entities/passive income, or a source category is unsupported; numeric thresholds remain checksum-pinned in the 2026 rule bundle |
| Accounting profit/loss | Frozen profit-and-loss facts mapped to supported ordinary Australian income and expense categories | Company-return profit/loss facts and reconciliation starting amount | An unmapped material account, unsupported income/deduction category, or unreviewed evidence |
| Deductions and add-backs | Reviewed expense mappings, private/non-deductible portions, provisions/accruals, and evidence | Ordinary deductions plus explicit permanent/temporary add-backs and subtractions | Legal characterisation is uncertain or a bundle rule does not cover the adjustment |
| Depreciation | Asset cost/date, business use, disposal, accounting method, tax method/election, and evidence | Prime-cost/diminishing-value tax depreciation and bundle-supported simplified/instant write-off treatment | Unsupported pool/election, private-use change, rollover, intangible, balancing adjustment, or missing evidence |
| Capital gains | Supported asset disposal proceeds/cost base and bundle CGT-schedule predicate | Core return capital-gain facts only when no CGT schedule or concession is required | CGT schedule threshold, rollover, small-business concession, foreign asset, share/unit, crypto, or uncertain cost base |
| Revenue losses | Current-year result; imported prior-year balance; ownership/continuity answers and evidence | Current-year revenue loss and application of a verified carried-forward revenue loss | Ownership change, same/similar-business judgment, losses schedule requirement, capital loss use, or uncertain eligibility |
| Dividends and franking | Dividends paid, franking credits allocated, tax payments/refunds, and opening franking balance evidence | Simple franking-account reconciliation and ordinary dividends paid | Dividend income, franking deficit, streaming, off-market distribution, benchmark issue, or unexplained franking difference |
| PAYG and tax credits | Posted PAYG instalments/tax payments plus retained ATO/payment evidence | Credits and expected company tax payable/refund reconciliation | Unsupported offset/credit, foreign credit, missing evidence, or ledger/ATO mismatch |
| Payroll summary reconciliation | Ledger wages, deductible super, PAYGW and clearing balances; imported EOFY summary totals | Evidence-only comparison; the return consumes reviewed ledger/tax-adjustment facts | Tammy does not calculate payroll or STP; any mismatch, employee-level adjustment, FBT/PSI issue, or unsupported payroll tax effect blocks close |
| Applicability questions | Explicit answers for TOFA, PSI, interposed elections, consolidation, R&D, international dealings, clubs, life insurance, and every 2026 schedule predicate | Supported `not applicable` facts with evidence/attestation | Any affirmative or unknown answer outside the supported matrix |

The numeric rate, threshold, rounding, label, and schedule-predicate values in this table must also exist as exact checksum-pinned 2026 bundle data with official source references and golden cases. Code must not contain a fallback value when the accepted bundle is absent or disagrees.

### 3.3 Fail-closed exclusions

Tammy must block declaration and submission when it detects or the user identifies:

- a consolidated or multiple-entry consolidated group;
- a substituted accounting period;
- non-residency, permanent establishments, foreign income, foreign assets, foreign tax, transfer pricing, controlled foreign companies, or international related-party dealings;
- R&D tax incentives, reportable tax positions, thin capitalisation, TOFA, petroleum or resource-rent tax, life-insurance business, pooled development funds, or other specialist company categories;
- a CGT schedule, loss schedule, international dealings schedule, or other attachment that the installed report bundle does not explicitly support;
- an ownership change or loss-utilisation test requiring a same-or-similar-business judgment;
- a shareholder or associate loan requiring Division 7A treatment beyond a zero balance or a separately evidenced compliant resolution;
- complex franking, franking-deficit, streaming, off-market distribution, or benchmark-rule treatment;
- unsupported payroll, FBT, TPAR, inventory, multi-currency, trust-distribution, partnership-distribution, primary-production, or cryptocurrency facts that affect the return; or
- any report label, schedule, calculation, declaration, validation, or delivery requirement absent from the exact installed rule and service bundle.

A blocked return remains usable as an EOFY workpaper and export pack. The UI names each unsupported condition and the affected return area. It does not estimate, omit, coerce to zero, or silently fall back to a previous income year's rule.

### 3.4 Deferred products

Sole-trader, partnership, trust, SMSF, FBT, TPAR, STP finalisation, complete BAS/IAS lodgment, inventory, payroll, multi-currency, and cloud collaboration are separate product slices. iOS is also separate: this slice ships on the existing macOS desktop architecture because the current secure workspace, native helpers, and machine-credential boundary are macOS-owned. Protobuf contracts remain platform-neutral so a later iOS review/upload client does not require a second tax model.

## 4. Selected architecture

### 4.1 Vertical slice

The selected approach completes only the accounting, evidence, close, tax, and delivery capabilities needed for the constrained company return. It does not complete every item in the broader self-reporting backlog first, and it does not create a standalone manual tax form disconnected from the books.

The rejected alternatives are:

- **Complete every accounting module first.** This has strong breadth but delays an end-to-end user outcome and makes report provenance difficult to verify until late.
- **Build a manual company-return form first.** This creates duplicate data entry, weak reconciliation, and return values that cannot be traced to approved books.
- **Send directly from the renderer or core.** This would expose credential use and transport parsing to larger, less isolated processes and would break the approved machine-credential boundary.

### 4.2 Process responsibilities

| Unit | Responsibility | Must not do |
|---|---|---|
| React renderer | Show workflow, safe previews, candidates, validations, reports, declarations, status, and receipts | Read local paths, parse untrusted files, hold credential bytes, build SBR envelopes, or call the network |
| Electron main | Own native file selection/drop mediation, one-shot intake handles, bounded IPC, process launch, and scenario authority | Parse accounting content, retain paths after handoff, or open machine credentials |
| Document extractor | Parse bounded document bytes in a sandbox and return deterministic candidates with page/region provenance | Post accounting entries or decide tax treatment |
| Transaction importer | Parse bounded CSV/OFX/QFX inputs, normalise rows, detect duplicates, and produce a review batch | Create journals before explicit review/commit |
| Go core | Own organisations, evidence metadata, accounting, close, tax facts, rules, reports, declarations, authorisation, audit, and submission state | Read machine-credential bytes, retain credential passwords, or open external network sockets |
| SBR helper | Own secure credential/Product ID access, official message signing, PLS/SBR transport, status queries, and bounded response parsing | Calculate tax, alter report values, accept renderer-created envelopes, or persist accounting data |
| Signed report bundle | Define one exact income year, company-return service, schemas, label mappings, rules, validation assets, declarations, and conformance identity | Fall back across years or mutate after acceptance |

Each process exposes narrow generated Protobuf operations. There is no generic IPC, arbitrary filesystem API, generic HTTP client, or renderer-controlled service name or endpoint.

### 4.3 Primary data flow

```text
native intake
  -> encrypted evidence / reviewed import batch
  -> reviewed accounting document or transaction
  -> journal, subledger, GST fact, and evidence provenance
  -> reconciled and locked financial period
  -> immutable financial-close snapshot
  -> explicit annual tax adjustments and elections
  -> immutable company-return snapshot
  -> validation and fresh-auth declaration
  -> canonical official payload and hash
  -> isolated SBR helper using Product ID and RAM machine credential
  -> ATO status and receipt
```

No later stage can mutate an earlier immutable object. A source change invalidates the dependent close and report and requires an explicit reopen or amendment path.

## 5. User workflow

The primary navigation gains **EOFY & Company Tax**. The screen is one workbench with five stages.

### 5.1 Collect

The user can select or drop a bounded batch of PDF, PNG, JPEG, CSV, OFX, and QFX files. The native boundary gives the renderer opaque item IDs and safe display metadata only. Main transfers each selected source to its owning helper/core operation through a one-shot, expiring handle; no local path is returned to React or stored in business records.

Each item shows one of: queued, hashing, extracting, needs review, ready, duplicate, unsupported, or failed. It never creates a journal automatically.

Retention is deterministic. Symlinks, FIFOs/devices, unstable or swapped files, oversize inputs, MIME/magic mismatches, polyglots, disallowed embedded content, decompression-limit failures, and hash/length mismatches are rejected before an `EvidenceDocument` is committed; all temporary bytes are removed. User cancellation before durable commit also retains nothing. A supported, bounded, stable file that passes intake but is corrupt, password-protected, or fails extraction is stored only as the encrypted original with status `NEEDS_MANUAL_REVIEW`; no plaintext derivative is retained. Parser crash or timeout follows the same rule after helper cleanup. Unlinked, undeclared evidence may be explicitly deleted with an audit event; evidence linked to a posted transaction, frozen close, declared return, or receipt is immutable and may only be archived under retention policy.

Document review shows:

- a bounded local preview;
- extracted supplier/customer, reference, date, subtotal, GST, total, payment, and asset candidates;
- confidence and exact source page/region for each candidate;
- duplicate and arithmetic diagnostics;
- account, GST, capital-versus-expense, private/non-deductible, withholding, and evidence treatment; and
- the explicit target action: retain only, create a draft bill/invoice, link to an existing transaction, or classify as an EOFY-only record.

Transaction import shows column/field mapping where needed, opening and closing statement balances, duplicates, normalized rows, and diagnostics before commit. Committed rows remain unmatched until the user confirms an existing source or creates a reviewed accounting transaction.

Bounds are inherited from the accounting-suite design: at most 50 MiB per document, 200 pages, 50 MiB raw import, 100 MiB decoded text, 100,000 transaction rows, and the existing decompression, field, diagnostic, memory, and time limits. One UI batch is capped at 50 files and 500 MiB total so progress, cancellation, and storage checks remain predictable.

### 5.2 Reconcile

The close checklist covers:

- every bank and credit-card account reconciled to a retained statement;
- receivables and payables control accounts reconciled to source subledgers;
- uncategorised, suspense, clearing, evidence-pending, and duplicate-review balances cleared;
- GST, PAYG, wages, super, payroll-clearing, FBT, and tax-payment accounts reconciled where present; payroll/STP is evidence-only and a non-zero FBT or unsupported payroll effect blocks this slice;
- fixed-asset acquisitions, disposals, accounting depreciation, and tax depreciation reviewed;
- shareholder/director loan, dividend, franking, and equity accounts reviewed;
- trial balance balanced and all journals within the period posted; and
- backup plus audit-chain verification completed for the reporting revision.

Blocking checks prevent close. Warnings require an explicit resolution note. A successful close creates a `FinancialStatementApproval`: an authorised director/user views the exact statement hashes, attests that the books and statements are approved for company-tax preparation, and confirms with purpose-bound fresh authentication. This is a product sign-off, not a handwritten, qualified electronic, or cryptographic company-law signature. The resulting `FinancialCloseSnapshot` contains the exact financial revision, trial balance, statements, report parameters, checklist results, rule fingerprints, evidence manifest hash, approval wording/version/hash, and approving identity/time.

Before declaration, reopening the period preserves the snapshot, marks dependent draft returns stale, and requires a reason plus fresh authentication. After a declaration exists, corrected books use `StartFinancialCloseCorrection`: it preserves the original period, journals, close, statements, approval, and return; creates a new correction working revision; accepts only explicit correction/reversal entries; and freezes a new close snapshot with a new approval. The new close feeds a replacement return when no return was accepted by the ATO, or an amendment when an original was accepted. The original close and return are never rewritten.

### 5.3 Tax adjustments

The book-to-tax reconciliation starts from accounting profit before income tax. Every adjustment has:

- adjustment type and return mapping;
- signed amount and accounting-period context;
- permanent or temporary classification;
- explanation;
- supporting evidence or an explicit election/reference;
- rule ID and rule-bundle fingerprint; and
- creator, reviewer, version, and audit event.

Supported adjustments include ordinary non-deductible expenses, exempt/non-assessable income supported by the bundle, accounting-versus-tax depreciation, provisions and accrual reversals supported by the bundle, tax payments/credits, current-year revenue losses, and eligible reviewed carried-forward revenue losses. The engine calculates taxable income and tax payable only from the frozen close plus reviewed adjustments.

### 5.4 Review and declare

The review stage shows:

- profit and loss, balance sheet, cash-flow statement, trial balance, general ledger, GST detail, fixed-asset schedule, franking-account reconciliation, and tax reconciliation;
- every company-return label and supported schedule grouped in the official order;
- source-to-label drill-down to accounting facts, adjustments, elections, rules, and evidence;
- tax calculation, PAYG instalments, credits, payments, expected payable/refund, and differences from the prior return version;
- validation outcomes classified as blocker, warning, information, or unsupported; and
- a deterministic redacted review PDF and encrypted full handoff pack.

Exports use a trusted-main save dialog and return no destination path to React. The default review PDF masks TFN, refund-account details, credential/service identifiers, and unrestricted evidence content. A full accountant handoff uses the existing authenticated encrypted-backup envelope in a `.tammy-eofy` archive, requires a new user passphrase plus fresh authentication, and includes the complete return, statements, reconciliation, supported evidence manifest, descriptors, rule/bundle fingerprints, and verification instructions. The passphrase is never stored or logged. Main writes a mode-`0600` temporary file in the chosen destination directory, fsyncs, atomically renames, and refuses overwrite unless the native dialog explicitly confirms it; cancellation or failure removes the temporary file. Audit retains only export type, return ID, content hash, safe destination classification, actor, and time. Tammy does not manage or delete the user's successfully exported plaintext review PDF or encrypted archive.

Declaration freezes the return. It requires no blockers, explicit acknowledgement of all warnings, the exact installed declaration and SBR end-user terms, a link to the current SBR privacy statement, and a purpose-bound fresh authentication factor. Tammy retains the declaration text/version/hash, accepted terms versions, user, organisation, return ID, timestamp, and report hash.

### 5.5 Lodge and track

The production lodge action is present only when the exact return is declared and the runtime proves:

- a valid, signed, report-specific production bundle;
- accepted DSP registration evidence;
- a Product ID enrolled for the exact company-return service;
- accepted conformance/self-certification and production-access evidence;
- an accessible, unexpired RAM machine credential whose ABN matches the verified organisation;
- current end-user terms and privacy references;
- no expired registration/profile/component evidence; and
- a supported packaged macOS build profile.

The user unlocks the machine credential through the helper using transient password input and a purpose-bound fresh factor. The renderer receives only redacted credential status. The core asks the helper to pre-lodge the already frozen canonical report. Official information outcomes append without changing authority. A new official warning requiring acknowledgement moves the report to `PRELODGE_REVIEW`, supersedes the current declaration authority without changing report facts/payload, and requires `AcknowledgeReturnWarning` plus a new purpose-bound `DeclareCompanyReturn`; every declaration version remains retained. A blocker requiring value changes creates a replacement draft and cannot mutate the declared snapshot. The final lodge action shows the exact company, ABN, income year, report version, tax result, service, environment, current declaration, and acknowledged warnings before dispatch.

After dispatch, the screen shows queued, sent, processing, accepted, rejected, or outcome unknown. Tammy stores and displays the ATO conversation/submission identifier, timestamps, response codes, label-linked validation outcomes, status history, and official receipt. It never represents payment, refund, or ATO account balance as controlled by Tammy.

## 6. Domain model and ownership

### 6.1 Intake and evidence

`ImportBatch` owns source kind, safe display metadata, size, hash, parser version, duplicate relation, job status, diagnostics, and reviewed/committed state. `EvidenceDocument` owns encrypted-blob identity, MIME, hash, safe name, extraction result identity, review status, retention policy, and links to targets. `EvidenceCandidate` is immutable extractor output; `EvidenceReview` is the user's versioned interpretation.

The document module owns evidence metadata and encrypted blob references. The banking module owns transaction import batches and statement lines. Sales, purchases, accounting, assets, and annual tax own the business objects created from a review. An imported payroll summary remains evidence plus bounded reconciliation totals; this slice creates no payroll domain object, employee record, pay run, or STP fact. Modules communicate through narrow read/write ports in one unit of work; no module writes another module's tables.

### 6.2 Financial close

`FinancialClose` is the mutable checklist for one organisation and income year. `CloseCheck` has a stable rule ID, severity, source revision, result, affected references, and resolution. `FinancialCloseSnapshot` is immutable and includes:

- organisation, ABN, currency, period, and income year;
- financial revision and all owned subledger revisions;
- approved statement and trial-balance hashes;
- checklist and reconciliation results;
- accounting-rule, GST-rule, and asset-rule fingerprints;
- evidence-manifest and audit-head hashes; and
- sign-off user and time.

Only one current close may exist for an organisation/year, but every frozen snapshot remains addressable.

### 6.3 Annual tax reconciliation

`TaxAdjustment` is versioned while the return is collecting and immutable after declaration. `TaxElection` records only an election explicitly supported by the installed rule bundle. `TaxReconciliation` is a deterministic projection from the close, adjustments, elections, and rule bundle; it is never edited directly.

The invariant is:

```text
accounting profit before tax
+ additions
- subtractions
- applied eligible revenue losses
= taxable income or tax loss
```

Every term has money/currency, sign convention, tax year, rule ID, provenance kind, source references, and evidence references. The calculation engine rejects missing provenance and mixed rule-bundle fingerprints.

### 6.4 Company return

`CompanyReturn` owns organisation, reporting period, income year, relationship kind (`ORIGINAL`, `REPLACEMENT`, or `AMENDMENT`), related return/attempt IDs, report-bundle fingerprint, source close ID/hash, tax-reconciliation hash, status, validation revision, declared snapshot hash, and delivery summary.

`ReturnFact` stores a bundle-defined fact ID and typed value. Its provenance is exactly one of:

- derived from a frozen book/report fact;
- derived from a reviewed tax adjustment;
- copied from a verified organisation/company profile;
- explicitly entered with retained evidence;
- explicitly elected under a bundle rule; or
- derived by a deterministic calculation rule.

Each fact records its rule/mapping ID and source references. Unknown official fields are not stored as generic key/value input. A report bundle must declare every supported field and its value type, cardinality, validation, mapping, and user presentation.

`CompanyTaxProfileInput` and `CompanyReturnInput` accept only the typed fields declared by `au-company-return-2026-preparation-v1`; they retain source/evidence and reviewer identity. `TaxElection` accepts only a bundle-declared election ID and typed choice, with required explanation/evidence. `ValidationAcknowledgement` binds a specific warning ID and validation revision to an actor and fresh factor. `Declaration` is append-only and binds the immutable report hash, current validation revision, all warning acknowledgements, exact wording/terms/privacy versions, actor, fresh factor, and time. A newer declaration supersedes authority from the older declaration without deleting it or changing the report payload.

### 6.5 Submission records

`SubmissionAttempt` owns return snapshot hash, official payload hash, environment, product identifier fingerprint, service ID, operation ID, idempotency identity, dispatch state, status, and response hashes. It never stores credential bytes, credential passwords, Product ID bytes, raw endpoint secrets, or unrestricted local paths.

`SubmissionReceipt` owns the bounded encrypted official response/receipt, safe display projection, conversation/submission identifier, received time, response schema fingerprint, and content hash. Status observations append; they do not overwrite history.

## 7. Report bundles and tax rules

### 7.1 Bundle identity

Company-return support is enabled only by an immutable bundle for one exact income year and official service version. A bundle contains or references:

- company profile/status questions and supported return facts;
- official label/service mappings;
- calculation and rounding rules;
- supported schedules and attachment rules;
- business validation and Schematron assets;
- declaration, privacy-reference, and end-user-terms versions;
- canonical message construction metadata;
- official response-code mappings;
- conformance fixture identity and expected results; and
- provenance, publisher, version, effective period, checksums, and signature metadata.

Tammy distinguishes two bundle classes:

- a repository-owned preparation bundle, built from reviewed public ATO forms, instructions, legislation inputs, and Tammy golden fixtures; and
- an externally issued SBR service bundle containing the exact registered service artefacts needed for EVTE or production.

A preparation bundle can enable local workpapers and exports but cannot enable SBR submission. EVTE and production require the exact signed external bundle and matching signed registration/component profile. The capability registry fails closed for an absent year, entity, service, schedule, rule, or bundle fingerprint.

### 7.2 Rule execution

Rules are pure deterministic functions over typed facts. They cannot query wall-clock time, ambient locale, the network, or mutable application state. Dates use organisation timezone and explicit income-year context. Money uses integer minor units and bundle-defined whole-dollar reporting/rounding. Calculations retain unrounded inputs and the exact rounded fact sent to the ATO.

Every rule result includes rule ID, bundle fingerprint, inputs, output, and diagnostics. A rule upgrade never rewrites a declared report. Recalculating a collecting report under a new bundle creates a new report version and shows the differences.

### 7.3 Legal and artefact change intake

For each supported income year, release owners must record:

- official source URLs or externally issued artefact identifiers;
- retrieval date, checksum, signature, and licensing/conditions metadata;
- legal/accounting review of changed fields, rates, thresholds, elections, declarations, and validations;
- changed golden fixtures and expected output hashes;
- applicable EVTE/conformance results; and
- the exact application versions allowed to activate the bundle.

There is no evergreen or "latest" tax configuration.

## 8. Report lifecycle and amendments

The public states are:

```text
COLLECTING
BLOCKED
REVIEW_READY
DECLARED
PRELODGE_REVIEW
SUBMISSION_PENDING
DELIVERED
REJECTED
OUTCOME_UNKNOWN
REPLACED
SUPERSEDED_BY_AMENDMENT
```

`BLOCKED` and `REVIEW_READY` are persisted report states updated from deterministic validation, not cosmetic renderer states. `DECLARED` freezes facts, rules, calculations, evidence manifest, and payload input and points to the current immutable declaration version. `PRELODGE_REVIEW` means official warnings require acknowledgement and a fresh declaration over the same immutable report. `SUBMISSION_PENDING` begins before helper dispatch is committed. `DELIVERED` requires an accepted official status/receipt. `REJECTED` means the ATO definitively did not accept that attempt. `OUTCOME_UNKNOWN` prevents any replacement, amendment, or blind second lodge until official reconciliation resolves it. `REPLACED` and `SUPERSEDED_BY_AMENDMENT` retain links to the successor.

Correction semantics are explicit:

- A collecting or review-ready return is edited in place under expected-version/idempotency rules.
- A declared report that was never dispatched may have its declaration withdrawn. The immutable declared snapshot remains retained, becomes `REPLACED`, and a new `REPLACEMENT` draft starts from an existing or corrected close. It uses the original-lodgment operation because no ATO acceptance occurred.
- A definitively rejected attempt remains rejected. Corrections create a `REPLACEMENT` linked to the rejected attempt and use the original/resubmission operation prescribed by the 2026 bundle; they are not labelled amendments.
- An `OUTCOME_UNKNOWN` report is locked. If status proves acceptance it becomes delivered; if status proves non-acceptance it becomes rejected and may be replaced. If the official status service cannot resolve it, the UI requires ATO/DSP support and never creates another submission identity.
- Only a delivered, accepted original can create an `AMENDMENT`. It states a reason, uses a newly frozen close where book corrections are needed, calculates exact fact/tax differences, undergoes full validation/declaration, and invokes the official 2026 amendment operation.

`StartFinancialCloseCorrection` creates the new source revision used by a replacement or amendment while preserving the original books, close, return, declaration, payload, response, and receipt. An edited copy is never presented as the original report.

## 9. SBR delivery design

### 9.1 Core/helper contract

The core gives the SBR helper a bounded generated command containing:

- report ID and declared snapshot hash;
- canonical official payload bytes produced from the accepted signed bundle;
- payload hash and schema/bundle fingerprints;
- organisation/ABN binding;
- exact environment, product identifier, and service identifier selected by signed runtime authority;
- operation and pending IDs; and
- operation type: pre-lodge, lodge, status, or reconcile.

The renderer cannot supply any of these authority fields. The helper independently verifies request bounds, profile fingerprints, service scope, ABN/credential binding, credential state, Product ID state, and payload hash before use.

The helper returns a bounded generated response with outcome, stable code, safe identifiers, response bytes/hash, status, and retry classification. The core validates and persists the response in one unit of work and appends the audit event before acknowledging completion to the renderer.

### 9.2 Dispatch and recovery

The existing prepare/commit/abort/reconcile mutation protocol is extended to report operations. Before network dispatch, core records an intent tied to the immutable report and the helper durably prepares the operation. After a definitive response, core records the result and tells the helper to commit. If either process crashes, startup reconciliation compares the pending IDs and hashes before allowing another action.

Automatic retry is allowed only when the helper proves that no request bytes could have been accepted and the bundle marks the operation retryable. A timeout, connection loss after dispatch, or malformed response after a possible acceptance becomes `OUTCOME_UNKNOWN`; Tammy invokes the official status/reconciliation service before another lodge. The identical report cannot acquire a second submission identity merely because the UI action is repeated.

### 9.3 Environments

- **Simulator:** deterministic, synthetic, network-disabled, no live credential accepted, and visibly labelled not lodged.
- **EVTE:** non-production official endpoints and issued test credentials only; requires signed registration and service artefacts.
- **Production:** exact signed production endpoint/profile, real Product ID and ABN-bound machine credential, accepted report-specific access, and release-approved package only.

Environment authority comes from fixed installed signed files and build profile. It cannot be selected with a renderer toggle, command-line URL, environment variable, or user-entered endpoint.

## 10. Security and privacy

- Original evidence is encrypted with the workspace key using opaque blob IDs and authenticated metadata. Plaintext derivatives and helper temporary files are not retained.
- SQLCipher stores accounting, tax, report, declaration, status, and receipt metadata. Sensitive official payloads and receipts are encrypted and covered by backup/evidence policy.
- Machine credentials and Product ID material remain in the SBR helper's approved secure vault/Keychain boundary and are excluded from workspace backup, logs, support bundles, renderer state, and core storage.
- Credential password, TOTP, and other fresh-factor input are transient, bounded, never logged or echoed, and cleared where mutable memory permits.
- File paths never enter renderer-visible business state, reports, audit payloads, exports, or support bundles. Main removes one-shot file handles after use, cancellation, expiry, navigation, renderer loss, or process exit.
- Untrusted documents and transaction files are parsed only by bounded sandboxed helpers with network disabled.
- SBR is the only product network path in this slice. The core and renderer remain network-disabled. Endpoint allowlisting, TLS/mTLS/authentication behaviour, and response bounds come from signed official profiles.
- Declaration and lodge require purpose-bound fresh authentication. The actor must have the organisation-level prepare/declare/lodge role. Role checks occur in the core and are revalidated inside the mutation unit of work.
- Every change, declaration, credential use, submission transition, response, receipt, export, close, reopen, and amendment appends a canonical audit event. Audit payloads contain hashes and safe identifiers rather than secrets or unrestricted source contents.
- Backup includes retained evidence, financial close, return snapshots, rule fingerprints, declarations, status history, and receipts. It excludes machine credentials, Product IDs, remembered passwords, active sessions, security bookmarks, transient intake handles, endpoint secrets, and operational logs.

The SBR privacy statement and end-user terms shown at declaration must come from the accepted bundle/profile. The app records agreement to the exact versions before submission, as required by the SBR conditions of use.

## 11. Public application contracts

Generated field numbers and filenames are implementation-plan mechanics. The semantic operations, authority, ownership, and outputs below are fixed: every command identifies one organisation and aggregate, carries authentication/idempotency/expected version, accepts only bundle-defined typed input, and returns the new aggregate version plus safe validation/status projections.

### 11.1 Native intake

- `SelectEvidenceFiles`
- `RegisterDroppedEvidenceFiles`
- `SelectTransactionFile`
- `CancelIntakeSelection`
- `SelectReturnExportDestination`

These are trusted-main channels, not Connect RPCs. They return opaque expiring handles and safe metadata only.

### 11.2 Documents

- `BeginEvidenceImport`
- `GetEvidenceImportJob`
- `ListEvidenceDocuments`
- `GetEvidenceReview`
- `SaveEvidenceReview`
- `CreateReviewedEvidenceTarget`
- `CancelEvidenceImport`

### 11.3 Banking

- `BeginStatementImport`
- `GetStatementImportReview`
- `CommitStatementImport`
- `ListStatementLines`
- `MatchStatementLine`
- `CreateTransactionFromStatementLine`
- `CompleteBankReconciliation`

### 11.4 Financial close

- `CreateFinancialClose`
- `GetFinancialClose`
- `ListCloseChecks`
- `ResolveCloseWarning`
- `FreezeFinancialClose`
- `ReopenFinancialClose`
- `StartFinancialCloseCorrection`
- `GetFinancialStatements`

### 11.5 Company tax

- `GetCompanyTaxProfile`
- `SetCompanyTaxProfile`
- `CreateCompanyReturn`
- `GetCompanyReturn`
- `ListCompanyReturnFacts`
- `SetCompanyReturnInput`
- `UpsertTaxAdjustment`
- `RemoveTaxAdjustment`
- `UpsertTaxElection`
- `RemoveTaxElection`
- `ValidateCompanyReturn`
- `AcknowledgeReturnWarning`
- `DeclareCompanyReturn`
- `WithdrawCompanyReturnDeclaration`
- `ExportCompanyReturnPack`
- `CreateCompanyReturnReplacement`
- `CreateCompanyReturnAmendment`

### 11.6 Submission

- `PreLodgeCompanyReturn`
- `LodgeCompanyReturn`
- `GetCompanyReturnSubmission`
- `RefreshCompanyReturnStatus`
- `ReconcileUnknownCompanyReturnSubmission`

All mutations carry authentication, an idempotency key, expected version where applicable, and purpose-bound fresh-factor context for high-risk operations. Queries are bounded and paginated. The desktop preload manifest imports the generated method registry and exposes each method explicitly; it does not expose a generic service/method tunnel.

## 12. Error model

Public failures use stable codes and safe messages. At minimum:

| Code | Meaning and recovery |
|---|---|
| `EVIDENCE_IMPORT_REJECTED` | Unsafe type, size, symlink, instability, parser limit, or unsupported content; retain no unsafe derivative and show a safe reason |
| `EVIDENCE_REVIEW_REQUIRED` | A candidate has not been approved; open its review |
| `IMPORT_DUPLICATE_REVIEW_REQUIRED` | File or transaction duplicates existing evidence; choose ignore or reviewed override |
| `FINANCIAL_CLOSE_BLOCKED` | One or more close checks failed; link to each check |
| `SOURCE_CLOSE_STALE` | Books changed after the return source snapshot; rebuild before declaration |
| `UNSUPPORTED_COMPANY_SCENARIO` | A detected fact requires an unsupported rule/schedule; export and seek professional handling |
| `REPORT_BUNDLE_UNAVAILABLE` | No exact accepted bundle for this income year/service; preparation or delivery remains unavailable as indicated |
| `REPORT_VALIDATION_FAILED` | One or more official or Tammy-owned blockers exist; link to facts and sources |
| `DECLARATION_REQUIRED` | The current immutable report has not been freshly declared |
| `PRELODGE_REVIEW_REQUIRED` | A new official warning superseded declaration authority; acknowledge it and create a new declaration over the unchanged report |
| `SBR_CREDENTIAL_NOT_READY` | Missing, locked, expired, revoked, inaccessible, or mismatched credential; open SBR readiness |
| `SBR_PRODUCT_SERVICE_NOT_READY` | Product/service registration, conformance, or runtime evidence is absent or stale |
| `ATO_VALIDATION_FAILED` | Official pre-lodge/lodge validation rejected the payload; attach label-linked outcomes |
| `SUBMISSION_OUTCOME_UNKNOWN` | Acceptance cannot be determined; status reconciliation is required and blind retry is blocked |
| `REPORT_REPLACEMENT_REQUIRED` | A declared but unaccepted return cannot be edited; withdraw/supersede authority and create a linked replacement |
| `AMENDMENT_REQUIRED` | An accepted delivered return cannot be edited; create a linked amendment from a new close when books changed |

Parser and external response details are reduced to reviewed stable codes before crossing into the renderer. Logs contain operation IDs and hashes, never document contents, report values, TFNs, credential material, passwords, or raw official payloads.

## 13. Task scenarios and operational knowledge

The Taskfiles remain the canonical front door. The implementation adds these scenario-oriented commands while retaining the existing build, package, release, and SBR readiness owners:

- `task dev:company-eofy` — launch the ordinary persistent local company-EOFY development app;
- `task dev:company-eofy:fresh` — launch a new retained workspace with no seeded accounting state;
- `task dev:company-eofy:demo` — launch a separate synthetic company dataset and visibly label it as demonstration data;
- `task test:company-eofy` — run focused contracts, core, helper, renderer, rule, and integration tests;
- `task test:company-eofy:e2e` — package and run the canonical local EOFY journey through deterministic simulated delivery;
- `task sbr:company-return:simulator` — launch the network-disabled company-return SBR simulator;
- `task sbr:company-return:evte` — validate signed external inputs and launch EVTE only when its adapter and artefacts exist;
- `task sbr:company-return:conformance` — run the exact installed official conformance suite and retain redacted evidence;
- `task sbr:company-return:production-readiness` — fail closed unless every external and release gate is current;
- `task package:company-eofy` — build and verify the ordinary macOS package without implying ATO production access; and
- `task release:company-eofy:check` — run clean-tree, contracts, rules, security, packaged E2E, App Store, and SBR external-gate checks without uploading or lodging.

Every task summary states its data root, supported platform, network authority, credential policy, environment, destructive/reset behaviour, retained evidence path, and what the result does not prove. No task accepts a credential password or live credential path on the command line. Production submission is an authenticated in-app action, never a Taskfile command.

## 14. Verification strategy

### 14.1 Rule and domain tests

- Unit tests cover every rule, mapping, rounding boundary, validation, lifecycle transition, permission, replay, conflict, stale-source, and unsupported-scenario branch.
- Property tests prove double-entry balance, close/report immutability, tax-reconciliation equations, provenance completeness, deterministic recalculation, and original/amendment preservation.
- Golden return fixtures cover at least: profitable service company, trading company with receivables/payables, current-year loss, eligible imported revenue loss, asset purchase/depreciation/disposal, ordinary dividend/franking activity, PAYG instalments/credits, ATO validation rejection, transport outcome unknown, and amendment.
- Each unsupported condition in section 3.3 has a fixture proving declaration and lodge are blocked without silent omission.
- Bundle tests prove exact year/service matching, signature/checksum verification, no fallback, change-impact manifests, and byte-identical output for identical inputs.
- `compliance/traceability/company-return-2026.csv` maps every section 3.2 requirement and 2026 supported return fact to its rule ID, official source/artefact ID, Protobuf fact, golden fixture, unit test, integration test, packaged E2E step, and activation mode. CI fails on an unmapped rule, fact, fixture, or capability.

### 14.2 Storage and integration tests

- SQLCipher integration tests cover all repositories, migrations, UoW rollback, crash points, revision changes, backup/restore, audit-chain verification, encrypted receipts, and exclusion of credential/session/intake state.
- Document/helper tests cover native, scanned, mixed, rotated, encrypted, corrupt, polyglot, decompression, oversize, symlink, FIFO, file-swap, cancellation, timeout, memory, and process-crash cases.
- Transaction-import tests cover CSV dialects, OFX/QFX, encoding, signs, dates, duplicates, balance checks, 100,000-row bounds, cancellation, and deterministic normalization.
- SBR helper tests cover credential/Product ID state, ABN mismatch, signed profile/service mismatch, pre-lodge, lodge, status, rejection, unknown outcome, restart reconciliation, response bounds, TLS/profile failure, and secret redaction.

### 14.3 Desktop and packaged E2E

The canonical macOS packaged E2E must use public renderer actions and generated APIs to:

1. create, unlock, and sign in to a fresh company workspace;
2. verify the organisation/ABN and configure the supported income year;
3. batch-select synthetic invoices, receipts, statements, payroll summary, and asset evidence through the real native intake boundary;
4. observe the real helper jobs, review candidates, create accounting targets, and prove no automatic posting;
5. import OFX/QFX and CSV fixtures, review duplicates, match/create transactions, and reconcile every account;
6. produce balanced statements, resolve the close checklist, back up, verify audit, and freeze the close;
7. add reviewed tax adjustments/elections, prepare the company return, drill from selected labels to sources, and export a deterministic pack;
8. prove a source mutation invalidates an undeclared return;
9. validate and declare using fresh authentication and exact terms;
10. submit through the network-disabled simulator using only synthetic credential authority, receive a deterministic official-shaped receipt, restart, and reproduce the complete state;
11. create and deliver a linked amendment while preserving the original; and
12. exit with zero unexpected sockets, helpers, core processes, plaintext temporary files, secrets, paths, or fixture canaries in logs/support output.

Separate packaged negative journeys cover every high-risk blocker and crash boundary. Tests must not seed final screen state directly in the renderer or database.

### 14.4 EVTE, conformance, and production evidence

When external artefacts are issued, the same canonical scenarios run against EVTE test identities and the exact official conformance suite. Evidence records exact source SHA, app/package/helper/bundle/profile fingerprints, toolchain, environment, service ID, fixture IDs, expected/actual hashes, timestamps, and redacted results.

Production activation requires a clean release candidate built from the exact conformance-tested revision and bundle. A production smoke test may perform only an ATO-approved non-lodgment connectivity/status operation unless the user separately authorises a real legal submission in the application. Automated tests never lodge a real return.

## 15. Delivery decomposition

This section is the programme index. Each numbered dependency stage receives its own focused implementation plan, acceptance gate, and final integration evidence; no single implementation plan spans multiple independent stages. The work remains one product outcome and is implemented in dependency order:

1. **Contracts and capability registry:** add company entity/report kinds, report lifecycle, generated use cases, transition fixtures, permissions, and fail-closed capability entries.
2. **Bounded batch intake:** replace renderer-owned single-file reading and pasted CSV with trusted-main intake, document/transaction helpers, encrypted evidence, review jobs, and duplicate controls.
3. **Accounting prerequisites:** complete the minimum sales, purchases, settlements, banking, financial reports, fixed assets, period controls, and cross-reconciliation needed by the supported company boundary.
4. **Financial close:** implement checklist, resolutions, freeze/correction, approved statements, evidence manifest, and stale-dependent-report behaviour.
5. **Annual tax reconciliation:** implement adjustments, elections, tax facts, deterministic book-to-tax calculation, provenance, and unsupported-condition guardrails.
6. **Company return preparation:** implement versioned bundle loading, report facts, supported schedules, calculation, validation, source drill-down, export, declaration, and amendments.
7. **SBR company-return simulator:** extend the core/helper journaled mutation contract with official-shaped pre-lodge/lodge/status/reconcile operations and deterministic fixtures.
8. **Desktop workbench and task scenarios:** connect the five-stage UI, accessible upload/review flows, report viewer, status/receipt, and scenario commands.
9. **EVTE and conformance:** integrate only the issued exact official transport/service artefacts, execute required suites, and retain evidence.
10. **Production activation:** accept signed production authority, run release and operational gates, and enable the lodge action only for the exact approved service/build/bundle.

Each implementation increment starts with a named failing test, implements the smallest behaviour, and passes focused tests before broader verification. Each dependency boundary has an integration test before the next slice consumes it.

## 16. External gates

The following are externally owned release gates rather than repository implementation work:

1. Register Tammy as a digital service provider through Online services for DSPs using myID and RAM authority.
2. Confirm in the ATO Service Registry and applicable Business Implementation Guide that the reporting party may use the selected company-return service for the intended self-lodgment flow.
3. Obtain the exact company-return SDK/taxonomy, message structure tables, validation rules, Schematron, response messages, implementation guides, and conformance suite for the selected income year/service.
4. Obtain the issued Product ID and service enrolment, EVTE access, test identities/ABNs/credentials, endpoint profiles, and component artefacts.
5. Execute end-to-end, EVTE, and official conformance cases and retain exact evidence.
6. Submit and have accepted the required report-specific self-certification and production-access declaration.
7. Obtain/renew the company's RAM machine credential and verify its ABN/authorisation is valid for the reporting party.
8. Maintain current SBR privacy/end-user terms, security requirements, product registration, change monitoring, and successful-test records.

Until gates 1–6 are closed, production lodging remains unavailable even if a real machine credential is installed. If gate 2 proves that the direct reporting-party use case is not permitted for this service, Tammy ships the complete deterministic accountant/agent handoff pack and does not expose production lodge.

## 17. Definition of done

The local company EOFY slice is complete when:

- a fresh packaged macOS application completes the canonical journey from batch source intake to frozen return, simulated receipt, restart, and amendment;
- every return value traces to verified company details, frozen books, explicit adjustments/elections, deterministic rules, and retained evidence;
- financial statements, subledgers, tax facts, close, tax reconciliation, company return, exports, backup/restore, and audit chain reconcile exactly;
- unsupported companies and missing report capabilities fail closed with actionable blockers;
- task scenarios provide clear setup, launch, test, package, release, simulator, EVTE, conformance, and readiness knowledge;
- all unit, property, contract, integration, security, renderer, and packaged E2E tests pass; and
- no UI, task, documentation, package, or evidence claims that a simulated, exported, EVTE, or manually confirmed report was lodged in production.

Direct production company-return lodgment is additionally complete only when every external gate in section 16 is closed for the exact report year/service/build, the EVTE and conformance evidence passes, production readiness passes, and an authorised user can submit a real declared return in-app and retain the official accepted status/receipt.

## 18. Authoritative external references

- [ATO overview of digital services](https://softwaredevelopers.ato.gov.au/overview-of-our-services)
- [SBR software development steps](https://www.sbr.gov.au/digital-service-providers/software-development-steps)
- [Online services for digital service providers](https://www.sbr.gov.au/digital-service-providers/software-development-steps/online-services-dsps)
- [SBR implementation support products](https://www.sbr.gov.au/digital-service-providers/sbr-implementation-support-products)
- [SBR disclaimer and conditions of use](https://www.sbr.gov.au/digital-service-providers/developer-tools/sbr-disclaimer-and-conditions-use)
- [ATO machine credentials and RAM](https://softwaredevelopers.ato.gov.au/usingmygovidramandmachinecredentials)
- [ATO machine-to-machine authentication](https://softwaredevelopers.ato.gov.au/M2M)
- [ATO Software Developers Product Register](https://softwaredevelopers.ato.gov.au/product-register)
- [ATO company tax rates](https://www.ato.gov.au/tax-rates-and-codes/company-tax-rates)

Official artefacts issued through authenticated DSP channels are authoritative for the exact registered service and override public explanatory material where they differ. Tammy records that precedence in the accepted report bundle and release evidence.
