# Tammy Core Business Accounting Suite Design

**Status:** Approved in interactive design; written-spec review issues being addressed
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

### 4.6 Canonical encodings, descriptors, and unknown fields

Protobuf defines type and semantic meaning; it does not replace the foundation audit chain's RFC 8785 canonical encoding.

The core applies these exact rules:

- Connect handlers reject unknown fields on persistent command requests. A newer client cannot silently make an older core hash or execute only the fields it understands.
- The semantic idempotency hash covers the fully-qualified message name, schema fingerprint, and the RFC 8785 encoding of the normalized Protobuf JSON mapping after authentication metadata and idempotency key are removed.
- Default and absent values are normalized according to each field's declared presence semantics before hashing. Map fields are prohibited in hash-bearing command and audit payloads unless the enclosing specification defines a stable repeated-entry replacement.
- An audit event has a typed Protobuf payload, but its chain bytes remain the predecessor's RFC 8785 canonical event representation. The canonical event includes the Protobuf type URL, schema fingerprint, and normalized JSON payload.
- Evidence bundles retain `payload.pb` as the exact stored bytes, `payload.json` as the RFC 8785 canonical Protobuf JSON view, and `descriptors.pb` as the transitive `FileDescriptorSet` needed to interpret that payload. The manifest hashes the stored bytes; a verifier never reserializes a payload to decide whether its hash is valid.
- Every release retains its descriptor-set fingerprint and compatible decoder fixtures. Removing a field reserves its name and number; evidence decoding preserves unknown binary fields when it reads and re-emits a stored binary payload.

This contract is cross-language tested in Go and TypeScript and is used by idempotency records, report snapshots, simulator fixtures, and standalone evidence verification.

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

| Port | Implemented by | Called by | Operations and result |
|---|---|---|---|
| `ContactSnapshotPort` | Contacts | Sales, Purchases, Settlements | `Snapshot(tx, contact_id, expected_version) -> ContactSnapshot`; rejects inactive, wrong-role, or stale contacts |
| `AccountingPostingPort` | Accounting | Sales, Purchases, Settlements, Banking orchestrators | `Post(tx, actor, PostingIntent) -> PostedJournal`; `Reverse(tx, actor, SourceRef, date, reason) -> PostedJournal`; source type/revision uniqueness makes both idempotent inside the command |
| `ReceivableSourcePort` | Sales | Settlements, Reporting | `Allocatable(tx, invoice_or_credit_id) -> ReceivableSource`; returns immutable original/credited amounts, contact, currency, line-level tax facts, lifecycle, and source revision |
| `PayableSourcePort` | Purchases | Settlements, Reporting | `Allocatable(tx, bill_or_credit_id) -> PayableSource` with the equivalent supplier projection |
| `AllocationReadPort` | Settlements | Sales, Purchases, Reporting, Tax | `ForSources(tx, source_refs, as_of) -> AllocationSet`; includes allocation/reversal IDs, financial and tax amounts, consideration dates, and settlement revision |
| `SettlementTaxFactPort` | Settlements | Tax | `Snapshot(tx, organisation, period) -> SettlementTaxFactSet`; returns immutable recognition and adjustment facts plus `tax_source_revision` |
| `PaymentMatchPort` | Settlements | Banking orchestrators | `Matchable(tx, payment_id) -> PaymentMatchProjection`; validates direction, amount remaining, financial account, state, and version |
| `CashTransactionPort` | Accounting | Banking orchestrators | `CreateSpend`, `CreateReceive`, `CreateTransfer`, and `Reverse` accept typed intents and return journal/source projections |
| `ReviewedDocumentPort` | Documents | Purchase and Banking intake orchestrators | `Accepted(tx, review_id, target_kind, expected_version) -> ReviewedCandidate`; returns a sealed candidate and evidence refs exactly once per target command |
| `TaxReportImpactPort` | Tax | Every source-changing orchestrator | `ApplySourceImpact(tx, command) -> TaxImpactProjection`; applies the state rules in Section 11.2 atomically |
| `FinancialReportReadPort` | Accounting, Settlements, Banking composition adapter | Reporting | `Snapshot(tx, query, financial_revision) -> FinancialSnapshot`; returns typed, immutable input sets without table access |
| `AuditAppender` | Audit | Every command handler | `Append(tx, AuditEventDraft) -> AuditEventProjection` using the same unit of work |

No port exposes repositories, SQL handles, or module table shapes. All port requests and projections are Protobuf messages or generated application types with a one-to-one Protobuf representation.

### 5.4 Public use-case catalogue

Read queries require any role with accounting-read permission and do not take idempotency keys. Every persistent command below takes an expected aggregate version where it changes an existing aggregate and follows the foundation idempotency contract. `admin` means `workspace_admin`; `preparer` means `business_preparer`. Administrators may perform preparer commands. Principal failures are typed error details, not English-only strings.

