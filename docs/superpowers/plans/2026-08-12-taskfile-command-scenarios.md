# Taskfile Command Scenarios Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add one scenario-oriented Taskfile command surface for Tammy setup, local development, testing, builds, packages, CI, diagnostics, and locally validated Mac App Store candidates without duplicating existing implementation logic or adding upload behavior.

**Architecture:** A root `Taskfile.yml` includes eight responsibility-specific Taskfiles. Local tasks delegate through the pinned `mise` environment to existing pnpm/Node/Go owners, while CI tasks are an explicit direct-command exception after Actions provisions pinned runtimes. A Node contract test parses the YAML and workflows so command names, safety boundaries, platform restrictions, and documentation cannot drift.

**Tech Stack:** Go Task 3.52.0, mise, pnpm 11, Node.js 24, Go 1.26, YAML, Node test runner, GitHub Actions, Electron Forge, Playwright, macOS signing tools.

**Normative design:** `docs/superpowers/specs/2026-08-12-taskfile-command-scenarios-design.md`

---

## Chunk 1: Executable Taskfile contract and local scenarios

### Task 1: Pin Task and add the reusable clean-tree guard

**Files:**

- Modify: `mise.toml`
- Create: `scripts/check-clean-tree.mjs`
- Create: `scripts/check-clean-tree.test.mjs`

- [ ] **Step 1: Write failing pin and clean-tree tests.**

  Assert `mise.toml` pins Task `3.52.0`. Test an injectable `checkCleanTree(run)` function that accepts only successful `git status --porcelain=v1` with empty stdout, returns exact changed paths in `DIRTY_WORKTREE` errors, rejects spawn/non-zero status failures, never mutates files, and exposes a zero-argument CLI.

- [ ] **Step 2: Run the test and verify RED.**

  Run: `rtk mise exec -- node --test scripts/check-clean-tree.test.mjs`

  Expected: FAIL because the pin and guard do not exist.

- [ ] **Step 3: Pin Task 3.52.0.**

  Add `task = "3.52.0"` to `mise.toml`. Do not change Node or Go pins.

- [ ] **Step 4: Implement the read-only clean-tree guard.**

  Use `execFile`/`spawn` with `git`, `status`, `--porcelain=v1`, `shell: false`, and inherited repository cwd. Print changed paths to stderr before failing. Do not expose any delete/reset/stash action.

- [ ] **Step 5: Run focused tests and verify GREEN.**

  Run: `rtk mise exec -- node --test scripts/check-clean-tree.test.mjs`

  Expected: PASS.

- [ ] **Step 6: Install the newly pinned Task tool.**

  Run: `rtk mise install`

  Then: `rtk mise exec -- task --version`

  Expected: exact version `3.52.0`.

- [ ] **Step 7: Commit the green pin and guard.**

  ```sh
  rtk git add mise.toml scripts/check-clean-tree.mjs scripts/check-clean-tree.test.mjs
  rtk git commit -m "feat: add task orchestration prerequisites"
  ```

### Task 2: Add root, setup, development, and diagnostic scenarios

**Files:**

- Create: `Taskfile.yml`
- Create: `taskfiles/setup.yml`
- Create: `taskfiles/dev.yml`
- Create: `taskfiles/diagnostics.yml`
- Create: `taskfiles/test.yml`
- Create: `taskfiles/build.yml`
- Create: `taskfiles/package.yml`
- Create: `taskfiles/release.yml`
- Create: `taskfiles/ci.yml`
- Create: `scripts/check-taskfiles.test.mjs`

- [ ] **Step 1: Write the failing local-front-door contract.**

  Parse YAML with the repository `yaml` package and assert the root includes, required descriptions/summaries, implemented aliases, sequential `cmds` rather than `deps`, explicit supported-target failure preconditions, pinned local execution, no destructive task, and read-only diagnostics. Assert no root alias references a task that does not yet exist.

- [ ] **Step 2: Run and verify RED.**

  Run: `rtk mise exec -- node --test scripts/check-taskfiles.test.mjs`

  Expected: FAIL because the root and local scenario files do not exist.

