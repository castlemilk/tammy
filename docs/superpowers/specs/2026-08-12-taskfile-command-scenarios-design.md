# Taskfile Command Scenarios Design

**Status:** Approved for implementation  
**Date:** 12 August 2026  
**Scope:** Developer, CI, packaging, and local Mac App Store candidate orchestration

## 1. Purpose

Tammy currently has correct build and verification logic spread across root `package.json` scripts, Go commands, GitHub Actions workflows, the developer handbook, and the macOS App Store runbook. The implementation logic is intentionally specialized: SQLCipher provenance, supervised-core startup, protobuf evidence, packaged Electron isolation, Apple signing, and clean-source verification already have tested owners.

Add Go Task as a small, discoverable orchestration layer. After the one-time `mise install` prerequisite, a developer should be able to run `mise exec -- task --list`, choose a scenario, and receive the same safe workflow used by CI without needing to reconstruct command order from several documents. Taskfiles must delegate to existing pnpm, Node, Go, Electron Forge, and release scripts rather than reimplementing them.

## 2. Selected structure

Use one root aggregator and scenario-owned includes:

| File | Responsibility |
| --- | --- |
| `Taskfile.yml` | Root discovery, default task, aliases, shared variables, and scenario includes. |
| `taskfiles/setup.yml` | Post-bootstrap pinned-tool refresh, frozen dependency installation, and toolchain validation. |
| `taskfiles/dev.yml` | Local launch, core build, and safe development-data diagnostics. |
| `taskfiles/test.yml` | Focused core, desktop, contract, SQLCipher, quick, full, and release verification. |
| `taskfiles/build.yml` | SQLCipher, Go core, build manifest, and ordinary desktop package construction. |
| `taskfiles/package.yml` | Ordinary signed local package verification and packaged Playwright journeys. |
| `taskfiles/release.yml` | Repository App Store checks, development-signed app, distribution candidate, and local `.pkg` validation. |
| `taskfiles/ci.yml` | Stable scenario entry points called after runner-specific provisioning. |
| `taskfiles/diagnostics.yml` | Read-only toolchain, core, package, and data-location diagnostics. |

Pin Go Task `3.52.0` in `mise.toml`. The one command that necessarily precedes Task is `mise install`; after it completes, documentation consistently invokes `mise exec -- task ...`. `mise exec -- task setup` then refreshes the pinned tools, installs frozen dependencies, and verifies the toolchain. Calling `setup:tools` cannot bootstrap a machine that does not yet have Task; it is an update/repair task for an already bootstrapped checkout and intentionally runs bare `mise install` to avoid a nonsensical nested `mise exec -- mise install` invocation.

CI runners retain their existing pinned Actions-managed Node/Go provisioning. They install Task exactly with `go install github.com/go-task/task/v3/cmd/task@v3.52.0`, add Go's bin directory to the runner path, and verify `task --version` reports `3.52.0`. `ci:*` tasks are the explicit exception to local `mise exec --`: they invoke the already pinned Actions-managed `pnpm`, `go`, and `node` executables directly. This avoids installing a second Node/Go toolchain while retaining one exact scenario definition.

Do not add shell-script wrappers, another application CLI, YAML generation, dynamic includes, platform compatibility shims, or a second copy of release logic.

## 3. Command surface

The root aliases optimize for common scenarios; detailed operations remain namespaced.

| Scenario | Primary task | Supporting tasks |
| --- | --- | --- |
| Post-bootstrap setup/update | `task setup` | `setup:tools`, `setup:deps`, `setup:check` |
| Launch locally | `task dev` | `dev:launch`, `dev:core`, `dev:data:show` |
| Fast iteration | `task test` | `test:core`, `test:desktop`, `test:contracts`, `test:sqlcipher` |
| Complete verification | `task verify` | `verify:quick`, `verify:full`, `verify:release` |
| Build artifacts | `task build` | `build:sqlcipher`, `build:core`, `build:manifest`, `build:desktop` |
| Local package | `task package` | `package:verify`, `package:e2e` |
| App Store preparation | — | `release:check`, `release:development`, `release:candidate` |
| Deploy candidate | `task deploy:mas` | Produces and validates a signed `.pkg`; never uploads it. |
| CI | — | `ci:contracts`, `ci:linux`, `ci:macos`, `ci:windows-smoke`, `ci:windows11` |
| Troubleshooting | — | `diagnose:toolchain`, `diagnose:core`, `diagnose:package`, `diagnose:data` |

`mise exec -- task` and `mise exec -- task --list` are the front door. The default task prints the categorized task list plus concise Tammy boundaries: local-first encrypted accounting, development-only BAS workpapers, no ATO submission, local packages are not App Store evidence, and upload remains manual.

The root aliases execute these exact targets:

| Root task | Exact sequential target |
| --- | --- |
| `setup` | `setup:tools`, then `setup:deps`, then `setup:check`. |
| `dev` | `dev:launch`. |
| `test` | The existing `pnpm test` owner exactly once. |
| `verify` | `verify:full`. |
| `build` | `build:desktop`. |
| `package` | `package:verify`. |
| `deploy:mas` | `release:candidate`; it stops after local `.pkg` validation and SHA-256 reporting. |

Ordered, mutating, evidence-producing, and release chains use sequential `cmds` or direct task invocations. They do not express ordering through Task `deps`, which may execute concurrently. No CI, verification, build-manifest, packaging, or release task uses Task `status`, `sources`, `generates`, or timestamp caching; every invocation reruns the owning checks.

## 4. Delegation and data flow

Local tasks run from the repository root and call pinned implementation commands through `mise exec --`. CI tasks call the Actions-pinned binaries directly as the single documented exception. Task aliases use sequential direct task calls; they do not duplicate command sequences when an existing root pnpm script already owns that sequence.

`setup:tools` is the other explicit exception: it calls bare `mise install` because it is repairing/updating the environment that `mise exec` would otherwise attempt to enter. All other local implementation commands use `mise exec --`.

The intended flow is:

```text
developer or CI scenario
  -> root Taskfile alias
  -> scenario Taskfile task
  -> pinned existing pnpm / Node / Go command
  -> existing validation, build, package, or release implementation
  -> explicit artifact path and observed result
```

Examples:

- `dev:launch` delegates to `pnpm desktop:start`, preserving supervised-core startup and the `local-core-development` data root.
- `test:contracts` delegates to `pnpm contracts`, preserving protobuf formatting, lint, breaking checks, generation, descriptors, transitions, and E2E coverage policy.
- `package:e2e` delegates to `pnpm desktop:e2e`, preserving source-manifest verification, package signature/resource checks, isolated Electron user data, Playwright journeys, and orphan detection.
- `release:candidate` and `deploy:mas` delegate to `pnpm desktop:make:mas`, preserving the repository's clean-tree, metadata, entitlement, provisioning, signing, core-authentication, installer construction, and package validation checks.

Build and package ownership is deliberately distinct:

- `build:sqlcipher` calls `pnpm sqlcipher:build` and is supported only for `darwin/arm64` and `win32/x64` with their required native toolchains.
- `build:core` calls `pnpm core:build`, which owns the SQLCipher build followed by the Go core build.
- `build:manifest` calls `pnpm build:manifest` and requires an already built core.
- `build:desktop` sequentially runs `build:core`, `build:manifest`, and Electron Forge's ordinary platform package command. It produces a raw local app bundle for build diagnosis; it is not verified evidence.
- `package:verify` calls `pnpm desktop:package`, which rebuilds and then authenticates the ordinary local package, signatures/resources, source manifest, and packaged core.
- `package:e2e` calls `pnpm desktop:e2e`, which owns `package:verify` plus Playwright acceptance. There is no `package:launch` task because the repository has no safe cross-platform packaged-launch owner.

The native-build restriction propagates to every public task that transitively builds SQLCipher: `dev`, `dev:launch`, `dev:core`, `build`, `build:core`, `build:desktop`, `package`, `package:verify`, and `package:e2e` support only `darwin/arm64` or `win32/x64`. They use explicit target preconditions that fail with a clear unsupported-target message; they do not use Go Task's `platforms` filter because that silently skips mismatched tasks. Read-only diagnostics, contracts, default tests, and platform-neutral repository checks remain available on Linux.

## 5. Safety and domain knowledge

Every public task has a short description. High-risk or slow tasks also have a detailed `summary` explaining prerequisites, expected artifacts, data behavior, and evidence limits.

The Taskfiles enforce or clearly communicate these Tammy-specific rules:

- Development data persists below Electron user data in `local-core-development`; packaged data uses `local-core`.
- No task deletes workspace data, passwords, recovery codes, keys, databases, imported documents, or attempt journals.
- Data diagnostics are read-only and print only owned paths and exact process/path guidance. They never perform broad process-name kills.
- SQLCipher-tagged tests require the supported cgo/platform boundary and are kept separate from fast iteration.
- Developers edit source protobufs only; pinned generation updates reviewable Go and TypeScript outputs.
- Ordinary packages are ad-hoc development artifacts, not Apple approval, ATO approval, or external assurance.
- `release:check` is platform-neutral and validates repository-owned App Store inputs without signing credentials. Signed `release:development`, `release:candidate`, and `deploy:mas` require `darwin/arm64` and fail closed on a dirty tree, missing or malformed Apple inputs, unfinished metadata, invalid profiles, signing mismatch, or package verification failure.
- `release:development` forces `TAMMY_MACOS_SIGNING_MODE=development` and requires `TAMMY_MACOS_BUILD_NUMBER`, `TAMMY_MACOS_EXPORT_COMPLIANCE`, `TAMMY_MACOS_PROVISIONING_PROFILE`, `TAMMY_MACOS_PRIVACY_POLICY_URL`, `TAMMY_MACOS_SIGNING_IDENTITY`, `TAMMY_MACOS_SUPPORT_URL`, and `TAMMY_MACOS_TEAM_ID`. It rejects an installer identity and produces a locally runnable MAS app without a `.pkg`.
- `release:candidate` and `deploy:mas` force `TAMMY_MACOS_SIGNING_MODE=distribution` and require the same inputs plus `TAMMY_MACOS_INSTALLER_IDENTITY`. They print the validated `.pkg` path and SHA-256, then stop. They must not invoke Transporter, `xcrun altool`, App Store Connect APIs, or any upload operation.
- Apple certificates, profiles, private keys, credentials, and session tokens remain operator-owned and outside the repository.
- Packaged E2E remains the release-relevant proof for renderer/preload/core changes and retains exact orphan diagnostics.

Task preconditions should use Go Task's declarative `preconditions`, platform lists, and variables where they improve clarity. They must not weaken checks already implemented by the underlying scripts and must not use cached status/source/generate skipping for safety or evidence tasks.

## 6. Verification tiers

The tiers make cost and evidence explicit:

- `verify:quick`: toolchain check; the existing `pnpm test` owner exactly once; connect-client and desktop typechecks; contract checks; repository lint; and `git diff --check`. It excludes SQLCipher-tagged integration and packaging. It does not separately repeat the Node, default Go, or desktop tests already owned by `pnpm test`.
- `verify:full` on Linux: exactly `verify:quick`; Linux is not a supported native SQLCipher build target.
- `verify:full` on `darwin/arm64`: `verify:quick`, `pnpm sqlcipher:test`, and `go test -race -tags tammy_sqlcipher ./services/core/internal/storage/sqlcipher/... -count=1`.
- `verify:full` on `win32/x64`: `verify:quick`, followed by `test:sqlcipher`, which requires the exact supported Windows LLVM/MSVC/SDK environment and runs the SQLCipher build plus tagged storage tests with `TAMMY_ORDINARY_SQLITE3` set to the built ordinary probe.
- `verify:release`: `darwin/arm64` only; clean-tree guard, production contracts, `verify:full`, ordinary `package:e2e`, then platform-neutral `release:check`. It does not sign a distribution candidate unless the operator separately invokes `release:candidate` or `deploy:mas` with explicit inputs.

Existing package scripts remain individually runnable for debugging. Documentation presents Task scenarios first and lists implementation commands as reference/troubleshooting details.

## 7. CI integration

GitHub Actions continues to own checkout, runner identity, virtual display provisioning, pinned Node/Go activation, Apple runner selection, Windows SDK/LLVM path validation, environment export, artifact upload, concurrency, and job timeouts.

After provisioning, workflows invoke these exact scenario tasks in order:

- Ubuntu calls `task ci:contracts`, then `task ci:linux`. `ci:contracts` verifies Task/toolchain pins, refuses any dirty tree, runs actionlint, protobuf-breaking policy tests, `pnpm contracts`, and descriptor evidence using the workflow-provided `TAMMY_EVIDENCE_SUBJECT_REVISION`. `ci:linux` runs Go race tests, desktop tests under the already provisioned `xvfb-run`, desktop typecheck, and lint.
- macOS arm64 calls `task ci:macos`. It verifies Task/toolchain pins and a clean tree; sets `TAMMY_SQLCIPHER_REPRODUCIBILITY=1` for SQLCipher Node tests; runs tagged SQLCipher Go race tests; and runs `pnpm desktop:e2e`.
- Windows Server smoke calls `task ci:windows-smoke`. It verifies the `WINDOWS_SERVER_SMOKE_ONLY` classification, Task/toolchain pins, protobuf process-safety tests, a clean tree, descriptor evidence using `TAMMY_EVIDENCE_SUBJECT_REVISION`, desktop tests, and desktop typecheck.
- Self-hosted Windows 11 calls `task ci:windows11` after Actions verifies Windows 11 23H2+, exact LLVM/MSVC/SDK/Perl paths, and exports `INCLUDE`, `LIB`, and `TAMMY_SQLCIPHER_COMSPEC`. The task verifies Task/toolchain pins and a clean tree; runs SQLCipher Node tests and the SQLCipher build; sets `TAMMY_ORDINARY_SQLITE3` to the newly built ordinary probe for the tagged Go tests; runs packaged E2E; and writes `apps/desktop/test-results/windows11-result.json` with the release-gate classification, status, run ID, and run attempt.

