# Tammy

Tammy is a local-first Australian accounting desktop application. The current development vertical slice runs an Electron interface over a supervised Go core and keeps the workspace in a local SQLCipher database.

Today the connected application can create and reopen an encrypted workspace, confirm its recovery code, sign in, create the organisation and AU chart of accounts, post balanced manual journals, show a trial balance, review retained documents, import and reconcile bank-statement rows, create a local BAS draft, and show consolidated local activity.

This is development software, not a production release. BAS workpapers are **drafts only**: Tammy has no declaration, lodgement, submission, or ATO/SBR transport in the current app. Local macOS packages use ad-hoc development signing and are not App Store approval or external assurance evidence.

## Quick start

Use the pinned toolchain from the repository root:

```sh
rtk mise install
rtk mise exec -- corepack prepare pnpm@11.15.0 --activate
rtk mise exec -- pnpm install --frozen-lockfile
rtk mise exec -- pnpm check:toolchain
rtk mise exec -- pnpm desktop:start
```

Development mode keeps its encrypted workspace below Electron's user-data directory in `local-core-development`. See the [developer handbook](docs/development/foundation.md#development-workspace) before resetting local data.

## Principal checks

```sh
rtk mise exec -- pnpm contracts
rtk mise exec -- go test ./services/core/...
rtk mise exec -- pnpm desktop:test
rtk mise exec -- pnpm desktop:typecheck
rtk mise exec -- pnpm lint
rtk git diff --check
```

Build an ad-hoc signed local macOS package with `rtk mise exec -- pnpm desktop:package`. Exercise that package with `rtk mise exec -- pnpm desktop:e2e`.

Check the repository-owned Mac App Store profile with `rtk mise exec -- pnpm check:macos-store`. Signed packaging and upload require operator-owned Apple certificates, profiles, legal decisions, metadata, and approval; follow the [macOS App Store runbook](docs/release/macos-app-store.md).

## Documentation

- [Current technical state](docs/development/tech-state.md) — implemented, development-only, deferred, and external boundaries.
- [Developer handbook](docs/development/foundation.md) — setup, commands, repository map, local data, and safe changes.
- [Local accounting walkthrough](docs/development/local-accounting-walkthrough.md) — the current setup-to-audit journey.
- [macOS App Store release runbook](docs/release/macos-app-store.md) — repository checks, signing inputs, packaging, inspection, metadata, upload, and remaining operator gates.
- [Application documentation and release design](docs/superpowers/specs/2026-08-10-application-documentation-macos-release-design.md) — approved direction, not current release evidence.
- [Accounting walkthrough design](docs/superpowers/specs/2026-08-09-accounting-tax-walkthrough-ui-design.md) and [implementation plan](docs/superpowers/plans/2026-08-09-accounting-tax-walkthrough-ui.md) — design intent and historical implementation detail.
