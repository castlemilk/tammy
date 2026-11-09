# Tammy web application

The web application uses the same Go/SQLCipher accounting engine as the desktop app. A loopback Node gateway holds the core capability and authenticated session; the React browser app receives an HttpOnly, SameSite session cookie and a constrained application API.

This is a working single-business local web foundation. The current core runs on macOS arm64 and Windows x64; it does not yet provide a Linux hosted runtime, hosted identity, tenant isolation, bank feeds, or ATO lodgement. The [implementation plan](../superpowers/plans/2026-10-27-web-application.md) separates those next milestones from the current release.

## Run

Use the repository's pinned toolchain:

```sh
mise install
mise exec -- pnpm install --frozen-lockfile
mise exec -- pnpm web:start
```

Open `http://127.0.0.1:4174`. `web:start` builds the native core and browser assets, then serves the application and API on one origin. Create a workspace, retain and confirm its recovery code, and complete business setup. Workspace encryption and administrator authentication use separate credentials.

For Vite hot reload, build the core once and run these in separate terminals:

```sh
mise exec -- pnpm core:build
TAMMY_WEB_ORIGIN=http://127.0.0.1:5173 mise exec -- pnpm web:server
```

```sh
mise exec -- pnpm web:dev
```

Open `http://127.0.0.1:5173`. The proxy forwards `/api` to the gateway on port 4174. Keep the exact `127.0.0.1` host: the gateway validates Host and Origin. The hot-reload commands above use POSIX environment syntax; on PowerShell set `$env:TAMMY_WEB_ORIGIN` before starting the server.

## Data and lifecycle

The default web data directory is `~/.tammy-web/development`. It is separate from Electron's user-data directory. `TAMMY_WEB_DATA_ROOT` selects another dedicated directory, `TAMMY_WEB_PORT` changes the gateway port, and `TAMMY_CORE_BINARY` selects a built core. Do not point two running gateways at the same data directory.

The native core owns encrypted persistence, money calculations, accounting invariants and command idempotency. Gateway metadata contains workspace/business identifiers, not passwords or core credentials. Browser and MCP sessions expire when the gateway stops; reopen with the workspace passphrase and administrator credentials. Closing the server stops its supervised core.

As with desktop development, the core runs with `--development-memory-anchors`. This is development behavior, not production security or durable authentication-rate-limit evidence. The HTTP listener stays on loopback; this profile is not suitable for public exposure.

## Accounting workflows

- Add ordinary accounts before posting a manual journal. Installed system/control accounts remain protected.
- Post a balanced journal with explicit intent, then inspect the journal and trial balance. Retrying the same operation identifier preserves the original accounting effect.
- Review document details explicitly. A reviewed document is not represented as a posted purchase or paid bill.
- PDF, PNG and JPEG intake is limited to 512 KiB per file in this web release, within the existing core transport limit. Review extracted or manually entered details before saving.
- Bank matching and completed reconciliation are separate actions.
- GST workpapers are **Draft — not lodged**. The currently supported reviewed-document reporting period is 2024; current-year support is not implied.
- Intelligence is deterministic and linked to source records. It does not represent an LLM connection or autonomous bookkeeping.

## Validation

```sh
mise exec -- pnpm web:typecheck
mise exec -- pnpm web:test
mise exec -- pnpm web:build
mise exec -- pnpm web:e2e
```

`web:test` includes gateway security and real-core integration checks; build the native core first when running it independently. Browser tests use isolated temporary workspace data. The [application API guide](web-api.md) describes the route and credential contract.

The dedicated `web-ci.yml` workflow runs the build, gateway/MCP integration, native lifecycle regressions and browser journey on macOS arm64. For broader native tests on macOS, use a canonical temporary directory (`TMPDIR=/private/tmp`) because the core intentionally rejects symlinked path components; the system `/var` temporary path can trigger those protections in test fixtures.

## Connect an MCP client

The MCP adapter uses stdio and exposes read-only tools for the overview, accounts, journals, trial balance, document metadata, bank lines, draft GST workpaper and insights. It has no posting, payment, arbitrary RPC or SQL tools.

Set a locally generated secret of at least 32 characters as `TAMMY_MCP_TOKEN` in both the gateway and the MCP client's process environment. Keep it out of source control. Start the gateway with that environment, then unlock the workspace in the browser. Configure the MCP client to launch:

```sh
mise exec -- pnpm --silent --dir /absolute/path/to/tammy/apps/web mcp
```

Replace the repository path with the actual absolute path and set the client's working directory to the repository root so mise resolves the pinned toolchain. If the gateway uses another port, supply the same `TAMMY_WEB_PORT` to the adapter. The stdio adapter contacts only `127.0.0.1` and the explicitly enabled gateway read endpoints. Logging out, expiry or server shutdown removes its active accounting session; a machine token alone does not unlock the workspace.

This transport is for a trusted local MCP client. Remote MCP, OAuth, independently scoped public API keys and hosted organisations are future work.

## Design

The existing [design context](../../.impeccable.md) and [accounting wireframes](../../output/imagegen/tammy-accounting-tax-wireframes.png) establish forest green actions, warm off-white surfaces, compact tables, and a serif Tammy wordmark. The interface typeface is IBM Plex Sans, bundled from `@fontsource-variable/ibm-plex-sans` and served by the gateway, so the browser makes no external font requests. Layout spacing sits on a 4px grid apart from icon gaps and 2px nudges. Interface text uses five sizes at weights 400 and 500, the bold serif wordmark is the one exception, and totals carry a ledger double rule. The [web overview concept](../../output/imagegen/tammy-web-overview.png) adapts that language to a wider browser layout.

The web concept was generated with the built-in image tool using the existing wireframe as reference. Its brief specified the real Overview/Docs/Banking/BAS attention projections, a next-step list, recent-journal table, and source-linked intelligence; it explicitly prohibited fabricated financial metrics, charts, and unsupported automation claims.

The implementation was visually checked in the in-app browser, then compared directly with the concept using `view_image` and a real-server Playwright screenshot at 1536 × 1024. The review covered the forest/warm palette, serif wordmark and text hierarchy, navigation rail, summary/attention/next-step layout, journal table, and footer. Tablet and mobile screenshots at 785 and 390 pixels were also inspected; cramped tablet summaries were fixed. Intentional differences are capability-driven copy such as “No draft” for an absent GST draft, protected-account setup wording, and omitting an unimplemented Settings link. No material layout mismatches remained in the reviewed overview; other workflows follow the same component system.
