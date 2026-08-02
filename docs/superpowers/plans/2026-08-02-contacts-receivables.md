# Contacts and Receivables Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Slice 2: contact management, quotes, sales invoices/credits, customer receipts/refunds/allocations, statements, aged receivables, and sales-to-ledger packaged E2E.

**Architecture:** Contacts, Sales, and Settlements own separate normalized tables and generated services. Sales snapshots contacts and submits typed posting intents through `AccountingPostingPort`; Settlements reads immutable receivable projections through `ReceivableSourcePort`, records immutable allocation events, and posts settlement/tax facts in the same unit of work. Every public projection is Protobuf-defined.

**Tech Stack:** Buf/Protobuf, Connect-Go/Connect-ES, Go/SQLCipher, Electron/React/TypeScript, Vitest, Playwright, property tests.

---

**Normative designs:** `docs/superpowers/specs/2026-08-02-core-business-accounting-suite-design.md` §§4–8, 11–16 and the unchanged security/storage rules in `docs/superpowers/specs/2026-07-19-tammy-local-first-accounting-sbr-design.md`.

**Prerequisite:** The Slice 1 exit gate in `2026-08-02-ledger-gst-kernel.md` is green and retained.

**Required skills while executing:** `@superpowers:test-driven-development`, `@security-best-practices`, `@frontend-design`, `@playwright`, and `@superpowers:verification-before-completion`.

**Micro-TDD rule:** For each named test case below, add one case, run its narrow command to observe the named failure, implement the smallest behavior, rerun to `PASS`, and only then take the next case. No task may commit a deliberately red fixture.

## Slice 2 RPC and UoW map

| Service/RPCs declared in this slice | Owner, required ports, and transaction rule | Named preload/route | Scenario |
|---|---|---|---|
| Contact `CreateContact`, `UpdateContact`, `SetContactStatus`, `MergeContacts`, `GetContact`, `ListContacts` | Contacts; ordinary commands use authorizer/idempotency/UoW, `OpenSalesReferencePort` for archive/merge/snapshot impact, and AuditAppender; queries use `UoW.Read` | matching lower-camel methods; `/contacts` | E2E-02 |
| Sales `CreateQuote`, `UpdateQuote`, `MarkQuoteSent`, `AcceptQuote`, `DeclineQuote`, `ExpireQuote`, `CancelQuote`, `ConvertQuoteToInvoiceDraft`, `GetQuote`, `ListQuotes` | Sales; ContactSnapshotPort, TaxReportImpactPort for source-affecting conversion, AuditAppender | matching named methods; `/sales/quotes` | E2E-03 |
| Sales `CreateInvoiceDraft`, `UpdateInvoiceDraft`, `CancelInvoiceDraft`, `IssueInvoice`, `CreateCreditNote`, `CorrectInvoice`, `GetInvoice`, `ListInvoices`, `GetSalesCredit`, `ListSalesCredits`, `GetReceivableOpenItem`, `ListReceivableOpenItems` | Sales; ContactSnapshotPort, AccountingPostingPort, AllocationReadPort, SettlementAdjustmentWritePort, TaxReportImpactPort, AuditAppender in one UoW | matching named methods; `/sales/invoices` | E2E-04/E2E-08 |
| Settlement `RecordCustomerReceipt`, `RecordCustomerRefund`, `AllocatePayment`, `ReverseAllocations`, `ReversePayment`, `GetPayment`, `ListPayments`, `GetAllocation`, `ListAllocations`, `GetUnallocatedCredit`, `ListUnallocatedCredits` | Settlements; ContactSnapshotPort, ReceivableSourcePort, AccountingPostingPort, TaxReportImpactPort, BankingDependencyPort, AuditAppender in one UoW | matching named methods; `/sales/money-in` | E2E-07/E2E-08 |
| Reporting `GetCustomerStatement`, `GetAgedReceivables` | Reporting; FinancialReportReadPort composition over ReceivableSourcePort/AllocationReadPort/Accounting read projection in one pinned `UoW.Read` | `getCustomerStatement`, `getAgedReceivables`; `/reports/receivables` | E2E-04/E2E-07 |

Supplier payment/refund RPCs are not declared until Slice 3. Every persistent row above uses the shared authorization/idempotency/audit/result coordinator, expected aggregate versions where applicable, and one caller-owned UoW. `coverage.yaml` is populated from this exact list before generation passes.

