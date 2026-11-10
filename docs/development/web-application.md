# Tammy web application

The web application uses the same Go/SQLCipher accounting engine as the desktop app. A loopback Node gateway in `apps/web/server` supervises its own core process and holds the core capability and authenticated session; the React browser app receives an HttpOnly, SameSite session cookie and a constrained application API. `apps/web/server/facade.mjs` maps each allowlisted route to Connect RPCs through `@tammy/connect-client`.

This is a working single-business local web foundation. The core builds for macOS arm64 and Windows x64; there is no Linux hosted runtime, hosted identity, tenant isolation, bank feed, or ATO lodgement yet. The [implementation plan](../superpowers/plans/2026-10-27-web-application.md) separates those next milestones from the current release.

The browser covers a subset of the desktop app: workspace setup and unlock, ordinary accounts, balanced manual journals, the trial balance, document upload and review, bank import, matching and reconciliation, attention insights, and draft GST workpapers. It has no invoice, purchase, BAS preparation, export, SBR readiness or activity screens, and it does not offer the desktop's local API pairing.

## Run

Use the repository's pinned toolchain:

```sh
mise install
mise exec -- task setup
mise exec -- pnpm web:start
```

Open `http://127.0.0.1:4174`. `web:start` builds the native core and browser assets, then serves the application and API on one origin. Create a workspace, retain and confirm its recovery code, and complete business setup. Workspace encryption and administrator authentication use separate credentials, each 16 to 128 characters in the form; the core accepts 15 or more. Confirm the recovery code within 15 minutes; the recovery screen shows the deadline in local time. After the deadline, or after 5 incorrect codes, the setup is discarded and the setup form returns with the reason.

For Vite hot reload of the browser app, build the core once, then start the gateway and the Vite server in separate terminals:

```sh
mise exec -- pnpm core:build
TAMMY_WEB_ORIGIN=http://127.0.0.1:5173 mise exec -- pnpm web:server
```

```sh
mise exec -- pnpm web:dev
```

Open `http://127.0.0.1:5173`. The Vite server uses port 5173 and fails rather than choosing another port. It proxies `/api` to the gateway at `http://127.0.0.1:4174` and rewrites the Host header for the gateway, but the browser's Origin stays `http://127.0.0.1:5173`, so the gateway must be started with that `TAMMY_WEB_ORIGIN`. To run the gateway on another port, give the same value to `TAMMY_WEB_PORT` for `web:server` and to `TAMMY_WEB_PROXY_PORT` for `web:dev`. Keep the exact `127.0.0.1` host: the gateway validates Host and Origin. `web:server` does not watch files, so restart it after changing `apps/web/server`. The commands above use POSIX environment syntax; on PowerShell, set `$env:TAMMY_WEB_ORIGIN` and any port variable before starting each process.

## Configuration

| Variable | Read by | Default | Purpose |
| --- | --- | --- | --- |
| `TAMMY_WEB_PORT` | Gateway (`server/main.ts`) and MCP adapter (`server/mcp.ts`) | `4174` | Gateway port on `127.0.0.1`. The adapter uses it to reach the gateway. |
| `TAMMY_WEB_PROXY_PORT` | Vite development server (`vite.config.ts`) | `4174` | Gateway port that `web:dev` proxies `/api` to. Set it to the gateway's `TAMMY_WEB_PORT`. |
| `TAMMY_WEB_ORIGIN` | Gateway | `http://127.0.0.1:<TAMMY_WEB_PORT>` | Browser origin allowed to send writes. It must have the form `http://127.0.0.1:<port>`; use `http://127.0.0.1:5173` with `web:dev`. |
| `TAMMY_WEB_DATA_ROOT` | Gateway | `~/.tammy-web/development` | Dedicated encrypted data directory. |
| `TAMMY_CORE_BINARY` | Gateway | `apps/desktop/resources/core/<platform>-<arch>/tammy-core` (`tammy-core.exe` on Windows) | Core executable built by `pnpm core:build`. |
| `TAMMY_MCP_TOKEN` | Gateway and MCP adapter | Unset | Shared MCP secret of at least 32 characters. While it is unset, the gateway rejects every MCP request. |

The web package scripts run in `apps/web`, so give `TAMMY_WEB_DATA_ROOT` and `TAMMY_CORE_BINARY` as absolute paths. The gateway authenticates the core executable before starting it, so `TAMMY_CORE_BINARY` must name a regular file with a single hard link, not a symlink. The gateway refuses to start with an invalid port, an origin of another form, or a token shorter than 32 characters. The MCP adapter exits unless the token has at least 32 characters and the port is between 1 and 65535.

