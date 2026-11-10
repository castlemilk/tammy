# Tammy Web Application Implementation Plan

> **For agentic workers:** Use the already assigned implementation agents and an independent adversarial review. Steps use checkbox syntax for tracking; actual verification receipts, rather than checkboxes alone, determine completion.

**Goal:** Build a polished, usable browser application over Tammy's existing accounting engine, with working accounting journeys, a scoped read-only MCP interface, and evidence-backed attention insights.

**Architecture:** A same-origin Node gateway supervises the existing Go core and uses the generated protobuf clients over its authenticated loopback TLS transport. Go remains the sole authority for accounting, persistence, permissions and idempotency. The browser and MCP adapter use an explicitly allowlisted application interface; a gateway session holds core authentication credentials outside browser storage.

**Tech Stack:** React, TypeScript, Vite, existing Tammy design tokens, Node HTTP gateway, generated Connect/protobuf client, Go/SQLCipher core, Playwright.

## Scope and evidence

This increment is a real browser application running against a local, single-workspace development service. It is not hosted multi-tenant accounting. The core currently supports SQLCipher on macOS arm64 and Windows amd64, activates one workspace, and contains development-only security anchors and local audit adapters. These constraints must be documented accurately.

Reuse the connected capability set in `docs/development/tech-state.md`: workspace setup/unlock, recovery confirmation, administrator sign-in, organisation creation, accounts, balanced manual journals, trial balance, document intake/review, bank statement import/match/reconciliation, BAS drafts, overview and activity. Generated but unimplemented RPCs are not product features. Sales invoicing, complete payables, bank feeds, ATO lodgement, autonomous posting, and hosted production security remain later work.

The design source is `.impeccable.md`, existing renderer tokens, and `output/imagegen/tammy-accounting-tax-wireframes.png`: warm off-white, forest green, compact tables, quiet hierarchy, restrained borders, and serif Tammy wordmark. No synthetic financial metrics or silently seeded customer data.

## Chunk 1: Working browser release

### Task 1: Application and shared boundary

**Files:** Create `apps/web/package.json`, `apps/web/index.html`, `apps/web/vite.config.ts`, `apps/web/tsconfig.json`; update root `package.json` scripts. Organise browser components under `apps/web/src/`, gateway modules under `apps/web/server/`, and browser tests under `apps/web/tests/`.

- [x] Establish build, typecheck, unit and E2E commands using existing pinned tooling and workspace dependencies.
- [x] Reuse the generated schemas and the existing `apps/desktop/src/main/core-client.ts` transport semantics. Extract shared runtime helpers only where required; do not rewrite accounting rules in TypeScript.
- [x] Define a fixed list of exposed operations with request/response schemas and read/write classification. Reject arbitrary method names, URLs, filesystem paths and unsupported capability routes.
- [x] Preserve integer minor units and protobuf integer semantics through JSON serialization. Format money only at the presentation boundary.

### Task 2: Gateway and browser authentication

**Files:** `apps/web/server/` runtime, API, session and security modules with focused tests.

- [x] Launch a dedicated core with a distinct web-development data directory, await authenticated readiness, and terminate the exact child on shutdown. Never reuse or reset the user's desktop data directory.
- [x] Bind the development gateway to loopback. Serve browser/API under one origin; development proxy configuration must preserve the same protection.
- [x] Keep the core capability, TLS material and core authentication context server-side. Use random HttpOnly SameSite session cookies, expiry, logout invalidation and no-store responses for sensitive data. Use Secure cookies when HTTPS is enabled; plainly document loopback HTTP development behavior.
- [x] Enforce expected Host and Origin on browser mutations, JSON content type, bounded bodies, and explicit operation allowlists. Reject unauthenticated financial reads/writes and browser-supplied authentication overrides.
- [x] Preserve journal operation IDs on retries. Ambiguous journal responses freeze the original payload/key across in-app navigation; a real response-loss E2E verifies one posting. Other route replay guarantees remain operation-specific as documented in the API guide.
- [x] Map safe typed core errors to useful user actions without leaking passwords, source contents, capability values or local paths.
- [x] Reconcile document-size claims with the core transport's current 1 MiB global limit. Either fix and test limits through the entire path or advertise/enforce the smaller supported boundary.

### Task 3: Polished real accounting workflows

**Files:** `apps/web/src/` shell, route screens, API client and styles; reuse desktop workflow components where practical without importing Electron-only runtime code into the browser.

- [x] Provide clear first-use setup, recovery confirmation, unlock/sign-in and organisation creation. Distinguish passphrase, administrator password and recovery code; avoid persisting any in browser storage.
- [x] Build responsive navigation, a calm business header, Overview attention queue, and real route loading/empty/error states. Deep links and refresh must preserve the intended route while respecting authentication.
- [x] Connect chart, journal creation/detail and trial balance. Show explicit posting intent, balanced totals and meaningful core validation failures.
- [x] Connect document intake/review, bank import/match/reconciliation, and BAS draft workflows with the precise current core semantics. Keep matching distinct from completed reconciliation and reviewed documents distinct from posted transactions.
- [x] Label BAS output “Draft — not lodged”; show supported reporting periods/capabilities rather than implying current-year support absent from the registry.
- [x] Show recent journals from core projections on Overview. A separate consolidated activity/audit-evidence screen remains outside this release.
- [x] Provide labels, semantic tables, focus styles and reduced-motion behavior. Browser evidence covers keyboard entry and widths 1536, 785 and 390 pixels; a complete keyboard-only/accessibility audit remains future hardening.

