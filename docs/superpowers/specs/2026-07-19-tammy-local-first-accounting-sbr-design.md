# Tammy Local-First Accounting and Direct SBR Design

**Status:** Approved in design review  
**Date:** 19 July 2026  
**Initial certification target:** ATO Activity Statements AS.0004 (2025), single-record interactions  
**Product control model:** Client-controlled desktop software  
**Expected OSF classification:** Category D, subject to written DPO confirmation

## 1. Purpose

Tammy is a commercial, local-first desktop accounting product for Australian businesses and authorised accounting professionals. It must remain useful without internet access or a vendor-operated cloud. When a user explicitly initiates an ATO interaction, the desktop product may connect directly to SBR using the client organisation's own RAM machine credential.

This design covers the first executable vertical slice:

1. create and unlock an encrypted local workspace;
2. configure a business and unique local user;
3. create and use a chart of accounts;
4. post the canonical balanced, GST-bearing sale and purchase journals;
5. inspect the journal, general ledger, and trial balance;
6. derive a BAS workpaper;
7. perform local validation;
8. capture the applicable declaration;
9. submit through a simulated EVTE transport;
10. retain the response and complete audit evidence; and
11. restart the application and prove that state persists.

This slice establishes the architecture used by later sales, purchases, banking, reconciliation, payroll, agent-practice, and additional SBR modules. Those later workflows require their own focused design specifications before implementation.

## 2. Product boundaries

### 2.1 Included

- Cross-platform Electron desktop shell, initially packaged for macOS and Windows.
- React renderer using Tailwind CSS and shadcn/ui.
- A bundled Go core service using Connect-Go.
- Protobuf contracts governed by Buf and consumed through Connect-ES v2 or later.
- Local encrypted accounting storage and encrypted portable backups.
- Unique local users, permissions, session controls, and audit history.
- Exactly one business organisation per Milestone 1 workspace; roles and accounting data are scoped to that workspace organisation.
- Double-entry journals, ledger views, trial balance, GST classifications, and BAS workpapers.
- Versioned ATO artefacts and a service-adapter boundary for `LDG.List`, `AS.Get`, `AS.Validate`, and `AS.Submit`.
- Direct desktop SBR using the client's machine credential once current ATO developer material and EVTE access are available.
- Compliance documents and retained test evidence needed to support DPO, OSF, EVTE, conformance, PVT, and whitelisting activities.

### 2.2 Explicitly excluded from the first slice

- Vendor-hosted accounting storage, identity, synchronisation, or submission relay.
- CAA and Software Subscription IDs. These apply to a future DSP-controlled cloud relay, not the selected direct desktop path.
- Automatic upload of a client's machine credential to any cloud service.
- Direct individual taxpayer income-tax-return self-lodgment.
- myTax interaction or browser automation.
- Batch or bulk Activity Statement submission.
- Blind retry of any submission with an ambiguous outcome.
- Bank feeds, electronic invoicing, payroll, inventory, and multi-currency transactions.
- Multi-organisation agent-practice workspaces. They require a later tenancy and client-switching specification.
- Production claims such as "ATO approved", "ATO accredited", or "ATO endorsed".

### 2.3 External interactions

Core accounting, reporting, local validation, evidence review, audit review, backup, and restore must work without internet access.

External interactions are separate, explicit adapters:

- direct ATO SBR or EVTE submission;
- optional bank feeds;
- optional software update checks;
- optional encrypted backup or synchronisation added under a later design; and
- a possible future CAA cloud relay certified as a separate operating mode.

Disabling or losing any adapter must not prevent local accounting work.

### 2.4 Initial platform matrix

Milestone 1 supports these exact targets:

| Operating system | Architecture | Minimum version | Packaging |
|---|---|---|---|
| macOS | Apple silicon `arm64` | macOS 14 Sonoma | Signed and notarised DMG |
| Windows | `x86_64` | Windows 11 23H2 | Signed per-user installer |

Linux, Intel macOS, Windows 10, and Windows on ARM are outside Milestone 1. Adding a target requires packaged E2E, key-store, encryption, backup/restore, and native-library tests on that target. Direct SBR support on either initial target also depends on the current ATO credential component supporting that operating system and architecture; Milestone 1 uses the simulator and makes no claim about native ATO component compatibility.

## 3. Regulatory position

Current ATO guidance identifies desktop and locally hosted products as client-controlled products. Commercial client-controlled software consuming low-, medium-, or high-risk APIs ordinarily falls into OSF Category D. Category D requires audit logging, unique user-based access, a less-than-24-hour remembered session, an applicable security self-assessment, TLS 1.3 for in-scope data in transit, entity validation, supply-chain disclosure, and other controls where the DSP controls the implementation.

ATO and RAM guidance also states:

- desktop or locally hosted software users create and install their own machine credential;
- the principal authority or Machine Credential Administrator remains responsible for that credential;
- a client machine credential must not be uploaded to a vendor cloud;
- SBR2 M2M testing is available in EVTE after DSP and product registration; and
- machine credentials integrate with the ATO MAS-ST/STS and current developer kit.

The following require written DPO confirmation before certification scope is frozen:

1. Tammy's client-controlled classification and Category D assignment.
2. Whether the first product registration can be restricted to direct desktop, single-record interactions.
3. Required `LDG.List` and AS.0004 (2025) EVTE and conformance cases.
4. Product ID distribution and protection expectations for a packaged desktop product.
5. The current ATO SDK/ADK components and licences that a Go product may wrap or redistribute.
6. Machine-credential keystore integration and supported target operating systems.
7. Current endpoints, pMode values, reasonable-use limits, test ABNs, and EVTE credentials.
8. Whether Production Verification Testing is required for this product and service combination.

No implementation may claim production SBR support until these external gates are complete.

## 4. Architectural decision

### 4.1 Selected approach

Tammy uses a client-controlled Electron application, a bundled local Go service, an encrypted SQLite workspace, and an isolated SBR helper.

```text
┌───────────────────────────────────────────────────────────────┐
│ Electron renderer                                             │
│ React, Tailwind, shadcn/ui, no Node.js or filesystem access   │
└───────────────────────────────┬───────────────────────────────┘
                                │ allowlisted contextBridge API
┌───────────────────────────────▼───────────────────────────────┐
│ Electron main                                                 │
│ lifecycle, window policy, local Connect-ES client, packaging   │
└───────────────────────────────┬───────────────────────────────┘
                                │ pinned TLS 1.3 loopback Connect
┌───────────────────────────────▼───────────────────────────────┐
│ Local Go core                                                 │
│ identity, accounting, tax, evidence, audit, orchestration      │
└───────────────┬───────────────────────────────┬───────────────┘
                │                               │ explicit only
┌───────────────▼───────────────┐  ┌────────────▼──────────────┐
│ Encrypted SQLite workspace    │  │ Isolated SBR helper       │
│ domain state + audit          │  │ credential + STS + AS4    │
└───────────────────────────────┘  └────────────┬──────────────┘
                                                │ TLS 1.3
                                   ┌────────────▼──────────────┐
                                   │ ATO EVTE or production    │
                                   └───────────────────────────┘
```

### 4.2 Rejected approaches

**DSP-controlled cloud core:** This would make the product dependent on vendor infrastructure, introduce CAA and Software IDs, and likely move the primary mode into Category B.

**Embedded PostgreSQL:** A full local database server adds process supervision, ports, memory, upgrade, backup, and packaging complexity without improving the single-workspace transactional model.

**Microservices:** Multiple deployable services would enlarge the local attack surface and make transactions, evidence, updates, and support more difficult. Clear module boundaries inside a single Go service provide sufficient separation.

**Dual desktop and cloud certification in the first release:** Maintaining two credential, authorisation, hosting, and operational models would double the certification and failure surface. A cloud relay remains a future, separately designed mode.

## 5. Runtime processes and trust boundaries

### 5.1 Renderer

The renderer is untrusted presentation code. It:

- renders React views;
- performs immediate usability validation;
- calls only allowlisted preload methods; and
- never receives database paths, filesystem handles, API capability tokens, machine credentials, Product IDs, or unrestricted network primitives.

Electron requirements:

- `sandbox: true`;
- `contextIsolation: true`;
- `nodeIntegration: false`;
- a strict content security policy without `unsafe-eval`;
- navigation and new-window requests denied unless explicitly allowlisted;
- all permission requests denied unless required by an approved feature;
- a custom application protocol rather than arbitrary `file://` navigation; and
- renderer assets bundled locally so the product starts offline.

### 5.2 Electron main

Electron main:

