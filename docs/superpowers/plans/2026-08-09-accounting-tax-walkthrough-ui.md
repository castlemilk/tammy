# Accounting and Tax Walkthrough UI Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the diagnostics-only Electron landing page with the approved, real offline accounting walkthrough and four wireframe-aligned primary screens.

**Architecture:** This plan is the UI/integration overlay for the already-reviewed accounting programme plans. Domain work remains in the Go modules and ordered slice plans; this overlay fixes navigation, route composition, preload boundaries, deterministic walkthrough fixtures, cross-screen provenance, and the final packaged manual-validation experience. Generated Protobuf remains the only business boundary and no renderer fixture or placeholder may substitute for an unfinished core service.

**Tech Stack:** Buf/Protobuf, Connect-Go/Connect-ES, Go/SQLCipher, Electron 43, React 19, TypeScript 7, Tailwind 4, Vitest/Testing Library, Playwright, Rust document helper, pdf-inspector, bundled local OCR.

---

**Normative design:** `docs/superpowers/specs/2026-08-09-accounting-tax-walkthrough-ui-design.md`  
**Parent programme:** `docs/superpowers/plans/2026-08-02-core-accounting-programme.md`  
**Subsystem plans:**

- `docs/superpowers/plans/2026-08-02-ledger-gst-kernel.md`
- `docs/superpowers/plans/2026-08-02-contacts-receivables.md`
- `docs/superpowers/plans/2026-08-02-payables-document-intake.md`
- `docs/superpowers/plans/2026-08-02-banking-reconciliation.md`
- `docs/superpowers/plans/2026-08-02-reports-bas.md`
- `docs/superpowers/plans/2026-08-02-accounting-release-hardening.md`

**Execution boundary:** Tasks 1–6 below do not duplicate domain implementation. Execute the owning subsystem task first, then apply its route/view integration here. Never add static accounting state to the renderer to make a route look complete.

## Chunk 1: Wireframe-aligned application and walkthrough integration

### Task 1: Lock the walkthrough route and projection contracts

**Files:**
- Create: `proto/tammy/v1/overview.proto`
- Modify: `proto/tammy/v1/fixtures.proto`
- Modify: `proto/tammy/v1/events.proto`
- Create: `services/core/internal/contracts/overview_proto_test.go`
- Create: `test/fixtures/walkthrough/noncash-supplier-month.pb.json`
- Modify: `test/e2e/coverage.yaml`
- Modify: `packages/connect-client/package.json`

- [ ] **Step 1: Write the failing descriptor test.** Require `OverviewService.GetAttentionSummary`, bounded attention counts/items, typed resource refs, exact financial/module revisions, civil as-of date, reporting period, and the four accounting-read role outcomes from Design §6.1.
- [ ] **Step 2: Run the focused RED.** Run `rtk go test ./services/core/internal/contracts -run '^TestOverviewDescriptorsExposeBoundedAttentionSummary$' -count=1`; expect failure because `overview.proto` and its descriptor do not exist.
- [ ] **Step 3: Add the minimal Protobuf contract.** Define no mutation and no renderer routes in the response. Add Buf validation: UUIDv7 organisation, required dates, maximum eight attention items, explicit enum kinds, required `SourceRef`, and bounded labels.
- [ ] **Step 4: Encode the exact deterministic oracle.** Add the approved `$1,000.00` opening, `$319.00` bill, `$29.00` GST, `$319.00` payment, `$681.00` bank, `$1,000.00` trial-balance totals, and `1B $29.00` non-cash BAS facts as canonical Protobuf JSON. Do not include current-time values.
- [ ] **Step 5: Add coverage before production exposure.** Declare the new query as `declared_future`; its coverage row names planned preload `getAttentionSummary`, exact roles/list/projections/failures, and future walkthrough cases. Do not add that method to production `preload-methods.json` while the row is future. Overview has no lifecycle transition fixture.
- [ ] **Step 6: Generate and verify before commit.** Run `rtk mise exec -- pnpm proto:format`, `rtk mise exec -- pnpm proto:lint`, two `rtk mise exec -- pnpm proto:generate` passes with byte-identical generated output, the focused Go contract test, `rtk mise exec -- pnpm proto:descriptors:check`, `rtk mise exec -- pnpm transitions:check`, and `rtk mise exec -- pnpm e2e:coverage`; every command must exit zero. Do not run the umbrella generated-tree cleanliness check while the intended generated delta is uncommitted.
- [ ] **Step 7: Commit and run the clean contract gate.** Run `rtk git add proto services/core/internal/contracts services/core/internal/gen packages/connect-client test && rtk git commit -m "feat: define walkthrough overview contracts"`, then `rtk mise exec -- pnpm contracts`; require exit zero on the committed tree.

