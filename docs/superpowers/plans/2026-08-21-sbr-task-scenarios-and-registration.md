# SBR Task Scenarios and Registration Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give operators one truthful Taskfile front door for local accounting, isolated fresh accounting, the network-disabled SBR simulator, EVTE readiness, diagnostics, tests, and evidence without accepting secrets through Task, environment, argv, logs, or repository files.

**Architecture:** Add an `sbr` Taskfile namespace that delegates to small Node preflight/evidence owners and the existing Electron launcher. Node validates only repository/host inputs; the Go core independently authenticates every profile, component, registration, endpoint, helper, credential-vault, and Product-ID decision before use. Full doctor checks run in the app after real unlock/sign-in, so Task never bypasses workspace authorization. Ordinary accounting remains the existing development application. EVTE stays fail-closed until Tammy receives and records the required DSP/product/component approvals; production is structurally absent.

**Tech Stack:** Go Task 3.52, Node.js ESM, Node test runner, YAML, RFC 8785 canonical JSON, Ed25519, Electron, existing `mise` and `pnpm` owners.

---

## Chunk 1: Scenario front door and signed readiness inputs

### Task 1: Lock the public Task graph with a failing contract

**Files:**
- Modify: `scripts/check-taskfiles.test.mjs`
- Test: `scripts/check-taskfiles.test.mjs`

- [ ] Add `taskfiles/sbr.yml` to `scenarioFiles` and add exact expected root references for:
  - `dev:accounting -> dev:launch`
  - `dev:accounting:fresh -> sbr:launch-accounting-fresh`
  - `dev:sbr:simulator -> sbr:launch-simulator`
  - `dev:sbr:evte -> sbr:launch-evte`
  - `sbr:doctor -> sbr:run-doctor`
  - `sbr:registration:check -> sbr:run-registration-check`
  - `test:sbr -> sbr:run-test`
  - `evidence:sbr -> sbr:run-evidence`
- [ ] Assert every public SBR task has the exact host guard and observable error `UNSUPPORTED_SBR_TARGET:<platform>/<arch>` unless the real target is `darwin/arm64`.
- [ ] Assert the ordinary `dev:accounting` alias retains the existing `darwin/arm64|win32/x64` SQLCipher guard.
- [ ] Add negative fixtures proving no SBR task command, variable, precondition, or summary accepts or prints a credential path, credential password, private key, Product ID, or endpoint URL.
- [ ] Extend the finite executable allowlist only for the exact new Node owners named below; do not add generic `node scripts/*` or shell allowances.
- [ ] Run `rtk mise exec -- node --test scripts/check-taskfiles.test.mjs`.
- [ ] Confirm RED because the new namespace and aliases do not exist.

### Task 2: Add exact Task scenarios

**Files:**
- Create: `taskfiles/sbr.yml`
- Create: `scripts/sbr-incomplete.mjs`
- Create: `scripts/sbr-incomplete.test.mjs`
- Modify: `Taskfile.yml`
- Modify: `taskfiles/dev.yml`
- Modify: `scripts/check-taskfiles.test.mjs`

- [ ] Include `taskfiles/sbr.yml` as namespace `sbr` in the root Taskfile.
- [ ] Add the four root development aliases and the four root SBR utility aliases from Task 1.
- [ ] Implement the SBR target guard as the existing pinned `mise exec -- node -e` style, using only `process.platform/process.arch`; no caller-controlled target override in shipped tasks.
- [ ] Keep ordered/mutating scenarios as sequential direct `task:` calls; do not use concurrent deps.
- [ ] Define exact implementation commands:
  - `sbr:launch-accounting-fresh`: `mise exec -- node scripts/launch-local-scenario.mjs accounting-fresh`
  - `sbr:launch-simulator`: `mise exec -- node scripts/launch-local-scenario.mjs sbr-simulator`
  - `sbr:launch-evte`: `mise exec -- node scripts/launch-local-scenario.mjs sbr-evte`
  - `sbr:run-doctor`: preflight with `mise exec -- node scripts/check-sbr-readiness.mjs doctor-preflight`, then launch the app at the authenticated doctor route so the core obtains workspace/session scope through real sign-in;
  - `sbr:run-registration-check`: `mise exec -- node scripts/check-sbr-readiness.mjs registration`;
  - `sbr:run-test`: exact focused commands added only after the helper/core/desktop plans land, followed by `mise exec -- pnpm contracts`;
  - `sbr:run-evidence`: `mise exec -- node scripts/write-sbr-evidence.mjs simulator`; EVTE evidence is created only by the authenticated in-app conformance flow after external inputs exist.
