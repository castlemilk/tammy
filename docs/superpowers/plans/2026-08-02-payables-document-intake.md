# Payables and Document Intake Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Slice 3: supplier bills/credits/payments/refunds, aged payables, encrypted PDF/image evidence, local native-text extraction plus Tesseract fallback, mandatory candidate review, and draft-only handoff proven in packaged E2E.

**Architecture:** Purchases snapshots suppliers and posts through the shared Accounting and Settlement ports. Documents stores originals and derivatives as authenticated encrypted blobs, drives a persisted restart-safe extraction job, and launches a separately sandboxed Rust helper over bounded length-delimited Protobuf stdio. The helper classifies/extracts native text with pinned pdf-inspector, rasterizes only pages/regions requiring pixels through pinned PDFium, and OCRs those pixels with statically linked Tesseract; it can return candidates but cannot access the workspace or post anything.

**Tech Stack:** Buf/Protobuf, Go/Connect/SQLCipher, Rust 1.97.1, pdf-inspector commit `a15ec2d68d51dbe6a39d1da688ec7a3f642d846c`, PDFium commit `f4facdd2652f771eb11d605a82fea2bbfbe66d9f`, Tesseract 5.5.2 commit `6e1d56a847e697de07b38619356550e5cf4e8633`, Leptonica 1.87.0 commit `13275a278eb55b5746e33f95fbf5a2c8f604b3ab`, tessdata commit `ced78752cc61322fb554c280d13360b35b8684e4`, AES-GCM evidence envelopes, Electron/React, Vitest, Playwright.

---

**Normative designs:** `docs/superpowers/specs/2026-08-02-core-business-accounting-suite-design.md` §§4–8, 10–16 plus unchanged foundation storage/security/backup rules.

**Prerequisite:** Slice 2 is green and its signed artefact/evidence are retained.

**Dependency rule:** Unlimited-OCR is not part of this plan. The optional experiment in Design Appendix A remains blocked until this slice passes and receives a separate threat model/design approval.

**Required skills while executing:** `@superpowers:test-driven-development`, `@security-best-practices`, `@frontend-design`, `@playwright`, and `@superpowers:verification-before-completion`.

**Micro-TDD rule:** Implement every named fixture/failure as one red → minimal green cycle with its narrow Go/Rust/Node/Vitest command before moving to the next. Broad matrix commands are final checks, never substitutes for observing each failing case.

## Slice 3 RPC and UoW map

| Service/RPCs declared in this slice | Owner, required ports, and transaction class | Named preload/route | Scenario |
|---|---|---|---|
| Purchases `CreateBillDraft`, `UpdateBillDraft`, `CancelBillDraft`, `ApproveBill`, `CreateSupplierCredit`, `RecordTaxDocumentEvidence`, `CorrectBill`, `GetBill`, `ListBills`, `GetSupplierCredit`, `ListSupplierCredits`, `GetPayableOpenItem`, `ListPayableOpenItems` | Purchases; ordinary commands use ContactSnapshotPort, AccountingPostingPort, AllocationReadPort, SettlementAdjustmentWritePort, TaxReportImpactPort, ReviewedDocumentPort when applicable, AuditAppender, one UoW | matching lower-camel methods; `/purchases/bills` | E2E-05/E2E-08 |
| Settlement `RecordSupplierPayment`, `RecordSupplierRefund`, `AllocatePayment`, `ReverseAllocations`, `ReversePayment`, `GetPayment`, `ListPayments`, `GetAllocation`, `ListAllocations`, `GetUnallocatedCredit`, `ListUnallocatedCredits` | Settlements; ContactSnapshotPort, PayableSourcePort, AccountingPostingPort, TaxReportImpactPort, BankingDependencyPort, AuditAppender, one UoW | matching named methods; `/purchases/money-out` | E2E-07/E2E-08 |
| Reporting `GetSupplierActivityStatement`, `GetAgedPayables` | Reporting; revision-pinned FinancialReportReadPort over PayableSourcePort, AllocationReadPort, and Accounting control projection | matching named methods; `/reports/payables` | E2E-05/E2E-07 |
| Document `IngestDocument` | client-streaming persistent ingest; Documents evidence/job repository and helper launcher, audited result commit | `ingestDocumentFromHandle`; `/documents/inbox` | E2E-05/E2E-06 |
| Document `ProvidePdfPassword` | fresh security challenge; Documents/helper, no ordinary retry/idempotency secret | `providePdfPassword`; `/documents/inbox` | E2E-06 |
| Document `CancelExtraction`, `RetryExtraction`, `SaveReview`, `CreateTargetDraft`, `SupersedeDocumentReview` | Documents ordinary job/review commands; ReviewedDocumentPort plus Purchases or BankingIntakeDraftPort, AuditAppender, one UoW | matching named methods; `/documents/inbox|review` | E2E-05/E2E-06 |
| Document `GetEvidencePage` | server-streaming evidence query; Documents encrypted blob repository in `UoW.Read`, ordered bounded chunks | `streamEvidencePage` over one named MessagePort; `/documents/inbox|review` | E2E-06 |
| Document `GetExtractionJob`, `ListExtractionJobs`, `GetDocumentReview`, `ListDocumentReviews` | bounded unary queries; Documents job/review repositories in `UoW.Read` | matching named methods; `/documents/inbox|review` | E2E-06 |

`coverage.yaml` contains this exact public surface; helper protocol messages and Banking draft messages are not public services. The changed proto set generates exactly `services/core/internal/gen/tammy/v1/{purchases,documents,banking,settlements,reporting,events,fixtures}.pb.go` and `packages/connect-client/src/gen/tammy/v1/{purchases,documents,banking,settlements,reporting,events,fixtures}_pb.ts`; service-bearing files generate `services/core/internal/gen/tammy/v1/tammyv1connect/{purchases,documents,settlements,reporting}.connect.go`. These files are committed with `go.sum`, package exports, and lockfiles whenever changed.

