# Core Business Accounting Programme Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the approved, offline-first Australian small-business accounting suite as six independently accepted vertical slices, using Buf/Protobuf contracts throughout and proving every included workflow in the packaged Electron application.

**Architecture:** The Go core owns all accounting rules and encrypted persistence behind generated Connect services. Protobuf messages are the application boundary across Go, Electron main/preload, and React; SQLite is a normalized persistence detail. Each slice extends one shared accounting kernel, adds descriptor-driven traceability before public RPCs ship, and passes real packaged E2E before the next slice begins.

**Tech Stack:** Buf 1.72 with v2 config, Protobuf proto3, Connect-Go/Connect-ES, Go 1.26.4, SQLCipher 4.15.0, Electron 43, React 19, TypeScript 7, Vitest, Playwright, Rust 1.97.1, pinned pdf-inspector, PDFium, Tesseract 5.5.2, pnpm 11.15, macOS arm64 and Windows x64 packaging.

---

**Normative design:** `docs/superpowers/specs/2026-08-02-core-business-accounting-suite-design.md`

**Foundation baseline:** commit `d094602` (`fix: sign packaged macOS application`) and `docs/superpowers/plans/2026-07-19-offline-desktop-foundation.md`

**Execution rule:** Work in this order. Do not begin a later slice until every checkbox under the predecessor's exit gate is complete and its packaged artefact is retained as release evidence.

## Chunk 1: Programme sequencing and release gates

### Task 1: Establish programme-level contract checks

**Files:**
- Create: `test/e2e/coverage.yaml`
- Create: `scripts/check-e2e-coverage.mjs`
- Create: `scripts/check-e2e-coverage.test.mjs`
- Create: `scripts/build-descriptors.mjs`
- Create: `scripts/build-descriptors.test.mjs`
- Create: `scripts/build-transition-index.mjs`
- Create: `scripts/build-transition-index.test.mjs`
- Create: `scripts/check-contracts.mjs`
- Create: `scripts/check-contracts.test.mjs`
- Create: `scripts/check-generated-tree.mjs`
- Create: `scripts/check-generated-tree.test.mjs`
- Create: `compliance/contracts/descriptors.pb`
- Create: `compliance/contracts/descriptor-manifest.json`
- Create: `test/e2e/transitions.yaml`
- Create: `apps/desktop/src/shared/preload-methods.json`
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/main/ipc.ts`
- Modify: `apps/desktop/src/main/ipc.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Create: `apps/desktop/src/preload/index.test.ts`
- Modify: `buf.yaml`
- Create: `buf.lock`
- Modify: `package.json`
- Modify: `pnpm-lock.yaml`
- Modify: `.gitignore`
- Modify: `.github/workflows/foundation-ci.yml`