- launches and supervises the Go core;
- receives the core bootstrap record through the child's inherited standard stream;
- owns the local Connect endpoint and per-launch capability;
- pins the core's per-launch TLS identity;
- implements the generated Connect-ES client;
- converts protobuf values into structured-clone-safe results for the preload API;
- enforces the window, URL, download, and update policy; and
- shuts the core down cleanly.

React never receives a generic IPC invoke method. Preload exposes one typed function per approved product use case.

### 5.3 Go core

At startup the core:

1. binds to a random loopback port;
2. creates an ephemeral TLS 1.3 server identity;
3. creates a cryptographically random per-launch capability;
4. emits only the port, certificate pin, and capability to its parent process;
5. requires both certificate pinning and the capability on every Connect call; and
6. rejects non-loopback traffic.

The API is unavailable to LAN peers and unrelated local processes. Logs redact bootstrap secrets.

### 5.4 SBR helper

The SBR helper is a separate local process with a narrow request/response protocol. It:

- is launched only for credential management, connectivity tests, validation, submission, or reconciliation;
- receives the minimum report and message context;
- interacts with the approved ATO credential/STS developer component;
- prevents credential material from reaching Electron or ordinary Go modules;
- keeps passwords and decrypted keys in memory only as long as required;
- returns signed transport results and structured status, not private key material; and
- records security-relevant lifecycle events through the core.

The production helper is enabled only after the relevant ATO kit and licence are obtained. Until then, a deterministic simulator implements the same application port.

## 6. Repository and contract structure

```text
apps/
  desktop/                    Electron main, preload, React renderer
services/
  core/                       Go Connect server and composition root
  sbr-helper/                 Isolated ATO credential/transport process
proto/
  tammy/v1/                   Public local API contracts
packages/
  connect-client/             Buf-generated Connect-ES client
  ui/                         shadcn primitives and product components
internal/
  identity/
  organisations/
  accounting/
  taxrules/
  tax/
  evidence/
  sbr/
  audit/
  artefacts/
  integrations/
  platform/
test/
  e2e/
  fixtures/
compliance/
  dpo/
  osf/
  passwords/
  threat-model/
  evidence/
```

Buf governs all protobuf contracts. CI must run formatting, linting, generation consistency, and breaking-change checks. Generated files are never edited manually.

The initial API packages are:

- `tammy.v1.SystemService`
- `tammy.v1.WorkspaceService`
- `tammy.v1.IdentityService`
- `tammy.v1.OrganisationService`
- `tammy.v1.AccountingService`
- `tammy.v1.TaxService`
- `tammy.v1.SBRService`
- `tammy.v1.AuditService`

Protobuf messages describe API intent rather than mirroring database rows. Mutating requests carry an idempotency key. Monetary fields use a currency code plus signed integer minor units. Rates and quantities use explicit scaled integers or decimal strings with a declared scale; binary floating point is prohibited for posted accounting values.

### 6.1 Transaction and dependency rules

`platform.UnitOfWork` owns every write transaction. An application service opens the unit of work, loads aggregates through module repositories, invokes domain behaviour, appends an audit record, and commits once. Domain modules never call `Commit` and never query another module's tables.

Documented application dependencies are:

```text
TaxRules ← Accounting ── AccountingReadPort ──→ Tax
    └──────────────────────────────────────────→ Tax
Organisations ── OrganisationReadPort ─────────→ Tax
Tax ───────── ReportReadPort ──────────────────→ Evidence
Artefacts ── ArtefactReadPort ─────────────────→ Accounting + Tax + SBR
Identity ── IdentityAuthorizer ────────────────→ all handlers
Organisations + Tax + Evidence + Artefacts ───→ SBR
All command handlers ──────────────────────────→ AuditAppender
```

`TaxRules` is a pure, versioned GST calculation kernel and catalogue. It has no dependency on Accounting or Tax, so Accounting can validate and expand tax-aware source lines while Tax can aggregate the same rule identifiers without a cycle. `AuditAppender` accepts the current `UnitOfWork` and does not create a nested transaction. Read ports return immutable application projections rather than repositories or database handles. The composition root supplies in-memory fakes for unit tests, SQLite adapters for integration tests, and the simulator or helper adapter for SBR contract tests.

The cross-module ports are operation-level contracts:

```go
type TxScope interface {
    TransactionID() string
}

type UnitOfWork interface {
    Do(ctx context.Context, fn func(context.Context, TxScope) error) error
    Read(ctx context.Context, fn func(context.Context, TxScope) error) error
}

type IdentityAuthorizer interface {
    Require(ctx context.Context, tx TxScope, actor UserID, permissions ...Permission) error
    RequireRecentTOTP(ctx context.Context, tx TxScope, actor UserID, maxAge time.Duration) error
}

type AccountingReadPort interface {
    TaxPostings(ctx context.Context, tx TxScope, organisation OrganisationID, period DateRange) (TaxPostingSet, error)
    TrialBalance(ctx context.Context, tx TxScope, organisation OrganisationID, asOf civil.Date) (TrialBalanceProjection, error)
}

type OrganisationReadPort interface {
    ReportingProfile(ctx context.Context, tx TxScope, organisation OrganisationID) (ReportingProfile, error)
    CurrentVerification(ctx context.Context, tx TxScope, organisation OrganisationID, at time.Time) (VerificationProjection, error)
}

type ReportReadPort interface {
    ForDeclaration(ctx context.Context, tx TxScope, report ReportID) (DeclarationReportProjection, error)
    ForSubmission(ctx context.Context, tx TxScope, report ReportID) (SubmissionReportProjection, error)
}

type EvidenceReadPort interface {
    CurrentDeclaration(ctx context.Context, tx TxScope, report ReportID, contentHash Hash) (DeclarationProjection, error)
}

type ArtefactReadPort interface {
    RuleBundle(ctx context.Context, tx TxScope, bundle BundleID) (RuleBundleProjection, error)
    ServiceDefinition(ctx context.Context, tx TxScope, interaction InteractionID) (ServiceDefinitionProjection, error)
}

type AuditAppender interface {
    Append(ctx context.Context, tx TxScope, event AuditEventDraft) (AuditEventProjection, error)
}
```

`TaxPostingSet` contains a ledger revision plus immutable posting projections: journal and line IDs, posting date, account, gross/net/GST minor units, tax-rule ID, and source hash. `ReportingProfile` contains ABN, legal name, GST basis, reporting period, and initiating-party role. Declaration and submission projections contain report version, organisation, period, content hash, state, BAS values, rule/artefact versions, and signatory requirements. Every cross-module read in a write command receives the same `TxScope`, so authorisation, source data, report state, declaration, and audit commit against one SQLite snapshot.

### 6.2 First-slice use-case catalogue