- [ ] **Step 3: Add root discovery and exact aliases.**

  Create all eight included scenario files immediately so the root is always parseable; the not-yet-implemented test/build/package/release/CI files contain only Task version `3` until their green slices below. The default task prints `task --list` plus the current product boundaries. Add exact sequential aliases:

  - `setup` -> `setup:tools`, `setup:deps`, `setup:check`
  - `dev` -> `dev:launch`
  - `test` -> one `mise exec -- pnpm test`

  Add verify/build/package wrappers only in Task 3 and `deploy:mas` only in Task 5, in the same commit as their real targets.

- [ ] **Step 4: Add post-bootstrap setup tasks.**

  - `setup:tools`: bare `mise install` only, documented as post-bootstrap repair/update.
  - `setup:deps`: `mise exec -- corepack prepare pnpm@11.15.0 --activate` followed by frozen install.
  - `setup:check`: `mise exec -- pnpm check:toolchain` and `mise exec -- task --version`, rejecting any version other than `3.52.0`.

- [ ] **Step 5: Add guarded local development tasks.**

  Both tasks use an explicit `mise exec -- node -e ...` precondition that accepts only Node targets `darwin/arm64` and `win32/x64` and exits non-zero with `UNSUPPORTED_SQLCIPHER_TARGET:<target>` otherwise; do not use Task `platforms`, which silently skips. `dev:core` runs `mise exec -- pnpm core:build`; `dev:launch` runs `mise exec -- pnpm desktop:start`. Summaries explain persistence under `local-core-development`, development-memory-anchor limits, and no production evidence.

- [ ] **Step 6: Add read-only diagnostics.**

  - `diagnose:toolchain`: versions and `pnpm check:toolchain`.
  - `diagnose:core`: expected owned core binary/build-manifest paths and exact process/path guidance without killing anything.
  - `diagnose:package`: package locator verification/help, no rebuild.
  - `diagnose:data`: platform-specific expected development and packaged data roots, explicitly no deletion.

- [ ] **Step 7: Run the contract test and verify GREEN.**

  Expected: PASS for the local-front-door slice. Tests must not assert not-yet-implemented release, CI, or documentation behavior.

- [ ] **Step 8: Exercise safe tasks.**

  Run:

  ```sh
  rtk mise exec -- task --list
  rtk mise exec -- task --summary dev:launch
  rtk mise exec -- task diagnose:data
  rtk mise exec -- task diagnose:toolchain
  ```

  Expected: PASS; no files or processes are deleted.

- [ ] **Step 9: Commit the green local front door.**

  ```sh
  rtk git add Taskfile.yml taskfiles/setup.yml taskfiles/dev.yml taskfiles/diagnostics.yml taskfiles/test.yml taskfiles/build.yml taskfiles/package.yml taskfiles/release.yml taskfiles/ci.yml scripts/check-taskfiles.test.mjs
  rtk git commit -m "feat: add taskfile development scenarios"
  ```

### Task 3: Add test, build, and package scenarios

**Files:**

- Modify: `taskfiles/test.yml`
- Modify: `taskfiles/build.yml`
- Modify: `taskfiles/package.yml`
- Modify: `Taskfile.yml`
- Modify: `scripts/check-taskfiles.test.mjs`

- [ ] **Step 1: Extend the contract test and verify RED.**

  Add assertions for focused test tasks, included internal `test:verify:*` targets, public root `verify:*` wrappers, root `build` and `package` aliases, explicit native-target failure preconditions, exact commands, sequential verification order, the clean-tree guard in release verification, distinct raw-build versus verified-package ownership, and no `package:launch`/Task caching fields. Add the root wrappers only now: `verify` -> `verify:full`, `verify:quick` -> `test:verify:quick`, `verify:full` -> `test:verify:full`, `verify:release` -> `test:verify:release`, `build` -> `build:desktop`, and `package` -> `package:verify`.

  Run: `rtk mise exec -- node --test scripts/check-taskfiles.test.mjs`

  Expected: FAIL because the stub scenario files and root do not contain the required tasks/wrappers.

- [ ] **Step 2: Add focused test tasks.**

  Implement `test:core`, `test:desktop`, `test:contracts`, and `test:sqlcipher`. `test:sqlcipher` is platform-aware: macOS arm64 runs the pinned Node SQLCipher tests and tagged storage tests; Windows x64 requires the exact existing native environment and ordinary probe handoff; Linux fails with a clear unsupported precondition.

