# Local Accounting and SBR Registration Readiness Design

- **Date:** 2026-08-21
- **Status:** Approved for planning
- **Initial target:** macOS 14+, Apple silicon
- **SBR delivery stage:** simulator and registration evidence first; EVTE when ATO inputs are issued; production disabled

## 1. Outcome

Tammy will have clear Taskfile scenarios for launching the current complete local accounting walkthrough, creating or unlocking an encrypted workspace, signing in as a named application user, and exercising every accounting workflow currently implemented. The same front door will expose a registration-ready Standard Business Reporting (SBR) development profile without presenting simulator or incomplete EVTE work as an ATO lodgement.

Tammy will support the lifecycle of a business-owned, RAM-issued machine credential through an isolated local SBR helper. The credential never leaves the client machine, is never passed through Task arguments or environment variables, and is never stored in the repository. Direct ATO production access remains impossible until Tammy has completed DSP and product registration, service-specific conformance, security evidence, and production authorisation.

“Complete local accounting walkthrough” means the current product surface: workspace setup/recovery confirmation, lock/unlock, application-user sign-in, organisation creation, chart of accounts, manual journals, trial balance, bank statement import/matching/reconciliation, document ingestion/review, BAS workpaper preparation, overview, and the chronological activity projection. It does not claim that every future accounting or BAS backlog item is implemented.

## 2. Regulatory and product boundary

Current ATO guidance says that desktop or locally hosted SBR software users create and install their own machine credential through RAM. The principal authority or Machine Credential Administrator is responsible for its use and safeguarding. A client credential must not be uploaded to a vendor cloud. Machine credentials authenticate SBR transactions through the Machine Authentication Service and Secure Token Service, and DSPs test compatibility in SBR EVTE before production use.

Therefore the first deliverable has three explicit states:

1. **Accounting only:** always available offline after normal local authentication.
2. **Simulator:** deterministic, visibly non-ATO, test-signed, and unable to select EVTE or production endpoints.
3. **EVTE-ready:** installed but unavailable until the ATO-approved credential component, Tammy DSP/product registration identifiers, EVTE service artefacts, and compatible test credential are supplied.

`PRODUCTION` is not an enum value, endpoint profile, adapter, task, UI state, or compiled code path in this scope. Editable repository evidence cannot enable it. Production requires a later design, externally verifiable ATO authorisation, and a new reviewed implementation.

A RAM credential owned by the developer is useful for credential-lifecycle work, but it is not Tammy DSP registration, product approval, service conformance, or permission to submit a BAS.

## 3. Chosen architecture

### 3.1 Scenario front door

Root Task aliases remain thin scenario entry points. They delegate to existing pnpm, Go, packaging, and evidence owners instead of duplicating their command sequences.

| Task | Behaviour |
|---|---|
| `task dev:accounting` | Checks the supported host/toolchain, builds required native resources, and launches the ordinary persistent development app. Setup and sign-in occur through the real UI. |
| `task dev:accounting:fresh` | Creates a new isolated Electron user-data root and launches the same app without deleting or modifying the persistent development workspace. It prints the retained root after exit. |
| `task dev:sbr:simulator` | Launches an isolated test-signed profile whose authenticated SBR profile selects `SIMULATOR`; it displays `SIMULATOR — NOT FOR ATO LODGMENT` throughout the app. |
| `task dev:sbr:evte` | Runs registration and component checks, then launches an `EVTE` profile. Missing or incompatible evidence produces a stable error before app launch. |
| `task sbr:doctor` | Performs read-only, redacted checks of platform, helper integrity, component compatibility, build profile, registration metadata, endpoint artefacts, and credential capability/expiry. |
| `task sbr:registration:check` | Validates the repository-owned registration evidence schema and reports every external item still required. It accepts no secrets. |
| `task test:sbr` | Runs helper protocol, credential lifecycle, simulator parity, transport-state, and security tests. |
| `task evidence:sbr` | Produces a deterministic redacted evidence bundle for DSP registration/conformance review. It excludes credentials, passwords, raw tax identifiers, all Product IDs, and workspace values. |

Every public `dev:sbr:*`, `sbr:*`, and SBR packaging/evidence task has an explicit host precondition and fails with `UNSUPPORTED_SBR_TARGET:<platform>/<arch>` unless the host is `darwin/arm64`. Ordinary accounting tasks retain the repository's existing `darwin/arm64` and `win32/x64` support. Tasks must not accept a credential password, credential bytes, private-key path, Product ID, or secret token. Credential import, replacement, unlock, and removal are explicit user actions in the app.