- [ ] Until prerequisites land, use these exact mappings: `launch-simulator -> scripts/sbr-incomplete.mjs simulator -> SBR_IMPLEMENTATION_INCOMPLETE:simulator`; `launch-evte -> ... evte -> ...:evte`; `run-doctor -> ... doctor -> ...:doctor`; `run-test -> ... test -> ...:test`; `run-evidence -> ... evidence -> ...:evidence`. The owner accepts only enum `simulator|evte|doctor|test|evidence`, exits non-zero, and never launches Electron or creates output. Contract tests execute all five public tasks.
- [ ] Make Task summaries state:
  - simulator is synthetic and network-disabled;
  - EVTE is non-production and requires signed external inputs;
  - no live credential is accepted by Task;
  - current BAS remains preparation-only with no submit/lodge action;
  - production is unavailable.
- [ ] Run the focused Taskfile contract and confirm GREEN.
- [ ] Run `rtk mise exec -- task --list` and all new `--summary` commands; confirm they render without evaluating a secret or launching Electron.
- [ ] Commit: `feat: add SBR task scenarios`

### Task 3: Implement isolated local accounting launch ownership

**Files:**
- Create: `scripts/launch-local-scenario.mjs`
- Create: `scripts/launch-local-scenario.test.mjs`
- Modify: `apps/desktop/src/main/core-launch.ts`
- Modify: `apps/desktop/src/main/core-launch.test.ts`
- Modify: `apps/desktop/src/main/index-production.ts`
- Modify: `apps/desktop/src/main/index-production.test.ts`
- Modify: `package.json`

- [ ] Write Node tests that inject the process runner and assert exact argv for each scenario:
  - `accounting-fresh` creates a unique directory under the OS temp root, passes it only as Electron `--user-data-dir`, retains it, and prints the absolute retained root once after exit;
  - SBR modes are rejected with `SBR_IMPLEMENTATION_INCOMPLETE` in this task; their authenticated wiring occurs after Task 4 and the helper/core prerequisite plan;
  - no command includes credential paths, passwords, Product IDs, private keys, or endpoint URLs;
  - signal forwarding terminates the child and cleanup runs once;
- [ ] Run `rtk mise exec -- node --test scripts/launch-local-scenario.test.mjs` and confirm RED.
- [ ] Add a narrow `desktop:start:scenario` package owner that starts Electron Forge with only the user-data/profile arguments emitted by the launcher.
- [ ] Do not add `--sbr-profile` in this task. Implement only `accounting-fresh` plus the existing persistent accounting scenario; SBR launch remains the tested incomplete stub.
- [ ] Implement the Node accounting owner with injected filesystem/process/clock dependencies, bounded child output, deterministic error codes, retained-root printing, and signal forwarding.
- [ ] Run the focused Node and Electron main tests and confirm GREEN.
- [ ] Run `rtk mise exec -- pnpm desktop:typecheck`.
- [ ] Commit: `feat: launch isolated accounting and SBR scenarios`

### Task 4: Define and authenticate the SBR profile

**Files:**
- Create: `scripts/sbr-profile-schema.mjs`
- Create: `scripts/sbr-profile-schema.test.mjs`
- Create: `test/fixtures/sbr/sbr-profile-v1.example.json`
- Create: `test/fixtures/sbr/sbr-profile-v1.example.sig`
- Create: `config/sbr/simulator/profile-public-key.pem`
- Create: `config/sbr/evte/.gitkeep`
- Modify: `.gitignore`

