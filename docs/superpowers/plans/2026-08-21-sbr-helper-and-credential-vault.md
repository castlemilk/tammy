# SBR Helper and Credential Vault Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a local-only, least-privilege SBR helper boundary and RAM machine-credential vault that can be exercised safely with synthetic credentials now and can accept an issued macOS credential later without redesign.

**Architecture:** The Go core owns authorization, server-derived workspace/organisation/ABN binding, audit, signed-profile verification, component staging, durable transmission state, and redacted credential metadata. A separate `tammy-sbr-helper` process is the only process that opens/parses the chooser-selected credential file and the only process that accesses the Keychain vault or Product IDs. Core forwards a bounded path operation without reading credential bytes. Simulator composition has no network client and runs with an OS policy that denies network. EVTE remains unavailable until an authenticated ATO component bundle and registration manifest are present. SQLCipher stores only opaque bindings and non-secret metadata.

**Tech Stack:** Go 1.26, ConnectRPC/protobuf edition 2023, SQLCipher, macOS Security framework via a narrow adapter, stdio framed protocol, Ed25519/SHA-256/HMAC, existing audit/idempotency/identity composition patterns.

---

## Chunk 1: Helper protocol, secure storage, and core capability

### Task 1: Define the closed SBR contract

**Files:**
- Create: `proto/tammy/v1/sbr.proto`
- Modify: `buf.gen.yaml` only if generation configuration requires the new source automatically
- Modify: `packages/connect-client/package.json`
- Create: `services/core/internal/contracts/sbr_contract_test.go`
- Modify: `test/e2e/coverage.yaml`
- Modify: `scripts/slice-one-coverage-policy.mjs`
- Modify: `scripts/check-slice-one-coverage-policy.test.mjs`

- [ ] Add enums with no production member:
  - `SbrEnvironment`: `UNSPECIFIED`, `SIMULATOR`, `EVTE`;
  - `SbrReadinessState`: `UNAVAILABLE`, `READY_FOR_SIMULATOR`, `READY_FOR_EVTE_PRE_CONFORMANCE`, `READY_FOR_EVTE_POST_CONFORMANCE`;
  - `MachineCredentialState`: `MISSING`, `PRESENT`, `INACCESSIBLE`, `INCOMPATIBLE`, `REVOKED`, `EXPIRED`, `ABN_MISMATCH`.
- [ ] Add exactly nine workspace-authenticated RPCs:
  - `GetSbrReadiness`
  - `ImportMachineCredential`
  - `GetMachineCredentialStatus`
  - `UnlockMachineCredential`
  - `ReplaceMachineCredential`
  - `RemoveMachineCredential`
  - `ImportSbrProductId`
  - `RemoveSbrProductId`
  - `RunSbrReadinessFixture`
- [ ] Import/replace contain one absolute `selected_local_path` supplied only by trusted Electron main, an optional `security_scoped_bookmark` bounded to 64 KiB and permitted only for MAS, plus a password bounded to 1 KiB. Core forwards these to helper without opening credential bytes. Renderer/preload never receive path/bookmark. Responses expose only redacted metadata. Descriptor tests prove bookmark/path occur only in import/replace and never in responses.
- [ ] Product-ID import carries a bounded transient value plus exact EVTE product/service scope from trusted app UI to core/helper; helper alone stores it. Status returns only `PRESENT|MISSING|INACCESSIBLE` and no Product-ID-derived fingerprint outside the authenticated doctor view; evidence exports none.
- [ ] Public reads contain only `AuthenticationContext`; mutations contain only one `CommandContext`, using its existing `fresh_factor`. Do not accept workspace ID, organisation ID, ABN, opaque scope, or a second fresh-factor field from renderer/main. Core derives all binding values server-side and includes them only in its authenticated helper envelope.
- [ ] Cap selected path at 4 KiB, password and Product ID at 1 KiB, protocol messages at 1 MiB, and all strings/repeated fields with explicit Buf validation.
- [ ] Do not add submit/lodge RPCs. `RunSbrReadinessFixture` accepts the fixed simulator fixture ID plus a closed failure-case enum; it contains no live organisation/ABN/BAS/report inputs.
- [ ] Add descriptor tests proving exactly nine methods, no `PRODUCTION` symbol, no endpoint URL or credential-byte field, and all bounds/auth contexts. Assert the path field occurs only in import/replace and is not exported in any response.
- [ ] Run `rtk mise exec -- go test ./services/core/internal/contracts -run Sbr -count=1` and confirm RED.
- [ ] Generate Go/TS outputs, add the TS package export, and immediately add all nine RPCs to `test/e2e/coverage.yaml` as `declared_future`, with policy assertions in `scripts/slice-one-coverage-policy.mjs` and `scripts/check-slice-one-coverage-policy.test.mjs`. Promote only the methods exercised by later packaged E2E.
- [ ] Run the contract tests GREEN and `rtk mise exec -- pnpm contracts`.
- [ ] Commit: `feat: define local SBR contracts`