| Service and RPC | Role | Request → result | Principal failures |
|---|---|---|---|
| `ContactService.CreateContact` | admin, preparer | contact fields → contact | duplicate ABN warning unresolved, invalid defaults |
| `ContactService.UpdateContact` | admin, preparer | contact ID, version, field mask, values → contact | stale version, invalid field, merged contact |
| `ContactService.SetContactStatus` | admin, preparer | contact ID, version, active, reason → contact | referenced draft, stale version |
| `ContactService.MergeContacts` | admin | source/target IDs, versions, resolution choices → target and alias | conflicting ABN, issued-history rewrite, already merged |
| `ContactService.GetContact/ListContacts` | accounting read | ID or filters/page → contact projection/page | not found, invalid cursor |
| `AccountingService.CreateAccount` | admin, preparer | code, name, type/classification/defaults → account | duplicate code, invalid classification |
| `AccountingService.UpdateAccount` | admin | ID, version, field mask → account | control field immutable, stale version |
| `AccountingService.SetAccountStatus` | admin | ID, version, status, reason → account | system/control account, non-zero draft dependency |
| `AccountingService.PostOpeningConversion` | admin | conversion date, account balances, open items, financial-account openings → conversion result | imbalance, control mismatch, GST remainder mismatch, already posted |
| `AccountingService.ReplaceOpeningConversion` | admin + fresh TOTP | prior conversion, replacement, date, reason → reversal/replacement | closed period, later dependency, mismatch |
| `AccountingService.PostManualJournal` | admin, preparer | date, memo, debit/credit/tax lines → journal | imbalance, closed period, invalid account/tax treatment |
| `AccountingService.ReverseJournal` | admin, preparer | journal, reversal date, reason → reversal | already reversed, system-source workflow required, closed period |
| `AccountingService.ClosePeriod/ReopenPeriod` | admin + fresh TOTP | end date or period ID/reason → period and tax impact | source conflict, unresolved transmission, stale version |
| `AccountingService.GetJournal/ListJournals/GetGeneralLedger/GetTrialBalance` | accounting read | ID or filters/as-of/page → typed projection | invalid period/cursor |
| `SalesService.CreateQuote/UpdateQuote` | admin, preparer | draft fields or ID/version/field mask → quote | invalid contact/line, terminal state |
| `SalesService.MarkQuoteSent/AcceptQuote/DeclineQuote/ExpireQuote/CancelQuote` | admin, preparer | quote ID/version, event date/reason → quote | invalid transition, stale version |
| `SalesService.ConvertQuoteToInvoiceDraft` | admin, preparer | accepted quote ID/version → invoice draft | already converted, inactive contact |
| `SalesService.CreateInvoiceDraft/UpdateInvoiceDraft/CancelInvoiceDraft` | admin, preparer | draft fields or ID/version → invoice | invalid line/totals, terminal state |
| `SalesService.IssueInvoice` | admin, preparer | draft ID/version, issue acknowledgement → issued invoice and journal | duplicate number, closed period, changed contact/tax rules |
| `SalesService.CreateCreditNote` | admin, preparer | contact, optional invoice, lines, reason → credit and journal | exceeds linked remainder, invalid standalone credit, closed period |
| `SalesService.CorrectInvoice` | admin, preparer | invoice, correction date/reason, replacement draft → reversal/replacement | allocated source unresolved, closed period, prior correction |
| `SalesService.Get/List…` | accounting read | IDs or filters/page → quotes, invoices, credits, open items | invalid cursor |
| `PurchasesService.CreateBillDraft/UpdateBillDraft/CancelBillDraft` | admin, preparer | draft fields or ID/version → bill | invalid supplier/line, terminal state |
| `PurchasesService.ApproveBill` | admin, preparer | draft ID/version, tax-document status, acknowledgement → bill and journal | duplicate reference unresolved, missing evidence acknowledgement, closed period |
| `PurchasesService.CreateSupplierCredit` | admin, preparer | supplier, optional bill, lines, adjustment-note status, reason → credit and journal | exceeds linked remainder, invalid standalone credit, closed period |
| `PurchasesService.RecordTaxDocumentEvidence` | admin, preparer | bill/credit, evidence ref, held date, document status/version → eligibility and optional GST reclassification | evidence mismatch, unsupported exception, closed/final tax period |
| `PurchasesService.CorrectBill` | admin, preparer | bill, correction date/reason, replacement draft → reversal/replacement | allocated source unresolved, closed period, prior correction |
| `PurchasesService.Get/List…` | accounting read | IDs or filters/page → bills, credits, open items | invalid cursor |
| `SettlementService.RecordCustomerReceipt/RecordSupplierPayment` | admin, preparer | contact, financial account, consideration date, amount, optional allocations → payment, allocations, journals | over-allocation, incompatible contact/currency, closed tax period |
| `SettlementService.RecordCustomerRefund/RecordSupplierRefund` | admin, preparer | contact, financial account, date, amount, credit/overpayment allocations → refund and journals | insufficient credit, incompatible direction, closed period |
| `SettlementService.AllocatePayment` | admin, preparer | payment/version, source allocations, effective tax handling → allocations and GST journals/facts | over-allocation, closed/final tax period, stale open item |
| `SettlementService.ReverseAllocations` | admin, preparer | allocation IDs, reason, tax treatment → inverse allocations/facts | reconciled dependency, later reallocation, closed/final period |
| `SettlementService.ReversePayment` | admin, preparer | payment ID/version, date/reason, allocation policy → payment and journal reversals | completed reconciliation, unresolved allocations, already reversed |
| `SettlementService.Get/List…` | accounting read | IDs or filters/page → payments, allocations, unallocated credits | invalid cursor |
| `DocumentService.IngestDocument` | admin, preparer | bytes through approved file handle, type hint → evidence and queued job | type/size/page limit, duplicate source, encrypted password required |
| `DocumentService.ProvidePdfPassword` | ingesting actor | job/version, ephemeral password → resumed job | wrong password, attempt limit, terminal job |
| `DocumentService.CancelExtraction/RetryExtraction` | ingesting actor, admin | job/version → job | commit point passed, terminal limit, stale version |
| `DocumentService.SaveReview` | admin, preparer | candidate/version, field decisions, target kind → sealed review | arithmetic unresolved, stale candidate, missing evidence |
| `DocumentService.CreateTargetDraft` | admin, preparer | sealed review/version → bill, spend-money, or statement-import draft | already consumed for target, target validation failure |
| `DocumentService.SupersedeDocumentReview` | admin, preparer | review/version, new target kind, reason → replacement review | posted target, stale version, missing reason |
| `DocumentService.Get/List…` | accounting read | evidence/job/review filters → projections | evidence unavailable, invalid cursor |
| `BankingService.CreateFinancialAccount/UpdateFinancialAccount` | admin | ledger account and institution/display/opening settings → financial account | wrong account type, duplicate mapping, opening mismatch |
| `BankingService.StageStatementImport` | admin, preparer | financial account, evidence, format/profile → staging job/result | corrupt/unsupported format, mapping required, limit |
| `BankingService.CommitStatementImport` | admin, preparer | stage/version, accepted/excluded rows → import batch and lines | exact duplicate, stale stage, balance/sign mismatch |
| `BankingService.SetStatementLineExclusion` | admin, preparer | line/version, excluded, reason → statement line state | completed reconciliation, active match, stale version |
| `BankingService.SaveImportProfile` | admin, preparer | account, mapping formats → profile | invalid mapping, unsafe formula/input |
| `BankingService.ConfirmMatch/Unmatch` | admin, preparer | line/payment components and versions → confirmed match state | sum mismatch, stale version, completed reconciliation |
| `BankingService.CreateSpendMoney/CreateReceiveMoney` | admin, preparer | line/version, accounting and tax fields → source, journal, match | closed period, invalid tax/account, line remainder |
| `BankingService.ConfirmCashTransactionDraft` | admin, preparer | reviewed-document cash draft/version → source and journal | arithmetic unresolved, evidence changed, closed period |
| `BankingService.CreateTransfer/CompleteTransfer` | admin, preparer | source/destination accounts, lines, date, amount → transfer, journal, matches | same account, amount mismatch, duplicate side |
| `BankingService.CreateReconciliation/UpdateReconciliation` | admin, preparer | account, statement range/balances, selected lines/version → draft | gap/overlap, opening mismatch, locked dependency |
| `BankingService.CompleteReconciliation/UndoReconciliation` | admin, preparer | draft/version or completed ID/reason → reconciliation | non-zero difference, unmatched amount, later dependency |
| `BankingService.Get/List…` | accounting read | account/import/line/match/reconciliation filters → projections | invalid cursor |
| `ReportingService.GenerateReport` | accounting read | report kind, parameters, expected financial revision → report snapshot | revision changed, classification missing, invalid range |
| `ReportingService.ExportReport` | accounting read | report ID, format, destination → persisted export job/artefact | stale report, destination failure, render failure |
| `TaxService.CreateBASWorkpaper/ValidateBAS` | admin, preparer | organisation/period or report ID/version → versioned workpaper/validation | mixed rules, missing tax evidence, stale source, blocking validation |
| `TaxService.RecordAdjustment` | admin, preparer | source, awareness date, note status, consideration rule → adjustment fact | unsupported rule, missing note, closed/final period |
| `TaxService.AcceptDeclaration` | lodger + fresh TOTP | report/version/content hash/acknowledgement → declaration | stale/superseded report, wrong declaration |
| `TaxService.ExportBAS` | admin, preparer, lodger, auditor | report ID/version, format/destination → export artefact | stale source, invalid format, evidence failure |
| `AuditService.VerifyChain/ExportEvidence` | admin, auditor | range/filter or destination → integrity/export result | chain mismatch, destination failure |