| RPC/use case | Authorised role | Request → result | Transaction owner and ports | Principal failures |
|---|---|---|---|---|
| `WorkspaceService.CreateWorkspace` | unauthenticated local setup | path, passphrase, first-admin identity/password → pending workspace and one-time recovery secret | Workspace; key store, storage factory | path exists, weak passphrase, invalid administrator, key-store failure |
| `WorkspaceService.ConfirmRecovery` | pending first administrator | prompted recovery-secret groups → active workspace | Workspace; header store, audit | wrong recovery groups, setup expired |
| `WorkspaceService.UnlockWorkspace` | local user at closed app | workspace path, passphrase, recovery, or remembered-workspace choice → unauthenticated open workspace | Workspace; key store, encrypted storage | wrong key, corrupt header, unsupported schema |
| `WorkspaceService.EstablishMovedWorkspaceTrust` | staged `workspace_admin` with fresh TOTP or recovery | destination installation ID and proof → writable workspace/new mirror | Workspace + Audit; key store | chain invalid, non-admin, stale factor |
| `WorkspaceService.BackupWorkspace` | `workspace_admin` | destination, backup passphrase → backup manifest | Workspace; all-storage snapshot, audit signer | destination failure, integrity failure |
| `WorkspaceService.RestoreWorkspace` | admin authenticated or recovered from staged backup | backup path/passphrase, normal admin proof or break-glass recovery proof, operation key → restored summary | External restore journal; staging storage, staged Identity/Audit | wrong key, non-admin, factor required, signature/schema failure |
| `IdentityService.SignIn` | unauthenticated open workspace user | username, password → session | Identity; user repository, session store | invalid credentials, locked account |
| `IdentityService.CreateUser` | `workspace_admin` | username, display name, initial roles → pending user and one-time activation code | Identity; audit | duplicate username, invalid role |
| `IdentityService.ActivateUser` | pending local user | username, activation code, new password → active user | Identity; audit | code invalid/expired, weak password |
| `IdentityService.ChangePassword` | authenticated user | current password, new password → session invalidation result | Identity; audit | current password invalid, weak/reused password |
| `IdentityService.AssignRoles` | `workspace_admin` | user ID, complete role set → user | Identity; audit | last-admin removal, invalid role |
| `IdentityService.EnrolTOTP` | authenticated user | current password → provisioning secret | Identity; factor repository, audit | password invalid, already enrolled |
| `IdentityService.ConfirmTOTP` | enrolling user | one-time code → enabled factor | Identity; factor repository, audit | invalid or replayed code |
| `IdentityService.AssertTOTP` | authenticated enrolled user | one-time code → five-minute elevated marker | Identity; factor/session repository, audit | invalid/replayed code, factor unavailable |
| `IdentityService.DisableTOTP` | authenticated user | current password and TOTP → disabled factor | Identity; audit | invalid proof, lodgment becomes unavailable |
| `IdentityService.ResetUserAuthentication` | `workspace_admin` with fresh TOTP | target user → pending user and new activation code | Identity; audit | target is last admin, admin factor stale |
| `IdentityService.RecoverAdministrator` | locked-app break glass | recovery secret, admin username, new workspace/user passwords → reset administrator | Workspace + Identity; audit | recovery invalid, user not admin |
| `OrganisationService.CreateOrganisation` | `workspace_admin` | ABN, legal name, GST settings → organisation | Organisations; audit | invalid ABN format, duplicate ABN |
| `OrganisationService.RecordEntityVerification` | `workspace_admin` | organisation, source metadata, evidence hash → verification | Organisations; evidence blob port, audit | source invalid, evidence missing, details mismatch |
| `AccountingService.CreateAccount` | `workspace_admin`, `business_preparer` | code, name, type → account | Accounting; audit | duplicate code, invalid type |
| `AccountingService.SetAccountStatus` | `workspace_admin` | account ID, active/archived, reason → account | Accounting; audit | system account, invalid transition |
| `AccountingService.ListTaxCodes` | any preparing role | organisation, posting date → rule IDs, labels, and treatments | Read-only Accounting; OrganisationReadPort, ArtefactReadPort, TaxRules | rule bundle unavailable, invalid date |
| `AccountingService.PostJournal` | `workspace_admin`, `business_preparer` | date, memo, source lines/rule IDs, optional correction reversal ID → posted journal | Accounting; OrganisationReadPort, ArtefactReadPort, TaxRules, audit | unbalanced, closed period, invalid treatment/correction |
| `AccountingService.ReverseJournal` | `workspace_admin`, `business_preparer` | journal ID, reversal date/reason → linked reversing journal | Accounting; TaxRules, audit | already reversed, closed period, invalid date |
| `AccountingService.ClosePeriod` | `workspace_admin` with fresh TOTP | organisation, end date → closed period | Accounting; audit | open draft/report conflict, already closed |
| `AccountingService.ReopenPeriod` | `workspace_admin` with fresh TOTP | period ID, reason → reopened period | Accounting; audit | not closed, missing reason |
| `AccountingService.GetTrialBalance` | any first-slice role | organisation, as-of date → account balances and totals | Read-only Accounting | invalid period, permission denied |
| `TaxService.CreateBASWorkpaper` | `workspace_admin`, `business_preparer` | organisation, period, rule bundle → workpaper with provenance | Tax; AccountingReadPort, OrganisationReadPort, TaxRules, audit | overlapping report, rule unavailable |
| `TaxService.ValidateBAS` | `workspace_admin`, `business_preparer` | report ID → validation run and report state | Tax; ArtefactReadPort, TaxRules, audit | stale source, blocking validation |
| `TaxService.AcceptDeclaration` | `business_lodger` | report ID, declaration version, acknowledgement → declaration | Evidence orchestrator; ReportReadPort, audit | TOTP required, stale report, wrong declaration |
| `SBRService.SubmitActivityStatement` | `business_lodger` | report ID, environment → durable transmission status | SBR orchestrator; identity, organisation, report, declaration, artefact, gateway, audit | unverified entity, stale TOTP, invalid state, credential unavailable |
| `SBRService.ReconcileTransmission` | `business_lodger` | transmission ID → reconciled report/transmission state | SBR orchestrator; gateway, audit | not unknown, inconclusive response, unavailable service |
| `AuditService.VerifyChain` | `workspace_admin`, `auditor` | optional sequence range → integrity result | Read-only Audit | chain mismatch, missing event |
| `AuditService.ExportEvidence` | `workspace_admin`, `auditor` | destination, filters → signed export manifest | Audit; evidence reader, signing key | destination failure, chain invalid |

Queries omitted from the table are read-only projections and still require role checks. Ordinary mutations commit domain state and the audit event in one `UnitOfWork`. Restore uses the separately defined external operation journal because it replaces the database that normally stores idempotency and audit state.

### 6.3 Idempotency contract

The idempotency scope is `(workspace_id, actor_user_id, fully_qualified_rpc_name, idempotency_key)`. The key is a client-generated UUID and is required on every mutation after workspace creation.

The core stores:

- the scope;
- a deterministic protobuf hash of the semantic request, excluding authentication metadata and the idempotency key;
- execution state;
- committed response or error;
- created time; and
- resulting resource identifier.

A unique database constraint elects one execution. A concurrent duplicate waits at most two seconds for the elected transaction, then either returns the committed result or an `ABORTED` response with retry details; it never runs the command twice. Reusing a key with a different request hash returns `IDEMPOTENCY_CONFLICT`. Financial, report, declaration, transmission, and backup keys are retained for the workspace lifetime. Reversible administrative-command records are retained for at least 30 days. Tests use an injected clock/scheduler rather than sleeping.

Restore cannot rely on an idempotency row inside a database it may replace. A small external restore-operation journal in application data scopes keys by `(target_workspace_id, operation_key)`, stores the backup manifest hash and states `PREPARED`, `STAGED`, `SWAPPED`, and `COMPLETE`, and contains no accounting values or passwords. Entries are chained and HMAC-authenticated with an installation key held in the OS credential store. The journal is fsync'd at every transition and retained for the workspace lifetime. Startup resumes or rolls back the recorded transition; a reused key with a different manifest hash fails with `IDEMPOTENCY_CONFLICT`.

## 7. Module boundaries

Each module owns its domain rules, schema objects, repository interface, Connect handlers, and tests. A module may depend only on documented application ports, not another module's tables.

### 7.1 Identity

Responsibilities:

- unique local users;
- Argon2id password verification;
- offline TOTP enrolment and verification;
- one-time user activation and break-glass administrator recovery;
- role assignments;
- failed-login lockout;
- 30-minute inactivity locking;
- no remembered application-user session across process exit; and
- authentication and authorisation audit events.

The first slice supports `workspace_admin`, `business_preparer`, `business_lodger`, and `auditor`. Roles are additive. A workspace administrator does not receive lodgment permission unless also assigned `business_lodger`.

Workspace passphrases and application-user passwords share this Milestone 1 policy:

- normalise to Unicode NFC before strength checks, hashing, and comparison; never trim;
- accept 15 through 128 Unicode code points and at most 1,024 UTF-8 bytes;
- allow spaces and all printable Unicode without composition rules;
- reject a case-folded match in the exact 10,000-entry `compliance/passwords/tammy-common-passwords-v1.txt` denylist whose checksum is pinned in the release manifest;
- retain the prior five user-password verifiers and prior three workspace-passphrase verifiers and reject reuse; and
- compare verifiers in constant time.

The initial Argon2id verifier parameters are exactly 64 MiB memory, 3 iterations, parallelism 1, a random 16-byte salt, and a 32-byte result. Parameters and policy version are stored with every verifier. A later release may increase parameters under a versioned migration but may never reinterpret or silently weaken an existing verifier. "Weak password" and "reused password" in the API catalogue mean these exact rules.

| Permission | Admin | Preparer | Lodger | Auditor |
|---|:---:|:---:|:---:|:---:|
| Manage workspace and users | ✓ |  |  |  |
| Manage organisation and verification | ✓ |  |  |  |
| Read accounts, journals, and reports | ✓ | ✓ | ✓ | ✓ |
| Create accounts and post journals | ✓ | ✓ |  |  |
| Reverse journals | ✓ | ✓ |  |  |
| Archive/reactivate accounts | ✓ |  |  |  |
| Close/reopen periods | ✓ + fresh TOTP |  |  |  |
| Prepare and locally validate BAS | ✓ | ✓ |  |  |
| Accept lodgment declaration |  |  | ✓ |  |
| Submit or reconcile SBR |  |  | ✓ |  |
| Read complete audit log | ✓ |  | relevant report only | ✓ |
| Export audit evidence | ✓ |  |  | ✓ |

