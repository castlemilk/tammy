# Secure Workspace, Ledger, and GST Kernel Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Slice 1: an encrypted single-organisation workspace with local roles, auditable/idempotent commands, a balanced immutable ledger, Australian GST controls, opening conversion, periods, and packaged journal/trial-balance workflows.

**Architecture:** Build the secure platform substrate first because every financial command depends on it. Generated Protobuf/Connect contracts enter a Go application composition root, execute through authorization and one SQLCipher-backed unit of work, append hash-linked audit events, and return binary Protobuf through allowlisted Electron IPC. Accounting policies expand typed intents into immutable normalized journal/tax facts.

**Tech Stack:** Buf 1.72/Protobuf, Connect-Go/Connect-ES, Go 1.26.4, SQLCipher 4.15.0 with `database/sql`, Electron 43/React 19/TypeScript 7, Vitest, Playwright, Node 24 test runner.

---

**Normative designs:** `docs/superpowers/specs/2026-08-02-core-business-accounting-suite-design.md` §§3.2, 4–6, 8.3–8.4, 11.1–11.2, 12–16 and `docs/superpowers/specs/2026-07-19-tammy-local-first-accounting-sbr-design.md` §§5–8, 10–14. Where the newer design deliberately refines an accounting rule, it controls; all unchanged foundation security/recovery/backup rules remain mandatory.

**Prerequisite:** Programme Task 1 in `docs/superpowers/plans/2026-08-02-core-accounting-programme.md` is green and committed.

**Required skills while executing:** `@superpowers:test-driven-development`, `@security-best-practices`, `@frontend-design`, `@playwright`, and `@superpowers:verification-before-completion`.

**Micro-TDD rule for every task:** Add one named case at a time, run the narrow command and observe the stated missing-symbol or assertion failure, implement only enough for that case, rerun to `PASS`, then refactor while green. Never commit a deliberately red fixture or bundle an unobserved test matrix into one implementation step.

## Slice 1 RPC and transaction map

Every row is added to `test/e2e/coverage.yaml` in the same commit as its descriptor. `ordinary` means authorization, semantic-hash/idempotency election, domain work, accounting/tax effects, audit append, deterministic response storage, and one commit occur inside one `UnitOfWork`. `challenge` means every call is a fresh rate-limited attempt and is never automatically retried. `restore` uses the fsync'd external operation journal, not the ordinary-command envelope.

| RPC(s) | Class; owner and operation-level ports | Named preload method(s); production route | Packaged scenario |
|---|---|---|---|
| `CreateWorkspace`, `ConfirmRecovery` | setup journal/challenge; Workspace, header store, storage factory, AuditAppender | `createWorkspace`, `confirmRecovery`; `/setup/workspace` | E2E-00 |
| `UnlockWorkspace`, `LockWorkspace`, `ForgetRememberedWorkspace`, `GetWorkspaceState` | challenge/session action/query; Workspace, key store, encrypted storage | matching named methods; `/unlock` | E2E-00 |
| `ChangePassphrase` | ordinary after current-passphrase challenge + new fresh TOTP; Workspace, header/key store, AuditAppender | `changeWorkspacePassphrase`; `/settings/security` | E2E-00 |
| `RecoverWorkspace`, `RecoverAdministrator` | challenge then recovery-operation election; Workspace/Identity, header/key store, AuditAppender | `recoverWorkspace`, `recoverAdministrator`; `/recover` | E2E-00 |
| `EstablishMovedWorkspaceTrust` | challenge + ordinary; Workspace/Audit, mirror/key store | `establishMovedWorkspaceTrust`; `/workspace-trust` | E2E-00/E2E-16 |
| `BackupWorkspace`, `CancelBackup`, `GetBackupJob`, `ListBackupJobs` | ordinary persisted job/queries; Backup, all-storage snapshot, audit signer | matching named methods; `/settings/backup` | E2E-00/E2E-16 |
| `RestoreWorkspace`, `GetRestoreStatus` | restore/query; external journal, staged Identity/Audit/invariants | `restoreWorkspace`, `getRestoreStatus`; `/restore` | E2E-00/E2E-16 |
| `ExportPreRestoreArchive`, `CancelPreRestoreArchiveExport`, `GetPreRestoreArchiveExportJob`, `ListPreRestoreArchiveExportJobs`, `DeletePreRestoreArchive`, `GetPreRestoreArchive`, `ListPreRestoreArchives` | persisted export/ordinary delete/admin queries after password + fresh TOTP where mutating; Restore evidence repository/export job, AuditAppender; deletion requires retained age ≥12 months and reason | matching lower-camel methods; `/restore/evidence` | E2E-00/E2E-16 |
| `TransferOwnership` | ordinary + fresh TOTP; Workspace/Organisation, OrganisationImpactPort, IdentitySessionImpactPort, AuditAppender | `transferWorkspaceOwnership`; `/settings/ownership` | E2E-00 |
| `SignIn`, `SignOut`, `GetSession`, `GetCurrentUser` | challenge/session action/query; Identity/session store | matching named methods; `/sign-in` | E2E-00 |
| `CreateUser`, `AssignRoles`, `ResetUserAuthentication`, `GetUser`, `ListUsers` | ordinary/query; Identity, IdentitySessionImpactPort, AuditAppender; reset requires a new fresh TOTP | matching named methods; `/settings/users` | E2E-00 |
| `ActivateUser` | fresh activation challenge; Identity/attempt journal/session; never ordinary-idempotent or automatically retried | `activateUser`; `/activate` | E2E-00 |
| `ChangePassword` | ordinary authenticated persistent command; Identity, session impact, AuditAppender in one UoW | `changePassword`; `/settings/security` | E2E-00 |
| `EnrolTOTP`, `DisableTOTP` | ordinary authenticated persistent commands; Identity/factor/session impact, AuditAppender in one UoW; disable requires a new fresh assertion | `enrolTotp`, `disableTotp`; `/settings/security` | E2E-00 |
| `ConfirmTOTP`, `AssertTOTP` | fresh rate-limited challenges; Identity/factor/attempt journal; never ordinary-idempotent or automatically retried | `confirmTotp`, `assertTotp`; `/settings/security` | E2E-00 |
| `CreateOrganisation`, `UpdateOrganisation`, `RecordEntityVerification`, `GetOrganisation` | ordinary/query; Organisations, OrganisationImpactPort, evidence blob port, AuditAppender; identity/GST update requires a new fresh TOTP | matching named methods; `/setup/organisation`, `/settings/organisation` | E2E-00 |
| `CreateAccount`, `UpdateAccount`, `SetAccountStatus`, `ListTaxCodes` | ordinary except query; Accounting, OrganisationReadPort, ArtefactReadPort, TaxRules, AuditAppender | `createAccount`, `updateAccount`, `setAccountStatus`, `listTaxCodes`; `/accounting/chart` | E2E-01 |
| `PostOpeningConversion`, `ReplaceOpeningConversion` | ordinary; Accounting orchestrator, opening AR/AP/financial-account ports, AccountingPostingPort, TaxReportImpactPort, AuditAppender; replacement requires a new fresh TOTP | `postOpeningConversion`, `replaceOpeningConversion`; `/accounting/opening-balances` | E2E-01 |
| `PostManualJournal`, `ReverseJournal` | ordinary; Accounting, ArtefactReadPort/TaxRules, TaxReportImpactPort, AuditAppender | `postManualJournal`, `reverseJournal`; `/accounting/journals` | E2E-14 |
| `ClosePeriod`, `ReopenPeriod` | ordinary + fresh TOTP; Accounting, TaxReportImpactPort, AuditAppender | `closePeriod`, `reopenPeriod`; `/accounting/periods` | E2E-14 |
| `GetAccount`, `ListAccounts`, `GetJournal`, `ListJournals`, `GetGeneralLedger`, `GetTrialBalance` | query; Accounting read repositories within `UnitOfWork.Read` | corresponding lower-camel named methods; `/accounting/*` | E2E-01/E2E-14 |
| `VerifyChain`, `ListAuditEvents`, `ExportEvidence`, `CancelAuditExport`, `GetAuditExportJob`, `ListAuditExportJobs` | query/persisted export; Audit, evidence reader/signing key | matching named methods; `/audit` | E2E-00/E2E-16 |