Query names abbreviated with `Get/List…` are expanded into explicit RPC declarations in their owning `.proto` files; there is no generic query RPC. The implementation plan must trace every declared RPC to at least one use-case row and E2E scenario.

### 5.5 State and idempotency rules

Draft creation, update, cancellation, extraction password challenges, and explicit job retries have the exact replay behaviour declared in their RPC comments. All committed financial mutations retain idempotency records for the workspace lifetime. Query, password-attempt, and cancellation polling operations are not retried automatically.

Aggregate lifecycle enums and their allowed transitions are declared in Protobuf comments and generated transition fixtures. The core owns transition enforcement. The renderer uses the same descriptors to present allowed actions but cannot override the core. Every terminal-to-nonterminal attempt returns `INVALID_STATE_TRANSITION` with current state and allowed operations.

## 6. Accounting kernel

### 6.1 Chart of accounts

An account has a code, name, type, optional subtype, normal balance, tax default, active status, report classification, cash-flow classification, and system/control designation. Initial account types are asset, liability, equity, revenue, other revenue, expense, other expense, and contra.

Workspace setup can install a versioned Australian small-business template. The user may add, rename, archive, or reactivate ordinary accounts. System control accounts for receivables, payables, current GST receivable, current GST payable, deferred GST receivable, deferred GST payable, GST evidence pending, pending GST adjustment asset/liability, current-year earnings, retained earnings, and opening-balance equity cannot be deleted or repurposed. An archived account remains visible in history and rejects new postings.

Opening balances use one staged conversion aggregate rather than an isolated journal. It contains:

- ordinary account balances;
- customer opening items with contact, original issue/due dates, outstanding gross/net/GST, tax-rule provenance, and prior GST attribution;
- supplier opening items with the equivalent fields;
- unallocated customer or supplier credits;
- each financial account's ledger-normal opening balance, latest statement date/balance, and explicitly outstanding statement or ledger items; and
- the balancing opening-balance-equity amount.

For non-cash GST, opening items are marked `PRIOR_PERIOD_ATTRIBUTED` and never enter a new BAS. For cash GST, each opening item states the gross/net/GST already attributed and the remaining deferred amount; future consideration can recognize only the retained remainder. The conversion validator rejects negative remainders and requires original equals attributed plus deferred for gross, net, and GST.

Posting the conversion atomically creates typed opening receivable/payable items, financial-account opening state, and one balanced conversion journal. Receivable and payable journal control totals must equal their opening-item totals. Each financial-account journal balance must equal its normalized statement opening plus explicitly outstanding items. Direct manual posting to a subledger or financial control account is prohibited.

The workspace records whether conversion is draft or posted. Replacing a posted conversion requires no later dependent posting, or an explicit full reversal/replacement plan that reverses every opening item and journal in one unit of work. It never edits prior lines.

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
- current, deferred, evidence-pending, and adjustment-pending GST control accounts and the matching tax-subledger states in Section 8; and
- report totals and the underlying trial balance.

Invariant checks run in integration tests, before backup activation, after restore, after migration staging, and on explicit workspace verification.

## 7. Contacts, sales, and purchases

### 7.1 Contacts

One contact may be both a customer and supplier. It has display and legal names, ABN, addresses, email, phone, payment terms, default accounts and tax codes, notes, active status, and an optimistic version.

Issued sales documents and approved purchase documents retain a contact snapshot containing the displayed name, legal name, ABN, address, and delivery details used at the time. Later contact edits do not rewrite history.

Possible duplicates are reviewable suggestions based on normalized ABN, email, and name. The system never merges contacts automatically. A user-confirmed merge retains both IDs through an alias record and rewrites only mutable references.

### 7.2 Document numbering and arithmetic

Sales invoices, sales credit notes, and quotes have separate organisation-scoped numbering sequences. A sequence has a prefix, next positive integer, and padding policy. The final number is allocated transactionally only when an invoice or credit is posted or a quote is first marked sent. Draft display numbers are explicitly non-final. Gaps remain visible and are never reused; uniqueness is enforced by document type and number. Supplier bills retain the supplier's reference and use an internal immutable Tammy sequence as a second identity.

Every source document declares `TAX_EXCLUSIVE` or `TAX_INCLUSIVE`. All ordinary lines have a positive quantity, non-negative unit price, account, description, and tax rule. Credits use positive line values and the document kind reverses the accounting sign; negative invoice/bill quantities are prohibited.

The exact line algorithm is:

1. quantity is a scaled integer with scale zero through six;
2. unit price is scaled money with zero through six digits beyond the currency's minor unit;
3. exact rational multiplication produces the pre-discount amount;
4. the pre-discount amount is rounded once to minor units, nearest with an exact half away from zero;
5. the line may have either a basis-point percentage discount or fixed-minor-unit discount, never both;
6. percentage discount is rounded by the same rule and fixed discount cannot exceed the pre-discount amount;
7. for tax-exclusive lines, the discounted amount is net and the rule calculates GST before gross is set to net plus GST;
8. for tax-inclusive lines, the discounted amount is gross and the rule extracts GST before net is set to gross minus GST; and
9. document subtotal, GST, and total are exact sums of finalized line amounts.