## Data and lifecycle

The default web data directory is `~/.tammy-web/development`. It is separate from Electron's user-data directory, and the gateway creates it with mode 0700. Do not point two running gateways at the same data directory.

The native core owns encrypted persistence, money calculations, accounting invariants and command idempotency. The gateway's `web-workspace.json` file in the data directory holds only workspace, setup and organisation identifiers, the setup stage and, while a setup is pending, its confirmation deadline and count of incorrect recovery codes. It holds no passwords, recovery codes or core credentials. The gateway does not start if the file is malformed, and it deletes the file when it discards a setup. If the core's workspace file exists without this record, setup stops with a 409 that names the data folder, because the core would refuse to create another workspace there.

Browser sessions end after 30 minutes without requests and whenever the gateway stops. The core session behind a signed-in browser also ends 30 minutes after sign-in or the last successful write, because reads, including MCP tools, do not extend it; the next read then returns the browser to the unlock screen. MCP access ends with the browser session. Reopen with the workspace passphrase and administrator credentials. If the setup or unlock form outlives its browser session, for example across a gateway restart, the browser starts a new session and sends the request once more. Unlocking in another browser, a private window, or the same browser after it lost its cookie takes over from the earlier session. The gateway first signs in to the open workspace with the supplied username and password, so wrong ones leave the earlier session signed in, and after three failures in 15 minutes it refuses further takeovers for a while, before the core would lock the administrator account. Right ones sign the earlier session out; the workspace is then locked and unlocked with the supplied passphrase. See [authentication](web-api.md#authentication) for the exact rules.

Closing the server stops its supervised core. If the core exits unexpectedly, the gateway notices within about 250 ms, writes `Tammy web stopped: the accounting core stopped unexpectedly (<code>). Start the web server again.` to stderr, closes its listener and open connections, stops the supervisor and exits with status 1. Requests in that interval can still receive a 500 response.

As with desktop development, the core runs with `--development-memory-anchors`. This is development behavior, not production security or durable authentication-rate-limit evidence. The HTTP listener stays on loopback; this profile is not suitable for public exposure. The flag also clears the core's attempt journals at every start, so the gateway keeps its own count of incorrect recovery codes. The core has no operation to cancel a pending setup, so the gateway discards one by sending five deliberately invalid confirmations, which make the core delete the pending workspace; otherwise its leftover files would block the next setup.

## Accounting workflows

- Setup creates the organisation as an Australian private company with non-cash GST, quarterly GST reporting and a June year end, and records the tax-rule bundle shipped with the core, using the same reference as desktop setup. The browser does not offer the desktop's choice of cash or non-cash GST. Organisations created by earlier development builds of the web app keep an older reference (`e2f9cd…`) that the desktop treats as a known legacy reference. The repair for that reference needs a fresh TOTP factor, which web administrators do not enrol, so the web app cannot apply it; see [Local BAS preparation rules](bas-preparation.md).
- Add ordinary accounts before posting a manual journal. Installed system/control accounts remain protected.
- Post a balanced journal with explicit intent, then inspect the journal and trial balance. Retrying the same operation identifier preserves the original accounting effect.
- Review document details explicitly. A reviewed document is not represented as a posted purchase or paid bill.
- PDF, PNG and JPEG intake is limited to 512 KiB per file, so a base64 upload fits the gateway's 768 KiB request limit and the core's 1 MiB message limit. The browser extracts text only from uncompressed native PDF text operators; images and other PDFs need manual entry, and there is no file preview. Review extracted or manually entered details before saving.
- Bank matching and completed reconciliation are separate actions.
- The GST & BAS screen creates a **Draft — not lodged** workpaper for any period within calendar 2024. That covers the second half of AU tax year 2024 and the first half of tax year 2025; the desktop instead accepts periods ending in AU tax years 2024 to 2027, from 1 July 2023 to 30 June 2027. The screen shows no figure (—) for sales (G1), GST on sales (1A) or the net GST position. GST credits (1B) count only posted purchases, which the browser cannot create. Reviewed documents not yet posted as purchases are listed as provisional GST candidates whatever their date, under the core's total for all of them, labelled `Total provisional candidates, all dates`. When records change after the draft was created, the screen says so and asks for another draft, and it lists the core's review warnings. See [current bounds](web-api.md#current-bounds).
- Insights are deterministic rules over the overview counts: documents awaiting review, unmatched bank lines and draft GST workpapers. Each links to the relevant screen. They do not represent an LLM connection or autonomous bookkeeping.

## Validation

```sh
mise exec -- pnpm web:typecheck
mise exec -- pnpm web:test
mise exec -- pnpm web:build
mise exec -- pnpm web:e2e
```

`web:test` runs the browser unit tests and the server tests in `apps/web/server`: facade and gateway tests, gateway security checks, and real-core integration tests for setup, journal replay and restart, recovery-code limits across a restart, browser takeover and its attempt limit, setup leaving no workspace file behind a rejected password, the MCP adapter and core exit. The integration test starts the MCP adapter with the running Node binary and `tsx` from `apps/web`, so it does not need mise. Build the native core first when running it independently. `web:e2e` builds the core, then Playwright starts a gateway on port 4176 with a temporary data directory and a Vite server on port 5174, so it does not touch the default development ports or data. Install its browser once with `mise exec -- pnpm --dir apps/web exec playwright install chromium`. The [application API guide](web-api.md) describes the route and credential contract.

The dedicated `web-ci.yml` workflow is set up to run the typecheck, build, web tests including the gateway/MCP integration, native lifecycle regressions and browser journey on the hosted `macos-15` arm64 runner. For broader native tests on macOS, use a canonical temporary directory (`TMPDIR=/private/tmp`) because the core intentionally rejects symlinked path components; the system `/var` temporary path can trigger those protections in test fixtures.

## Connect an MCP client

This adapter is separate from the desktop's [local API and MCP](local-api-mcp.md): it uses no connection file, reads only the web workspace, and cannot stage invoice proposals.

The adapter uses stdio and exposes eight read-only tools for the overview, accounts, journals, trial balance, document metadata, bank lines, draft GST workpaper and insights. It has no resources and no posting, payment, arbitrary RPC or SQL tools. The [API guide](web-api.md#read-only-mcp) lists the tool names. A request that the gateway rejects with a 4xx status other than 401 comes back as a tool error with the gateway's code and message, such as `invalid_argument: Use a valid date in YYYY-MM-DD format.`

Set a locally generated secret of at least 32 characters as `TAMMY_MCP_TOKEN` in both the gateway and the MCP client's process environment. Keep it out of source control. Start the gateway with that environment, then unlock the workspace in the browser. Configure the MCP client to launch:

```sh
mise exec -- pnpm --silent --dir /absolute/path/to/tammy/apps/web mcp
```

Replace the repository path with the actual absolute path and set the client's working directory to the repository root so mise resolves the pinned toolchain. If the gateway uses another port, supply the same `TAMMY_WEB_PORT` to the adapter. The stdio adapter contacts only `127.0.0.1` and the gateway's fixed MCP read endpoints. Logging out, expiry or server shutdown removes its active accounting session; a machine token alone does not unlock the workspace. When another browser unlocks, the adapter reads that browser's session.

This transport is for a trusted local MCP client. Remote MCP, OAuth, independently scoped public API keys and hosted organisations are future work.

## Design

The existing [design context](../../.impeccable.md) and [accounting wireframes](../../output/imagegen/tammy-accounting-tax-wireframes.png) establish forest green actions, warm off-white surfaces, compact tables, and a serif Tammy wordmark. The interface typeface is IBM Plex Sans, bundled from `@fontsource-variable/ibm-plex-sans` and served by the gateway, so the browser makes no external font requests. Layout spacing sits on a 4px grid apart from icon gaps and 2px nudges. Interface text uses five sizes at weights 400 and 500, the bold serif wordmark is the one exception, and totals carry a ledger double rule. The [web overview concept](../../output/imagegen/tammy-web-overview.png) adapts that language to a wider browser layout.

The web concept was generated with the built-in image tool using the existing wireframe as reference. Its brief specified the real Overview/Docs/Banking/BAS attention projections, a next-step list, recent-journal table, and source-linked intelligence; it explicitly prohibited fabricated financial metrics, charts, and unsupported automation claims.

The implementation was visually checked in the in-app browser, then compared directly with the concept using `view_image` and a real-server Playwright screenshot at 1536 × 1024. The review covered the forest/warm palette, serif wordmark and text hierarchy, navigation rail, summary/attention/next-step layout, journal table, and footer. Tablet and mobile screenshots at 785 and 390 pixels were also inspected; cramped tablet summaries were fixed. Intentional differences are capability-driven copy such as “No draft” for an absent GST draft, protected-account setup wording, and omitting an unimplemented Settings link. No material layout mismatches remained in the reviewed overview; other workflows follow the same component system.
