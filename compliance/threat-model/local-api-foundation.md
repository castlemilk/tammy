# Local API foundation threat model

## Scope and evidence boundary

This threat model covers Tammy's implemented Electron foundation: the electron-vite main, preload, and app bundles; the fixed electron-conveyor boundary; the supervised Go child process and authenticated Connect transport; ordinary electron-builder directory packages; and the local SBR simulator and machine-credential file-intake boundary. It describes repository controls and the tests that exercise them.

The connected accounting and SBR-readiness surfaces do not establish a production tax product. Company EOFY preparation and company returns remain open. Production ATO/SBR lodgement, DSP registration, service conformance, and production approval remain open. Apple signing, upload, and App Review remain external gates. Hosted macOS and Windows 11 results are not inferred from local evidence.

## Assets

- The per-process capability token and ephemeral TLS material that authenticate the local Connect server.
- The integrity of the Electron main and preload code, app bundle, fixed Conveyor manifest, generated contracts, packaged ASAR, Go core, and SBR helper.
- The encrypted accounting workspace, catalogue, installation key, audit records, and retained local evidence.
- Machine-credential file selections, security-scoped bookmarks, and opaque machine-credential handles retained only in Electron main for one bounded use.
- The parent-to-child lifecycle relationship and exact-process shutdown evidence.

## Trust boundaries

| Boundary | Data crossing it | Security decision |
| --- | --- | --- |
| App to preload | Calls through `window.conveyor` for exactly system, workspace, organisation, accounting, banking, documents, reporting, and sbr | Treat app content as untrusted. Enforce a fixed method allowlist and exact arity in the preload. Expose no Node primitive and no streams or events. |
| Preload to Electron main | One allowlisted Conveyor channel and method plus validated input | Conveyor sender authorization accepts only the current live window's exact main frame at the approved app URL. Authorization precedes method lookup and validation. |
| Electron main to Go child | The known child executable, minimal environment, parent stdin, readiness stdout, and bounded lifecycle stderr | Main owns launch and exact-process shutdown. Readiness is parsed once and logs are bounded and redacted. |
| Electron main to loopback Connect server | TLS 1.3 HTTP/1.1 requests with a per-process capability header | Pin the child CA, require exactly one capability, use an ephemeral IPv4 loopback port, and apply bounded timeouts and body limits. |
| App to machine-credential file intake | A native chooser request followed by a single-use UUIDv7 handle | Keep the selected path and optional security-scoped bookmark in main-process memory. The app receives only opaque machine-credential handles with a five-minute lifetime. |
| Custom protocol to packaged app assets | A `tammy://app/` URL resolved inside `app.asar` | Require the expected ASAR root and bounded regular-file response. Reject traversal, aliases, directories, symbolic ambiguity, and malformed fetch results. |
| Builder input to packaged runtime | The electron-builder configuration, ASAR payload, native resources, and Electron executable | Require ASAR integrity fuses in configuration and perform fuse verification against the packaged executable before accepting package evidence. |
| Local application to external networks | No network is required for ordinary accounting or the simulator | Production CSP blocks app egress. The SBR simulator is network-disabled and cannot select EVTE or production endpoints. |

## Attacker assumptions

- App content can become hostile through a rendering defect or injected content.
- Another unprivileged local process can connect to loopback and guess ports but cannot read the parent pipe or process memory.
- Malformed, duplicated, oversized, trailing, expired, or replayed transport and handle input is hostile.
- Unsigned or ad-hoc development packages can be tampered with outside the checks performed for one observed artifact.
- An administrator, root process, same-account debugger, or malware able to inspect or alter process memory is outside the protection of these in-process controls.
- The public loopback interface and a renderer-provided string are never authentication boundaries.

## Threats, controls, and exact tests