- [ ] Write tests for exact-key parsing of `sbr-profile-v1.json` with:
  - `schema_version: 1`;
  - `environment: SIMULATOR|EVTE` only;
  - `target: darwin/arm64`;
  - helper SHA-256;
  - component, registration, and endpoint-profile SHA-256 or literal `NONE` according to environment;
  - RFC 3339 `issued_at` and strictly future `expires_at`;
  - no `PRODUCTION` token anywhere in schema, fixtures, or exported constants.
- [ ] Add RFC 8785 canonicalization using a small pinned dependency only if the repository has no compliant implementation; do not hand-roll number canonicalization.
- [ ] Verify detached Ed25519 signatures over the canonical bytes.
- [ ] Add negative tests for unknown fields, duplicate JSON keys, expired profiles, wrong target, wrong hashes, malformed signatures, wrong keys, `PRODUCTION`, symlinks, group/world-writable files, and oversized files.
- [ ] The Go core embeds both authoritative public keys in its binary. Node keeps separate source-pinned advisory copies solely for preflight; it never reads a runtime-provided trust key. `profile-public-key.pem` is fixture documentation/test input only and is never a runtime trust root. The simulator private test key may exist only in test source; the EVTE private key never enters the repository.
- [ ] Ignore `config/sbr/evte/*` except `.gitkeep`; this directory is populated from externally issued, signed release inputs and never committed.
- [ ] Run the focused Node test and confirm RED before implementation, then GREEN.
- [ ] Keep these as schema fixtures with a fixed test helper hash. Do not create the runtime `config/sbr/simulator` profile until the real deterministic helper binary exists.
- [ ] Commit: `feat: authenticate SBR runtime profiles`

### Task 4A: Wire authenticated SBR launch after the core prerequisite lands

**Dependencies:** Complete helper/vault plan Tasks 1–7 first so the core owns `--sbr-profile` and authoritative authentication.

**Files:**
- Modify: `scripts/launch-local-scenario.mjs`
- Modify: `scripts/launch-local-scenario.test.mjs`
- Modify: `apps/desktop/src/main/core-launch.ts`
- Modify: `apps/desktop/src/main/core-launch.test.ts`
- Modify: `apps/desktop/src/main/index-production.ts`
- Modify: `apps/desktop/src/main/index-production.test.ts`
- Create: `config/sbr/simulator/sbr-profile-v1.json`
- Create: `config/sbr/simulator/sbr-profile-v1.sig`

- [ ] Add RED tests for simulator locator `config/sbr/simulator/sbr-profile-v1.json` and fixed EVTE locator `config/sbr/evte/sbr-profile-v1.json`; Node advisory preflight rejects invalid files but never labels them trusted.
- [ ] Extend core launch argv with one absolute `--sbr-profile` locator. Electron rejects duplicate/relative/unknown switches; core independently opens/authenticates it.
- [ ] Simulator uses a unique retained user-data root. EVTE uses normal persistent development user data and fails before Electron launch if any registration preflight gap exists.
- [ ] Generate/sign the runtime simulator profile against the deterministic helper hash, verify it through Node advisory and Go authoritative owners, and reject any stale fixture hash.
- [ ] Run `rtk mise exec -- node --test scripts/launch-local-scenario.test.mjs` and `rtk mise exec -- pnpm --dir apps/desktop test -- src/main/core-launch.test.ts src/main/index-production.test.ts`; confirm RED then GREEN.
- [ ] Keep both launch stubs in place until desktop Tasks 1–7 land, so a profile cannot launch without the required simulator banner/readiness UI.
- [ ] Commit: `feat: wire authenticated SBR launches`

### Task 5: Define and authenticate component bundles

**Files:**
- Create: `scripts/sbr-component-schema.mjs`
- Create: `scripts/sbr-component-schema.test.mjs`
- Create: `docs/development/sbr-component-manifest.example.json`

