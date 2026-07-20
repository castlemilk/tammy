# Offline Desktop Foundation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a packaged, entirely offline Electron desktop shell that boots a bundled Go Connect service over capability-authenticated ephemeral TLS, exposes a generated Connect-ES v2 diagnostics call only through a sandboxed preload bridge, and passes target-platform end-to-end tests.

**Architecture:** Electron main is the sole parent and RPC client of a Go child process bound to `127.0.0.1:0`. The Go core generates a 256-bit capability and ephemeral CA, emits their process-bootstrap record only through its parent-owned stdout pipe, and serves TLS 1.3; the sandboxed React renderer receives a narrow typed IPC projection without any port, certificate, capability, filesystem, Node, or network access. Protobuf is the source of truth; Buf v2 generates Connect-Go and unified Protobuf-ES v2 service definitions.

**Tech Stack:** Go 1.26.4, Connect-Go 1.20.0, Buf CLI 1.72.0, Protobuf Go 1.36.11, pnpm 11.15.0, Node 24.18.0, Electron 43.1.1, React 19.2.7, TypeScript 7.0.2, Connect-ES/Connect-Node 2.1.2, Protobuf-ES 2.12.1, Vite 8.1.5, Tailwind CSS 4.3.3, shadcn 4.13.1, Vitest 4.1.10, Playwright 1.61.1, and actionlint 1.7.12.

---

## Scope and delivery sequence

This is Plan 1 of the approved Milestone 1 design in [`docs/superpowers/specs/2026-07-19-tammy-local-first-accounting-sbr-design.md`](../specs/2026-07-19-tammy-local-first-accounting-sbr-design.md). It establishes the process, API, UI, packaging, and test boundaries that every later subsystem uses. It does not claim to implement accounting, encrypted workspaces, real machine credentials, or SBR approval.

Follow-on plans are deliberately separate:

1. encrypted SQLCipher workspace, OS key store, local identity, TOTP, recovery, and audit genesis;
2. double-entry ledger, GST kernel, canonical trial balance, BAS workpaper, and deterministic SBR simulator;
3. audit evidence export, backup/restore generations, transmission crash recovery, and packaged recovery E2E;
4. approved ATO artefact import, isolated credential helper, EVTE/conformance evidence, DPO/OSF approval work, signing, and release hardening.

Before implementing this plan, read and follow:

- `@superpowers:test-driven-development` for every behavior change;
- `@security-best-practices` for Go and TypeScript security-sensitive code;
- `@frontend-design` for the compact ledger-first desktop surface;
- `@playwright` for packaged Electron verification; and
- `@superpowers:verification-before-completion` before claiming any task or chunk complete.

## Fixed technical decisions

- Pin the versions above exactly in manifests and lockfiles. Upgrade only in a dedicated dependency change with the same verification suite.
- Use Buf v2 configuration. Connect-ES v2 service definitions come from the unified `protoc-gen-es` 2.12.1 plugin; do not install or run `protoc-gen-connect-es`.
- Generated files are committed and checked for cleanliness, but never hand-edited.
- Use Connect protocol over HTTP/1.1 inside TLS 1.3. HTTP/2 adds no value to the unary local foundation call and complicates lifecycle testing.
- Electron main creates the Connect-ES client. The renderer has `connect-src 'none'` and cannot import `@connectrpc/connect`, generated service definitions, or Electron modules.
- The core creates the capability as exactly 32 random bytes encoded as unpadded Base64URL. It emits the capability once through its parent-owned stdout pipe and never accepts it from Electron or places it in argv, environment variables, URLs, logs, crash metadata, renderer state, or persisted storage.
- The core creates a unique ephemeral CA, leaf certificate, and capability in memory for each process. The CA and leaf use a long practical X.509 validity window so normal certificate verification cannot expire while that supervised process is still running; their security lifetime ends with the process because keys and capability are never persisted or reused. Readiness returns the public CA certificate, selected loopback port, and capability over stdout; Electron main parses the bounded record and never forwards its security fields.
- Production startup fails closed on malformed or extra readiness output, TLS validation failure, capability failure, core exit, or timeout. There is no HTTP fallback.
- This plan packages unsigned development artifacts only. Signing, notarisation, MSIX identity, and production update policy belong to the release-hardening plan.

## Repository map

The following structure is locked for this plan:

```text
.
├── .editorconfig                         # whitespace and newline policy
├── .github/workflows/foundation-ci.yml   # target-platform verification
├── .node-version                         # Node 24.18.0 pin
├── mise.toml                             # executable Node and Go activation
├── biome.json                            # JS/TS/JSON formatting and lint
├── buf.gen.yaml                          # pinned Go and ES generators
├── buf.yaml                              # Buf v2 module/lint/breaking policy
├── go.work                               # Go workspace containing services/core
├── go.work.sum                           # exact Go workspace module checksums
├── package.json                          # root commands and tool pins
├── pnpm-lock.yaml                        # exact JS dependency graph
├── pnpm-workspace.yaml                   # apps/* and packages/* workspace
├── tsconfig.base.json                    # strict shared TS policy
├── proto/tammy/v1/system.proto
├── apps/
│   └── desktop/
│       ├── assets/icons/README.md
│       ├── components.json
│       ├── forge.config.ts
│       ├── index.html
│       ├── package.json
│       ├── playwright.config.ts
│       ├── resources/core/.gitkeep
│       ├── scripts/find-packaged-app.mjs
│       ├── src/
│       │   ├── main/
│       │   │   ├── core-client.ts
│       │   │   ├── core-client.test.ts
│       │   │   ├── core-process.ts
│       │   │   ├── core-process.test.ts
│       │   │   ├── ipc.ts
│       │   │   ├── security.ts
│       │   │   └── index.ts
│       │   ├── preload/index.ts
│       │   ├── renderer/
│       │   │   ├── app.tsx
│       │   │   ├── app.test.tsx
│       │   │   ├── main.tsx
│       │   │   ├── styles.css
│       │   │   ├── components/ui/{badge,button,card,separator}.tsx
│       │   │   ├── features/diagnostics/diagnostics-card.tsx
│       │   │   └── lib/utils.ts
│       │   └── shared/
│       │       ├── desktop-api.ts
│       │       └── readiness.ts
│       ├── tests/e2e/foundation.spec.ts
│       ├── tsconfig.json
│       ├── vite.main.config.ts
│       ├── vite.preload.config.ts
│       ├── vite.renderer.config.ts
│       └── vitest.config.ts
├── services/core/
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/tammy-core/
│   │   ├── main.go
│   │   └── main_test.go
│   └── internal/
│       ├── buildinfo/info.go
│       ├── gen/tammy/v1/
│       │   ├── system.pb.go
│       │   └── tammyv1connect/system.connect.go
│       ├── system/service.go
│       ├── system/service_test.go
│       └── transport/
│           ├── capability.go
│           ├── capability_test.go
│           ├── certificate.go
│           ├── readiness.go
│           ├── readiness_test.go
│           ├── server.go
│           └── server_integration_test.go
├── packages/connect-client/
│   ├── package.json
│   ├── src/gen/tammy/v1/*                # generated ES; committed
│   └── tsconfig.json
├── scripts/
│   ├── build-core.mjs
│   ├── check-generated.mjs
│   ├── check-toolchain.mjs
│   ├── check-toolchain.test.mjs
│   └── write-build-manifest.mjs
└── compliance/
    ├── build/toolchain.lock.json
    └── traceability/foundation.csv
```

## Chunk 1: Reproducible API and secure Go core

### Task 1: Pin the polyglot workspace and executable toolchain

**Files:**

- Modify: `.gitignore`
- Create: `.editorconfig`
- Create: `.node-version`
- Create: `.npmrc`
- Create: `mise.toml`
- Create: `package.json`
- Create: `pnpm-workspace.yaml`
- Create: `tsconfig.base.json`
- Create: `biome.json`
- Create: `go.work`
- Generate: `go.work.sum`
- Create: `services/core/go.mod`
- Generate: `services/core/go.sum`
- Create: `packages/connect-client/package.json`
- Create: `packages/connect-client/tsconfig.json`
- Create: `scripts/check-toolchain.test.mjs`
- Create: `scripts/check-toolchain.mjs`

- [ ] **Step 1: Create the root manifests with exact pins**

Before installing anything, extend `.gitignore` to:

```gitignore
.superpowers/
.worktrees/
node_modules/
.pnpm-store/
**/.vite/
**/out/
**/dist/
**/coverage/
**/test-results/
**/playwright-report/
apps/desktop/resources/core/*
!apps/desktop/resources/core/.gitkeep
apps/desktop/resources/build/*
!apps/desktop/resources/build/.gitkeep
```

Use this root `package.json`:

```json
{
  "name": "tammy",
  "private": true,
  "type": "module",
  "packageManager": "pnpm@11.15.0",
  "engines": {
    "node": "24.18.0",
    "pnpm": "11.15.0"
  },
  "scripts": {
    "check:toolchain": "node scripts/check-toolchain.mjs",
    "test:toolchain": "node --test scripts/check-toolchain.test.mjs",
    "proto:format:check": "buf format --diff --exit-code",
    "proto:lint": "buf lint",
    "proto:generate": "buf generate",
    "proto:breaking": "buf breaking --against .git#branch=master",
    "format": "biome check --write .",
    "lint": "biome check ."
  },
  "devDependencies": {
    "@biomejs/biome": "2.5.4",
    "@bufbuild/buf": "1.72.0",
    "@bufbuild/protoc-gen-es": "2.12.1",
    "typescript": "7.0.2"
  }
}
```

Use:

```yaml
# pnpm-workspace.yaml
packages:
  - apps/*
  - packages/*

minimumReleaseAge: 1440

onlyBuiltDependencies: ["@bufbuild/buf"]
```

Use `.npmrc` values `save-exact=true`, `strict-peer-dependencies=true`, and `verify-store-integrity=true`. Set `.node-version` to `24.18.0`.