- [ ] **Step 3: Add explicit verification tiers.**

  Inside the included `test.yml`, define `verify:quick`, `verify:full`, and `verify:release`; Go Task exposes them as `test:verify:*`. Root wrappers expose the approved `verify:*` API.

  - included `test:verify:quick`: sequential `mise exec -- pnpm check:toolchain`, `mise exec -- pnpm test` exactly once, `mise exec -- pnpm --filter @tammy/connect-client typecheck`, `mise exec -- pnpm desktop:typecheck`, `mise exec -- pnpm contracts`, `mise exec -- pnpm lint`, and `git diff --check`.
  - included `test:verify:full`: invoke included quick, then platform-specific SQLCipher verification on macOS/Windows; quick only on Linux.
  - included `test:verify:release`: explicit precondition accepting only `darwin/arm64` and failing with `UNSUPPORTED_MACOS_RELEASE_TARGET:<target>` otherwise; sequential `mise exec -- node scripts/check-clean-tree.mjs`, production contracts, included full verification, ordinary packaged E2E, and repository-only App Store check.

- [ ] **Step 4: Add build tasks with distinct ownership.**

  - `build:sqlcipher`: `pnpm sqlcipher:build`.
  - `build:core`: `pnpm core:build`.
  - `build:manifest`: `pnpm build:manifest`.
  - `build:desktop`: explicit `darwin/arm64` or `win32/x64` failure precondition; sequential `mise exec -- pnpm core:build`, `mise exec -- pnpm build:manifest`, and `mise exec -- pnpm --dir apps/desktop package`. It is not verified evidence.

  All transitive native-build tasks declare the supported platform precondition.

- [ ] **Step 5: Add package verification tasks.**

  - `package:verify`: `pnpm desktop:package`.
  - `package:e2e`: `pnpm desktop:e2e`.

  Both summaries explain source/core/package authentication, isolated Electron data, orphan checks, and evidence limits. Do not add `package:launch`.

- [ ] **Step 6: Run the Taskfile contract test and safe focused tasks.**

  ```sh
  rtk mise exec -- node --test scripts/check-taskfiles.test.mjs
  rtk mise exec -- task --summary verify:release
  rtk mise exec -- task test:contracts
  ```

  Expected: contract test PASS for all implemented local scenarios; contracts PASS.

- [ ] **Step 7: Commit green local test/build/package scenarios.**

  ```sh
  rtk git add Taskfile.yml taskfiles/test.yml taskfiles/build.yml taskfiles/package.yml scripts/check-taskfiles.test.mjs
  rtk git commit -m "feat: add taskfile verification scenarios"
  ```

---

## Chunk 2: Release ownership, CI adoption, and documentation

### Task 4: Harden the local Mac App Store package owner

**Files:**

- Modify: `scripts/package-macos-store.mjs`
- Modify: `scripts/package-macos-store.test.mjs`

- [ ] **Step 1: Write failing package-validation tests.**

  Refactor the test seam around an injectable command runner and package hasher, then add tests requiring this exact distribution order after `productbuild`:

  - `/usr/sbin/pkgutil --check-signature <pkg>` as a fatal validation;
  - Node-owned streaming SHA-256 calculation;
  - `/usr/sbin/spctl --assess --type install --verbose=4 <pkg>` as an observational Gatekeeper assessment;
  - one final JSON line containing `app`, `pkg`, `pkgSha256`, and classified `gatekeeperAssessment`.

  Test development mode still stops at the `.app`; runner spawn failure, missing command output, and unclassifiable assessment fail closed; package signature failure is fatal; accepted and ordinary pre-App-Store-rejected `spctl` results are both classified; the final JSON schema is stable; and no environment secret appears in output or errors.

- [ ] **Step 2: Run the focused test and verify RED.**

  Run: `rtk mise exec -- node --test scripts/package-macos-store.test.mjs`

  Expected: FAIL because the plan ends at `productbuild` and the runner cannot classify observational commands or calculate a package hash.

- [ ] **Step 3: Implement minimal release-owner validation.**

  Keep command planning and execution in the Node module. Run `productbuild`, fatal `pkgutil`, streaming SHA-256, observational `spctl`, then JSON in that exact order. Capture only assessment output needed for classification, redact unsafe details, treat classified `spctl` rejection as non-fatal, and fail on command-execution or classification failure. Do not implement upload, notarization, credential storage, or Task-specific branching.

- [ ] **Step 4: Run focused and release tests.**

  ```sh
  rtk mise exec -- node --test scripts/package-macos-store.test.mjs scripts/check-macos-store.test.mjs
  rtk mise exec -- pnpm check:macos-store
  ```

  Expected: PASS without operator credentials in repository-check mode.