### 3.2 Process boundary

```text
React renderer
  └─ generated, allow-listed IPC commands (no private key material)
       └─ Electron main
            └─ authenticated local core
                 └─ narrow stdio protocol
                      └─ isolated sbr-helper
                           ├─ local credential vault / approved ATO component
                           ├─ MAS-ST / STS
                           └─ simulator or EVTE SBR endpoint profile
```

The existing Go core remains the accounting and durable workflow authority. A new `services/sbr-helper` process owns credential access and, once registered, EVTE transport. It is launched only for credential management, readiness/connectivity checks, or synthetic protocol tests in this slice. It has no database path or unrestricted application capability. The current BAS workpaper remains preparation-only and cannot be passed to the helper.

The core and helper use a versioned, length-bounded local protocol over inherited stdio. SBR uses a separate RFC 8785 canonical JSON `sbr-profile-v1.json` and detached Ed25519 signature. The core embeds only public verification keys: a repository test key that can sign `SIMULATOR` profiles and an EVTE release key whose private half is held outside the repository. The signed fields are `schema_version`, `environment` (`SIMULATOR` or `EVTE` only), `target`, helper SHA-256, component-manifest SHA-256 or `NONE`, registration-manifest SHA-256 or `NONE`, endpoint-profile SHA-256 or `NONE`, `issued_at`, and `expires_at`.

`sbr-component-v1.json` contains exactly `schema_version: 1`, component name/version/target, and a sorted list of normalized relative bundle paths with byte length and SHA-256. The registration manifest's `component_manifest_sha256` and the signed profile's component-manifest hash must equal the hash of those exact manifest bytes. The endpoint profile is a single canonical JSON document whose hash must likewise match both the registration manifest and signed profile.

Before credential access, the core opens the profile, signature, helper, component manifest, every declared component file, registration manifest, and endpoint profile with no-follow semantics; requires regular files owned by the current user and not group/world writable; hashes their bytes; verifies the detached profile signature and exact cross-manifest hashes; and rejects expired or wrong-target profiles. It copies the helper and component files from those retained descriptors into a newly created `0700` per-launch directory using exclusive creation, rejects links and undeclared paths, makes files read-only/executable as declared, and re-verifies the staged hashes. It passes endpoint-profile bytes over the inherited protocol; the helper never reopens an endpoint path. It then launches the exact staged helper path and gives it only the exact staged component root. A packaged EVTE helper must additionally pass macOS static-code validation with the same Team ID as the parent plus the pinned helper identifier `com.tammy.desktop.sbr-helper` and helper-specific designated requirement compiled into the core. Simulator profiles cannot name a component, registration manifest, or endpoint file, and the helper starts under an injected network dialer that always returns `SBR_SIMULATOR_NETWORK_FORBIDDEN`; packaged E2E also proves zero socket creation. The existing unsigned general build manifest is provenance only and is not treated as an SBR trust root.

### 3.3 Credential lifecycle

The UI exposes import, inspect, unlock-for-use, replace, and remove operations. Import uses a native file chooser, then gives the helper the selected local path through a single bounded request. Electron and ordinary Go code do not parse or retain credential bytes. The helper validates the original format through the approved ATO component and preserves the original format required by that component.

Every vault record is namespaced by an installation ID plus an opaque scope derived as `HMAC(installation_key, workspace_id || organisation_id || canonical_ABN)`. The workspace stores only the opaque scope and redacted credential metadata. Import requires `workspace_admin` with a fresh high-risk TOTP assertion. Inspect requires `workspace_admin` or `business_lodger`; replace and remove require `workspace_admin` plus fresh TOTP. Any future credential use requires `business_lodger`, fresh TOTP, current independent organisation verification, a credential ABN matching that verification, and the eventual declared-report gate. The helper rejects a different workspace, organisation, or ABN with `SBR_CREDENTIAL_ORGANISATION_MISMATCH` before key unlock or network access.

The local vault:

- lives outside the encrypted accounting workspace and outside backups;
- is readable only by the current OS account and SBR helper;
- requires macOS Keychain, or a secure store explicitly accepted in writing for the approved ATO component, for the installation key and vault-wrapping secret;
- stores metadata required to show organisation identity, issuer, serial/fingerprint, creation, expiry, component version, and state without returning private material;
- never stores the credential password in configuration, workspace data, logs, evidence, crash reports, Task variables, or the process command line;
- zeroes password and decrypted-key buffers after each bounded use;
- audits import, unlock, use, failure, expiry, replacement, removal, and suspected compromise without recording credential bytes or secrets; and
- requires reimport after restore or migration to another machine.

There is no plaintext or application-managed-key fallback. If the approved secure store is unavailable, import and use fail with `SBR_SECURE_STORE_UNAVAILABLE` while accounting remains available. Until the approved ATO credential component and licence are obtained, production code implements the helper protocol, vault boundary, metadata model, and deterministic synthetic credential adapter only. The real adapter is enabled by the authenticated EVTE profile and component manifest, never by a loose development flag.

ATO Product IDs are environment- and service-scoped secrets. They are imported through an explicit helper-owned operation into a distinct Keychain item namespaced by installation, `EVTE`, product, and service. Raw Product IDs are prohibited from repository files, Task variables, environment variables, argv, workspace data, logs, crash reports, renderer responses, and evidence bundles. Doctor returns only `PRESENT`, `MISSING`, or `INACCESSIBLE` plus a non-reversible fingerprint.

### 3.4 Registration and endpoint evidence

The exact `sbr-registration-v1.json` manifest contains only these fields:

- `schema_version: 1`, `environment: "EVTE"`, and `target: "darwin/arm64"`;
- `product_id_scope` with the exact non-secret product namespace identifier and enrolled service ID used to address the separately imported Product ID; the secret Product ID value is never present in the manifest;
- `dsp_registration` and `product_registration`, each with `state: NOT_STARTED | SUBMITTED | APPROVED`, an external reference, decision date, and optional expiry;
- `osf_assessment` with `category`, `state: NOT_STARTED | IN_REVIEW | APPROVED`, external reference, decision date, and revalidation date;
- `component` with name, version, `component_manifest_sha256`, `licence_state: NOT_OBTAINED | REVIEW_REQUIRED | APPROVED`, and target;
- one or more `services`, each with exact service ID, taxonomy/release version, artefact SHA-256 values, `enrolment_state: NOT_STARTED | SUBMITTED | APPROVED`, and `conformance_state: NOT_STARTED | RUNNING | PASSED`;
- `evte_access` with `state: NOT_REQUESTED | REQUESTED | APPROVED`, external reference, issued date, and expiry;
- `endpoint_profile` with ID, revision, `endpoint_profile_sha256`, issued date, and expiry; and
- `review` with reviewer identity, approval timestamp, and revalidation date.

The manifest is canonical JSON with a detached Ed25519 signature under the EVTE release trust root described above. Pre-conformance `dev:sbr:evte` launch requires: both registrations `APPROVED`; OSF `APPROVED`; component licence `APPROVED`; current component target and cross-manifest hashes; EVTE access `APPROVED` and unexpired; a current endpoint profile; at least one service whose enrolment is `APPROVED` and conformance is `NOT_STARTED`, `RUNNING`, or `PASSED`; a present and accessible service-scoped Product ID; and all decision/revalidation dates current. `task evidence:sbr` may run approved-enrolment conformance cases in EVTE and records `RUNNING` evidence without mutating the signed input manifest. A service can be reported as post-conformance-ready only when a newly reviewed and signed manifest records `PASSED`; the current product still does not consume that service for BAS submission. Repository examples use only non-approved placeholder states and cannot satisfy the gate.