**Generated output map:** the changed proto set generates exactly `services/core/internal/gen/tammy/v1/{contact,sales,settlements,reporting,accounting,events,fixtures}.pb.go` and `packages/connect-client/src/gen/tammy/v1/{contact,sales,settlements,reporting,accounting,events,fixtures}_pb.ts`. The four service-bearing files also generate `services/core/internal/gen/tammy/v1/tammyv1connect/{contact,sales,settlements,reporting}.connect.go`. Generated paths, `packages/connect-client/package.json`, `services/core/go.sum`, and `pnpm-lock.yaml` are included whenever changed.

**Per-task red/green index:** Execute one named subtest at a time under the global Micro-TDD rule. Task 1 starts with `rtk go test ./services/core/internal/contracts -run '^TestReceivablesDescriptor/CreateContact$'`; Task 2 runs package-specific seeds `rtk go test -tags tammy_sqlcipher ./services/core/internal/contacts -run '^TestContactRepository/optimistic_version$'`, `rtk go test -tags tammy_sqlcipher ./services/core/internal/sales -run '^TestSalesRepository/frozen_issue$'`, and `rtk go test -tags tammy_sqlcipher ./services/core/internal/settlements -run '^TestSettlementRepository/immutable_allocation$'`; Task 3 runs `rtk go test -tags tammy_sqlcipher ./services/core/internal/contacts -run '^TestContactService/customer_role$'` and `rtk go test -tags tammy_sqlcipher ./services/core/internal/sales -run '^TestContactReferenceAdapter/rewrite_mutable_draft$'`; Task 4 starts with `rtk go test ./services/core/internal/platform/documentmath -run '^TestCalculate/half_away_boundary$'`; Task 5 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/sales -run '^TestQuoteTransitions/draft_to_sent$'`; Task 6 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/sales -run '^TestInvoiceService/issue_non_cash$'`; Task 7 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/settlements -run '^TestSettlementService/receipt_and_allocate_cash_gst$'`; Task 8 runs `rtk go test -tags tammy_sqlcipher ./services/core/internal/reporting -run '^TestAgedReceivables/thirty_day_boundary$'` and `rtk go test -tags tammy_sqlcipher ./services/core/internal/accounting -run '^TestReceivablesControlReadPort/as_of_balance$'`; Task 9 starts with `rtk pnpm --filter @tammy/desktop test -- -t 'createContact invokes real generated service'` and must first fail because the Task 1 temporary typed-unavailable binding remains; Task 10 starts with `rtk pnpm exec node --test --test-name-pattern 'slice 2 cash GST cross-projection oracle' scripts/check-core-accounting-evidence.test.mjs` and must first return `SLICE_EVIDENCE_ORACLE_MISSING` until the packaged runtime assertion/result tuple is retained. Tasks 1–8 first report the named missing constructor/method or typed assertion. Within each listed package, add one snake-case subtest beneath that package's named top-level test, substitute its exact final path in the same `-run '^TopLevel/exact_case$'` command, observe the typed outcome mismatch, implement only that case, and rerun to `PASS` before the next case.

## Chunk 1: Contracts, contacts, and deterministic sales documents

### Task 1: Add contact, sales, and settlement contracts

**Files:**
- Create: `proto/tammy/v1/contact.proto`
- Create: `proto/tammy/v1/sales.proto`
- Create: `proto/tammy/v1/settlements.proto`
- Modify: `proto/tammy/v1/accounting.proto`
- Modify: `proto/tammy/v1/events.proto`
- Modify: `proto/tammy/v1/fixtures.proto`
- Create: `test/fixtures/sales/arithmetic.pb.json`
- Create: `test/fixtures/sales/transitions.pb.json`
- Create: `services/core/internal/contracts/receivables_proto_test.go`
- Create: `packages/connect-client/src/receivables-fixtures.test.ts`
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/main/rpc-router.ts`
- Modify: `apps/desktop/src/main/rpc-router.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Modify: `test/e2e/coverage.yaml`

- [ ] Write a failing descriptor test requiring exactly the Contact, Sales, and Settlement RPCs in the Slice 2 map, owned lifecycles, posting intents, contact snapshots, receivable/allocation/tax projections, and typed failures. Reporting RPCs land in Task 8; supplier Settlement RPCs must be absent until Slice 3.
- [ ] Add contracts without handwritten duplicate request/response types. Mark persistent commands with idempotency/expected-version semantics in comments and enumerate exact allowed transitions.
- [ ] Encode the Contact/Sales/Settlement lifecycles in `test/fixtures/sales/transitions.pb.json`, run `rtk pnpm transitions:generate`, and prove the generated transition index adds every allowed and invalid Slice 2 edge without drift.
- [ ] Add golden arithmetic fixtures for inclusive/exclusive values, six-decimal quantities, fractional-minor-unit prices, fixed/percentage discounts, mixed tax, half-away-from-zero boundaries, and credits.
- [ ] Extend `coverage.yaml` atomically for every Task 1 Contact/Sales/Settlement RPC, preload method, role, transition, invalid transition, replay/conflict, stale version, principal failure, and query list state under `E2E-02`, `E2E-03`, `E2E-04`, `E2E-07`, or `E2E-08`. Add each generated codec to the imported production preload registry/router now; handlers may return the typed not-yet-available failure until their owning implementation task, but no unregistered placeholder or generic tunnel is allowed.
- [ ] Run `rtk pnpm proto:generate`, `rtk pnpm --filter @tammy/connect-client test -- receivables-fixtures`, and `rtk pnpm --filter @tammy/desktop test`; limit Task 1 tests to descriptor shape, production codec registration, and round-trip fixture decoding. Calculation assertions land green with Task 4 and are never committed red.
- [ ] Run `rtk pnpm contracts`; expect exit 0 and no generated drift.
- [ ] Commit: `rtk git add proto services/core/internal/contracts services/core/internal/gen packages/connect-client apps/desktop test && rtk git commit -m "feat: define receivables protobuf contracts"`.

### Task 2: Add the receivables schema and repositories

**Files:**
- Create: `services/core/internal/storage/migrations/0010_contacts_sales_settlements.sql`
- Create: `services/core/internal/storage/migrations/0010_contacts_sales_settlements_test.go`
- Create: `services/core/internal/contacts/repository.go`
- Create: `services/core/internal/contacts/repository_test.go`
- Create: `services/core/internal/sales/repository.go`
- Create: `services/core/internal/sales/repository_test.go`
- Create: `services/core/internal/settlements/repository.go`
- Create: `services/core/internal/settlements/repository_test.go`

- [ ] Write failing repository tests for optimistic versions, stable pagination, active/merged aliases, immutable snapshots, sequence uniqueness/gaps, frozen issued rows, unique source revisions, immutable allocations/reversals, and source/payment remainder election under concurrency.
- [ ] Define normalized contact/address/role/alias/suggestion/snapshot tables; quote/invoice/credit/line/sequence tables; payment/allocation/reversal/tax-fact tables; and module-owned indexes/constraints.
- [ ] Ensure issued documents and immutable events cannot be updated/deleted, mutable references follow a confirmed alias, and historical snapshots never do.
- [ ] Add migration-prefix and rollback tests using an encrypted workspace.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/storage/migrations/... ./services/core/internal/contacts/... ./services/core/internal/sales/... ./services/core/internal/settlements/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/storage services/core/internal/contacts services/core/internal/sales services/core/internal/settlements && rtk git commit -m "feat: persist contacts and receivables"`.

