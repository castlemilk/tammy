# Tammy Core Business Accounting Suite Design

**Status:** Approved in interactive design; pending written-spec review  
**Date:** 2 August 2026  
**Delivery model:** Vertical slices on a shared accounting kernel  
**Product model:** Local-first, one organisation per encrypted workspace  
**Tax boundary:** Australian GST and BAS workpapers, validation, export, and simulator  

## 1. Purpose

This specification extends the approved [Tammy Local-First Accounting and Direct SBR Design](./2026-07-19-tammy-local-first-accounting-sbr-design.md) from its foundation slice into a fully usable core business accounting suite.

The suite must let an Australian small business complete ordinary bookkeeping offline:

1. configure its chart of accounts and opening balances;
2. manage customers and suppliers;
3. issue invoices and credit notes;
4. record bills, receipts, and supplier credits;
5. receive, make, allocate, and reverse payments;
6. import and reconcile bank and credit-card statements;
7. attach and locally extract bills, receipts, and statements;
8. maintain a correct double-entry general ledger;
9. calculate GST on either cash or non-cash basis;
10. prepare, validate, simulate, and export a BAS workpaper;
11. produce financial and subledger reports;
12. review an immutable audit trail; and
13. create, verify, restore, and migrate encrypted backups.

Every capability is delivered as an executable vertical slice. A slice is complete only when its domain rules, Protobuf contracts, generated Go and TypeScript types, encrypted persistence, desktop workflow, permissions, audit evidence, migration behaviour, automated tests, and packaged Electron E2E scenario all pass.

## 2. Scope and product boundary

### 2.1 Included

- One organisation per encrypted workspace.
- Multiple unique local users with additive roles.
- Australian dollar, single-currency accounting.
- Cash and non-cash GST accounting bases.
- Contacts that may be customers, suppliers, or both.
- Quotes, sales invoices, sales credit notes, customer payments, and allocations.
- Supplier bills, supplier credits, outgoing payments, and allocations.
- Unallocated receipts and payments.
- Chart of accounts, opening balances, manual journals, periods, reversals, general ledger, and trial balance.
- Bank and credit-card accounts.
- Manual CSV and OFX/QFX statement import.
- Exact duplicate prevention, reviewable similarity warnings, matching, transfers, and reconciliation.
- PDF, PNG, and JPEG evidence for bills, receipts, and bank or credit-card statements.
- Local native-PDF extraction, selective local OCR, confidence and page-coordinate evidence, and mandatory human review before posting.
- Profit and loss, balance sheet, cash-flow statement, trial balance, general ledger, journal, GST detail, aged receivables, aged payables, customer statement, and supplier statement reports.
- BAS workpapers with complete source provenance, local validation, simulator flow, and PDF/CSV/Protobuf-JSON evidence exports.
- Hash-linked audit history and signed evidence exports.
- Encrypted portable backup, restore, integrity verification, and migration recovery.
- macOS arm64 and Windows x86-64 packaged-app validation under the platform matrix in the foundation design.

### 2.2 Explicitly excluded

- Payroll and Single Touch Payroll.
- Inventory, landed cost, and cost-of-goods stock movements.
- Multi-currency accounting or foreign-exchange revaluation.
- Multi-organisation or accountant-practice switching inside one workspace.
- Live bank feeds.
- Production ATO/SBR submission.
- Income-tax return preparation or lodgment.
- Electronic invoicing networks, payment gateways, and card processing.
- Purchase-order, stock-receipt, fixed-asset-register, depreciation-schedule, budgeting, and project-accounting modules.
- Vendor-hosted identity, accounting storage, extraction, synchronisation, or submission relay.
- Automatic approval or posting based on OCR, matching scores, or other heuristics.

Live bank-feed and ATO/SBR adapters remain explicit, disabled boundaries. The local workflows remain complete when every external adapter is absent.

## 3. Architectural decisions

### 3.1 Selected delivery architecture

Tammy uses vertical slices built on one shared accounting kernel. Source modules own business workflow and evidence. They do not write balances or journal tables directly.