Stable readiness precedence is: `UNSUPPORTED_SBR_TARGET`, `SBR_PROFILE_MISSING`, `SBR_PROFILE_INVALID`, `SBR_PROFILE_UNTRUSTED`, `SBR_PROFILE_EXPIRED`, `SBR_HELPER_UNTRUSTED`, `SBR_COMPONENT_MISSING`, `SBR_COMPONENT_UNTRUSTED`, `SBR_COMPONENT_LICENCE_NOT_APPROVED`, `SBR_REGISTRATION_MANIFEST_MISSING`, `SBR_REGISTRATION_MANIFEST_INVALID`, `SBR_REGISTRATION_MANIFEST_UNTRUSTED`, `SBR_REGISTRATION_MANIFEST_EXPIRED`, `SBR_DSP_REGISTRATION_NOT_APPROVED`, `SBR_PRODUCT_REGISTRATION_NOT_APPROVED`, `SBR_OSF_ASSESSMENT_NOT_APPROVED`, `SBR_EVTE_ACCESS_NOT_APPROVED`, `SBR_ENDPOINT_PROFILE_MISSING`, `SBR_ENDPOINT_PROFILE_UNTRUSTED`, `SBR_ENDPOINT_PROFILE_EXPIRED`, `SBR_SERVICE_ENROLMENT_NOT_APPROVED`, `SBR_PRODUCT_ID_MISSING`, `SBR_PRODUCT_ID_INACCESSIBLE`, `SBR_SECURE_STORE_UNAVAILABLE`, `SBR_CREDENTIAL_MISSING`, `SBR_CREDENTIAL_INCOMPATIBLE`, `SBR_CREDENTIAL_REVOKED`, `SBR_CREDENTIAL_EXPIRED`, and `SBR_CREDENTIAL_ORGANISATION_MISMATCH`. Post-conformance status additionally uses `SBR_SERVICE_CONFORMANCE_NOT_PASSED`. `sbr:registration:check` reports all independent missing items; launch and credential operations return the first applicable code in this order before side effects. No manifest or task in this scope represents production.

## 4. Application flow

1. The developer runs `mise install`, `mise exec -- task setup`, then `mise exec -- task dev:accounting` or a specific SBR scenario.
2. Tammy launches the real local core and shows setup or unlock according to the selected data root.
3. The user creates or unlocks the workspace and signs in with a named application account. Taskfiles never seed or expose a plaintext login.
4. Ordinary accounting and the current chronological activity projection continue regardless of SBR helper or credential availability.
5. In SBR settings, an authorised workspace user selects a RAM credential. The helper validates and imports it locally, binds it to the exact workspace organisation, and returns only redacted metadata.
6. The doctor checks credential state and the authenticated environment profile. It performs no submission.
7. The simulator exercises synthetic helper protocol and transport-state fixtures only. It cannot consume the current BAS draft and adds no lodge, declaration, submit, or receipt control to the product UI.
8. EVTE connectivity and service-specific synthetic conformance fixtures become available only after the exact registration, component, artefact, Product ID, credential, and service gates pass. Connecting the real BAS workpaper requires a later complete-BAS/declaration design.
9. Production remains structurally absent.

### 4.1 Deterministic simulator contract

The simulator accepts only the canonical `SIM-SBR-READINESS-V1` fixture: fixed organisation `Wattle & Co Test Pty Ltd`, simulator-only ABN `11 000 000 560`, service `SIM.READINESS.0001`, payload hash over the retained canonical fixture bytes, clock `2026-06-30T00:00:00Z`, message ID `SIM.MSG.0001`, conversation ID `SIM.CONV.0001`, and receipt `SIM-READY-0001`. It never reads a live BAS workpaper or user accounting values.

The first call persists `PREPARED`, then the deterministic accepted response and receipt. Repeating the exact idempotency key returns the owned original result without a second dispatch record; the same key with different bytes returns `IDEMPOTENCY_CONFLICT`. Named fixtures cover `NOT_STARTED`, `MAYBE_SENT`, malformed response, helper death, and timeout. Restart converts orphaned `DISPATCHING` to `UNKNOWN` and never resends. The simulator process is created with no network entitlement/capability, receives an injected dialer that always fails, and the packaged test observes zero listening or connected sockets for the helper.

## 5. Failure and security behaviour

- Accounting startup succeeds when SBR is absent, expired, revoked, unregistered, or offline.
- A missing helper, component, registration artefact, Product ID, secure store, or incompatible credential fails with the ordered stable readiness code above and remediation text; no network request starts.
- Import never overwrites an existing credential. Replacement is staged, validated, atomically swapped, and auditable.
- Credential removal is explicit and confirms that direct SBR will become unavailable; it does not affect accounting data.
- Helper stderr and application logs are structured and redacted. Unknown helper text is not relayed to the renderer.
- EVTE endpoint allow-lists and certificate requirements are pinned by the authenticated profile. Custom endpoint entry is unsupported.
- A transport result distinguishes `NOT_STARTED`, `MAYBE_SENT`, and `RESPONSE_RECEIVED`. Unknown outcomes are never automatically retried.
- Simulator identifiers and fixtures are rejected by EVTE. There is no production profile.
- Neither Taskfiles nor diagnostic/evidence bundles read arbitrary user credential directories or print keychain contents.

