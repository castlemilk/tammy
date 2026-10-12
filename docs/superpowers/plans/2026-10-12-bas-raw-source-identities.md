# BAS Raw Source Identities Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents are available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Retain noncanonical legacy source identities and their nullable original provenance throughout BAS calculation coverage, findings, lineage, and evidence without fabricating canonical hashes or changing accepted migration 0013 data.

**Architecture:** Keep `SourceRef` strict and wire-compatible. Add a BAS-local raw identity message that stores the exact source row’s type and ID, preserves nullable original revision/hash, and carries a distinct computed proof hash plus proof scope. Persist raw coverage in a new immutable companion table and merge it with canonical rows by one shared sequence and one shared bound; canonical-only historical calculations continue to read from migration 0013 unchanged.

**Tech Stack:** Protocol Buffers and Buf validation, generated Go/TypeScript contracts, Go 1.26.4 under mise, SQLite/SQLCipher immutable migration 0014, deterministic protobuf frames, SQLCipher native integration tests.

---

## File map

- `proto/tammy/v1/bas_preparation.proto` defines raw source identities, source XOR, ordered mixed lineage/evidence, and raw validation-finding sources.
- `services/core/internal/gen/tammy/v1/bas_preparation.pb.go` and `packages/connect-client/src/gen/tammy/v1/bas_preparation_pb.ts` are generated outputs; regenerate them through the pinned Buf task, never by hand.
- `services/core/internal/contracts/bas_preparation_contract_test.go` and `services/core/internal/contracts/bas_preparation_semantics_test.go` pin wire numbers, requiredness, XOR, bounds, and compatibility behavior.
- `services/core/internal/storage/migrations/0014_bas_raw_source_identities.sql` adds the raw companion table and cross-table guards. `0013_bas_preparation.sql` is immutable and must retain SHA-256 `1fd46bdded18cdb399eeca9f2dbacebdf3cd904f1488cefabdc3694a94d309ed`.
- `services/core/internal/storage/migrations/embed.go` embeds migration 0014. Migration inventory/count tests in `0001_platform_test.go`, `0003_audit_idempotency_test.go`, `0007_sbr_readiness_test.go`, `0008_invoices_test.go`, and `0009_invoice_branding_test.go` advance their expected migration list/count.
- `services/core/internal/storage/sqlcipher/migrate_test.go` checks the schema-14 target and new table. `bas_preparation_migration_test.go` verifies 13-to-14 upgrade, unchanged canonical rows, reopen, and backup/restore.
- `services/core/internal/baspreparation/calculation_repository.go` validates and writes the mutually exclusive source variants, writes both tables in one sequence, and verifies a merged full-body read. `validation_repository.go` validates ordered finding identities while retaining the existing canonical projection.
- `services/core/internal/baspreparation/calculation_repository_test.go` and `validation_repository_test.go` cover mixed round trips, old-frame compatibility, bounds, corruption, rollback, and exact raw provenance. Add a focused `calculation_raw_identity_test.go` only if these suites become difficult to review.
- `services/core/internal/app/local_composition_sqlcipher_integration_test.go`, `services/core/internal/workspace/storage_upgrade_test.go`, and `services/core/internal/localproduct/storage_upgrade_sqlcipher_integration_test.go` assert that current application opens select schema 14.

## Contract decisions

