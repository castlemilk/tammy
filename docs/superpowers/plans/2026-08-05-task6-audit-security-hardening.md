# Task 6 Audit Security Hardening Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every Task 6 production-completeness, integrity, concurrency, key-lifecycle, bounded-resource, and migration finding while preserving the approved encrypted local-first audit behavior.

**Architecture:** Keep Task 8's global composition root untouched, but make Task 6 expose concrete SQL/typed-port adapters and safe factories. Move audit verification/export to bounded iterators and immutable registries, make cryptographic identity and key history self-verifying, and fence all state transitions with SQL/CAS semantics. Each numbered reviewer finding is a separate RED→GREEN chunk; the only commit operation is the user-requested final amend of the existing Task 6 commit.

**Tech Stack:** Go 1.26, ConnectRPC-generated handlers, protobuf deterministic encoding, SQLCipher/SQLite, Ed25519/XChaCha20-Poly1305, deterministic ZIP, Darwin Keychain, Windows Credential Manager.

---

## Chunk 1: Constructible production adapters

**Files:**
- Create: `services/core/internal/audit/production.go`
- Test: `services/core/internal/audit/production_integration_test.go`
- Modify: `services/core/internal/audit/mirror.go`
- Modify: `services/core/internal/audit/export_worker.go`

- [ ] Add a failing encrypted-workspace integration test that constructs a SQL full-chain verifier, typed trust proof verifier, typed evidence provider registry, opaque destination resolver, and the closest generated Audit/Workspace handler boundary without test-only verifier/provider/destination fakes.
- [ ] Run the focused tagged test and confirm missing concrete constructors/handler produce RED.
- [ ] Implement `SQLMirrorVerifier` over caller-provided read transactions using bounded chain verification.
- [ ] Implement trust proof and evidence provider adapters over narrow typed ports; validate every returned actor/proof/evidence object.
- [ ] Implement UUIDv7 capability registry and a bounded atomic filesystem destination that never accepts caller paths, enforces base-directory containment, uses restrictive files, fsync, and same-directory rename.
- [ ] Expose a Task 6 factory that wires these components into `Service`, `TrustCoordinator`, `MirrorReconciler`, and `EvidenceExportWorker` without registering the Task 8 process composition root.
- [ ] Run production-boundary integration test GREEN.

## Chunk 2: Cancellation transition fencing

**Files:**
- Modify: `services/core/internal/audit/export_job.go`
- Modify: `services/core/internal/audit/service.go`
- Test: `services/core/internal/audit/export_job_test.go`
- Test: `services/core/internal/audit/service_test.go`

- [ ] Add RED tests for cancellation after `DESTINATION_COMMITTING`, cancellation after completion, commit/cancel election races, and exact replay.
- [ ] Change cancellation SQL to update only cancellable states/stages and return the transitioned job.
- [ ] On zero rows, reload once and return a typed `COMMIT_ALREADY_COMPLETED`/version conflict without writing idempotency completion or audit events.
- [ ] Append cancellation audit/idempotency completion only after a real transition.
- [ ] Run focused job/service tests GREEN and race them.

## Chunk 3: Immutable descriptor registry and historical schemas

**Files:**
- Create: `services/core/internal/audit/descriptors.go`
- Modify: `services/core/internal/audit/export.go`
- Modify: `services/core/internal/audit/export_worker.go`
- Modify: `services/core/internal/storage/migrations/0003_audit_idempotency.sql`
- Test: `services/core/internal/audit/export_test.go`
- Test: `services/core/internal/audit/export_job_test.go`

- [ ] Add RED compatible-evolution tests with events under two descriptor fingerprints, deterministic archive membership, exact replay, and standalone verification.
- [ ] Add immutable SQL descriptor storage keyed by SHA-256 fingerprint and a resolver/registry API that rejects noncanonical or hash-mismatched descriptor sets.
- [ ] Resolve every event fingerprint during snapshot verification and archive each referenced set under `descriptors/<hex>.pb` in fingerprint order.
- [ ] Validate each payload against its matching descriptor set during build and standalone verification.
- [ ] Run descriptor/export integration tests GREEN.

## Chunk 4: Unlock-scoped revocable DEK leases

**Files:**
- Create: `services/core/internal/audit/key_lease.go`
- Modify: `services/core/internal/audit/export_worker.go`
- Test: `services/core/internal/audit/export_job_test.go`

- [ ] Add RED tests proving workers store no raw DEK, acquire once per run, zero released key bytes, and fail after lock/revoke/close.
- [ ] Define `WorkspaceKeyLeaseProvider` and revocable lease interfaces with exact 32-byte transient copies.
- [ ] Remove `DEK []byte` from worker config/state; acquire immediately before archive signing and defer zero/release on all paths.
- [ ] Run lifecycle and memory-zero tests GREEN and under the race detector.

## Chunk 5: Ordered commit-to-mirror publication

**Files:**
- Modify: `services/core/internal/audit/appender.go`
- Modify: `services/core/internal/audit/mirror.go`
- Test: `services/core/internal/audit/appender_integration_test.go`
- Test: affected workspace/identity integration tests

