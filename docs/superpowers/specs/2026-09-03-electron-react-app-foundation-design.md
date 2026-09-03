# Electron React App Foundation Migration

**Status:** Approved design  
**Date:** 2026-09-03  
**Upstream reference:** `guasam/electron-react-app` v13 at `d68dfcd916239f7691e31cd25e88174652b967b7`

## Goal

Move Tammy's desktop application from Electron Forge and hand-written IPC onto the useful foundation patterns from `electron-react-app`: `electron-vite`, `electron-builder`, and `electron-conveyor`.

The migration keeps Tammy's accounting, SBR, local encryption, security, release, and verification behavior. It does not preserve a legacy runtime or compatibility layer.

## Decisions

- Perform a staged in-place migration so every foundation change can be verified independently.
- Ship only the new foundation at the end; remove Forge, the old channel API, and `window.tammy`.
- Keep Tammy's existing renderer product and navigation. Do not import the starter's welcome screen or generic product styling.
- Use the upstream structure and patterns, adapted to Tammy's monorepo and release requirements rather than copying placeholder configuration.
- Preserve the sandbox in development and production. Do not adopt upstream's `ELECTRON_DISABLE_SANDBOX=1` development command.
- Preserve Tammy's App Store policy scripts rather than adopting upstream's generic signing, permissions, update URL, or publishing configuration.

## Desktop Structure

The desktop workspace remains at `apps/desktop` and uses these boundaries:

```text
apps/desktop/
  electron.vite.config.ts
  electron-builder.yml
  src/
    app/                 React renderer and existing feature screens
    conveyor/
      client.ts          typed renderer client and query client
      init.ts            Tammy handler context and primitives
      router.ts          complete main-process API registration
      modules/           domain-oriented queries and commands
    main/                lifecycle, Core process, security, file intake
    preload/             minimal Conveyor bridge and launch scenario
    shared/              schemas and pure shared types only
```

Path aliases cover `@/app`, `@/conveyor`, `@/main`, `@/shared`, and `@/resources`. Main-only runtime values must not enter renderer bundles. The existing application shell remains the renderer root.

## Runtime Lifecycle

Tammy retains its explicit startup sequence:

1. Register the application scheme before Electron becomes ready.
2. Wait for Electron readiness and install session security.
3. Start the local Go Core and confirm diagnostics.
4. Create the Conveyor router context around the ready Core client and mediated file intake.
5. Create the sandboxed window, install window guards, and load the trusted application URL.
6. Show the window only after `ready-to-show`.

Shutdown unregisters the owned IPC surface, clears sensitive in-memory file state, destroys the window, stops Core, releases security leases, and exits. Tammy's default-deny scheme, protocol, CSP, permission, download, navigation, and popup handlers remain installed monotonically; releasing a lease must never clear or weaken them. A failed Core startup never exposes a usable application window.

## Typed Process Boundary

`electron-conveyor` replaces the hand-written channel constants, preload facade, and IPC router. Modules are divided by product scenario:

- `system`: diagnostics and release-safe capability information
- `workspace`: create, recover, unlock, and sign in
- `organisation`: entity details, users, TOTP, and verification
- `accounting`: accounts, journals, and trial balance
- `banking`: statement import, matching, reconciliation, and summary
- `documents`: ingestion, listing, retrieval, and review
- `reporting`: BAS, EOFY, company tax, capability, and attention state
- `sbr`: registration readiness, product ID, simulator actions, and machine credentials

The renderer imports only the router type. The runtime router and Core client remain main-process-only.

Queries are used for reads and commands for state changes. React Query integration supplies cache keys and focused invalidation where caching helps the UI. Credential passwords, file handles, recovery material, and other secrets use uncached imperative commands and are never placed in Conveyor stores or React Query data.

Every input crossing the renderer boundary has a Zod schema. Existing maximum byte sizes, exact object shapes, UUID constraints, string limits, protobuf copies, and response bounds remain enforced. Handlers accept calls only from the owning trusted application window and main frame.

A patched router-level `authorize(event)` hook validates every procedure before context creation, method lookup, Zod input validation, or domain handling. It requires the original `event.sender`, `event.senderFrame`, owning `BrowserWindow`, registered Tammy window, main frame, and canonical trusted application URL to agree. Development accepts only the single canonical loopback origin validated from `ELECTRON_RENDERER_URL`; production accepts only `tammy://app/`. A missing, destroyed, child-frame, replaced-window, unvalidated-origin, or non-Tammy sender receives only `IPC_SENDER_REJECTED`. The same hook protects the synchronous manifest listener before it returns method metadata.