The CI tasks preserve evidence classification. Windows Server remains smoke-only and must never be described as Windows 11 release evidence. The Windows 11 task preserves the ordinary-probe environment handoff and packaged evidence generation. Workflow-level runner/path provisioning, virtual display availability, and failure artifact uploads remain in Actions because Taskfiles do not own GitHub runner or artifact retention.

A small tested cross-platform Node guard owns clean-tree validation for Task scenarios; YAML does not duplicate Bash and PowerShell implementations.

## 8. Local MAS package validation owner

Extend `scripts/package-macos-store.mjs`, not Task YAML, after `productbuild`:

1. run `/usr/sbin/pkgutil --check-signature <pkg>` and fail on a non-zero result;
2. calculate the package SHA-256 in Node and include it in the final single-line JSON result with the app and package paths;
3. run `/usr/sbin/spctl --assess --type install --verbose=4 <pkg>` while capturing its exit status and redacted textual result;
4. include the Gatekeeper assessment status in the JSON/release evidence.

`spctl` is observational for a pre-submission Mac App Store package: the runbook explicitly notes that Apple may reject local assessment before App Store processing. A non-zero `spctl` result does not invalidate an otherwise correctly signed MAS package, but command execution failure, missing output, or inability to classify the result fails closed. `pkgutil` signature failure is always fatal. The Node release-owner tests cover command order, fatal signature failure, SHA output, observational `spctl` acceptance/rejection classification, and secret redaction.

## 9. Testing strategy

Add a repository Node test that parses the Taskfile hierarchy as YAML and verifies the executable contract before implementation:

- all scenario files are included exactly once;
- every required public task exists and has a description;
- aliases resolve to real namespaced tasks;
- local implementation commands use `mise exec --`; CI tasks are the explicit Actions-pinned direct-command exception and `setup:tools` is the explicit bare-`mise install` repair exception;
- Task is pinned to `3.52.0` locally and checked at the start of every CI scenario;
- `deploy:mas` invokes local MAS packaging and contains no upload command or upload credential;
- no destructive reset/delete task exists;
- signed release tasks (`release:development`, `release:candidate`, and `deploy:mas`) declare the exact required `TAMMY_MACOS_*` inputs and an explicit macOS arm64 failure precondition; platform-neutral `release:check` does not;
- development summaries document persistent data and development-memory-anchor limits;
- CI tasks exist for each workflow scenario;
- workflow files invoke the canonical `ci:*` tasks after provisioning;
- ordered chains use sequential commands rather than potentially concurrent dependencies;
- safety/evidence tasks contain no Task caching fields that could skip owner checks;
- README and handbook examples name real tasks.

Run the test before adding Taskfiles and record RED because the hierarchy does not exist. Then add the minimal Taskfiles, update workflows and documentation, and verify GREEN.

Runtime validation includes:

- `task --list` and `task --summary <task>`;
- safe read-only tasks such as diagnostics and toolchain checks;
- focused Taskfile contract tests;
- root script tests, contracts, full tests, and typecheck;
- `actionlint` for workflow changes;
- ordinary package and packaged E2E when orchestration changes touch those paths;
- release check in repository mode without operator credentials;
- plan inspection proving `deploy:mas` cannot upload.

Do not execute distribution signing or fabricate Apple inputs merely to test orchestration. Unit-test the declarative surface and rely on the existing release-script tests for secret-dependent command planning.

## 10. Documentation

Update `README.md`, `docs/development/foundation.md`, and `docs/release/macos-app-store.md` so scenario tasks are the primary commands. Preserve the underlying command sequences as implementation reference and troubleshooting context.

The developer handbook must retain:

- exact persistent-data locations and development-memory-anchor behavior;
- recoverable reset guidance with the app/core stopped;
- module ownership and generated-code rules;
- focused-before-broad TDD workflow;
- loader/linker contention diagnostics without weakened product timeouts;
- packaged orphan evidence guidance.

The release runbook must retain every operator checklist item, environment variable, signed-build inspection step, manual Transporter upload instruction, and rollback/release-record requirement. Taskfiles improve discovery; they do not collapse legal or operational gates into a misleading one-command release claim.

## 11. Non-goals

- Uploading to App Store Connect.
- Notarization or Developer ID distribution.
- Automatic workspace-data reset or process termination.
- Replacing pnpm scripts, Node build/release modules, Electron Forge, Buf, Go tests, or GitHub Actions provisioning.
- Adding cloud deployment, ATO/SBR deployment, compatibility shims, or generalized workflow-generation machinery.
- Claiming local or simulated checks as Apple, App Store, ATO, SBR, or regulatory approval.