Use this executable tool activation:

```toml
# mise.toml
[tools]
node = "24.18.0"
go = "1.26.4"
```

Use this complete editor policy:

```ini
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
indent_style = space
indent_size = 2
trim_trailing_whitespace = true

[*.go]
indent_style = tab

[*.md]
trim_trailing_whitespace = false
```

Use this complete shared TypeScript policy:

```json
{
  "$schema": "https://json.schemastore.org/tsconfig",
  "compilerOptions": {
    "target": "ES2024",
    "lib": ["ES2024", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "exactOptionalPropertyTypes": true,
    "useUnknownInCatchVariables": true,
    "verbatimModuleSyntax": true,
    "isolatedModules": true,
    "resolveJsonModule": true,
    "forceConsistentCasingInFileNames": true,
    "skipLibCheck": false,
    "noEmit": true,
    "jsx": "react-jsx",
    "types": []
  }
}
```

Use this complete Biome configuration:

```json
{
  "$schema": "https://biomejs.dev/schemas/2.5.4/schema.json",
  "vcs": {
    "enabled": true,
    "clientKind": "git",
    "useIgnoreFile": true
  },
  "files": {
    "includes": [
      "**",
      "!**/internal/gen",
      "!**/src/gen",
      "!**/.vite",
      "!**/out",
      "!**/playwright-report"
    ]
  },
  "formatter": {
    "enabled": true,
    "indentStyle": "space",
    "indentWidth": 2,
    "lineWidth": 100
  },
  "javascript": {
    "formatter": {
      "quoteStyle": "double",
      "semicolons": "always"
    }
  },
  "linter": {
    "enabled": true,
    "rules": {
      "preset": "recommended"
    }
  },
  "assist": {
    "enabled": true,
    "actions": {
      "source": {
        "organizeImports": "on"
      }
    }
  }
}
```

- [ ] **Step 2: Create the minimal Go and proto-package manifests**

`services/core/go.mod`:

```go
module github.com/tammyapp/tammy/services/core

go 1.26.4

require (
	connectrpc.com/connect v1.20.0
	google.golang.org/protobuf v1.36.11
)
```

`go.work`:

```go
go 1.26.4

use ./services/core
```

`packages/connect-client/package.json`:

```json
{
  "name": "@tammy/connect-client",
  "version": "0.0.0",
  "private": true,
  "type": "module",
  "dependencies": {
    "@bufbuild/protobuf": "2.12.1"
  },
  "exports": {
    "./tammy/v1/system_pb.js": "./src/gen/tammy/v1/system_pb.ts"
  }
}
```

`packages/connect-client/tsconfig.json`:

```json
{
  "extends": "../../tsconfig.base.json",
  "compilerOptions": {
    "composite": true,
    "declaration": true,
    "emitDeclarationOnly": true,
    "noEmit": false,
    "outDir": "dist",
    "rootDir": "src",
    "lib": ["ES2024"]
  },
  "include": ["src/**/*.ts"]
}
```

- [ ] **Step 3: Install and activate the exact executable toolchain**

If `mise` is absent, run the applicable installation command:

```bash
# macOS
rtk brew install mise

# Windows PowerShell
rtk winget install jdx.mise
```

Then run on either target:

```bash
rtk mise trust
rtk mise install
rtk mise exec -- node --version
rtk mise exec -- go version
```

Expected: Node prints `v24.18.0` and Go prints `go1.26.4`.

- [ ] **Step 4: Install the exact JavaScript dependency graph**

Run:

```bash
rtk mise exec -- corepack prepare pnpm@11.15.0 --activate
rtk mise exec -- corepack enable pnpm
rtk mise exec -- pnpm install
```

Expected: `pnpm-lock.yaml` is created; no peer-dependency or lifecycle-script warning is ignored.

- [ ] **Step 5: Write the failing toolchain contract test**

`scripts/check-toolchain.test.mjs` must inject fake command output and assert exact acceptance of Node `v24.18.0`, pnpm `11.15.0`, Go `go1.26.4`, and Buf `1.72.0`; it must separately assert a readable mismatch for every tool. Import `validateToolVersions` from `check-toolchain.mjs`. Also cover argv-less dynamic import, pure `win32` and `darwin` command plans, sanitized command-execution failures, and a current-platform CLI subprocess.

Core test:

```js
import assert from "node:assert/strict";
import test from "node:test";
import { validateToolVersions } from "./check-toolchain.mjs";

test("accepts the pinned toolchain", () => {
  assert.deepEqual(
    validateToolVersions({
      node: "v24.18.0",
      pnpm: "11.15.0",
      go: "go version go1.26.4 darwin/arm64",
      buf: "1.72.0",
    }),
    [],
  );
});

test("reports every mismatch", () => {
  assert.deepEqual(
    validateToolVersions({
      node: "v22.20.0",
      pnpm: "10.9.3",
      go: "go version go1.26.3 darwin/arm64",
      buf: "1.71.0",
    }),
    [
      "Node must be v24.18.0 (received v22.20.0)",
      "pnpm must be 11.15.0 (received 10.9.3)",
      "Go must be go1.26.4 (received go1.26.3)",
      "Buf must be 1.72.0 (received 1.71.0)",
    ],
  );
});
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `rtk mise exec -- pnpm test:toolchain`

Expected: FAIL with `ERR_MODULE_NOT_FOUND` or a missing validator/command-planning export.

- [ ] **Step 7: Implement the toolchain checker**

Export a pure `validateToolVersions(outputs)` function and a pure cross-platform command planner, and keep command execution in the `import.meta.main` CLI branch. Invoke Node with `process.execPath`. Run pnpm through `process.execPath` and its absolute lifecycle-provided JavaScript entry, with a validated Corepack JavaScript-entry fallback derived from the pinned Node installation. Resolve the pinned `@bufbuild/buf/bin/buf` JavaScript wrapper through ESM and run it through `process.execPath`. Go may remain a direct native `go` or `go.exe` call. Validate every selected entry as an absolute regular file, reject `.cmd` and `.bat` shims, and use `execFileSync` with argument arrays only—never a shell. Keep execution errors stable without exposing environment values. Parse the Go version from the third whitespace-delimited token. Print every mismatch to stderr and exit 1, or print `toolchain ok` and exit 0.

- [ ] **Step 8: Verify tests and formatting**

Run:

```bash
rtk mise exec -- pnpm test:toolchain
rtk mise exec -- pnpm check:toolchain
rtk mise exec -- pnpm lint
rtk mise exec -- go work sync
rtk mise exec -- go -C services/core mod download
```

Expected: all Node toolchain tests PASS, including argv-less import and the current-platform CLI subprocess; the real executable check prints `toolchain ok`; Biome reports no errors, warnings, or deprecations; the Go workspace sync exits 0; and `go.work.sum` plus `services/core/go.sum` record the exact Connect-Go and Protobuf Go module checksums.

- [ ] **Step 9: Commit the workspace**

```bash
rtk git add .gitignore .editorconfig .node-version .npmrc mise.toml package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json biome.json go.work go.work.sum services/core/go.mod services/core/go.sum packages/connect-client scripts/check-toolchain.mjs scripts/check-toolchain.test.mjs
rtk git commit -m "build: pin the desktop foundation toolchain"
```

### Task 2: Define and generate the system diagnostics contract

**Files:**

- Create: `buf.yaml`
- Create: `buf.gen.yaml`
- Create: `proto/tammy/v1/system.proto`
- Create: `services/core/internal/contracts/system_proto_test.go`
- Generate: `services/core/internal/gen/tammy/v1/system.pb.go`
- Generate: `services/core/internal/gen/tammy/v1/tammyv1connect/system.connect.go`
- Generate: `packages/connect-client/src/gen/tammy/v1/system_pb.ts`

- [ ] **Step 1: Write the failing Go contract test**

```go
package contracts_test

import (
    "testing"

    tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
    tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
)

func TestSystemContract(t *testing.T) {
    if tammyv1connect.SystemServiceName != "tammy.v1.SystemService" {
        t.Fatalf("unexpected service name %q", tammyv1connect.SystemServiceName)
    }
    response := &tammyv1.GetDiagnosticsResponse{
        ApiVersion:      "tammy.v1",
        CoreVersion:     "test",
        RuntimeMode:     tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE,
        NetworkRequired: false,
    }
    if response.GetRuntimeMode() != tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE {
        t.Fatal("offline runtime enum is not usable")
    }
}
```

- [ ] **Step 2: Run the contract test to verify it fails**

Run: `rtk mise exec -- go test ./services/core/internal/contracts`

Expected: FAIL because generated packages do not exist.

- [ ] **Step 3: Create Buf v2 configuration**

`buf.yaml`:

```yaml
version: v2
modules:
  - path: proto
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

`buf.gen.yaml`:

```yaml
version: v2
clean: true
plugins:
  - remote: buf.build/protocolbuffers/go:v1.36.11
    revision: 1
    out: services/core/internal/gen
    opt:
      - paths=source_relative
  - remote: buf.build/connectrpc/go:v1.20.0
    revision: 1
    out: services/core/internal/gen
    opt:
      - paths=source_relative
  - local: protoc-gen-es
    out: packages/connect-client/src/gen
    opt:
      - target=ts
      - import_extension=js
inputs:
  - directory: proto
```

- [ ] **Step 4: Define the versioned diagnostics API**

Use proto edition `2023`, package `tammy.v1`, and this Go option:

```proto
option go_package = "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1;tammyv1";
```

Define:

```proto
enum RuntimeMode {
  RUNTIME_MODE_UNSPECIFIED = 0;
  RUNTIME_MODE_OFFLINE = 1;
}

message GetDiagnosticsRequest {}

message GetDiagnosticsResponse {
  string api_version = 1;
  string core_version = 2;
  RuntimeMode runtime_mode = 3;
  bool network_required = 4;
}

service SystemService {
  rpc GetDiagnostics(GetDiagnosticsRequest) returns (GetDiagnosticsResponse);
}
```

