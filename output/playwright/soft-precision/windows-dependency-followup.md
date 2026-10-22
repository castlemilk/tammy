# Windows ASAR dependency setup follow-up

22 October 2026. Follows the [descriptor correction](descriptor-verification-followup.md).
All 41 original capture hashes are preserved. No package rebuild is needed locally
for this test-only change.

## Observed failure and its budget

At exact SHA `a390e38469a257d32867dc2c2ccea8381c428329`,
[Windows Server job 119679232033](https://github.com/castlemilk/tammy/actions/runs/39735226452/job/119679232033)
passed all 43 descriptor tests and real canonical evidence verification. Desktop
tests then reported 392 passed, one skipped and one ASAR timeout at the outer 30s
test limit. The log printed `Downloading Electron binary...` during that test.

Inspection of the installed pinned Electron 43.1.1 source confirms its `index.js`
calls `getElectronPath()` immediately. When the binary/path marker is absent it
synchronously spawns `install.js`, inheriting stdio. The ASAR test called
`require("electron")` inside its 30s callback. Dependency download/extraction was
therefore inside the runtime budget, before `runHarnessProcess` could start its
own 20s timer. The log alone does not prove that dependency acquisition was the
only Windows failure or establish a runtime-readiness failure.

## Scoped correction and bounds

The root `pnpm desktop:test` command now prepares the pinned dependency before
starting Vitest. Preparation resolves package metadata without importing Electron's
entry point. An already prepared executable requires no installer. An absent or
incomplete native binary invokes that pinned package's existing installer once,
with a separate **120s maximum**, force termination on expiry, a 256 KiB buffered
output limit and no shell. Zero, negative, infinite and larger deadlines are
rejected. Setup errors retain their original cause, code and available diagnostics.
Success is accepted only after the expected native executable and installed
version/path markers are verified.

The integration test now reads only the prepared binary. Missing/mismatched
dependencies fail explicitly with `ELECTRON_TEST_DEPENDENCY_NOT_READY`; this path
cannot download or enter Electron's JavaScript entry point. Runtime limits remain
**20s harness + 5s close confirmation**, within the unchanged **30s outer test**.
Existing harness tests continue to cover hanging children, force termination,
missing close confirmation and bounded output. No hang is treated as success.

Eleven focused setup regressions cover the old implicit-installer trap, prepared
and absent binaries, native paths/version pins, no-op reuse, installer command
bounds, real asynchronous setup completion, a real hanging child's termination,
real nonzero-exit diagnostics, incomplete installer output and the command wiring.
These tests run in both existing Ubuntu and Windows Server CI scenarios. Native
Windows verification comes from the existing hosted workflow; no new environment,
release permission or manual retry is introduced.

Local checkpoint: the final descriptor/Task/evidence/setup bundle passed **100/100
tests** in 9.93s; typecheck and scoped Biome passed. An earlier full
desktop attempt in the restricted local shell reported 392 passed, one skipped,
one Electron SIGABRT before harness output, and a core-build suite failure opening
`~/Library/Caches/go-build` (`operation not permitted`). That completed attempt
was not retried or escalated. It is not native-runtime passing evidence. Hosted
results for the final pushed evidence-child SHA are recorded in the draft PR.

## Lint inventory, cache and ownership

The complete pinned Biome JSON report contains **861 errors, 387 warnings and 646
infos across 55 tracked paths**, all under `output/`. Every affected file is
byte-identical to the original base. No affected path belongs to this styling PR.
[lint-path-inventory.json](lint-path-inventory.json) lists every path, category,
severity count and last tracked author/commit, without source excerpts or runtime
logs.

| Existing artifact group | Paths | Errors | Warnings | Infos |
| --- | ---: | ---: | ---: | ---: |
| `tammy-income-bas-exports-2026-10-06-d04a5322` | 15 | 15 | 0 | 0 |
| `tammy-income-bas-exports-2026-10-06` | 15 | 15 | 0 | 0 |
| `tammy-invoice-demo` | 2 | 807 | 387 | 646 |
| `tammy-purchase-payments-gst-accepted-9ad0b82b-2026-10-10` | 10 | 11 | 0 | 0 |
| `tammy-purchase-payments-gst-preacceptance-4bb79919-2026-10-10` | 6 | 6 | 0 | 0 |
| `tammy-purchase-payments-gst-preacceptance-755a36d6-2026-10-10` | 7 | 7 | 0 | 0 |

**Cache inclusion is not the lint cause:** no `.pnpm-store` path has a diagnostic.
The cache's 23,293 tracked paths caused the earlier historical Git-output volume,
a separate issue. `.gitignore` already excludes `.pnpm-store`; no ignore rule,
tracked cache state, history or local cache content is changed here.

All 55 files' last tracked change is `e725aa2450bcdde80d15c74b0b07133a103b2e67`,
authored by Ben Ebsworth. No CODEOWNERS file exists. Historical authorship does not
confirm the current artifact owner; a separate cleanup proposal is deferred until
that ownership is confirmed. The original local cache is preserved at
`/Users/benebsworth/projects/tammy/.pnpm-store`, and concurrent worktrees remain
untouched. No broad reformat, cache untracking or deletion is part of this PR.