### Task 3: Implement contacts and immutable snapshots

**Files:**
- Create: `services/core/internal/contacts/contact.go`
- Create: `services/core/internal/contacts/contact_test.go`
- Create: `services/core/internal/contacts/duplicates.go`
- Create: `services/core/internal/contacts/duplicates_test.go`
- Create: `services/core/internal/contacts/service.go`
- Create: `services/core/internal/contacts/service_test.go`
- Create: `services/core/internal/contacts/snapshot_port.go`
- Create: `services/core/internal/contacts/snapshot_port_test.go`
- Create: `services/core/internal/contacts/open_sales_reference_port.go`
- Create: `services/core/internal/contacts/open_sales_reference_port_test.go`
- Create: `services/core/internal/sales/contact_reference_adapter.go`
- Create: `services/core/internal/sales/contact_reference_adapter_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Write tests for customer/supplier/both roles, ABN checksum/normalization, defaults, field masks, archive/reactivate, referenced-draft blocking, duplicate warnings, explicit merge choices, aliases, and immutable issued snapshots.
- [ ] Implement normalized duplicate suggestions by exact ABN and ranked email/name; never auto-merge and require an admin-confirmed merge.
- [ ] Define Contacts-owned `OpenSalesReferencePort.CheckAndRewrite(tx, source_contact_id, target_contact_id, expected_versions)`. Its Sales adapter returns all referencing quote/invoice drafts, blocks archive when policy requires, rewrites only mutable draft references during a confirmed merge, reports whether an open source snapshot changed, and never changes issued snapshots or exposes Sales tables.
- [ ] Implement `ContactSnapshotPort.Snapshot(tx, contact_id, expected_version)` with inactive, wrong-role, merged-alias, and stale-contact outcomes.
- [ ] In this Contacts-owned task, mark contact changes that alter a snapshot used by an existing quote or invoice draft on the Slice 1 financial change-set; prove exactly one financial revision increment for the owning UoW and none for an unused contact edit, rollback, or replay.
- [ ] Route all commands through the common UoW/audit/idempotency pipeline and all lists through stable signed cursors.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/contacts/... ./services/core/internal/sales/... ./services/core/internal/app/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/contacts services/core/internal/sales services/core/internal/app && rtk git commit -m "feat: add contacts and immutable snapshots"`.