### Task 2: Build the bounded protobuf helper protocol before the helper

**Files:**
- Create: `services/sbr-helper/go.mod`
- Create: `services/sbr-helper/internal/protocol/frame.go`
- Create: `services/sbr-helper/internal/protocol/frame_test.go`
- Create: `services/sbr-helper/internal/protocol/messages.go`
- Create: `services/sbr-helper/internal/protocol/messages_test.go`
- Create: `services/core/internal/sbrhelper/protocol.go`
- Create: `services/core/internal/sbrhelper/protocol_test.go`
- Create: `test/fixtures/sbr/helper-protocol-v1.bin`
- Modify: `go.work`

- [ ] Add `./services/sbr-helper` to `go.work` before running root-pattern helper tests.
- [ ] Use `protowire` directly: a 4-byte big-endian length followed by one canonical message. Closed request operations are `STATUS=1`, `UNLOCK=2`, `FIXTURE=3`, `PREPARE_MUTATION=4`, `COMMIT_MUTATION=5`, `ABORT_MUTATION=6`, `RECONCILE_MUTATION=7`; mutation kinds are `IMPORT_CREDENTIAL=1`, `REPLACE_CREDENTIAL=2`, `REMOVE_CREDENTIAL=3`, `IMPORT_PRODUCT_ID=4`, `REMOVE_PRODUCT_ID=5`. Exact fields are protocol version, UUIDv7 request/operation IDs, Unix-millisecond deadline, core-authored scope, mutation kind, selected path/bookmark only for credential import/replace, transient password/Product-ID only where relevant, authenticated endpoint-profile bytes only for EVTE, and closed simulator case. Responses contain request ID, closed outcome, redacted result, stable error code, and pending-item ID only.
- [ ] Decoder rejects unknown/duplicate/out-of-order fields, non-minimal varints, invalid UTF-8, forbidden operation-field combinations, and any re-encoding unequal to input. Remove all JSON parsing/key logic from this protocol.
- [ ] Create one cross-package golden binary fixture and assert byte-for-byte parity from helper and core encoders.
- [ ] Reject zero/oversized lengths, trailing bytes, invalid IDs, expired deadlines, unknown operations/mutation kinds, and more than one in-flight request.
- [ ] Ensure parser errors are stable codes and never echo payload bytes.
- [ ] Add ownership/non-aliasing tests for every byte slice crossing the boundary.
- [ ] Run `rtk mise exec -- go test ./services/sbr-helper/internal/protocol ./services/core/internal/sbrhelper -run Protocol -count=1`; confirm RED because packages/fixture are absent. Implement minimally and rerun for GREEN.
- [ ] Commit: `feat: define bounded SBR helper protocol`

### Task 3: Implement the helper with a network-impossible simulator

**Files:**
- Create: `services/sbr-helper/cmd/tammy-sbr-helper/main.go`
- Create: `services/sbr-helper/cmd/tammy-sbr-helper/main_test.go`
- Create: `services/sbr-helper/internal/runner/runner.go`
- Create: `services/sbr-helper/internal/runner/runner_test.go`
- Create: `services/sbr-helper/internal/simulator/adapter.go`
- Create: `services/sbr-helper/internal/simulator/adapter_test.go`
- Create: `test/fixtures/sbr/simulator-readiness-v1.json`
- Create: `services/sbr-helper/internal/evte/adapter_unavailable.go`
- Create: `services/sbr-helper/internal/platform/network_darwin.go`
- Create: `services/sbr-helper/internal/platform/network_darwin_test.go`
- Create: `services/sbr-helper/internal/platform/bookmark_darwin.go`
- Create: `services/sbr-helper/internal/platform/bookmark_darwin_test.go`