**Per-task red/green index:** Task 1 begins with `rtk go test ./services/core/internal/contracts -run '^TestPayablesDocumentsDescriptor/CreateBillDraft$'` and `rtk pnpm --filter @tammy/connect-client test -- -t 'helper protocol round trip'`; Task 2 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/purchases -run '^TestPurchaseRepository/frozen_approved_bill$'`, `rtk go test -tags tammy_sqlcipher ./services/core/internal/documents -run '^TestDocumentRepository/one_target_consumption$'`, and `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking -run '^TestIntakeDraftRepository/one_review_target$'`; Task 3 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/purchases -run '^TestPurchaseService/approve_cash_basis$'`; Task 4 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/settlements -run '^TestSupplierSettlement/payment_and_allocate$'` and `rtk go test -tags tammy_sqlcipher ./services/core/internal/reporting -run '^TestAgedPayables/due_date_default$'`; Task 5 uses `rtk pnpm exec node --test --test-name-pattern 'pdf-inspector revision' scripts/vendor-document-deps.test.mjs` and `rtk cargo test -p tammy-document-extractor --test dependency_smoke --locked native_page_count`; Task 6 uses `rtk cargo test -p tammy-document-extractor --test protocol --locked begin_chunk_end`; Task 7 uses `rtk pnpm exec node --test --test-name-pattern 'denies outbound socket' scripts/check-document-helper-sandbox.test.mjs` and `rtk go test ./services/core/internal/documents/launcher -run '^TestLauncher/denies_outbound_socket$'`; Task 8 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/documents -run '^TestExtractionJob/requeues_running_on_start$'` and `rtk go test -tags tammy_sqlcipher ./services/core/internal/transport -run '^TestDocumentUpload/rejects_out_of_order_chunk$'`; Task 9 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/documents -run '^TestCreateTargetDraft/one_time_consumption$'`, `rtk go test -tags tammy_sqlcipher ./services/core/internal/purchases -run '^TestCreateBillDraft/reviewed_handoff$'`, and `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking -run '^TestIntakeDraftPort/reviewed_statement$'`; Task 10 uses `rtk go test -tags tammy_sqlcipher ./services/core/internal/backup -run '^TestBackup/document_evidence$'` and `rtk go test -tags tammy_sqlcipher ./services/core/internal/restore -run '^TestRestore/tampered_evidence$'`; Task 11 uses `rtk pnpm --filter @tammy/desktop test -- -t 'review never posts'`; and Task 12 uses `rtk pnpm test:e2e:packaged -- --grep 'E2E-05 native PDF bill'`. Each first run must expose the named missing symbol/assertion, typed boundary failure, or absent production control. Add one exact subtest per remaining matrix case in its owning package, rerun the same package command with that final test path/filter, implement only that case, and rerun to `PASS` before the broad regression command.

## Chunk 1: Payables contracts and financial workflows

### Task 1: Define purchases, documents, and helper contracts

**Files:**
- Create: `proto/tammy/v1/purchases.proto`
- Create: `proto/tammy/v1/documents.proto`
- Create: `proto/tammy/v1/banking.proto`
- Modify: `proto/tammy/v1/settlements.proto`
- Modify: `proto/tammy/v1/reporting.proto`
- Modify: `proto/tammy/v1/events.proto`
- Modify: `proto/tammy/v1/fixtures.proto`
- Create: `services/core/internal/contracts/payables_documents_proto_test.go`
- Create: `packages/connect-client/src/payables-documents-fixtures.test.ts`
- Modify: `packages/connect-client/package.json`
- Modify: `pnpm-lock.yaml`
- Modify: `services/core/internal/transport/registrar.go`
- Modify: `services/core/internal/transport/registrar_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Create: `services/core/internal/app/slice3_placeholders.go`
- Create: `services/core/internal/app/slice3_placeholders_test.go`
- Modify: `services/core/internal/settlements/service.go`
- Modify: `services/core/internal/settlements/service_integration_test.go`
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/main/rpc-router.ts`
- Modify: `apps/desktop/src/main/rpc-router.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Create: `test/fixtures/documents/helper-protocol.pb.json`
- Create: `test/fixtures/payables/transitions.pb.json`
- Create: `test/fixtures/documents/transitions.pb.json`
- Modify: `test/e2e/coverage.yaml`

- [ ] Write a failing descriptor test requiring exactly the RPCs in the Slice 3 map, client-streaming ingest frames, outgoing settlement/refund operations, aged-payables query, helper frame/candidate/evidence messages, and review target kinds.
- [ ] Define `banking.proto` now only for shared `SpendMoneyDraft`, `StatementImportDraft`, and `StagedStatementLine` messages used by reviewed handoff; do not expose Slice 4 Banking RPCs yet.
- [ ] Define helper input/progress/password-state/output frames in `documents.proto` with protocol version, job ID, opaque job-directory capability (not a caller path), original hash, locale, hard limits, page/coordinate evidence, engine/version, confidence, typed terminal failures, and no workspace/database/session fields.
- [ ] Define generated `DocumentRenderPolicy` values now. OCR policy `pdf-ocr-raster-v1` fixes 300 DPI, `ceil(points × 300 / 72)` dimensions, PDF rotation before allocation, 20,000-pixel edge and 100-megapixel/page limits, PDFium flags `FPDF_ANNOT|FPDF_LCD_TEXT`, BGRA→8-bit sRGB RGBA, white alpha composite, no system-font lookup, and pinned fallback-font hashes. Evidence policy `pdf-evidence-preview-v1` fits within 2,000×2,000 pixels, uses fixed-filter/no-metadata PNG, and caps one preview at 16 MiB.
- [ ] Define `GetEvidencePage` as a generated server-streaming RPC whose ordered chunks are ≤512 KiB and carry sequence, total length, SHA-256, MIME, and render-policy ID; map it only to the named `streamEvidencePage` preload MessagePort while every other Get/List RPC remains bounded unary.
- [ ] Define the initial `IngestDocument` metadata frame with `source_display_name`: a Unicode-normalized, control-free basename/display label of at most 255 bytes with all path separators removed. It is provenance only; no absolute/parent path or security-scoped handle value enters Protobuf, the helper protocol, logs, audit payloads, or public evidence projections.
- [ ] Define exact job and review transitions, including three password failures, cancellation election, sealed/superseded/consumed review behavior, and separate target/posting states.
- [ ] Encode all Purchases/Settlement transitions in `test/fixtures/payables/transitions.pb.json` and all Document job/review transitions in `test/fixtures/documents/transitions.pb.json`; run `rtk pnpm transitions:generate` before the coverage check.
- [ ] Update `coverage.yaml` with every new public RPC, role, transition, principal failure, replay/conflict, query state, preload method, and `E2E-05`/`E2E-06`/`E2E-07`/`E2E-08` scenario.
- [ ] Export every new generated Protobuf module from `packages/connect-client/package.json`, add an export-resolution/round-trip test, generate Go/TypeScript, register Purchases and Document generated handlers in the production composition root, and add every Slice 3 codec/method—including `streamEvidencePage`'s named MessagePort—to the imported production desktop/preload registry with typed `FEATURE_NOT_READY` behavior until its owning task binds the implementation; no generic tunnel is permitted.
- [ ] Extend the existing Slice 2 Settlement and Reporting core handler surfaces in the same contract change. `slice3_placeholders.go` supplies exact narrow typed `FEATURE_NOT_READY` delegates for every new supplier Settlement/Reporting method plus not-yet-implemented Purchases/Documents methods, and its tests invoke every generated method through the production registrar. Tasks 3, 4, and 8 replace these delegates; no generated Connect interface is left uncompilable or accidentally served by an embedded unimplemented handler.
- [ ] Run `rtk pnpm contracts && rtk pnpm --filter @tammy/connect-client test && rtk pnpm --filter @tammy/desktop test && rtk go test -tags tammy_sqlcipher ./services/core/internal/contracts/... ./services/core/internal/settlements/... ./services/core/internal/app/... ./services/core/internal/transport/...`; confirm descriptor/fixture/export/preload/registration tests pass.
- [ ] Commit: `rtk git add proto services/core/internal/contracts services/core/internal/gen packages/connect-client pnpm-lock.yaml services/core/internal/settlements services/core/internal/transport services/core/internal/app apps/desktop/src/shared apps/desktop/src/main apps/desktop/src/preload test && rtk git commit -m "feat: define payables and document contracts"`.