**Generated output map:** Each changed service proto produces its exact `services/core/internal/gen/tammy/v1/<file>.pb.go`, service-bearing files produce `services/core/internal/gen/tammy/v1/tammyv1connect/<service>.connect.go`, and every file produces `packages/connect-client/src/gen/tammy/v1/<file>_pb.ts`. `packages/connect-client/package.json`, `services/core/go.sum`, and `pnpm-lock.yaml` are updated atomically when generation/runtime dependencies change. Generated files are listed in the commit diff and never edited by hand.

For Slice 1, `<file>` is exactly `common`, `workspace`, `identity`, `organisation`, `accounting`, `audit`, `events`, and `fixtures`; Connect outputs are exactly `workspace.connect.go`, `identity.connect.go`, `organisation.connect.go`, `accounting.connect.go`, and `audit.connect.go` under `tammyv1connect`. TypeScript outputs are the same eight basenames with `_pb.ts`.

## Chunk 1: Canonical contracts and secure platform

### Task 1: Define Slice 1 Protobuf contracts and golden fixtures

**Files:**
- Create: `proto/tammy/v1/common.proto`
- Create: `proto/tammy/v1/workspace.proto`
- Create: `proto/tammy/v1/identity.proto`
- Create: `proto/tammy/v1/organisation.proto`
- Create: `proto/tammy/v1/accounting.proto`
- Create: `proto/tammy/v1/audit.proto`
- Create: `proto/tammy/v1/events.proto`
- Create: `proto/tammy/v1/fixtures.proto`
- Create: `test/fixtures/proto/canonical-requests.json`
- Create: `test/fixtures/proto/transitions.pb.json`
- Create: `services/core/internal/contracts/canonical_fixture_test.go`
- Create: `packages/connect-client/src/canonical-fixture.test.ts`
- Modify: `packages/connect-client/package.json`
- Modify: `services/core/go.mod`
- Modify: `services/core/go.sum`
- Modify: `pnpm-lock.yaml`
- Modify: `buf.gen.yaml`
- Modify: `test/e2e/coverage.yaml`

- [ ] Write a failing Go contract test that loads generated descriptors and requires `Money`, `Decimal`, `CivilDate`, `SourceRef`, `CommandContext`, `PageRequest`, typed error details, all Slice 1 lifecycle enums, and every RPC in the Slice 1 transaction map.
- [ ] Run `rtk pnpm proto:generate && rtk go test ./services/core/internal/contracts/...` and confirm the missing descriptors fail the test.
- [ ] Add commented Protobuf contracts with `_UNSPECIFIED = 0`, UUIDv7 strings, signed `int64` minor units, scaled decimals, civil dates, expected versions, UUID idempotency keys, explicit presence, no financial floats/maps/`Any`, and typed principal failures.
- [ ] Expand abbreviated query operations into explicit `GetAccount`, `ListAccounts`, `GetJournal`, `ListJournals`, `GetGeneralLedger`, and `GetTrialBalance` RPCs.
- [ ] Add `test/fixtures/proto/transitions.pb.json` plus canonical request cases covering absent versus explicitly present fields, sorted/deduplicated `FieldMask`, repeated-order significance, int64 JSON strings, timestamp normalization, enum names, and unknown-field rejection; run `rtk pnpm transitions:generate` and require `test/e2e/transitions.yaml` to contain every Slice 1 lifecycle edge.
- [ ] Update `coverage.yaml` in the same change with each fully-qualified RPC, named preload method, roles, lifecycle transitions, principal failures, list states, replay/conflict cases, and scenario `E2E-00`, `E2E-01`, or `E2E-14`.
- [ ] Add the connect-client `test` script/export entries, then run `rtk pnpm proto:format:check`, `rtk pnpm proto:lint`, `rtk pnpm proto:generate`, `rtk pnpm contracts`, `rtk go test ./services/core/internal/contracts/...`, and `rtk pnpm --filter @tammy/connect-client test`; every command exits 0.
- [ ] Run generation twice and compare a staged snapshot plus `rtk git status --short -- services/core/internal/gen packages/connect-client/src/gen`; fail on modified or untracked generated drift.
- [ ] Commit: `rtk git add proto test/fixtures test/e2e services/core/internal/contracts services/core/internal/gen packages/connect-client services/core/go.mod services/core/go.sum pnpm-lock.yaml buf.gen.yaml && rtk git commit -m "feat: define secure ledger protobuf contracts"`.

### Task 2: Implement deterministic platform primitives

**Files:**
- Create: `services/core/internal/platform/clock/clock.go`
- Create: `services/core/internal/platform/ids/ids.go`
- Create: `services/core/internal/platform/money/money.go`
- Create: `services/core/internal/platform/money/money_test.go`
- Create: `services/core/internal/platform/canonical/request.go`
- Create: `services/core/internal/platform/canonical/request_test.go`
- Create: `services/core/internal/platform/uow/uow.go`
- Create: `services/core/internal/platform/uow/uow_test.go`
- Create: `services/core/internal/platform/faults/faults.go`
- Create: `services/core/internal/platform/paging/cursor.go`
- Create: `services/core/internal/platform/paging/cursor_test.go`

- [ ] Write table tests for checked int64 addition/subtraction/multiplication, exact-half-away-from-zero GST allocation including negative ties, largest-remainder distribution with stable line-ID tie-break, debit/credit signs, and overflow rejection.
- [ ] Run `rtk go test ./services/core/internal/platform/...` and confirm missing packages fail.
- [ ] Implement integer-only money/rate primitives; keep Protobuf at boundaries and explicit domain types inside calculations.
- [ ] Write cross-language fixture tests for RFC 8785 normalized Protobuf JSON and semantic request hashes; include unknown binary fields, field masks, explicit defaults, timestamps, and an unrelated descriptor addition that must not change an old request hash.
- [ ] Implement semantic hash algorithm `v1`, removing only authentication metadata and idempotency key, and rejecting unknown fields before hashing.
- [ ] Write a fake-transaction test proving one `UnitOfWork` either commits domain state, idempotency result, and audit event once or rolls all three back.
- [ ] Implement `TxScope`, transaction-owned repositories, typed faults, deterministic signed cursors, injectable clock, and UUIDv7 generator.
- [ ] Run `rtk go test ./services/core/internal/platform/... -count=1` and confirm all tests pass without sleeps.
- [ ] Commit: `rtk git add services/core/internal/platform && rtk git commit -m "feat: add deterministic accounting platform primitives"`.

### Task 3: Pin and prove the SQLCipher storage boundary