- [ ] Start with one import/skeleton test, run `rtk pnpm exec node --test --test-name-pattern "checker module exists" scripts/check-e2e-coverage.test.mjs`, and observe the missing-module failure. Create only the empty checker module and rerun to the named missing-export failure, then add the smallest exported entry point and rerun to pass.
- [ ] Add the first rule subtest for a descriptor RPC absent from coverage; run `rtk pnpm exec node --test --test-name-pattern "missing descriptor RPC" scripts/check-e2e-coverage.test.mjs` and expect exactly `E2E_COVERAGE_RPC_MISSING`, then implement only that rule and rerun to pass.
- [ ] Add and complete these checker cycles strictly in order, rerunning the identical command after each minimal named validation branch: `rtk pnpm exec node --test --test-name-pattern "unknown coverage RPC" scripts/check-e2e-coverage.test.mjs` → `E2E_COVERAGE_RPC_UNKNOWN`; `rtk pnpm exec node --test --test-name-pattern "missing transition" scripts/check-e2e-coverage.test.mjs` → `E2E_COVERAGE_TRANSITION_MISSING`; `rtk pnpm exec node --test --test-name-pattern "unknown transition" scripts/check-e2e-coverage.test.mjs` → `E2E_COVERAGE_TRANSITION_UNKNOWN`; `rtk pnpm exec node --test --test-name-pattern "missing role outcome" scripts/check-e2e-coverage.test.mjs` → `E2E_COVERAGE_ROLE_MISSING`; `rtk pnpm exec node --test --test-name-pattern "extra role outcome" scripts/check-e2e-coverage.test.mjs` → `E2E_COVERAGE_ROLE_EXTRA`; `rtk pnpm exec node --test --test-name-pattern "missing production preload" scripts/check-e2e-coverage.test.mjs` → `E2E_COVERAGE_PRELOAD_MISSING`; `rtk pnpm exec node --test --test-name-pattern "missing scenario case" scripts/check-e2e-coverage.test.mjs` → `E2E_COVERAGE_CASE_MISSING`; `rtk pnpm exec node --test --test-name-pattern "malformed coverage manifest" scripts/check-e2e-coverage.test.mjs` → `E2E_COVERAGE_MANIFEST_INVALID`; and `rtk pnpm exec node --test --test-name-pattern "duplicate YAML key" scripts/check-e2e-coverage.test.mjs` → `E2E_COVERAGE_YAML_DUPLICATE_KEY`. Do not add the next subtest until the prior command passes.
- [ ] Add exact root dev dependencies `@bufbuild/protobuf@2.12.1` and `yaml@2.8.1`, update `pnpm-lock.yaml`, and decode `google.protobuf.FileDescriptorSet` with the generated well-known schema plus YAML `parseDocument` duplicate-key rejection.
- [ ] Implement the validator rule by rule. `build-transition-index.mjs --write` reads every exact slice fixture (`test/fixtures/proto/transitions.pb.json`, `sales/transitions.pb.json`, `payables/transitions.pb.json`, `documents/transitions.pb.json`, `banking/transitions.pb.json`, `reporting/transitions.pb.json`, and `tax/transitions.pb.json`), sorts by fully-qualified enum/transition ID, and writes `transitions.yaml`; `--check` regenerates in memory and fails on drift. Its first test uses the valid empty index. `preload-methods.json` is imported and asserted by the current `desktop-api.ts`, `ipc.ts`, and preload tests now, not deferred to Slice 1.
- [ ] Add a failing descriptor-build test that expects the canonical command to omit source information and source-retention options.
- [ ] Implement `build-descriptors.mjs` around exactly `buf build --as-file-descriptor-set --exclude-source-info --exclude-source-retention-options --output compliance/contracts/descriptors.pb` and write `descriptor-manifest.json` with exactly `path`, `byteLength`, lowercase `sha256`, `bufVersion`, `module`, and `gitRevision`.
- [ ] Give the Buf module identity `buf.build/tammyapp/tammy`, add the pinned Protovalidate dependency, run `rtk pnpm exec buf dep update`, and retain `buf.lock`. Descriptor evidence manifests use that exact module identity and require `TAMMY_SOURCE_REVISION`; CI passes the subject SHA and evidence generation rejects a dirty tree before writing output.
- [ ] Add exactly `/.tmp/contracts/` to `.gitignore`. Implement `build-descriptors.mjs --validation` to write `.tmp/contracts/descriptors.pb` plus its validation manifest without requiring a Git revision or clean worktree, and `--evidence` to write `compliance/contracts/descriptors.pb` plus the retained manifest only when the tree is clean and `TAMMY_SOURCE_REVISION` equals `git rev-parse HEAD`. `check-contracts.mjs` orchestrates validation output, generated-tree drift, transition drift, and coverage using `.tmp/contracts/descriptors.pb`; it cleans its temp directory on success/failure and its test proves no ignored temp residue remains.
- [ ] Add exact root scripts: `proto:descriptors:check: "node scripts/build-descriptors.mjs --validation"`, `proto:descriptors:evidence: "node scripts/build-descriptors.mjs --evidence"`, `transitions:generate: "node scripts/build-transition-index.mjs --write"`, `transitions:check: "node scripts/build-transition-index.mjs --check"`, `e2e:coverage: "node scripts/check-e2e-coverage.mjs --descriptors .tmp/contracts/descriptors.pb"`, `contracts: "pnpm proto:format:check && pnpm proto:lint && pnpm proto:breaking && pnpm proto:generate && node scripts/check-contracts.mjs"`, `test: "node --test scripts/*.test.mjs && go test ./services/core/... && pnpm desktop:test"`, `typecheck: "go test ./services/core/... && pnpm desktop:typecheck"`, `package: "pnpm desktop:package"`, and `test:e2e:packaged: "pnpm desktop:e2e"`. Package-signature tests remain under `desktop:package`/packaged E2E and are intentionally absent from the clean-checkout unit aggregate.
- [ ] Seed `coverage.yaml` with E2E-00..17 and map `tammy.v1.SystemService.GetDiagnostics` to production `getSystemDiagnostics`, case `foundation/offline-ready`, and diagnostics projection. Mark roles/list/idempotency as `not_applicable_pre_workspace_system_query` rather than claiming unexecuted role/error/list cases; the checker permits only this named reviewed exception.
- [ ] Add descriptor-manifest cycles one at a time, rerunning each identical command after its minimal implementation: `rtk pnpm exec node --test --test-name-pattern "validation mode omits source revision" scripts/build-descriptors.test.mjs` → `DESCRIPTOR_VALIDATION_REVISION_FORBIDDEN`; `rtk pnpm exec node --test --test-name-pattern "real descriptor byte length" scripts/build-descriptors.test.mjs` → `DESCRIPTOR_MANIFEST_LENGTH_MISMATCH`; `rtk pnpm exec node --test --test-name-pattern "lowercase descriptor sha256" scripts/build-descriptors.test.mjs` → `DESCRIPTOR_MANIFEST_SHA256_INVALID`; `rtk pnpm exec node --test --test-name-pattern "pinned buf version" scripts/build-descriptors.test.mjs` → `DESCRIPTOR_MANIFEST_BUF_VERSION_MISMATCH`; `rtk pnpm exec node --test --test-name-pattern "exact buf module" scripts/build-descriptors.test.mjs` → `DESCRIPTOR_MANIFEST_MODULE_MISMATCH`; `rtk pnpm exec node --test --test-name-pattern "missing source revision" scripts/build-descriptors.test.mjs` → `DESCRIPTOR_SOURCE_REVISION_REQUIRED`; `rtk pnpm exec node --test --test-name-pattern "mismatched source revision" scripts/build-descriptors.test.mjs` → `DESCRIPTOR_SOURCE_REVISION_MISMATCH`; and `rtk pnpm exec node --test --test-name-pattern "dirty evidence tree" scripts/build-descriptors.test.mjs` → `DESCRIPTOR_EVIDENCE_DIRTY_TREE`. Each implementation adds only that manifest check before the same command must pass.
- [ ] Run `rtk pnpm exec node --test scripts/check-e2e-coverage.test.mjs scripts/build-descriptors.test.mjs scripts/build-transition-index.test.mjs scripts/check-generated-tree.test.mjs scripts/check-contracts.test.mjs` and `rtk pnpm contracts`; confirm green with dirty-tree validation mode and no `.tmp` residue.
- [ ] Commit the checker source first: `rtk git add .gitignore package.json pnpm-lock.yaml buf.yaml buf.lock scripts test/e2e apps/desktop/src/shared apps/desktop/src/main/ipc.ts apps/desktop/src/main/ipc.test.ts apps/desktop/src/preload .github/workflows/foundation-ci.yml && rtk git commit -m "test: enforce descriptor driven e2e coverage"`.
- [ ] From that clean source commit run `TAMMY_SOURCE_REVISION=$(rtk git rev-parse HEAD) rtk pnpm proto:descriptors:evidence`, verify the manifest identifies that subject commit, then `rtk git add compliance/contracts && rtk git commit -m "build: retain programme contract descriptors"`. Future slices use `pnpm contracts` while dirty and only regenerate retained evidence after their code commit.