- [ ] Add a RED SQLCipher test with two real caller-owned mirrored transactions whose post-commit callbacks drain out of order.
- [ ] Add a per-workspace publisher that serializes callback reconciliation, reloads the committed SQL head, fully verifies monotonic history, and CAS-advances only verified predecessors/equal heads.
- [ ] Ensure an older callback observing a newer committed verified head succeeds idempotently; rollback/ahead/divergence remains fail-closed.
- [ ] Run audit, workspace, and identity concurrency tests GREEN under `-race`.

## Chunk 6: Bind protobuf payload type into event hash

**Files:**
- Modify: `services/core/internal/audit/appender.go`
- Modify: `services/core/internal/audit/verifier.go`
- Modify: `services/core/internal/audit/repository.go`
- Modify: `proto/tammy/v1/audit.proto` and generated outputs if the canonical version marker is persisted
- Test: `services/core/internal/audit/appender_test.go`
- Test: `services/core/internal/audit/verifier_test.go`
- Test: `services/core/internal/audit/appender_integration_test.go`

- [ ] Add RED golden tests showing identical payload bytes/JSON under different FQ protobuf type URLs produce different hashes and independent OpenSSL/SHA-256 vectors match.
- [ ] Define one unambiguous pre-release canonical envelope version containing type URL, schema fingerprint, canonical payload JSON, and the existing metadata exactly once.
- [ ] Store/reload the type identity needed for deterministic verification and reject mismatched payload oneofs/types.
- [ ] Update verifier and migration compatibility for the single pre-release formula; reject ambiguous legacy/new mixes.
- [ ] Run golden, verifier, and SQLCipher tests GREEN.

## Chunk 7: Verifiable signing-key rotation chain

**Files:**
- Modify: `services/core/internal/audit/keys.go`
- Modify: `services/core/internal/audit/export.go`
- Modify: `services/core/internal/audit/workspace_bootstrap.go`
- Modify: `services/core/internal/storage/migrations/0003_audit_idempotency.sql`
- Test: `services/core/internal/audit/keys_integration_test.go`
- Test: `services/core/internal/audit/export_test.go`

- [ ] Add RED tests for tampered, missing, reordered, and forked rotation chains plus an old event exported under the current signer.
- [ ] Rotate in one caller transaction: persist successor/proof, one-way retire predecessor, CAS-update authenticated chain header key anchor, and append the rotation audit event.
- [ ] Load retained public key history from the immutable root through the active signer.
- [ ] Archive a deterministic root anchor and every public rotation proof; verify key IDs and every cross-signature before manifest signature/event acceptance.
- [ ] Run rotation/archive tests GREEN.

## Chunk 8: Bounded streaming verification/export

**Files:**
- Modify: `services/core/internal/audit/repository.go`
- Modify: `services/core/internal/audit/verifier.go`
- Modify: `services/core/internal/audit/service.go`
- Modify: `services/core/internal/audit/export.go`
- Modify: `services/core/internal/audit/export_worker.go`
- Test: `services/core/internal/audit/service_paging_test.go`
- Test: `services/core/internal/audit/export_test.go`

- [ ] Add RED large-chain tests with allocation ceilings and stable pagination no-gap/no-repeat assertions.
- [ ] Add ordered SQL event iterators/checkpoints over a fixed generation/sequence/head snapshot.
- [ ] Verify the full chain once in bounded pages, apply filters only after verification, and query the requested page directly instead of rematerializing history per page.
- [ ] Enforce member count/cumulative sizes before copying provider data; stream deterministic stored ZIP members without a whole-history object map.
- [ ] Add bounded ZIP reads/writes and reject overflow before allocation.
- [ ] Run large synthetic and paging tests GREEN.

## Chunk 9: Current migration integrity constraints

**Files:**
- Modify: `services/core/internal/storage/migrations/0003_audit_idempotency.sql`
- Modify: exact migration SHA fixture/manifest
- Test: `services/core/internal/storage/migrations/0003_audit_idempotency_test.go`
- Test: `services/core/internal/storage/sqlcipher/migrate_test.go`

- [ ] Add RED integration tests for header rollback/non-CAS updates, key mutation/deletion/reactivation, and two active keys.
- [ ] Add monotonic/CAS header triggers, signing-key insert/update/delete triggers permitting only one-way retirement, and a partial unique active-key index per workspace.
- [ ] Update only the unshipped 0003 exact digest; leave 0001/0002 bytes unchanged.
- [ ] Run migration tests GREEN.

## Final verification and amend

- [ ] Run exact audit/idempotency focused race tests.
- [ ] Run affected workspace/identity race tests.
- [ ] Run default and `tammy_sqlcipher` core suites, vets, contract/lint/typecheck/test commands.
- [ ] Cross-compile Windows/amd64 with `CGO_ENABLED=0` and readonly modules; make no Windows execution claim.
- [ ] Confirm `git diff --check`, inspect the complete diff, amend only the existing Task 6 commit, and confirm a clean worktree/new SHA.