### Task 4: Read-only MCP and grounded intelligence

**Files:** `apps/web/server/` shared query facade, MCP adapter and insight derivation; browser integration/help surface and tests.

- [x] Expose discoverable MCP tools for attention summary, accounts, journal list with lines, trial balance, documents, bank status, current BAS draft and insights. Capability discovery is also exposed through the authenticated browser API; a dedicated MCP capabilities/detail tool remains deferred.
- [x] Authenticate the adapter independently of unauthenticated browser requests. Scope access to the active workspace and read-only operations; do not expose setup, login secrets, posting, raw SQL or arbitrary RPC dispatch as tools.
- [x] Use the same facade and core projections as the browser. MCP results must identify data scope, freshness and draft status. Provide tested connection instructions matching the transport actually implemented.
- [x] Derive insights for documents awaiting review, unmatched bank lines and existing draft GST workpapers. Each links to the relevant source screen. Missing-draft, reconciliation-ready and imbalance insights are not yet implemented.
- [x] Present deterministic rule-based insights honestly. Do not claim an LLM connection, future prediction or autonomous accounting. Treat document descriptions as untrusted content, never tool instructions.

### Task 5: Validation and adversarial review

**Files:** `apps/web/tests/` real-server Playwright journeys; focused gateway/MCP tests; update `README.md`, `docs/development/tech-state.md` and relevant development commands.

- [x] Test a blank workspace through visible UI: create, confirm recovery, sign in, create organisation, create ordinary accounts, post a balanced journal, and independently confirm exact amounts in the trial balance. The web app has no activity screen.
- [x] Test invalid oversized journal input and retained same-key replay, with independent unchanged trial-balance totals. Existing core tests cover journal balancing; a dedicated browser-API unbalanced request case remains additional coverage.
- [x] Test document review, statement import/match/completed reconciliation, and draft BAS with the supported fixture period. Assert exact statuses and amounts, not screenshots alone.
- [x] Restart the server/core and reopen the workspace; verify persisted records and route authentication. Use isolated test data and public commands, never direct business-data inserts.
- [x] Test unauthenticated access, hostile origins/hosts, bounded payloads, logout/expired sessions, pending-setup ownership and session churn. Review confirms server-owned authentication and fixed operation allowlists. An actual stdio MCP client discovers tools and reads the same retained journal; no mutation tool is registered.
- [x] Inspect screenshots at desktop and narrow widths, exercise keyboard form entry, and fail tests on unexpected browser exceptions. Retain traces/screenshots for failures.
- [x] Run the pinned-toolchain web/desktop typecheck, web build, frontend/server tests and browser E2E. Run the complete untagged Go suite plus affected native SQLCipher app/workspace tests; verify frozen dependencies and CI workflow syntax. Final Biome check passes across all 44 web files and the root package manifest.
- [x] Complete independent adversarial review of authentication, accounting semantics, upload limits, financial claims, lifecycle cleanup and controls. Fixed findings include anonymous-session exhaustion, pending-owner races, setup restart/ABN correction, expired sessions, integer DTOs, PDF parser freezing and ambiguous journal retry. Focused regressions and the full visible journey pass.

## Observed release scope and validation status

The implementation provides bounded document intake and manual review, with a linear scanner for uncompressed native PDF text operators. It does not provide a source-file preview, general PDF decoding, OCR, or source-to-payables posting. The upload limit is 512 KiB. List queries are explicitly capped at 200 rows without continuation; attention counters are all-time projections. Insights link to source screens rather than opening an exact item. API replay guarantees are operation-specific: journal posting supports retained replay, while review/match use optimistic versions and require refetching after uncertain responses. GST drafts cover any period within calendar 2024. GST credits (1B) count only posted purchases, which the browser cannot create, so reviewed documents of any date are provisional candidates under the core's all-date total. A pending setup must be confirmed within 15 minutes and is discarded after five incorrect recovery codes, and unlocking in another browser with the right credentials takes over the signed-in session. See `docs/development/web-api.md`.

Historical validation receipts, recorded before the rebase onto `b15ddb45` and its reconciled-purchase GST semantics: 11 frontend tests and 15 server tests pass; complete visible browser E2E passes including commit-then-response-loss retry with exactly one journal, document review, statement reconciliation, draft GST credits, lock/unlock, no unexpected browser errors and overflow checks at 1536/785/390 pixels. Exact observed amounts include $319.00 journal/trial balance, $681.00 bank closing balance and $29.00 reviewed-document GST credits. Web/desktop typecheck, web build, frozen install, Actionlint, Biome, the full untagged Go suite and native SQLCipher app/workspace tests pass. Independent reviewer reran all 11 frontend tests and the PDF/money subset; parent independently ran the complete web tests, browser E2E, build, lint, desktop typecheck, frozen install, Actionlint and untagged Go suite. Native SQLCipher app/workspace receipts came from the backend worker. No hosted deployment or production-security assurance is implied.

