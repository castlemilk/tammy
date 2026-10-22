# Core Accounting Release Hardening Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Slice 6: complete permission/negative/concurrency coverage, verifiable audit evidence, migration and backup/restore resilience, hostile-input containment, measured scale, cross-platform signed packaged E2E, and reproducible release evidence.

**Architecture:** Harden the already functional five-slice product without changing its domain boundaries. Descriptor-derived matrices drive role and lifecycle tests; backup/restore and audit evidence operate on real encrypted workspaces; failure injection proves atomicity; packaged macOS/Windows artefacts run the same canonical public-Protobuf oracles under enforced network denial. Release evidence hashes every shipped contract, native dependency, artefact, fixture, and result.

**Tech Stack:** Existing Buf/Go/SQLCipher/Rust/Electron stack, Node evidence verifiers, Playwright, Go race/fuzz/property tests, cargo tests, platform signing/sandbox tools, SBOM/vulnerability/licence scanners.

---

**Normative designs:** `docs/superpowers/specs/2026-08-02-core-business-accounting-suite-design.md` §§12–17 and `docs/superpowers/specs/2026-07-19-tammy-local-first-accounting-sbr-design.md` §§7.7, 8, 12–14.

**Prerequisite:** Slices 1–5 and the canonical/cash-basis packaged month oracles are green and retained.

**Scope rule:** Slice 6 expands matrices and adversarial proof. It does not introduce security, backup, migration, audit, or packaged behavior that should have been present when first required; if a predecessor gate is missing, fix and reaccept that predecessor first.

**Required skills while executing:** `@superpowers:test-driven-development`, `@security-best-practices`, `@playwright`, and `@superpowers:verification-before-completion`.

**Micro-TDD rule:** Add each matrix/checker/failure-injection case separately, run the narrow named test to its expected failure, implement only that rule, rerun to `PASS`, then continue. Broad release commands run only after all focused cases are green.

**Per-task red/green index:** Task 1 starts with `rtk pnpm exec node --test --test-name-pattern 'omitted descriptor RPC' scripts/generate-e2e-matrix.test.mjs`; Task 2 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/app -run 'TestRoleMatrix/auditor_mutation_denied$'`; Task 3 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/audit -run 'TestVerifyChain/tampered_event_bytes$'`; Task 4 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/backup -run 'TestBackupJob/cancel_before_rename$'`; Task 5 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/restore -run 'TestRestoreJournal/death_after_swap$'`; Task 6 with `rtk go test -tags tammy_sqlcipher ./services/core/internal/storage/migrations -run 'TestMigrationJournal/death_after_migrated$'`; Task 7 with `rtk pnpm exec node --test --test-name-pattern 'document decompression limit' scripts/generate-hostile-fixtures.test.mjs`; Task 8 with `rtk pnpm exec node --test --test-name-pattern 'missing machine record' scripts/run-performance.test.mjs`; Task 9 with `rtk pnpm --filter @tammy/desktop test -- -t 'discovers E2E-00 through E2E-17'`; Task 10 with `rtk pnpm exec node --test --test-name-pattern 'unpinned scanner' scripts/check-release-security.test.mjs`; and Task 11 with `rtk pnpm test:e2e:packaged -- --grep 'E2E-16 audit tamper'`. Add only the named test first, observe its named assertion or missing-symbol failure, implement the smallest rule, rerun that same command to `PASS`, then continue one case at a time before the broad gate.

## Chunk 1: Authorization, audit, backup, restore, and migrations

### Task 1: Freeze the complete public RPC/transition/role/failure matrix

**Files:**
- Create: `test/e2e/generated/roles.json`
- Create: `test/e2e/generated/failures.json`
- Create: `test/e2e/generated/envelopes.json`
- Create: `scripts/generate-e2e-matrix.mjs`
- Create: `scripts/generate-e2e-matrix.test.mjs`
- Modify: `scripts/check-e2e-coverage.mjs`
- Modify: `scripts/check-e2e-coverage.test.mjs`
- Modify: `test/e2e/coverage.yaml`
- Create: `apps/desktop/tests/e2e/generated-role-matrix.spec.ts`
- Create: `apps/desktop/tests/e2e/generated-negative-matrix.spec.ts`