- [ ] Define narrow injected ports: credential signer, component client, clock, random source, and dialer.
- [ ] Retain exact canonical fixture bytes at `test/fixtures/sbr/simulator-readiness-v1.json` for `SIM-SBR-READINESS-V1`: `Wattle & Co Test Pty Ltd`, ABN `11 000 000 560`, service `SIM.READINESS.0001`, clock `2026-06-30T00:00:00Z`, message `SIM.MSG.0001`, conversation `SIM.CONV.0001`, receipt `SIM-READY-0001`. Pin its SHA-256 in tests and include it in the semantic idempotency hash. Cover exact replay and cases `ACCEPTED`, `NOT_STARTED`, `MAYBE_SENT`, `MALFORMED_RESPONSE`, `HELPER_DEATH`, `TIMEOUT`.
- [ ] Construct simulator composition without `net`, `net/http`, or DNS client imports and with an injected dialer that returns exactly `SBR_SIMULATOR_NETWORK_FORBIDDEN`.
- [ ] Define the macOS file/network authority exactly:
  - MAS uses Electron `securityScopedBookmarks: true`; core forwards bookmark bytes (<=64 KiB) beside the path; helper resolves and starts/stops the resource through a Darwin cgo NSURL bridge. Parent/helper share pinned App Sandbox, user-selected read-only, and Keychain/app-group authority; helper has no network client/server entitlement.
  - development and ordinary non-MAS package launch the helper through `/usr/bin/sandbox-exec -f <core-owned-0600-profile>`; the profile permits the exact selected file, staged helper/component root, inherited stdio, and required test Keychain service while denying all network. Path is inside the temporary profile file, never argv, and core removes it after child start/exit. Failure to install either policy is `SBR_HELPER_SANDBOX_UNAVAILABLE`.
- [ ] Make EVTE adapter return `SBR_COMPONENT_UNAVAILABLE` until an authenticated component implementation is compiled in later; it must not call generic HTTP/TLS.
- [ ] Close stdin/parent death terminates the helper; malformed frames fail closed; stdout contains frames only; stderr contains bounded code-only lifecycle JSON.
- [ ] Add tests for bookmark resolution/start/stop, sandbox profile escaping and exact allowlist, no credential path in argv/logs, `go list -deps` forbidden network packages, signed entitlements, continuous PID socket sampling, buffer zeroing, and clean exit.
- [ ] Run `rtk mise exec -- go test -race ./services/sbr-helper/... -count=1`; confirm RED before implementation and GREEN after.
- [ ] Commit: `feat: add network-disabled SBR helper`

### Task 4: Authenticate and stage helper/component resources

**Files:**
- Create: `services/core/internal/sbrprofile/profile.go`
- Create: `services/core/internal/sbrprofile/profile_test.go`
- Create: `services/core/internal/sbrprofile/registration.go`
- Create: `services/core/internal/sbrprofile/registration_test.go`
- Create: `services/core/internal/sbrprofile/component.go`
- Create: `services/core/internal/sbrprofile/component_test.go`
- Create: `services/core/internal/sbrprofile/files_darwin.go`
- Create: `services/core/internal/sbrprofile/files_darwin_test.go`
- Create: `services/core/internal/sbrhelper/launcher.go`
- Create: `services/core/internal/sbrhelper/launcher_darwin.go`
- Create: `services/core/internal/sbrhelper/launcher_unsupported.go`
- Create: `services/core/internal/sbrhelper/launcher_test.go`
- Modify: `services/core/cmd/tammy-core/main.go`
- Modify: `services/core/cmd/tammy-core/main_test.go`
- Modify: `apps/desktop/forge.config.ts`
- Modify: `scripts/write-build-manifest.mjs`
- Modify: `scripts/write-build-manifest.test.mjs`
- Modify: `scripts/build-manifest-schema.mjs`
- Create: `scripts/build-manifest-schema.test.mjs`
- Create: `scripts/build-sbr-helper.mjs`
- Create: `scripts/build-sbr-helper.test.mjs`
- Modify: `package.json`
- Modify: `apps/desktop/scripts/find-packaged-app.mjs`
- Modify: `apps/desktop/scripts/find-packaged-app.test.mjs`
- Modify: `apps/desktop/tests/e2e/package-signature.test.mjs`
- Modify: `apps/desktop/release/macos/profile.ts`
- Modify: `apps/desktop/src/main/release-profile.test.ts`
- Create: `apps/desktop/release/macos/entitlements.mas.sbr-helper.plist`
- Modify: `taskfiles/ci.yml`
- Modify: `scripts/windows-sqlcipher-workflow.test.mjs`
- Create: `scripts/verify-sbr-helper-signature.mjs`
- Create: `scripts/verify-sbr-helper-signature.test.mjs`
- Create: `services/core/internal/sbrprofile/files_unsupported.go`
- Create: `services/core/internal/sbr/module_unsupported.go`
- Create: `services/core/internal/sbr/windows_accounting_contract_test.go`