There is no hidden document-level rounding or whole-document discount. Freight, surcharges, discounts across the whole sale, and rounding differences are explicit lines with explicit accounts and tax rules. A supplier document whose displayed total differs by at most one cent may use a user-visible rounding line to the configured rounding account; larger mismatches block approval.

Linked credits cannot exceed the source document's uncredited gross, net, and GST amounts per tax treatment. A standalone credit is allowed only as an explicit contact credit balance with its own reason and tax evidence. The prior phrase “allowed over-credit policy” is replaced by these exact rules.

Golden arithmetic fixtures cover inclusive and exclusive values, six-decimal quantities, fractional-cent unit prices, fixed and percentage discounts, mixed tax treatments, exact-half-cent signs, credits, and the one-cent supplier rounding line in both Go and TypeScript.

### 7.3 Quotes

Quotes are non-posting documents with draft, sent, accepted, declined, expired, cancelled, and converted lifecycle states. A quote can be converted once into an invoice draft. Later quote edits do not alter the invoice draft.

Allowed transitions are `DRAFT → SENT | CANCELLED`, `SENT → ACCEPTED | DECLINED | EXPIRED | CANCELLED`, and `ACCEPTED → CONVERTED | CANCELLED`. Conversion is terminal and retains the created invoice-draft ID.

### 7.4 Sales invoices and credit notes

An invoice draft is editable and has no ledger effect. Issuing it atomically:

1. freezes the document number, contact snapshot, lines, tax rules, terms, and totals;
2. posts debit receivables and credits revenue plus current GST payable on non-cash basis or deferred GST payable on cash basis;
3. appends the issued event; and
4. returns the invoice and journal projections.

Issued invoices are immutable. Lifecycle and settlement state are separate. Settlement state is derived as unpaid, part-paid, paid, or overpaid from credits and allocations.

Invoice lifecycle is exactly `DRAFT → ISSUED | CANCELLED` and `ISSUED → CORRECTED`. `CORRECTED` means the original is retained and has one linked reversal/replacement; it does not hide the original. Credits do not change lifecycle. Settlement state is a projection, not a mutable transition.

A commercial reduction uses a sales credit note with its own number and posting. A data-entry correction uses an explicit reverse-and-replace workflow. Both retain links to the affected invoice and cannot reduce the remaining document balance below the allowed over-credit policy. Tammy does not silently void an issued invoice.

### 7.5 Bills and supplier credits

A bill draft is editable and has no ledger effect. Approval freezes its supplier snapshot, reference, dates, lines, tax treatment, tax-invoice evidence state, attachments, and totals, then posts debits to expense or asset, a debit to the appropriate current, deferred, or evidence-pending GST receivable control under Section 8.3, and a credit to payables.

The pair `(supplier_contact_id, normalized_supplier_reference)` is unique among active approved bills unless a user records an explicit duplicate-reference override reason. Similar amount/date/reference findings are warnings, not automatic rejection.

Approved bills are immutable. Supplier credits and entered-in-error reversals are linked new documents with linked journals. Payment state is derived from credits and allocations.

Bill lifecycle is exactly `DRAFT → APPROVED | CANCELLED` and `APPROVED → CORRECTED`. Supplier credits do not change lifecycle. Payment state is a projection from immutable credit/allocation events.

## 8. Payments and allocations

### 8.1 Settlement and open-item model

A settlement has exactly one kind:

- customer receipt: debit bank/clearing, credit receivables;
- supplier payment: debit payables, credit bank/clearing;
- customer refund: debit receivables, credit bank/clearing; or
- supplier refund: debit bank/clearing, credit payables.

It records consideration date, posting date, amount, currency, financial or clearing account, contact, reference, method, source, lifecycle, and optimistic version. Lifecycle is `POSTED` or `REVERSED`; correction creates a new linked settlement.

Invoices and bills are debit-magnitude open items in their respective subledgers. Credit notes, supplier credits, unallocated customer receipts, and unallocated supplier payments are credit-magnitude open items. Customer refunds consume customer credit; supplier refunds consume supplier credit. An overpayment remains an explicit unallocated credit and continues to reconcile to the receivables or payables control account.

The open-item equation for a document is:

```text
original gross - linked credit applications - payment allocations
  + reversed credit applications + reversed payment allocations
  = open gross
```

The equivalent signed equation applies to credit open items. A source can reach zero but cannot cross zero. A linked credit application and a payment allocation are distinct event types.

### 8.2 Allocation rules

An incoming receipt may allocate across customer invoices for the same contact and currency. An outgoing payment may allocate across supplier bills. A customer or supplier credit may allocate across compatible debit items, and a refund may allocate only against compatible credit or overpayment items. Cross-contact and cross-currency allocation is prohibited.

Allocations cannot exceed either the settlement/credit remainder or target open remainder. Optimistic source/payment versions and the containing SQLite write transaction prevent concurrent over-allocation. The remaining amount is derived from immutable allocation and reversal events rather than a mutable balance column.

If a document contains mixed tax treatments, an allocation is distributed over remaining source-line gross amounts using exact rational arithmetic and the largest-remainder method. Ties use stable source-line ID order. The allocation stores its finalized gross/net/GST split. The sum of active and reversed recognition facts can never exceed each source line's posted gross, net, or GST amount.

### 8.3 Cash and non-cash GST attribution

For non-cash GST, invoice issue and bill approval create attribution facts. Payment allocation does not recognize GST again. A purchase fact also records whether the required tax invoice is held. If it is not held, the input-tax-credit fact is `DEFERRED_MISSING_TAX_INVOICE` and BAS excludes it until a later eligible period after evidence is recorded or a versioned exception rule is selected.

For cash GST, source posting initially uses deferred GST control accounts:

- invoice: credit deferred GST payable;
- bill: debit deferred GST receivable;
- customer-receipt allocation: debit deferred GST payable and credit current GST payable; and
- supplier-payment allocation with required evidence: debit current GST receivable and credit deferred GST receivable.

If supplier-payment consideration exists but required tax-invoice evidence does not, the reclassification moves the amount from deferred GST receivable to the GST-evidence-pending account. Recording eligible evidence later moves it from evidence pending to current GST receivable in the eligible tax period.

The immutable consideration date comes from the settlement, not the later bookkeeping date on which a user happens to allocate it. Cash-basis facts and their GST reclassification journal use that consideration date. A later allocation into an open period therefore backdates to the real consideration date. If that accounting or tax period is closed, the command stops with `LATE_ATTRIBUTION_REQUIRES_RESOLUTION`; it never silently attributes to today. The user must either reopen an unfinalized period or create an explicit current-period correction under a versioned rule that permits it. A finalized or dispatched historical BAS is never edited.