- Preserve `BasSourceCoverage.source` as an ordinary `SourceRef` field 1 so its generated Go API remains unchanged. Remove only its required-field annotation and add ordinary `BasRawSourceRef raw_source = 10`; enforce `has(source) != has(raw_source)` with a Buf CEL rule and the repository validator. `SourceRef` itself remains unchanged and continues to require canonical UUIDv7, positive revision, and a 32-byte content hash.
- `BasRawSourceRef` stores `type`, opaque nonblank `id`, optional `original_revision`, optional `original_content_hash`, `computed_proof_hash`, and an explicit proof-scope enum. Do not impose an invented fixed raw-ID length such as 128 or 256 bytes. Bound the full identity by the existing finite protobuf frame and shared capture payload limits and return the repository's typed bound error before writes. Optional original fields are copied only from that exact source row. A SQL NULL remains absent, never zero or an empty hash. The computed proof hash is always a separate 32-byte value and never fills an absent original hash.
- Proof scope distinguishes a source-row projection, a complete journal proof, and a reference-only projection. A reference-only proof does not claim the referenced document/rule body was loaded. Repository validation checks hash shape, scope, and equality against the retained normalized/body fields that are actually present. A producer-specific test recomputes a proof only when it has the exact committed source data; generic repository validation must not claim it can authenticate an opaque reference-only digest without its source body. Reject unspecified or unsupported scope values.
- Add `BasSourceIdentity` with ordinary fields `SourceRef canonical_source = 1` and `BasRawSourceRef raw_source = 10`; enforce their XOR with CEL and repository validation, preserving generated field accessors. Add ordered `lineage_identities = 11` and `evidence_identities = 12` to `BasSourceCoverage`. Existing `lineage = 6` and `evidence = 7` remain the canonical projections. If an ordered identity list is populated, its canonical projection must exactly equal the corresponding old list, including order and every hash/revision. If the new list is empty, the old canonical list remains the complete legacy representation. The combined canonical-plus-raw count is at most 20 for each list.
- Add ordered `repeated BasSourceIdentity source_identities = 6` to `BasValidationFinding`; retain canonical `sources = 5` as its exact ordered canonical projection. When the new list is empty, canonical sources remain the legacy representation. Limit the selected mixed list to 20 without counting its compatibility projection twice. Validation and finding protobuf frames continue to commit every field.
- A raw candidate identifies its own retained table row. For example, a `tax_facts` row with no original row revision/hash keeps both absent and has a separate module proof hash. Its underlying document/journal reference is retained separately in lineage and verified against the parent raw projection; never borrow the underlying source hash to make the tax-fact row look canonical.
- The canonical and raw coverage tables share one sequence, one maximum of 1,000 rows, and one 8 MiB identity payload budget for new mixed captures. Assign sequence after ordering the combined candidates, then partition rows by identity kind. Read with an ordered union and require exactly `1..N`; enforce owner, full protobuf, normalized scalar, presence, proof-scope, and content-hash equality for every row. Preserve the existing canonical-only wire/table path and its accepted 16 MiB coverage-frame read limit for historical schema-13 frames. The new 8 MiB limit applies to newly written mixed/raw captures; a merged reader must select the legacy bound for canonical-only retained frames rather than rejecting accepted history. Do not insert dummy canonical rows or sentinel revisions/hashes.

## Chunk 1: Add the contract without weakening canonical provenance

### Task 1: Define and generate raw identity contracts

**Files:**
- Modify: `proto/tammy/v1/bas_preparation.proto`
- Generate: `services/core/internal/gen/tammy/v1/bas_preparation.pb.go`
- Generate: `packages/connect-client/src/gen/tammy/v1/bas_preparation_pb.ts`
- Test: `services/core/internal/contracts/bas_preparation_contract_test.go`
- Test: `services/core/internal/contracts/bas_preparation_semantics_test.go`

- [ ] **Step 1: Add contract tests first.** Pin existing field numbers and types, canonical `SourceRef` strictness, field-1/field-10 XOR, raw optional-field presence, proof scopes, ordered-list canonical projection equality, compatibility when ordered lists are empty, and combined 20-item limits. Include a non-UUID legacy ID longer than 256 bytes that still fits the established frame/capture budget, plus an over-budget typed failure case, and a tax-fact example with absent original revision/hash plus a distinct computed proof hash.
- [ ] **Step 2: Run the contract RED.** Run `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test ./services/core/internal/contracts -run 'TestBAS(Raw|SourceVariants|Coverage|ValidationFinding)' -count=1`. Root owns every Go command; the missing raw messages/fields should be the only expected compile or contract failure.
- [ ] **Step 3: Add the additive proto schema.** Keep all existing field numbers, types, and ordinary accessors. Add `raw_source = 10`, ordered wrapper lists, and CEL XOR/projection constraints. Add comments defining the difference between original nullable provenance and computed proof, and between source-row, complete-journal, and reference-only proof scopes. Do not change `common.proto` `SourceRef` validation.
- [ ] **Step 4: Format, lint, and generate.** Run `rtk proxy mise exec -- pnpm proto:format`, `rtk proxy mise exec -- pnpm proto:lint`, and `rtk proxy mise exec -- pnpm proto:generate`; inspect the Go and TypeScript diffs and confirm old field numbers are unchanged.
- [ ] **Step 5: Pass contract checks.** Root runs `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test ./services/core/internal/contracts -count=1` and `rtk proxy mise exec -- pnpm contracts`. Require the contract package to pass and generated-output checks to report no drift.