### Task 2: Finish the real Slice 1 core before presenting accounting routes

**Files:**
- Follow exactly: `docs/superpowers/plans/2026-08-02-ledger-gst-kernel.md` Tasks 7–12
- Create: `services/core/internal/overview/service.go`
- Create: `services/core/internal/overview/service_test.go`
- Create: `services/core/internal/overview/service_integration_test.go`
- Modify: `services/core/internal/app/composition.go`
- Modify: `services/core/internal/transport/registrar.go`

- [ ] **Step 1: Complete encrypted backup/restore.** Execute ledger plan Task 7 through its focused, tagged race, restart, tamper, and crash gates; commit the reviewed result.
- [ ] **Step 2: Complete production composition.** Execute ledger plan Task 8 and prove its existing System, Workspace, Identity, Organisation, Accounting, and Audit handlers boot through one real production composition while undeclared routes return not found. Overview is added only after its focused RED/GREEN below.
- [ ] **Step 3: Complete organisation/accounts.** Execute ledger plan Task 9 with the versioned Australian chart and non-cash GST rule bundle.
- [ ] **Step 4: Complete posting/projections.** Execute ledger plan Task 10 with immutable balanced journals, GST facts, cash-flow components, ledger, trial-balance, and invariant checks.
- [ ] **Step 5: Complete opening/period controls.** Execute ledger plan Task 11 so blank onboarding creates the primary financial account and verified opening conversion.
- [ ] **Step 6: Complete Slice 1 production IPC and UI with one reviewed gate deferral.** Execute every Ledger Task 12 implementation/test/commit requirement for Workspace, Identity, ownership/security, backup/restore, organisation, period, opening-balance, Accounting, Audit, generated-client, route, preload, and production composition. Run ordinary `pnpm contracts` green. Its final `contracts:production` invocation is deferred only because this overlay's already-declared `OverviewService.GetAttentionSummary` remains the single planned future row; no other future row is allowed. Task 3 promotes/binds Overview and immediately runs the deferred production gate. Do not substitute the focused shell overlay for any Task 12 production path.
- [ ] **Step 7: Start Overview with a genuine RED.** Add an integration test creating state only through public commands; run `rtk go test -tags tammy_sqlcipher ./services/core/internal/overview -run '^TestAttentionSummaryUsesOneVerifiedSnapshot$' -count=1` and expect the missing service/port failure.
- [ ] **Step 8: Implement the read-only composition.** Read bounded module ports in one snapshot, pin revisions, return at most eight stable attention refs, and own no business tables. Rerun the identical focused test to PASS.
- [ ] **Step 9: Register Overview after implementation.** Extend composition/registrar tests so Overview now boots beside the existing handlers and an undeclared route still returns not found; run their focused tagged tests RED before registration and GREEN afterward.
- [ ] **Step 10: Verify and commit.** Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/overview/... ./services/core/internal/accounting/... ./services/core/internal/organisations/... ./services/core/internal/app/... ./services/core/internal/transport/... -race -count=1`; require exit zero. Run `rtk git add services/core/internal/overview services/core/internal/app services/core/internal/transport && rtk git commit -m "feat: compose walkthrough accounting services"`.

### Task 3: Replace the landing page with the production app shell

**Files:**
- Modify: `apps/desktop/src/shared/proto-ipc.ts`
- Modify: `apps/desktop/src/shared/proto-ipc.test.ts`
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/main/core-client.ts`
- Modify: `apps/desktop/src/main/core-client.test.ts`
- Modify: `apps/desktop/src/main/index-production.ts`
- Modify: `apps/desktop/src/main/electron-lifecycle.test.ts`
- Modify: `apps/desktop/src/main/rpc-router.ts`
- Modify: `apps/desktop/src/main/rpc-router.test.ts`
- Modify: `apps/desktop/src/main/ipc.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Modify: `apps/desktop/src/renderer/app-shell/app-shell.tsx`
- Create: `apps/desktop/src/renderer/app-shell/app-shell.test.tsx`
- Modify: `apps/desktop/src/renderer/app-shell/navigation.tsx`
- Create: `apps/desktop/src/renderer/app-shell/router.tsx`
- Create: `apps/desktop/src/renderer/app-shell/router.test.tsx`
- Create: `apps/desktop/src/renderer/app-shell/workspace-menu.tsx`
- Create: `apps/desktop/src/renderer/features/onboarding/demo-seed.ts`
- Create: `apps/desktop/src/renderer/features/onboarding/demo-seed.test.ts`
- Create: `apps/desktop/src/main/demo-fixture-intake.ts`
- Create: `apps/desktop/src/main/demo-fixture-intake.test.ts`
- Modify: `apps/desktop/src/renderer/features/setup/setup-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/setup/setup-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/overview/overview-screen.tsx`
- Create: `apps/desktop/src/renderer/features/overview/overview-screen.test.tsx`
- Modify: `apps/desktop/src/renderer/features/ledger/accounts-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/ledger/journal-screen.tsx`
- Create: `apps/desktop/src/renderer/features/ledger/journal-detail-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/ledger/general-ledger-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/ledger/trial-balance-screen.tsx`
- Create: `apps/desktop/src/renderer/features/ledger/walkthrough-ledger-screens.test.tsx`
- Create: `apps/desktop/src/renderer/features/settings/settings-screen.tsx`
- Modify: `apps/desktop/src/renderer/app.tsx`
- Modify: `apps/desktop/src/renderer/app.test.tsx`
- Modify: `apps/desktop/src/renderer/styles.css`
- Modify: `apps/desktop/tests/e2e/foundation.spec.ts`
- Create: `apps/desktop/tests/e2e/accounting-shell.spec.ts`

- [ ] **Step 1: Write the guarded-route RED.** Require locked `/overview` to redirect to `/unlock`, unauthenticated deep links to preserve a safe return route through `/sign-in`, authenticated unknown routes to return to `/overview`, and reload to restore only an authorised route. Journal detail uses canonical `/accounting/journals?journal=<uuidv7>`; reject duplicate/malformed/unknown query keys and restore a valid selected journal after reload. Run `rtk mise exec -- pnpm --dir apps/desktop test -- -t 'guarded walkthrough routes'`; expect missing-router failures.
- [ ] **Step 2: Implement separate complete-route and primary-navigation tables.** Preserve every Slice 1 route from its production task. Pre-auth routes are `/setup/workspace`, `/unlock`, and `/sign-in`; authenticated nested routes include `/workspace-trust`, `/restore`, `/restore/evidence`, `/settings/security`, `/settings/backup`, `/settings/users`, `/settings/organisation`, `/settings/ownership`, `/accounting/opening-balances`, and `/accounting/periods`. The initial primary sidebar contains only Overview (`/overview`), Chart of accounts (`/accounting/chart`), Journals (`/accounting/journals`), General ledger (`/accounting/general-ledger`), Trial balance (`/accounting/trial-balance`), Audit trail (`/audit`), and Settings (`/settings`). Journal detail is a validated durable `journal=<uuidv7>` query on the canonical journals route, never transient-only route state. Documents, Banking, and GST/BAS remain absent until their owning screens land. Update coverage only for the new `/overview` and `/settings` hub routes; preserve every canonical subsystem route. Rerun the guarded-route test to PASS.
- [ ] **Step 3: Write and implement the typed Overview boundary.** Start with failing core-client/router/preload tests requiring `getAttentionSummary`; then bind its generated client, binary codecs, IPC sender checks, and frozen named preload function. Reject unknown type URLs, oversized/malformed bytes, duplicate registration, and any generic method-name argument. Promote the coverage row only in this change and rerun the exact focused tests to PASS.
- [ ] **Step 4: Write the shell visual RED.** Assert Local data, business menu, exact currently available sidebar order, one main heading, compact rail, visible focus, Overview cards, blank actions, and absence of the disabled “comes next” control. Run the focused Vitest and expect missing wireframe structure.
- [ ] **Step 5: Implement the shell only.** Refine the Slice 1 shell/navigation, add the Settings hub, and apply the imagegen board's warm off-white/forest-green tokens, 1024×700 compact behavior, reduced motion, semantic landmarks, and focus order. Rerun `rtk mise exec -- pnpm --dir apps/desktop test -- -t 'walkthrough app shell'` to PASS before changing ledger views.
- [ ] **Step 6: Refine ledger views as separate micro-cycles.** For each of Accounts, Journals/detail, General ledger, and Trial balance, add one named case to `walkthrough-ledger-screens.test.tsx`, run `rtk mise exec -- pnpm --dir apps/desktop test -- -t '<screen> wireframe projection'` to the specific layout/provenance failure, modify only that existing Slice 1 view, and rerun to PASS. Generated projections are authoritative; React formats integer minor units but computes no accounting totals.
- [ ] **Step 7: Write and implement blank/demo onboarding.** Run `rtk mise exec -- pnpm --dir apps/desktop test -- -t 'blank and demo onboarding orchestration'` RED before adding the coordinator. The blank test drives real workspace, identity, organisation, chart, bank, and opening-conversion commands. `demo-seed.ts` is a typed orchestration state machine over those same named methods; it stores no accounting projection and rereads Overview/accounts/journals after every phase. `demo-fixture-intake.ts` may only return an approved capability for an exact packaged synthetic file hash and never writes business state. At Slice 1 it seeds prerequisites/opening only and labels Demo data. Rerun the identical test to PASS.
- [ ] **Step 8: Run and commit focused green gates.** Run desktop unit/type/lint plus descriptor/transition/coverage subchecks; require exit zero. Run `rtk git add apps/desktop test/e2e && rtk git commit -m "feat: add accounting desktop workspace"`.
- [ ] **Step 9: Run clean production and packaged gates.** On the committed tree run `rtk mise exec -- pnpm contracts:production` and `rtk mise exec -- pnpm desktop:e2e -- --grep 'accounting shell|blank workspace|demo workspace'`; require real core calls, offline denial, screenshots, no page/console errors, persistence after restart, and clean process exit.

### Task 4: Integrate document review and supplier posting screens

**Files:**
- Execute first: complete `docs/superpowers/plans/2026-08-02-contacts-receivables.md` Tasks 1–10 and its Slice 2 exit gate
- Execute: `docs/superpowers/plans/2026-08-02-payables-document-intake.md`
- Modify: `apps/desktop/src/renderer/features/documents/document-inbox-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/documents/document-review-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/documents/document-review-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/documents/review-handoff-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/purchases/bills-screen.tsx`
- Create: `apps/desktop/src/renderer/features/walkthrough/document-screens.test.tsx`
- Modify: `apps/desktop/src/renderer/features/onboarding/demo-seed.ts`
- Modify: `apps/desktop/src/renderer/features/onboarding/demo-seed.test.ts`
- Modify: `apps/desktop/src/renderer/app-shell/navigation.tsx`
- Create: `apps/desktop/tests/e2e/walkthrough-documents.spec.ts`

- [ ] **Step 1: Complete predecessor and owning plans without duplicate files.** Complete the full contacts/receivables plan and Slice 2 exit gate as required by the parent programme, then execute the complete payables/document plan through its Task 11 UI. The overlay starts only after those files and their subsystem tests are green.
- [ ] **Step 2: Write the wireframe-specific side-by-side RED.** In the existing review test require preview controls, candidate source highlights, editable fields/confidence, inline supplier resolution, arithmetic/GST diagnostics, and primary **Save review** action. Run `rtk mise exec -- pnpm --dir apps/desktop test -- -t 'walkthrough document review'`; expect the focused layout/action failure.
- [ ] **Step 3: Modify existing views for distinct explicit actions.** Refine the subsystem review and bills screens; do not recreate them. After `SaveReview`, the new handoff screen shows **Create bill draft**; the existing bill view then shows **Approve bill**. Preserve sealed-review immutability and draft corrections. Rerun the identical focused test to PASS.
- [ ] **Step 4: Extend demo orchestration through posting with a RED/GREEN cycle.** Run `rtk mise exec -- pnpm --dir apps/desktop test -- -t 'demo supplier document through approved bill'` RED. The test must ingest the exact bundled synthetic PDF through the approved fixture capability, wait on the real extraction job, resolve/create the supplier, call `SaveReview`, call `CreateTargetDraft`, then call `ApproveBill`, and finally reread Documents, Purchases, Journal, and Overview projections. GST-detail is not yet public and is asserted only after Reporting lands in Task 5. Implement only this state-machine phase; fixture JSON never becomes screen state. Rerun the identical test to PASS before Banking work begins.
- [ ] **Step 5: Run local extraction E2E.** In the packaged app with network denied, cover native PDF, scanned PDF, image receipt, mixed/rotated page, password, corrupt/oversize, duplicate, cancellation, restart, helper containment, and zero plaintext residue. Every command must exit zero.
- [ ] **Step 6: Assert the exact posting oracle.** Approval produces Dr Expense `$290`, Dr GST Receivable `$29`, Cr Payables `$319`, linked source/audit refs, and no payment or reconciliation yet.
- [ ] **Step 7: Verify and commit.** Run the payables plan's complete core/helper/desktop/packaged gates, require all green, then run `rtk git add apps/desktop test/e2e && rtk git commit -m "feat: align local document review with walkthrough"`.

### Task 5: Integrate banking, GST/BAS, and audit trace screens

**Files:**
- Execute: `docs/superpowers/plans/2026-08-02-banking-reconciliation.md`
- Execute: `docs/superpowers/plans/2026-08-02-reports-bas.md`
- Modify: `apps/desktop/src/renderer/features/banking/accounts-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/banking/import-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/banking/match-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/banking/reconciliation-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/tax/bas-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/audit/audit-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/onboarding/demo-seed.ts`
- Modify: `apps/desktop/src/renderer/features/onboarding/demo-seed.test.ts`
- Create: `apps/desktop/src/renderer/features/walkthrough/cross-links.tsx`
- Create: `apps/desktop/src/renderer/features/walkthrough/walkthrough-screens.test.tsx`
- Create: `apps/desktop/tests/e2e/accounting-tax-walkthrough.spec.ts`

- [ ] **Step 1: Complete Banking through its packaged exit gate.** Preserve separate supplier payment/allocation, statement match, and reconciliation commands and states.
- [ ] **Step 2: Complete Reporting/Tax with an explicit build-profile boundary.** Execute the complete subsystem plan. The verified `TEST_SIGNED_SIMULATOR` package retains its mandatory simulator banner/routes/tests; the ordinary local-accounting review package uses the normal build profile and exposes only report/BAS workpaper create, review, validate, and export. Production ATO/SBR is absent from both. The focused BAS view must say **Draft — not lodged**.
- [ ] **Step 3: Write cross-screen REDs.** Require source→sealed review→bill→journal→ledger→GST fact→BAS→audit links and bank statement→payment→match→reconciliation links using typed refs. Run `rtk mise exec -- pnpm --dir apps/desktop test -- -t 'walkthrough cross links'`; expect missing cross-link/rendering failures.
- [ ] **Step 4: Modify existing Banking/BAS/Audit views.** Do not recreate subsystem screens. Apply the approved hierarchy and use `unmatched`, `part matched`, `fully matched`, `not reconciled`, `draft`, and `completed` exactly; no score auto-confirms. BAS values come only from retained projections and the normal profile contains no declaration/lodge/prepare/submit controls. Audit integrity uses the production verifier. Rerun the identical cross-link test to PASS.
- [ ] **Step 5: Extend demo orchestration with an exact RED/GREEN cycle.** Run `rtk mise exec -- pnpm --dir apps/desktop test -- -t 'demo payment match reconciliation and bas'` RED. Require the state machine to drive the existing public payment/allocation, statement import, match, reconciliation, and BAS-workpaper methods after Task 4's posted bill, then reread Banking, Journal, Trial balance, GST-detail, BAS, Audit, and Overview production projections. Assert the bill's `$29.00` GST fact appears in GST detail and is not recognized again by payment. Implement that exact phase, prove the fixture contains inputs only, and rerun to PASS.
- [ ] **Step 6: Add final navigation with an exact RED/GREEN cycle.** Run `rtk mise exec -- pnpm --dir apps/desktop test -- -t 'final walkthrough navigation'` RED, then implement the exact ten-item final sidebar order. Require every primary action plus locked/unauthenticated route guards and preserve all nested subsystem routes. Rerun to PASS.
- [ ] **Step 7: Run the exact month oracle.** Verify the `$319` supplier payment, `$681` closing bank, zero reconciliation difference, `$1,000` debit/credit trial-balance totals, `1B $29`, and `$29` refundable BAS across public projections.
- [ ] **Step 8: Verify and commit.** Run contracts, full default/tagged/race/vet core gates, desktop test/typecheck/lint, and focused packaged E2E with all commands exiting zero. Run `rtk git add apps/desktop test/e2e && rtk git commit -m "feat: complete accounting tax walkthrough"`.

### Task 6: Close out local manual validation

**Files:**
- Modify: `apps/desktop/tests/e2e/accounting-tax-walkthrough.spec.ts`
- Create: `apps/desktop/tests/e2e/manual-validation.spec.ts`
- Create: `docs/development/local-accounting-walkthrough.md`
- Modify: `README.md`
- Modify: `compliance/traceability/core-accounting.csv`
- Modify after clean validation only: `compliance/evidence/core-accounting/manifest.json`

- [ ] **Step 1: Write the final visual/keyboard REDs.** Add Overview, Document review, Journal detail, and GST/BAS cases at 1536×1024 plus 1024×700, a keyboard-only primary workflow, accessibility assertions, and zero page/console errors. Run the exact Playwright grep and observe the new missing artifact/step assertion before implementation.
- [ ] **Step 2: Implement visual and error-hygiene support.** Compare structure and critical visibility rather than brittle pixel text rasterization; retain screenshot artifacts and trace/video only on failure. Rerun the exact focused grep to PASS.
- [ ] **Step 3: Publish and test manual steps.** Document blank/demo startup, fixture import, expected exact balances, retry/reset, and how to distinguish draft BAS from lodgement. Add a docs-link/package smoke assertion; include no secrets, arbitrary paths, or developer-only mutation.
- [ ] **Step 4: Commit the candidate source without retained evidence.** Run focused desktop/E2E/docs checks with exit zero, then `rtk git add apps/desktop docs README.md compliance/traceability && rtk git commit -m "test: verify accounting tax walkthrough"`. Do not create or stage `compliance/evidence` yet.
- [ ] **Step 5: Request independent code and visual review.** Resolve every critical/important finding, rerun focused gates, and commit any fixes before final validation; leave the tree clean.
- [ ] **Step 6: Run clean packaged validation.** From the final clean source commit run `rtk mise exec -- pnpm contracts:production`, `rtk mise exec -- pnpm lint`, `rtk mise exec -- pnpm typecheck`, `rtk mise exec -- pnpm test`, and `rtk mise exec -- pnpm desktop:e2e`; require exit zero and zero skips for production walkthrough cases.
- [ ] **Step 7: Materialize retained evidence separately.** Record the validated subject source revision and exact descriptor/schema/package/fixture/result hashes in `compliance/evidence/core-accounting/manifest.json`, run the existing evidence verifier to PASS, and commit only retained evidence with `rtk git add compliance/evidence/core-accounting && rtk git commit -m "build: retain walkthrough validation evidence"`. Never rewrite the source revision to the evidence-only commit.
- [ ] **Step 8: Automate and start the review build.** Use the existing packaged Playwright `electronHarness` from `apps/desktop/tests/e2e/fixtures.ts`; it resolves the verified packaged executable, launches it offline through `_electron`, observes the real window/core, captures failures, and proves orphan cleanup. Run the full walkthrough there first. After it passes on macOS, launch the same source-verified `apps/desktop/out/Tammy-darwin-arm64/Tammy.app` with `rtk open -na` and leave that reviewed build running for the user's manual validation.

## Completion gate

- [ ] Every approved sidebar route is real and every visible primary action works.
- [ ] Blank and demo workspaces use public production commands and survive restart.
- [ ] Local native/scanned extraction reaches review without network access or automatic posting.
- [ ] Review, target draft, source approval, supplier payment, match, and reconciliation remain separate auditable commands.
- [ ] Journal, ledger, trial balance, bank, GST/BAS, and audit projections match the exact deterministic oracle.
- [ ] In the ordinary local-accounting review profile BAS always says **Draft — not lodged** and exposes no declaration/lodgement/submission control; the separate verified test-signed simulator profile retains its mandatory simulator warning and simulator-only workflow assertions.
- [ ] Packaged Playwright walkthrough and manual review instructions are green on the local review build.