For each cash-basis source line, this invariant holds independently for gross, net, and GST:

```text
original source amount
  = deferred remainder
  + recognized consideration
  + pending evidence/adjustment
  + finalized adjustment amount
```

The tax subledger proves that invariant and reconciles its current/deferred/pending totals to the corresponding GST control accounts.

### 8.4 Credits, refunds, and GST adjustments

A credit first divides the affected source amount between unrecognized/deferred and previously recognized portions. The deferred portion reverses the deferred GST control immediately. The recognized portion creates a typed `GSTAdjustmentEvent` with:

- source and credit references;
- awareness date;
- gross/net/GST amount;
- increasing or decreasing direction;
- whether the organisation is liable to provide consideration;
- adjustment-note status and any versioned exception rule;
- consideration provided/received to date; and
- attribution state and period.

TaxRules applies these deterministic timing rules from the retained rule bundle:

- an ordinary adjustment is attributable when the organisation becomes aware of it;
- on cash basis, when the adjustment makes the organisation liable to provide consideration, attribution occurs only as that consideration is provided;
- a decreasing adjustment that requires an adjustment note remains pending until the note is held or a retained exception applies; and
- partial consideration attributes the adjustment proportionally using the Section 8.2 allocation algorithm.

Pending amounts post to dedicated pending-GST adjustment control accounts. A customer or supplier refund, credit application, evidence event, or explicit adjustment command reclassifies the eligible amount between pending and current GST controls. The commercial credit, refund settlement, allocation, tax adjustment, and reclassification journal remain separately inspectable and linked.

### 8.5 Reversal and dependency rules

Allocation changes use linked reversal and replacement events and exact inverse GST facts/journals; prior rows are not edited. `ReversePayment` either receives the complete set of allocations to reverse in the same command or fails with `PAYMENT_HAS_ACTIVE_ALLOCATIONS`. It also fails while any dependent bank match is in a completed reconciliation. Unmatching and undoing reconciliation are explicit prior commands.

A credit or allocation cannot be reversed after a later refund, reallocation, finalized BAS adjustment, or dependent correction without reversing those dependants in reverse order. The error returns the blocking resource references. Idempotent replay returns the original complete dependency result.

## 9. Banking and reconciliation

### 9.1 Financial accounts

A bank or credit-card account maps one-to-one to a postable ledger account and records account type, display name, institution, masked account number, opening statement date and balance, and active status. Full credentials and live-feed tokens are outside this scope.

Tammy normalizes every statement amount and balance to ledger sign: positive means a debit to the linked ledger account and negative means a credit. A deposit into an asset bank account is normally positive; a withdrawal is negative. A credit-card purchase increases the liability with a credit and is therefore negative; a credit-card payment decreases the liability with a debit and is positive. The parser retains the institution's original amount/direction text beside this normalized value so a user can diagnose mapping.

Activating a financial account requires the Section 6.1 conversion state. Its normalized opening statement balance plus explicitly outstanding statement/ledger items must equal the linked ledger account's signed opening balance.

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

Import date ranges may overlap because banks often re-export prior rows. Exact identities remain blocked and reviewable similarities remain staged. Completed reconciliation ranges, not import ranges, determine period ownership. One imported line may belong to at most one completed reconciliation.

### 9.3 Matching and transaction creation

Tammy suggests candidate matches using exact amount, compatible ledger sign, date distance, contact aliases, document references, and existing allocation state. Scores and reasons are visible. Suggestions do not commit automatically.

A user may confirm one of these actions:

- match an existing incoming or outgoing payment;
- match multiple payments to one statement line;
- match one payment to multiple statement lines;
- create and allocate a payment;
- create a direct receive-money or spend-money transaction;
- create or complete a same-currency bank transfer;
- record bank fees or interest; or
- leave the line unmatched with a note.

Confirmed matching is an immutable `MatchGroup` with one or more `MatchComponent` rows. Each component links one statement line to one payment, transfer side, or direct cash source and carries a signed ledger-normal amount. A statement line's confirmed component sum may not exceed its amount or cross zero. Its match state is `UNMATCHED`, `PART_MATCHED`, or `FULLY_MATCHED` from the remaining signed amount. A payment or transfer side has the equivalent remaining amount, so many statement lines can match one payment and one statement line can match many payments without ambiguity.

Completion of a group requires the selected components to balance exactly on both the statement and accounting sides. Split creation is explicit. A transfer creates one linked accounting event between two financial accounts; each imported side can link to the same transfer. Different statement dates do not create a second journal.

Match confirmation uses expected versions for every participating statement line and target. Unmatching appends inverse components and is prohibited after a containing reconciliation is completed, after the target is reversed, or while another draft reconciliation owns the line. These dependencies are checked in the same transaction.

### 9.4 Reconciliation

A reconciliation belongs to one financial account and statement period. It records ledger-normal opening and closing statement balances, statement start/end dates, included statement lines, included confirmed match components, and a calculated difference.

The first reconciliation starts from the verified opening conversion. Every later completed range begins after the prior completed end date and uses the prior closing balance. Date overlap is prohibited. A date gap requires an explicit gap reason and proof that no unassigned imported statement line exists in the gap. A later draft may exist concurrently, but it cannot complete before every earlier draft on that account is completed or cancelled.

Completion requires:

```text
prior normalized closing balance + included normalized statement movement
  = entered normalized closing balance

prior normalized closing balance + newly cleared ledger movement
  = entered normalized closing balance

sum(statement-side match components) - sum(ledger-side match components) = 0
```

Every included statement line must be fully matched or explicitly excluded from the statement before completion; an explanatory unmatched note is not sufficient. Outstanding ledger movements remain uncleared for a later statement and do not enter newly cleared movement.

These equations work unchanged for negative credit-card liability balances because all terms use ledger sign. Completed reconciliations are immutable. Undo requires a preparer or administrator, a reason, and no later completed reconciliation that depends on it. Undo creates a linked audit event and releases its match ownership without deleting evidence or match components. Concurrent complete, undo, match, and unmatch commands use account and reconciliation versions under one write transaction.

## 10. Document extraction

### 10.1 Supported documents and evidence

Document intake accepts PDF, PNG, and JPEG files for supplier bills, receipts, bank statements, and credit-card statements. The original bytes are encrypted and retained before parsing. All derivatives refer to the original SHA-256 hash.

Encrypted evidence includes MIME detection, byte length, page count, ingestion actor/time, source path display name, extraction engine versions, page images where necessary, candidate values, coordinates, confidence, review decisions, and final target document IDs.