### Task 2: Add payables, evidence, jobs, and draft-handoff storage

**Files:**
- Create: `services/core/internal/storage/migrations/0020_purchases_documents.sql`
- Create: `services/core/internal/storage/migrations/0020_purchases_documents_test.go`
- Create: `services/core/internal/purchases/repository.go`
- Create: `services/core/internal/purchases/repository_test.go`
- Create: `services/core/internal/documents/repository.go`
- Create: `services/core/internal/documents/repository_test.go`
- Create: `services/core/internal/documents/blobstore/store.go`
- Create: `services/core/internal/documents/blobstore/store_test.go`
- Create: `services/core/internal/documents/blobstore/crypto_port.go`
- Create: `services/core/internal/documents/blobstore/envelope.go`
- Create: `services/core/internal/documents/blobstore/envelope_test.go`
- Create: `services/core/internal/banking/intake_draft_repository.go`
- Create: `services/core/internal/banking/intake_draft_repository_test.go`
- Create: `services/core/internal/banking/intake_draft_read_port.go`
- Create: `services/core/internal/banking/intake_draft_read_port_test.go`

- [ ] Write failing repository tests for immutable approved bills/credits/snapshots, supplier-reference uniqueness plus explicit override, attachment links, evidence/job/candidate/review states, encrypted sanitized source display name, one target consumption, and draft-only Banking handoffs.
- [ ] Define normalized purchase/document tables and the minimal Banking-owned intake-draft tables. Do not let Documents write Banking tables directly.
- [ ] Write blob-store tests for random per-blob nonce, authenticated encryption, workspace-bound associated data, SHA-256/length/MIME metadata, ciphertext tamper, wrong workspace key, atomic rename/fsync, crash temp cleanup, and absence of original/derivative plaintext.
- [ ] Implement an AES-256-GCM envelope backed by the unlocked workspace key through a narrow `EvidenceCryptoPort`; paths are opaque IDs under the workspace evidence root, never caller-provided.
- [ ] Implement Banking-owned `IntakeDraftReadPort.GetTarget(tx, review_id, target_ref)` returning a bounded generated target status/hash/version projection. Documents uses this port to populate the target projection nested in public `GetDocumentReview`; Slice 3 adds no public Banking service and neither module reads the other's tables.
- [ ] Add migration tests for every prior schema prefix, rollback, integrity, and audit/journal/subledger checks.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/storage/... ./services/core/internal/purchases/... ./services/core/internal/documents/... ./services/core/internal/banking/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/storage services/core/internal/purchases services/core/internal/documents services/core/internal/banking && rtk git commit -m "feat: persist payables and encrypted evidence"`.

### Task 3: Implement bills, supplier credits, and corrections

**Files:**
- Create: `services/core/internal/purchases/bill.go`
- Create: `services/core/internal/purchases/bill_test.go`
- Create: `services/core/internal/purchases/service.go`
- Create: `services/core/internal/purchases/service_integration_test.go`
- Create: `services/core/internal/purchases/payable_port.go`
- Create: `services/core/internal/purchases/payable_port_test.go`
- Create: `services/core/internal/purchases/calculator_adapter.go`
- Create: `services/core/internal/purchases/calculator_adapter_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Reuse `platform/documentmath` through a Purchases adapter and run every §7.2 golden fixture: inclusive/exclusive, scale 0–6, fractional minor units, fixed/percentage discount, mixed tax, positive quantities, exact-half-away-from-zero including credits, explicit freight/surcharge/rounding lines, and byte-equal Go/TypeScript totals.
- [ ] Write tests for bill draft/update/cancel, immutable internal Tammy sequence, one-cent explicit rounding line, larger total mismatch, exact/near supplier-reference duplicate handling, evidence state, approve, linked/standalone supplier credit, over-credit rejection, correction/reversal, closed/final tax period, and allocation dependencies.
- [ ] Implement approval as one UoW that freezes supplier snapshot/reference/dates/lines/tax/evidence/attachments/totals, posts expense-or-asset/current-deferred-evidence GST/payables intent, and appends events/audit.
- [ ] Encode GST evidence timing exactly. Non-cash approval of a `$220` taxable bill with evidence posts `Dr Expense 200 / Dr Current GST 20 / Cr Payables 220`; without required evidence it posts `Dr Expense 200 / Dr Evidence-pending GST 20 / Cr Payables 220`. Cash-basis approval always posts `Dr Expense 200 / Dr Deferred GST 20 / Cr Payables 220`, regardless of evidence. A `$110` half-payment allocates `$10` from deferred to current when evidence exists, or from deferred to evidence-pending when it does not. `RecordTaxDocumentEvidence` later reclassifies `Dr Current GST 10 / Cr Evidence-pending GST 10` only in an eligible period with provenance; a closed eligible period returns `LATE_ATTRIBUTION_REQUIRES_RESOLUTION` without mutation.
- [ ] Implement immutable supplier credits and reverse-and-replace corrections. In the same UoW, split recognized/deferred amounts, use awareness date and liable-to-provide-consideration timing, enforce adjustment-note/exception rules, proportionally attribute partial consideration, and post exact pending/current/finalized tax facts and journals through `SettlementAdjustmentWritePort`/`AccountingPostingPort`.
- [ ] Assert for every credit/refund/reversal that original equals deferred + recognized + evidence/adjustment pending + finalized adjustment independently for gross/net/GST and that every GST control equals its tax-subledger state.
- [ ] Implement `PayableSourcePort.Allocatable` and explicit Get/List/OpenItem queries without exposing tables.
- [ ] In this Purchases-owned task, mark bill draft create/update/cancel, approval, credit, evidence reclassification, and correction on the shared financial change-set; prove one financial revision increment per successful UoW and none on read, rollback, or replay.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/purchases/... ./services/core/internal/accounting/... ./services/core/internal/app/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/purchases services/core/internal/app && rtk git commit -m "feat: post bills and supplier credits"`.

### Task 4: Add supplier payments, refunds, and aged payables

**Files:**
- Modify: `services/core/internal/settlements/service.go`
- Modify: `services/core/internal/settlements/service_integration_test.go`
- Create: `services/core/internal/settlements/payables_test.go`
- Create: `services/core/internal/reporting/payables.go`
- Create: `services/core/internal/reporting/payables_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Write symmetric supplier payment/refund tests for split allocations, credits, unallocated payment, overpayment, cross-contact/currency denial, reversed allocations/payment, and exact payables open-item equation.
- [ ] Add cash-basis mixed-line tests covering deferred/current/evidence-pending GST, consideration date, missing evidence, later evidence, adjustment note, partial refund, late closed-period resolution, and invariant totals.
- [ ] Implement outgoing settlements through `PayableSourcePort`, `AccountingPostingPort`, `TaxReportImpactPort`, and the same immutable allocation engine; do not duplicate the receivables arithmetic.
- [ ] Emit immutable CashFlowFact components from every supplier payment/refund cash posting, assert each cash line sum, and mark payment/refund/allocation/reversal changes on the shared financial change-set exactly once per UoW.
- [ ] Implement aged payables through revision-pinned `FinancialReportReadPort`; retain organisation timezone/as-of date and due-date default or explicit issue-date basis, put a balance into the older bucket only after it exceeds the upper bound, show credits/overpayments separately, and cross-check the payables control account.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/settlements/... ./services/core/internal/reporting/... ./services/core/internal/purchases/... ./services/core/internal/app/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/settlements services/core/internal/reporting services/core/internal/app && rtk git commit -m "feat: add payable settlements and ageing"`.

## Chunk 2: Sandboxed extraction and reviewed handoff

### Task 5: Pin and build the native extraction dependency chain

**Files:**
- Create: `Cargo.toml`
- Create: `Cargo.lock`
- Create: `helpers/document-extractor/Cargo.toml`
- Create: `helpers/document-extractor/build.rs`
- Create: `helpers/document-extractor/src/lib.rs`
- Create: `helpers/document-extractor/src/main.rs`
- Create: `helpers/document-extractor/tests/dependency_smoke.rs`
- Create: `third_party/pdf-inspector/REVISION`
- Create: `third_party/pdf-inspector/LICENSE`
- Create: `third_party/pdfium/REVISION`
- Create: `third_party/pdfium/SHA256SUMS`
- Create: `third_party/pdfium/LICENSE`
- Create: `third_party/pdfium/DEPS.lock.json`
- Create: `third_party/pdfium/CIPD.lock.json`
- Create: `third_party/pdfium/BUILD_ARGS.gn`
- Create: `third_party/depot_tools/REVISION`
- Create: `third_party/depot_tools/LICENSE`
- Create: `third_party/tesseract/VERSION`
- Create: `third_party/tesseract/SHA256SUMS`
- Create: `third_party/tesseract/LICENSE`
- Create: `third_party/leptonica/VERSION`
- Create: `third_party/leptonica/SHA256SUMS`
- Create: `third_party/leptonica/LICENSE`
- Create: `third_party/tessdata/REVISION`
- Create: `third_party/tessdata/LICENSE`
- Create: `third_party/tessdata/eng.traineddata.sha256`
- Create: `third_party/tessdata/osd.traineddata.sha256`
- Create: `third_party/fonts/REVISION`
- Create: `third_party/fonts/NotoSans-Regular.ttf`
- Create: `third_party/fonts/NotoSans-Regular.ttf.sha256`
- Create: `third_party/fonts/LICENSE`
- Create: `scripts/vendor-document-deps.mjs`
- Create: `scripts/vendor-document-deps.test.mjs`
- Create: `scripts/build-document-helper.mjs`
- Create: `scripts/build-document-helper.test.mjs`
- Modify: `mise.toml`
- Modify: `package.json`
- Modify: `compliance/build/toolchain.lock.json`
- Modify: `.github/workflows/foundation-ci.yml`
- Modify: `.github/workflows/foundation-windows11-e2e.yml`

- [ ] Add failing pin/checksum tests for pdf-inspector revision `a15ec2d68d51dbe6a39d1da688ec7a3f642d846c`, PDFium revision `f4facdd2652f771eb11d605a82fea2bbfbe66d9f`, Tesseract 5.5.2 commit `6e1d56a847e697de07b38619356550e5cf4e8633`, Leptonica 1.87.0 commit `13275a278eb55b5746e33f95fbf5a2c8f604b3ab`, tessdata commit `ced78752cc61322fb554c280d13360b35b8684e4`, Noto Latin/Greek/Cyrillic revision `cb097900c74b26e6dcab899b4f07b2bc79dd80c4` and exact `NotoSans-Regular.ttf`, source archives, English/orientation traineddata, Rust 1.97.1, licenses, SBOM inputs, and per-target artefact hashes.
- [ ] Pin depot_tools commit `e154c8eda5e63cbe85a765ae9d06e2b7af05139e`, PDFium's complete DEPS closure, every resolved CIPD instance ID (including GN/Clang/sysroots/codecs), Ninja `v1.13.1` commit `79feac0f3e3bc9da9effc586cd5fea41e7550051`, macOS SDK/Xcode build, and Windows SDK/MSVC build in the exact lock files. Vendor all source/data archives into the offline build cache only after checksum verification; a second build with networking disabled must resolve nothing ambient.
- [ ] Create a Rust workspace and generate helper Protobuf types from repository schemas at build time; do not maintain Rust-only duplicate DTOs.
- [ ] Write a smoke test that exercises pdf-inspector through a narrow adapter for page count, native/mixed/scanned classification, positioned spans, reading order, and table cells. If the pinned public API cannot meet the approved contract, stop for a reviewed design amendment rather than silently replacing it.
- [ ] Build PDFium from the pinned official source for macOS arm64 and Windows x64 with JavaScript/XFA/network-facing optional components disabled; package only the reviewed runtime library beside the helper with exact ABI/hash/licence metadata. Build Tesseract/Leptonica as pinned static libraries and prove no `tesseract` child executable is invoked or packaged.
- [ ] Use and freeze Task 1's generated `DocumentRenderPolicy` in the native build/runtime tests; package the checksum-pinned Noto Sans fallback and forbid ambient/system-font lookup.
- [ ] Add an in-memory feasibility test that opens native, scanned, mixed, rotated, and password-encrypted PDFs through `FPDF_LoadMemDocument64`, enforces the policy before allocation, rasterizes deterministic RGBA, and feeds Tesseract without a plaintext page file. Require byte-identical render/evidence hashes across two runs of each target/toolchain. Run macOS locally plus a dedicated Windows 11 `pdfium-document-helper-preflight` job now, before Task 6; stop for design review if either target cannot satisfy it.
- [ ] Generate license/SBOM metadata and run repository vulnerability scanning with reviewed exceptions only.
- [ ] Run `rtk pnpm exec node --test scripts/vendor-document-deps.test.mjs scripts/build-document-helper.test.mjs` and `rtk cargo test -p tammy-document-extractor --test dependency_smoke --locked`; retain the local target hashes/toolchain lock.
- [ ] Commit the reviewed Task 5 source/workflow first: `rtk git add Cargo.toml Cargo.lock helpers third_party scripts mise.toml package.json compliance/build .github/workflows && rtk git commit -m "build: pin local document extraction stack"`.
- [ ] Trigger the Windows 11 `pdfium-document-helper-preflight` job at that exact commit SHA and retain its target/toolchain hashes. Task 6 remains blocked until it passes; on failure, make a new reviewed fix commit, rerun the local gates, and trigger Windows again at the new SHA.

### Task 6: Implement the bounded helper protocol and extraction pipeline

**Files:**
- Create: `helpers/document-extractor/src/protocol.rs`
- Create: `helpers/document-extractor/src/limits.rs`
- Create: `helpers/document-extractor/src/classify.rs`
- Create: `helpers/document-extractor/src/native_text.rs`
- Create: `helpers/document-extractor/src/render.rs`
- Create: `helpers/document-extractor/src/pdfium.rs`
- Create: `helpers/document-extractor/tests/pdfium.rs`
- Create: `helpers/document-extractor/src/ocr.rs`
- Create: `helpers/document-extractor/src/layout.rs`
- Create: `helpers/document-extractor/src/infer.rs`
- Create: `helpers/document-extractor/src/sandbox.rs`
- Create: `helpers/document-extractor/src/sandbox_darwin.rs`
- Create: `helpers/document-extractor/src/sandbox_windows.rs`
- Create: `helpers/document-extractor/tests/protocol.rs`
- Create: `helpers/document-extractor/tests/corpus.rs`
- Create: `test/fixtures/documents/corpus/README.md`
- Create: `test/fixtures/documents/corpus/manifest.json`
- Create: `scripts/generate-document-fixtures.mjs`

- [ ] Generate synthetic/redistributable fixtures for native, scanned, mixed, rotated, table, PNG, JPEG, encrypted, wrong-password, corrupt, polyglot, embedded-file, high-compression, oversize-page/count/pixels, and hostile protocol cases; retain license/source/hash metadata.
- [ ] Write protocol tests for four-byte big-endian length-delimited frames with `BeginDocument`, ordered ≤1 MiB `DocumentChunk` frames, `EndDocument(total_length, sha256)`, 16 MiB per-frame and 50 MiB total input, 128 MiB total output, sequence replay/gap/truncation/hash mismatch, version mismatch, ordered progress, stdout purity, stderr redaction, and deterministic results.
- [ ] Implement magic-byte/MIME validation; 50 MiB input, 200 pages, 500 rendered megapixels, 1 GiB memory, ten-minute wall limits; no recursive/embedded extraction; and output only candidate/evidence Protobuf.
- [ ] Route native regions through pdf-inspector positioned text. Rasterize scanned pages and image-only regions in memory through the pinned PDFium C API, apply PDF rotation before the pixel budget, and send only bounded pixels to statically linked Tesseract TSV/hOCR; retain source page/word coordinates, transform, confidence, and all engine versions.
- [ ] Implement deterministic layout/field candidates for every bill/receipt/statement field in Design §10.3; keep contact/account/tax/duplicate suggestions separate from extracted evidence.
- [ ] Ensure PDF passwords exist only in mutable input memory for the current attempt and are cleared where supported; never print or persist them.
- [ ] Run `rtk cargo test -p tammy-document-extractor --locked`, then run corpus tests twice and confirm byte-identical results for the same engine/toolchain.
- [ ] Commit: `rtk git add helpers test/fixtures/documents scripts/generate-document-fixtures.mjs && rtk git commit -m "feat: extract local document candidates"`.

### Task 7: Prove macOS and Windows helper containment before integration

**Files:**
- Create: `helpers/document-extractor/entitlements.plist`
- Create: `scripts/package-document-helper.mjs`
- Create: `scripts/check-document-helper-sandbox.mjs`
- Create: `scripts/check-document-helper-sandbox.test.mjs`
- Create: `apps/desktop/tests/e2e/document-helper-sandbox.spec.ts`
- Create: `services/core/internal/documents/launcher/launcher.go`
- Create: `services/core/internal/documents/launcher/launcher_darwin.go`
- Create: `services/core/internal/documents/launcher/launcher_windows.go`
- Create: `services/core/internal/documents/launcher/launcher_test.go`
- Create: `services/core/internal/documents/launcher/launcher_darwin_test.go`
- Create: `services/core/internal/documents/launcher/launcher_windows_test.go`
- Create: `services/core/internal/documents/launcher/readiness.go`
- Create: `services/core/internal/documents/launcher/readiness_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`
- Modify: `services/core/cmd/tammy-core/main.go`
- Modify: `apps/desktop/forge.config.ts`
- Modify: `proto/tammy/v1/documents.proto`
- Modify: `helpers/document-extractor/src/main.rs`
- Modify: `helpers/document-extractor/src/protocol.rs`
- Modify: `helpers/document-extractor/src/sandbox.rs`
- Modify: `helpers/document-extractor/src/sandbox_darwin.rs`
- Modify: `helpers/document-extractor/src/sandbox_windows.rs`
- Modify: `helpers/document-extractor/tests/protocol.rs`
- Modify: `scripts/build-manifest-schema.mjs`
- Create: `scripts/build-manifest-schema.test.mjs`
- Modify: `scripts/write-build-manifest.mjs`
- Modify: `scripts/write-build-manifest.test.mjs`
- Modify: `apps/desktop/scripts/find-packaged-app.mjs`
- Modify: `apps/desktop/scripts/find-packaged-app.test.mjs`
- Modify: `apps/desktop/tests/e2e/package-signature.test.mjs`
- Modify: `compliance/build/toolchain.lock.json`
- Modify: `.github/workflows/foundation-ci.yml`
- Modify: `.github/workflows/foundation-windows11-e2e.yml`

- [ ] Add a private helper-protocol `VerifyContainment` health operation used by the production launcher before enabling intake. It carries only protocol/job-capability versions and returns booleans/typed failures after bounded attempts to open outbound/loopback sockets, read an outside sentinel, and spawn a process; it has no public Connect RPC, file path, generic command, or unrestricted diagnostic payload. Resource/output/parent-death probes remain external launcher tests. Prove the health-checked executable SHA-256 equals the helper copied into the packaged-app build manifest.
- [ ] Implement the production launcher interface with Darwin and Windows adapters. Darwin creates the opaque random `0700` job capability, checks the separately signed App-Sandbox/no-network entitlement, passes only inherited stdio/job handle, and enforces parent/resource termination. Windows creates the AppContainer/restricted token/no-network capability and one-process Job Object, closes unrelated handles, and sets memory/CPU/lifetime/kill-on-close limits.
- [ ] Pass exactly two filesystem capabilities: a random read/write job directory and a read-only hash-verified model/runtime root containing PDFium, `eng.traineddata`, `osd.traineddata`, and pinned fallback fonts. The helper verifies every expected hash before engine initialization; sandbox tests prove these resources load while every other app/workspace/user path is denied.
- [ ] Wire `launcher.ReadinessPreflight` into the production composition root/core startup in Task 7. It invokes the private `VerifyContainment` health operation through the real platform launcher before Documents can be enabled; packaged tests observe the production startup result/log and compare the launched helper/resource hashes to the app manifest. No external spawn or duplicate sandbox implementation may satisfy this gate.
- [ ] Make the Rust helper validate its sandbox marker/job capability and apply its own supported resource/no-child-process defenses before reading document bytes.
- [ ] Confirm cancellation/crash/timeout deletes all decrypted inputs, rendered pages, OCR output, and temporary files while retaining only the core-encrypted original and typed job state.
- [ ] Extend the established manifest writer/schema, packaged-app verifier, Forge copy/sign ordering, and signature tests with helper/PDFium/Tesseract/tessdata resource paths and hashes. Make packaging fail on absent sandbox evidence, unsigned/wrong-hash helper, wrong PDFium ABI/hash, missing macOS nested-code signature/entitlements, failed Windows Authenticode verification, dynamic Tesseract child dependency, unexpected DLL/dylib, or SBOM/vulnerability/licence manifest gap.
- [ ] First run `rtk go test ./services/core/internal/documents/launcher -run '^TestReadinessPreflight/production_launcher$'`, observe the missing production startup binding, wire the real launcher, and rerun it to `PASS`; also keep `TestLauncher/denies_outbound_socket` as the narrow platform denial proof.
- [ ] Run `rtk pnpm contracts && rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep "document helper sandbox"` locally. The Playwright case observes the core startup readiness result from the production launcher and never spawns the helper directly.
- [ ] Stop Slice 3 if either platform fails; do not add an unsandboxed fallback.
- [ ] Commit the reviewed Task 7 source/workflow: `rtk git add proto services/core/internal/gen packages/connect-client/src/gen helpers scripts apps/desktop services/core/internal/documents/launcher services/core/internal/app services/core/cmd/tammy-core compliance/build/toolchain.lock.json .github/workflows && rtk git commit -m "security: sandbox packaged document extraction"`.
- [ ] Trigger the corresponding Windows 11 packaged containment gate at that exact commit SHA. Task 8 remains blocked until it passes; on failure, make a new reviewed fix commit, rerun the local package/containment gate, and trigger Windows again at the new SHA.

### Task 8: Orchestrate encrypted ingestion and restart-safe jobs

**Files:**
- Create: `services/core/internal/documents/helper_client.go`
- Create: `services/core/internal/documents/helper_client_test.go`
- Create: `services/core/internal/documents/ingest.go`
- Create: `services/core/internal/documents/ingest_test.go`
- Create: `services/core/internal/documents/extraction_job.go`
- Create: `services/core/internal/documents/extraction_job_test.go`
- Create: `services/core/internal/documents/evidence_read.go`
- Create: `services/core/internal/documents/evidence_read_test.go`
- Create: `services/core/internal/documents/service.go`
- Create: `services/core/internal/documents/service_integration_test.go`
- Create: `services/core/internal/transport/document_upload.go`
- Create: `services/core/internal/transport/document_upload_test.go`
- Modify: `services/core/internal/transport/server.go`
- Modify: `services/core/internal/transport/server_integration_test.go`
- Modify: `services/core/internal/transport/registrar.go`
- Modify: `services/core/internal/transport/registrar_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/cmd/tammy-core/main.go`

- [ ] Write transport tests proving the existing 1 MiB unary/global limit remains unchanged while only `IngestDocument` accepts ordered ≤512 KiB Connect client-stream chunks up to 50 MiB. Reject missing/duplicate/out-of-order chunks, length/hash mismatch, early disconnect, and oversize before evidence commit.
- [ ] Write integration tests that stream/encrypt the original before parsing, pass bytes rather than source paths, create random `0700` jobs, enforce all helper limits, validate complete output before atomic candidate insert, and scrub temp files on every outcome.
- [ ] Sanitize the Electron-main-provided display label again at the core boundary, persist it only in the SQLCipher-protected evidence metadata and encrypted backup, and expose only that label through authorized document/review projections. Tests prove the absolute/parent path and security-scoped handle never reach renderer, helper, logs, audit, public Protobuf, or plaintext files.
- [ ] Write persisted job tests for operation key, semantic input hash, attempt, stage, checkpoint hash, cancellation flag, optional committed-result ref, queued/running/waiting/completed/retryable/terminal/cancelled transitions, startup running→queued attempt increment, same-job retry without replacing a prior candidate, three automatic process failures, no automatic retry for password/user/validation failures, cancellation-versus-commit election, parent death, and committed-result reconstruction.
- [ ] Write encrypted-PDF tests: `WAITING_FOR_PASSWORD`, each call is a new non-idempotent/non-auto-retried attempt, secret excluded from audit/idempotency/logs, three failures terminal, and explicit new extraction required.
- [ ] Implement `IngestDocument`, `ProvidePdfPassword`, `CancelExtraction`, `RetryExtraction`, and Get/List queries through generated handlers and persisted jobs; Task 7's production readiness preflight remains the launcher used here rather than a second construction path.
- [ ] Encrypt accepted page renditions immediately after helper output validation and before candidate commit. Implement server-streaming `GetEvidencePage` to authorize, authenticate/decrypt one ≤16 MiB policy-bound preview into memory, emit ordered ≤512 KiB generated chunks with total length/SHA-256/MIME/policy ID, and clear buffers; Electron main forwards chunks over one named MessagePort. It never creates a plaintext cache, oversized unary response, or caller path.
- [ ] Log only job IDs, hashes, engine versions, counts, durations, and typed failures; add a capture test searching for source content, contacts, paths, and passwords.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/documents/... ./services/core/internal/app/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/documents services/core/internal/transport services/core/internal/app services/core/cmd/tammy-core && rtk git commit -m "feat: orchestrate encrypted document extraction"`.

