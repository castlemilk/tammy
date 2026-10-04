# Invoice income and BAS Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development. Steps use checkbox syntax for tracking.

**Goal:** Automatically post issued invoices to business income and expose correct dated sales and receipts in Overview and BAS.

**Architecture:** Keep invoice documents immutable while recording typed journal/posting links and immutable receipts in the same encrypted command transaction. Business and BAS projections read the complete shared sources; no amounts are inferred from displayed invoice lists or private costs.

**Tech Stack:** Go, SQLCipher, additive protobuf/Connect, Electron conveyor, React, Vitest and packaged Playwright.

## Chunk 1: Contracts and accounting

Files: `proto/tammy/v1/{invoices,accounting,tax}.proto`, generated clients, `services/core/internal/{invoices,accounting,localproduct,storage/migrations}/`.

- [x] Write and run failing contract and accounting behavior tests for new income/payment RPCs, balanced income/GST, duplicate prevention, partial payment allocation and closed-period rollback.
- [x] Generate additive contracts and migration 0010. Implement transaction-scoped accounting link/receipt repositories and issue/post/payment commands with authenticated audited idempotency.
- [x] Add typed receivable/payment journal sources, safe revenue account setup, controlled cash allocations and revision updates. Keep private costs out of postings and block generic reversal of typed invoice entries.
- [x] Run focused default and SQLCipher race tests, inspect real ledger balances and exact receipt allocations.

## Chunk 2: BAS and business projection

Files: `services/core/internal/reporting/bas_repository.go`, `services/core/internal/invoices/income_summary.go`, `services/core/internal/localproduct/`, `services/core/internal/reportingcapability/`.

- [x] Write failing tests for G1/1A invoice sources, cash timing, mixed treatments, period boundaries, source staleness/regeneration, legacy links and more than 200 invoices.
- [x] Implement aggregate business income/receipts/outstanding reads. Add saved BAS sales provenance and basis-aware attribution; flag unsupported cash purchase evidence and exclude unsubstantiated credits.
- [x] Enable supported local GST workpaper years for the current workflow without claiming complete BAS lodgement or company return support.
- [x] Run repository/integration/contract checks and review the accounting design independently.

## Chunk 3: Desktop and acceptance

Files: `apps/desktop/src/{shared,main,conveyor,app/features}/`, `apps/desktop/tests/e2e/`.

- [x] Write failing bridge/projection/UI tests, then implement income/payment API wiring, invoice accounting panel and business Overview summary with explicit periods.
- [x] Add BAS sales source rows, basis and staleness guidance, regeneration and safe period inputs. Update issue confirmation to explain that issuing records business income.
- [x] Extend actual packaged accounting acceptance with invoice issue → income/ledger → payment → BAS → restart. Verify draft exclusion and immutable branded export behavior.
- [x] Run desktop/package tests and typechecks, Go checks, protobuf/contracts, lint and independent code review. Commit clean source, package, run E2E, inspect screenshots and record results.

Commands use `rtk` prefixes. JavaScript tooling uses `rtk proxy mise exec -- pnpm …`; Go SQLCipher checks use `rtk proxy mise exec -- go test -race -tags tammy_sqlcipher …`. Source must be committed and clean before packaging.

Final evidence: [Invoice income and BAS acceptance](../../development/invoice-income-bas-acceptance-2026-10-04.md).