**Files:**
- Create: `third_party/sqlcipher/VERSION`
- Create: `third_party/sqlcipher/SHA256SUMS`
- Create: `third_party/sqlcipher/LICENSE`
- Create: `scripts/vendor-sqlcipher.mjs`
- Create: `scripts/vendor-sqlcipher.test.mjs`
- Create: `scripts/build-sqlcipher.mjs`
- Create: `scripts/build-sqlcipher.test.mjs`
- Create: `services/core/internal/storage/sqlcipher/driver.go`
- Create: `services/core/internal/storage/sqlcipher/driver_integration_test.go`
- Create: `services/core/internal/storage/sqlcipher/database.go`
- Create: `services/core/internal/storage/sqlcipher/database_test.go`
- Modify: `services/core/go.mod`
- Modify: `services/core/go.sum`
- Modify: `scripts/build-core.mjs`
- Modify: `scripts/build-core.test.mjs`
- Modify: `scripts/build-manifest-schema.mjs`
- Modify: `scripts/write-build-manifest.mjs`
- Modify: `scripts/write-build-manifest.test.mjs`
- Modify: `apps/desktop/forge.config.ts`
- Modify: `apps/desktop/tests/e2e/package-signature.test.mjs`
- Modify: `package.json`
- Modify: `pnpm-lock.yaml`
- Modify: `compliance/build/toolchain.lock.json`
- Modify: `.github/workflows/foundation-ci.yml`
- Modify: `.github/workflows/foundation-windows11-e2e.yml`

- [ ] Add failing Node tests requiring the vendoring script to accept only the pinned official SQLCipher release URL/checksum and the build script to emit a target-specific static library, headers, license, version, and resource hash.
- [ ] Implement reproducible vendoring/build around official SQLCipher 4.15.0 source; do not silently use a system SQLite/SQLCipher. Link through a small `database/sql` driver adapter and keep CGo flags in the build script, not ad hoc developer shell state.
- [ ] Add a Go integration test that creates a database, verifies `PRAGMA cipher_version`, writes data, closes it, proves the file lacks the SQLite plaintext header and sentinel text, rejects an ordinary SQLite reader, rejects a wrong key, and reopens with the correct key.
- [ ] Add tests for `cipher_integrity_check`, secure-delete settings, WAL/checkpoint behavior, crash cleanup, busy timeout, foreign keys, and fail-closed behavior when the cipher library/key is unavailable.
- [ ] Run `rtk pnpm exec node --test scripts/vendor-sqlcipher.test.mjs scripts/build-sqlcipher.test.mjs` and `rtk go test -tags tammy_sqlcipher ./services/core/internal/storage/sqlcipher/... -count=1`.
- [ ] Add macOS arm64 and Windows x64 CI jobs that build this exact driver and run the encrypted-file test before any domain migration work proceeds.
- [ ] Replace the packaged core's forced `CGO_ENABLED=0` path in `scripts/build-core.mjs` with target-specific `CGO_ENABLED=1`, `tammy_sqlcipher` build tags, pinned compiler/static-library paths, and tests rejecting absent/wrong-target inputs. Windows builds run on the Windows 11 x64 runner; do not claim a macOS CGo cross-build.
- [ ] Extend the build manifest, Forge resources, signature tests, and licence/resource checks with the SQLCipher version/library hash; verify the packaged core reports the pinned cipher and contains no ordinary SQLite fallback.
- [ ] Stop and amend this plan if either supported target cannot pass. Plain SQLite, OS-only file permissions, or a stale unofficial SQLCipher fork is not an acceptable fallback.
- [ ] Commit: `rtk git add third_party scripts services/core apps/desktop package.json pnpm-lock.yaml compliance/build .github/workflows && rtk git commit -m "build: pin encrypted sqlcipher storage"`.

### Task 4: Add migrations, repository discipline, and crash-safe jobs

**Files:**
- Create: `services/core/internal/storage/migrations/embed.go`
- Create: `services/core/internal/storage/migrations/0001_platform.sql`
- Create: `services/core/internal/storage/migrations/0001_platform_test.go`
- Create: `services/core/internal/storage/migrations/0002_ledger.sql`
- Create: `services/core/internal/storage/migrations/0002_ledger_test.go`
- Create: `services/core/internal/storage/sqlcipher/migrate.go`
- Create: `services/core/internal/storage/sqlcipher/migrate_test.go`
- Create: `services/core/internal/platform/jobs/runner.go`
- Create: `services/core/internal/platform/jobs/runner_test.go`
- Create: `services/core/internal/testkit/encrypted_workspace.go`
- Create: `services/core/internal/testkit/proto_commands.go`
- Create: `services/core/internal/revisions/repository.go`
- Create: `services/core/internal/revisions/repository_test.go`
- Create: `services/core/internal/organisations/evidence_repository.go`
- Create: `services/core/internal/organisations/evidence_repository_test.go`