Add comments to every public declaration so `STANDARD` lint passes.

- [ ] **Step 5: Generate both language targets**

Run:

```bash
rtk mise exec -- pnpm proto:format:check
rtk mise exec -- pnpm proto:lint
rtk mise exec -- pnpm proto:generate
rtk mise exec -- go -C services/core mod tidy
```

Expected: one Go message file, one Go Connect file, and one unified Protobuf-ES v2 file are generated. No `_connect.ts` file exists.

- [ ] **Step 6: Verify the generated contract**

Run:

```bash
rtk mise exec -- go test ./services/core/internal/contracts
rtk mise exec -- pnpm proto:format:check
rtk mise exec -- pnpm proto:lint
rtk git diff --check
```

Expected: Go contract test PASS, Buf format and lint checks PASS, and no whitespace errors.

- [ ] **Step 7: Commit the contract**

```bash
rtk git add buf.yaml buf.gen.yaml proto services/core/go.mod services/core/go.sum services/core/internal/contracts services/core/internal/gen packages/connect-client/src/gen
rtk git commit -m "feat: define the system diagnostics API"
```

### Task 3: Implement the diagnostics application service

**Files:**

- Create: `services/core/internal/buildinfo/info.go`
- Create: `services/core/internal/system/service.go`
- Create: `services/core/internal/system/service_test.go`

- [ ] **Step 1: Write the failing service test**

Create a table test that constructs `system.NewService(buildinfo.Info{Version: "0.1.0-test"})`, calls `GetDiagnostics`, and asserts:

```go
if got.Msg.GetApiVersion() != "tammy.v1" ||
    got.Msg.GetCoreVersion() != "0.1.0-test" ||
    got.Msg.GetRuntimeMode() != tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE ||
    got.Msg.GetNetworkRequired() {
    t.Fatalf("unexpected diagnostics: %+v", got.Msg)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `rtk mise exec -- go test ./services/core/internal/system`

Expected: FAIL because `NewService` is undefined.

- [ ] **Step 3: Implement the immutable build-info value**

`buildinfo.Info` has one `Version string` field. `buildinfo.Current()` reads a package variable populated by `-ldflags`, substitutes `dev` only when empty, and returns a value rather than exposing the variable.

- [ ] **Step 4: Implement the Connect handler**

`system.Service` stores `buildinfo.Info`, satisfies `tammyv1connect.SystemServiceHandler`, ignores the empty request, and returns a new `connect.Response` with the four exact contract values. Add a compile-time interface assertion.

- [ ] **Step 5: Run focused and package tests**

Run:

```bash
rtk mise exec -- go test ./services/core/internal/system -run TestService_GetDiagnostics -v
rtk mise exec -- go test ./services/core/...
```

Expected: all tests PASS.

- [ ] **Step 6: Commit the service**

```bash
rtk git add services/core/internal/buildinfo services/core/internal/system
rtk git commit -m "feat: report offline core diagnostics"
```

### Task 4: Enforce the per-process capability at the Connect boundary

**Files:**

- Create: `services/core/internal/transport/capability.go`
- Create: `services/core/internal/transport/capability_test.go`

- [ ] **Step 1: Write failing interceptor tests**

Use an in-memory unary next handler that increments an atomic counter. Cover:

1. the exact capability permits the call once;
2. a missing header returns `connect.CodeUnauthenticated`;
3. a wrong same-length value returns `connect.CodeUnauthenticated`;
4. a wrong different-length value returns the same public error;
5. padded and otherwise malformed Base64URL values return the same public error;
6. duplicate capability headers return the same public error;
7. no rejected request reaches the next handler; and
8. the error and formatted response never contain either capability.

Use the exported constant `CapabilityHeader = "X-Tammy-Capability"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `rtk mise exec -- go test ./services/core/internal/transport -run Capability -v`

Expected: FAIL because `NewCapabilityInterceptor` is undefined.

- [ ] **Step 3: Implement constant-time capability validation**

Decode and validate the expected unpadded Base64URL capability once at construction; require exactly 32 decoded bytes. Require `req.Header().Values(CapabilityHeader)` to contain exactly one value. Hash both the expected and supplied header with SHA-256 and compare the fixed-size digests using `subtle.ConstantTimeCompare`. Missing, duplicate, or malformed supplied values fail with the same `connect.CodeUnauthenticated` and message `local capability rejected`. Never log request headers.

The public constructor is:

```go
func NewCapabilityInterceptor(expected string) (connect.UnaryInterceptorFunc, error)
```

- [ ] **Step 4: Run interceptor and race tests**

Run:

```bash
rtk mise exec -- go test ./services/core/internal/transport -run Capability -v
rtk mise exec -- go test -race ./services/core/internal/transport
```

Expected: all cases PASS and the race detector is clean.

- [ ] **Step 5: Commit the interceptor**

```bash
rtk git add services/core/internal/transport/capability.go services/core/internal/transport/capability_test.go
rtk git commit -m "feat: authenticate local Connect calls"
```

### Task 5: Serve Connect on ephemeral loopback TLS and implement child readiness

**Files:**

- Create: `services/core/internal/transport/certificate.go`
- Create: `services/core/internal/transport/readiness.go`
- Create: `services/core/internal/transport/readiness_test.go`
- Create: `services/core/internal/transport/server.go`
- Create: `services/core/internal/transport/server_integration_test.go`
- Create: `services/core/cmd/tammy-core/main.go`
- Create: `services/core/cmd/tammy-core/main_test.go`

- [ ] **Step 1: Write failing readiness-record tests**

Construct a record with a fixed capability and assert the JSON line is bounded, contains exactly the declared fields, and round-trips:

```json
{"protocol":"tammy-core-ready-v1","port":12345,"ca_pem":"-----BEGIN CERTIFICATE-----...","capability":"<43-character unpadded Base64URL>"}
```

Test rejection of an invalid port, malformed PEM, padded Base64, non-32-byte decoded capability, a record over 64 KiB, unknown fields, and trailing JSON. Assert formatted errors and stderr never contain the capability.

- [ ] **Step 2: Run readiness tests to verify they fail**

Run: `rtk mise exec -- go test ./services/core/internal/transport -run Readiness -v`

Expected: FAIL because `ReadinessRecord` validation/encoding is undefined.

- [ ] **Step 3: Implement bounded readiness encoding**

`ReadinessRecord` contains protocol, port, CA PEM, and capability. Validate the port range, parse the CA with `pem.Decode` plus `x509.ParseCertificate`, and require exactly 32 bytes from `base64.RawURLEncoding.Strict().DecodeString`. `WriteReadiness` validates first, marshals once, enforces a 64 KiB maximum, appends exactly one newline, and performs one write to the supplied parent pipe.

- [ ] **Step 4: Write failing certificate and server integration tests**

Start the server with an injected clock and randomness source. Assert:

- the listener is IPv4 `127.0.0.1` with an OS-selected non-zero port;
- the returned PEM authenticates the leaf for IP `127.0.0.1`;
- two server launches produce different CA certificates and capabilities;
- negotiated TLS is exactly TLS 1.3;
- a generated Connect-Go client with the correct header receives offline diagnostics;
- missing/wrong capability receives `Unauthenticated`;
- duplicate capability headers are rejected rather than joined or first-value selected;
- plain HTTP fails;
- explicit `Server.Shutdown` completes within three seconds; and
- neither the private key nor capability appears in captured stderr.

- [ ] **Step 5: Run the integration test to verify it fails**

Run: `rtk mise exec -- go test ./services/core/internal/transport -run 'Certificate|Server' -v`

Expected: FAIL because the ephemeral certificate and server constructors are undefined.

- [ ] **Step 6: Implement in-memory CA and leaf creation**

Generate an ECDSA P-256 CA and a separate ECDSA P-256 leaf for every core launch. Both certificates are valid from injected `now - 1 minute` through `now + 100 years`, a long practical X.509 window that spans the lifetime of a continuously running supervised process under normal TLS verification. The CA has `IsCA`, `BasicConstraintsValid`, certificate-signing key usage, and a random 128-bit positive serial. The leaf has a separate random serial, `127.0.0.1` as its only SAN, and server-auth extended usage. Generate the 32-byte capability from `crypto/rand.Reader` in the same constructor. Return the TLS certificate, CA PEM, and encoded capability; keep the CA key, leaf key, and capability only in core memory, never persist or reuse them, and terminate their security lifetime when the supervised core exits.

- [ ] **Step 7: Implement the loopback server**

Create `net.Listen("tcp4", "127.0.0.1:0")`, then wrap it with:

```go
&tls.Config{
    Certificates: []tls.Certificate{leaf},
    MinVersion:   tls.VersionTLS13,
    MaxVersion:   tls.VersionTLS13,
    NextProtos:   []string{"http/1.1"},
}
```

Register `tammyv1connect.NewSystemServiceHandler` on a dedicated `http.ServeMux` with the capability interceptor and a 1 MiB decoded request-message limit. Wrap the complete mux in a 1,048,581-byte HTTP request-body limit, allowing one maximum-sized message plus the 5-byte Connect envelope. Configure `http.Server` with a 2-second read-header timeout, 5-second whole-request read timeout, 5-second response write timeout, 30-second idle timeout, 16 KiB max headers, and no global default mux. `Server.Ready()` returns:

```json
{"protocol":"tammy-core-ready-v1","port":12345,"ca_pem":"-----BEGIN CERTIFICATE-----...","capability":"<43-character unpadded Base64URL>"}
```

Use JSON encoding for the one readiness line on stdout. All operational logs go to stderr as structured messages without secrets.

- [ ] **Step 8: Write the failing cross-platform executable contract test**

First create a compile-only `main.go` stub:

```go
package main

import "os"

func run(_ *os.File, _ *os.File, _ *os.File) int { return 0 }

func main() { os.Exit(run(os.Stdin, os.Stdout, os.Stderr)) }
```

Then, in `cmd/tammy-core/main_test.go`, build the real command to `filepath.Join(t.TempDir(), binaryName)` where `binaryName` gains `.exe` on Windows. Spawn it with explicit stdin/stdout/stderr pipes and no shell. The test must:

1. read one newline-terminated record with a 64 KiB limit and validate protocol, loopback port, CA, and 32-byte capability;
2. call generated Connect-Go diagnostics using the returned CA and capability;
3. close stdin, require process exit 0 within three seconds, and require stdout EOF with no second record;
4. assert stderr contains neither capability nor `PRIVATE KEY`;
5. run a child with a deliberately closed stdout pipe and require a non-zero readiness-write exit; and
6. never print the capability, even on assertion failure.

- [ ] **Step 9: Run the executable test to verify its first assertion fails**

Run: `rtk mise exec -- go test ./services/core/cmd/tammy-core -run Process -v`

Expected before completing `main`: FAIL because no valid readiness record is produced.

- [ ] **Step 10: Implement deterministic lifecycle in `main`**

Keep process orchestration in a `run(stdin, stdout, stderr) int` function so exit-code paths are unit-testable without `os.Exit`. `main()` only calls `os.Exit(run(os.Stdin, os.Stdout, os.Stderr))`. `run` constructs the server, begins serving, writes exactly one readiness line to stdout, then treats the inherited stdin pipe only as a parent-liveness channel and waits for EOF or `SIGINT`/`SIGTERM`. It calls `Shutdown` with a three-second context and returns non-zero for certificate/randomness/start/readiness-write/serve failures. Link the version with:

```text
-X github.com/tammyapp/tammy/services/core/internal/buildinfo.version=<version>
```

- [ ] **Step 11: Run security-focused Go verification**

Run:

```bash
rtk mise exec -- gofmt -w services/core
rtk mise exec -- go vet ./services/core/...
rtk mise exec -- go test -race ./services/core/...
```

Expected: vet is silent, every unit/integration/process test passes under the race detector on the current target, and no fixed temporary path is used.

- [ ] **Step 12: Commit the secure core**

```bash
rtk git add services/core/cmd services/core/internal/transport
rtk git commit -m "feat: serve the local API over ephemeral TLS"
```

### Chunk 1 verification checkpoint

- [ ] Run:

```bash
rtk mise exec -- pnpm test:toolchain
rtk mise exec -- pnpm check:toolchain
rtk mise exec -- pnpm proto:format:check
rtk mise exec -- pnpm proto:lint
rtk mise exec -- pnpm proto:generate
rtk git diff --exit-code -- services/core/internal/gen packages/connect-client/src/gen
rtk mise exec -- go vet ./services/core/...
rtk mise exec -- go test -race ./services/core/...
rtk git diff --check
rtk git status --short
```

Expected: all commands pass and the worktree is clean.

## Chunk 2: Electron supervision, typed IPC, and offline React surface

### Task 6: Configure the pinned Electron, React, Tailwind, and shadcn workspace

**Files:**

- Create: `apps/desktop/package.json`
- Create: `apps/desktop/forge.config.ts`
- Create: `apps/desktop/tsconfig.json`
- Create: `apps/desktop/vite.main.config.ts`
- Create: `apps/desktop/vite.preload.config.ts`
- Create: `apps/desktop/vite.renderer.config.ts`
- Create: `apps/desktop/vitest.config.ts`
- Create: `apps/desktop/src/renderer/test/setup.ts`
- Create: `apps/desktop/src/renderer/test/tooling.fixture.ts`
- Create: `apps/desktop/src/renderer/test/tooling.test.ts`
- Create: `apps/desktop/components.json`
- Create: `apps/desktop/index.html`
- Create: `apps/desktop/resources/core/.gitkeep`
- Modify: `package.json`
- Modify: `pnpm-workspace.yaml`

- [ ] **Step 1: Create the exact desktop dependency manifest**

Use:

```json
{
  "name": "@tammy/desktop",
  "productName": "Tammy",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "main": ".vite/build/main.js",
  "scripts": {
    "start": "electron-forge start",
    "package": "electron-forge package",
    "make": "electron-forge make",
    "test": "vitest run",
    "test:watch": "vitest",
    "typecheck": "tsc --noEmit",
    "e2e": "playwright test"
  },
  "dependencies": {
    "@bufbuild/protobuf": "2.12.1",
    "@connectrpc/connect": "2.1.2",
    "@connectrpc/connect-node": "2.1.2",
    "@radix-ui/react-slot": "1.3.0",
    "@tammy/connect-client": "workspace:*",
    "class-variance-authority": "0.7.1",
    "clsx": "2.1.1",
    "lucide-react": "1.25.0",
    "react": "19.2.7",
    "react-dom": "19.2.7",
    "tailwind-merge": "3.6.0",
    "zod": "4.4.3"
  },
  "devDependencies": {
    "@electron-forge/cli": "7.11.2",
    "@electron-forge/maker-squirrel": "7.11.2",
    "@electron-forge/maker-zip": "7.11.2",
    "@electron-forge/plugin-fuses": "7.11.2",
    "@electron-forge/plugin-vite": "7.11.2",
    "@electron-forge/shared-types": "7.11.2",
    "@electron/fuses": "2.1.3",
    "@playwright/test": "1.61.1",
    "@tailwindcss/vite": "4.3.3",
    "@testing-library/react": "16.3.2",
    "@testing-library/user-event": "14.6.1",
    "@types/node": "26.1.1",
    "@types/react": "19.2.17",
    "@types/react-dom": "19.2.3",
    "@vitejs/plugin-react": "6.0.3",
    "electron": "43.1.1",
    "jsdom": "29.1.1",
    "shadcn": "4.13.1",
    "tailwindcss": "4.3.3",
    "vite": "8.1.5",
    "vitest": "4.1.10"
  }
}
```

- [ ] **Step 2: Add the root scripts that are valid before application composition**

Add these root scripts:

```json
{
  "desktop:test": "pnpm --dir apps/desktop test",
  "desktop:typecheck": "pnpm --dir apps/desktop typecheck"
}
```

Extend the lifecycle allowlist to
`onlyBuiltDependencies: ["@bufbuild/buf", "electron", "electron-winstaller", "esbuild"]`.
`electron-winstaller` is the fourth approved package after auditing the exact `5.4.4`
published tarball: its install hook only selects the bundled host-architecture 7-Zip
executable and DLL with two package-local file copies. It performs no network access,
command execution, or external writes. Pin that package and the audited security updates
with exact workspace overrides. Because pnpm 11 executes build policy through
`allowBuilds`, mirror the same four audited approvals there while retaining the explicit
`onlyBuiltDependencies` list:

```yaml
allowBuilds:
  "@bufbuild/buf": true
  electron: true
  electron-winstaller: true
  esbuild: true

overrides:
  electron-winstaller: 5.4.4
  tar: 7.5.20
  tmp: 0.2.7
```

No install script beyond those four pinned packages is approved.
Native Windows x64 execution of the audited `electron-winstaller` hook and MakerSquirrel
is explicitly deferred to Task 14's `windows-server-x64-package-smoke` job on Windows
Server 2025; macOS cannot validate that Squirrel path.

- [ ] **Step 3: Create the complete Forge configuration**

Use constructor syntax and exact entry paths:

```ts
import { MakerSquirrel } from "@electron-forge/maker-squirrel";
import { MakerZIP } from "@electron-forge/maker-zip";
import { FusesPlugin } from "@electron-forge/plugin-fuses";
import { VitePlugin } from "@electron-forge/plugin-vite";
import type { ForgeConfig } from "@electron-forge/shared-types";
import { FuseV1Options, FuseVersion } from "@electron/fuses";

const config: ForgeConfig = {
  packagerConfig: {
    asar: true,
    executableName: "Tammy",
    extraResource: ["resources/core"],
  },
  makers: [
    new MakerSquirrel(
      { authors: "Tammy", description: "Local-first Australian accounting software" },
      ["win32"],
    ),
    new MakerZIP({}, ["darwin"]),
  ],
  plugins: [
    new VitePlugin({
      concurrent: 2,
      build: [
        { entry: "src/main/index.ts", config: "vite.main.config.ts" },
        { entry: "src/preload/index.ts", config: "vite.preload.config.ts" },
      ],
      renderer: [{ name: "main_window", config: "vite.renderer.config.ts" }],
    }),
    new FusesPlugin({
      version: FuseVersion.V1,
      [FuseV1Options.RunAsNode]: false,
      [FuseV1Options.EnableCookieEncryption]: true,
      [FuseV1Options.EnableNodeOptionsEnvironmentVariable]: false,
      [FuseV1Options.EnableNodeCliInspectArguments]: true,
      [FuseV1Options.EnableEmbeddedAsarIntegrityValidation]: true,
      [FuseV1Options.OnlyLoadAppFromAsar]: true,
    }),
  ],
};

export default config;
```

`EnableNodeCliInspectArguments` remains enabled only because this is an unsigned test artifact and Playwright's Electron driver requires it. The release-hardening plan creates a separately fused production artifact with it disabled.

- [ ] **Step 4: Create complete TypeScript and Vite configurations**

`apps/desktop/tsconfig.json` extends `../../tsconfig.base.json`, includes `src`, `tests`, every
Vite config, `forge.config.ts`, and `playwright.config.ts`, adds types
`["node", "vite/client"]`, and maps `@/*` to `./src/*` without `baseUrl`. Keep the root
`skipLibCheck: false`, but set it to `true` in this app config only: pinned Forge `7.11.2`
publishes an undeclared `NewCtx`, and its listr2 `7.0.2` dependency has a type-only `rxjs`
import without the peer installed. Remove this scoped exception when those pins are upgraded;
Tammy source and config API use remain strictly checked.

Use these build policies:

```ts
// vite.main.config.ts and vite.preload.config.ts
import { defineConfig } from "vite";

export default defineConfig({
  build: {
    sourcemap: false,
    minify: true,
    rollupOptions: { external: ["electron"] },
  },
});
```

```ts
// vite.renderer.config.ts
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  base: "./",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: { sourcemap: false, minify: true },
});
```

`vitest.config.ts` uses `jsdom`, `globals: false`, restores and clears mocks, loads `src/renderer/test/setup.ts`, and includes `src/**/*.test.ts` plus `src/**/*.test.tsx`. Create the setup file in this task with explicit `afterEach(cleanup)` imports from Vitest and Testing Library so every test starts with an empty DOM; it must not require an undeclared matcher package.

Add a tooling smoke test that loads the real renderer config, covers the `@` alias with a
type-only source probe, and verifies the HTML entry remains local and has no CSP meta tag.

- [ ] **Step 5: Configure shadcn without generating network-dependent code**

Create `components.json` with style `new-york`, React Server Components disabled, TSX enabled, Tailwind CSS entry `src/renderer/styles.css`, neutral base colour, CSS variables enabled, and aliases:

```json
{
  "components": "@/renderer/components",
  "utils": "@/renderer/lib/utils",
  "ui": "@/renderer/components/ui",
  "lib": "@/renderer/lib",
  "hooks": "@/renderer/hooks"
}
```

The actual four audited shadcn-derived components are committed in Task 10. Do not run an unpinned `shadcn@latest` command.

- [ ] **Step 6: Create the local-only HTML entry**

`index.html` contains `<title>Tammy</title>`, the root element, and the local module entry `/src/renderer/main.tsx`; it has no remote asset or script. It contains no CSP meta tag. Task 9 installs exactly one environment-specific CSP response header: a strict offline production policy for `tammy://app/`, or a development-only policy that permits the exact Vite origin/HMR WebSocket.

- [ ] **Step 7: Install and verify configuration resolution**

Run:

```bash
rtk mise exec -- pnpm install
rtk mise exec -- pnpm --dir apps/desktop exec electron --version
rtk mise exec -- pnpm --dir apps/desktop exec vite --version
rtk mise exec -- pnpm desktop:typecheck
rtk mise exec -- pnpm desktop:test
rtk mise exec -- pnpm lint
rtk mise exec -- pnpm audit --audit-level high
rtk mise exec -- pnpm install --frozen-lockfile
```

Expected: Electron prints `v43.1.1`, Vite prints `8.1.5`, TypeScript, Vitest, Biome, audit,
and frozen installation all pass.

- [ ] **Step 8: Commit desktop tooling**

```bash
rtk git add package.json pnpm-lock.yaml pnpm-workspace.yaml apps/desktop/package.json apps/desktop/forge.config.ts apps/desktop/tsconfig.json apps/desktop/vite.* apps/desktop/vitest.config.ts apps/desktop/components.json apps/desktop/index.html apps/desktop/resources/core/.gitkeep apps/desktop/src/renderer/test
rtk git commit -m "build: configure the Electron React workspace"
```

### Task 7: Parse readiness and supervise the Go child process

**Files:**

- Create: `apps/desktop/src/shared/readiness.ts`
- Create: `apps/desktop/src/shared/readiness.test.ts`
- Create: `apps/desktop/src/main/core-process.ts`
- Create: `apps/desktop/src/main/core-process.test.ts`

- [ ] **Step 1: Write failing strict-readiness parser tests**

Cover one valid record plus invalid JSON, unknown fields, extra lines, over 64 KiB, non-loopback/invalid port, malformed CA, padded or wrong-length capability, and errors that never echo record content. The returned value is frozen and has exact keys `protocol`, `port`, `caPem`, and `capability`.

- [ ] **Step 2: Run the parser tests to verify they fail**

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- readiness.test.ts`

Expected: FAIL because `parseReadiness` is undefined.

- [ ] **Step 3: Implement strict bounded readiness parsing**

Export:

```ts
export interface CoreReadiness {
  readonly protocol: "tammy-core-ready-v1";
  readonly port: number;
  readonly caPem: string;
  readonly capability: string;
}

export function parseReadiness(bytes: Uint8Array): Readonly<CoreReadiness>;
```

Reject input over 65,536 bytes or anything other than one newline-terminated JSON object. Use a strict Zod object, `new X509Certificate(caPem)` to validate the certificate, and `Buffer.from(value, "base64url")` followed by an exact 32-byte length and canonical unpadded re-encoding check. Public errors use stable codes only.

- [ ] **Step 4: Write failing process-supervisor tests with injected fakes**

Model states `IDLE → STARTING → READY → STOPPING → STOPPED` plus terminal `FAILED`. Inject `spawn`, clock, timers, and logger. Assert:

- spawn uses an absolute binary path, `shell: false`, no arguments, piped stdin/stdout/stderr, and an allowlisted non-secret environment;
- readiness resolves once and its fields are not logged;
- timeout, malformed/extra stdout, spawn error, and early exit reject with stable redacted errors;
- stderr lines are bounded and redacted before logging;
- `stop()` closes stdin, waits up to three seconds, then kills only if needed;
- concurrent `start()`/`stop()` calls are deterministic; and
- capability, CA, and port are absent from public diagnostic projections.

- [ ] **Step 5: Run supervisor tests to verify they fail**

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- core-process.test.ts`

Expected: FAIL because `CoreProcess` is undefined.

- [ ] **Step 6: Implement the process supervisor**

Use `node:child_process.spawn` directly, never a shell. The production environment allowlist contains only `SYSTEMROOT`, `WINDIR`, `TEMP`, `TMP`, `TMPDIR`, and `LANG` when present. Parse stdout incrementally and fail above 64 KiB before readiness. After one record, any later stdout byte is fatal. Keep readiness in a private field, expose it only to main-process composition, and replace it with `undefined` on stop/failure.

- [ ] **Step 7: Verify process-state tests**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- readiness.test.ts core-process.test.ts
rtk mise exec -- pnpm --dir apps/desktop typecheck
```

Expected: all tests PASS and TypeScript is clean.

- [ ] **Step 8: Commit core supervision**

```bash
rtk git add apps/desktop/src/shared/readiness* apps/desktop/src/main/core-process*
rtk git commit -m "feat: supervise the local Go core"
```

### Task 8: Create the Electron-main Connect-ES v2 client

**Files:**

- Create: `apps/desktop/src/main/core-client.ts`
- Create: `apps/desktop/src/main/core-client.test.ts`
- Create: `apps/desktop/src/main/core-client.integration.test.ts`

- [ ] **Step 1: Write failing client projection and interceptor tests**

Inject a Connect `Transport` fake. Assert `getDiagnostics()` calls `SystemService.getDiagnostics`, adds exactly one `X-Tammy-Capability` header, and returns only:

```ts
export interface SystemDiagnostics {
  readonly apiVersion: string;
  readonly coreVersion: string;
  readonly runtimeMode: "offline";
  readonly networkRequired: false;
}
```

Assert no returned object or public error contains the port, CA, capability, request headers, or raw protobuf.

- [ ] **Step 2: Run the client test to verify it fails**

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- core-client.test.ts`

Expected: FAIL because `createCoreClient` is undefined.

- [ ] **Step 3: Implement the generated Connect-ES client**

Import `SystemService` and `RuntimeMode` from `@tammy/connect-client/tammy/v1/system_pb.js`. Use `createClient` from Connect-ES 2.1.2. Build the production transport with:

```ts
createConnectTransport({
  baseUrl: `https://127.0.0.1:${readiness.port}`,
  httpVersion: "1.1",
  defaultTimeoutMs: 5_000,
  nodeOptions: {
    ca: readiness.caPem,
    rejectUnauthorized: true,
    minVersion: "TLSv1.3",
    maxVersion: "TLSv1.3",
  },
  interceptors: [capabilityInterceptor(readiness.capability)],
});
```

The interceptor uses `request.header.set`, never appends. Reject a response unless it has the expected API version, offline enum, and `networkRequired === false`.

- [ ] **Step 4: Write the failing real Connect-Go/Connect-ES integration test**

Build the Go core into `mkdtemp(join(tmpdir(), "tammy-core-test-"))` using `execFile("go", ["build", ...])`; add `.exe` on Windows and remove the directory in `afterAll`. Start it through `CoreProcess`, call through the real Node transport, assert offline diagnostics, then stop it and assert exit 0. A second call with a changed capability must return `Unauthenticated`.

- [ ] **Step 5: Run the integration test**

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- core-client.integration.test.ts`

Expected: PASS. If it fails, use the exact Go/ES generation or TLS option mismatch only as troubleshooting evidence; resolve the incompatibility without weakening TLS validation, then rerun until it passes.

- [ ] **Step 6: Verify unit and interoperability suites**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- core-client
rtk mise exec -- pnpm --dir apps/desktop typecheck
rtk mise exec -- go test ./services/core/...
```

Expected: all client tests and Go tests PASS.

- [ ] **Step 7: Commit the main-process RPC client**

```bash
rtk git add apps/desktop/src/main/core-client*
rtk git commit -m "feat: call the core with Connect ES"
```

### Task 9: Expose one validated preload use case and harden the renderer boundary

**Files:**

- Create: `apps/desktop/src/shared/desktop-api.ts`
- Create: `apps/desktop/src/main/ipc.ts`
- Create: `apps/desktop/src/main/ipc.test.ts`
- Create: `apps/desktop/src/main/security.ts`
- Create: `apps/desktop/src/main/security.test.ts`
- Create: `apps/desktop/src/preload/index.ts`

- [ ] **Step 1: Write failing sender, CSP, and navigation-policy tests**

Test pure policies for:

- only the current main frame at `tammy://app/` is accepted in production;
- only the exact Vite origin is accepted in development;
- lookalike hosts, subframes, `file:`, `data:`, `javascript:`, HTTP, and HTTPS are denied;
- production CSP has `connect-src 'none'`, no `unsafe-eval`, and no remote origin;
- permission, download, navigation, and window-open decisions deny by default.

