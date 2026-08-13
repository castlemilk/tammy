# Banking and Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Slice 4: financial accounts, CSV/OFX/QFX staging, duplicate diagnostics, many-to-many matching, direct cash transactions, transfers, fees/interest, and bank/credit-card reconciliation with packaged E2E.

**Architecture:** Documents owns encrypted import source blobs and exposes immutable evidence references; Banking owns profiles, staging/jobs, statement lines, match components, transfers, and reconciliation state. Parsers produce generated staging projections before mutation. User-confirmed orchestrators compose evidence, settlement, cash, accounting, tax-impact, and audit ports under one UoW; suggestions are explainable and never commit automatically.

**Tech Stack:** Buf/Protobuf, Go/Connect/SQLCipher, standard Go CSV/XML parsing with bounded SGML normalization, Electron/React/TypeScript, Vitest, Playwright, property/concurrency tests.

---

**Normative design:** `docs/superpowers/specs/2026-08-02-core-business-accounting-suite-design.md` §§5, 8.5, 9, 12–15.

**Prerequisite:** Slice 3 is green, including the signed document helper and Banking intake-draft handoff.

**Required skills while executing:** `@superpowers:test-driven-development`, `@security-best-practices`, `@frontend-design`, `@playwright`, and `@superpowers:verification-before-completion`.

**Micro-TDD rule:** Take each named parser/sign/state/equation/concurrency case through one narrow red command, minimal implementation, and narrow `PASS` before the next row; broad commands are task-final regression checks only.

## Slice 4 RPC and UoW map

| Banking RPCs declared in this slice | Owner, ports, and class | Named preload/route | Scenario |
|---|---|---|---|
| `CreateFinancialAccount`, `UpdateFinancialAccount`, `GetFinancialAccount`, `ListFinancialAccounts` | Banking; ordinary commands use `LinkedLedgerOpeningReadPort.Read(tx, ledger_account_id, as_of)` and AuditAppender; queries use UoW.Read | matching lower-camel methods; `/banking/accounts` | E2E-09/E2E-11 |
| `StageStatementImport`, `CancelStatementImport`, `RetryStatementImport`, `GetStatementImportJob`, `ListStatementImportJobs`, `GetStatementImportStage`, `ListStatementImportStages`, `SaveImportProfile`, `GetImportProfile`, `ListImportProfiles` | client-streaming Banking persisted ingest/job plus queries/profile commands; DocumentEvidencePort, parser, AuditAppender at result commit | `stageStatementImportFromHandle` plus matching named methods; `/banking/imports` | E2E-09 |
| `CommitStatementImport`, `SetStatementLineExclusion`, `GetStatementImport`, `ListStatementImports`, `GetStatementLine`, `ListStatementLines` | Banking ordinary commands/query; staged result, versions, AuditAppender | matching named methods; `/banking/imports` | E2E-09/E2E-11 |
| `GetMatchSuggestions` | Banking query; PaymentCandidateSearchPort plus Banking line read projection, no mutation | `getMatchSuggestions`; `/banking/match` | E2E-10 |
| `ConfirmMatch`, `Unmatch`, `GetMatch`, `ListMatches` | Banking ordinary commands/queries; PaymentMatchPort or typed `SettlementRecordingPort`, exactly one AccountingPostingPort only when a new settlement is created, TaxReportImpactPort.ApplySourceImpact once for each financial match/unmatch command, Banking match repository, AuditAppender, one outer Banking UoW | matching named methods; `/banking/match` | E2E-10 |
| `CreateSpendMoney`, `CreateReceiveMoney` | Banking statement-line orchestration; `CashTransactionPort` is the sole accounting-posting path and returns the generated accounting source/CashFlowFact projection; TaxReportImpactPort.ApplySourceImpact once, match repository, AuditAppender, one UoW | matching named methods; `/banking/match` | E2E-10 |
| `ConfirmCashTransactionDraft` | Banking reviewed-draft orchestration with no statement line; ReviewedDocumentPort consumption, CashTransactionPort as sole accounting-posting path, TaxReportImpactPort.ApplySourceImpact once, AuditAppender, revisions/result, one UoW | `confirmCashTransactionDraft`; `/banking/match` | E2E-10 |
| `CreateTransfer`, `CompleteTransfer`, `GetTransfer`, `ListTransfers` | Banking ordinary commands/query; CashTransactionPort, TaxReportImpactPort.ApplySourceImpact once per financial create/complete command, match repository, AuditAppender | matching named methods; `/banking/match` | E2E-10 |
| `CreateReconciliation`, `UpdateReconciliation`, `CancelReconciliation`, `CompleteReconciliation`, `UndoReconciliation`, `GetReconciliation`, `ListReconciliations` | Banking ordinary commands/query; account/line/match repositories, `ReconciliationLedgerMovementReadPort.Read(tx, ledger_account_id, start_exclusive, end_inclusive)`, composition invariant adapter, TaxReportImpactPort.ApplySourceImpact once for complete/undo/re-complete, AuditAppender, one UoW | matching named methods; `/banking/reconcile` | E2E-11 |

Every ordinary command uses the shared authorization/idempotency/audit/result coordinator, versions all participants, and rolls back all port effects together. `coverage.yaml` contains this exact list. Generation produces exactly `services/core/internal/gen/tammy/v1/{banking,accounting,settlements,events,fixtures}.pb.go`, `packages/connect-client/src/gen/tammy/v1/{banking,accounting,settlements,events,fixtures}_pb.ts`, and `services/core/internal/gen/tammy/v1/tammyv1connect/banking.connect.go`; package exports, Go sums, and pnpm lock changes are committed atomically whenever changed.