All users authenticate with a password in Milestone 1. A `business_lodger` and any administrator using a high-risk command must enrol TOTP. Platform passkeys are deferred until a separate cross-platform design confirms offline recovery and Electron platform-authenticator behaviour.

Authentication timing is deterministic:

| State/control | Rule | Recovery |
|---|---|---|
| Pending workspace setup | Recovery confirmation must finish within 15 minutes of `CreateWorkspace`; no business data may be entered while pending | Expiry securely deletes the pending database/header and setup restarts |
| Pending user | Activation code is 128 random bits, stored hashed, valid for 24 hours, and locked after five failures | Administrator issues a new code |
| Failed sign-in | Five consecutive failures within 15 minutes lock the user for 15 minutes; state persists across restart; successful sign-in clears the window | Time expiry or `ResetUserAuthentication` by an administrator |
| Closed workspace | Database is closed and no DEK is present | Passphrase, recovery secret, or a still-valid remembered-workspace item opens `UNAUTHENTICATED` |
| Unauthenticated open workspace | DEK is in core memory and only sign-in/recovery RPCs are allowed for at most five minutes | Successful sign-in enters `AUTHENTICATED`; timeout or explicit cancel closes and zeroes the DEK |
| Authenticated workspace | One user session and the DEK are active | 30 minutes without accepted user activity, explicit lock/sign-out, OS session lock/sleep, core exit, or app exit invalidates the session, closes SQLite, and zeroes the DEK |
| Remembered workspace | OS-key-store DEK item lasts at most 23 hours 59 minutes; this is not a user session | Passphrase or recovery secret |
| TOTP assertion | Six-digit, 30-second RFC 6238 value with a one-step clock window; accepted counters cannot replay | User enters a new value |
| Elevated TOTP freshness | Valid for five minutes in the same session | User enters a new value |

There is no independent remembered or locked user session in Milestone 1. A session lock always closes the workspace. Re-entry first opens the workspace using the passphrase, recovery secret, or remembered-workspace item, then requires the user's password. TOTP is asserted separately for high-risk actions.

One fresh TOTP assertion may cover declaration and submission only when the session, report ID, and report content hash remain unchanged and dispatch begins inside five minutes. The core checks freshness again immediately before `DISPATCHING`. Reconciliation, factor reset, moved-workspace mirror establishment, period close, and period reopen always require a new assertion.

Disabling TOTP immediately removes elevated permission; a lodger cannot declare or submit until re-enrolled. `ResetUserAuthentication` invalidates all target-user sessions and factors and returns the account to pending activation. The sole administrator who loses all factors uses the workspace recovery secret through `RecoverAdministrator`; the operation resets the workspace passphrase and that administrator's password, disables their factors, invalidates every session, and appends a high-risk audit event.

### 7.2 Organisations

Responsibilities:

- business identity and ABN;
- legal and display names;
- reporting role;
- financial year and GST settings;
- workspace membership;
- entity-validation evidence; and
- changes that require re-verification.

Offline setup may record an organisation as `UNVERIFIED`. SBR actions remain disabled until independent entity validation evidence is recorded.

Milestone 1 permits one organisation record per workspace. Attempting to create a second returns `ORGANISATION_LIMIT_REACHED`; changing the existing ABN supersedes entity verification and is audited as a high-risk change.

An entity-verification record contains:

- organisation ID and ABN;
- verified legal name and entity type;
- source method: `ABR_ONLINE`, `ABR_EXTRACT_MANUAL`, or `SIMULATOR_FIXTURE`;
- source reference and lookup time;
- recorder user ID and record time;
- evidence object hash;
- outcome: `VERIFIED`, `FAILED`, `EXPIRED`, or `SUPERSEDED`; and
- expiry time.

Only `workspace_admin` may record or supersede verification. `ABR_EXTRACT_MANUAL` requires a saved independent-source extract or capture whose hash is retained in evidence storage. Product policy expires verification after 12 months and immediately supersedes it after an ABN, verified legal-name, entity-type, or workspace-ownership change. A DPO response may require a shorter interval.

`SIMULATOR_FIXTURE` exists only in test-signed builds with the SBR environment fixed to `SIMULATOR`. It cannot enable EVTE or production controls. The canonical packaged E2E records this fixture before BAS preparation and submission. Release builds require `ABR_ONLINE` or `ABR_EXTRACT_MANUAL`.

### 7.3 Accounting

Responsibilities:

- chart of accounts;
- journals and journal lines;
- accounting periods;
- general ledger;
- trial balance; and
- reversal and correction workflows.

Invariants:

- every posted journal has at least two lines;
- total debits equal total credits in minor units;
- all lines share the journal currency in the first slice;
- posted journals are immutable;
- corrections use linked reversing and replacement journals;
- closed periods reject new postings unless reopened by an authorised user;
- account type and status determine valid posting behaviour; and
- repeated idempotency keys return the original result.

An archived account remains in history and reports but rejects new postings. The five canonical system/control accounts cannot be archived while a report rule bundle references them. A reversal is a new posted journal dated in an open period, with every debit and credit inverted, an immutable link to the original, and a mandatory reason. A journal can have only one direct reversal.

A correction is a reversal followed by `PostJournal` with `correction_of_reversal_journal_id`. The referenced journal must be a reversal in the same organisation and currency, must not already have a correction, and the replacement date must be in an open period. A uniqueness constraint permits one replacement link. Neither the original nor reversal is edited.

Closing a period prevents posting or reversal on or before its end date. It fails while a pre-dispatch BAS report for that period is stale or in draft. Reopening requires an administrator, fresh TOTP, and a reason; it creates an audit event and supersedes any pre-dispatch BAS validation for the affected period. Dispatched report snapshots remain immutable and later corrections use the revision rule in Section 7.4.

#### Canonical accounting and GST fixture

Milestone 1 ships this executable fixture as the acceptance oracle:

| Code | Account | Type |
|---|---|---|
| `1100` | Bank | Asset |
| `1200` | GST receivable | Asset |
| `2200` | GST payable | Liability |
| `4000` | Consulting revenue | Revenue |
| `6100` | Software expense | Expense |

Source transaction A is a GST-inclusive taxable sale for `$1,100.00`. Posting produces debit Bank `$1,100.00`, credit Consulting revenue `$1,000.00`, and credit GST payable `$100.00`.

Source transaction B is a creditable GST-inclusive software purchase for `$220.00`. Posting produces debit Software expense `$200.00`, debit GST receivable `$20.00`, and credit Bank `$220.00`.

The trial balance must contain `$1,100.00` total debits and `$1,100.00` total credits. Closing balances are Bank debit `$880.00`, GST receivable debit `$20.00`, Software expense debit `$200.00`, GST payable credit `$100.00`, and Consulting revenue credit `$1,000.00`.

Initial tax codes are:

- `GST_SALES_10_INCLUSIVE`;
- `GST_PURCHASES_10_INCLUSIVE`;
- `GST_FREE_SALES`;
- `GST_FREE_PURCHASES`;
- `INPUT_TAXED`;
- `OUT_OF_SCOPE`.

For the two inclusive codes, GST is the taxable gross amount divided by 11 using exact integer/rational arithmetic. Fractions of one cent round to the nearest cent with an exact half-cent rounded away from zero. The stored source line records gross, net, GST, tax code, and derived control-account line IDs so `gross = net + GST` remains inspectable.

For the canonical BAS workpaper:

| BAS label | Expected source | Expected displayed value |
|---|---|---:|
| `G1` Total sales | Transaction A gross sale | `$1,100` |
| `1A` GST on sales | GST payable from A | `$100` |
| `1B` GST on purchases | GST receivable from B | `$20` |
| Net GST payable | `1A - 1B` | `$80` |

BAS presentation drops cents and does not round up, consistent with current ATO BAS instructions. Values sent to SBR must additionally satisfy the exact AS.0004 schema and rule bundle selected for the report.

### 7.3.1 TaxRules kernel

`TaxRules` owns the schemas and pure algorithms for interpreting immutable GST rule definitions; it does not persist or select bundles. Artefacts owns rule-bundle storage, checksums, lifecycle, and enumeration. The organisation reporting profile selects the active bundle for new postings, and each posted line/report retains that bundle ID.

