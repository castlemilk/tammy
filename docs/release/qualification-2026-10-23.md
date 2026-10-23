# Tammy qualification checkpoint — 23 October 2026

This checkpoint supplements the Soft Precision invoice work at `ff3ddc3005898aa933ae3396b36f04f96efd233a`. It records observed checks and remaining release inputs. It does not change the release feature boundary, self-attest legal declarations, or qualify an ordinary package as a Mac App Store candidate.

## Qualification corrections

The first focused release-tooling run passed 46 of 54 tests. Seven failures came from fixtures created through macOS's `/var` temporary-directory alias. Strict production code correctly rejects aliased release roots, durable-record paths and installed-input paths; data removal correctly returns physical paths. The eighth failure was the active containment probe running inside the tool sandbox.

The same unchanged focused tests passed 53 of 54 with a physical temporary parent. The four containment tests separately passed outside the tool sandbox, retaining actual macOS sandbox enforcement. The corrected fixtures resolve their temporary parent physically. Production verification and cleanup code are unchanged.

A full script run then exposed four public-site fixture failures and one SBR installed-input fixture failure with the same alias cause. The two unchanged files passed all 24 cases with the physical temporary parent. Their fixtures receive the same correction. A stale foundation assertion is also updated to require the financial workspace journey; all prior suites and zero-retry requirements remain required.

The new regression constructs an aliased checkout ancestor and verifies rejection before any command runs or output directory is created, while preserving a sentinel file. This protects the strict production boundary rather than accepting aliased paths.

Results with the normal macOS temporary environment:

- Focused release-tooling suite: **55 passed, zero failures/skips**.
- Complete repository script tier: **788 passed, zero failures, one expected skip**, in 33.11 seconds. The skipped archive-reproducibility case is enabled by the separate `TAMMY_SQLCIPHER_REPRODUCIBILITY=1 pnpm sqlcipher:test` scenario.
- Repository-owned `release:check` passed. Build 5 remains `REPOSITORY_READY` with `CANDIDATE_EVIDENCE_INVALID`.
- The full script tier is now required by `task ci:macos`, before the existing native reproducibility, storage race, development and packaged journeys. No existing gate or retry policy is removed.

Hosted results for this follow-up must be assessed at its exact pushed head. The earlier UI head passed all six hosted jobs in [PR CI](https://github.com/castlemilk/tammy/actions/runs/39873310904) and [push CI](https://github.com/castlemilk/tammy/actions/runs/39873305758), including two 19-case packaged runs with zero retries/skips. Those results are not represented as hosted validation of a later commit.

## Implemented workflow and support boundary

| Area | Current observed coverage | Release/support limit |
| --- | --- | --- |
| Workspace and accounting | Local setup, recovery, sign-in, organisation, journals and trial balance in the packaged journeys | MAS sandbox qualification remains outstanding |
| Documents and banking | Local document review, explicit CSV import and reconciliation | No live feed or automatic posting claim |
| Invoices | Draft editing, templates, reviewed expenses, explicit issuance, one receipt under repeated clicks, cancellation, PDF export, persistence and narrow keyboard navigation | No customer sending, gateway, inventory or Paid/Overdue lifecycle invention |
| Purchases and BAS | Purchase payments/GST claims and local BAS review, freeze, exports and linked revisions in the reporting branch | Simpler GST and supported PAYG scope only; unsupported facts block review/freeze; no ATO lodgement |
| SBR | Authenticated local inputs and deterministic simulator readiness | No official ATO, EVTE or production qualification claim |
| Windows | Existing Windows Server smoke checks | Windows 11 native release qualification remains separate and absent |
| Future contracts | 77 future RPC and 111 future transition entries remain declared | This follow-up does not approve the reporting branch's shipping-surface policy change |

The current public [support](https://tammy-accounting.castlemilk.chatgpt.site/support) and [privacy](https://tammy-accounting.castlemilk.chatgpt.site/privacy) pages loaded in Chrome. Support identifies version 0.1.0, macOS 14+, arm64, reproduction/error details and safe data-removal instructions; it asks users not to send records or secrets. Direct non-browser HTTP probes received 403 responses, so those probes are not recorded as successful route checks or as proof of a browser outage. The Sites connector reports the existing site active and public. No site was republished.

## Required MAS inputs

Existing signing identities are available. Three existing Tammy distribution profiles authenticated with matching installed identities. No matching macOS development profile was found in installed profile locations, Downloads, or the account's all-types/all-platforms profile list; the visible Tammy profiles are App Store distribution profiles. No profile, device registration, certificate or permission was created.

The [release runbook](macos-app-store.md) requires an accountable export-compliance determination before signing. Build 5 has no recorded determination; an intended metadata configuration is not a confirmed declaration. That input has been requested from the operator.

The frozen source check for the earlier UI head bound build 5 to source tree `0608418e06a3349737b9761b3d4a7dae86900d33`. A later candidate must use its own exact clean source identity. It needs development-signed sandbox journeys and canonical store captures, privacy/runtime-egress evidence, a verified distribution package and durable exact-build records before the pre-upload gate can advance. The separate release-scope decision remains unresolved. No signing, agreement acceptance, upload or submission occurred in this follow-up.

All prior screenshot and archive bytes are retained. This change affects test fixtures, test expectations, the canonical CI task and this checkpoint; it does not change financial calculations, authorization, runtime path checks, release policy or application pixels. Private signing inspection data and raw test/runtime logs are kept outside the repository.