- [ ] Write failing migration tests for a fresh encrypted workspace, each schema prefix, checksum mismatch, interrupted staged migration, rollback, and reopen of the original file.
- [ ] Define normalized platform tables for authenticated header-operation IDs, metadata, users, roles, sessions, TOTP/recovery state, attempt-journal anchors, idempotency, audit envelopes/mirror metadata, jobs/checkpoints, backup/restore evidence, bounded organisation-verification evidence objects, and migrations; define ledger tables for organisation, accounts, periods, opening conversions/items, journals/lines, tax facts, cash-flow facts, retained rule bundles/tax-code catalogue, and revisions.
- [ ] Implement an Organisations-owned verification-evidence repository storing ≤1 MiB PDF/JPEG/PNG bytes only inside SQLCipher with MIME/length/SHA-256/created-by metadata, immutable supersession links, tamper/hash tests, and no plaintext sidecar. This is a deliberately bounded foundation store; Slice 3 Documents remains owner of general document evidence.
- [ ] Enforce foreign keys, unique source type/ID/revision, unique account code per organisation, one direct reversal, balanced-posting commit guard, immutable posted rows, checked status values, and module table ownership.
- [ ] Implement copy-migrate-integrity-check-atomic-activate; never migrate the only encrypted file in place without a recoverable predecessor.
- [ ] Write a crash-safe job runner test proving lease election, checkpoint resume, cancellation before commit point, deterministic retries, and no sleep-based polling.
- [ ] Implement `testkit` helpers that build state via public Protobuf commands except in repository/migration boundary tests.
- [ ] Implement one monotonic `financial_revision` plus ledger, settlement, banking, tax-source, organisation-profile, and rule-bundle revisions now. Each relevant UoW increments financial revision exactly once; reads, rollbacks, and idempotent replays do not.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/storage/... ./services/core/internal/platform/jobs/... ./services/core/internal/organisations/... -count=1`.
- [ ] Commit: `rtk git add services/core/internal/storage services/core/internal/platform/jobs services/core/internal/testkit services/core/internal/revisions services/core/internal/organisations && rtk git commit -m "feat: add encrypted transactional persistence"`.

### Task 5: Implement workspace creation, key recovery, and local identity

**Files:**
- Create: `services/core/internal/workspace/service.go`
- Create: `services/core/internal/workspace/service_test.go`
- Create: `services/core/internal/workspace/repository.go`
- Create: `services/core/internal/workspace/keys.go`
- Create: `services/core/internal/workspace/keys_darwin.go`
- Create: `services/core/internal/workspace/keys_windows.go`
- Create: `services/core/internal/workspace/keys_test.go`
- Create: `services/core/internal/workspace/header.go`
- Create: `services/core/internal/workspace/header_test.go`
- Create: `services/core/internal/workspace/attempt_journal.go`
- Create: `services/core/internal/workspace/attempt_journal_test.go`
- Create: `services/core/internal/workspace/remembered_key.go`
- Create: `services/core/internal/workspace/remembered_key_test.go`
- Create: `compliance/passwords/tammy-common-passwords-v1.txt`
- Create: `compliance/passwords/SHA256SUMS`
- Create: `services/core/internal/identity/service.go`
- Create: `services/core/internal/identity/service_test.go`
- Create: `services/core/internal/identity/totp.go`
- Create: `services/core/internal/identity/totp_test.go`
- Create: `services/core/internal/identity/repository.go`
- Create: `services/core/internal/authorisation/policy.go`
- Create: `services/core/internal/authorisation/policy_test.go`

- [ ] Write service tests for create/15-minute recovery confirmation, explicit lock/unlock, wrong passphrase, restart, change passphrase, break-glass workspace/admin recovery, idle/session/OS-lock expiry, optional remember/forget/23h59 expiry, user activation/password/history/lockout, roles, TOTP enrollment/replay/freshness/cooldown/disable/reset, ownership transfer, and last-admin protection.
- [ ] Prove envelope semantics separately: `ActivateUser`, `ConfirmTOTP`, and `AssertTOTP` count every call as a fresh challenge attempt with no replay cache; `ChangePassword`, `EnrolTOTP`, and `DisableTOTP` require operation key/semantic hash and return the retained result on exact replay or `IDEMPOTENCY_CONFLICT` on changed input. A repeated enrol/disable cannot create a second factor/audit mutation.
- [ ] Add separate missing-factor, stale-marker, and newly asserted-factor tests for `ChangePassphrase`, `ResetUserAuthentication`, and ownership transfer; each high-risk command requires a new assertion and cannot reuse a marker reserved for another action.
- [ ] Take every service case through the micro-TDD loop using its narrow Go test; the packaged visible/named-preload red/green cycle is owned by Task 12 after the production composition root exists.
- [ ] Implement the exact password policy: Unicode NFC without trim, 15–128 code points/≤1,024 UTF-8 bytes, pinned 10,000-entry case-folded denylist, constant-time verification, Argon2id 64 MiB/3 iterations/parallelism 1/16-byte salt/32-byte result, prior five user-password and prior three workspace-passphrase verifiers.
- [ ] Implement a random 256-bit DEK wrapped by the passphrase KEK and a separate one-time-displayed grouped-Base32 256-bit recovery secret via HKDF-SHA-256/AES-256-GCM. Recovery sets a new passphrase; it is never an ordinary unlock and is not a set of single-use recovery codes.
- [ ] Implement the authenticated two-slot header with monotonic version/recovery operation ID and crash cases before database audit commit, before slot activation, and after activation; startup elects exactly one matching slot.
- [ ] Implement the optional remembered-workspace DEK item only after explicit consent, expiring within 23h59 and removed on passphrase/recovery/admin recovery/forget. It never creates or remembers an application-user session.
- [ ] Implement the chained HMAC attempt journal and exact five-attempt/15-minute cooldowns for workspace/recovery/TOTP plus pending-setup/user expiry rules; challenge RPCs are never automatically retried.
- [ ] Keep passphrases, TOTP secrets, recovery plaintext, SQLCipher keys, and ephemeral tokens out of Protobuf persistence, logs, crash reports, renderer state, and command-line arguments; zero mutable secret buffers where practical.
- [ ] Implement local users with `workspace_admin`, `business_preparer`, `business_lodger`, and `auditor` roles, one active session, 30-minute inactivity close/DEK zeroing, and centralized deny-by-default authorization.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/workspace/... ./services/core/internal/identity/... ./services/core/internal/authorisation/... -count=1`.
- [ ] Commit: `rtk git add services/core/internal/workspace services/core/internal/identity services/core/internal/authorisation compliance/passwords && rtk git commit -m "feat: add encrypted workspaces and local identity"`.

### Task 6: Implement audit chaining and command idempotency

**Files:**
- Create: `services/core/internal/audit/appender.go`
- Create: `services/core/internal/audit/appender_test.go`
- Create: `services/core/internal/audit/service.go`
- Create: `services/core/internal/audit/service_test.go`
- Create: `services/core/internal/audit/repository.go`
- Create: `services/core/internal/audit/verifier.go`
- Create: `services/core/internal/audit/verifier_test.go`
- Create: `services/core/internal/audit/mirror.go`
- Create: `services/core/internal/audit/mirror_darwin.go`
- Create: `services/core/internal/audit/mirror_windows.go`
- Create: `services/core/internal/audit/mirror_test.go`
- Create: `services/core/internal/audit/export.go`
- Create: `services/core/internal/audit/export_test.go`
- Create: `services/core/internal/audit/keys.go`
- Create: `services/core/internal/audit/keys_test.go`
- Create: `services/core/internal/audit/export_job.go`
- Create: `services/core/internal/audit/export_job_test.go`
- Create: `services/core/cmd/tammy-evidence-verify/main.go`
- Create: `services/core/cmd/tammy-evidence-verify/main_test.go`
- Create: `services/core/internal/idempotency/elector.go`
- Create: `services/core/internal/idempotency/elector_test.go`
- Create: `services/core/internal/idempotency/repository.go`