- [ ] **Step 5: Commit release-owner hardening.**

  ```sh
  rtk git add scripts/package-macos-store.mjs scripts/package-macos-store.test.mjs
  rtk git commit -m "fix: validate local app store packages"
  ```

### Task 5: Add signed release and package-only deploy scenarios

**Files:**

- Modify: `taskfiles/release.yml`
- Modify: `scripts/check-taskfiles.test.mjs`

- [ ] **Step 1: Extend the Taskfile contract and verify RED.**

  Assert `release:check` is platform-neutral; signed tasks explicitly fail unsupported targets with `UNSUPPORTED_MACOS_RELEASE_TARGET:<target>`, run the clean-tree guard before their owner, force the correct signing mode, and declare exact inputs; development rejects the installer identity; candidate requires it; and deploy contains only a sequential `release:candidate` task call with no upload surface.

  Run: `rtk mise exec -- node --test scripts/check-taskfiles.test.mjs`

  Expected: FAIL because the release include is still a stub and the root has no deploy wrapper.

- [ ] **Step 2: Add platform-neutral repository readiness.**

  `release:check` delegates to `mise exec -- pnpm check:macos-store` and has no signing variables or macOS-only guard.

- [ ] **Step 3: Add macOS arm64 development signing.**

  `release:development` explicitly accepts only `darwin/arm64`; requires non-empty `TAMMY_MACOS_BUILD_NUMBER`, `TAMMY_MACOS_EXPORT_COMPLIANCE`, `TAMMY_MACOS_PROVISIONING_PROFILE`, `TAMMY_MACOS_PRIVACY_POLICY_URL`, `TAMMY_MACOS_SIGNING_IDENTITY`, `TAMMY_MACOS_SUPPORT_URL`, and `TAMMY_MACOS_TEAM_ID`; rejects non-empty `TAMMY_MACOS_INSTALLER_IDENTITY`; sequentially runs `mise exec -- node scripts/check-clean-tree.mjs`; and runs `TAMMY_MACOS_SIGNING_MODE=development mise exec -- pnpm desktop:make:mas`. Its summary names `apps/desktop/out/Tammy-mas-arm64/Tammy.app` and states no installer/upload is produced.

- [ ] **Step 4: Add distribution candidate and deploy alias.**

  `release:candidate` explicitly accepts only `darwin/arm64`; requires the same seven inputs plus non-empty `TAMMY_MACOS_INSTALLER_IDENTITY`; sequentially runs the clean-tree guard; runs `TAMMY_MACOS_SIGNING_MODE=distribution mise exec -- pnpm desktop:make:mas`; and reports the validated package JSON. Root `deploy:mas` sequentially invokes only `{task: release:candidate}`. No upload token, Transporter, `xcrun`, API, or upload command may appear.

- [ ] **Step 5: Run Taskfile contract verification and verify GREEN.**

  Run:

  ```sh
  rtk mise exec -- node --test scripts/check-taskfiles.test.mjs
  rtk mise exec -- task release:check
  rtk mise exec -- task --summary deploy:mas
  ```

  Expected: contract test PASS for the implemented release slice; repository release check PASS.

- [ ] **Step 6: Commit green release scenarios.**

  ```sh
  rtk git add taskfiles/release.yml scripts/check-taskfiles.test.mjs Taskfile.yml
  rtk git commit -m "feat: add taskfile release scenarios"
  ```

### Task 6: Move CI execution behind canonical scenario tasks

**Files:**

- Modify: `taskfiles/ci.yml`
- Modify: `.github/workflows/foundation-ci.yml`
- Modify: `.github/workflows/foundation-windows11-e2e.yml`
- Modify: `scripts/check-taskfiles.test.mjs`
- Modify: `scripts/windows-sqlcipher-workflow.test.mjs`
- Modify: `scripts/verify-descriptor-evidence.test.mjs`
- Modify: `scripts/check-foundation-evidence.test.mjs`

- [ ] **Step 1: Record the workflow and staging baseline.**

  Run before editing:

  ```sh
  rtk git status --short
  rtk git diff -- .github/workflows/foundation-ci.yml .github/workflows/foundation-windows11-e2e.yml
  rtk git diff --cached
  ```

  Preserve every unrelated/user-owned path and any pre-existing workflow hunk.