### Task 9: Implement review decisions and one-time draft handoff

**Files:**
- Create: `services/core/internal/documents/review.go`
- Create: `services/core/internal/documents/review_test.go`
- Create: `services/core/internal/documents/reviewed_port.go`
- Create: `services/core/internal/documents/reviewed_port_test.go`
- Create: `services/core/internal/documents/target_service.go`
- Create: `services/core/internal/documents/target_service_integration_test.go`
- Create: `services/core/internal/banking/intake_draft_port.go`
- Create: `services/core/internal/banking/intake_draft_port_test.go`
- Modify: `services/core/internal/purchases/service.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Write tests recording every accepted/edited/rejected candidate and evidence box; unresolved arithmetic/GST/missing evidence blocks sealing.
- [ ] Implement immutable one-kind reviews and `ReviewedDocumentPort.Accepted`; consuming a review is elected once by `(review_id, target_kind, operation_key)` in the caller's UoW.
- [ ] Implement bill handoff through Purchases with validated supplier/lines/tax/evidence; paid-receipt handoff through `BankingIntakeDraftPort` with financial account/contact/tax lines; statement handoff with selected account, explicit sign mapping, the exact shared `StagedStatementLine`, and duplicate diagnostics; and attach-only evidence. No handoff posts, approves, creates a payment, or commits statement lines.
- [ ] Require explicit `SupersedeDocumentReview` to change kind; prohibit after prior target posting and retain candidate/user/target/final references.
- [ ] Prove a business-target failure rolls back consumption and audit; identical replay returns the same draft, changed target conflicts.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/documents/... ./services/core/internal/purchases/... ./services/core/internal/banking/... ./services/core/internal/app/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/documents services/core/internal/purchases services/core/internal/banking services/core/internal/app && rtk git commit -m "feat: review and hand off document drafts"`.