### Task 2: Add immutable migration 0014 and prove upgrade preservation

**Files:**
- Create: `services/core/internal/storage/migrations/0014_bas_raw_source_identities.sql`
- Modify: `services/core/internal/storage/migrations/embed.go`
- Modify: `services/core/internal/storage/migrations/0001_platform_test.go`
- Modify: `services/core/internal/storage/migrations/0003_audit_idempotency_test.go`
- Modify: `services/core/internal/storage/migrations/0007_sbr_readiness_test.go`
- Modify: `services/core/internal/storage/migrations/0008_invoices_test.go`
- Modify: `services/core/internal/storage/migrations/0009_invoice_branding_test.go`
- Modify: `services/core/internal/storage/sqlcipher/migrate_test.go`
- Modify: `services/core/internal/storage/sqlcipher/bas_preparation_migration_test.go`

- [ ] **Step 1: Add migration REDs.** Assert version 14 includes the raw companion table and its guards, while schema 13 does not. Build a version-13 database containing canonical BAS coverage; include a canonical-only coverage frame strictly above 8 MiB and within the accepted 16 MiB limit, and snapshot every canonical row, including sequence, all normalized candidate/disposition/amount/currency/date/classification/reason fields, canonical lineage/evidence frames, nullable values, protobuf bytes, content hashes, calculation parent, and migration ledger before upgrading.
- [ ] **Step 2: Run migration REDs.** Root runs `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/storage/migrations ./services/core/internal/storage/sqlcipher -run 'Migration.*(Raw|Coverage|Prefix)|BasRaw' -count=1`; require failure because migration 0014/table are absent.
- [ ] **Step 3: Create the companion table.** Store the exact five-column calculation parent tuple `(calculation_id, organisation_id, family_id, calculation_version, calculation_hash)`, shared sequence, every normalized candidate/disposition/amount/currency/date/classification/reason field, ordered lineage/evidence frame columns, raw identity scalars with SQL-nullable original revision/hash, computed proof hash/scope, full coverage protobuf, and row content hash. Keep optional original SQL values genuinely nullable; do not use a sentinel. Add a composite foreign key to the calculation row, SQLite type/length checks consistent with the full bounded frame, and primary/unique constraints for the raw row's calculation/version/sequence. Do not add an independent short ID cap or alter migration 0013.
- [ ] **Step 4: Add immutable and cross-table guards.** Add no-update/no-delete triggers and a same-table conflicting-insert guard to the raw table, so `INSERT OR REPLACE` cannot silently replace an immutable raw row. Add insert guards on both the raw table and the existing canonical table so an `INSERT OR REPLACE` cannot duplicate a calculation/version/sequence across kinds. Keep the pre-existing canonical table and triggers otherwise intact.
- [ ] **Step 5: Embed migration 0014 and update inventories.** Append the migration to `embed.go` and update migration names/counts. Extend SQLCipher expected tables and supported prefixes with target 14. Add the 13-to-14 assertion that canonical snapshots and migration rows through 13 are byte-for-byte unchanged; verify relational integrity and that no synthetic source/hash rows appear.
- [ ] **Step 6: Verify rollback, reopen, and restore.** Test failed 0014 application leaves the database at version 13 with no partial table/ledger row. Upgrade a real encrypted fixture to 14, close/reopen it, and round-trip online backup/restore; require canonical historical frames to remain identical and readable.

## Chunk 2: Persist mixed coverage with one identity and one sequence

### Task 3: Validate, write, and read mixed canonical/raw coverage