- [ ] Write tests for the exact domain-separated genesis/event formula, predecessor/length hashing, typed payload/schema fingerprint retention, actor/session/source/result metadata, concurrent appends, tamper localization, and exact binary/canonical-JSON preservation.
- [ ] Write mirror tests for database-ahead crash repair after full verification, mirror-ahead rollback denial, moved-workspace read-only mode, declined trust, and trust establishment through either passphrase + admin password + fresh TOTP or recovery proof + audited administrator break-glass before `WORKSPACE_MIRROR_ESTABLISHED` enables writes.
- [ ] Write signed-export tests for encrypted Ed25519 private key, header public key/ID, canonical events/manifest, selected evidence, hashes/signature, secret exclusion, and standalone verification inputs.
- [ ] Implement Audit export as a persisted job with operation/input hashes, checkpoint/progress/result ref, cancellation before verified destination rename, startup reconstruction, and destination-hash recovery; build the standalone verifier without database access.
- [ ] Write tests for first execution, exact replay returning stored bytes, same-key changed-request conflict, in-flight election, failed-command retry, workspace-lifetime retention, and replay after an unrelated compatible descriptor change.
- [ ] Implement audit append and idempotency election inside the caller-owned `TxScope`; neither package may commit independently.
- [ ] Persist semantic-hash algorithm version, fully-qualified request type, normalized hash, deterministic result bytes/type, outcome, created/completed instants, and command actor.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/audit/... ./services/core/internal/idempotency/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/audit services/core/internal/idempotency && rtk git commit -m "feat: enforce auditable idempotent commands"`.

### Task 7: Implement encrypted backup, staged restore, and crash recovery

**Files:**
- Create: `services/core/internal/backup/format.go`
- Create: `services/core/internal/backup/format_test.go`
- Create: `services/core/internal/backup/service.go`
- Create: `services/core/internal/backup/service_integration_test.go`
- Create: `services/core/internal/backup/job.go`
- Create: `services/core/internal/backup/job_test.go`
- Create: `services/core/internal/backup/provider_registry.go`
- Create: `services/core/internal/backup/provider_registry_test.go`
- Create: `services/core/internal/restore/journal.go`
- Create: `services/core/internal/restore/journal_test.go`
- Create: `services/core/internal/restore/service.go`
- Create: `services/core/internal/restore/service_integration_test.go`
- Create: `services/core/internal/restore/pre_restore_archive.go`
- Create: `services/core/internal/restore/pre_restore_archive_test.go`
- Create: `services/core/internal/restore/pre_restore_export_job.go`
- Create: `services/core/internal/restore/pre_restore_export_job_test.go`
- Create: `services/core/internal/restore/provider_registry.go`
- Create: `services/core/internal/restore/provider_registry_test.go`

- [ ] Write `tammy-backup-v1` tests for a consistent online SQLCipher snapshot, workspace header, current evidence/rules/artefacts, canonical signed manifest, schema/app/audit head and hashes, plus explicit exclusion of machine vault/passwords/remembered keys/sessions/RPC material/logs.
- [ ] Take each backup/restore restart case through the micro-TDD loop using its narrow Go integration test; the packaged visible/named-preload red/green cycle is owned by Task 12.
- [ ] Implement a random 256-bit archive key, chunked AES-256-GCM, and backup-specific Argon2id KEK using the password policy but not the workspace KEK/history; require approved destination and atomic verified rename.
- [ ] Write external restore-journal tests for fsync'd `PREPARED`, `STAGED`, `SWAPPED`, `COMPLETE`, changed-manifest idempotency conflict, and process death at every write/rename/fsync.
- [ ] Implement staged decrypt/signature/object/database/schema/audit/invariant verification, staged admin/TOTP or recovery proof, session/remembered-key invalidation, generation increment, exact `WORKSPACE_RESTORED` event, mirror update, and atomic swap/rollback.
- [ ] Implement encrypted `tammy-pre-restore-v1` retention with the restored DEK wrapping its archive key; verify it before deleting the rollback directory and retain it at least 12 months.
- [ ] Implement bounded admin-only `GetPreRestoreArchive`/`ListPreRestoreArchives` and export-job Get/List projections with signed stable cursors, archive IDs/versions/created-at/retention eligibility/hash/status and no decrypted bytes. They are the only discovery path after restart or multiple restores.
- [ ] Implement generated `ExportPreRestoreArchive` as a persisted restart-safe job and `DeletePreRestoreArchive` as an ordinary command. Both require an unlocked restored workspace, admin role/password, fresh TOTP, operation key and expected archive/job version. Export stages to an approved destination capability, records input/destination hashes and `QUEUED/WRITING/VERIFIED/COMPLETED/CANCELLED/FAILED_RETRYABLE`, permits cancellation only before verified atomic rename, fsyncs file/directory, then commits result/audit. Startup removes partial temps, verifies a completed destination hash before reconstructing success, or leaves an explicit retryable job requiring destination reapproval; SQL never spans the filesystem write. Delete rejects age under 12 months, missing reason, stale version, or active export, and appends audit/result atomically.
- [ ] Add `rtk go test -tags tammy_sqlcipher ./services/core/internal/restore -run '^TestPreRestoreArchiveCommands/(list_after_restart|multiple_restore_versions|export_authorized|export_wrong_password|export_stale_totp|cancel_before_rename|death_after_rename|destination_hash_recovery|delete_before_12_months|delete_after_12_months|replay_conflict)$' -count=1`, taking each named subtest red→green before the broad restore gate.
- [ ] Establish typed Backup/Restore provider registries now. Each module contributes an immutable projection plus validator through the composition root; the registries own ordering/version dispatch and forbid Backup/Restore from querying another module's tables.
- [ ] Prove wrong password/authentication/signature/schema/invariant failures leave active bytes unchanged and startup completes or reverses an interrupted swap.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/backup/... ./services/core/internal/restore/... -race -count=1`; all crash matrix cases pass.
- [ ] Commit: `rtk git add services/core/internal/backup services/core/internal/restore && rtk git commit -m "feat: backup and restore encrypted workspaces"`.

## Chunk 2: Organisation, accounting kernel, UI, and acceptance

### Task 8: Build the application composition root and generated service registration

**Files:**
- Create: `services/core/internal/app/composition.go`
- Create: `services/core/internal/app/composition_test.go`
- Create: `services/core/internal/app/command.go`
- Create: `services/core/internal/app/command_test.go`
- Create: `services/core/internal/transport/registrar.go`
- Create: `services/core/internal/transport/registrar_test.go`
- Modify: `services/core/internal/transport/server.go`
- Modify: `services/core/cmd/tammy-core/main.go`

- [ ] Write an integration test that boots the server with a real encrypted workspace and asserts every generated service is registered while an undeclared path returns not found.
- [ ] Refactor `server.go` so transport receives a registrar/composition root instead of constructing `SystemService` directly.
- [ ] Implement an ordinary-command coordinator that opens the UoW first, then performs actor authorization, unknown-field rejection, request hashing, idempotency election, domain/financial work, audit, deterministic result persistence, and one commit. Security challenges use attempt policies and restore uses its external journal; neither is forced through this coordinator.
- [ ] Add rollback tests that inject failure after source save, ledger posting, audit append, and result serialization and prove no partial state survives.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/app/... ./services/core/internal/transport/... ./services/core/cmd/tammy-core/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/app services/core/internal/transport services/core/cmd/tammy-core && rtk git commit -m "refactor: compose generated core services"`.

### Task 9: Implement organisation setup and the chart of accounts

**Files:**
- Create: `services/core/internal/organisations/domain.go`
- Create: `services/core/internal/organisations/service.go`
- Create: `services/core/internal/organisations/service_test.go`
- Create: `services/core/internal/organisations/repository.go`
- Create: `services/core/internal/organisations/read_port.go`
- Create: `services/core/internal/organisations/read_port_test.go`
- Create: `services/core/internal/organisations/verification.go`
- Create: `services/core/internal/organisations/evidence_intake_port.go`
- Create: `services/core/internal/organisations/evidence_intake_port_test.go`
- Create: `services/core/internal/organisations/ownership_service.go`
- Create: `services/core/internal/organisations/ownership_service_test.go`
- Create: `services/core/internal/accounting/accounts.go`
- Create: `services/core/internal/accounting/accounts_test.go`
- Create: `services/core/internal/accounting/account_repository.go`
- Create: `services/core/internal/accounting/service.go`
- Create: `services/core/internal/accounting/service_test.go`
- Create: `services/core/internal/accounting/templates/au_small_business_v1.json`
- Create: `services/core/internal/accounting/templates/template_test.go`
- Create: `services/core/internal/artefacts/rule_repository.go`
- Create: `services/core/internal/artefacts/rule_repository_test.go`
- Create: `services/core/internal/artefacts/bundles/au_gst_v1.pb.json`
- Create: `services/core/internal/artefacts/bundles/au_gst_v1.sha256`
- Create: `services/core/internal/taxrules/kernel.go`
- Create: `services/core/internal/taxrules/kernel_test.go`

- [ ] Write tests for exactly one organisation, ABN checksum, legal/display identity, timezone, AUD-only currency, GST registration/basis/effective dates, version conflicts, bounded encrypted entity-verification evidence/expiry/supersession/hash mismatch, ownership transfer, session invalidation, and unresolved-report impact rollback.
- [ ] Add missing/stale/new TOTP tests for identity, ABN/legal entity, GST basis/frequency, and rule-bundle changes in `UpdateOrganisation`; ordinary display/contact changes follow the approved lower-risk rule.
- [ ] Write account property/table tests for code uniqueness, type/normal-balance/report/cash-flow classification, archive/reactivate, and prohibition on deleting/repurposing/posting manually to every named control account.
- [ ] Implement the versioned Australian small-business template including receivable, payable, current/deferred/evidence/adjustment GST, earnings, retained earnings, bank, and opening-equity controls.
- [ ] Implement Artefacts-owned immutable rule bundles/tax-code catalogue plus the pure versioned TaxRules kernel. Accounting reads rules only through `ArtefactReadPort`; no mutable GST policy is owned by Accounting.
- [ ] Route `OrganisationService` and account commands through the common command pipeline; `RecordEntityVerification` accepts only a validated ≤1 MiB PDF/JPEG/PNG payload and metadata through `OrganisationEvidenceIntakePort`, retains its hash/immutable object atomically, and never accepts a caller path. Queries use signed cursor pagination and stable ordering.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/organisations/... ./services/core/internal/accounting/... ./services/core/internal/artefacts/... ./services/core/internal/taxrules/... -count=1`.
- [ ] Commit: `rtk git add services/core/internal/organisations services/core/internal/accounting services/core/internal/artefacts services/core/internal/taxrules && rtk git commit -m "feat: add organisation accounts and tax rules"`.