```text
Contacts ─┬─ Sales documents ───────┐
          └─ Purchase documents ────┤
Documents/OCR ── reviewed drafts ───┤
Settlements ── payments/allocations ┤
Banking ── matches/transfers ───────┤
                                    ▼
                         Accounting posting port
                                    │
                         ┌──────────▼──────────┐
                         │ Accounting kernel   │
                         │ journals + periods  │
                         │ GST posting facts   │
                         └───────┬─────────────┘
                                 │ immutable projections
                  ┌──────────────┼──────────────┐
                  ▼              ▼              ▼
             Tax and BAS      Reporting     Audit/evidence
```

The accounting kernel is the only write authority for journals and balances. A source workflow submits a typed posting intent. The kernel validates accounts, period, organisation, currency, source revision, GST treatment, idempotency, and balancing before it creates an immutable journal.

### 3.2 Foundation controls remain normative

The runtime process, encryption, renderer sandbox, Electron-main boundary, loopback TLS, local capability, unit-of-work, authentication, TOTP, audit, backup, SBR isolation, and one-organisation controls in the foundation design remain normative. This specification adds business modules and contracts without weakening those controls.

### 3.3 Rejected alternatives

**Domain-by-domain backend first:** This delays real user and cross-domain validation until most data structures are already expensive to change.

**Broad prototype followed by hardening:** This permits posting, tax, reconciliation, and migration mistakes to become structural. Financial correctness cannot be postponed.

**Opaque Protobuf blobs as the primary database:** This would weaken relational constraints, indexes, joins, migrations, report queries, and repair diagnostics. Protobuf is the application and interchange model; normalized encrypted SQLite remains the persistence model.

## 4. Buf and Protobuf as the application data model

### 4.1 Source-of-truth rule

`buf.yaml` governs every application boundary. Protobuf definitions are the source of truth for:

- Connect RPC services;
- commands and command results;
- query filters and projections;
- common identifiers, money, dates, decimals, pagination, and source references;
- immutable domain and audit event payloads;
- document-helper requests, progress, and results;
- import staging and diagnostic records exposed to callers;
- report snapshots and evidence manifests;
- simulator fixtures and cross-language golden fixtures; and
- supported machine-readable exports.

Buf generates Go messages and Connect handlers for the core and Protobuf-ES messages for Electron main, preload, renderer, fixtures, and tests. Handwritten duplicate request or response interfaces are prohibited.

Normalized SQLite tables remain module-owned implementation details. Posted accounting values are stored as typed columns and constraints, not as unqueryable serialized aggregates. A deterministic Protobuf encoding may additionally be retained for idempotency results, audit event envelopes, report snapshots, evidence manifests, and export artefacts.

### 4.2 Repository structure

```text
proto/tammy/v1/
  common.proto
  system.proto
  workspace.proto
  identity.proto
  organisation.proto
  contact.proto
  accounting.proto
  sales.proto
  purchases.proto
  settlements.proto
  documents.proto
  banking.proto
  reporting.proto
  tax.proto
  audit.proto
  events.proto
  fixtures.proto
```

All initial files remain in `tammy.v1` so cross-domain references are explicit without premature package versioning. Files define application intent rather than mirroring database rows.

The initial Connect services are:

- `SystemService`
- `WorkspaceService`
- `IdentityService`
- `OrganisationService`
- `ContactService`
- `AccountingService`
- `SalesService`
- `PurchasesService`
- `SettlementService`
- `DocumentService`
- `BankingService`
- `ReportingService`
- `TaxService`
- `AuditService`

### 4.3 Common scalar and reference rules

- Resource IDs are opaque UUIDv7 strings wrapped by domain-specific message fields. Callers never infer meaning from an ID.
- `Money` contains an ISO 4217 currency code and signed `int64` minor units.
- Posted values never use binary floating point.
- Rates and quantities use a signed coefficient and declared decimal scale.
- Civil accounting dates use a local `CivilDate` message, not UTC timestamps.
- Instants use `google.protobuf.Timestamp` and are written by the core clock.
- Update commands use `google.protobuf.FieldMask` with an expected aggregate version.
- Persistent commands carry a UUID idempotency key.
- Cross-module provenance uses `SourceRef { type, id, revision, content_hash }`.
- Page evidence uses a page number and normalized bounding rectangle, plus extraction engine, engine version, and confidence.
- Every enum begins with an `_UNSPECIFIED = 0` value.
- Removed fields reserve both name and number.
- Public messages and fields carry comments and stable semantic meaning.

