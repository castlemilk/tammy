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
4. post a balanced, GST-bearing journal;
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

## 7. Module boundaries

Each module owns its domain rules, schema objects, repository interface, Connect handlers, and tests. A module may depend only on documented application ports, not another module's tables.

### 7.1 Identity

Responsibilities:

- unique local users;
- Argon2id password verification;
- optional offline TOTP and platform passkey adapters;
- role assignments;
- failed-login lockout;
- 30-minute inactivity locking;
- remembered-session expiry under 24 hours; and
- authentication and authorisation audit events.

The first slice supports `workspace_admin`, `business_preparer`, `business_lodger`, and `auditor`. Later agent roles extend the permission catalogue without changing the authentication boundary.

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
  → SUBMITTING
  → ACCEPTED | REJECTED | UNKNOWN | TECHNICAL_FAILURE
```

`PREFILLED` and `ATO_VALIDATED` states become reachable when the real `LDG.List`, `AS.Get`, and `AS.Validate` adapters are enabled. Revisions start a new linked report and require a new declaration.

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
- a random workspace data-encryption key;
- workspace keys wrapped by a key held in the operating-system credential store;
- restrictive filesystem permissions;
- foreign keys enabled;
- WAL mode with checkpoint policy;
- embedded, forward-only migrations;
- a verified encrypted backup before migration;
- periodic integrity checks;
- explicit busy timeouts and bounded retry for safe local lock contention;
- backup manifests containing schema and application versions; and
- restore verification before replacing an active workspace.

The OS credential store is an additional protection layer, not a substitute for the workspace passphrase. A user may create a portable recovery key during setup. Losing both the passphrase/recovery key and the OS-wrapped key makes the encrypted workspace unrecoverable; the UI must state this clearly.

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
2. Tammy verifies organisation status, role permission, report content hash, artefact version, and declaration requirements.
3. The user reviews and accepts the exact declaration.
4. The core commits the declaration and changes the report to `SUBMITTING`.
5. The user unlocks the local machine credential.
6. The core passes the minimum request to the SBR helper.
7. The helper obtains the required token, constructs and signs the current SBR2/ebMS3 message, and sends it directly to EVTE or production.
8. The full response is persisted atomically before downstream processing.
9. The response is correlated using application transaction ID, SBR Message ID, Conversation ID, and payload hash.
10. The report changes to `ACCEPTED`, `REJECTED`, `UNKNOWN`, or `TECHNICAL_FAILURE`.
11. The UI presents ATO messages verbatim, a human explanation where available, and the correlation reference.

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

The overview prioritises cash, receivables, payables, GST position, recent activity, reconciliation work, and the next compliance action. Tables remain dense and keyboard navigable. Risky actions use explicit verbs, show their effect, and require confirmation proportional to the consequence.

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
- TOTP and platform passkeys are supported without a cloud dependency.
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
- idempotency returns one committed result;
- closed periods reject posting;
- amounts round and aggregate according to the versioned rule set;
- BAS field provenance is complete;
- report transitions reject invalid paths;
- changed source data invalidates validation and declaration;
- audit hash chains verify and tampering is detected; and
- unknown SBR outcomes never trigger automatic resubmission.

### 13.2 Storage and integration tests

- migration from every supported prior schema;
- encrypted open, close, backup, restore, and wrong-key behaviour;
- process interruption around transaction boundaries;
- WAL recovery;
- outbox/domain/audit atomicity;
- concurrent read and bounded writer contention;
- corrupt backup rejection; and
- OS key-store adapter contract tests.

### 13.3 Contract tests

- Buf format and lint;
- Buf breaking-change checks against the release baseline;
- generated-code cleanliness;
- Connect-Go/Connect-ES interoperability;
- structured error details;
- all mutating RPCs require idempotency and authorisation; and
- the simulator and production SBR adapter share the same gateway contract suite.

### 13.4 SBR tests

- deterministic XML/XBRL golden fixtures;
- namespaces and attachment identifiers;
- exact schema validation against the selected service package;
- Schematron and published validation cases;
- external entities, DTD retrieval, expansion, and malformed XML rejection;
- correlation and duplicate-response handling;
- response persistence before presentation;
- network failure before, during, and after send;
- lost, duplicate, and malformed responses;
- preserved ATO error, warning, and information messages;
- `message.ping` when applicable;
- required EVTE and conformance cases; and
- PVT only under an approved, monitored transaction plan.

### 13.5 Electron end-to-end tests

Playwright launches the packaged Electron application and bundled Go service. The critical test:

1. starts with networking disabled;
2. creates and locks an encrypted workspace;
3. signs in as a unique user;
4. configures an organisation;
5. creates accounts;
6. rejects an unbalanced journal;
7. posts a balanced GST-bearing journal;
8. verifies ledger and trial balance;
9. generates and validates the BAS workpaper;
10. captures the declaration;
11. receives an accepted simulated response;
12. verifies audit evidence;
13. exits all application processes;
14. relaunches; and
15. proves persisted values and audit-chain integrity.

Additional E2E cases cover role denial, session expiry, wrong workspace password, backup/restore, rejected submission, unknown outcome, keyboard navigation, and optional integration failure.

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

- clean install on supported macOS and Windows versions;
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

