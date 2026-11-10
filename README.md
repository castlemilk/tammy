# Tammy

Tammy is a local-first Australian accounting application. The current development vertical slice runs an Electron desktop interface over a supervised Go core and keeps the workspace in a local SQLCipher database. A separate local [web application](#web-application) runs its own instance of the core behind a loopback Node gateway, with its own encrypted workspace and a smaller set of workflows.

Today the connected desktop application can create and reopen an encrypted workspace, confirm its recovery code, sign in, create the organisation and AU chart of accounts, post balanced manual journals, show a trial balance, review retained documents, import and reconcile bank-statement rows, create a local BAS draft, and show consolidated local activity.

The invoice builder supports reviewed expense costs, editable line items, exact AUD/GST totals, encrypted drafts, immutable issued invoices, and offline customer PDFs adapted from pdfcn. Issuing atomically posts business income, receivables and basis-aware GST; separately recorded dated receipts reduce the balance owing. Older issued documents can remain unposted and require explicit review before adding them to accounts. Issue and PDF export do not email or deliver the invoice. See [Invoice builder](docs/development/invoice-builder.md).

The native Integrations menu can enable a session-scoped local API and stdio MCP adapter for the signed-in organisation. Clients can read data and stage an invoice proposal for human review; they cannot save or issue invoices or post accounting entries. Access requires Tammy's private connection file. See [Local API and MCP](docs/development/local-api-mcp.md) for pairing and client setup. The web application has a separate read-only MCP adapter; see [Local MCP integrations](#local-mcp-integrations).

This is development software, not a production release. BAS workpapers are **drafts only**: there is no production SBR path and no BAS submit or lodge action. Local macOS packages use ad-hoc development signing and are not App Store approval or external assurance evidence.

The desktop foundation builds with electron-vite into `apps/desktop/out/main/index.js`, `apps/desktop/out/preload/index.cjs`, and `apps/desktop/out/renderer/index.html`. The app receives one fixed `window.conveyor` surface with exactly system, workspace, organisation, accounting, banking, basPreparation, documents, purchases, reporting, and sbr modules. Ordinary electron-builder artifacts live at `.tmp/electron-builder/ordinary/mac-arm64/Tammy.app` on macOS and `.tmp/electron-builder/ordinary/win-unpacked/Tammy.exe` on Windows.

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

The web application in `apps/web` is a React client served by a loopback Node gateway. The gateway supervises its own Go core, keeps the core capability and signed-in session on the server, and gives the browser an HttpOnly session cookie. It is not one of the public browser tools described in [Product truth](PRODUCT.md); those tools do not connect to a Tammy workspace.

After `mise exec -- task setup`, build the core and browser assets and start the gateway:

```sh
mise exec -- pnpm web:start
```

Open `http://127.0.0.1:4174`. By default the web app keeps its own encrypted workspace in `~/.tammy-web/development`; it does not open the desktop application's data. It supports one business and one signed-in browser session; unlocking in another browser with the right credentials signs the earlier one out. The browser covers workspace setup and unlock, ordinary accounts, balanced manual journals, the trial balance, document upload and review, bank import, matching and reconciliation, rule-based attention insights, and draft GST workpapers for periods within calendar 2024. It has no invoice, purchase, BAS preparation, export, SBR readiness, or activity screens. The core builds for macOS arm64 and Windows x64, and the web CI workflow is set up for macOS arm64. Hosted authentication, multiple businesses, and public deployment are later milestones in the [web application plan](docs/superpowers/plans/2026-10-27-web-application.md).

The [web application guide](docs/development/web-application.md) covers hot reload, configuration, data storage, validation, and MCP setup. The [local web API](docs/development/web-api.md) describes the gateway's routes, replay rules, and bounds.

## Local MCP integrations

Tammy has two separate local MCP integrations. Each reads only its own interface's workspace.

- Desktop local API and MCP (`packages/local-api`): the native Integrations menu writes a private connection file that authorises an Electron-owned loopback broker. It offers 14 read tools, one invoice proposal tool, and 10 resources for the signed-in organisation. See [Local API and MCP](docs/development/local-api-mcp.md).
- Web MCP adapter (`apps/web/server/mcp.ts`): it reads the web workspace through fixed gateway endpoints. The gateway and the adapter must share a `TAMMY_MCP_TOKEN` secret of at least 32 characters, and a workspace must be unlocked in the browser. It offers eight read-only tools, with no resources and no invoice proposal tool. See [Connect an MCP client](docs/development/web-application.md#connect-an-mcp-client).

Neither integration can post accounting entries, save or issue invoices, or lodge a BAS.

## Documentation

- [Product truth](PRODUCT.md) — current native capabilities, independent browser tools and their boundaries, with primary repo references.
- [Current technical state](docs/development/tech-state.md) — implemented, development-only, deferred, and external boundaries.
- [Xero replacement roadmap](docs/roadmap/xero-replacement-roadmap.md) — 259-feature parity matrix, milestone gates, and first engineering backlog.
- [Developer handbook](docs/development/foundation.md) — setup, commands, repository map, local data, and safe changes.
- [Local SBR readiness](docs/development/sbr-local-readiness.md) — accounting, simulator, credential handling, and external registration scenarios.
- [Local API and MCP](docs/development/local-api-mcp.md) — private connection-file pairing, read coverage, revocation, and stdio client setup.
- [Invoice builder](docs/development/invoice-builder.md) — expense-based line items, MCP proposals, draft/issue workflow, and customer PDFs.
- [Web application](docs/development/web-application.md) — local web app startup, hot reload, configuration, data storage, validation, and its read-only MCP adapter.
- [Local web API](docs/development/web-api.md) — gateway sessions, routes, request fields, replay rules, and current bounds.
- [Local accounting walkthrough](docs/development/local-accounting-walkthrough.md) — the current setup-to-audit journey.
- [User journey test matrix](docs/development/user-journey-test-matrix.md) — executable coverage by renderer, local core, and packaged Electron layer.
- [macOS App Store release runbook](docs/release/macos-app-store.md) — repository checks, signing inputs, packaging, inspection, metadata, upload, and remaining operator gates.
- [Application documentation and release design](docs/superpowers/specs/2026-08-10-application-documentation-macos-release-design.md) — approved direction, not current release evidence.
- [Accounting walkthrough design](docs/superpowers/specs/2026-08-09-accounting-tax-walkthrough-ui-design.md) and [implementation plan](docs/superpowers/plans/2026-08-09-accounting-tax-walkthrough-ui.md) — design intent and historical implementation detail.