Accounting obtains the organisation's bundle ID through `OrganisationReadPort`, loads it through `ArtefactReadPort`, and calls `TaxRules.Enumerate(bundle)` or `ExpandSourceLine(bundle, ruleID, grossAmount)`. Tax loads the report's retained bundle and calls `Rule` and `AggregateClassification` to map posted projections into BAS categories.

The kernel has no database, user, journal, report, catalogue, or transport access. Its contract suite covers the canonical rules, sign handling, exact-half-cent behaviour, unsupported rule IDs, and deterministic results. New legislative rules are stored by Artefacts as new immutable bundle versions rather than changing prior definitions.

### 7.4 Tax

Responsibilities:

- selection of the rule and BAS-mapping bundles retained by each workpaper/report;
- consumption of the immutable tax treatment captured by Accounting on journal lines;
- versioned BAS field mappings;
- BAS workpapers;
- local validation;
- Activity Statement report states; and
- calculation provenance from report fields to source journal lines.

The module never embeds a service-version constant throughout UI code. A report retains the tax rule, field mapping, and artefact-bundle versions used to prepare it.

The first report state machine is:

```text
DRAFT
  → LOCALLY_VALIDATED
  → DECLARED
  → SUBMISSION_PREPARED
  → DISPATCHING
  → ACCEPTED | REJECTED | UNKNOWN | TECHNICAL_FAILURE_SAFE
```

Transitions and recovery rules:

- Reversing a contributing journal or posting a new in-period journal while the report is `LOCALLY_VALIDATED`, `DECLARED`, or `SUBMISSION_PREPARED` moves it to `DRAFT`, cancels any not-yet-dispatched transmission, supersedes validation and declaration, and retains them as historical evidence.
- Once a report is `DISPATCHING` or in a terminal submission state, its source snapshot and payload are immutable. Later in-period postings require a linked correction or revision workflow and never alter the dispatched payload.
- `LOCALLY_VALIDATED → DECLARED` occurs only in the same transaction that stores a declaration for the current report content hash.
- `DECLARED → SUBMISSION_PREPARED` creates the durable transmission identifiers and payload hash before network activity.
- A user may cancel `SUBMISSION_PREPARED` back to `DECLARED` while no dispatch has begun.
- Credential unlock and payload construction failures occur before `DISPATCHING` and return the report to `DECLARED` with a safe technical-failure record.
- `DISPATCHING` means remote transmission may have begun. A crash, helper EOF, timeout, or error that cannot prove no bytes were sent changes the report to `UNKNOWN`.
- `TECHNICAL_FAILURE_SAFE → DECLARED` permits an explicit retry because the helper attested that no network send began.
- `UNKNOWN` is terminal until reconciliation. Reconciliation may move it to `ACCEPTED`, `REJECTED`, or `DECLARED` only when an authoritative result proves the original payload was not received. An inconclusive result leaves it `UNKNOWN`.
- `REJECTED` may create a corrected linked `DRAFT`; an accepted report may create only a linked revision workflow.

`PREFILLED` and `ATO_VALIDATED` become additional pre-declaration states when the real `LDG.List`, `AS.Get`, and `AS.Validate` adapters are enabled. Revisions start a new linked report and require a new declaration.

### 7.5 Evidence

Responsibilities:

- declaration text versions;
- declaration acceptance;
- signatory and authority;
- attachment metadata;
- content hashes;
- encrypted evidence storage; and
- retention policy.

A submission requiring a declaration is impossible unless the declaration belongs to the same organisation, report, reporting period, and report content hash.

The deterministic simulator fixture uses:

- organisation `Wattle & Co Test Pty Ltd`;
- syntactically valid, simulator-only ABN `11 000 000 560`, which is never looked up or transmitted;
- reporting period 1 April through 30 June 2026;
- the two accounting transactions in Section 7.3 dated 30 June 2026;
- declaration version `SIM-AS-DECL-2026-01` with text "I declare that the information in this simulated Activity Statement is true and correct.";
- simulator result code `SIM.ACCEPTED`;
- simulator receipt `SIM-2026-Q4-0001`; and
- amount payable `$80`.

The simulator declaration and response are visibly marked non-ATO and are impossible to select for EVTE or production. Real declaration wording and response mapping come only from the applicable approved artefact bundle.

### 7.6 SBR

Responsibilities:

- application-level orchestration;
- supported service definitions;
- message and conversation identifiers;
- payload hashes;
- submission idempotency;
- response correlation;
- transport outcome classification; and
- reconciliation of ambiguous outcomes.

Ports:

```go
type ObligationGateway interface {
    List(ctx context.Context, request ListRequest) (ListResult, error)
}

type ActivityStatementGateway interface {
    Get(ctx context.Context, request GetRequest) (GetResult, error)
    Validate(ctx context.Context, request ValidateRequest) (ValidateResult, error)
    Submit(ctx context.Context, request SubmitRequest) (SubmitResult, error)
    Reconcile(ctx context.Context, request ReconcileRequest) (ReconcileResult, error)
}
```

Every gateway error carries a send phase:

- `NOT_STARTED`: the helper proves no connection or write began;
- `MAYBE_SENT`: the remote endpoint may have received any part of the request; or
- `RESPONSE_RECEIVED`: complete response bytes are available for atomic persistence.

The durable submission protocol is:

1. Deterministically build the payload and allocate application transaction, SBR Message, and Conversation IDs.
2. Commit a `PREPARED` transmission with payload hash, report content hash, artefact version, and identifiers.
3. Unlock the credential and finish all work that can fail safely before network access.
4. Commit the transmission and report as `DISPATCHING`.
5. Invoke the helper exactly once with the already persisted identifiers and payload.
6. If response bytes are returned, commit the encrypted raw response, parsed result, transmission state, report state, and audit event atomically before showing any result.
7. If the helper reports `NOT_STARTED`, commit `TECHNICAL_FAILURE_SAFE`; if it reports `MAYBE_SENT`, exits unexpectedly, or times out, commit `UNKNOWN`.
8. On startup, resume a `PREPARED` transmission only after user confirmation, but convert every orphaned `DISPATCHING` transmission to `UNKNOWN` without sending.

A process death after ATO acceptance but before local response persistence therefore recovers as `UNKNOWN`, never as a retryable failure. Reconciliation uses the persisted original identifiers and payload hash.

The simulator and the real helper adapter must pass the same contract suite.

### 7.7 Audit

Responsibilities:

- append-only security and domain events;
- at least 12 months of accessible history;
- hash chaining;
- filtered user views;
- integrity verification; and
- signed evidence export.

Every ordinary write transaction includes the domain change and audit record. The next audit hash covers the prior hash plus a canonical representation of the new event. The application exposes verification but no ordinary delete or update operation for audit rows.

There is one monotonically sequenced chain per workspace audit generation. Event content uses RFC 8785 JSON Canonicalization Scheme encoding. The chain is:

```text
genesis = SHA-256("tammy-audit-v1" || workspace_id || chain_salt)
event_hash[n] = SHA-256(
  "tammy-audit-event-v1" ||
  event_hash[n-1] ||
  uint64_be(canonical_event_length) ||
  canonical_event_bytes
)
```

The random chain salt and genesis hash are stored in the workspace header. A database uniqueness constraint serialises the sequence. After commit, the latest `{workspace_id, generation, head}` is mirrored to the OS credential store. A database head ahead of the mirror in the same generation after a crash may repair the mirror after full verification; a mirror generation or head ahead of the database signals rollback and locks evidence export unless a matching in-progress authorised restore exists in the external restore journal.

Workspace creation establishes the first OS mirror after recovery confirmation and administrator sign-in. If a moved workspace is opened under an OS account with no mirror, Tammy verifies the chain from genesis and opens read-only with an explicit warning that this device cannot independently detect pre-move rollback. Establishing trust requires workspace passphrase or recovery proof, a `workspace_admin` password, and fresh TOTP (or the audited administrator break-glass path). One transaction appends `WORKSPACE_MIRROR_ESTABLISHED` with the prior head, destination installation-ID hash, and `prior_mirror_unavailable=true`; only then is the resulting generation/head written as the new OS baseline and writes are enabled. Declining leaves the workspace read-only.

An authorised restore starts a new audit generation from the backup head. Its first event is `WORKSPACE_RESTORED` and contains the prior active generation/head, backup generation/head, backup manifest hash, external restore operation ID, and hash of the encrypted pre-restore workspace archive. The chain's previous hash is the backup head, so all restored events remain verifiable; the generation increment and restore event make the deliberate history branch explicit. The pre-restore encrypted archive is retained as evidence for at least 12 months rather than discarded.

