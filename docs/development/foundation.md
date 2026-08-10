# Developer handbook

Tammy is an offline-first Electron application backed by a supervised Go core and an encrypted local SQLCipher workspace. For the exact capability boundary, read [Current technical state](tech-state.md); for a manual product exercise, use the [Local accounting walkthrough](local-accounting-walkthrough.md).

All commands below run from the repository root. This workspace requires the `rtk` prefix.

## Toolchain setup

```sh
rtk mise install
rtk mise exec -- corepack prepare pnpm@11.15.0 --activate
rtk mise exec -- pnpm install --frozen-lockfile
rtk mise exec -- pnpm check:toolchain
```

Pinned Node, Go, Buf, and pnpm versions live in `mise.toml` and `package.json`. Use them when generating contracts, building the core, or reporting verification evidence.

## Run and build

Start the development application:

```sh
rtk mise exec -- pnpm desktop:start
```

Build an unsigned local package:

```sh
rtk mise exec -- pnpm desktop:package
```

Exercise the packaged lifecycle:

```sh
rtk mise exec -- pnpm desktop:e2e
```

These are development commands. They do not sign, notarise, upload, or obtain App Store/ATO approval.

## Development workspace

Electron starts the development core with a data root below its user-data directory named `local-core-development`; on macOS the usual parent is `~/Library/Application Support/Tammy`. The Go composition stores its private material below `local-core-development/core`, including the encrypted workspace directory, encrypted catalogue, installation key, and security-attempt journals.

The data root is persistent: restarting `desktop:start` should reopen the same SQLCipher workspace. Development also passes `--development-memory-anchors`. On startup the core removes only its owned `workspace-attempts.journal` and `identity-attempts.journal` before recreating them against in-memory anchors. This avoids development restart failure but deliberately means attempt/cooldown durability is not production evidence. Packaged mode uses `local-core` and does not enable this reset.

Do not manually delete individual files. For a deliberate blank-workspace reset, stop both Electron and the core, then move the entire `local-core-development` directory aside so it remains recoverable. Never include that directory, passwords, recovery codes, installation keys, databases, or imported documents in a commit.

## Repository map

| Path | Ownership |
| --- | --- |
| `apps/desktop/src/main` | Electron lifecycle, core supervision, IPC routing, CSP, and packaged-path checks. |
| `apps/desktop/src/preload` | Named, bounded renderer API; no raw transport or filesystem surface. |
| `apps/desktop/src/renderer` | Setup, workspace shell, accounting, documents, banking, BAS, and activity UI. |
| `proto/tammy/v1` | Versioned protobuf contracts and validation annotations. |
| `services/core/cmd/tammy-core` | Core process entry point and development/process configuration. |
| `services/core/internal/app` | Production/local composition and ordinary-command coordination. |
| `services/core/internal/storage` | SQLCipher adapter, migrations, locks, and recovery-safe storage. |
| `services/core/internal/{accounting,documents,banking,reporting}` | Module-owned domain rules, repositories, and services. |
| `services/core/internal/{workspace,identity,audit,backup,restore}` | Workspace security, identity, audit evidence, and recovery services. |
| `packages/connect-client` | Generated Connect-ES client contracts consumed by desktop. |
| `test/e2e` | Contract coverage declarations and end-to-end evidence fixtures. |
| `docs/superpowers` | Historical designs and implementation plans; not current-state claims. |

## Contracts and generated code

```sh
rtk mise exec -- pnpm proto:format:check
rtk mise exec -- pnpm proto:lint
rtk mise exec -- pnpm proto:breaking
rtk mise exec -- pnpm proto:generate
rtk git diff --exit-code -- services/core/internal/gen/tammy/v1 packages/connect-client/src/gen/tammy/v1
```

Edit protobuf source, not generated Go or TypeScript. A generated diff after the pinned generation command must be reviewed and committed with its source contract; an unexpected diff means generation is not byte-stable.

## Tests and checks

Use the smallest relevant check while iterating, then the broader affected gates:

```sh
rtk mise exec -- go test ./services/core/...
rtk mise exec -- go test -tags=tammy_sqlcipher ./services/core/...
rtk mise exec -- pnpm desktop:test
rtk mise exec -- pnpm desktop:typecheck
rtk mise exec -- pnpm lint
rtk mise exec -- pnpm contracts
rtk git diff --check
```

The SQLCipher-tagged suite requires the supported cgo/platform toolchain and can take substantially longer than default unit tests. Use `rtk mise exec -- pnpm desktop:e2e` when a change crosses Electron, preload, generated transport, or packaged-core boundaries.

## Safe change workflow

1. Read the current-state doc and the owning design/plan section.
2. Confirm module ownership and the generated RPC/coverage row before editing.
3. Add a focused failing test, run it, and confirm it fails for the intended missing behavior.
4. Make the smallest implementation change. Use `apply_patch` for authored files and pinned Buf generation for protobuf output.
5. Run the focused test, affected default/tagged tests, contract checks when relevant, and a real packaged or SQLCipher E2E when the boundary warrants it.
6. Inspect `git diff`, preserve unrelated work, run `rtk git diff --check`, and commit only observed results. Never claim unsigned packages, simulated fixtures, or local checks as external approval evidence.

## Troubleshooting

- If tool versions drift, rerun the four setup commands above.
- If the core binary is missing or stale, run `rtk mise exec -- pnpm core:build`.
- If development startup reports attempt-journal authentication, stop all owned Tammy processes and restart normally so the development-only reset can run. Do not delete individual files while the core is live.
- If a workspace does not reopen, retain the whole data root and diagnostics. Partial deletion can destroy the evidence needed to distinguish passphrase, catalogue, database, or recovery failure.
- If packaged E2E reports an orphan, retain its exact path and PID diagnostics. Do not use a broad process-name kill that could hide the defect or terminate another developer's process.

## Further reading

- [Current technical state](tech-state.md)
- [Local accounting walkthrough](local-accounting-walkthrough.md)
- [Application documentation and macOS release design](../superpowers/specs/2026-08-10-application-documentation-macos-release-design.md)
- [Accounting walkthrough UI design](../superpowers/specs/2026-08-09-accounting-tax-walkthrough-ui-design.md)
- [Current documentation/release plan](../superpowers/plans/2026-08-10-application-documentation-macos-release.md)

The macOS App Store runbook is intentionally not linked until its packaging pass creates and verifies it.
