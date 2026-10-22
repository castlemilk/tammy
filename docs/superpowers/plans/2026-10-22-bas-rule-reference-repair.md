# BAS rule-reference closeout implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development for the independent bridge, repair UI and packaged acceptance tasks. Root owns integration, verification and documentation.

**Goal:** Allow both newly created and existing workspaces to use the exact retained bundled tax rules while preserving Core integrity, approval and history requirements.

**Architecture:** Setup pins the canonical bundled source identity. A narrowly scoped administrator action repairs only the known historical checksum error through the existing `UpdateOrganisation` command. Core remains responsible for optimistic concurrency, fresh-factor consumption, effective-date validation and auditing. Retained calculations and frozen reports are never rewritten.

**Tech stack:** Existing protobuf/Connect, Electron Conveyor, React, SQLCipher Core and packaged Playwright acceptance.

Status: implementation and final packaged verification in progress. This is a correction required by lifecycle Task 11, within the already authorised programme.

## Evidence and design

The verified `47f47d1e` packaged application failed its first BAS regeneration. A temporary native read-only diagnostic reproduced the failure in the rule-bundle reader: the organisation's source hash disagreed with the retained bundle. Setup had pinned `e2f9cde094db43c30260dc54f089a1ab835912e75e8c29add5c7a240e90497e4`; the actual bundled bytes and embedded checksum are `1da4e89d4fa785da7591070e8a6054f91df5ed02bb4ec10a0c894a15141506de`. Correcting only that reference in memory made the complete read-only capture pass. The temporary code and copied encrypted fixture were removed.

Both references name `tax_rule_bundle`, ID `018f0000-0000-7000-8000-000000000022`, revision `1`. New setup must use the current checksum. Existing workspaces require an explicit repair because silently editing their organisation profile would bypass its existing approval and audit policy. Merely fixing new setup leaves the existing-workspace journey broken.

The repair control checks the current organisation on demand and offers an action only for the exact known historical source tuple. It displays the current identity/version, requires a workspace administrator, a reason, a future Melbourne civil effective date and a real TOTP assertion with the existing `update_organisation_high_risk` purpose. The mutation sends the exact current organisation version and changes only `active_tax_rule_bundle`. A stale version, invalid date, failed factor or rejected command leaves the panel requiring another review. Other mismatched references require separate investigation rather than automatic replacement.

The reporting capability catalogue and onboarding copy must also describe the implemented supported local preparation path truthfully. Simulator, EVTE and production delivery remain unavailable.

## Integrated acceptance corrections

The ordinary packaged journey exposed a second prerequisite: account TOTP setup was nested inside the SBR-ready surface. An ordinary BAS workspace without an authenticated SBR profile therefore could not enrol the factor required by organisation repair and BAS approval. Expose self-service factor setup for the supported administrator and business-lodger roles independently of SBR readiness. Keep credential administration and simulator actions under their existing environment, role and factor checks. Confirm the actual enabled factor through the authenticated identity query; showing a credential-import button is not the acceptance signal for BAS security.

The legacy-input fixture also required a test-only correction. Conveyor encodes the typed request inside a `google.protobuf.Any` envelope. The fixture must verify its exact type URL, descend into the payload, change only the intended source digest, and forward to the real handler. A regression uses the actual frame codec to prove the exact forwarded bytes and one-shot restoration.

An intermittent pre-window failure initially exposed only `LOCAL_ENGINE_UNAVAILABLE`. Fixed startup-stage and concrete error-code diagnostics now preserve a bounded explanation without retaining raw errors, credentials, readiness content or paths. The failure remains unclassified until a reproduction provides a specific stage/code; it does not justify an unmeasured timeout increase.

## Chunk 1: Canonical setup and bounded bridge

- [x] Reproduce the setup checksum mismatch with a test deriving ID, revision and SHA-256 from the actual bundled bytes; verify RED and GREEN for both GST bases.
- [ ] Centralise the canonical bundled reference and the exact known historical mismatch in `apps/desktop/src/shared/bundled-tax-rule.ts`; keep the setup regression tied to Core's actual bundle bytes.
- [ ] Expose existing `UpdateOrganisation` through `apps/desktop/src/main/core-client.ts` and `apps/desktop/src/conveyor/modules/organisation.ts`, using bounded typed protobuf frames and existing sanitisation.
- [ ] Verify byte fidelity, canonical result decoding and sanitised failures in focused bridge tests; regenerate and validate the desktop manifest/contracts.

## Chunk 2: Explicit repair and truthful capability

- [ ] Add `apps/desktop/src/app/features/bas/legacy-rule-reference-repair.tsx` and mount it in Prepare without changing the BAS workflow state machine.
- [ ] Test exact legacy detection, correct and unrelated references, administrator restriction, exact update mask/version/reason/date, purpose-bound fresh authentication and failed/stale commands.
- [ ] Update the build-pinned capability catalogue to advertise only supported local BAS preparation for its listed business/year combinations; preserve unavailable delivery modes and fail-closed unlisted combinations.
- [ ] Update onboarding copy so it no longer claims the implemented local preparation workflow is unavailable.
- [ ] Make account TOTP setup independent of SBR readiness, test supported roles and unavailable SBR responses, and retain all existing credential and simulator authority checks.

## Chunk 3: Integrated acceptance and closeout

- [ ] In the packaged main journey, reproduce the previous setup input through a one-shot test-only creation-frame substitution while calling the real Core handler. Do not modify the database, fabricate financial facts or replace Core responses.
- [ ] Use the real administrator repair form and TOTP flow, verify the updated organisation identity/version, then complete the full invoice/purchase/PAYG/BAS/frozen export/revision journey.
- [ ] Retain the newly corrected setup path in the other packaged guards and regressions. Verify failed export, same-total stale freeze and stale revision conflict cases.
- [ ] Run focused tests, `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- task verify:quick`, contract/coverage checks and the complete `task package:e2e`; inspect actual PDF/CSV files and screenshots.
- [ ] Complete independent requirement/quality review, record final clean source and artifact hashes, update programme/development status and preserve the worktree for maintainer review.