- [ ] First add tests for exact `sbr-component-v1.json` keys: `schema_version: 1`, component name/version/target, and `files` sorted by normalized UTF-8 relative path with exact byte length and SHA-256.
- [ ] Reject absolute paths, `..`, empty/dot segments, backslashes, Unicode-normalization aliases, duplicate paths, symlinks, undeclared files, unknown keys, wrong target, invalid lengths/hashes, and manifests above 256 KiB.
- [ ] Verify the registration manifest's `component_manifest_sha256` and signed profile component hash equal SHA-256 of the exact canonical component-manifest bytes.
- [ ] Run `rtk mise exec -- node --test scripts/sbr-component-schema.test.mjs` and confirm RED because the owner is absent; implement minimally and rerun for GREEN.
- [ ] Commit: `feat: define SBR component bundles`

### Task 6: Define registration and endpoint manifests

**Files:**
- Create: `scripts/sbr-registration-schema.mjs`
- Create: `scripts/sbr-registration-schema.test.mjs`
- Create: `docs/development/sbr-registration-manifest.example.json`
- Create: `docs/development/sbr-endpoint-profile.example.json`

- [ ] Write exact-key tests for `sbr-registration-v1.json` using the schema approved in the design spec: DSP registration, product registration, OSF, component licence, EVTE access, endpoint profile, per-service enrolment/conformance, and independent review/revalidation.
- [ ] Pin allowed states exactly. For pre-conformance EVTE readiness, allow service conformance `NOT_STARTED|RUNNING|PASSED`; require `PASSED` only for the distinct post-conformance-ready result.
- [ ] Define an endpoint-profile schema containing opaque service identifiers and endpoint metadata, but never pass its bytes to Task/Electron. It is authenticated, hashed into registration/profile manifests, then passed as bounded bytes from core to helper.
- [ ] Require detached EVTE Ed25519 signatures verified by the EVTE public key embedded in the Go core; Node preflight uses a separately pinned source copy. Do not accept a replaceable resource key and never commit the private half.
- [ ] Add negative tests for unknown keys, bad state transitions, expired approvals, unapproved component target, missing service enrolment, hash mismatch, signature mismatch, and any `PRODUCTION` environment.
- [ ] Keep the examples explicitly non-runnable and filled with placeholders, so they cannot accidentally satisfy readiness.
- [ ] Run `rtk mise exec -- node --test scripts/sbr-registration-schema.test.mjs`; confirm RED before implementation and GREEN afterward.
- [ ] Commit: `feat: define SBR registration evidence`

### Task 7: Implement fail-closed doctor and registration checks

**Files:**
- Create: `scripts/check-sbr-readiness.mjs`
- Create: `scripts/check-sbr-readiness.test.mjs`
- Create: `scripts/sbr-readiness-codes.mjs`
- Modify: `scripts/check-taskfiles.test.mjs`

- [ ] Encode the exact deterministic readiness-code precedence from the approved design spec in one shared constant list.
- [ ] Write tests that inject filesystem, signature verifier, clock, and app launcher. EVTE launch fixtures fail on the first ordered code; registration and doctor EVTE preflight accumulate every independent external gap. Simulator doctor continues to its fixed authenticated launch after reporting those gaps as non-blocking. Neither surface emits a path or secret-derived material.
- [ ] `doctor-preflight` deterministically reads live EVTE inputs only from `config/sbr/evte/{sbr-profile-v1.json,sbr-profile-v1.sig,sbr-component-v1.json,sbr-registration-v1.json,sbr-registration-v1.sig,sbr-endpoint-profile-v1.json}` and reports all registration gaps. It then launches the simulator profile at a fixed retained doctor data root `~/Library/Application Support/Tammy/local-sbr-doctor-simulator` and route `/settings/sbr?doctor=1`; after real unlock/sign-in the core RPC checks secure-store, credential metadata/expiry, and workspace binding. It does not claim EVTE credential/Product-ID readiness unless the separately signed EVTE app profile is launched.
- [ ] `registration` reads only the fixed live `config/sbr/evte/` paths above. It never reads the non-runnable `docs/development/*.example.json` files. It reports a sorted `missing_items` array containing every independent gap, plus pre-conformance/post-conformance state when applicable.
- [ ] Output one bounded JSON object containing status codes and non-secret fingerprints only. Never output credential labels, paths, passwords, Product IDs, endpoint URLs, or raw manifests.
- [ ] Run `rtk mise exec -- node --test scripts/check-sbr-readiness.test.mjs`; confirm RED before implementation and GREEN afterward.
- [ ] Run the preflight test command and `rtk mise exec -- task sbr:registration:check`; the repository placeholder evidence must report all external registration gaps and never imply EVTE/production readiness. Full doctor GREEN is deferred to the desktop packaged E2E after authenticated RPCs exist.
- [ ] Commit: `feat: add SBR readiness diagnostics`