- [ ] Write failing checker/generator tests for an omitted RPC, unknown RPC, missing named preload method, missing/extra role outcome, missing lifecycle edge/invalid edge, missing replay/conflict, missing stale-version/principal failure, and missing empty/populated/filter/page query state.
- [ ] Keep `test/e2e/coverage.yaml` as the sole hand-maintained authority. Generate read-only `generated/roles.json`, `failures.json`, and `envelopes.json` from it plus descriptors/transition fixtures; CI fails if generated views drift.
- [ ] Require every coverage row to name production preload, visible UI or hidden-role denial, public projection oracle, packaged spec/fixture, and envelope class: ordinary command, query, fresh challenge, setup/recovery election, persisted job, or external restore.
- [ ] For each ordinary command assert all documented operation-level ports receive one active `TxScope` and no cross-module repository/SQL handle; queries use `UoW.Read` without idempotency; challenges count each attempt and are not retried; restore alone uses the external journal.
- [ ] Populate the full coverage matrix for `SystemService` plus all Workspace, Identity, Organisation, Contact, Accounting, Sales, Purchases, Settlement, Document, Banking, Reporting, Tax, and Audit RPCs.
- [ ] Add the generated specs first and run `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep "generated role matrix|generated negative matrix"`; require the first run to fail on the named missing generated case/production denial. Implement only the generator/matrix wiring, rerun `rtk pnpm contracts`, repackage, and rerun the identical packaged grep to pass with zero omissions, unknowns, focused cases, or skips before commit.
- [ ] Commit: `rtk git add test/e2e scripts apps/desktop/tests/e2e && rtk git commit -m "test: freeze complete rpc permission matrix"`.

### Task 2: Prove centralized authorization and concurrency failure behavior

**Files:**
- Modify: `services/core/internal/authorisation/policy.go`
- Modify: `services/core/internal/authorisation/policy_test.go`
- Create: `services/core/internal/app/role_matrix_integration_test.go`
- Create: `services/core/internal/app/concurrency_integration_test.go`
- Create: `services/core/internal/app/failure_injection_integration_test.go`
- Create: `apps/desktop/tests/e2e/concurrency.spec.ts`

- [ ] Drive every RPC as `workspace_admin`, `business_preparer`, `business_lodger`, `auditor`, unauthenticated, locked-workspace, stale-session, and deactivated-user actors; assert exact typed allow/deny details from the `coverage.yaml` role rows.
- [ ] Prove core authorization uses the envelope declared in coverage: same write `TxScope` for ordinary mutations, same read snapshot for queries, authenticated attempt scope for challenges, and external-journal proof for restore. Renderer visibility never grants access and auditor cannot mutate through named preload calls.
- [ ] Run `E2E-15` stale-version, rapid double-submit, changed-request idempotency conflict, and concurrent allocation/match/reconciliation elections with deterministic barriers rather than sleeps.
- [ ] Inject failure after source save, posting, tax fact, allocation/match/reconciliation, audit append, revision increment, and result persistence for every financial command family; assert source/journal/tax/audit/idempotency/revision all roll back.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/app/... ./services/core/internal/authorisation/... -race -count=20` and `rtk pnpm desktop:e2e -- --grep "E2E-15"` against the packaged artefact.
- [ ] Commit: `rtk git add services/core/internal apps/desktop/tests/e2e && rtk git commit -m "test: harden permissions and transaction elections"`.

### Task 3: Complete signed audit evidence and rollback detection

**Files:**
- Modify: `services/core/internal/audit/export.go`
- Modify: `services/core/internal/audit/export_test.go`
- Modify: `services/core/internal/audit/export_job.go`
- Modify: `services/core/internal/audit/export_job_test.go`
- Modify: `services/core/internal/audit/keys.go`
- Modify: `services/core/internal/audit/keys_test.go`
- Modify: `services/core/internal/audit/mirror.go`
- Modify: `services/core/internal/audit/mirror_darwin.go`
- Modify: `services/core/internal/audit/mirror_windows.go`
- Modify: `services/core/internal/audit/mirror_test.go`
- Create: `services/core/internal/audit/descriptor_catalog.go`
- Create: `services/core/internal/audit/descriptor_catalog_test.go`
- Modify: `services/core/cmd/tammy-evidence-verify/main.go`
- Modify: `services/core/cmd/tammy-evidence-verify/main_test.go`
- Modify: `apps/desktop/src/renderer/features/audit/audit-screen.tsx`
- Create: `apps/desktop/src/renderer/features/audit/audit-screen.test.tsx`
- Modify: `services/core/internal/audit/service.go`
- Modify: `services/core/internal/app/composition.go`

- [ ] Write exact chain tests for the approved domain-separated genesis/event formula, canonical event length, sequence uniqueness, typed payload/schema fingerprint, generation changes, concurrent append, tamper localization, and ≥12-month accessible retention.
- [ ] Write OS-mirror tests for normal commit, database-ahead crash repair after full verification, mirror-ahead rollback lock, moved-workspace read-only warning, decline, and explicit trust establishment with passphrase/admin password/fresh TOTP and `WORKSPACE_MIRROR_ESTABLISHED`.
- [ ] Implement Ed25519 key creation encrypted under workspace DEK, public key/header ID, rotation with cross-signature, and no rewrite of prior events.
- [ ] Implement Audit `VerifyChain` and `ExportEvidence` with filtered preview, canonical `events.jsonl`, public key, and a signed manifest. For every exported event include exact stored `payload.pb`, a non-authoritative canonical `payload.json` display rendering, schema fingerprint, and the transitive historical `descriptors.pb` set selected from an immutable fingerprint→descriptor-hash catalogue; selected evidence objects remain separately hashed. Exclude secrets/full unrestricted documents.
- [ ] Harden the existing export job's operation/input hashes, checkpoints/progress, cancellation before verified rename, destination-hash recovery, restart reconstruction, and three-failure/explicit-retry behavior.
- [ ] Build a standalone verifier that validates hashes, sequence, chain, stored payload bytes, historical descriptor fingerprints/transitive imports, canonical display JSON, cross-signed keys, and signature without database access or an installed matching Tammy version. It never reserializes JSON to establish the stored payload hash.
- [ ] Add tamper fixtures for event bytes/order, `payload.pb`, display JSON mismatch, missing/wrong/transitively incomplete historical descriptor set, fingerprint collision/mismatch, evidence bytes, manifest, signature, wrong key, missing object, and rollback mirror.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/audit/... ./services/core/internal/app/... ./services/core/cmd/tammy-evidence-verify/... -race -count=1` and `rtk pnpm --filter @tammy/desktop test -- audit-screen`; expect all tamper/job/UI cases pass.
- [ ] Commit: `rtk git add services/core/internal/audit services/core/internal/app services/core/cmd/tammy-evidence-verify apps/desktop/src/renderer/features/audit && rtk git commit -m "test: harden verifiable audit evidence"`.