For `ConfirmMatch` against an existing payment, Banking validates through `PaymentMatchPort`, calls `TaxReportImpactPort.ApplySourceImpact` once for the statement-match change, writes the Banking match/audit/result, and commits once; it performs no journal or Settlement tax-fact post. For a typed new settlement, Banking calls `SettlementRecordingPort.Record` to reuse the existing Settlement path for payment/allocation plus Settlement-owned immutable allocation/GST recognition facts, calls `AccountingPostingPort.Post` exactly once with the returned posting intent, calls `TaxReportImpactPort.ApplySourceImpact` exactly once for report lifecycle impact, writes Banking match/audit/result, marks revisions, and commits once. Statement-line spend/receive/fee/interest calls `CashTransactionPort` exactly once as the accounting posting path, calls `ApplySourceImpact` once, and writes a Banking match. `ConfirmCashTransactionDraft` instead consumes reviewed evidence and atomically writes only the cash source, journal/CashFlowFacts, tax impact, audit/result, and revisions; it has no statement match, and a later imported line uses separate `ConfirmMatch`. A transfer calls `CashTransactionPort.CreateTransfer` once, calls `ApplySourceImpact` once for its in-period journal, adds one or two Banking match sides, writes audit/result, and commits once. Reconciliation complete/undo/re-complete likewise calls `ApplySourceImpact` once; draft-only and query commands do not. Failure-injection tests assert those orders and rollback every projection under the caller-owned `TxScope`.

| Mutation/RPC family | Revision effect on successful commit |
|---|---|
| create a mandatory one-to-one mapped account; update display name, institution, masked number, archive/reactivate status, or linked-ledger mapping after validating existing Accounting conversion/dependency state | banking revision exactly once; financial delta 0; Banking never creates/replaces opening conversion |
| stage/cancel/retry import job; save import profile | operational job/profile revision only; financial delta 0 |
| `CommitStatementImport`, line exclude/re-include | banking exactly once; financial delta 0 |
| suggestions and every Get/List | no revision |
| `ConfirmMatch`, `Unmatch`, spend/receive/confirmed reviewed cash, transfer create/complete | financial + banking exactly once, even with settlement/accounting/tax effects |
| reconciliation create/update/cancel | reconciliation draft revision only; financial delta 0 |
| reconciliation complete/undo/re-complete | financial + banking exactly once |

Every row is tested for success, multi-effect one-increment, rollback, exact replay, and changed conflict.

**Per-task red/green index:** Task 1 starts with `rtk go test ./services/core/internal/contracts -run '^TestBankingDescriptor/CreateFinancialAccount$'` and `rtk pnpm --filter @tammy/connect-client test -- -t 'banking generated export'`; Task 2 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking -run '^TestBankingRepository/immutable_imported_value$'`; Task 3 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking -run '^TestOpeningEquation/outstanding_ledger_cheque$'` and `rtk go test -tags tammy_sqlcipher ./services/core/internal/accounting -run '^TestLinkedLedgerOpeningReadPort/existing_conversion$'`; Task 4 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking/imports -run '^TestParseCSV/utf16_bom$'`, `rtk go test -tags tammy_sqlcipher ./services/core/internal/documents -run '^TestStatementEvidencePort/encrypts_stream$'`, and `rtk go test -tags tammy_sqlcipher ./services/core/internal/transport -run '^TestStatementUpload/rejects_out_of_order_chunk$'`; Task 5 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking -run '^TestConfirmMatch/one_to_many$'` and `rtk go test -tags tammy_sqlcipher ./services/core/internal/settlements -run '^TestPaymentCandidateSearch/amount_and_date_window$'`; Task 6 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking -run '^TestConfirmMatch/create_customer_receipt$'`, `rtk go test -tags tammy_sqlcipher ./services/core/internal/settlements -run '^TestSettlementRecordingPort/customer_receipt$'`, and `rtk go test -tags tammy_sqlcipher ./services/core/internal/accounting -run '^TestCashTransactionPort/spend_money$'`; Task 7 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking -run '^TestReconciliation/first_bank_equation$'` then `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking -run '^TestReconciliationFailureInjection/after_state$'`; Task 8 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/backup -run '^TestBackup/banking_projection$'`, `rtk go test -tags tammy_sqlcipher ./services/core/internal/restore -run '^TestRestore/tampered_banking_source$'`, and `rtk pnpm exec node --test scripts/check-core-accounting-evidence.test.mjs`; Task 9 with `rtk pnpm --filter @tammy/desktop test -- -t 'bank import sign preview'`; and Task 10 with `rtk pnpm exec node --test --test-name-pattern 'slice 4 reconciliation cross-projection oracle' scripts/check-core-accounting-evidence.test.mjs`, initially returning `SLICE_EVIDENCE_ORACLE_MISSING`. Each new named case first exposes its missing symbol, typed error, wrong equation/revision, partial-state, or absent evidence tuple; implement only that case and rerun the identical package command to `PASS` before adding the next case or broad regression.

## Chunk 1: Banking contracts, imports, and matching

### Task 1: Complete Banking Protobuf contracts and traceability

**Files:**
- Modify: `proto/tammy/v1/banking.proto`
- Modify: `proto/tammy/v1/accounting.proto`
- Modify: `proto/tammy/v1/settlements.proto`
- Modify: `proto/tammy/v1/events.proto`
- Modify: `proto/tammy/v1/fixtures.proto`
- Create: `services/core/internal/contracts/banking_proto_test.go`
- Create: `services/core/internal/banking/service_handler.go`
- Create: `services/core/internal/banking/service_handler_test.go`
- Create: `packages/connect-client/src/banking-fixtures.test.ts`
- Modify: `packages/connect-client/package.json`
- Modify: `pnpm-lock.yaml`
- Modify: `services/core/internal/transport/registrar.go`
- Modify: `services/core/internal/transport/registrar_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Create: `test/fixtures/banking/sign-fixtures.pb.json`
- Create: `test/fixtures/banking/transitions.pb.json`
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/main/rpc-router.ts`
- Modify: `apps/desktop/src/main/rpc-router.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Modify: `test/e2e/coverage.yaml`