Tammy creates one router after Core readiness rather than relying on an import-time side effect. `electron-conveyor@0.4.0` is pinned and carries one small repository-owned pnpm patch because the upstream router has no injectable IPC boundary, pre-validation guard, or disposer and always installs global stream handlers. The patch adds only an injected `ipcMain` boundary, a guard that runs before method lookup and input validation, `streams: false`, and ownership-safe idempotent disposal for procedure and manifest registrations. It is covered by Tammy integration tests and can be dropped when upstream provides the same contract. This migration uses queries and commands only; stores, streams, events, and multi-window state are out of scope.

The preload implements the minimal bridge shape expected by Conveyor but allowlists the eight Tammy module channels and their exact methods before invoking Electron IPC. Its manifest function can call only the fixed guarded `conveyor:manifest` channel. Because streams and events are out of scope, its required `subscribe` member always rejects and never attaches an IPC listener. It does not expose Conveyor's unrestricted stock `invoke(channel, method, ...)` or `subscribe(channel, ...)` bridge. The main-process guard and schemas remain authoritative even if renderer code bypasses the typed client.

Machine-credential selection remains a separate main-process intake boundary. The renderer receives only a UUIDv7 opaque handle—never a path or bookmark. A handle expires after five minutes, can be consumed once, is generation-bound, and is cleared during shutdown. MAS intake requires exactly one canonical bounded security-scoped bookmark; non-MAS intake rejects bookmarks. Temporary and retained bookmark bytes are wiped on use, expiry, replacement, rejection, and cleanup.

## Errors and Security

Main-process failures are mapped to stable Tammy error codes carried by Conveyor errors. Renderer behavior branches on codes, not message text. Internal messages, paths, subprocess output, credential data, and raw Core failures are not exposed.

The following invariants remain mandatory:

- renderer sandbox enabled
- context isolation enabled and Node integration disabled
- restrictive CSP and application protocol
- trusted main-frame sender ownership
- navigation, popup, permission, download, and external-link guards
- allowlisted public links only
- bounded, copied binary payloads
- no filesystem, shell, arbitrary URL, or raw IPC escape hatch
- no secret persistence in renderer state

Conveyor is a typed transport, not the security boundary by itself; Tammy's independent guards remain authoritative.

## Build and Packaging

`electron-vite` becomes the sole builder for main, preload, and renderer output. The sandbox-compatible Conveyor preload is bundled rather than externalized.

The output contract is fixed rather than inferred:

- main entry: `src/main/index.ts` to `out/main/index.js`
- preload entry: `src/preload/index.ts` to sandbox-compatible CommonJS `out/preload/index.cjs`
- renderer entry: `src/app/index.html` to `out/renderer/index.html`
- desktop package `main`: `./out/main/index.js`
- development URL: validated `ELECTRON_RENDERER_URL`
- production URL: `tammy://app/`, backed by `app.getAppPath()/out/renderer`

The custom protocol validates the physical unpacked or ASAR renderer root before serving it. CSP and sender validation use the same canonical application URL. All `.vite/build`, `.vite/renderer/main_window`, and Forge environment constants are replaced together; no fallback locator remains.

`electron-builder` replaces Forge for unpacked, ordinary macOS, Mac App Store, and Windows artifacts. Its configuration must include Tammy's exact identifiers and product metadata while delegating policy-sensitive work to existing release scripts:

- reserved build number and application version
- app and helper bundle identifiers
- App Store category and privacy manifest
- parent, child, Core, and SBR-helper entitlements
- provisioning profile and signing identities
- signed native binaries whose authenticated hashes must not change
- packaged resource permissions
- deterministic output beneath `.tmp`

Ordinary platform resources are explicit: macOS receives `build`, `core`, `sqlcipher`, `sbr`, and `sbr-helper`; Windows receives `build`, `core`, and `sqlcipher`; MAS also receives `PrivacyInfo.xcprivacy`. Unknown platform/architecture combinations fail closed.

MAS packaging keeps the existing authenticated two-copy flow. Builder first assembles an unsigned app beneath `.tmp/macos-release/<version>/build-<number>/.builder-unsigned` without signing or mutating native resources. Tammy authenticates and publishes the immutable unsigned content manifest. The release step then signs and verifies the SBR helper, builds the signed SBR profile from that helper, signs and verifies Core, rebuilds the authenticated build manifest, replaces the four resource trees in a fresh signing copy, and finally signs the Electron app while excluding Core and the SBR helper from re-signing. Distribution mode then creates the installer package. Unsigned, development-signed, and distribution roots remain deterministic and never overwrite one another.