- [ ] **Step 2: Extend exact workflow-policy tests and verify RED.**

  Extend the Taskfile contract and the three named workflow-policy tests to require exact CI task ownership, order, Task installation/version pin, platform classification, descriptor revision propagation, `xvfb-run`, reproducibility mode, Windows ordinary-probe handoff, result JSON, and unchanged artifact retention.

  Run:

  ```sh
  rtk mise exec -- node --test scripts/check-taskfiles.test.mjs scripts/windows-sqlcipher-workflow.test.mjs scripts/verify-descriptor-evidence.test.mjs scripts/check-foundation-evidence.test.mjs
  ```

  Expected: FAIL because the CI include is still a stub and the workflows do not invoke `ci:*`.

- [ ] **Step 3: Add exact CI scenario tasks.**

  Each CI task starts with direct `task --version`, exact `3.52.0` validation, `pnpm check:toolchain`, and `node scripts/check-clean-tree.mjs`. CI deliberately uses Actions-provisioned executables and never `mise exec --`.

  - `ci:contracts`: version/toolchain/clean checks; `pnpm ci:lint`; `pnpm test:proto-breaking`; `pnpm contracts`; `pnpm proto:descriptors:verify` with inherited `TAMMY_EVIDENCE_SUBJECT_REVISION`.
  - `ci:linux`: version/toolchain/clean checks; `go test -race ./services/core/...`; `command -v xvfb-run`; `xvfb-run -a pnpm desktop:test`; `pnpm desktop:typecheck`; `pnpm lint`.
  - `ci:macos`: version/toolchain/clean checks; `TAMMY_SQLCIPHER_REPRODUCIBILITY=1 pnpm sqlcipher:test`; `go test -race -tags tammy_sqlcipher ./services/core/internal/storage/sqlcipher/... -count=1`; `pnpm desktop:e2e`.
  - `ci:windows-smoke`: exact `WINDOWS_SERVER_SMOKE_ONLY` classification and AMD64 checks; version/toolchain/clean checks; `pnpm test:proto-breaking`; descriptor verification with inherited revision; desktop test/typecheck.
  - `ci:windows11`: version/toolchain/clean checks, then one Task multiline `cmd` explicitly launches `pwsh -NoProfile -NonInteractive -Command '& { ... }'`. Inside that single PowerShell process it runs SQLCipher test/build, resolves `.tmp/sqlcipher/ordinary/win32-x64/ordinary-sqlite3.exe`, assigns `$env:TAMMY_ORDINARY_SQLITE3`, runs tagged Go tests, packaged E2E, and writes the exact `WINDOWS11_23H2_X64_RELEASE_GATE`/`PASSED` JSON using inherited `GITHUB_RUN_ID` and `GITHUB_RUN_ATTEMPT`. Do not rely on the workflow step's shell to interpret Task commands.

- [ ] **Step 4: Update Actions provisioning and invocation.**

  Retain checkout, pinned Node/Go, pnpm activation, frozen install, platform/runner/native-tool validation, environment export, virtual display provisioning, artifact upload, evidence classification, concurrency, and job timeouts. On Bash runners run `go install github.com/go-task/task/v3/cmd/task@v3.52.0`, append `$(go env GOPATH)/bin` to `$GITHUB_PATH`, and verify `task --version`. On PowerShell append `$(go env GOPATH)\bin` to `$env:GITHUB_PATH` and verify the same exact version. Replace only owned command blocks with ordered `task ci:*` calls; Ubuntu calls contracts then linux.

- [ ] **Step 5: Run CI contract tests and actionlint; verify GREEN.**

  ```sh
  rtk mise exec -- node --test scripts/check-clean-tree.test.mjs scripts/check-taskfiles.test.mjs scripts/windows-sqlcipher-workflow.test.mjs scripts/verify-descriptor-evidence.test.mjs scripts/check-foundation-evidence.test.mjs
  rtk mise exec -- pnpm ci:lint
  ```

  Expected: PASS; workflow-policy tests reflect the new canonical entry points.

- [ ] **Step 6: Audit exact staged scope and commit CI adoption.**

  ```sh
  rtk git add taskfiles/ci.yml scripts/check-taskfiles.test.mjs scripts/windows-sqlcipher-workflow.test.mjs scripts/verify-descriptor-evidence.test.mjs scripts/check-foundation-evidence.test.mjs
  rtk git add .github/workflows/foundation-ci.yml .github/workflows/foundation-windows11-e2e.yml
  rtk git diff --cached --check
  rtk git diff --cached
  rtk git commit -m "ci: use taskfile scenarios"
  ```