### Task 4: Implement one canonical sales arithmetic engine

**Files:**
- Create: `services/core/internal/platform/documentmath/calculator.go`
- Create: `services/core/internal/platform/documentmath/calculator_test.go`
- Create: `services/core/internal/platform/documentmath/calculator_property_test.go`
- Create: `services/core/internal/sales/calculator_adapter.go`
- Create: `services/core/internal/sales/calculator_adapter_test.go`
- Create: `packages/connect-client/src/sales-calculator.ts`
- Create: `packages/connect-client/src/sales-calculator.test.ts`

- [ ] Write Go table/property tests directly from Design §7.2 and the golden fixtures. Prove positive quantity, non-negative unit price, scale 0–6, one line rounding, one discount kind, fixed-discount ceiling, tax extraction/addition, exact document sums, and no hidden document rounding.
- [ ] Implement the authoritative Go calculator in the cross-domain `platform/documentmath` package using exact rationals and checked integer conversion with half away from zero; Sales owns only an adapter from its generated commands.
- [ ] Implement the TypeScript preview calculator using `bigint` only; compare its generated Protobuf results byte-for-byte with every golden fixture and randomized exported seed corpus.
- [ ] Reject negative ordinary lines; represent reversal sign in document kind. Require explicit freight/surcharge/rounding lines with accounts/tax rules.
- [ ] Run `rtk go test ./services/core/internal/platform/documentmath/... ./services/core/internal/sales/... -count=1` and `rtk pnpm --filter @tammy/connect-client test`.
- [ ] Commit: `rtk git add services/core/internal/platform/documentmath services/core/internal/sales packages/connect-client test/fixtures && rtk git commit -m "feat: add deterministic source document arithmetic"`.

### Task 5: Implement quote workflows and document numbering

**Files:**
- Create: `services/core/internal/sales/quote.go`
- Create: `services/core/internal/sales/quote_test.go`
- Create: `services/core/internal/sales/numbering.go`
- Create: `services/core/internal/sales/numbering_test.go`
- Create: `services/core/internal/sales/quote_service.go`
- Create: `services/core/internal/sales/quote_service_test.go`