### 10.2 Local extraction pipeline

An isolated native document helper uses a length-delimited Protobuf stdin/stdout protocol. It receives document bytes, an ephemeral job directory, locale, limits, and an optional in-memory PDF password; it never receives a workspace path, database handle, local-core capability, user session, or unrestricted destination path.

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

`firecrawl/pdf-inspector` is the first-stage Rust library for PDF classification, positioned text, reading order, tables, and per-page OCR routing. It performs no OCR itself. Scanned pages use a pinned, bundled, statically linked Tesseract build and English orientation/language data initially. Tesseract TSV or hOCR output preserves word coordinates and confidence. Both dependencies are pinned by commit/release and checksum, built reproducibly, included in the SBOM and licence notices, scanned for known vulnerabilities, and exercised by the packaged hostile-document corpus before release.

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

### 10.4 Reviewed-candidate handoff

Saving a review seals an immutable `DocumentReview` with one explicit target kind:

| Reviewed kind | Target command | Result before posting |
|---|---|---|
| Supplier invoice or receipt requiring payables | `CreateBillDraftFromReview` | purchase bill draft with evidence attachment |
| Paid receipt or cash purchase | `CreateSpendMoneyDraftFromReview` | banking spend-money draft with financial account, contact, lines, and evidence |
| Bank statement | `CreateStatementImportDraftFromReview` | ordinary bank staging result for selected bank account |
| Credit-card statement | `CreateStatementImportDraftFromReview` | ordinary bank staging result for selected credit-card account |
| Unsupported or historical evidence | `AttachReviewOnly` | evidence link with no financial draft |

The user chooses the target when classification is ambiguous. Statement rows use the exact same `StagedStatementLine` Protobuf model, sign-mapping confirmation, duplicate gate, and `CommitStatementImport` command as CSV/OFX/QFX; extraction cannot bypass banking validation. A paid receipt does not create a posted cash transaction until the user reviews the spend-money draft and invokes its separate confirmation command. A bill draft does not create a payment merely because the image says “paid”.

`CreateTargetDraft` is idempotent by `(review_id, target_kind, operation_key)`. An identical replay returns the same draft. Changing target kind requires an explicit `SupersedeDocumentReview` event and is prohibited after the prior target is posted. Candidate fields, user edits, target fields, and final document references remain linked in evidence.

### 10.5 Human review and safety

Extraction may create or update only a draft. Review shows the original page beside candidate fields, highlights source coordinates, surfaces confidence, and requires the user to resolve arithmetic or GST mismatches. Accepting extraction records every accepted, changed, and rejected candidate. Approval and posting remain separate authorised commands.

Initial hard limits are 50 MiB input, 200 PDF pages, 500 megapixels of rendered page data across the job, 16 MiB per Protobuf frame, 128 MiB total protocol output, 1 GiB resident memory, and ten minutes wall time. Limits are lowerable by policy but require a new reviewed version to increase. The helper validates magic bytes rather than extensions, rejects recursive/embedded file extraction, uses a core-created `0700` job directory with random names, receives document bytes rather than arbitrary source paths, and returns no caller-selected path.

The packaged macOS helper is a separately signed App-Sandbox executable with no network client/server entitlement and file access restricted to its job container. The packaged Windows helper runs in an AppContainer with no network capabilities, a restricted token, and a Job Object limiting one process, memory, CPU, child-process creation, and lifetime. Tesseract is linked into the helper so OCR does not escape through a child process. Closed inherited handles, kill-on-parent-exit, and protocol byte limits apply on both platforms.

Before Slice 3 can ship, a packaged security feasibility gate must prove on both supported platforms that the helper cannot open an outbound or loopback socket, cannot read a sentinel outside its job directory, cannot spawn a process, is killed at memory/CPU limits, and leaves no plaintext derivative after exit. Failure blocks document intake release; it cannot silently fall back to an unsandboxed helper.

An encrypted PDF is retained and the job enters `WAITING_FOR_PASSWORD`. `ProvidePdfPassword` is a security-challenge RPC: every call is a new attempt, it is never auto-retried or written to idempotency/audit payloads, and the password exists only in renderer/core/helper memory for that attempt. Three failures terminate the job; retry requires a new explicit extraction job. Buffers are zeroed after use where the language/runtime permits.

Logs contain only job IDs, hashes, engine versions, counts, durations, and typed failures. Crash, timeout, cancellation, corrupt input, or sandbox denial leaves the encrypted original evidence and a retryable or terminal extraction result but no partial business document, import batch, or journal.

## 11. GST, BAS, and reporting

### 11.1 GST facts

Every GST-bearing source line retains gross, net, GST, tax-rule ID, rule-bundle ID, control-account line IDs, source revision, and recognition basis. Exact integer/rational arithmetic and the foundation rounding rules apply.

The initial tax codes remain those in the foundation design and are extended only through immutable rule-bundle versions. A reporting period cannot contain mixed active bundles. A configuration change is accepted only on the first day of a later empty reporting period; workpaper creation returns `MIXED_TAX_RULE_BUNDLES` if imported or corrupt data violates the invariant.

### 11.2 Financial revision and snapshot contract

The workspace maintains one monotonic `financial_revision`. A committed unit of work increments it exactly once when it changes any journal, source document, contact snapshot used by an open document, allocation, GST evidence/adjustment, statement match, completed reconciliation, reporting classification, organisation reporting setting, or tax/rule mapping. Modules also retain local ledger, settlement, banking, and tax-source revisions for diagnostics.

A `RevisionVector` contains financial, ledger, settlement, banking, tax-source, organisation-profile, and rule-bundle revisions. Report and BAS generation run in one SQLite read transaction, read the vector before and after projection, and fail if it changes. The snapshot retains the vector, canonical query, complete source content hash, and generated content hash. A caller can pass an expected financial revision and receives `FINANCIAL_REVISION_CHANGED` rather than a mixed snapshot.

Journal-only reports can prove their totals against the ledger revision, but aged, banking, GST, BAS, and combined reports pin the complete vector. This prevents allocation, evidence, or reconciliation changes that do not otherwise need a journal from escaping snapshot invalidation.

### 11.3 BAS workpaper and authoritative state transitions

Creating or regenerating a BAS workpaper reads one immutable revision vector and reporting profile. It stores:

- organisation and period;
- cash or non-cash basis;
- complete revision vector and source content hash;
- rule and mapping bundle versions;
- every BAS field value;
- field-to-GST-fact provenance;
- warnings and blocking validations;
- content hash; and
- creator and creation time.

The predecessor report/transmission state machine remains authoritative. `TaxReportImpactPort` applies these exact source-change rules in the same transaction as the cause:

- if a source changes while the report is `DRAFT`, the current calculation version becomes outdated and a new retained calculation version is required before validation;
- an in-period journal, allocation, evidence-eligibility change, or tax adjustment while the report is `LOCALLY_VALIDATED`, `DECLARED`, or `SUBMISSION_PREPARED` moves the report to `DRAFT`, cancels a `PREPARED` transmission, and retains the prior calculation, validation, declaration, and payload as superseded evidence;
- organisation identity, GST configuration, ownership, or rule-bundle impact moves every affected pre-dispatch report to terminal `SUPERSEDED` and cancels a `PREPARED` transmission;
- any affected `DISPATCHING` or `UNKNOWN` transmission blocks the source command with `TRANSMISSION_OUTCOME_UNRESOLVED`; and
- `ACCEPTED` and `REJECTED` reports remain immutable. Later changes create a linked correction or revision report and never alter the dispatched snapshot.

“Outdated” is a read projection reason, not an additional persisted report state. Regeneration creates a new calculation version under the same `DRAFT` report or a linked correction/revision report as required above. Validation never edits calculated values and applies only to the current content hash.

The local simulator exercises prepare, validate, declaration preview, submit, technical failure, rejection, acceptance, and ambiguous-response reconciliation states without claiming ATO connectivity. User exports are:

- human-readable BAS workpaper PDF;
- field and source-detail CSV;
- canonical Protobuf JSON evidence bundle; and
- signed audit/evidence package.

Production SBR controls remain disabled until the external gates in the foundation design are satisfied.

### 11.4 Financial reports

Reports use accounting and subledger read projections at a pinned revision vector. Initial reports are:

- profit and loss for a date range, with optional comparison;
- balance sheet as of a date;
- indirect cash-flow statement for a date range;
- trial balance as of a date;
- general ledger and journal detail;
- GST detail by code and source;
- aged receivables and aged payables using configurable ageing date and buckets;
- customer and supplier activity statements; and
- bank reconciliation summary and detail.

All journal-query arithmetic uses signed debit balance: debits are positive and credits negative. Each chart account has a report section, display-order key, normal balance, contra-parent where applicable, and cash-flow classification. A report displays a normal-balance amount as positive; contra accounts appear in their parent's section with the opposite presentation sign. The underlying signed value is retained in Protobuf exports.

The executable formulas are:

```text
trial balance signed account balance = opening signed balance + debits - credits

revenue displayed = -(revenue and other-revenue signed balances after contra sign)
expense displayed = expense and other-expense signed balances after contra sign
net profit = revenue displayed - expense displayed

assets displayed = asset signed balances after contra sign
liabilities displayed = -liability signed balances after contra sign
contributed/retained equity displayed = -equity signed balances after contra sign
current-year earnings = net profit from financial-year start through report date
assets = liabilities + contributed/retained equity + current-year earnings
```

Retained earnings is the posted retained-earnings account. Current-year earnings is derived and is not double-counted when an explicit year-end journal closes prior-year income/expense accounts. A year-end close wizard previews and posts an ordinary auditable journal; it never changes report formula or history.

The indirect cash-flow statement starts with net profit, adds configured non-cash profit-and-loss adjustments, and applies signed changes in non-cash balance-sheet accounts according to each account's operating, investing, financing, or excluded classification. Transfers between cash accounts are excluded. Its final check is:

```text
opening cash + operating cash + investing cash + financing cash = closing cash
```

Missing or conflicting account classifications are blocking report validations, not an “unclassified” plug. Disposal gains/losses and other non-cash items require explicit account mappings and golden fixtures.

Aged reports calculate each source's outstanding amount from active credits and allocations as of the ageing date. Age is based on due date by default or issue date when explicitly selected. Boundary days belong to the older bucket only after they exceed the configured upper bound. Credits and overpayments appear separately and reconcile with control accounts.

Comparative columns run the same formulas with their own date range and retained revision vector. They do not reuse current classifications if a prior report snapshot retained a different version. Report headings retain organisation identity, basis, filters, generation time, vector, and totals. PDF, CSV, and canonical evidence-bundle exports all retain the report ID and content hash.

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

Document extraction, import staging, report/evidence rendering, and backup use persisted job projections with queued, running, waiting-for-input, completed, failed-retryable, failed-terminal, and cancelled states. Jobs report bounded progress through typed Protobuf messages. Each job has an operation key, input semantic hash, attempt, stage, stage checkpoint hash, cancellation flag, and optional committed result reference.

| Job | Restart-safe stages | Commit point | Cancellation and retry rule |
|---|---|---|---|
| Document extraction | classification, page rendering, native extraction, OCR, field inference can restart from retained encrypted original | atomic candidate-set insert after complete bounded output validates | cancel kills helper before candidate insert; after insert it is completed. Retry uses same job/input hash and replaces no prior candidate |
| Statement import staging | parse, mapping preview, sign diagnostics, duplicate analysis | atomic staging-result insert; no statement line or journal exists yet | cancel before insert; retry recomputes staging. `CommitStatementImport` is a separate user command and is never a background retry |
| Report/BAS rendering | read retained report snapshot, render temporary PDF/CSV/evidence files | artefact bytes and manifest are encrypted/hashed, then artefact row commits | cancel before artefact commit; retry reuses report content hash and may return an identical completed artefact |
| Audit/evidence export | collect retained objects, build temporary archive, verify hashes/signature | atomic rename of verified archive to approved destination plus committed export result | cancel before rename; startup checks destination hash before deciding complete versus retryable |
| Backup | SQLite snapshot, collect blobs/artefacts, encrypt temporary archive, verify manifest | atomic rename to approved destination plus committed backup manifest | cancel before rename; incomplete temp file is deleted. Same operation key returns verified completed destination or restarts from a new temp file |

Restore and migration do not use the generic resume rule. They use the predecessor's fsync'd external restore operation journal and the Section 13 migration staging/swap states because their commit point replaces the active database.

On startup, a queued job remains queued; a running restart-safe stage returns to queued with an incremented attempt; a job with a durable committed-result reference is reconstructed as completed; and an unverified temporary output is deleted. Extraction and staging stop after three automatic process-failure attempts and require explicit user retry. User-input failures, password challenges, validation failures, and financial commands are never auto-retried.

Cancellation is cooperative first and forceful at the documented helper timeout. It cannot cross a commit point. A cancellation and commit race is elected by one storage transaction: either the result commits and cancellation reports `COMMIT_ALREADY_COMPLETED`, or cancellation commits and no result can appear.

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