### Task 10: Implement journals, GST facts, and posting policies

**Files:**
- Create: `services/core/internal/accounting/journal.go`
- Create: `services/core/internal/accounting/journal_test.go`
- Create: `services/core/internal/accounting/posting_policy.go`
- Create: `services/core/internal/accounting/posting_policy_test.go`
- Create: `services/core/internal/accounting/posting_service.go`
- Create: `services/core/internal/accounting/posting_service_integration_test.go`
- Create: `services/core/internal/accounting/journal_repository.go`
- Create: `services/core/internal/accounting/tax_fact_repository.go`
- Create: `services/core/internal/accounting/tax_fact_repository_test.go`
- Create: `services/core/internal/accounting/cashflow.go`
- Create: `services/core/internal/accounting/cashflow_test.go`
- Create: `services/core/internal/accounting/invariants.go`
- Create: `services/core/internal/accounting/invariants_test.go`

- [ ] Start with property tests generating valid/invalid line sets and proving at least two lines, AUD currency, equal debits/credits, checked totals, active/postable accounts, open date, unique source revision, and immutable committed rows.
- [ ] Take each manual-journal/period case through the micro-TDD loop using its narrow Go integration test; Task 12 owns the packaged route/preload red/green cycle.
- [ ] Add GST examples for GST-free, input-taxed, taxable inclusive/exclusive, mixed lines, discounts, negative credit lines, half-cent boundaries, and stable remainder allocation.
- [ ] Implement versioned posting policies that accept typed `PostingIntent`; source workflows cannot pass arbitrary control-account lines.
- [ ] Implement non-cash current GST facts and cash-basis deferred facts with original/attributed/remaining gross/net/GST fields and source/rule provenance.
- [ ] Require every cash-account line to have immutable debit-positive `CashFlowFact` components whose sum equals the line. System posting policies classify deterministically; manual cash journals require explicit operating/investing/financing/transfer allocation and noncash exclusions.
- [ ] Implement manual journal posting with explicit debits/credits and tax treatments, stricter control-account rejection, typed failures, and audit events.
- [ ] Implement `AccountingPostingPort.Post/Reverse`, general-ledger and trial-balance queries, and invariant verification through repositories owned by Accounting.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/accounting/... -race -count=1` and inspect the property-test seed in any failure.
- [ ] Commit: `rtk git add services/core/internal/accounting && rtk git commit -m "feat: post balanced journals gst and cashflow facts"`.

### Task 11: Implement opening conversion and period controls

**Files:**
- Create: `services/core/internal/accounting/opening_conversion.go`
- Create: `services/core/internal/accounting/opening_conversion_test.go`
- Create: `services/core/internal/accounting/opening_conversion_service.go`
- Create: `services/core/internal/accounting/opening_conversion_integration_test.go`
- Create: `services/core/internal/accounting/period.go`
- Create: `services/core/internal/accounting/period_test.go`
- Create: `services/core/internal/accounting/period_service.go`
- Create: `services/core/internal/accounting/period_service_test.go`
- Create: `services/core/internal/sales/opening_port.go`
- Create: `services/core/internal/sales/opening_repository.go`
- Create: `services/core/internal/sales/opening_repository_test.go`
- Create: `services/core/internal/purchases/opening_port.go`
- Create: `services/core/internal/purchases/opening_repository.go`
- Create: `services/core/internal/purchases/opening_repository_test.go`
- Create: `services/core/internal/banking/opening_port.go`
- Create: `services/core/internal/banking/opening_repository.go`
- Create: `services/core/internal/banking/opening_repository_test.go`

- [ ] Write table/property tests for ordinary, AR, AP, unallocated credit, bank/credit-card, outstanding movement, opening-equity, prior-attributed non-cash GST, and cash-GST remainder equations.
- [ ] Take each opening-conversion/mismatch/replacement case through the micro-TDD loop using its narrow Go integration test; Task 12 owns the packaged route/preload red/green cycle.
- [ ] Prove mismatch, negative remainder, direct-control posting, duplicate posting, later-dependency replacement, and stale-version cases fail without partial rows.
- [ ] Prove `ReplaceOpeningConversion` requires a new fresh TOTP with distinct missing/stale/success cases in addition to version/dependency checks.
- [ ] Implement one staged opening-conversion aggregate and one atomic posting command. The orchestrator writes opening customer items through Sales' `OpeningReceivablePort`, supplier items through Purchases' `OpeningPayablePort`, and financial-account opening state through Banking's `OpeningFinancialAccountPort`, while Accounting owns the conversion/journal/tax facts. Later slices extend these repositories without moving rows across module ownership.
- [ ] Implement full reversal/replacement only when dependencies and period rules allow; never mutate original posted lines or facts.
- [ ] Write close/reopen tests for every blocked command class, fresh-TOTP requirement, mandatory reason, tax-impact callback, and linked correction/reversal limits.
- [ ] Implement periods and inject `TaxReportImpactPort` as a narrow interface with a no-report behavior for Slice 1; later Slice 5 replaces the adapter without changing Accounting.
- [ ] Run `rtk go test -tags tammy_sqlcipher ./services/core/internal/accounting/... -race -count=1`.
- [ ] Commit: `rtk git add services/core/internal/accounting services/core/internal/sales services/core/internal/purchases services/core/internal/banking && rtk git commit -m "feat: add opening conversion and period controls"`.

### Task 12: Add binary Protobuf IPC and the Slice 1 production UI

**Files:**
- Create: `apps/desktop/src/shared/proto-ipc.ts`
- Create: `apps/desktop/src/shared/proto-ipc.test.ts`
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/shared/preload-methods.json`
- Create: `apps/desktop/src/main/rpc-router.ts`
- Create: `apps/desktop/src/main/rpc-router.test.ts`
- Create: `apps/desktop/src/main/os-lock.ts`
- Create: `apps/desktop/src/main/os-lock.test.ts`
- Create: `apps/desktop/src/main/organisation-evidence-intake.ts`
- Create: `apps/desktop/src/main/organisation-evidence-intake.test.ts`
- Modify: `apps/desktop/src/main/index-lifecycle.ts`
- Modify: `apps/desktop/src/main/electron-lifecycle.test.ts`
- Modify: `apps/desktop/src/main/ipc.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Create: `apps/desktop/src/renderer/app-shell/app-shell.tsx`
- Create: `apps/desktop/src/renderer/app-shell/navigation.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/workspace-screen.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/workspace-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/security-screen.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/security-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/moved-trust-screen.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/moved-trust-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/ownership-screen.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/ownership-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/setup/setup-screen.tsx`
- Create: `apps/desktop/src/renderer/features/setup/organisation-settings-screen.tsx`
- Create: `apps/desktop/src/renderer/features/setup/organisation-settings-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/identity/users-screen.tsx`
- Create: `apps/desktop/src/renderer/features/identity/users-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/backup-screen.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/backup-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/restore-screen.tsx`
- Create: `apps/desktop/src/renderer/features/workspace/restore-screen.test.tsx`
- Create: `apps/desktop/src/renderer/features/ledger/accounts-screen.tsx`
- Create: `apps/desktop/src/renderer/features/ledger/opening-balances-screen.tsx`
- Create: `apps/desktop/src/renderer/features/ledger/journal-screen.tsx`
- Create: `apps/desktop/src/renderer/features/ledger/general-ledger-screen.tsx`
- Create: `apps/desktop/src/renderer/features/ledger/trial-balance-screen.tsx`
- Create: `apps/desktop/src/renderer/features/ledger/periods-screen.tsx`
- Create: `apps/desktop/src/renderer/features/audit/audit-screen.tsx`
- Create: `apps/desktop/src/renderer/features/ledger/ledger-workflows.test.tsx`
- Modify: `apps/desktop/src/renderer/app.tsx`
- Modify: `apps/desktop/src/renderer/styles.css`
- Create: `apps/desktop/tests/e2e/workspace.spec.ts`
- Create: `apps/desktop/tests/e2e/backup-restore.spec.ts`
- Create: `apps/desktop/tests/e2e/ledger-periods.spec.ts`
- Create: `apps/desktop/tests/e2e/opening-conversion.spec.ts`

- [ ] Write codec/router tests proving each channel accepts only its generated request bytes, enforces a per-method size limit, rejects unknown channels/fields, invokes only its generated Connect method, and returns only its generated response bytes or typed fault bytes.
- [ ] Implement and test the organisation-evidence picker in Electron main: accept only an OS-approved handle, validate magic/MIME and ≤1 MiB before creating the generated `RecordEntityVerification` request, expose only `recordEntityVerificationFromHandle`, and never reveal an unrestricted path or raw file API to the renderer.
- [ ] Replace handwritten request/response shapes at the accounting boundary with generated Protobuf types; keep Electron main as the sole Connect client and expose one named preload method per RPC.
- [ ] Write accessible React tests for locked, first-run, loading, empty, populated, filtered, paginated, validation, stale-version, permission-denied, offline, and retry/recovery states.
- [ ] Implement production navigation for Workspace, Setup, Chart, Opening Balances, Journals, General Ledger, Trial Balance, Periods, and Audit status. Every visible control must perform its real action or be omitted.
- [ ] Build the four packaged specs as vertical micro-cycles in this task. Add one visible workflow case, run its exact grep against the package and observe the missing route/preload/control assertion, implement the minimum codec/composition/UI path, rerun to pass, then continue: `E2E-00 workspace identity`, `E2E-00 backup restore`, `E2E-01 opening conversion`, and `E2E-14 ledger periods`. Include each spec in this task's commit.
- [ ] In the backup/restore vertical, add named preload/codec/UI controls for retained pre-restore archive list/status, approved-handle export, and explicit post-12-month deletion. Run `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep 'E2E-00 pre-restore evidence'` red on the missing control, implement it through the generated service only, and rerun to pass before committing Task 12.
- [ ] Use integer-string input parsing and generated messages; render minor units with `Intl.NumberFormat` only after accounting calculations are complete.
- [ ] Preserve CSP, context isolation, no Node renderer access, and no generic RPC/network primitive.
- [ ] Wire Electron `powerMonitor` suspend and OS session-lock events through `os-lock.ts` to the production `lockWorkspace` method; prove session invalidation, SQLite close, DEK zeroing, idempotent repeated events, and clean listener disposal.
- [ ] Run `rtk pnpm --filter @tammy/desktop test`, `rtk pnpm typecheck`, `rtk pnpm lint`, and fix every accessibility/query ambiguity uncovered by tests.
- [ ] Commit: `rtk git add apps/desktop && rtk git commit -m "feat: add secure ledger desktop workflows"`.

### Task 13: Prove Slice 1 in the signed packaged application

**Files:**
- Modify: `apps/desktop/tests/e2e/workspace.spec.ts`
- Modify: `apps/desktop/tests/e2e/opening-conversion.spec.ts`
- Modify: `apps/desktop/tests/e2e/ledger-periods.spec.ts`
- Modify: `apps/desktop/tests/e2e/backup-restore.spec.ts`
- Create: `apps/desktop/tests/e2e/offline-network-guard.ts`
- Create: `scripts/run-offline-e2e-darwin.mjs`
- Create: `scripts/run-offline-e2e-darwin.test.mjs`
- Create: `scripts/run-offline-e2e-windows.ps1`
- Create: `scripts/sign-windows.mjs`
- Create: `scripts/sign-windows.test.mjs`
- Create: `apps/desktop/tests/e2e/support/accounting-fixtures.ts`
- Create: `apps/desktop/tests/e2e/support/workspace-driver.ts`
- Create: `apps/desktop/tests/e2e/support/runtime-coverage.ts`
- Create: `apps/desktop/tests/e2e/support/runtime-coverage.test.ts`
- Create: `compliance/evidence/core-accounting/slice-1-runtime-coverage.json`
- Create: `test/fixtures/accounting/opening-conversion.pb.json`
- Create: `test/fixtures/restore/aged-pre-restore.tammy-backup`
- Create: `test/fixtures/restore/aged-pre-restore.manifest.json`
- Create: `scripts/generate-pre-restore-age-fixture.mjs`
- Create: `scripts/generate-pre-restore-age-fixture.test.mjs`
- Create: `tools/restore-fixture/main.go`
- Create: `tools/restore-fixture/main_test.go`
- Create: `compliance/traceability/core-accounting.csv`
- Create: `compliance/evidence/core-accounting/manifest.json`
- Create: `scripts/check-core-accounting-evidence.mjs`
- Create: `scripts/check-core-accounting-evidence.test.mjs`
- Modify: `apps/desktop/playwright.config.ts`
- Modify: `apps/desktop/tests/e2e/fixtures.ts`
- Modify: `apps/desktop/forge.config.ts`
- Modify: `apps/desktop/tests/e2e/package-signature.test.mjs`
- Modify: `.github/workflows/foundation-windows11-e2e.yml`
- Modify: `package.json`

- [ ] Extend the Task 12 packaged `E2E-00` vertical cases: create workspace, record recovery material, create organisation/users/roles, lock/unlock, wrong passphrase, TOTP gate, restart, offline startup, and persistence.
- [ ] Add trace-labelled E2E-00 happy/failure cases for every mapped RPC: passphrase change with missing/stale/new TOTP; workspace/admin recovery and cooldowns; remember/expiry/forget; moved trust decline plus normal/recovery proofs; activation/change/reset password; assign/list users; TOTP enrol/confirm/assert/replay/disable; ownership transfer; organisation get/update/verification with approved encrypted evidence; audit list/verify/export/cancel/restart; backup get/list/cancel/restart; restore status and changed-key conflict. Explicitly execute the four declared role outcomes and every principal failure from coverage. `runtime-coverage.ts` records canonical `{caseId, fullyQualifiedRpc, actorRole, outcomeCode}` tuples at the generated-client boundary; its test/checker compares the sorted observed file byte-for-byte with tuples expanded from `coverage.yaml`, rejecting missing, extra, duplicate, or scenario-only claims.
- [ ] Write preliminary runtime tuples/results only to the run's untracked temporary evidence directory; materialize `slice-1-runtime-coverage.json` and the tracked manifest only after the reviewed clean-source commit and descriptor evidence generation.
- [ ] Write packaged `E2E-01`: install chart, stage/post ordinary plus AR/AP/bank openings and cash-GST remainder, reject every mismatch, inspect public projections, and exercise allowed full replacement.
- [ ] Require `ReplaceOpeningConversion` missing/stale/new TOTP cases and every account/query list state in E2E-01.
- [ ] Write the Slice 1 part of `E2E-14`: balanced/unbalanced manual journals, close/reopen, closed-period denial, reversal link, stale version, rapid double-submit replay, and changed-request idempotency conflict.
- [ ] Add backup/restore acceptance to E2E-00: create encrypted backup, restore into a fresh installation directory, prove wrong passphrase leaves active bytes unchanged, then compare organisation/journal/opening/GST/audit public projections and the linked restore generation.
- [ ] Build `aged-pre-restore.tammy-backup` only with the unshipped `tools/restore-fixture` composition: inject its frozen Clock dependency (production composition always binds the real clock and exposes no clock RPC/flag), create/restore/export through public generated commands at a time 13 months before the fixture as-of, and retain source/tool/command/descriptor/schema/archive/audit hashes in the manifest. Random DEKs/archive keys/nonces/salts/UUIDs/recovery/TOTP values remain cryptographically random, so regeneration is not byte-identical: the generator test creates a fresh semantically equivalent encrypted fixture, validates the same public projections/age/transition/invariants and provenance schema, separately verifies the retained fixture's fixed byte hashes/signatures, and rejects direct repository/SQL mutation or any fixture-clock symbol/fixed secret in the packaged app.
- [ ] Through public packaged RPCs, List/Get multiple retained archives/jobs after restart; test a newly created archive's under-12-month deletion denial, then restore the signed/encrypted aged fixture into a fresh installation and delete its eligible retained archive with admin password/fresh TOTP/reason. Exercise export create/status/cancel/restart/destination-hash recovery plus exact replay/changed conflict and all four role outcomes; record every generated-client tuple with zero repository/UI bypass.
- [ ] Add public-projection assertions that journal debits equal credits, trial balance equals journal totals, control accounts equal opening subledgers, and all GST controls equal tax facts.
- [ ] Add an evidence checker test that fails on an unsigned app, wrong descriptor/schema/fixture hash, absent scenario/traceability row, plaintext workspace, unexpected listener, or incomplete logs.
- [ ] Add root `core-accounting:evidence` as `node scripts/check-core-accounting-evidence.mjs`; support `--slice N` and require the exact retained slice manifest/results.
- [ ] Run `rtk pnpm exec node --test scripts/generate-pre-restore-age-fixture.test.mjs && rtk go test ./tools/restore-fixture/...`, then `rtk pnpm contracts && rtk pnpm lint && rtk pnpm typecheck && rtk pnpm test`.
- [ ] Change Playwright `testMatch` from `foundation.spec.ts` to all `*.spec.ts` and add a config unit test that fails if the four new specs are undiscovered or skipped.
- [ ] Implement `offline-network-guard.ts` to deny/record Electron session requests and use the platform firewall/sandbox layer to reject DNS/TCP/UDP/HTTP/WebSocket except the capability-authenticated core endpoint; any attempt fails the run.
- [ ] Implement the macOS harness with a temporary deny-all `pf` anchor and explicit core-loopback allow rule, requiring test-runner privileges and guaranteed cleanup; implement the Windows 11 harness with temporary Firewall outbound/loopback block rules scoped to the app/core executables and guaranteed cleanup. Observe the unmodified signed production app/core at the OS layer; use separate external DNS/TCP/UDP probe executables as negative controls proving the deny rules work. Add no test RPC, core hook, production control path, or in-process network probe. Absent privilege blocks the release gate.
- [ ] Configure Windows package signing with the runner-provided certificate identity, run `signtool verify /pa /all` on installer/app/core, and extend the cross-platform signature test. The native document helper does not exist in Slice 1 and is signed/contained only when Slice 3 introduces it. Unsigned or test-only output cannot satisfy release evidence.
- [ ] Run `rtk pnpm package && rtk pnpm test:e2e:packaged -- --grep "E2E-00|E2E-01|E2E-14"` and require the real bundled CGo/SQLCipher core, encrypted database, signatures, zero unexpected listeners, zero network attempts, and zero required skips.
- [ ] Quit, relaunch from Finder, repeat unlock/trial-balance/audit checks, and confirm no detached core process remains.
- [ ] Run `rtk pnpm exec node --test scripts/check-core-accounting-evidence.test.mjs`; retain preliminary package results outside tracked evidence until the clean source commit exists.
- [ ] Request independent code review; resolve all critical/important findings, rerun affected gates, and commit the reviewed source without retained descriptor/result evidence: `rtk git add apps/desktop test tools/restore-fixture compliance/traceability scripts package.json .github/workflows/foundation-windows11-e2e.yml && rtk git commit -m "test: accept secure ledger and gst slice"`.
- [ ] From that clean source commit run `TAMMY_SOURCE_REVISION=$(rtk git rev-parse HEAD) rtk pnpm proto:descriptors:evidence`; verify the manifest's subject revision equals `rtk git rev-parse HEAD`, rerun the macOS package/E2E and same-subject Windows 11 gate, write the Slice 1 evidence manifest/runtime tuples, and run `rtk pnpm core-accounting:evidence -- --slice 1` expecting `slice 1 evidence verified`.
- [ ] Commit only retained evidence: `rtk git add compliance/contracts compliance/evidence/core-accounting && rtk git commit -m "build: retain slice 1 accounting evidence"`.

## Slice 1 exit gate

- [ ] All Slice 1 RPCs, transitions, roles, replay/conflict paths, typed failures, and list states are present in `test/e2e/coverage.yaml` and pass its checker.
- [ ] The supported target builds prove SQLCipher encryption and fail closed; no plaintext fallback or secret leakage exists.
- [ ] E2E-00, E2E-01, and Slice 1 E2E-14 pass from the signed packaged app against real generated clients, core, key store, and encrypted storage.
- [ ] Ledger, opening subledger, and GST cross-projection invariants pass before/after restart.
- [ ] Buf, generated-tree, Go race, renderer, accessibility, packaging, signature, process-lifecycle, and evidence checks are green.
- [ ] The evidence manifest retains source, descriptor, schema, SQLCipher, fixture, artefact, and test-result hashes.