## Chunk 3: UI and packaged acceptance

### Task 10: Extend backup and restore to encrypted document evidence

**Files:**
- Modify: `services/core/internal/backup/format.go`
- Modify: `services/core/internal/backup/format_test.go`
- Modify: `services/core/internal/backup/service_integration_test.go`
- Modify: `services/core/internal/restore/service.go`
- Modify: `services/core/internal/restore/service_integration_test.go`
- Create: `services/core/internal/documents/backup_projection.go`
- Create: `services/core/internal/documents/backup_projection_test.go`
- Modify: `services/core/internal/backup/provider_registry.go`
- Modify: `services/core/internal/backup/provider_registry_test.go`
- Modify: `services/core/internal/restore/provider_registry.go`
- Modify: `services/core/internal/restore/provider_registry_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/app/composition_test.go`

- [ ] Add a red backup test with native/scanned originals, encrypted page renditions, candidate/review decisions, engine metadata, bill/receipt/statement/attach-only target links, and orphan temp files. Require only referenced encrypted objects in the signed manifest and no plaintext/temp entries.
- [ ] Implement a Documents-owned immutable backup projection consumed by Backup; Backup never queries document tables or reads plaintext content.
- [ ] Register the Documents provider/validator in the production Backup/Restore registries and composition root.
- [ ] Add restore tests for missing/extra/tampered ciphertext, wrong object hash/key/target link, and complete fresh-install recovery. Any failure leaves the active workspace byte-for-byte unchanged.
- [ ] Back up and restore the encrypted sanitized source display name. After restore, use public `GetEvidencePage`, `GetDocumentReview` (including its Banking-owned target projection through `IntakeDraftReadPort`), and bill queries to prove original hashes, derivatives, decisions, target links, and display label match; helper versions need not rerun extraction and no public Banking RPC is invented.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/backup/... ./services/core/internal/restore/... ./services/core/internal/documents/... ./services/core/internal/app/... -race -count=1`; expect production registry plus evidence restore/tamper cases pass.
- [ ] Commit: `rtk git add services/core/internal/backup services/core/internal/restore services/core/internal/documents services/core/internal/app && rtk git commit -m "feat: preserve document evidence in backup restore"`.

### Task 11: Build payables and side-by-side review workflows

**Files:**
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/main/rpc-router.ts`
- Modify: `apps/desktop/src/main/rpc-router.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Create: `apps/desktop/src/main/file-intake.ts`
- Create: `apps/desktop/src/main/file-intake.test.ts`
- Create: `apps/desktop/src/renderer/features/purchases/bills-screen.tsx`
- Create: `apps/desktop/src/renderer/features/purchases/payments-screen.tsx`
- Create: `apps/desktop/src/renderer/features/purchases/aged-payables-screen.tsx`
- Create: `apps/desktop/src/renderer/features/documents/document-inbox-screen.tsx`
- Create: `apps/desktop/src/renderer/features/documents/document-review-screen.tsx`
- Create: `apps/desktop/src/renderer/features/documents/document-inbox-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/documents/document-review-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/documents/document-accessibility.test.tsx`
- Create: `apps/desktop/tests/e2e/payables.spec.ts`
- Create: `apps/desktop/tests/e2e/document-intake.spec.ts`
- Create: `apps/desktop/tests/e2e/cash-gst-payables.spec.ts`
- Modify: `apps/desktop/src/renderer/app-shell/navigation.tsx`

- [ ] Write main/preload tests proving the OS picker returns an opaque approved handle plus sanitized basename/display label, main validates/reads bounded bytes, renderer never receives an unrestricted/parent path or raw file API, and each RPC remains named/schema-bound. Show the retained label in inbox/review UI and assert path/control canaries are absent from helper, logs, audit, and evidence exports.
- [ ] Write React tests for drag/pick ingestion, progress/restart, password attempts, cancel/retry, page navigation, highlighted coordinates, confidence, field decisions, arithmetic/tax resolution, target selection, supersession, and separate draft approval.
- [ ] Add `E2E-05 native PDF bill`, run `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep 'E2E-05 native PDF bill'`, and require the missing Bills control; implement only the Bills/Money Out/Aged Payables codec/UI path and rerun to pass.
- [ ] Add `E2E-06 scanned review to draft`, run `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep 'E2E-06 scanned review to draft'`, and require the missing Inbox/Review control; implement only the side-by-side review, chunked evidence MessagePort, and draft-handoff UI and rerun to pass.
- [ ] Add `E2E-07 cash GST supplier half-payment`, run `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep 'E2E-07 cash GST supplier half-payment'`, and require the missing allocation/evidence resolution control; implement only that workflow and rerun to pass. Then complete remaining accessible navigation/React cases. Task 12 only expands exhaustive matrices/evidence.
- [ ] Make “save review”, “create draft”, “approve bill”, and future Banking confirmation visibly separate actions with server state refreshed after each.
- [ ] Render page bytes through an ephemeral renderer `Blob` URL, verify the response hash, and revoke the URL on page change/unmount. Tests assert no local path, long-lived byte cache, or plaintext derivative remains.
- [ ] Run `rtk pnpm --filter @tammy/desktop test && rtk pnpm typecheck && rtk pnpm lint`.
- [ ] Commit: `rtk git add apps/desktop && rtk git commit -m "feat: add payables and document review workflows"`.

### Task 12: Accept Slice 3 in packaged macOS and Windows apps

**Files:**
- Modify: `apps/desktop/tests/e2e/payables.spec.ts`
- Modify: `apps/desktop/tests/e2e/document-intake.spec.ts`
- Modify: `apps/desktop/tests/e2e/cash-gst-payables.spec.ts`
- Modify: `apps/desktop/tests/e2e/support/runtime-coverage.ts`
- Modify: `apps/desktop/tests/e2e/support/runtime-coverage.test.ts`
- Create: `compliance/evidence/core-accounting/slice-3-runtime-coverage.json`
- Modify: `compliance/traceability/core-accounting.csv`
- Modify: `compliance/evidence/core-accounting/manifest.json`
- Modify: `scripts/check-core-accounting-evidence.mjs`
- Modify: `scripts/check-core-accounting-evidence.test.mjs`
- Modify: `.github/workflows/foundation-windows11-e2e.yml`

- [ ] Expand packaged `E2E-05`: approve a GST-inclusive native-PDF `$220.00` bill. Require the exact non-cash/cash approval journals and evidence state from Task 3; for cash basis prove `$110` partial-payment reclassifies `$10` deferred→current with evidence or deferred→evidence-pending without it, later evidence moves `$10` pending→current, and closed-period evidence returns the typed resolution failure. After full payment require Payables/deferred/pending zero, Bank credit `$220`, aged payable zero, cash-flow `-$220`, linked evidence/source/tax/audit refs, then supplier credit/correction paths.
- [ ] Implement packaged `E2E-06`: scanned/mixed/rotated/encrypted/corrupt/oversize inputs, OCR fallback, password attempts, review edits, bill/receipt/statement/attach-only handoffs, cancellation/restart, and no automatic posting.
- [ ] Complete payables `E2E-07`/`E2E-08`: split payments, credits/refunds/reversals, cash-GST part payments, deferred/current/pending controls, late allocation, evidence/adjustment notes, partial refund, and BAS-ready facts.
- [ ] For every public RPC, execute the coverage-driven four-role outcome, allowed/invalid transition, stale version, exact replay/changed conflict, principal failure, and empty/populated/filter/page query cases; require zero skips. Extend the generated-client-boundary runtime tracer and write canonical `{caseId, fullyQualifiedRpc, actorRole, outcomeCode}` tuples to `slice-3-runtime-coverage.json`; its checker expands `coverage.yaml` and rejects missing, extra, duplicate, skipped, or scenario-only claims.
- [ ] Keep preliminary runtime tuples/results in the run's untracked temporary evidence directory; materialize `slice-3-runtime-coverage.json` and the tracked manifest only after the reviewed clean-source commit and descriptor evidence generation.
- [ ] Quit/relaunch, back up the Slice 3 workspace, restore into a fresh installation context, and compare bill/payment/ledger/GST/ageing/audit projections plus encrypted original/page/candidate/review/target hashes.
- [ ] Run `rtk pnpm contracts && rtk pnpm lint && rtk pnpm typecheck && rtk pnpm test && rtk cargo test --workspace --locked`.
- [ ] Run macOS arm64 `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep "E2E-05|E2E-06|E2E-07|E2E-08"`; require real core/helper/encrypted stores, signatures, offline guard, and zero skips.
- [ ] Verify signed helper identity/hash, sandbox probes, no network, no plaintext residues, no child process, resource termination, and clean parent-exit behavior in retained evidence.
- [ ] Run `rtk pnpm exec node --test scripts/check-core-accounting-evidence.test.mjs`; retain preliminary artefacts outside tracked evidence until the clean source commit exists.
- [ ] Request independent review, resolve critical/important findings, rerun affected gates, and commit reviewed source without retained descriptor/result evidence: `rtk git add apps/desktop test compliance/traceability scripts .github/workflows/foundation-windows11-e2e.yml && rtk git commit -m "test: accept payables and document intake slice"`.
- [ ] From that clean source commit run `TAMMY_SOURCE_REVISION=$(rtk git rev-parse HEAD) rtk pnpm proto:descriptors:evidence`; verify the manifest subject, rerun the exact macOS packaged/helper gate, then run `.github/workflows/foundation-windows11-e2e.yml` job `windows11-23h2-x64-packaged-e2e` at that same committed revision and require the identical matrix plus AppContainer/Job Object evidence. Assert no generated core/preload method remains on `FEATURE_NOT_READY`, write the Slice 3 evidence manifest, and run `rtk pnpm core-accounting:evidence -- --slice 3` expecting `slice 3 evidence verified`.
- [ ] Commit only retained evidence: `rtk git add compliance/contracts compliance/evidence/core-accounting && rtk git commit -m "build: retain slice 3 accounting evidence"`.

## Slice 3 exit gate

- [ ] Purchases/payments/open items/control accounts/tax facts/aged payables cross-reconcile under cash and non-cash GST.
- [ ] Native and scanned PDFs reach reviewable drafts, and extraction/review can never approve or post.
- [ ] Every supported/hostile document path and every new descriptor-discovered RPC/transition/role/failure is covered.
- [ ] Both supported packaged targets prove sandbox containment, signed/pinned helper resources, encrypted evidence, and zero plaintext residue.
- [ ] E2E-05, E2E-06, and Slice 3 portions of E2E-07/E2E-08 pass against the real signed packaged apps.