Tests create state through public Protobuf commands, not direct production-database inserts, except repository/migration tests whose purpose is the storage boundary. Internal domain behaviour is not mocked in integration or E2E tests. Only explicitly external ATO and future bank-feed adapters use deterministic simulators.

Clocks, IDs, job scheduling, and external outcomes are injectable. Tests do not depend on sleeps. Fixture documents are synthetic or redistributable and contain no real customer data.

### 15.2 Exhaustive packaged-E2E traceability contract

`test/e2e/coverage.yaml` is a machine-checked traceability manifest keyed by fully-qualified RPC and lifecycle transition. CI reads the generated `FileDescriptorSet` and fails when a public Tammy RPC is absent, a referenced RPC no longer exists, or an allowed transition lacks a scenario. The manifest links each entry to:

- at least one packaged happy path through the named preload method and real Go core;
- every allowed lifecycle transition and one invalid-transition attempt;
- permission outcomes for admin, preparer, lodger, and auditor;
- idempotent replay and changed-request conflict for persistent commands;
- stale-version and principal domain failure coverage;
- empty, populated, filter, and pagination coverage for list queries; and
- the public projections that prove postconditions.

Playwright uses visible UI for normal workflows. For an operation intentionally hidden from a role, it may invoke that operation's existing named preload method from the packaged renderer to prove the core denies it; no generic or test-only RPC tunnel is added. The complete role/RPC matrix is data-driven from the manifest.

The initial packaged scenario catalogue is:

| Scenario | Required coverage |
|---|---|
| `E2E-00` | workspace create/unlock/recovery, organisation, all roles, restart, offline startup |
| `E2E-01` | chart template, opening ordinary/AR/AP/bank balances, cash-GST remainders, mismatch rejection, full replacement |
| `E2E-02` | customer/supplier/both contacts, update/archive, duplicate warning, merge and immutable snapshots |
| `E2E-03` | quote draft/update/sent/accepted/declined/expired/cancelled/conversion and invalid transitions |
| `E2E-04` | inclusive/exclusive/mixed/discount sales arithmetic, issue, partial credit, standalone credit, correct/reverse, numbering gaps |
| `E2E-05` | bill reference duplicate handling, tax-invoice evidence, approve, supplier credit, correct/reverse, native-PDF bill handoff |
| `E2E-06` | scanned/mixed/encrypted/corrupt/oversize documents, OCR fallback, password attempts, review edits, receipt/statement/attach-only handoffs, cancellation and restart |
| `E2E-07` | non-cash receipts/payments, split allocations, credits, unallocated amounts, overpayments, customer/supplier refunds, reversal dependency order |
| `E2E-08` | cash-GST part-payments, mixed lines, deferred/current/pending controls, late allocation resolution, missing tax invoice, adjustment note, partial refund and BAS attribution |
| `E2E-09` | CSV profile, OFX 1.x/2.x and QFX import, asset/credit-card signs, exact duplicates, fuzzy review, overlapping import ranges |
| `E2E-10` | partial many-to-many matching, create receipt/payment, spend/receive money, fee, interest, two-sided transfer, unmatch conflicts |
| `E2E-11` | first/subsequent bank and credit-card reconciliations, outstanding movements, gap acknowledgement, non-zero rejection, lock, undo/recomplete |
| `E2E-12` | all financial reports, contra/current-year/retained-earnings formulas, comparisons, ageing boundaries, cash-flow reconciliation, PDF/CSV/evidence export |
| `E2E-13` | BAS cash/non-cash creation, missing-evidence validation, declaration, prepared cancellation, simulator outcomes, source invalidation, supersession, correction/revision, exports |
| `E2E-14` | manual journals, close/reopen, closed-period source/payment/allocation failures, linked corrections and tax impact |
| `E2E-15` | complete role/RPC matrix, stale versions, rapid double submit, idempotency conflict, concurrent allocation/match/reconciliation election |
| `E2E-16` | audit chain/export/tamper, backup/restart/restore, wrong passphrase, migration from every release, staged failure rollback, invariant verification |
| `E2E-17` | signed packaged helpers, no network/filesystem/process escape, resource kill, cleanup, macOS and Windows artefact manifests |

No slice is complete while one of its new descriptor-discovered RPCs, transitions, roles, or principal failures is absent from this manifest. Fast domain/contract tests may cover more combinatorial values, but they do not replace the required packaged path.

### 15.3 Canonical packaged E2E oracle

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

### 15.4 Cross-projection oracles

Acceptance tests compare the same truth through independent public projections:

- journal totals versus trial balance;
- trial balance versus profit and loss plus balance sheet;
- receivables/payables control accounts versus aged open items;
- bank ledger balance versus reconciliation;
- current/deferred/evidence-pending/adjustment-pending GST controls versus the tax subledger, eligible GST detail, and BAS provenance; and
- restored workspace projections versus the pre-backup manifest.

### 15.5 Performance fixtures

Benchmarks cover at least 250,000 journal lines, 100,000 imported statement lines, 20,000 contacts, and 10,000 evidence documents. Provisional goals are 300 ms for ordinary paginated lists, two seconds for standard financial reports, and five seconds for a 10,000-line statement staging import, excluding user review and OCR. They become release-blocking only after the implementation plan records exact reference hardware, operating-system power mode, cold/warm cache method, fixture generator/version, run count, percentile calculation, and CI variance budget.

## 16. Delivery slices

Implementation planning produces one ordered programme index and one focused implementation-plan document per slice. A slice plan must enumerate exact `.proto`, generated, Go, SQL migration, TypeScript, UI, fixture, and test files; map every new RPC through the Section 5 ports and unit of work; and add its Section 15 traceability rows before implementation begins. Slice plans may refine task order and file placement but may not change the financial, state, security, or evidence rules in this specification without a reviewed amendment.

Only one slice is in implementation at a time. Later slice plans may be drafted early, but a later slice cannot bypass the packaged acceptance and invariant gates of its predecessors.

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

## Appendix A. Optional Unlimited-OCR experiment

The core release does not depend on `baidu/Unlimited-OCR`. Its official inference path currently requires a large Python and NVIDIA CUDA model stack and loads model-supplied remote code, which is incompatible with Tammy's default cross-platform, offline, sandboxed helper boundary.

After the Tesseract-backed Slice 3 passes its security and E2E gates, a separate experiment may evaluate a user-installed Unlimited-OCR service on loopback. That work requires its own threat model, model/code/version pinning, consent and health UI, data-retention proof, no-cloud enforcement, benchmark corpus, failure isolation, and design approval. The adapter may improve candidates but can never approve or post. It is not part of any completion criterion in this specification.