Workspace creation generates an Ed25519 audit-export keypair. The private key is encrypted under the workspace data-encryption key; the public key and key ID are stored in the workspace header. Export is a ZIP containing canonical `events.jsonl`, `manifest.json`, selected evidence objects, the public key, and `signature.ed25519` over the manifest hash. The app and a small standalone verifier both verify object hashes, event sequence, chain hashes, and signature without database access. Key rotation creates a cross-signed new public key and never rewrites prior events.

### 7.8 Artefacts

Responsibilities:

- immutable imported ATO packages;
- checksums and provenance;
- immutable GST rule bundles and their user-facing tax-code catalogue;
- Service Registry metadata;
- schemas, validation rules, messages, and declaration text;
- lifecycle status; and
- compatibility test triggers.

Source-control fixtures may contain only public or appropriately licensed artefacts. Restricted ATO material is imported into a local protected artefact store and excluded from public source distributions unless the licence permits redistribution.

### 7.9 Integrations

Each external integration implements an explicit port, permission manifest, health state, and disable switch. No integration may read arbitrary workspace tables. Optional cloud storage or relays require a new threat model and DPO classification review before release.

## 8. Local storage

Each workspace is an independently movable encrypted SQLite database plus an encrypted evidence directory.

Storage requirements:

- SQLCipher-compatible AES-256 page encryption;
- restrictive filesystem permissions;
- foreign keys enabled;
- WAL mode with checkpoint policy;
- embedded, forward-only migrations;
- a verified encrypted backup before migration;
- periodic integrity checks;
- a five-second SQLite busy timeout and at most five pre-transaction `BUSY` retries with full jitter bounded from 10 to 250 milliseconds; once a write transaction begins it is never replayed by the storage adapter;
- backup manifests containing schema and application versions; and
- restore verification before replacing an active workspace.

### 8.1 Workspace-key lifecycle

Workspace encryption and user authentication are distinct:

1. `CreateWorkspace` generates a random 256-bit workspace data-encryption key (DEK).
2. The user chooses a workspace passphrase. Argon2id derives a passphrase key-encryption key (KEK) using the exact versioned parameters in Section 7.1 and a random salt stored in the header.
3. AES-256-GCM wraps the DEK with the passphrase KEK using a random nonce and authenticated workspace/version metadata.
4. Setup generates a separate random 256-bit recovery secret, displays it once in grouped Base32, and requires the user to confirm selected groups. HKDF-SHA-256 derives a recovery KEK that wraps the same DEK with AES-256-GCM.
5. The unencrypted workspace header contains only format/KDF metadata, salts, nonces, wrapped DEKs, audit public material, and ciphertext hashes. It contains no accounting or identity data.
6. SQLCipher uses the unwrapped DEK to open the database. The DEK is zeroed when the workspace locks or the core exits.

If the user explicitly enables "remember this workspace on this OS account", the DEK is stored as a secret in macOS Keychain or Windows Credential Manager. This deliberately permits workspace decryption without re-entering the workspace passphrase, but it never signs in an application user. The remembered item expires after at most 23 hours 59 minutes and is removed on passphrase/recovery change, administrator recovery, explicit "forget this workspace", or user request. Automatic and ordinary workspace locks zero the in-memory DEK but do not delete a still-valid remembered-workspace item.

After workspace decryption, each natural person signs in with their own application password; TOTP is asserted separately when a high-risk action requires it. Milestone 1 permits one active application session at a time. Additional users do not receive or wrap the workspace DEK; an authorised person first unlocks the workspace on the device, then the user authenticates. Changing the workspace passphrase rewraps the same DEK. Recovery rewraps the DEK under a new passphrase and invalidates remembered-workspace entries.

Losing the passphrase, recovery secret, and any still-valid remembered-workspace item makes the workspace unrecoverable. The UI states this during setup and backup.

### 8.2 Backup and restore

A `tammy-backup-v1` archive contains:

- a consistent SQLite online-backup snapshot, not a copied live database/WAL pair;
- the workspace header;
- evidence objects referenced by the snapshot;
- the simulator/public artefact bundle needed by the snapshot;
- a canonical manifest of paths, sizes, hashes, schema version, application version, and audit head; and
- the audit-export public key and signature over the manifest.

Backups always exclude the machine-credential vault, credential passwords, remembered-workspace keys, active sessions, local RPC bootstrap material, and operational logs. Restricted ATO artefacts are included only when their licence permits encrypted backup; otherwise the manifest records the required checksum and restore marks the SBR adapter unavailable until the artefact is reimported.

Backup briefly acquires the workspace write gate, completes the SQLite online snapshot, captures evidence hashes, and then releases the gate. A random 256-bit archive key encrypts the complete archive using chunked AES-256-GCM. A backup-specific passphrase-derived Argon2id KEK wraps the archive key; it does not reuse the workspace passphrase KEK. Backup passphrases use the same normalization, length, denylist, and Argon2id parameters as Section 7.1 but have no cross-backup history.

The retained pre-restore archive uses format `tammy-pre-restore-v1`. It contains the active workspace header, encrypted database, evidence, and redistributable artefacts plus a canonical manifest signed by the staged workspace audit-export key. It explicitly excludes the separately located machine-credential vault, credential passwords, OS-key-store items, local RPC bootstrap material, and operational logs.

A new random 256-bit archive key encrypts `tammy-pre-restore-v1` with the same chunked AES-256-GCM envelope as a backup. The restored workspace DEK wraps that archive key with AES-256-GCM and authenticated operation/manifest metadata; the wrap is stored in the restored evidence record. Export or recovery requires an unlocked restored workspace plus administrator password and fresh TOTP. Export produces the original encrypted workspace bundle, which still requires its original workspace passphrase or recovery secret to open. The archive and key wrap are retained for at least 12 months; deletion after that period is an explicit audited administrator action.

Restore never writes over the active workspace in place:

1. fsync external restore-journal state `PREPARED` with operation key and backup manifest hash;
2. decrypt into a new staging directory;
3. verify authenticated encryption, manifest signature, every object hash, database integrity, schema compatibility, and the backup audit chain;
4. open staging and either authenticate a staged `workspace_admin` with workspace passphrase, user password, and TOTP, or use the staged recovery secret plus administrator username/new passwords to execute the same audited break-glass reset as `RecoverAdministrator`;
5. copy any active workspace into an encrypted pre-restore evidence archive, record its hash and OS-mirrored generation/head, and add the archive to staging;
6. invalidate restored sessions and remembered-workspace references;
7. set `generation = max(backup_generation, active_mirror_generation) + 1` and append the `WORKSPACE_RESTORED` event described in Section 7.7 to the staged database;
8. fsync external journal state `STAGED` with the new audit head;
9. atomically rename the active workspace to a rollback directory and staging to active, then fsync state `SWAPPED`;
10. open and verify the restored workspace, update the OS audit mirror, and fsync state `COMPLETE`; and
11. delete the temporary rollback directory only after the encrypted pre-restore archive is verified inside restored evidence.

Startup uses the external journal to finish or reverse a partial swap. `PREPARED` and `STAGED` leave the active workspace unchanged; `SWAPPED` verifies the new workspace and mirror or restores the rollback directory. A wrong passphrase, failed administrator authentication, or verification failure leaves the active workspace byte-for-byte unchanged.

The canonical restore test expects organisation data, both canonical journals, exact balances, the `$80` BAS result, declaration, accepted simulator transmission, and original evidence hashes to match the backup. The audit chain matches the backup through its manifest head, then contains exactly one new `WORKSPACE_RESTORED` event in the incremented generation; its new head therefore intentionally differs from the backup manifest. The later distinguishable journal is absent from live accounting but remains recoverable inside the hashed pre-restore archive. The machine credential remains absent and must be imported again.

The initial logical entities are:

- `workspace`
- `user`
- `user_factor`
- `session`
- `organisation`
- `membership`
- `entity_verification`
- `account`
- `accounting_period`
- `journal`
- `journal_line`
- `tax_code`
- `bas_workpaper`
- `bas_workpaper_line`
- `report`
- `report_value`
- `validation_run`
- `declaration`
- `transmission`
- `ato_response`
- `audit_event`
- `artefact_bundle`
- `service_definition`

## 9. Primary data flows

### 9.1 Posting a journal

1. React collects lines and performs immediate form checks.
2. Electron main sends a generated `PostJournal` Connect request with an idempotency key.
3. The accounting application service verifies identity, permission, period, accounts, amounts, and balance.
4. One SQLite transaction writes the immutable posted journal, journal lines, and audit event.
5. The response contains the journal identifier and resulting ledger totals.
6. React invalidates the relevant query caches and renders the committed state.