### Task 2: Execute Slice 1 — secure workspace, ledger, and GST kernel

**Plan:** `docs/superpowers/plans/2026-08-02-ledger-gst-kernel.md`

- [ ] Complete every task in the Slice 1 plan using `@superpowers:subagent-driven-development` and `@superpowers:test-driven-development`.
- [ ] Confirm the generated descriptor contains only the Slice 1 services plus the existing system service and every public method has a `coverage.yaml` entry.
- [ ] Confirm `E2E-00`, `E2E-01`, and the Slice 1 portion of `E2E-14` pass against the signed packaged application with a real encrypted workspace.
- [ ] Confirm an independent reviewer reports no unresolved critical or important findings.
- [ ] Tag retained evidence as `slice-1-ledger-gst` in `compliance/evidence/core-accounting/manifest.json`.
- [ ] Run `rtk pnpm core-accounting:evidence -- --slice 1`, create an encrypted backup, restore it in a fresh installation context, and require `slice 1 evidence verified`.

### Task 3: Execute Slice 2 — contacts and receivables

**Plan:** `docs/superpowers/plans/2026-08-02-contacts-receivables.md`

- [ ] Complete every task in the Slice 2 plan after Slice 1's exit gate.
- [ ] Confirm `E2E-02`, `E2E-03`, `E2E-04`, and the receivables portions of `E2E-07` and `E2E-08` pass in the packaged app.
- [ ] Cross-reconcile issued sales documents, settlement allocations, the receivables control account, customer statements, aged receivables, and non-cash GST facts.
- [ ] Retain the packaged artefact, logs, descriptor hash, fixture hash, and public-projection oracle in the evidence manifest.
- [ ] Run `rtk pnpm core-accounting:evidence -- --slice 2`, backup/restore the Slice 2 workspace, and require identical public projections plus `slice 2 evidence verified`.