Current validation receipts, 2026-11-10, after the rebase onto `b15ddb45`. The commands ran through mise with pinned Node 24.18.0 and a canonical `/private/tmp` `TMPDIR`, with the core binary in `apps/desktop/resources/core/darwin-arm64` rebuilt from these sources by `pnpm core:build`.

- `pnpm web:typecheck`, `pnpm web:build` and `pnpm exec biome check apps/web` (46 files) pass.
- `pnpm web:test` passes 19 Vitest tests in 7 files and 44 server tests: facade 19, gateway 15, real-core integration 7 and security 3.
- The real-core integration tests check that a draft for 1 April to 30 June 2024 shows GST credits (1B) of $0.00 and lists provisional candidates dated in the period, in September 2025 and undated, under the core's total of $1,029.50. They also check that `sourcesChanged` is false after creation and true after another document is reviewed, that Overview `basStatus` is `action_needed`, and that the organisation records the `au_gst_v1` bundle hash. Five incorrect recovery codes, including codes entered before a gateway restart, discard the setup, and a new setup then succeeds. A newer browser takes over the signed-in session with MCP following it. An empty takeover request and three wrong passwords leave the signed-in browser and MCP working, a fourth takeover is refused with 429 before it reaches the core, and an unlock after logout is not limited. A rejected 129-character administrator password leaves no workspace file, and a workspace file without a setup record is reported as 409. Killing the core stops the gateway with `CORE_EXITED`. The MCP adapter reports `invalid_argument` for the date `2024-02-30`.
- `pnpm --dir apps/web e2e` passes the browser journey: the recovery deadline notice, commit-then-response-loss retry with exactly one journal, a $319.00 trial balance, document review, statement reconciliation with a $681.00 closing balance, a GST draft with GST credits (1B) of $0.00 and a provisional candidate total of $29.00, lock and unlock, no unexpected browser errors, and no horizontal overflow on the Overview at 785 and 390 pixels.
- A probe against the real core confirmed that reusing a journal key with different lines returns the same 400 response as an unbalanced journal. It also confirmed that a draft for 10 February to 5 March 2024 is accepted while July to September 2023 and January to March 2025 are rejected, and that MCP `POST` and `DELETE` return 403 while `HEAD` returns 401.
- `pnpm lint`, `pnpm contracts` and `node --test scripts/*.test.mjs` (942 passed, 1 opt-in reproducibility test skipped) pass, as does `go test -tags=tammy_sqlcipher ./services/core/internal/app/...`. The tagged workspace, identity, audit and command packages passed on the same core sources. The untagged Go suite fails only the two `sbrhelper` tests that also fail on `origin/master` on this Mac: a copied `/bin/sleep` is killed by a launch constraint, and a simulator profile is a gitignored build output.
- No `web-ci.yml` run has completed; the only run, for an earlier push of this branch, was cancelled. Actionlint 1.7.12 (`pnpm ci:lint`) and `pnpm desktop:typecheck` pass.

## Chunk 2: Sequenced hosted expansion

These are future implementation milestones, not claims about this release.

1. **Hosted runtime and durable security:** Linux SQLCipher support with real CI evidence; durable security anchors and audit sinks; managed secrets/key encryption and rotation; encrypted backups and restore drills; concurrency and process supervision. Decide deployment region and retention policy with the product owner before provisioning paid infrastructure.
2. **Identity and tenancy:** Hosted identity/session lifecycle, invitations, organisation memberships/roles, tenant-scoped resources, per-tenant key/storage isolation and cross-tenant adversarial tests. Every resource lookup must enforce membership server-side. Do not retrofit tenancy with a browser-provided organisation ID alone.
3. **Public API and credentials:** Versioned documented schema, scoped/revocable API keys stored as hashes, organisation binding, quotas, pagination, idempotency, structured safe errors and audit attribution. Keep browser credentials, machine credentials and core capabilities separate.
4. **Remote MCP and intelligence:** Supported authenticated MCP transport, user/organisation consent, least-privilege scopes, source-linked responses, model provider configuration and explicit review of any proposed writes. Financial mutations retain core authorization, human intent and idempotency regardless of caller.
5. **Full business suite:** Implement sales, purchases, contacts, settlements and report workflows as independently tested domain increments from existing design specs. Keep approval, payment, bank match, reconciliation, GST recognition and lodgement as distinct state transitions.

## Completion criteria

The increment is complete when the browser performs the supported end-to-end accounting journeys against the real persistent core, the MCP connection reads those same records under a bounded scope, insights are traceable to those records, the design matches Tammy's established language, and the validation receipts demonstrate actual behavior. Document all remaining hosted/platform and capability limits alongside exact startup/test commands.