- [ ] Write transition tests for exactly `DRAFT → SENT|CANCELLED`, `SENT → ACCEPTED|DECLINED|EXPIRED|CANCELLED`, and `ACCEPTED → CONVERTED|CANCELLED`; test every terminal-to-nonterminal rejection.
- [ ] Test final number allocation only on first send, concurrent allocation uniqueness, visible/non-reused gaps, cancelled command rollback, and separate quote/invoice/credit sequences.
- [ ] Implement draft/update/transition/conversion services; conversion snapshots the accepted quote into one invoice draft and records its ID exactly once.
- [ ] In this Sales-owned task, mark quote create/update/cancel/transitions and the conversion-created invoice draft on the financial change-set; prove one financial revision increment per successful UoW and none on read, rollback, or replay. Ordinary invoice-draft commands are owned and tested in Task 6.
- [ ] Verify authorization, expected versions, idempotent replay/conflict, audit events, and stable queries in service integration tests.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/sales/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/sales && rtk git commit -m "feat: add quote lifecycle and numbering"`.

## Chunk 2: Invoices, credits, settlements, UI, and acceptance

### Task 6: Issue, credit, correct, and query sales invoices

**Files:**
- Create: `services/core/internal/sales/invoice.go`
- Create: `services/core/internal/sales/invoice_test.go`
- Create: `services/core/internal/sales/invoice_service.go`
- Create: `services/core/internal/sales/invoice_service_integration_test.go`
- Create: `services/core/internal/sales/receivable_port.go`
- Create: `services/core/internal/sales/receivable_port_test.go`
- Create: `services/core/internal/settlements/adjustment_write_port.go`
- Create: `services/core/internal/settlements/adjustment_write_port_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Modify: `services/core/internal/transport/registrar.go`
- Modify: `services/core/internal/transport/registrar_test.go`

- [ ] Write service tests for draft/update/cancel, issue, linked partial credit, standalone contact credit, over-credit rejection by gross/net/GST/tax treatment, correction, allocated dependency, closed period, and prior correction.
- [ ] Implement issue as one UoW: freeze final number/contact snapshot/lines/rules/terms/totals; call `AccountingPostingPort.Post`; append typed event/audit; retain deterministic result.
- [ ] For non-cash basis post receivables/revenue/current GST payable facts; for cash basis use deferred GST payable. Prove both via public journal/tax projections.
- [ ] Keep lifecycle (`DRAFT`, `ISSUED`, `CANCELLED`, `CORRECTED`) separate from derived settlement state (`UNPAID`, `PART_PAID`, `PAID`, `OVERPAID`).
- [ ] Implement credit posting and reverse-and-replace correction as linked immutable documents/journals; never edit or silently void an issued invoice. In the same Sales-owned UoW, call `AllocationReadPort` and `SettlementAdjustmentWritePort.ApplyCredit` to split deferred versus already-recognized gross/net/GST, reverse deferred control immediately, create the awareness/consideration/adjustment-note event, and post pending/current reclassification through `AccountingPostingPort` before audit/result commit.
- [ ] Add exact credit tests for awareness date, liability-to-provide-consideration timing, required adjustment note/exception, proportional partial consideration, recognized/deferred/pending/finalized invariant, exact inverse reversal facts/journals, and current/deferred/pending control reconciliation.
- [ ] Emit immutable CashFlowFact components with every cash-affecting source posting and assert their sum equals each cash journal line.
- [ ] In this Sales-owned task, mark invoice-draft create/update/cancel, invoice issue, credit, and correction on the financial change-set and prove exactly one financial revision increment per successful UoW, including multi-journal corrections, and none on rollback or replay.
- [ ] Implement `ReceivableSourcePort.Allocatable` and explicit Get/List/OpenItem queries without exposing Sales tables.
- [ ] Add failure-injection subtests one at a time and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/sales -run '^TestInvoiceServiceFailureInjection/(after_document_freeze|after_number_allocation|after_ledger_posting|after_tax_adjustment|after_audit_append|after_revision_increment|after_response_persistence)$' -count=1`; each new case must first expose its named partial-state assertion, then pass after that exact checkpoint joins the caller UoW. Issue/credit/correction leave no partial state and reuse no rolled-back number.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/sales/... ./services/core/internal/accounting/... ./services/core/internal/settlements/... ./services/core/internal/app/... ./services/core/internal/transport/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/sales services/core/internal/settlements services/core/internal/app services/core/internal/transport && rtk git commit -m "feat: post invoices and sales credits"`.

### Task 7: Record receipts, refunds, and immutable allocations