### Task 4: Execute Slice 3 — payables and document intake

**Plan:** `docs/superpowers/plans/2026-08-02-payables-document-intake.md`

- [ ] Complete every task in the Slice 3 plan after Slice 2's exit gate.
- [ ] Confirm native, scanned, mixed, rotated, encrypted, corrupt, and size-limited PDF paths use a sandboxed helper and can never approve or post.
- [ ] Confirm `E2E-05`, `E2E-06`, and the payables/cash-GST parts of `E2E-07` and `E2E-08` pass in the packaged app.
- [ ] Cross-reconcile approved purchases, settlement allocations, the payables control account, aged payables, evidence state, and GST facts.
- [ ] Run `rtk pnpm core-accounting:evidence -- --slice 3`, backup/restore encrypted evidence and helper metadata, and require `slice 3 evidence verified`.

### Task 5: Execute Slice 4 — banking and reconciliation

**Plan:** `docs/superpowers/plans/2026-08-02-banking-reconciliation.md`

- [ ] Complete every task in the Slice 4 plan after Slice 3's exit gate.
- [ ] Confirm `E2E-09`, `E2E-10`, and `E2E-11` pass for bank and credit-card accounts in the packaged app.
- [ ] Cross-reconcile bank journal balances, imported-line dispositions, many-to-many matches, transfers, outstanding movements, and reconciliation statements.
- [ ] Confirm duplicate imports and concurrent matching/reconciliation attempts are deterministic and auditable.
- [ ] Run `rtk pnpm core-accounting:evidence -- --slice 4`, backup/restore imports/matches/reconciliations, and require `slice 4 evidence verified`.

### Task 6: Execute Slice 5 — reports and BAS

**Plan:** `docs/superpowers/plans/2026-08-02-reports-bas.md`

- [ ] Complete every task in the Slice 5 plan after Slice 4's exit gate.
- [ ] Confirm `E2E-12`, `E2E-13`, and the complete `E2E-14` pass in the packaged app.
- [ ] Run the canonical month oracle and second cash-basis scenario from Design §15.3; verify every cross-projection oracle from §15.4.
- [ ] Confirm BAS simulator outcomes are labelled non-lodgement simulations and production ATO/SBR remains absent/disabled.
- [ ] Run `rtk pnpm core-accounting:evidence -- --slice 5`, backup/restore all reports/BAS/exports, and require `slice 5 evidence verified`.

### Task 7: Execute Slice 6 — release hardening

**Plan:** `docs/superpowers/plans/2026-08-02-accounting-release-hardening.md`