Forge packaging hooks become small explicit pre-pack or after-pack scripts with unit tests. Existing artifact inspection and release evidence are updated for Builder's directory names while preserving exact-revision, manifest, signature, orphan-process, and resource checks. No auto-update provider is added in this migration.

Electron fuses remain an explicit release invariant. Builder's fuse step must set and artifact verification must assert:

- `RunAsNode=false`
- `EnableCookieEncryption=true`
- `EnableNodeOptionsEnvironmentVariable=false`
- `EnableNodeCliInspectArguments=true`
- `EnableEmbeddedAsarIntegrityValidation=true`
- `OnlyLoadAppFromAsar=true`

ASAR integrity metadata must be present and valid before an artifact can pass package or MAS verification.

If substantial upstream source is copied, its MIT copyright and license notice are retained.

## Task Scenarios

The public Taskfile interface remains stable while its implementation changes:

- `task dev`: build native dependencies and run the complete Electron development application
- `task test`: repository tests
- `task verify:quick`: fast lint, type, and unit verification
- `task verify:full`: quick verification plus SQLCipher integration on supported native targets; Linux remains quick-only
- `task build`: unpacked diagnostic application
- `task package`: ordinary authenticated local package
- `task package:e2e`: authenticated packaged Electron acceptance with isolated user data and orphan-process evidence
- `task deploy:mas`: signed, locally validated MAS candidate without automatic submission
- existing SBR doctor, registration, evidence, screenshot, site, and release scenarios

`task verify:release` remains the owner that composes clean-tree and production-contract checks with `verify:full`, `package:e2e`, and App Store input checks. CI keeps packaged evidence on supported macOS arm64 and Windows x64 jobs rather than moving it into Linux or `verify:full`.

Direct package scripts remain focused implementation primitives; Task remains the documented operator interface containing startup and release domain knowledge.

## Migration Sequence

1. Add electron-vite configuration and adapt entry points while preserving current behavior.
2. Replace Forge packaging with electron-builder, port release hooks and fuses, and migrate all artifact locators.
3. Introduce Conveyor context, router, and one domain module with boundary tests.
4. Migrate the remaining domain modules and renderer callers by scenario.
5. Reduce the preload to the Conveyor bridge and launch-scenario data.
6. Migrate packaged Playwright helpers from direct `window.tammy` calls to the typed Conveyor test surface, then delete Forge, old channels, old router, `window.tammy`, and generated Forge assumptions.
7. Run complete unit, integration, packaged E2E, and macOS artifact verification.

Each step may be a separate commit, but there is only one supported runtime in the final tree.

## Verification

Automated checks must prove:

- all Conveyor schemas, handler contexts, error mappings, sender guards, and limits
- sensitive values are uncached and redacted
- Core startup, failed startup, shutdown, and cleanup behavior
- renderer reads, mutations, invalidations, and user-visible failure states
- electron-vite development startup
- unpacked application launch
- complete Playwright accounting, document, reporting, and SBR simulator journeys
- packaged Playwright calls exercise the public Conveyor bridge/client used by the application; no renderer mock substitutes for Electron IPC
- ordinary macOS package authentication
- unsigned MAS staging and signed-candidate validation when local credentials are available
- deterministic artifacts and screenshot discovery
- expected Electron fuse values and valid embedded ASAR integrity
- absence of Forge dependencies, `.vite` assumptions, Forge environment constants, legacy channel APIs, and `window.tammy`

## Acceptance Criteria

- Tammy launches locally through `task dev` with its complete native Core and supported SBR simulator inputs.
- Existing accounting and reporting scenarios behave the same through typed Conveyor modules.
- Ordinary and MAS packaging use electron-builder and pass Tammy's artifact checks.
- Packaged E2E tests exercise the migrated application rather than mocked renderer-only behavior.
- The final dependency and source trees contain no Electron Forge runtime or compatibility IPC layer.
- Security assertions are at least as strict as before the migration.

## Non-goals

- redesigning Tammy's screens or navigation
- adding multi-window product features solely because the starter supports them
- adding auto-update infrastructure
- enabling live ATO submission before the separate production SBR requirements are satisfied
- preserving Forge or old IPC compatibility
