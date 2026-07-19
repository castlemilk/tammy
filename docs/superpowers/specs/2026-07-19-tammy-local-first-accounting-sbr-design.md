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
│ domain state + audit + outbox │  │ credential + STS + AS4    │
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

`platform.UnitOfWork` owns every write transaction. An application service opens the unit of work, loads aggregates through module repositories, invokes domain behaviour, appends audit and outbox records, and commits once. Domain modules never call `Commit` and never query another module's tables.

Documented application dependencies are:

```text
Identity ───────────────────────────────────────────────┐
Organisations ──────────────────────────────────────┐   │
Accounting ── AccountingReadPort ──→ Tax            │   │
Tax ───────── ReportReadPort ──────→ Evidence        │   │
Identity + Organisations + Tax + Evidence + Artefacts│   │
                         └─────────→ SBR ←────────────┘   │
All command handlers ── AuditAppender + OutboxAppender ←──┘
```

`AuditAppender` and `OutboxAppender` accept the current `UnitOfWork`; they do not create nested transactions. Read ports return immutable application projections rather than repositories or database handles. The composition root supplies in-memory fakes for unit tests, SQLite adapters for integration tests, and the simulator or helper adapter for SBR contract tests.

### 6.2 First-slice use-case catalogue

| RPC/use case | Authorised role | Request → result | Transaction owner and ports | Principal failures |
|---|---|---|---|---|
| `WorkspaceService.CreateWorkspace` | unauthenticated local setup | path, passphrase, first-admin identity/password → pending workspace and one-time recovery secret | Workspace; key store, storage factory | path exists, weak passphrase, invalid administrator, key-store failure |
| `WorkspaceService.ConfirmRecovery` | pending first administrator | prompted recovery-secret groups → active workspace | Workspace; header store, audit | wrong recovery groups, setup expired |
| `WorkspaceService.UnlockWorkspace` | local user at locked app | workspace path, passphrase or remembered-device choice → unlock challenge | Workspace; key store, encrypted storage | wrong key, corrupt header, unsupported schema |
| `WorkspaceService.BackupWorkspace` | `workspace_admin` | destination, backup passphrase → backup manifest | Workspace; all-storage snapshot, audit signer | destination failure, integrity failure |
| `WorkspaceService.RestoreWorkspace` | `workspace_admin` at locked app | backup path, backup passphrase → restored workspace summary | Workspace; staging restore, key store | wrong key, signature failure, incompatible schema |
| `IdentityService.SignIn` | unlocked workspace user | username, password, optional TOTP → session | Identity; user repository, session store | invalid credentials, locked account, factor required |
| `IdentityService.CreateUser` | `workspace_admin` | username, display name, initial roles → user | Identity; audit/outbox | duplicate username, invalid role |
| `IdentityService.AssignRoles` | `workspace_admin` | user ID, complete role set → user | Identity; audit/outbox | last-admin removal, invalid role |
| `IdentityService.EnrolTOTP` | authenticated user | current password → provisioning secret | Identity; factor repository, audit | password invalid, already enrolled |
| `IdentityService.ConfirmTOTP` | enrolling user | one-time code → enabled factor | Identity; factor repository, audit | invalid or replayed code |
| `OrganisationService.CreateOrganisation` | `workspace_admin` | ABN, legal name, GST settings → organisation | Organisations; audit/outbox | invalid ABN format, duplicate ABN |
| `OrganisationService.RecordEntityVerification` | `workspace_admin` | organisation, source metadata, evidence hash → verification | Organisations; evidence blob port, audit/outbox | source invalid, evidence missing, details mismatch |
| `AccountingService.CreateAccount` | `workspace_admin`, `business_preparer` | code, name, type → account | Accounting; audit/outbox | duplicate code, invalid type |
| `AccountingService.PostJournal` | `workspace_admin`, `business_preparer` | date, memo, source lines, tax codes → posted journal | Accounting; tax-code read port, audit/outbox | unbalanced, closed period, invalid account/tax treatment |
| `AccountingService.GetTrialBalance` | any first-slice role | organisation, as-of date → account balances and totals | Read-only Accounting | invalid period, permission denied |
| `TaxService.CreateBASWorkpaper` | `workspace_admin`, `business_preparer` | organisation, period, rule bundle → workpaper with provenance | Tax; AccountingReadPort, OrganisationReadPort, audit/outbox | overlapping report, rule unavailable |
| `TaxService.ValidateBAS` | `workspace_admin`, `business_preparer` | report ID → validation run and report state | Tax; artefact/rule port, audit/outbox | stale source, blocking validation |
| `TaxService.AcceptDeclaration` | `business_lodger` | report ID, declaration version, acknowledgement → declaration | Evidence orchestrator; ReportReadPort, audit/outbox | MFA required, stale report, wrong declaration |
| `SBRService.SubmitActivityStatement` | `business_lodger` | report ID, environment → durable transmission status | SBR orchestrator; identity, organisation, report, declaration, artefact, gateway, audit/outbox | unverified entity, no MFA, invalid state, credential unavailable |
| `SBRService.ReconcileTransmission` | `business_lodger` | transmission ID → reconciled report/transmission state | SBR orchestrator; gateway, audit/outbox | not unknown, inconclusive response, unavailable service |
| `AuditService.VerifyChain` | `workspace_admin`, `auditor` | optional sequence range → integrity result | Read-only Audit | chain mismatch, missing event |
| `AuditService.ExportEvidence` | `workspace_admin`, `auditor` | destination, filters → signed export manifest | Audit; evidence reader, signing key | destination failure, chain invalid |