- [ ] Write a failing descriptor test requiring exactly the RPCs in the Slice 4 map, all account/import/line/match/transfer/reconciliation states, parser diagnostics, original and normalized signs/balances/remainders/components, versions, source evidence, and typed principal failures.
- [ ] Add service/messages compatibly to the Slice 3 `banking.proto`; keep statement/draft message field numbers stable and reserve removed names/numbers.
- [ ] Define generated `LinkedLedgerOpeningState` and `ReconciliationLedgerMovements` operation projections in `accounting.proto`, including ledger account/version, existing Accounting-owned opening-conversion state, normalized opening/current balance, immutable dated ledger movement IDs/amounts, exact as-of/range bounds, and content hash. They contain no Banking-owned match/clearance state and are narrow cross-module port values, not public Accounting RPCs or table-shaped DTOs.
- [ ] Create the one concrete generated `BankingServiceHandler` adapter now. It implements the complete generated interface, delegates each RPC to a narrow account/import/match/cash/transfer/reconciliation application interface, and returns typed `FEATURE_NOT_READY` only for delegates intentionally bound by later tasks. Register this adapter once; later tasks replace delegates and its test rejects any method left on the temporary binding at the slice gate.
- [ ] Define normalized amount semantics in field comments: positive is debit to the linked ledger account and negative is credit, unchanged for negative credit-card liability balances.
- [ ] Encode import-job, line-disposition, transfer, and reconciliation lifecycles in `test/fixtures/banking/transitions.pb.json`; run `rtk pnpm transitions:generate` and require coverage to consume the drift-free index.
- [ ] Extend `coverage.yaml` for every new RPC, named preload method, role, lifecycle/invalid transition, replay/conflict, stale/concurrency failure, empty/filter/page state, and `E2E-09`/`E2E-10`/`E2E-11` mapping.
- [ ] Add every generated request/response codec and temporary typed-unavailable handler binding to the imported production preload/router registry in the same change; no descriptor RPC may rely on a later unregistered placeholder or generic tunnel.
- [ ] Export every new generated module and run `rtk pnpm proto:generate && rtk pnpm contracts && rtk pnpm --filter @tammy/connect-client test && rtk pnpm --filter @tammy/desktop test && rtk go test -tags tammy_sqlcipher ./services/core/internal/banking/... ./services/core/internal/app/... ./services/core/internal/transport/...`; confirm generated/export/preload trees are clean, the one handler is registered, and old Slice 3 fixture bytes decode unchanged.
- [ ] Commit: `rtk git add proto services/core/internal/contracts services/core/internal/gen services/core/internal/banking packages/connect-client pnpm-lock.yaml services/core/internal/transport services/core/internal/app apps/desktop/src/shared apps/desktop/src/main apps/desktop/src/preload test && rtk git commit -m "feat: define banking protobuf contracts"`.

### Task 2: Add Banking storage constraints and migration coverage

**Files:**
- Create: `services/core/internal/storage/migrations/0030_banking.sql`
- Create: `services/core/internal/storage/migrations/0030_banking_test.go`
- Create: `services/core/internal/banking/repository.go`
- Create: `services/core/internal/banking/repository_test.go`
- Create: `services/core/internal/banking/import_repository.go`
- Create: `services/core/internal/banking/import_repository_test.go`
- Create: `services/core/internal/banking/match_repository.go`
- Create: `services/core/internal/banking/match_repository_test.go`
- Create: `services/core/internal/banking/reconciliation_repository.go`
- Create: `services/core/internal/banking/reconciliation_repository_test.go`
- Modify: `services/core/internal/banking/intake_draft_repository.go`

- [ ] Write failing repository tests for one-to-one ledger mapping, immutable imported values, account-scoped stable-ID uniqueness, fallback fingerprint/ordinal identity, overlap retention, exclusion events, signed remaining amounts, match components, one completed reconciliation ownership, sequence ordering, and optimistic versions.
- [ ] Add normalized financial-account, import-profile/stage/job/batch, source/statement-line, diagnostics, line-state event, match-group/component/inverse, transfer, reconciliation, and included-line tables with focused module-owned repositories/constraints/indexes.
- [ ] Extend the Slice 3 Banking intake draft rather than migrating it into another module. Document review references remain stable.
- [ ] Add encrypted migration tests from every prior schema prefix, staged-failure rollback, audit-chain verification, journal/subledger/GST checks, and invariant verification after activation.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/storage/migrations/... ./services/core/internal/banking/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/storage services/core/internal/banking && rtk git commit -m "feat: persist banking and reconciliation state"`.

### Task 3: Implement financial accounts and ledger-sign normalization

**Files:**
- Create: `services/core/internal/banking/account.go`
- Create: `services/core/internal/banking/account_test.go`
- Create: `services/core/internal/banking/signs.go`
- Create: `services/core/internal/banking/signs_test.go`
- Create: `services/core/internal/banking/account_service.go`
- Create: `services/core/internal/banking/account_service_test.go`
- Create: `services/core/internal/accounting/banking_opening_read_port.go`
- Create: `services/core/internal/accounting/banking_opening_read_port_test.go`
- Create: `services/core/internal/app/banking_accounting_invariant.go`
- Create: `services/core/internal/app/banking_accounting_invariant_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Modify: `services/core/internal/banking/service_handler.go`
- Modify: `services/core/internal/banking/service_handler_test.go`