Protovalidate annotations enforce local shape constraints where practical. Domain services remain responsible for authorisation, cross-field rules, current-state checks, ledger balance, period rules, tax calculations, and cross-aggregate invariants.

### 4.4 Renderer and IPC transport

Electron main remains the only Connect client because it owns the per-launch local-core capability and certificate pin. The renderer never receives a generic RPC or network primitive.

For each allowlisted use case:

1. the renderer creates a generated Protobuf request;
2. the request is encoded to a `Uint8Array` for IPC;
3. preload checks the exact channel and maximum payload size;
4. Electron main decodes the expected message, invokes the generated Connect client, and encodes the expected response;
5. the renderer decodes the response with the generated schema.

The preload API exposes one named method per approved use case. It does not accept a service or method name from the renderer. This keeps Protobuf end-to-end without turning IPC into an unrestricted tunnel.

### 4.5 Buf quality gates

CI and local verification run:

- `buf format --diff --exit-code`;
- `buf lint` using the existing `STANDARD` rules;
- `buf breaking` against the protected base branch using `FILE` compatibility;
- pinned generation through `buf.gen.yaml` and `buf.lock`;
- generated-tree cleanliness checks;
- Go and TypeScript compile checks using generated types; and
- binary and Protobuf-JSON golden round-trips for shared fixtures.

A schema change and its generated output, migration, compatibility test, and consumer changes land atomically. Generated files are never manually edited.

## 5. Transaction and module rules

### 5.1 Unit of work

Every financial command executes in one `platform.UnitOfWork`. The application service:

1. authorises the actor;
2. elects the idempotent command execution;
3. loads aggregates through module repositories or documented ports;
4. invokes domain behaviour;
5. obtains and posts any accounting intent;
6. appends a typed Protobuf audit event;
7. persists the deterministic result; and
8. commits once.

Any failure rolls back the source state, journal, allocations, reconciliation changes, idempotency result, and audit event together. Domain modules never call `Commit` and never query another module's tables.

### 5.2 Module ownership

| Module | Owns | Does not own |
|---|---|---|
| Contacts | Party identity, addresses, communication details, customer/supplier flags | Issued-document snapshots or balances |
| Sales | Quotes, invoices, invoice lines, credit notes, delivery state | Journal rows, payment rows, bank lines |
| Purchases | Bills, bill lines, supplier credits, source-document relationship | Journal rows, payment rows, extracted raw evidence |
| Settlements | Incoming/outgoing payments, reversals, allocations, overpayments | Invoice/bill content or bank evidence |
| Documents | Encrypted blobs, extraction jobs, candidates, field evidence, review decisions | Approved bills, journals, or statement imports |
| Banking | Financial accounts, import batches, statement lines, matches, transfers, reconciliations | Sales/purchase documents or journal calculations |
| Accounting | Accounts, journals, lines, periods, posting facts, source links | Source workflow state or report snapshots |
| Tax | BAS workpapers, validations, field provenance, report states | Mutable journal/source data or transport credentials |
| Reporting | Read projections and generated report artefacts | Source-of-truth balances |
| Audit | Hash-linked audit envelopes and signed evidence exports | Domain mutation |

Application orchestrators compose ports under one transaction. Cross-module ports accept the active `TxScope` and return immutable Protobuf-defined application projections.

### 5.3 Required ports

The composition root adds these operation-level ports to the foundation set:

- `ContactSnapshotPort`
- `AccountingPostingPort`
- `ReceivableOpenItemPort`
- `PayableOpenItemPort`
- `SettlementTaxFactPort`
- `BankMatchTargetPort`
- `DocumentCandidatePort`
- `FinancialReportReadPort`
- `AuditAppender`

No port exposes repositories, SQL handles, or module table shapes.

## 6. Accounting kernel

### 6.1 Chart of accounts

An account has a code, name, type, optional subtype, normal balance, tax default, active status, report classification, and system/control designation. Initial account types are asset, liability, equity, revenue, other revenue, expense, other expense, and contra.

Workspace setup can install a versioned Australian small-business template. The user may add, rename, archive, or reactivate ordinary accounts. System control accounts for receivables, payables, GST receivable, GST payable, current-year earnings, and retained earnings cannot be deleted or repurposed. An archived account remains visible in history and rejects new postings.

