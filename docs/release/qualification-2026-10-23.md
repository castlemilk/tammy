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

## Clean-runner follow-up

At `6cc9a53f8c56206975e1cd3b86679f93b418f81f`, both hosted macOS jobs ran the newly required script tier and reported 780 passed, eight failed and one expected skip. Original evidence remains in [PR CI](https://github.com/castlemilk/tammy/actions/runs/39878692136/job/120170535923) and [push CI](https://github.com/castlemilk/tammy/actions/runs/39878632137/job/120170336654). No failed job was manually retried.

Six failures came from tests relying on the local mise bootstrap, which Actions deliberately does not install. CLI tests now execute the already pinned Node directly. Task tests resolve and verify Task 3.52.0, then use a temporary Node-only mise bridge to exercise real Task preconditions and literal arguments without permitting tool installation, builds or signing. Local command contracts still require mise. A controlled run with mise absent passed all 11 CLI/Task cases.

One signing-resource fixture read the workstation's previously generated build manifest. It now constructs its own strict synthetic manifest through the existing manifest owner. Its test passed with the ambient manifest absent; the original generated file was restored byte-identically.

The remaining profile failure required native diagnostics. At `fe987e7d`, macOS reported 785 passed, three failed and one expected skip. One unconverted CLI bootstrap and coloured Task output were corrected. At `af5b0dff`, the script tier reached 786 passed, two failed and one expected skip. Its synthetic-profile diagnostics established that macOS 14 returned exit 1 for every required field extracted as JSON, while XML extraction succeeded for the same array, dictionary and date fields. All selected profile fields now use XML; only a typed date is accepted as the expiry. This preserves selective extraction without converting certificate data, and preserves the prior true-value handling for the all-devices restriction. Task list and report parsing remove only terminal control sequences; malformed JSON and report-field assertions remain strict.

The original diagnostic evidence is retained in [the native run](https://github.com/castlemilk/tammy/actions/runs/39881610891/job/120180024872). A controlled comparison also demonstrated that the old reader accepted an ISO-looking string as an expiry; the corrected reader rejects it. Profile class, identity, expiry, certificate and hash gates are unchanged. Native macOS 14 compatibility must still be confirmed by the corrected exact-head hosted run.

The corrected local full script tier passed **788 cases, zero failures and one expected skip**, in 58.63 seconds. The focused follow-up passed all 68 cases. No application pixels or financial code changed.

After the evidence-based selected-field XML correction, the final full script tier passed **788 cases, zero failures and one expected skip**, in 22.75 seconds. All 59 focused reader/Task/launcher cases also passed with CI colour output enabled. A separate full-tier stress run setting contradictory `NO_COLOR`/`FORCE_COLOR` options produced four strict CLI-stderr failures from Node's own configuration warning; those assertions remain unchanged. That stress experiment is not recorded as a passing supported run or used to relax any gate.

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

The current public [support](https://tammy-accounting.castlemilk.chatgpt.site/support) and [privacy](https://tammy-accounting.castlemilk.chatgpt.site/privacy) pages loaded in both ordinary and anonymous Incognito Chrome sessions. Support identifies version 0.1.0, macOS 14+, arm64, reproduction/error details and safe data-removal instructions; it asks users not to send records or secrets. Direct non-browser HTTP probes received 403 responses, so those probes are not recorded as successful route checks or as proof of a browser outage. The Sites connector reports the existing site active and public. No site was republished.

## Required MAS inputs

Existing signing identities are available. Three existing Tammy distribution profile files authenticated with matching installed identities. No matching macOS development profile was found in installed profile locations, Downloads, or the account's all-types/all-platforms profile list; the visible Tammy profiles are App Store distribution profiles. No profile, device registration, certificate or permission was created.

The [release runbook](macos-app-store.md) requires an accountable export-compliance determination before signing. Build 5 has no recorded determination; an intended metadata configuration is not a confirmed declaration. That input has been requested from the operator.

The frozen source check for the earlier UI head bound build 5 to source tree `0608418e06a3349737b9761b3d4a7dae86900d33`. A later candidate must use its own exact clean source identity. It needs development-signed sandbox journeys and canonical store captures, privacy/runtime-egress evidence, a verified distribution package and durable exact-build records before the pre-upload gate can advance. The separate release-scope decision remains unresolved. No signing, agreement acceptance, upload or submission occurred in this follow-up.

All prior screenshot and archive bytes are retained. This change affects test fixtures, test expectations, portable release-profile date extraction, the canonical CI task and this checkpoint; it does not change financial calculations, authorization, runtime path checks, release policy or application pixels. Private signing inspection data and raw test/runtime logs are kept outside the repository.