- [ ] In Go, independently parse RFC 8785 canonical profile/registration/component/endpoint JSON with exact-key and duplicate-key rejection, verify pinned Ed25519 trust roots, enforce simulator `NONE` fields, dates/target, exact component file list, exact profile↔registration↔component↔endpoint hashes, and the approved first-code precedence before any side effect. Node preflight is advisory only.
- [ ] Write tests for opening profile/helper/component inputs with `O_NOFOLLOW`, checking regular-file type, current user ownership, non-writable group/world mode, size bounds, stable descriptor identity, exact hashes, signature, target, and dates.
- [ ] Stage authenticated bytes into a core-owned `0700` runtime directory: helper `0500`, component executables `0500`, non-executables `0400`; use exclusive creation, fsync, and rehash before executing only the staged helper.
- [ ] Pass authenticated endpoint-profile bytes over the framed protocol; helper must never reopen an endpoint path.
- [ ] Add a tested target-conditional `sbr-helper:build` owner. On `darwin/arm64` it builds the helper into `apps/desktop/resources/sbr-helper/darwin-arm64/` and records its hash; on `win32/x64` it emits an authenticated `SBR_UNAVAILABLE_ON_TARGET` manifest entry and no helper resource. Core/package owners invoke it conditionally. Include helper source and Go module checksums in Darwin manifest provenance.
- [ ] In packaged EVTE validation, require the helper designated requirement to contain the application Team ID and identifier `com.tammy.desktop.sbr-helper`; sign it with helper-specific App Sandbox entitlements and no network client/server entitlement. Simulator development accepts only the exact repository test hash/profile.
- [ ] Extend core CLI with one exact `--sbr-profile` absolute path accepted only beside `--data-root`; reject duplicates, relative paths, symlinks, or unsupported target. Do not accept credential/Product ID/endpoint args.
- [ ] Package the helper and signed profile inputs as explicit conditional Electron resources. Verification keys are embedded in the core binary; any human-readable key copy is diagnostic provenance only and never consulted. Build manifest records only `sbr_status` plus helper/profile fingerprints and cannot authorize SBR.
- [ ] Extend Forge resources, packaged locator/verifier, release signing owner, signature tests, build-manifest schema/writer, and package tests conditionally for Darwin. The three unsupported files leave accounting available while SBR returns `UNSUPPORTED_SBR_TARGET:<target>`. Extend the existing Windows PowerShell sequence to run `mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/storage/sqlcipher/... ./services/core/internal/sbr ./services/core/internal/app ./services/core/cmd/tammy-core -count=1` with an immediate `$LASTEXITCODE` guard; update its workflow contract. Add tamper/swap/symlink/TOCTOU/signature/designated-requirement/entitlement tests.
- [ ] Run `rtk mise exec -- go test ./services/core/internal/sbrprofile ./services/core/internal/sbrhelper ./services/core/cmd/tammy-core -count=1` and `rtk mise exec -- node --test scripts/build-sbr-helper.test.mjs scripts/build-manifest-schema.test.mjs scripts/write-build-manifest.test.mjs scripts/verify-sbr-helper-signature.test.mjs scripts/windows-sqlcipher-workflow.test.mjs apps/desktop/scripts/find-packaged-app.test.mjs`; capture RED before implementation, then GREEN.
- [ ] Commit: `feat: authenticate SBR helper resources`