Queries omitted from the table are read-only projections and still require role checks. Each mutation above commits its domain state, audit event, and outbox event in one `UnitOfWork`.

### 6.3 Idempotency contract

The idempotency scope is `(workspace_id, actor_user_id, fully_qualified_rpc_name, idempotency_key)`. The key is a client-generated UUID and is required on every mutation after workspace creation.

The core stores:

- the scope;
- a deterministic protobuf hash of the semantic request, excluding authentication metadata and the idempotency key;
- execution state;
- committed response or error;
- created time; and
- resulting resource identifier.

A unique database constraint elects one execution. A concurrent duplicate waits for the elected transaction for a bounded period, then either returns the committed result or an `ABORTED` response with retry details; it never runs the command twice. Reusing a key with a different request hash returns `IDEMPOTENCY_CONFLICT`. Financial, report, declaration, transmission, backup, and restore keys are retained for the workspace lifetime. Reversible administrative-command records are retained for at least 30 days.

## 7. Module boundaries

Each module owns its domain rules, schema objects, repository interface, Connect handlers, and tests. A module may depend only on documented application ports, not another module's tables.

### 7.1 Identity

Responsibilities:

- unique local users;
- Argon2id password verification;
- offline TOTP enrolment and verification;
- role assignments;
- failed-login lockout;
- 30-minute inactivity locking;
- remembered-session expiry under 24 hours; and
- authentication and authorisation audit events.

The first slice supports `workspace_admin`, `business_preparer`, `business_lodger`, and `auditor`. Roles are additive. A workspace administrator does not receive lodgment permission unless also assigned `business_lodger`.

| Permission | Admin | Preparer | Lodger | Auditor |
|---|:---:|:---:|:---:|:---:|
| Manage workspace and users | ✓ |  |  |  |
| Manage organisation and verification | ✓ |  |  |  |
| Read accounts, journals, and reports | ✓ | ✓ | ✓ | ✓ |
| Create accounts and post journals | ✓ | ✓ |  |  |
| Prepare and locally validate BAS | ✓ | ✓ |  |  |
| Accept lodgment declaration |  |  | ✓ |  |
| Submit or reconcile SBR |  |  | ✓ |  |
| Read complete audit log | ✓ |  | relevant report only | ✓ |
| Export audit evidence | ✓ |  |  | ✓ |

All users authenticate with a password in Milestone 1. A `business_lodger` must also enrol and satisfy TOTP before accepting a declaration, submitting, or reconciling. Platform passkeys are deferred until a separate cross-platform design confirms offline recovery and Electron platform-authenticator behaviour.

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

### 7.4 Tax

Responsibilities:

- GST tax-code definitions;
- tax treatment captured on journal lines;
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
6. If response bytes are returned, commit the encrypted raw response, parsed result, transmission state, report state, and audit/outbox events atomically before showing any result.
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