**Files:**
- Create: `services/core/internal/settlements/payment.go`
- Create: `services/core/internal/settlements/payment_test.go`
- Create: `services/core/internal/settlements/allocation.go`
- Create: `services/core/internal/settlements/allocation_test.go`
- Create: `services/core/internal/settlements/allocation_property_test.go`
- Create: `services/core/internal/settlements/service.go`
- Create: `services/core/internal/settlements/service_integration_test.go`
- Create: `services/core/internal/settlements/read_ports.go`
- Create: `services/core/internal/settlements/read_ports_test.go`
- Create: `services/core/internal/settlements/banking_dependency_port.go`
- Create: `services/core/internal/settlements/banking_dependency_port_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Modify: `services/core/internal/transport/registrar.go`
- Modify: `services/core/internal/transport/registrar_test.go`

- [ ] Write tests for customer receipts/refunds, optional allocations, split allocations, credit applications, overpayments, unallocated credits, cross-contact/currency denial, and the exact open-item equation.
- [ ] Add allocation property tests for concurrent elections, stable largest-remainder line distribution, no source/payment zero crossing, and active plus reversed gross/net/GST never exceeding source lines.
- [ ] Implement settlement postings and immutable allocation/reversal events in one UoW using `ContactSnapshotPort`, `ReceivableSourcePort`, `AccountingPostingPort`, `TaxReportImpactPort`, `BankingDependencyPort`, and `AuditAppender`.
- [ ] For non-cash GST, prove allocation creates no second recognition. For cash GST, reclassify deferred to current using immutable consideration date and return `LATE_ATTRIBUTION_REQUIRES_RESOLUTION` for a closed period.
- [ ] Implement credit/refund adjustment facts and pending/current GST reclassification with explicit awareness/evidence/consideration provenance; later Slice 5 consumes them through `SettlementTaxFactPort`.
- [ ] Implement reverse allocation/payment dependency ordering and return all blocking resource refs. Banking-match dependencies are represented through a narrow adapter until Slice 4 binds it.
- [ ] Implement `AllocationReadPort`, `SettlementTaxFactPort`, and `PaymentMatchPort`; explicit Get/List projections include payments, allocations, and unallocated credits and are generated immutable messages.
- [ ] In this Settlements-owned task, mark receipt/refund/allocation/reversal and their GST-source changes on the Slice 1 financial change-set; prove exactly one financial revision increment per UoW, none on rollback/replay, and the expected local ledger/settlement/tax revisions.
- [ ] Inject failure after payment save, journal, allocation, GST fact, audit, revision, and response persistence for receipt/refund/allocation/allocation reversal/payment reversal; each rolls back every module.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/settlements/... ./services/core/internal/sales/... ./services/core/internal/accounting/... ./services/core/internal/app/... ./services/core/internal/transport/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/settlements services/core/internal/app services/core/internal/transport && rtk git commit -m "feat: add receivable settlements and allocations"`.

### Task 8: Add customer statements and aged receivables

**Files:**
- Create: `services/core/internal/reporting/receivables.go`
- Create: `services/core/internal/reporting/receivables_test.go`
- Create: `services/core/internal/reporting/ageing.go`
- Create: `services/core/internal/reporting/ageing_test.go`
- Create: `services/core/internal/reporting/financial_report_read_port.go`
- Create: `services/core/internal/reporting/financial_report_read_port_test.go`
- Create: `services/core/internal/accounting/receivables_control_read_port.go`
- Create: `services/core/internal/accounting/receivables_control_read_port_test.go`
- Create: `proto/tammy/v1/reporting.proto`
- Modify: `packages/connect-client/package.json`
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/main/rpc-router.ts`
- Modify: `apps/desktop/src/main/rpc-router.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Modify: `services/core/internal/transport/registrar.go`
- Modify: `services/core/internal/transport/registrar_test.go`
- Modify: `test/e2e/coverage.yaml`

- [ ] Create `reporting.proto` with only `GetCustomerStatement` and `GetAgedReceivables` Slice 2 RPCs. Generate and update descriptor coverage plus the imported production preload registry/router atomically; the descriptor test now requires the complete Slice 2 map.
- [ ] Write as-of tests for issue, credit, receipt, allocation/reversal, overpayment, contact snapshot/display choice, current/future due, and exact 1/30/31/60/61/90/91-day boundaries. Prove due date is the default ageing basis and explicit issue-date basis can place the same invoice in a different bucket; retain the selected basis in the response.
- [ ] Implement read models through `FinancialReportReadPort`, composed from `ReceivableSourcePort`, `AllocationReadPort`, and Accounting's receivables-control projection, not direct cross-module SQL. State the as-of timezone/date and ageing basis in each response.
- [ ] In one SQLite read transaction, retain the complete Slice 1 `RevisionVector`, canonical query, source/result hashes, and before/after vector; return `FINANCIAL_REVISION_CHANGED` rather than a mixed statement/ageing snapshot.
- [ ] Cross-check total aged open items against the receivables control balance and return an invariant failure rather than a misleading report when they differ.
- [ ] Export `reporting_pb.ts` through `packages/connect-client/package.json`, add its export-resolution assertion to the existing connect-client fixture test, and run `rtk pnpm contracts && rtk pnpm --filter @tammy/connect-client test && rtk pnpm --filter @tammy/desktop test && rtk pnpm --filter @tammy/desktop typecheck && rtk go test -tags tammy_sqlcipher ./services/core/internal/reporting/... ./services/core/internal/accounting/... ./services/core/internal/app/... ./services/core/internal/transport/... -count=1`.
- [ ] Commit: `rtk git add proto services/core/internal/gen packages/connect-client services/core/internal/reporting services/core/internal/accounting services/core/internal/app services/core/internal/transport apps/desktop/src/shared apps/desktop/src/main apps/desktop/src/preload test/e2e && rtk git commit -m "feat: report customer statements and ageing"`.

