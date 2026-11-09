# Local web API

Tammy's browser API is a local development interface over the existing Go accounting core. It serves one encrypted workspace and one active signed-in browser session. It is not a hosted multi-tenant API, and the browser session is not a public API key.

The gateway binds to `127.0.0.1`; its default port is `4174`. Its expected browser origin defaults to that address and can be configured with `TAMMY_WEB_ORIGIN` for the Vite development origin. Requests must use the gateway's exact Host. Browser mutations require the configured Origin and `Content-Type: application/json`. Credentials and response data must not be logged.

## Authentication

Start with `GET /api/session`. This creates a random `tammy_session` HttpOnly, SameSite=Strict cookie when needed and returns `setup`, `recovery`, `locked`, or `authenticated`. The gateway rotates the cookie after authentication and retains the core session and organisation context on the server. Financial queries and commands require the authenticated cookie. Client-supplied core authentication or organisation fields are not used to select an account.

Sessions expire after 30 minutes without use. `POST /api/session/logout` accepts `{}` and invalidates the browser session, signs out from the core, and attempts to lock the workspace. The development server uses loopback HTTP; it is not an HTTPS production session configuration. Do not expose it through a public tunnel or bind it to an external interface.

| Method and route | Purpose |
| --- | --- |
| `GET /api/session` | Obtain browser session and current setup/authentication state |
| `POST /api/workspace/create` | Create encrypted workspace and return the one-time recovery code |
| `POST /api/workspace/confirm` | Confirm saved recovery code, sign in and complete organisation setup |
| `POST /api/workspace/unlock` | Unlock existing workspace and sign in |
| `POST /api/session/logout` | Sign out and lock the workspace |

Workspace creation accepts `displayName`, `username`, `workspacePassphrase`, and `administratorPassword`. Confirmation accepts `setupId`, `recoveryCode`, `username`, `administratorPassword`, `businessLegalName`, `businessDisplayName`, and `abn`; resuming setup after a restart may also require `workspacePassphrase`. Unlock accepts `workspacePassphrase`, `username`, and `administratorPassword`. Keep passwords and the recovery code out of browser storage and saved request examples.

## Accounting routes

| Method and route | Request / result |
| --- | --- |
| `GET /api/overview` | Attention counts and source references |
| `GET /api/accounts` | Account list |
| `POST /api/accounts` | `idempotencyKey`, `code`, `name`, `type`; returns created account |
| `GET /api/journals` | Journals with lines and account names |
| `POST /api/journals` | `idempotencyKey`, `date`, `description`, `lines`; returns posted journal |
| `GET /api/trial-balance?asOfDate=YYYY-MM-DD` | Trial balance and exact debit/credit totals; defaults to today's Melbourne date |
| `GET /api/documents` | Retained document review records |
| `POST /api/documents/upload` | `idempotencyKey`, `sourceDisplayName`, `mimeType`, `originalBase64`, optional `extractedText` and `candidate`; retains document for review |
| `POST /api/documents/:id/review` | `idempotencyKey`, `version`, supplier/reference/date and subtotal/GST/total amounts; returns reviewed document |
| `GET /api/banking` | Imported statement lines and independent banking summary |
| `POST /api/banking/import` | `idempotencyKey`, `openingBalanceMinor`, `lines` with `date`, `description`, `amountMinor` |
| `POST /api/banking/:id/match` | `idempotencyKey`, `version`, `reference`; returns matched line |
| `POST /api/banking/reconcile` | `idempotencyKey`; returns completed reconciliation count and balance |
| `GET /api/bas` | Current workpaper, or `workpaper: null` |
| `POST /api/bas` | `idempotencyKey`, `periodStart`, `periodEnd`; returns draft workpaper |
| `GET /api/insights` | Deterministic attention suggestions derived from core records |
| `GET /api/capabilities` | API, MCP, intelligence and reporting availability/boundaries |

Journal lines carry `accountId`, `description`, `debitMinor`, and `creditMinor`. Document reviews carry `supplierName`, `invoiceNumber`, `date`, `subtotalMinor`, `gstMinor`, and `totalMinor`. Account creation currently accepts `asset`, `liability`, `equity`, `income`, or `expense`.

Uploads accept PDF, PNG and JPEG with matching file signatures, at most 512 KiB decoded. `originalBase64` must be canonical base64. Optional extracted text is limited to 32 KiB; it is supplied text, not a promise of server OCR. Candidate fields use the document-review shape and remain subject to human review. JSON request bodies are capped at 768 KiB. Retention and review do not create accounting postings.

The browser attempts bounded extraction of uncompressed native PDF text operators before upload. This is not a general PDF renderer, font decoder or OCR engine. Images and unsupported PDF content need manual entry. The review screen has no source-file preview; currently extracted text is visible immediately after upload, rather than fetched back as a source-preview API.