Every write transaction includes the domain change, audit record, and durable outbox entry. The next audit hash covers the prior hash plus a canonical representation of the new event. The application exposes verification but no ordinary delete or update operation for audit rows.

There is one monotonically sequenced chain per workspace. Event content uses RFC 8785 JSON Canonicalization Scheme encoding. The chain is:

```text
genesis = SHA-256("tammy-audit-v1" || workspace_id || chain_salt)
event_hash[n] = SHA-256(
  "tammy-audit-event-v1" ||
  event_hash[n-1] ||
  uint64_be(canonical_event_length) ||
  canonical_event_bytes
)
```

The random chain salt and genesis hash are stored in the workspace header. A database uniqueness constraint serialises the sequence. After commit, the latest chain head is mirrored to the OS credential store. A database head ahead of the mirror after a crash may repair the mirror after full verification; a credential-store head ahead of the database signals rollback and locks evidence export.

Workspace creation generates an Ed25519 audit-export keypair. The private key is encrypted under the workspace data-encryption key; the public key and key ID are stored in the workspace header. Export is a ZIP containing canonical `events.jsonl`, `manifest.json`, selected evidence objects, the public key, and `signature.ed25519` over the manifest hash. The app and a small standalone verifier both verify object hashes, event sequence, chain hashes, and signature without database access. Key rotation creates a cross-signed new public key and never rewrites prior events.

### 7.8 Artefacts

Responsibilities:

- immutable imported ATO packages;
- checksums and provenance;
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
- explicit busy timeouts and bounded retry for safe local lock contention;
- backup manifests containing schema and application versions; and
- restore verification before replacing an active workspace.

### 8.1 Workspace-key lifecycle

Workspace encryption and user authentication are distinct:

1. `CreateWorkspace` generates a random 256-bit workspace data-encryption key (DEK).
2. The user chooses a workspace passphrase. Argon2id derives a passphrase key-encryption key (KEK) using a random salt and parameters stored in the header. The initial minimum is 64 MiB memory, 3 iterations, and parallelism no greater than 4, with a startup benchmark permitted to increase but never silently decrease the stored parameters.
3. AES-256-GCM wraps the DEK with the passphrase KEK using a random nonce and authenticated workspace/version metadata.
4. Setup generates a separate random 256-bit recovery secret, displays it once in grouped Base32, and requires the user to confirm selected groups. HKDF-SHA-256 derives a recovery KEK that wraps the same DEK with AES-256-GCM.
5. The unencrypted workspace header contains only format/KDF metadata, salts, nonces, wrapped DEKs, audit public material, and ciphertext hashes. It contains no accounting or identity data.
6. SQLCipher uses the unwrapped DEK to open the database. The DEK is zeroed when the workspace locks or the core exits.

If the user explicitly enables "remember this workspace on this OS account", the DEK is stored as a secret in macOS Keychain or Windows Credential Manager. This deliberately permits workspace decryption without re-entering the workspace passphrase, but it never signs in an application user. The remembered item expires or is deleted within 24 hours and is removed on explicit workspace lock, password-risk action, or user request.

After workspace decryption, each natural person signs in with their own application password and TOTP where required. Milestone 1 permits one active application session at a time. Additional users do not receive or wrap the workspace DEK; an authorised person first unlocks the workspace on the device, then the user authenticates. Changing the workspace passphrase rewraps the same DEK. Recovery rewraps the DEK under a new passphrase and invalidates remembered-device entries.

Losing the passphrase, recovery secret, and any still-valid remembered-device item makes the workspace unrecoverable. The UI states this during setup and backup.

### 8.2 Backup and restore

A `tammy-backup-v1` archive contains:

- a consistent SQLite online-backup snapshot, not a copied live database/WAL pair;
- the workspace header;
- evidence objects referenced by the snapshot;
- the simulator/public artefact bundle needed by the snapshot;
- a canonical manifest of paths, sizes, hashes, schema version, application version, and audit head; and
- the audit-export public key and signature over the manifest.