### Task 7: Make Task scenarios the documented front door

**Files:**

- Modify: `README.md`
- Modify: `docs/development/foundation.md`
- Modify: `docs/release/macos-app-store.md`
- Modify: `scripts/check-taskfiles.test.mjs`

- [ ] **Step 1: Add failing documentation assertions.**

  Require the docs to name real tasks and the exact bootstrap flow:

  ```sh
  mise install
  mise exec -- task setup
  mise exec -- task dev
  ```

  Require primary examples for `test`, `verify`, `package:e2e`, `release:check`, `release:development`, `release:candidate`, and `deploy:mas`. Assert the release runbook still states upload is manual and retains every operator input/checklist boundary.

- [ ] **Step 2: Record the dirty baseline, then run the contract and verify RED.**

  Before editing, run:

  ```sh
  rtk git status --short
  rtk git diff -- README.md docs/development/foundation.md docs/release/macos-app-store.md
  rtk git diff --cached
  ```

  Expected: FAIL because docs still lead with pnpm commands.

- [ ] **Step 3: Update README and handbook.**

  Lead with scenario tasks and preserve underlying commands as implementation/troubleshooting reference. Retain persistent data roots, development-memory-anchor limits, recoverable reset guidance, module ownership, generated-code rules, TDD workflow, loader/linker diagnostics, and orphan evidence guidance.

- [ ] **Step 4: Update the App Store runbook.**

  Lead repository readiness with `release:check`, development signing with `release:development`, and distribution packaging with `release:candidate`/`deploy:mas`. Retain all environment variables, signed-build inspection, `pkgutil`, observational `spctl`, metadata, manual Transporter upload, release-record, and rollback steps.

- [ ] **Step 5: Run documentation and command-surface tests.**

  ```sh
  rtk mise exec -- node --test scripts/check-taskfiles.test.mjs scripts/check-foundation-evidence.test.mjs
  rtk mise exec -- task --list
  rtk mise exec -- task diagnose:data
  ```

  Expected: PASS.

- [ ] **Step 6: Commit documentation.**

  Stage only Taskfile-related hunks in user-modified files.

  ```sh
  rtk git add -p README.md docs/development/foundation.md docs/release/macos-app-store.md
  rtk git add scripts/check-taskfiles.test.mjs
  rtk git diff --cached --check
  rtk git diff --cached
  rtk git commit -m "docs: document taskfile workflows"
  ```

### Task 8: Run complete orchestration verification

**Files:** None expected unless verification exposes a defect.

- [ ] **Step 1: Verify the command contract and workflows.**

  ```sh
  rtk mise exec -- node --test scripts/check-taskfiles.test.mjs scripts/check-clean-tree.test.mjs scripts/package-macos-store.test.mjs
  rtk mise exec -- pnpm ci:lint
  rtk mise exec -- task --list
  ```

- [ ] **Step 2: Run safe local scenarios.**

  ```sh
  rtk mise exec -- task setup:check
  rtk mise exec -- task test:contracts
  rtk mise exec -- task verify:quick
  rtk mise exec -- task release:check
  ```

- [ ] **Step 3: Run native and packaged scenarios on macOS arm64.**

  ```sh
  rtk mise exec -- task test:sqlcipher
  rtk mise exec -- task package:e2e
  ```

  Expected: PASS with package signature/resource verification, clean source/core/package manifest, isolated Playwright journeys, and no orphaned core.

- [ ] **Step 4: Run repository integrity checks.**

  ```sh
  rtk mise exec -- pnpm test
  rtk mise exec -- pnpm typecheck
  rtk mise exec -- pnpm contracts
  rtk git diff --check
  ```

  Repository-wide lint may expose pre-existing unrelated formatting debt; all changed lintable files must pass Biome.

- [ ] **Step 5: Inspect release plans without signing.**

  Confirm `release:development`, `release:candidate`, and `deploy:mas` summaries, platform guards, forced modes, and variables. Do not fabricate credentials or execute signed release tasks.

- [ ] **Step 6: Commit any verification-only corrections.**

  If no corrections are required, do not create an empty commit.