**Files:**
- Modify: `services/core/internal/baspreparation/calculation_repository.go`
- Modify: `services/core/internal/baspreparation/calculation_repository_test.go`
- Create if needed: `services/core/internal/baspreparation/calculation_raw_identity_test.go`

- [ ] **Step 1: Write mixed repository REDs.** Save and read an ordered manifest containing canonical and raw candidate rows, canonical and raw lineage/evidence, and raw validation finding identities. Assert exact order, both table placements, exact nullable SQL provenance, exact underlying lineage, computed proof hash distinct from original content hash, and deterministic full protobuf/hash round-trip.
- [ ] **Step 2: Write boundary and rollback REDs.** Cover both source variants unset/set, canonical plus raw both set, raw type/ID/revision/hash/proof-scope invalidity, a long noncanonical journal ID accepted within the shared budget, an over-budget ID rejected with a typed bound error, combined lineage/evidence/finding item count 20 and 21, combined 1,000/1,001 candidates, and the shared 8 MiB/one-byte-over payload. Assert bound failures leave no canonical or raw rows and preserve caller-owned earlier work after cancellation or savepoint rollback.
- [ ] **Step 3: Implement preparation and insert.** Normalize one ordered coverage list before partitioning. Validate each selected identity, raw nullable presence, proof scope, and all nested ordered references. Calculate one shared row count/byte budget; insert canonical and raw rows with consecutive shared sequence values in the existing caller transaction/savepoint. Keep old canonical-only `SourceRef` inputs and generated accessors valid.
- [ ] **Step 4: Implement merged verification.** Count and preflight both tables together, then read an ordered union. Keep the accepted 16 MiB bound for canonical-only historical frames, applying the new 8 MiB bound to mixed/raw captures. Require row sequence `1..N`, correct calculation/org/family/version/hash owner, no duplicate identity/sequence, exact SQL NULL-vs-value presence, full protobuf equality with the parent calculation, canonical projection equality for newly populated ordered lists, and recomputed stored-row content hashes. Legacy canonical-only calculations with empty new identity lists must retain their old interpretation and original rows. Explicitly read the schema-13 fixture above 8 MiB and within 16 MiB after the upgrade to prove that accepted history remains readable.
- [ ] **Step 5: Verify repository behavior.** Root runs `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/baspreparation -run 'CalculationRepository.*(Coverage|Raw|Identity)' -count=1` and the same command with `-race`. Require the mixed round-trip, bounds, malformed bodies, SQL NULL presence, and rollback tests to pass.

### Task 4: Preserve raw identities in validation findings and old frames

**Files:**
- Modify: `services/core/internal/baspreparation/validation_repository.go`
- Modify: `services/core/internal/baspreparation/validation_repository_test.go`
- Modify if shared validation helpers require it: `services/core/internal/baspreparation/profile_repository.go`

- [ ] **Step 1: Add finding REDs.** Retain ordered `source_identities` and the exact canonical `sources` projection through append, exact read, historical read, and encrypted reopen. Reject invalid identity XOR, bad proof scopes, normalized/body mismatches, and combined source count 21 without writes.
- [ ] **Step 2: Implement narrow raw validation.** Keep `validBASSourceRef` strict and unchanged. Add a BAS raw-reference validator for nonblank opaque identifiers bounded by existing frame/capture limits, optional revision preserving the actual signed SQL value where historical storage permits zero; never synthesize a positive revision, optional exact 32-byte original hash, mandatory exact 32-byte computed proof hash, and valid explicit proof scope. Validate every raw finding before serialization and again on read; check computed-hash shape and retained equality without claiming to recompute an opaque reference-only proof whose body is absent.
- [ ] **Step 3: Prove backward compatibility.** Read and re-marshal pre-0014 canonical-only calculation and validation fixtures without adding placeholder raw identities or changing source arrays. Ensure adding field 6 leaves older finding source field 5 semantics and wire number unchanged.
- [ ] **Step 4: Verify findings.** Root runs `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -tags tammy_sqlcipher ./services/core/internal/baspreparation -run 'ValidationRepository.*(Raw|Finding|Corrupt)' -count=1` and the same command with `-race`; require no partial append on rejected input.