- [ ] Write table tests for asset deposits/withdrawals, credit-card purchases/payments, original debit/credit markers, signed balances, zero, overflow, and contradictory source fields.
- [ ] Write account tests for mandatory one-to-one postable ledger mapping on create/update, ledger classification, duplicate mapping, masked account display, archive/reactivate, read-only conversion-state validation, and exact activation equation `normalized opening statement balance - outstanding statement-side movement + outstanding ledger-side movement = linked debit-positive ledger opening`. Include a bank counterexample where a ledger cheque absent from a `$0` statement is `0 - 0 + (-100) = -100`, plus deposits and equivalent negative credit-card cases; stale updates fail.
- [ ] Implement Accounting-owned `LinkedLedgerOpeningReadPort.Read(tx TxScope, ledgerAccountID, asOf) (LinkedLedgerOpeningState, error)` and bind it in composition. The composition-owned invariant adapter combines that generated Accounting projection with a Banking-owned opening projection; neither module imports the other's repository or queries the other's tables.
- [ ] Implement create/update/query services through the shared command pipeline. Create/Update always retain exactly one postable linked ledger account and only validate Accounting's existing opening-conversion/dependency state through the read port. Opening conversion creation/replacement remains exclusively in Accounting's `PostOpeningConversion` and fresh-TOTP `ReplaceOpeningConversion` commands. Never store credentials or live-feed tokens.
- [ ] Apply the effect-based revision table: every account create/update/mapping/status change increments Banking state only and has financial delta 0 because it changes none of the normative financial-revision triggers. Test success, rollback, replay, and conflict.
- [ ] Retain original institution amount/direction text beside normalized signed values and surface mapping diagnostics to the caller.
- [ ] Bind the account delegate and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking/... ./services/core/internal/accounting/... ./services/core/internal/app/... ./services/core/internal/transport/... -count=1`.
- [ ] Commit: `rtk git add services/core/internal/banking services/core/internal/accounting services/core/internal/app && rtk git commit -m "feat: add signed financial accounts"`.

### Task 4: Implement bounded CSV, OFX 1.x, OFX 2.x, and QFX staging

**Files:**
- Create: `services/core/internal/banking/imports/parser.go`
- Create: `services/core/internal/banking/imports/csv.go`
- Create: `services/core/internal/banking/imports/ofx.go`
- Create: `services/core/internal/banking/imports/normalise.go`
- Create: `services/core/internal/banking/imports/parser_test.go`
- Create: `services/core/internal/banking/imports/duplicates.go`
- Create: `services/core/internal/banking/imports/duplicates_test.go`
- Create: `services/core/internal/banking/import_service.go`
- Create: `services/core/internal/banking/import_service_integration_test.go`
- Create: `services/core/internal/banking/import_job.go`
- Create: `services/core/internal/banking/import_job_test.go`
- Create: `services/core/internal/documents/evidence_port.go`
- Create: `services/core/internal/documents/evidence_port_test.go`
- Create: `services/core/internal/transport/statement_upload.go`
- Create: `services/core/internal/transport/statement_upload_test.go`
- Modify: `services/core/internal/transport/server.go`
- Modify: `services/core/internal/transport/server_integration_test.go`
- Modify: `services/core/internal/banking/service_handler.go`
- Modify: `services/core/internal/banking/service_handler_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Modify: `services/core/internal/banking/intake_draft_port.go`
- Modify: `services/core/internal/banking/intake_draft_port_test.go`
- Create: `test/fixtures/banking/csv/manifest.json`
- Create: `test/fixtures/banking/ofx/manifest.json`
- Create: `scripts/generate-banking-fixtures.mjs`

