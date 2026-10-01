# Company invoice template implementation plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development. Steps use checkbox syntax for tracking.

**Goal:** Add encrypted company invoice defaults and logo management, with stable saved invoices and correctly branded PDFs.

**Architecture:** Store immutable company logo assets separately from versioned per-organisation defaults. Invoices snapshot text and asset references; export and issue resolve only the saved asset. Keep the existing invoice draft and MCP contract unchanged.

**Tech Stack:** Go, SQLCipher, protobuf/Connect, Electron conveyor, React, TypeScript, Forme PDF, Vitest and Playwright.

## Chunk 1: Storage and contracts

- [x] Add additive template, logo and optional invoice-logo contracts in `proto/tammy/v1/invoices.proto`; regenerate Go and TypeScript.
- [x] Write failing domain/repository/service tests for version-zero defaults, save conflict/idempotency, PNG/JPEG content bounds and MIME checks, organisation isolation, invoice logo preservation/removal and immutable issued invoices.
- [x] Add migration and immutable asset/audit repositories under `services/core/internal/invoices/` and local-product wiring. Keep invoice payload limits unchanged; give logo RPCs bounded independent frame budgets.
- [ ] Run focused Go tests, SQLCipher integration tests, proto formatting/lint and contract checks. Confirm old data loads without a logo.

## Chunk 2: PDF and desktop bridge

- [x] Write failing tests for real PNG/JPEG embedding, aspect ratio, local-only sources, invalid logo failure and privacy exclusions in `packages/invoices/src/`.
- [x] Implement browser-safe logo validation and contained image header; fail if a requested valid logo was silently dropped by Forme.
- [x] Add the four core-client/conveyor methods and shared template/logo projections in `apps/desktop/src/`. Test protobuf bounds, authenticated RPC forwarding and malformed responses.
- [x] Fetch a persisted invoice's logo under session guards for export and issue. Test that changing live defaults cannot affect saved exports and that stale sessions abort asset retrieval/rendering.
- [x] Run invoice package tests/typecheck and focused desktop bridge tests/typecheck.

## Chunk 3: Editor and acceptance

- [x] Write failing UI tests for saved defaults, initial-load races, new draft defaults, explicit apply, logo upload/remove, version conflicts and issued restrictions.
- [x] Add a compact company template editor and logo preview under `apps/desktop/src/app/features/invoices/`; retain the existing table and accounting style.
- [x] Extend `apps/desktop/tests/e2e/` acceptance with real file upload, template persistence, due-date/payment text, PDF image objects, restart and unchanged earlier invoice snapshots after template edits.
- [ ] Run all desktop tests, relevant package tests and typechecks. Review changed code independently and fix verified findings.
- [ ] Commit source to obtain a clean package manifest, build the actual desktop app, and run the packaged acceptance gate. Inspect exported PDF/screenshot visually.
- [ ] Update the dedicated sample workspace with sample logo/defaults and verify export without changing its $160.50 draft total. Record results and commit test/docs evidence.

All shell commands begin with `rtk`; JavaScript tooling uses `rtk proxy mise exec -- pnpm …` from the worktree. Package verification requires a committed clean source revision.

Implementation verification: 783 desktop tests, 32 invoice/PDF tests and 32 local API tests pass. Desktop, invoice and generated-client typechecks pass. Independent review identified an empty JPEG scan and animated PNG policy mismatch; both have regression tests and fixes. Packaged acceptance and sample verification remain pending the clean source build.