### 9.2 Preparing a BAS workpaper

1. The user selects an organisation and reporting period.
2. Tax queries posted journal lines and their explicit tax treatments.
3. Versioned mapping rules aggregate source values into BAS fields.
4. Every derived value retains links to contributing journal lines and the rule version.
5. Local validation records errors, warnings, and information separately.
6. A successful run changes the report to `LOCALLY_VALIDATED`.
7. A later source-data change invalidates validation and declaration only before `DISPATCHING`, as defined in Section 7.4. After dispatch begins, the payload snapshot remains immutable and the change creates a correction/revision requirement.

### 9.3 Direct SBR submission

1. The lodger opens the locally validated report.
2. Tammy verifies current independent entity-validation evidence, the `business_lodger` role, recent TOTP, report content hash, artefact version, and declaration version.
3. The user reviews and accepts the exact declaration.
4. One transaction stores the declaration for the current content hash and changes the report from `LOCALLY_VALIDATED` to `DECLARED`.
5. A second transaction deterministically stores the payload hash and all identifiers and changes the report to `SUBMISSION_PREPARED`.
6. The user unlocks the local machine credential. A failure here safely returns the report to `DECLARED`.
7. The core commits `DISPATCHING` before invoking the helper.
8. The helper obtains the required token, constructs and signs the current SBR2/ebMS3 message, and sends it directly to EVTE or production.
9. A complete response is persisted atomically before downstream processing. A crash or uncertain send recovers as `UNKNOWN`.
10. The response is correlated using application transaction ID, SBR Message ID, Conversation ID, and payload hash.
11. The report changes to `ACCEPTED`, `REJECTED`, `UNKNOWN`, or `TECHNICAL_FAILURE_SAFE`.
12. The UI presents ATO messages verbatim, a human explanation where available, and the correlation reference.

No `UNKNOWN` submission may be resubmitted. Reconciliation must first determine whether the ATO received and processed the original payload.

## 10. User experience

The selected visual direction is a compact, ledger-first desktop workspace.

Permanent left navigation:

- Overview
- Sales
- Purchases
- Banking
- Accounting
- Tax
- Reports
- Audit
- Settings

The first slice enables Overview, Accounting, Tax, Audit, and Settings. Disabled future modules are omitted rather than shown as dead navigation.

The workspace header shows:

- organisation context;
- reporting period where applicable;
- offline/online state;
- local save state;
- active user; and
- command palette access.

The Milestone 1 overview shows cash, revenue, expenses, GST position, recent posted journals, and the next BAS action. Receivables, payables, and reconciliation cards appear only after their later modules exist. Tables remain dense and keyboard navigable. Risky actions use explicit verbs, show their effect, and require confirmation proportional to the consequence.

SBR is never represented as background synchronisation. The UI distinguishes:

- local accounting state;
- ATO service availability;
- validation state;
- declaration state;
- transmission state; and
- acceptance or rejection.

A test-signed simulator build displays a permanent `SIMULATOR — NOT FOR ATO LODGMENT` banner in the application frame and submission confirmation. The environment comes from a signed build manifest, not a user-editable preference. EVTE and production builds fail closed if they encounter a `SIMULATOR_FIXTURE` verification, `SIM-*` declaration, or `SIM.*` response code.

Accessibility target:

- WCAG 2.2 AA for renderer workflows;
- complete keyboard operation;
- visible focus;
- semantic labels and error associations;
- reduced-motion support;
- non-colour status cues; and
- Australian English copy.

## 11. Error model

Connect error details use stable product error codes and structured field violations.

Each user-visible error contains:

- stable code;
- category;
- safe summary;
- actionable next step;
- field path when applicable;
- retry classification;
- correlation ID; and
- original ATO code and message where applicable.

Categories:

- `VALIDATION`
- `AUTHENTICATION`
- `AUTHORISATION`
- `STORAGE`
- `CREDENTIAL`
- `ATO_CHANNEL`
- `ATO_AUTHORISATION`
- `ATO_INTERACTIVE`
- `ATO_BACKEND`
- `TRANSPORT`
- `UNKNOWN_OUTCOME`
- `INTERNAL`

Policies:

- expected business-rule failures do not become generic internal errors;
- operational logs exclude raw tax data, credentials, Product IDs, TFNs, and unredacted Software IDs;
- original ATO messages are retained without alteration;
- malformed ATO responses are quarantined while the raw encrypted response remains available to an authorised local user;
- transport retries are permitted only where the operation is proven not to have been accepted by the remote endpoint; and
- the UI never labels an ATO outage as a report rejection.

## 12. Security controls

These are Tammy's product-selected baseline controls for the expected Category D desktop model. They do not assert that the DPO has assigned Category D or accepted the self-assessment; written confirmation remains an external gate under Sections 3 and 14.

### 12.1 Local identity

- No shared application accounts.
- Argon2id password hashing and versioned history use the exact Milestone 1 policy in Section 7.1.
- Five failed attempts trigger the persisted 15-minute lockout defined in Section 7.1.
- Inactivity locking occurs at or before 30 minutes.
- Application-user sessions are never remembered; an optional remembered-workspace key expires after at most 23 hours 59 minutes and still requires user sign-in.
- TOTP is implemented without a cloud dependency and is mandatory for lodgment actions; passkeys are outside Milestone 1.
- Lodgment permission is distinct from preparation permission.

### 12.2 Credential protection

- Machine credentials are created and managed by the client organisation through RAM.
- Tammy never sends a client credential to the DSP.
- The original credential format is preserved for the approved ATO component.
- Credential files use restrictive permissions and an encrypted local container where compatible with the ATO component.
- Credential passwords are not logged or persisted in application configuration.
- Unlock, use, failure, expiry, replacement, and removal are audited.
- Private key material never enters the renderer.
- Cancellation and suspected compromise have explicit removal and incident workflows.

### 12.3 Release security

- Application packages and updates are signed.
- Updates remain optional so offline operation is not blocked.
- Every release produces an SBOM.
- Go, npm, native library, and Electron dependencies are scanned.
- CI runs static analysis, secret detection, and licence checks.
- Production builds exclude development tools and source maps containing sensitive implementation details.
- Release evidence records toolchain versions, source revision, artefact hashes, tests, and signing identity.

### 12.4 Local support model

There is no hidden vendor support channel. Support exports are user-created, scope-previewed, redacted, encrypted, and time-limited. A support bundle never includes a machine credential or credential password.

## 13. Testing strategy

### 13.1 Go unit and property tests

- debit equals credit for every posted journal;
- posted journals cannot mutate;
- reversal exactly inverts lines, links once, and preserves the audit relationship;
- archived accounts reject posting but retain balances;
- period close/reopen permissions, TOTP, date, and report invalidation rules;
- concurrent idempotency duplicates return one committed result;
- idempotency-key reuse with a different payload returns `IDEMPOTENCY_CONFLICT`;
- closed periods reject posting;
- the canonical `$1,100` sale and `$220` purchase produce the exact fixture balances and `$80` BAS result;
- half-cent, negative, and whole-dollar BAS rounding follow the versioned rule set;
- BAS field provenance is complete;
- report transitions reject invalid paths;
- changed source data invalidates validation and declaration;
- entity verification expires and supersedes on defined high-risk changes;
- the permission matrix denies preparer submission and admin-only user management;
- Unicode password normalization, length/denylist/history rules, and exact Argon2id parameters;
- activation expiry/attempts, persisted login lockout timing, TOTP replay/freshness, factor reset, and administrator recovery;
- audit hash chains verify and tampering is detected; and
- unknown SBR outcomes never trigger automatic resubmission.

### 13.2 Storage and integration tests

- migration from every supported prior schema;
- encrypted open, close, backup, restore, and wrong-key behaviour;
- process interruption around transaction boundaries;
- WAL recovery;
- domain/audit atomicity;
- concurrent read and bounded writer contention;
- corrupt and wrong-passphrase backup rejection without active-workspace changes;
- staged restore rollback after open failure;
- restore-journal recovery from `PREPARED`, `STAGED`, and `SWAPPED`;
- authorised restore generation, archived prior head, new audit event, and OS mirror update;
- exact canonical data and evidence hashes plus the expected restore generation/event/head;
- passphrase rewrap, recovery rewrap, remembered-workspace expiry, and DEK zeroing;
- closed/unauthenticated/authenticated workspace transitions and five-minute pre-auth timeout;
- audit-head rollback detection against the OS credential-store mirror;
- missing-mirror read-only opening and authorised mirror establishment;
- pre-restore archive authenticated recovery and machine-credential exclusion; and
- OS key-store adapter contract tests.