Opening balances use a dedicated, balanced conversion journal with a conversion date and mandatory opening-balance-equity line. The workspace records whether opening balances are draft or posted. Replacing posted opening balances uses reversal and replacement; it never edits prior lines.

### 6.2 Journals and postings

Every posted journal:

- belongs to the workspace organisation;
- has one currency, initially AUD;
- contains at least two lines;
- has equal debit and credit minor-unit totals;
- uses active, postable accounts;
- has a posting date in an open period;
- retains a unique source reference and source revision;
- retains tax-rule and rule-bundle provenance where relevant;
- is immutable after commit; and
- is linked to its reversal or correction when applicable.

Source modules submit `PostingIntent` messages. They do not submit arbitrary journal lines for system workflows. The kernel expands source lines using versioned posting policies and GST rules, then returns the committed journal projection. Manual journals use a separate command with stricter permissions and explicit debit/credit lines.

### 6.3 Corrections and periods

Posted journals cannot be edited or deleted. Corrections create a linked reversing journal and, when required, one linked replacement journal. Reasons are mandatory. A journal has at most one direct reversal and one correction replacement.

Closing a period blocks every new source posting, manual journal, allocation tax fact, or reversal dated on or before the close date. Reopening requires a workspace administrator, recent TOTP, and a reason. Reopening supersedes affected pre-dispatch BAS workpapers and is fully audited.

### 6.4 Ledger reconciliation invariants

The following must always agree for the same as-of instant:

- total journal debits and credits;
- receivables control-account balance and open customer items;
- payables control-account balance and open supplier items;
- each bank or credit-card ledger account and its reconciled statement balance;
- GST control-account movements and GST detail facts; and
- report totals and the underlying trial balance.

Invariant checks run in integration tests, before backup activation, after restore, after migration staging, and on explicit workspace verification.

## 7. Contacts, sales, and purchases

### 7.1 Contacts

One contact may be both a customer and supplier. It has display and legal names, ABN, addresses, email, phone, payment terms, default accounts and tax codes, notes, active status, and an optimistic version.

Issued sales documents and approved purchase documents retain a contact snapshot containing the displayed name, legal name, ABN, address, and delivery details used at the time. Later contact edits do not rewrite history.

Possible duplicates are reviewable suggestions based on normalized ABN, email, and name. The system never merges contacts automatically. A user-confirmed merge retains both IDs through an alias record and rewrites only mutable references.

### 7.2 Quotes

Quotes are non-posting documents with draft, sent, accepted, declined, expired, cancelled, and converted lifecycle states. A quote can be converted once into an invoice draft. Later quote edits do not alter the invoice draft.

### 7.3 Sales invoices and credit notes

An invoice draft is editable and has no ledger effect. Issuing it atomically:

1. freezes the document number, contact snapshot, lines, tax rules, terms, and totals;
2. posts debit receivables and credits revenue and GST payable as required;
3. appends the issued event; and
4. returns the invoice and journal projections.

Issued invoices are immutable. Lifecycle and settlement state are separate. Settlement state is derived as unpaid, part-paid, paid, or overpaid from credits and allocations.

A commercial reduction uses a sales credit note with its own number and posting. A data-entry correction uses an explicit reverse-and-replace workflow. Both retain links to the affected invoice and cannot reduce the remaining document balance below the allowed over-credit policy. Tammy does not silently void an issued invoice.

### 7.4 Bills and supplier credits

A bill draft is editable and has no ledger effect. Approval freezes its supplier snapshot, reference, dates, lines, tax treatment, attachments, and totals, then posts debits to expense or asset and GST receivable accounts and a credit to payables.

The pair `(supplier_contact_id, normalized_supplier_reference)` is unique among active approved bills unless a user records an explicit duplicate-reference override reason. Similar amount/date/reference findings are warnings, not automatic rejection.

Approved bills are immutable. Supplier credits and entered-in-error reversals are linked new documents with linked journals. Payment state is derived from credits and allocations.

## 8. Payments and allocations

A settlement records direction, date, amount, currency, financial account or clearing account, payer/payee contact, reference, method, source, and lifecycle. Recording a payment posts bank or clearing against receivables/payables. Reversing it creates a linked inverse payment and journal.

An incoming payment may allocate across multiple sales invoices or credit balances for the same contact. An outgoing payment may allocate across multiple bills or supplier credits for the same contact. A payment may remain partly or wholly unallocated. Allocations cannot exceed the payment remainder or open-item remainder.

