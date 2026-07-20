# Local API foundation threat model

## Scope and evidence boundary

This threat model covers the implemented offline desktop foundation: an Electron renderer and main process, a supervised Go child process, and the generated Connect protocol used for the system-diagnostics call. It describes controls that exist in this repository and the tests that exercise them.

It does not claim implementation or approval of accounting workflows, Activity Statements, credential custody, ATO transport, SBR/EVTE/DPO/OSF conformance, signed production packages, or production readiness. The DPO confirmations listed as open in Section 3 of the design remain open. Hosted CI, Windows 11, and Windows Server results are not local evidence.

## Assets

- The per-process capability token that authorises the local Connect call.
- The ephemeral TLS private key and the emitted CA certificate used to pin the child process.
- The integrity of the desktop application, preload bridge, generated contract, and Go core binary.
- The parent-to-child lifecycle relationship and the single readiness record.
- The diagnostics response and lifecycle logs. The foundation does not yet persist accounting or credential data.

## Trust boundaries

| Boundary | Data crossing it | Security decision |
| --- | --- | --- |
| Renderer to preload | One typed `getSystemDiagnostics()` invocation and a structured result | Treat renderer content as untrusted; expose no generic IPC or Node primitive. |
| Preload to Electron main | A fixed IPC channel and the invoking frame identity | Accept only the expected live `webContents`, exact main frame, and `tammy://app/` URL. |
| Electron main to Go child | The child executable, minimal environment, parent stdin, readiness stdout, and lifecycle stderr | Main owns process launch and shutdown; readiness is bounded and parsed once; logs are projected and redacted. |
| Electron main to loopback Connect server | TLS 1.3 HTTP/1.1 request with a capability header | Pin the child CA, require the capability, use an ephemeral loopback port, and apply timeouts. |
| Custom protocol to packaged assets | A `tammy://app/` URL and a file response | Resolve inside the packaged renderer root; reject traversal, symbolic links, non-files, and oversized assets. |
| Local application to external networks | No foundation traffic is required | Production CSP sets `connect-src 'none'`; cloud interaction is not needed for startup or diagnostics. |

## Attacker assumptions

- Renderer content can become hostile through a future rendering bug or injected content.
- Another unprivileged local process can connect to loopback and can guess ports, but cannot read the parent pipe or process memory.
- Malformed, duplicated, oversized, or trailing readiness and HTTP header input is hostile.
- Packaged files can be tampered with when the application is unsigned; this is a residual development-build risk.
- An administrator, root process, same-account debugger, or malware able to inspect or modify process memory is outside the protection offered by the capability and TLS controls.
- The public loopback interface is not an authentication boundary.

## Threats, controls, and exact tests