### Task 8: Generate non-secret SBR evidence

**Files:**
- Create: `scripts/write-sbr-evidence.mjs`
- Create: `scripts/write-sbr-evidence.test.mjs`
- Modify: `.gitignore`
- Modify: `docs/development/foundation.md`

- [ ] Write tests proving evidence is generated only from current command results, is written atomically beneath `.tmp/sbr-evidence/`, and contains exact command, timestamp, revision, target, profile/manifest fingerprints, readiness codes, test exit statuses, and simulator zero-socket assertion.
- [ ] Prohibit secrets, absolute paths, all Product-ID-derived values (including fingerprints), and all workspace/user/organisation/ABN values recursively in the evidence schema.
- [ ] Refuse dirty source for releasable evidence; permit clearly labelled local diagnostic evidence only when explicitly requested by the owner script.
- [ ] Document the exact scenario flow and explain that EVTE evidence is not production approval.
- [ ] Run `rtk mise exec -- node --test scripts/write-sbr-evidence.test.mjs`; confirm RED before implementation and GREEN afterward.
- [ ] Before integration, run `rtk mise exec -- task evidence:sbr` and require exact `SBR_IMPLEMENTATION_INCOMPLETE:evidence`, non-zero exit, and no `.tmp/sbr-evidence` bundle. Actual zero-socket evidence is generated only in Task 9 after packaged observation exists.
- [ ] Commit: `feat: record SBR readiness evidence`

### Task 9: Integrate the scenario only after prerequisite plans land

**Dependencies:** Complete `2026-08-21-sbr-helper-and-credential-vault.md` Tasks 1–8 and `2026-08-21-sbr-desktop-simulator-and-e2e.md` Tasks 1–7 first. Until then, the five prerequisite-dependent public tasks use the exact incomplete owner from Task 2.

**Files:**
- Modify: `taskfiles/sbr.yml`
- Modify: `scripts/check-taskfiles.test.mjs`
- Modify: `scripts/write-sbr-evidence.mjs`
- Modify: `apps/desktop/tests/e2e/sbr-result.ts`
- Modify: `apps/desktop/src/main/sbr-result.test.ts`