Allocation changes after posting are represented by allocation reversal and replacement events. They do not edit prior tax facts. Optimistic versions and the containing SQLite write transaction prevent concurrent over-allocation.

For cash-basis GST, every allocation creates immutable tax-recognition facts. If a document contains mixed tax treatments, the allocation is distributed over remaining document-line gross amounts using exact rational arithmetic and the largest-remainder method. Ties use stable line ID order. Credits and reversals produce signed inverse facts. The sum of recognition facts can never exceed the source line's posted gross, net, or GST amount.

For non-cash GST, invoice issue and bill approval facts drive BAS; settlement allocation does not recognize GST again.

## 9. Banking and reconciliation

### 9.1 Financial accounts

A bank or credit-card account maps one-to-one to a postable ledger account and records account type, display name, institution, masked account number, opening statement date and balance, and active status. Full credentials and live-feed tokens are outside this scope.

### 9.2 CSV and OFX/QFX import

Imports use a staging workflow:

1. retain the encrypted source file and SHA-256 hash;
2. parse into a Protobuf staging result without ledger mutation;
3. require account and, for CSV, column/date/amount mapping confirmation;
4. validate dates, signs, balances, encoding, and row limits;
5. present duplicate and overlap diagnostics;
6. commit accepted immutable statement lines in one batch; and
7. append an audit event with source hash, parser version, counts, and exclusions.

Import profiles can be saved per financial account. Profiles store mappings and formats, never example financial values.

When OFX/QFX supplies a stable financial-institution transaction ID, exact uniqueness is scoped to the financial account and stable ID. Without one, Tammy computes a normalized candidate fingerprint from posted date, amount, normalized description/reference, and occurrence ordinal within the source statement. An exact prior source-line identity is blocked. Similarity across different sources is presented for review because two legitimate transactions can otherwise look identical.

Imported statement lines are immutable evidence. Excluding, matching, unmatching, or reconciling a line creates state events and links without changing imported date, amount, description, reference, or source identity.

### 9.3 Matching and transaction creation

Tammy suggests candidate matches using exact amount, compatible direction, date distance, contact aliases, document references, and existing allocation state. Scores and reasons are visible. Suggestions do not commit automatically.

A user may confirm one of these actions:

- match an existing incoming or outgoing payment;
- match multiple payments to one statement line;
- match one payment to multiple statement lines;
- create and allocate a payment;
- create a direct receive-money or spend-money transaction;
- create or complete a same-currency bank transfer;
- record bank fees or interest; or
- leave the line unmatched with a note.

Match amounts must sum exactly to the statement-line amount. Split creation is explicit. A transfer creates one linked accounting event between two financial accounts; each imported side can link to the same transfer. Different statement dates do not create a second journal.

### 9.4 Reconciliation

A reconciliation belongs to one financial account and statement period. It records opening balance, closing balance, statement start/end dates, included statement lines, included ledger movements, and a calculated difference.

Completion requires:

```text
opening statement balance + included statement-line movement = closing statement balance
closing statement balance - reconciled ledger balance = 0
```

Completed reconciliations are immutable. Undo requires a preparer or administrator, a reason, and no later completed reconciliation that depends on it. Undo creates a linked audit event and restores the affected match/reconciliation states without deleting evidence.

## 10. Document extraction

### 10.1 Supported documents and evidence

Document intake accepts PDF, PNG, and JPEG files for supplier bills, receipts, bank statements, and credit-card statements. The original bytes are encrypted and retained before parsing. All derivatives refer to the original SHA-256 hash.

Encrypted evidence includes MIME detection, byte length, page count, ingestion actor/time, source path display name, extraction engine versions, page images where necessary, candidate values, coordinates, confidence, review decisions, and final target document IDs.

### 10.2 Local extraction pipeline

An isolated native document helper uses a length-delimited Protobuf stdin/stdout protocol. It has no accounting database handle and no outbound network access.

```text
Original document
  → type and safety validation
  → pdf-inspector classification
       ├─ native/mixed text page → positioned local extraction
       └─ scanned/image page → render page → bundled Tesseract OCR
  → deterministic layout and field inference
  → Protobuf candidate set with coordinates/confidence
  → user review
  → explicit draft creation/update
  → separate approval/posting command
```