| Threat | Implemented control | Automated evidence |
| --- | --- | --- |
| A non-parent process discovers the loopback port | Bind IPv4 loopback on an ephemeral port; require a random 32-byte capability in addition to TLS. The readiness record carrying the port, CA, and capability is written only to the supervised parent pipe. | `services/core/internal/transport/server_integration_test.go`; `services/core/internal/transport/capability_test.go`; `services/core/internal/transport/readiness_test.go` |
| Parent-pipe secret disclosure | Emit exactly one bounded readiness JSON record; reject unknown fields, trailing data, invalid port/CA/capability values, and records over 64 KiB. Do not place secrets in lifecycle logs. | `services/core/internal/transport/readiness_test.go`; `services/core/cmd/tammy-core/main_test.go`; `apps/desktop/src/main/core-process.test.ts` |
| Reuse or expiry of local API credentials | Generate a new ECDSA P-256 CA, leaf certificate, and random capability for each core process; keep private keys and the capability in process memory only and never persist or reuse them. The certificate's 100-year temporal validity spans any practical live process without rotation, while the identity still dies with the supervised core process. The server permits TLS 1.3 only, and the Electron client pins the emitted CA. | `services/core/internal/transport/server_integration_test.go`; `apps/desktop/src/main/core-client.test.ts`; `apps/desktop/src/main/core-client.integration.test.ts` |
| Capability ambiguity or header smuggling | Read all `X-Tammy-Capability` values and require exactly one strict unpadded base64url value representing 32 bytes; hash before constant-time comparison. | `services/core/internal/transport/capability_test.go`; `services/core/internal/transport/server_integration_test.go` |
| Plain HTTP or remote-interface access | Listen on `127.0.0.1` only and serve TLS 1.3 only. The client constructs an `https://127.0.0.1:<port>` origin. | `services/core/internal/transport/server_integration_test.go`; `apps/desktop/src/main/core-client.test.ts` |
| Slow or oversized local requests | Configure a 2-second header timeout, 5-second whole-request read timeout, 5-second response write timeout, 30-second idle timeout, and 16 KiB maximum HTTP header size. Keep Connect's request-message size limit in force. The client request timeout is 5 seconds. | `services/core/internal/transport/server_integration_test.go` exercises partial TLS handshakes, incomplete headers, and incomplete bodies with missing and valid capabilities; `apps/desktop/src/main/core-client.test.ts` |
| Renderer compromise reaches Node or arbitrary local IPC | Enable sandboxing and context isolation; disable Node integration; expose one frozen typed preload method; provide no generic invoke or transport bridge. | `apps/desktop/src/main/security.test.ts`; `apps/desktop/src/main/ipc.test.ts`; `apps/desktop/tests/e2e/foundation.spec.ts` |
| Renderer forges a privileged IPC sender | Validate the exact expected `webContents`, the exact main-frame object, live state, and the `tammy://app/` application URL before serving the call. | `apps/desktop/src/main/ipc.test.ts`; `apps/desktop/src/main/index.test.ts` |
| Renderer exfiltrates data over the network | Apply a production CSP with `connect-src 'none'`, deny navigation, new windows, permissions, device access, and downloads. | `apps/desktop/src/main/security.test.ts`; `apps/desktop/tests/e2e/foundation.spec.ts` |
| Custom protocol escapes the renderer asset root | Canonicalise the requested path and require a regular non-symbolic-link file within the renderer root with a 16 MiB limit. | `apps/desktop/src/main/security.test.ts` |
| Child binary or environment injection | Spawn a known absolute binary with `shell: false`, no caller-supplied arguments, and a minimal allowlisted environment. | `apps/desktop/src/main/core-process.test.ts`; `apps/desktop/src/main/index.test.ts` |
| Child outlives its desktop parent | Treat parent stdin EOF and process signals as shutdown; allow a bounded graceful close and then terminate; verify the packaged process path has no orphan after exit. | `services/core/internal/transport/server_lifecycle_test.go`; `services/core/cmd/tammy-core/main_test.go`; `apps/desktop/src/main/core-process.test.ts`; `apps/desktop/src/main/process-check.test.ts`; `apps/desktop/tests/e2e/foundation.spec.ts` |
| Logs expose readiness credentials | Keep Go lifecycle logging to structured component, event, and level fields; bound and redact child stderr before logging in Electron; return only safe diagnostics projections to the renderer. | `services/core/cmd/tammy-core/main_test.go`; `apps/desktop/src/main/core-process.test.ts`; `apps/desktop/src/main/core-client.test.ts` |

## Residual risks and required follow-up

- A same-user debugger, administrator, root process, or endpoint compromise can inspect or modify process memory and defeat the local capability model. Platform hardening and signed distribution are future release work.
- The parent pipe is confidential only to the extent provided by OS process isolation. It is deliberately not placed in command-line arguments, shared files, renderer state, or logs.
- The development server needs a development-only CSP allowance for its local tooling. Production packages use the stricter custom-protocol CSP and must remain the release evidence path.
- The foundation has no encrypted accounting workspace, machine credential store, Activity Statement engine, ATO transport, or SBR submission implementation. Those features require separate threat-model expansion and the still-open DPO decisions.
- Local darwin-arm64 packaged E2E demonstrates only the current offline shell. Hosted macOS evidence is not produced. The Windows Server workflow is smoke-only and is not Windows 11 evidence. Windows 11 evidence is not produced.
- Unsigned packages are development artifacts. Code signing, notarisation, installer trust, update integrity, recovery, and external conformance evidence remain release gates.

The traceability status and retained-evidence classification for these controls is recorded in `compliance/traceability/foundation.csv`.
