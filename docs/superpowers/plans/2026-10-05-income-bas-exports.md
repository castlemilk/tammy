# Income and BAS exports implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. Track each task below; verify the authorised reporting programme one phase at a time.

**Goal:** Export the displayed income summary and saved GST workpaper to exact, complete PDF and CSV files through the native save boundary.

**Architecture:** Pure reporting renderers consume immutable, validated snapshots. Trusted main acquires the authenticated organisation/report pair with profile-version checks before and after the transactional read, enforces displayed revision/identity, and guards the captured session at every asynchronous save boundary. React supplies source identity, dates and format only. The narrow file writer retains invoice behavior.

**Tech stack:** TypeScript, React/Conveyor, Forme PDF, Electron, existing generated Connect contracts and Go SQLCipher reporting reads.

**Authority:** The user's 2026-10-05 instruction to continue the reporting programme authorises implementing its three linked designs. No ATO submission is authorised. Reuse `issue/sbr-registration-ready`; do not create another checkout.

## Chunk 1: Exact renderers

- [x] Add a focused reporting package with snapshot validation, integer-cent formatting, RFC 4180 CSV escaping and text-formula neutralisation. Use the existing income/BAS read fields; do not invent absent purchase provenance.
- [x] First write and run failing renderer tests for exact/negative/zero totals, identities/dates/currency, Unicode/formula cells, all 1,000 + 1,000 retained rows, empty sources and overflow/16 MiB rejection.
- [x] Add income-summary CSV/PDF and fixed rectangular metadata/totals/sales/purchases/warnings BAS CSV/PDF. Reuse invoice PDF typography/font policy and repeat source table headers. Retain snapshot/revision/acquisition provenance and `Draft — not lodged` identity.
- [x] Parse CSV independently and inspect PDF text/page counts. Verify unsupported glyphs fail PDF explicitly while Unicode CSV remains available.

## Chunk 2: Authenticated export boundary

- [x] Add failing main integration tests for profile changes before/between acquisition reads, retained identity after later profile/ledger changes, income revision mismatch and replaced BAS identity.
- [x] Implement immutable acquisition using GetOrganisation/report/GetOrganisation; compare requested organisation and displayed version for both reads. Compare financial revision or BAS ID/version/period and copy complete bounded sources before rendering.
- [x] Extract only a narrow validated native writer from `apps/desktop/src/main/invoice-export.ts`; retain all invoice export behavior. Guard session after acquisition/dialog/rendering/write and immediately before activation. Reject symlink/directory/invalid paths, write mode 0600 temporary files, fsync and atomically activate; cleanup on all failures and cancellation.
- [x] Test locks/sign-out/workspace changes at each asynchronous boundary, cancellation, symlinks, size limits and temporary cleanup. Retain bounded distinct refresh/render/save errors without paths or private data.

## Chunk 3: Desktop integration

- [x] Add typed bounded income/BAS export requests to the reporting Conveyor module/context and production registration. React cannot supply bytes, templates or paths.
- [x] Add actions to Overview and GST & BAS, retaining authoritative organisation version, selected dates, financial revision and displayed BAS identity. Disable actions while invalid/loading and retain dates on completion/error/cancellation.
- [x] Write renderer integration tests for action payloads, dates, disabled states, session changes and error guidance.
- [ ] Run pinned toolchain reporting tests, invoice regression tests, desktop unit tests/typecheck/build and contract checks appropriate to unchanged read RPCs. Use `rtk proxy mise exec -- ...` from the worktree.

## Chunk 4: Packaged acceptance and handoff

- [ ] Extend the real packaged invoice/accounting journey with all four exports, native cancellation, stale saved workpaper export, explicit regeneration and restart. Compare independently parsed PDF/CSV fields and every source row to authoritative core records; cover cash/non-cash attribution.
- [ ] Run packaged acceptance and existing invoice/template/SBR regression journeys; inspect screenshots and PDF pages and record artifact hashes/source revision/results.
- [ ] Obtain spec compliance review, fix/reverify findings, then code quality review before marking this phase complete. Commit the validated phase and update programme/backlog status with evidence.
- [ ] Proceed to a separate purchase-payment implementation plan, reserving migration 0012 for purchase storage and 0013 for the BAS lifecycle. Do not implement the shared source/storage contracts concurrently.