### Task 4: Harden backup creation and cancellation/restart behavior

**Files:**
- Modify: `services/core/internal/backup/format.go`
- Modify: `services/core/internal/backup/format_test.go`
- Modify: `services/core/internal/backup/service.go`
- Modify: `services/core/internal/backup/service_integration_test.go`
- Modify: `services/core/internal/backup/job.go`
- Modify: `services/core/internal/backup/job_test.go`
- Create: `test/fixtures/backup/manifest.json`
- Modify: `apps/desktop/src/renderer/features/workspace/backup-screen.tsx`

- [ ] Assert no public Backup contract/schema change is needed. If a defect requires one, amend and reaccept Slice 1 with exact field numbers/types/generated outputs/consumers/Buf breaking/coverage before returning here.
- [ ] Write archive tests for `tammy-backup-v1`: SQLite online snapshot, header, referenced evidence/artefacts/rules, the exact ordered migration manifest with migration IDs/checksums, canonical signed manifest, schema/app/audit head, object paths/sizes/hashes, and explicit exclusion of credential vault/passwords/remembered keys/sessions/RPC bootstrap/logs.
- [ ] Implement random archive key plus chunked AES-256-GCM and a backup-specific Argon2id KEK; do not reuse workspace KEK/history. Test Unicode NFC without trimming, 15–128 Unicode code points, ≤1,024 UTF-8 bytes, spaces/printable Unicode, exact pinned 10,000-password denylist/hash, random 16-byte salt, Argon2id 64 MiB/3 iterations/parallelism 1/32-byte result and stored policy version; backup passphrases have no cross-backup history. Validate truncation/reordering/replay/tamper/wrong passphrase.
- [ ] Write persisted job tests for write gate, snapshot/checkpoint, cancellation before atomic destination rename, same-key completed verification/retry, restart temp cleanup, destination failure, and no partial visible backup.
- [ ] Prove the workspace write gate is released immediately after the online snapshot/evidence reference set is captured, before archive encryption/rendering; later writes do not leak into that manifest.
- [ ] Prove restricted artefacts are included only when redistribution permits; otherwise retain checksum/provenance and restore marks the dependent adapter unavailable until explicit reimport.
- [ ] Require approved destination handle, password confirmation, scope preview, and clear unrecoverability warning.
- [ ] Run `rtk pnpm contracts && rtk go test -tags tammy_sqlcipher ./services/core/internal/backup/... -race -count=1 && rtk pnpm --filter @tammy/desktop test`.
- [ ] Commit: `rtk git add services/core/internal/backup apps/desktop test/fixtures/backup && rtk git commit -m "test: harden encrypted backup creation"`.

### Task 5: Prove restore and external-journal crash recovery

**Files:**
- Modify: `services/core/internal/restore/journal.go`
- Modify: `services/core/internal/restore/journal_test.go`
- Modify: `services/core/internal/restore/service.go`
- Modify: `services/core/internal/restore/service_integration_test.go`
- Modify: `services/core/internal/restore/pre_restore_archive.go`
- Modify: `services/core/internal/restore/pre_restore_archive_test.go`
- Modify: `apps/desktop/src/renderer/features/workspace/restore-screen.tsx`
- Modify: `apps/desktop/src/renderer/features/workspace/restore-screen.test.tsx`
- Modify: `apps/desktop/tests/e2e/backup-restore.spec.ts`