| Threat | Implemented control | Automated evidence |
| --- | --- | --- |
| Renderer reaches an unknown main-process capability | The restricted bridge accepts only the committed eight-module manifest, fixed method allowlist, and exact zero-or-one-input arity. It exposes query/command invocation and a validated manifest with no streams or events. | `apps/desktop/src/preload/conveyor-bridge.test.ts`; `apps/desktop/src/conveyor/router.test.ts`; `apps/desktop/src/conveyor/no-legacy-ipc.test.ts` |
| Renderer forges a privileged sender | Conveyor sender authorization checks the registered live `webContents`, exact main frame identity, non-destroyed state, and approved application URL before context creation, method lookup, or schema validation. Rejected calls return only the fixed rejection code. | `apps/desktop/src/conveyor/guard.test.ts`; `apps/desktop/src/conveyor/registration.test.ts`; `apps/desktop/src/conveyor/upstream-contract.test.ts` |
| A second router replaces or leaks a privileged channel | The patched router owns each channel, refuses replacement, disables stream handlers, and disposes only its own registrations idempotently. Main registers it only while the supervised Core is ready. | `apps/desktop/src/conveyor/upstream-contract.test.ts`; `apps/desktop/src/conveyor/registration.test.ts`; `apps/desktop/src/main/index.test.ts` |
| App content supplies a raw machine-credential path | Electron's native chooser validates one absolute regular non-symlink file and retains the path in main-process memory. It returns a bounded UUIDv7 handle that expires after five minutes and is consumed once; bookmarks are copied and zeroed. | `apps/desktop/src/main/sbr-file-intake.test.ts`; `apps/desktop/src/conveyor/modules/sbr.test.ts`; `apps/desktop/src/preload/conveyor-bridge.test.ts` |
| A non-parent process discovers the Core port | Bind IPv4 loopback on an ephemeral port and require a random 32-byte capability in addition to pinned TLS. Emit the port, CA, and capability only through the supervised readiness pipe. | `services/core/internal/transport/server_integration_test.go`; `services/core/internal/transport/capability_test.go`; `services/core/internal/transport/readiness_test.go` |
| Parent-pipe input or logs disclose local credentials | Emit exactly one bounded readiness record; reject unknown fields, trailing data, and invalid values. Keep lifecycle logs structured and project only safe diagnostics to the app. | `services/core/internal/transport/readiness_test.go`; `services/core/cmd/tammy-core/main_test.go`; `apps/desktop/src/main/core-process.test.ts` |
| Capability ambiguity or request smuggling reaches Core | Require exactly one strict unpadded base64url capability representing 32 bytes, hash before constant-time comparison, enforce TLS 1.3, and bound headers, body, messages, and timeouts. | `services/core/internal/transport/capability_test.go`; `services/core/internal/transport/server_integration_test.go`; `apps/desktop/src/main/core-client.test.ts` |
| Packaged app code is loaded outside the authenticated archive | electron-builder enables embedded ASAR integrity validation and only-load-from-ASAR while disabling run-as-Node and Node-options injection. The package acceptance reads the actual executable fuse wire rather than trusting configuration alone. | `apps/desktop/scripts/electron-builder-config.test.mjs`; `apps/desktop/tests/e2e/package-signature.test.mjs` |
| Custom protocol escapes or confuses the ASAR boundary | The protocol owner authenticates the physical app archive, normalises only the approved app URL, bounds returned bytes, rejects aliases and malformed entries, and serves a strict content type and CSP. | `apps/desktop/src/main/security.test.ts`; `apps/desktop/src/main/security.asar-harness.test.ts`; `apps/desktop/tests/e2e/foundation.spec.ts` |
| Renderer exfiltrates data or opens a new capability | Apply a production CSP with `connect-src 'none'`; deny navigation, new windows, permissions, devices, and downloads except the two explicitly validated external support/privacy links. | `apps/desktop/src/main/security.test.ts`; `apps/desktop/tests/e2e/foundation.spec.ts`; `apps/desktop/tests/e2e/app-store-privacy.spec.ts` |
| Core or helper outlives Electron | Treat parent stdin EOF and process signals as shutdown, allow a bounded graceful close, and then terminate the exact owned process. Package E2E verifies no process remains at the authenticated executable path. | `services/core/internal/transport/server_lifecycle_test.go`; `apps/desktop/src/main/core-process.test.ts`; `apps/desktop/tests/e2e/process-check.test.ts`; `apps/desktop/tests/e2e/foundation.spec.ts` |

## Residual risks and required follow-up

- A same-user debugger, administrator, root process, or endpoint compromise can inspect or alter process memory and defeat local handle, capability, and TLS controls.
- Local machine-credential lifecycle and a network-disabled simulator do not prove an approved credential component, DSP registration, EVTE access, service conformance, or production ATO transport.
- The company EOFY and company-return workflows are not complete. No migration or package result changes that product boundary.
- Local darwin-arm64 package evidence does not establish hosted macOS or Windows 11 results. The Windows Server job remains smoke-only.
- Unsigned and ad-hoc packages are development artifacts. Apple signing identities and profiles, upload, processing, and App Review have not been observed and remain release gates.

The traceability status and retained-evidence classification for these controls is recorded in `compliance/traceability/foundation.csv`.
