# Supported-flow qualification assessment — 22 October 2026

Baseline: merged master `9e1395ca06ac8d7aed6b201a82a6309c6e271a37`.
This assessment does not change the production gate or define a new release policy.
The accompanying [gap report](supported-flow-gap-report-2026-10-22.json) lists every
remaining future RPC, its desktop exposure, native binding evidence and transition.

## What the gate is counting

Before this follow-up, the manifest contained 55 future RPCs and 65 future
transitions. These counts do **not** mean 55 missing screens or 65 unimplemented
features. They describe a wider contract surface than the connected desktop app.

All 55 future RPCs are absent from `apps/desktop/src/shared/preload-methods.json`.
Their route labels are planned metadata, not evidence that the route implements
that operation. Examples: the current chart is readable without the unused
GetAccount endpoint; the local activity page is not AuditService chain verification.

The shipped SQLCipher composition is selected by
`services/core/cmd/tammy-core/composition_local_sqlcipher.go` and assembled by
`services/core/internal/app/local_composition_sqlcipher.go` plus the local product
module. A static audit of those concrete handlers distinguishes:

| Future RPC category | Count | Actual gap |
| --- | ---: | --- |
| Native handler method present | 26 | Still no desktop IPC/preload workflow and no complete end-to-end qualification. A method body is not a production-readiness claim. |
| Generated unimplemented fallback in a registered handler | 23 | Native adapter/product integration is missing, as well as desktop integration and qualification. |
| Service absent from the selected native composition | 6 | AuditService is not registered by this local composition; a separate composition type accepting audit handlers does not make it available here. |

The services are Workspace 21, Identity 15, Accounting 10, Audit 6 and Organisation
3. Twenty of the 55 entries are queries. Forty-six have matching non-generated Go
method declarations somewhere in the core; this is **source presence**, not proof
that the shipped composition calls them or that their roles, failures and durable
behaviour are qualified. Unsupported RPCs were not invoked for this audit.

Examples of missing native integration include backup/restore and moved-workspace
trust adapters, opening conversion, journal reversal, period close/reopen, general
ledger, tax-code listing and entity verification. Examples of native-backed but
unconnected desktop paths include explicit locking, identity/user administration,
organisation updates and account update/archive. These require real product work,
not a coverage-label change. Their capability and security decisions remain with
the existing domain contracts and owners.

## Truthful qualification added

The new packaged case `foundation/workspace-recovery-confirmation` calls the
existing create/confirm desktop methods through the normal authenticated IPC
boundary in an isolated synthetic profile. It checks:

1. CreateWorkspace returns PENDING_RECOVERY.
2. A wrong confirmation is rejected; replaying the same setup still returns the
   same pending workspace and version.
3. Correct confirmation returns that workspace as UNAUTHENTICATED.
4. Exact confirmation replay remains UNAUTHENTICATED at the same version.

The native case passed locally in 6.6 seconds, without retries, using the existing
authenticated package whose source is `8daf67c8c712c3dd1a62980ac160e4e1653ec40c`
and core hash is `ed02e5ce9892fb7b523d7ccee4176538592221b80ca5a26daadd1c2870b9f62a`.
The core source is unchanged from baseline. Hosted CI builds the follow-up source
again and must verify the registered case before the evidence is treated as final.

The case is an explicit native IPC contract test, paired with the existing full
setup/recovery/sign-in GUI journey. It does not claim a new recovery UI or general
account-recovery qualification. Automatic retries are disabled for this case so
the current fixture cannot record its recovery-response frames in a retry trace.
Only synthetic inputs are used; response secrets are kept in memory, are not
assertion/report payloads, and their byte buffers are cleared. The browser remains
on the empty setup page, so failure screenshots/video do not show a recovery code.
No new screenshot, trace, runtime log, credential or recovery value is committed.

The existing current-workflows journey already asserts both sides of these real
state changes through decoded responses:

- Document NEEDS_REVIEW → REVIEWED, including the retained reviewed projection.
- Bank statement UNMATCHED → MATCHED → RECONCILED, including summary counts and
  unchanged integer minor-unit balances.

Those three transitions were absent from the transition index, rather than being
future app features. The new document/banking fixture files record the existing
repository transition rules; `transitions:generate` regenerates the index. The
rules are supported by the guarded SQL updates in documents/repository.go and
banking/repository.go. No protobuf wire fields, application controller, financial
calculation, security permission, or checker policy changes are included.

The result is four qualified transitions, with **55 RPCs and 64 transitions still
future**. The production gate must continue failing until those remaining entries
are genuinely qualified.

## Missing tests for supported behaviour

The current app does reopen/unlock and sign in, but the existing journey does not
assert both authoritative WorkspaceState projections for LOCKED → UNAUTHENTICATED
or UNAUTHENTICATED → AUTHENTICATED. An active Session DTO alone is not the
WorkspaceState DTO. Restart also proves a fresh session identifier, rather than
an explicit expired-state projection and old-session rejection. These are narrower
qualification gaps for supported behaviour; they should not be confused with
unimplemented backup, restore, user administration or accounting-control screens.

This follow-up does not promote those states by inference or add GetWorkspaceState
or GetSession to the production preload merely to make tests easier. The next
qualification pass needs a safe, authoritative state observer and complete failure
assertions while keeping the existing production boundary intact.

## Recommended release boundary

Do not publish the current full-contract build as production-qualified. Its
existing gate remains appropriate and is not waived here. The currently connected
product is an offline Australian workspace with local owner setup/reopen, source
document review, explicit CSV matching/reconciliation, ordinary account creation,
manual journals/trial balance, draft reviewed-document GST workpaper and local
activity. It does not establish the wider security administration, backup/restore,
verification/export or accounting-control workflows listed above.

A narrower public release would require an explicit product decision, an enforced
production API boundary and fresh security/compatibility qualification. It cannot
be achieved by calling future entries production, ignoring the checker, or
assuming unused RPCs are harmless. That product/implementation work is outside
this visual uplift. App Store Connect authentication and submission qualification
also remain separate; this task does not retry credentials or upload anything.

## Concurrent ownership and preservation

The separate `issue/sbr-registration-ready` worktree is active at `3a897073` and
has uncommitted core-client, lifecycle, startup-diagnostics and Electron fixture
work. It was inspected read-only. This branch does not modify those files or
import its unmerged features. The new case is in foundation.spec.ts; the existing
current-workflows.spec.ts and shared fixtures are untouched by this follow-up.
All 81 screenshot hashes and the 55 archived evidence files remain protected.