- [ ] Generate individually named golden fixtures for UTF-8/UTF-16/BOM, CRLF, quoted/multiline, separate debit-credit, signed amount, formulas, invalid dates/numbers, repeated headers, empty/truncated/oversize rows, OFX 1.x SGML, OFX 2.x XML, QFX, missing/duplicate FITID, balances/timezones, corrupt/nested/DTD/entity input, and overlaps; the generator manifest records expected row/diagnostic hashes.
- [ ] Add the resource-limit subtests sequentially and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking/imports -run '^TestImportLimits/(raw_50_mib|decoded_text_100_mib|rows_100000|field_1_mib|nesting_64|diagnostics_10000|diagnostic_text_8_mib|dtd_denied|entity_denied)$' -count=1`; each new row first returns the wrong/missing named `IMPORT_*_LIMIT` diagnostic, then passes after only that bound is implemented.
- [ ] Enforce 50 MiB raw input, 100 MiB total decoded/normalized text (`IMPORT_DECODED_TEXT_LIMIT`), 100,000 rows, 1 MiB decoded field, 64 XML/SGML nesting levels, 10,000 diagnostics and 8 MiB total diagnostic text (`IMPORT_DIAGNOSTIC_LIMIT`), disabled DTD/external/internal entities, locale-explicit dates/decimals, stable normalization, original text retention, and formula-safe display/export.
- [ ] Write duplicate tests for account+FITID exact identity, fallback normalized date/amount/description/reference+occurrence ordinal, exact prior blocking, fuzzy warning only, same-source duplicates, and legitimate equal transactions.
- [ ] Implement `StageStatementImport` as client streaming: ordered ≤512 KiB chunks, ≤50 MiB total, declared length/SHA-256/operation key, and typed early-disconnect/duplicate/gap/hash failures while the global unary limit remains 1 MiB. `DocumentEvidencePort.StageStatementStream` writes authenticated ciphertext to a private staged object and returns a sealed ref/cleanup token without publishing metadata. In the caller-owned UoW, persist evidence metadata, Banking job, audit, idempotency result, and commit hook together; the hook atomically renames/fsyncs the staged object before SQL commit. Rename failure rolls back SQL and deletes staging; SQL failure after rename removes the final object; startup recovery removes staged/final objects lacking committed metadata by operation key and requeues committed jobs. Banking never owns encrypted blobs. CSV requires explicit mapping/sign confirmation and no staging path creates a statement line or journal.
- [ ] Persist operation key/input hash/attempt/stage/checkpoint hash/cancellation flag/result ref; test queued/running→queued restart, completed reconstruction, three process failures then explicit retry, no automatic validation retry, and cancellation-versus-result election.
- [ ] Consume Slice 3 reviewed statement drafts through the same parser/sign/duplicate/staging result and later `CommitStatementImport` command; no document-specific bypass exists.
- [ ] Implement safe saved profiles containing only account mapping/format metadata; never retain example values.
- [ ] Implement `CommitStatementImport` as one user command electing accepted/excluded rows and appending audit counts/hash/parser version; it is never background retried.
- [ ] Implement exclusion as an immutable state event requiring reason/version and rejecting active match or completed reconciliation; re-inclusion appends an inverse event and never edits evidence.
- [ ] Implement explicit Get/List committed-import, job, stage, profile, and statement-line projections with stable signed cursors and bounded filters; no query exposes encrypted bytes or repository rows.
- [ ] Bind the import delegate and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking/imports/... ./services/core/internal/banking/... ./services/core/internal/documents/... ./services/core/internal/transport/... ./services/core/internal/app/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/banking services/core/internal/documents services/core/internal/transport services/core/internal/app test/fixtures/banking scripts/generate-banking-fixtures.mjs && rtk git commit -m "feat: stage csv ofx and qfx statements"`.

### Task 5: Implement explainable suggestions and many-to-many confirmed matches

**Files:**
- Create: `services/core/internal/banking/suggestions.go`
- Create: `services/core/internal/banking/suggestions_test.go`
- Create: `services/core/internal/banking/match.go`
- Create: `services/core/internal/banking/match_test.go`
- Create: `services/core/internal/banking/match_property_test.go`
- Create: `services/core/internal/banking/match_service.go`
- Create: `services/core/internal/banking/match_service_integration_test.go`
- Create: `services/core/internal/settlements/payment_candidate_search_port.go`
- Create: `services/core/internal/settlements/payment_candidate_search_port_test.go`
- Modify: `services/core/internal/banking/service_handler.go`
- Modify: `services/core/internal/banking/service_handler_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Write suggestion tests for exact amount/sign, date distance, aliases, references, allocation remainder, deterministic score/reasons/order, and zero automatic state mutation.
- [ ] Write match property tests generating one-to-many and many-to-one components and proving neither statement nor accounting side crosses zero, completed groups balance exactly, and derived state is unmatched/part/full.
- [ ] Implement immutable match groups/components and inverse unmatch components; require expected versions for every line/target under one write transaction.
- [ ] Implement `PaymentCandidateSearchPort.Search(tx, account_id, direction, signed_remainder, date_window, cursor)` in Settlements to enumerate bounded generated candidate projections for suggestions; bind `PaymentMatchPort.Matchable(payment_id)` for exact target validation. Banking never reads settlement tables.
- [ ] Block unmatch after completed reconciliation, target reversal, or another draft owning the line and return all blocking resource refs.
- [ ] Implement explicit Get/List confirmed-match projections with immutable component/inverse links, signed cursor pagination, and source/target remainder hashes.
- [ ] Bind the match/query delegate and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking/... ./services/core/internal/settlements/... ./services/core/internal/app/... ./services/core/internal/transport/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/banking services/core/internal/settlements services/core/internal/app && rtk git commit -m "feat: match banking evidence many to many"`.

## Chunk 2: Cash creation, reconciliation, UI, and acceptance

### Task 6: Create payments, cash transactions, fees, interest, and transfers from lines