### Task 5: Add the helper-owned Keychain vault seam

**Files:**
- Create: `services/sbr-helper/internal/vault/vault.go`
- Create: `services/sbr-helper/internal/vault/vault_test.go`
- Create: `services/sbr-helper/internal/vault/keychain_darwin.go`
- Create: `services/sbr-helper/internal/vault/keychain_darwin_test.go`
- Create: `services/sbr-helper/internal/vault/unsupported.go`
- Create: `services/sbr-helper/internal/vault/product_id.go`
- Create: `services/sbr-helper/internal/vault/product_id_test.go`

- [ ] Define helper-only primitives `StageCreate`, `StageReplace`, `StageDelete` (pending tombstone for credential or Product ID), `PendingStatus`, `Promote`, `Abort`, `ReadMetadata`, and `Unlock`. All five public mutations use `PREPARE_MUTATION` → staged item/tombstone → core SQL/audit commit → `COMMIT_MUTATION`; failure uses `ABORT_MUTATION`; restart uses `RECONCILE_MUTATION` against core-owned operation state. Delete is never immediate before core commit.
- [ ] On first helper use, create random installation ID, installation key, and vault-wrapping secret in separate Keychain items. Apply an access-control/code requirement restricted to the signed helper identifier and Team ID; development uses a separately named test access group and never opens production items.
- [ ] Store credential/component records in a version-1 AES-256-GCM envelope using the 32-byte vault-wrapping secret, fresh 96-bit random nonce, and AAD `tammy-sbr-vault-v1 || installation_id || scope || record_kind || component_version`. On read, authenticate before parse; rotation stages a new envelope then promotes it through the same protocol. Zero wrapping-key copies, plaintext, password, and decrypted-key buffers after use. Product IDs use the same envelope with service scope in AAD.
- [ ] Derive the credential namespace as `HMAC-SHA256(installation_key, workspace_id || 0x00 || organisation_id || 0x00 || canonical_ABN)` and encode only the digest in Keychain account names.
- [ ] The helper opens the chooser path no-follow, validates owner/type/mode/size/stability, passes the original format to the synthetic/approved component, and stores the resulting component-compatible opaque record. Never persist the credential password; use it only for bounded import/unlock, then zero transient password/decrypted-key buffers.
- [ ] Store Product ID as a separate Keychain item scoped by installation, EVTE, product, and service. Only `PRESENT|MISSING|INACCESSIBLE` plus a non-reversible fingerprint leaves the port.
- [ ] Write fake-store tests for exact namespace separation, cross-workspace/org/ABN denial, create collision, replace/delete semantics, inaccessible store, copy ownership, buffer zeroing, and absence of secrets in errors/logs.
- [ ] Add a macOS integration test that uses a unique temporary Keychain service namespace, has the helper open a synthetic fixture path, validates/unlocks/replaces/deletes, and removes only its own items. Never use the user’s real RAM credential in automated tests.
- [ ] Run `rtk mise exec -- go test ./services/sbr-helper/internal/vault -count=1`; capture RED then GREEN. Run `rtk mise exec -- env TAMMY_SBR_KEYCHAIN_INTEGRATION=1 go test -tags tammy_sbr_keychain_integration ./services/sbr-helper/internal/vault -run Keychain -count=1` for the synthetic opt-in integration.
- [ ] Commit: `feat: add SBR credential vault`

### Task 6: Persist only credential bindings and readiness metadata

**Files:**
- Create: `services/core/internal/storage/migrations/0007_sbr_readiness.sql`
- Create: `services/core/internal/storage/migrations/0007_sbr_readiness_test.go`
- Modify: `services/core/internal/storage/migrations/embed.go`
- Modify: `services/core/internal/app/local_composition_sqlcipher.go`
- Modify: `services/core/internal/app/local_composition_sqlcipher_integration_test.go`
- Create: `services/core/internal/sbr/repository_sqlcipher.go`
- Create: `services/core/internal/sbr/repository_sqlcipher_test.go`
- Modify: `services/core/internal/backup/snapshot_source_sqlcipher.go`
- Modify: `services/core/internal/backup/snapshot_source_sqlcipher_integration_test.go`
- Modify: `services/core/internal/restore/sqlcipher_workspace.go`
- Modify: `services/core/internal/restore/sqlcipher_workspace_integration_test.go`