### Task 9: Build receivables IPC and production workflows

**Files:**
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/main/rpc-router.ts`
- Modify: `apps/desktop/src/main/rpc-router.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Create: `apps/desktop/src/renderer/features/contacts/contacts-screen.tsx`
- Create: `apps/desktop/src/renderer/features/contacts/contacts-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/sales/quotes-screen.tsx`
- Create: `apps/desktop/src/renderer/features/sales/invoices-screen.tsx`
- Create: `apps/desktop/src/renderer/features/sales/receipts-screen.tsx`
- Create: `apps/desktop/src/renderer/features/sales/customer-statement-screen.tsx`
- Create: `apps/desktop/src/renderer/features/sales/receivables-workflows.test.tsx`
- Modify: `apps/desktop/src/renderer/app-shell/navigation.tsx`
- Modify: `apps/desktop/src/renderer/app-shell/app-shell.tsx`

- [ ] Add router/preload codec tests for each new named method and its exact request/response schema/payload limit.
- [ ] Write React tests for duplicate-contact resolution, role flags, quote actions by allowed transition, invoice arithmetic preview/server correction, issue confirmation, credits/corrections, receipt allocation, overpayment, statement/ageing filters, and typed failure recovery.
- [ ] Implement Contact, Quotes, Invoices, Money In, Customer Statements, and Aged Receivables navigation with accessible forms/tables/dialogs and no placeholder controls.
- [ ] Display provisional draft number versus immutable final number explicitly and preserve server-returned totals as authoritative.
- [ ] Run `rtk pnpm --filter @tammy/desktop test && rtk pnpm typecheck && rtk pnpm lint`.
- [ ] Commit: `rtk git add apps/desktop && rtk git commit -m "feat: add receivables desktop workflows"`.

### Task 10: Accept Slice 2 in the packaged app

**Files:**
- Create: `apps/desktop/tests/e2e/contacts.spec.ts`
- Create: `apps/desktop/tests/e2e/quotes.spec.ts`
- Create: `apps/desktop/tests/e2e/receivables.spec.ts`
- Create: `apps/desktop/tests/e2e/receivables-cash-gst.spec.ts`
- Modify: `apps/desktop/tests/e2e/support/runtime-coverage.ts`
- Modify: `apps/desktop/tests/e2e/support/runtime-coverage.test.ts`
- Create: `compliance/evidence/core-accounting/slice-2-runtime-coverage.json`
- Create: `test/fixtures/receivables/canonical-month.pb.json`
- Modify: `compliance/traceability/core-accounting.csv`
- Modify: `compliance/evidence/core-accounting/manifest.json`
- Modify: `scripts/check-core-accounting-evidence.mjs`
- Modify: `scripts/check-core-accounting-evidence.test.mjs`
- Modify: `.github/workflows/foundation-windows11-e2e.yml`