`firecrawl/pdf-inspector` is the first-stage Rust library for PDF classification, positioned text, reading order, tables, and per-page OCR routing. It performs no OCR itself. Scanned pages use a pinned, bundled Tesseract build and English orientation/language data initially. Tesseract TSV or hOCR output preserves word coordinates and confidence.

`baidu/Unlimited-OCR` is not bundled because its official inference path requires a large Python/CUDA model stack and trusted remote model code. A future optional adapter may call a user-installed, loopback-only Unlimited-OCR endpoint. It is disabled by default, has an explicit health/version check and consent screen, and never falls back to a cloud endpoint.

### 10.3 Extraction targets

Bill and receipt candidates include:

- supplier name and ABN;
- invoice or receipt number;
- issue, supply, and due dates;
- currency;
- description, quantity, unit amount, line total, and likely account;
- subtotal, GST, total, amount paid, and amount due;
- payment reference; and
- document-type classification.

Statement candidates include:

- institution and masked account number;
- statement start/end;
- opening and closing balances;
- transaction date, optional value date, amount, description, reference, and balance; and
- page and row evidence for each transaction.

Inferred contact, ledger account, tax code, and duplicate links are separate suggestions. They never replace extracted evidence.

### 10.4 Human review and safety

Extraction may create or update only a draft. Review shows the original page beside candidate fields, highlights source coordinates, surfaces confidence, and requires the user to resolve arithmetic or GST mismatches. Accepting extraction records every accepted, changed, and rejected candidate. Approval and posting remain separate authorised commands.

The helper enforces configured byte, page, pixel, memory, and processing limits; validates magic bytes rather than extensions; rejects encrypted PDFs until the user explicitly unlocks them in an approved flow; uses private temporary files; removes derivatives after encrypted ingestion; and redacts document content from logs. Crash, timeout, cancellation, or corrupt input leaves the original evidence and a retryable or terminal extraction result but no partial business document or journal.

## 11. GST, BAS, and reporting

### 11.1 GST facts

Every GST-bearing source line retains gross, net, GST, tax-rule ID, rule-bundle ID, control-account line IDs, source revision, and recognition basis. Exact integer/rational arithmetic and the foundation rounding rules apply.

The initial tax codes remain those in the foundation design and are extended only through immutable rule-bundle versions. A reporting period cannot contain mixed active bundles without an explicit future migration design.

### 11.2 BAS workpaper

Creating a BAS workpaper reads one immutable ledger revision and reporting profile. It stores:

- organisation and period;
- cash or non-cash basis;
- ledger revision;
- rule and mapping bundle versions;
- every BAS field value;
- field-to-GST-fact provenance;
- warnings and blocking validations;
- content hash; and
- creator and creation time.

Changing source accounting after creation marks a pre-dispatch workpaper stale. Regeneration creates a new version and retains the previous workpaper. Validation never edits calculated values.

The local simulator exercises prepare, validate, declaration preview, submit, technical failure, rejection, acceptance, and ambiguous-response reconciliation states without claiming ATO connectivity. User exports are:

- human-readable BAS workpaper PDF;
- field and source-detail CSV;
- canonical Protobuf JSON evidence bundle; and
- signed audit/evidence package.

Production SBR controls remain disabled until the external gates in the foundation design are satisfied.

### 11.3 Financial reports

Reports use accounting and subledger read projections at a pinned ledger revision. Initial reports are:

- profit and loss for a date range, with optional comparison;
- balance sheet as of a date;
- indirect cash-flow statement for a date range;
- trial balance as of a date;
- general ledger and journal detail;
- GST detail by code and source;
- aged receivables and aged payables using configurable ageing date and buckets;
- customer and supplier activity statements; and
- bank reconciliation summary and detail.

Report headings retain organisation identity and basis. Filters, generation time, ledger revision, and totals are included in exports. Profit and loss and balance-sheet totals reconcile to the same trial balance. Current-year earnings are derived for presentation; year-end close journals are explicit user actions, not silent rewrites.

## 12. Authorisation and audit

The foundation roles remain additive:

- `workspace_admin`
- `business_preparer`
- `business_lodger`
- `auditor`