## 6. Testing and evidence

Implementation follows red-green TDD and adds these layers:

1. **Taskfile contract tests:** exact public aliases, ordered delegation, `darwin/arm64` SBR guards with the exact unsupported-target code, no credential/Product-ID/secret arguments, isolated fresh roots, and fail-closed EVTE prerequisites.
2. **Helper unit tests:** bounded protocol, no-follow path validation, profile signature/hash/expiry verification, macOS code-identity policy, vault namespacing and permissions, Keychain failure, Product-ID isolation, metadata redaction, password zeroing boundary, cross-workspace/ABN rejection, import/replace/remove state machine, and endpoint-profile rejection.
3. **Core contract tests:** exact role/TOTP/organisation gates, helper binary authentication, synthetic simulator/EVTE adapter parity, durable synthetic transmission states, idempotency, and unknown-outcome recovery.
4. **SQLCipher integration tests:** credential metadata references and audit events without private material; backup/restore excludes the vault and requires reimport.
5. **Desktop tests:** setup/login remains real, SBR status is fail-closed, native import is main/helper mediated, and renderer-facing objects contain no private material or unrestricted paths.
6. **Packaged E2E:** an isolated macOS package creates a user, signs in, completes the current accounting walkthrough, imports a synthetic credential, runs doctor, executes the canonical synthetic readiness fixture through the simulator test surface, records the deterministic receipt as test evidence rather than a BAS outcome, restarts/unlocks, proves the credential is workspace-bound, observes zero helper sockets, and proves clean helper/core exit.
7. **EVTE evidence:** once external inputs exist, service-specific tests run only through `task evidence:sbr`; results are retained as external-test evidence and never relabelled production.

The full repository test, typecheck, contract, native SQLCipher, changed-file formatting, clean-tree, and packaged E2E gates remain required in proportion to the files changed.

## 7. Delivery sequence

1. Add Taskfile scenarios and registration manifest/checker without changing the current product boundary.
2. Add the helper protocol, binary authentication, simulator adapter, and packaged lifecycle.
3. Add the secure local credential vault and synthetic credential lifecycle.
4. Add application settings, exact roles/TOTP/readiness projections, redacted doctor, and chronological activity/audit-chain evidence.
5. Add durable synthetic SBR readiness orchestration and the deterministic simulator contract without connecting the current BAS draft.
6. Integrate the approved ATO credential component and EVTE artefacts after they are issued; run conformance and registration evidence.
7. Design and approve production enablement separately after external authorisation.

This sequence keeps each increment useful and testable while avoiding a false claim that a RAM credential alone enables ATO lodgement.

## 8. Explicit non-goals

- Uploading a client machine credential to Tammy or any vendor cloud.
- Passing credentials or passwords through Taskfiles, environment variables, URLs, logs, or process arguments.
- Any production ATO profile, endpoint, adapter, task, control, or submission code.
- An iOS machine-credential implementation in this slice.
- Windows credential/helper support in the first slice.
- Completing every future accounting, payroll, tax, or individual-return backlog item.
- Connecting the current partial BAS workpaper to SBR, or adding declaration, lodgement, amendment, manual-fallback, or official-receipt UI in this slice.
- Scraping myGov, myTax, RAM, Access Manager, or ATO web sessions.

## 9. Current official references

- [Using Digital ID, RAM and machine credentials](https://softwaredevelopers.ato.gov.au/usingdigitalidramandmachinecredentials)
- [Machine to Machine authentication solution](https://softwaredevelopers.ato.gov.au/M2M)
- [DSP conditions of use](https://softwaredevelopers.ato.gov.au/usingourservices/dsp-conditions-use)
- [Standard Business Reporting](https://softwaredevelopers.ato.gov.au/sbr)
- [SBR software development steps](https://www.sbr.gov.au/digital-service-providers/software-development-steps)
- [Businesses and organisations online services](https://www.ato.gov.au/online-services/businesses-and-organisations-online-services)

These requirements and artefacts are versioned external dependencies and must be revalidated before EVTE integration and every production release.
