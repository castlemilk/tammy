# Tammy

Tammy is a local-first Australian accounting application with desktop and web interfaces. Both use a supervised Go accounting core and keep the workspace in a local SQLCipher database.

Today the connected application can create and reopen an encrypted workspace, confirm its recovery code, sign in, create the organisation and AU chart of accounts, post balanced manual journals, show a trial balance, review retained documents, import and reconcile bank-statement rows, create a local BAS draft, and show consolidated local activity.

The invoice builder supports reviewed expense costs, editable line items, exact AUD/GST totals, encrypted drafts, immutable issued invoices, and offline customer PDFs adapted from pdfcn. Issuing atomically posts business income, receivables and basis-aware GST; separately recorded dated receipts reduce the balance owing. Older issued documents can remain unposted and require explicit review before adding them to accounts. Issue and PDF export do not email or deliver the invoice. See [Invoice builder](docs/development/invoice-builder.md).

The native Integrations menu can enable a session-scoped local API and stdio MCP adapter for the signed-in organisation. Clients can read data and stage an invoice proposal for human review; they cannot save or issue invoices or post accounting entries. Access requires Tammy's private connection file. See [Local API and MCP](docs/development/local-api-mcp.md) for pairing and client setup.

This is development software, not a production release. BAS workpapers are **drafts only**: there is no production SBR path and no BAS submit or lodge action. Local macOS packages use ad-hoc development signing and are not App Store approval or external assurance evidence.

The desktop foundation builds with electron-vite into `apps/desktop/out/main/index.js`, `apps/desktop/out/preload/index.cjs`, and `apps/desktop/out/renderer/index.html`. The app receives one fixed `window.conveyor` surface with exactly system, workspace, organisation, accounting, banking, documents, reporting, and sbr modules. Ordinary electron-builder artifacts live at `.tmp/electron-builder/ordinary/mac-arm64/Tammy.app` on macOS and `.tmp/electron-builder/ordinary/win-unpacked/Tammy.exe` on Windows.

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
mise exec -- task verify:quick
mise exec -- task verify:full
mise exec -- task test:desktop:dev
mise exec -- task integrations:test
mise exec -- task test:invoices
mise exec -- task build
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
mise exec -- task dev:sbr:evte
```

The first scenario retains a new isolated accounting root; the second is synthetic and network-disabled; the third prints the static external registration handoff checklist without accepting credentials and exits non-zero while blocked. EVTE remains blocked until externally issued signed inputs are installed. See [Local SBR readiness](docs/development/sbr-local-readiness.md) before handling a RAM machine credential.

Inspect the redacted Mac App Store state with `mise exec -- task release:state` and check the repository-owned profile with `mise exec -- task release:check`. Signed packaging uses `mise exec -- task release:development` or `mise exec -- task release:candidate`; `mise exec -- task deploy:mas` produces and locally validates a candidate only. These scenarios use the deterministic `.tmp/macos-release/<version>/build-<number>/` root. Apple certificates, profiles, legal decisions, signing, upload, review, and approval remain operator-owned; follow the [macOS App Store runbook](docs/release/macos-app-store.md).

## Web application

Build the accounting core and browser app, then start the same-origin local web server:

```sh
mise exec -- pnpm web:start
```

Open `http://127.0.0.1:4174`. The web app creates a separate encrypted workspace; it does not open the desktop application's data. The current service runs locally on macOS arm64 or Windows x64 and supports one business. Hosted authentication, multiple businesses, and public deployment are separate milestones in the [web application plan](docs/superpowers/plans/2026-10-27-web-application.md).

The browser uses a protected gateway with server-held core credentials. Read-only MCP tools and source-linked, rule-based insights use that same accounting service. See the [web development guide](docs/development/web-application.md) for development, validation, data storage, and MCP setup.

## Documentation

- [Product truth](PRODUCT.md) — current native capabilities, independent browser tools and their boundaries, with primary repo references.
- [Current technical state](docs/development/tech-state.md) — implemented, development-only, deferred, and external boundaries.
- [Xero replacement roadmap](docs/roadmap/xero-replacement-roadmap.md) — 259-feature parity matrix, milestone gates, and first engineering backlog.
- [Developer handbook](docs/development/foundation.md) — setup, commands, repository map, local data, and safe changes.
- [Local SBR readiness](docs/development/sbr-local-readiness.md) — accounting, simulator, credential handling, and external registration scenarios.
- [Local API and MCP](docs/development/local-api-mcp.md) — private connection-file pairing, read coverage, revocation, and stdio client setup.
- [Invoice builder](docs/development/invoice-builder.md) — expense-based line items, MCP proposals, draft/issue workflow, and customer PDFs.
- [Local accounting walkthrough](docs/development/local-accounting-walkthrough.md) — the current setup-to-audit journey.
- [User journey test matrix](docs/development/user-journey-test-matrix.md) — executable coverage by renderer, local core, and packaged Electron layer.
- [macOS App Store release runbook](docs/release/macos-app-store.md) — repository checks, signing inputs, packaging, inspection, metadata, upload, and remaining operator gates.
- [Application documentation and release design](docs/superpowers/specs/2026-08-10-application-documentation-macos-release-design.md) — approved direction, not current release evidence.
- [Accounting walkthrough design](docs/superpowers/specs/2026-08-09-accounting-tax-walkthrough-ui-design.md) and [implementation plan](docs/superpowers/plans/2026-08-09-accounting-tax-walkthrough-ui.md) — design intent and historical implementation detail.