### 13.3 Contract tests

- Buf format and lint;
- Buf breaking-change checks against the release baseline;
- generated-code cleanliness;
- Connect-Go/Connect-ES interoperability;
- structured error details;
- every use-case-catalogue permission decision;
- all mutating RPCs require idempotency and authorisation;
- read ports prevent cross-module repository/table access; and
- the simulator and production SBR adapter share the same gateway contract suite.

### 13.4 SBR tests

- deterministic XML/XBRL golden fixtures;
- namespaces and attachment identifiers;
- exact schema validation against the selected service package;
- Schematron and published validation cases;
- external entities, DTD retrieval, expansion, and malformed XML rejection;
- correlation and duplicate-response handling;
- response persistence before presentation;
- process or network failure in `PREPARED`, before `DISPATCHING`, during dispatch, after remote acceptance, and after response receipt;
- startup recovery resumes only `PREPARED` and converts orphaned `DISPATCHING` to `UNKNOWN`;
- `NOT_STARTED`, `MAYBE_SENT`, and `RESPONSE_RECEIVED` helper classifications;
- lost, duplicate, and malformed responses;
- preserved ATO error, warning, and information messages;
- EVTE/production rejection of every simulator-only verification, declaration, and response marker;
- `message.ping` when applicable;
- required EVTE and conformance cases; and
- PVT only under an approved, monitored transaction plan.

### 13.5 Electron end-to-end tests

Playwright launches the packaged Electron application and bundled Go service. The critical test:

1. starts the test-signed simulator build with networking disabled and asserts the permanent simulator banner;
2. creates an encrypted workspace, confirms its recovery secret, then locks it;
3. unlocks the workspace and signs in as its unique administrator;
4. assigns that user the additional `business_lodger` role and enrols TOTP;
5. configures an organisation and records `SIMULATOR_FIXTURE` entity verification;
6. creates the five canonical accounts;
7. rejects an unbalanced journal;
8. posts the `$1,100` GST-inclusive sale and `$220` GST-inclusive purchase;
9. verifies `$1,100` trial-balance totals and every exact closing balance;
10. generates and validates `G1=$1,100`, `1A=$100`, `1B=$20`, net payable `$80`;
11. satisfies TOTP and captures the versioned declaration;
12. receives an accepted deterministic simulator response;
13. verifies the report state, response hash, and audit evidence;
14. exits every application process;
15. relaunches, unlocks, and signs in; and
16. proves exact persisted values and audit-chain integrity.

A second packaged E2E creates a backup after step 13, adds a distinguishable later journal, restores the backup, and proves that the later journal is absent while the canonical organisation, journals, balances, `$80` BAS, declaration, accepted simulator transmission, and evidence hashes match the backup. It verifies the new audit generation and single `WORKSPACE_RESTORED` event, the archived pre-restore workspace hash, and the new OS-mirrored head. It also proves the machine-credential vault and prior sessions are absent.

Additional E2E cases cover pending-user activation/expiry, administrator break-glass recovery, preparer submission denial, administrator-without-lodger denial, lodger TOTP enforcement, workspace/session lock transitions, wrong workspace and backup passwords, moved-workspace missing-mirror trust establishment, rejected submission, safe pre-send failure, orphaned-dispatch `UNKNOWN`, reconciliation, keyboard navigation, audit rollback detection, and refusal to start after signed-build-manifest tampering.

## 14. Compliance evidence

The repository contains a requirement traceability matrix with these columns:

- source requirement ID;
- source and version;
- applicability;
- design section;
- implementation component;
- automated test;
- retained evidence;
- owner;
- status; and
- DPO confirmation reference.

The first compliance structure includes:

- business-purpose and intended-user statement;
- DPO request and response register;
- OSF Category D self-assessment;
- data-flow and trust-boundary diagrams;
- threat model;
- machine-credential lifecycle;
- audit event catalogue;
- entity-validation procedure;
- supply-chain and third-party register;
- secure development and release procedure;
- SBOM and vulnerability evidence;
- incident and credential-compromise runbooks;
- backup and recovery exercise evidence;
- ATO artefact manifest;
- conformance result records; and
- compliant marketing wording.

The software can create evidence, but the DSP's legal entity, authorised representatives, ABN, RAM access, DPO registration, security attestations, EVTE access, conformance declaration, PVT coordination, and final whitelisting require authorised human action.

## 15. Delivery decomposition

### Milestone 1: Offline foundation vertical slice

The executable path defined in Section 1, using the deterministic SBR simulator.

Exit criteria:

- clean install on the exact macOS `arm64` and Windows `x86_64` matrix in Section 2.4;
- no internet required for setup, accounting, BAS preparation, audit, backup, or restore;
- all critical unit, integration, contract, and Electron E2E tests pass;
- packaged-app restart persistence passes;
- security baseline checks pass; and
- traceability exists for every implemented requirement.

### Milestone 2: EVTE direct SBR

- import the current restricted artefacts;
- integrate the approved ATO credential/STS component;
- implement `message.ping`, `LDG.List`, `AS.Get`, `AS.Validate`, and `AS.Submit`;
- execute failure-boundary and conformance suites; and
- retain evidence.

Exit requires accepted EVTE/conformance evidence, not merely successful local fixtures.

### Milestone 3: Accounting breadth

Separate focused specifications cover contacts, invoices, bills, payments, bank import, reconciliation, financial reports, period close, and accountant workflows. All reuse the established accounting, audit, identity, and storage boundaries.

### Milestone 4: Production verification and release

- production isolation and signed release candidate;
- incident and support readiness;
- approved PVT where required;
- final self-certification;
- whitelisting; and
- wording limited to "ATO registered software product" or another DPO-approved description.

## 16. Definition of done for this design

The first implementation is complete only when:

1. The packaged Electron app launches its bundled Go service without external infrastructure.
2. The renderer has no direct privileged platform access.
3. The encrypted workspace and recovery model work on macOS and Windows.
4. Unique-user and session controls satisfy the selected Category D baseline.
5. Journal posting and reporting invariants are enforced by the Go domain layer.
6. BAS values are traceable to source lines and rule versions.
7. Declaration and submission state transitions are enforced.
8. The simulator and real-adapter port share an executable contract suite.
9. Audit and business state commit atomically.
10. Ambiguous submission outcomes cannot cause duplicate lodgment.
11. Offline packaged-app E2E and restart-persistence tests pass.
12. The compliance traceability matrix links implementation to retained evidence.
13. No marketing or runtime path implies ATO production approval before whitelisting.

## 17. Authoritative references

- [ATO digital wholesale services conditions of use](https://softwaredevelopers.ato.gov.au/usingourservices/dsp-conditions-use)
- [Requirements for products and services, including Category D](https://softwaredevelopers.ato.gov.au/RequirementsforDSPs)
- [Using Digital ID, RAM and machine credentials](https://softwaredevelopers.ato.gov.au/usingmygovidramandmachinecredentials)
- [Machine to Machine authentication solution](https://softwaredevelopers.ato.gov.au/M2M)
- [RAM: who needs a machine credential](https://info.authorisationmanager.gov.au/manage-authorisations/resources-for-business-software-users-and-providers/create-and-manage-machine-credentials/who-needs-a-machine-credential)
- [RAM Business Machine Certificate terms of use](https://info.authorisationmanager.gov.au/privacy-and-security/terms-and-conditions/terms-of-use-business-machine-certificate)
- [ATO Activity Statements AS.0004 (2025)](https://www.sbr.gov.au/digital-service-providers/developer-tools/australian-taxation-office-ato/activity-statements)
- [ATO common artefacts and reference documents](https://www.sbr.gov.au/digital-service-providers/developer-tools/australian-taxation-office-ato/ato-common-artefacts-and-reference-documents)
- [SBR software development steps](https://www.sbr.gov.au/digital-service-providers/software-development-steps)
- [ATO: completing your BAS for GST](https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/gst/in-detail/managing-gst-in-your-business/reporting-paying-and-activity-statements/completing-your-bas-for-gst/complete-your-bas)
- [ATO: rounding of GST on tax invoices](https://www.ato.gov.au/businesses-and-organisations/gst-excise-and-indirect-taxes/gst/in-detail/rules-for-specific-transactions/invoicing)
- [Electron 43 release](https://www.electronjs.org/blog/electron-43-0)