- [ ] Implement packaged `E2E-02`, `E2E-03`, `E2E-04`, the receivables half of `E2E-07`, and receivables `E2E-08`, including every matrix role, invalid transition, stale version, exact replay/changed conflict, empty/populated/filter/page state, and principal failure named in coverage.
- [ ] Extend the generated-client-boundary tracer and write canonical `{caseId, fullyQualifiedRpc, actorRole, outcomeCode}` tuples to `slice-2-runtime-coverage.json`; compare them with tuples expanded from `coverage.yaml` and reject missing, extra, duplicate, skipped, or scenario-only claims.
- [ ] Keep preliminary runtime tuples/results in the run's untracked temporary evidence directory; materialize `slice-2-runtime-coverage.json` and the tracked manifest only after the reviewed clean-source commit and descriptor evidence generation.
- [ ] In the real packaged app, issue a GST-inclusive `$1,100.00` consulting invoice and allocate its `$1,100.00` receipt. For cash basis require issue `Dr Receivables 1,100 / Cr Revenue 1,000 / Cr Deferred GST payable 100`, receipt `Dr Bank 1,100 / Cr Receivables 1,100`, and allocation `Dr Deferred GST payable 100 / Cr Current GST payable 100`; the final receivables and deferred-GST balances are zero and current GST payable is `$100`. For non-cash basis require the issue to credit current GST immediately and allocation to create no second GST recognition. In both bases require statement/ageing agreement, one payment, source tax provenance, cash-flow fact `$1,100`, and linked audit events.
- [ ] Extend that oracle with a `$110.00` customer credit and refund. On non-cash basis require credit `Dr Revenue 100 / Dr Current GST payable 10 / Cr Receivables 110`, then refund `Dr Receivables 110 / Cr Bank 110` with no further GST entry. On cash basis after the original invoice is fully paid, require the refund-liable credit to retain `Dr Revenue 100 / Dr GST adjustment-pending control 10 / Cr Receivables 110` while current GST remains `$100`; when `$110` consideration is refunded require `Dr Receivables 110 / Cr Bank 110` plus `Dr Current GST payable 10 / Cr GST adjustment-pending control 10`. Final current GST payable is `$90`, adjustment-pending and customer credit are zero, revenue is net `$900`, cash-flow fact is `-110`, and retained awareness/liability/adjustment-note/consideration provenance proves the timing without double recognition.
- [ ] Add cash-basis part-payment, mixed-tax, credit, overpayment, refund, late-allocation, and reverse-dependency paths without direct SQL or mocked domain modules.
- [ ] Quit/relaunch, then create a Slice 2 backup and restore it into a fresh installation context; compare every public journal/subledger/tax/report/audit projection and evidence manifest hash.
- [ ] Run `rtk pnpm contracts && rtk pnpm lint && rtk pnpm typecheck && rtk pnpm test`.
- [ ] Run `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep "E2E-02|E2E-03|E2E-04|E2E-07|E2E-08"` and retain artefact/log/descriptor/fixture hashes.
- [ ] Run `.github/workflows/foundation-windows11-e2e.yml` job `windows11-23h2-x64-packaged-e2e` at the same revision and require the same Slice 2 matrix, signatures, encrypted storage, offline guard, and zero required skips.
- [ ] Run `rtk pnpm exec node --test scripts/check-core-accounting-evidence.test.mjs`; retain preliminary artefacts outside tracked evidence until the clean source commit exists.
- [ ] Request independent review, resolve critical/important findings, rerun affected tests, and commit reviewed source without retained descriptor/result evidence: `rtk git add apps/desktop test compliance/traceability scripts .github/workflows/foundation-windows11-e2e.yml && rtk git commit -m "test: accept contacts and receivables slice"`.
- [ ] From that clean source commit run `TAMMY_SOURCE_REVISION=$(rtk git rev-parse HEAD) rtk pnpm proto:descriptors:evidence`; verify the manifest subject, rerun the exact macOS/Windows Slice 2 packaged gates at that source revision, write the Slice 2 evidence manifest, and run `rtk pnpm core-accounting:evidence -- --slice 2` expecting `slice 2 evidence verified`.
- [ ] Commit only retained evidence: `rtk git add compliance/contracts compliance/evidence/core-accounting && rtk git commit -m "build: retain slice 2 accounting evidence"`.

## Slice 2 exit gate

- [ ] All new descriptor RPCs and lifecycles have complete machine-checked traceability.
- [ ] Issued snapshots, numbers, lines, tax rules, journal links, credits, allocations, reversals, and audit events are immutable and survive restart.
- [ ] Receivables control, open items, statements, aged receivables, tax facts, and journal projections cross-reconcile for non-cash and cash GST fixtures.
- [ ] E2E-02, E2E-03, E2E-04, and the receivables portions of E2E-07 and E2E-08 pass against the signed packaged app.