## Chunk 3: Close upgrade, corruption, and integration paths

### Task 5: Exercise cross-table integrity and current schema selection

**Files:**
- Modify: `services/core/internal/baspreparation/calculation_repository_test.go`
- Modify: `services/core/internal/storage/sqlcipher/bas_preparation_migration_test.go`
- Modify: `services/core/internal/storage/sqlcipher/migrate_test.go`
- Modify: `services/core/internal/app/local_composition_sqlcipher_integration_test.go`
- Modify: `services/core/internal/workspace/storage_upgrade_test.go`
- Modify: `services/core/internal/localproduct/storage_upgrade_sqlcipher_integration_test.go`

- [ ] **Step 1: Test hostile retained-state corruption.** Inject, through test-only trigger removal where required, a sequence gap, cross-table duplicate sequence, missing or extra raw row, wrong calculation owner/hash, malformed full protobuf, changed normalized scalar, SQL NULL converted to zero/empty, unknown proof scope, changed row hash, and raw body whose canonical projection disagrees with old arrays. Every exact read must fail closed without returning a partial calculation.
- [ ] **Step 2: Test database guards directly.** Verify raw updates/deletes and same-table conflicting inserts are rejected, including `INSERT OR REPLACE`. Both canonical-to-raw and raw-to-canonical duplicate sequence inserts must fail, including `INSERT OR REPLACE`. Confirm foreign-organisation calculation tuples cannot be inserted.
- [ ] **Step 3: Update current application schema fixtures.** Advance only current-target assertions to version 14 and include the companion table in schema-14 inventories; retain explicit version-13 prefix/upgrade tests. Do not rewrite old fixture migration history or accepted checksums.
- [ ] **Step 4: Verify native integration.** Root runs `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/storage/migrations ./services/core/internal/storage/sqlcipher ./services/core/internal/baspreparation ./services/core/internal/contracts ./services/core/internal/app ./services/core/internal/workspace ./services/core/internal/localproduct -count=1`. Require schema-13 compatibility, schema-14 selection, mixed reopen/backup, corruption rejection, and cross-table guards to pass.

### Task 6: Run final contracts, Go, build, and diff gates

**Files:** all files listed above; no additional production scope.

- [ ] **Step 1: Verify migration 0013 is untouched.** Run `rtk proxy shasum -a 256 services/core/internal/storage/migrations/0013_bas_preparation.sql` and require `1fd46bdded18cdb399eeca9f2dbacebdf3cd904f1488cefabdc3694a94d309ed`.
- [ ] **Step 2: Run generated contract gates.** Run `rtk proxy mise exec -- pnpm contracts`; require Buf format, lint, breaking check, Go/TypeScript generation, and contract drift check to pass.
- [ ] **Step 3: Run affected native race gates.** With Go 1.26.4 from `mise.toml`, root runs `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/storage/migrations ./services/core/internal/storage/sqlcipher ./services/core/internal/baspreparation ./services/core/internal/contracts ./services/core/internal/app ./services/core/internal/workspace ./services/core/internal/localproduct -count=1`.
- [ ] **Step 4: Run default Core and production SQLCipher build.** Root runs `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test ./services/core/... -count=1` and `rtk proxy mise exec -- pnpm core:build`; the latter is the pinned production build owner that supplies vendored SQLCipher flags and resources.
- [ ] **Step 5: Review and commit the bounded slice.** Run `rtk proxy git diff --check`; review generated and SQL diffs, confirm no source-hash borrowing or changes to migration 0013, then commit only the raw identity contract, migration, repository, and test files after all root-owned gates pass.

## Execution constraints

- Repository shell commands use `rtk proxy`. Run tools through the pinned `mise` environment (`go = 1.26.4`, `node = 24.18.0` in `mise.toml`). Use `TMPDIR=/private/tmp GOFLAGS=-p=1` for Go commands on the supported macOS native SQLCipher target.
- Root owns all Go commands, `gofmt`, tests, and builds. A delegated editor must not run them; it may prepare tests and source changes for root’s review.
- This plan covers additive BAS raw-identity persistence and read integrity only. It does not claim completion of source capture or any wider reporting-programme acceptance work.