- [ ] **Step 2: Run security tests to verify they fail**

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- security.test.ts ipc.test.ts`

Expected: FAIL because the policies and IPC registrar are undefined.

- [ ] **Step 3: Define the structured-clone-safe desktop API**

`desktop-api.ts` exports `SystemDiagnostics`, `TammyDesktopAPI`, and the private channel constant. `TammyDesktopAPI` contains exactly:

```ts
readonly getSystemDiagnostics: () => Promise<SystemDiagnostics>;
```

Augment `Window` with `readonly tammy: TammyDesktopAPI`. Do not export a generic invoke, event listener, path, shell, or transport API.

- [ ] **Step 4: Implement validated IPC and preload**

`ipcMain.handle` verifies `event.senderFrame === mainWindow.webContents.mainFrame` and the exact allowlisted URL before calling the injected diagnostics function. It throws a stable `IPC_SENDER_REJECTED` error otherwise. Preload uses `contextBridge.exposeInMainWorld` with `Object.freeze({ getSystemDiagnostics: () => ipcRenderer.invoke(channel) })`; it never exposes `ipcRenderer`.

- [ ] **Step 5: Implement application protocol and window security policy**

Before `ready`, register `tammy` as a standard, secure, fetch-enabled scheme and call `app.enableSandbox()`. After `ready`, map only `tammy://app/` paths to files under the compiled renderer root after resolving and checking containment. Configure:

```ts
webPreferences: {
  preload: preloadPath,
  sandbox: true,
  contextIsolation: true,
  nodeIntegration: false,
  webSecurity: true,
  allowRunningInsecureContent: false,
  spellcheck: false,
}
```

Deny all permissions and downloads. Deny `will-navigate` unless the URL is the already loaded exact application URL. Return `{ action: "deny" }` from every `setWindowOpenHandler`. Install one CSP response header, never two: production uses `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'`; development uses the exact Vite origin as `self`, permits only its matching `ws:` HMR origin in `connect-src`, and permits `unsafe-inline` only in `style-src` for injected HMR styles. Neither policy permits remote application data.

- [ ] **Step 6: Run boundary tests and type checking**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- security.test.ts ipc.test.ts
rtk mise exec -- pnpm --dir apps/desktop typecheck
```

Expected: every allow/deny table case passes.

- [ ] **Step 7: Commit the renderer boundary**

```bash
rtk git add apps/desktop/src/shared/desktop-api.ts apps/desktop/src/main/ipc* apps/desktop/src/main/security* apps/desktop/src/preload
rtk git commit -m "feat: expose a hardened desktop bridge"
```

### Task 10: Build the compact ledger-first offline diagnostics surface

**Files:**

- Create: `apps/desktop/src/renderer/lib/utils.ts`
- Create: `apps/desktop/src/renderer/components/ui/badge.tsx`
- Create: `apps/desktop/src/renderer/components/ui/button.tsx`
- Create: `apps/desktop/src/renderer/components/ui/card.tsx`
- Create: `apps/desktop/src/renderer/components/ui/separator.tsx`
- Create: `apps/desktop/src/renderer/features/diagnostics/diagnostics-card.tsx`
- Create: `apps/desktop/src/renderer/app.tsx`
- Create: `apps/desktop/src/renderer/app.test.tsx`
- Create: `apps/desktop/src/renderer/main.tsx`
- Create: `apps/desktop/src/renderer/styles.css`

- [ ] **Step 1: Write failing accessible UI tests**

Mock only `window.tammy.getSystemDiagnostics`. Test:

- initial status is announced as `Starting local engine`;
- success shows `Local engine ready`, `Offline`, API/core versions, and `No cloud required`;
- failure shows `Local engine unavailable` with a retry button that calls the typed method again;
- the workspace action is visibly disabled and labelled `Workspace setup comes next`;
- future module names are absent until their implementation plan ships;
- navigation landmarks, headings, focus order, and status live region are semantic; and
- neither raw errors nor transport security fields render.

- [ ] **Step 2: Run the UI test to verify it fails**

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- app.test.tsx`

Expected: FAIL because `App` is undefined.

- [ ] **Step 3: Add the audited shadcn primitives and utility**

Create small local `Badge`, `Button`, `Card`, and `Separator` components using Radix Slot only where `asChild` is supported. Use `cn(...inputs)` backed by `clsx` and `tailwind-merge`. Keep each component below 100 lines and retain a comment with the shadcn source/version used for audit provenance.

- [ ] **Step 4: Define the restrained desktop visual system**

`styles.css` imports Tailwind v4 and defines light/dark CSS variables with:

- warm neutral application background;
- white/near-black surfaces;
- one eucalyptus green accent for local/ready state;
- amber/red only for warnings/errors;
- tabular numerals for financial/status values;
- compact 12/14/16 px typography rhythm;
- 4/8/12/16/24/32 px spacing;
- visible 2 px focus rings;
- no gradients, glass effects, oversized hero text, or decorative animations; and
- `prefers-reduced-motion` disabling non-essential transitions.

- [ ] **Step 5: Implement the workspace shell and diagnostics state**

Use a 216 px left rail, 44 px title bar, and flexible content pane. The rail contains product/workspace context and only the active Overview destination; Accounts, Journal, BAS, Submissions, Audit, and all other future modules are omitted. `DiagnosticsCard` owns only load/retry presentation; `App` owns the typed call state. Do not add mock balances, fake ATO status, or a cloud sign-in.

- [ ] **Step 6: Run UI, type, and lint verification**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- app.test.tsx
rtk mise exec -- pnpm --dir apps/desktop typecheck
rtk mise exec -- pnpm lint
```

Expected: UI tests PASS, TypeScript is clean, and Biome is clean.

- [ ] **Step 7: Commit the renderer**

```bash
rtk git add apps/desktop/src/renderer
rtk git commit -m "feat: add the offline desktop foundation screen"
```

### Task 11: Compose Electron main with deterministic start and shutdown

**Files:**

- Create: `scripts/build-core.mjs`
- Create: `scripts/build-core.test.mjs`
- Create: `apps/desktop/src/main/index.ts`
- Create: `apps/desktop/src/main/index.test.ts`
- Create: `apps/desktop/src/main/vite-env.d.ts`
- Modify: `package.json`

- [ ] **Step 1: Write the failing core-build resolver tests**

Test pure functions for only `darwin/arm64` and `win32/x64`, `.exe` selection, development and packaged resource paths, version-safe linker arguments, and rejection of Linux, Intel macOS, Windows ARM, path traversal, or an absent binary.

- [ ] **Step 2: Run resolver tests to verify they fail**

Run: `rtk mise exec -- node --test scripts/build-core.test.mjs`

Expected: FAIL because the resolver/build argument functions are undefined.

- [ ] **Step 3: Implement the cross-platform core build script**

Use `execFile` with:

```text
go build -trimpath -buildvcs=true
-ldflags=-s -w -X github.com/tammyapp/tammy/services/core/internal/buildinfo.version=<package version>
-o apps/desktop/resources/core/<platform>-<arch>/tammy-core[.exe]
./services/core/cmd/tammy-core
```

Set `CGO_ENABLED=0`; never use a shell. Build only the current supported native target unless an explicit CI target is provided. Hash the binary with SHA-256 and return its path/hash without printing environment values.
Before building, remove every generated entry under `apps/desktop/resources/core` except the committed zero-byte `.gitkeep`, then create only the selected target directory and binary. Resolver tests seed stale target and arbitrary files and prove the cleanup removes them without touching `.gitkeep`.

Add the root scripts only after this executable exists:

```json
{
  "core:build": "node scripts/build-core.mjs",
  "desktop:start": "pnpm core:build && pnpm --dir apps/desktop start",
  "desktop:package": "pnpm core:build && pnpm --dir apps/desktop package"
}
```

- [ ] **Step 4: Write the failing application composition test**

Inject fakes for app readiness, `CoreProcess`, client, window factory, IPC registrar, and logger. Assert the order is:

1. register scheme and enable sandbox before ready;
2. start core;
3. complete a diagnostics RPC;
4. register IPC;
5. create/show the window;
6. on quit, unregister IPC, close window, stop core, then exit.

Core startup/diagnostics failure must create no normal application window and must return a stable local-engine error path.

- [ ] **Step 5: Run composition tests to verify they fail**

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- index.test.ts`

Expected: FAIL because `startDesktopApplication` is undefined.

- [ ] **Step 6: Implement main composition**

Keep `index.ts` as a composition root under 150 lines. It registers the custom scheme before ready, installs security policy, resolves and starts the bundled core, constructs the client, performs the startup diagnostic, registers IPC, creates the BrowserWindow hidden, loads the exact Vite URL in development or `tammy://app/` when packaged, and shows only after `ready-to-show`. A single guarded shutdown promise owns all cleanup.

- [ ] **Step 7: Build and run the full desktop in development**

Run:

```bash
rtk mise exec -- node --test scripts/build-core.test.mjs
rtk mise exec -- node scripts/build-core.mjs
rtk mise exec -- pnpm desktop:test
rtk mise exec -- pnpm desktop:typecheck
rtk mise exec -- pnpm desktop:start
```

Expected: Tammy opens, shows `Local engine ready` and `Offline`, creates no external network request, and closing the window leaves no `tammy-core` process.

- [ ] **Step 8: Commit the composed application**

```bash
rtk git add scripts/build-core* apps/desktop/src/main/index* apps/desktop/src/main/vite-env.d.ts apps/desktop/resources package.json pnpm-lock.yaml
rtk git commit -m "feat: boot the offline desktop application"
```

### Chunk 2 verification checkpoint

- [ ] Run:

```bash
rtk mise exec -- pnpm proto:format:check
rtk mise exec -- pnpm proto:lint
rtk mise exec -- pnpm proto:generate
rtk git diff --exit-code -- services/core/internal/gen packages/connect-client/src/gen
rtk mise exec -- go test -race ./services/core/...
rtk mise exec -- pnpm desktop:test
rtk mise exec -- pnpm desktop:typecheck
rtk mise exec -- pnpm lint
rtk git diff --check
rtk git status --short
```

Expected: all tests and checks pass and the worktree is clean.

## Chunk 3: Packaging, end-to-end proof, CI, and compliance evidence

### Task 12: Produce a verifiable packaged layout and build manifest

**Files:**

- Create: `apps/desktop/resources/build/.gitkeep`
- Create: `apps/desktop/scripts/find-packaged-app.mjs`
- Create: `apps/desktop/scripts/find-packaged-app.test.mjs`
- Create: `scripts/write-build-manifest.mjs`
- Create: `scripts/write-build-manifest.test.mjs`
- Create: `compliance/build/toolchain.lock.json`
- Modify: `apps/desktop/forge.config.ts`
- Modify: `package.json`

- [ ] **Step 1: Write failing package-layout tests**

Test pure path resolution for:

| Target | App executable | Bundled core |
|---|---|---|
| `darwin/arm64` | `out/Tammy-darwin-arm64/Tammy.app/Contents/MacOS/Tammy` | `.../Contents/Resources/core/darwin-arm64/tammy-core`; manifest `.../Contents/Resources/build/build-manifest.json` |
| `win32/x64` | `out/Tammy-win32-x64/Tammy.exe` | `out/Tammy-win32-x64/resources/core/win32-x64/tammy-core.exe`; manifest `.../resources/build/build-manifest.json` |

Reject other platform/architecture pairs and path traversal. Verification must require a regular application executable, regular core executable, exact SHA-256 match with the pre-package core, core outside `app.asar`, and executable permission on macOS. Seed stale and arbitrary source/packaged resource files and require rejection unless the recursive file sets are exactly `.gitkeep` plus the selected target core under `core/`, and `.gitkeep` plus `build-manifest.json` under `build/`; both `.gitkeep` files must be zero-byte regular files.

- [ ] **Step 2: Run package-layout tests to verify they fail**

Run: `rtk mise exec -- node --test apps/desktop/scripts/find-packaged-app.test.mjs`

Expected: FAIL because `resolvePackagedLayout` and `verifyPackagedLayout` are undefined.

- [ ] **Step 3: Implement package discovery and verification**

Use only `node:path`, `node:fs/promises`, and `node:crypto`. Accept the source manifest path as a required `--source-manifest` argument; never discover a different binary or manifest heuristically. Recursively enumerate both source and packaged `core/` and `build/` trees and fail on any entry outside the exact allowlists from Step 1. Require the packaged manifest at the exact platform path, require its bytes to equal the source manifest, read `core_sha256` from that authenticated comparison, and require the external packaged core to match it. CLI mode prints only canonical app/core/manifest paths and hashes as JSON. It returns non-zero on a missing or extra file, manifest mismatch, core hash mismatch, ASAR-contained core, or lost executable bit.

- [ ] **Step 4: Write failing deterministic manifest tests**

Inject command outputs and file hashes. Assert stable key ordering and exact fields:

```json
{
  "schema": "tammy-build-manifest-v1",
  "source_revision": "<40-hex>",
  "source_dirty": false,
  "target": "darwin-arm64",
  "versions": {},
  "lockfiles": {},
  "protobuf_tree_sha256": "<64-hex>",
  "core_sha256": "<64-hex>",
  "test_profile": "foundation-packaged-e2e",
  "sbr_status": "SIMULATOR_NOT_IMPLEMENTED",
  "signed": false
}
```

Reject dirty source in CI mode, missing pins, unsupported targets, malformed hashes, any credential/environment field, or a manifest write that leaves a stale/temporary file in the cleaned build staging directory.

- [ ] **Step 5: Run manifest tests to verify they fail**

Run: `rtk mise exec -- node --test scripts/write-build-manifest.test.mjs`

Expected: FAIL because `createBuildManifest` is undefined.

- [ ] **Step 6: Implement manifest generation**

Read versions from committed manifests, execute tools with `execFile` and arrays, hash `pnpm-lock.yaml`, `services/core/go.sum`, every sorted file under `proto/`, and the selected core binary. Before writing, remove every generated entry under `apps/desktop/resources/build` except the committed zero-byte `.gitkeep`. Write canonical two-space JSON plus newline to `apps/desktop/resources/build/build-manifest.json` through a temporary file and atomic rename, and ensure no temporary file survives success or failure. Never read arbitrary environment variables.

- [ ] **Step 7: Pin the human-readable toolchain evidence**

`compliance/build/toolchain.lock.json` records every version in this plan, retrieval date `2026-07-19`, package/module name, official source URL, target OS/architecture, and the fact that Connect-ES v2 uses unified `protoc-gen-es` without `protoc-gen-connect-es`.

- [ ] **Step 8: Include the manifest and enforce the package pipeline**

Change Forge `extraResource` to `["resources/core", "resources/build"]`. Change root scripts to:

```json
{
  "build:manifest": "node scripts/write-build-manifest.mjs",
  "desktop:package": "pnpm core:build && pnpm build:manifest && pnpm --dir apps/desktop package && node apps/desktop/scripts/find-packaged-app.mjs --verify --source-manifest apps/desktop/resources/build/build-manifest.json"
}
```

- [ ] **Step 9: Commit the complete packaging implementation before identifying its source**

Run:

```bash
rtk mise exec -- node --test apps/desktop/scripts/find-packaged-app.test.mjs scripts/write-build-manifest.test.mjs
rtk git add apps/desktop/forge.config.ts apps/desktop/resources/build/.gitkeep apps/desktop/scripts package.json scripts/write-build-manifest* compliance/build/toolchain.lock.json
rtk git commit -m "build: verify the packaged desktop layout"
```

Expected: tests PASS and the implementation commit succeeds.

- [ ] **Step 10: Package and verify the clean committed revision**

Run:

```bash
rtk git status --porcelain
rtk mise exec -- pnpm desktop:package
rtk git status --porcelain
```

Expected: both status checks are empty. Forge packages Tammy; source revision equals the Task 12 commit, `source_dirty` is false, source and packaged manifests are byte-identical, the external core matches `core_sha256`, and the manifest contains no secret or unsupported SBR claim.

### Task 13: Prove the packaged application works offline with Playwright

**Files:**

- Create: `apps/desktop/playwright.config.ts`
- Create: `apps/desktop/tests/e2e/fixtures.ts`
- Create: `apps/desktop/tests/e2e/foundation.spec.ts`
- Create: `apps/desktop/tests/e2e/process-check.ts`
- Modify: `apps/desktop/package.json`
- Modify: `package.json`

- [ ] **Step 1: Write the packaged E2E test before wiring its command**

The test imports the custom Electron fixture described in Step 3 and begins by asserting `test.info().project.name` equals the exact current target (`darwin-arm64` or `win32-x64`). The fixture locates the packaged executable through `find-packaged-app.mjs` and launches it with Playwright `_electron.launch({ executablePath, chromiumSandbox: true, offline: true, ...artifactOptions })` so network emulation is active before any window loads. The test then asserts:

1. the first window title is `Tammy`;
2. `Starting local engine` resolves to `Local engine ready`;
3. `Offline` and `No cloud required` are visible;
4. API/core versions render and no future module names render;
5. `Object.keys(window.tammy)` is exactly `["getSystemDiagnostics"]`;
6. renderer `process`, `require`, and Node globals are absent;
7. external HTTP/HTTPS fetch rejects under CSP while the main-owned Connect call still succeeds;
8. no unexpected renderer console error or page error occurs;
9. in the healthy state keyboard Tab order skips the disabled workspace control and never exposes the absent retry control, while the failure-state retry order remains covered by the renderer unit test; and
10. closing Electron exits within five seconds and leaves no matching bundled-core process.

Never inspect or print main-process readiness secrets. `process-check.ts` uses `pgrep` with the exact packaged core path on macOS. On Windows it calls `powershell.exe` with a fixed argument array and a fixed `Get-CimInstance Win32_Process` script that compares `ExecutablePath` to the exact canonical core path passed in `TAMMY_EXPECTED_CORE`; it parses only process ID and executable path. Image-name-only evidence is rejected.

- [ ] **Step 2: Run E2E to verify the missing configuration failure**

Run: `rtk mise exec -- pnpm --dir apps/desktop e2e`

Expected: FAIL at the first assertion because Playwright's default unnamed project does not equal the required target name. This remains deterministic even when Task 12 already produced a valid package.

- [ ] **Step 3: Configure one packaged-only Playwright project**

Use a 30-second test timeout, 10-second expect timeout, zero retries locally/two in CI, and output under `test-results/`. The project name is the exact current target. It must not launch the Vite development server.

Because the app is launched manually through `_electron`, implement the artifact policy in `fixtures.ts` rather than relying on Playwright's standard browser fixtures. Extend Playwright Test with an Electron fixture that launches with `offline: true`, `artifactsDir`, and `recordVideo.dir` under `testInfo.outputPath()`, and starts `electronApplication.context().tracing` only when `testInfo.retry === 1`. In fixture teardown, capture a page screenshot on failure, stop and attach the trace on a failed first retry, close Electron, save and attach video only on failure, delete video on success, and remove unretained raw artifact directories. Await every close/save/delete before checking for an orphan. Export this custom `test` and `expect`, and import only those in `foundation.spec.ts`.

- [ ] **Step 4: Add root packaged-E2E orchestration**

Add:

```json
{
  "desktop:e2e": "pnpm desktop:package && pnpm --dir apps/desktop e2e"
}
```

- [ ] **Step 5: Run the real packaged E2E**

Run: `rtk mise exec -- pnpm desktop:e2e`