- [ ] Write state-machine tests for fsync'd `PREPARED`, `STAGED`, `SWAPPED`, and `COMPLETE`, injecting process death before/after every write/rename/fsync and proving startup finishes or reverses safely.
- [ ] Verify staged authenticated encryption/signature/objects/database/schema/audit/invariants before swap, then prove both exact authentication alternatives: staged admin with workspace passphrase + user password + fresh TOTP, or staged recovery secret + admin username + new workspace/user passwords executing audited `RecoverAdministrator`.
- [ ] Implement staged admin authentication or audited administrator break-glass, invalidate sessions/remembered keys, create/verify encrypted `tammy-pre-restore-v1`, and retain its DEK-wrapped key for ≥12 months.
- [ ] Prove changed manifest with reused operation key returns `IDEMPOTENCY_CONFLICT`; restored machine credentials, remembered references, prior sessions, RPC bootstrap, and operational logs remain absent.
- [ ] Require the reaccepted Slice 1 generated `ExportPreRestoreArchive`/`DeletePreRestoreArchive` contracts, handlers, client exports, named preload methods, coverage rows, and UI controls before this task starts. Test export through the public RPC requires unlocked restored workspace + admin password + fresh TOTP/approved handle, and deletion through its separate RPC after 12 months requires version/reason and an audited admin command; no automatic expiry deletion or repository-only path occurs.
- [ ] Set generation to `max(backup_generation, active_mirror_generation)+1`, append exactly one `WORKSPACE_RESTORED` linked to backup head, update mirror, and delete rollback only after the pre-restore archive verifies.
- [ ] Assert canonical restore truth: organisation, journals, balances, `$80` BAS, declaration, accepted simulator result, original evidence hashes; later distinguishable journal absent live but recoverable in pre-restore archive.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/restore/... -race -count=1` and packaged `rtk pnpm desktop:e2e -- --grep "backup restart restore|E2E-16"`.
- [ ] Commit: `rtk git add services/core/internal/restore apps/desktop/src/renderer/features/workspace apps/desktop/tests/e2e && rtk git commit -m "test: prove atomic encrypted restore"`.

### Task 6: Migrate and recover every retained release schema

**Files:**
- Create: `test/fixtures/migrations/manifest.json`
- Create: `scripts/capture-release-workspace.mjs`
- Create: `scripts/capture-release-workspace.test.mjs`
- Create: `services/core/internal/storage/migrations/all_releases_test.go`
- Create: `services/core/internal/storage/migrations/failure_matrix_test.go`
- Create: `services/core/internal/storage/migrations/migration_journal.go`
- Create: `services/core/internal/storage/migrations/migration_journal_test.go`
- Modify: `services/core/internal/storage/sqlcipher/migrate.go`

- [ ] For each released schema, verify the signed release tag/commit, create a clean historical Git worktree, restore its frozen Buf/Go/pnpm/Rust/native toolchains and lockfiles, build that historical signed package/core, and seed the encrypted workspace only through that historical release's public Protobuf commands. Capture the resulting database/evidence with source tag/revision, historical binary/package/toolchain/lock/descriptor/schema/fixture/audit hashes; current code never pretends to emit an old schema and fixture databases are never hand-edited.
- [ ] For every fixture, close sessions, verify encrypted recovery snapshot, stage copy, apply ordered checksummed migrations, run FK/integrity/audit/journal/subledger/bank/GST/report invariants, atomically activate, and retain recovery metadata.
- [ ] Define a domain-separated migration-journal HMAC key derived from an installation key stored only in macOS Keychain/Windows Credential Manager. Chain every record over prior MAC plus operation/schema/source/staging/recovery hashes and `PREPARED`, `MIGRATED`, `SWAPPED`, `VERIFIED`, `COMPLETE`; records contain no accounting values, every transition is fsync'd, tamper/wrong-installation/replay/gap fails closed, and the authenticated journal/recovery metadata is retained until the next verified backup before explicit cleanup.
- [ ] Inject failure at copy, backup verification, every migration statement group, invariant check, each journal fsync, rename, directory fsync, reopen, and mirror update. Assert exact prior/active/staged byte hashes and that startup either keeps prior active or verifies/completes/reverses the swap.
- [ ] Retain recovery metadata until the next verified backup. A failure writes only a safe typed local diagnostic with operation/schema/stage/hash IDs and no accounting data.
- [ ] Prove compatible unknown-field/evidence payload round trips and retained descriptor decoders for every release.
- [ ] Run `rtk pnpm exec node --test scripts/capture-release-workspace.test.mjs && rtk go test -tags tammy_sqlcipher ./services/core/internal/storage/migrations/... -race -count=1`.
- [ ] Commit: `rtk git add test/fixtures/migrations scripts services/core/internal/storage && rtk git commit -m "test: migrate every encrypted release schema"`.

## Chunk 2: Adversarial inputs, scale, cross-platform package, and evidence

### Task 7: Run the hostile document/import and secret-leak matrix

**Files:**
- Create: `test/security/hostile-inputs.yaml`
- Create: `scripts/generate-hostile-fixtures.mjs`
- Create: `scripts/generate-hostile-fixtures.test.mjs`
- Create: `apps/desktop/tests/e2e/hostile-inputs.spec.ts`
- Create: `apps/desktop/tests/e2e/secret-leak.spec.ts`
- Modify: `test/fixtures/documents/corpus/manifest.json`
- Modify: `test/fixtures/banking/csv/manifest.json`
- Modify: `test/fixtures/banking/ofx/manifest.json`
- Create: `test/security/fuzz-policy.json`
- Create: `helpers/document-extractor/fuzz/Cargo.toml`
- Create: `helpers/document-extractor/fuzz/fuzz_targets/protocol.rs`
- Create: `helpers/document-extractor/fuzz/fuzz_targets/document.rs`
- Create: `services/core/internal/banking/imports/fuzz_test.go`
- Modify: `mise.toml`
- Modify: `compliance/build/toolchain.lock.json`

- [ ] Generate deterministic safe fixtures for truncation/polyglot/decompression/pixel/page/frame/nesting/entity/encoding/formula/path/control-character/duplicate/fuzz-regression cases and retain seed/tool/license/hash metadata.
- [ ] Assert exact existing bounds: document 50 MiB input, 200 pages, 20,000-pixel edge, 100 MP/page, 500 rendered MP total, 16 MiB frame, 128 MiB output, 1 GiB RSS, and 10 minutes; import 50 MiB raw, 100 MiB decoded/normalized text, 100,000 rows, 1 MiB decoded field, 64 nesting levels, 10,000 diagnostics, and 8 MiB diagnostic text with DTD/external/internal entities disabled. Require the exact typed terminal/retryable failures, no business mutation, encrypted-original retention, zero temp bytes, sandbox survival rules, and restart behavior.
- [ ] Capture core/helper/main/renderer/stdout/stderr/audit/export/support-bundle output and search for passphrases, recovery secret, TOTP secret/code, PDF password, SQLCipher key, capability, credential, raw contact/document content, unrestricted paths, and fixture canaries.
- [ ] Pin the fuzz-only Rust nightly and `cargo-fuzz` checksum in `toolchain.lock.json`; run each Rust target for 60 seconds with 1 GiB RSS/output limits and retained seed/crash hashes, and each Go parser target with `-fuzztime=60s`. Promote every crash to the regression manifest before green.
- [ ] Add each packaged hostile/secret case first, run `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep "hostile inputs|secret leak"`, and require its named limit/leak/sandbox assertion to fail before implementing only that case. Run `rtk pnpm exec node --test scripts/generate-hostile-fixtures.test.mjs`, `rtk cargo test --workspace --locked`, `rtk cargo fuzz --manifest-path helpers/document-extractor/fuzz/Cargo.toml run protocol -- -max_total_time=60 -rss_limit_mb=1024`, `rtk cargo fuzz --manifest-path helpers/document-extractor/fuzz/Cargo.toml run document -- -max_total_time=60 -rss_limit_mb=1024`, and `rtk go test -tags tammy_sqlcipher ./services/core/internal/banking/imports -fuzz Fuzz -fuzztime=60s`; then repackage and rerun the identical hostile/secret grep to pass with zero skips and no canary outside encrypted originals.
- [ ] Commit: `rtk git add test/security test/fixtures scripts apps/desktop/tests/e2e helpers/document-extractor/fuzz services/core/internal/banking/imports mise.toml compliance/build/toolchain.lock.json && rtk git commit -m "test: harden hostile local inputs"`.

### Task 8: Add reproducible scale fixtures and record non-blocking benchmarks

**Files:**
- Create: `services/core/internal/testkit/scale.go`
- Create: `services/core/internal/testkit/scale_test.go`
- Create: `services/core/internal/performance/bench_test.go`
- Create: `test/performance/policy.json`
- Create: `scripts/run-performance.mjs`
- Create: `scripts/run-performance.test.mjs`
- Modify: `package.json`

- [ ] Generate state through public Protobuf commands for at least 250,000 journal lines, 100,000 statement lines, 20,000 contacts, and 10,000 evidence documents with a fixed fixture version/seed/hash.
- [ ] Make `policy.json` scope only `latencyGoals.releaseBlocking: false` and record runner OS/build, CPU model/count, RAM, storage, power mode, cold/warm cache procedure, fixture generator/version/seed, three cold and seven warm runs, p50/p95 calculation, and raw-sample retention. The schema rejects a global/non-latency `releaseBlocking: false`.
- [ ] Measure ordinary paginated lists, standard reports, and 10,000-line statement staging against the provisional 300 ms/2 s/5 s goals; report variance and failures without claiming a release gate.
- [ ] Put unconditional `hardFailure: true` correctness/resource assertions in a separate policy section: all financial invariants hold, helper peak RSS ≤1 GiB, core peak RSS ≤2 GiB for the declared fixture, post-job temp bytes = 0, no orphan process, cancellation ≤10 seconds, and encrypted workspace/evidence size ≤3× generated logical fixture bytes. The runner exits nonzero for any invariant, containment, cleanup, cancellation, or resource breach regardless of the informational latency flag.
- [ ] Add root `performance:record` as `node scripts/run-performance.mjs --temporary-output`; it creates/prints a run directory under the OS temporary directory outside the worktree. Its unit test rejects a missing/inconsistent policy or machine record and proves the default command never writes tracked evidence or dirties Git status.
- [ ] Run `rtk pnpm exec node --test scripts/run-performance.test.mjs && rtk pnpm performance:record`; retain the completed machine/policy/raw/result record only in the run's untracked temporary evidence directory until the final clean source revision exists.
- [ ] A future reviewed plan may set `latencyGoals.releaseBlocking: true` only after fixed reference hardware and CI variance budget are approved; correctness/resource gates are already unconditional.
- [ ] Commit: `rtk git add services/core/internal/testkit services/core/internal/performance test/performance scripts package.json && rtk git commit -m "test: record accounting scale benchmarks"`.

### Task 9: Enforce signed offline packaged E2E on macOS and Windows 11

**Files:**
- Modify: `apps/desktop/playwright.config.ts`
- Modify: `apps/desktop/tests/e2e/fixtures.ts`
- Modify: `apps/desktop/tests/e2e/offline-network-guard.ts`
- Modify: `scripts/run-offline-e2e-darwin.mjs`
- Modify: `scripts/run-offline-e2e-darwin.test.mjs`
- Modify: `scripts/run-offline-e2e-windows.ps1`
- Create: `scripts/verify-native-dependency-closure.mjs`
- Create: `scripts/verify-native-dependency-closure.test.mjs`
- Create: `apps/desktop/tests/e2e/package-manifest.spec.ts`
- Create: `apps/desktop/tests/e2e/offline-suite.spec.ts`
- Create: `apps/desktop/src/main/playwright-discovery.test.ts`
- Modify: `apps/desktop/forge.config.ts`
- Modify: `scripts/build-manifest-schema.mjs`
- Modify: `scripts/build-manifest-schema.test.mjs`
- Modify: `scripts/write-build-manifest.mjs`
- Modify: `scripts/write-build-manifest.test.mjs`
- Modify: `scripts/check-core-accounting-evidence.mjs`
- Modify: `scripts/check-core-accounting-evidence.test.mjs`
- Modify: `.github/workflows/foundation-ci.yml`
- Modify: `.github/workflows/foundation-windows11-e2e.yml`

- [ ] Make Playwright discover every `*.spec.ts`; add a unit test that enumerates expected E2E-00..17 files and fails on omission/focus/skip/test-only RPC tunnel.
- [ ] Enforce network denial at both OS sandbox/firewall test layer and Electron session layer, fail on any attempted DNS/TCP/UDP/HTTP/WebSocket/loopback connection except the pinned local core channel, and retain attempt logs.
- [ ] On a pinned macOS 14 Sonoma arm64 runner, record `sw_vers` product/build and `uname -m=arm64`; require the app, nested core/helper/frameworks and signed DMG to pass `codesign --verify --deep --strict --verbose=4`, require the retained `notarytool` submission ID/status `Accepted`, run `xcrun stapler validate` for app/DMG, `spctl --assess --type execute` for the installed app, Gatekeeper open assessment for the DMG, and `hdiutil verify`. No ad-hoc/development signature or unstapled/unnotarized distribution qualifies.
- [ ] On Windows 11 23H2 x64, retain edition/version/build/architecture and require pinned-Windows-SDK `signtool verify /pa /all` for the per-user installer, app/core/helper and every shipped PE/DLL including PDFium; no unsigned “authenticated resource identity” substitute qualifies. Enumerate Mach-O dependencies with retained-Xcode `otool -L`/`dyld_info` and PE imports with pinned-Windows-SDK `dumpbin /dependents`, recursively resolve every shipped/native system edge, and compare the exact graph/hashes/ABI to PDFium `DEPS.lock.json`, `CIPD.lock.json`, BUILD_ARGS/runtime manifest and Tesseract/Leptonica/SQLCipher locks. Reject ambient, missing, extra, wrong-architecture, writable-search-path, or unexpected library edges.
- [ ] Verify bundled pdf-inspector/PDFium/Tesseract/tessdata/fonts/descriptor hashes and the independently Go-verified fixed `TEST_SIGNED_SIMULATOR` profile. Require the permanent `SIMULATOR — NOT FOR ATO LODGMENT` root-frame/confirmation warning and reject any argv/env/runtime profile override, missing banner, devtools/source maps, or unexpected binary.
- [ ] Run all E2E-00..17 plus canonical/cash-basis/negative matrices against the packaged artefact using the real generated client, Go core, encrypted DB/blob store, and native helper.
- [ ] Make macOS arm64 and Windows 11 x64 workflows upload named `core-accounting-<platform>-<source-sha>` artefacts containing app/package manifest, logs, traces-on-failure, descriptor/schema/native/SBOM/license/signature hashes, and coverage result.
- [ ] Run `rtk pnpm exec node --test scripts/verify-native-dependency-closure.test.mjs scripts/run-offline-e2e-darwin.test.mjs` plus desktop discovery/unit tests locally, then make the reviewed source/workflow commit: `rtk git add apps/desktop scripts .github/workflows && rtk git commit -m "test: enforce cross platform packaged accounting e2e"`.
- [ ] At that exact commit SHA, run pinned macOS 14 arm64 `rtk pnpm desktop:e2e` (it packages the evidenced notarized DMG) and retain its OS build/dependency-closure attestation. Run `.github/workflows/foundation-windows11-e2e.yml` job `windows11-23h2-x64-packaged-e2e` at the same SHA with its PowerShell/native-closure tests; require exact Windows 11 23H2 x64 attestation, `WINDOWS11_23H2_X64_RELEASE_GATE=true`, equal locked dependency graphs, and zero required skips. Task 10 remains blocked until both pass; failures require a new reviewed fix commit and complete rerun. Windows Server, newer/different unrecorded macOS, Intel macOS, Windows 10, and ARM Windows never qualify.

### Task 10: Produce SBOM, dependency, licence, secret, and static-analysis evidence

**Files:**
- Create: `scripts/check-release-security.mjs`
- Create: `scripts/check-release-security.test.mjs`
- Create: `compliance/security/allowlist.json`
- Create: `compliance/security/scanner-policy.json`
- Create: `compliance/security/advisory-sources.lock.json`
- Create: `compliance/licenses/NOTICE.generated.txt`
- Modify: `package.json`
- Modify: `pnpm-lock.yaml`
- Modify: `services/core/go.sum`
- Modify: `Cargo.lock`
- Modify: `mise.toml`
- Modify: `compliance/build/toolchain.lock.json`
- Modify: `.github/workflows/foundation-ci.yml`

- [ ] Write checker tests that fail on an unpinned dependency, checksum/license omission, unreviewed vulnerability, secret pattern, forbidden production endpoint, source map/devtool, unsigned binary, or manifest/SBOM drift.
- [ ] Generate CycloneDX components for Go, npm, Rust, SQLCipher, pdf-inspector, PDFium plus its locked DEPS/CIPD/native-codec closure, Tesseract, Leptonica, traineddata, fonts, and packaged executables/resources with versions, source revisions, licenses, checksums, and dependency relationships; keep the preliminary SBOM/result only in the run's untracked temporary evidence directory until the final clean source revision exists.
- [ ] Pin these exact scanner/generator versions plus per-platform archive/module checksums and invocations in `toolchain.lock.json`/`scanner-policy.json`: `govulncheck v1.1.4`, `gosec v2.25.0`, `pnpm audit` from pinned pnpm `11.15.0`, `cargo-audit 0.22.2`, `osv-scanner v2.3.8`, `gitleaks v8.30.1`, `@cyclonedx/cyclonedx-npm 6.0.0`, `cyclonedx-gomod v1.10.0`, and `cargo-cyclonedx 0.5.9`; pin `codesign` to the retained macOS/Xcode build and `signtool` to the retained Windows SDK build. The checker rejects `latest`, ranges, missing hashes, wrong reported versions, or expired exceptions.
- [ ] Make advisory inputs reproducible and offline: lock the Go vulnerability DB snapshot/index plus fetched module records, RustSec advisory-db Git commit/archive, OSV database snapshot, and exact raw registry response used by `pnpm audit` with source URL, retrieval time, byte length, SHA-256 and signature/provenance where available. Scanner invocations consume only those retained snapshots; `advisory-sources.lock.json` and final `advisory-inputs.tar.zst` must agree byte-for-byte, and a network attempt or mutable live DB fails.
- [ ] Run the pinned Go/npm/Rust/native vulnerability, static-analysis, secret, and licence commands against the locked advisory inputs; every accepted finding records ID, component/version, advisory snapshot/hash, owner, rationale, compensating control, approval, expiry date, and evidence hash.
- [ ] Verify optional updates cannot block offline operation and no hidden support channel exists.
- [ ] Add root `release:security` as `node scripts/check-release-security.mjs --temporary-output`; it creates/prints a run directory under the OS temporary directory outside the worktree. Its test proves every configured scanner result/evidence hash/advisory input is required and the default command never writes tracked evidence or dirties Git status.
- [ ] Run `rtk pnpm exec node --test scripts/check-release-security.test.mjs && rtk pnpm release:security`; expected result is zero unreviewed findings.
- [ ] Commit: `rtk git add scripts compliance/security compliance/licenses compliance/build/toolchain.lock.json package.json pnpm-lock.yaml services/core/go.sum Cargo.lock mise.toml .github/workflows && rtk git commit -m "build: record release security policy"`.

### Task 11: Run final E2E-16/E2E-17 and publish the release evidence manifest

**Files:**
- Create: `apps/desktop/tests/e2e/audit-migration.spec.ts`
- Create: `apps/desktop/tests/e2e/package-security.spec.ts`
- Modify: `compliance/traceability/core-accounting.csv`
- Modify: `compliance/evidence/core-accounting/manifest.json`
- Create: `compliance/release/external-gates.json`
- Create: `compliance/evidence/core-accounting/performance.json` (after clean source commit)
- Create: `compliance/evidence/core-accounting/sbom.cdx.json` (after clean source commit)
- Create: `compliance/evidence/core-accounting/advisory-inputs.tar.zst` (after clean source commit)
- Create: `docs/development/core-accounting.md`
- Create: `README.md`

- [ ] Complete packaged `E2E-16`: audit verify/export/tamper, backup/restart/restore, wrong passphrase, every release migration, each staged failure, and invariant verification.
- [ ] Complete packaged `E2E-17`: signed helpers, network/filesystem/process denial, resource kill, cleanup, parent exit, macOS/Windows package/resource manifests, independently verified simulator profile/banner/fixture marker containment, and zero unexpected binaries.
- [ ] Run from clean checkout: `rtk pnpm install --frozen-lockfile`, `rtk pnpm check:toolchain`, `rtk pnpm contracts`, `rtk pnpm lint`, `rtk pnpm typecheck`, `rtk pnpm test`, `rtk go test -tags tammy_sqlcipher ./services/core/... -race -count=1`, and `rtk cargo test --workspace --locked`; this is the exact programme root gate, with checker/connect-client/desktop/package unit coverage rather than desktop-only substitutes.
- [ ] Run macOS `rtk pnpm desktop:package && rtk pnpm desktop:e2e` and the exact Windows 11 packaged workflow; require all E2E-00..17/canonical/negative matrices pass with zero required skips.
- [ ] Record only `subjectSourceRevision` inside hashed evidence as the code SHA used to build/test both platforms. Build the final manifest with descriptor bytes/hash/length/Buf/module, schema/migration, fixtures, app/core/helper/native dependencies, SBOM/licences/advisory-inputs, notarization/stapling/Gatekeeper and Authenticode/native-closure evidence, signing identities, macOS/Windows artefacts/OS builds, network-denial logs, test results, and informational performance record. Do not place a self-referential evidence-commit SHA inside that commit's hashed payload.
- [ ] Keep all preliminary runtime tuples/results under the test run's untracked temporary evidence directory through source review/commit. Materialize `compliance/evidence/core-accounting/**` only after clean-source descriptor generation; the checker proves each retained tuple/result hash came from the subject revision.
- [ ] Record unresolved DPO/ATO/SBR/external verification gates and explicit unsupported claims in `compliance/release/external-gates.json`; production SBR/live bank features remain disabled and the release does not imply regulatory approval or lodgement connectivity.
- [ ] Request independent code/security review, resolve all critical/important findings, repeat affected source gates, then commit the reviewed E2E specs/traceability/docs without retained descriptor/results: `rtk git add apps/desktop/tests/e2e/audit-migration.spec.ts apps/desktop/tests/e2e/package-security.spec.ts compliance/traceability/core-accounting.csv compliance/release/external-gates.json docs/development/core-accounting.md README.md && rtk git commit -m "test: finalize core accounting release source"`.
- [ ] From that clean source commit run `TAMMY_SOURCE_REVISION=$(rtk git rev-parse HEAD) rtk pnpm proto:descriptors:evidence`, rebuild/retest the exact macOS and same-subject Windows 11 packages, then run `TAMMY_SOURCE_REVISION=$(rtk git rev-parse HEAD) rtk pnpm exec node scripts/run-performance.mjs --evidence-output compliance/evidence/core-accounting/performance.json` and `TAMMY_SOURCE_REVISION=$(rtk git rev-parse HEAD) rtk pnpm exec node scripts/check-release-security.mjs --evidence-output compliance/evidence/core-accounting --sbom-output compliance/evidence/core-accounting/sbom.cdx.json --advisory-inputs-output compliance/evidence/core-accounting/advisory-inputs.tar.zst`. Populate the final manifest with that `subjectSourceRevision` and run `rtk pnpm core-accounting:evidence`; expect `core accounting release evidence verified` with no unstaged generated drift outside retained evidence.
- [ ] Commit exactly the retained descriptor/result evidence: `rtk git add compliance/contracts compliance/evidence/core-accounting && rtk git commit -m "release: accept core accounting suite"`. After the commit exists, identify its SHA non-self-referentially with `rtk git tag -s core-accounting-evidence-v1 -m "signed core accounting evidence; subject and manifest are inside the tagged payload" HEAD`; the signed tag target is the evidence commit and the tag/release record remains outside the hashed evidence payload.

## Slice 6 exit gate

- [ ] `test/e2e/coverage.yaml`, its generated role/failure/envelope views, descriptors, transition fixtures, production preload API, and packaged specs agree with no omissions or unknowns.
- [ ] E2E-00 through E2E-17, canonical month, cash-basis month, negative matrices, migration fixtures, and cross-projection oracles pass on signed macOS arm64 and Windows 11 x64 artefacts.
- [ ] Audit, backup, restore, migration, helper/import containment, offline/network denial, and release security checks retain independently verifiable evidence.
- [ ] Scale correctness gates pass; latency measurements remain explicitly informational until a reviewed fixed-hardware policy makes them blocking.
- [ ] Every included workflow is accessible offline in production UI with no placeholder controls, and every excluded integration remains absent or disabled without unsupported claims.