- [ ] Add tables only for non-secret credential binding metadata, authenticated profile/registration fingerprints, readiness transitions, and idempotency results. Store no credential bytes, password, Product ID, key reference path, endpoint URL, or private material.
- [ ] Bind every row to one workspace ID, organisation ID, canonical ABN, schema version, and immutable credential fingerprint. Add exact uniqueness/foreign-key/check constraints.
- [ ] Raise `localMigrationTarget` from 6 to 7 and update migration embedding/order tests.
- [ ] Add SQLCipher tests proving secrets are absent from every table/column and raw database byte scan; prove cross-workspace/org/ABN reads and mutations return not found/permission denied.
- [ ] Define the cross-process mutation protocol: core creates a durable `PREPARED` mutation with opaque operation ID; helper stages an owned pending Keychain item and returns redacted metadata; core commits binding/audit and sends `COMMIT`; helper atomically promotes and retires the old item. On failure core sends `ABORT`; on restart helper/core reconcile only owned pending IDs against core status without exposing secrets.
- [ ] Add exact durable simulator transport states `PREPARED`, `DISPATCHING`, `NOT_STARTED`, `MAYBE_SENT`, `RESPONSE_RECEIVED`, `ACCEPTED`, `FAILED`, `UNKNOWN`. Map cases exactly: pre-dispatch failure→`NOT_STARTED`; uncertain write/helper death/timeout→`MAYBE_SENT`; syntactically received response→`RESPONSE_RECEIVED` before validation; malformed response→`FAILED`; accepted response→`ACCEPTED`; startup orphaned `DISPATCHING|MAYBE_SENT`→`UNKNOWN`. Only `NOT_STARTED` may be explicitly retried with a new idempotency key; unknown is never resent. Same key/same semantic bytes returns the owned original without a new dispatch; same key/different bytes returns `IDEMPOTENCY_CONFLICT`.
- [ ] Add backup/restore tests proving vault contents are outside backup, restored opaque bindings are marked `REIMPORT_REQUIRED`, and no helper/Keychain access happens during archive restore.
- [ ] Add crash-point tests for every create/replace/delete mutation before stage, after stage, after SQL commit, and after helper commit; reconciliation must converge without resurrecting removed secrets or losing the old record before durable replacement.
- [ ] Run `rtk mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/storage/migrations ./services/core/internal/sbr ./services/core/internal/backup ./services/core/internal/restore -count=1`; confirm RED from missing migration/state handling, implement, and rerun GREEN.
- [ ] Commit: `feat: persist SBR credential bindings`

### Task 7: Implement authorization, audit, and SBR service

**Files:**
- Modify: `services/core/internal/authorisation/policy.go`
- Modify: `services/core/internal/authorisation/policy_test.go`
- Modify: `services/core/internal/app/local_composition_sqlcipher.go`
- Modify: `services/core/internal/app/local_composition_sqlcipher_integration_test.go`
- Create: `services/core/internal/sbr/service.go`
- Create: `services/core/internal/sbr/service_test.go`
- Create: `services/core/internal/sbr/module_sqlcipher.go`
- Create: `services/core/internal/sbr/module_sqlcipher_integration_test.go`
- Modify: `services/core/cmd/tammy-core/composition_local_sqlcipher.go`
- Modify: `proto/tammy/v1/audit.proto`
- Modify: `services/core/internal/audit/appender.go`
- Modify: `services/core/internal/contracts/audit_metadata_test.go`