Backups always exclude the machine-credential vault, credential passwords, remembered-device keys, active sessions, local RPC bootstrap material, and operational logs. Restricted ATO artefacts are included only when their licence permits encrypted backup; otherwise the manifest records the required checksum and restore marks the SBR adapter unavailable until the artefact is reimported.

Backup briefly acquires the workspace write gate, completes the SQLite online snapshot, captures evidence hashes, and then releases the gate. A random 256-bit archive key encrypts the complete archive using chunked AES-256-GCM. A backup-specific passphrase-derived Argon2id KEK wraps the archive key; it does not reuse the workspace passphrase KEK.

Restore never writes over the active workspace in place:

1. decrypt into a new staging directory;
2. verify authenticated encryption, manifest signature, every object hash, database integrity, schema compatibility, and audit chain;
3. invalidate restored sessions and remembered-device references;
4. atomically rename the active workspace to a rollback directory and staging to active;
5. open and verify the restored workspace; and
6. delete the rollback directory only after explicit success.

A wrong backup passphrase or failed verification leaves the active workspace byte-for-byte unchanged. The canonical restore test verifies organisation data, both posted journals, exact account balances, the `$80` BAS result, declaration, accepted simulator transmission, evidence hashes, and audit head. The machine credential remains absent and must be imported again.

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
- `outbox_event`
- `artefact_bundle`
- `service_definition`

## 9. Primary data flows

### 9.1 Posting a journal

1. React collects lines and performs immediate form checks.
2. Electron main sends a generated `PostJournal` Connect request with an idempotency key.
3. The accounting application service verifies identity, permission, period, accounts, amounts, and balance.
4. One SQLite transaction writes the immutable posted journal, journal lines, audit event, and outbox event.
5. The response contains the journal identifier and resulting ledger totals.
6. React invalidates the relevant query caches and renders the committed state.

### 9.2 Preparing a BAS workpaper

1. The user selects an organisation and reporting period.
2. Tax queries posted journal lines and their explicit tax treatments.
3. Versioned mapping rules aggregate source values into BAS fields.
4. Every derived value retains links to contributing journal lines and the rule version.
5. Local validation records errors, warnings, and information separately.
6. A successful run changes the report to `LOCALLY_VALIDATED`.
7. Any later source-data change invalidates the validation and declaration.

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

### 12.1 Local identity

- No shared application accounts.
- Argon2id password hashing with parameters stored per account and reviewed against current guidance.
- Five failed attempts trigger a lockout security event and bounded lockout.
- Inactivity locking occurs at or before 30 minutes.
- Remembered sessions expire in less than 24 hours.
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
- reversal preserves the audit relationship;
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
- audit hash chains verify and tampering is detected; and
- unknown SBR outcomes never trigger automatic resubmission.

### 13.2 Storage and integration tests

- migration from every supported prior schema;
- encrypted open, close, backup, restore, and wrong-key behaviour;
- process interruption around transaction boundaries;
- WAL recovery;
- outbox/domain/audit atomicity;
- concurrent read and bounded writer contention;
- corrupt and wrong-passphrase backup rejection without active-workspace changes;
- staged restore rollback after open failure;
- exact canonical data, evidence hashes, and audit head after restore;
- passphrase rewrap, recovery rewrap, remembered-device expiry, and DEK zeroing;
- audit-head rollback detection against the OS credential-store mirror; and
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
- `message.ping` when applicable;
- required EVTE and conformance cases; and
- PVT only under an approved, monitored transaction plan.

### 13.5 Electron end-to-end tests

Playwright launches the packaged Electron application and bundled Go service. The critical test:

1. starts with networking disabled;
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

A second packaged E2E creates a backup after step 13, adds a distinguishable later journal, restores the backup, and proves that the later journal is absent while the canonical organisation, journals, balances, `$80` BAS, declaration, accepted simulator transmission, evidence hashes, and audit head match the backup manifest. It also proves the machine-credential vault and prior sessions are absent.

Additional E2E cases cover preparer submission denial, administrator-without-lodger denial, lodger TOTP enforcement, session expiry, wrong workspace and backup passwords, rejected submission, safe pre-send failure, orphaned-dispatch `UNKNOWN`, reconciliation, keyboard navigation, and audit rollback detection.

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
9. Audit, outbox, and business state commit atomically.
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