- [ ] Complete every task in the Slice 6 plan after Slice 5's exit gate.
- [ ] Confirm `E2E-15`, `E2E-16`, and `E2E-17` plus the full descriptor-derived suite pass on macOS arm64 and Windows x64 packaged artefacts.
- [ ] Confirm migration, backup/restore, audit-chain verification, hostile input, and native-helper containment gates pass; retain scale timings as informational until the approved fixed-hardware policy is release-blocking.
- [ ] Produce one release-evidence manifest containing toolchain, source commit, descriptor, schema, artefact, fixture, and result hashes.

### Task 8: Run the programme completion gate

**Files:**
- Verify: `compliance/traceability/core-accounting.csv`
- Verify: `compliance/evidence/core-accounting/manifest.json`
- Verify: `docs/development/core-accounting.md`
- Verify: `README.md`

- [ ] Run `rtk pnpm install --frozen-lockfile` and `rtk pnpm check:toolchain` from a clean checkout.
- [ ] Run `rtk pnpm contracts`, `rtk pnpm lint`, `rtk pnpm typecheck`, and `rtk pnpm test`.
- [ ] Run `rtk pnpm package` and `rtk pnpm test:e2e:packaged` on macOS arm64; require all required cases pass with zero skips.
- [ ] Run `.github/workflows/foundation-windows11-e2e.yml` job `windows11-23h2-x64-packaged-e2e` at the same Git source revision and require the evolved `core-accounting-windows11-x64-<sha>` artefact (renamed from the foundation artifact in Slice 6) with zero required skips.
- [ ] Verify the full E2E manifest has no omitted RPC, lifecycle transition, role outcome, idempotency case, stale-version case, list-state case, or principal domain failure.
- [ ] Verify every included workflow is reachable in production navigation and no production control is a placeholder.
- [ ] Verify no network connection occurs during startup, normal accounting, document extraction, reporting, backup, or restore.
- [ ] Verify `rtk git status --short` is empty, the signed `core-accounting-evidence-v1` tag points to Slice 6's evidence commit, and that tagged payload names the tested `subjectSourceRevision`. Do not mutate source/docs/evidence or create a post-evidence commit after the signed release record.

## Plan index and dependency map

| Order | Plan | Primary scenarios | Hard dependency |
|---:|---|---|---|
| 1 | `2026-08-02-ledger-gst-kernel.md` | E2E-00, E2E-01, E2E-14 partial | Signed offline foundation |
| 2 | `2026-08-02-contacts-receivables.md` | E2E-02, E2E-03, E2E-04, E2E-07/08 partial | Slice 1 accounting kernel |
| 3 | `2026-08-02-payables-document-intake.md` | E2E-05, E2E-06, E2E-07/08 partial | Slice 2 settlement primitives |
| 4 | `2026-08-02-banking-reconciliation.md` | E2E-09, E2E-10, E2E-11 | Slices 1–3 source documents and payments |
| 5 | `2026-08-02-reports-bas.md` | E2E-12, E2E-13, E2E-14 | Complete accounting fact model |
| 6 | `2026-08-02-accounting-release-hardening.md` | E2E-15, E2E-16, E2E-17 | All functional slices |

## Non-negotiable programme invariants

- Generated Go and TypeScript files are committed but never hand-edited.
- Persistent commands use UUID idempotency keys and canonical semantic request hashes; release-wide descriptor fingerprints are evidence, not idempotency inputs.
- Every ordinary state mutation crosses authorization, idempotency election, one SQL unit of work, domain invariants, audit append, and deterministic result persistence before commit. Security challenges and external restore follow their separately specified attempt/journal rules.
- Monetary values use integer minor units plus explicit currency; rates use scaled integers. JavaScript floating point does not perform accounting arithmetic.
- Posted journals are immutable. Corrections and reversals link to originals and preserve the audit trail.
- No plaintext database fallback exists. A failed SQLCipher/key-store gate aborts workspace creation/open.
- Document extraction only creates candidates. Explicit human review is required before a business draft, and the normal approval action remains separate.
- Tests create integration/E2E state through public Protobuf commands. Direct SQL is reserved for repository and migration boundary tests.
- Production ATO/SBR, live bank feeds, payroll, inventory, multi-currency, and the other Design §2.2 exclusions remain out of scope.
