# BAS invoice nomination implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. ROOT alone edits active Go files, runs Go/gofmt/tests/builds and commits. Agents draft under `/tmp/*.pending` or review read-only. Reuse `issue/sbr-registration-ready`.

**Goal:** Produce exact bounded invoice structural candidate identities before financial body hydration.

**Architecture:** A pure invoices-owned mapper validates the complete accepted physical-key reservation and parent graph, emits child identities or disjoint parent fallbacks, then applies the local candidate fence. It chooses no amount, basis, period or eligibility policy and changes no existing reader.

**Tech stack:** Go 1.26.4, native SQLCipher 4.15.0, schema 14.

Design: [invoice nomination](../specs/2026-10-15-bas-invoice-nomination-design.md).

## Chunk 1: Test-first module mapping

Files: create `services/core/internal/invoices/reporting_nomination.go`, `reporting_nomination_test.go`, `reporting_nomination_sqlcipher_test.go`.

- [x] Independently review the design/plan before production changes; distinguish valid undated drafts/zero-valued allocations from synthetic posted/receipt no-child fixtures.
- [x] Draft native real-writer exact identities/counts, full-capture comparison, input/state/transaction ownership, zero allocation and both-basis journeys. ROOT reads and activates tests while no Go process runs.
- [x] ROOT runs native API-absence RED: `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -tags tammy_sqlcipher ./internal/invoices -run '^TestInvoiceBASNomination' -count=1 -timeout=10m` from `services/core`.
- [x] Implement the exact API/types from the spec with complete family/count/charge/graph validation, deterministic order, disjoint fallback and no partial result.
- [x] Add and observe focused behavioral REDs before missing bound/error behavior is implemented: exact 1,000/1,001 candidates, caller allowance, malformed/duplicate/cross-invoice keys and reservation drift. Verify cancellation and input ownership alongside the final implementation. Synthetic key tests establish mapper behavior, not financial body validity.
- [x] ROOT formats and runs focused default/native race checks; resolves only reproduced discrepancies.

## Chunk 2: Reviews and acceptance

- [x] Independent specification review clears the final implementation before independent quality review. Both cleared with no material findings.
- [x] ROOT runs complete affected invoice/reporting native race checks with no nomination/scalar exclusions, existing real TLS invoice/BAS journeys, default Core and pinned production SQLCipher build. Read complete logs/exit statuses and independently verify the binary hash.
- [x] Update the complete-source/lifecycle/programme records with exact evidence and remaining purchase/global boundaries. Check staged scope/whitespace and commit.

The helper receives keys that were already admitted by aggregate scalar/body preflight fences in the future coordinator; it does not itself prove that those fences ran. A later producer must compare hydrated complete physical identities and proofs with these reservations. No contracts, migration, purchase semantics or existing route behavior change in this checkpoint.

## Verification checkpoint (accepted, 2026-10-15)

The first default test compile exposed a fixture dependency on `orgID` declared only by native-tagged tests (`/tmp/tammy-bas-invoice-nomination-api-default-red-2026-10-15.log`). ROOT gave the pure tests their own organisation constant before recording the genuine missing-API default/native REDs (`/tmp/tammy-bas-invoice-nomination-api-default-red-final-2026-10-15.log`, `/tmp/tammy-bas-invoice-nomination-api-native-red-2026-10-15.log`). The first structural mapper then failed actual budget, malformed/duplicate/parent and reservation assertions in 0.552s (`/tmp/tammy-bas-invoice-nomination-behavior-default-red-2026-10-15.log`); ROOT added validation and the local candidate ceiling.

Focused default race passed in 1.626s (`/tmp/tammy-bas-invoice-nomination-default-green-2026-10-15.log`). Focused native race passed in 3.740s (`/tmp/tammy-bas-invoice-nomination-focused-native-first-2026-10-15.log`), selecting all default and three native nomination tests without exclusions. Each real-writer GST-basis journey retained 23 physical keys and exactly 14 structural nominees, independently matching complete reader/writer identities, date/state distinctions, zero allocations and committed fingerprints. Receipt-without-children and 1,000/1,001 native cases establish structural count behavior only. Independent specification then quality review cleared the active sources with no material findings using inherited model settings after requested GPT-6 Luna Max usage was unavailable.

Complete affected invoice/reporting native race exited 0 in 79.187s/20.331s (`/tmp/tammy-bas-invoice-nomination-complete-native-race-2026-10-15.log`), without exclusions. ROOT read its full output. Existing real TLS invoice issuance/posting/payment, BAS save/replay and purchase commit/audit rollback journeys exited 0 in 55.486s (`/tmp/tammy-bas-invoice-nomination-real-tls-final-2026-10-15.log`). These preserve existing routes and do not exercise a combined producer or new workflow endpoint.

All default Core packages exited 0 (`/tmp/tammy-bas-invoice-nomination-default-core-final-2026-10-15.log`). The pinned SQLCipher 4.15.0 production build exited 0 (`/tmp/tammy-bas-invoice-nomination-production-build-final-2026-10-15.log`); ROOT independently verified binary SHA-256 `4bd1e2d3348f90df84550195100b48c91adafbaeb48c99c8f357428c26548e02`. Formatting and staged whitespace/scope checks passed. The only production addition is the pure invoice nomination module; existing readers, routes, contracts and migrations are unchanged. Purchase nomination, exact cross-module union, duplicate-proof verification, global reservations and the combined producer remain open. No overall Task 5 checkbox is complete.