**Files:**
- Create: `services/core/internal/banking/cash_service.go`
- Create: `services/core/internal/banking/cash_service_integration_test.go`
- Create: `services/core/internal/banking/orchestrator_failure_test.go`
- Create: `services/core/internal/banking/transfer.go`
- Create: `services/core/internal/banking/transfer_test.go`
- Create: `services/core/internal/banking/transfer_service.go`
- Create: `services/core/internal/banking/transfer_service_integration_test.go`
- Create: `services/core/internal/accounting/cash_transaction_port.go`
- Create: `services/core/internal/accounting/cash_transaction_port_test.go`
- Create: `services/core/internal/settlements/recording_port.go`
- Create: `services/core/internal/settlements/recording_port_test.go`
- Modify: `services/core/internal/banking/service_handler.go`
- Modify: `services/core/internal/banking/service_handler_test.go`
- Modify: `services/core/internal/documents/target_service.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Write integration tests for `ConfirmMatch` with typed create/allocate customer-receipt or supplier-payment intent, direct spend/receive, bank fee, interest, reviewed receipt draft confirmation, partial line remainder, tax/evidence decisions, and closed-period rollback.
- [ ] Implement narrow Settlements-owned `SettlementRecordingPort.Record(tx, intent)` by invoking the existing Settlement domain path under the caller's `TxScope`. It writes payment/allocation-owned state and immutable allocation/GST recognition facts, and returns a generated matchable source/ref, remainder, and one typed accounting posting intent; it does not post a journal or mutate report lifecycle. The outer Banking orchestrator alone invokes Accounting once and `TaxReportImpactPort.ApplySourceImpact` once, and owns authorization/idempotency, Banking match components, audit/result persistence, revisions, and commit.
- [ ] Implement `CashTransactionPort.CreateSpend/CreateReceive/CreateTransfer/Reverse` using typed intents as the sole Accounting-owned journal/CashFlowFact posting path and return a generated accounting source projection. Banking never supplies arbitrary journal lines and never also calls `AccountingPostingPort` for these commands.
- [ ] Require `ConfirmCashTransactionDraft` after document review; verify and consume candidate/evidence version and arithmetic, then atomically persist cash source, journal/CashFlowFacts, tax impact, audit/result, and revisions with no statement match. A later imported line is matched only by separate `ConfirmMatch`; no extraction/review command can reach either posting path.
- [ ] Write transfer tests for same-account denial, one accounting event, two optional imported sides, different statement dates, one/two-sided completion, duplicate side, amount mismatch, and replay/conflict.
- [ ] Implement create/complete transfer and match both statement sides to the same source without a second journal.
- [ ] Require immutable CashFlowFact components for every spend/receive/fee/interest/payment and for both transfer cash lines. Each component sum equals its cash journal line, classifications are deterministic or explicit from the reviewed intent, and transfer components net to zero in reporting.
- [ ] Add each command-family checkpoint sequentially and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking -run '^TestBankingOrchestratorFailure/(after_settlement_or_cash_source|after_journal|after_tax_fact|after_match_component|after_audit|after_revision|after_response_persistence)$' -count=1`; each new case first exposes its named surviving partial projection, then passes when the outer Banking UoW owns rollback.
- [ ] Bind cash/transfer delegates and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking/... ./services/core/internal/accounting/... ./services/core/internal/settlements/... ./services/core/internal/documents/... ./services/core/internal/app/... ./services/core/internal/transport/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/banking services/core/internal/accounting services/core/internal/documents services/core/internal/settlements services/core/internal/app && rtk git commit -m "feat: create banking transactions and transfers"`.

### Task 7: Implement first, subsequent, undo, and re-complete reconciliation

**Files:**
- Create: `services/core/internal/banking/reconciliation.go`
- Create: `services/core/internal/banking/reconciliation_test.go`
- Create: `services/core/internal/banking/reconciliation_property_test.go`
- Create: `services/core/internal/banking/reconciliation_service.go`
- Create: `services/core/internal/banking/reconciliation_service_integration_test.go`
- Create: `services/core/internal/banking/reconciliation_failure_test.go`
- Create: `services/core/internal/accounting/reconciliation_movement_read_port.go`
- Create: `services/core/internal/accounting/reconciliation_movement_read_port_test.go`
- Modify: `services/core/internal/app/banking_accounting_invariant.go`
- Modify: `services/core/internal/app/banking_accounting_invariant_test.go`
- Modify: `services/core/internal/banking/service_handler.go`
- Modify: `services/core/internal/banking/service_handler_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Modify: `services/core/internal/accounting/invariants.go`

- [ ] Write exact equation tests from Design §9.4 for asset-bank and negative credit-card balances, first opening conversion, subsequent prior close, included movement, newly cleared ledger movement, component equality, and zero difference.
- [ ] Add property/concurrency tests for exact next start after prior completed end, range overlap, explicit gap reason plus query proving no unassigned gap line, earlier draft cancel/complete ordering, partially matched/unexcluded lines, retained included-component snapshot, outstanding ledger movement, simultaneous complete/undo/match/unmatch election, and version conflicts.
- [ ] Implement draft create/update/cancel and immutable completion. Cancellation releases draft ownership with an audit event; every included line is fully matched or explicitly excluded, and an unmatched note is insufficient.
- [ ] Implement undo as a linked event requiring preparer/admin, reason, and no later completed dependency; retain evidence/components and release ownership. Re-completion creates/links a new completion revision.
- [ ] Implement Accounting-owned `ReconciliationLedgerMovementReadPort.Read(tx TxScope, ledgerAccountID, startExclusive, endInclusive) (ReconciliationLedgerMovements, error)` with immutable ledger movements and a stable content hash but no clearance state. Extend the composition-owned cross-projection invariant adapter to join those movements to Banking-owned match snapshots, derive newly cleared/outstanding movement, and combine them with statement/reconciliation projections at the same `TxScope`/as-of instant; neither side reads the other's tables.
- [ ] Add one reconciliation failpoint subtest at a time and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking -run '^TestReconciliationFailureInjection/(after_state|after_component_snapshot|after_invariant_check|after_audit|after_revision|after_response_persistence)$' -count=1`; each first exposes its named partial-state assertion and then passes only when every draft/completion/undo effect shares the Banking caller's UoW.
- [ ] Extend explicit invariant verification so ledger account, reconciled statement balance, cleared movement, and outstanding movement agree at the same as-of instant.
- [ ] Assert the concrete generated Banking handler has no remaining `FEATURE_NOT_READY` delegate and every RPC reaches its intended application interface through the production registrar.
- [ ] Bind the reconciliation delegate and run `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking/... ./services/core/internal/accounting/... ./services/core/internal/app/... ./services/core/internal/transport/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/banking services/core/internal/accounting services/core/internal/app && rtk git commit -m "feat: reconcile bank and credit card accounts"`.

### Task 8: Extend backup/restore and audit evidence for Banking

**Files:**
- Modify: `services/core/internal/backup/service_integration_test.go`
- Modify: `services/core/internal/restore/service_integration_test.go`
- Create: `services/core/internal/banking/backup_projection.go`
- Create: `services/core/internal/banking/backup_projection_test.go`
- Modify: `services/core/internal/backup/provider_registry.go`
- Modify: `services/core/internal/backup/provider_registry_test.go`
- Modify: `services/core/internal/restore/provider_registry.go`
- Modify: `services/core/internal/restore/provider_registry_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Modify: `scripts/check-core-accounting-evidence.mjs`
- Modify: `scripts/check-core-accounting-evidence.test.mjs`

