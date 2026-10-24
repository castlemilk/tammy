# Recurring ASAR deadline — bounded phase diagnosis

Trial balance qualification stays open and further UI screens are paused. This
change adds observability; it does not claim a reliability fix or erase failures.
The [structured comparison](asar-phase-diagnosis-2026-10-24.json) contains selected
measurements only. No raw CI/runtime logs, environment values, paths, credentials,
recovery material or trace payloads are committed.

## Original executions

| Exact UI head / original job | Queue | Setup | Canonical checks | Vitest suite | ASAR |
| --- | --- | --- | --- | --- | --- |
| [3defba91 push /120701216864](https://github.com/castlemilk/tammy/actions/runs/40037731966/job/120701216864) | 2s | 88s | 359s | 204.74s | 20,051ms, failed |
| [3defba91 PR /120701230105](https://github.com/castlemilk/tammy/actions/runs/40037736157/job/120701230105) | 2s | 82s | 304s | 163.28s | 5,707ms, passed |
| [a11cf9f2 push /120721010351](https://github.com/castlemilk/tammy/actions/runs/40043885505/job/120721010351) | 2s | 98s | 367s | 207.13s | 20,047ms, failed |
| [a11cf9f2 PR /120721028082](https://github.com/castlemilk/tammy/actions/runs/40043890584/job/120721028082) | 3s | 99s | 432s | 237.49s | 3,287ms, passed |

Each pair shares the exact source tree. Harness/security sources, Vitest
configuration, package manifests and dependency lock are byte-identical to the
prior green Journals head `8cb09e34`. The expected synthetic HTTP 200/body/headers
result was captured in both failed cases; completion still exceeded the deadline.
The passing second PR suite was slower overall than its failing push counterpart,
so overall suite speed does not explain the specific delay. Late timestamped
Electron messages do not establish a long post-result exit hang.

All four use runner 2.338.0 and ubuntu-24.04 image 20261012.334.1. Provisioner
versions match within each pair (20261012.601 then 20261021.618). Runner names differ.
These public facts do not establish physical contention or a host fault.

## Inspected lifecycle, parallelism, paths and cache

- The test already flushes the result and calls `app.exit`. The lifecycle owner
  waits for `close`, drains output after `exit`, kills its owned detached process
  group on timeout and requires confirmed close before temporary-file cleanup.
- The unchanged harness budget is 20s, close confirmation 5s, outer case 30s.
  Exact-result, isolation, headers and failure assertions remain unchanged.
- `desktop:test` uses one Vitest worker. No concurrent-case option is enabled.
  No worker, retry, workflow or timeout policy changes are introduced.
- The ASAR and profile use a fresh `mkdtemp` directory; actual userData realpath is
  asserted. There is no fixed shared writable profile. The prepared Electron
  executable is shared read-only within a job and resolved by pinned metadata.
- `xvfb-run` wraps `desktop:test`; the helper uses its inherited DISPLAY instead
  of starting another display when DISPLAY exists. Pinned Electron acquisition
  and verification run before Vitest and outside the runtime deadline.
- Package-manager caching is disabled in workflow setup. The original logs do
  not expose a causal filesystem/Node compile-cache difference; cold/warm cache
  behavior and physical resources cannot be inferred. No cache is cleared or
  untracked, including the unrelated tracked `.pnpm-store`.

## Narrow phase diagnostics

The child emits fixed numeric markers for body entry, security import, readiness,
protocol installation, fetch/header/body, result write/flush and exit request.
The parent records monotonic receipt, spawn return, deadline, termination, exit
and close. Different parent/child clocks are not subtracted; their relative
measurements distinguish pre-body launch delay from measured child phases.
Runtime-version fields accept only exact numeric Electron/Node versions and known
platform/architecture names. Other context is restricted to public platform,
Node/kernel/core count, the whitelisted GitHub event, DISPLAY/cache option presence
and D-Bus transport class. No environment value or raw path is logged by this summary.

Collection retains each known phase once, bounds line framing to 512 bytes,
discards oversized/noisy lines and recovers on newline. Raw process output keeps
its existing independent 256KiB cap. Summaries are emitted once on success or
failure directly to the test worker output so Vitest successful-console suppression
cannot hide passing comparisons. Diagnostic delivery does not change the original
process outcome. Original untimed markers remain for comparison.

Five focused regressions cover fragmented/invalid/duplicate/oversized markers,
privacy of the summary, exit-versus-drained-close ordering, a correct result still
failing at the unchanged deadline, and missing-close cleanup safety. All 17 original
lifecycle tests remain. Final local validation: **23/23 /two files, 1.77s, zero skips**,
with pinned Electron 43.1.1 on Darwin arm64. The observed local ASAR lifecycle is
**503.176ms**, body entry ~309ms, import ~46ms, readiness ~68ms, result flush ~479ms,
confirmed close ~503ms. This is not a Linux reproduction or reliability repair.
The initial `console.info` transport did not appear on a passing local run; the
bounded direct summary emitter was verified before the diagnostic push.

Only the existing automatic push/PR workflow is used to obtain Linux phase evidence.
No manual dispatch, blind retry, added workflow job, background monitor, package
rebuild loop, release action or security change is included. All 860 screenshots
and 55 archives remain untouched. Trial balance stays at its already recorded
visual/runtime evidence, and publication holds remain. A source fix requires a
demonstrated cause from the phase evidence; a green diagnostic run alone is not
proof that the recurring failure was repaired.