## Exact values and replay

Money is AUD integer cents. Inputs accept safe JSON integers or canonical decimal integer strings within signed 64-bit range. Outputs use a JSON number when safe and a decimal string otherwise. Consumers must support both forms without converting large strings through JavaScript `Number`. Dates are valid calendar dates in `YYYY-MM-DD` form. Resource IDs and operation keys are canonical UUIDv7 values; optimistic versions are positive integer values, preferably decimal strings.

Financial command requests carry an `idempotencyKey`. Generate it once when the user submits an operation, and never generate a fresh key merely because a network failure left the outcome unknown. Manual journal posting uses core idempotency: retrying the unchanged command with its original key returns the retained journal; a different command under that key conflicts. A changed key represents new intent and can create a second posting, so inspect an uncertain result before editing and resubmitting.

Replay guarantees are operation-specific. The current document-review and bank-line-match handlers require a command context but rely on `version`, not retained idempotency results. After a successful write, repeating their old version returns a conflict. Refetch the record after an uncertain response and inspect its state before deciding whether another action is required. Do not assume every route has the same replay guarantee as journal posting.

The gateway derives deterministic journal line IDs from the operation key and line positions. The core checks balancing, account permissions, versions and posting constraints. Browser totals are previews, not accounting authority. Reviewing a document does not post a journal. Matching a bank line does not complete reconciliation. A BAS workpaper is always a draft and is never lodged.

Errors use an `error` object with a safe `message` and stable `code`: `invalid_request` or `invalid_argument` with HTTP 400, `unauthenticated` with 401, `forbidden` with 403, `not_found` with 404, `conflict` with 409, `request_too_large` with 413, `resource_exhausted` with 429, or `internal` with 500. A conflict requires examining the current record/operation before changing or retrying it; it is not a reason to generate a fresh key and blindly repost.

## Read-only MCP

The MCP server uses stdio. Set the same secret `TAMMY_MCP_TOKEN` of at least 32 characters on the gateway and MCP process, and set `TAMMY_WEB_PORT` to the gateway's port if it differs from 4174. Configure the MCP client to launch `mise exec -- pnpm --silent --dir /absolute/path/to/tammy/apps/web mcp`, substituting the repository path. The `--silent` flag keeps package-manager status output out of the protocol stream. Supply the token through the client's environment configuration; never put a real token into documentation, command-line arguments or a browser page.

The tools are `get_overview`, `list_accounts`, `list_journals`, `get_trial_balance`, `list_documents`, `list_bank_lines`, `get_gst_workpaper`, and `get_insights`. `get_trial_balance` accepts optional `asOfDate`; other tools take no arguments. All are marked read-only and call a fixed query allowlist. They cannot create workspaces, authenticate users, post journals, modify documents or invoke arbitrary core methods.

MCP requires both the configured token and an active authenticated browser workspace. It follows that single workspace; there is no tenant selector. Logout or session expiry removes that access. Successful reads refresh the shared session's idle-use timestamp. MCP returns JSON as text content, retaining exact money values and list-bound metadata. This is a local stdio integration; there is no public remote MCP endpoint or OAuth/API-key lifecycle in this release.

## Current bounds

List reads request at most 200 records and return `page: { limit, returnedCount, mayHaveMore, cursorSupported }`. `mayHaveMore` is conservatively true when 200 rows were returned; it is not an exact total. The current local core does not support continuation cursors on these routes. Do not treat a full 200-row list as exhaustive or derive financial totals from it; use the trial balance and banking summary projections. The core attention projection currently counts all retained records, including fields whose protobuf names end in `InPeriod`; these are not verified period-filtered analytics.

Reporting supports the 2024 reviewed-document GST workpaper. Only reviewed-document purchase GST credits are calculated. `salesG1Minor`, `gstOnSales1aMinor` and `netGstPayableMinor` are `null`, rather than exposing the core's unsupported placeholder zeroes as real sales/net results; the workpaper's `limitations` explains this boundary. It does not provide a complete BAS, current-year coverage, declaration or ATO lodgement. Insights are deterministic rules, not model predictions. This release has no remote bank feeds, public API keys, tenant selection or autonomous financial mutation.

“Current workpaper” means the latest retained draft. The core does not verify whether its sources have changed or mark it stale automatically. Review its source records and regenerate the draft after relevant changes; do not infer freshness from its presence or the Overview draft count.

The implementation source is `apps/web/server/gateway.mjs` for routes/security, `apps/web/server/facade.mjs` for validation and protobuf mapping, `apps/web/src/api.ts` for the browser client, and the generated schemas in `packages/connect-client/src/gen/tammy/v1/` for core contracts. The browser DTO layer deliberately exposes only the connected subset; changes should update server mappings, browser types, contract tests and this document together.