- [ ] Add a red Slice 4 backup test containing financial accounts/mappings/opening conversions, CSV/OFX evidence refs, profiles, queued/running/retryable/cancelled/completed jobs and checkpoints, completed stages/batches, immutable lines, exclusions, many-to-many matches, transfers, draft/completed/undone reconciliations, and audit links.
- [ ] Implement a Banking-owned immutable backup projection; Backup consumes it without cross-module SQL and includes referenced encrypted source objects via Documents.
- [ ] Register the Banking provider/validator in the production Backup/Restore registries and composition root.
- [ ] Restore into a fresh installation, first compare retained pre-restore public projections/content hashes for every financial account and every queued/running/retryable/cancelled/completed import job/checkpoint. On startup assert a retained `RUNNING` job deterministically becomes `QUEUED` with the prescribed attempt increment/checkpoint hash, while all other states remain semantically identical; never restore a phantom worker. Then run all journal/subledger/bank/GST/reconciliation/audit invariants. Missing/tampered source evidence or Banking rows aborts before swap.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/backup/... ./services/core/internal/restore/... ./services/core/internal/banking/... ./services/core/internal/app/... -race -count=1` and `rtk pnpm exec node --test scripts/check-core-accounting-evidence.test.mjs`; expect production registry, Slice 4 backup/restore/tamper cases, and evidence-checker tests pass.
- [ ] Commit: `rtk git add services/core/internal/backup services/core/internal/restore services/core/internal/banking services/core/internal/app scripts && rtk git commit -m "feat: preserve banking state in backup restore"`.

### Task 9: Build Banking IPC and production workflows

**Files:**
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/main/rpc-router.ts`
- Modify: `apps/desktop/src/main/rpc-router.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Create: `apps/desktop/src/renderer/features/banking/accounts-screen.tsx`
- Create: `apps/desktop/src/renderer/features/banking/import-screen.tsx`
- Create: `apps/desktop/src/renderer/features/banking/match-screen.tsx`
- Create: `apps/desktop/src/renderer/features/banking/reconciliation-screen.tsx`
- Create: `apps/desktop/src/renderer/features/banking/accounts-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/banking/import-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/banking/match-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/banking/reconciliation-screen.test.tsx`
- Create: `apps/desktop/src/main/statement-file-intake.ts`
- Create: `apps/desktop/src/main/statement-file-intake.test.ts`
- Create: `apps/desktop/tests/e2e/bank-imports.spec.ts`
- Create: `apps/desktop/tests/e2e/bank-matching.spec.ts`
- Create: `apps/desktop/tests/e2e/reconciliation.spec.ts`
- Modify: `apps/desktop/src/renderer/app-shell/navigation.tsx`

- [ ] Add named IPC codec/payload tests for every new method and implement `stageStatementImportFromHandle`: OS-approved handle only, Electron main streams ≤512 KiB chunks through the generated client, renderer receives no unrestricted path/raw file API, and cancellation closes the handle and leaves no plaintext copy.
- [ ] Write React tests for account setup, mapping preview, sign diagnostics, duplicate review, staged exclusions, explainable suggestions, partial many-to-many editing, direct cash/payment/transfer flows, first/subsequent reconciliation, gap acknowledgement, zero-difference gate, undo, and typed recovery.
- [ ] Implement Bank Accounts, Imports, Match, and Reconcile navigation with accessible virtualized/paginated tables and no auto-confirm or placeholder action.
- [ ] Before implementing each corresponding UI path, add its thin packaged case, run `rtk pnpm package`, then run exactly `rtk pnpm test:e2e:packaged -- --grep "E2E-09 CSV import"`, `rtk pnpm test:e2e:packaged -- --grep "E2E-10 many-to-many match"`, or `rtk pnpm test:e2e:packaged -- --grep "E2E-11 first reconciliation"`; require the first run to fail on the named missing production control. Implement only that vertical codec/UI workflow, rerun desktop unit/type/lint checks, rerun `rtk pnpm package`, and rerun the identical grep to `PASS` before the next workflow. Commit these specs here; Task 10 expands matrices/evidence.
- [ ] Show original institution values beside normalized ledger-sign values and display both sides/component remainder before confirmation.
- [ ] Run `rtk pnpm --filter @tammy/desktop test && rtk pnpm typecheck && rtk pnpm lint && rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep "E2E-09 CSV import|E2E-10 many-to-many match|E2E-11 first reconciliation"`; all three packaged workflows must be green before commit.
- [ ] Commit: `rtk git add apps/desktop && rtk git commit -m "feat: add banking and reconciliation workflows"`.

### Task 10: Accept Slice 4 in the signed packaged app

**Files:**
- Modify: `apps/desktop/tests/e2e/bank-imports.spec.ts`
- Modify: `apps/desktop/tests/e2e/bank-matching.spec.ts`
- Modify: `apps/desktop/tests/e2e/reconciliation.spec.ts`
- Modify: `apps/desktop/tests/e2e/support/runtime-coverage.ts`
- Modify: `apps/desktop/tests/e2e/support/runtime-coverage.test.ts`
- Create: `compliance/evidence/core-accounting/slice-4-runtime-coverage.json`
- Create: `test/fixtures/banking/reconciliation-month.pb.json`
- Modify: `compliance/traceability/core-accounting.csv`
- Modify: `compliance/evidence/core-accounting/manifest.json`
- Modify: `scripts/check-core-accounting-evidence.mjs`
- Modify: `scripts/check-core-accounting-evidence.test.mjs`
- Modify: `.github/workflows/foundation-windows11-e2e.yml`

- [ ] Implement packaged `E2E-09` for CSV profile, OFX 1.x/2.x, QFX, bank/credit-card signs, exact duplicates, fuzzy review, overlaps, corrupt limits, empty/filter/page states, and import restart/cancel.
- [ ] Implement packaged `E2E-10` for partial many-to-many matches, existing/new payments, spend/receive, fee, interest, two-sided transfer, document-reviewed receipt confirmation, and unmatch dependencies.
- [ ] Implement packaged `E2E-11` for first/subsequent bank and credit-card reconciliations, outstanding movements, gap acknowledgement, non-zero rejection, lock, concurrent election, undo, and re-completion.
- [ ] Drive the coverage manifest's four-role, invalid-state, stale-version, replay/conflict, principal-failure, and empty/populated/filter/page cases for every Slice 4 RPC with zero skips.
- [ ] Extend the generated-client-boundary tracer and write canonical `{caseId, fullyQualifiedRpc, actorRole, outcomeCode}` tuples to `slice-4-runtime-coverage.json`; compare them with tuples expanded from `coverage.yaml` and reject missing, extra, duplicate, skipped, or scenario-only claims.
- [ ] Keep preliminary runtime tuples/results in the run's untracked temporary evidence directory; materialize `slice-4-runtime-coverage.json` and the tracked manifest only after the reviewed clean-source commit and descriptor evidence generation.
- [ ] In the canonical month, require the `$1,100` customer receipt and `$220` supplier payment to match imported lines, one asset-bank statement movement of net `$880`, debit-positive bank ledger balance `$880`, fully balanced match components, completed reconciliation difference `$0`, cash-flow facts net `$880`, and matching audit refs.
- [ ] Assert through public projections that journal bank balance, payment/cash sources, match components, statement movements, reconciliation equations, and audit events agree before/after restart and after fresh-install backup restore.
- [ ] Run `rtk pnpm contracts && rtk pnpm lint && rtk pnpm typecheck && rtk pnpm test`.
- [ ] Run `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep "E2E-09|E2E-10|E2E-11"` with network denied; retain parser/fixture/descriptor/artefact/result hashes.
- [ ] Run `.github/workflows/foundation-windows11-e2e.yml` job `windows11-23h2-x64-packaged-e2e` at the same revision; require the identical matrix, 23H2 attestation, signatures, offline guard, encrypted storage, and zero skips.
- [ ] Run `rtk pnpm exec node --test scripts/check-core-accounting-evidence.test.mjs`; retain preliminary artefacts outside tracked evidence until the clean source commit exists.
- [ ] Request independent review, resolve critical/important findings, rerun affected gates, and commit reviewed source without retained descriptor/result evidence: `rtk git add apps/desktop test compliance/traceability scripts .github/workflows/foundation-windows11-e2e.yml && rtk git commit -m "test: accept banking and reconciliation slice"`.
- [ ] From that clean source commit run `TAMMY_SOURCE_REVISION=$(rtk git rev-parse HEAD) rtk pnpm proto:descriptors:evidence`; verify the manifest subject, rerun the exact macOS/Windows Slice 4 packaged gates at that revision, write the Slice 4 evidence manifest, and run `rtk pnpm core-accounting:evidence -- --slice 4` expecting `slice 4 evidence verified`.
- [ ] Commit only retained evidence: `rtk git add compliance/contracts compliance/evidence/core-accounting && rtk git commit -m "build: retain slice 4 accounting evidence"`.

## Slice 4 exit gate

- [ ] Every new RPC/transition/role/failure/list state has machine-checked descriptor traceability and packaged coverage.
- [ ] Imported evidence is immutable, duplicate decisions are reviewable, matching is never automatic, and all concurrent remainder elections are deterministic.
- [ ] Bank/credit-card ledger balances, source payments/cash transactions, match groups, statement balances, and reconciliation projections cross-reconcile.
- [ ] E2E-09, E2E-10, and E2E-11 pass against the signed packaged app using real encrypted storage and generated clients.