- [ ] Extend `LocalModuleIdentity` with one transaction-scoped `AuthorizeWithin(..., authorisation.Action)` seam backed by current identity roles; retain existing administrator helpers. Add composition tests so business-lodger authorization is real and Windows/non-SBR accounting composition remains unaffected.
- [ ] Add deny-by-default actions for inspect/import/unlock/replace/remove/use/Product-ID management. Import requires workspace admin + fresh factor purpose `sbr_machine_credential_import`; unlock, replace, remove, and Product-ID changes use distinct purposes; inspect permits admin or business lodger; future use permits business lodger only.
- [ ] Core derives workspace ID from activation, loads organisation ID/ABN/current independent verification server-side, derives the opaque scope, and sends it to helper. It never trusts workspace/org/ABN supplied by renderer and never parses certificate bytes. Helper returns redacted component-validated ABN/expiry; core requires exact match before commit.
- [ ] Before any helper operation, require active session, current independent organisation verification, exact credential binding, authenticated current profile/registration, and appropriate role/fresh factor.
- [ ] Add closed audit payloads for credential imported/unlocked/used/failed/expired/replaced/removed/suspected-compromise, Product ID state changed, profile accepted/rejected, and helper fixture prepared/dispatching/completed/unknown. Payloads include fingerprints/status codes only.
- [ ] Implement `RunSbrReadinessFixture` only for simulator and only fixed synthetic data. Explicitly reject any BAS/report identifiers or EVTE without the authenticated component.
- [ ] Compose `SbrModule` beside `LedgerModule`, using `LocalWorkspaceActivation` and route activation pattern. Keep routes registered but unavailable before unlock.
- [ ] Add tests for every authorization matrix row, stale/consumed factor, replay/idempotency conflict, no-bump failures, cross-binding, current verification expiry, Product ID missing/inaccessible, import pending recovery, helper malformed/death/timeout, orphaned dispatch recovery without resend, and audit redaction.
- [ ] Add real-server SQLCipher integration through generated Connect clients for all nine RPCs, including helper fake assertions that core-authored scope—not public request data—is forwarded.
- [ ] Run `rtk mise exec -- go test ./services/core/internal/authorisation ./services/core/internal/sbr -count=1`; confirm RED from missing actions/service, implement, and rerun GREEN. Then run `rtk mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/sbr ./services/core/internal/app ./services/core/cmd/tammy-core -count=1` and `rtk mise exec -- pnpm contracts`.
- [ ] Commit: `feat: integrate local SBR readiness service`

### Task 8: Verify the helper/vault plan

**Files:**
- Modify if required: `docs/development/tech-state.md`

- [ ] Run `rtk mise exec -- go test -race ./services/sbr-helper/... -count=1`.
- [ ] Run `rtk mise exec -- go test -race ./services/core/internal/sbrprofile ./services/core/internal/sbrhelper ./services/core/internal/sbr -count=1`.
- [ ] Run `rtk mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/storage/migrations ./services/core/internal/sbr ./services/core/internal/app ./services/core/cmd/tammy-core -count=1`.
- [ ] Run `rtk mise exec -- pnpm contracts` and `rtk mise exec -- pnpm typecheck`.
- [ ] Run `rtk mise exec -- env TAMMY_SBR_KEYCHAIN_INTEGRATION=1 go test -tags tammy_sbr_keychain_integration ./services/sbr-helper/internal/vault -run Keychain -count=1`; do not import the user’s credential.
- [ ] Make signature verification profile-aware and test both branches. Run `rtk mise exec -- node --test scripts/verify-sbr-helper-signature.test.mjs`. For ordinary packaging, run `rtk mise exec -- pnpm desktop:package` then `rtk mise exec -- node scripts/verify-sbr-helper-signature.mjs --ordinary`; it resolves the helper inside the ordinary `.app`, verifies its authenticated hash/identifier/signature, confirms MAS-only App Sandbox/bookmark/app-group entitlements are absent, and pairs that result with the tested sandbox-exec deny-network profile. For the separately authorized MAS gate, run the existing signed candidate owner (`rtk mise exec -- task release:candidate` with its required Apple inputs), then `rtk mise exec -- node scripts/verify-sbr-helper-signature.mjs --mas`; only this branch requires Team ID parity, App Sandbox, user-selected read-only, pinned Keychain/app-group, and no network entitlement. Never apply MAS entitlements to the ordinary helper.
- [ ] On a Windows x64 runner run `rtk mise exec -- task ci:windows11`; require the expanded tagged SBR/app/core command and existing accounting/package workflow GREEN with no helper resource. On macOS, run only structural non-Darwin build-tag tests without claiming a Windows result.
- [ ] Run `rtk git diff --check`, `gofmt -d` on changed Go files, and scan tracked/generated artefacts for fixture passwords or private keys.
- [ ] Request a spec-compliance review and a security/quality review; resolve every Critical/Important finding before handing off this chunk.
