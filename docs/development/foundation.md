# Offline desktop foundation development

## Evidence boundary

The current foundation is a self-contained offline Electron shell backed by a supervised Go process. Startup, the diagnostics call, packaging, and local packaged E2E require no cloud service. External CI and future SBR services are optional for local development, but external platform and conformance evidence cannot be claimed until those systems have actually run.

The repository does not yet implement accounting, Activity Statements, credential storage, ATO transport, or SBR submission. Packages produced by these commands are unsigned development artifacts and are not production-ready or approval evidence.

All commands below run from the repository root. The `rtk` prefix is required by this workspace.

## Toolchain setup

```sh
rtk mise install
rtk mise exec -- corepack prepare pnpm@11.15.0 --activate
rtk mise exec -- pnpm install --frozen-lockfile
rtk mise exec -- pnpm check:toolchain
```

The pinned versions are declared in `mise.toml` and `package.json`. Do not substitute globally installed Node, Go, Buf, or pnpm versions when producing evidence.

## Generate and validate contracts

```sh
rtk mise exec -- pnpm proto:format:check
rtk mise exec -- pnpm proto:lint
rtk mise exec -- pnpm proto:breaking
rtk mise exec -- pnpm proto:generate
rtk git diff --exit-code -- services/core/internal/gen/tammy/v1 packages/connect-client/src/gen/tammy/v1
```

The final command must be clean. A generated-code diff means the checked-in Connect-Go or Connect-ES v2 output does not match the protobuf source and pinned generators.

## Unit and foundation checks

```sh
rtk mise exec -- pnpm test:toolchain
rtk mise exec -- pnpm test:proto-breaking
rtk mise exec -- node --test scripts/check-foundation-evidence.test.mjs
rtk mise exec -- go test -race ./services/core/...
rtk mise exec -- pnpm desktop:test
rtk mise exec -- pnpm desktop:typecheck
rtk mise exec -- pnpm lint
rtk mise exec -- pnpm compliance:foundation
```

The compliance command validates the fixed traceability schema, required design rows, conservative statuses, and Windows evidence classification. It validates the evidence register's structure; it does not create external evidence or confer approval.

## Run in development

```sh
rtk mise exec -- pnpm desktop:start
```

This builds the local Go core and starts Electron Forge. Close the application window or press `Ctrl-C` in the owning terminal. Electron closes the child input pipe and waits for the Go process to stop; a bounded forced termination is the fallback.

Development mode is useful for iteration, but its local development-server CSP is not packaged-release evidence.

## Build the unsigned package

```sh
rtk mise exec -- pnpm desktop:package
```

The output is an unsigned local development package. Building it does not demonstrate code signing, notarisation, Windows support, SBR conformance, or production readiness.

## Packaged end-to-end tests

Run the supported local packaged flow:

```sh
rtk mise exec -- pnpm desktop:e2e
```

For repeated lifecycle and orphan-process coverage:

```sh
rtk mise exec -- pnpm --dir apps/desktop e2e --repeat-each=3
```

The E2E harness starts the packaged application, exercises the offline diagnostics call under the production custom protocol and CSP, exits the app, and verifies that the exact packaged core process path has not been orphaned.

The local supported evidence target is `darwin-arm64`. Hosted macOS results remain unproduced until hosted CI runs. The Windows Server 2025 job is classified `WINDOWS_SERVER_SMOKE_ONLY`; its `WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64` artifact is not Windows 11 evidence. Windows 11 23H2 x64 evidence must come from the separately gated `windows11-x64-foundation-evidence` workflow artifact and is currently unproduced.

## Clean-shutdown verification

The normal verification command is:

```sh
rtk mise exec -- pnpm desktop:e2e
```

For a focused lifecycle check:

```sh
rtk mise exec -- pnpm --dir apps/desktop test src/main/core-process.test.ts src/main/index.test.ts
rtk mise exec -- go test -race ./services/core/cmd/tammy-core ./services/core/internal/transport
```

Do not use a broad process-name kill as routine cleanup or as evidence. It can hide an orphan bug and can terminate unrelated development processes. If a test fails, preserve the exact packaged-path diagnostics emitted by the E2E harness before manually terminating that exact process.

## Troubleshooting

If the toolchain check fails, reinstall the pinned tools and dependencies:

```sh
rtk mise install
rtk mise exec -- pnpm install --frozen-lockfile
rtk mise exec -- pnpm check:toolchain
```

If Electron reports that the core binary is missing or stale:

```sh
rtk mise exec -- pnpm core:build
rtk mise exec -- pnpm --dir apps/desktop test src/main/core-process.test.ts
```

If the generated client and server disagree:

```sh
rtk mise exec -- pnpm proto:generate
rtk mise exec -- pnpm test:proto-breaking
rtk mise exec -- go test -race ./services/core/...
rtk mise exec -- pnpm desktop:test
```

If packaged E2E fails, rerun it once with the exact local target and retain the emitted failure diagnostics:

```sh
rtk mise exec -- pnpm desktop:e2e
```

Do not reclassify a Windows Server smoke result as Windows 11 validation, and do not describe an absent hosted result as passing.
