# Tammy

Tammy is a local-first Australian accounting desktop application. The current development vertical slice runs an Electron interface over a supervised Go core and keeps the workspace in a local SQLCipher database.

Today the connected application can create and reopen an encrypted workspace, confirm its recovery code, sign in, create the organisation and AU chart of accounts, post balanced manual journals, show a trial balance, review retained documents, import and reconcile bank-statement rows, create a local BAS draft, and show consolidated local activity.

This is development software, not a production release. BAS workpapers are **drafts only**: there is no production SBR path and no BAS submit or lodge action. Local macOS packages use ad-hoc development signing and are not App Store approval or external assurance evidence.

## Quick start

Install the pinned toolchain once, then use Task scenarios from the repository root:

```sh
mise install
mise exec -- task setup
mise exec -- task dev
```

`setup` repairs the pinned tools, installs frozen dependencies, and checks the toolchain; `dev` starts the supported local application. Development mode keeps its encrypted workspace below Electron's user-data directory in `local-core-development`. See the [developer handbook](docs/development/foundation.md#development-workspace) before resetting local data.

## Common scenarios

```sh
mise exec -- task --list
mise exec -- task test
mise exec -- task verify
mise exec -- task package
mise exec -- task package:e2e
mise exec -- task diagnose:data
```

`package` authenticates an ordinary local artifact; `package:e2e` additionally proves the packaged runtime, isolated user data, and no orphan core. Neither is production or App Store evidence. The developer handbook keeps the underlying pnpm, Go, and Node commands for focused implementation and troubleshooting.

For the macOS SBR readiness work, use the scenario-specific front doors:

```sh
mise exec -- task dev:accounting:fresh
mise exec -- task dev:sbr:simulator
mise exec -- task sbr:registration:check
```

The first scenario retains a new isolated accounting root; the second is synthetic and network-disabled; the third prints the static external registration handoff checklist without accepting credentials and exits non-zero while blocked. EVTE remains blocked until externally issued signed inputs are installed. See [Local SBR readiness](docs/development/sbr-local-readiness.md) before handling a RAM machine credential.

Check the repository-owned Mac App Store profile with `mise exec -- task release:check`. Signed packaging uses `mise exec -- task release:development` or `mise exec -- task release:candidate`; `mise exec -- task deploy:mas` produces and locally validates a candidate only. Apple certificates, profiles, legal decisions, metadata, upload, and approval remain operator-owned; follow the [macOS App Store runbook](docs/release/macos-app-store.md).

## Documentation

- [Current technical state](docs/development/tech-state.md) — implemented, development-only, deferred, and external boundaries.
- [Developer handbook](docs/development/foundation.md) — setup, commands, repository map, local data, and safe changes.
- [Local SBR readiness](docs/development/sbr-local-readiness.md) — accounting, simulator, credential handling, and external registration scenarios.
- [Local accounting walkthrough](docs/development/local-accounting-walkthrough.md) — the current setup-to-audit journey.
- [User journey test matrix](docs/development/user-journey-test-matrix.md) — executable coverage by renderer, local core, and packaged Electron layer.
- [macOS App Store release runbook](docs/release/macos-app-store.md) — repository checks, signing inputs, packaging, inspection, metadata, upload, and remaining operator gates.
- [Application documentation and release design](docs/superpowers/specs/2026-08-10-application-documentation-macos-release-design.md) — approved direction, not current release evidence.
- [Accounting walkthrough design](docs/superpowers/specs/2026-08-09-accounting-tax-walkthrough-ui-design.md) and [implementation plan](docs/superpowers/plans/2026-08-09-accounting-tax-walkthrough-ui.md) — design intent and historical implementation detail.