Administrators manage workspace, users, settings, accounts, periods, migrations, and backups. Preparers manage contacts and ordinary bookkeeping, approve source documents, post permitted journals, allocate payments, and reconcile accounts. Lodgers can read relevant accounting and BAS evidence, accept declarations, and use only enabled simulator/SBR actions. Auditors have read-only access to accounting, reports, evidence, reconciliation, and the complete audit chain.

Renderer visibility is not authorisation. Every handler checks permissions in the core under the same transaction as its read or mutation.

Audit payloads are typed Protobuf events in a deterministic `AuditEventEnvelope`. The chain covers actor, session, organisation, command and idempotency IDs, event type, affected resource references, before/after semantic hashes where applicable, time, prior-event hash, and event hash. Sensitive values, passwords, keys, full document contents, and unrestricted contact details are excluded.

## 13. Storage, migration, backup, and recovery

Each module owns normalized SQLite tables, indexes, constraints, and repositories. Foreign keys, `CHECK` constraints, unique source references, integer money columns, and optimistic versions provide storage-level defence in depth. Cross-module access still occurs only through documented ports.

Migrations are ordered, forward-only, embedded SQL assets with IDs and checksums. Before any non-trivial migration:

1. close active user sessions;
2. create and verify an encrypted recovery snapshot;
3. copy the workspace into migration staging;
4. apply migrations transactionally where SQLite permits;
5. run foreign-key, integrity, audit-chain, journal-balance, subledger-control, and schema-version checks;
6. atomically swap the staged workspace into place; and
7. retain recovery metadata until the next verified backup.

Failure leaves the prior workspace active and records a local recovery diagnostic without exposing accounting data.

Portable backups include the encrypted database, evidence blobs, rule bundles, migration manifest, audit head, schema version, and checksums. Restore decrypts into staging, verifies the complete manifest and invariants, then uses the foundation external restore journal for atomic activation. E2E proves backup and restore into a fresh installation context.

## 14. Error and job model

Synchronous financial commands return stable typed error details, including resource, field path, current version, rule identifier, and safe remediation where applicable. Principal classes include validation, permission, stale version, duplicate source, closed period, imbalance, allocation conflict, reconciliation difference, stale report, extraction limit, and corrupted evidence.

The renderer maps typed errors to field-level or workflow-level guidance. It does not parse English strings or infer retry safety.

Document extraction, large imports, report rendering, backup, restore, and migration use persisted job projections with queued, running, completed, failed-retryable, failed-terminal, and cancelled states. Jobs report bounded progress through typed Protobuf messages. Restart recovery resumes only explicitly restart-safe stages. Financial commands are never retried blindly; persistent mutations use the foundation idempotency contract.

## 15. Verification strategy

### 15.1 Test layers

Every slice includes:

- pure Go domain tests;
- property tests for balancing, signs, GST rounding, allocation distribution, and reconciliation arithmetic;
- SQLite repository and constraint tests;
- cross-module unit-of-work rollback and concurrency tests;
- Connect contract and Protovalidate tests;
- Buf lint, breaking, generation, and golden-serialization tests;
- TypeScript generated-client and IPC codec tests;
- React workflow, accessibility, empty, loading, error, and recovery-state tests;
- document-helper Rust tests and native-binary contract tests;
- parser fixtures for CSV, OFX 1.x, OFX 2.x, QFX, text PDF, scanned PDF, mixed PDF, rotated pages, tables, corrupt input, encrypted input, and hostile-size limits;
- migration tests from every released schema version;
- backup/restore and audit-chain tamper tests;
- macOS and Windows packaging, signature, bundled-core, helper, and resource-manifest tests; and
- packaged Electron Playwright E2E using the real generated client, Go core, encrypted SQLite, native helper, and renderer.

Tests create state through public Protobuf commands, not direct production-database inserts, except repository/migration tests whose purpose is the storage boundary. Internal domain behaviour is not mocked in integration or E2E tests. Only explicitly external ATO/bank adapters and the optional Unlimited-OCR endpoint use deterministic simulators.

Clocks, IDs, job scheduling, and external outcomes are injectable. Tests do not depend on sleeps. Fixture documents are synthetic or redistributable and contain no real customer data.

### 15.2 Canonical packaged E2E oracle

The primary packaged-app scenario performs this complete month:

1. create and unlock an encrypted workspace;
2. create the organisation, users, and chart;
3. create customer and supplier contacts;
4. issue a GST-inclusive `$1,100.00` consulting invoice;
5. ingest a synthetic `$220.00` supplier bill PDF, inspect/extract it, review every candidate, and approve the bill;
6. import statement lines for the `$1,100.00` receipt and `$220.00` payment;
7. create or match payments and allocate them to the invoice and bill;
8. reconcile the bank account to `$880.00`;
9. verify the trial balance, receivables/payables controls, profit and loss, balance sheet, cash flow, aged reports, and bank reconciliation;
10. create and locally validate a BAS with `G1 $1,100`, `1A $100`, `1B $20`, and net GST payable `$80`;
11. export BAS evidence and verify its manifest;
12. close and restart the packaged app and prove persistence;
13. create an encrypted backup;
14. restore it in a fresh installation context; and
15. verify the audit chain and all golden balances again.

A second packaged scenario covers cash-basis GST with part-payments, mixed-tax lines, credit notes, supplier credits, payment reversal, exact and fuzzy duplicate imports, a transfer, direct bank fee, reconciliation undo/recompletion, a scanned-page OCR fallback, and stale BAS regeneration.

Negative packaged scenarios cover permission denial, closed-period posting, over-allocation, unbalanced manual journal, corrupt import, OCR failure, wrong backup passphrase, migration failure recovery, and duplicate idempotency submission.

### 15.3 Cross-projection oracles

Acceptance tests compare the same truth through independent public projections:

- journal totals versus trial balance;
- trial balance versus profit and loss plus balance sheet;
- receivables/payables control accounts versus aged open items;
- bank ledger balance versus reconciliation;
- GST control movements versus GST detail and BAS provenance; and
- restored workspace projections versus the pre-backup manifest.

### 15.4 Performance fixtures

Benchmarks cover at least 250,000 journal lines, 100,000 imported statement lines, 20,000 contacts, and 10,000 evidence documents. On supported reference hardware, ordinary paginated lists should respond within 300 ms, standard financial reports within two seconds, and a 10,000-line statement staging import within five seconds, excluding user review and OCR. Performance failures block release only after reference hardware and budgets are recorded in CI.

## 16. Delivery slices

### Slice 1: Ledger and GST kernel

Chart template, opening balances, accounts, manual/source posting engine, GST rules, periods, journal, general ledger, trial balance, invariant verification, and the first packaged accounting oracle.

### Slice 2: Contacts and receivables

Contacts, snapshots, quotes, invoices, credit notes, incoming payments, allocations, customer statements, aged receivables, and sales-to-ledger E2E.

### Slice 3: Payables and document intake

Bills, supplier credits, outgoing payments, allocations, aged payables, encrypted document evidence, pdf-inspector, Tesseract fallback, candidate review, and bill-to-ledger E2E.

### Slice 4: Banking and reconciliation

Financial accounts, CSV/OFX/QFX staging, duplicate diagnostics, match suggestions, confirmed matches, direct cash transactions, transfers, fees/interest, reconciliation, undo, and banking E2E.

### Slice 5: Reports and BAS

Complete financial reports, cash/non-cash GST fact aggregation, BAS versioning, local validation, simulator, evidence exports, stale regeneration, and reporting/BAS E2E.

### Slice 6: Release hardening

Full permission matrix, audit evidence export, migration from every release, backup/restore, hostile document/import cases, scale benchmarks, macOS/Windows packaged E2E, and release evidence.

Cross-cutting controls are implemented when first needed and remain release gates in every later slice. Slice 6 expands the matrix; it does not postpone security, backup behaviour, migrations, audit, or packaged validation.

## 17. Completion criteria

The core business accounting suite is complete only when:

- all six slices satisfy their vertical definition of done;
- no production UI control is a placeholder;
- every included workflow works offline in the packaged app;
- the canonical and negative packaged E2E suites pass on supported platforms;
- Buf formatting, lint, breaking, generation, and generated-tree checks pass;
- all financial values cross-reconcile through public projections;
- backup, restore, migration recovery, and audit verification pass with real encrypted workspaces;
- scanned and native PDFs both reach a reviewable draft without automatic posting;
- cash and non-cash BAS fixtures produce retained, explainable provenance;
- live bank and production ATO adapters remain disabled and make no unsupported product claim; and
- release evidence records tool versions, fixtures, checksums, platform results, and unresolved external gates.