Expected: PASS on the current supported target with networking emulated offline, no renderer escape, and no orphaned core.

- [ ] **Step 6: Repeat the process-death boundary three times**

Run: `rtk mise exec -- pnpm --dir apps/desktop e2e --repeat-each=3`

Expected: three clean launches and exits with no port collision, reused CA/capability, timeout, or orphan.

- [ ] **Step 7: Commit packaged E2E**

```bash
rtk git add apps/desktop/playwright.config.ts apps/desktop/tests/e2e apps/desktop/package.json package.json pnpm-lock.yaml
rtk git commit -m "test: verify the packaged offline desktop"
```

### Task 14: Add target-aware continuous integration

**Files:**

- Create: `.github/workflows/foundation-ci.yml`
- Create: `.github/workflows/foundation-windows11-e2e.yml`
- Modify: `package.json`

- [ ] **Step 1: Write the primary CI workflow**

Set `permissions: { contents: read }`, concurrency cancellation per ref, and 30-minute timeouts. Pin actions by commit:

```text
actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd
actions/setup-node@a0853c24544627f65ddf259abe73b1d18a591444
actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16
actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a
```

Jobs:

- `contracts` on `ubuntu-24.04`: Node 24.18.0, Go 1.26.4, frozen pnpm install, actionlint 1.7.12 workflow validation, Buf format/lint/breaking/generation cleanliness, Go race tests, `xvfb-run -a pnpm desktop:test` (after explicitly asserting `xvfb-run` is installed), and desktop type/lint tests;
- `macos14-arm64-packaged` on `macos-14`: assert `uname -m` is `arm64`, then run `pnpm desktop:e2e` and upload failure artifacts;
- `windows-server-x64-package-smoke` on `windows-2025`: assert AMD64, run
  `pnpm desktop:test`, `pnpm desktop:typecheck`, `pnpm core:build`,
  `pnpm build:manifest`, and `pnpm --dir apps/desktop make` so the native Windows x64
  `electron-winstaller` hook and MakerSquirrel path are exercised; label all evidence
  `WINDOWS_SERVER_SMOKE_ONLY`.

The Windows Server job uploads `apps/desktop/out/make/squirrel.windows/**` with the pinned
`actions/upload-artifact` SHA as
`WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64`, sets `retention-days: 30`, and sets
`if-no-files-found: error`. The job must fail if Forge produces no Squirrel maker artifact.
This retained output is Windows Server x64 smoke evidence only, not Windows 11 release-gate
evidence.

Every job uses the pinned `setup-node` and `setup-go` action SHAs above with `node-version: 24.18.0` and `go-version: 1.26.4`, then runs `corepack prepare pnpm@11.15.0 --activate`, `pnpm install --frozen-lockfile`, `pnpm check:toolchain`, and refuses a dirty generated tree. No package, E2E, or evidence job may use a runner-default Node or Go.
Every checkout uses `fetch-depth: 0` so `buf breaking --against .git#branch=master` can resolve the committed comparison branch.

Add root script `"ci:lint": "go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12"`. The version is immutable in the command and recorded in `compliance/build/toolchain.lock.json`; never use `@latest`.

- [ ] **Step 2: Add the real Windows 11 23H2 E2E workflow**

Use `workflow_dispatch` and a job conditioned on repository variable `TAMMY_WINDOWS11_RUNNER_ENABLED == 'true'`, with `runs-on: [self-hosted, windows, x64, tammy-win11-23h2]`. The first PowerShell step verifies the edition/build is Windows 11 23H2 or later and x86_64; then it runs the same frozen install and `pnpm desktop:e2e`. Upload its manifest/results as `windows11-x64-foundation-evidence`.

The Windows Server smoke job never satisfies the Windows 11 release gate. Until the self-hosted workflow passes, compliance status remains `NOT_YET_VERIFIED`.

- [ ] **Step 3: Validate workflow syntax and local command parity**

Run:

```bash
rtk mise exec -- pnpm lint
rtk mise exec -- pnpm ci:lint
rtk mise exec -- pnpm proto:format:check
rtk mise exec -- pnpm proto:breaking
rtk mise exec -- pnpm desktop:e2e
rtk git diff --check
```

Expected: local equivalents pass and the workflow files are ready to commit. There is no
local macOS equivalent for the Squirrel maker smoke; only the Task 14
`windows-server-x64-package-smoke` job may produce that evidence. External job results do
not exist yet and must remain recorded as `NOT_YET_VERIFIED`.

- [ ] **Step 4: Commit CI**

```bash
rtk git add .github/workflows package.json
rtk git commit -m "ci: verify the offline desktop foundation"
```

- [ ] **Step 5: Retain post-commit CI evidence when the branch is authorised for push**

After a later authorised push or pull request, require the contracts, macOS packaged, and
Windows Server smoke jobs to be green. Retain the macOS artifact identifiers and the
`WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64` Squirrel artifact identifier in the
traceability matrix. Windows Server remains smoke evidence only and never satisfies the
Windows 11 release gate. If no push is authorised during plan execution, leave every
external CI status `NOT_YET_VERIFIED`; never represent local validation as a GitHub-hosted
result.

### Task 15: Establish foundation traceability and developer operations

**Files:**

- Create: `compliance/traceability/foundation.csv`
- Create: `compliance/threat-model/local-api-foundation.md`
- Create: `docs/development/foundation.md`
- Create: `scripts/check-foundation-evidence.mjs`
- Create: `scripts/check-foundation-evidence.test.mjs`
- Modify: `package.json`

- [ ] **Step 1: Write the failing evidence validator tests**

Fixtures cover missing/duplicate requirement IDs, empty design/test/evidence cells, invalid status, a Windows Server result incorrectly marked as Windows 11 evidence, and a clean canonical matrix. Require at least:

```text
DESIGN-5.1, DESIGN-5.2, DESIGN-5.3, DESIGN-6, DESIGN-10, DESIGN-11.1, DESIGN-13.3, DESIGN-13.5
```

- [ ] **Step 2: Run evidence tests to verify they fail**

Run: `rtk mise exec -- node --test scripts/check-foundation-evidence.test.mjs`

Expected: FAIL because `validateFoundationEvidence` is undefined.

- [ ] **Step 3: Implement the dependency-free CSV validator**

Parse the committed fixed header:

```text
source_requirement_id,source_and_version,applicability,design_section,implementation_component,automated_test,retained_evidence,owner,status,dpo_confirmation_reference
```

Permit statuses only `IMPLEMENTED_VERIFIED`, `IMPLEMENTED_PARTIAL_TARGET`, `NOT_YET_VERIFIED`, `PLANNED`, and `NOT_APPLICABLE`. Fail closed on malformed quoting, duplicates, missing required rows or fields, or Windows target misclassification.

- [ ] **Step 4: Write the initial traceability matrix**

Map every foundation requirement to exact applicability, files, tests, CI artifact names, owner, DPO reference where required, and current status. macOS can become `IMPLEMENTED_VERIFIED` only after packaged E2E; Windows remains `NOT_YET_VERIFIED` until the Windows 11 workflow passes. State explicitly that no Activity Statement, credential, ATO transport, or approval claim exists yet.

- [ ] **Step 5: Document the local API threat model**

Cover assets, trust boundaries, attacker assumptions, loopback exposure, parent-pipe secrecy, TLS/capability rotation, duplicate header rejection, renderer compromise, IPC sender validation, CSP/custom protocol, child lifecycle, logs, residual risks, and the exact tests/controls mitigating each threat.

- [ ] **Step 6: Document deterministic development operations**

`docs/development/foundation.md` contains exact mise setup, generation, unit, development, package, E2E, troubleshooting, and clean-shutdown commands. Explain that the application runs offline, external CI is optional for local use, unsigned packages are test artifacts, and Windows Server smoke is not Windows 11 evidence.

- [ ] **Step 7: Add and run the evidence command**

Add root script `"compliance:foundation": "node scripts/check-foundation-evidence.mjs"` and run:

```bash
rtk mise exec -- node --test scripts/check-foundation-evidence.test.mjs
rtk mise exec -- pnpm compliance:foundation
```

Expected: validator tests PASS and the real matrix prints `foundation evidence ok`.

- [ ] **Step 8: Commit documentation and evidence**

```bash
rtk git add compliance/traceability compliance/threat-model docs/development scripts/check-foundation-evidence* package.json
rtk git commit -m "docs: trace the offline desktop foundation"
```

### Chunk 3 and plan completion verification

- [ ] **Step 1: Run the complete reproducibility gate**

```bash
rtk mise exec -- pnpm check:toolchain
rtk mise exec -- pnpm ci:lint
rtk mise exec -- pnpm proto:format:check
rtk mise exec -- pnpm proto:lint
rtk mise exec -- pnpm proto:breaking
rtk mise exec -- pnpm proto:generate
rtk git diff --exit-code -- services/core/internal/gen packages/connect-client/src/gen
rtk mise exec -- go vet ./services/core/...
rtk mise exec -- go test -race ./services/core/...
rtk mise exec -- pnpm desktop:test
rtk mise exec -- pnpm desktop:typecheck
rtk mise exec -- pnpm lint
rtk mise exec -- pnpm compliance:foundation
```

Expected: every command passes.

- [ ] **Step 2: Run packaged offline acceptance**

Run: `rtk mise exec -- pnpm desktop:e2e`

Expected: the packaged application passes all foundation assertions and exits without an orphan.

- [ ] **Step 3: Inspect the final evidence and worktree**

Run:

```bash
rtk git diff --check
rtk git status --short
rtk git log --oneline --decorate -15
```

Expected: no whitespace errors, clean worktree, and focused commits matching the tasks above.

- [ ] **Step 4: Record target limitations honestly**

If Windows 11 23H2 E2E has not run, leave its traceability status `NOT_YET_VERIFIED` and report it as the next external verification action. Do not describe the foundation, accounting, SBR integration, or product as production approved.