- [ ] Replace `run-test` with this exact ordered command list:
  1. `mise exec -- go test -race ./services/sbr-helper/... -count=1`
  2. `env TAMMY_SBR_KEYCHAIN_INTEGRATION=1 mise exec -- go test -tags tammy_sbr_keychain_integration ./services/sbr-helper/internal/vault -run Keychain -count=1`
  3. `mise exec -- node scripts/test-sbr-security-bookmark.mjs`
  4. `mise exec -- go test -race ./services/core/internal/sbrprofile ./services/core/internal/sbrhelper ./services/core/internal/sbr -count=1`
  5. `mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/storage/migrations ./services/core/internal/sbr ./services/core/internal/backup ./services/core/internal/restore ./services/core/internal/app ./services/core/cmd/tammy-core -count=1`
  6. `mise exec -- node --test scripts/sbr-profile-schema.test.mjs scripts/sbr-component-schema.test.mjs scripts/sbr-registration-schema.test.mjs scripts/check-sbr-readiness.test.mjs scripts/write-sbr-evidence.test.mjs scripts/test-sbr-security-bookmark.test.mjs`
  7. `mise exec -- pnpm --dir apps/desktop test -- src/preload/index.test.ts src/main/core-client.test.ts src/main/rpc-router.test.ts src/main/ipc.test.ts src/main/sbr-file-intake.test.ts src/main/index-production.test.ts src/main/index.test.ts src/main/playwright-config.test.ts src/renderer/features/sbr/sbr-readiness-screen.test.tsx src/renderer/features/sbr/machine-credential-form.test.tsx src/renderer/features/sbr/product-id-form.test.tsx src/renderer/features/sbr/organisation-verification-form.test.tsx src/renderer/features/sbr/sbr-simulator-panel.test.tsx`
  8. `mise exec -- node --test scripts/check-slice-one-coverage-policy.test.mjs scripts/check-e2e-coverage.test.mjs`
  9. `mise exec -- pnpm --dir apps/desktop test -- src/main/e2e-process-check.test.ts src/main/sbr-result.test.ts`
  10. `mise exec -- pnpm contracts`
- [ ] `sbr-result.ts` atomically writes `.tmp/sbr-e2e/latest/result.json` mode `0600` only after the exact-SHA packaged test. Exact keys: `schema`, `source_revision`, `profile_sha256`, `helper_sha256`, `fixture_sha256`, `socket_samples`, `socket_violations`, `core_path_verified`, `helper_path_verified`, `core_orphans`, `helper_orphans`, `playwright_status`, `recorded_at`. Validation requires schema `tammy-sbr-e2e-result-v1`, 40-hex current revision, 64-hex hashes, at least one socket sample, zero violations/orphans, both path booleans true, status `PASSED`, bounded UTC timestamp, exact keys/no absolute paths. Fixture cleanup owns removal before each run; failed tests never leave a pass result.
- [ ] Replace all five stubs only now. `run-doctor` reports EVTE gaps without blocking, then launches fixed simulator doctor. `launch-evte` remains fail-closed on its first readiness code. `run-evidence` consumes only the validated exact-revision result above.
- [ ] Add graph tests proving ordered execution and no duplicate test owner.
- [ ] Run `rtk mise exec -- task test:sbr` and capture the full exit status.
- [ ] Run authenticated doctor through packaged E2E; do not attempt to obtain workspace credential status from an unauthenticated CLI.
- [ ] Run simulator evidence only from the canonical fixture and exact-revision packaged result. EVTE evidence remains in-app and unavailable until signed external inputs exist.
- [ ] Commit: `feat: integrate SBR scenario verification`

### Task 10: Verify the scenario plan

**Files:**
- Modify if required: `README.md`
- Modify if required: `docs/development/foundation.md`

- [ ] Run `rtk mise exec -- node --test scripts/check-taskfiles.test.mjs scripts/launch-local-scenario.test.mjs scripts/sbr-profile-schema.test.mjs scripts/sbr-component-schema.test.mjs scripts/sbr-registration-schema.test.mjs scripts/check-sbr-readiness.test.mjs scripts/write-sbr-evidence.test.mjs`.
- [ ] Run `rtk mise exec -- pnpm desktop:typecheck`.
- [ ] Run `rtk mise exec -- task test:contracts`.
- [ ] Run `rtk mise exec -- task --list` and inspect all new summaries.
- [ ] Run `rtk git diff --check` and scoped Biome checks for changed JS/TS/JSON files.
- [ ] Run `rtk git ls-files config/sbr/evte` and require only `.gitkeep`; run the tracked-secret scanner and require no EVTE private key, Product ID, machine credential, password, live endpoint URL, or runnable approved registration manifest. Documentation placeholder examples are allowed only when schema validation proves they cannot satisfy readiness.
- [ ] Request a spec-compliance review, then a quality/security review; fix all Critical/Important findings before handing off this chunk.
